package coherence

import (
	"fmt"
	"sync"
	"time"

	"github.com/yourname/cache-engine/internal/cache"
)

// NodeSnapshot holds the state of a single cache node.
type NodeSnapshot struct {
	NodeID  string        `json:"nodeId"`
	Entries []cache.Entry `json:"entries"`
	Len     int           `json:"len"`
}

// Coordinator manages multi-node cache invalidation.
type Coordinator struct {
	mu        sync.RWMutex
	nodes     map[string]cache.Cache
	bus       *EventBus
	done      map[string]chan struct{}
	closeOnce sync.Once
}

// NewCoordinator creates a coordinator with the given nodes.
func NewCoordinator(nodes map[string]cache.Cache, bus *EventBus) *Coordinator {
	c := &Coordinator{
		nodes: nodes,
		bus:   bus,
		done:  make(map[string]chan struct{}),
	}
	for nodeID := range nodes {
		ch := bus.Subscribe(nodeID)
		done := make(chan struct{})
		c.done[nodeID] = done
		go c.handleEvents(nodeID, ch, done)
	}
	return c
}

func (c *Coordinator) handleEvents(nodeID string, ch chan Event, done chan struct{}) {
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return
			}
			if e.From == nodeID {
				continue
			}
			c.mu.RLock()
			n, exists := c.nodes[nodeID]
			c.mu.RUnlock()
			if !exists {
				continue
			}
			switch e.Type {
			case EventInvalidate:
				n.Delete(e.Key)
			case EventUpdate:
				_ = n.Set(e.Key, e.Value, 0)
			}
		case <-done:
			return
		}
	}
}

// Set writes a key on the given node and broadcasts invalidation to others.
func (c *Coordinator) Set(nodeID, key string, value []byte, ttl time.Duration) error {
	c.mu.RLock()
	n, ok := c.nodes[nodeID]
	c.mu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown node: %s", nodeID)
	}
	if err := n.Set(key, value, ttl); err != nil {
		return err
	}
	c.bus.Publish(Event{Type: EventInvalidate, Key: key, From: nodeID})
	return nil
}

// Snapshot returns the state of all nodes.
func (c *Coordinator) Snapshot() map[string]NodeSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]NodeSnapshot)
	for id, n := range c.nodes {
		snap := n.Snapshot()
		result[id] = NodeSnapshot{
			NodeID:  id,
			Entries: snap.Entries,
			Len:     n.Len(),
		}
	}
	return result
}

// Bus returns the event bus (for SSE).
func (c *Coordinator) Bus() *EventBus { return c.bus }

// Close shuts down all event handlers.
func (c *Coordinator) Close() {
	c.closeOnce.Do(func() {
		for nodeID, done := range c.done {
			close(done)
			c.bus.Unsubscribe(nodeID)
		}
	})
}
