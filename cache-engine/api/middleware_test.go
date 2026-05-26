package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (r *flushRecorder) Flush() {
	r.flushed = true
}

func TestLoggingMiddlewarePreservesFlusher(t *testing.T) {
	handler := loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		require.True(t, ok, "wrapped response writer must preserve http.Flusher")
		flusher.Flush()
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/sse", nil)
	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	handler.ServeHTTP(rec, req)

	require.True(t, rec.flushed)
	require.Equal(t, http.StatusNoContent, rec.Code)
}
