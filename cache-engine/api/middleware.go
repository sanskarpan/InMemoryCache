package api

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

func corsMiddleware(cfg ServerConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && cfg.isOriginAllowed(origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")
			if r.Method == http.MethodOptions {
				if origin != "" && !cfg.isOriginAllowed(origin) {
					http.Error(w, "origin not allowed", http.StatusForbidden)
					return
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if origin != "" && !cfg.isOriginAllowed(origin) {
				http.Error(w, "origin not allowed", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func apiKeyMiddleware(cfg ServerConfig, metrics *Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope := authScopeForRequest(r)
			bearer := bearerToken(r)
			if cfg.validateAPIKey(apiKeyHeader(r)) || cfg.validateAPIKey(bearer) || cfg.validateAccessToken(bearer, scope) || cfg.validateAccessToken(queryAccessToken(r), scope) {
				next.ServeHTTP(w, r)
				return
			}
			metrics.recordAuthRejection()
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		})
	}
}

type rateLimitBucket struct {
	count   int
	resetAt time.Time
}

func rateLimitMiddleware(cfg ServerConfig, metrics *Metrics) func(http.Handler) http.Handler {
	if cfg.RateLimitRequests <= 0 || cfg.RateLimitWindow <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	limiter, err := newSQLiteRateLimiter(cfg.StateDBPath)
	if err != nil {
		panic(err)
	}

	clientID := func(r *http.Request) string {
		if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
			parts := strings.Split(forwarded, ",")
			return strings.TrimSpace(parts[0])
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil && host != "" {
			return host
		}
		return r.RemoteAddr
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions || r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
				next.ServeHTTP(w, r)
				return
			}

			id := clientID(r)
			allowed, retryAfter, err := limiter.Allow(r.Context(), id, cfg.RateLimitRequests, cfg.RateLimitWindow)
			if err != nil {
				http.Error(w, "rate limiter unavailable", http.StatusServiceUnavailable)
				return
			}
			if !allowed {
				if retryAfter < 0 {
					retryAfter = 0
				}
				metrics.recordRateLimitRejection()
				w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

func (sw *statusWriter) Flush() {
	if flusher, ok := sw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (sw *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := sw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func (sw *statusWriter) Unwrap() http.ResponseWriter {
	return sw.ResponseWriter
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		route := routePattern(r)
		slog.Info("http_request",
			slog.String("method", sanitizeLogValue(r.Method)),
			slog.String("path", sanitizeURLPath(r.URL)),
			slog.String("route", route),
			slog.Int("status", sw.status),
			slog.String("request_id", chimw.GetReqID(r.Context())),
			slog.String("trace_id", traceIDFromContext(r.Context())),
			slog.String("remote_addr", sanitizeLogValue(clientAddress(r))),
			slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
		)
	})
}

func sanitizeLogValue(v string) string {
	return strings.NewReplacer("\n", "_", "\r", "_", "\t", "_").Replace(v)
}

func sanitizeURLPath(u *url.URL) string {
	if u == nil {
		return ""
	}
	return sanitizeLogValue(u.EscapedPath())
}

func recoveryMiddleware(metrics *Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					metrics.recordRecoveredPanic()
					slog.Error("panic_recovered",
						slog.Any("panic", rec),
						slog.String("request_id", chimw.GetReqID(r.Context())),
						slog.String("trace_id", traceIDFromContext(r.Context())),
						slog.String("method", sanitizeLogValue(r.Method)),
						slog.String("path", sanitizeURLPath(r.URL)),
						slog.String("stack", string(debug.Stack())),
					)
					http.Error(w, "internal server error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func metricsMiddleware(metrics *Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: 200}
			next.ServeHTTP(sw, r)
			metrics.recordHTTPRequest(r.Method, routePattern(r), sw.status, time.Since(start))
		})
	}
}

func routePattern(r *http.Request) string {
	if r == nil {
		return ""
	}
	if routeContext := chi.RouteContext(r.Context()); routeContext != nil {
		pattern := routeContext.RoutePattern()
		if pattern != "" {
			return pattern
		}
	}
	return sanitizeURLPath(r.URL)
}

func clientAddress(r *http.Request) string {
	if r == nil {
		return ""
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}
