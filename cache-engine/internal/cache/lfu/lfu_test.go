package lfu

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLFU_BasicGetSetDelete(t *testing.T) {
	c := New(10)
	c.Set("k", []byte("v"), 0)
	v, ok := c.Get("k")
	assert.True(t, ok)
	assert.Equal(t, []byte("v"), v)

	ok = c.Delete("k")
	assert.True(t, ok)
	_, ok = c.Get("k")
	assert.False(t, ok)
}

func TestLFU_FrequencyTracking(t *testing.T) {
	c := New(10)
	c.Set("a", []byte("1"), 0)
	c.Get("a") // freq=2
	c.Get("a") // freq=3

	// Access b once
	c.Set("b", []byte("2"), 0)
	c.Get("b") // freq=2

	// b has freq=2, a has freq=3
	// internal check via Snapshot
	snap := c.Snapshot()
	freqMap := make(map[string]int)
	for _, e := range snap.Entries {
		freqMap[e.Key] = e.Freq
	}
	assert.Equal(t, 3, freqMap["a"])
	assert.Equal(t, 2, freqMap["b"])
}

func TestLFU_EvictsLeastFrequent(t *testing.T) {
	c := New(3)
	c.Set("a", []byte("1"), 0)
	c.Set("b", []byte("2"), 0)
	c.Set("c", []byte("3"), 0)

	c.Get("a")
	c.Get("a") // freq=3
	c.Get("b") // freq=2
	// c still at freq=1

	// Adding d must evict c (lowest freq)
	c.Set("d", []byte("4"), 0)

	_, hitC := c.Get("c")
	assert.False(t, hitC, "c should be evicted (lowest freq)")

	_, hitA := c.Get("a")
	assert.True(t, hitA, "a should still be present")
}

func TestLFU_MinFreqResetOnSet(t *testing.T) {
	c := New(2)
	c.Set("a", []byte("1"), 0)
	c.Get("a") // freq=2
	c.Set("b", []byte("2"), 0) // minFreq must be reset to 1

	// Now cache is full. Add c — should evict b (freq=1), not a (freq=2)
	c.Set("c", []byte("3"), 0)

	_, okB := c.Get("b")
	assert.False(t, okB, "b should be evicted (freq=1)")
	_, okA := c.Get("a")
	assert.True(t, okA, "a should still be present (freq=2)")
}

func TestLFU_MinFreqUpdateWhenListEmpty(t *testing.T) {
	c := New(3)
	c.Set("a", []byte("1"), 0)
	c.Set("b", []byte("2"), 0)
	c.Set("c", []byte("3"), 0)
	// a=1, b=1, c=1; minFreq=1

	c.Get("a") // a=2
	c.Get("b") // b=2
	c.Get("c") // c=2; now all are freq=2, freqList[1] is empty → minFreq should be 2

	// Set new key: minFreq = 1
	c.Set("d", []byte("4"), 0)
	// Should evict one of the freq=1 entries (d was just inserted at freq=1)
	assert.Equal(t, 3, c.Len())
}

func TestLFU_Capacity1(t *testing.T) {
	c := New(1)
	c.Set("a", []byte("1"), 0)
	c.Set("b", []byte("2"), 0)
	_, ok := c.Get("a")
	assert.False(t, ok)
	_, ok = c.Get("b")
	assert.True(t, ok)
}

func TestLFU_TTLExpiry(t *testing.T) {
	c := New(100)
	c.Set("k", []byte("v"), 50*time.Millisecond)

	_, hit := c.Get("k")
	assert.True(t, hit)

	time.Sleep(100 * time.Millisecond)

	_, hit = c.Get("k")
	assert.False(t, hit)
	assert.Equal(t, int64(1), c.Stats().TTLExpiries.Load())
}

func TestLFU_Peek_NoFreqChange(t *testing.T) {
	c := New(2)
	c.Set("a", []byte("1"), 0)
	c.Set("b", []byte("2"), 0)
	c.Get("b") // b freq=2

	// Peek a should not change its freq=1
	v, ok := c.Peek("a")
	assert.True(t, ok)
	assert.Equal(t, []byte("1"), v)

	// Add c — should evict a (freq=1), not b (freq=2)
	c.Set("c", []byte("3"), 0)
	_, okA := c.Get("a")
	assert.False(t, okA, "a should be evicted (freq=1)")
}

func TestLFU_Concurrent(t *testing.T) {
	c := New(100)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("k%d", i%20)
			c.Set(key, []byte("v"), 0)
			c.Get(key)
		}(i)
	}
	wg.Wait()
}
