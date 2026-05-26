package benchmark

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBenchmark_ZipfHitRates(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping benchmark test in short mode")
	}

	r := &Runner{}
	results := r.Run(Config{
		Duration:    5 * time.Second,
		Concurrency: 2,
		CacheSize:   500,
		WorkloadN:   10000,
		Workload:    "zipf",
		Policies:    []string{"lru", "lfu", "arc"},
	})

	var lruHR, lfuHR, arcHR float64
	for _, res := range results {
		switch res.Policy {
		case "lru":
			lruHR = res.HitRate
		case "lfu":
			lfuHR = res.HitRate
		case "arc":
			arcHR = res.HitRate
		}
	}

	assert.Greater(t, arcHR, lruHR, "ARC should outperform LRU on Zipf workload")
	assert.Greater(t, lfuHR, 0.70, "LFU should achieve >70%% hit rate on Zipf workload")
	assert.Greater(t, arcHR, 0.70, "ARC should achieve >70%% hit rate on Zipf workload")
}

func BenchmarkLRU(b *testing.B) {
	r := &Runner{}
	var totalOpsPerSec float64
	cfg := Config{
		Duration:    100 * time.Millisecond,
		Concurrency: 1,
		CacheSize:   1000,
		WorkloadN:   10000,
		Workload:    "zipf",
		Policies:    []string{"lru"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results := r.Run(cfg)
		totalOpsPerSec += results[0].OpsPerSec
	}
	b.ReportMetric(totalOpsPerSec/float64(b.N), "cache_ops/sec")
}

func BenchmarkLFU(b *testing.B) {
	r := &Runner{}
	var totalOpsPerSec float64
	cfg := Config{
		Duration:    100 * time.Millisecond,
		Concurrency: 1,
		CacheSize:   1000,
		WorkloadN:   10000,
		Workload:    "zipf",
		Policies:    []string{"lfu"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results := r.Run(cfg)
		totalOpsPerSec += results[0].OpsPerSec
	}
	b.ReportMetric(totalOpsPerSec/float64(b.N), "cache_ops/sec")
}

func BenchmarkARC(b *testing.B) {
	r := &Runner{}
	var totalOpsPerSec float64
	cfg := Config{
		Duration:    100 * time.Millisecond,
		Concurrency: 1,
		CacheSize:   1000,
		WorkloadN:   10000,
		Workload:    "zipf",
		Policies:    []string{"arc"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results := r.Run(cfg)
		totalOpsPerSec += results[0].OpsPerSec
	}
	b.ReportMetric(totalOpsPerSec/float64(b.N), "cache_ops/sec")
}
