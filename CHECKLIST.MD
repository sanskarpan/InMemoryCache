# Implementation Checklist

Work strictly in phase order. Each phase builds on the previous. Check items as you complete them. Every phase must pass its tests before proceeding.

---

## Phase 0 — Scaffolding

### Backend
- [ ] `go mod init github.com/yourname/cache-engine`
- [ ] `go get github.com/go-chi/chi/v5 github.com/stretchr/testify`
- [ ] Create full directory tree from SPEC.md project structure
- [ ] Stub `cmd/server/main.go` that compiles and prints "cache engine starting"
- [ ] Create `Makefile` with: `build`, `test`, `bench`, `run`, `lint`, `dev`

### Frontend
- [ ] `cd web && npm create vite@latest . -- --template react-ts`
- [ ] Install: `tailwindcss @tailwindcss/vite framer-motion zustand ky react-resizable-panels`
- [ ] `npx shadcn@latest init`
- [ ] Add shadcn components: `button badge card tabs select slider switch progress tooltip separator scroll-area`
- [ ] Configure Tailwind CSS
- [ ] `vite.config.ts`: proxy `/api` and `/sse` → `http://localhost:8080`
- [ ] Verify `npm run dev` runs clean

---

## Phase 1 — Generic Doubly-Linked List

This is the foundation for all three eviction policies. Get it right first.

- [ ] Define `Node[T any]` struct with `Prev`, `Next *Node[T]`, `Value T`
- [ ] Implement `List[T any]` struct with `head`, `tail *Node[T]`, `len int`
- [ ] Implement `PushFront(value T) *Node[T]`
- [ ] Implement `PushBack(value T) *Node[T]`
- [ ] Implement `MoveToFront(node *Node[T])`
- [ ] Implement `Remove(node *Node[T])`
- [ ] Implement `Front() *Node[T]`
- [ ] Implement `Back() *Node[T]`
- [ ] Implement `Len() int`
- [ ] Implement `ToSlice() []T` (front → back order, for snapshots)
- [ ] Write tests: push front/back, move to front, remove middle, remove head/tail, empty list operations, len tracking
- [ ] **Invariant test:** after any sequence of operations, `list.head.Prev == nil` and `list.tail.Next == nil` and `Len()` matches actual node count

---

## Phase 2 — Stats

- [ ] Define `Stats` struct with `atomic.Int64` fields: Hits, Misses, Sets, Deletes, Evictions, TTLExpiries, BytesStored, WriteStoreOps, ReadStoreOps
- [ ] Implement `HitRate() float64`
- [ ] Implement `MissRate() float64`
- [ ] Implement `EvictionRate() float64`
- [ ] Implement `Snapshot() StatsSnapshot` (returns plain struct copy, no atomics)
- [ ] Implement `Reset()` (zero all counters)
- [ ] Implement `WindowedStats` ring buffer: 60 buckets of 1-second windows
- [ ] `WindowedStats.Record(hit bool, eviction bool)` — writes to current bucket
- [ ] `WindowedStats.Series(n int) []StatsBucket` — returns last n seconds of data
- [ ] Background goroutine in `WindowedStats` that advances the ring buffer every second
- [ ] Write tests: atomic increment race detection (`go test -race`), hit rate calculation, window rotation

---

## Phase 3 — Cache Interface + Entry

- [ ] Define `Entry` struct: Key string, Value []byte, ExpiresAt time.Time, Freq int, CreatedAt time.Time, HitCount int64, SizeBytes int, List string (for ARC)
- [ ] Define `Cache` interface: Get, Set, Delete, Peek, Keys, Len, Capacity, Stats, Snapshot, Purge, Close
- [ ] Define `PolicyType` enum: LRU, LFU, ARC
- [ ] Define `WritePolicyType` enum: WriteThrough, WriteBack, WriteAround
- [ ] Define `Config` struct for cache construction: Capacity, DefaultTTL, Policy, OnEvict callback

---

## Phase 4 — LRU Cache

- [ ] Define `lruNode` struct: Key string, Value []byte, ExpiresAt time.Time, prev/next *lruNode
- [ ] Implement `LRUCache` struct: capacity, items map[string]*lruNode, head/tail pointers, stats, mu sync.RWMutex
- [ ] Implement `Get(key)`: lookup, check expiry (lazy), move-to-front, stats.Hit/Miss
- [ ] Implement `Set(key, value, ttl)`: insert-or-update, evict-tail if full, stats.Set/Eviction
- [ ] Implement `Delete(key)`: remove from map and list, stats.Delete
- [ ] Implement `Peek(key)`: lookup + expiry check WITHOUT moving to front
- [ ] Implement `Keys()`: iterate list front→back, return all non-expired keys
- [ ] Implement `Snapshot()`: return []Entry ordered MRU→LRU with TTL, freq=1 for all
- [ ] Implement `Purge()`: clear map and list, reset stats
- [ ] Implement `Close()`: no-op (no goroutines in base LRU)
- [ ] Write unit tests:
  - [ ] Basic get/set/delete
  - [ ] Eviction when full (correct key evicted — the LRU one)
  - [ ] LRU order after multiple accesses
  - [ ] TTL expiry via lazy check
  - [ ] Peek doesn't change eviction order
  - [ ] Capacity=1 edge case
  - [ ] Overwrite updates value and moves to MRU
  - [ ] Concurrent access with `go test -race`

---

## Phase 5 — LFU Cache

- [ ] Define `lfuNode` struct: Key, Value, Freq int, ExpiresAt, prev/next *lfuNode
- [ ] Implement `LFUCache` struct: capacity, minFreq int, items map[string]*lfuNode, freqMap map[int]*list.List[*lfuNode], stats, mu
- [ ] Implement `incrementFreq(node)`: remove from freqMap[node.freq], node.freq++, add to front of freqMap[node.freq]. If freqMap[oldFreq] empty and oldFreq == minFreq: minFreq++
- [ ] Implement `Get(key)`: lookup, expiry check, incrementFreq, return value
- [ ] Implement `Set(key, value, ttl)`: if exists → update value + incrementFreq; if full → evict tail of freqMap[minFreq]; add new node to freqMap[1] front; minFreq = 1
- [ ] Implement `Delete(key)`: remove from map and freqMap[node.freq]
- [ ] Implement `Peek(key)`: lookup without frequency change
- [ ] Implement `Snapshot()`: return entries grouped by frequency, within-group ordered by recency (front to back)
- [ ] Write unit tests:
  - [ ] Frequency tracking (access key N times, verify freq=N)
  - [ ] LFU eviction chooses lowest frequency, ties broken by recency
  - [ ] minFreq reset to 1 on new Set
  - [ ] minFreq update when minFreq list becomes empty after incrementFreq
  - [ ] Eviction when capacity=1
  - [ ] After eviction, minFreq is correct
  - [ ] Concurrent access race test

---

## Phase 6 — ARC Cache

- [ ] Define `arcNode` struct: Key, Value string, ExpiresAt time.Time, list string (T1/T2/B1/B2)
- [ ] Implement `ARCCache` struct: capacity, p int, T1/T2/B1/B2 *list.List[*arcNode], itemMap map[string]*arcNode, stats, mu
- [ ] Implement `replace(preferT2 bool)`: evict from T1 or T2 based on p and preferT2 hint; add ghost to B1/B2
- [ ] Implement `Get(key)`:
  - [ ] T1 hit → move to T2 front (promotion)
  - [ ] T2 hit → move to T2 front (MRU)
  - [ ] Not in T1/T2 → miss
  - [ ] Expiry check (lazy) on hit
- [ ] Implement `Set(key, value, ttl)`:
  - [ ] Already in T1 or T2 → update value, trigger Get-like promotion
  - [ ] In B1 → increase p, call replace(preferT2=false), move from B1 to T2 front
  - [ ] In B2 → decrease p, call replace(preferT2=true), move from B2 to T2 front
  - [ ] New key → manage T1+B1 and total size, call replace if needed, add to T1 front
- [ ] Implement `delete(key)`: remove from whichever list it's in
- [ ] Implement `Purge()`: clear all 4 lists, p=0
- [ ] Implement `Snapshot()`: return entries with List field indicating T1/T2; ghost entries with List B1/B2 (no value); include ArcP int
- [ ] Write unit tests:
  - [ ] p increases when B1 hit occurs
  - [ ] p decreases when B2 hit occurs
  - [ ] p clamped to [0, capacity]
  - [ ] T1 → T2 promotion on second access to same key
  - [ ] Ghost entry created when T1 key is evicted
  - [ ] Scan resistance: sequential scan of N > capacity keys should NOT evict all T2 entries (verify with a small fixed working set that survives a scan)
  - [ ] Total live entries never exceeds capacity
  - [ ] Concurrent access race test

---

## Phase 7 — TTL Wheel

- [ ] Define `ttlEntry` struct: Key string, ExpAt time.Time
- [ ] Implement `minHeap` implementing `heap.Interface` (from `container/heap`): ordered by ExpAt ascending
- [ ] Implement `TTLWheel` struct: heap, index map[string]int (heap position for O(log n) removal), onExpire callback, mu sync.Mutex, stop chan
- [ ] Implement `Schedule(key string, ttl time.Duration)`: push onto heap with expAt = now+ttl
- [ ] Implement `Cancel(key string)`: remove from heap using index map
- [ ] Implement `Run()`: goroutine that sleeps until heap[0].ExpAt, pops all expired entries, calls onExpire for each
- [ ] Implement `Stop()`: signal goroutine to exit
- [ ] Wire TTLWheel into LRU, LFU, ARC: Set() calls wheel.Schedule when ttl > 0, Delete() calls wheel.Cancel, onExpire calls cache.Delete + stats.TTLExpiry++
- [ ] Write tests:
  - [ ] Entry expires after TTL duration
  - [ ] Cancelled TTL does not fire
  - [ ] Multiple TTLs fire in correct order
  - [ ] Re-set same key updates TTL (cancel old, schedule new)
  - [ ] `go test -race` passes

---

## Phase 8 — Backing Store + Write Policies

- [ ] Implement `MemoryStore`: map[string][]byte with RWMutex + configurable sleep latency in Get/Set
- [ ] Implement `WriteThroughCache` wrapper:
  - [ ] Set: call store.Set first; if error, return error without touching cache; then cache.Set
  - [ ] Get: cache.Get; on miss, call store.Get, populate cache, return value
  - [ ] Delete: cache.Delete + store.Delete
- [ ] Implement `WriteBackCache` wrapper:
  - [ ] Set: cache.Set + mark key in dirty map
  - [ ] Get: cache.Get; on miss, read-through from store
  - [ ] Register eviction callback on underlying cache: if evicted key is dirty, flush to store synchronously before discarding
  - [ ] Background flush goroutine: drains dirty map every 100ms (configurable interval), calls store.Set
  - [ ] `Flush()` method: force-flush all dirty keys synchronously
  - [ ] `Close()`: flush all dirty, stop goroutine
  - [ ] `DirtyCount() int`: returns current dirty key count (for API/UI)
- [ ] Implement `WriteAroundCache` wrapper:
  - [ ] Set: only calls store.Set, does NOT touch cache
  - [ ] Get: cache.Get; on miss, store.Get (do NOT populate cache)
  - [ ] Delete: cache.Delete + store.Delete
- [ ] Write tests:
  - [ ] Write-through: after Set, both cache and store contain value
  - [ ] Write-through: store failure aborts set, cache unchanged
  - [ ] Write-back: after Set, only cache contains value; after flush, store contains value
  - [ ] Write-back: evicted dirty key flushed before discard (verify store has value after eviction)
  - [ ] Write-around: after Set, cache miss; store has value
  - [ ] Read-through on miss: store value populated into cache on next Get

---

## Phase 9 — Sharded Cache

- [ ] Implement `ShardedCache` struct: `[256]*shardEntry` array
- [ ] Each `shardEntry`: `mu sync.RWMutex` + `cache Cache`
- [ ] Constructor: `NewSharded(totalCapacity int, factory func(capacity int) Cache)` — creates 256 shards each with capacity = max(1, totalCapacity/256)
- [ ] Implement FNV-1a hash routing: `fnv.New32a()`, write key bytes, `hash & 0xFF` → shard index
- [ ] Implement Get, Set, Delete, Peek, Purge as: hash key → lock shard → delegate → unlock
- [ ] Aggregate Stats: sum all 256 shards' counters
- [ ] Aggregate Len: sum all 256 shards' Len()
- [ ] Aggregate Snapshot: concatenate all shards' entries (may be large; limit to 50 per shard = 12800 max)
- [ ] Write tests:
  - [ ] Keys route to consistent shards (same key always same shard)
  - [ ] Concurrent Gets and Sets from 100 goroutines with `go test -race`
  - [ ] Aggregate stats sum correctly
  - [ ] Total capacity honored

---

## Phase 10 — Cache Coherence

- [ ] Implement `EventBus` with Subscribe(nodeID string) chan Event, Publish(e Event), Unsubscribe(nodeID)
- [ ] Use buffered channels (size 100) to prevent slow subscribers from blocking publisher
- [ ] Implement `Coordinator` struct: 3 named `Cache` instances, EventBus, self nodeID
- [ ] Implement `Coordinator.Set(nodeID, key, value, ttl)`: write to named node, publish Invalidate event to others
- [ ] Implement event handler goroutine per node: receives events, calls Delete on own cache
- [ ] Implement `Coordinator.Snapshot()`: returns state of all 3 nodes for API
- [ ] Wire 3 nodes to separate LRU caches (lru-node-a, lru-node-b, lru-node-c) on startup
- [ ] Write tests:
  - [ ] Set on node-a causes node-b and node-c to delete the key
  - [ ] Events from own node are ignored
  - [ ] Slow subscriber doesn't block publisher (buffered channel test)

---

## Phase 11 — Benchmark Suite

- [ ] Implement `SequentialWorkload`: keys 0..N cycling
- [ ] Implement `UniformRandomWorkload`: `rand.Intn(N)`
- [ ] Implement `ZipfWorkload`: `rand.NewZipf(r, 1.2, 1.0, uint64(N))`
- [ ] Implement `TemporalWorkload`: hot set of 100 keys, shift every 5000 ops (new random hot set)
- [ ] Implement `ScanResistantWorkload`: 80% from 200-key hot set + 20% sequential scan over 10000 keys
- [ ] Implement `WriteHeavyWorkload`: 80% Set + 20% Get, uniform key distribution
- [ ] Implement `BenchmarkRunner.Run(config)`:
  - [ ] Create cache per policy
  - [ ] Spawn `config.Concurrency` goroutines
  - [ ] Each goroutine: loop `workload.Next()`, execute op, record latency
  - [ ] Collect latency samples in `[]int64` for P50/P95/P99 calculation
  - [ ] Run for `config.Duration`, wait for all goroutines
  - [ ] Return `[]BenchmarkResult`
- [ ] Implement P50/P95/P99 calculation: sort latency samples, index by percentile
- [ ] Write benchmark test (`_test.go` with `func BenchmarkLRU`, `BenchmarkLFU`, `BenchmarkARC`):
  - [ ] Run `go test -bench=. -benchmem` and confirm LRU/LFU/ARC each > 1M ops/sec
  - [ ] Verify ARC hit rate > LRU hit rate on Zipf workload

---

## Phase 12 — HTTP API

- [ ] Set up chi router
- [ ] CORS middleware: allow `localhost:5173`
- [ ] Logging middleware: method, path, duration, status
- [ ] Panic recovery middleware
- [ ] Register all 4 stores in a `StoreRegistry` (map[string]Cache)
- [ ] Implement `GET /api/cache/{store}/{key}` handler
- [ ] Implement `PUT /api/cache/{store}/{key}` handler (base64 decode value from JSON)
- [ ] Implement `DELETE /api/cache/{store}/{key}` handler
- [ ] Implement `GET /api/cache/{store}/stats` handler
- [ ] Implement `GET /api/cache/{store}/snapshot` handler
- [ ] Implement `DELETE /api/cache/{store}/purge` handler
- [ ] Implement `POST /api/cache/{store}/config` handler (update capacity/TTL/write policy)
- [ ] Implement `POST /api/coherence/set` handler
- [ ] Implement `GET /api/coherence/nodes` handler
- [ ] Implement `POST /api/benchmark/run` handler (runs async, returns 202 with job ID)
- [ ] Implement `GET /api/benchmark/results` handler
- [ ] Implement SSE endpoint `GET /api/sse/stats/{store}`:
  - [ ] Set headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `X-Accel-Buffering: no`
  - [ ] Goroutine: every 500ms, marshal stats snapshot, write `data: {json}\n\n`, flush
  - [ ] Detect client disconnect via request context cancellation
- [ ] Implement SSE endpoint `GET /api/sse/coherence` (broadcasts EventBus events)
- [ ] Write API tests with `httptest`:
  - [ ] GET miss → 200 with hit=false
  - [ ] PUT then GET → 200 with hit=true
  - [ ] DELETE existing key → 200 found=true
  - [ ] GET unknown store → 404 STORE_NOT_FOUND
  - [ ] Stats endpoint returns correct hit rate after known operations
  - [ ] Benchmark run → 202; poll results → 200 with data

---

## Phase 13 — Frontend Infrastructure

- [ ] React Router v6: routes `/`, `/playground`, `/visualizer`, `/benchmarks`
- [ ] API client `src/api/client.ts`: typed functions for all endpoints
- [ ] TypeScript types `src/types/index.ts`: mirror all backend DTOs exactly
- [ ] Zustand store: `activePolicies`, `snapshots`, `stats`, `benchmarkResults`, `isRunningBenchmark`
- [ ] `useSSE` hook: wraps `EventSource`, parses JSON, handles reconnect on error (retry with 2s delay)
- [ ] `useStats` hook: subscribes to SSE for a given store, updates Zustand
- [ ] `Nav` component with active link highlighting
- [ ] Global error boundary

---

## Phase 14 — Dashboard Page

- [ ] Three policy cards (LRU / LFU / ARC) with live hit rate
- [ ] Hit rate gauge: semicircle SVG gauge 0–100%, updates via SSE
- [ ] Ops/sec counter with large number + trend arrow (up/down from last second)
- [ ] Capacity fill bar: `size / capacity` percentage
- [ ] Eviction sparkline: last 20 data points, lightweight SVG `area` sparkline
- [ ] Write policy badge and quick config button
- [ ] Bottom: overlay multi-line SVG chart with 3 lines (one per policy), 60-second rolling window
- [ ] X-axis shows relative time (−60s to 0), Y-axis 0–100% hit rate
- [ ] Legend with colored squares per policy

---

## Phase 15 — Playground Page

- [ ] Policy selector dropdown (6 options including sharded variants)
- [ ] Key / Value / TTL inputs with validation
- [ ] GET / SET / DELETE / PEEK buttons (keyboard shortcuts: G/S/D/P)
- [ ] Result display: animated hit/miss badge with color (green/red), value display, TTL countdown
- [ ] TTL countdown: `setInterval` every 100ms, display as progress bar + `Xs remaining`
- [ ] Operation log: virtualized list of last 200 operations, auto-scroll to bottom
- [ ] Each log row: relative time, op badge, key, hit/miss, latency (µs), value preview
- [ ] Watch mode toggle: polls GET every 200ms for watched key, updates TTL bar live
- [ ] Write policy panel:
  - [ ] Policy selector: write-through / write-back / write-around
  - [ ] Store latency slider: 0–100ms (live update via POST /config)
  - [ ] Write-back panel: dirty count badge + last flush time + FLUSH NOW button
  - [ ] Response time display: shows backing-store latency vs cache latency side by side

---

## Phase 16 — Visualizer Page

- [ ] Fetch snapshot every 1s (polling) for the selected policy tab
- [ ] **LRU Viewer:**
  - [ ] Horizontal scrolling card list (Framer Motion `AnimatePresence` + `layout`)
  - [ ] Each card: key (truncated 10 chars), value preview, TTL bar (if set)
  - [ ] Cards use `layoutId={key}` so they animate position changes
  - [ ] Hit: card pulses green border then moves to front
  - [ ] Eviction: card animates out with red overlay from the right end
  - [ ] Max 20 cards visible, rest shown as "+N more" badge
- [ ] **LFU Viewer:**
  - [ ] Column layout, one column per active frequency value
  - [ ] Column header: "f=N" with entry count badge
  - [ ] Items within column: cards stacked vertically, MRU at top
  - [ ] `minFreq` column: amber left border highlight
  - [ ] On access: card animates (Framer Motion `layout`) from old column to new column
  - [ ] Color saturation scales with frequency: low freq = pale, high freq = saturated
- [ ] **ARC Viewer:**
  - [ ] 4-section grid: T1 | B1 | T2 | B2
  - [ ] T1 and T2 show full cards with values; B1 and B2 show key names only (ghost)
  - [ ] B1/B2 sections have dashed border and lower opacity (ghost visual)
  - [ ] p-indicator: horizontal bar spanning T1 section showing target p size
  - [ ] `arcP` number displayed: "p = 312 / 1000"
  - [ ] List size counts under each section header
  - [ ] Last operation badge: "T1 hit → T2", "B1 hit → p↑ = 315", "cold miss → T1", etc.
- [ ] Operation type tooltip explaining what each list contains (hover "T1" → "Recently used once, in cache")

---

## Phase 17 — Benchmarks Page

- [ ] Configuration form: workload picker, cache size slider, duration radio, concurrency, policy checkboxes, write policy
- [ ] RUN button: POST /benchmark/run → poll GET /benchmark/results every 500ms until complete
- [ ] Progress indicator: animated progress bar with elapsed / total time
- [ ] Results charts (appear on completion):
  - [ ] Grouped bar chart: Hit Rate per policy (lightweight SVG bars)
  - [ ] Grouped bar chart: P50 / P95 / P99 latency per policy
  - [ ] Bar chart: Eviction count per policy
  - [ ] Optional ops/sec chart if available
- [ ] "Why did X win?" explanation card: static text per workload type explaining expected results
- [ ] Preset scenario buttons: one click sets config + runs benchmark
- [ ] Result history: keep last 5 benchmark runs in Zustand, allow comparison

---

## Phase 18 — Integration & Polish

- [ ] Smoke test: start backend, verify all 4 stores respond to GET/PUT/DELETE
- [ ] Smoke test: Dashboard loads with live charts updating
- [ ] Smoke test: Visualizer animates on manual Playground operations
- [ ] Smoke test: Benchmark runs for all 3 policies on Zipf workload, ARC wins
- [ ] Smoke test: Coherence page shows invalidation events after cross-node write
- [ ] Handle SSE reconnection gracefully (backend restart shouldn't freeze frontend)
- [ ] Loading skeletons on all data-dependent panels
- [ ] Empty states with helpful messages (no benchmark results yet, cache is empty)
- [ ] Mobile layout: minimum 768px, graceful degradation
- [ ] README.md: setup in 3 commands, architecture diagram, screenshot
- [ ] `go test ./... -race` passes with zero failures
- [ ] `cd web && npx tsc --noEmit` passes with zero errors

---

## Phase 19 — Stretch Goals

- [ ] W-TinyLFU: count-min sketch admission filter to improve LFU hit rate
- [ ] LRU-K: tracks last K access times, evicts entry with oldest K-th access
- [ ] Cache warming: pre-populate from a key frequency profile
- [ ] Prometheus metrics endpoint (`/metrics`) with standard cache metrics
- [ ] Benchmark replay: record and replay real access traces
- [ ] Export benchmark results as CSV
- [ ] Dark mode for frontend
- [ ] Animated coherence diagram: 3-node grid with animated invalidation arrows

---

## Test Coverage Requirements

| Phase | Minimum tests |
|---|---|
| List | All operations, invariant test |
| LRU | Eviction order, TTL, concurrency |
| LFU | Frequency tracking, minFreq, concurrency |
| ARC | p adaptation, ghost promotion, scan resistance |
| TTL Wheel | Expiry timing, cancel, concurrent scheduling |
| Write policies | Through/back/around semantics, eviction-flush |
| Sharded | Routing, concurrency, stats aggregation |
| Coherence | Invalidation propagation, slow subscriber |
| Benchmark | Hit rate assertions on Zipf (ARC > LFU > LRU) |
| API | All endpoints happy path + error cases |

Run: `go test ./... -race -count=1`
