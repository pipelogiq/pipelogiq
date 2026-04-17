# v0.3.0-preview.4

Fifth preview release focused on resilience: graceful worker shutdown, orphaned stage recovery, and build provenance.

## What's Changed

### Graceful Worker Shutdown

The Pipelogiq worker now shuts down cleanly on SIGTERM. All internal goroutines (publisher, result/status consumers, timeout watcher, orphan recovery) are tracked with a `WaitGroup` and allowed to finish before the process exits. Previously, a SIGTERM would cancel all contexts immediately, causing running stage handlers to fail mid-execution and leaving stages stuck in `Running`.

RabbitMQ message handler contexts now use `context.WithoutCancel()` so in-flight handlers can complete their work — including publishing results back to the engine — even after the parent context is cancelled.

### Orphaned Stage Recovery

A new `runOrphanRecovery` goroutine periodically scans for stages stuck in `Running` or `Pending` beyond a configurable threshold (half the active timeout, minimum 30s). Stuck stages are reset to `NotStarted` with row-level locking, allowing the publisher to re-schedule them. This closes the gap where a worker crash could leave stages permanently stuck.

### Build Provenance

Docker images now carry version, commit, and build date metadata:

- **Build args**: `PIPELOGIQ_VERSION`, `PIPELOGIQ_COMMIT`, `PIPELOGIQ_BUILD_DATE` injected via `ldflags` at compile time
- **OCI labels**: `org.opencontainers.image.version`, `.revision`, `.created`
- **Runtime env vars**: `APP_VERSION`, `APP_COMMIT`, `APP_BUILD_DATE` as fallback for `GET /version`
- **CI**: `release.yml` workflow now extracts and passes all three build args automatically

The `version.Get()` function resolves fields using a priority chain: ldflags > env vars > sentinel defaults.

## Companion SDK Release

**pipelogiq-sdk-net v0.3.0-preview.5** includes matching .NET-side changes:

- **Second-model critic** — `AgentCriticHandler` reviews think decisions with a separate LLM (OpenAI or Claude) before execution. Three modes: `CriticOnFinal`, `CriticOnMutating`, `CriticOnEveryStep`. Configurable per-pipeline via `AgentRunOverrides`
- **Graceful .NET worker shutdown** — consumer cancellation, in-flight job draining with configurable `DrainGracePeriod`, drain-safe publish tokens for critical operations

## Upgrade Notes

- No database migrations required
- Rebuild and redeploy both `pipelogiq-app` and `pipelogiq-worker`
- Upgrade `pipelogiq-sdk-net` to `0.3.0-preview.5` for matching graceful shutdown
- Set `PIPELOGIQ_VERSION`, `PIPELOGIQ_COMMIT`, `PIPELOGIQ_BUILD_DATE` env vars or build args for accurate version info

## Files Changed

- `apps/go/internal/worker/worker.go` — WaitGroup-based shutdown, orphan recovery goroutine
- `apps/go/internal/store/store.go` — `RecoverOrphanedStages()` implementation
- `apps/go/internal/mq/rabbit.go` — `context.WithoutCancel()` for handler contexts
- `apps/go/internal/version/version.go` — env var fallback for build fields
- `infra/docker/Dockerfile.app` — build args, ldflags, OCI labels, env vars
- `infra/docker/Dockerfile.worker` — build args, ldflags, OCI labels, env vars
- `infra/compose/docker-compose.*.yml` — build args and env vars for all compose variants
- `.github/workflows/release.yml` — commit and build date extraction

**Full Changelog**: https://github.com/pipelogiq/pipelogiq/compare/v0.3.0-preview.3...v0.3.0-preview.4
