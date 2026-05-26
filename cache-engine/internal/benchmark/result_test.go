package benchmark

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputeLatencyStatsUsesExactAverage(t *testing.T) {
	avg, p50, p95, p99 := computeLatencyStats([]int64{10, 20}, 1000, 4)

	assert.Equal(t, int64(250), avg)
	assert.Equal(t, int64(10), p50)
	assert.Equal(t, int64(10), p95)
	assert.Equal(t, int64(10), p99)
}
