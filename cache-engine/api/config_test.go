package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourname/cache-engine/internal/cache"
	"github.com/yourname/cache-engine/internal/cache/lru"
	"github.com/yourname/cache-engine/internal/coherence"
	"github.com/yourname/cache-engine/internal/store"
)

func newTestServer(t *testing.T, cfg ServerConfig) http.Handler {
	t.Helper()
	backing := store.NewMemoryStore(0)
	entry, err := NewStoreEntry("lru", "write-through", 16, backing)
	require.NoError(t, err)
	coord := coherence.NewCoordinator(map[string]cache.Cache{"node-a": lru.New(4)}, coherence.NewBus())
	t.Cleanup(coord.Close)
	server, err := NewServer(map[string]*StoreEntry{"lru": entry}, coord, cfg)
	require.NoError(t, err)
	return server
}

func TestConfigCanSwitchWritePolicy(t *testing.T) {
	backing := store.NewMemoryStore(0)
	entry, err := NewStoreEntry("lru", "write-through", 16, backing)
	require.NoError(t, err)
	require.NoError(t, entry.Cache.Set("k", []byte("value"), 0))

	coord := coherence.NewCoordinator(map[string]cache.Cache{"node-a": lru.New(4)}, coherence.NewBus())
	t.Cleanup(coord.Close)
	cfg := NewServerConfig(nil, "")
	cfg.Environment = "development"
	cfg.AllowInsecureNoAuth = true
	server, err := NewServer(map[string]*StoreEntry{"lru": entry}, coord, cfg)
	require.NoError(t, err)
	body := bytes.NewBufferString(`{"writePolicy":"write-back"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/cache/lru/config", body)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "write-back", entry.WritePolicy)
	value, ok := entry.Cache.Get("k")
	require.True(t, ok)
	assert.Equal(t, []byte("value"), value)
}

func TestConfigRejectsUnsupportedWritePolicySwitch(t *testing.T) {
	coord := coherence.NewCoordinator(map[string]cache.Cache{"node-a": lru.New(4)}, coherence.NewBus())
	t.Cleanup(coord.Close)
	cfg := NewServerConfig(nil, "")
	cfg.Environment = "development"
	cfg.AllowInsecureNoAuth = true
	server, err := NewServer(map[string]*StoreEntry{
		"lru-sharded": {
			Cache:            lru.New(16),
			Policy:           "lru",
			WritePolicy:      "none",
			Kind:             storeKindSharded,
			AllowWritePolicy: false,
		},
	}, coord, cfg)
	require.NoError(t, err)
	body := bytes.NewBufferString(`{"writePolicy":"write-back"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/cache/lru-sharded/config", body)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCORSRejectsUnknownOrigin(t *testing.T) {
	cfg := NewServerConfig([]string{"http://allowed.example"}, "")
	cfg.Environment = "development"
	cfg.AllowInsecureNoAuth = true
	handler := newTestServer(t, cfg)
	req := httptest.NewRequest(http.MethodGet, "/api/cache/lru/stats", nil)
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestAPIKeyMiddlewareRejectsMissingKey(t *testing.T) {
	handler := newTestServer(t, NewServerConfig([]string{"http://allowed.example"}, "secret"))
	req := httptest.NewRequest(http.MethodGet, "/api/cache/lru/stats", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAPIKeyMiddlewareAcceptsValidKey(t *testing.T) {
	handler := newTestServer(t, NewServerConfig([]string{"http://allowed.example"}, "secret"))
	req := httptest.NewRequest(http.MethodGet, "/api/cache/lru/stats", nil)
	req.Header.Set("X-API-Key", "secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp StatsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "write-through", resp.WritePolicy)
}

func TestIssueAndValidateAccessToken(t *testing.T) {
	cfg := NewServerConfig(nil, "secret")
	token, _, err := cfg.issueAccessToken("sse")
	require.NoError(t, err)

	assert.True(t, cfg.validateAccessToken(token, "sse"))
	assert.False(t, cfg.validateAccessToken(token, "api"))
}

func TestSSETokenEndpointIssuesToken(t *testing.T) {
	handler := newTestServer(t, NewServerConfig([]string{"http://allowed.example"}, "secret"))
	req := httptest.NewRequest(http.MethodPost, "/api/auth/sse-token", nil)
	req.Header.Set("X-API-Key", "secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp["token"])
}

func TestSetRejectsNegativeTTL(t *testing.T) {
	cfg := NewServerConfig(nil, "")
	cfg.Environment = "development"
	cfg.AllowInsecureNoAuth = true
	handler := newTestServer(t, cfg)
	req := httptest.NewRequest(http.MethodPut, "/api/cache/lru/key", bytes.NewBufferString(`{"value":"dmFsdWU=","ttlMs":-1}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestConfigRejectsInvalidLatency(t *testing.T) {
	cfg := NewServerConfig(nil, "")
	cfg.Environment = "development"
	cfg.AllowInsecureNoAuth = true
	handler := newTestServer(t, cfg)
	req := httptest.NewRequest(http.MethodPost, "/api/cache/lru/config", bytes.NewBufferString(`{"storeLatencyMs":-2}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestBenchmarkRunRejectsInvalidWorkload(t *testing.T) {
	cfg := NewServerConfig(nil, "")
	cfg.Environment = "development"
	cfg.AllowInsecureNoAuth = true
	handler := newTestServer(t, cfg)
	req := httptest.NewRequest(http.MethodPost, "/api/benchmark/run", bytes.NewBufferString(`{"workload":"bogus"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRequestRejectsUnknownFields(t *testing.T) {
	cfg := NewServerConfig(nil, "")
	cfg.Environment = "development"
	cfg.AllowInsecureNoAuth = true
	handler := newTestServer(t, cfg)
	req := httptest.NewRequest(http.MethodPost, "/api/coherence/set", bytes.NewBufferString(`{"node":"node-a","key":"k","value":"dmFsdWU=","extra":true}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestServerConfigRejectsMissingAPIKeyByDefault(t *testing.T) {
	cfg := NewServerConfig(nil, "")

	err := cfg.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "CACHE_ENGINE_API_KEY")
}
