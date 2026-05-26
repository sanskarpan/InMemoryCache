# Cache Engine

In-memory cache engine built in Go with a React control plane. It supports LRU, LFU, ARC, write-through, write-back, write-around, TTL expiry, sharding, coherence simulation, benchmarking, structured logs, request tracing, metrics, and SQLite-backed state persistence.

## Production posture

- Secure by default: the API refuses to start without `CACHE_ENGINE_API_KEY` unless insecure dev mode is explicitly enabled.
- Demo seeding is off by default: sample data is loaded only when `CACHE_ENGINE_SEED_DEMO_DATA=true`.
- Graceful shutdown is built in: the process drains HTTP, flushes dirty cache state, persists benchmark job metadata, and then closes shared resources.
- Operational hooks are included: `/healthz`, `/readyz`, authenticated `/metrics`, JSON logs, `Traceparent` propagation, Prometheus alert rules, Docker, Compose, Kubernetes manifests, Terraform, and SQLite backup/migration commands.

## Quick start

Backend:

```bash
cp .env.example .env
# edit CACHE_ENGINE_API_KEY and CACHE_ENGINE_ALLOWED_ORIGINS first
export $(grep -v '^#' .env | xargs)
go run ./cmd/server
```

Frontend:

```bash
cd web
npm install
npm run dev
```

The frontend expects the backend on the same origin under `/api` in production. For local Vite development it talks to the backend through the existing relative API client, so the browser origin must be allowlisted by `CACHE_ENGINE_ALLOWED_ORIGINS`.

## Development mode

Unauthenticated local development is still possible, but only with an explicit opt-in:

```bash
CACHE_ENGINE_ENV=development \
CACHE_ENGINE_ALLOW_INSECURE_NO_AUTH=true \
CACHE_ENGINE_ALLOWED_ORIGINS=http://localhost:5173 \
go run ./cmd/server
```

Optional demo data:

```bash
CACHE_ENGINE_SEED_DEMO_DATA=true go run ./cmd/server
```

## Environment

| Variable | Required | Default | Purpose |
|---|---:|---|---|
| `CACHE_ENGINE_ENV` | No | `production` | Runtime mode. `development` is the only mode allowed to bypass auth. |
| `CACHE_ENGINE_ADDR` | No | `:8080` | HTTP listen address. |
| `CACHE_ENGINE_API_KEY` | Yes in production | none | API key for all authenticated routes and SSE token minting. |
| `CACHE_ENGINE_ALLOWED_ORIGINS` | Yes for browser clients | none in production | Comma-separated browser origins allowed by CORS. |
| `CACHE_ENGINE_STATE_DB_PATH` | No | `data/cache-engine.db` | SQLite state path for backing store and rate limiting. |
| `CACHE_ENGINE_BACKING_STORE_DRIVER` | No | `sqlite` | Backing store driver. `memory` is available for testing only. |
| `CACHE_ENGINE_RATE_LIMIT_REQUESTS` | No | `120` | Requests per window per client. |
| `CACHE_ENGINE_RATE_LIMIT_WINDOW_MS` | No | `60000` | Rate limit window length. |
| `CACHE_ENGINE_SSE_TOKEN_TTL_MS` | No | `120000` | Signed SSE access token lifetime. |
| `CACHE_ENGINE_SHUTDOWN_TIMEOUT_MS` | No | `15000` | Graceful shutdown budget. |
| `CACHE_ENGINE_LOG_FORMAT` | No | `json` | `json` or `text`. |
| `CACHE_ENGINE_LOG_LEVEL` | No | `info` | `debug`, `info`, `warn`, `error`. |
| `CACHE_ENGINE_SEED_DEMO_DATA` | No | `false` | Explicitly seed demo cache data on boot. |
| `CACHE_ENGINE_ALLOW_INSECURE_NO_AUTH` | No | `false` | Development-only escape hatch to disable auth. |

The browser bundle does not need the API key in production. The production frontend container proxies `/api` server-side and injects the upstream API key internally. Do not ship `VITE_CACHE_ENGINE_API_KEY` in a public frontend build.

## Architecture

```text
cache-engine/
├── api/                chi HTTP server, auth, tracing, metrics, SSE
├── cmd/admin/          SQLite backup and migration commands
├── cmd/server/         Runtime bootstrap and graceful shutdown
├── internal/cache/     LRU, LFU, ARC, sharded wrapper
├── internal/store/     SQLite store and write policy wrappers
├── internal/coherence/ Multi-node invalidation simulation
├── internal/benchmark/ Workload generators and runner
├── internal/stats/     Atomic counters and snapshots
├── internal/ttl/       TTL scheduling
└── web/                React + Vite + Tailwind + local SVG charts
```

## Operations

### Health and readiness

- `GET /healthz`
- `GET /readyz`

These endpoints bypass API-key auth by design so orchestration systems can probe them.

### Metrics

- `GET /metrics`

The metrics endpoint is protected by the same auth middleware as the rest of the API in production mode. It emits Prometheus text-format metrics for:

- HTTP request counts and duration
- readiness and benchmark activity
- auth and rate-limit rejections
- panic recoveries
- per-store hits, misses, evictions, TTL expiries, dirty counts, capacity, bytes stored
- process uptime and heap memory

For Prometheus Operator deployments, apply the monitoring bundle under [deploy/monitoring](/Users/sanskar/dev/Research/Projects/InMemoryCache/deploy/monitoring), place the scrape API key in the `monitoring` namespace secret described in [secret.example.yaml](/Users/sanskar/dev/Research/Projects/InMemoryCache/deploy/monitoring/secret.example.yaml:1), and use the provided [ServiceMonitor](/Users/sanskar/dev/Research/Projects/InMemoryCache/deploy/monitoring/service-monitor.yaml:1). The server accepts both `X-API-Key` and `Authorization: Bearer <api-key>` for non-SSE API authentication.

### Tracing and logs

- Every request gets or propagates a W3C `Traceparent` header.
- The server also returns `X-Trace-Id` on every response.
- Logs are centralized through `log/slog` and include request method, route, status, request ID, trace ID, remote IP, and duration.

### SQLite state, migrations, and backups

Schema creation and migration run automatically at startup through `internal/store/sqlite.go`. For operational workflows:

```bash
make migrate
make backup BACKUP_OUT=backups/cache-engine-$(date +%Y%m%dT%H%M%S).sqlite3
```

Equivalent direct commands:

```bash
go run ./cmd/admin migrate --db data/cache-engine.db
go run ./cmd/admin backup --db data/cache-engine.db --out backups/cache-engine.sqlite3
```

Because the process uses SQLite in WAL mode, back up the database through the admin command rather than a blind file copy.

## Deployment scaffolding

- Backend container: [Dockerfile](/Users/sanskar/dev/Research/Projects/InMemoryCache/cache-engine/Dockerfile:1)
- Frontend container: [web/Dockerfile](/Users/sanskar/dev/Research/Projects/InMemoryCache/cache-engine/web/Dockerfile:1)
- Frontend proxy template: [web/nginx.conf.template](/Users/sanskar/dev/Research/Projects/InMemoryCache/cache-engine/web/nginx.conf.template:1)
- Local multi-container run: [docker-compose.yml](/Users/sanskar/dev/Research/Projects/InMemoryCache/docker-compose.yml:1)
- Kubernetes manifests: [deploy/kubernetes](/Users/sanskar/dev/Research/Projects/InMemoryCache/deploy/kubernetes)
- Monitoring bundle: [deploy/monitoring](/Users/sanskar/dev/Research/Projects/InMemoryCache/deploy/monitoring)
- Prometheus alert rules: [deploy/monitoring/prometheus-rules.yaml](/Users/sanskar/dev/Research/Projects/InMemoryCache/deploy/monitoring/prometheus-rules.yaml:1)
- Prometheus scrape config: [deploy/monitoring/service-monitor.yaml](/Users/sanskar/dev/Research/Projects/InMemoryCache/deploy/monitoring/service-monitor.yaml:1)
- Grafana dashboard: [deploy/monitoring/cache-engine-dashboard.json](/Users/sanskar/dev/Research/Projects/InMemoryCache/deploy/monitoring/cache-engine-dashboard.json:1)
- Alert routing example: [deploy/monitoring/alertmanager-config.example.yaml](/Users/sanskar/dev/Research/Projects/InMemoryCache/deploy/monitoring/alertmanager-config.example.yaml:1)
- Terraform deployment baseline: [infra/terraform](/Users/sanskar/dev/Research/Projects/InMemoryCache/infra/terraform)
- CI: [.github/workflows/ci.yml](/Users/sanskar/dev/Research/Projects/InMemoryCache/.github/workflows/ci.yml:1)

## Make targets

```bash
make build
make test
make bench
make loadtest
make run
make lint
make dev
make migrate
make backup
```

## Frontend

The frontend is route-split and uses local SVG chart components instead of a heavyweight charting library. The main pages are:

- Dashboard
- Playground
- Visualizer
- Benchmarks

## Load testing

Use the dedicated load-test runner to validate the API, SSE, and benchmark endpoints under sustained traffic:

```bash
export CACHE_ENGINE_API_KEY=...
make loadtest
```

Useful flags:

- `--base-url http://127.0.0.1:8080`
- `--mode api|sse|benchmark|all`
- `--duration 1m`
- `--concurrency 32`
- `--api-pause 5ms`
- `--sse-clients 8`
- `--benchmark-runs 5`

The load test uses `X-Forwarded-For` fan-out so the rate limiter is exercised with realistic request volume instead of tripping on a single client IP.

## Backup and restore

For SQLite backup and restore procedures, use the dedicated runbook at [docs/runbooks/sqlite-backup-restore.md](/Users/sanskar/dev/Research/Projects/InMemoryCache/docs/runbooks/sqlite-backup-restore.md:1).

## Security

Security reporting instructions live in [SECURITY.md](/Users/sanskar/dev/Research/Projects/InMemoryCache/SECURITY.md:1).

## Validation

Backend:

```bash
go test ./...
```

Frontend:

```bash
cd web
npm run lint
npm run test
npm run build
```
