package store

import (
	"context"
	"time"
)

// BackingStore is the interface for persistent/remote storage.
type BackingStore interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte) error
	Delete(ctx context.Context, key string) error
}

// ConfigurableStore is a backing store with runtime-configurable latency.
type ConfigurableStore interface {
	BackingStore
	SetLatency(d time.Duration)
	GetLatency() time.Duration
}
