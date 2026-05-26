# Cache Engine — Internals Reference

Deep-dive reference on how each algorithm works, why certain design choices were made, and what makes each policy correct. Read this when you need to understand *why* the code looks the way it does.

---

## The Generic Doubly-Linked List: Foundation of Everything

All three eviction policies share one data structure: a doubly-linked list with O(1) move-to-front and removal. Getting this right is the most important part of the project.

### Why Not `container/list`?

Go's stdlib `container/list` uses `interface{}` / `any` values, which requires a type assertion on every access and disables compile-time type checking. Our generic `List[T]` uses type parameters (Go 1.18+), giving:
- Type safety: `List[*lruNode]` can only hold `*lruNode`
- No heap allocations for type assertions
- Cleaner code

### Pointer Discipline

The list owns its nodes. The cache's `items` map points into the list's nodes (not copies). This is the core of O(1) operations:

```
items["user:1"] ──────────────────────────────────────────┐
                                                           ↓
list: head ←→ [user:3, v3] ←→ [user:1, v1] ←→ [user:2, v2] ←→ tail
```

When we call `Get("user:1")`, we look up the node pointer in O(1), then call `MoveToFront(node)` which rearranges 4 pointers — also O(1):

```
Before: ... ←→ [user:3] ←→ [user:1] ←→ [user:2] ←→ ...
After:  [user:1] ←→ [user:3] ←→ [user:2] ←→ ...
        ↑ head
```

### The Four MoveToFront Cases

A common bug is forgetting edge cases:

| Scenario | What must change |
|---|---|
| Node is already head | Nothing (early return) |
| Node is the only element | Nothing (early return) |
| Node is the tail | node.prev.next = nil; list.tail = node.prev; insert at head |
| Node is middle | node.prev.next = node.next; node.next.prev = node.prev; insert at head |

After move: `node.prev = nil`, `node.next = old head`, `old head.prev = node`, `list.head = node`.

---

## LRU: Why It Works

LRU exploits **temporal locality** — the observation that recently-used items are likely to be used again soon.

### O(1) Proof

Naive LRU keeps a sorted list. On access, find item and move to front: O(n) find. The hashmap+list trick gives O(1):
- `items[key]` → pointer to node: O(1) average
- `MoveToFront(pointer)`: O(1) pointer manipulation
- Eviction: remove `list.tail`: O(1)

### When LRU Fails: The Scan Problem

Consider a cache of size 3 with a hot working set {A, B, C} and a sequential scan {1, 2, 3, 4, 5}:

```
Initial: cache = [C, B, A]  (all hits)
Scan 1:  cache = [1, C, B]  evict A
Scan 2:  cache = [2, 1, C]  evict B
Scan 3:  cache = [3, 2, 1]  evict C
Access A: MISS — was hot, now gone!
```

LRU treats every element equally — it doesn't know that {A, B, C} were accessed many times and {1, 2, 3} only once. This is exactly what LFU and ARC fix.

---

## LFU: O(1) Frequency Tracking

The naive LFU (min-heap of frequencies) is O(log n) per operation. The O(1) solution by Shah, Mitra, and Matani (2010) uses two hashmaps:

```
itemMap:  key → node (node stores freq, value, pointers into freqList)
freqList: freq → doubly-linked-list of nodes with that frequency
          {1: [E, D, C], 2: [B], 5: [A]}  (front = MRU at that freq)
minFreq:  1  (used for O(1) eviction)
```

**On access of key K (freq f):**
1. Remove node from `freqList[f]`
2. If `freqList[f]` is now empty AND f == minFreq: `minFreq = f+1`
3. Add node to front of `freqList[f+1]`
4. Update node.freq = f+1

**On eviction:**
Remove `freqList[minFreq].Back()` — the LRU item among all items with minimum frequency.

**On new Set:**
Reset `minFreq = 1`. New items always start at freq=1, so the next eviction target must be at freq=1.

### Why minFreq Needs Careful Handling

```
freqList: {1: [D, C, B], 2: [A]}, minFreq=1

Access B: freq[B] becomes 2
freqList: {1: [D, C], 2: [B, A]}, minFreq still 1 (list[1] not empty)

Access C: freq[C] becomes 2
freqList: {1: [D], 2: [C, B, A]}, minFreq still 1

Access D: freq[D] becomes 2
freqList: {1: [], 2: [D, C, B, A]}, minFreq = 2 (list[1] now empty AND 1 == minFreq)

Now: Set("E"): minFreq = 1 (reset), freqList: {1: [E], 2: [D,C,B,A]}
Next eviction: evict from freqList[1].Back() = "E" (just inserted)
```

### When LFU Fails: Frequency Pollution

```
Time 1: user:1 accessed 1000 times (high freq, in cache)
Time 2: workload shifts — user:1 no longer accessed
Time 3: new keys user:2, user:3, ... accessed many times
Problem: user:1 still has freq=1000, never evicted despite being stale
```

ARC handles this by keeping ghost entries and adapting p. W-TinyLFU (Caffeine) handles this with a frequency sketch that decays over time (halve all counts periodically). We don't implement decay, but the benchmark shows this weakness on the Temporal workload.

---

## ARC: Adaptive Between Recency and Frequency

### The Insight

Neither LRU (pure recency) nor LFU (pure frequency) is always best. ARC:
1. Tracks both recently-used (T1) and frequently-used (T2) items
2. Uses ghost lists (B1, B2) to detect what the workload needs
3. Adjusts the split between T1 and T2 (the p parameter) based on observed hit patterns

**Ghost list as a "what would have been a hit" oracle:**
- B1 contains keys recently evicted from T1 (were recently used, but not frequently)
- B2 contains keys recently evicted from T2 (were frequently used)
- If a new Set hits B1: "we should have kept this in T1" → increase p (give more space to T1)
- If a new Set hits B2: "we should have kept this in T2" → decrease p (give more space to T2)

### The p Adjustment Formula

```go
// B1 hit: increase p
delta := max(1, len(B2) / len(B1))
p = min(p + delta, capacity)

// B2 hit: decrease p
delta := max(1, len(B1) / len(B2))
p = max(p - delta, 0)
```

The `|B2|/|B1|` ratio matters: if B2 is much larger than B1, each B2 hit carries more evidence that T2 should be larger (many more items recently evicted from T2 than from T1). A single B2 hit shifts p by more when B2 is large.

### Invariant Verification

At any point, these must hold:
```
|T1| + |T2| ≤ capacity         (cache is not over capacity)
|T1| + |B1| ≤ capacity         (ghost list doesn't explode)
|T2| + |B2| ≤ capacity         (ghost list doesn't explode)
0 ≤ p ≤ capacity               (target is bounded)
```

The `replace()` function maintains the first invariant. The `Set` function's pre-processing (evicting from B1/B2 when they exceed bounds) maintains the others.

### ARC vs LRU on Sequential Scans

```
ARC cache size 20, hot working set of 10 keys, scan of 100 keys:

Initial state after warmup:
T1: []  (hot keys promoted to T2 after second access)
T2: [hot0..hot9] (10 hot keys, all in T2)
B1: []
B2: []
p: 10 (balanced)

Scan begins: cold0, cold1, cold2...

cold0 Set: T1=[cold0], T2=[hot0..hot9], p=10
cold1 Set: T1=[cold1,cold0], T2=[hot0..hot9], p=10
...
cold10 Set: cache full (|T1|+|T2|=20)
  replace(false): |T1|=10 = p=10 → evict from T2? NO: |T1| not > p
  Actually: |T1|=10, p=10 → 10 > 10 is false → evict from T2 (LRU of T2)
  Oops? No — this is the CORRECT behavior: scan keys are in T1 (seen once), hot keys in T2.
  replace() evicts LRU of T2... hot9 goes to B2.

cold11 Set → cold11 is new, |T1|+|B1|=11 < 20 → replace():
  |T1|=10 > p=10? No. Evict LRU of T2 = hot8 → B2.

But then: cold11 scan misses B1 → no p adjustment yet.

When B2 starts filling up with hot keys and next hot key access misses:
  hot9 is now in B2 (ghost). Accessing hot9...
  Wait: hot9 won't be "Set" again, user does Get. Get returns miss.
  But next SET of hot9 (if workload re-heats) → hits B2 → p decreases!

Key insight: The scan resistance requires the HOT keys to be accessed again AFTER the scan,
at which point they hit B2 and shift p to protect T2.
```

This is why the scan-resistance test heats up keys, scans, then re-accesses hot keys and checks survival. ARC doesn't protect hot keys *during* the scan (it can't — it doesn't know future accesses), but it recovers faster than LRU because the ghost lists give it information to adapt.

---

## TTL: Min-Heap vs Time Wheel

We use a min-heap because it's simpler to implement correctly and sufficient for the number of keys we handle (≤ millions).

**Min-heap properties:**
- Schedule: O(log n) — push onto heap
- Cancel: O(log n) — requires position index map, then heap.Remove
- Next expiry: O(1) — peek heap[0]
- Pop expired: O(log n) per item

**The position index map** is essential for O(log n) cancel (otherwise cancel would be O(n) scan):
```go
type ttlHeap struct {
    entries []ttlEntry
    index   map[string]int  // key → current position in entries slice
}

func (h *ttlHeap) Swap(i, j int) {
    h.entries[i], h.entries[j] = h.entries[j], h.entries[i]
    h.index[h.entries[i].key] = i  // update positions after swap
    h.index[h.entries[j].key] = j
}
```

**Why not a time wheel?** A hierarchical time wheel (Hashed and Hierarchical Timing Wheels, Varghese & Lauck 1987) is O(1) for all operations and used by kernel network timers. For cache TTLs:
- We don't need microsecond precision (milliseconds are fine)
- We have relatively few concurrent TTLs (< 100k)
- Implementation complexity: a correct 5-level time wheel is ~400 lines
- Min-heap: ~80 lines

For > 10M concurrent TTLs, switch to a time wheel.

---

## Sharding: Why 256 Shards?

### Lock Contention Model

With a single mutex, N goroutines contending for the same lock have average wait time proportional to `(N-1) × (critical_section_time)`. With K shards (assuming uniform key distribution), each shard has `N/K` goroutines contending, reducing wait time by K.

**Why 256 specifically:**
- Power of 2: shard index = `hash & 0xFF` (single bitwise AND, faster than modulo)
- Memory: 256 × (1 `sync.RWMutex` = 24 bytes) = 6KB overhead — negligible
- Granularity: with 8 goroutines, expected contention per shard = 8/256 ≈ 0.03 goroutines. Essentially zero contention.

### Uniform Distribution Assumption

FNV-1a is fast but not cryptographically random. For typical cache keys (user IDs, URLs, product IDs), it distributes well. For adversarial keys (all identical, or all differing by one byte in the same position), distribution may be poor. In practice, caches see organic keys and FNV-1a works fine.

### The Capacity Trade-off

With 256 shards and totalCapacity=1024:
- Each shard capacity = 4
- A single hot key pattern can fill all slots in one shard

With totalCapacity=256:
- Each shard capacity = 1
- Useless — every Set evicts the previous entry

Use sharding only when `totalCapacity >= 1000` for meaningful cache behavior. The API enforces a minimum of 256 total capacity for sharded caches.

---

## Write Policies: The Consistency vs Performance Trade-off

### Write-Through: Strong Consistency

```
Write path:  store.Set() → cache.Set()
Read path:   cache.Get() → [miss] → store.Get() → cache.Set()

Consistency: Always consistent (write to store first)
Write latency: store_latency + cache_write_latency ≈ store_latency (dominated)
Read latency (hit): cache_latency (~microseconds)
Read latency (miss): store_latency + cache_latency ≈ store_latency
```

If `store.Set()` fails, the cache is NOT updated. This prevents a state where the cache has a value the store doesn't.

### Write-Back: Eventual Consistency

```
Write path:  cache.Set() + mark dirty → return immediately
             [background] dirty_key → store.Set()
Read path:   cache.Get() → [miss] → store.Get() → cache.Set()

Consistency: Eventual (gap between write and flush)
Write latency: cache_write_latency ≈ microseconds (regardless of store latency)
Risk: Data loss if process crashes before flush
```

The critical correctness property: **dirty keys must be flushed before eviction**. If a dirty key is evicted from the cache, we must flush it synchronously before discarding. This is implemented via an eviction callback registered on the underlying cache.

```
Without flush-on-evict:
  1. Set("user:1", "alice") → dirty
  2. Set 1000 more keys → "user:1" evicted from cache
  3. Now: cache doesn't have "user:1", store doesn't have "user:1"
  4. Data is LOST

With flush-on-evict:
  1. Set("user:1", "alice") → dirty
  2. 1000 more Sets → "user:1" about to be evicted
  3. Eviction callback fires → store.Set("user:1", "alice") synchronously
  4. Eviction proceeds → data is SAVED in store
```

### Write-Around: Cache Bypass for One-Time Writes

Use case: bulk import of data that won't be read soon. Writing 1M records through the cache would:
1. Evict all useful cache entries
2. Cache all 1M records (which won't be read again)
3. Degrade cache effectiveness for the next hour

Write-around sends writes directly to the store, leaving the cache untouched. The cache remains warm for the frequently-accessed data.

---

## Cache Coherence: The Multi-Node Problem

In a distributed system, multiple nodes may cache the same key. When one node updates a key, other nodes have stale data. This is the **cache coherence problem**.

### Invalidation vs Update Protocols

**Invalidation:** "I'm changing user:1. Everyone delete your copy."
- Simpler
- Next read causes a cache miss and re-fetches from source
- Avoids sending the new value to nodes that might not need it

**Update:** "I'm changing user:1 to {name: Alice}. Everyone update your copy."
- More complex
- No cache miss after update
- Wastes bandwidth if updated nodes never read the key

We implement invalidation (simpler, more common in practice).

### Event Bus Design

```
Node A writes key K:
  1. cache_A.Set(K, V)
  2. bus.Publish(Event{Type: Invalidate, Key: K, From: "node-a"})

Bus delivers to node-b and node-c:
  3. cache_b.Delete(K)  // node-b invalidates
  4. cache_c.Delete(K)  // node-c invalidates
```

The `From` field prevents a node from processing its own events (would cause it to delete the key it just set).

**Buffered channels prevent cascading failures:** If node-c is slow (processing its channel), node-a should not block waiting for node-c to receive the event. Hence `make(chan Event, 100)` per subscriber. If the buffer fills (node is too slow), the oldest events are dropped — this is acceptable for an invalidation protocol because a later invalidation supersedes an earlier one.

---

## Statistics: Why Atomics Instead of Mutexes

Cache stats are updated on every Get/Set/Delete. With a mutex:
```
Get("key"):
  mu.Lock()       // serialize access
  ... lookup ...
  stats.Hits++    // update under lock
  mu.Unlock()
```

The stats update extends the critical section, reducing throughput. With atomics:
```
Get("key"):
  mu.RLock()
  ... lookup ...
  mu.RUnlock()
  stats.Hits.Add(1)  // concurrent-safe, no lock needed
```

`atomic.Int64.Add(1)` compiles to a single `LOCK XADD` instruction on x86 — no kernel involvement, no context switch, ~5ns. A mutex lock/unlock is ~25ns under contention.

For 10M Get/sec, the difference is:
- Mutex stats: 10M × 25ns = 250ms/sec wasted on stat contention
- Atomic stats: 10M × 5ns = 50ms/sec (10% of an operation per stat update)

---

## Benchmark: Expected Hit Rate Analysis

### Zipf Distribution

Zipf(s=1.2, N=10000) means:
- Most popular key: requested with probability ∝ 1/1^1.2 = 1
- 2nd: ∝ 1/2^1.2 ≈ 0.44
- 10th: ∝ 1/10^1.2 ≈ 0.063
- 100th: ∝ 1/100^1.2 ≈ 0.0063

With cache size 500 and 10000 keys, the cache can hold the top 5% of keys. The top 5% of a Zipf(1.2) distribution accounts for:
```
Sum(1/k^1.2 for k=1..500) / Sum(1/k^1.2 for k=1..10000) ≈ 0.88
```

So optimal hit rate is ~88%. LRU achieves ~72% because it occasionally evicts popular items when less-popular items are accessed (recency bias). LFU and ARC achieve closer to the 88% optimum.

### Sequential Access (Worst Case)

Sequential access of N >> cache_size keys: hit rate = `cache_size / N`. With N=10000, cache=500: hit rate = 5%.

All three policies fail equally on pure sequential access — there is no temporal locality to exploit.

### Why ARC Beats LFU on Temporal Workload

After a workload shift (hot keys change), LFU still has high-frequency counts for stale keys. New hot keys must exceed old hot keys' accumulated frequency to displace them. For a sharp workload shift, this never happens:

```
Before: key_A accessed 1000 times (freq=1000)
After:  key_A never accessed, key_B accessed 50 times (freq=50)
LFU: key_A stays (1000 > 50), key_B gets evicted when cache fills
```

ARC doesn't have this problem. When key_B is evicted and later re-Set (workload repeats), it hits B1 (ghost of T1) and shifts p toward T1, making room for new entries. ARC forgets old history; LFU doesn't.

---

## Memory Layout

Each cache node's memory footprint:

**LRU node:**
```go
type lruNode struct {
    key       string      // 16 bytes (header + pointer)
    value     []byte      // 24 bytes (header + pointer + len)
    expiresAt time.Time   // 24 bytes
    prev      *lruNode    // 8 bytes
    next      *lruNode    // 8 bytes
}
// Total: ~80 bytes per node + key/value heap allocation
```

**LFU node** adds `freq int` (8 bytes) = ~88 bytes.

**ARC node** adds `list string` (16 bytes, using "T1"/"T2"/"B1"/"B2") = ~96 bytes. Plus ghost nodes have nil `value` (only key, list, pointers) = ~52 bytes.

For a cache of 10,000 entries: ~800KB for nodes alone, plus key and value heap allocations. Well within typical memory budgets.

---

## Performance Tuning Checklist

If benchmarks show lower than expected throughput:

**Check lock granularity:**
- Are you using `sync.RWMutex` for LRU? Yes, but only `Lock()` (not `RLock()`) because Get mutates the list. If you switched to a sharded cache, verify shard distribution is uniform.

**Check allocations:**
- `go test -bench=. -benchmem` should show 0 allocs/op for Get hits
- If you see allocations, you're creating closures or copying structs in the hot path

**Check false sharing:**
- `atomic.Int64` fields in the same struct may share a cache line on multi-core
- Pad stats fields with `_ [56]byte` if throughput on 16+ cores is lower than expected

**Check GC pressure:**
- Large caches with many small values cause frequent GC. Pre-allocate node pools if GC pause time exceeds 1ms.
- Consider storing values as `unsafe.Pointer` to interned byte arrays (advanced optimization, not required for v1)