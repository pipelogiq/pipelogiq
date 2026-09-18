# User activity audit

Administrators open **Settings → Audit** (`/settings/audit`) to see user activity
across the installation. Filter by action, acting user, and outcome; filters and
the current page are preserved in the URL. The list is ordered newest first.
Expand an event to inspect its operation details, including bulk results.

## Coverage

The internal API records one event for each authenticated mutation request,
including requests rejected by role or application access checks, validation
failures, and server failures. Explicit handler events merge into that request's
event so user administration does not produce duplicate entries.

| Operation | Action names | Selected details |
| --- | --- | --- |
| Retry or skip a stage | `stage.rerun`, `stage.skip` | Stage ID; whether the retry also resets subsequent stages |
| Pause or resume a pipeline | `pipeline.pause`, `pipeline.resume` | Pipeline ID |
| Bulk operations | `pipeline.bulk_pause`, `pipeline.bulk_resume`, `stage.bulk_rerun`, `stage.bulk_skip` | Requested target IDs, totals, and each target's ID, scope and success flag |
| Schedule operations | `schedule.pause`, `schedule.resume`, `schedule.archive`, `schedule.trigger`, `schedule.update_policies` | Schedule/application IDs, previous/new status or policies; created run/pipeline IDs for a manual trigger |
| User administration | `user.create`, `user.update`, `user.password_reset` | Target user; role, active status and application memberships for create/update |
| Applications and API keys | `application.create`, `application.update`, `api_key.create`, `api_key.disable` | Application/key IDs when available; new application creation via key generation |
| Action policies | `policy.create`, `policy.update`, `policy.delete`, `policy.duplicate`, `policy.promote`, `policy.enable`, `policy.disable`, `policy.pause`, `policy.resume`, `policy.preview` | Route target; created/duplicated policy IDs |
| Observability settings | `observability.configure`, `observability.delete`, `observability.test` | Integration type, target and operation outcome for configuration, deletion and connection tests |
| Retention | `retention.update`, `retention.purge` | Application target and HTTP result |
| Signed export | `audit.export` | Application and exported record count when completed |
| Authentication | `auth.login`, `auth.logout`, `auth.password` | Sign-in, sign-out and password change results |

If a request is rejected before its body is decoded, the event still has the
route's action and result, but body-derived target IDs/options may be absent.
Bulk validation failures can use the generic `pipeline.bulk_action` action;
application requests rejected before decoding can use `application.save`.
Other internal mutation routes fall back to a method-and-route action, so adding
a route does not silently remove it from audit coverage.

Bulk requests produce one event with per-target results. Mixed success is
`partial`; all targets failing is `failed`, even when the bulk HTTP response is
200. Target lists contain the unique positive IDs accepted by the endpoint.
Individual backend error strings are omitted from the stored details.
An observability connection test returning `success: false` is also recorded as
`failed`, even when its HTTP response is 200.

Normal reads, opening pages, automatic refreshes and WebSocket updates do not
create audit events. The explicit signed export request is logged even though
it uses GET. Export remains subject to its existing Pro licence and application
access checks; the ordinary user activity log is available without Pro.

This log covers dashboard user activity. Worker/SDK API-key operations and
automatic scheduler execution do not acquire a human actor or create events in
this log. Their execution history remains in the pipeline, schedule and worker
history. Requests with missing, invalid or expired session cookies are rejected
before the mutation audit middleware; public sign-in/sign-out handlers record
their own outcomes.
Mutation requests with a correctly signed, unexpired cookie for a revoked
session or inactive account reach the audit middleware and are logged as
`denied` when the account/session check rejects them.

## Identity, outcomes and stored data

Each event stores its timestamp, action, target, outcome, remote address and
selected details. Authenticated operations include the acting user's stable
`userId` and session email. The user filter uses that ID, so an email change does
not split the account's history. User options use the current account email when
available, or an email retained in its history.

A failed sign-in may contain the submitted email as an **attempted identity**,
but has no authenticated `userId`. It must not be interpreted as an action by
the owner of that address. Successful sign-ins gain an authenticated user ID
only after credentials and account status have been checked.

| Outcome | Meaning |
| --- | --- |
| `ok` | The API operation completed successfully |
| `failed` | Validation, execution or authentication failed |
| `partial` | A bulk operation succeeded for some targets and failed for others |
| `denied` | Access was denied, such as a role or session rejection |
| `rate_limited` | Sign-in attempts exceeded the configured limit |

Application-scope rejections keep the endpoint's existing 404 response and are
recorded as `failed`. The audit does not change what an unauthorized caller can
learn about a target.

Details are explicitly selected by the handlers. Passwords and hashes, API key
values, cookies/tokens, pipeline inputs, schedule definitions, context values,
and whole request/response bodies are not copied into the audit log. Routine
mutation details include the HTTP method, route pattern and status.
Untrusted text metadata is bounded to the database column lengths; invalid
UTF-8 and NUL characters are replaced so malformed targets cannot discard an
event. Long target strings may therefore be truncated in history.
Signed stage-log exports apply the same sensitive-value masking as pipeline
details, including historical sensitive values. Logs and masking values are
read from one database snapshot so concurrent SDK writes cannot bypass masking.

Storage is **best effort**: recording an event has a separate short timeout,
and failure is written to the API's server log without rolling back or rejecting
the operation. There is no transactional guarantee that every successful change
has a persisted audit row. Monitor `audit record failed` server errors when
operating the service.

## Internal API

Both endpoints require an active administrator session. Through the web proxy,
prefix these paths with `/api`.

`GET /audit` accepts these optional query parameters; filters are combined with
AND and applied in the database before counting or pagination:

| Parameter | Contract |
| --- | --- |
| `action` | Exact stored action string, at most 100 bytes after trimming |
| `userId` | Positive actor ID; `0` selects events without an authenticated user ID; omit for all actors |
| `outcome` | One of `ok`, `failed`, `partial`, `denied`, `rate_limited`; omit for all outcomes |
| `page` | One-based page, default 1, maximum 10,000,000 |
| `pageSize` | 1–100, default 50 |

For example:

```http
GET /api/audit?action=stage.rerun&userId=42&outcome=ok&page=1&pageSize=50
```

The response is an object with `items`, `totalCount`, `page`, `pageSize`, and
`totalPages`. Items contain `id`, `ts`, `action`, `outcome`, and optional
`userId`, `email`, `target`, `remoteAddr`, and `details`. `details` is a
JSON-encoded string, preserving the existing audit row representation. Ordering
is `ts DESC, id DESC`; an empty result has `items: []` and `totalPages: 0`.
Invalid query values return 400, while non-administrators receive 403.

`GET /audit/options` returns:

```json
{
  "actions": ["pipeline.pause", "stage.rerun"],
  "users": [{ "id": 42, "email": "operator@example.com" }]
}
```

Options come from the entire retained audit history, independently of the
current page or filters. They include historical action names and actors no
longer present in the account table. The unauthenticated option is represented
by `userId=0` in list requests; it is not a user object in the options response.

## Upgrade and verification

Apply `database/changelog.xml` through the normal Compose migration flow or
`make migrate-up` before starting the updated API. The additive
`index audit history filters` migration creates indexes on `(action, ts, id)`
and `(user_id, action, ts, id)` for filtered history queries.

Existing audit rows remain unchanged, including method-and-route action names
from earlier versions. The UI preserves those raw action names. Operations that
were never audited previously cannot be recovered retroactively. Internal
clients using the earlier `/audit?limit=...` array response must switch to the
paginated object and `pageSize` parameter.

Run `go test ./internal/api ./internal/store` from `apps/go` for audit middleware,
operation attribution, access checks, bulk outcomes, secret omission, filters
and pagination. For PostgreSQL-specific store coverage, apply the migrations to
a disposable database, set `PIPELOGIQ_TEST_DATABASE_URL`, and run
`go test -tags integration ./internal/store -run TestPostgresAudit -count=1`.
The web suite
(`npm test` from `apps/web`) covers filters, pagination, detail rendering and
request serialization.
