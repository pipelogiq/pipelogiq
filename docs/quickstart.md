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
PIPELOGIQ_VERSION=v0.4.0-preview.1 docker compose -f infra/compose/docker-compose.registry.yml up -d
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

This builds and starts: PostgreSQL, RabbitMQ, Grafana Tempo, Grafana, pipelogiq-app, and pipelogiq-worker.

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

# 2. App — web dashboard + API (runs migrations automatically)
make compose-app-up

# 3. Worker
make compose-worker-up
```

**Stop individual components:**

```bash
make compose-worker-down
make compose-app-down
make compose-infra-down
```

You can also use `docker compose` directly:

```bash
docker compose -f infra/compose/docker-compose.infra.yml up -d
docker compose -f infra/compose/docker-compose.app.yml up --build -d
docker compose -f infra/compose/docker-compose.worker.yml up --build -d
```

---

### Verify health

Once startup completes, check that services are running:

| Check | Command |
|---|---|
| API health (via nginx) | `curl http://localhost:3300/api/healthz` |
| External API health | `curl http://localhost:8081/healthz` |
| API version (via nginx) | `curl http://localhost:3300/api/version` |
| Worker metrics | `curl http://localhost:9090/metrics` |

> **Note:** Port `8080` (internal API) is not exposed from the `pipelogiq-app` container. The internal API is accessible through nginx on port `3300` under the `/api/` prefix. When running without Docker, `localhost:8080` is accessible directly.

Management UIs:

| Service | URL                    | Credentials |
|---|------------------------|---|
| Dashboard | http://localhost:3300  | `ADMIN_EMAIL` from `.env` + the one-time password printed in the API log |
| RabbitMQ Management | http://localhost:15672 | guest / guest |
| Grafana | http://localhost:3100  | admin / admin |

The registry Compose file publishes only `3300` and `8081`, and keeps Tempo and
Grafana behind the `observability` profile. See
[Co-hosted production deployment](#co-hosted-production-deployment) below.

---

## Compose files reference

| File                          | Description |
|-------------------------------|---|
| `docker-compose.build.yml`    | Full stack — builds all images from source |
| `docker-compose.registry.yml` | Full stack — pulls pre-built images from `ghcr.io/pipelogiq` |
| `docker-compose.infra.yml`    | Infrastructure only (Postgres, RabbitMQ, Tempo, Grafana) |
| `docker-compose.app.yml`      | `pipelogiq-app` only (web + API, build from source) |
| `docker-compose.worker.yml`   | `pipelogiq-worker` only (build from source) |

All files use the external `pipelogiq` Docker network. The `docker-compose.registry.yml` file
supports the `PIPELOGIQ_VERSION` environment variable to pin a release tag (defaults to `latest`).

---

## Co-hosted production deployment

`docker-compose.registry.yml` is written for a single VPS that already runs
another Compose project with its own reverse proxy. Both projects join the
external `pipelogiq` network, so the proxy addresses Pipelogiq by container name
and nothing but the proxy is exposed to the internet.

### Ports

Only two ports are published on the host:

| Port | Service | Notes |
|---|---|---|
| `3300` | dashboard (`pipelogiq-app:80`) | Put authentication in front of it. There is no anonymous access, but the login page should not be public either. |
| `8081` | external API (`pipelogiq-app:8081`) | API-key authenticated. This is what SDK clients and HTTP workers call. |

Nothing else is published. Postgres, RabbitMQ (`5672` and the management UI on
`15672`), the internal API (`8080`), and the metrics listeners (`9090`, `9091`)
stay on the `pipelogiq` network. Publishing any of them puts an unauthenticated
administrative surface on a public address. Reach them for debugging through
`docker compose exec` or an SSH tunnel.

### Environment

Create the network once, then set the release, the secret, and the bootstrap
application:

```bash
docker network create pipelogiq 2>/dev/null || true
```

In `.env`:

```bash
PIPELOGIQ_VERSION=v0.5.0-preview.1
JWT_SECRET=<openssl rand -base64 48>
POSTGRES_PASSWORD=<openssl rand -hex 24>
BOOTSTRAP_APPLICATION_NAME=your-app
BOOTSTRAP_API_KEY=<openssl rand -hex 32>
ADMIN_EMAIL=ops@example.com
```

On first start the API creates the application, stores the API key, and logs one
line naming the application and its queue prefix — never the key itself:

```
bootstrap: application ready name=your-app applicationId=1 appId=app_1 createdApplication=true createdApiKey=true
```

`appId` is what an HTTP worker substitutes into `queues.stageNextPattern`; see
[HTTP workers](http-workers.md). Both bootstrap variables are ignored once the
application exists, so rotating or revoking a key in the dashboard survives a
restart.

Start it:

```bash
docker compose -f infra/compose/docker-compose.registry.yml up -d
```

### Tracing

Tempo and Grafana are behind the `observability` profile and do not start by
default. A host that already runs Grafana Alloy or another OTLP collector should
leave the profile off and point the exporter at the existing collector:

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=alloy:4317
```

To run the bundled stack instead:

```bash
docker compose -f infra/compose/docker-compose.registry.yml --profile observability up -d
```

Leaving `OTEL_EXPORTER_OTLP_ENDPOINT` empty disables OTLP export; the services
run normally without it.

### Caddy

In the other project's `Caddyfile`, proxying by container name over the shared
network. Basic auth guards the dashboard; the external API authenticates itself
with `X-API-Key`:

```caddyfile
ops.example.com {
    encode zstd gzip

    basic_auth {
        # caddy hash-password --plaintext '<password>'
        ops $2a$14$replace.with.your.own.bcrypt.hash
    }

    reverse_proxy pipelogiq-app:80
}

pipeline-api.example.com {
    encode zstd gzip

    # The dashboard's WebSocket lives on the internal API and is not exposed
    # here; this host serves the API-key surface only.
    reverse_proxy pipelogiq-app:8081
}
```

The proxy container must be attached to the `pipelogiq` network for those names
to resolve. In the other project's Compose file:

```yaml
services:
  caddy:
    networks:
      - default
      - pipelogiq

networks:
  pipelogiq:
    external: true
```

### Resource limits

Limits depend on the host, so the file ships none. On a shared VPS, add them per
service (Compose v2 non-swarm keys):

```yaml
services:
  pipelogiq-postgres:
    mem_limit: 2g
    cpus: "1.0"
  pipelogiq-rabbitmq:
    mem_limit: 512m
    cpus: "0.5"
  pipelogiq-app:
    mem_limit: 512m
    cpus: "0.5"
  pipelogiq-worker:
    mem_limit: 512m
    cpus: "0.5"
```

All data lives in named volumes (`pgdata`, `rabbitmq-data`, `grafana-data`,
`tempo-data`) and every service restarts `unless-stopped`.

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
# 1. Sign in to the dashboard (http://localhost:3300) and create an application
#    and API key. The administrator is provisioned from ADMIN_EMAIL on first start;
#    its one-time password is printed once in the API log:
#      docker logs pipelogiq-app 2>&1 | grep 'one-time administrator password'

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
