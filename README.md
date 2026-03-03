# Pipelogiq

[![CI](https://github.com/pipelogiq/pipelogiq/actions/workflows/ci.yml/badge.svg)](https://github.com/pipelogiq/pipelogiq/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

> **Preview (v0.x).** Breaking changes may occur before v1.0.

Execution control plane for distributed, event-driven workflows.

## What It Does

- **Pipeline orchestration** — define multi-stage pipelines; stages execute sequentially with automatic state tracking (NotStarted, Pending, Running, Completed, Failed)
- **Stage execution** — pull-based job gateway for external workers; built-in retry with configurable max attempts and intervals
- **Worker management** — bootstrap, heartbeat, event reporting, and graceful shutdown protocol for distributed worker fleets
- **Observability bridge** — OpenTelemetry trace propagation (`traceparent`), Prometheus metrics, integration config for Grafana/Tempo/Sentry/Datadog
- **Real-time dashboard** — React UI with WebSocket updates, pipeline inspection, stage logs, and worker monitoring

## What It Is Not

- **Not APM** — Pipelogiq orchestrates workflows and bridges telemetry; it does not collect or store application metrics/traces itself
- **Not a message broker** — it uses RabbitMQ internally but does not replace your messaging infrastructure
- **Not CI/CD** — pipelines here are runtime workflow executions, not build/deploy pipelines
- **Not low-code** — pipelines are defined via API; there is no visual drag-and-drop builder

## Quick Start

Requires Docker and Docker Compose.

### Option A — pre-built images (fastest)

No local build required. Pulls the latest images from GitHub Container Registry.

```bash
docker network create pipelogiq 2>/dev/null || true
cp .env.example .env
make compose-latest-up
```

Pin a specific release:

```bash
PIPELOGIQ_VERSION=v0.3.0 docker compose -f infra/compose/docker-compose.registry.yml up -d
```

### Option B — build from source

```bash
docker network create pipelogiq 2>/dev/null || true
cp .env.example .env
make compose-up
```

Once running:

| Service | URL |
|---|---|
| Dashboard | http://localhost:3300 |
| External API | http://localhost:8081 |
| RabbitMQ UI | http://localhost:15672 (guest/guest) |
| Grafana | http://localhost:3000 (admin/admin) |
| Worker metrics | http://localhost:9090/metrics |

Health checks: `GET /healthz` and `GET /readyz` on both API ports. When using Docker Compose, reach the internal API via nginx on `:3300` (e.g. `curl http://localhost:3300/api/healthz`) or use the external API directly on `:8081`. Port `:8080` is internal to the `pipelogiq-app` container.

Stop everything:

```bash
make compose-down          # full stack (build)
make compose-latest-down   # pre-built image stack
```

See [docs/quickstart.md](docs/quickstart.md) for local development without Docker and component-level compose usage.

## Architecture

```
                ┌──────────────────────────┐
                │      pipelogiq-app        │
                │  ┌──────────┐            │
                │  │ React UI │ :3300       │
                │  └────┬─────┘            │
                │       │ nginx proxy       │
                │  ┌────▼──────────────┐   │
                │  │  pipelogiq-api    │   │──── PostgreSQL
                │  │  :8080 (int)      │   │──── RabbitMQ
                │  │  :8081 (ext)      │   │
                │  └───────────────────┘   │
                └──────────────────────────┘
                              │
              ┌───────────────┴──────────────┐
              ▼                              ▼
   ┌────────────────────┐        ┌─────────────────┐
   │  pipelogiq-worker  │        │ External Workers │
   │  :9090 (metrics)   │        │  (via SDK/API)   │
   └────────────────────┘        └─────────────────┘
```

- **pipelogiq-app** — single container with the React dashboard (nginx) and the API. nginx proxies `/api/` and `/ws` to the co-located API at `localhost:8080`. Exposes the dashboard on `:3300` and the external API on `:8081`
- **pipelogiq-worker** — polls for ready stages and dispatches them to RabbitMQ queues; processes results and manages pipeline state; Prometheus metrics on `:9090`
- **External workers** — pull jobs from the external API, execute stage logic, and report results back
- **Database migrations** — managed by Liquibase (`database/changelog.xml`); run automatically by `pipelogiq-app` on startup via its entrypoint script

See [docs/architecture.md](docs/architecture.md) for details.

## Repository Structure

```
apps/go/         Go services (API binary, worker binary)
apps/web/        React + Vite dashboard
database/        Liquibase changelog
infra/           Dockerfiles, Docker Compose files, observability config
  compose/         docker-compose.build.yml         — full stack (build from source)
                   docker-compose.registry.yml  — full stack (pre-built GHCR images)
                   docker-compose.infra.yml   — Postgres, RabbitMQ, Tempo, Grafana
                   docker-compose.app.yml     — pipelogiq-app only (build from source)
                   docker-compose.worker.yml  — pipelogiq-worker only (build from source)
  docker/          Dockerfiles and nginx config
docs/            Documentation
```

## Development

```bash
make help              # Show all targets
make test              # Run Go tests
make lint              # go vet + ESLint
make fmt               # gofmt + check formatting
make build             # Build Go binaries
make dev               # Start infra (Postgres, RabbitMQ, Tempo, Grafana)
make run-api           # Run API from source
make run-worker        # Run worker from source
make run-web           # React dev server (:3300)
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full development guide.

## Compatibility

Server and SDKs are versioned independently. The external API (`:8081`) provides the integration contract. See [releases](https://github.com/pipelogiq/pipelogiq/releases) for version history.

## Documentation

- [Quick Start](docs/quickstart.md) — Docker Compose setup, local dev, first pipeline
- [Architecture](docs/architecture.md) — control plane, data plane, workers, execution flow
- [Observability](docs/observability.md) — trace context, metrics, integration config
- [Policies](docs/policies.md) — rate limit, retry, timeout, circuit breaker (experimental)
- [Contributing](CONTRIBUTING.md) — development setup, tests, PR process
- [Security Policy](SECURITY.md) — vulnerability reporting

## Releases

This project uses [Semantic Versioning](https://semver.org/). Preview releases (v0.x) may include breaking changes between minor versions.

See [CHANGELOG.md](CHANGELOG.md) for version history and [RELEASE.md](RELEASE.md) for the release process.

## License

Apache License 2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
