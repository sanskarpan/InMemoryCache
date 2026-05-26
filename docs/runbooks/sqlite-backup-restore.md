# SQLite backup and restore runbook

This runbook covers the SQLite state file used by the Cache Engine backend for:

- backing store persistence
- rate limiting state
- benchmark job persistence

The database is configured by `CACHE_ENGINE_STATE_DB_PATH` and defaults to `data/cache-engine.db`.

## When to use this runbook

Use this procedure when you need to:

- take a consistent backup before maintenance
- restore a known-good snapshot after data corruption
- migrate the state file between environments
- verify that a failed deployment can be rolled back safely

## Prerequisites

- shell access to the node, container, or Kubernetes pod that owns the active state file
- the Cache Engine admin command available in the same repo checkout
- enough disk space for one additional SQLite snapshot

## Backup procedure

Preferred method:

```bash
cd cache-engine
go run ./cmd/admin backup \
  --db data/cache-engine.db \
  --out backups/cache-engine-$(date +%Y%m%dT%H%M%S).sqlite3
```

Equivalent Make target:

```bash
cd cache-engine
make backup BACKUP_OUT=backups/cache-engine-$(date +%Y%m%dT%H%M%S).sqlite3
```

Notes:

- the admin command performs a consistent SQLite snapshot
- do not copy only `cache-engine.db` while the server is running
- if WAL mode is active, the admin command handles the live journal state for you

## Restore procedure

1. Stop the Cache Engine process cleanly.
2. Save the current database file somewhere safe in case you need to roll back again.
3. Replace the active state file with the backup snapshot.
4. Run the migration command against the restored file.
5. Start the service and verify readiness.

Example:

```bash
cd cache-engine
cp data/cache-engine.db backups/cache-engine.bad.$(date +%s).sqlite3
cp backups/cache-engine-20260526T120000.sqlite3 data/cache-engine.db
go run ./cmd/admin migrate --db data/cache-engine.db
go run ./cmd/server
```

## Validation after restore

After the service comes back:

- `GET /readyz` should return `200`
- `GET /healthz` should return `200`
- `GET /metrics` should return `200` with auth
- `GET /api/cache/{store}/stats` should show the expected store size and hit/miss counters
- benchmark job history should still be readable from `GET /api/benchmark/results`

If the application runs behind Kubernetes, perform the backup from a maintenance pod or from the same container image with the PVC mounted at the same path as the application. Avoid copying the file from the live pod filesystem while the process is still writing.

If it runs in Docker Compose, back up from the backend container’s mounted volume path instead of the host filesystem so you capture the live database used by the process.

## Rollback

If the restore is incorrect or incomplete:

1. stop the process again
2. restore the pre-restore copy saved in step 2
3. re-run migrations
4. re-check readiness

## Integrity check

Optional low-level verification:

```bash
sqlite3 data/cache-engine.db 'PRAGMA integrity_check;'
```

Use this before and after restore if you suspect corruption or an interrupted file copy.
