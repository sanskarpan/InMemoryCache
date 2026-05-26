package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

type traceContextKey string

const traceIDContextKey traceContextKey = "trace_id"

func requestTracingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID := parseTraceID(r.Header.Get("Traceparent"))
		if traceID == "" {
			traceID = randomHex(16)
		}
		spanID := randomHex(8)
		if traceID == "" || spanID == "" {
			http.Error(w, "trace initialization failed", http.StatusServiceUnavailable)
			return
		}

		traceparent := "00-" + traceID + "-" + spanID + "-01"
		w.Header().Set("Traceparent", traceparent)
		w.Header().Set("X-Trace-Id", traceID)

		ctx := context.WithValue(r.Context(), traceIDContextKey, traceID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func traceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	traceID, _ := ctx.Value(traceIDContextKey).(string)
	return traceID
}

func parseTraceID(traceparent string) string {
	parts := strings.Split(strings.TrimSpace(traceparent), "-")
	if len(parts) != 4 || len(parts[1]) != 32 {
		return ""
	}
	traceID := strings.ToLower(parts[1])
	if _, err := hex.DecodeString(traceID); err != nil {
		return ""
	}
	return traceID
}

func randomHex(bytesLen int) string {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return hex.EncodeToString(buf)
}
