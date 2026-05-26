package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/yourname/cache-engine/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usageAndExit("missing command")
	}

	switch os.Args[1] {
	case "backup":
		runBackup(os.Args[2:])
	case "migrate":
		runMigrate(os.Args[2:])
	default:
		usageAndExit(fmt.Sprintf("unknown command: %s", os.Args[1]))
	}
}

func runBackup(args []string) {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	dbPath := fs.String("db", filepath.Join("data", "cache-engine.db"), "Path to the SQLite database")
	outPath := fs.String("out", filepath.Join("backups", fmt.Sprintf("cache-engine-%s.sqlite3", time.Now().UTC().Format("20060102T150405Z"))), "Output path for the backup file")
	fs.Parse(args)

	if err := store.BackupSQLiteDatabase(*dbPath, *outPath); err != nil {
		slog.Error("sqlite_backup_failed", slog.Any("error", err), slog.String("db", *dbPath), slog.String("out", *outPath))
		os.Exit(1)
	}
	slog.Info("sqlite_backup_completed", slog.String("db", *dbPath), slog.String("out", *outPath))
}

func runMigrate(args []string) {
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	dbPath := fs.String("db", filepath.Join("data", "cache-engine.db"), "Path to the SQLite database")
	fs.Parse(args)

	if err := store.MigrateSQLiteDatabase(*dbPath); err != nil {
		slog.Error("sqlite_migration_failed", slog.Any("error", err), slog.String("db", *dbPath))
		os.Exit(1)
	}
	slog.Info("sqlite_migration_completed", slog.String("db", *dbPath))
}

func usageAndExit(message string) {
	fmt.Fprintf(os.Stderr, "%s\n\nUsage:\n  go run ./cmd/admin backup --db data/cache-engine.db --out backups/cache-engine.sqlite3\n  go run ./cmd/admin migrate --db data/cache-engine.db\n", message)
	os.Exit(2)
}
