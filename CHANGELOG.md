# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project follows [Semantic Versioning](https://semver.org/). v0.x releases may include breaking changes.

## [0.1.0] - 2026-02-19

First public preview release.

### Added

- **Pipeline API** — create multi-stage pipelines, track execution state (NotStarted → Pending → Running → Completed/Failed)
- **Stage execution engine** — sequential stage execution with automatic retry (configurable max attempts and interval)
- **Rerun / skip** — rerun failed stages or skip them to unblock the pipeline
- **Pending watchdog** — stages stuck in Pending beyond a configurable timeout are marked Failed
- **Internal API** (`:8080`) — JWT/cookie auth for the web dashboard; endpoints for pipelines, stages, applications, API keys, workers, policies, observability
- **External API** (`:8081`) — API-key auth for SDK clients and external workers; pull-based job gateway, log submission, worker lifecycle
- **Worker protocol** — bootstrap, heartbeat, event reporting, and graceful shutdown with session TTL enforcement
- **WebSocket** — real-time pipeline/stage status broadcasts to dashboard clients
- **React dashboard** — pipeline list, pipeline detail with stage logs, worker monitoring, settings, observability config
- **Observability bridge** — OpenTelemetry trace propagation, integration config for Grafana/Tempo/Sentry/Datadog, connection testing
- **Action policies** (experimental) — CRUD for rate limit, retry, timeout, circuit breaker policies (not yet enforced at runtime)
- **Prometheus metrics** — counters for stage lifecycle events and external API operations
- **Dead-letter queue** — configurable DLQ per RabbitMQ queue (disabled by default)
- **Docker Compose stack** — Postgres, RabbitMQ, Grafana Tempo, Grafana, API, and Worker with Liquibase auto-migration
- **SQLite fallback** — run Go services locally without Postgres
- **Version endpoint** — `GET /version` returns build version, commit, and date
- **Project documentation** — quickstart, architecture, observability, and policy docs

### Known Limitations

- Policies are stored in a JSON file, not in the database; enforcement is not implemented
- Stage execution is strictly serial; `depends_on` and `run_in_parallel_with` options are stored but ignored
- Per-stage timeout is stored but not enforced
- RBAC roles are stored but not checked
- WebSocket endpoint has no authentication
- No published SDK; workers must implement the HTTP protocol directly

[0.1.0]: https://github.com/pipelogiq/pipelogiq/releases/tag/v0.1.0
