# Cache Engine Stabilization Todo

This document tracks the bugs and production-readiness gaps identified during the audit and follow-up fix pass. Items are ordered by priority and include root cause, impact, edge cases, and intended fix direction.

## 1. TTL Expiry Rehydrated Expired Data From The Backing Store

- Status: Fixed
- Severity: Critical
- Root cause:
  - TTL metadata existed only in the in-memory cache.
  - Store wrappers persisted raw values only.
  - Cache miss after expiry caused read-through logic to reload stale values from the store with no TTL.
- Impact:
  - TTL contract was broken end-to-end.
  - Expired data could reappear indefinitely.
- Edge cases:
  - Write-back flushing stale entries.
  - Legacy raw values already persisted before the fix.
  - Read-through racing shortly after local expiry.
- Fix:
  - Persist TTL metadata in store values.
  - Reject and delete expired persisted data during reads.
  - Reapply remaining TTL when rehydrating live values.

## 2. SSE Endpoints Were Broken By Middleware

- Status: Fixed
- Severity: Critical
- Root cause:
  - Logging middleware wrapped `http.ResponseWriter` but dropped `http.Flusher`.
- Impact:
  - SSE stats and coherence streams failed at runtime.
- Edge cases:
  - Any future streaming endpoint would have the same problem.
- Fix:
  - Preserve `Flusher`, `Hijacker`, and `Unwrap` on the wrapper.
  - Keep regression tests around middleware behavior.

## 3. API Was Unauthenticated And Used Wildcard CORS

- Status: Fixed
- Severity: Critical
- Root cause:
  - CORS used `*`.
  - There was no auth or authorization layer.
- Impact:
  - Any browser origin could call all read/write endpoints.
  - Unsafe for public deployment.
- Edge cases:
  - Reverse-proxied exposure.
  - Dev tooling hiding insecure defaults.
- Fix:
  - Replace wildcard CORS with allowlist behavior.
  - Add optional API-key enforcement.
  - Thread client support through the frontend.

## 4. Write Policy Reconfiguration Was Advertised But Not Implemented

- Status: Fixed
- Severity: High
- Root cause:
  - `ConfigRequest` exposed `writePolicy`.
  - Runtime only created write-through stores.
  - Config handler ignored `writePolicy`.
- Impact:
  - API contract mismatch.
  - Write-back and write-around code was unreachable in the live system.
- Edge cases:
  - Switching away from write-back with dirty data.
  - Rehydrating remaining TTL during store rebuilds.
  - Unsupported stores such as the sharded cache.
- Fix:
  - Rebuild stores safely on supported policy changes.
  - Flush before switching away from write-back.
  - Surface supported policy changes in the UI.

## 5. Cache Implementations Exposed Mutable Internal Buffers

- Status: Fixed
- Severity: High
- Root cause:
  - `Set` stored caller-owned slices directly.
  - `Get`, `Peek`, `Snapshot`, and eviction callbacks returned internal slices directly.
- Impact:
  - Callers could mutate cache state without synchronization.
  - Snapshots were not isolated.
- Edge cases:
  - Caller mutates a slice after `Set`.
  - Consumer mutates a slice returned by `Get` or `Peek`.
  - Wrapper callbacks accidentally mutate internal memory.
- Fix:
  - Copy slices on ingress and on all outward-facing returns.
  - Add regression coverage around mutation attempts.

## 6. Coherence Failed Silently For Unknown Nodes

- Status: Fixed
- Severity: Medium
- Root cause:
  - Coordinator returned `nil` for unknown node IDs.
- Impact:
  - API callers got misleading success behavior.
- Edge cases:
  - Typos and stale UI state.
- Fix:
  - Return explicit errors and surface them in HTTP responses.

## 7. Frontend Was Not Buildable Or Lint-Clean

- Status: Fixed
- Severity: Medium
- Root cause:
  - Incompatible `ky` config.
  - Recharts typing issues.
  - React lint violations around effects and purity.
- Impact:
  - Frontend could not be treated as releasable.
- Fix:
  - Correct API client setup.
  - Move SSE-driven updates into callback flows.
  - Clean up effect logic and typing.

## 8. Frontend Bundle Size Was Excessive

- Status: Fixed
- Severity: Medium
- Root cause:
  - All route pages were eagerly imported into the main bundle.
  - Benchmark and dashboard charts depended on a large general-purpose charting library for relatively simple visualizations.
- Impact:
  - Slow initial load.
  - Poor caching granularity.
- Edge cases:
  - Users visiting only one page still download all heavy routes.
  - Small chart regressions should not justify a large third-party dependency in the critical path.
- Fix:
  - Add route-level lazy loading with `Suspense`.
  - Replace heavyweight charting dependencies with local SVG components sized to the actual dashboard and benchmark use cases.

## 9. Benchmark Polling Cleanup And Resilience Need Hardening

- Status: Fixed
- Severity: Low
- Root cause:
  - Polling is functional but not especially defensive around navigation and transient failures.
- Impact:
  - Possible stale state or orphaned polling under interrupted runs.
- Fix:
  - Add cleanup and failure handling around active polling intervals.

## 10. No Frontend Automated Test Suite

- Status: Fixed
- Severity: Medium
- Root cause:
  - The frontend had linting and build checks only.
  - There was no automated regression signal for shared UI state behavior.
- Impact:
  - Refactors could silently break frontend logic with no fast feedback loop.
- Edge cases:
  - Store update merging.
  - Benchmark state transitions.
- Fix:
  - Add a lightweight Vitest-based frontend test suite.
  - Cover shared Zustand state updates as a baseline regression layer.

## 11. API Had No Rate Limiting

- Status: Fixed
- Severity: Medium
- Root cause:
  - The API accepted unbounded request rates from a single client.
- Impact:
  - Easy abuse path for accidental or hostile traffic spikes.
  - Benchmark and SSE-adjacent endpoints could amplify load.
- Edge cases:
  - Reverse-proxied deployments using `X-Forwarded-For`.
  - Probe endpoints needing to remain accessible.
- Fix:
  - Add per-client fixed-window rate limiting with `Retry-After`.
  - Exempt health/readiness and preflight traffic.

## 12. Benchmark Results Were Ephemeral

- Status: Fixed
- Severity: Medium
- Root cause:
  - Benchmark jobs lived only in memory inside the handler.
- Impact:
  - Restarting the server dropped historical benchmark results.
  - Operational debugging and result comparison were fragile.
- Edge cases:
  - First boot with no persisted file.
  - Corrupt or partially written result files.
  - Active benchmark jobs persisting completion state.
- Fix:
  - Persist benchmark job state to disk with atomic file replacement.
  - Reload prior jobs on startup.

## 13. No Dedicated Health Or Readiness Probes

- Status: Fixed
- Severity: Low
- Root cause:
  - The server exposed application APIs only.
- Impact:
  - Deployment automation had no clean probe contract.
  - Liveness checks had to abuse business endpoints.
- Edge cases:
  - Probe endpoints should stay outside API-key protection.
  - Readiness should fail if stores or persistence are not initialized.
- Fix:
  - Add unauthenticated `/healthz` and `/readyz` endpoints.
  - Validate store/coordinator/persistence readiness separately from liveness.

## 14. Frontend Toolchain Contained Known Advisories

- Status: Fixed
- Severity: Low
- Root cause:
  - Transitive dependencies included vulnerable versions of `postcss` and `brace-expansion`.
- Impact:
  - Elevated supply-chain risk during local and CI builds.
- Fix:
  - Run `npm audit fix` and revalidate lint, build, and tests.

## 15. Authenticated SSE Was Broken After API-Key Hardening

- Status: Fixed
- Severity: High
- Root cause:
  - REST requests used `X-API-Key`.
  - `EventSource` does not send arbitrary headers, so SSE streams could not authenticate when API-key protection was enabled.
- Impact:
  - The dashboard and playground lost live stats in secured deployments.
- Edge cases:
  - SSE should stay functional without weakening the rest of the API surface.
- Fix:
  - Mint short-lived signed SSE access tokens from an authenticated API route.
  - Use `access_token` query auth for SSE only, instead of exposing the raw API key in stream URLs.

## 16. Invalid API Inputs Were Silently Coerced Or Loosely Parsed

- Status: Fixed
- Severity: High
- Root cause:
  - JSON decoders accepted unknown fields.
  - Negative TTLs were interpreted as immortal entries by lower layers.
  - Benchmark and config endpoints did not reject impossible numeric values or unknown workloads.
- Impact:
  - Bad requests could mutate the system in surprising ways.
  - Client bugs were harder to detect and debug.
- Edge cases:
  - Oversized request bodies.
  - Multiple JSON documents in one body.
  - Unknown enum-like values.
- Fix:
  - Add strict JSON decoding with body size limits and unknown-field rejection.
  - Reject negative TTLs, invalid latency values, and unsupported benchmark workloads.

## 17. Store Reconfiguration Shared Mutable Runtime State Unsafely

- Status: Fixed
- Severity: High
- Root cause:
  - Request handlers and SSE readers accessed `StoreEntry.Cache` and `WritePolicy` concurrently while config changes could swap and close the active cache.
- Impact:
  - Reconfiguration could race with live reads, writes, stats snapshots, or SSE pushes.
- Edge cases:
  - Closing a write-back wrapper while another request still holds a pointer to it.
  - Stats readers observing partially updated runtime metadata.
- Fix:
  - Add per-store locking around runtime cache access and write-policy swaps.

## 18. Benchmark Latency Sampling Grew Without Bound

- Status: Fixed
- Severity: Medium
- Root cause:
  - Every benchmark worker appended every latency sample for the full run duration.
- Impact:
  - Long or high-throughput benchmark runs could consume excessive memory.
- Edge cases:
  - 60-second runs with many goroutines and millions of operations.
- Fix:
  - Switch percentile estimation to bounded reservoir samples.
  - Keep average latency exact via running totals.

## 19. Write-Around Store Operation Metrics Counted Failed Writes

- Status: Fixed
- Severity: Low
- Root cause:
  - `WriteStoreOps` was incremented before the backing-store write succeeded.
- Impact:
  - Observability around backing-store write success was inaccurate.
- Fix:
  - Increment the counter only after a successful store write.

## 20. Default Backing Store Was Not Durable

- Status: Fixed
- Severity: High
- Root cause:
  - The application used an in-memory backing store with simulated latency as the default runtime store.
- Impact:
  - All persisted cache state disappeared on process restart.
  - The write-policy demonstrations were not production-grade because the underlying persistence layer was ephemeral.
- Edge cases:
  - Restart after write-through writes.
  - Read-through after process replacement.
  - Runtime latency controls still needed to work with the durable store.
- Fix:
  - Replace the default backing store with an embedded SQLite-backed store.
  - Keep a configurable in-memory fallback only when explicitly requested.

## 21. Rate Limiting Was Single-Process Only

- Status: Fixed
- Severity: Medium
- Root cause:
  - Initial rate limiting used in-memory buckets inside one process.
- Impact:
  - Restarting the process reset client limits.
  - Multiple processes would not share request counts.
- Edge cases:
  - Rolling restart during an active abuse window.
  - Shared state needed to survive process replacement.
- Fix:
  - Move rate-limit state into SQLite with atomic upserts by client and window.
  - Reuse the same durable state database across restarts.

## 22. Auth Was Not Secure By Default

- Status: Fixed
- Severity: Critical
- Root cause:
  - API-key auth was optional at startup.
  - An unset `CACHE_ENGINE_API_KEY` implicitly created an open API surface.
- Impact:
  - A production deployment could accidentally boot without authentication.
- Edge cases:
  - Browser clients protected only by CORS.
  - Operators assuming auth was enabled because support existed in code.
- Fix:
  - Fail startup unless `CACHE_ENGINE_API_KEY` is configured.
  - Allow unauthenticated mode only with explicit `CACHE_ENGINE_ENV=development` and `CACHE_ENGINE_ALLOW_INSECURE_NO_AUTH=true`.

## 23. Server Booted In Demo Mode

- Status: Fixed
- Severity: High
- Root cause:
  - Startup always seeded fake cache data.
- Impact:
  - Fresh deployments started with non-production state.
  - Readiness and metrics were polluted by demo traffic.
- Edge cases:
  - Operators restoring from backup expecting an empty cache.
  - Performance baselines skewed by warm demo entries.
- Fix:
  - Disable seeding by default.
  - Re-enable it only through `CACHE_ENGINE_SEED_DEMO_DATA=true`.

## 24. Process Shutdown Was Abrupt

- Status: Fixed
- Severity: High
- Root cause:
  - The HTTP server ran through `ListenAndServe()` without signal handling or coordinated application shutdown.
- Impact:
  - In-flight requests could be cut off.
  - Dirty write-back entries and benchmark metadata were at risk during termination.
- Edge cases:
  - SIGTERM during benchmark execution.
  - SIGTERM while write-back keys were still dirty.
- Fix:
  - Add signal-driven graceful shutdown with timeout.
  - Drain HTTP first, then flush/persist caches and close shared resources.

## 25. Operational Observability Was Incomplete

- Status: Fixed
- Severity: High
- Root cause:
  - The service had health/readiness only.
  - There was no metrics endpoint, no consistent structured logging, and no request tracing contract.
- Impact:
  - Production debugging and alerting were weak.
  - Incidents had limited correlation context across requests and logs.
- Edge cases:
  - Authentication spikes.
  - Elevated rate limiting.
  - Long-tail request latency without route-level visibility.
- Fix:
  - Add authenticated Prometheus metrics.
  - Centralize logs through structured `slog`.
  - Propagate/generate `Traceparent` and `X-Trace-Id` per request.
  - Add Prometheus alert rules for readiness, auth failures, rate limiting, and memory pressure.

## 26. Deployment Scaffolding Was Missing

- Status: Fixed
- Severity: Medium
- Root cause:
  - The repo contained runnable source but no standard production packaging or deployment assets.
- Impact:
  - There was no canonical container build, CI workflow, Kubernetes baseline, or IaC entrypoint.
- Edge cases:
  - Different environments inventing different runbooks.
  - Drift between local and deployed execution.
- Fix:
  - Add backend and frontend Dockerfiles.
  - Add Compose, GitHub Actions CI, Kubernetes manifests, and Terraform deployment scaffolding.

## 27. SQLite Had No Backup Or Migration Story

- Status: Fixed
- Severity: Medium
- Root cause:
  - The durable state layer auto-created tables but exposed no formal migration or backup workflow.
- Impact:
  - Operators had no supported way to snapshot or evolve the database.
- Edge cases:
  - Restoring a production state file.
  - Future schema changes needing explicit version tracking.
- Fix:
  - Add schema version tracking.
  - Add admin commands for `migrate` and `backup`.
  - Document backup expectations and deployment procedures.

## 28. Local Kubernetes Validation Path Was Not Reproducible

- Status: Fixed
- Severity: Medium
- Root cause:
  - The repo had Kubernetes manifests, but no committed overlay or documented path for loading local images into a disposable cluster.
  - A naive nested kustomize overlay also failed because `kubectl apply -k` hit ancestor-cycle and load-restriction edge cases.
- Impact:
  - Cluster validation depended on ad hoc shell commands.
  - Future deployment verification would have been easy to break or skip.
- Edge cases:
  - Engineers trying to validate image changes against `kind`.
  - CI or local scripts invoking `kubectl apply -k` directly against the overlay.
- Fix:
  - Add a committed `deploy/kubernetes/overlays/kind` overlay.
  - Document the required `kubectl kustomize --load-restrictor=LoadRestrictionsNone ... | kubectl apply -f -` flow.
  - Align the overlay with local image tags, local browser origins, and a local-only API key.

## 29. Frontend Reverse Proxy Was Docker-Specific

- Status: Fixed
- Severity: High
- Root cause:
  - The nginx config hardcoded Docker DNS behavior and a bare backend service hostname.
  - That worked in Compose after lazy resolution changes, but failed in Kubernetes where the resolver and search-domain behavior differed.
- Impact:
  - The frontend served HTML but returned `502 Bad Gateway` for proxied `/api` and `/metrics` calls in a real cluster.
  - The deployment looked healthy at the pod level while user-facing functionality was broken.
- Edge cases:
  - Nginx resolving service names across runtimes with different DNS resolvers.
  - Pod restarts where service lookup happens after the backend address changes.
- Fix:
  - Add a startup hook that derives DNS resolvers from `/etc/resolv.conf`.
  - Parameterize the upstream host through `CACHE_ENGINE_UPSTREAM_HOST`.
  - Keep Docker on the short service name and set Kubernetes to the fully-qualified service DNS name.

## 30. Terraform PVC And Deployment Ordering Deadlocked

- Status: Fixed
- Severity: High
- Root cause:
  - The Terraform PVC resource waited for binding while the cluster storage class used `WaitForFirstConsumer`.
  - The backend deployment, which would have become the first consumer, was not created until after the PVC resource completed.
- Impact:
  - `terraform apply` stalled indefinitely on the state PVC in a real cluster.
  - The IaC path could not produce a functioning backend deployment.
- Edge cases:
  - Local-path or cloud storage classes that defer volume binding until scheduling.
  - Partial applies that leave namespace resources created but never reach the backend rollout.
- Fix:
  - Set `wait_until_bound = false` on the Terraform PVC resource.
  - Revalidate the full apply path in a live cluster until both backend and frontend rolled out successfully.

## 31. Monitoring Scrape Auth Was In The Wrong Namespace

- Status: Fixed
- Severity: Medium
- Root cause:
  - The original `ServiceMonitor` and scrape secret lived in the application namespace.
  - Under a real Prometheus Operator stack, the monitoring resources needed to live with the operator-facing auth material in the `monitoring` namespace while still targeting the app service in `cache-engine`.
- Impact:
  - The Prometheus rules loaded, but the cache-engine scrape job was absent from the rendered Prometheus config.
  - Metrics and alerts would never have fired even though the YAML applied cleanly.
- Edge cases:
  - Authenticated scrapes against services in other namespaces.
  - Teams installing kube-prometheus-stack with standard monitoring-namespace conventions.
- Fix:
  - Move the `ServiceMonitor` and `PrometheusRule` manifests to the `monitoring` namespace.
  - Add a dedicated monitoring-side API key secret example.
  - Keep the scrape target pointed at the `cache-engine` service namespace.
