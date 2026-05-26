package stats

import (
	"sync"
	"time"
)

// StatsBucket holds stats for a 1-second window.
type StatsBucket struct {
	Timestamp time.Time `json:"timestamp"`
	Hits      int64     `json:"hits"`
	Misses    int64     `json:"misses"`
	Evictions int64     `json:"evictions"`
}

// WindowedStats maintains a 60-second rolling window of stats.
type WindowedStats struct {
	mu      sync.Mutex
	buckets [60]StatsBucket
	head    int
	ticker  *time.Ticker
	done    chan struct{}
}

// NewWindowedStats creates a new WindowedStats and starts the background ticker.
func NewWindowedStats() *WindowedStats {
	w := &WindowedStats{
		ticker: time.NewTicker(time.Second),
		done:   make(chan struct{}),
	}
	now := time.Now()
	for i := range w.buckets {
		w.buckets[i].Timestamp = now
	}
	go w.advance()
	return w
}

func (w *WindowedStats) advance() {
	for {
		select {
		case t := <-w.ticker.C:
			w.mu.Lock()
			w.head = (w.head + 1) % 60
			w.buckets[w.head] = StatsBucket{Timestamp: t}
			w.mu.Unlock()
		case <-w.done:
			return
		}
	}
}

// Record records a single operation into the current bucket.
func (w *WindowedStats) Record(hit bool, eviction bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if hit {
		w.buckets[w.head].Hits++
	} else {
		w.buckets[w.head].Misses++
	}
	if eviction {
		w.buckets[w.head].Evictions++
	}
}

// Series returns the last n seconds of data, oldest first.
func (w *WindowedStats) Series(n int) []StatsBucket {
	if n > 60 {
		n = 60
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	result := make([]StatsBucket, n)
	for i := 0; i < n; i++ {
		idx := (w.head - n + 1 + i + 60) % 60
		result[i] = w.buckets[idx]
	}
	return result
}

// Stop stops the background goroutine.
func (w *WindowedStats) Stop() {
	w.ticker.Stop()
	close(w.done)
}
