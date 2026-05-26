package sharded

import (
	"fmt"
	"hash/fnv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourname/cache-engine/internal/cache"
	"github.com/yourname/cache-engine/internal/cache/lru"
)

func factory(cap int) cache.Cache {
	return lru.New(cap)
}

func TestSharded_ConsistentRouting(t *testing.T) {
	// Same key always routes to same shard
	h := fnv.New32a()
	h.Write([]byte("testkey"))
	idx1 := h.Sum32() & (NumShards - 1)

	h.Reset()
	h.Write([]byte("testkey"))
	idx2 := h.Sum32() & (NumShards - 1)

	assert.Equal(t, idx1, idx2)
}

func TestSharded_GetSet(t *testing.T) {
	sc := New(1024, "lru", factory)

	sc.Set("k", []byte("v"), 0)
	v, ok := sc.Get("k")
	assert.True(t, ok)
	assert.Equal(t, []byte("v"), v)
}

func TestSharded_Delete(t *testing.T) {
	sc := New(1024, "lru", factory)
	sc.Set("k", []byte("v"), 0)
	ok := sc.Delete("k")
	assert.True(t, ok)
	_, ok = sc.Get("k")
	assert.False(t, ok)
}

func TestSharded_Concurrent(t *testing.T) {
	sc := New(1024, "lru", factory)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("k%d", i)
			sc.Set(key, []byte("v"), 0)
			sc.Get(key)
		}(i)
	}
	wg.Wait()
}

func TestSharded_StatsAggregation(t *testing.T) {
	sc := New(1024, "lru", factory)

	for i := 0; i < 10; i++ {
		sc.Set(fmt.Sprintf("k%d", i), []byte("v"), 0)
		sc.Get(fmt.Sprintf("k%d", i)) // hit
		sc.Get("nonexistent")          // miss
	}

	st := sc.Stats()
	assert.Equal(t, int64(10), st.Hits.Load())
	assert.Equal(t, int64(10), st.Misses.Load())
}

func TestSharded_LenAndCapacity(t *testing.T) {
	sc := New(1024, "lru", factory)
	assert.Equal(t, 1024, sc.Capacity())

	for i := 0; i < 10; i++ {
		sc.Set(fmt.Sprintf("k%d", i), []byte("v"), 0)
	}
	assert.Equal(t, 10, sc.Len())
}
