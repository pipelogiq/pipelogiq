# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project follows [Semantic Versioning](https://semver.org/). v0.x releases may include breaking changes.

## [0.3.0-preview.1] - 2026-04-07

Preview release line realigned to `v0.3.0-preview.1` to match the current platform maturity and published deployment/documentation examples.

### Changed

- **Release line renumbering** — the active preview release line now uses `v0.3.0-preview.1`
- **Documentation refresh** — README, quickstart, OpenAPI metadata, compose examples, and release references now consistently point to `v0.3.0-preview.1`

## [0.1.0-preview.3] - 2026-03-19

Third preview release. Policy enforcement is now live at runtime: retry policies with configurable backoff are applied automatically when stages fail, and all policy events are persisted to PostgreSQL.

### Added

- **Policy-based retry enforcement** — retry policies are now evaluated at runtime when a stage fails. `maxAttempts`, `baseDelayMs`, `maxDelayMs`, `backoff` (`fixed`, `linear`, `exponential`), and `jitter` are all applied. Policy retry takes precedence over per-stage `maxRetries`/`retryInterval` options; stage options remain as fallback when no policy matches
- **`retryOn` error code filtering** — retry policies can declare `retryOn.errorCodes` to retry only on specific failure codes (e.g. `RATE_LIMIT_EXCEEDED`, `TIMEOUT`, `UPSTREAM_ERROR`). Stages that do not match any declared code are marked `Failed` immediately without retrying
- **`ErrorCode` on stage results** — workers can now report a structured error code alongside the failure message. The .NET SDK exposes `StageResult.RateLimitExceeded(msg)`, `StageResult.Timeout(msg)`, and `StageResult.UpstreamError(msg)` factory helpers, plus a general `StageResult.Error(msg, errorCode)` overload
- **DB-backed policy runtime** — policies and runtime trigger events are now persisted to PostgreSQL (`policy` and `policy_event` tables). The API server seeds the database from the file store on startup. Both the API and worker now read active policies directly from the database, removing the dependency on a shared policy file at runtime
- **`source` and `origin` policy columns** — new Liquibase migration adds `source` (`system` / `pipeline_inline`) and `origin` (jsonb) columns to the `policy` table
- **Policy DB sync** — create, update, delete, duplicate, promote, enable/disable/pause/resume, and inline import operations now sync to the database asynchronously in addition to writing to the file store

### Fixed

- **Worker policy runtime** — the worker binary was creating a new file-backed policy repository on every `RuntimePolicies()` call (because the repo was `nil`). This made it impossible to use policies in a distributed deployment where the worker has no access to the policy file. Both the API and worker now use the DB-backed runtime
- **Dual repository bug in API** — the API binary previously instantiated two separate `policyRepository` instances (one for the runtime, one for the HTTP server). Changes made through the HTTP handlers were not visible to the runtime. Both now use the same repository instance, plus DB as the durable backend

### Changed

- **Retry delay calculation** — backoff delay is now computed by `computeBackoffDelay()` using the policy rule. The legacy `retryInterval` field on stage options continues to work as a fixed-delay fallback

## [0.1.0-preview.2] - 2026-03-18

Second preview release focused on external stage control, timeout handling, and RabbitMQ/observability hardening.

### Added

- **External stage control API** — `POST /pipelines/{pipelineId}/stages` appends stages to an existing pipeline; `POST /stages/{stageId}/resume` resumes stages waiting for external approval
- **External API docs and tests** — OpenAPI coverage plus endpoint and validation tests for the new stage-control flow
- **Pipeline context search** — pipeline search now matches pipeline context item keys and values
- **Context value viewer** — dashboard support for inspecting larger or structured pipeline context payloads

### Changed

- **Active timeout handling** — timeout enforcement now covers active stages instead of pending-only flows, while keeping compatibility aliases for existing callers
- **RabbitMQ topology management** — standardized `/dev`, `/test`, and `/prod` vhosts; added topology ownership configuration and worker bootstrap metadata; improved mismatch handling
- **Worker dispatch loop** — publishing now supports faster back-to-back stage dispatch when work is available
- **Trace ID normalization** — pipeline trace IDs are derived from incoming `traceparent` headers when present, otherwise generated as valid W3C trace IDs

### Fixed

- **Observability defaults** — corrected Tempo service references, OTLP endpoint defaults, receiver bindings, and moved Grafana to port `3100` to avoid common local conflicts
- **Compose wiring** — corrected the worker's Tempo dependency in the registry compose stack
- **SQLite concurrency** — stage approval resume now retries transient SQLite lock conflicts instead of surfacing deadlock errors
- **Frontend cleanup** — fixed context viewer lint and formatting issues

## [0.1.0-preview.1] - 2026-03-03

First public preview release.

### Added

#### Core pipeline engine
- **Pipeline API** — create multi-stage pipelines; track execution state (`NotStarted` → `Pending` → `Running` → `Completed` / `Failed`)
- **Stage execution** — sequential stage execution with configurable retry (max attempts and interval per stage)
- **Rerun / skip** — rerun a failed stage or skip it to unblock the pipeline without restarting from scratch
- **Pending watchdog** — stages stuck in `Pending` beyond a configurable timeout are automatically marked `Failed`

#### API
- **Internal API** (`:8080`) — JWT/cookie auth for the web dashboard. Endpoints: auth, pipelines, stages, applications, API keys, workers, policies, observability, WebSocket (`/ws`), health (`/healthz`, `/readyz`), version (`/version`)
- **External API** (`:8081`) — API-key auth (`X-API-Key`) for SDK clients and external workers. Pull-based job gateway, log submission, worker lifecycle endpoints, version (`/version`)

#### Workers
- **Worker protocol** — bootstrap (receive session token and queue topology), heartbeat, event reporting, graceful shutdown with session TTL enforcement
- **Built-in worker** (`pipelogiq-worker`) — publisher, result consumer, status consumer, pending watchdog, Prometheus metrics on `:9090`

#### Dashboard
- **React dashboard** — pipeline list, pipeline detail with stage logs and context, worker monitoring, settings, observability config; real-time updates via WebSocket

#### Observability
- **OpenTelemetry trace propagation** — `traceparent` header forwarded through the job gateway to external workers
- **Observability bridge** — integration config for Grafana/Tempo, Sentry, and Datadog; connection testing from the dashboard
- **Prometheus metrics** — counters for stage lifecycle events and external API operations

#### Infrastructure
- **`pipelogiq-app` container** — single image bundling the React dashboard (nginx) and the API server (supervisord). nginx serves the dashboard on `:3300` and proxies `/api/` and `/ws` to the co-located API at `localhost:8080`, eliminating service-name coupling
- **`pipelogiq-worker` container** — separate image for the built-in worker; Prometheus metrics on `:9090`
- **Liquibase auto-migration** — `pipelogiq-app-entrypoint.sh` runs `liquibase update` on startup before starting nginx and the API; controlled by `LIQUIBASE_ENABLED`
- **Docker Compose stack** — `docker-compose.build.yml` (build from source) and `docker-compose.registry.yml` (pre-built GHCR images), plus individual `docker-compose.infra.yml`, `docker-compose.app.yml`, `docker-compose.worker.yml` for component-level control
- **GHCR images** — `ghcr.io/pipelogiq/pipelogiq-app` and `ghcr.io/pipelogiq/pipelogiq-worker`; pinnable via `PIPELOGIQ_VERSION`
- **Grafana Tempo** — pre-configured datasource and trace explorer
- **SQLite fallback** — run Go services locally without Postgres (data stored at `apps/go/data/pipelogiq.db` when `DATABASE_URL` is unset)
- **Version endpoint** — `GET /version` returns build version, commit hash, and build date

#### Other
- **Action policies** (experimental) — CRUD for rate-limit, retry, timeout, and circuit-breaker policies
- **Dead-letter queue** — optional per-queue DLQ in RabbitMQ; configurable TTL (disabled by default)
- **Project documentation** — quickstart, architecture, observability, policy, contributing, and security docs

### Known Limitations

- Stage execution is strictly serial; `depends_on` and `run_in_parallel_with` fields are stored but ignored
- RBAC roles are stored but not checked
- WebSocket endpoint has no authentication
- No published SDK; external workers must implement the HTTP protocol directly

[0.1.0-preview.1]: https://github.com/pipelogiq/pipelogiq/releases/tag/v0.1.0-preview.1
[0.1.0-preview.2]: https://github.com/pipelogiq/pipelogiq/releases/tag/v0.1.0-preview.2
[0.1.0-preview.3]: https://github.com/pipelogiq/pipelogiq/releases/tag/v0.1.0-preview.3
[0.3.0-preview.1]: https://github.com/pipelogiq/pipelogiq/releases/tag/v0.3.0-preview.1
