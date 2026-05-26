package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/yourname/cache-engine/internal/coherence"
)

type Server struct {
	handler *Handler
	router  http.Handler
	metrics *Metrics
}

// NewServer sets up the chi router with all routes.
func NewServer(stores map[string]*StoreEntry, coordinator *coherence.Coordinator, cfg ServerConfig) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	h := NewHandler(stores, coordinator, cfg)
	metrics := newMetrics()
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(requestTracingMiddleware)
	r.Use(recoveryMiddleware(metrics))
	r.Use(loggingMiddleware)
	r.Use(metricsMiddleware(metrics))

	r.Get("/healthz", h.HandleHealth)
	r.Get("/readyz", h.HandleReady)

	r.Group(func(r chi.Router) {
		r.Use(rateLimitMiddleware(cfg, metrics))
		r.Use(corsMiddleware(cfg))
		if cfg.requiresAPIKey() {
			r.Use(apiKeyMiddleware(cfg, metrics))
		}

		r.Get("/metrics", metricsHandler(h, metrics))

		r.Route("/api", func(r chi.Router) {
			r.Post("/auth/sse-token", h.HandleSSEToken)

			// Cache operations
			r.Route("/cache/{store}", func(r chi.Router) {
				r.Get("/{key}", h.HandleGet)
				r.Put("/{key}", h.HandleSet)
				r.Delete("/{key}", h.HandleDelete)
				r.Get("/peek/{key}", h.HandlePeek)
				r.Get("/stats", h.HandleStats)
				r.Get("/snapshot", h.HandleSnapshot)
				r.Delete("/purge", h.HandlePurge)
				r.Post("/config", h.HandleConfig)
			})

			// Coherence
			r.Post("/coherence/set", h.HandleCoherenceSet)
			r.Get("/coherence/nodes", h.HandleCoherenceNodes)

			// Benchmarks
			r.Post("/benchmark/run", h.HandleBenchmarkRun)
			r.Get("/benchmark/results", h.HandleBenchmarkResults)
			r.Get("/benchmark/results/{jobID}", h.HandleBenchmarkResult)

			// SSE
			r.Get("/sse/stats/{store}", h.ServeStatsSSE)
			r.Get("/sse/coherence", h.ServeCoherenceSSE)
		})
	})

	return &Server{
		handler: h,
		router:  r,
		metrics: metrics,
	}, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil || s.handler == nil {
		return nil
	}
	return s.handler.Shutdown(ctx)
}
