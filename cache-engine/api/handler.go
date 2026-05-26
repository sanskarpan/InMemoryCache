package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/cache-engine/internal/benchmark"
	"github.com/yourname/cache-engine/internal/cache"
	"github.com/yourname/cache-engine/internal/coherence"
)

// Handler holds all HTTP handlers.
type Handler struct {
	stores      map[string]*StoreEntry
	mu          sync.RWMutex
	coordinator *coherence.Coordinator
	cfg         ServerConfig
	benchJobs   sync.Map // id → *BenchmarkJob
	benchMu     sync.Mutex
	benchActive bool
	benchStore  *benchmarkJobStore
}

// NewHandler creates a new Handler.
func NewHandler(stores map[string]*StoreEntry, coordinator *coherence.Coordinator, cfg ServerConfig) *Handler {
	h := &Handler{
		stores:      stores,
		coordinator: coordinator,
		cfg:         cfg,
		benchStore:  newBenchmarkJobStore(cfg.BenchmarkResultsPath),
	}
	h.loadBenchmarkJobs()
	return h
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("json_encode_response_failed", slog.Any("error", err))
	}
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, APIError{Code: code, Message: msg})
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errors.New("request body must contain a single JSON object")
		}
		return err
	}
	return nil
}

func isSupportedWorkload(name string) bool {
	switch name {
	case "", "sequential", "uniform", "zipf", "temporal", "scan-resistant", "write-heavy":
		return true
	default:
		return false
	}
}

func (h *Handler) getStore(name string) (*StoreEntry, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.stores[name]
	return s, ok
}

func (h *Handler) loadBenchmarkJobs() {
	if h.benchStore == nil {
		return
	}
	jobs, err := h.benchStore.load()
	if err != nil {
		slog.Error("benchmark_job_restore_failed", slog.Any("error", err))
		return
	}
	for _, job := range jobs {
		h.benchJobs.Store(job.ID, job)
	}
}

func (h *Handler) benchmarkJobsSnapshot() []*BenchmarkJob {
	jobs := make([]*BenchmarkJob, 0, 16)
	h.benchJobs.Range(func(_, v any) bool {
		jobs = append(jobs, v.(*BenchmarkJob))
		return true
	})
	sort.Slice(jobs, func(i, j int) bool {
		jobs[i].mu.RLock()
		left := jobs[i].StartedAt
		jobs[i].mu.RUnlock()
		jobs[j].mu.RLock()
		right := jobs[j].StartedAt
		jobs[j].mu.RUnlock()
		return left.After(right)
	})
	return jobs
}

func (h *Handler) benchmarkJobByID(jobID string) (*BenchmarkJob, bool) {
	if jobID == "" {
		return nil, false
	}
	v, ok := h.benchJobs.Load(jobID)
	if !ok {
		return nil, false
	}
	return v.(*BenchmarkJob), true
}

func (h *Handler) persistBenchmarkJobs() {
	if h.benchStore == nil {
		return
	}
	if err := h.benchStore.save(h.benchmarkJobsSnapshot()); err != nil {
		slog.Error("benchmark_job_persist_failed", slog.Any("error", err))
	}
}

func (h *Handler) Shutdown(_ context.Context) error {
	h.persistBenchmarkJobs()

	h.mu.RLock()
	entries := make([]*StoreEntry, 0, len(h.stores))
	for _, entry := range h.stores {
		if entry != nil {
			entries = append(entries, entry)
		}
	}
	h.mu.RUnlock()

	for _, entry := range entries {
		entry.mu.Lock()
		if entry.Cache != nil {
			entry.Cache.Close()
			entry.Cache = nil
		}
		entry.mu.Unlock()
	}
	return nil
}

func cloneEntriesIntoCache(dst cache.Cache, entries []cache.Entry) error {
	for _, entry := range entries {
		value := append([]byte(nil), entry.Value...)
		ttl := time.Duration(0)
		if entry.TTLMs > 0 {
			ttl = time.Duration(entry.TTLMs) * time.Millisecond
		}
		if err := dst.Set(entry.Key, value, ttl); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) reconfigureWritePolicy(storeName string, entry *StoreEntry, writePolicy string) (*StoreEntry, error) {
	entry.mu.Lock()
	defer entry.mu.Unlock()

	if !entry.AllowWritePolicy {
		return nil, fmt.Errorf("store %s does not support write-policy changes", storeName)
	}
	if writePolicy == entry.WritePolicy {
		return entry, nil
	}

	if flusher, ok := entry.Cache.(interface{ Flush() }); ok {
		flusher.Flush()
	}
	snapshot := entry.Cache.Snapshot()
	base, err := newBaseCache(entry.Policy, entry.Cache.Capacity())
	if err != nil {
		return nil, err
	}
	if err := cloneEntriesIntoCache(base, snapshot.Entries); err != nil {
		base.Close()
		return nil, err
	}
	wrapped, err := wrapCache(base, writePolicy, entry.BackingStore)
	if err != nil {
		base.Close()
		return nil, err
	}

	oldCache := entry.Cache
	entry.Cache = wrapped
	entry.WritePolicy = writePolicy
	oldCache.Close()
	return entry, nil
}

func (h *Handler) readyChecks() error {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.stores) == 0 {
		return fmt.Errorf("no stores configured")
	}
	for name, entry := range h.stores {
		if entry == nil {
			return fmt.Errorf("store %s is not initialized", name)
		}
		entry.mu.RLock()
		cacheReady := entry.Cache != nil
		entry.mu.RUnlock()
		if !cacheReady {
			return fmt.Errorf("store %s is not initialized", name)
		}
	}
	if h.coordinator == nil {
		return fmt.Errorf("coordinator is not initialized")
	}
	if h.benchStore != nil {
		if err := h.benchStore.ready(); err != nil {
			return fmt.Errorf("benchmark result store unavailable: %w", err)
		}
	}
	return nil
}

// GET /healthz
func (h *Handler) HandleHealth(w http.ResponseWriter, _ *http.Request) {
	h.mu.RLock()
	storeCount := len(h.stores)
	coherenceEnabled := h.coordinator != nil
	h.mu.RUnlock()
	h.benchMu.Lock()
	benchActive := h.benchActive
	h.benchMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"status":            "ok",
		"stores":            storeCount,
		"coherenceEnabled":  coherenceEnabled,
		"benchmarkActive":   benchActive,
		"benchmarkBackedUp": h.benchStore != nil,
		"time":              time.Now().UTC(),
	})
}

// GET /readyz
func (h *Handler) HandleReady(w http.ResponseWriter, _ *http.Request) {
	if err := h.readyChecks(); err != nil {
		writeError(w, http.StatusServiceUnavailable, "NOT_READY", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// GET /api/cache/{store}/{key}
func (h *Handler) HandleGet(w http.ResponseWriter, r *http.Request) {
	storeName := chi.URLParam(r, "store")
	key := chi.URLParam(r, "key")

	if len(key) > 512 {
		writeError(w, http.StatusBadRequest, "KEY_TOO_LONG", "key must not exceed 512 bytes")
		return
	}

	entry, ok := h.getStore(storeName)
	if !ok {
		writeError(w, http.StatusNotFound, "STORE_NOT_FOUND", "unknown store: "+storeName)
		return
	}

	entry.mu.RLock()
	defer entry.mu.RUnlock()

	value, hit := entry.Cache.Get(key)
	resp := GetResponse{Key: key, Hit: hit}
	if hit {
		resp.Value = base64.StdEncoding.EncodeToString(value)
	}
	writeJSON(w, http.StatusOK, resp)
}

// PUT /api/cache/{store}/{key}
func (h *Handler) HandleSet(w http.ResponseWriter, r *http.Request) {
	storeName := chi.URLParam(r, "store")
	key := chi.URLParam(r, "key")

	if len(key) > 512 {
		writeError(w, http.StatusBadRequest, "KEY_TOO_LONG", "key must not exceed 512 bytes")
		return
	}

	entry, ok := h.getStore(storeName)
	if !ok {
		writeError(w, http.StatusNotFound, "STORE_NOT_FOUND", "unknown store: "+storeName)
		return
	}

	var req PutRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if req.TTLMs < 0 {
		writeError(w, http.StatusBadRequest, "INVALID_TTL", "ttlMs must be greater than or equal to 0")
		return
	}

	value, err := base64.StdEncoding.DecodeString(req.Value)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_VALUE", "value must be base64 encoded")
		return
	}

	if len(value) > 1<<20 {
		writeError(w, http.StatusRequestEntityTooLarge, "VALUE_TOO_LARGE", "value exceeds 1MB")
		return
	}

	entry.mu.RLock()
	defer entry.mu.RUnlock()

	ttl := time.Duration(req.TTLMs) * time.Millisecond
	if err := entry.Cache.Set(key, value, ttl); err != nil {
		writeError(w, http.StatusBadGateway, "STORE_WRITE_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, PutResponse{Key: key, WritePolicy: entry.WritePolicy})
}

// DELETE /api/cache/{store}/{key}
func (h *Handler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	storeName := chi.URLParam(r, "store")
	key := chi.URLParam(r, "key")

	if len(key) > 512 {
		writeError(w, http.StatusBadRequest, "KEY_TOO_LONG", "key must not exceed 512 bytes")
		return
	}

	entry, ok := h.getStore(storeName)
	if !ok {
		writeError(w, http.StatusNotFound, "STORE_NOT_FOUND", "unknown store: "+storeName)
		return
	}

	entry.mu.RLock()
	defer entry.mu.RUnlock()

	found := entry.Cache.Delete(key)
	writeJSON(w, http.StatusOK, DeleteResponse{Key: key, Found: found})
}

// GET /api/cache/{store}/stats
func (h *Handler) HandleStats(w http.ResponseWriter, r *http.Request) {
	storeName := chi.URLParam(r, "store")
	entry, ok := h.getStore(storeName)
	if !ok {
		writeError(w, http.StatusNotFound, "STORE_NOT_FOUND", "unknown store: "+storeName)
		return
	}

	entry.mu.RLock()
	defer entry.mu.RUnlock()

	st := entry.Cache.Stats()
	resp := StatsResponse{
		Policy:        entry.Policy,
		WritePolicy:   entry.WritePolicy,
		Capacity:      entry.Cache.Capacity(),
		Size:          entry.Cache.Len(),
		HitRate:       st.HitRate(),
		Hits:          st.Hits.Load(),
		Misses:        st.Misses.Load(),
		Evictions:     st.Evictions.Load(),
		TTLExpiries:   st.TTLExpiries.Load(),
		BytesStored:   st.BytesStored.Load(),
		WriteStoreOps: st.WriteStoreOps.Load(),
		ReadStoreOps:  st.ReadStoreOps.Load(),
	}
	if dc, ok := entry.Cache.(interface{ DirtyCount() int }); ok {
		resp.DirtyCount = dc.DirtyCount()
	}
	if entry.BackingStore != nil {
		ms := entry.BackingStore.GetLatency().Milliseconds()
		resp.StoreLatencyMs = ms
	}
	writeJSON(w, http.StatusOK, resp)
}

// POST /api/cache/{store}/config
func (h *Handler) HandleConfig(w http.ResponseWriter, r *http.Request) {
	storeName := chi.URLParam(r, "store")
	entry, ok := h.getStore(storeName)
	if !ok {
		writeError(w, http.StatusNotFound, "STORE_NOT_FOUND", "unknown store: "+storeName)
		return
	}

	var req ConfigRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if req.StoreLatencyMs < -1 {
		writeError(w, http.StatusBadRequest, "INVALID_STORE_LATENCY", "storeLatencyMs must be -1 or greater")
		return
	}

	entry.mu.RLock()
	backing := entry.BackingStore
	entry.mu.RUnlock()
	if req.StoreLatencyMs >= 0 && backing != nil {
		backing.SetLatency(time.Duration(req.StoreLatencyMs) * time.Millisecond)
	}
	if req.WritePolicy != "" {
		h.mu.Lock()
		current, exists := h.stores[storeName]
		if !exists {
			h.mu.Unlock()
			writeError(w, http.StatusNotFound, "STORE_NOT_FOUND", "unknown store: "+storeName)
			return
		}
		updated, err := h.reconfigureWritePolicy(storeName, current, req.WritePolicy)
		h.mu.Unlock()
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_WRITE_POLICY", err.Error())
			return
		}
		entry = updated
	}
	if req.FlushNow {
		entry.mu.RLock()
		if flusher, ok := entry.Cache.(interface{ Flush() }); ok {
			flusher.Flush()
		}
		entry.mu.RUnlock()
	}

	entry.mu.RLock()
	resp := map[string]any{"status": "ok", "writePolicy": entry.WritePolicy}
	if entry.BackingStore != nil {
		resp["storeLatencyMs"] = entry.BackingStore.GetLatency().Milliseconds()
	}
	entry.mu.RUnlock()
	writeJSON(w, http.StatusOK, resp)
}

// GET /api/cache/{store}/snapshot
func (h *Handler) HandleSnapshot(w http.ResponseWriter, r *http.Request) {
	storeName := chi.URLParam(r, "store")
	entry, ok := h.getStore(storeName)
	if !ok {
		writeError(w, http.StatusNotFound, "STORE_NOT_FOUND", "unknown store: "+storeName)
		return
	}
	entry.mu.RLock()
	defer entry.mu.RUnlock()

	snap := entry.Cache.Snapshot()
	writeJSON(w, http.StatusOK, snap)
}

// DELETE /api/cache/{store}/purge
func (h *Handler) HandlePurge(w http.ResponseWriter, r *http.Request) {
	storeName := chi.URLParam(r, "store")
	entry, ok := h.getStore(storeName)
	if !ok {
		writeError(w, http.StatusNotFound, "STORE_NOT_FOUND", "unknown store: "+storeName)
		return
	}
	entry.mu.RLock()
	defer entry.mu.RUnlock()

	entry.Cache.Purge()
	writeJSON(w, http.StatusOK, map[string]string{"status": "purged"})
}

// POST /api/coherence/set
func (h *Handler) HandleCoherenceSet(w http.ResponseWriter, r *http.Request) {
	var req CoherenceSetRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if req.Key == "" || req.Node == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "node and key are required")
		return
	}
	value, err := base64.StdEncoding.DecodeString(req.Value)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_VALUE", "value must be base64")
		return
	}
	if err := h.coordinator.Set(req.Node, req.Key, value, 0); err != nil {
		writeError(w, http.StatusInternalServerError, "SET_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GET /api/coherence/nodes
func (h *Handler) HandleCoherenceNodes(w http.ResponseWriter, r *http.Request) {
	snap := h.coordinator.Snapshot()
	writeJSON(w, http.StatusOK, snap)
}

// POST /api/benchmark/run
func (h *Handler) HandleBenchmarkRun(w http.ResponseWriter, r *http.Request) {
	h.benchMu.Lock()
	if h.benchActive {
		h.benchMu.Unlock()
		writeError(w, http.StatusConflict, "BENCHMARK_RUNNING", "a benchmark is already running")
		return
	}
	h.benchActive = true
	h.benchMu.Unlock()

	var req BenchmarkRunRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		h.benchMu.Lock()
		h.benchActive = false
		h.benchMu.Unlock()
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if !isSupportedWorkload(req.Workload) {
		h.benchMu.Lock()
		h.benchActive = false
		h.benchMu.Unlock()
		writeError(w, http.StatusBadRequest, "INVALID_WORKLOAD", "unsupported workload")
		return
	}
	if req.DurationSec < 0 {
		h.benchMu.Lock()
		h.benchActive = false
		h.benchMu.Unlock()
		writeError(w, http.StatusBadRequest, "INVALID_DURATION", "durationSec must be greater than or equal to 0")
		return
	}
	if req.CacheSize < 0 {
		h.benchMu.Lock()
		h.benchActive = false
		h.benchMu.Unlock()
		writeError(w, http.StatusBadRequest, "INVALID_CACHE_SIZE", "cacheSize must be greater than or equal to 0")
		return
	}
	if req.WorkloadN < 0 {
		h.benchMu.Lock()
		h.benchActive = false
		h.benchMu.Unlock()
		writeError(w, http.StatusBadRequest, "INVALID_WORKLOAD_SIZE", "workloadN must be greater than or equal to 0")
		return
	}

	if req.DurationSec > 60 {
		req.DurationSec = 60
	}
	if req.Concurrency < 1 {
		req.Concurrency = 1
	}
	if req.Concurrency > 32 {
		req.Concurrency = 32
	}
	if req.CacheSize > 50000 {
		req.CacheSize = 50000
	}
	for _, policy := range req.Policies {
		if _, err := newBaseCache(policy, 1); err != nil {
			h.benchMu.Lock()
			h.benchActive = false
			h.benchMu.Unlock()
			writeError(w, http.StatusBadRequest, "INVALID_POLICY", err.Error())
			return
		}
	}

	jobID := fmt.Sprintf("bench-%d", time.Now().UnixMilli())
	job := &BenchmarkJob{ID: jobID, Status: "running", StartedAt: time.Now()}
	h.benchJobs.Store(jobID, job)

	// Prune old jobs — keep at most 50
	count := 0
	h.benchJobs.Range(func(_, _ any) bool { count++; return true })
	if count > 50 {
		var oldest string
		var oldestTime time.Time
		h.benchJobs.Range(func(k, v any) bool {
			j := v.(*BenchmarkJob)
			j.mu.RLock()
			st := j.StartedAt
			j.mu.RUnlock()
			if oldest == "" || st.Before(oldestTime) {
				oldest = k.(string)
				oldestTime = st
			}
			return true
		})
		if oldest != "" && oldest != jobID {
			h.benchJobs.Delete(oldest)
		}
	}
	h.persistBenchmarkJobs()

	go func() {
		defer func() {
			h.benchMu.Lock()
			h.benchActive = false
			h.benchMu.Unlock()
		}()
		defer func() {
			if rec := recover(); rec != nil {
				if v, ok := h.benchJobs.Load(jobID); ok {
					j := v.(*BenchmarkJob)
					j.mu.Lock()
					j.Status = "error"
					j.Error = fmt.Sprintf("panic: %v", rec)
					j.mu.Unlock()
				}
				h.persistBenchmarkJobs()
			}
		}()

		if len(req.Policies) == 0 {
			req.Policies = []string{"lru", "lfu", "arc"}
		}
		if req.DurationSec == 0 {
			req.DurationSec = 5
		}
		if req.CacheSize == 0 {
			req.CacheSize = 500
		}

		r := &benchmark.Runner{}
		results := r.Run(benchmark.Config{
			Duration:    time.Duration(req.DurationSec) * time.Second,
			Concurrency: req.Concurrency,
			CacheSize:   req.CacheSize,
			Policies:    req.Policies,
			Workload:    req.Workload,
			WorkloadN:   req.WorkloadN,
		})

		if v, ok := h.benchJobs.Load(jobID); ok {
			j := v.(*BenchmarkJob)
			j.mu.Lock()
			j.Status = "done"
			j.Results = results
			j.mu.Unlock()
		}
		h.persistBenchmarkJobs()
	}()

	writeJSON(w, http.StatusAccepted, job)
}

// GET /api/benchmark/results
func (h *Handler) HandleBenchmarkResults(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.benchmarkJobsSnapshot())
}

// GET /api/benchmark/results/{jobID}
func (h *Handler) HandleBenchmarkResult(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	job, ok := h.benchmarkJobByID(jobID)
	if !ok {
		writeError(w, http.StatusNotFound, "BENCHMARK_NOT_FOUND", "unknown benchmark job")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// GET /api/cache/{store}/peek/{key}
func (h *Handler) HandlePeek(w http.ResponseWriter, r *http.Request) {
	storeName := chi.URLParam(r, "store")
	key := chi.URLParam(r, "key")

	if len(key) > 512 {
		writeError(w, http.StatusBadRequest, "KEY_TOO_LONG", "key must not exceed 512 bytes")
		return
	}

	entry, ok := h.getStore(storeName)
	if !ok {
		writeError(w, http.StatusNotFound, "STORE_NOT_FOUND", "unknown store: "+storeName)
		return
	}
	entry.mu.RLock()
	defer entry.mu.RUnlock()

	value, hit := entry.Cache.Peek(key)
	resp := GetResponse{Key: key, Hit: hit}
	if hit {
		resp.Value = base64.StdEncoding.EncodeToString(value)
	}
	writeJSON(w, http.StatusOK, resp)
}
