# Claude Code Master Prompt — In-Memory Cache Engine

Read this entire document. Then read `SPEC.md` and `CHECKLIST.md`. Build in the exact phase order defined in CHECKLIST.md. Do not skip ahead.

---

## What You Are Building

A production-quality in-memory cache engine from scratch with three eviction policies, multiple write strategies, TTL expiration, sharded concurrency, cache coherence simulation, and a live React dashboard with cache-state visualizer.

**Core data-structure chain:**
```
Generic Doubly-Linked List
    ↓ used by
LRU (list + hashmap)
LFU (hashmap of lists keyed by frequency)
ARC (four lists: T1, T2, B1 ghost, B2 ghost)
    ↓ wrapped by
ShardedCache (256 independent shards)
WriteThroughCache / WriteBackCache / WriteAroundCache
    ↓ exposed via
HTTP API (chi) + SSE (live stats)
    ↓ visualized in
React + Framer Motion frontend
```

---

## Non-Negotiable Rules

1. **Phase order is mandatory.** Phase N depends on phase N−1. Tests must pass before continuing.
2. **No external cache libraries.** No `go-cache`, `ristretto`, `bigcache`, etc. Every cache data structure is hand-written.
3. **Lock-free stats.** All Stats counters use `sync/atomic` types (`atomic.Int64`). No mutex on the stats struct.
4. **Race-free code.** Every concurrent component passes `go test -race`. Run it.
5. **ARC must be correct.** The ARC algorithm has specific invariants (see SPEC.md). Write the scan-resistance test explicitly — it's what separates a correct ARC from a broken one.
6. **Framer Motion for visualizer animations.** The LRU linked-list animation using `layoutId` is the signature feature of the frontend. It must work.

---

## Phase 0 — Setup

```bash
mkdir cache-engine && cd cache-engine
go mod init github.com/yourname/cache-engine
go get github.com/go-chi/chi/v5 github.com/stretchr/testify/assert github.com/stretchr/testify/require

# Create structure
mkdir -p cmd/server
mkdir -p internal/{cache/{list,lru,lfu,arc,sharded},ttl,store,stats,coherence,benchmark}
mkdir -p api testdata/workloads

cd web
npm create vite@latest . -- --template react-ts
npm install tailwindcss @tailwindcss/vite framer-motion zustand ky react-resizable-panels
npx shadcn@latest init
npx shadcn@latest add button badge card tabs select slider switch progress tooltip separator scroll-area
```

`Makefile`:
```makefile
.PHONY: build test bench run dev lint

build:
	go build -o bin/server ./cmd/server

test:
	go test ./... -race -count=1 -v

bench:
	go test ./internal/... -bench=. -benchmem -benchtime=5s

run:
	go run ./cmd/server

dev:
	make run & sleep 1 && (cd web && npm run dev)

lint:
	go vet ./...
	cd web && npx tsc --noEmit
```

---

## Phase 1 — Generic Doubly-Linked List

**Write the test file first, then the implementation.**

```go
// internal/cache/list/list.go
package list

type Node[T any] struct {
    Value T
    prev  *Node[T]
    next  *Node[T]
}

type List[T any] struct {
    head *Node[T]
    tail *Node[T]
    len  int
}
```

Critical: `MoveToFront` must handle all four cases: node is already head (no-op), node is tail (update tail pointer), node is middle, and list has only one element (no-op). Off-by-one errors here corrupt all three cache policies.

Test the invariant explicitly:
```go
func checkInvariants(t *testing.T, l *List[int]) {
    t.Helper()
    if l.head != nil {
        assert.Nil(t, l.head.prev, "head.prev must be nil")
    }
    if l.tail != nil {
        assert.Nil(t, l.tail.next, "tail.next must be nil")
    }
    // count nodes and verify matches Len()
    count := 0
    cur := l.head
    for cur != nil { count++; cur = cur.next }
    assert.Equal(t, l.Len(), count)
}
```

Call `checkInvariants` after every operation in tests.

---

## Phase 4 — LRU Cache

**Implementing lazy TTL correctly:**

```go
func (c *LRUCache) Get(key string) ([]byte, bool) {
    c.mu.Lock()
    defer c.mu.Unlock()

    node, ok := c.items[key]
    if !ok {
        c.stats.Misses.Add(1)
        return nil, false
    }

    // Lazy TTL check
    if !node.expiresAt.IsZero() && time.Now().After(node.expiresAt) {
        c.removeNode(node)
        c.stats.Misses.Add(1)
        c.stats.TTLExpiries.Add(1)
        return nil, false
    }

    c.list.MoveToFront(node)
    c.stats.Hits.Add(1)
    return node.value, true
}
```

**Important:** Use a write lock (not read lock) for Get operations because `MoveToFront` mutates the list. A RWMutex with `RLock` in Get would cause a data race.

**Eviction callback:** The LRU cache accepts an optional `OnEvict func(key string, value []byte)` in its config. This is called when a key is evicted (whether by capacity overflow or TTL expiry). The WriteBackCache uses this callback to flush dirty keys to the backing store.

---

## Phase 5 — LFU Cache

**The `minFreq` invariant is subtle.** After incrementing a node's frequency:
```go
func (c *LFUCache) incrementFreq(node *lfuNode) {
    oldFreq := node.freq
    c.freqList[oldFreq].Remove(node)

    // If the old freq list is now empty AND it was the minFreq, increment minFreq
    // (only safe to increment because no new items will have freq < oldFreq+1 now)
    if c.freqList[oldFreq].Len() == 0 {
        delete(c.freqList, oldFreq)
        if oldFreq == c.minFreq {
            c.minFreq = oldFreq + 1
        }
    }

    node.freq++
    if c.freqList[node.freq] == nil {
        c.freqList[node.freq] = list.New[*lfuNode]()
    }
    c.freqList[node.freq].PushFront(node)
}
```

**`minFreq` reset on Set:** When adding a new entry, `minFreq` MUST be reset to 1 — even if there are existing entries with freq=1. This is because a new entry always starts at freq=1.

**Eviction test to write:**
```go
func TestLFU_EvictsLeastFrequent(t *testing.T) {
    c := lfu.New(3)
    c.Set("a", []byte("1"), 0)
    c.Set("b", []byte("2"), 0)
    c.Set("c", []byte("3"), 0)

    // Access 'a' and 'b' multiple times
    c.Get("a"); c.Get("a")  // freq=3
    c.Get("b")              // freq=2
    // 'c' still at freq=1

    // Adding 'd' must evict 'c' (lowest freq)
    c.Set("d", []byte("4"), 0)

    _, hitC := c.Get("c")
    assert.False(t, hitC, "c should be evicted (lowest freq)")

    _, hitA := c.Get("a")
    assert.True(t, hitA, "a should still be present")
}
```

---

## Phase 6 — ARC Cache

**ARC is the hardest component.** Read the original paper (Megiddo & Modha, USENIX FAST 2003) concept if needed. Here is the complete `Set` pseudocode rewritten as Go logic:

```go
func (c *ARCCache) Set(key string, value []byte, ttl time.Duration) error {
    c.mu.Lock()
    defer c.mu.Unlock()

    // Case 1: Key already in cache (T1 or T2) — update in place
    if node, ok := c.itemMap[key]; ok && (node.list == "T1" || node.list == "T2") {
        node.value = value
        node.expiresAt = expiresAt(ttl)
        // Promote to T2 front (treat as second access)
        c.removeFromList(node)
        node.list = "T2"
        c.T2.PushFront(node)
        return nil
    }

    // Case 2: Key is a B1 ghost → increase p, add to T2
    if node, ok := c.itemMap[key]; ok && node.list == "B1" {
        delta := 1
        if c.B2.Len() > c.B1.Len() {
            delta = c.B2.Len() / c.B1.Len()
        }
        c.p = min(c.p+delta, c.capacity)
        c.replace(false)
        c.B1.Remove(node)
        node.value = value
        node.list = "T2"
        node.expiresAt = expiresAt(ttl)
        c.T2.PushFront(node)
        return nil
    }

    // Case 3: Key is a B2 ghost → decrease p, add to T2
    if node, ok := c.itemMap[key]; ok && node.list == "B2" {
        delta := 1
        if c.B1.Len() > c.B2.Len() {
            delta = c.B1.Len() / c.B2.Len()
        }
        c.p = max(c.p-delta, 0)
        c.replace(true)
        c.B2.Remove(node)
        node.value = value
        node.list = "T2"
        node.expiresAt = expiresAt(ttl)
        c.T2.PushFront(node)
        return nil
    }

    // Case 4: Completely new key → add to T1
    // Manage T1 + B1 size
    liveSize := c.T1.Len() + c.T2.Len()
    if c.T1.Len()+c.B1.Len() == c.capacity {
        if c.T1.Len() < c.capacity {
            // Delete LRU of B1 (a ghost; no value, just frees ghost slot)
            lruB1 := c.B1.Back()
            c.B1.Remove(lruB1)
            delete(c.itemMap, lruB1.Value.key)
            c.replace(false)
        } else {
            // T1 is full (B1 is empty) — evict from T1 directly
            lruT1 := c.T1.Back()
            c.T1.Remove(lruT1)
            delete(c.itemMap, lruT1.Value.key)
            c.stats.Evictions.Add(1)
        }
    } else if liveSize >= c.capacity {
        // Total ghost+live exceeds 2*capacity — remove a B2 ghost
        if c.T1.Len()+c.T2.Len()+c.B1.Len()+c.B2.Len() >= 2*c.capacity {
            lruB2 := c.B2.Back()
            if lruB2 != nil {
                c.B2.Remove(lruB2)
                delete(c.itemMap, lruB2.Value.key)
            }
        }
        if liveSize >= c.capacity {
            c.replace(false)
        }
    }

    // Add to T1 front
    node := &arcNode{key: key, value: value, list: "T1", expiresAt: expiresAt(ttl)}
    c.T1.PushFront(node) // using your generic list
    c.itemMap[key] = node
    return nil
}

func (c *ARCCache) replace(preferT2 bool) {
    // If T1 is non-empty AND (T1 is larger than target OR this hit came from B2):
    if c.T1.Len() > 0 && (c.T1.Len() > c.p || (preferT2 && c.T1.Len() == c.p)) {
        // Evict LRU of T1, add as B1 ghost
        lru := c.T1.Back()
        c.T1.Remove(lru)
        lru.Value.value = nil   // ghost: keep key, discard value
        lru.Value.list = "B1"
        c.B1.PushFront(lru)
    } else if c.T2.Len() > 0 {
        // Evict LRU of T2, add as B2 ghost
        lru := c.T2.Back()
        c.T2.Remove(lru)
        lru.Value.value = nil
        lru.Value.list = "B2"
        c.B2.PushFront(lru)
    }
    c.stats.Evictions.Add(1)
}
```

**Scan-resistance test (critical):**
```go
func TestARC_ScanResistance(t *testing.T) {
    // Working set: keys 0-9 (10 hot keys)
    // Scan: keys 100-199 (100 cold keys, scanned once)
    // Cache size: 20 (fits working set + some scans)

    c := arc.New(20)

    // Warm up working set
    for i := 0; i < 10; i++ {
        c.Set(fmt.Sprintf("hot%d", i), []byte("v"), 0)
    }
    // Access working set multiple times (should be in T2)
    for round := 0; round < 5; round++ {
        for i := 0; i < 10; i++ {
            c.Get(fmt.Sprintf("hot%d", i))
        }
    }

    // Scan 100 cold keys (should NOT evict hot keys from T2)
    for i := 0; i < 100; i++ {
        c.Set(fmt.Sprintf("cold%d", i), []byte("v"), 0)
    }

    // Working set should survive the scan
    survived := 0
    for i := 0; i < 10; i++ {
        if _, ok := c.Get(fmt.Sprintf("hot%d", i)); ok {
            survived++
        }
    }
    // ARC should protect at least 7/10 hot keys
    // (LRU would protect 0, LFU might protect all but can get stuck)
    assert.GreaterOrEqual(t, survived, 7,
        "ARC should protect hot working set during sequential scan")
}
```

---

## Phase 7 — TTL Wheel

**Wire the TTL wheel into the cache.** The cache's Set() must call `wheel.Schedule(key, ttl)` and Delete() must call `wheel.Cancel(key)`. The wheel's `onExpire` callback must call `cache.deleteInternal(key)` (bypasses the public Delete stats path to avoid double-counting).

**Important timing issue in tests:** Don't test TTL with `time.Sleep(ttl + 1ms)` — this creates flaky tests. Instead:
```go
func TestTTL_Expiry(t *testing.T) {
    c := lru.New(100, lru.WithTTL(50*time.Millisecond))
    c.Set("k", []byte("v"), 50*time.Millisecond)

    _, hit := c.Get("k")
    assert.True(t, hit, "should hit before TTL")

    time.Sleep(100 * time.Millisecond)  // 2x TTL margin

    _, hit = c.Get("k")
    assert.False(t, hit, "should miss after TTL")
    assert.Equal(t, int64(1), c.Stats().TTLExpiries.Load())
}
```

Use 50ms TTL with 100ms sleep for reliable timing in CI environments.

---

## Phase 8 — Write Policies

**Write-back eviction callback wiring:**

When creating the underlying cache for a WriteBackCache, register an eviction callback:
```go
type WriteBackCache struct {
    inner  Cache          // the actual LRU/LFU/ARC
    store  BackingStore
    dirty  sync.Map       // key → struct{} (concurrent-safe)
    flush  chan string
    done   chan struct{}
}

func NewWriteBack(inner Cache, store BackingStore) *WriteBackCache {
    wb := &WriteBackCache{
        inner: inner,
        store: store,
        flush: make(chan string, 1000),
        done:  make(chan struct{}),
    }
    // Register eviction callback on the inner cache
    inner.SetEvictionCallback(func(key string, value []byte) {
        if _, dirty := wb.dirty.LoadAndDelete(key); dirty {
            // MUST flush synchronously — this is the last chance to save this value
            ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
            defer cancel()
            _ = store.Set(ctx, key, value)
        }
    })
    go wb.flushLoop()
    return wb
}

func (wb *WriteBackCache) flushLoop() {
    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()
    for {
        select {
        case <-ticker.C:
            wb.dirty.Range(func(k, _ any) bool {
                key := k.(string)
                if v, ok := wb.inner.Peek(key); ok {
                    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
                    _ = wb.store.Set(ctx, key, v)
                    cancel()
                    wb.dirty.Delete(key)
                }
                return true
            })
        case <-wb.done:
            return
        }
    }
}
```

---

## Phase 9 — Sharded Cache

**FNV-1a routing:**
```go
import "hash/fnv"

func (s *ShardedCache) shardIndex(key string) uint8 {
    h := fnv.New32a()
    _, _ = h.Write([]byte(key))
    return uint8(h.Sum32())  // auto-truncates to 0-255
}
```

**Capacity distribution:** With `totalCapacity / 256` per shard, a cache of 256 entries gives capacity=1 per shard — be careful in tests. Minimum shard capacity is 1. Test with `totalCapacity >= 256` to avoid surprising behavior.

**Stats aggregation** must iterate all 256 shards. Use a helper:
```go
func (s *ShardedCache) Stats() *stats.Stats {
    agg := &stats.Stats{}
    for i := range s.shards {
        sh := s.shards[i].cache.Stats()
        agg.Hits.Add(sh.Hits.Load())
        agg.Misses.Add(sh.Misses.Load())
        // ... etc
    }
    return agg
}
```

---

## Phase 12 — HTTP API

**SSE implementation:**
```go
func (h *Handler) ServeStatsSSE(w http.ResponseWriter, r *http.Request) {
    storeName := chi.URLParam(r, "store")
    cache, ok := h.registry[storeName]
    if !ok {
        http.Error(w, "store not found", 404)
        return
    }

    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.Header().Set("X-Accel-Buffering", "no")  // disable nginx buffering
    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "streaming not supported", 500)
        return
    }

    ticker := time.NewTicker(500 * time.Millisecond)
    defer ticker.Stop()
    for {
        select {
        case <-r.Context().Done():  // client disconnected
            return
        case <-ticker.C:
            snap := cache.Stats().Snapshot()
            snap.Timestamp = time.Now()
            data, _ := json.Marshal(snap)
            fmt.Fprintf(w, "data: %s\n\n", data)
            flusher.Flush()
        }
    }
}
```

**Benchmark run is async** — it can take 10–30 seconds. Return 202 immediately with a job ID:
```go
type benchmarkJob struct {
    ID      string
    Status  string    // "running", "done", "error"
    Results []BenchmarkResult
}
// store in handler's sync.Map, poll with GET /benchmark/results/{id}
```

---

## Phase 16 — Visualizer (Frontend)

**LRU Framer Motion animation:**

```tsx
import { AnimatePresence, motion } from 'framer-motion';

export function LRUViewer({ entries }: { entries: Entry[] }) {
  return (
    <div className="flex gap-3 overflow-x-auto py-4">
      <AnimatePresence initial={false}>
        {entries.slice(0, 20).map((entry, i) => (
          <motion.div
            key={entry.key}           // React key
            layoutId={entry.key}      // Framer Motion shared layout ID
            initial={{ opacity: 0, x: 60 }}
            animate={{ opacity: 1, x: 0 }}
            exit={{ opacity: 0, x: -60, backgroundColor: '#ef4444' }}
            transition={{ type: 'spring', stiffness: 300, damping: 30 }}
            className={`
              flex-shrink-0 w-36 rounded-lg border p-3
              ${i === 0 ? 'border-green-400 bg-green-50' : 'border-gray-200 bg-white'}
            `}
          >
            <div className="font-mono text-xs text-gray-500 truncate">{entry.key}</div>
            <div className="text-sm font-medium truncate mt-1">{atob(entry.value ?? '')}</div>
            {entry.ttlMs > 0 && (
              <TTLBar ttlMs={entry.ttlMs} />
            )}
          </motion.div>
        ))}
      </AnimatePresence>
      {entries.length > 20 && (
        <div className="flex-shrink-0 w-24 flex items-center justify-center text-gray-400 text-sm">
          +{entries.length - 20} more
        </div>
      )}
    </div>
  );
}
```

The `layoutId` prop is the magic: when the same key appears in a different position on re-render, Framer Motion automatically animates it sliding to its new position. This gives the "cards shifting left on access" effect without any manual animation code.

**TTL countdown bar:**
```tsx
function TTLBar({ ttlMs }: { ttlMs: number }) {
  const [remaining, setRemaining] = useState(ttlMs);
  const [initial] = useState(ttlMs);

  useEffect(() => {
    const start = Date.now();
    const interval = setInterval(() => {
      const elapsed = Date.now() - start;
      setRemaining(Math.max(0, ttlMs - elapsed));
    }, 100);
    return () => clearInterval(interval);
  }, [ttlMs]);

  const pct = (remaining / initial) * 100;
  return (
    <div className="mt-2 h-1 bg-gray-100 rounded">
      <div
        className="h-1 rounded transition-all"
        style={{ width: `${pct}%`, background: pct > 50 ? '#22c55e' : pct > 20 ? '#f59e0b' : '#ef4444' }}
      />
    </div>
  );
}
```

---

## Benchmark Expected Results

When you run `POST /benchmark/run` with:
```json
{ "workload": "zipf", "policies": ["lru","lfu","arc"], "cacheSize": 500, "durationSec": 10 }
```

Expected approximate hit rates with 10,000-key Zipf(s=1.2) workload, cache=500:
- LRU: ~72% hit rate
- LFU: ~88% hit rate  
- ARC: ~89% hit rate

If your LFU or ARC hit rate is worse than LRU, there is a bug in frequency tracking or ARC's p adaptation.

When you run with workload `"sequential"` (sequential scan of 10,000 keys, cache=500):
- LRU: ~5% (catastrophic — every access evicts what was just fetched)
- LFU: ~5% (similar, first-access keys all have freq=1)
- ARC: ~5% initially, but if mixed with a hot working set, ARC recovers

When you run `"scan-resistant"` (80% hot-200 + 20% scan-10000, cache=200):
- LRU: ~45% (scans periodically evict hot keys)
- LFU: ~75% (hot keys accumulate high freq, scans can't displace them)
- ARC: ~80% (adapts — when scan misses hit B1, p decreases to protect T2)

These benchmarks validate correctness. Write assertions:
```go
func TestBenchmark_ZipfHitRates(t *testing.T) {
    // ... run benchmark ...
    assert.Greater(t, arcResult.HitRate, lruResult.HitRate,
        "ARC should outperform LRU on Zipf workload")
    assert.Greater(t, lfuResult.HitRate, 0.80,
        "LFU should achieve >80%% hit rate on Zipf workload")
}
```

---

## Common Pitfalls

**LRU:**
- Using `RLock` for Get — wrong. Get mutates the list (MoveToFront). Use `Lock`.
- Not handling the "key already exists" case in Set — you must update value AND move to front, not add a duplicate.
- Eviction callback not called when TTL expires — only called on capacity eviction, not TTL.

**LFU:**
- `minFreq` not reset to 1 on Set of a new key. This causes the next eviction to wrongly evict a freq>1 key.
- Not checking if `freqList[oldFreq]` is now empty before incrementing `minFreq`. If it's not empty, `minFreq` should NOT change.
- Memory leak: `freqList` map entries never deleted when freq lists become empty. Always `delete(c.freqList, oldFreq)` when the list empties.

**ARC:**
- Not enforcing the invariant `|T1|+|T2| ≤ capacity` before adding a new item. Always call `replace()` before adding.
- p going negative or above capacity. Always clamp: `p = min(max(p+delta, 0), capacity)`.
- Ghost entries consuming memory forever. The total ghost size is bounded by the `|T1|+|B1| ≤ capacity` invariant. Enforce it.
- Forgetting to set `node.value = nil` when creating ghost entries (memory leak if values are large).

**Write-Back:**
- Not flushing dirty keys on `Close()`. On shutdown, always call `Flush()` then stop the goroutine.
- Race condition: key being flushed by background goroutine while simultaneously being evicted. Use `sync.Map` for dirty set (atomic LoadAndDelete) or protect with mutex.

**Sharding:**
- Using `%` for shard routing instead of `&` bitmask. `& 0xFF` is faster and equivalent for power-of-2 shard counts.
- Deadlock: acquiring multiple shard locks in different orders. Never hold more than one shard lock at a time.

**SSE:**
- Not calling `flusher.Flush()` after each event. Without Flush(), events are buffered and the client sees nothing.
- Not checking `r.Context().Done()` — goroutine leaks when client disconnects.
- Frontend `EventSource` not handling `onerror` — connection silently dies after first network hiccup.

---

## Definition of Done

1. `go test ./... -race -count=1` — zero failures, zero races
2. `cd web && npx tsc --noEmit` — zero type errors
3. Dashboard loads with live hit rates updating every 500ms
4. LRU Visualizer: set a key, watch card appear at front; set 20+ keys, watch oldest evicted from right
5. LFU Visualizer: access a key 5 times, watch it move to f=5 column
6. ARC Visualizer: sequential scan, watch p adjust
7. Benchmark: Zipf workload, ARC and LFU both beat LRU (verified in chart)
8. Write-back: set latency slider to 50ms, show write-back dramatically faster than write-through
9. Coherence: set key on node-a, watch it invalidate on node-b and node-c in live SSE feed
10. `make build` produces a working binary
