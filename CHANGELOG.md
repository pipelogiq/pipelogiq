# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project follows [Semantic Versioning](https://semver.org/). v0.x releases may include breaking changes.

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

- Policy enforcement is not implemented at runtime; policies are stored but not applied
- Stage execution is strictly serial; `depends_on` and `run_in_parallel_with` fields are stored but ignored
- Per-stage timeout is stored but not enforced
- RBAC roles are stored but not checked
- WebSocket endpoint has no authentication
- No published SDK; external workers must implement the HTTP protocol directly

[0.1.0-preview.1]: https://github.com/pipelogiq/pipelogiq/releases/tag/v0.1.0-preview.1
