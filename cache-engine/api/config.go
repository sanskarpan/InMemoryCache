package api

import (
	"crypto/subtle"
	"errors"
	"strings"
	"time"
)

type ServerConfig struct {
	AllowedOrigins       map[string]struct{}
	APIKey               string
	Environment          string
	AllowInsecureNoAuth  bool
	StateDBPath          string
	RateLimitRequests    int
	RateLimitWindow      time.Duration
	BenchmarkResultsPath string
	SSETokenTTL          time.Duration
	ShutdownTimeout      time.Duration
}

func NewServerConfig(allowedOrigins []string, apiKey string) ServerConfig {
	cfg := ServerConfig{
		AllowedOrigins:    make(map[string]struct{}),
		APIKey:            strings.TrimSpace(apiKey),
		Environment:       "production",
		SSETokenTTL:       2 * time.Minute,
		RateLimitRequests: 120,
		RateLimitWindow:   time.Minute,
		ShutdownTimeout:   15 * time.Second,
	}
	for _, origin := range allowedOrigins {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			continue
		}
		cfg.AllowedOrigins[trimmed] = struct{}{}
	}
	return cfg
}

func (c ServerConfig) Validate() error {
	if !c.AllowInsecureNoAuth && strings.TrimSpace(c.APIKey) == "" {
		return errors.New("CACHE_ENGINE_API_KEY is required unless insecure dev mode is explicitly enabled")
	}
	if c.AllowInsecureNoAuth && c.Environment != "development" {
		return errors.New("insecure no-auth mode is only allowed when CACHE_ENGINE_ENV=development")
	}
	if c.ShutdownTimeout <= 0 {
		return errors.New("shutdown timeout must be greater than zero")
	}
	if c.RateLimitRequests < 0 {
		return errors.New("rate limit requests must not be negative")
	}
	if c.RateLimitWindow < 0 {
		return errors.New("rate limit window must not be negative")
	}
	return nil
}

func (c ServerConfig) isOriginAllowed(origin string) bool {
	if origin == "" {
		return true
	}
	if len(c.AllowedOrigins) == 0 {
		return false
	}
	_, ok := c.AllowedOrigins[origin]
	return ok
}

func (c ServerConfig) requiresAPIKey() bool {
	return !c.AllowInsecureNoAuth
}

func (c ServerConfig) validateAPIKey(headerValue string) bool {
	if !c.requiresAPIKey() {
		return true
	}
	if len(headerValue) != len(c.APIKey) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(headerValue), []byte(c.APIKey)) == 1
}
