# Pipelogiq v0.4.0-preview.1

**Released:** 2026-09-04
**Pairs with:** `pipelogiq-sdk-net 0.4.0-preview.1`
**Images:** `ghcr.io/pipelogiq/pipelogiq-app:v0.4.0-preview.1`, `ghcr.io/pipelogiq/pipelogiq-worker:v0.4.0-preview.1`

This release makes Pipelogiq usable for workflows with real side effects — where a
duplicate execution costs money — and closes the authorization gap that let any signed-in
user read every application's data.

The minor version moves (rather than another `0.3.2` preview) because the authorization
model changes and the API can refuse to start on a configuration that worked before.
Read [Before you upgrade](#before-you-upgrade) first.

---

## Highlights

### Reliable execution

- **Idempotent pipeline creation.** `POST /pipelines/idempotent` creates a pipeline or
  returns the one already created under the same application-scoped key. Reusing a key
  with a semantically different request is reported as a conflict rather than silently
  resolved. The uniqueness authority is a database index, so this holds across API
  replicas. `POST /pipelines/by-idempotency-key` reconciles an unknown outcome without
  putting the key in a URL or an access log.
- **Classified stage failures.** Handlers return explicitly retryable or terminal results
  carrying a standard error code (`TIMEOUT`, `UPSTREAM_ERROR`, `RATE_LIMIT_EXCEEDED`,
  `TRANSPORT_UNAVAILABLE`, `BUSINESS_REJECTED`, `VALIDATION_ERROR`, `INVALID_STATE`,
  `MISSING_REQUIRED_DATA`). Stages accept a retry allowlist by error code, a backoff
  strategy (`fixed`, `linear`, `exponential`), a delay cap and jitter.
- **Execution leases and result fencing.** Every dispatch carries an execution id and
  attempt number. A worker holds a lease while it runs and renews it in the background;
  a result arriving under a superseded execution id is rejected, so a stalled worker
  cannot overwrite the result of the worker that replaced it.
- **Cooperative cancellation.** `POST /pipelines/{id}/cancel` moves an application's
  pipeline and its unfinished stages to a terminal cancelled state in one transaction and
  fences the execution token immediately.
- **Publisher confirms and recovery.** Stages are marked dispatched only after the broker
  confirms the publish. The worker recovers unconfirmed dispatches and expired leases, so
  a lost publish no longer strands a pipeline silently.
- **Richer status.** Attempts, next retry time, last error code, failure disposition and
  terminal flags are exposed through the external status contract.

### Security

- **Application-scoped authorization on the internal API.** Access derives from
  `user_application` membership and is enforced on pipelines, stages, context, logs,
  workers, API keys and WebSocket updates. Cross-application requests answer `404` so ids
  stay unenumerable; bulk operations report out-of-scope targets individually instead of
  acting on them; the WebSocket hub fans an update out only to connections scoped to its
  application and drops any payload whose owning application cannot be resolved.
- **Sensitive context redaction.** Context items marked sensitive are replaced with
  `[REDACTED]` in status responses, dashboard stage logs and WebSocket projections,
  including serialized occurrences inside log text.
- **Administrator bootstrap.** The administrator is provisioned on startup from
  `ADMIN_EMAIL`. With no `ADMIN_PASSWORD_HASH`, a one-time random password is generated
  and printed once in the API log.
- **Demo accounts removed.** The three accounts seeded by migration — whose bcrypt hashes
  are published in this repository's history — are deleted on startup.
- **Metrics off the public surface.** Prometheus metrics moved to a dedicated listener;
  they were previously readable without authentication through the dashboard port.
- **Published example `JWT_SECRET` values are rejected** instead of quietly accepted.

### Fixed

- **Migrations are re-runnable.** Index changesets use `CREATE INDEX IF NOT EXISTS` and
  table changesets carry `preConditions onFail="MARK_RAN"`. An index that already existed
  previously failed the changeset and left `pipelogiq-app` restarting indefinitely.
- **RabbitMQ retry publishes use a short-lived channel.** `amqp091-go` retains every
  `NotifyPublish` listener for the life of a channel, so repeated registration on the
  long-lived consumer channel eventually blocked its reader.
- The quickstart's dashboard credentials work; the Grafana URL in the README matches the
  port Compose publishes.

---

## Before you upgrade

### 1. Generate a JWT secret

The API refuses to start with a published example value, and the Compose files now require
the variable explicitly.

```bash
openssl rand -base64 48
```

Put the result in `JWT_SECRET` in your `.env`. Existing sessions signed with the old
secret are invalidated — users sign in again.

### 2. Decide who the administrator is

```bash
ADMIN_EMAIL=admin@your-company.com
ADMIN_PASSWORD_HASH=          # empty → one-time password printed once in the API log
```

If a real person currently uses `jegor@gmail.com`, either create them a proper account
before upgrading or set that address as `ADMIN_EMAIL` so it is preserved and promoted.

### 3. Back up PostgreSQL

The schema migration is additive — new nullable columns and indexes, no drops and no type
narrowing — but authorization changes are not trivially reversible in an operational sense.

### 4. Move Prometheus targets

If anything scrapes `http://<dashboard-host>:3300/api/metrics`, repoint it at the API's
metrics listener (`METRICS_ADDR`, default `:9091`) inside the network. Worker metrics on
`:9090` are unchanged.

---

## Upgrade

```bash
docker network create pipelogiq 2>/dev/null || true
PIPELOGIQ_VERSION=v0.4.0-preview.1 docker compose --env-file .env \
  -f infra/compose/docker-compose.registry.yml up -d
```

Migrations run inside `pipelogiq-app` on startup. Watch for the bootstrap lines:

```bash
docker logs pipelogiq-app 2>&1 | grep -E "liquibase migration completed|bootstrap:"
```

### Verify application membership

This is the step that most often surprises after upgrading: an account outside
`user_application` now sees nothing. The administrator adopts only applications that have
no members at all.

```sql
SELECT u.email,
       coalesce(string_agg(ua.application_id::text, ','), 'none') AS applications
FROM "user" u
LEFT JOIN user_application ua ON ua.user_id = u.id
GROUP BY u.email;
```

Grant missing membership explicitly:

```sql
INSERT INTO user_application (user_id, application_id) VALUES (<user_id>, <application_id>);
```

### Smoke test

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:3300/api/healthz   # 200
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8081/healthz       # 200
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:3300/api/metrics   # 404 — expected
curl -s http://localhost:3300/api/version
```

---

## Rollback

The database migration is additive, so `v0.3.2-preview.6` can run against the upgraded
schema: old code ignores the added columns. Two caveats:

- The demo accounts are gone and are not restored by rolling back. The bootstrapped
  administrator remains and works on the older version.
- Pipelines created with an idempotency key keep it; the old server ignores the column.

Roll back by pinning the previous image tag and restarting. Do not roll the schema back.

---

## Known limitations

Carried forward deliberately and documented in
[the reliable execution guide](../reliable-execution.md):

- Pipelogiq does not guarantee exactly-once external side effects. Handlers must keep a
  stable external idempotency key and reconcile an unknown outcome before issuing another
  command.
- PostgreSQL-to-RabbitMQ dispatch is not one atomic transaction. Publisher confirms plus
  recovery still allow a duplicate delivery in the confirm-persist race; a transactional
  outbox is the intended hardening.
- A lease excludes competing workers only while it is valid. A handler that ignores
  cooperative cancellation may continue past expiry.
- Inline policy import remains post-create and best-effort. Critical retry behaviour
  should use durable `StageOptions`.

## Not in this release

Named so they are not mistaken for solved: API keys are still stored in plain text,
integration tokens are still unencrypted, there is no login rate limiting, the dashboard
still renders authorization errors as empty states, action policies still live in a
JSON file with the database as a mirror, and there is no SSO.
