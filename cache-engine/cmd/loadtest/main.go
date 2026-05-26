package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yourname/cache-engine/api"
)

type loadConfig struct {
	baseURL              string
	apiKey               string
	store                string
	mode                 string
	duration             time.Duration
	concurrency          int
	apiPause             time.Duration
	sseClients           int
	sseMinEvents         int
	benchmarkRuns        int
	benchmarkDuration    time.Duration
	benchmarkConcurrency int
	benchmarkCacheSize   int
	benchmarkWorkload    string
	benchmarkWorkloadN   int
	benchmarkPolicies    []string
}

type phaseSummary struct {
	Phase    string        `json:"phase"`
	Duration time.Duration `json:"duration"`
	Ops      int64         `json:"ops,omitempty"`
	Errors   int64         `json:"errors,omitempty"`
	Events   int64         `json:"events,omitempty"`
	Runs     int           `json:"runs,omitempty"`
}

func main() {
	cfg := loadConfig{}
	flag.StringVar(&cfg.baseURL, "base-url", getenvDefault("CACHE_ENGINE_BASE_URL", "http://127.0.0.1:8080"), "Base URL for the Cache Engine API")
	flag.StringVar(&cfg.apiKey, "api-key", os.Getenv("CACHE_ENGINE_API_KEY"), "API key for authenticated endpoints")
	flag.StringVar(&cfg.store, "store", getenvDefault("CACHE_ENGINE_LOADTEST_STORE", "lru"), "Cache store to exercise")
	flag.StringVar(&cfg.mode, "mode", getenvDefault("CACHE_ENGINE_LOADTEST_MODE", "all"), "Test mode: api, sse, benchmark, or all")
	flag.DurationVar(&cfg.duration, "duration", 45*time.Second, "Duration for API and SSE soak phases")
	flag.IntVar(&cfg.concurrency, "concurrency", 16, "Concurrent API workers")
	flag.DurationVar(&cfg.apiPause, "api-pause", 0, "Optional pause between API requests to reduce request rate")
	flag.IntVar(&cfg.sseClients, "sse-clients", 4, "Concurrent SSE streams to hold open")
	flag.IntVar(&cfg.sseMinEvents, "sse-min-events", 3, "Minimum SSE events each stream must receive")
	flag.IntVar(&cfg.benchmarkRuns, "benchmark-runs", 2, "Number of benchmark jobs to submit")
	flag.DurationVar(&cfg.benchmarkDuration, "benchmark-duration", 5*time.Second, "Duration for each submitted benchmark job")
	flag.IntVar(&cfg.benchmarkConcurrency, "benchmark-concurrency", 4, "Concurrency used inside each benchmark job")
	flag.IntVar(&cfg.benchmarkCacheSize, "benchmark-cache-size", 500, "Cache size used inside each benchmark job")
	flag.StringVar(&cfg.benchmarkWorkload, "benchmark-workload", "zipf", "Benchmark workload to submit")
	flag.IntVar(&cfg.benchmarkWorkloadN, "benchmark-workload-n", 10000, "Benchmark workload key space")
	policies := flag.String("benchmark-policies", "lru,lfu,arc", "Comma-separated benchmark policies")
	flag.Parse()

	cfg.baseURL = strings.TrimRight(strings.TrimSpace(cfg.baseURL), "/")
	if cfg.baseURL == "" {
		log.Fatal("base URL must not be empty")
	}
	if _, err := url.Parse(cfg.baseURL); err != nil {
		log.Fatalf("invalid base URL: %v", err)
	}
	if strings.TrimSpace(cfg.apiKey) == "" {
		log.Fatal("api key is required; set CACHE_ENGINE_API_KEY or pass --api-key")
	}
	if cfg.duration <= 0 {
		log.Fatal("duration must be greater than zero")
	}
	if cfg.concurrency <= 0 {
		log.Fatal("concurrency must be greater than zero")
	}
	if cfg.sseClients <= 0 {
		log.Fatal("sse-clients must be greater than zero")
	}
	if cfg.sseMinEvents <= 0 {
		log.Fatal("sse-min-events must be greater than zero")
	}
	if cfg.benchmarkRuns <= 0 {
		log.Fatal("benchmark-runs must be greater than zero")
	}
	if cfg.benchmarkDuration <= 0 {
		log.Fatal("benchmark-duration must be greater than zero")
	}
	if cfg.benchmarkConcurrency <= 0 {
		log.Fatal("benchmark-concurrency must be greater than zero")
	}
	if cfg.benchmarkCacheSize <= 0 {
		log.Fatal("benchmark-cache-size must be greater than zero")
	}
	if cfg.benchmarkWorkloadN <= 0 {
		log.Fatal("benchmark-workload-n must be greater than zero")
	}
	cfg.benchmarkPolicies = splitList(*policies)
	if len(cfg.benchmarkPolicies) == 0 {
		cfg.benchmarkPolicies = []string{"lru", "lfu", "arc"}
	}

	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        128,
			MaxIdleConnsPerHost: 64,
			MaxConnsPerHost:     64,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	modes := map[string]bool{
		"api":       false,
		"sse":       false,
		"benchmark": false,
		"all":       false,
	}
	if _, ok := modes[cfg.mode]; !ok {
		log.Fatalf("unsupported mode %q", cfg.mode)
	}
	if cfg.mode == "all" {
		modes["api"] = true
		modes["sse"] = true
		modes["benchmark"] = true
	} else {
		modes[cfg.mode] = true
	}

	summaries := make([]phaseSummary, 0, 3)
	if modes["api"] {
		summary, err := runAPIPhase(client, cfg)
		if err != nil {
			log.Fatalf("api phase failed: %v", err)
		}
		summaries = append(summaries, summary)
	}
	if modes["sse"] {
		summary, err := runSSEPhase(client, cfg)
		if err != nil {
			log.Fatalf("sse phase failed: %v", err)
		}
		summaries = append(summaries, summary)
	}
	if modes["benchmark"] {
		summary, err := runBenchmarkPhase(client, cfg)
		if err != nil {
			log.Fatalf("benchmark phase failed: %v", err)
		}
		summaries = append(summaries, summary)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(map[string]any{
		"baseURL":   cfg.baseURL,
		"store":     cfg.store,
		"summaries": summaries,
	}); err != nil {
		log.Fatalf("encode summary: %v", err)
	}
}

func runAPIPhase(client *http.Client, cfg loadConfig) (phaseSummary, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), cfg.duration)
	defer cancel()

	keys := make([]string, max(32, cfg.concurrency*4))
	for i := range keys {
		keys[i] = fmt.Sprintf("loadtest:%d", i)
	}

	for i := 0; i < min(len(keys), cfg.concurrency*2); i++ {
		body := api.PutRequest{Value: base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("seed-%d", i))), TTLMs: 60000}
		path := fmt.Sprintf("/api/cache/%s/%s", cfg.store, url.PathEscape(keys[i]))
		if err := doJSONRequest(ctx, client, cfg, phaseClientID(1, i, i), http.MethodPut, path, body, nil); err != nil {
			return phaseSummary{}, fmt.Errorf("seed key %q: %w", keys[i], err)
		}
	}

	var ops int64
	var errorsSeen int64
	var wg sync.WaitGroup
	for workerID := 0; workerID < cfg.concurrency; workerID++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			workerCtx := ctx
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID+1)*1_000_003))
			writeSeq := 0
			requestSeq := 0
			for {
				select {
				case <-workerCtx.Done():
					return
				default:
				}

				key := keys[rng.Intn(len(keys))]
				path := fmt.Sprintf("/api/cache/%s/%s", cfg.store, url.PathEscape(key))
				op := rng.Intn(100)
				requestSeq++
				clientID := phaseClientID(1, workerID, requestSeq)
				var err error

				switch {
				case op < 42:
					err = doRequest(workerCtx, client, cfg, clientID, http.MethodGet, path, nil, nil)
				case op < 72:
					value := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("worker-%d-seq-%d", workerID, writeSeq)))
					writeSeq++
					err = doJSONRequest(workerCtx, client, cfg, clientID, http.MethodPut, path, api.PutRequest{
						Value: value,
						TTLMs: 60000,
					}, nil)
				case op < 84:
					err = doRequest(workerCtx, client, cfg, clientID, http.MethodDelete, path, nil, nil)
				case op < 92:
					err = doRequest(workerCtx, client, cfg, clientID, http.MethodGet, fmt.Sprintf("/api/cache/%s/peek/%s", cfg.store, url.PathEscape(key)), nil, nil)
				default:
					err = doRequest(workerCtx, client, cfg, clientID, http.MethodGet, fmt.Sprintf("/api/cache/%s/stats", cfg.store), nil, nil)
				}

				if err != nil {
					atomic.AddInt64(&errorsSeen, 1)
					continue
				}
				atomic.AddInt64(&ops, 1)
				if cfg.apiPause > 0 {
					select {
					case <-workerCtx.Done():
						return
					case <-time.After(cfg.apiPause):
					}
				}
			}
		}(workerID)
	}

	wg.Wait()
	return phaseSummary{
		Phase:    "api",
		Duration: time.Since(start),
		Ops:      ops,
		Errors:   errorsSeen,
	}, nil
}

func runSSEPhase(client *http.Client, cfg loadConfig) (phaseSummary, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), cfg.duration)
	defer cancel()

	var events int64
	var errorsSeen int64
	var wg sync.WaitGroup

	for clientID := 0; clientID < cfg.sseClients; clientID++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()
			workerCtx, workerCancel := context.WithTimeout(ctx, cfg.duration)
			defer workerCancel()

			token, err := mintSSEToken(workerCtx, client, cfg, phaseClientID(2, clientID, 0))
			if err != nil {
				atomic.AddInt64(&errorsSeen, 1)
				return
			}
			req, err := http.NewRequestWithContext(workerCtx, http.MethodGet, cfg.baseURL+"/api/sse/stats/"+url.PathEscape(cfg.store)+"?access_token="+url.QueryEscape(token), nil)
			if err != nil {
				atomic.AddInt64(&errorsSeen, 1)
				return
			}
			req.Header.Set("X-Forwarded-For", workerIP(phaseClientID(2, clientID, 1)))
			resp, err := client.Do(req)
			if err != nil {
				if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
					atomic.AddInt64(&errorsSeen, 1)
				}
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				atomic.AddInt64(&errorsSeen, 1)
				return
			}

			reader := bufio.NewReader(resp.Body)
			seen := 0
			for {
				select {
				case <-workerCtx.Done():
					if seen < cfg.sseMinEvents {
						atomic.AddInt64(&errorsSeen, 1)
					}
					return
				default:
				}
				line, err := reader.ReadString('\n')
				if err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || workerCtx.Err() != nil || errors.Is(err, io.EOF) {
						if seen < cfg.sseMinEvents {
							atomic.AddInt64(&errorsSeen, 1)
						}
						return
					}
					atomic.AddInt64(&errorsSeen, 1)
					return
				}
				if strings.HasPrefix(line, "data: ") {
					seen++
					atomic.AddInt64(&events, 1)
				}
			}
		}(clientID)
	}

	wg.Wait()
	return phaseSummary{
		Phase:    "sse",
		Duration: time.Since(start),
		Events:   events,
		Errors:   errorsSeen,
	}, nil
}

func runBenchmarkPhase(client *http.Client, cfg loadConfig) (phaseSummary, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), cfg.duration+time.Duration(cfg.benchmarkRuns)*(cfg.benchmarkDuration+10*time.Second))
	defer cancel()

	var runs int
	for i := 0; i < cfg.benchmarkRuns; i++ {
		job, err := submitBenchmark(ctx, client, cfg, i)
		if err != nil {
			return phaseSummary{}, err
		}
		if err := awaitBenchmark(ctx, client, cfg, job.ID, i); err != nil {
			return phaseSummary{}, err
		}
		runs++
	}

	if err := verifyBenchmarkHistory(ctx, client, cfg, runs); err != nil {
		return phaseSummary{}, err
	}

	return phaseSummary{
		Phase:    "benchmark",
		Duration: time.Since(start),
		Runs:     runs,
	}, nil
}

func submitBenchmark(ctx context.Context, client *http.Client, cfg loadConfig, workerID int) (*api.BenchmarkJob, error) {
	reqBody := api.BenchmarkRunRequest{
		Workload:    cfg.benchmarkWorkload,
		Policies:    cfg.benchmarkPolicies,
		CacheSize:   cfg.benchmarkCacheSize,
		DurationSec: int(cfg.benchmarkDuration.Round(time.Second).Seconds()),
		Concurrency: cfg.benchmarkConcurrency,
		WorkloadN:   cfg.benchmarkWorkloadN,
	}
	if reqBody.DurationSec <= 0 {
		reqBody.DurationSec = 1
	}
	var job api.BenchmarkJob
	if err := doJSONRequest(ctx, client, cfg, phaseClientID(3, workerID, 0), http.MethodPost, "/api/benchmark/run", reqBody, &job); err != nil {
		return nil, fmt.Errorf("submit benchmark: %w", err)
	}
	if job.ID == "" {
		return nil, errors.New("benchmark response did not include a job id")
	}
	return &job, nil
}

func awaitBenchmark(ctx context.Context, client *http.Client, cfg loadConfig, jobID string, workerID int) error {
	deadline := time.Now().Add(cfg.benchmarkDuration + 20*time.Second)
	pollCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	pollCount := 0

	for {
		var job api.BenchmarkJob
		if err := doRequest(pollCtx, client, cfg, phaseClientID(3, workerID, pollCount), http.MethodGet, "/api/benchmark/results/"+url.PathEscape(jobID), nil, &job); err != nil {
			return fmt.Errorf("poll benchmark %s: %w", jobID, err)
		}
		switch job.Status {
		case "done":
			return nil
		case "error":
			if job.Error != "" {
				return fmt.Errorf("benchmark %s failed: %s", jobID, job.Error)
			}
			return fmt.Errorf("benchmark %s failed", jobID)
		}

		select {
		case <-pollCtx.Done():
			return fmt.Errorf("benchmark %s timed out", jobID)
		case <-ticker.C:
		}
		pollCount++
	}
}

func verifyBenchmarkHistory(ctx context.Context, client *http.Client, cfg loadConfig, expectedMin int) error {
	var jobs []api.BenchmarkJob
	if err := doRequest(ctx, client, cfg, phaseClientID(3, 0, 999), http.MethodGet, "/api/benchmark/results", nil, &jobs); err != nil {
		return fmt.Errorf("fetch benchmark history: %w", err)
	}
	if len(jobs) < expectedMin {
		return fmt.Errorf("benchmark history too small: got %d want at least %d", len(jobs), expectedMin)
	}
	return nil
}

func mintSSEToken(ctx context.Context, client *http.Client, cfg loadConfig, workerID int) (string, error) {
	var payload map[string]any
	if err := doRequest(ctx, client, cfg, workerID, http.MethodPost, "/api/auth/sse-token", nil, &payload); err != nil {
		return "", fmt.Errorf("mint sse token: %w", err)
	}
	token, _ := payload["token"].(string)
	if token == "" {
		return "", errors.New("sse token response did not include a token")
	}
	return token, nil
}

func doJSONRequest(ctx context.Context, client *http.Client, cfg loadConfig, workerID int, method, path string, reqBody any, respOut any) error {
	var body io.Reader
	if reqBody != nil {
		raw, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	return doRequestWithBody(ctx, client, cfg, workerID, method, path, body, respOut, "application/json")
}

func doRequest(ctx context.Context, client *http.Client, cfg loadConfig, workerID int, method, path string, _ any, respOut any) error {
	return doRequestWithBody(ctx, client, cfg, workerID, method, path, nil, respOut, "")
}

func doRequestWithBody(ctx context.Context, client *http.Client, cfg loadConfig, workerID int, method, path string, body io.Reader, respOut any, contentType string) error {
	req, err := http.NewRequestWithContext(ctx, method, cfg.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", cfg.apiKey)
	req.Header.Set("X-Forwarded-For", workerIP(workerID))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return fmt.Errorf("%s %s returned %s: %s", method, path, resp.Status, strings.TrimSpace(string(msg)))
	}
	if respOut == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(respOut)
}

func workerIP(workerID int) string {
	third := workerID / 250
	fourth := workerID%250 + 1
	if third > 249 {
		third = third % 250
	}
	return fmt.Sprintf("10.250.%d.%d", third+1, fourth)
}

func phaseClientID(phase, worker, seq int) int {
	return phase*1_000_000 + worker*10_000 + seq + 1
}

func splitList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func getenvDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
