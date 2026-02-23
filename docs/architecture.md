# Architecture

## Overview

Pipelogiq has two planes:

- **Control plane** — the API server and built-in worker that manage pipeline state, stage execution ordering, and real-time updates
- **Data plane** — external workers (connected via SDK or raw HTTP) that pull stage jobs, execute domain logic, and report results

```
                ┌─────────────┐
                │  React UI   │ :3300 (dev)
                └──────┬──────┘
                       │ /api, /ws
                       ▼
              ┌────────────────┐
              │  pipeline-api  │
              │  :8080 (int)   │──── PostgreSQL
              │  :8081 (ext)   │──── RabbitMQ
              └───────┬────────┘
                      │
         ┌────────────┼────────────┐
         ▼                         ▼
  ┌──────────────┐       ┌─────────────────┐
  │pipeline-worker│       │ External Workers │
  │  (built-in)  │       │   (via SDK/API)  │
  └──────────────┘       └─────────────────┘
```

## Components

### pipeline-api

The API server exposes two HTTP servers on separate ports:

**Internal API (`:8080`)** — serves the React dashboard and admin operations. Authentication is JWT-based (HS256 token in an HttpOnly cookie). Endpoints include:

- Auth (login, logout, current user)
- Pipelines (CRUD, stages, context, logs, rerun, skip)
- Applications and API keys
- Workers and worker events
- Observability config, traces, insights
- Action policies
- WebSocket (`/ws`) for real-time pipeline updates
- Health (`/healthz`, `/readyz`), metrics (`/metrics`), version (`/version`)

**External API (`:8081`)** — serves SDK clients and external workers. Authentication is API-key based (`X-API-Key` header). Endpoints include:

- `POST /pipelines` — create a pipeline
- `POST /jobs/pull` — pull the next stage job for a handler
- `POST /jobs/ack` — acknowledge or reject a stage job
- `POST /logs` — submit application logs
- `POST /workers/bootstrap` — register a worker and receive a session token
- `POST /workers/heartbeat` — report worker health and metrics
- `POST /workers/events` — submit worker events
- `POST /workers/shutdown` — graceful shutdown notification

### pipeline-worker

The built-in worker runs inside the control plane and handles:

- **Publisher** — polls the database for stages ready to execute and publishes them to RabbitMQ queues
- **Result consumer** — processes stage results from workers, updates pipeline state, and triggers the next stage
- **Status consumer** — handles out-of-band stage status updates
- **Pending watchdog** — marks stages that have been in Pending state too long as Failed
- **Prometheus metrics** — exposes counters on `:9090`

### External workers

External workers connect to the external API (`:8081`) to:

1. **Bootstrap** — register with the control plane, receive a session token and queue topology
2. **Pull jobs** — long-poll for the next stage job matching their handler name
3. **Execute** — run domain logic for the stage
4. **Ack/Nack** — report success or failure with optional result data, logs, and context item updates
5. **Heartbeat** — periodically report health metrics (CPU, memory, queue lag, in-flight count)
6. **Shutdown** — notify the control plane before stopping

### Database

PostgreSQL is the primary datastore. Schema is managed by Liquibase (`database/changelog.xml`) and auto-migrated on API startup.

For local development, SQLite is available as a fallback when `DATABASE_URL` is not set.

### Message broker

RabbitMQ handles stage job dispatch and result collection. Key exchanges and queues:

- Per-handler queues: `{APP_ID}.stage.{handlerName}` — stage jobs routed by handler
- `StageResult` — worker results
- `StageUpdated.fanout` — broadcasts stage updates to WebSocket clients
- Dead-letter queues (optional, disabled by default) — captures failed messages

### React dashboard

Single-page app built with React 19, TypeScript, Vite, TanStack Query, Tailwind CSS, and Radix UI. Communicates with the internal API and receives real-time updates via WebSocket.

## Stage Execution Flow

1. A pipeline is created via `POST /pipelines` (external API)
2. The publisher finds the first ready stage and marks it `Pending`
3. The stage job is published to the handler's RabbitMQ queue
4. A worker pulls the job, executes it, and acks with a result
5. The result consumer updates the stage status (`Completed` or `Failed`)
6. If completed, the publisher picks the next stage; if failed and retries remain, the stage is rescheduled
7. When all stages complete (or a stage fails with no retries), the pipeline is marked complete

## Deployment

The provided Docker Compose file (`infra/compose/docker-compose.yml`) runs the full stack. For production, the API and worker binaries can be deployed independently — they only need access to PostgreSQL and RabbitMQ.

Build metadata (version, commit, date) is injected at build time via `ldflags`. Use `make build` to produce binaries with version info, or `curl localhost:8080/version` to check a running instance.
