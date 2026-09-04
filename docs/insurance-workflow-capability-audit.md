# Insurance workflow capability audit

This audit evaluates Pipelogiq as the orchestrator for a critical insurance workflow:

```text
Payment confirmed
  -> Validate claim prerequisites
  -> Ensure claim
  -> Publish insurance data to MedicalCase
  -> Mark arrival allowed
```

The cancellation workflow is a separate pipeline:

```text
Booking cancelled or payer changed
  -> Ensure external insurance reservation cancelled
  -> Persist cancellation result
```

The audit baseline was server `v0.3.2-preview.6` with
`pipelogiq-sdk-net 0.3.2-preview.5`. The reliability additions described in
this document ship in server `v0.4.0-preview.1` and SDK `0.4.0-preview.1`.

## Verdict

Pipelogiq is suitable for this workflow only as an **at-least-once-oriented
orchestrator around an idempotent consumer handler**. It does not provide
exactly-once execution of an external claim, cancellation, or refund.

The normal, non-event pipeline path now has the primitives needed to:

- create one pipeline after an ambiguous client timeout;
- classify transient and terminal failures;
- expose status, attempts, timestamps, and the last error code;
- fence stale execution results and avoid two different workers holding the
  same valid lease;
- recover a database-to-broker dispatch interrupted by a process failure;
- redact explicitly sensitive context values from status and UI payloads; and
- cancel a pipeline cooperatively.

These primitives do not remove the ambiguity between an external side effect
and the consumer persisting or returning its result. `EnsureClaim` must retain
a permanent external idempotency key and claim-attempt state in the consumer
database. After `OutcomeUnknown`, it must query the previous claim status
before considering another POST.

Use a normal `WithAction` pipeline created in response to the payment event.
Do not model the claim stage as `isEvent`, and do not keep a pipeline waiting
for payment.

## Pre-change capability findings

The following table records the `v0.3.2-preview.6` / SDK
`0.3.2-preview.5` findings. “Proposed action” records the action taken in the
target preview or an explicitly retained limitation.

| Requirement | Current support | Evidence | Gap | Proposed action |
|---|---|---|---|---|
| Delivery semantics | Partial. RabbitMQ consumers use manual acknowledgement, durable queues, and persistent messages, so an already published delivery can be redelivered. Pipeline scheduling commits `Pending` before publishing. | Scheduler transaction and dispatch payload: [`store.go`](../apps/go/internal/store/store.go#L726-L966). Publish then record dispatch: [`worker.go`](../apps/go/internal/worker/worker.go#L241-L322). Manual ACK/NACK: [`rabbit.go`](../apps/go/internal/mq/rabbit.go#L145-L199). | The baseline had no atomic DB/broker boundary, publisher confirmation, persistent execution lease, or fencing token. A crash after the external side effect but before `StageResult` can execute the handler again. | Added publisher confirms, `executionId`, persistent lease/renewal, stale-result fencing, undispatched-stage recovery, and expired-lease recovery. Duplicates remain possible; a transactional outbox is the recommended long-term improvement. |
| Idempotent pipeline creation | No on legacy `POST /pipelines`. Each accepted call inserts a new row and returns its ID. | Legacy create remains intentionally unchanged: [`external.go`](../apps/go/internal/api/external.go#L205-L280), [`store.go`](../apps/go/internal/store/store.go#L123-L163). | A response timeout after commit leaves the caller unable to distinguish success from failure; retrying creates a duplicate. | Added opt-in `POST /pipelines/idempotent`, an application-scoped unique key, canonical request hash, deterministic conflict response, and lookup by key. |
| Pipeline status API | Partial. `SendAsync` already receives the pipeline response and ID, and the server exposes `GET /pipelines/{pipelineId}`. Full detail already includes stages, context, logs, and timestamps. | External status handler: [`external.go`](../apps/go/internal/api/external.go#L498-L549). Full-detail loading: [`store.go`](../apps/go/internal/store/store.go#L413-L439). | The SDK response did not expose all server fields. There was no application-facing lookup by idempotency key, and stage attempt, last error code, failure disposition, and explicit terminal flags were absent. External keyword search is still not exposed. | Added SDK status DTO coverage, stage execution/retry attempts, `nextRetryAt`, last error code, failure disposition, terminal flags, and `POST /pipelines/by-idempotency-key`. Keep keyword search as an internal dashboard operation. |
| Retry classification and delay | Partial. `MaxRetries`, `RetryInterval`, `TimeOut`, and policy-based `retryOn`/backoff existed. A generic stage error could fall through to broad retries. | Result state machine: [`store.go`](../apps/go/internal/store/store.go#L1296-L1657). Existing policy backoff and matching: [`policy_runtime.go`](../apps/go/internal/store/policy_runtime.go#L706-L782). | No explicit terminal/retryable result disposition in the SDK. Inline `StageOptions` could not filter error codes or select exponential/linear backoff and jitter. A configured policy mismatch could fall through to generic stage retry. | Added `retryable`, terminal-code protection, `retryOnErrorCodes`, fixed/linear/exponential backoff, cap, jitter, and status metadata. A configured policy mismatch is terminal and no longer falls back. |
| Stable execution metadata | Partial. Pipeline ID, stage ID, trace ID, and stage span ID existed. Handler scopes were separate, but there was no execution token or attempt identity in the wire contract. | Target wire fields: [`messages.go`](../apps/go/internal/types/messages.go#L5-L38). Scheduling and attempt allocation: [`store.go`](../apps/go/internal/store/store.go#L893-L966). | Redelivery could not be distinguished from a new execution. `IStageContext` did not expose attempt, pipeline idempotency key, effective timeout, or cancellation token. `tracestate` was not exposed as technical execution metadata. | Added optional execution metadata and a backward-compatible SDK execution-context extension. A supplied `tracestate` context value is propagated explicitly; it remains empty when the producer does not supply one. |
| Context durability and security | Partial. Initial context and stage definitions are committed transactionally; result context is persisted with the stage result. Context is sent raw to the handler and status/UI previously exposed raw values. | Transactional create: [`store.go`](../apps/go/internal/store/store.go#L123-L200). Result/context transaction: [`store.go`](../apps/go/internal/store/store.go#L1462-L1653). | Handler-memory changes are lost if no result is committed. Parallel stages can read the same snapshot and overwrite the same key last-writer-wins. There was no sensitive marker or size validation. Context is not a safe business source of truth. | Added `isSensitive`, monotonic sensitive marking, public-response/WebSocket redaction, log substitution for known marked values, and create-request limits. Do not put claim content, tokens, or PII in context or logs. Parallel context write conflict detection remains a gap. |
| Cancellation and recovery | Partial. Internal authenticated operations can pause/resume pipelines and rerun/skip stages. Delayed retry uses `next_retry_at`. Legacy orphan recovery resets stale running stages. | Internal routes: [`server.go`](../apps/go/internal/api/server.go#L112-L125). Manual rerun: [`pipeline_ext.go`](../apps/go/internal/store/pipeline_ext.go#L287-L402). Recovery loop: [`worker.go`](../apps/go/internal/worker/worker.go#L190-L239). | No application-scoped cancel API. Running handlers could not observe cancellation. Recovery did not have a lease owner or stale-result fence. Programmatic terminal repair remains an operator/internal-API action. | Added application-scoped cancellation, lease renewal, cooperative cancellation token, expired-lease recovery, and result fencing. Cancellation cannot undo an external side effect already started. |
| Backward compatibility | Existing builder, `WithAction`, `StageOptions`, `IStageHandler`, `IStageContext`, and `StageResult.Error/Success` contracts were in active use. Server and SDK were both preview versions. | Legacy create route remains available: [`external.go`](../apps/go/internal/api/external.go#L158-L167). Additive wire fields: [`messages.go`](../apps/go/internal/types/messages.go#L5-L38). | Requiring new fields on the old endpoint or mandatory members on `IStageContext` would break SDK `0.3.2-preview.5`. New server queries also require schema migration before process startup. | Kept legacy behavior, made new wire fields optional, accepts legacy results without fencing metadata, and uses nullable/defaulted additive columns. Strong reliability behavior requires the target SDK. |

## Post-change guarantees and boundaries

### Pipeline creation

`POST /pipelines/idempotent` uses a database uniqueness constraint on
`(application_id, idempotency_key)` as the concurrency authority. Sequential
and concurrent equivalent requests return the same pipeline ID. A key reused
with a different canonical request returns HTTP `409`.

The key is scoped to the application resolved from `X-API-Key`. It has no
time-to-live in Pipelogiq: it remains reserved while its pipeline row remains
in the database. The schema and transaction are shown in
[`changelog.xml`](../database/changelog.xml#L1077-L1094) and
[`pipeline_idempotency.go`](../apps/go/internal/store/pipeline_idempotency.go#L27-L124).

Trace IDs, `traceparent`, and `tracestate` are excluded from the request hash so
that a transport retry may carry a new trace without causing a conflict.
Stage order and all business-relevant pipeline content still participate in
the hash.

The legacy `POST /pipelines` remains non-idempotent by design. This preserves
old clients but means it must not be used for the critical claim path.

### Delivery and worker failure

The normal scheduler allocates a new `executionId`, increments `attempt`, and
commits `Pending` before publishing. Server-side publisher confirms are awaited
before `dispatched_at` is recorded
([`rabbit.go`](../apps/go/internal/mq/rabbit.go#L70-L115),
[`worker.go`](../apps/go/internal/worker/worker.go#L272-L306)).

If the database commit happened but no confirmed dispatch was recorded, the
recovery loop returns the stage to `NotStarted`. If the broker confirmed the
publish but writing `dispatched_at` failed, both the old delivery and a later
delivery can exist. Their execution IDs differ, so a target-SDK worker rejects
the stale delivery. This is duplicate-safe orchestration, not an atomic
database/broker transaction.

A valid lease prevents two different workers from executing the same execution
concurrently. The default lease is 60 seconds and is renewed while the handler
runs. After lease expiry another worker can pick up the stage with a new
execution ID. The old process may still be running or may already have made an
external call; Pipelogiq can reject its late result, but cannot roll back that
call. See [`stage_lease.go`](../apps/go/internal/store/stage_lease.go#L16-L164)
and the recovery methods in
[`stage_lease.go`](../apps/go/internal/store/stage_lease.go#L285-L350).

The server result transaction treats a duplicate result, a stale execution ID,
an old attempt, and a result arriving after terminal pipeline state as a safe
no-op
([`store.go`](../apps/go/internal/store/store.go#L1365-L1396)).

This stronger path applies to normal, non-event stages. The compatibility
single-stage `isEvent` auto-publish path does not allocate execution metadata
or a lease
([`external.go`](../apps/go/internal/api/external.go#L461-L495)), and event
stages are excluded from the normal scheduler
([`store.go`](../apps/go/internal/store/store.go#L738-L781)). Do not use
`isEvent` for insurance claim or cancellation side effects.

### Failure classification

The target contract supports an explicit nullable `retryable` field:

- `false` is terminal;
- `true` makes a non-terminal error eligible for the configured retry policy;
- omitted preserves the legacy result contract.

The server always treats these codes as terminal, even if a client
accidentally marks them retryable:

- `BUSINESS_REJECTED`;
- `VALIDATION_ERROR`;
- `INVALID_STATE`;
- `MISSING_REQUIRED_DATA` and `REQUIRED_DATA_MISSING`.

Existing agent terminal codes `TOOL_LOOP`, `BUDGET_EXCEEDED`, and
`LLM_INVALID_REQUEST` remain terminal. The classification guard is in
[`store.go`](../apps/go/internal/store/store.go#L1660-L1673).

Transient failures are retried only when they are eligible under the selected
policy or `StageOptions.retryOnErrorCodes`. For the insurance workflow,
configure at least:

- `TIMEOUT`;
- `UPSTREAM_ERROR`;
- `RATE_LIMIT_EXCEEDED`;
- `TRANSPORT_UNAVAILABLE`.

`MaxRetries` is the number of retries after the initial execution.
`RetryInterval` is the base delay in seconds. Backoff may be `fixed` (default),
`linear`, or `exponential`; `MaxRetryInterval` caps the delay and `Jitter`
adds up to approximately ±10%. The stage remains `RetryScheduled` until
`nextRetryAt`. A terminal stage stops the pipeline by default. A later stage
runs after failure only when `RunNextIfFailed` is explicitly enabled.

### Status

`GET /pipelines/{pipelineId}` and lookup by idempotency key return:

- pipeline status, creation/finish timestamps, and `isTerminal`;
- stages and stage terminal state;
- `attempt` (execution dispatch count);
- `retryAttempt` (automatic retry count);
- start, finish, and `nextRetryAt` timestamps;
- `lastErrorCode` and `failureDisposition`;
- stage logs, input/output, keywords, and redacted context.

The status DTO is defined in
[`api.go`](../apps/go/internal/types/api.go#L55-L100), and the store projection
is in [`store.go`](../apps/go/internal/store/store.go#L533-L576).

Application-facing lookup by keyword is not part of this change. The internal
dashboard can filter/search keywords
([`pipeline_ext.go`](../apps/go/internal/store/pipeline_ext.go#L90-L147)).
For recovery after an ambiguous create response, use the idempotency-key lookup
instead.

### Context and redaction

`isSensitive` is durable and cannot be downgraded from true by a later context
update. Public API responses, internal dashboard responses, and WebSocket
pipeline updates replace marked values with `[REDACTED]`
([`redaction.go`](../apps/go/internal/types/redaction.go#L5-L66),
[`worker.go`](../apps/go/internal/worker/worker.go#L525-L542)).
The dedicated dashboard stage-log endpoint applies the same substitution.

This is a guardrail, not a data-loss-prevention system:

- raw context must reach the selected handler;
- unmarked data remains visible;
- transformed or encoded secret values may not match literal-value redaction;
- application logs outside the stage-result path are the consumer's
  responsibility; and
- stage input/output can contain sensitive data independently of context.

The idempotent create endpoint accepts at most 64 context items, a 300-character
key, and a 64 KiB value per item; the full request body is limited to 1 MiB
([`external.go`](../apps/go/internal/api/external.go#L1271-L1287),
[`external.go`](../apps/go/internal/api/external.go#L1474-L1501)).
Equivalent limits are not yet enforced on every broker-originated result
update.

For this scenario, keep context limited to non-secret technical identifiers:

- `tenantId`;
- `privateInsuranceProcessId`;
- `orderServiceId`;
- an opaque technical correlation key.

Do not store claim content, eligibility/coverage responses, access tokens,
personal data, or the only copy of business state in pipeline context.

### Cancellation and recovery

`POST /pipelines/{pipelineId}/cancel` atomically marks the pipeline and every
unfinished stage `Cancelled` and clears the execution token and lease. Repeated
cancellation of the same cancelled pipeline is idempotent. Cancelling an
already completed or failed pipeline returns a conflict. Application ownership
is enforced
([`pipeline_cancel.go`](../apps/go/internal/store/pipeline_cancel.go#L14-L100)).

Cancellation of a running handler is cooperative. Lease renewal is denied once
the pipeline is terminal, and the target SDK cancels the handler execution
token. A handler that ignores cancellation can continue; its late result is
fenced, but an external side effect cannot be undone.

Automatic recovery covers confirmed retry delays, DB-to-broker dispatch gaps,
expired leases, and the legacy stale-running path. Manual rerun/repair is still
an internal authenticated dashboard/API operation rather than a consumer SDK
operation.

### Persistence

Pipeline, stage, retry, lease, and context state is stored in PostgreSQL.
RabbitMQ queues are durable and messages are persistent. Restart durability
therefore depends on durable storage being configured for both products; no
Redis state is involved.

The registry and infrastructure compose files mount PostgreSQL and RabbitMQ
volumes
([`docker-compose.registry.yml`](../infra/compose/docker-compose.registry.yml#L18-L49),
[`docker-compose.infra.yml`](../infra/compose/docker-compose.infra.yml#L7-L40)).
The source-build all-in-one compose file currently mounts PostgreSQL but not
RabbitMQ
([`docker-compose.build.yml`](../infra/compose/docker-compose.build.yml#L17-L52)).
Do not use an ephemeral RabbitMQ data directory for the critical production
workflow.

## Consumer obligations for `EnsureClaim`

Pipelogiq does not own the insurance claim state machine. The consumer must:

1. Persist the workflow intent and an opaque, stable pipeline idempotency key
   in its own database before calling Pipelogiq.
2. Persist a stable external claim request/idempotency key before the first
   POST.
3. On a first execution with no prior claim attempt, perform one POST using
   that external key.
4. Treat a timeout or lost response as `OutcomeUnknown` and durably retain the
   attempted external request identity.
5. On every later execution of `OutcomeUnknown`, call the external status GET
   first. A second blind POST is a handler bug.
6. Return success without an external call when local business state is already
   confirmed.
7. Return a terminal result for a durable business rejection or invalid local
   state.
8. Return a classified transient result for timeout, rate limit, transport, or
   upstream availability failures.
9. Make cancellation and publication stages idempotent too.
10. Treat the consumer database, not pipeline context, as the business source
    of truth.

The external insurance service should also honor the stable claim key. If it
does not provide an idempotent POST or a status lookup by request identity,
Pipelogiq cannot make an ambiguous claim submission safe.

## Remaining risks and recommended next architecture

The critical residual risk is the non-atomic transition from PostgreSQL to
RabbitMQ. Publisher confirms plus dispatch recovery prevent the common
“committed Pending but never published” stall, and execution fencing makes the
confirm-persist race safe for a target-SDK worker. They deliberately permit
duplicate delivery.

A transactional outbox is the recommended next server-side improvement:

1. Insert the execution dispatch and outbox row in the same database
   transaction.
2. Claim outbox rows with row locking and publish with confirms.
3. Mark the outbox row sent only after broker confirmation.
4. Retain `executionId` as the consumer fence and deduplication identity.
5. Roll out additively, with the current scheduler reading both legacy
   unsent-stage state and outbox rows until migration is complete.

This would reduce the recovery race but still would not make arbitrary external
side effects exactly once. Consumer idempotency and status reconciliation
remain mandatory.

Inline policies are imported after the pipeline row is created and therefore
do not share the idempotent-create transaction. An import failure can leave an
otherwise valid pipeline without the intended inline policy. The critical
insurance example consequently expresses retry behavior through durable
`StageOptions`; making inline-policy import atomic would require a separate
storage/API redesign.
