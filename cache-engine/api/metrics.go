package api

import (
	"bytes"
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type httpMetricKey struct {
	Method string
	Route  string
	Status string
}

type httpMetricValue struct {
	count      atomic.Int64
	durationNs atomic.Int64
}

type Metrics struct {
	startedAt           time.Time
	httpRequests        sync.Map
	rateLimitRejections atomic.Int64
	authRejections      atomic.Int64
	panicRecoveries     atomic.Int64
}

func newMetrics() *Metrics {
	return &Metrics{startedAt: time.Now().UTC()}
}

func (m *Metrics) recordHTTPRequest(method, route string, status int, duration time.Duration) {
	if route == "" {
		route = "unmatched"
	}
	key := httpMetricKey{
		Method: method,
		Route:  route,
		Status: strconv.Itoa(status),
	}
	value, _ := m.httpRequests.LoadOrStore(key, &httpMetricValue{})
	metric := value.(*httpMetricValue)
	metric.count.Add(1)
	metric.durationNs.Add(duration.Nanoseconds())
}

func (m *Metrics) recordRateLimitRejection() {
	m.rateLimitRejections.Add(1)
}

func (m *Metrics) recordAuthRejection() {
	m.authRejections.Add(1)
}

func (m *Metrics) recordRecoveredPanic() {
	m.panicRecoveries.Add(1)
}

func metricsHandler(h *Handler, metrics *Metrics) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		var buf bytes.Buffer
		writeMetricLine := func(line string, args ...any) {
			_, _ = fmt.Fprintf(&buf, line, args...)
			_ = buf.WriteByte('\n')
		}

		ready := 1
		if err := h.readyChecks(); err != nil {
			ready = 0
		}

		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)

		h.mu.RLock()
		storeNames := make([]string, 0, len(h.stores))
		for name := range h.stores {
			storeNames = append(storeNames, name)
		}
		sort.Strings(storeNames)
		coherenceEnabled := h.coordinator != nil
		h.mu.RUnlock()

		h.benchMu.Lock()
		benchmarkActive := h.benchActive
		h.benchMu.Unlock()

		writeMetricLine("# HELP cache_engine_ready Whether the service is ready to receive traffic")
		writeMetricLine("# TYPE cache_engine_ready gauge")
		writeMetricLine("cache_engine_ready %d", ready)

		writeMetricLine("# HELP cache_engine_coherence_enabled Whether coherence coordination is configured")
		writeMetricLine("# TYPE cache_engine_coherence_enabled gauge")
		writeMetricLine("cache_engine_coherence_enabled %d", boolAsInt(coherenceEnabled))

		writeMetricLine("# HELP cache_engine_benchmark_active Whether a benchmark run is currently active")
		writeMetricLine("# TYPE cache_engine_benchmark_active gauge")
		writeMetricLine("cache_engine_benchmark_active %d", boolAsInt(benchmarkActive))

		writeMetricLine("# HELP cache_engine_process_uptime_seconds Process uptime in seconds")
		writeMetricLine("# TYPE cache_engine_process_uptime_seconds gauge")
		writeMetricLine("cache_engine_process_uptime_seconds %.3f", time.Since(metrics.startedAt).Seconds())

		writeMetricLine("# HELP cache_engine_process_memory_bytes Go heap allocation in bytes")
		writeMetricLine("# TYPE cache_engine_process_memory_bytes gauge")
		writeMetricLine("cache_engine_process_memory_bytes %d", mem.Alloc)

		writeMetricLine("# HELP cache_engine_rate_limit_rejections_total Total rejected requests due to rate limiting")
		writeMetricLine("# TYPE cache_engine_rate_limit_rejections_total counter")
		writeMetricLine("cache_engine_rate_limit_rejections_total %d", metrics.rateLimitRejections.Load())

		writeMetricLine("# HELP cache_engine_auth_rejections_total Total rejected requests due to authentication failure")
		writeMetricLine("# TYPE cache_engine_auth_rejections_total counter")
		writeMetricLine("cache_engine_auth_rejections_total %d", metrics.authRejections.Load())

		writeMetricLine("# HELP cache_engine_panic_recoveries_total Total panics recovered by HTTP middleware")
		writeMetricLine("# TYPE cache_engine_panic_recoveries_total counter")
		writeMetricLine("cache_engine_panic_recoveries_total %d", metrics.panicRecoveries.Load())

		writeMetricLine("# HELP cache_engine_http_requests_total Total HTTP requests by route and status")
		writeMetricLine("# TYPE cache_engine_http_requests_total counter")
		writeMetricLine("# HELP cache_engine_http_request_duration_seconds_sum Total request duration in seconds by route")
		writeMetricLine("# TYPE cache_engine_http_request_duration_seconds_sum counter")
		writeMetricLine("# HELP cache_engine_http_request_duration_seconds_count Request duration sample count by route")
		writeMetricLine("# TYPE cache_engine_http_request_duration_seconds_count counter")
		httpKeys := make([]httpMetricKey, 0, 32)
		metrics.httpRequests.Range(func(key, _ any) bool {
			httpKeys = append(httpKeys, key.(httpMetricKey))
			return true
		})
		sort.Slice(httpKeys, func(i, j int) bool {
			if httpKeys[i].Route == httpKeys[j].Route {
				if httpKeys[i].Method == httpKeys[j].Method {
					return httpKeys[i].Status < httpKeys[j].Status
				}
				return httpKeys[i].Method < httpKeys[j].Method
			}
			return httpKeys[i].Route < httpKeys[j].Route
		})
		for _, key := range httpKeys {
			value, _ := metrics.httpRequests.Load(key)
			sample := value.(*httpMetricValue)
			labels := fmt.Sprintf(`method="%s",route="%s",status="%s"`, metricLabelValue(key.Method), metricLabelValue(key.Route), metricLabelValue(key.Status))
			writeMetricLine("cache_engine_http_requests_total{%s} %d", labels, sample.count.Load())
			writeMetricLine("cache_engine_http_request_duration_seconds_sum{%s} %.6f", labels, float64(sample.durationNs.Load())/float64(time.Second))
			writeMetricLine("cache_engine_http_request_duration_seconds_count{%s} %d", labels, sample.count.Load())
		}

		writeMetricLine("# HELP cache_engine_store_entry_count Current cache entry count")
		writeMetricLine("# TYPE cache_engine_store_entry_count gauge")
		writeMetricLine("# HELP cache_engine_store_capacity Configured cache capacity")
		writeMetricLine("# TYPE cache_engine_store_capacity gauge")
		writeMetricLine("# HELP cache_engine_store_dirty_entries Current dirty entry count for write-back stores")
		writeMetricLine("# TYPE cache_engine_store_dirty_entries gauge")
		writeMetricLine("# HELP cache_engine_store_backing_store_latency_milliseconds Configured artificial backing store latency")
		writeMetricLine("# TYPE cache_engine_store_backing_store_latency_milliseconds gauge")
		writeMetricLine("# HELP cache_engine_store_hits_total Total cache hits")
		writeMetricLine("# TYPE cache_engine_store_hits_total counter")
		writeMetricLine("# HELP cache_engine_store_misses_total Total cache misses")
		writeMetricLine("# TYPE cache_engine_store_misses_total counter")
		writeMetricLine("# HELP cache_engine_store_sets_total Total cache sets")
		writeMetricLine("# TYPE cache_engine_store_sets_total counter")
		writeMetricLine("# HELP cache_engine_store_deletes_total Total cache deletes")
		writeMetricLine("# TYPE cache_engine_store_deletes_total counter")
		writeMetricLine("# HELP cache_engine_store_evictions_total Total cache evictions")
		writeMetricLine("# TYPE cache_engine_store_evictions_total counter")
		writeMetricLine("# HELP cache_engine_store_ttl_expiries_total Total cache TTL expiries")
		writeMetricLine("# TYPE cache_engine_store_ttl_expiries_total counter")
		writeMetricLine("# HELP cache_engine_store_bytes_stored Current bytes stored in cache")
		writeMetricLine("# TYPE cache_engine_store_bytes_stored gauge")
		writeMetricLine("# HELP cache_engine_store_write_store_ops_total Total writes propagated to the backing store")
		writeMetricLine("# TYPE cache_engine_store_write_store_ops_total counter")
		writeMetricLine("# HELP cache_engine_store_read_store_ops_total Total reads fetched from the backing store")
		writeMetricLine("# TYPE cache_engine_store_read_store_ops_total counter")

		for _, name := range storeNames {
			entry, ok := h.getStore(name)
			if !ok {
				continue
			}
			entry.mu.RLock()
			stats := entry.Cache.Stats().Snapshot()
			size := entry.Cache.Len()
			capacity := entry.Cache.Capacity()
			writePolicy := entry.WritePolicy
			dirtyCount := 0
			if dirty, ok := entry.Cache.(interface{ DirtyCount() int }); ok {
				dirtyCount = dirty.DirtyCount()
			}
			backingLatencyMs := float64(0)
			if entry.BackingStore != nil {
				backingLatencyMs = float64(entry.BackingStore.GetLatency().Milliseconds())
			}
			entry.mu.RUnlock()

			labels := fmt.Sprintf(`store="%s",policy="%s",write_policy="%s"`, metricLabelValue(name), metricLabelValue(entry.Policy), metricLabelValue(writePolicy))
			writeMetricLine("cache_engine_store_entry_count{%s} %d", labels, size)
			writeMetricLine("cache_engine_store_capacity{%s} %d", labels, capacity)
			writeMetricLine("cache_engine_store_dirty_entries{%s} %d", labels, dirtyCount)
			writeMetricLine("cache_engine_store_backing_store_latency_milliseconds{%s} %.0f", labels, backingLatencyMs)
			writeMetricLine("cache_engine_store_hits_total{%s} %d", labels, stats.Hits)
			writeMetricLine("cache_engine_store_misses_total{%s} %d", labels, stats.Misses)
			writeMetricLine("cache_engine_store_sets_total{%s} %d", labels, stats.Sets)
			writeMetricLine("cache_engine_store_deletes_total{%s} %d", labels, stats.Deletes)
			writeMetricLine("cache_engine_store_evictions_total{%s} %d", labels, stats.Evictions)
			writeMetricLine("cache_engine_store_ttl_expiries_total{%s} %d", labels, stats.TTLExpiries)
			writeMetricLine("cache_engine_store_bytes_stored{%s} %d", labels, stats.BytesStored)
			writeMetricLine("cache_engine_store_write_store_ops_total{%s} %d", labels, stats.WriteStoreOps)
			writeMetricLine("cache_engine_store_read_store_ops_total{%s} %d", labels, stats.ReadStoreOps)
		}

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write(buf.Bytes())
	}
}

func boolAsInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func metricLabelValue(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`)
	return replacer.Replace(value)
}
