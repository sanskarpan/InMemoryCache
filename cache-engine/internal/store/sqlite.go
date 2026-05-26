package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db        *sql.DB
	latencyNs atomic.Int64
}

const currentSchemaVersion = 1

func NewSQLiteStore(path string, latency time.Duration) (*SQLiteStore, error) {
	if path == "" {
		return nil, errors.New("sqlite path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	store := &SQLiteStore{db: db}
	store.latencyNs.Store(latency.Nanoseconds())
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) init() error {
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at INTEGER NOT NULL
		)
	`); err != nil {
		return err
	}

	var version int
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version); err != nil {
		return err
	}
	if version >= currentSchemaVersion {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if version < 1 {
		if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS cache_store (
				key TEXT PRIMARY KEY,
				value BLOB NOT NULL,
				updated_at INTEGER NOT NULL
			)
		`); err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			1,
			time.Now().UTC().Unix(),
		); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func BackupSQLiteDatabase(path string, destination string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("sqlite path is required")
	}
	if strings.TrimSpace(destination) == "" {
		return errors.New("backup destination is required")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return err
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	stmt := fmt.Sprintf("VACUUM INTO %s", quoteSQLiteLiteral(destination))
	if _, err := db.Exec(stmt); err != nil {
		return err
	}
	return nil
}

func MigrateSQLiteDatabase(path string) error {
	store, err := NewSQLiteStore(path, 0)
	if err != nil {
		return err
	}
	return store.Close()
}

func quoteSQLiteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) SetLatency(d time.Duration) {
	s.latencyNs.Store(d.Nanoseconds())
}

func (s *SQLiteStore) GetLatency() time.Duration {
	return time.Duration(s.latencyNs.Load())
}

func (s *SQLiteStore) sleep(ctx context.Context) error {
	ns := s.latencyNs.Load()
	if ns <= 0 {
		return nil
	}
	timer := time.NewTimer(time.Duration(ns))
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *SQLiteStore) Get(ctx context.Context, key string) ([]byte, error) {
	if err := s.sleep(ctx); err != nil {
		return nil, err
	}
	var value []byte
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM cache_store WHERE key = ?`, key).Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("key not found: %s", key)
		}
		return nil, err
	}
	return append([]byte(nil), value...), nil
}

func (s *SQLiteStore) Set(ctx context.Context, key string, value []byte) error {
	if err := s.sleep(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO cache_store (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key,
		append([]byte(nil), value...),
		time.Now().UnixMilli(),
	)
	return err
}

func (s *SQLiteStore) Delete(ctx context.Context, key string) error {
	if err := s.sleep(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM cache_store WHERE key = ?`, key)
	return err
}

func (s *SQLiteStore) Keys(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key FROM cache_store ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := make([]string, 0, 128)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}
