# Reliable execution and upgrade guide

This document describes the additive reliability contract targeted by
Pipelogiq server `v0.4.0-preview.1` and `pipelogiq-sdk-net
0.4.0-preview.1`.


## Guarantee model

Pipelogiq provides at-least-once-oriented scheduling with duplicate-delivery
protection for normal, non-event stages when the target SDK is used:

- PostgreSQL is the durable orchestration state;
- RabbitMQ delivery and redelivery are acknowledged explicitly;
- server and target-SDK publishes use persistent messages and publisher
  confirms;
- an execution lease prevents different workers from holding the same active
  execution concurrently;
- execution ID and attempt fence stale results; and
- recovery reschedules unconfirmed dispatches and expired leases.

It does not provide exactly-once external side effects. A handler can complete
an external operation and crash before its result is durably accepted. It can
also keep running after its lease expires or after cooperative cancellation.
Every side-effecting handler must therefore be idempotent or reconcile a
previous unknown outcome before issuing another command.

The reliability contract assumes PostgreSQL and RabbitMQ use persistent
volumes, backups, and an availability configuration appropriate for the
workflow. Loss of either durable store is outside this guarantee.

## Upgrade order

1. Back up PostgreSQL and confirm RabbitMQ uses persistent storage.
2. Apply the additive Liquibase changes in
   [`database/changelog.xml`](../database/changelog.xml#L1077-L1158).
3. Deploy server/API and worker target `v0.4.0-preview.1`.
4. Verify `/version`, health endpoints, migrations, and RabbitMQ connectivity.
5. Install `pipelogiq-sdk-net 0.4.0-preview.1` from NuGet.
6. Upgrade critical workers and the pipeline-creating consumer to SDK
   `0.4.0-preview.1`.
7. Opt critical workflows into idempotent creation and classified retries.
8. Keep legacy workers only for non-critical pipelines during the transition.

Migrate the database before starting a new server or worker because target code
queries the new columns. The migration is additive:

- old pipelines have a null idempotency key;
- old stages start with execution attempt `0`;
- old context items default to `is_sensitive=false`; and
- new lease, dispatch, error, and retry-policy columns are nullable.

An old server can ignore the added columns if an application rollback is
needed. A target SDK must not be deployed before the target server, because the
new routes do not exist on `v0.3.2-preview.6`.

## Idempotent pipeline creation

### SDK contract

The target SDK keeps `PipelineBuilder.Create`, `WithAction`, and `SendAsync`
unchanged and adds an opt-in:

```csharp
var pipeline = await PipelineBuilder
    .Create("private-insurance-claim")
    .WithIdempotencyKey(pipelineCreationKey)
    .AddContextItem("tenantId", tenantId)
    .AddContextItem("privateInsuranceProcessId", processId)
    .AddContextItem("orderServiceId", orderServiceId)
    .AddKeyword("workflow", "private-insurance-claim")
    .WithAction<ValidateClaimPrerequisitesHandler>("validate-prerequisites")
    .WithAction<EnsureClaimHandler>("ensure-claim", new StageOptions
    {
        MaxRetries = 5,
        RetryInterval = 10,
        RetryOnErrorCodes =
        [
            StageErrorCodes.Timeout,
            StageErrorCodes.UpstreamError,
            StageErrorCodes.RateLimitExceeded,
            StageErrorCodes.TransportUnavailable,
        ],
        Backoff = "exponential",
        MaxRetryInterval = 300,
        Jitter = true,
        TimeOut = 60,
    })
    .WithAction<PublishInsuranceDataHandler>("publish-insurance-data")
    .WithAction<MarkArrivalAllowedHandler>("mark-arrival-allowed")
    .SendAsync(cancellationToken);

await consumerStore.SavePipelineIdAsync(processId, pipeline.Id, cancellationToken);
```

This sample shows the orchestration contract only. The handler and
`consumerStore` are consumer-owned types; Pipelogiq core does not define
insurance entities.

When `WithIdempotencyKey` is absent, `SendAsync` retains the legacy
non-idempotent endpoint. When it is present, the SDK uses the fail-safe
endpoint. The SDK methods are:

- `GetPipelineAsync(pipelineId)`;
- `GetPipelineByIdempotencyKeyAsync(key)`; and
- `CancelPipelineAsync(pipelineId)`.

### HTTP contract

Create:

```http
POST /pipelines/idempotent
X-API-Key: <application API key>
Content-Type: application/json

{
  "idempotencyKey": "opaque-stable-key",
  "name": "private-insurance-claim",
  "stages": [
    {
      "stageName": "ensure-claim",
      "stageHandlerName": "EnsureClaimHandler",
      "input": "{\"privateInsuranceProcessId\":\"process-1\"}",
      "options": {
        "maxRetries": 5,
        "retryInterval": 10,
        "timeOut": 60,
        "retryOnErrorCodes": [
          "TIMEOUT",
          "UPSTREAM_ERROR",
          "RATE_LIMIT_EXCEEDED",
          "TRANSPORT_UNAVAILABLE"
        ],
        "backoff": "exponential",
        "maxRetryInterval": 300,
        "jitter": true
      }
    }
  ],
  "pipelineContextItems": [
    {
      "key": "tenantId",
      "value": "\"tenant-1\"",
      "valueType": "System.String"
    }
  ]
}
```

A newly created pipeline returns HTTP `201`:

```json
{
  "pipeline": {
    "id": 273,
    "idempotencyKey": "opaque-stable-key",
    "status": "NotStarted",
    "isTerminal": false
  },
  "created": true,
  "wasExisting": false
}
```

An equivalent repeated request returns HTTP `200`, the same pipeline ID,
`created:false`, and `wasExisting:true`. A different canonical request under
the same key returns HTTP `409` with
`application/problem+json` type
`https://api.pipelogiq.dev/errors/idempotency-conflict`.

Lookup after an ambiguous HTTP outcome:

```http
POST /pipelines/by-idempotency-key
X-API-Key: <application API key>
Content-Type: application/json

{"idempotencyKey":"opaque-stable-key"}
```

The key is placed in the body rather than the URL to avoid common URL/access
log exposure. The response is the matching pipeline or HTTP `404`.

The uniqueness scope is the authenticated application. The same key may be
used by a different application. Pipelogiq does not expire the key
independently; it remains reserved for the lifetime of the pipeline row.
After trimming, the key is required, is limited to 200 Unicode characters, and
must not contain control characters.

The key is returned in status responses. Use an opaque value without tokens,
PII, claim content, or a raw business identifier. Persist it in the consumer
database before the first request.

The request hash ignores API/trace credentials and normalises keyword and
context ordering. A retry with a different trace can therefore resolve the
existing pipeline. Stage order and other pipeline content remain
business-significant; changing them under the same key returns the conflict
instead of silently accepting a different workflow.

## Status API

Get status by ID:

```http
GET /pipelines/273
X-API-Key: <application API key>
```

The server verifies application ownership and returns HTTP `403` for a
pipeline owned by another application. The response includes pipeline and
stage terminal flags, stages, timestamps, execution and retry attempts,
`nextRetryAt`, `lastErrorCode`, `failureDisposition`, logs, keywords, and
redacted context.

Interpret the counters as follows:

| Field | Meaning |
|---|---|
| `attempt` | One-based execution-dispatch count. It increments when the scheduler allocates a new execution token, including recovery. |
| `retryAttempt` | Number of automatic retries scheduled after failed results. |
| `lastErrorCode` | Most recently persisted structured stage error code. |
| `failureDisposition` | `retryable` while a retry is scheduled, or `terminal` for a final failure. |
| `nextRetryAt` | Earliest UTC time at which a `RetryScheduled` stage is eligible again. |
| `isTerminal` | Explicit terminal projection on both pipeline and stage. |

Pipeline terminal statuses are `Completed`, `Failed`, and `Cancelled`. Stage
terminal statuses are `Completed`, `Failed`, `Skipped`, and `Cancelled`.

There is no external/SDK keyword-search endpoint in this preview. Use
idempotency-key lookup for create recovery and store the returned pipeline ID
in the consumer database.

## Retry and terminal results

The target SDK preserves `StageResult.Success` and `StageResult.Error` and adds
explicit factories:

```csharp
return StageResult.RetryableError(
    "The upstream service is temporarily unavailable.",
    StageErrorCodes.UpstreamError);

return StageResult.TerminalError(
    "The insurer rejected the claim.",
    StageErrorCodes.BusinessRejected);
```

Convenience factories include:

- `StageResult.Timeout(...)`;
- `StageResult.UpstreamError(...)`;
- `StageResult.RateLimitExceeded(...)`;
- `StageResult.TransportUnavailable(...)`;
- `StageResult.BusinessRejected(...)`;
- `StageResult.ValidationError(...)`; and
- `StageResult.InvalidState(...)`.

The result wire field is nullable:

```json
{
  "stageId": 81,
  "executionId": "fenced-execution-id",
  "attempt": 2,
  "isSuccess": false,
  "result": "upstream unavailable",
  "errorCode": "UPSTREAM_ERROR",
  "retryable": true
}
```

`retryable:true` does not retry without remaining attempts and a matching
policy. `retryable:false` stops automatic retry. Known business-terminal codes
remain terminal even if incorrectly marked true.

### StageOptions fallback policy

The target `StageOptions` fields are:

| Field | Semantics |
|---|---|
| `MaxRetries` | Maximum retries after the initial execution. |
| `RetryInterval` | Base delay in seconds; must be positive for fallback retry. |
| `RetryOnErrorCodes` | Optional allowlist. Omitted retains legacy “any non-terminal code” behavior. |
| `Backoff` | `fixed` (default), `linear`, or `exponential`. |
| `MaxRetryInterval` | Optional delay cap in seconds. |
| `Jitter` | Optional approximately ±10% delay variation. |
| `TimeOut` | Effective execution timeout in seconds. |

Policy-runtime retry rules take precedence over `StageOptions`. If a retry
policy is configured and its `retryOn.errorCodes` does not match, the failure
is terminal; the server does not fall back to a broader `StageOptions` retry.

A failed stage does not unlock the next stage unless that later stage
explicitly opts into `RunNextIfFailed`. Do not enable that option on the normal
claim sequence.

## Execution metadata, leases, and fencing

For normal stages, `StageNext` contains:

| Field | Stability |
|---|---|
| `pipelineId` | Stable for the pipeline. |
| `stageId` | Stable for the stage row. |
| `idempotencyKey` | Stable for an idempotently created pipeline. Empty on legacy pipelines. |
| `executionId` | Stable for broker redelivery of one dispatch. Replaced when recovery or retry creates a new execution. |
| `attempt` | One-based and incremented with each newly allocated execution ID. |
| `timeoutSeconds` | Effective stage timeout carried to the target SDK. |
| `traceId` / `spanId` | Pipeline/stage trace identity. |
| `traceparent` | Derived W3C parent when stored trace/span IDs are valid. |
| `tracestate` | Stable when supplied as pipeline context; empty when the producer does not supply it. |

The target SDK exposes these values through the optional
`IStageExecutionContext` extension without adding members to the existing
`IStageContext`. A legacy handler can continue to use `IStageContext`.
Reliability-aware code can use `context.AsExecutionContext()` or
`context.GetCancellationToken()`.

Before invoking a handler, the target runner calls:

```http
POST /stages/{stageId}/lease/acquire
X-Worker-Session: <bootstrap session token>
Content-Type: application/json

{"executionId":"...","workerId":"..."}
```

It renews the same lease at
`POST /stages/{stageId}/lease/renew`. These are worker-protocol endpoints, not
business-consumer APIs. The server validates that the worker session belongs
to the pipeline application. The default lease is 60 seconds; the target SDK
normally renews no later than every 20 seconds.

A current lease excludes another worker owner. Once it expires, the stage is
eligible for recovery. Lease expiry is not proof that the old process stopped.
The new execution receives a new token and the server ignores results carrying
the old token or attempt.

`TimeOut` and cancellation are cooperative. The target SDK cancels the
execution-context token on timeout, shutdown, cancellation observed through
lease renewal, or lease loss. A handler must pass that token to HTTP, database,
and other asynchronous calls. Code that ignores it may continue after the
pipeline is terminal.

## Sensitive context

The target SDK adds:

```csharp
builder.AddSensitiveContextItem("shortLivedCredential", credential);
```

This sends `isSensitive:true`. Once a key is sensitive, a later result cannot
downgrade it. Status/UI/WebSocket projections return:

```json
{
  "key": "shortLivedCredential",
  "value": "[REDACTED]",
  "isSensitive": true
}
```

The raw value is still delivered to the selected handler. Known marked values
are substituted in persisted stage logs, the dedicated dashboard log endpoint,
and status input/output projections, but this cannot detect derived, encoded,
truncated, or unmarked secrets.

Prefer not to put credentials or insurance data in context at all. In the
insurance workflow, context should contain only `tenantId`,
`privateInsuranceProcessId`, `orderServiceId`, and an opaque correlation key.
The claim and reservation state belongs in the consumer database.

## Cancellation

Application cancellation:

```http
POST /pipelines/273/cancel
X-API-Key: <application API key>
Content-Type: application/json

{}
```

The operation:

- verifies application ownership;
- marks every unfinished stage `Cancelled`;
- clears retry scheduling, execution token, and lease;
- marks the pipeline terminal `Cancelled`; and
- is idempotent when repeated for the same cancelled pipeline.

An already completed or failed pipeline returns HTTP `409`. A missing or
other-application pipeline returns HTTP `404`.

Cancellation is cooperative. It cannot undo a claim, reservation, or
cancellation request that already reached an external system. The consumer
must use the separate idempotent cancellation workflow to converge external
state.

Manual rerun of a terminal stage remains an operator operation through the
internal authenticated API/dashboard. It is intentionally not inferred from
an application cancellation or exposed as an automatic consumer repair call.

## Correct `EnsureClaim` state machine

Maintain claim-attempt state in the consumer database, for example:

```text
NotSent
  -> persist external request ID/idempotency key
  -> POST claim once
  -> Confirmed | BusinessRejected | OutcomeUnknown

OutcomeUnknown
  -> GET status for the persisted request ID
  -> Confirmed | BusinessRejected | still unknown/transient
```

Handler rules:

```text
if local claim is Confirmed:
    return Success without an external call

if local claim is BusinessRejected:
    return terminal BUSINESS_REJECTED

if a prior request exists or outcome is unknown:
    GET status; never POST a replacement claim blindly

if no request exists:
    persist a permanent request identity before POST
    POST once with that identity

map timeout, transport, rate limit, and temporary upstream failure
to classified retryable results
```

There is inevitably a failure window between an external side effect and local
state persistence. The permanent external key plus status lookup closes that
window. If the external API provides neither facility, the integration cannot
be made safe by Pipelogiq retries.

## Separate cancellation pipeline

Create cancellation as a normal pipeline with its own stable creation key,
for example a key derived from the reservation identity and cancellation
intent:

```csharp
var cancellationPipeline = await PipelineBuilder
    .Create("private-insurance-reservation-cancellation")
    .WithIdempotencyKey(cancellationPipelineKey)
    .AddContextItem("tenantId", tenantId)
    .AddContextItem("privateInsuranceProcessId", processId)
    .AddContextItem("orderServiceId", orderServiceId)
    .WithAction<EnsureReservationCancelledHandler>(
        "ensure-reservation-cancelled",
        new StageOptions
        {
            MaxRetries = 8,
            RetryInterval = 10,
            RetryOnErrorCodes =
            [
                StageErrorCodes.Timeout,
                StageErrorCodes.UpstreamError,
                StageErrorCodes.RateLimitExceeded,
                StageErrorCodes.TransportUnavailable,
            ],
            Backoff = "exponential",
            MaxRetryInterval = 600,
            Jitter = true,
        })
    .WithAction<PersistCancellationResultHandler>("persist-cancellation-result")
    .SendAsync(cancellationToken);
```

The first stage must check the consumer's durable cancellation state and the
external reservation status before sending another cancellation request.

## Backward compatibility

Server `v0.4.0-preview.1` continues to accept:

- legacy `POST /pipelines`;
- pipeline rows without idempotency metadata;
- `StageNext` and `StageResult` payloads without execution metadata;
- `StageResult.Error` without `retryable`;
- old `StageOptions`; and
- context items without `isSensitive`.

Old SDK `0.3.2-preview.5` can continue to run against the target server, but
its messages do not participate fully in lease acquisition and result fencing,
and it cannot opt into idempotent creation, retry filtering, or sensitive
context. Use target SDK `0.4.0-preview.1` for the critical insurance workflow.

The normal non-event path is the reliability path. `AsEvent`/`isEvent` retains
its legacy direct-publish behavior for compatibility and must not host the
critical external side effect.

## Long-term transactional outbox

The current design intentionally avoids a breaking protocol/storage redesign.
Publisher confirms plus DB recovery are the minimal compatible improvement.
They still have a confirm-persist race that can create duplicate deliveries.

The recommended future migration is a PostgreSQL transactional outbox:

1. Add an outbox table keyed by `executionId`.
2. Insert the stage transition and outbox record in one transaction.
3. Have dispatchers claim rows with `FOR UPDATE SKIP LOCKED`.
4. Publish with confirms, then mark the outbox record sent.
5. Retain sent rows long enough for diagnosis and deduplication.
6. Introduce the dispatcher behind a feature flag and temporarily support both
   legacy unsent stages and outbox records.
7. Remove the legacy scan only after all pending pre-migration stages have
   drained.

An inbox/deduplication record for result messages can further simplify
diagnosis, but neither pattern replaces idempotency at the external insurance
boundary.

Inline-policy import is currently a post-create, best-effort operation rather
than part of the idempotent creation transaction. Critical workflows should
put their retry classification in `StageOptions`, as this guide does. Atomic
inline-policy import needs a separate storage/API design.
