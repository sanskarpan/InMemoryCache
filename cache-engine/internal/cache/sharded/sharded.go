package sharded

import (
	"hash/fnv"
	"sync"
	"time"

	"github.com/yourname/cache-engine/internal/cache"
	"github.com/yourname/cache-engine/internal/stats"
)

const NumShards = 256

type shardEntry struct {
	mu    sync.Mutex
	cache cache.Cache
}

// ShardedCache distributes keys across 256 shards to reduce lock contention.
type ShardedCache struct {
	shards   [NumShards]*shardEntry
	capacity int // total capacity
	policy   string
}

// New creates a new ShardedCache.
// factory is called once per shard with the per-shard capacity.
func New(totalCapacity int, policy string, factory func(capacity int) cache.Cache) *ShardedCache {
	perShard := totalCapacity / NumShards
	if perShard < 1 {
		perShard = 1
	}
	sc := &ShardedCache{capacity: totalCapacity, policy: policy}
	for i := range sc.shards {
		sc.shards[i] = &shardEntry{cache: factory(perShard)}
	}
	return sc
}

func (sc *ShardedCache) shardFor(key string) *shardEntry {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return sc.shards[h.Sum32()&(NumShards-1)]
}

func (sc *ShardedCache) Get(key string) ([]byte, bool) {
	s := sc.shardFor(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cache.Get(key)
}

func (sc *ShardedCache) Set(key string, value []byte, ttl time.Duration) error {
	s := sc.shardFor(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cache.Set(key, value, ttl)
}

func (sc *ShardedCache) Delete(key string) bool {
	s := sc.shardFor(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cache.Delete(key)
}

func (sc *ShardedCache) Peek(key string) ([]byte, bool) {
	s := sc.shardFor(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cache.Peek(key)
}

func (sc *ShardedCache) Keys() []string {
	var all []string
	for i := range sc.shards {
		s := sc.shards[i]
		s.mu.Lock()
		all = append(all, s.cache.Keys()...)
		s.mu.Unlock()
	}
	return all
}

func (sc *ShardedCache) Len() int {
	total := 0
	for i := range sc.shards {
		s := sc.shards[i]
		s.mu.Lock()
		total += s.cache.Len()
		s.mu.Unlock()
	}
	return total
}

func (sc *ShardedCache) Capacity() int { return sc.capacity }

func (sc *ShardedCache) Stats() *stats.Stats {
	agg := &stats.Stats{}
	for i := range sc.shards {
		s := sc.shards[i]
		s.mu.Lock()
		sh := s.cache.Stats()
		agg.Hits.Add(sh.Hits.Load())
		agg.Misses.Add(sh.Misses.Load())
		agg.Sets.Add(sh.Sets.Load())
		agg.Deletes.Add(sh.Deletes.Load())
		agg.Evictions.Add(sh.Evictions.Load())
		agg.TTLExpiries.Add(sh.TTLExpiries.Load())
		agg.BytesStored.Add(sh.BytesStored.Load())
		agg.WriteStoreOps.Add(sh.WriteStoreOps.Load())
		agg.ReadStoreOps.Add(sh.ReadStoreOps.Load())
		s.mu.Unlock()
	}
	return agg
}

func (sc *ShardedCache) Snapshot() cache.SnapshotResult {
	var entries []cache.Entry
	truncated := false
	for i := range sc.shards {
		s := sc.shards[i]
		s.mu.Lock()
		snap := s.cache.Snapshot()
		if len(snap.Entries) > 50 {
			snap.Entries = snap.Entries[:50]
			truncated = true
		}
		entries = append(entries, snap.Entries...)
		s.mu.Unlock()
	}
	return cache.SnapshotResult{Policy: sc.policy + "-sharded", Entries: entries, Capacity: sc.capacity, Truncated: truncated}
}

func (sc *ShardedCache) Purge() {
	for i := range sc.shards {
		s := sc.shards[i]
		s.mu.Lock()
		s.cache.Purge()
		s.mu.Unlock()
	}
}

func (sc *ShardedCache) Close() {
	for i := range sc.shards {
		sc.shards[i].cache.Close()
	}
}

func (sc *ShardedCache) SetEvictionCallback(fn func(string, []byte)) {
	for i := range sc.shards {
		sc.shards[i].cache.SetEvictionCallback(fn)
	}
}
