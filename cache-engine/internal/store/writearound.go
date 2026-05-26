package store

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/yourname/cache-engine/internal/cache"
	"github.com/yourname/cache-engine/internal/stats"
)

// WriteAroundCache writes directly to the backing store, bypassing the cache.
type WriteAroundCache struct {
	inner cache.Cache
	store BackingStore
}

// NewWriteAround creates a write-around wrapper.
func NewWriteAround(inner cache.Cache, store BackingStore) *WriteAroundCache {
	return &WriteAroundCache{inner: inner, store: store}
}

func (w *WriteAroundCache) Set(key string, value []byte, ttl time.Duration) error {
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
		return err
	}
	w.inner.Stats().WriteStoreOps.Add(1)
	return nil
}

func (w *WriteAroundCache) Get(key string) ([]byte, bool) {
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
		slog.Error("write_around_decode_failed", slog.String("key", key), slog.Any("error", err))
		return nil, false
	}
	if _, expired := ttlFromExpiry(expiresAt); expired {
		if err := w.store.Delete(ctx, key); err != nil {
			slog.Error("write_around_delete_expired_failed", slog.String("key", key), slog.Any("error", err))
		}
		return nil, false
	}
	w.inner.Stats().ReadStoreOps.Add(1)
	// Do NOT populate cache on read-miss (write-around semantics)
	return v, true
}

func (w *WriteAroundCache) Delete(key string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = w.store.Delete(ctx, key)
	return w.inner.Delete(key)
}

func (w *WriteAroundCache) Peek(key string) ([]byte, bool) { return w.inner.Peek(key) }
func (w *WriteAroundCache) Keys() []string                 { return w.inner.Keys() }
func (w *WriteAroundCache) Len() int                       { return w.inner.Len() }
func (w *WriteAroundCache) Capacity() int                  { return w.inner.Capacity() }
func (w *WriteAroundCache) Stats() *stats.Stats            { return w.inner.Stats() }
func (w *WriteAroundCache) Snapshot() cache.SnapshotResult { return w.inner.Snapshot() }
func (w *WriteAroundCache) Purge()                         { w.inner.Purge() }
func (w *WriteAroundCache) Close()                         { w.inner.Close() }
func (w *WriteAroundCache) SetEvictionCallback(fn func(string, []byte)) {
	w.inner.SetEvictionCallback(fn)
}
