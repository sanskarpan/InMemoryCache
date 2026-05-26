package lfu

import (
	"sync"
	"time"

	"github.com/yourname/cache-engine/internal/cache"
	"github.com/yourname/cache-engine/internal/cache/list"
	"github.com/yourname/cache-engine/internal/stats"
)

type lfuNode struct {
	key       string
	value     []byte
	freq      int
	expiresAt time.Time
	createdAt time.Time
	hitCount  int64
	// list node pointer — stored on the node for O(1) removal
	listNode *list.Node[*lfuNode]
}

// LFUCache implements an O(1) Least Frequently Used cache (Shah et al. 2010).
type LFUCache struct {
	mu       sync.Mutex
	capacity int
	minFreq  int
	items    map[string]*lfuNode
	freqList map[int]*list.List[*lfuNode]
	st       *stats.Stats
	onEvict  func(key string, value []byte)
}

// New creates a new LFU cache.
func New(capacity int) *LFUCache {
	if capacity <= 0 {
		capacity = 1
	}
	return &LFUCache{
		capacity: capacity,
		items:    make(map[string]*lfuNode),
		freqList: make(map[int]*list.List[*lfuNode]),
		st:       &stats.Stats{},
	}
}

func (c *LFUCache) incrementFreq(n *lfuNode) {
	oldFreq := n.freq
	fl := c.freqList[oldFreq]
	if fl != nil {
		fl.Remove(n.listNode)
		if fl.Len() == 0 {
			delete(c.freqList, oldFreq)
			if oldFreq == c.minFreq {
				c.minFreq = oldFreq + 1
			}
		}
	}

	n.freq++
	if c.freqList[n.freq] == nil {
		c.freqList[n.freq] = list.New[*lfuNode]()
	}
	n.listNode = c.freqList[n.freq].PushFront(n)
}

func (c *LFUCache) evictOne() {
	fl := c.freqList[c.minFreq]
	if fl == nil || fl.Len() == 0 {
		return
	}
	// evict LRU at minFreq (tail = least recently used at this frequency)
	victim := fl.Back()
	if victim == nil {
		return
	}
	fl.Remove(victim)
	if fl.Len() == 0 {
		delete(c.freqList, c.minFreq)
	}
	node := victim.Value
	c.st.BytesStored.Add(-int64(len(node.value)))
	delete(c.items, node.key)
	c.st.Evictions.Add(1)
	if c.onEvict != nil {
		c.onEvict(node.key, append([]byte(nil), node.value...))
	}
}

// Get retrieves a value and increments its frequency.
func (c *LFUCache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	n, ok := c.items[key]
	if !ok {
		c.st.Misses.Add(1)
		return nil, false
	}

	if !n.expiresAt.IsZero() && time.Now().After(n.expiresAt) {
		// Remove from freq list
		fl := c.freqList[n.freq]
		if fl != nil {
			fl.Remove(n.listNode)
			if fl.Len() == 0 {
				delete(c.freqList, n.freq)
			}
		}
		c.st.BytesStored.Add(-int64(len(n.value)))
		delete(c.items, key)
		c.st.Misses.Add(1)
		c.st.TTLExpiries.Add(1)
		return nil, false
	}

	c.incrementFreq(n)
	n.hitCount++
	c.st.Hits.Add(1)
	return append([]byte(nil), n.value...), true
}

// Set inserts or updates a key.
func (c *LFUCache) Set(key string, value []byte, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = now.Add(ttl)
	}

	if n, ok := c.items[key]; ok {
		valueCopy := append([]byte(nil), value...)
		// Adjust BytesStored by the delta
		c.st.BytesStored.Add(int64(len(valueCopy)) - int64(len(n.value)))
		n.value = valueCopy
		n.expiresAt = expiresAt
		c.incrementFreq(n)
		c.st.Sets.Add(1)
		return nil
	}

	if len(c.items) >= c.capacity {
		c.evictOne()
	}

	n := &lfuNode{
		key:       key,
		value:     append([]byte(nil), value...),
		freq:      1,
		expiresAt: expiresAt,
		createdAt: now,
	}
	if c.freqList[1] == nil {
		c.freqList[1] = list.New[*lfuNode]()
	}
	n.listNode = c.freqList[1].PushFront(n)
	c.items[key] = n
	c.minFreq = 1
	c.st.Sets.Add(1)
	c.st.BytesStored.Add(int64(len(value)))
	return nil
}

// Delete removes a key.
func (c *LFUCache) Delete(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	n, ok := c.items[key]
	if !ok {
		return false
	}
	fl := c.freqList[n.freq]
	if fl != nil {
		fl.Remove(n.listNode)
		if fl.Len() == 0 {
			delete(c.freqList, n.freq)
		}
	}
	c.st.BytesStored.Add(-int64(len(n.value)))
	delete(c.items, key)
	c.st.Deletes.Add(1)
	return true
}

// Peek returns a value without changing frequency.
func (c *LFUCache) Peek(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	n, ok := c.items[key]
	if !ok {
		return nil, false
	}
	if !n.expiresAt.IsZero() && time.Now().After(n.expiresAt) {
		// Clean up expired entry from freqList to avoid memory leak
		fl := c.freqList[n.freq]
		if fl != nil {
			fl.Remove(n.listNode)
			if fl.Len() == 0 {
				delete(c.freqList, n.freq)
			}
		}
		c.st.BytesStored.Add(-int64(len(n.value)))
		c.st.TTLExpiries.Add(1)
		delete(c.items, key)
		return nil, false
	}
	return append([]byte(nil), n.value...), true
}

// Keys returns all non-expired keys.
func (c *LFUCache) Keys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	keys := make([]string, 0, len(c.items))
	for k, n := range c.items {
		if n.expiresAt.IsZero() || now.Before(n.expiresAt) {
			keys = append(keys, k)
		}
	}
	return keys
}

// Len returns the number of entries.
func (c *LFUCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

// Capacity returns the max capacity.
func (c *LFUCache) Capacity() int { return c.capacity }

// Stats returns the stats struct.
func (c *LFUCache) Stats() *stats.Stats { return c.st }

// Snapshot returns all entries grouped by frequency.
func (c *LFUCache) Snapshot() cache.SnapshotResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	entries := make([]cache.Entry, 0, len(c.items))
	pos := 0
	for freq, fl := range c.freqList {
		node := fl.Front()
		for node != nil {
			n := node.Value
			var ttlMs int64
			if !n.expiresAt.IsZero() {
				remaining := n.expiresAt.Sub(now)
				if remaining > 0 {
					ttlMs = remaining.Milliseconds()
				}
			}
			entries = append(entries, cache.Entry{
				Key:       n.key,
				Value:     append([]byte(nil), n.value...),
				ExpiresAt: n.expiresAt,
				TTLMs:     ttlMs,
				Freq:      freq,
				CreatedAt: n.createdAt,
				HitCount:  n.hitCount,
				SizeBytes: len(n.value),
				Position:  pos,
			})
			pos++
			node = node.Next()
		}
	}
	return cache.SnapshotResult{Policy: "lfu", Entries: entries, Capacity: c.capacity}
}

// Purge removes all entries.
func (c *LFUCache) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*lfuNode)
	c.freqList = make(map[int]*list.List[*lfuNode])
	c.minFreq = 0
	c.st.Reset()
}

// Close is a no-op.
func (c *LFUCache) Close() {}

// SetEvictionCallback registers an eviction callback.
func (c *LFUCache) SetEvictionCallback(fn func(key string, value []byte)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onEvict = fn
}
