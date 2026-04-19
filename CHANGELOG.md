# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project follows [Semantic Versioning](https://semver.org/). v0.x releases may include breaking changes.

## [0.3.2-preview.3] - 2026-04-19

Third `0.3.2` preview keeps the platform docs and release metadata aligned with the latest SDK robustness fix for duplicate OpenAI tool-call argument keys.

### Changed

- Public docs, OpenAPI metadata, and compose version pinning now reference `v0.3.2-preview.3`.
- Upgrade target for the matching .NET SDK is now `pipelogiq-sdk-net 0.3.2-preview.3`.

## [0.3.2-preview.2] - 2026-04-19

Second `0.3.2` preview aligns the platform with the latest SDK fixes for OpenAI request compatibility and tightens pipeline behavior around terminal invalid-request failures.

### Changed

- **Terminal invalid-request handling** — stage-result processing now treats `LLM_INVALID_REQUEST` as a non-retryable failure, so invalid planner/think requests fail immediately instead of going into automatic `RetryScheduled` loops.
- **Pipeline side-panel header density** — pause/resume actions in pipeline inspection now sit inline with `Started` and `Duration`, reducing wasted vertical space in the details header.
- Public docs, OpenAPI metadata, and compose version pinning now reference `v0.3.2-preview.2`.
- Upgrade target for the matching .NET SDK is now `pipelogiq-sdk-net 0.3.2-preview.2`.

## [0.3.2-preview.1] - 2026-04-19

First `0.3.2` preview aligns the platform with the new SDK release that adds OpenAI planner support and per-step model/provider routing, while refining how AI usage is presented in pipeline inspection.

### Added

- **Dedicated AI usage tab in pipeline inspection** — the dashboard pipeline side panel now renders pipeline-level and stage-level AI usage in a separate `AI Usage` tab when usage data is available, keeping token/cost diagnostics visible without mixing them into raw output and context payloads.

### Changed

- **Pipeline stage AI usage presentation** — stage-level LLM usage is now displayed as its own block instead of being embedded inside the `Output` section, and pipeline usage summaries are no longer duplicated inside per-stage views.
- **Context tab cleanup for usage metadata** — agent usage accumulator keys (`agent:session:*`) are now hidden from the `Context` tab because they are represented in the dedicated AI usage surface instead.
- Public docs, OpenAPI metadata, and compose version pinning now reference `v0.3.2-preview.1`.
- Upgrade target for the matching .NET SDK is now `pipelogiq-sdk-net 0.3.2-preview.1`.

## [0.3.1-preview.4] - 2026-04-18

Fourth `0.3.1` preview aligns the platform docs with the next SDK prerelease and highlights agent-run diagnostics that matter in live troubleshooting.

### Added

- **Pipeline-side LLM usage inspection** — the dashboard pipeline side panel can now show per-stage and per-session token usage, cache activity, model/provider metadata, and estimated USD cost when the matching SDK/runtime emits usage summaries.

### Changed

- **Agent terminal failure visibility** — platform release guidance now expects terminal tool-loop outcomes to surface as failed pipelines instead of green completions when paired with `pipelogiq-sdk-net 0.3.1-preview.4`.
- Public docs, OpenAPI metadata, and compose version pinning now reference `v0.3.1-preview.4`.
- Upgrade target for the matching .NET SDK is now `pipelogiq-sdk-net 0.3.1-preview.4`.

## [0.3.1-preview.3] - 2026-04-18

Third `0.3.1` preview realigns release metadata, docs, and downstream package references to the next available prerelease after `0.3.1-preview.2` was already published.

### Changed

- Public docs, OpenAPI metadata, and compose version pinning now reference `v0.3.1-preview.3`
- Upgrade target for the matching .NET SDK is now `pipelogiq-sdk-net 0.3.1-preview.3`

## [0.3.1-preview.2] - 2026-04-18

Second `0.3.1` preview focused on making retry/recovery paths safer and aligning release metadata with the updated SDK/runtime pair.

### Added

- **Authenticated pipeline detail endpoint** — external API clients can now call `GET /pipelines/{pipelineId}` to inspect current pipeline state through the same application-scoped API key used by workers and SDK clients

### Changed

- **Application-scoped queue naming** — worker bootstrap and stage publishing now derive queue prefixes from the authenticated application queue id instead of the server-global app id, keeping external workers and runtime publishers aligned
- **Append audit source labeling** — appended-stage audit logs now record whether stages were added via the API or via `stage_result`, making retry and recovery analysis much easier in live traces

### Fixed

- **Redelivery-safe scheduling** — `GetStageToExecute` now revalidates the stage state after lock acquisition, avoiding duplicate dispatch when a stage changed state between candidate selection and the row lock
- **Invalid stage-status regressions** — stage status updates now reject illegal transitions such as `Completed → Running`, preventing stale redeliveries from reviving terminal stages
- **Terminal-pipeline append guard** — result processing now refuses to append follow-up stages into pipelines that are already terminal

### Upgrade Notes

- Rebuild and redeploy both `pipelogiq-app` and `pipelogiq-worker`
- Upgrade `pipelogiq-sdk-net` to `0.3.1-preview.2` so worker/runtime retry deduplication and broker recovery behavior stay aligned

## [0.3.1-preview.1] - 2026-04-18

First `0.3.1` preview focused on transactional stage continuation and cleaner worker/runtime compatibility with the updated .NET SDK.

### Added

- **Appended-stage transport in stage results** — `stage_result` messages can now carry appended stage definitions, so follow-up work can be scheduled as part of the stage outcome instead of a separate append-stages HTTP round trip
- **Stage-name to stage-id context map** — appended stages are now indexed into pipeline context, allowing downstream resume/confirmation logic to resolve responder and follow-up stages after transactional appends

### Changed

- **Transactional append on result processing** — `UpdateStageResult` now persists appended stages inside the same database transaction that stores the stage outcome, reducing race windows between result persistence and next-stage creation

### Fixed

- **Agent follow-up stage races** — built-in agent flows no longer depend on a second REST call to append think/tool/critic/responder stages after a stage finishes
- **Worker scheduling query placeholders** — `GetStageToExecute` now uses consistent SQL parameter numbering, avoiding `could not determine data type of parameter $2` failures in the worker path

### Upgrade Notes

- Rebuild and redeploy both `pipelogiq-app` and `pipelogiq-worker`
- Upgrade `pipelogiq-sdk-net` to `0.3.1-preview.1` so SDK workers emit appended stages in `stage_result` instead of the legacy append-stages API path

## [0.3.0-preview.5] - 2026-04-18

Sixth preview release focused on stage dispatch performance: event-driven scheduling, batch dispatch, dependency-based parallel execution, and reduced poll latency.

### Added

- **Dependency-based parallel stage execution** — `GetStageToExecute` now evaluates the `depends_on` column from `stage_options`. Stages with explicit `depends_on` only wait for the named dependencies to reach a terminal state, enabling fan-out/fan-in and diamond DAG patterns within a single pipeline. Stages without `depends_on` retain strictly sequential scheduling for backward compatibility
- **Event-driven publisher wake** — result and status consumers now signal the publisher goroutine via an internal channel immediately after processing a stage outcome. The publisher wakes up on this signal instead of waiting for the next poll tick, reducing inter-stage latency from up to 1 second to under 5 milliseconds
- **Batch stage dispatch** — the publisher now dispatches up to 10 stages per cycle without pausing between them. When at least one stage is dispatched, it immediately checks for more work. The poll/wake wait only triggers when no stages are available
- **Scheduling test suite** — new `stage_scheduling_test.go` covering sequential scheduling (backward compat), `depends_on` parallel dispatch, diamond dependency patterns, mixed sequential/dependency modes, cross-pipeline batch dispatch, and throughput benchmarks (up to 100 pipelines × 5 stages)

### Changed

- **Default poll interval reduced** — `WORKER_POLL_INTERVAL` default lowered from 1 second to 200 milliseconds; minimum allowed value lowered from 100ms to 50ms. The event-driven wake means the poll interval is now only a fallback, not the primary dispatch trigger
- **Scheduling SQL refactored** — the `GetStageToExecute` CTE now joins `stage_options` to read `depends_on` and `run_next_if_failed` in a single pass. The blanket `NOT EXISTS (... status = Pending)` check that prevented any parallel dispatch within a pipeline has been removed; individual dependency checks are sufficient

### Fixed

- **Sequential-only scheduling limitation** — prior to this release, `depends_on` and `run_in_parallel_with` columns were stored but ignored at runtime, forcing strictly serial execution regardless of actual dependencies (noted as a known limitation since v0.1.0-preview.1). `depends_on` is now fully evaluated during stage scheduling

### Upgrade Notes

- No database migrations required
- Rebuild and redeploy `pipelogiq-worker` to pick up all dispatch performance improvements
- Existing pipelines are unaffected — stages without `depends_on` continue to execute sequentially
- To use parallel dispatch, set `depends_on` in stage options when creating pipeline stages (comma-separated stage names)
- Optionally tune `WORKER_POLL_INTERVAL` (new default 200ms is appropriate for most workloads)

**Full Changelog**: https://github.com/pipelogiq/pipelogiq/compare/v0.3.0-preview.4...v0.3.0-preview.5

## [0.3.0-preview.4] - 2026-04-17

Fifth preview release focused on resilience: graceful worker shutdown, orphaned stage recovery, and build provenance.

### Added

- **Orphaned stage recovery** — new `runOrphanRecovery` goroutine periodically scans for stages stuck in `Running` or `Pending` longer than half the active timeout and resets them to `NotStarted` for re-scheduling. Runs on startup and then on a ticker. Changes are logged with `orphan_recovery` source via `LogStageChange`
- **Build provenance in Docker images** — Dockerfiles for `pipelogiq-app` and `pipelogiq-worker` now accept `PIPELOGIQ_VERSION`, `PIPELOGIQ_COMMIT`, and `PIPELOGIQ_BUILD_DATE` build args. Values are injected via Go `ldflags` at compile time and also exposed as `APP_VERSION`, `APP_COMMIT`, `APP_BUILD_DATE` env vars for runtime fallback. OCI labels (`org.opencontainers.image.version`, `.revision`, `.created`) are set on the final images
- **Version endpoint resilience** — `version.Get()` now resolves version fields from ldflags first, then falls back to env vars (`APP_VERSION`, `APP_COMMIT`, `APP_BUILD_DATE`), then to sentinel defaults. This ensures `GET /version` returns accurate build info in all deployment modes (ldflags, env-only, bare binary)
- **CI release metadata** — `release.yml` workflow now extracts commit and build date alongside the version tag and passes all three as Docker build args

### Changed

- **Graceful worker shutdown** — `Worker.Run()` now uses a `sync.WaitGroup` to wait for all internal goroutines (publisher, consumers, timeout watcher, orphan recovery) to finish before returning. Previously the worker exited immediately on context cancellation, potentially abandoning in-flight stage processing
- **Drain-safe message handler context** — RabbitMQ consumer now creates handler contexts with `context.WithoutCancel(ctx)` instead of passing the parent context directly. This ensures in-flight message handlers can complete their work (including publishing results back) even after SIGTERM triggers parent context cancellation

### Fixed

- **In-flight stage loss on restart** — before this release, a worker receiving SIGTERM would cancel all contexts immediately, causing running stage handlers to fail mid-execution. The combination of `WaitGroup`-based drain, `WithoutCancel` handler contexts, and orphan recovery eliminates this failure mode

### Upgrade Notes

- No database migrations required
- Rebuild and redeploy both `pipelogiq-app` and `pipelogiq-worker` to pick up graceful shutdown, orphan recovery, and build provenance
- For the best SDK-side compatibility, upgrade `pipelogiq-sdk-net` to `0.3.0-preview.6` which includes matching graceful shutdown on the .NET worker side
- Set `PIPELOGIQ_VERSION`, `PIPELOGIQ_COMMIT`, `PIPELOGIQ_BUILD_DATE` env vars or build args for accurate `GET /version` output in your deployment

**Full Changelog**: https://github.com/pipelogiq/pipelogiq/compare/v0.3.0-preview.3...v0.3.0-preview.4

## [0.3.0-preview.3] - 2026-04-13

Fourth preview release focused on worker resilience, clearer diagnostics, and easier pipeline inspection in the dashboard.

### Added

- **Deployment footer in the dashboard** — the sidebar now shows the active environment label and the live deployed version from `GET /version`, making it easier to confirm where you are working
- **Richer worker diagnostics in the dashboard** — worker registry rows now surface `statusReason` and `lastError`, and worker activity cards render structured event details instead of only the headline message

### Changed

- **Pipeline inspection readability** — long log lines, JSON payloads, and context values now wrap inside the pipeline side panel and detail view, so logs and context can be read without horizontal scrolling
- **Context payload viewing** — expanded context previews and full-view dialogs now keep large structured values inside the available panel width while preserving formatted output

### Fixed

- **RabbitMQ consumer CPU spin** — `pipelogiq-worker` no longer enters a tight loop when a RabbitMQ delivery channel closes during consume/reconnect handling
- **Worker polling safety** — invalid or too-small worker poll intervals are now clamped to safe values with explicit warnings, preventing accidental busy-polling from bad environment configuration
- **Timeout watcher safety** — invalid stage timeout settings now fall back to sane defaults instead of risking pathological ticker behavior

### Upgrade Notes

- No database migrations are required for this preview
- Rebuild and redeploy both `pipelogiq-app` and `pipelogiq-worker` to pick up the worker CPU fix, worker diagnostics improvements, and dashboard inspection updates
- For the best worker-side compatibility, upgrade `pipelogiq-sdk-net` to `0.3.0-preview.3`

**Full Changelog**: https://github.com/pipelogiq/pipelogiq/compare/v0.3.0-preview.2...v0.3.0-preview.3

## [0.3.0-preview.2] - 2026-04-12

Third preview release focused on pipeline inspection, richer runtime diagnostics, and agent-flow stability.

### Added

- **Richer stage execution logs** — stages now persist execution scheduling details, result summaries, input previews, and append-time audit context instead of only status-change events
- **Agent execution diagnostics** — agent stages now emit structured logs for think steps, tool dispatch, confirmation delivery, responder delivery, and terminal loop/budget paths
- **Pipeline logs UX refresh** — the dashboard `Logs` tab now shows per-stage cards with handler name, created/started/finished timestamps, duration, input, output, and raw stage log entries

### Changed

- **Failure continuation semantics** — stages marked with `run_next_if_failed` now remain runnable after an upstream failure, and pipelines stay active while a valid continuation stage still exists
- **Append-stage audit detail** — appended stages now record handler, retry/timeout options, parallel/depends-on metadata, and input preview in the persisted audit trail
- **Pipeline detail visibility** — pipeline side panel now surfaces stage input alongside stage output, reducing the need to inspect context separately for basic debugging

### Fixed

- **Agent responder duplication** — the .NET agent flow now appends `agent:responder` idempotently instead of repeatedly creating duplicate responder stages in terminal error paths
- **Terminal agent follow-up execution** — responder stages appended after tool-loop or budget terminal conditions now execute correctly instead of being stranded behind a failed predecessor
- **Login page hygiene** — removed exposed demo credentials from the dashboard sign-in screen

### Upgrade Notes

- No database migrations are required for this preview
- Rebuild and redeploy both `pipelogiq-app` and `pipelogiq-worker` to pick up the richer runtime logging and `run_next_if_failed` scheduling fixes
- For the best troubleshooting experience, upgrade `pipelogiq-sdk-net` to `0.3.0-preview.2` as well so worker-side stage logs include the new agent diagnostics

**Full Changelog**: https://github.com/pipelogiq/pipelogiq/compare/v0.3.0-preview.1...v0.3.0-preview.2

## [0.3.0-preview.1] - 2026-04-07

Preview release line realigned to `v0.3.0-preview.1`. This GitHub release includes all changes merged after `v0.2.0-preview.1`, including the previously untagged March preview work, plus the final versioning and documentation refresh.

### Added

- **Role-aware JWT sessions** — internal JWTs now carry user roles, WebSocket access is routed through authenticated paths, and admin-only controls now protect applications, API keys, worker operations, observability, and policy management
- **Configurable CORS allowlist** — both internal and external APIs now honor `CORS_ALLOWED_ORIGINS` instead of reflecting arbitrary origins
- **Policy provenance and explainability** — policies now track `source` (`system` vs `pipeline_inline`) and `origin` metadata, support inline import from pipeline definitions, and expose effective stage policy resolution via `GET /policies/effective/stages/{stageId}`
- **Policy governance actions** — inline policies can be promoted to system policies, orphaned inline policies are surfaced explicitly, and the dashboard now shows source/provenance filters and resolution semantics
- **Approval outcome context injection** — resumed approval stages now write `agent:approved` and `agent:rejectionReason` into pipeline context for downstream stages

### Changed

- **External stage ownership enforcement** — external append/resume flows now require the target pipeline/stage to belong to the calling application, closing cross-app access paths
- **DB-backed policy runtime** — policy CRUD and inline imports now sync to PostgreSQL, and runtime evaluation can resolve effective policies from the persisted dataset
- **Preview line renumbering** — the active preview release line now uses `v0.3.0-preview.1`
- **Documentation refresh** — README, quickstart, OpenAPI metadata, compose examples, and release references now consistently point to `v0.3.0-preview.1`

### Fixed

- **External endpoint test coverage** — test fixtures now include `pipeline_context_item`, aligning external stage control tests with approval-context writes

### Upgrade Notes

- Run database migrations before starting `pipelogiq-app`; this release adds `policy.source` and `policy.origin` columns and expands policy runtime persistence
- Set a strong `JWT_SECRET` (minimum 32 characters). The API now fails fast if it is missing or too short
- Review `CORS_ALLOWED_ORIGINS` for every deployed dashboard/API origin. Origins outside the allowlist now receive `403 Forbidden`
- External clients can only append stages to, or resume stages within, pipelines owned by their own `application_id`
- Imported inline policies are read-only in the dashboard until promoted to a system policy

**Full Changelog**: https://github.com/pipelogiq/pipelogiq/compare/v0.2.0-preview.1...v0.3.0-preview.1

## [0.1.0-preview.3] - 2026-03-19

Draft preview section prepared before the release line was renumbered. The shipped GitHub release for this work is `v0.3.0-preview.1`.

### Added

- **Policy-based retry enforcement** — retry policies are now evaluated at runtime when a stage fails. `maxAttempts`, `baseDelayMs`, `maxDelayMs`, `backoff` (`fixed`, `linear`, `exponential`), and `jitter` are all applied. Policy retry takes precedence over per-stage `maxRetries`/`retryInterval` options; stage options remain as fallback when no policy matches
- **`retryOn` error code filtering** — retry policies can declare `retryOn.errorCodes` to retry only on specific failure codes (e.g. `RATE_LIMIT_EXCEEDED`, `TIMEOUT`, `UPSTREAM_ERROR`). Stages that do not match any declared code are marked `Failed` immediately without retrying
- **`ErrorCode` on stage results** — workers can now report a structured error code alongside the failure message. The .NET SDK exposes `StageResult.RateLimitExceeded(msg)`, `StageResult.Timeout(msg)`, and `StageResult.UpstreamError(msg)` factory helpers, plus a general `StageResult.Error(msg, errorCode)` overload
- **DB-backed policy runtime** — policies and runtime trigger events are now persisted to PostgreSQL (`policy` and `policy_event` tables). The API server seeds the database from the file store on startup. Both the API and worker now read active policies directly from the database, removing the dependency on a shared policy file at runtime
- **`source` and `origin` policy columns** — new Liquibase migration adds `source` (`system` / `pipeline_inline`) and `origin` (jsonb) columns to the `policy` table
- **Policy DB sync** — create, update, delete, duplicate, promote, enable/disable/pause/resume, and inline import operations now sync to the database asynchronously in addition to writing to the file store

### Fixed

- **Worker policy runtime** — the worker binary was creating a new file-backed policy repository on every `RuntimePolicies()` call (because the repo was `nil`). This made it impossible to use policies in a distributed deployment where the worker has no access to the policy file. Both the API and worker now use the DB-backed runtime
- **Dual repository bug in API** — the API binary previously instantiated two separate `policyRepository` instances (one for the runtime, one for the HTTP server). Changes made through the HTTP handlers were not visible to the runtime. Both now use the same repository instance, plus DB as the durable backend

### Changed

- **Retry delay calculation** — backoff delay is now computed by `computeBackoffDelay()` using the policy rule. The legacy `retryInterval` field on stage options continues to work as a fixed-delay fallback

## [0.1.0-preview.2] - 2026-03-18

Second preview release focused on external stage control, timeout handling, and RabbitMQ/observability hardening.

### Added

- **External stage control API** — `POST /pipelines/{pipelineId}/stages` appends stages to an existing pipeline; `POST /stages/{stageId}/resume` resumes stages waiting for external approval
- **External API docs and tests** — OpenAPI coverage plus endpoint and validation tests for the new stage-control flow
- **Pipeline context search** — pipeline search now matches pipeline context item keys and values
- **Context value viewer** — dashboard support for inspecting larger or structured pipeline context payloads

### Changed

- **Active timeout handling** — timeout enforcement now covers active stages instead of pending-only flows, while keeping compatibility aliases for existing callers
- **RabbitMQ topology management** — standardized `/dev`, `/test`, and `/prod` vhosts; added topology ownership configuration and worker bootstrap metadata; improved mismatch handling
- **Worker dispatch loop** — publishing now supports faster back-to-back stage dispatch when work is available
- **Trace ID normalization** — pipeline trace IDs are derived from incoming `traceparent` headers when present, otherwise generated as valid W3C trace IDs

### Fixed

- **Observability defaults** — corrected Tempo service references, OTLP endpoint defaults, receiver bindings, and moved Grafana to port `3100` to avoid common local conflicts
- **Compose wiring** — corrected the worker's Tempo dependency in the registry compose stack
- **SQLite concurrency** — stage approval resume now retries transient SQLite lock conflicts instead of surfacing deadlock errors
- **Frontend cleanup** — fixed context viewer lint and formatting issues

## [0.1.0-preview.1] - 2026-03-03

First public preview release.

### Added

#### Core pipeline engine
- **Pipeline API** — create multi-stage pipelines; track execution state (`NotStarted` → `Pending` → `Running` → `Completed` / `Failed`)
- **Stage execution** — sequential stage execution with configurable retry (max attempts and interval per stage)
- **Rerun / skip** — rerun a failed stage or skip it to unblock the pipeline without restarting from scratch
- **Pending watchdog** — stages stuck in `Pending` beyond a configurable timeout are automatically marked `Failed`

#### API
- **Internal API** (`:8080`) — JWT/cookie auth for the web dashboard. Endpoints: auth, pipelines, stages, applications, API keys, workers, policies, observability, WebSocket (`/ws`), health (`/healthz`, `/readyz`), version (`/version`)
- **External API** (`:8081`) — API-key auth (`X-API-Key`) for SDK clients and external workers. Pull-based job gateway, log submission, worker lifecycle endpoints, version (`/version`)

#### Workers
- **Worker protocol** — bootstrap (receive session token and queue topology), heartbeat, event reporting, graceful shutdown with session TTL enforcement
- **Built-in worker** (`pipelogiq-worker`) — publisher, result consumer, status consumer, pending watchdog, Prometheus metrics on `:9090`

#### Dashboard
- **React dashboard** — pipeline list, pipeline detail with stage logs and context, worker monitoring, settings, observability config; real-time updates via WebSocket

#### Observability
- **OpenTelemetry trace propagation** — `traceparent` header forwarded through the job gateway to external workers
- **Observability bridge** — integration config for Grafana/Tempo, Sentry, and Datadog; connection testing from the dashboard
- **Prometheus metrics** — counters for stage lifecycle events and external API operations

#### Infrastructure
- **`pipelogiq-app` container** — single image bundling the React dashboard (nginx) and the API server (supervisord). nginx serves the dashboard on `:3300` and proxies `/api/` and `/ws` to the co-located API at `localhost:8080`, eliminating service-name coupling
- **`pipelogiq-worker` container** — separate image for the built-in worker; Prometheus metrics on `:9090`
- **Liquibase auto-migration** — `pipelogiq-app-entrypoint.sh` runs `liquibase update` on startup before starting nginx and the API; controlled by `LIQUIBASE_ENABLED`
- **Docker Compose stack** — `docker-compose.build.yml` (build from source) and `docker-compose.registry.yml` (pre-built GHCR images), plus individual `docker-compose.infra.yml`, `docker-compose.app.yml`, `docker-compose.worker.yml` for component-level control
- **GHCR images** — `ghcr.io/pipelogiq/pipelogiq-app` and `ghcr.io/pipelogiq/pipelogiq-worker`; pinnable via `PIPELOGIQ_VERSION`
- **Grafana Tempo** — pre-configured datasource and trace explorer
- **SQLite fallback** — run Go services locally without Postgres (data stored at `apps/go/data/pipelogiq.db` when `DATABASE_URL` is unset)
- **Version endpoint** — `GET /version` returns build version, commit hash, and build date

#### Other
- **Action policies** (experimental) — CRUD for rate-limit, retry, timeout, and circuit-breaker policies
- **Dead-letter queue** — optional per-queue DLQ in RabbitMQ; configurable TTL (disabled by default)
- **Project documentation** — quickstart, architecture, observability, policy, contributing, and security docs

### Known Limitations

- `run_in_parallel_with` field is stored but not yet evaluated (use `depends_on` for parallel stage execution)
- RBAC roles are stored but not checked
- WebSocket endpoint has no authentication
- No published SDK; external workers must implement the HTTP protocol directly

[0.3.2-preview.3]: https://github.com/pipelogiq/pipelogiq/releases/tag/v0.3.2-preview.3
[0.3.2-preview.2]: https://github.com/pipelogiq/pipelogiq/releases/tag/v0.3.2-preview.2
[0.3.2-preview.1]: https://github.com/pipelogiq/pipelogiq/releases/tag/v0.3.2-preview.1
[0.1.0-preview.1]: https://github.com/pipelogiq/pipelogiq/releases/tag/v0.1.0-preview.1
[0.1.0-preview.2]: https://github.com/pipelogiq/pipelogiq/releases/tag/v0.1.0-preview.2
[0.3.1-preview.4]: https://github.com/pipelogiq/pipelogiq/releases/tag/v0.3.1-preview.4
[0.3.1-preview.3]: https://github.com/pipelogiq/pipelogiq/releases/tag/v0.3.1-preview.3
[0.3.1-preview.2]: https://github.com/pipelogiq/pipelogiq/releases/tag/v0.3.1-preview.2
[0.3.1-preview.1]: https://github.com/pipelogiq/pipelogiq/releases/tag/v0.3.1-preview.1
[0.3.0-preview.1]: https://github.com/pipelogiq/pipelogiq/releases/tag/v0.3.0-preview.1
[0.3.0-preview.2]: https://github.com/pipelogiq/pipelogiq/releases/tag/v0.3.0-preview.2
[0.3.0-preview.3]: https://github.com/pipelogiq/pipelogiq/releases/tag/v0.3.0-preview.3
[0.3.0-preview.4]: https://github.com/pipelogiq/pipelogiq/releases/tag/v0.3.0-preview.4
[0.3.0-preview.5]: https://github.com/pipelogiq/pipelogiq/releases/tag/v0.3.0-preview.5
