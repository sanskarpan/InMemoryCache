package api

import (
	"encoding/json"
	"sync"
	"time"
)

// APIError is the standard error response.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

// GetResponse is returned by GET /cache/{store}/{key}.
type GetResponse struct {
	Key   string `json:"key"`
	Value string `json:"value,omitempty"` // base64
	Hit   bool   `json:"hit"`
	TTLMs int64  `json:"ttlMs,omitempty"`
	Freq  int    `json:"freq,omitempty"`
}

// PutRequest is the body for PUT /cache/{store}/{key}.
type PutRequest struct {
	Value string `json:"value"` // base64
	TTLMs int64  `json:"ttlMs,omitempty"`
}

// PutResponse is returned by PUT.
type PutResponse struct {
	Key         string `json:"key"`
	WritePolicy string `json:"writePolicy"`
}

// DeleteResponse is returned by DELETE.
type DeleteResponse struct {
	Key   string `json:"key"`
	Found bool   `json:"found"`
}

// StatsResponse is returned by GET /cache/{store}/stats.
type StatsResponse struct {
	Policy         string  `json:"policy"`
	WritePolicy    string  `json:"writePolicy"`
	Capacity       int     `json:"capacity"`
	Size           int     `json:"size"`
	HitRate        float64 `json:"hitRate"`
	Hits           int64   `json:"hits"`
	Misses         int64   `json:"misses"`
	Evictions      int64   `json:"evictions"`
	TTLExpiries    int64   `json:"ttlExpiries"`
	BytesStored    int64   `json:"bytesStored"`
	WriteStoreOps  int64   `json:"writeStoreOps"`
	ReadStoreOps   int64   `json:"readStoreOps"`
	DirtyCount     int     `json:"dirtyCount,omitempty"`
	StoreLatencyMs int64   `json:"storeLatencyMs,omitempty"`
}

// CoherenceSetRequest is the body for POST /coherence/set.
type CoherenceSetRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"` // base64
	Node  string `json:"node"`
}

// BenchmarkRunRequest is the body for POST /benchmark/run.
type BenchmarkRunRequest struct {
	Workload    string   `json:"workload"`
	Policies    []string `json:"policies"`
	CacheSize   int      `json:"cacheSize"`
	DurationSec int      `json:"durationSec"`
	Concurrency int      `json:"concurrency"`
	WorkloadN   int      `json:"workloadN,omitempty"`
}

// BenchmarkJob tracks an async benchmark run.
// mu guards Status and Results which are written by the benchmark goroutine
// and read concurrently by polling HTTP handlers.
type BenchmarkJob struct {
	mu        sync.RWMutex
	ID        string      `json:"id"`
	Status    string      `json:"status"` // running, done, error
	Error     string      `json:"error,omitempty"`
	Results   interface{} `json:"results,omitempty"`
	StartedAt time.Time   `json:"startedAt"`
}

// MarshalJSON acquires a read lock so concurrent writers don't race with JSON encoding.
func (j *BenchmarkJob) MarshalJSON() ([]byte, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()
	type alias struct {
		ID        string      `json:"id"`
		Status    string      `json:"status"`
		Error     string      `json:"error,omitempty"`
		Results   interface{} `json:"results,omitempty"`
		StartedAt time.Time   `json:"startedAt"`
	}
	return json.Marshal(alias{
		ID:        j.ID,
		Status:    j.Status,
		Error:     j.Error,
		Results:   j.Results,
		StartedAt: j.StartedAt,
	})
}

// ConfigRequest is the body for POST /cache/{store}/config.
type ConfigRequest struct {
	StoreLatencyMs int    `json:"storeLatencyMs"`
	WritePolicy    string `json:"writePolicy,omitempty"`
	FlushNow       bool   `json:"flushNow,omitempty"`
}
