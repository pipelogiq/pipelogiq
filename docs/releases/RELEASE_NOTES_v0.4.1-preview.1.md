# Pipelogiq v0.4.1-preview.1

**Released:** 2026-09-06
**Pairs with:** `pipelogiq-sdk-net 0.4.0-preview.1` (unchanged)
**Images:** `ghcr.io/pipelogiq/pipelogiq-app:v0.4.1-preview.1`, `ghcr.io/pipelogiq/pipelogiq-worker:v0.4.1-preview.1`

This release lets a worker run stages over HTTP alone, with no broker connection,
and reshapes the Compose deployment for a host that already runs something else.

It is additive. Every existing AMQP worker, SDK client, and pipeline behaves
exactly as before; no schema changes and no configuration is newly required.

---

## Highlights

### HTTP workers

- **`POST /stages/{stageId}/result`.** The missing half of the HTTP job gateway.
  A worker that pulled a job over HTTP can now report its outcome over HTTP too.
  The body is the `stage_result` message an AMQP worker publishes, verbatim, so
  the SDK's `StageResultDto` serialises to it without a new contract type.

  The API validates the caller and the shape, then publishes the message onto
  the `StageResult` queue with persistent delivery and publisher confirms. It
  never writes stage state itself. Retry policies, execution-ID fencing,
  WebSocket updates, and metrics keep working unchanged because the worker
  process still owns the state machine.

  `202` means queued, not applied — a stale, duplicate, or terminal-stage result
  is accepted and dropped by the consumer, exactly as for AMQP workers. That is
  what makes retrying a `503` safe.

- **`POST /jobs/pull` is scoped and can long-poll.** The endpoint handed out raw
  broker messages without checking the API key or the queue. It now
  authenticates and accepts only `app_{applicationId}_{handler}_StageNext` for
  the caller's own application; handler segments containing a path separator,
  whitespace, or a control character are rejected.

  `waitSeconds` (`0..20`, default `0`) waits for a message instead of answering
  `204` immediately, so an HTTP worker no longer has to hot-poll. Clients that
  omit it are unchanged.

- **The in-flight sweeper is on a real schedule.** Messages pulled but never
  acked are requeued at half the visibility timeout rather than a fixed ten
  seconds, and each requeue is logged.

- **[`docs/http-workers.md`](../http-workers.md)** is the full contract:
  bootstrap, exact queue-name construction, the pull/lease/execute/result/ack
  loop, every lease refusal reason, a failure matrix, a sequence diagram, and a
  curl walkthrough. It is written to be sufficient without reading Go.

### Operations

- **Only two ports are published.** `3300` (dashboard) and `8081` (external
  API). Postgres, RabbitMQ and its management UI, the internal API, and the
  metrics listeners stay on the `pipelogiq` network, where a co-hosted reverse
  proxy reaches them by container name. Every service restarts
  `unless-stopped` and keeps its named volume.

- **Tracing is optional.** Tempo and Grafana moved behind
  `--profile observability` and no longer start by default. A host that already
  runs Grafana Alloy or another collector points `OTEL_EXPORTER_OTLP_ENDPOINT`
  at it and starts neither.

- **Bootstrap an application from the environment.** `BOOTSTRAP_APPLICATION_NAME`
  and `BOOTSTRAP_API_KEY` create the application and its key on first start, so
  a fresh host needs no dashboard clicks. Startup logs one line naming the
  application and its queue prefix, never the key.

- **`scripts/http-worker-smoke.sh`** plays the whole protocol with `curl`
  against `make compose-up`, including a retryable failure followed by a
  success, and asserts the pipeline reaches `Completed` with one retry recorded.

---

## New and changed endpoints

| Endpoint | Change |
|---|---|
| `POST /stages/{stageId}/result` | New. `202` on accept; `400`, `401`, `403`, `404`, `503` as problem+json. |
| `POST /jobs/pull` | Now requires a valid API key (`401`) and a queue owned by that application (`403`). Accepts `waitSeconds` (`400` outside `0..20`). |
| `POST /jobs/ack` | Unchanged behaviour, now documented in OpenAPI including when to requeue. |

New metrics: `api_stage_results_posted_total{status_code}` and
`api_stage_result_duration_ms`.

---

## Before you upgrade

### 1. Check whether anything publishes the closed ports

The registry Compose file no longer publishes `5441` (Postgres), `5672` and
`15672` (RabbitMQ), `3200`/`4317`/`4318` (Tempo), `3100` (Grafana), or `9090`
(worker metrics). If a tool outside the `pipelogiq` network depends on one of
them, either attach that tool to the network or re-enable the specific port; the
lines are present and commented in the file.

### 2. Decide about tracing

If you relied on the bundled Tempo and Grafana, add the profile:

```bash
docker compose -f infra/compose/docker-compose.registry.yml --profile observability up -d
```

Otherwise set `OTEL_EXPORTER_OTLP_ENDPOINT` to your own collector, or leave it
empty to disable OTLP export.

### 3. Confirm nothing pulls another application's queue

`POST /jobs/pull` now answers `403` for a queue outside the caller's
application. This was previously accepted. A client that relied on it was
reading another tenant's stage jobs and must be fixed, not worked around.

---

## Upgrade

```bash
docker network create pipelogiq 2>/dev/null || true
PIPELOGIQ_VERSION=v0.4.1-preview.1 docker compose --env-file .env \
  -f infra/compose/docker-compose.registry.yml up -d
```

There are no migrations in this release. Rolling back is a matter of pinning the
previous image tag.

### Smoke test

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8081/healthz   # 200
curl -s http://localhost:3300/api/version
```

Full HTTP worker protocol, including a retry:

```bash
BOOTSTRAP_API_KEY=<your key> scripts/http-worker-smoke.sh
```

---

## Known limitations

Unchanged from `v0.4.0-preview.1` and documented in
[the reliable execution guide](../reliable-execution.md): no exactly-once
external side effects, dispatch is not one atomic transaction with the database,
a lease excludes competing workers only while valid, and inline policy import is
post-create and best-effort.

New to this release:

- **Pull is a poll.** An HTTP worker with a 20-second long poll sees a new job
  within a few hundred milliseconds; an AMQP consumer is pushed it immediately.
  Prefer AMQP when the dependency is acceptable.
- **The .NET SDK has no HTTP transport yet.** A consumer that wants HTTP-only
  operation implements the protocol against `docs/http-workers.md`. The SDK
  packages follow in a later release.
- **`BOOTSTRAP_API_KEY` is stored as written.** API keys are still held in plain
  text, as all keys are today; treat the variable like any other secret.
