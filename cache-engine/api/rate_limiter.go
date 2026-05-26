package api

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type rateLimiter interface {
	Allow(ctx context.Context, clientID string, limit int, window time.Duration) (bool, time.Duration, error)
}

type memoryRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]rateLimitBucket
}

func newMemoryRateLimiter() rateLimiter {
	return &memoryRateLimiter{buckets: make(map[string]rateLimitBucket)}
}

func (m *memoryRateLimiter) Allow(_ context.Context, clientID string, limit int, window time.Duration) (bool, time.Duration, error) {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	bucket, ok := m.buckets[clientID]
	if !ok || now.After(bucket.resetAt) {
		bucket = rateLimitBucket{resetAt: now.Add(window)}
	}
	bucket.count++
	m.buckets[clientID] = bucket
	for key, existing := range m.buckets {
		if now.After(existing.resetAt) {
			delete(m.buckets, key)
		}
	}
	if bucket.count > limit {
		return false, time.Until(bucket.resetAt), nil
	}
	return true, 0, nil
}

type sqliteRateLimiter struct {
	db *sql.DB
}

func newSQLiteRateLimiter(path string) (rateLimiter, error) {
	if path == "" {
		return newMemoryRateLimiter(), nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS rate_limits (
			client_key TEXT NOT NULL,
			window_start INTEGER NOT NULL,
			count INTEGER NOT NULL,
			PRIMARY KEY (client_key, window_start)
		)
	`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &sqliteRateLimiter{db: db}, nil
}

func (s *sqliteRateLimiter) Allow(ctx context.Context, clientID string, limit int, window time.Duration) (bool, time.Duration, error) {
	now := time.Now().UTC()
	windowStart := now.Truncate(window)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, 0, err
	}
	defer tx.Rollback()

	var count int
	row := tx.QueryRowContext(ctx, `
		INSERT INTO rate_limits (client_key, window_start, count)
		VALUES (?, ?, 1)
		ON CONFLICT(client_key, window_start) DO UPDATE SET count = count + 1
		RETURNING count
	`, clientID, windowStart.UnixMilli())
	if err := row.Scan(&count); err != nil {
		return false, 0, err
	}
	_, _ = tx.ExecContext(ctx, `DELETE FROM rate_limits WHERE window_start < ?`, windowStart.Add(-2*window).UnixMilli())
	if err := tx.Commit(); err != nil {
		return false, 0, err
	}
	if count > limit {
		return false, window - now.Sub(windowStart), nil
	}
	return true, 0, nil
}
