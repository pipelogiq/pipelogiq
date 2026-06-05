# v0.3.2-preview.6

Sixth `0.3.2` preview release focused on day-to-day pipeline operations in the dashboard, clearer retry/failure visibility, and faster pipeline list/search performance.

## What's Changed

### Dashboard Bulk Operations

The pipeline list now supports bulk operational actions for selected rows:

- Pause selected pipelines
- Resume selected pipelines
- Rerun failed stages from selected pipelines
- Skip failed stages from selected pipelines

Bulk actions are intentionally scoped to the pipeline list. The pipeline detail view keeps individual stage controls only, so stage inspection remains focused and uncluttered.

### Retry And Throttle Visibility

The dashboard now distinguishes retry and throttling states instead of flattening them into generic pending UI:

- `RetryScheduled` is shown as `Rescheduled`
- `Throttled` is shown as `Throttled`
- Next scheduled run time is shown in list/status surfaces and pipeline inspection

This makes delayed execution easier to distinguish from ordinary pending work.

### Failure History Marker

Stages that failed and later recovered now keep an icon-only failure-history marker. This preserves incident context after reruns while keeping the table and side-panel UI compact.

### Pipeline List And Search Performance

Pipeline list rendering and search were tightened:

- Visible-page stages are loaded in one batch query
- Pipeline context and keywords are loaded in batch for the current page
- Stage failure history is loaded in a bounded batch query for the displayed stages
- Dashboard search precomputes matching pipeline IDs instead of using correlated text-search subqueries
- Search totals are returned through the page query for active searches, avoiding a second heavy text scan

### Database Indexes

New indexes support the faster list/search paths:

- Pipeline created/status/application lookup indexes
- Stage lookup by pipeline
- Pipeline keyword and context lookup indexes
- Stage log lookup by stage and created time
- PostgreSQL trigram search indexes for pipeline names, stage names/descriptions, keyword values, and pipeline context keys/values

## Upgrade Notes

- Rebuild and redeploy `pipelogiq-app`
- Run Liquibase migrations so the new indexes are present
- The PostgreSQL migration enables `pg_trgm` if needed
- Local stacks with `LIQUIBASE_ENABLED=false` need the migration applied manually before search-performance testing
- No worker or SDK upgrade is required for these dashboard/API changes

## Files Changed

- `apps/go/internal/api/handlers.go` — bulk action endpoint handling
- `apps/go/internal/api/server.go` — bulk action route
- `apps/go/internal/store/pipeline_ext.go` — optimized pipeline list/search loading
- `apps/go/internal/store/store.go` — batched context, keyword, and failure-history loading
- `apps/go/internal/types/api.go` — bulk action and stage metadata response types
- `apps/web/src/pages/Pipelines.tsx` — pipeline-list bulk toolbar
- `apps/web/src/components/pipelines/PipelineTable.tsx` — selection and status/failure markers
- `apps/web/src/components/pipelines/PipelineSidePanel.tsx` — retry timing and icon-only failure marker
- `apps/web/src/components/ui/status-badge.tsx` — rescheduled/throttled status display
- `database/changelog.xml` — lookup and search indexes
- `docs/openapi.external.yaml`, `README.md`, `docs/` — release metadata and documentation refresh

**Full Changelog**: https://github.com/pipelogiq/pipelogiq/compare/v0.3.2-preview.5...v0.3.2-preview.6
