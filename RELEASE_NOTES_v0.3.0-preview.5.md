# v0.3.0-preview.5

Sixth preview release focused on stage dispatch performance: event-driven scheduling, batch dispatch, dependency-based parallel execution, and reduced poll latency.

## What's Changed

### Event-Driven Stage Dispatch

The publisher goroutine no longer relies solely on polling to discover ready stages. When a stage result or status update is processed by the consumer goroutines, they immediately signal the publisher via an internal channel. The publisher wakes up on this signal and dispatches the next eligible stage within milliseconds — down from up to 1 second of poll latency in prior releases.

The poll interval (`WORKER_POLL_INTERVAL`) remains as a fallback for edge cases where stages become ready without a consumer trigger (e.g. retry timers, orphan recovery). The default has been reduced from 1 second to 200 milliseconds and the minimum from 100ms to 50ms.

### Batch Stage Dispatch

The publisher now dispatches up to 10 stages per cycle without pausing between them. When at least one stage is dispatched, it immediately checks for more work instead of waiting for the next poll tick. This dramatically improves throughput when multiple stages become eligible simultaneously — for example, after a fan-out stage completes or when stages from separate pipelines are ready at the same time.

### Dependency-Based Parallel Stage Execution

The `depends_on` column in `stage_options`, which was previously stored but ignored, is now fully evaluated during stage scheduling. Stages with explicit `depends_on` only wait for the named dependency stages to reach a terminal state (Completed, Skipped, or Failed with `runNextIfFailed`), enabling fan-out, fan-in, and diamond DAG patterns within a single pipeline.

Stages without `depends_on` retain the existing strictly sequential behavior for backward compatibility.

**Supported patterns:**

```
Fan-out:      A → B(depends_on=A), C(depends_on=A)         → B and C run in parallel after A
Diamond:      A → B(depends_on=A), C(depends_on=A) → D(depends_on=B,C)  → D waits for both B and C
Sequential:   A → B → C (no depends_on)             → unchanged, runs in order
```

### Scheduling SQL Refactored

The `GetStageToExecute` CTE now joins `stage_options` to read `depends_on` and `run_next_if_failed` in a single pass instead of using nested correlated subqueries. The blanket `NOT EXISTS (status = Pending)` check that prevented any form of parallel dispatch within a pipeline has been removed; individual dependency checks are sufficient and more correct.

### Test Coverage

New `stage_scheduling_test.go` provides comprehensive coverage:

- **TestSequentialScheduling** — backward compatibility for pipelines without `depends_on`
- **TestDependsOnParallelDispatch** — fan-out: A → B, C dispatched in parallel
- **TestDependsOnDiamond** — diamond: A → B, C → D waits for both B and C
- **TestMixedSequentialAndDependsOn** — mixed sequential and dependency modes in one pipeline
- **TestCrossPipelineDispatch** — batch dispatch from independent pipelines
- **TestEmptyPipelineReturnsNil** — no crash on empty state
- **BenchmarkGetStageToExecute** — throughput from 1×5 to 100×1 pipelines
- **BenchmarkParallelDependsOn** — fan-out of 20 stages from a single root

## Companion SDK Release

**pipelogiq-sdk-net v0.3.0-preview.9** includes:

- **JsonElement serialization fix** — `BaseApiClient` now uses `SdkJsonSerializer` with custom Newtonsoft converters for `System.Text.Json.JsonElement`, fixing the `{ "ValueKind": 3 }` bug that caused all agent tool calls via `AppendAgentStagesAsync` to fail
- **Mandatory lineItems enforcement** — `saveBudgetResult` tool definition now marks `lineItems` as required with clear rejection language, preventing the LLM from taking the totals-only fallback path

## Upgrade Notes

- No database migrations required
- Rebuild and redeploy `pipelogiq-worker` to pick up all dispatch performance improvements
- Existing pipelines are unaffected — stages without `depends_on` continue to execute sequentially
- To use parallel dispatch, set `depends_on` in stage options when creating pipeline stages (comma-separated stage names)
- Optionally tune `WORKER_POLL_INTERVAL` (new default 200ms; previous default was 1s)
- Upgrade `pipelogiq-sdk-net` to `0.3.0-preview.9` for the JsonElement serialization fix

## Files Changed

- `apps/go/internal/worker/worker.go` — event-driven wake channel, batch dispatch loop, reduced default poll interval
- `apps/go/internal/store/store.go` — `GetStageToExecute` SQL refactored for `depends_on` evaluation
- `apps/go/internal/store/stage_scheduling_test.go` — new scheduling test suite and benchmarks

**Full Changelog**: https://github.com/pipelogiq/pipelogiq/compare/v0.3.0-preview.4...v0.3.0-preview.5
