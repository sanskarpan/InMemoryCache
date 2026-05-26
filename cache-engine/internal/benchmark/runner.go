package benchmark

import (
	"math/rand"
	"sync"
	"time"

	"github.com/yourname/cache-engine/internal/cache"
	"github.com/yourname/cache-engine/internal/cache/arc"
	"github.com/yourname/cache-engine/internal/cache/lfu"
	"github.com/yourname/cache-engine/internal/cache/lru"
)

// Config holds benchmark configuration.
type Config struct {
	Duration    time.Duration
	Concurrency int
	CacheSize   int
	Policies    []string
	Workload    string
	WorkloadN   int // key space size
}

// Runner runs benchmarks.
type Runner struct{}

const maxLatencySamplesPerWorker = 4096
const deadlineCheckInterval = 256

// Run executes the benchmark and returns results for each policy.
func (r *Runner) Run(cfg Config) []BenchmarkResult {
	if cfg.Duration == 0 {
		cfg.Duration = 10 * time.Second
	}
	if cfg.Concurrency == 0 {
		cfg.Concurrency = 4
	}
	if cfg.CacheSize == 0 {
		cfg.CacheSize = 500
	}
	if cfg.WorkloadN == 0 {
		cfg.WorkloadN = 10000
	}

	results := make([]BenchmarkResult, 0, len(cfg.Policies))
	for _, policy := range cfg.Policies {
		result := runPolicy(policy, cfg)
		results = append(results, result)
	}
	return results
}

func newCache(policy string, capacity int) cache.Cache {
	switch policy {
	case "lfu":
		return lfu.New(capacity)
	case "arc":
		return arc.New(capacity)
	default:
		return lru.New(capacity)
	}
}

func newWorkload(name string, n int) Workload {
	switch name {
	case "sequential":
		return NewSequential(n)
	case "uniform":
		return NewUniformRandom(n)
	case "zipf":
		return NewZipf(n)
	case "temporal":
		return NewTemporal(n)
	case "scan-resistant":
		return NewScanResistant(200, n)
	case "write-heavy":
		return NewWriteHeavy(n)
	default:
		return NewZipf(n)
	}
}

func runPolicy(policy string, cfg Config) BenchmarkResult {
	c := newCache(policy, cfg.CacheSize)
	defer c.Close()

	// Pre-warm the cache with a consistent access pattern
	warmup := newWorkload(cfg.Workload, cfg.WorkloadN)
	for i := 0; i < cfg.CacheSize; i++ {
		_ = c.Set(warmup.Next().Key, []byte("v"), 0)
	}

	var mu sync.Mutex
	var allSamples []int64
	var totalOps int64
	var totalLatencyNs int64

	deadline := time.Now().Add(cfg.Duration)
	var wg sync.WaitGroup

	for g := 0; g < cfg.Concurrency; g++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			w := newWorkload(cfg.Workload, cfg.WorkloadN)
			localSamples := make([]int64, 0, maxLatencySamplesPerWorker)
			localSeen := int64(0)
			localOps := int64(0)
			localLatencyNs := int64(0)
			// #nosec G404 -- deterministic pseudo-randomness is intentional for reproducible reservoir sampling.
			rng := rand.New(rand.NewSource(int64(workerID + 1)))
			untilDeadlineCheck := deadlineCheckInterval

			for {
				untilDeadlineCheck--
				if untilDeadlineCheck == 0 {
					if time.Now().After(deadline) {
						break
					}
					untilDeadlineCheck = deadlineCheckInterval
				}

				op := w.Next()
				start := time.Now()

				if op.Type == OpSet {
					_ = c.Set(op.Key, op.Value, 0)
				} else {
					if _, ok := c.Get(op.Key); !ok {
						_ = c.Set(op.Key, op.Value, 0)
					}
				}

				ns := time.Since(start).Nanoseconds()
				localLatencyNs += ns
				localSeen++
				localOps++
				if len(localSamples) < maxLatencySamplesPerWorker {
					localSamples = append(localSamples, ns)
				} else {
					idx := rng.Int63n(localSeen)
					if idx < int64(len(localSamples)) {
						localSamples[idx] = ns
					}
				}
			}

			mu.Lock()
			allSamples = append(allSamples, localSamples...)
			totalOps += localOps
			totalLatencyNs += localLatencyNs
			mu.Unlock()
		}(g)
	}

	wg.Wait()

	st := c.Stats()
	avg, p50, p95, p99 := computeLatencyStats(allSamples, totalLatencyNs, totalOps)

	return BenchmarkResult{
		Policy:       policy,
		Workload:     cfg.Workload,
		HitRate:      st.HitRate(),
		OpsPerSec:    float64(totalOps) / cfg.Duration.Seconds(),
		AvgLatencyNs: avg,
		P50LatencyNs: p50,
		P95LatencyNs: p95,
		P99LatencyNs: p99,
		Evictions:    st.Evictions.Load(),
		TotalOps:     totalOps,
	}
}
