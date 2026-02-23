# Action Policies

> **Status: Experimental.** Policy CRUD is functional, but policies are not enforced at runtime. This feature is under active development.

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
| `GET` | `/policies/preview` | Preview which stages a policy targets |
| `GET` | `/policies/insights` | Policy trigger statistics |

## Current Limitations

- **No runtime enforcement** — policies are stored and manageable through the API and dashboard, but the execution engine does not evaluate them during stage execution. This is the primary gap being worked on.
- **File-backed storage** — policies are stored in `./data/policies.json` rather than the database. Migration to DB-backed storage is planned.
- **No trigger events** — the `PolicyEventTypeTriggered` event exists in the model but is never emitted.

## What "Throttled" Means

When policy enforcement is implemented, a stage that exceeds a rate limit or hits an open circuit breaker will be placed in a `Throttled` state. The stage remains in the queue but is not dispatched to workers until the policy condition clears. This status is modeled in the frontend but not yet produced by the backend.
