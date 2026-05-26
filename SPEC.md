# In-Memory Cache Engine — Technical Specification

## Overview

A full-stack in-memory cache engine built from scratch in Go with a React + TypeScript frontend. Implements three eviction policies (LRU, LFU, ARC), write-through/write-back/write-around strategies, per-key TTL expiration, sharded locking for concurrency, cache coherence via pub/sub invalidation, and a benchmark suite with real workload generators. The frontend provides a live dashboard, interactive playground, cache state visualizer, and benchmark comparison charts.

---

## Tech Stack

### Backend
| Concern | Choice |
|---|---|
| Language | Go 1.22+ |
| HTTP Router | `chi` v5 |
| Live events | Server-Sent Events (SSE, stdlib) |
| Hashing | `hash/fnv` (stdlib, FNV-1a) |
| Concurrency | `sync.RWMutex`, `sync/atomic` |
| Testing | `testing` stdlib + `testify` |

### Frontend
| Concern | Choice |
|---|---|
| Language | TypeScript 5+ strict |
| Framework | React 18 + Vite 5 |
| Styling | Tailwind CSS v3 |
| UI Components | shadcn/ui |
| Charts | Local SVG chart components |
| Animation | Framer Motion (cache visualizer) |
| State | Zustand |
| Live data | native `EventSource` API (SSE) |
| HTTP client | `ky` |

---

## Project Structure

```
cache-engine/
├── cmd/
│   └── server/
│       └── main.go                  # Entry point, wires all components
├── internal/
│   ├── cache/
│   │   ├── cache.go                 # Cache interface + Entry type
│   │   ├── list/
│   │   │   ├── list.go              # Generic doubly-linked list (used by all policies)
│   │   │   └── list_test.go
│   │   ├── lru/
│   │   │   ├── lru.go               # LRU implementation
│   │   │   └── lru_test.go
│   │   ├── lfu/
│   │   │   ├── lfu.go               # LFU implementation
│   │   │   └── lfu_test.go
│   │   ├── arc/
│   │   │   ├── arc.go               # ARC implementation
│   │   │   └── arc_test.go
│   │   └── sharded/
│   │       ├── sharded.go           # Sharded wrapper (reduces lock contention)
│   │       └── sharded_test.go
│   ├── ttl/
│   │   ├── wheel.go                 # TTL management via min-heap
│   │   └── wheel_test.go
│   ├── store/
│   │   ├── store.go                 # BackingStore interface
│   │   ├── memory.go                # Simulated backing store with configurable latency
│   │   ├── writethrough.go          # Write-through wrapper
│   │   ├── writeback.go             # Write-back wrapper (dirty set + async flusher)
│   │   └── writearound.go           # Write-around wrapper
│   ├── stats/
│   │   ├── stats.go                 # Atomic stats counters
│   │   └── window.go                # Sliding-window time-series (ring buffer of 60 1s buckets)
│   ├── coherence/
│   │   ├── coordinator.go           # Multi-cache invalidation coordinator
│   │   └── bus.go                   # In-process event bus (pub/sub)
│   └── benchmark/
│       ├── runner.go                # Benchmark orchestration
│       ├── workloads.go             # Workload generators
│       └── result.go                # Results aggregation + reporting
├── api/
│   ├── server.go                    # HTTP server + route setup
│   ├── handler.go                   # All HTTP handlers
│   ├── sse.go                       # SSE broadcaster and per-store streams
│   ├── middleware.go                # CORS, logging, recovery
│   └── dto.go                       # Request/Response types
├── web/
│   ├── src/
│   │   ├── main.tsx
│   │   ├── App.tsx
│   │   ├── pages/
│   │   │   ├── Dashboard.tsx        # Multi-cache overview with live stats
│   │   │   ├── Playground.tsx       # Interactive get/set/delete + watch mode
│   │   │   ├── Visualizer.tsx       # Cache internals animation
│   │   │   └── Benchmarks.tsx       # Run benchmarks, compare policies
│   │   ├── components/
│   │   │   ├── visualizer/
│   │   │   │   ├── LRUViewer.tsx    # Animated linked-list view
│   │   │   │   ├── LFUViewer.tsx    # Frequency-bucket column view
│   │   │   │   └── ARCViewer.tsx    # 4-list T1/B1/T2/B2 diagram
│   │   │   ├── StatsCard.tsx        # Hit rate, miss rate, evictions
│   │   │   ├── LiveChart.tsx        # Rolling time-series line chart
│   │   │   ├── BenchmarkChart.tsx   # Policy comparison bar/line chart
│   │   │   ├── WritePolicy.tsx      # Write policy selector + dirty set viewer
│   │   │   └── TTLBadge.tsx         # TTL countdown pill per cache entry
│   │   ├── store/
│   │   │   └── cacheStore.ts        # Zustand store
│   │   ├── hooks/
│   │   │   ├── useSSE.ts            # EventSource hook with reconnect
│   │   │   └── useStats.ts          # Live stats subscriber
│   │   ├── api/
│   │   │   └── client.ts
│   │   └── types/
│   │       └── index.ts
│   ├── package.json
│   ├── vite.config.ts
│   └── tailwind.config.ts
├── testdata/
│   └── workloads/                   # Pre-recorded workload traces for benchmark replay
├── go.mod
├── Makefile
└── README.md
```

---

## Backend: Core Interfaces

### Cache Interface

```go
// internal/cache/cache.go

type Entry struct {
    Key       string
    Value     []byte
    ExpiresAt time.Time  // zero = no TTL
    Freq      int        // used by LFU/ARC for inspection
    CreatedAt time.Time
    HitCount  int64
}

type Cache interface {
    Get(key string) ([]byte, bool)
    Set(key string, value []byte, ttl time.Duration) error
    Delete(key string) bool
    Peek(key string) ([]byte, bool)    // get without affecting eviction order
    Keys() []string                    // snapshot of all live keys
    Len() int                          // number of entries currently held
    Capacity() int
    Stats() *stats.Stats
    Snapshot() []Entry                 // full state snapshot for visualizer
    Purge()                            // remove all entries
    Close()                            // stop background goroutines
}
```

The `Snapshot()` method is what the visualizer uses — it returns ordered entries (MRU→LRU for LRU, grouped by frequency for LFU, by list for ARC) so the frontend can render internal state without knowing about pointers.

### BackingStore Interface

```go
// internal/store/store.go

type BackingStore interface {
    Get(ctx context.Context, key string) ([]byte, error)
    Set(ctx context.Context, key string, value []byte) error
    Delete(ctx context.Context, key string) error
    Flush(ctx context.Context) error   // force flush all dirty entries (write-back only)
}
```

---

## Backend: Eviction Policies

### LRU Cache (`internal/cache/lru`)

**Data structures:**
- `items map[string]*lruNode` — O(1) lookup
- `list *doublyLinkedList[lruNode]` — ordered by recency, head = MRU, tail = LRU

**Operations:**
```
Get(key):
  node = items[key]
  if !found → miss, return nil, false
  if node.expAt set && now > node.expAt → delete, return nil, false  (lazy TTL)
  list.MoveToFront(node)
  stats.Hit()
  return node.value, true

Set(key, value, ttl):
  if key exists:
    update value + expAt
    list.MoveToFront(node)
    return
  if len(items) == capacity:
    evict tail node
    stats.Eviction()
  node = newNode(key, value, ttl)
  list.PushFront(node)
  items[key] = node
  ttlWheel.Schedule(key, ttl)   // if ttl > 0
  stats.Set()

Delete(key):
  node = items[key]
  if !found → return false
  list.Remove(node)
  delete(items, key)
  stats.Delete()
  return true
```

**Complexity:** Get O(1), Set O(1), Delete O(1), Eviction O(1).

### LFU Cache (`internal/cache/lfu`)

**Data structures:**
- `items map[string]*lfuNode` — key → node (carries freq)
- `freqList map[int]*doublyLinkedList[lfuNode]` — freq → list of nodes at that freq
- `minFreq int` — tracks the minimum frequency for O(1) eviction

```
Get(key):
  node = items[key]
  if !found → miss
  if expired → delete, miss
  incrementFreq(node)         // move node from freqList[f] to freqList[f+1]
  if freqList[minFreq] is empty → minFreq++
  return node.value, true

Set(key, value, ttl):
  if key exists → update value, incrementFreq(node), return
  if full → evict tail of freqList[minFreq]
  node = newNode(key, value, freq=1, ttl)
  freqList[1].PushFront(node)
  items[key] = node
  minFreq = 1

incrementFreq(node):
  oldFreq = node.freq
  freqList[oldFreq].Remove(node)
  node.freq++
  if freqList[node.freq] == nil → freqList[node.freq] = newList()
  freqList[node.freq].PushFront(node)
```

**Complexity:** Get O(1), Set O(1), Eviction O(1). This is the O(1) LFU by Shah et al. (2010).

### ARC Cache (`internal/cache/arc`)

ARC (Adaptive Replacement Cache, Megiddo & Modha, IBM 2003) dynamically balances recency (LRU) and frequency (LFU) based on workload patterns.

**Four lists:**
- `T1` — recently used once (in cache, has value)
- `T2` — used multiple times (in cache, has value)
- `B1` — ghost of recently evicted T1 entries (key only, no value)
- `B2` — ghost of recently evicted T2 entries (key only, no value)
- `p` — target size for T1 (0 ≤ p ≤ capacity), adjusted adaptively

**Invariants:**
- `|T1| + |T2| ≤ capacity` (live entries)
- `|T1| + |B1| ≤ capacity`
- `|T2| + |B2| ≤ capacity`
- `|T1| + |T2| + |B1| + |B2| ≤ 2 × capacity`

```
Get(key):
  if key in T1 → move to T2 (promote: seen again), return hit
  if key in T2 → move to T2 front (MRU), return hit
  return miss

Set(key, value, ttl):
  if key in T1 or T2 → update value, call Get logic, return
  
  if key in B1:
    // Recent eviction from T1 → increase p (favor recency)
    p = min(p + max(1, |B2|/|B1|), capacity)
    replace(key, value, targetT2=true)
    move key from B1 to T2
    
  elif key in B2:
    // Recent eviction from T2 → decrease p (favor frequency)
    p = max(p - max(1, |B1|/|B2|), 0)
    replace(key, value, targetT2=true)
    move key from B2 to T2
    
  else:
    // Completely new key → add to T1
    if |T1| + |B1| == capacity:
      if |T1| < capacity: delete LRU of B1, replace()
      else: delete LRU of T1
    elif |T1| + |T2| + |B1| + |B2| >= 2*capacity:
      delete LRU of B2
    replace()
    add to T1 front

replace():
  if |T1| > 0 AND (|T1| > p OR (key in B2 AND |T1| == p)):
    evict LRU of T1 → add key to B1 (ghost)
  else:
    evict LRU of T2 → add key to B2 (ghost)
```

**Why ARC is better than LRU or LFU alone:**
- LRU fails on scan patterns (one large scan thrashes the entire cache)
- LFU fails when hot keys change (high-freq stale entries crowd out new hot keys)
- ARC adapts: when it sees scan misses hitting B2 (frequency ghosts), it shifts p to protect T2. When it sees recency misses hitting B1, it shifts p to protect T1.

**Complexity:** Get O(1), Set O(1), Eviction O(1).

---

## Backend: Sharded Cache (`internal/cache/sharded`)

Wraps any `Cache` implementation with N shards to reduce lock contention.

```go
const NumShards = 256  // power of 2

type ShardedCache struct {
    shards   [NumShards]*shardEntry
    policy   PolicyFactory
    capacity int           // capacity per shard = total / NumShards
    stats    *stats.Stats  // aggregated across shards
}

type shardEntry struct {
    mu    sync.RWMutex
    cache Cache
}

func (s *ShardedCache) shardFor(key string) *shardEntry {
    h := fnv.New32a()
    h.Write([]byte(key))
    return s.shards[h.Sum32()&(NumShards-1)]
}
```

**When to use sharding:** Under concurrent load with many goroutines, a single mutex becomes a bottleneck. 256 shards reduce contention by ~256x assuming uniform key distribution. The tradeoff is that capacity per shard is `total/256` which can cause uneven fill with skewed workloads.

Stats aggregation: each Get/Set/Delete atomically updates the shard-local stats. The top-level `Stats()` call sums all 256 shards' counters.

---

## Backend: TTL Expiration (`internal/ttl`)

### Min-Heap Approach

```go
type TTLWheel struct {
    mu      sync.Mutex
    heap    ttlHeap          // min-heap of {key, expAt}
    index   map[string]int   // key → heap position for O(log n) delete
    onExpire func(key string)
    stop    chan struct{}
}

// heap ordered by expAt ascending (soonest expiry at heap[0])
type ttlEntry struct {
    key   string
    expAt time.Time
}
```

**Goroutine loop:**
```
for {
    if heap.Len() == 0 → sleep 100ms
    next = heap[0].expAt
    sleep until next
    for heap[0].expAt <= now:
        key = heap.Pop().key
        onExpire(key)  // calls cache.Delete(key), updates stats.TTLExpiry
}
```

**Active vs lazy expiration:**
- Lazy: on every `Get()`, check if `node.expAt > 0 && time.Now().After(node.expAt)`. Treat as miss, delete node. Zero extra overhead until entry is accessed.
- Active: background goroutine wakes when next TTL fires, deletes expired entries even if never accessed. Reclaims memory promptly.

Both are implemented. Active uses the min-heap. Lazy is built into Get().

---

## Backend: Write Policies (`internal/store`)

### Simulated Backing Store

```go
type MemoryStore struct {
    mu      sync.RWMutex
    data    map[string][]byte
    latency time.Duration  // configurable simulated I/O latency (default 5ms)
}
```

Sleeping `latency` duration on each operation simulates real database round-trip time. This makes write-through vs write-back differences visible in benchmarks.

### Write-Through

```go
type WriteThroughCache struct {
    cache  Cache
    store  BackingStore
}

func (w *WriteThroughCache) Set(key string, value []byte, ttl time.Duration) error {
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    if err := w.store.Set(ctx, key, value); err != nil {
        return fmt.Errorf("backing store write failed: %w", err)
    }
    return w.cache.Set(key, value, ttl)
}

func (w *WriteThroughCache) Get(key string) ([]byte, bool) {
    if v, ok := w.cache.Get(key); ok {
        return v, true
    }
    // Cache miss: read-through from backing store
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    v, err := w.store.Get(ctx, key)
    if err != nil { return nil, false }
    _ = w.cache.Set(key, v, 0)  // populate cache
    return v, true
}
```

**Properties:** Strong consistency (cache and store always in sync). Write latency = store latency. Good for read-heavy workloads.

### Write-Back (Write-Behind)

```go
type WriteBackCache struct {
    cache    Cache
    store    BackingStore
    dirty    map[string]struct{}  // keys with unsaved changes
    dirtyMu  sync.Mutex
    flushCh  chan string           // signals keys to flush
    done     chan struct{}
}
```

Background flusher goroutine:
```
loop:
  key = <-flushCh  (or batch after 100ms timer)
  store.Set(ctx, key, cache.Peek(key))
  dirty.delete(key)
```

`Peek()` is used (not `Get()`) to avoid affecting eviction order during flush.

**On eviction callback:** When the cache evicts a dirty key, it MUST flush to store synchronously before discarding. The eviction callback:
```go
func (w *WriteBackCache) onEvict(key string, value []byte) {
    w.dirtyMu.Lock()
    if _, dirty := w.dirty[key]; dirty {
        delete(w.dirty, key)
        w.dirtyMu.Unlock()
        ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
        _ = w.store.Set(ctx, key, value)  // must not lose data
    } else {
        w.dirtyMu.Unlock()
    }
}
```

**Properties:** Write latency = O(1) (just cache write + mark dirty). Eventual consistency. Data loss risk on crash before flush. Good for write-heavy workloads.

### Write-Around

Writes bypass the cache entirely and go directly to the backing store. Reads are still cache-served. Used when write data is unlikely to be re-read soon (e.g., bulk inserts, log data).

```go
func (w *WriteAroundCache) Set(key string, value []byte, _ time.Duration) error {
    ctx, _ := context.WithTimeout(context.Background(), 2*time.Second)
    return w.store.Set(ctx, key, value)  // cache NOT updated
}
```

---

## Backend: Cache Coherence (`internal/coherence`)

Simulates a multi-node cache scenario where multiple cache instances must be kept consistent.

### Event Bus

```go
type EventBus struct {
    mu          sync.RWMutex
    subscribers map[string][]chan Event
}

type Event struct {
    Type  EventType  // Invalidate, Update, Flush
    Key   string
    Value []byte
    From  string    // node ID that originated the event
}

type EventType int
const (
    EventInvalidate EventType = iota
    EventUpdate
    EventFlush
)
```

### Coordinator

```go
type Coordinator struct {
    nodes  map[string]Cache   // nodeID → cache instance
    bus    *EventBus
    self   string             // this node's ID
}

// When this node writes a key, publish invalidation to all other nodes
func (c *Coordinator) Set(key string, value []byte, ttl time.Duration) error {
    if err := c.nodes[c.self].Set(key, value, ttl); err != nil { return err }
    c.bus.Publish(Event{Type: EventInvalidate, Key: key, From: c.self})
    return nil
}

// Other nodes receive invalidation and delete the key
func (c *Coordinator) handleEvent(e Event) {
    if e.From == c.self { return }  // ignore own events
    switch e.Type {
    case EventInvalidate:
        c.nodes[c.self].Delete(e.Key)
    case EventUpdate:
        c.nodes[c.self].Set(e.Key, e.Value, 0)
    }
}
```

The frontend visualizes this as a 3-node grid: set a value on node A, watch it invalidate on nodes B and C in real time (via SSE).

---

## Backend: Statistics (`internal/stats`)

### Atomic Counters

```go
type Stats struct {
    Hits            atomic.Int64
    Misses          atomic.Int64
    Sets            atomic.Int64
    Deletes         atomic.Int64
    Evictions       atomic.Int64
    TTLExpiries     atomic.Int64
    BytesStored     atomic.Int64
    WriteStoreOps   atomic.Int64  // backing store writes
    ReadStoreOps    atomic.Int64  // backing store reads (cache misses)
    StoredAt        time.Time
}

func (s *Stats) HitRate() float64 {
    h, m := s.Hits.Load(), s.Misses.Load()
    if h+m == 0 { return 0 }
    return float64(h) / float64(h+m)
}

func (s *Stats) Snapshot() StatsSnapshot { ... }
```

### Sliding Window (Time Series)

```go
type WindowedStats struct {
    mu      sync.Mutex
    buckets [60]StatsBucket  // one per second, ring buffer
    head    int              // current bucket index
    ticker  *time.Ticker
}

type StatsBucket struct {
    Timestamp time.Time
    Hits      int64
    Misses    int64
    Evictions int64
}
```

The SSE endpoint streams the last 60 seconds of windowed stats every 500ms — this feeds the rolling line chart on the dashboard.

---

## Backend: Benchmark Suite (`internal/benchmark`)

### Workload Generators

```go
type Workload interface {
    Name() string
    Next() Operation
    Reset()
}

type Operation struct {
    Type  OpType  // Get or Set
    Key   string
    Value []byte
}
```

**Workloads:**

`SequentialWorkload` — keys 0, 1, 2, … N in order, cycling. Tests cache with capacity < N. LRU performs worst (constant eviction of the "oldest" key which is needed next cycle).

`UniformRandomWorkload` — uniform random over key space 0..N. All policies perform similarly.

`ZipfWorkload` — Zipf(s=1.2) distribution over key space. ~20% of keys get ~80% of accesses. LFU and ARC significantly outperform LRU.
```go
func (z *ZipfWorkload) Next() Operation {
    key := strconv.Itoa(int(z.zipf.Uint64()))  // z.zipf from math/rand.NewZipf
    return Operation{Type: Get, Key: key}
}
```

`TemporalWorkload` — hot set shifts every 5000 operations. Simulates changing popularity. ARC adapts, LFU suffers (stale frequency counts).

`ScanResistantWorkload` — 80% small hot working set + 20% sequential scans. LRU gets thrashed by scans; ARC detects scan pattern (keys hit B1 repeatedly) and protects T2.

`WriteHeavyWorkload` — 80% Set, 20% Get. Tests write policy performance difference.

### Runner

```go
type BenchmarkConfig struct {
    Duration    time.Duration  // default 10s
    Concurrency int            // goroutines, default 4
    CacheSize   int            // entries
    Policies    []PolicyType   // lru, lfu, arc
    Workload    WorkloadType
    WritePolicy WritePolicyType
}

type BenchmarkResult struct {
    Policy       PolicyType
    Workload     WorkloadType
    HitRate      float64
    OpsPerSec    float64
    AvgLatencyNs int64
    P99LatencyNs int64
    Evictions    int64
    TotalOps     int64
}
```

Results are returned as `[]BenchmarkResult` (one per policy), enabling the frontend to render side-by-side policy comparison charts.

---

## API Specification

Base URL: `http://localhost:8080/api`

All store names: `lru`, `lfu`, `arc`, `lru-sharded`, `lfu-sharded`, `arc-sharded`

### Cache Operations

**`GET /cache/{store}/{key}`**
```json
// 200 hit
{ "key": "user:42", "value": "eyJpZCI6NDJ9", "hit": true, "ttlMs": 4823, "freq": 7 }
// 200 miss
{ "key": "user:42", "hit": false }
```

**`PUT /cache/{store}/{key}`**
```json
// Request
{ "value": "eyJpZCI6NDJ9", "ttlMs": 60000 }
// Response
{ "key": "user:42", "evicted": false, "writePolicy": "write-through", "storeLatencyMs": 4 }
```

**`DELETE /cache/{store}/{key}`**
```json
{ "key": "user:42", "found": true }
```

**`GET /cache/{store}/stats`**
```json
{
  "policy": "lru",
  "capacity": 1000,
  "size": 847,
  "hitRate": 0.923,
  "hits": 45231,
  "misses": 3791,
  "evictions": 1204,
  "ttlExpiries": 88,
  "bytesStored": 2097152,
  "opsPerSec": 12450.3,
  "writePolicy": "write-through",
  "sharded": false,
  "shardCount": 1
}
```

**`GET /cache/{store}/snapshot`**

Returns full internal state for the visualizer:
```json
{
  "policy": "lru",
  "entries": [
    { "key": "user:1", "sizeBytes": 48, "ttlMs": 59012, "freq": 1, "position": 0, "list": "T1" },
    ...
  ],
  "ghostEntries": [
    { "key": "user:99", "list": "B1" }
  ],
  "arcP": 312,
  "listSizes": { "T1": 200, "T2": 647, "B1": 153, "B2": 312 }
}
```

**`DELETE /cache/{store}/purge`** — remove all entries

**`POST /cache/{store}/config`** — update capacity, TTL, write policy at runtime
```json
{ "capacity": 2000, "defaultTtlMs": 30000, "writePolicy": "write-back", "storeLatencyMs": 10 }
```

### Coherence

**`POST /coherence/set`** — write through coordinator (broadcasts invalidation)
```json
{ "key": "user:1", "value": "...", "node": "node-a" }
```

**`GET /coherence/nodes`** — state of all 3 nodes

### Benchmarks

**`POST /benchmark/run`**
```json
{
  "workload": "zipf",
  "policies": ["lru", "lfu", "arc"],
  "cacheSize": 500,
  "durationSec": 10,
  "concurrency": 4,
  "writePolicy": "write-through"
}
```

**`GET /benchmark/results`** — latest results
```json
{
  "results": [
    { "policy": "lru", "hitRate": 0.71, "opsPerSec": 234000, "p99LatencyNs": 4200 },
    { "policy": "lfu", "hitRate": 0.89, "opsPerSec": 218000, "p99LatencyNs": 5100 },
    { "policy": "arc", "hitRate": 0.91, "opsPerSec": 201000, "p99LatencyNs": 6200 }
  ],
  "workload": "zipf",
  "cacheSize": 500,
  "durationSec": 10
}
```

### SSE (Live Stats)

**`GET /sse/stats/{store}`** — streams stats every 500ms
```
Content-Type: text/event-stream

data: {"hitRate":0.921,"hits":312,"misses":27,"evictions":5,"opsPerSec":8234,"timestamp":"2024-01-15T10:23:45Z"}

data: {"hitRate":0.924,...}
```

**`GET /sse/coherence`** — streams coherence events
```
data: {"event":"invalidate","key":"user:42","from":"node-a","to":["node-b","node-c"],"timestamp":"..."}
```

---

## Frontend: Page Specifications

### Dashboard (`/`)

Three-column card grid, one card per policy (LRU / LFU / ARC).
Each card shows:
- Hit rate gauge (semicircle, updates live via SSE)
- Ops/sec counter
- Entry count / capacity bar
- Evictions in last 60s (sparkline)
- Write policy badge
- Quick link to that policy's visualizer

Bottom section: side-by-side rolling line chart (60s window) overlaying all 3 policies' hit rates. Feed from SSE.

### Playground (`/playground`)

Left panel: operation form
- Policy selector dropdown
- Key input (text)
- Value input (text, base64 encoded on submit)
- TTL input (ms, 0 = no expiry)
- Buttons: GET / SET / DELETE / PEEK
- Result display: hit/miss badge, value, TTL remaining, freq count

Right panel: operation history log (last 50 ops)
- Each row: timestamp, op type, key, hit/miss, latency

Watch mode toggle: poll GET every 200ms for a given key, show TTL countdown bar live.

Write policy panel:
- Current write policy badge
- For write-back: show dirty key count + last flush time + manual flush button
- Store latency slider (1ms – 100ms) to make write-through vs write-back difference visible

### Visualizer (`/visualizer`)

Tabs: LRU | LFU | ARC

**LRU tab:** Horizontal scrolling list of entry cards, left = MRU, right = LRU.
- Cards show: key (truncated), value preview, TTL countdown bar
- On GET hit: card pulses green, slides to leftmost position (Framer Motion `layoutId`)
- On SET: new card appears at left with slide-in animation
- On eviction: rightmost card slides out with red flash
- Max 20 cards shown (most-recently-active)

**LFU tab:** Column layout where each column = a frequency bucket.
- Columns labeled "f=1", "f=2", "f=3", …
- Cards stack vertically within each column
- On access: card animates from old column to new column
- Color intensity scales with frequency (lighter = f=1, darker = higher freq)
- minFreq column highlighted with amber border

**ARC tab:** 4-section diagram:
```
┌────────────────┬────────────────┬────────────────┬────────────────┐
│  T1 (recent)   │  B1 (ghost)    │  T2 (frequent) │  B2 (ghost)    │
│  ▓▓▓▓▓▓▓▓▓▓   │  ░░░░░░░░░░   │  ▓▓▓▓▓▓▓▓▓▓▓▓  │  ░░░░░░░░░░   │
│  [cards]       │  [key names]   │  [cards]       │  [key names]   │
└────────────────┴────────────────┴────────────────┴────────────────┘
                           ← p →
```
- Horizontal slider shows current `p` value (target T1 size)
- Arrows animate when keys move between lists
- Hit type badge on each operation: "T1 hit", "T2 hit", "B1 hit → p↑", "B2 hit → p↓", "cold miss"

### Benchmarks (`/benchmarks`)

Top section: configuration form
- Workload picker: Sequential / Uniform Random / Zipf / Temporal / Scan-Resistant / Write-Heavy
- Cache size slider: 100 – 5000
- Duration: 5s / 10s / 30s
- Concurrency: 1 / 2 / 4 / 8
- Policies checkboxes: LRU / LFU / ARC (can run all 3 simultaneously)
- Write policy: through / back / around
- RUN BENCHMARK button (shows progress bar while running)

Results section (appears after run):
- Hit Rate Comparison bar chart (lightweight grouped SVG bars, one group per workload)
- Throughput line chart (ops/sec over time, 3 lines one per policy)
- Latency distribution: P50 / P95 / P99 per policy (grouped bar)
- Explanation card: describes why the winning policy won for this workload

Preset scenarios (one-click):
- "Zipf workload — see LFU/ARC outperform LRU"
- "Sequential scan — see LRU thrashing"
- "Temporal shift — see ARC adapt"
- "Write-heavy — compare write latency by policy"

---

## Pre-Populated Demo State

On startup, three named stores are created:

| Name | Policy | Capacity | Default TTL | Write Policy |
|---|---|---|---|---|
| `lru` | LRU | 1000 | none | write-through |
| `lfu` | LFU | 1000 | none | write-through |
| `arc` | ARC | 1000 | none | write-through |
| `lru-sharded` | LRU | 10000 | none | write-through (256 shards) |

Seed 200 entries into each store with random keys and values. Run a brief Zipf workload warm-up (10,000 ops) so the dashboard shows non-zero hit rates on first load.

---

## Error Handling

```go
type APIError struct {
    Code    string `json:"code"`
    Message string `json:"message"`
    Detail  string `json:"detail,omitempty"`
}
```

| Code | HTTP | Meaning |
|---|---|---|
| `STORE_NOT_FOUND` | 404 | Unknown store name |
| `KEY_NOT_FOUND` | 404 | Key doesn't exist (DELETE only) |
| `VALUE_TOO_LARGE` | 413 | Value > 1MB |
| `STORE_WRITE_ERROR` | 502 | Backing store write failed |
| `BENCHMARK_RUNNING` | 409 | Another benchmark is already running |
| `INVALID_CONFIG` | 400 | Invalid capacity / TTL value |

---

## Performance Targets

| Operation | Target (single store, no sharding) |
|---|---|
| Get (hit) | < 500 ns |
| Get (miss) | < 500 ns |
| Set (no eviction) | < 1 µs |
| Set (with eviction) | < 1 µs |
| Sharded Get (256 shards, 8 goroutines) | > 5M ops/sec |

Benchmarks (`go test -bench=.`) verify these targets.

---

## Non-Goals (v1)

- Distributed cache (no network, no Redis protocol)
- Persistence to disk
- SCAN / pattern-match key listing
- LRU-K (variant with K access history)
- TinyLFU / W-TinyLFU (Caffeine's admission policy)
- Cache warming strategies
- Prometheus metrics endpoint
