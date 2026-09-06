# Pipelogiq v0.5.0-preview.1

**Released:** 2026-09-06
**Pairs with:** `pipelogiq-sdk-net 0.4.0-preview.1` (SDK schedule support follows)
**Images:** `ghcr.io/pipelogiq/pipelogiq-app:v0.5.0-preview.1`, `ghcr.io/pipelogiq/pipelogiq-worker:v0.5.0-preview.1`

Two things arrive together: **schedules**, so a recurring trigger creates
ordinary pipelines, and a **commercial tier** beside the Apache-2.0 core.

The minor version moves because the schema grows and a licence concept exists
that did not before. Nothing breaks: the migration is additive, every existing
API behaves as it did, and a deployment with no licence configured is a complete
Pipelogiq for a single developer.

---

## Schedules

A schedule is a **stored pipeline definition plus a trigger**. That sentence is
the design. The scheduler creates pipelines and nothing else — it never executes
a handler, never publishes to a stage queue, and never touches stage state.
Execution, leases, retries, fencing, cancellation, WebSocket updates, and metrics
are the machinery that already runs every other pipeline.

```http
PUT /schedules/dispatch-due-emails
X-API-Key: <application API key>

{
  "kind": "Cron",
  "cronExpression": "*/1 * * * *",
  "timeZone": "Europe/Madrid",
  "overlapPolicy": "Skip",
  "catchupPolicy": "None",
  "jitterSeconds": 15,
  "definition": { "name": "dispatch-due-emails", "stages": [ … ] }
}
```

- **Cron, Interval, and Once.** Standard 5-field cron plus `@every`, `@hourly`,
  `@daily`; intervals from 10 seconds; one-shots that archive themselves.
- **Overlap policies** `Skip`, `Queue`, `Replace` decide what a tick does while a
  previous run is still in flight.
- **Catch-up policies** `None`, `One`, `All` decide what happens to ticks missed
  while the scheduler was down. Missed ticks are **recorded as `Skipped`, never
  silently dropped**, so a gap in the history explains itself.
- **Idempotent upsert.** Re-applying an unchanged definition on every deploy is a
  no-op: the version does not move and neither does the next run.
- **Three placeholders** in stage inputs and context values —
  `{{scheduledFor}}`, `{{runId}}`, `{{previousPipelineId}}`. Nothing else is
  templated, and nothing else is planned.
- **Dashboard.** A Schedules page beside Pipelines: the trigger in words, next
  and last run, a 20-run outcome strip, a health badge, a pause toggle, "Run
  now", and a run table linking each run to its ordinary pipeline view.
- **Metrics and alerts.** `schedule_runs_total`, `schedule_lag_seconds`,
  `schedule_next_run_timestamp_seconds`, `scheduler_tick_duration_seconds`, plus
  provisioned Grafana rules for an overdue schedule, failing runs, and a lagging
  scheduler.

### Running exactly once

Two guards. The scheduler claims due rows with `FOR UPDATE SKIP LOCKED` and
advances the cursor in the same transaction, so a second instance walks past a
row the first is holding — no leader election. The unique index on
`schedule_run(schedule_id, scheduled_for)` catches anything that slips through. A
test runs two schedulers against one database and asserts one run row and one
pipeline per tick.

### DST, stated plainly

Ticks are computed in the schedule's zone and stored in UTC. The two awkward
local times behave as cron conventionally does, and both are surprising enough to
put in release notes:

- **A local time inside the spring-forward gap is skipped that day.**
  `30 2 * * *` in `Europe/Madrid` does not run on 2026-03-29.
- **A local time inside the autumn-back repetition runs twice.** `30 2 * * *`
  runs at `00:30Z` and again at `01:30Z` on 2026-10-25.

Pick a local time outside 02:00–03:00, or use UTC, if neither is acceptable.
Interval schedules never consult the zone.

See [docs/schedules.md](../schedules.md).

---

## Licence and the Pro tier

**The core stays Apache-2.0.** Everything a single developer needs to run and
debug a pipeline is free and will remain free. A commercial tier is added beside
the core; no existing code is relicensed.

Three properties hold without exception:

1. **A licence never stops a pipeline.** Gating happens at the API surface,
   before work is created. Nothing in the execution path consults the licence, so
   an expired licence cannot stop a running pipeline.
2. **A licence check never touches the network.** Verification is a local Ed25519
   signature check against a key compiled into the binary. There is no activation
   call and no telemetry — by construction, not by configuration.
3. **A licensing problem degrades, it does not fail.** Missing, malformed,
   expired, unreadable: every failure resolves to Community and logs why. A
   deployment never refuses to start over a licence.

An expired token keeps its features for a **14-day grace period** behind a
dashboard banner, then falls back to Community.

### One image, not two

Pro code compiles only under the `pro` build tag. Without it, every Pro entry
point returns "not licensed", so the open repository builds and its tests pass
with no private code present. The released image is built with the tag, and the
**licence** decides what is allowed. CI now builds and tests both ways so the two
cannot drift.

### Feature gates

Generated from the gate names in `internal/license`:

| Gate | What it adds | Community | Pro | Status |
|---|---|---|---|---|
| `sso` | OIDC single sign-on with group-to-role mapping | — | yes | **this release** |
| `audit.export` | Signed NDJSON export of the audit trail, with retention | — | yes | **this release** |
| `retention` | Scheduled purge of finished pipelines, stage IO and logs | — | yes | **this release** |
| `schedules.advanced` | Catch-up and overlap policies beyond Skip and None | — | yes | next release |
| `alerting.oncall` | PagerDuty and Opsgenie routing | — | yes | next release |
| `approvals.slack` | Approve and reject stages from Slack | — | yes | next release |
| `ha.multi-instance` | Priority queues and control-plane replicas | — | yes | next release |
| `agent.cost-caps` | Per-application spend caps for agent stages | — | yes | next release |

Community keeps the current behaviour for all of these — including unbounded
retention, with the manual SQL documented in
[docs/licensing.md](../licensing.md).

A closed gate answers `402` with `application/problem+json` naming the gate, so
an operator knows exactly what to buy. The dashboard renders locked features with
an explanation and a pricing link rather than hiding them.

See [docs/licensing.md](../licensing.md) and [LICENSE-PRO.md](../../LICENSE-PRO.md).

---

## Schema changes

Additive, and safe to roll back by ignoring the new columns:

- new tables `schedule`, `schedule_run`, `retention_policy`;
- new nullable `pipeline.schedule_id` and `pipeline.schedule_run_id`.

No column is dropped, narrowed, or made non-nullable. `v0.4.1-preview.1` runs
against the upgraded schema unchanged.

---

## Before you upgrade

### 1. Nothing is required

There is no new mandatory configuration. `PIPELOGIQ_LICENSE` unset means
Community, which is what every existing deployment already is.

### 2. Decide whether you want scheduler replicas

The scheduler runs inside `pipelogiq-worker` as one more supervised goroutine.
Running several worker replicas is safe and needs no configuration.
`SCHEDULER_POLL_INTERVAL` defaults to `1s`.

### 3. If you want the schedule alerts

They need a Prometheus datasource, and the Compose stack ships none. Point
`PROMETHEUS_URL` at your own instance, scraping the API on `METRICS_ADDR`
(default `:9091`) and the worker on `:9090`.

---

## Upgrade

```bash
docker network create pipelogiq 2>/dev/null || true
PIPELOGIQ_VERSION=v0.5.0-preview.1 docker compose --env-file .env   -f infra/compose/docker-compose.registry.yml up -d
```

Migrations run inside `pipelogiq-app` on startup:

```bash
docker logs pipelogiq-app 2>&1 | grep -E "liquibase migration completed|licence:|bootstrap:"
```

### Smoke tests

```bash
BOOTSTRAP_API_KEY=<your key> scripts/http-worker-smoke.sh
BOOTSTRAP_API_KEY=<your key> scripts/schedules-smoke.sh
```

Confirm the tier:

```bash
curl -s http://localhost:8081/version    # "tier": "community"
```

---

## Rollback

Pin the previous image tag and restart. The schema is additive, so
`v0.4.1-preview.1` runs against it and ignores the new tables and columns.
Schedules stop firing while the old image runs — no run rows are created, and
none are lost.

---

## Known limitations

Carried forward from `v0.4.1-preview.1`: no exactly-once external side effects,
dispatch is not one atomic transaction with the database, a lease excludes
competing workers only while valid, inline policy import is post-create and
best-effort, and the .NET SDK still has no HTTP transport.

New in this release:

- **A schedule is not a workflow engine.** Hundreds of schedules per
  application, not tens of thousands; second-level precision is not a goal; and a
  schedule cannot depend on another schedule.
- **`Queue` overlap waits for the pipeline, not the schedule.** Three queued runs
  all wait for the same in-flight pipeline and then start together rather than
  forming a chain. Use `Skip` if you need strict serialisation.
- **The SDK has no `ScheduleBuilder` yet.** Register schedules over HTTP against
  `docs/openapi.external.yaml` until it ships.
- **A licence takes effect on process start**, because the token is an
  environment variable. Applying one needs a restart — never a rebuild, and never
  a different image.
