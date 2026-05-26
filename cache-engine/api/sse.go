package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/cache-engine/internal/coherence"
	"github.com/yourname/cache-engine/internal/stats"
)

// sseStatsPayload enriches StatsSnapshot with cache metadata for the frontend.
type sseStatsPayload struct {
	stats.StatsSnapshot
	Size        int    `json:"size"`
	Capacity    int    `json:"capacity"`
	WritePolicy string `json:"writePolicy"`
	DirtyCount  int    `json:"dirtyCount,omitempty"`
}

// ServeStatsSSE streams cache stats every 500ms.
func (h *Handler) ServeStatsSSE(w http.ResponseWriter, r *http.Request) {
	storeName := chi.URLParam(r, "store")
	entry, ok := h.getStore(storeName)
	if !ok {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			entry.mu.RLock()
			payload := sseStatsPayload{
				StatsSnapshot: entry.Cache.Stats().Snapshot(),
				Size:          entry.Cache.Len(),
				Capacity:      entry.Cache.Capacity(),
				WritePolicy:   entry.WritePolicy,
			}
			if dc, ok := entry.Cache.(interface{ DirtyCount() int }); ok {
				payload.DirtyCount = dc.DirtyCount()
			}
			entry.mu.RUnlock()
			data, err := json.Marshal(payload)
			if err != nil {
				slog.Error("sse_marshal_stats_error", slog.Any("error", err))
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// ServeCoherenceSSE streams coherence events.
func (h *Handler) ServeCoherenceSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Subscribe to coherence events with a unique SSE client ID
	clientID := fmt.Sprintf("sse-client-%d", time.Now().UnixNano())
	bus := h.coordinator.Bus()
	ch := bus.Subscribe(clientID)
	defer bus.Unsubscribe(clientID)

	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			eventType := "invalidate"
			switch e.Type {
			case coherence.EventUpdate:
				eventType = "update"
			case coherence.EventFlush:
				eventType = "flush"
			}
			payload := map[string]any{
				"event":     eventType,
				"key":       e.Key,
				"from":      e.From,
				"timestamp": time.Now(),
			}
			data, err := json.Marshal(payload)
			if err != nil {
				slog.Error("sse_marshal_coherence_event_error", slog.Any("error", err))
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}
