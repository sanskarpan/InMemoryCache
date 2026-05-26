package coherence

import "sync"

// EventType identifies the type of coherence event.
type EventType int

const (
	EventInvalidate EventType = iota
	EventUpdate
	EventFlush
)

// Event is a cache coherence event.
type Event struct {
	Type  EventType `json:"type"`
	Key   string    `json:"key"`
	Value []byte    `json:"value,omitempty"`
	From  string    `json:"from"`
}

// EventBus is an in-process pub/sub bus for coherence events.
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[string]chan Event
}

// NewBus creates a new EventBus.
func NewBus() *EventBus {
	return &EventBus{
		subscribers: make(map[string]chan Event),
	}
}

// Subscribe returns a channel that receives events for the given nodeID.
func (b *EventBus) Subscribe(nodeID string) chan Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan Event, 100)
	b.subscribers[nodeID] = ch
	return ch
}

// Unsubscribe removes the subscription.
func (b *EventBus) Unsubscribe(nodeID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ch, ok := b.subscribers[nodeID]; ok {
		close(ch)
		delete(b.subscribers, nodeID)
	}
}

// Publish sends an event to all subscribers except the sender.
func (b *EventBus) Publish(e Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for nodeID, ch := range b.subscribers {
		if nodeID == e.From {
			continue
		}
		// Non-blocking: drop incoming event if subscriber buffer is full.
		// Each subscriber has a 100-event buffer; slow consumers silently miss events.
		select {
		case ch <- e:
		default:
		}
	}
}
