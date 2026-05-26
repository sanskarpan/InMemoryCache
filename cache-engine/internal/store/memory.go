package store

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// MemoryStore is an in-memory backing store with configurable simulated latency.
type MemoryStore struct {
	mu        sync.RWMutex
	data      map[string][]byte
	latencyNs atomic.Int64 // simulated I/O latency in nanoseconds
}

// NewMemoryStore creates a new MemoryStore.
func NewMemoryStore(latency time.Duration) *MemoryStore {
	ms := &MemoryStore{data: make(map[string][]byte)}
	ms.latencyNs.Store(latency.Nanoseconds())
	return ms
}

// SetLatency updates the simulated I/O latency; safe to call concurrently.
func (m *MemoryStore) SetLatency(d time.Duration) {
	m.latencyNs.Store(d.Nanoseconds())
}

// GetLatency returns the current simulated I/O latency.
func (m *MemoryStore) GetLatency() time.Duration {
	return time.Duration(m.latencyNs.Load())
}

func (m *MemoryStore) sleep(ctx context.Context) error {
	ns := m.latencyNs.Load()
	if ns <= 0 {
		return nil
	}
	select {
	case <-time.After(time.Duration(ns)):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *MemoryStore) Get(ctx context.Context, key string) ([]byte, error) {
	if err := m.sleep(ctx); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[key]
	if !ok {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cp, nil
}

func (m *MemoryStore) Set(ctx context.Context, key string, value []byte) error {
	if err := m.sleep(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(value))
	copy(cp, value)
	m.data[key] = cp
	return nil
}

func (m *MemoryStore) Delete(ctx context.Context, key string) error {
	if err := m.sleep(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

// Keys returns all keys in the store.
func (m *MemoryStore) Keys() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]string, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	return keys
}
