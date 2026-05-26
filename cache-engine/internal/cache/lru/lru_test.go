package lru

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLRU_BasicGetSetDelete(t *testing.T) {
	c := New(10)
	err := c.Set("k", []byte("v"), 0)
	require.NoError(t, err)

	v, ok := c.Get("k")
	assert.True(t, ok)
	assert.Equal(t, []byte("v"), v)

	ok = c.Delete("k")
	assert.True(t, ok)

	_, ok = c.Get("k")
	assert.False(t, ok)
}

func TestLRU_EvictsLRU(t *testing.T) {
	c := New(3)
	c.Set("a", []byte("1"), 0)
	c.Set("b", []byte("2"), 0)
	c.Set("c", []byte("3"), 0)
	// Access a and b to make c the LRU
	c.Get("a")
	c.Get("b")
	// Add d — should evict c
	c.Set("d", []byte("4"), 0)

	_, ok := c.Get("c")
	assert.False(t, ok, "c should be evicted (LRU)")
	_, ok = c.Get("a")
	assert.True(t, ok)
}

func TestLRU_LRUOrder(t *testing.T) {
	c := New(3)
	c.Set("a", []byte("1"), 0)
	c.Set("b", []byte("2"), 0)
	c.Set("c", []byte("3"), 0)
	// MRU order should be c, b, a
	keys := c.Keys()
	assert.Equal(t, []string{"c", "b", "a"}, keys)

	// Access a — becomes MRU
	c.Get("a")
	keys = c.Keys()
	assert.Equal(t, []string{"a", "c", "b"}, keys)
}

func TestLRU_TTLExpiry(t *testing.T) {
	c := New(100)
	c.Set("k", []byte("v"), 50*time.Millisecond)

	_, hit := c.Get("k")
	assert.True(t, hit, "should hit before TTL")

	time.Sleep(100 * time.Millisecond)

	_, hit = c.Get("k")
	assert.False(t, hit, "should miss after TTL")
	assert.Equal(t, int64(1), c.Stats().TTLExpiries.Load())
}

func TestLRU_Peek_NoEvictionOrderChange(t *testing.T) {
	c := New(3)
	c.Set("a", []byte("1"), 0)
	c.Set("b", []byte("2"), 0)
	c.Set("c", []byte("3"), 0)

	// Peek at a — should NOT move it to front
	v, ok := c.Peek("a")
	assert.True(t, ok)
	assert.Equal(t, []byte("1"), v)

	// Add d — should evict a (still LRU)
	c.Set("d", []byte("4"), 0)
	_, ok = c.Get("a")
	assert.False(t, ok, "a should be evicted (Peek didn't change order)")
}

func TestLRU_Capacity1(t *testing.T) {
	c := New(1)
	c.Set("a", []byte("1"), 0)
	c.Set("b", []byte("2"), 0)

	_, ok := c.Get("a")
	assert.False(t, ok, "a should be evicted")
	_, ok = c.Get("b")
	assert.True(t, ok)
}

func TestLRU_OverwriteMovesToMRU(t *testing.T) {
	c := New(3)
	c.Set("a", []byte("1"), 0)
	c.Set("b", []byte("2"), 0)
	c.Set("c", []byte("3"), 0)
	// Overwrite a — should move to MRU
	c.Set("a", []byte("new"), 0)

	// Add d — should evict b (now LRU)
	c.Set("d", []byte("4"), 0)
	_, ok := c.Get("b")
	assert.False(t, ok, "b should be evicted")

	v, ok := c.Get("a")
	assert.True(t, ok)
	assert.Equal(t, []byte("new"), v)
}

func TestLRU_EvictionCallback(t *testing.T) {
	c := New(2)
	evicted := make(map[string][]byte)
	c.SetEvictionCallback(func(key string, value []byte) {
		evicted[key] = value
	})

	c.Set("a", []byte("1"), 0)
	c.Set("b", []byte("2"), 0)
	c.Set("c", []byte("3"), 0) // evicts a

	assert.Equal(t, []byte("1"), evicted["a"])
}

func TestLRU_Concurrent(t *testing.T) {
	c := New(100)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("k%d", i)
			c.Set(key, []byte("v"), 0)
			c.Get(key)
			c.Delete(key)
		}(i)
	}
	wg.Wait()
}

func TestLRU_Stats(t *testing.T) {
	c := New(10)
	c.Set("a", []byte("1"), 0)
	c.Get("a") // hit
	c.Get("b") // miss

	assert.Equal(t, int64(1), c.Stats().Hits.Load())
	assert.Equal(t, int64(1), c.Stats().Misses.Load())
	assert.Equal(t, float64(0.5), c.Stats().HitRate())
}
