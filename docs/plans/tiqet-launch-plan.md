# Pipelogiq changes for the tiqet.app launch

Created: 2026-09-06. Baseline: server `v0.4.0-preview.1`, SDK `0.4.0-preview.1`.

This plan describes the Pipelogiq work that unblocks tiqet.app
(`/Users/sergeifedulov/Documents/Dev/tiqet`) from running its order workflows on
Pipelogiq. It is self-contained: every requirement here is derived from the
current code, not from the tiqet plan, and can be implemented and released
without touching tiqet.

## Why tiqet cannot use Pipelogiq today

tiqet is a .NET 10 Minimal API whose repository rules forbid RabbitMQ in the
application (`AGENTS.md`: "Do not add RabbitMQ to this project"). The current
worker path is RabbitMQ-only:

- `PipelogiqSDK` `PipelineRunner` consumes `{appId}.stage.{handler}` queues over
  AMQP (`src/Pipelogiq.Sdk/Runner/PipelineRunner.cs`, `IConnection`) and
  publishes results to the `StageResult` queue with `BasicPublish`.
- The external API has an HTTP job gateway (`POST /jobs/pull`, `POST /jobs/ack`
  in `apps/go/internal/api/external.go`) that pulls raw AMQP messages, but
  there is **no HTTP endpoint that accepts a `StageResultMessage`**. The only
  consumer of results is `runStageResultConsumer` in
  `apps/go/internal/worker/worker.go`, fed by the `StageResult` queue.
- `POST /jobs/pull` does not verify that the requested queue belongs to the
  caller's application, and it returns `204` immediately on an empty queue, so a
  pure HTTP worker has to hot-poll.

Everything else tiqet needs already exists: idempotent creation
(`POST /pipelines/idempotent`), status by ID (`GET /pipelines/{id}`), leases
(`/stages/{id}/lease/acquire|renew`), classified retry with exponential backoff
(`StageOptions.retryOnErrorCodes`, `backoff`, `maxRetryInterval`, `jitter`),
execution-ID fencing in `Store.UpdateStageResult`, and cancellation.

## Explicit non-goals

- No new pipeline semantics (no "skip remaining stages" result flag). tiqet
  handles early completion by writing a context item and having later stages
  return `Success` as no-ops.
- No AI agent, RAG, or Telegram changes beyond what P7 gates behind a license.
- No breaking changes to existing AMQP workers. Everything below is additive.
- No usage metering, no per-seat licensing, no phone-home license checks.
- The core stays Apache-2.0. P7 adds a commercial tier next to it; it never
  relicenses existing code.

Two releases come out of this plan:

- `v0.4.1-preview.1` — P1, P2, P3, P5 (HTTP workers and co-hosted operations).
  tiqet's payment wait is a retrying stage, so tiqet needs nothing else to
  launch.
- `v0.5.0-preview.1` — P6 (Schedules) and P7 (license and Pro boundary). tiqet
  adopts P6 in its slice T8.

## Work items

Implement in order. Each item is one commit (Conventional Commits) with tests.

### P1. `POST /stages/{stageId}/result` on the external API

Purpose: let an HTTP worker report a stage result through the same path AMQP
workers use, so retry policies, fencing, WebSocket updates, and metrics keep
working unchanged.

Contract:

```http
POST /stages/{stageId}/result
X-API-Key: <application API key>
X-Worker-Session-Token: <token from /workers/bootstrap>
Content-Type: application/json

{
  "pipelineId": 273,
  "stageId": 81,
  "executionId": "…",          // required
  "attempt": 2,                // required, > 0
  "isSuccess": false,
  "result": "Stripe reports the checkout is still open.",
  "errorCode": "PAYMENT_PENDING",
  "retryable": true,
  "logs": [ { "level": "Info", "message": "…" } ],
  "contextItems": [ { "key": "paymentOutcome", "value": "\"Pending\"", "valueType": "System.String" } ],
  "appendedStages": []
}
```

The body is `types.StageResultMessage` verbatim (`apps/go/internal/types/messages.go`),
so the SDK's `StageResultDto` serialises to it without a new contract type.

Server behaviour (`apps/go/internal/api/external_reliability.go`, next to the
lease handlers):

1. Authenticate with the API key (existing `extractAPIKey` +
   `store.ValidateAPIKey`) and require the worker session token exactly like
   `handleStageLease` does (`extractWorkerSessionToken`).
2. Parse `stageId` from the route; it must equal `body.stageId`.
3. Load the stage and its pipeline; return `404` if it does not exist and `403`
   if the pipeline's application is not the caller's application.
4. Validate `executionId` and `attempt` are present. Reject with `400` and
   `application/problem+json` (use the existing `writeProblemJSON` helper) when
   missing, when `result` exceeds the same size limit as `stage_io`, or when
   `errorCode` contains control characters.
5. Publish the message to the `StageResult` queue with persistent delivery and
   publisher confirms — reuse the publish helper the API already uses for
   `StageNext`/`StageSetStatus` (look for the confirm-aware publish in
   `apps/go/internal/mq`). Do not call `store.UpdateStageResult` from the API
   process: the worker process owns `publishPipelineUpdate` and
   `triggerPublisher`, and duplicating that here would fork the state machine.
6. Respond `202 Accepted` with `{"accepted": true, "stageId": 81, "executionId": "…"}`.
   A stale or duplicate result is still `202` — fencing happens in the consumer,
   which is the existing contract for AMQP workers.
7. Broker unavailable → `503` with problem type
   `https://api.pipelogiq.dev/errors/broker-unavailable`.
8. Add Prometheus counter `api_stage_results_posted_total{status_code}` and a
   latency histogram `api_stage_result_duration_ms`, registered like
   `resumeStage`/`resumeDurationMs`.

Tests (`apps/go/internal/api/external_endpoints_test.go` style, in-memory
SQLite schema from `testschema_test.go`):

- accepted result is published with the exact JSON body;
- missing session token → `401`; foreign application → `403`; unknown stage →
  `404`; missing `executionId` → `400`; route/body `stageId` mismatch → `400`;
- a result for a terminal stage is still `202` (consumer decides).

Docs: add the route to `docs/openapi.external.yaml` (schema `StageResultSubmit`
reusing the field docs from the `stage_result` message), and describe it in
`docs/reliable-execution.md` under a new "HTTP workers" subsection.

### P2. Harden `POST /jobs/pull` for HTTP workers

File: `apps/go/internal/api/external.go`, `handlePullJob`.

1. **Application scoping.** Resolve the application from the API key and require
   `req.Queue == constants.StageNextQueueName(constants.ApplicationQueueID(app.AppID), handler)`
   for some handler; reject any other queue with `403`. Derive the allowed
   prefix once and compare with `strings.HasPrefix` plus a non-empty handler
   suffix without `/` or whitespace.
2. **Long poll.** Accept optional `"waitSeconds": 0..20` (default `0`, so
   existing clients are unchanged). When `> 0`, loop `mq.Get` with a 250 ms
   sleep until a message arrives or the deadline passes, then return `204` on
   timeout. Keep the total under the router's 60 s timeout.
3. **Expired in-flight tokens.** The `pending` map already stores `expires`
   and a sweeper exists around line 1190; confirm it nacks with requeue and
   runs on a ticker. If it only runs opportunistically, give it a dedicated
   goroutine with `GATEWAY_VISIBILITY_TIMEOUT / 2` period and a test.
4. **Ack semantics doc.** `POST /jobs/ack` with `requeue:true` returns the
   message to the queue; document that HTTP workers must call ack (no
   requeue) only after `POST /stages/{id}/result` returned `202`, and requeue
   when the lease could not be acquired or the process is shutting down before
   execution started.

Tests: queue outside the application → `403`; `waitSeconds` returns a message
published after the request started; `waitSeconds` above 20 → `400`.

### P3. HTTP worker protocol document

New `docs/http-workers.md`, linked from `docs/index.md` and the README. It is
the contract tiqet's in-process worker is written against, so it must be exact:

1. `POST /workers/bootstrap` with `supportedHandlers` → `workerId`,
   `workerSessionToken`, `application.appId`, `queues.stageNextPattern`,
   `heartbeat.intervalSec`.
2. For each handler: queue name `= stageNextPattern` with the handler
   substituted (state the exact pattern string the server returns).
3. Loop: `POST /jobs/pull {queue, waitSeconds: 20}` → `204` or a job whose
   `payload` is `StageNextMessage` (`stageId`, `pipelineId`, `executionId`,
   `attempt`, `timeoutSeconds`, `stageHandlerName`, `input`, `contextItems`,
   `traceparent`).
4. `POST /stages/{stageId}/lease/acquire {executionId, workerId}`; if
   `acquired:false`, `POST /jobs/ack {token, requeue:false}` and continue
   (another worker owns it or the execution is stale).
5. Renew the lease every `leaseExpiresAt - now` / 2 (server default lease is
   `store.DefaultStageLeaseDuration` = 1 minute) while the handler runs; stop
   the handler cooperatively if renewal is refused.
6. Execute; then `POST /stages/{stageId}/result` (P1); then
   `POST /jobs/ack {token}`.
7. `POST /workers/heartbeat` every `intervalSec` with `state`, `inFlightJobs`,
   `jobsProcessed`, `jobsFailed`; `POST /workers/shutdown` on stop.
8. Failure matrix: pull `429` → back off 1 s; result `503` → retry with
   backoff up to the lease expiry, then requeue; session token invalid (`401`)
   → re-bootstrap.

Include a sequence diagram (Mermaid) and a curl walkthrough against
`make compose-up`.

### P4. .NET SDK: HTTP transport (does not block tiqet)

tiqet implements its own thin client against P1–P3 to avoid the transitive
`RabbitMQ.Client` reference. Ship the same capability in the SDK afterwards so
other consumers get it for free:

1. `PipelogiqApiClient`: `PullJobAsync(queue, waitSeconds)`, `AckJobAsync(token, requeue)`,
   `PostStageResultAsync(StageResultDto)`.
2. Split packages so an HTTP-only consumer has no AMQP dependency:
   `PipelogiqSDK.Core` (Abstractions, Contracts, Api, Builders, Execution,
   StageHelper) ← `PipelogiqSDK` (AMQP runner, unchanged public API) and
   `PipelogiqSDK.Http` (`HttpPipelineRunner` implementing P3 with the same
   `RegisterHandler` surface and `StageExecutor`).
3. Example `examples/HttpWorker` and a compatibility note in
   `docs/compatibility.md`.
4. Publish `0.4.1-preview.1` of all packages.

### P5. Operations for a co-hosted deployment

tiqet runs on one VPS with Docker Compose and Caddy; Pipelogiq will run beside
it in its own Compose project on the shared external network `pipelogiq`.

1. `infra/compose/docker-compose.registry.yml`: confirm Postgres and RabbitMQ
   use named volumes and `restart: unless-stopped`; add `mem_limit`/`cpus`
   examples in comments; make Tempo and Grafana an optional profile
   (`--profile observability`) so a host that already runs Grafana Alloy can
   point `OTEL_EXPORTER_OTLP_ENDPOINT` at it and skip both.
2. Never publish `5672`, `15672`, `8080`, `9090`, `9091` on the host in the
   production example; only `8081` (external API) and `3300` (dashboard) are
   reachable, and only from the Docker network (Caddy in the tiqet project
   proxies `ops.<domain>` → `pipelogiq-app:80` with basic auth in front of the
   dashboard). Document the Caddy snippet.
3. Startup bootstrap of an application and API key from environment so a fresh
   host needs no dashboard clicks: `BOOTSTRAP_APPLICATION_NAME`,
   `BOOTSTRAP_APPLICATION_APP_ID`, `BOOTSTRAP_API_KEY` (plaintext read once,
   stored hashed the same way `handleGenerateApiKey` stores keys, no-op when the
   application already exists). Log a single line without the key.
4. `docs/releases/RELEASE_NOTES_v0.4.1-preview.1.md` + CHANGELOG entries for
   P1–P3, P5; bump versions in README badges and OpenAPI `info.version`.
5. Tag `v0.4.1-preview.1` so GHCR images exist for tiqet's Compose file.

### P6. Schedules

Purpose: a developer registers a recurring, interval, or one-shot trigger from
the SDK; every firing creates an ordinary pipeline; the dashboard shows the
schedule, its run history, and each run's stages and logs.

Design principle: **a schedule is a stored pipeline definition plus a trigger.**
The scheduler only creates pipelines. Execution, leases, retries, fencing,
WebSocket updates, and metrics are the existing machinery.

#### Schema (Liquibase, additive)

```
schedule
  id                  bigserial pk
  application_id      fk application
  name                text            -- unique per application
  status              text            -- Active | Paused | Archived
  kind                text            -- Cron | Interval | Once
  cron_expression     text null       -- 5-field cron, plus @every/@daily/@hourly
  time_zone           text            -- IANA, default UTC
  interval_seconds    int null
  run_at              timestamptz null  -- Once
  definition_json     jsonb           -- same body as POST /pipelines (stages, options, keywords, context)
  definition_version  int             -- +1 on every definition change
  overlap_policy      text            -- Skip | Queue | Replace  (default Skip)
  catchup_policy      text            -- None | One | All        (default None)
  catchup_max         int             -- for All, default 10
  jitter_seconds      int             -- default 0
  next_run_at         timestamptz null
  last_run_at         timestamptz null
  last_run_status     text null
  created_by          text            -- "application:{id}" or "user:{id}"
  created_at, updated_at

schedule_run
  id                  bigserial pk
  schedule_id         fk schedule
  scheduled_for       timestamptz     -- planned tick
  fired_at            timestamptz
  trigger             text            -- Cron | Interval | Once | Manual | Catchup
  outcome             text            -- Created | Skipped | Failed
  skip_reason         text null       -- Overlap | Paused | Catchup
  pipeline_id         fk pipeline null
  definition_version  int
  error               text null
  unique (schedule_id, scheduled_for)

pipeline
  + schedule_id       fk schedule null
  + schedule_run_id   fk schedule_run null
```

The unique index on `(schedule_id, scheduled_for)` is the idempotency guard:
two scheduler instances or a restart cannot fire the same tick twice.

#### Scheduler (`apps/go/internal/worker`)

New goroutine registered as `startWorker("scheduler", w.runScheduler)`; tick
every second (`SCHEDULER_POLL_INTERVAL`, default `1s`).

1. `SELECT … FROM schedule WHERE status = 'Active' AND next_run_at <= now()
   ORDER BY next_run_at FOR UPDATE SKIP LOCKED LIMIT 100` inside a
   transaction; this is the multi-instance guard, no leader election.
2. For each row compute the ticks between `next_run_at` and `now()`:
   - `catchup_policy = None`: record every missed tick except the latest as
     `Skipped/Catchup`, fire only the latest.
   - `One`: same, but the fired run carries `trigger = Catchup`.
   - `All`: fire up to `catchup_max` missed ticks in order, skip the rest.
3. Overlap check: an unfinished pipeline with this `schedule_id` exists →
   `Skip` records `Skipped/Overlap`; `Queue` creates the pipeline with its
   first stage `depends_on` the running pipeline's last stage (reuse the
   existing `dependsOn` option; document the limitation that queued runs wait
   for the *pipeline*, not the schedule); `Replace` cancels the running
   pipeline through the existing cancel path, then creates.
4. Create the pipeline with the store function behind
   `POST /pipelines/idempotent`, idempotency key
   `schedule:{scheduleId}:{scheduled_for RFC3339}`, `schedule_id` and
   `schedule_run_id` set, keywords from the definition plus
   `schedule={name}`. Substitute exactly three placeholders in stage inputs and
   context values: `{{scheduledFor}}` (RFC3339 UTC), `{{runId}}`,
   `{{previousPipelineId}}` (empty on first run). No other templating.
5. Insert `schedule_run`, update `last_run_*`, and set `next_run_at` from the
   expression in `time_zone` (use `github.com/robfig/cron/v3` parser only, not
   its runner) plus a random `0..jitter_seconds`. `Once` schedules become
   `Archived` after firing.
6. Failures to create a pipeline (DB or validation) record `Failed` with the
   error and still advance `next_run_at`; a schedule never wedges the loop.

DST: compute in the schedule's zone, store UTC; document robfig behaviour for
skipped and repeated local hours.

#### External API (`X-API-Key`)

```
PUT    /schedules/{name}                 create or update (idempotent upsert); body below
GET    /schedules                        list for the application (status filter, cursor)
GET    /schedules/{name}                 definition, policies, next/last run, health summary
POST   /schedules/{name}/pause
POST   /schedules/{name}/resume          recomputes next_run_at from now
POST   /schedules/{name}/trigger         body { "input"?: {...} } → creates a Manual run immediately
POST   /schedules/{name}/archive
GET    /schedules/{name}/runs            cursor-paged schedule_run rows with pipeline ids
```

Upsert body:

```json
{
  "kind": "Cron",
  "cronExpression": "*/1 * * * *",
  "timeZone": "Europe/Madrid",
  "intervalSeconds": null,
  "runAt": null,
  "overlapPolicy": "Skip",
  "catchupPolicy": "None",
  "catchupMax": 10,
  "jitterSeconds": 15,
  "definition": { "name": "dispatch-due-emails", "stages": [ … ], "pipelineKeywords": [ … ], "pipelineContextItems": [ … ] }
}
```

An unchanged body returns `200` without bumping `definition_version`; a
changed one returns `200` with the new version; a new schedule returns `201`.
Validation errors use the existing problem+json helper with per-field
messages (invalid cron, unknown zone, `Once` without `runAt`, interval below
`SCHEDULER_MIN_INTERVAL` default 10 s).

Internal API mirrors these routes under the membership-scoped router for the
dashboard, plus `GET /schedules/{id}/runs/{runId}` that resolves to the
pipeline detail.

#### SDK (`../pipelogiq-sdk-net`)

`ScheduleBuilder` reuses `PipelineBuilder` for the definition:

```csharp
await ScheduleBuilder
    .Create("dispatch-due-emails", options)
    .Cron("*/1 * * * *", timeZone: "Europe/Madrid")   // or .Every(TimeSpan) / .RunOnceAt(DateTimeOffset)
    .WithAction<DispatchDueEmailsHandler>("dispatch", new { batchSize = 200 })
    .OverlapPolicy(OverlapPolicy.Skip)
    .Catchup(CatchupPolicy.None)
    .Jitter(TimeSpan.FromSeconds(15))
    .AddKeyword("owner", "tiqet")
    .ApplyAsync(ct);
```

`PipelogiqApiClient` gains `PutScheduleAsync`, `GetScheduleAsync`,
`ListSchedulesAsync`, `PauseScheduleAsync`, `ResumeScheduleAsync`,
`TriggerScheduleAsync`, `ArchiveScheduleAsync`, `ListScheduleRunsAsync`. An
HTTP-only consumer (tiqet) uses the routes directly.

#### Dashboard (`apps/web`)

New page `Schedules` next to Pipelines:

- List: name, human-readable expression ("every minute", "daily 03:00
  Europe/Madrid"), next run, last run status chip, a 20-run outcome strip
  (green/red/grey), pause toggle, health badge ("3 of last 10 failed",
  "lagging 4 min").
- Detail: definition rendered like `PipelineDetail`, policies, version and
  author, "Run now" with optional input, and the run table: scheduled,
  fired, lag (`fired − scheduled`), duration, outcome/skip reason, link to the
  pipeline (which opens the existing detail with stages and logs).
- Runs stream over the existing WebSocket hub (`ScheduleRunCreated` event).

#### Metrics and alerts

`schedule_runs_total{schedule,outcome}`, `schedule_lag_seconds{schedule}`
(histogram), `schedule_next_run_timestamp_seconds{schedule}`,
`scheduler_tick_duration_seconds`. Alert rule shipped in the Grafana
provisioning: schedule silent for more than 2× its interval.

#### Tests

Table-driven tests for next-run computation (cron, interval, once, DST
transitions in `Europe/Madrid`, jitter bounds); scheduler tests on SQLite for
catch-up policies, overlap policies, idempotent double tick, archive after
`Once`; endpoint tests for every status code; SDK builder tests; a Docker
smoke that registers an `@every 10s` schedule and asserts three runs with
pipelines reaching `Completed`.

#### Docs

`docs/schedules.md` (concepts, policies, DST note, placeholders, limits:
hundreds of schedules per application, second-level precision is not a goal),
OpenAPI entries, README section, CHANGELOG.

### P7. License and Pro boundary

Purpose: a commercial tier for organizations, next to an Apache-2.0 core that
stays complete for a single developer. Rule for every feature: **what one
engineer needs to run and debug a pipeline is free; what an organization needs
to govern, comply, and scale is Pro.**

#### License token

- `PIPELOGIQ_LICENSE` holds a signed JWT (Ed25519). Claims: `sub` (company),
  `tier` (`pro` | `enterprise`), `engineers` (band ceiling), `exp`, `iat`,
  `features` (explicit list, so a tier can be extended without a new binary).
- Verified offline with the public key embedded at build time. No network
  call, ever. Missing or invalid → Community; expired → Community after a
  14-day grace period with a dashboard banner. **Pipelines never stop because
  of a license.**
- `GET /license` on the internal API returns tier, features, expiry, and the
  engineer band for the Settings page; the same data appears in `/version`
  as `"tier"`.
- A private tool `cmd/licensegen` (kept in a separate private repository,
  documented but not shipped) signs tokens; the public key file lives in
  `internal/license/pubkey.go`.

#### Feature gating

- `internal/license` exposes `Has(feature) bool`; handlers gate with one call
  and return `402` problem+json type
  `https://api.pipelogiq.dev/errors/license-required` naming the feature.
- Pro code lives in a private Go module compiled in with build tag `pro`; the
  public image is built with the tag and the gate, so there is one image and
  the license decides. Public CI builds without the module using stub
  packages that always return "not licensed", so the open repository still
  builds and tests.
- The dashboard reads `GET /license` once and renders locked features with a
  short explanation and a link to the pricing page rather than hiding them.

#### First three Pro features (this release)

1. **SSO** — OIDC login (generic provider; Entra ID and Google presets) with
   group-to-role mapping; local accounts remain for break-glass.
2. **Audit export** — every mutation on pipelines, schedules, policies,
   approvals, API keys, and users already lands in the internal audit tables;
   add signed NDJSON export per application and date range, plus a retention
   setting.
3. **Retention** — configurable purge of finished pipelines, stage IO, and
   logs per application (`keep 30d/90d/365d`), with optional archive to
   S3-compatible storage before purge. Community keeps the current
   unbounded behaviour with a documented manual SQL.

Candidates for the next release, listed so the gate names exist now:
`schedules.advanced` (catch-up and overlap policies beyond Skip/None),
`alerting.oncall` (PagerDuty/Opsgenie), `approvals.slack`, `ha.multi-instance`
(priority queues, control-plane replicas), `agent.cost-caps`.

#### Pricing page and terms

The page lives in `../pipelogiq-landing` (separate repository); this plan only
fixes the content so both repositories agree:

- Community — free, Apache-2.0, everything a developer needs.
- Pro Team (≤ 50 engineers) €2 400/yr · Pro Business (≤ 250) €6 000/yr ·
  Pro Scale (250+) €12 000/yr · Enterprise from €25 000/yr with SLA, source
  escrow, and air-gap bundle.
- Services: "Migrate from Hangfire in a week" fixed price, design-partner
  program (five seats), Agent Governance workshop.
- The core-stays-Apache-2.0 promise stated verbatim in README and pricing.

`LICENSE` remains Apache-2.0 for the repository; add `LICENSE-PRO.md`
describing the commercial terms for the `pro` module and the token.

#### Tests

Token verification (valid, expired within grace, expired past grace, wrong
key, tampered claims, missing), gate behaviour per tier, `402` payload,
stub-build compiles without the private module, SSO callback with a fake
provider, export format and retention purge on SQLite.

## Acceptance

- `make fmt lint test` clean; new tests cover every status code listed above.
- Schedules smoke passes and the dashboard shows the three runs with logs.
- A Community build without the private module builds, runs, and reports
  `tier: community`; a Pro token unlocks SSO, export, and retention without a
  restart.
- A shell script `scripts/http-worker-smoke.sh` runs against `make compose-up`:
  creates an application/API key through the bootstrap env, creates a two-stage
  pipeline via `/pipelines/idempotent`, plays the HTTP worker protocol with
  `curl` for both stages (one `retryable` failure first, then success), and
  asserts `GET /pipelines/{id}` reaches `Completed` with `retryAttempt = 1` on
  the first stage.
- `docs/http-workers.md` is enough for someone to implement a worker without
  reading Go.
