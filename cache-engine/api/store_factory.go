package api

import (
	"fmt"
	"sync"
	"time"

	"github.com/yourname/cache-engine/internal/cache"
	"github.com/yourname/cache-engine/internal/cache/arc"
	"github.com/yourname/cache-engine/internal/cache/lfu"
	"github.com/yourname/cache-engine/internal/cache/lru"
	"github.com/yourname/cache-engine/internal/store"
)

type storeKind string

const (
	storeKindStandard storeKind = "standard"
	storeKindSharded  storeKind = "sharded"
)

type StoreEntry struct {
	mu               sync.RWMutex
	Cache            cache.Cache
	Policy           string
	WritePolicy      string
	BackingStore     store.ConfigurableStore
	Kind             storeKind
	AllowWritePolicy bool
}

func NewStoreEntry(policy string, writePolicy string, capacity int, backing store.ConfigurableStore) (*StoreEntry, error) {
	base, err := newBaseCache(policy, capacity)
	if err != nil {
		return nil, err
	}
	wrapped, err := wrapCache(base, writePolicy, backing)
	if err != nil {
		base.Close()
		return nil, err
	}
	return &StoreEntry{
		Cache:            wrapped,
		Policy:           policy,
		WritePolicy:      writePolicy,
		BackingStore:     backing,
		Kind:             storeKindStandard,
		AllowWritePolicy: backing != nil,
	}, nil
}

func newBaseCache(policy string, capacity int) (cache.Cache, error) {
	switch policy {
	case "lru":
		return lru.New(capacity), nil
	case "lfu":
		return lfu.New(capacity), nil
	case "arc":
		return arc.New(capacity), nil
	default:
		return nil, fmt.Errorf("unsupported policy: %s", policy)
	}
}

func wrapCache(base cache.Cache, writePolicy string, backing store.ConfigurableStore) (cache.Cache, error) {
	switch writePolicy {
	case "write-through":
		if backing == nil {
			return nil, fmt.Errorf("write-through requires a backing store")
		}
		return store.NewWriteThrough(base, backing), nil
	case "write-back":
		if backing == nil {
			return nil, fmt.Errorf("write-back requires a backing store")
		}
		return store.NewWriteBack(base, backing, 100*time.Millisecond), nil
	case "write-around":
		if backing == nil {
			return nil, fmt.Errorf("write-around requires a backing store")
		}
		return store.NewWriteAround(base, backing), nil
	case "none":
		return base, nil
	default:
		return nil, fmt.Errorf("unsupported write policy: %s", writePolicy)
	}
}
