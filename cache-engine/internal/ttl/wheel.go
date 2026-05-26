package ttl

import (
	"container/heap"
	"sync"
	"time"
)

type ttlEntry struct {
	key   string
	expAt time.Time
	index int
}

type ttlHeap []*ttlEntry

func (h ttlHeap) Len() int            { return len(h) }
func (h ttlHeap) Less(i, j int) bool  { return h[i].expAt.Before(h[j].expAt) }
func (h ttlHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}
func (h *ttlHeap) Push(x any) {
	e := x.(*ttlEntry)
	e.index = len(*h)
	*h = append(*h, e)
}
func (h *ttlHeap) Pop() any {
	old := *h
	n := len(old)
	e := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	e.index = -1
	return e
}

// Wheel manages TTL expiration using a min-heap.
type Wheel struct {
	mu       sync.Mutex
	h        ttlHeap
	index    map[string]*ttlEntry
	onExpire func(key string)
	stop     chan struct{}
	trigger  chan struct{}
}

// New creates a new TTL Wheel and starts the background goroutine.
func New(onExpire func(key string)) *Wheel {
	w := &Wheel{
		h:        make(ttlHeap, 0),
		index:    make(map[string]*ttlEntry),
		onExpire: onExpire,
		stop:     make(chan struct{}),
		trigger:  make(chan struct{}, 1),
	}
	heap.Init(&w.h)
	go w.run()
	return w
}

// Schedule adds or updates a TTL for key.
func (w *Wheel) Schedule(key string, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	expAt := time.Now().Add(ttl)
	w.mu.Lock()
	if e, ok := w.index[key]; ok {
		// Cancel old and reschedule
		heap.Remove(&w.h, e.index)
		delete(w.index, key)
	}
	e := &ttlEntry{key: key, expAt: expAt}
	heap.Push(&w.h, e)
	w.index[key] = e
	w.mu.Unlock()

	// Wake up the run goroutine
	select {
	case w.trigger <- struct{}{}:
	default:
	}
}

// Cancel removes a key's TTL.
func (w *Wheel) Cancel(key string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if e, ok := w.index[key]; ok {
		heap.Remove(&w.h, e.index)
		delete(w.index, key)
	}
}

func (w *Wheel) run() {
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()

	for {
		w.mu.Lock()
		var nextDur time.Duration
		if w.h.Len() > 0 {
			nextDur = time.Until(w.h[0].expAt)
			if nextDur < 0 {
				nextDur = 0
			}
		} else {
			nextDur = time.Hour
		}
		w.mu.Unlock()

		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(nextDur)

		select {
		case <-w.stop:
			return
		case <-w.trigger:
			// Re-evaluate next expiry
			continue
		case <-timer.C:
			now := time.Now()
			w.mu.Lock()
			var expired []string
			for w.h.Len() > 0 && !w.h[0].expAt.After(now) {
				e := heap.Pop(&w.h).(*ttlEntry)
				delete(w.index, e.key)
				expired = append(expired, e.key)
			}
			w.mu.Unlock()

			for _, key := range expired {
				w.onExpire(key)
			}
		}
	}
}

// Stop shuts down the background goroutine.
func (w *Wheel) Stop() {
	close(w.stop)
}
