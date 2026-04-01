# Action Policies

> **Status: Experimental.** Policy CRUD, inline policy import, effective policy resolution, and partial runtime enforcement are functional. The policy engine is still evolving.

Action policies define rules that govern how stages and pipelines behave. They provide guardrails for rate limiting, retry behavior, timeouts, and circuit breaking.

## Policy Types

### Rate Limit

Controls the rate at which stages of a given type can be executed.

```json
{
  "name": "limit-kyc-checks",
  "type": "rate_limit",
  "rules": {
    "limit": 100,
    "windowSeconds": 60,
    "keyBy": "handler"
  },
  "targeting": {
    "stageHandlerNames": ["kyc-check"]
  }
}
```

Fields:
- `limit` — maximum number of executions allowed in the window
- `windowSeconds` — time window in seconds
- `keyBy` — grouping key (`handler`, `pipeline`, `application`)

### Retry

Overrides the default retry behavior for targeted stages.

```json
{
  "name": "aggressive-retry",
  "type": "retry",
  "rules": {
    "maxRetries": 5,
    "intervalSeconds": 30,
    "backoffMultiplier": 2
  },
  "targeting": {
    "stageNames": ["payment-processing"]
  }
}
```

Fields:
- `maxRetries` — maximum retry attempts
- `intervalSeconds` — initial delay between retries
- `backoffMultiplier` — multiplier applied to interval after each retry

### Timeout

Sets a maximum execution time for targeted stages.

```json
{
  "name": "quick-timeout",
  "type": "timeout",
  "rules": {
    "timeoutSeconds": 120
  },
  "targeting": {
    "tags": { "include": ["critical"] }
  }
}
```

Fields:
- `timeoutSeconds` — maximum allowed execution time

### Circuit Breaker

Stops executing stages when the failure rate exceeds a threshold.

```json
{
  "name": "payment-breaker",
  "type": "circuit_breaker",
  "rules": {
    "failureThreshold": 5,
    "windowSeconds": 300,
    "cooldownSeconds": 60
  },
  "targeting": {
    "stageHandlerNames": ["payment-gateway"]
  }
}
```

Fields:
- `failureThreshold` — number of failures to trigger the breaker
- `windowSeconds` — observation window
- `cooldownSeconds` — time before the breaker resets

## Targeting

Policies can target stages by:

- `pipelineIds` — specific pipeline IDs
- `stageNames` — stage name patterns
- `stageHandlerNames` — handler name patterns
- `tags.include` / `tags.exclude` — keyword-based inclusion/exclusion
- `environment` — deployment environment

## Policy Lifecycle

Policies have four states:

| State | Description |
|---|---|
| `enabled` | Active and will be evaluated (when enforcement is implemented) |
| `disabled` | Inactive; will not be evaluated |
| `paused` | Temporarily suspended |
| `draft` | Created but not yet activated |

## API Endpoints

All policy endpoints are on the internal API (`:8080`, requires JWT auth):

| Method | Path | Description |
|---|---|---|
| `GET` | `/policies` | List policies (supports filtering and sorting) |
| `POST` | `/policies` | Create a policy |
| `GET` | `/policies/{id}` | Get a policy by ID |
| `PUT` | `/policies/{id}` | Update a policy |
| `DELETE` | `/policies/{id}` | Delete a policy |
| `POST` | `/policies/{id}/duplicate` | Duplicate a policy |
| `POST` | `/policies/{id}/enable` | Enable a policy |
| `POST` | `/policies/{id}/disable` | Disable a policy |
| `POST` | `/policies/{id}/pause` | Pause a policy |
| `POST` | `/policies/{id}/resume` | Resume a policy |
| `POST` | `/policies/preview` | Preview which stages a policy targets |
| `GET` | `/policies/insights` | Policy trigger statistics |
| `GET` | `/policies/effective/stages/{stageId}` | Resolve effective policies for a stage with explainability |

## Current Limitations

- **File-backed storage** — policies are stored in `./data/policies.json` rather than the database. Migration to DB-backed storage is planned.
- **Partial runtime enforcement** — timeout, rate-limit, and circuit-breaker logic now feed stage dispatch and timeout watching, but retry/runtime behavior is not fully enforced yet.
- **Explainability is stage-scoped** — effective resolution is currently exposed per stage and not yet as a general-purpose policy simulator for arbitrary payloads.

## What "Throttled" Means

When a stage exceeds a matched rate limit, it is placed in a `Throttled` state and retained in the queue until `next_retry_at`. Circuit breakers can block dispatch entirely and fail the stage immediately. The effective resolution endpoint explains which policies matched, which rule won for each type, and which policy actually triggered runtime behavior.
