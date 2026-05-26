package arc

import (
	"sync"
	"time"

	"github.com/yourname/cache-engine/internal/cache"
	"github.com/yourname/cache-engine/internal/cache/list"
	"github.com/yourname/cache-engine/internal/stats"
)

type arcNode struct {
	key       string
	value     []byte
	list      string // "T1", "T2", "B1", "B2"
	expiresAt time.Time
	createdAt time.Time
	hitCount  int64
	listNode  *list.Node[*arcNode]
}

// ARCCache implements the Adaptive Replacement Cache (Megiddo & Modha, 2003).
type ARCCache struct {
	mu       sync.Mutex
	capacity int
	p        int // target size for T1

	T1 *list.List[*arcNode] // recently used once (in cache)
	T2 *list.List[*arcNode] // used multiple times (in cache)
	B1 *list.List[*arcNode] // ghost of evicted T1
	B2 *list.List[*arcNode] // ghost of evicted T2

	itemMap map[string]*arcNode
	st      *stats.Stats
	onEvict func(key string, value []byte)
}

// New creates a new ARC cache.
func New(capacity int) *ARCCache {
	if capacity <= 0 {
		capacity = 1
	}
	return &ARCCache{
		capacity: capacity,
		T1:       list.New[*arcNode](),
		T2:       list.New[*arcNode](),
		B1:       list.New[*arcNode](),
		B2:       list.New[*arcNode](),
		itemMap:  make(map[string]*arcNode),
		st:       &stats.Stats{},
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (c *ARCCache) removeFromList(n *arcNode) {
	switch n.list {
	case "T1":
		c.T1.Remove(n.listNode)
	case "T2":
		c.T2.Remove(n.listNode)
	case "B1":
		c.B1.Remove(n.listNode)
	case "B2":
		c.B2.Remove(n.listNode)
	}
}

// replace evicts a live entry from T1 or T2 and adds it as a ghost.
func (c *ARCCache) replace(preferT2 bool) {
	if c.T1.Len() > 0 && (c.T1.Len() > c.p || (preferT2 && c.T1.Len() == c.p)) {
		// Evict LRU of T1 → ghost in B1
		lru := c.T1.Back()
		c.T1.Remove(lru)
		n := lru.Value
		c.st.BytesStored.Add(-int64(len(n.value)))
		if c.onEvict != nil && n.value != nil {
			c.onEvict(n.key, append([]byte(nil), n.value...))
		}
		n.value = nil
		n.list = "B1"
		n.listNode = c.B1.PushFront(n)
	} else if c.T2.Len() > 0 {
		// Evict LRU of T2 → ghost in B2
		lru := c.T2.Back()
		c.T2.Remove(lru)
		n := lru.Value
		c.st.BytesStored.Add(-int64(len(n.value)))
		if c.onEvict != nil && n.value != nil {
			c.onEvict(n.key, append([]byte(nil), n.value...))
		}
		n.value = nil
		n.list = "B2"
		n.listNode = c.B2.PushFront(n)
	}
	c.st.Evictions.Add(1)
}

// Get retrieves a value from T1 or T2.
func (c *ARCCache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	n, ok := c.itemMap[key]
	if !ok {
		c.st.Misses.Add(1)
		return nil, false
	}

	if n.list == "B1" || n.list == "B2" {
		// Ghost entry — not a live hit
		c.st.Misses.Add(1)
		return nil, false
	}

	if !n.expiresAt.IsZero() && time.Now().After(n.expiresAt) {
		c.st.BytesStored.Add(-int64(len(n.value)))
		c.removeFromList(n)
		delete(c.itemMap, key)
		c.st.Misses.Add(1)
		c.st.TTLExpiries.Add(1)
		return nil, false
	}

	// Promote T1 → T2 or move T2 to front
	c.removeFromList(n)
	n.list = "T2"
	n.listNode = c.T2.PushFront(n)
	n.hitCount++
	c.st.Hits.Add(1)
	return append([]byte(nil), n.value...), true
}

// Set inserts or updates a key following the ARC algorithm.
func (c *ARCCache) Set(key string, value []byte, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	exp := time.Time{}
	if ttl > 0 {
		exp = now.Add(ttl)
	}

	// Case 1: Key already in cache (T1 or T2) — update in place
	if n, ok := c.itemMap[key]; ok && (n.list == "T1" || n.list == "T2") {
		valueCopy := append([]byte(nil), value...)
		c.st.BytesStored.Add(int64(len(valueCopy)) - int64(len(n.value)))
		n.value = valueCopy
		n.expiresAt = exp
		c.removeFromList(n)
		n.list = "T2"
		n.listNode = c.T2.PushFront(n)
		c.st.Sets.Add(1)
		return nil
	}

	// Case 2: Key is a B1 ghost → increase p, add to T2
	if n, ok := c.itemMap[key]; ok && n.list == "B1" {
		delta := 1
		if c.B1.Len() > 0 && c.B2.Len() > c.B1.Len() {
			delta = c.B2.Len() / c.B1.Len()
		}
		c.p = min(c.p+delta, c.capacity)
		c.replace(false)
		c.B1.Remove(n.listNode)
		valueCopy := append([]byte(nil), value...)
		c.st.BytesStored.Add(int64(len(valueCopy))) // ghost → live: add new value size
		n.value = valueCopy
		n.list = "T2"
		n.expiresAt = exp
		n.listNode = c.T2.PushFront(n)
		c.st.Sets.Add(1)
		return nil
	}

	// Case 3: Key is a B2 ghost → decrease p, add to T2
	if n, ok := c.itemMap[key]; ok && n.list == "B2" {
		delta := 1
		if c.B2.Len() > 0 && c.B1.Len() > c.B2.Len() {
			delta = c.B1.Len() / c.B2.Len()
		}
		c.p = max(c.p-delta, 0)
		c.replace(true)
		c.B2.Remove(n.listNode)
		valueCopy := append([]byte(nil), value...)
		c.st.BytesStored.Add(int64(len(valueCopy))) // ghost → live: add new value size
		n.value = valueCopy
		n.list = "T2"
		n.expiresAt = exp
		n.listNode = c.T2.PushFront(n)
		c.st.Sets.Add(1)
		return nil
	}

	// Case 4: Completely new key → add to T1
	liveSize := c.T1.Len() + c.T2.Len()

	if c.T1.Len()+c.B1.Len() == c.capacity {
		if c.T1.Len() < c.capacity {
			// Delete LRU of B1 ghost
			lruB1 := c.B1.Back()
			if lruB1 != nil {
				c.B1.Remove(lruB1)
				delete(c.itemMap, lruB1.Value.key)
			}
			c.replace(false)
		} else {
			// T1 is full (B1 is empty) — evict from T1 directly
			lruT1 := c.T1.Back()
			if lruT1 != nil {
				c.T1.Remove(lruT1)
				n := lruT1.Value
				c.st.BytesStored.Add(-int64(len(n.value)))
				if c.onEvict != nil {
					c.onEvict(n.key, append([]byte(nil), n.value...))
				}
				delete(c.itemMap, n.key)
				c.st.Evictions.Add(1)
			}
		}
	} else if liveSize >= c.capacity {
		// Total ghost+live exceeds 2*capacity — remove a B2 ghost
		if c.T1.Len()+c.T2.Len()+c.B1.Len()+c.B2.Len() >= 2*c.capacity {
			lruB2 := c.B2.Back()
			if lruB2 != nil {
				c.B2.Remove(lruB2)
				delete(c.itemMap, lruB2.Value.key)
			}
		}
		if c.T1.Len()+c.T2.Len() >= c.capacity {
			c.replace(false)
		}
	}

	// Add to T1 front
	n := &arcNode{
		key:       key,
		value:     append([]byte(nil), value...),
		list:      "T1",
		expiresAt: exp,
		createdAt: now,
	}
	n.listNode = c.T1.PushFront(n)
	c.itemMap[key] = n
	c.st.Sets.Add(1)
	c.st.BytesStored.Add(int64(len(value)))
	return nil
}

// Delete removes a key from whichever list it's in.
func (c *ARCCache) Delete(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	n, ok := c.itemMap[key]
	if !ok {
		return false
	}
	// Only decrement BytesStored for live entries (ghosts have nil value)
	if n.list == "T1" || n.list == "T2" {
		c.st.BytesStored.Add(-int64(len(n.value)))
	}
	c.removeFromList(n)
	delete(c.itemMap, key)
	c.st.Deletes.Add(1)
	return true
}

// Peek returns a value without affecting eviction order.
func (c *ARCCache) Peek(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	n, ok := c.itemMap[key]
	if !ok || n.list == "B1" || n.list == "B2" {
		return nil, false
	}
	if !n.expiresAt.IsZero() && time.Now().After(n.expiresAt) {
		c.st.BytesStored.Add(-int64(len(n.value)))
		c.removeFromList(n)
		delete(c.itemMap, key)
		c.st.TTLExpiries.Add(1)
		return nil, false
	}
	return append([]byte(nil), n.value...), true
}

// Keys returns all live non-expired keys.
func (c *ARCCache) Keys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	keys := make([]string, 0, c.T1.Len()+c.T2.Len())
	for _, n := range c.itemMap {
		if (n.list == "T1" || n.list == "T2") && (n.expiresAt.IsZero() || now.Before(n.expiresAt)) {
			keys = append(keys, n.key)
		}
	}
	return keys
}

// Len returns the number of live entries.
func (c *ARCCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.T1.Len() + c.T2.Len()
}

// Capacity returns the max capacity.
func (c *ARCCache) Capacity() int { return c.capacity }

// Stats returns the stats struct.
func (c *ARCCache) Stats() *stats.Stats { return c.st }

// Snapshot returns internal state for visualization.
func (c *ARCCache) Snapshot() cache.SnapshotResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	entries := make([]cache.Entry, 0)
	ghosts := make([]cache.Entry, 0)

	appendList := func(l *list.List[*arcNode], listName string) {
		pos := 0
		node := l.Front()
		for node != nil {
			n := node.Value
			var ttlMs int64
			if !n.expiresAt.IsZero() {
				remaining := n.expiresAt.Sub(now)
				if remaining > 0 {
					ttlMs = remaining.Milliseconds()
				}
			}
			e := cache.Entry{
				Key:       n.key,
				Value:     append([]byte(nil), n.value...),
				ExpiresAt: n.expiresAt,
				TTLMs:     ttlMs,
				CreatedAt: n.createdAt,
				HitCount:  n.hitCount,
				SizeBytes: len(n.value),
				List:      listName,
				Position:  pos,
			}
			if listName == "T1" || listName == "T2" {
				entries = append(entries, e)
			} else {
				ghosts = append(ghosts, e)
			}
			pos++
			node = node.Next()
		}
	}

	appendList(c.T1, "T1")
	appendList(c.T2, "T2")
	appendList(c.B1, "B1")
	appendList(c.B2, "B2")

	return cache.SnapshotResult{
		Policy:       "arc",
		Entries:      entries,
		GhostEntries: ghosts,
		ArcP:         c.p,
		Capacity:     c.capacity,
		ListSizes: map[string]int{
			"T1": c.T1.Len(),
			"T2": c.T2.Len(),
			"B1": c.B1.Len(),
			"B2": c.B2.Len(),
		},
	}
}

// Purge removes all entries from all lists.
func (c *ARCCache) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.T1 = list.New[*arcNode]()
	c.T2 = list.New[*arcNode]()
	c.B1 = list.New[*arcNode]()
	c.B2 = list.New[*arcNode]()
	c.itemMap = make(map[string]*arcNode)
	c.p = 0
	c.st.Reset()
}

// Close is a no-op.
func (c *ARCCache) Close() {}

// SetEvictionCallback registers an eviction callback.
func (c *ARCCache) SetEvictionCallback(fn func(key string, value []byte)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onEvict = fn
}
