# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Development Commands

All commands run from the repo root via `make`:

```bash
make build          # Compile Go binaries with version ldflags to apps/go/bin/
make run-api        # Run API from source (apps/go)
make run-worker     # Run worker from source (apps/go)
make run            # Build then run both binaries concurrently
make test           # Go tests: cd apps/go && go test ./...
make fmt            # Check Go formatting (exits non-zero if changes needed)
make lint           # go vet + ESLint
make tidy           # Sync go.mod/go.sum
make run-web        # React dev server (apps/web, port 3300)
make dev            # Start infra only (Postgres, RabbitMQ, Tempo, Grafana)
make compose-up     # Full stack via Docker Compose (--build)
make compose-down   # Stop Docker stack
make migrate-up     # Run Liquibase migrations manually
```

Frontend (from `apps/web`): `npm run dev`, `npm run build`, `npm run lint`.

Run a single Go test: `cd apps/go && go test ./internal/observability/repo/ -run TestName`

Docker Compose requires a pre-existing network: `docker network create pipelogiq 2>/dev/null || true`

## Architecture

**Pipelogiq** is a pipeline orchestration platform. The repo is a monorepo with no monorepo tooling — the Go module and web app are independent.

### Go Backend (`apps/go`)

Go 1.22 module named `pipelogiq` with two binaries:

- **pipeline-api** (`cmd/pipeline-api`) — Two HTTP servers:
  - Internal API (`:8080`): JWT/cookie auth, serves the web dashboard. Routes: auth, pipelines, stages, applications, API keys, workers, policies, observability, WebSocket at `/ws`, version at `/version`.
  - External API (`:8081`): API-key auth (`X-API-Key` header), for SDK clients and external workers. Routes: pipeline creation, job pull/ack, log submission, worker lifecycle, version at `/version`.
- **pipeline-worker** (`cmd/pipeline-worker`) — Processes stage jobs from RabbitMQ queues, exposes Prometheus metrics on `:9090`.

Key packages under `internal/`:
- `api/` — HTTP handlers, auth middleware, WebSocket hub
- `config/` — Config from env vars
- `db/` — Postgres (production) or SQLite (local dev fallback) with retry
- `mq/` — RabbitMQ client with OpenTelemetry tracing
- `store/` — Data access layer (sqlx-based)
- `worker/` — Stage orchestration, Prometheus metrics
- `observability/` — Sub-system with own http/repo/service layers
- `types/` — Shared domain types
- `telemetry/` — OpenTelemetry OTLP setup
- `version/` — Build version info (set via ldflags)

MQ channels defined in `internal/constants/channels.go`: StageResult, StageNext, StageStop, StageUpdated, StopPipeline, StageSetStatus.

### Frontend (`apps/web`)

React 19 + TypeScript + Vite 7 + TanStack Query 5 + Tailwind CSS + Radix UI/shadcn patterns.

Vite dev proxy: `/api` → `localhost:8080` (path rewrite strips `/api`), `/ws` → `ws://localhost:8080`.

Path alias: `@/` maps to `apps/web/src/`.

### Database

PostgreSQL 16 in production; SQLite fallback for local dev (at `apps/go/data/pipelogiq.db` when `DATABASE_URL` is unset). Schema managed by Liquibase (`database/changelog.xml`). Migrations run automatically in the API Docker container on startup.

### Infrastructure (`infra/`)

Docker Compose stack: Postgres (:5441), RabbitMQ (:5672/:15672), Grafana Tempo (:4317/:3200), Grafana (:3000), pipeline-api, pipeline-worker.

## CI

GitHub Actions CI (`.github/workflows/ci.yml`) runs on PRs and pushes to main:
- Go: formatting check, `go vet`, `go test`
- Web: `npm ci`, lint, build
- Docker: build both Dockerfiles

Release workflow (`.github/workflows/release.yml`) triggers on `v*` tags: builds cross-platform binaries and creates a GitHub Release with changelog notes.

## Coding Conventions

- **Go**: `gofmt` enforced. Structured logging via `slog`: `logg.Info("msg", "key", value)`. Explicit error returns, no panics in business logic. Context propagation on all DB/MQ calls.
- **TypeScript/React**: ESLint configured. Components PascalCase, hooks `use-kebab-case.ts` with `useX` names. API calls via TanStack Query.
- **Commits**: Conventional Commits style (`feat:`, `fix:`, `docs:`, `chore:`). Subject under 72 chars.

## Environment Setup

Copy `.env.example` to `.env` for Docker Compose. For local Go dev (outside Docker), minimum env vars:

```bash
export APP_ID=PipelogiqTest
export DATABASE_URL=postgres://pipelogiq:pipelogiq@localhost:5441/pipelogiq?sslmode=disable
export RABBITMQ_URL=amqp://guest:guest@localhost:5672/
```
