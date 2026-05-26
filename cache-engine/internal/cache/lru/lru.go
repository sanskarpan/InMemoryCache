package lru

import (
	"sync"
	"time"

	"github.com/yourname/cache-engine/internal/cache"
	"github.com/yourname/cache-engine/internal/stats"
)

type lruNode struct {
	key       string
	value     []byte
	expiresAt time.Time
	hitCount  int64
	createdAt time.Time
	prev      *lruNode
	next      *lruNode
}

// LRUCache implements a Least Recently Used cache.
type LRUCache struct {
	mu       sync.Mutex
	capacity int
	items    map[string]*lruNode
	head     *lruNode // MRU
	tail     *lruNode // LRU
	st       *stats.Stats
	onEvict  func(key string, value []byte)
}

// New creates a new LRU cache with the given capacity.
func New(capacity int) *LRUCache {
	if capacity <= 0 {
		capacity = 1
	}
	return &LRUCache{
		capacity: capacity,
		items:    make(map[string]*lruNode),
		st:       &stats.Stats{},
	}
}

func (c *LRUCache) pushFront(n *lruNode) {
	n.prev = nil
	n.next = c.head
	if c.head != nil {
		c.head.prev = n
	}
	c.head = n
	if c.tail == nil {
		c.tail = n
	}
}

func (c *LRUCache) removeNode(n *lruNode) {
	if n.prev != nil {
		n.prev.next = n.next
	} else {
		c.head = n.next
	}
	if n.next != nil {
		n.next.prev = n.prev
	} else {
		c.tail = n.prev
	}
	n.prev = nil
	n.next = nil
}

func (c *LRUCache) moveToFront(n *lruNode) {
	if n == c.head {
		return
	}
	c.removeNode(n)
	c.pushFront(n)
}

// Get retrieves a value and moves it to MRU position.
func (c *LRUCache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	n, ok := c.items[key]
	if !ok {
		c.st.Misses.Add(1)
		return nil, false
	}

	now := time.Now()
	if !n.expiresAt.IsZero() && now.After(n.expiresAt) {
		c.st.BytesStored.Add(-int64(len(n.value)))
		c.removeNode(n)
		delete(c.items, key)
		c.st.Misses.Add(1)
		c.st.TTLExpiries.Add(1)
		return nil, false
	}

	c.moveToFront(n)
	n.hitCount++
	c.st.Hits.Add(1)
	return append([]byte(nil), n.value...), true
}

// Set inserts or updates a key.
func (c *LRUCache) Set(key string, value []byte, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = now.Add(ttl)
	}

	if n, ok := c.items[key]; ok {
		valueCopy := append([]byte(nil), value...)
		// Update: adjust BytesStored by the delta
		c.st.BytesStored.Add(int64(len(valueCopy)) - int64(len(n.value)))
		n.value = valueCopy
		n.expiresAt = expiresAt
		c.moveToFront(n)
		c.st.Sets.Add(1)
		return nil
	}

	if len(c.items) >= c.capacity {
		// Evict LRU (tail)
		evicted := c.tail
		if evicted != nil {
			c.st.BytesStored.Add(-int64(len(evicted.value)))
			c.removeNode(evicted)
			delete(c.items, evicted.key)
			c.st.Evictions.Add(1)
			if c.onEvict != nil {
				c.onEvict(evicted.key, append([]byte(nil), evicted.value...))
			}
		}
	}

	n := &lruNode{
		key:       key,
		value:     append([]byte(nil), value...),
		expiresAt: expiresAt,
		createdAt: now,
	}
	c.pushFront(n)
	c.items[key] = n
	c.st.Sets.Add(1)
	c.st.BytesStored.Add(int64(len(value)))
	return nil
}

// Delete removes a key from the cache.
func (c *LRUCache) Delete(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	n, ok := c.items[key]
	if !ok {
		return false
	}
	c.st.BytesStored.Add(-int64(len(n.value)))
	c.removeNode(n)
	delete(c.items, key)
	c.st.Deletes.Add(1)
	return true
}

// Peek retrieves a value without affecting eviction order.
func (c *LRUCache) Peek(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	n, ok := c.items[key]
	if !ok {
		return nil, false
	}
	if !n.expiresAt.IsZero() && time.Now().After(n.expiresAt) {
		c.st.BytesStored.Add(-int64(len(n.value)))
		c.st.TTLExpiries.Add(1)
		c.removeNode(n)
		delete(c.items, key)
		return nil, false
	}
	return append([]byte(nil), n.value...), true
}

// Keys returns all non-expired keys in MRU→LRU order.
func (c *LRUCache) Keys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	keys := make([]string, 0, len(c.items))
	cur := c.head
	for cur != nil {
		if cur.expiresAt.IsZero() || now.Before(cur.expiresAt) {
			keys = append(keys, cur.key)
		}
		cur = cur.next
	}
	return keys
}

// Len returns the number of entries.
func (c *LRUCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

// Capacity returns the max capacity.
func (c *LRUCache) Capacity() int { return c.capacity }

// Stats returns the stats struct.
func (c *LRUCache) Stats() *stats.Stats { return c.st }

// Snapshot returns a slice of entries ordered MRU→LRU.
func (c *LRUCache) Snapshot() cache.SnapshotResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	entries := make([]cache.Entry, 0, len(c.items))
	pos := 0
	cur := c.head
	for cur != nil {
		var ttlMs int64
		if !cur.expiresAt.IsZero() {
			remaining := cur.expiresAt.Sub(now)
			if remaining > 0 {
				ttlMs = remaining.Milliseconds()
			}
		}
		entries = append(entries, cache.Entry{
			Key:       cur.key,
			Value:     append([]byte(nil), cur.value...),
			ExpiresAt: cur.expiresAt,
			TTLMs:     ttlMs,
			Freq:      1,
			CreatedAt: cur.createdAt,
			HitCount:  cur.hitCount,
			SizeBytes: len(cur.value),
			Position:  pos,
		})
		pos++
		cur = cur.next
	}
	return cache.SnapshotResult{Policy: "lru", Entries: entries, Capacity: c.capacity}
}

// Purge removes all entries.
func (c *LRUCache) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*lruNode)
	c.head = nil
	c.tail = nil
	c.st.Reset()
}

// Close is a no-op for base LRU (no background goroutines).
func (c *LRUCache) Close() {}

// SetEvictionCallback registers an eviction callback.
func (c *LRUCache) SetEvictionCallback(fn func(key string, value []byte)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onEvict = fn
}
