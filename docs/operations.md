# Operations

What an operator needs to run Pipelogiq in production: what to back up, how to
upgrade and roll back, how to rotate secrets, and what to watch.

## Components

| Component | Image | Runs as | Listens on |
|---|---|---|---|
| Migrations | `pipelogiq-migrate` | one-shot, before the app | — |
| Dashboard + API | `pipelogiq-app` | `nginx` (uid 101), unprivileged | `8080` dashboard and internal API, `8081` external API, `9091` metrics |
| Worker | `pipelogiq-worker` | `nobody` | `9090` metrics |

All three are published for `linux/amd64` and `linux/arm64`.

The app image runs nginx and the API under supervisord in one container. nginx
proxies `/api/` and `/ws` to the API on the loopback port `8088`; nothing but
nginx is reachable on `8080`.

## Configuration that must be set

| Variable | Why |
|---|---|
| `JWT_SECRET` | Signs sessions. The API refuses published example values. `openssl rand -base64 48`. |
| `DATABASE_URL` | Required. There is no silent fallback. |
| `SECRETS_ENCRYPTION_KEY` | Encrypts integration tokens at rest. `openssl rand -base64 32`. The same value must reach the worker. Without it, tokens are stored in clear and a warning is logged at startup. |
| `ADMIN_EMAIL` | The administrator provisioned on first start. With no `ADMIN_PASSWORD_HASH`, a one-time password is printed once in the API log. |
| `WORKER_RABBITMQ_URL` | The broker address handed to AMQP workers at bootstrap. Use a least-privilege RabbitMQ user, not the server's. `WORKER_BROKER_HANDOUT=false` hands out nothing; workers then set `PIPELOGIQ_RABBITMQ_URL` themselves. |
| `COOKIE_SECURE` | `auto` trusts TLS or `X-Forwarded-Proto: https`; `always` / `never` override. |

## Migrations

Migrations are **not** run by the app. The `pipelogiq-migrate` image applies the
Liquibase changelog and exits; Compose starts the app only after it succeeds
(`depends_on: service_completed_successfully`), and the Helm chart runs it as a
`pre-install,pre-upgrade` hook. A failed migration therefore stops the rollout
with the error in the migration container's log, instead of restarting the API in
a loop.

The changelog is additive and re-runnable: index and table changesets carry
`preConditions`, so applying the same version twice is a no-op, and applying it
to a database that already has some objects marks them as run.

The changelog declares `logicalFilePath="database/changelog.xml"`. Do not change
it: Liquibase identifies a changeset by id, author and this path, and a different
path would make every changeset look new.

To run migrations by hand:

```bash
docker run --rm --network pipelogiq \
  ghcr.io/pipelogiq/pipelogiq-migrate:v0.5.0-preview.1 \
  --url=jdbc:postgresql://pipelogiq-postgres:5432/pipelogiq \
  --username=pipelogiq --password="$POSTGRES_PASSWORD" \
  --searchPath=/liquibase --changeLogFile=database/changelog.xml update
```

## Backup

**PostgreSQL is the only state that matters.** RabbitMQ holds in-flight messages
only; the worker recovers unconfirmed dispatches and expired leases from the
database after a broker loss.

Back up with `pg_dump` on a schedule you can live with losing:

```bash
pg_dump --format=custom --no-owner "$DATABASE_URL" > pipelogiq-$(date -u +%Y%m%dT%H%M%SZ).dump
```

Keep the `SECRETS_ENCRYPTION_KEY` with the backup, separately from the dump:
integration tokens in the dump are unreadable without it.

## Restore

```bash
pg_restore --clean --if-exists --no-owner --dbname "$DATABASE_URL" pipelogiq-*.dump
```

Then start the same version the dump was taken from, or a newer one — the
migrate image brings the schema forward. Never restore a dump into a database
that a newer version already migrated and then start the older version.

## Upgrade

1. Read the release notes' **Before you upgrade** section.
2. Back up (above).
3. Pull the new tag and start. Compose:
   ```bash
   PIPELOGIQ_VERSION=vX.Y.Z docker compose --env-file .env \
     -f infra/compose/docker-compose.registry.yml up -d
   ```
   Helm:
   ```bash
   helm upgrade pipelogiq infra/helm/pipelogiq --set image.tag=vX.Y.Z
   ```
4. Confirm the migration container exited 0 and the app is healthy:
   ```bash
   docker logs pipelogiq-migrate | grep -E "Run:|ERROR"
   curl -fsS http://localhost:3300/api/readyz
   ```
5. Upgrade workers and the SDK after the server. A newer SDK against an older
   server fails on the first lease call.

Adjacent versions are tested; skipping versions is expected to work because the
changelog is cumulative, but is not tested.

## Rollback

Pin the previous image tag and restart. The schema stays at the newer version —
the older code ignores the added columns — so no schema rollback is needed or
supported. Data written only by the newer version (a schedule, a lease, an audit
row) stays inert.

## Secrets rotation

| Secret | How | Effect |
|---|---|---|
| `JWT_SECRET` | Change the variable, restart the app | Every dashboard session ends; users sign in again. |
| `SECRETS_ENCRYPTION_KEY` | Re-enter every integration secret in the dashboard **after** changing the key | Values encrypted under the old key cannot be read under the new one. Rotate by re-saving, not by editing the database. |
| API keys | Create a new key in Settings, move the worker to it, disable the old one | Disabled immediately; the worker's next call fails. |
| Administrator password | Settings → Password | Every other session of the account ends. |
| RabbitMQ worker user | Change `WORKER_RABBITMQ_URL`, restart the app, restart workers | Workers receive the new address at their next bootstrap. |

## What to watch

Prometheus scrapes `:9091` on the app and `:9090` on the worker. Provisioned
Grafana rules cover an overdue schedule, failing schedule runs and a lagging
scheduler. Beyond those, the signals that matter:

- `pipelogiq-migrate` exit code on every rollout.
- `stage_active_timed_out_total` rising: handlers are exceeding their timeout.
- Worker registry in the dashboard showing `offline` for a worker that should be
  up: heartbeat lost for longer than `WORKER_OFFLINE_AFTER`.
- The audit log (Settings → Audit): rate-limited sign-ins, failed password
  changes, API keys disabled.

## Retention

Community keeps every finished pipeline. The Pro `retention` gate adds a
scheduled purge; without it, trim manually with the transaction in
[licensing.md](licensing.md#retention) after a backup.
