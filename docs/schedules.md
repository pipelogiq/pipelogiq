# Schedules

A schedule is a **stored pipeline definition plus a trigger**. Every firing
creates an ordinary pipeline.

That sentence is the whole design. The scheduler creates pipelines and does
nothing else: it never executes a handler, never publishes to a stage queue, and
never touches stage state. Execution, leases, retries, fencing, cancellation,
WebSocket updates, and metrics are the machinery that already runs every other
pipeline. A scheduled run is indistinguishable from one created by
`POST /pipelines/idempotent`, except that it carries a `schedule_id` and a
`schedule=<name>` keyword.

## Registering a schedule

```http
PUT /schedules/dispatch-due-emails
X-API-Key: <application API key>
Content-Type: application/json

{
  "kind": "Cron",
  "cronExpression": "*/1 * * * *",
  "timeZone": "Europe/Madrid",
  "overlapPolicy": "Skip",
  "catchupPolicy": "None",
  "catchupMax": 10,
  "jitterSeconds": 15,
  "definition": {
    "name": "dispatch-due-emails",
    "stages": [
      { "stageName": "dispatch", "stageHandlerName": "DispatchDueEmailsHandler", "input": "{\"batchSize\":200}" }
    ],
    "pipelineKeywords": [ { "key": "owner", "value": "tiqet" } ]
  }
}
```

The name is the identity, scoped to the application and matched
case-insensitively. `PUT` is an upsert:

- a new name returns `201`;
- an unchanged body returns `200` and **does not** bump `definitionVersion` or
  move `nextRunAt` — re-applying the same schedule on every deploy is a no-op;
- a changed body returns `200` with a new version and a re-planned `nextRunAt`;
- re-applying an archived schedule brings it back Active.

`definition` is the same body `POST /pipelines` takes, and it is validated by the
same rules. A definition the create endpoint would reject is a schedule that
would fail on every tick, so it is rejected here instead.

## Kinds

| `kind` | Fields | Notes |
|---|---|---|
| `Cron` | `cronExpression`, `timeZone` | Standard 5-field syntax, plus `@every <duration>`, `@hourly`, `@daily`, `@midnight`. Six-field (seconds) expressions are **not** accepted. |
| `Interval` | `intervalSeconds` | Minimum 10 seconds (`SCHEDULER_MIN_INTERVAL`). Measured in wall-clock seconds; the time zone is irrelevant. |
| `Once` | `runAt` | Must be in the future. The schedule archives itself after firing. |

## Policies

### Overlap — what happens when a tick arrives while a run is still in flight

| `overlapPolicy` | Behaviour |
|---|---|
| `Skip` (default) | Record the tick as `Skipped/Overlap` with the in-flight pipeline's id. Nothing is created. |
| `Queue` | Create the pipeline, with its first stage depending on the last stage of the in-flight pipeline. |
| `Replace` | Cancel the in-flight pipeline through the ordinary cancel path, then create. |

**`Queue` has a limitation worth knowing before you choose it.** The dependency is
on that *pipeline*, not on the schedule. Three queued runs all wait for the same
in-flight pipeline and then start together, rather than forming a chain. If you
need strict serialisation, use `Skip` and make the work resumable.

`Replace` cancels cooperatively: a handler that ignores its cancellation token
keeps running after its pipeline is terminal. See
[Reliable execution](reliable-execution.md).

### Catch-up — what happens to ticks missed while the scheduler was down

| `catchupPolicy` | Behaviour |
|---|---|
| `None` (default) | Fire only the latest missed tick. Every earlier one is recorded as `Skipped/Catchup`. |
| `One` | Same, but the fired run is labelled `trigger: Catchup` so the history distinguishes it. |
| `All` | Fire up to `catchupMax` missed ticks in order, oldest first. Anything beyond the budget is recorded as `Skipped/Catchup`. |

Missed ticks are never silently dropped: each one gets a `schedule_run` row, so
the history explains a gap rather than leaving one.

A very long outage is capped at 1000 enumerated ticks. Past that the cursor
restarts from now, which is what keeps a 10-second interval schedule that was
down for a month from spinning.

### Jitter

`jitterSeconds` adds a random `0..n` offset to the *next firing*, not to the
recorded tick. Cron schedules snap back onto the expression on the following
computation, so jitter never accumulates into drift. Use it when many schedules
share one expression and you would rather not have them all hit the database in
the same second.

## Placeholders

Exactly three placeholders are substituted, in stage inputs and pipeline context
values only. Stage names, handler names, pipeline names, and keywords are left
alone. There is no other templating, and none is planned.

| Placeholder | Value |
|---|---|
| `{{scheduledFor}}` | The planned tick, RFC3339 in UTC. |
| `{{runId}}` | The `schedule_run` id. |
| `{{previousPipelineId}}` | The id of the last pipeline this schedule created; empty on the first run. |

```json
{ "stageName": "dispatch", "stageHandlerName": "DispatchHandler",
  "input": "{\"window\":\"{{scheduledFor}}\",\"previous\":\"{{previousPipelineId}}\"}" }
```

## Time zones and DST

Ticks are computed in the schedule's IANA zone and stored in UTC. A
"daily at 03:00 Europe/Madrid" schedule stays at 03:00 local across a transition.

The two awkward local times behave the way cron implementations conventionally
do, and both are surprising enough to state plainly:

- **A local time inside the hour that does not exist on the spring-forward day is
  skipped that day.** `30 2 * * *` in `Europe/Madrid` does not run at all on
  2026-03-29; the next run is 2026-03-30.
- **A local time inside the hour that occurs twice on the autumn-back day runs
  twice.** `30 2 * * *` runs at `00:30Z` and again at `01:30Z` on 2026-10-25.
  These are distinct UTC instants, so the `(schedule, scheduled_for)` uniqueness
  guard does not collapse them.

If neither is acceptable, pick a local time outside 02:00–03:00, or use UTC.
`Interval` schedules are unaffected — they add wall-clock seconds and never
consult the zone.

## Running exactly once

Two guards, in this order:

1. **The cursor advance.** The scheduler claims due schedules with
   `SELECT … FOR UPDATE SKIP LOCKED` and advances `next_run_at` inside the same
   transaction. A second instance walks straight past a row the first is holding
   and then sees nothing due. There is no leader election.
2. **The uniqueness index** on `schedule_run(schedule_id, scheduled_for)`. If
   anything slips past the first guard — a restart mid-batch, a clock jump — the
   insert conflicts and the second attempt does nothing.

Pipelines are additionally created under the idempotency key
`schedule:{scheduleId}:{scheduled_for}`, so even a retried creation resolves the
existing pipeline rather than making a second one.

## Manual runs

```http
POST /schedules/dispatch-due-emails/trigger
Content-Type: application/json

{ "input": { "batchSize": 1 } }
```

Creates a run immediately with `trigger: Manual`. It does **not** move the
cursor, and overlap and catch-up policies do not apply to it — a manual run was
explicitly asked for. The optional `input` replaces the first stage's input.

## Lifecycle

```http
POST /schedules/{name}/pause      # stops creating pipelines; keeps the definition
POST /schedules/{name}/resume     # recomputes nextRunAt from now
POST /schedules/{name}/archive    # terminal; re-applying the definition revives it
```

Resuming re-plans from the current time. A schedule paused for a week does not
wake up owing a week of ticks, regardless of its catch-up policy.

## Reading the history

```http
GET /schedules/{name}            # definition, policies, next/last run, health
GET /schedules/{name}/runs       # cursor-paged run rows with pipeline ids
GET /schedules?status=Active     # cursor-paged list
```

Each run row carries `scheduledFor`, `firedAt`, `lagSeconds` (`fired − scheduled`),
`trigger`, `outcome`, `skipReason`, `pipelineId`, `definitionVersion`, and
`error`.

| `outcome` | Meaning |
|---|---|
| `Created` | The tick produced a pipeline. `pipelineId` is set. |
| `Skipped` | The tick was deliberately not run. `skipReason` says why: `Overlap`, `Catchup`, or `Paused`. |
| `Failed` | The tick should have run but the pipeline could not be created. `error` carries the cause, and the schedule keeps ticking — a failure never wedges the loop. |

## Dashboard

**Schedules** sits next to Pipelines. The list shows the trigger in words
("every minute", "daily 03:00 Europe/Madrid"), next and last run, a 20-run
outcome strip, a health badge ("3 of last 10 failed", "lagging 4m"), and a pause
toggle. The detail page adds the rendered definition, the policies, the version
and author, "Run now" with an optional input override, and the run table — each
row links to the ordinary pipeline view with its stages and logs.

New runs arrive over the existing WebSocket as a `ScheduleRunCreated` event.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `SCHEDULER_POLL_INTERVAL` | `1s` | How often the worker looks for due schedules. Clamped to a 100 ms floor. |

The scheduler runs inside `pipelogiq-worker` as one more supervised goroutine. It
needs no separate process and no extra configuration; running several worker
replicas is safe.

## Metrics

| Metric | Type | Labels |
|---|---|---|
| `schedule_runs_total` | counter | `schedule`, `outcome` |
| `schedule_lag_seconds` | histogram | `schedule` |
| `schedule_next_run_timestamp_seconds` | gauge | `schedule` |
| `scheduler_tick_duration_seconds` | histogram | — |

Alert rules are provisioned in
`infra/observability/grafana/provisioning/alerting/schedules.yaml`: a schedule
overdue by more than twice its interval, a schedule whose runs are failing, and a
scheduler firing more than a minute late. They need a Prometheus datasource; the
Compose stack ships none, so point `PROMETHEUS_URL` at your own.

## Limits

These are deliberate, not accidental:

- **Hundreds of schedules per application**, not tens of thousands. One pass
  claims at most 100 due schedules.
- **Second-level precision is not a goal.** The scheduler ticks once a second and
  a tick fires when the pass reaches it. `lagSeconds` tells you what you actually
  got.
- **The minimum interval is 10 seconds.**
- **No templating beyond the three placeholders.**
- **A schedule cannot depend on another schedule.** Compose that inside one
  pipeline definition instead.
