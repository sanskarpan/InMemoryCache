package cache

import (
	"time"

	"github.com/yourname/cache-engine/internal/stats"
)

// Entry represents a single cache entry for snapshot/visualization.
type Entry struct {
	Key       string    `json:"key"`
	Value     []byte    `json:"value,omitempty"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
	TTLMs     int64     `json:"ttlMs,omitempty"`
	Freq      int       `json:"freq"`
	CreatedAt time.Time `json:"createdAt"`
	HitCount  int64     `json:"hitCount"`
	SizeBytes int       `json:"sizeBytes"`
	List      string    `json:"list,omitempty"` // T1/T2/B1/B2 for ARC
	Position  int       `json:"position"`
}

// SnapshotResult is returned by Snapshot().
type SnapshotResult struct {
	Policy       string         `json:"policy"`
	Entries      []Entry        `json:"entries"`
	GhostEntries []Entry        `json:"ghostEntries,omitempty"`
	ArcP         int            `json:"arcP,omitempty"`
	ListSizes    map[string]int `json:"listSizes,omitempty"`
	Capacity     int            `json:"capacity"`
	Truncated    bool           `json:"truncated,omitempty"`
}

// PolicyType identifies the eviction policy.
type PolicyType string

const (
	PolicyLRU PolicyType = "lru"
	PolicyLFU PolicyType = "lfu"
	PolicyARC PolicyType = "arc"
)

// WritePolicyType identifies the write strategy.
type WritePolicyType string

const (
	WritePolicyThrough WritePolicyType = "write-through"
	WritePolicyBack    WritePolicyType = "write-back"
	WritePolicyAround  WritePolicyType = "write-around"
)

// Cache is the core interface for all cache implementations.
type Cache interface {
	Get(key string) ([]byte, bool)
	Set(key string, value []byte, ttl time.Duration) error
	Delete(key string) bool
	Peek(key string) ([]byte, bool)
	Keys() []string
	Len() int
	Capacity() int
	Stats() *stats.Stats
	Snapshot() SnapshotResult
	Purge()
	Close()
	// SetEvictionCallback registers a function called when an entry is evicted.
	SetEvictionCallback(fn func(key string, value []byte))
}
