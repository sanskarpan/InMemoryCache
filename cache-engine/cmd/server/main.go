package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/yourname/cache-engine/api"
	"github.com/yourname/cache-engine/internal/cache"
	"github.com/yourname/cache-engine/internal/cache/lru"
	"github.com/yourname/cache-engine/internal/cache/sharded"
	"github.com/yourname/cache-engine/internal/coherence"
	"github.com/yourname/cache-engine/internal/store"
)

func main() {
	runtimeCfg := loadRuntimeConfig()
	setupLogger(runtimeCfg.logFormat, runtimeCfg.logLevel)
	slog.Info("cache_engine_starting",
		slog.String("environment", runtimeCfg.environment),
		slog.Bool("seed_demo_data", runtimeCfg.seedDemoData),
		slog.String("listen_addr", runtimeCfg.addr),
	)

	dataPath := strings.TrimSpace(os.Getenv("CACHE_ENGINE_STATE_DB_PATH"))
	if dataPath == "" {
		dataPath = filepath.Join("data", "cache-engine.db")
	}

	var backing store.ConfigurableStore
	driverName := strings.TrimSpace(os.Getenv("CACHE_ENGINE_BACKING_STORE_DRIVER"))
	switch driverName {
	case "", "sqlite":
		sqliteStore, err := store.NewSQLiteStore(dataPath, 5*time.Millisecond)
		if err != nil {
			exitWithError("backing_store_init_failed", err)
		}
		backing = sqliteStore
	case "memory":
		backing = store.NewMemoryStore(5 * time.Millisecond)
	default:
		exitWithError("unsupported_backing_store_driver", fmt.Errorf("%q", sanitizeLogValue(driverName)))
	}

	// Create caches
	lruStore, err := api.NewStoreEntry("lru", "write-through", 1000, backing)
	if err != nil {
		exitWithError("create_lru_store_failed", err)
	}
	lfuStore, err := api.NewStoreEntry("lfu", "write-through", 1000, backing)
	if err != nil {
		exitWithError("create_lfu_store_failed", err)
	}
	arcStore, err := api.NewStoreEntry("arc", "write-through", 1000, backing)
	if err != nil {
		exitWithError("create_arc_store_failed", err)
	}
	lruSharded := sharded.New(10000, "lru", func(cap int) cache.Cache {
		return lru.New(cap)
	})

	stores := map[string]*api.StoreEntry{
		"lru": lruStore,
		"lfu": lfuStore,
		"arc": arcStore,
		"lru-sharded": {
			Cache:            lruSharded,
			Policy:           "lru",
			WritePolicy:      "none",
			Kind:             "sharded",
			AllowWritePolicy: false,
		},
	}

	if runtimeCfg.seedDemoData {
		seedCaches(stores)
	}

	// Coherence nodes
	coherenceNodes := map[string]cache.Cache{
		"node-a": lru.New(100),
		"node-b": lru.New(100),
		"node-c": lru.New(100),
	}
	bus := coherence.NewBus()
	coord := coherence.NewCoordinator(coherenceNodes, bus)

	// Start HTTP server
	origins := strings.Split(os.Getenv("CACHE_ENGINE_ALLOWED_ORIGINS"), ",")
	if runtimeCfg.environment == "development" && len(origins) == 1 && strings.TrimSpace(origins[0]) == "" {
		origins = []string{
			"http://localhost:5173",
			"http://127.0.0.1:5173",
			"http://localhost:4173",
			"http://127.0.0.1:4173",
		}
	}
	serverCfg := api.NewServerConfig(origins, os.Getenv("CACHE_ENGINE_API_KEY"))
	serverCfg.Environment = runtimeCfg.environment
	serverCfg.AllowInsecureNoAuth = runtimeCfg.allowInsecureNoAuth
	serverCfg.StateDBPath = dataPath
	if limit := strings.TrimSpace(os.Getenv("CACHE_ENGINE_RATE_LIMIT_REQUESTS")); limit != "" {
		if parsed, err := strconv.Atoi(limit); err == nil {
			serverCfg.RateLimitRequests = parsed
		}
	}
	if window := strings.TrimSpace(os.Getenv("CACHE_ENGINE_RATE_LIMIT_WINDOW_MS")); window != "" {
		if parsed, err := strconv.Atoi(window); err == nil && parsed > 0 {
			serverCfg.RateLimitWindow = time.Duration(parsed) * time.Millisecond
		}
	}
	serverCfg.BenchmarkResultsPath = strings.TrimSpace(os.Getenv("CACHE_ENGINE_BENCHMARK_RESULTS_PATH"))
	if serverCfg.BenchmarkResultsPath == "" {
		serverCfg.BenchmarkResultsPath = filepath.Join("data", "benchmark-jobs.json")
	}
	if ttl := strings.TrimSpace(os.Getenv("CACHE_ENGINE_SSE_TOKEN_TTL_MS")); ttl != "" {
		if parsed, err := strconv.Atoi(ttl); err == nil && parsed > 0 {
			serverCfg.SSETokenTTL = time.Duration(parsed) * time.Millisecond
		}
	}
	if shutdownMs := strings.TrimSpace(os.Getenv("CACHE_ENGINE_SHUTDOWN_TIMEOUT_MS")); shutdownMs != "" {
		if parsed, err := strconv.Atoi(shutdownMs); err == nil && parsed > 0 {
			serverCfg.ShutdownTimeout = time.Duration(parsed) * time.Millisecond
		}
	}

	app, err := api.NewServer(stores, coord, serverCfg)
	if err != nil {
		exitWithError("server_config_invalid", err)
	}

	var cleanupOnce sync.Once
	cleanup := func(ctx context.Context) {
		cleanupOnce.Do(func() {
			if err := app.Shutdown(ctx); err != nil {
				slog.Error("application_shutdown_failed", slog.Any("error", err))
			}
			coord.Close()
			if closer, ok := backing.(interface{ Close() error }); ok {
				if err := closer.Close(); err != nil {
					slog.Error("backing_store_close_failed", slog.Any("error", err))
				}
			}
		})
	}

	shutdownSignals, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	shutdownDone := make(chan struct{})

	server := &http.Server{
		Addr:              cfgRuntimeAddr(),
		Handler:           app,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		<-shutdownSignals.Done()
		slog.Info("shutdown_signal_received", slog.Duration("timeout", serverCfg.ShutdownTimeout))
		ctx, cancel := context.WithTimeout(context.Background(), serverCfg.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			slog.Error("http_server_shutdown_failed", slog.Any("error", err))
			_ = server.Close()
		}
		cleanup(ctx)
		close(shutdownDone)
	}()

	slog.Info("http_server_listening", slog.String("addr", server.Addr))
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		cleanup(context.Background())
		exitWithError("http_server_failed", err)
	}
	<-shutdownDone
	slog.Info("cache_engine_stopped")
}

func seedCaches(stores map[string]*api.StoreEntry) {
	words := []string{"apple", "banana", "cherry", "delta", "echo", "foxtrot", "golf", "hotel"}
	for _, entry := range stores {
		for i := 0; i < 200; i++ {
			key := fmt.Sprintf("seed:%d", i)
			value := []byte(words[i%len(words)])
			_ = entry.Cache.Set(key, value, 0)
		}
		// Warm-up: 10000 Zipf-like accesses
		for i := 0; i < 10000; i++ {
			key := fmt.Sprintf("seed:%d", i%200)
			entry.Cache.Get(key)
		}
	}
}

func sanitizeLogValue(v string) string {
	return strings.NewReplacer("\n", "_", "\r", "_", "\t", "_").Replace(v)
}

type runtimeConfig struct {
	addr                string
	environment         string
	allowInsecureNoAuth bool
	seedDemoData        bool
	logFormat           string
	logLevel            string
}

func loadRuntimeConfig() runtimeConfig {
	return runtimeConfig{
		addr:                cfgRuntimeAddr(),
		environment:         cfgRuntimeEnv(),
		allowInsecureNoAuth: cfgRuntimeAllowInsecureNoAuth(),
		seedDemoData:        parseBoolEnv("CACHE_ENGINE_SEED_DEMO_DATA", false),
		logFormat:           strings.TrimSpace(defaultString(os.Getenv("CACHE_ENGINE_LOG_FORMAT"), "json")),
		logLevel:            strings.TrimSpace(defaultString(os.Getenv("CACHE_ENGINE_LOG_LEVEL"), "info")),
	}
}

func cfgRuntimeAddr() string {
	return strings.TrimSpace(defaultString(os.Getenv("CACHE_ENGINE_ADDR"), ":8080"))
}

func cfgRuntimeEnv() string {
	env := strings.ToLower(strings.TrimSpace(defaultString(os.Getenv("CACHE_ENGINE_ENV"), "production")))
	switch env {
	case "dev":
		return "development"
	case "development", "production":
		return env
	default:
		return "production"
	}
}

func cfgRuntimeAllowInsecureNoAuth() bool {
	return parseBoolEnv("CACHE_ENGINE_ALLOW_INSECURE_NO_AUTH", false)
}

func parseBoolEnv(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func setupLogger(format string, level string) {
	var handler slog.Handler
	logLevel := new(slog.LevelVar)
	logLevel.Set(parseLogLevel(level))
	options := &slog.HandlerOptions{Level: logLevel}
	if strings.EqualFold(format, "text") {
		handler = slog.NewTextHandler(os.Stdout, options)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, options)
	}
	slog.SetDefault(slog.New(handler))
}

func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func exitWithError(event string, err error) {
	slog.Error(event, slog.Any("error", err))
	os.Exit(1)
}
