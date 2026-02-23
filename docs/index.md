# Pipelogiq Documentation

Pipelogiq is an execution control plane for distributed, event-driven workflows. It provides pipeline orchestration, stage execution with retry, a worker management protocol, and an observability bridge — all accessible through a REST API and a real-time React dashboard.

Pipelogiq is designed for platform engineers, backend developers, and DevOps teams who need to define, run, and monitor multi-stage workflows across distributed services. Workers pull jobs from the API, execute stage logic, and report results back. The control plane handles ordering, retry, failure detection, and status tracking.

## What Pipelogiq Is Not

- **Not APM** — it orchestrates workflows and bridges telemetry to external systems, but does not collect or store application metrics or traces itself
- **Not a message broker** — it uses RabbitMQ internally but does not replace your messaging infrastructure
- **Not CI/CD** — pipelines are runtime workflow executions, not build or deploy pipelines
- **Not low-code** — pipelines are defined via API; there is no visual builder

## Documentation

- [Quick Start](quickstart.md) — get running with Docker Compose
- [Architecture](architecture.md) — system components, data flow, and deployment
- [Observability](observability.md) — tracing, metrics, and integration setup
- [Policies](policies.md) — action policies for rate limiting, retry, timeout, and circuit breaking

## Status

Pipelogiq is in preview (v0.x). The API surface and configuration may change between minor releases. See the [CHANGELOG](../CHANGELOG.md) for version history.
