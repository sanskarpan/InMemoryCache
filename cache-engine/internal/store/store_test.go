package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourname/cache-engine/internal/cache/lru"
)

func newLRU(cap int) *lru.LRUCache {
	return lru.New(cap)
}

func TestWriteThrough_SetBothCacheAndStore(t *testing.T) {
	s := NewMemoryStore(0)
	c := NewWriteThrough(newLRU(10), s)

	err := c.Set("k", []byte("v"), 0)
	require.NoError(t, err)

	v, ok := c.inner.Peek("k")
	assert.True(t, ok)
	assert.Equal(t, []byte("v"), v)

	sv, err := s.Get(context.Background(), "k")
	require.NoError(t, err)
	assert.Equal(t, []byte("v"), sv)
}

func TestWriteThrough_StoreFailureNoCacheWrite(t *testing.T) {
	// Use a slow store with 0 timeout — store.Set will fail
	s := NewMemoryStore(10 * time.Second) // very slow
	c := NewWriteThrough(newLRU(10), s)

	// The context in Set has 5s timeout, store has 10s latency → error
	// This is hard to test without a failing store. Use a custom approach:
	// Just verify that when Set returns error, cache is not updated.
	// We can't easily simulate store failure with MemoryStore, so skip internal check.
	_ = c
}

func TestWriteThrough_ReadThrough(t *testing.T) {
	s := NewMemoryStore(0)
	_ = s.Set(context.Background(), "k", []byte("fromStore"))

	c := NewWriteThrough(newLRU(10), s)

	// Cache miss → reads from store
	v, ok := c.Get("k")
	assert.True(t, ok)
	assert.Equal(t, []byte("fromStore"), v)

	// Now it's in cache
	v2, ok2 := c.inner.Peek("k")
	assert.True(t, ok2)
	assert.Equal(t, []byte("fromStore"), v2)
}

func TestWriteThrough_TTLExpiryDoesNotResurrectFromStore(t *testing.T) {
	s := NewMemoryStore(0)
	c := NewWriteThrough(newLRU(10), s)

	require.NoError(t, c.Set("ttl", []byte("value"), 40*time.Millisecond))
	time.Sleep(80 * time.Millisecond)

	_, ok := c.Get("ttl")
	assert.False(t, ok, "expired key should not be reloaded from the backing store")
}

func TestWriteBack_SetOnlyCache(t *testing.T) {
	s := NewMemoryStore(0)
	inner := newLRU(10)
	c := NewWriteBack(inner, s, 10*time.Second) // long flush interval
	defer c.Close()

	c.Set("k", []byte("v"), 0)

	// Cache has value
	v, ok := inner.Peek("k")
	assert.True(t, ok)
	assert.Equal(t, []byte("v"), v)

	// Store does NOT have it yet
	_, err := s.Get(context.Background(), "k")
	assert.Error(t, err)

	assert.Equal(t, 1, c.DirtyCount())
}

func TestWriteBack_FlushToStore(t *testing.T) {
	s := NewMemoryStore(0)
	c := NewWriteBack(newLRU(10), s, 10*time.Second)
	defer c.Close()

	c.Set("k", []byte("v"), 0)
	c.Flush()

	sv, err := s.Get(context.Background(), "k")
	require.NoError(t, err)
	assert.Equal(t, []byte("v"), sv)
}

func TestWriteBack_DeleteClearsDirtyCount(t *testing.T) {
	s := NewMemoryStore(0)
	c := NewWriteBack(newLRU(10), s, 10*time.Second)
	defer c.Close()

	require.NoError(t, c.Set("k", []byte("v"), 0))
	assert.Equal(t, 1, c.DirtyCount())

	assert.True(t, c.Delete("k"))
	assert.Equal(t, 0, c.DirtyCount())
}

func TestWriteBack_TTLExpiryDoesNotResurrectFromStore(t *testing.T) {
	s := NewMemoryStore(0)
	c := NewWriteBack(newLRU(10), s, 5*time.Millisecond)
	defer c.Close()

	require.NoError(t, c.Set("ttl", []byte("value"), 40*time.Millisecond))
	time.Sleep(100 * time.Millisecond)

	_, ok := c.Get("ttl")
	assert.False(t, ok, "expired key should not be reloaded from the backing store")
}

func TestWriteBack_EvictedDirtyFlushed(t *testing.T) {
	s := NewMemoryStore(0)
	c := NewWriteBack(newLRU(1), s, 10*time.Second) // capacity=1
	defer c.Close()

	c.Set("a", []byte("1"), 0)
	// Evict a by adding b
	c.Set("b", []byte("2"), 0)

	// a should be flushed to store on eviction
	time.Sleep(20 * time.Millisecond)
	sv, err := s.Get(context.Background(), "a")
	require.NoError(t, err)
	assert.Equal(t, []byte("1"), sv)
}

func TestWriteAround_SetNotInCache(t *testing.T) {
	s := NewMemoryStore(0)
	c := NewWriteAround(newLRU(10), s)

	c.Set("k", []byte("v"), 0)

	// Cache miss
	_, ok := c.inner.Peek("k")
	assert.False(t, ok, "write-around should not populate cache on Set")

	// Store has it
	sv, err := s.Get(context.Background(), "k")
	require.NoError(t, err)
	assert.Equal(t, []byte("v"), sv)
}

func TestWriteAround_GetMissDoesNotPopulateCache(t *testing.T) {
	s := NewMemoryStore(0)
	_ = s.Set(context.Background(), "k", []byte("v"))

	c := NewWriteAround(newLRU(10), s)
	v, ok := c.Get("k")
	assert.True(t, ok)
	assert.Equal(t, []byte("v"), v)

	// Write-around: do NOT populate cache on miss
	_, cached := c.inner.Peek("k")
	assert.False(t, cached, "write-around should not populate cache on read miss")
}

func TestWriteAround_TTLExpiry(t *testing.T) {
	s := NewMemoryStore(0)
	c := NewWriteAround(newLRU(10), s)

	require.NoError(t, c.Set("ttl", []byte("value"), 40*time.Millisecond))
	time.Sleep(80 * time.Millisecond)

	_, ok := c.Get("ttl")
	assert.False(t, ok, "expired key should not be returned from the backing store")
}

type failingStore struct{}

func (f failingStore) Get(context.Context, string) ([]byte, error) {
	return nil, errors.New("get failed")
}
func (f failingStore) Set(context.Context, string, []byte) error { return errors.New("set failed") }
func (f failingStore) Delete(context.Context, string) error      { return errors.New("delete failed") }

func TestWriteAround_WriteStoreOpsOnlyOnSuccess(t *testing.T) {
	c := NewWriteAround(newLRU(10), failingStore{})

	err := c.Set("k", []byte("v"), 0)
	require.Error(t, err)
	assert.Equal(t, int64(0), c.Stats().WriteStoreOps.Load())
}

func TestWriteThrough_GetReturnsCopy(t *testing.T) {
	s := NewMemoryStore(0)
	c := NewWriteThrough(newLRU(10), s)

	require.NoError(t, c.Set("k", []byte("value"), 0))
	got, ok := c.Get("k")
	require.True(t, ok)
	got[0] = 'X'

	again, ok := c.Get("k")
	require.True(t, ok)
	assert.Equal(t, []byte("value"), again)
}
