package benchmark

import "sort"

// BenchmarkResult holds results for a single policy benchmark run.
type BenchmarkResult struct {
	Policy       string  `json:"policy"`
	Workload     string  `json:"workload"`
	HitRate      float64 `json:"hitRate"`
	OpsPerSec    float64 `json:"opsPerSec"`
	AvgLatencyNs int64   `json:"avgLatencyNs"`
	P50LatencyNs int64   `json:"p50LatencyNs"`
	P95LatencyNs int64   `json:"p95LatencyNs"`
	P99LatencyNs int64   `json:"p99LatencyNs"`
	Evictions    int64   `json:"evictions"`
	TotalOps     int64   `json:"totalOps"`
}

// percentile computes the Nth percentile from a sorted slice of nanoseconds.
func percentile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

// computeLatencyStats sorts the samples and computes approximate P50/P95/P99
// from a bounded reservoir, while avg stays exact from the running sum.
func computeLatencyStats(samples []int64, totalLatencyNs, totalOps int64) (avg, p50, p95, p99 int64) {
	if totalOps == 0 {
		return
	}
	avg = totalLatencyNs / totalOps
	if len(samples) == 0 {
		return
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p50 = percentile(samples, 0.50)
	p95 = percentile(samples, 0.95)
	p99 = percentile(samples, 0.99)
	return
}
