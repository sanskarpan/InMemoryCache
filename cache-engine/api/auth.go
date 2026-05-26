package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type accessTokenClaims struct {
	Scope     string `json:"scope"`
	ExpiresAt int64  `json:"expiresAt"`
}

func (c ServerConfig) issueAccessToken(scope string) (string, time.Time, error) {
	if !c.requiresAPIKey() {
		return "", time.Time{}, errors.New("api key auth is not configured")
	}
	expiresAt := time.Now().Add(c.SSETokenTTL).UTC()
	claims := accessTokenClaims{
		Scope:     scope,
		ExpiresAt: expiresAt.Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", time.Time{}, err
	}
	mac := hmac.New(sha256.New, []byte(c.APIKey))
	mac.Write(payload)
	sig := mac.Sum(nil)
	token := base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig)
	return token, expiresAt, nil
}

func (c ServerConfig) validateAccessToken(token string, requiredScope string) bool {
	if !c.requiresAPIKey() || token == "" {
		return false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(c.APIKey))
	mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return false
	}
	var claims accessTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return false
	}
	if claims.Scope != requiredScope && claims.Scope != "api" {
		return false
	}
	return time.Now().UTC().Unix() < claims.ExpiresAt
}

func authScopeForRequest(r *http.Request) string {
	if strings.HasPrefix(r.URL.Path, "/api/sse/") {
		return "sse"
	}
	return "api"
}

func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	authz := r.Header.Get("Authorization")
	if !strings.HasPrefix(authz, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(authz, prefix))
}

func queryAccessToken(r *http.Request) string {
	return strings.TrimSpace(r.URL.Query().Get("access_token"))
}

func apiKeyHeader(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-API-Key"))
}

func (h *Handler) HandleSSEToken(w http.ResponseWriter, _ *http.Request) {
	token, expiresAt, err := h.cfg.issueAccessToken("sse")
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "TOKEN_ISSUE_ERROR", fmt.Sprintf("issue sse token: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":     token,
		"expiresAt": expiresAt,
	})
}
