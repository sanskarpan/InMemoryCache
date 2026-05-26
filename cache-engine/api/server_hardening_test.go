package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourname/cache-engine/internal/benchmark"
	"github.com/yourname/cache-engine/internal/cache"
	"github.com/yourname/cache-engine/internal/cache/lru"
	"github.com/yourname/cache-engine/internal/coherence"
)

func TestHealthEndpointBypassesAPIKey(t *testing.T) {
	handler := newTestServer(t, NewServerConfig(nil, "secret"))
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestReadyEndpointReportsReady(t *testing.T) {
	cfg := NewServerConfig(nil, "")
	cfg.Environment = "development"
	cfg.AllowInsecureNoAuth = true
	handler := newTestServer(t, cfg)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestRateLimitMiddlewareReturnsTooManyRequests(t *testing.T) {
	cfg := NewServerConfig(nil, "")
	cfg.Environment = "development"
	cfg.AllowInsecureNoAuth = true
	cfg.RateLimitRequests = 1
	cfg.RateLimitWindow = time.Minute
	handler := newTestServer(t, cfg)

	req1 := httptest.NewRequest(http.MethodGet, "/api/cache/lru/stats", nil)
	req1.RemoteAddr = "127.0.0.1:1234"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)

	req2 := httptest.NewRequest(http.MethodGet, "/api/cache/lru/stats", nil)
	req2.RemoteAddr = "127.0.0.1:1234"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusTooManyRequests, rec2.Code)
	assert.NotEmpty(t, rec2.Header().Get("Retry-After"))
}

func TestBenchmarkRunRejectsInvalidPolicy(t *testing.T) {
	cfg := NewServerConfig(nil, "")
	cfg.Environment = "development"
	cfg.AllowInsecureNoAuth = true
	handler := newTestServer(t, cfg)
	body := bytes.NewBufferString(`{"policies":["bogus"],"durationSec":1,"concurrency":1,"cacheSize":8}`)
	req := httptest.NewRequest(http.MethodPost, "/api/benchmark/run", body)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestMetricsEndpointRequiresAuthWhenConfigured(t *testing.T) {
	handler := newTestServer(t, NewServerConfig(nil, "secret"))
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestMetricsEndpointEmitsPrometheusText(t *testing.T) {
	handler := newTestServer(t, NewServerConfig(nil, "secret"))
	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthRec := httptest.NewRecorder()
	handler.ServeHTTP(healthRec, healthReq)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("X-API-Key", "secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/plain")
	assert.Contains(t, rec.Body.String(), "cache_engine_ready")
	assert.Contains(t, rec.Body.String(), "cache_engine_http_requests_total")
}

func TestHealthEndpointReturnsTraceHeaders(t *testing.T) {
	handler := newTestServer(t, NewServerConfig(nil, "secret"))
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("Traceparent"))
	assert.Len(t, strings.TrimSpace(rec.Header().Get("X-Trace-Id")), 32)
}

func TestBenchmarkJobsPersistAndRestore(t *testing.T) {
	cfg := NewServerConfig(nil, "")
	cfg.BenchmarkResultsPath = filepath.Join(t.TempDir(), "benchmarks", "jobs.json")

	coord := coherence.NewCoordinator(map[string]cache.Cache{"node-a": lru.New(4)}, coherence.NewBus())
	t.Cleanup(coord.Close)
	handler := NewHandler(map[string]*StoreEntry{}, coord, cfg)
	job := &BenchmarkJob{
		ID:        "bench-1",
		Status:    "done",
		StartedAt: time.Now().UTC(),
		Results: []benchmark.BenchmarkResult{
			{Policy: "lru", Workload: "zipf", TotalOps: 42},
		},
	}
	handler.benchJobs.Store(job.ID, job)
	handler.persistBenchmarkJobs()

	restored := NewHandler(map[string]*StoreEntry{}, coord, cfg)
	jobs := restored.benchmarkJobsSnapshot()
	require.Len(t, jobs, 1)
	require.Equal(t, "bench-1", jobs[0].ID)

	resultsJSON, err := json.Marshal(jobs[0].Results)
	require.NoError(t, err)
	assert.Contains(t, string(resultsJSON), `"policy":"lru"`)
}
