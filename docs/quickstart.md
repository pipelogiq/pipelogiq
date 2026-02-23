# Quick Start

## Prerequisites

- Docker and Docker Compose (v2+)
- Git

For local development without Docker, you also need:
- Go 1.22+
- Node.js 20+ and npm

## Docker Compose

All compose files live in `infra/compose/` and share the external Docker network `pipelogiq`.

### Create the network (once)

```bash
docker network create pipelogiq 2>/dev/null || true
cp .env.example .env
```

---

### Option A — Pre-built images (recommended for trying out)

Pulls the latest images from GitHub Container Registry. No local build required.

```bash
make compose-latest-up
```

To pull the latest images before starting:

```bash
make compose-latest-pull
make compose-latest-up
```

To pin a specific release version:

```bash
PIPELOGIQ_VERSION=v0.3.0 docker compose -f infra/compose/docker-compose.latest.yml up -d
```

Stop:

```bash
make compose-latest-down
```

---

### Option B — Build from source (full stack)

Builds all images locally and starts the complete stack.

```bash
make compose-up
```

This builds and starts: PostgreSQL, RabbitMQ, Grafana Tempo, Grafana, pipeline-api, pipeline-worker, and the React dashboard.

Stop:

```bash
make compose-down
```

---

### Option C — Component-level compose files

Start components independently. All share the `pipelogiq` network so they can communicate by container name.

**Start in order:**

```bash
# 1. Infrastructure (Postgres, RabbitMQ, Tempo, Grafana)
make compose-infra-up

# 2. API (waits for Postgres and RabbitMQ to be healthy)
make compose-api-up

# 3. Worker (waits for API to be healthy)
make compose-worker-up

# 4. Web dashboard
make compose-web-up
```

**Stop individual components:**

```bash
make compose-web-down
make compose-worker-down
make compose-api-down
make compose-infra-down
```

You can also use `docker compose` directly:

```bash
docker compose -f infra/compose/docker-compose.infra.yml up -d
docker compose -f infra/compose/docker-compose.api.yml up --build -d
docker compose -f infra/compose/docker-compose.worker.yml up --build -d
docker compose -f infra/compose/docker-compose.web.yml up --build -d
```

---

### Verify health

Once startup completes, check that services are running:

| Check | Command |
|---|---|
| API health | `curl http://localhost:8080/healthz` |
| External API health | `curl http://localhost:8081/healthz` |
| API version | `curl http://localhost:8080/version` |
| Worker metrics | `curl http://localhost:9090/metrics` |

Management UIs:

| Service | URL | Credentials |
|---|---|---|
| Dashboard | http://localhost:3300 | admin / admin123 (from `.env`) |
| RabbitMQ Management | http://localhost:15672 | guest / guest |
| Grafana | http://localhost:3000 | admin / admin |

---

## Compose files reference

| File | Description |
|---|---|
| `docker-compose.yml` | Full stack — builds all images from source |
| `docker-compose.latest.yml` | Full stack — pulls pre-built images from `ghcr.io/pipelogiq` |
| `docker-compose.infra.yml` | Infrastructure only (Postgres, RabbitMQ, Tempo, Grafana) |
| `docker-compose.api.yml` | `pipeline-api` only (build from source) |
| `docker-compose.worker.yml` | `pipeline-worker` only (build from source) |
| `docker-compose.web.yml` | React dashboard only (build from source) |

All files use the external `pipelogiq` Docker network. The `docker-compose.latest.yml` file
supports the `PIPELOGIQ_VERSION` environment variable to pin a release tag (defaults to `latest`).

---

## Local Development (without Docker for Go/Web)

Start only the infrastructure:

```bash
make dev
```

This starts Postgres, RabbitMQ, Tempo, and Grafana in detached mode. Then in separate terminals:

```bash
# Terminal 1: API server
export APP_ID=PipelogiqTest
export DATABASE_URL=postgres://pipelogiq:pipelogiq@localhost:5441/pipelogiq?sslmode=disable
export RABBITMQ_URL=amqp://guest:guest@localhost:5672/
make run-api

# Terminal 2: Worker
export APP_ID=PipelogiqTest
export DATABASE_URL=postgres://pipelogiq:pipelogiq@localhost:5441/pipelogiq?sslmode=disable
export RABBITMQ_URL=amqp://guest:guest@localhost:5672/
make run-worker

# Terminal 3: Frontend (dev server with hot-reload at :3300)
cd apps/web && npm install && npm run dev
```

The frontend dev server runs at http://localhost:3300 and proxies `/api` and `/ws` to `localhost:8080`.

### SQLite fallback

If `DATABASE_URL` is not set, the API falls back to a local SQLite database at `apps/go/data/pipelogiq.db`. This is useful for quick experimentation but does not support all features.

---

## Running Your First Pipeline

There is no demo pipeline included yet. To create a pipeline, use the external API:

```bash
# 1. Create an application and API key via the dashboard (http://localhost:3300)
#    or use the default admin credentials from .env

# 2. Create a pipeline via the external API
curl -X POST http://localhost:8081/pipelines \
  -H "Content-Type: application/json" \
  -H "X-API-Key: YOUR_API_KEY" \
  -d '{
    "name": "example-pipeline",
    "stages": [
      {
        "name": "step-1",
        "stageHandlerName": "example-handler"
      }
    ]
  }'
```

See the `examples/` directory (if present) or the [Architecture](architecture.md) doc for details on the worker protocol.

## Useful Make Targets

```bash
make help                # List all targets
make test                # Run Go tests
make lint                # Run go vet + ESLint
make fmt                 # Check Go formatting
make build               # Build Go binaries with version metadata
make compose-latest-up   # Start full stack from GHCR images
make compose-up          # Start full stack built from source
make dev                 # Start infra services only
```
