package store

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yourname/cache-engine/internal/cache"
	"github.com/yourname/cache-engine/internal/stats"
)

// WriteBackCache writes to the cache immediately and flushes to the store in the background.
type WriteBackCache struct {
	inner      cache.Cache
	store      BackingStore
	dirty      sync.Map // key → pendingWrite
	dirtyCount atomic.Int64
	done       chan struct{}
	interval   time.Duration
}

type pendingWrite struct {
	expiresAt time.Time
}

// NewWriteBack creates a write-back wrapper.
func NewWriteBack(inner cache.Cache, store BackingStore, flushInterval time.Duration) *WriteBackCache {
	if flushInterval <= 0 {
		flushInterval = 100 * time.Millisecond
	}
	wb := &WriteBackCache{
		inner:    inner,
		store:    store,
		done:     make(chan struct{}),
		interval: flushInterval,
	}
	// Flush dirty keys synchronously on eviction
	inner.SetEvictionCallback(func(key string, value []byte) {
		v, dirty := wb.dirty.LoadAndDelete(key)
		if dirty {
			wb.dirtyCount.Add(-1)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			pw, _ := v.(pendingWrite)
			if err := wb.flushValue(ctx, key, value, pw); err != nil {
				slog.Error("write_back_eviction_flush_failed", slog.String("key", key), slog.Any("error", err))
			} else {
				inner.Stats().WriteStoreOps.Add(1)
			}
		}
	})
	go wb.flushLoop()
	return wb
}

func (wb *WriteBackCache) flushLoop() {
	ticker := time.NewTicker(wb.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			wb.flushAll()
		case <-wb.done:
			return
		}
	}
}

func (wb *WriteBackCache) flushValue(ctx context.Context, key string, value []byte, pw pendingWrite) error {
	if _, expired := ttlFromExpiry(pw.expiresAt); expired {
		return wb.store.Delete(ctx, key)
	}

	encoded, err := encodeForStore(value, pw.expiresAt)
	if err != nil {
		return fmt.Errorf("encode backing store value: %w", err)
	}
	if err := wb.store.Set(ctx, key, encoded); err != nil {
		return err
	}
	return nil
}

func (wb *WriteBackCache) flushAll() {
	wb.dirty.Range(func(k, v any) bool {
		key := k.(string)
		pw, _ := v.(pendingWrite)
		// Atomically remove from dirty BEFORE peeking. Any concurrent Set will
		// re-mark dirty, ensuring we never permanently lose a write.
		if _, wasDirty := wb.dirty.LoadAndDelete(key); !wasDirty {
			return true
		}
		wb.dirtyCount.Add(-1)

		value, ok := wb.inner.Peek(key)
		if !ok {
			if _, expired := ttlFromExpiry(pw.expiresAt); expired {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				if err := wb.store.Delete(ctx, key); err != nil {
					slog.Error("write_back_delete_expired_failed", slog.String("key", key), slog.Any("error", err))
				}
				cancel()
			}
			// Already evicted — eviction callback already flushed it synchronously.
			return true
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := wb.flushValue(ctx, key, value, pw)
		cancel()
		if err != nil {
			// Re-mark dirty so the next tick retries.
			if _, loaded := wb.dirty.LoadOrStore(key, pw); !loaded {
				wb.dirtyCount.Add(1)
			}
			return true
		}
		wb.inner.Stats().WriteStoreOps.Add(1)
		return true
	})
}

func (wb *WriteBackCache) Set(key string, value []byte, ttl time.Duration) error {
	if err := wb.inner.Set(key, value, ttl); err != nil {
		return err
	}
	expiresAt := time.Time{}
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}
	if _, loaded := wb.dirty.LoadOrStore(key, pendingWrite{expiresAt: expiresAt}); !loaded {
		wb.dirtyCount.Add(1)
	} else {
		wb.dirty.Store(key, pendingWrite{expiresAt: expiresAt})
	}
	return nil
}

func (wb *WriteBackCache) Get(key string) ([]byte, bool) {
	if v, ok := wb.inner.Get(key); ok {
		return v, true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := wb.store.Get(ctx, key)
	if err != nil {
		return nil, false
	}
	v, expiresAt, err := decodeFromStore(raw)
	if err != nil {
		slog.Error("write_back_decode_failed", slog.String("key", key), slog.Any("error", err))
		return nil, false
	}
	ttl, expired := ttlFromExpiry(expiresAt)
	if expired {
		if err := wb.store.Delete(ctx, key); err != nil {
			slog.Error("write_back_delete_expired_failed", slog.String("key", key), slog.Any("error", err))
		}
		return nil, false
	}
	wb.inner.Stats().ReadStoreOps.Add(1)
	_ = wb.inner.Set(key, v, ttl)
	return v, true
}

func (wb *WriteBackCache) Delete(key string) bool {
	if _, loaded := wb.dirty.LoadAndDelete(key); loaded {
		wb.dirtyCount.Add(-1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = wb.store.Delete(ctx, key)
	return wb.inner.Delete(key)
}

// Flush force-flushes all dirty keys synchronously.
func (wb *WriteBackCache) Flush() {
	wb.flushAll()
}

// DirtyCount returns the number of dirty keys.
func (wb *WriteBackCache) DirtyCount() int {
	return int(wb.dirtyCount.Load())
}

func (wb *WriteBackCache) Peek(key string) ([]byte, bool) { return wb.inner.Peek(key) }
func (wb *WriteBackCache) Keys() []string                 { return wb.inner.Keys() }
func (wb *WriteBackCache) Len() int                       { return wb.inner.Len() }
func (wb *WriteBackCache) Capacity() int                  { return wb.inner.Capacity() }
func (wb *WriteBackCache) Stats() *stats.Stats            { return wb.inner.Stats() }
func (wb *WriteBackCache) Snapshot() cache.SnapshotResult { return wb.inner.Snapshot() }
func (wb *WriteBackCache) Purge() {
	wb.Flush()
	wb.inner.Purge()
}
func (wb *WriteBackCache) SetEvictionCallback(fn func(string, []byte)) {} // already registered

func (wb *WriteBackCache) Close() {
	wb.Flush()
	close(wb.done)
	wb.inner.Close()
}
