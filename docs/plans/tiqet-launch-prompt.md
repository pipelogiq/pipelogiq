# Prompt for the implementing model (Pipelogiq)

Paste everything below the line into a fresh Claude Code session started in
`/Users/sergeifedulov/Documents/Dev/Pipelogiq/pipelogiq`.

---

You are implementing `docs/plans/tiqet-launch-plan.md` in this repository.
Read it fully first, then `CLAUDE.md`, `docs/architecture.md`,
`docs/reliable-execution.md`, `apps/go/internal/api/external.go`,
`apps/go/internal/api/external_reliability.go`,
`apps/go/internal/worker/worker.go`, `apps/go/internal/types/messages.go`, and
the existing external endpoint tests. Do not start coding until you can
explain, in your own words, how a `StageResultMessage` travels from an AMQP
worker to `Store.UpdateStageResult` today.

Work in two releases, one commit per item, Conventional Commits style, subject
under 72 characters:

1. `v0.4.1-preview.1`: P1, P2, P3, P5 in that order, then the release commit
   and tag.
2. `v0.5.0-preview.1`: P6 (Schedules: schema, scheduler, external and internal
   API, dashboard page, metrics, docs), then P7 (license token, gating, the
   three Pro features, terms), then the release commit and tag. P6 and P7
   also need SDK work in `../pipelogiq-sdk-net` (`ScheduleBuilder`, client
   methods); do it in that repository as its own commits after the server side
   is merged.

P4 (SDK HTTP transport) is last and optional; ask before starting it.

Rules:

- P1–P5 are additive with no schema changes. P6 and P7 add Liquibase
  changesets that are additive (new tables, nullable columns) and must be
  safe to roll back by ignoring the new columns.
- The scheduler only creates pipelines. It never executes handlers, never
  bypasses the publisher, and never fires the same `(schedule, scheduled_for)`
  twice — prove that with a test that runs two scheduler instances against
  one database.
- The license check is offline. No outbound network call for licensing under
  any circumstances, and no code path where an expired license stops or
  fails a pipeline. The public repository must build and pass tests without
  the private Pro module (stub packages).
- Results posted over HTTP must go through the `StageResult` queue with
  publisher confirms, not directly into the store. The worker process owns the
  state machine.
- Every new endpoint gets: problem+json errors through the existing helper,
  Prometheus metrics registered like the neighbouring ones, an OpenAPI entry,
  and table-driven tests for each documented status code.
- `make fmt`, `make lint`, `make test` must pass before each commit. Run the
  Docker smoke (`scripts/http-worker-smoke.sh`) before the release commit and
  paste its output in the commit body.
- Follow `CLAUDE.md` conventions: `slog` structured logging, explicit errors, no
  panics in handlers, context on every DB/MQ call.
- Do not log API keys, session tokens, or result payloads.

Deliver at the end of each release: the list of commits, the smoke script
output, the exact `docker-compose.registry.yml` service names, ports, and
environment variables a consumer needs to run the release next to another
Compose project on the `pipelogiq` network, and (for `v0.5.0`) a sample
Community-vs-Pro feature table generated from the gate names in code.
