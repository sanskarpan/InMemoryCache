package coherence

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourname/cache-engine/internal/cache"
	"github.com/yourname/cache-engine/internal/cache/lru"
)

func makeNodes() map[string]cache.Cache {
	return map[string]cache.Cache{
		"node-a": lru.New(100),
		"node-b": lru.New(100),
		"node-c": lru.New(100),
	}
}

func TestCoordinator_Invalidation(t *testing.T) {
	bus := NewBus()
	nodes := makeNodes()
	coord := NewCoordinator(nodes, bus)
	defer coord.Close()

	// Pre-populate all nodes with "k"
	nodes["node-a"].Set("k", []byte("old"), 0)
	nodes["node-b"].Set("k", []byte("old"), 0)
	nodes["node-c"].Set("k", []byte("old"), 0)

	// Set on node-a — should invalidate b and c
	coord.Set("node-a", "k", []byte("new"), 0)

	// Give async handlers time to process
	time.Sleep(20 * time.Millisecond)

	_, okB := nodes["node-b"].Peek("k")
	_, okC := nodes["node-c"].Peek("k")
	assert.False(t, okB, "node-b should have k invalidated")
	assert.False(t, okC, "node-c should have k invalidated")

	// node-a still has it
	v, okA := nodes["node-a"].Peek("k")
	assert.True(t, okA)
	assert.Equal(t, []byte("new"), v)
}

func TestCoordinator_OwnEventIgnored(t *testing.T) {
	bus := NewBus()
	nodes := makeNodes()
	coord := NewCoordinator(nodes, bus)
	defer coord.Close()

	nodes["node-a"].Set("k", []byte("v"), 0)
	coord.Set("node-a", "k", []byte("new"), 0)

	time.Sleep(20 * time.Millisecond)

	// node-a should still have the key
	v, ok := nodes["node-a"].Peek("k")
	assert.True(t, ok)
	assert.Equal(t, []byte("new"), v)
}

func TestBus_SlowSubscriber(t *testing.T) {
	bus := NewBus()
	ch := bus.Subscribe("slow")

	// Fill up the buffer (capacity 100)
	for i := 0; i < 110; i++ {
		bus.Publish(Event{Type: EventInvalidate, Key: "k", From: "other"})
	}
	// Should not block — excess events dropped
	assert.LessOrEqual(t, len(ch), 100)
}

func TestCoordinator_UnknownNode(t *testing.T) {
	bus := NewBus()
	nodes := makeNodes()
	coord := NewCoordinator(nodes, bus)
	defer coord.Close()

	err := coord.Set("missing", "k", []byte("v"), 0)
	require.Error(t, err)
}
