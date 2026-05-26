package ttl

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWheel_Expiry(t *testing.T) {
	var mu sync.Mutex
	expired := []string{}
	w := New(func(key string) {
		mu.Lock()
		expired = append(expired, key)
		mu.Unlock()
	})
	defer w.Stop()

	w.Schedule("k", 50*time.Millisecond)
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	assert.Contains(t, expired, "k")
	mu.Unlock()
}

func TestWheel_Cancel(t *testing.T) {
	var mu sync.Mutex
	expired := []string{}
	w := New(func(key string) {
		mu.Lock()
		expired = append(expired, key)
		mu.Unlock()
	})
	defer w.Stop()

	w.Schedule("k", 50*time.Millisecond)
	w.Cancel("k")
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	assert.NotContains(t, expired, "k")
	mu.Unlock()
}

func TestWheel_MultipleOrder(t *testing.T) {
	var mu sync.Mutex
	order := []string{}
	w := New(func(key string) {
		mu.Lock()
		order = append(order, key)
		mu.Unlock()
	})
	defer w.Stop()

	w.Schedule("b", 80*time.Millisecond)
	w.Schedule("a", 40*time.Millisecond)
	w.Schedule("c", 120*time.Millisecond)

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	assert.Equal(t, []string{"a", "b", "c"}, order)
	mu.Unlock()
}

func TestWheel_Reschedule(t *testing.T) {
	var mu sync.Mutex
	count := 0
	w := New(func(key string) {
		mu.Lock()
		count++
		mu.Unlock()
	})
	defer w.Stop()

	w.Schedule("k", 50*time.Millisecond)
	// Reschedule extends the TTL
	w.Schedule("k", 200*time.Millisecond)

	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	assert.Equal(t, 0, count, "should not have fired yet after reschedule")
	mu.Unlock()
}
