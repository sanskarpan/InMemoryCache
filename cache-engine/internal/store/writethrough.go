package store

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/yourname/cache-engine/internal/cache"
	"github.com/yourname/cache-engine/internal/stats"
)

// WriteThroughCache writes to the backing store before the cache.
type WriteThroughCache struct {
	inner cache.Cache
	store BackingStore
}

// NewWriteThrough creates a write-through wrapper.
func NewWriteThrough(inner cache.Cache, store BackingStore) *WriteThroughCache {
	return &WriteThroughCache{inner: inner, store: store}
}

func (w *WriteThroughCache) Set(key string, value []byte, ttl time.Duration) error {
	expiresAt := time.Time{}
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}
	encoded, err := encodeForStore(value, expiresAt)
	if err != nil {
		return fmt.Errorf("encode backing store value: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.store.Set(ctx, key, encoded); err != nil {
		return fmt.Errorf("backing store write failed: %w", err)
	}
	w.inner.Stats().WriteStoreOps.Add(1)
	return w.inner.Set(key, value, ttl)
}

func (w *WriteThroughCache) Get(key string) ([]byte, bool) {
	if v, ok := w.inner.Get(key); ok {
		return v, true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := w.store.Get(ctx, key)
	if err != nil {
		return nil, false
	}
	v, expiresAt, err := decodeFromStore(raw)
	if err != nil {
		slog.Error("write_through_decode_failed", slog.String("key", key), slog.Any("error", err))
		return nil, false
	}
	ttl, expired := ttlFromExpiry(expiresAt)
	if expired {
		if err := w.store.Delete(ctx, key); err != nil {
			slog.Error("write_through_delete_expired_failed", slog.String("key", key), slog.Any("error", err))
		}
		return nil, false
	}
	w.inner.Stats().ReadStoreOps.Add(1)
	_ = w.inner.Set(key, v, ttl)
	return v, true
}

func (w *WriteThroughCache) Delete(key string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.store.Delete(ctx, key); err != nil {
		slog.Error("write_through_store_delete_failed", slog.String("key", key), slog.Any("error", err))
	}
	return w.inner.Delete(key)
}

func (w *WriteThroughCache) Peek(key string) ([]byte, bool) { return w.inner.Peek(key) }
func (w *WriteThroughCache) Keys() []string                 { return w.inner.Keys() }
func (w *WriteThroughCache) Len() int                       { return w.inner.Len() }
func (w *WriteThroughCache) Capacity() int                  { return w.inner.Capacity() }
func (w *WriteThroughCache) Stats() *stats.Stats            { return w.inner.Stats() }
func (w *WriteThroughCache) Snapshot() cache.SnapshotResult { return w.inner.Snapshot() }
func (w *WriteThroughCache) Purge()                         { w.inner.Purge() }
func (w *WriteThroughCache) Close()                         { w.inner.Close() }
func (w *WriteThroughCache) SetEvictionCallback(fn func(string, []byte)) {
	w.inner.SetEvictionCallback(fn)
}
