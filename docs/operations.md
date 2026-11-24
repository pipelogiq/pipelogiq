# Operations

What an operator needs to run Pipelogiq in production: what to back up, how to
upgrade and roll back, how to rotate secrets, and what to watch.

## Components

| Component | Image | Runs as | Listens on |
|---|---|---|---|
| Migrations | `pipelogiq-migrate` | one-shot, before the app | — |
| Dashboard + API | `pipelogiq-app` | `nginx` (uid 101), unprivileged | `8080` dashboard and internal API, `8081` external API, `9091` metrics |
| Worker | `pipelogiq-worker` | `nobody` | `9090` metrics |

The release workflow targets `linux/amd64` and `linux/arm64` for all three images.
Verify the tagged artifacts exist before deploying a newly prepared release.

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

## Serving the dashboard under a path

The combined app image supports `PIPELOGIQ_BASE_PATH=/pipelogiq` at **container
startup**, without rebuilding the frontend for each installation. Leave it empty
for the domain root. Accepted prefixes contain slash-separated letters, digits,
underscores and hyphens, with no trailing slash. This requires an image built with
base-path support; the previously published `v0.5.1` image does not include it.

Route `/pipelogiq` and its descendants to the app's port 8080, preserving the
prefix (no ingress rewrite). The app redirects `/pipelogiq` to `/pipelogiq/`, serves
SPA deep links and static resources there, and proxies `/pipelogiq/api/*` and
`/pipelogiq/ws` to the unchanged internal API. Outside paths return 404. Set
browser health/readiness probes to `/pipelogiq/api/healthz` and
`/pipelogiq/api/readyz`. Set `COOKIE_SECURE=true` when HTTPS terminates upstream.

The session cookie is scoped to `/pipelogiq/`, including cookie deletion on
logout. Different prefixes can host test/live on the same domain. Restarting with
a new prefix requires signing in at the new address. The SDK-facing port 8081
and worker protocol do not change. Vite development continues at `/`.

`PIPELOGIQ_BASE_PATH` is passed through by the supplied Compose files. With direct
Docker, use `-e PIPELOGIQ_BASE_PATH=/pipelogiq`; with Kubernetes, set the same
container environment variable and an Ingress Prefix path. Point any external
links (including Support's `PIPELOGIQ_PUBLIC_URL`) at the full prefixed URL.

Validate a newly built app image with:

```sh
python3 scripts/test-web-base-path.py YOUR_APP_IMAGE
```

This runs disposable containers with a local HTTP fixture and checks root and
prefixed pages, assets/fonts, deep links, cookie paths, WebSocket upgrades and
runtime configuration validation. It does not require production credentials.

## Migrations

Migrations are **not** run by the app. The `pipelogiq-migrate` image applies the
Liquibase changelog and exits; Compose starts the app only after it succeeds
(`depends_on: service_completed_successfully`), and the Helm chart runs it as a
`pre-install,pre-upgrade` hook. A failed migration therefore stops the rollout
with the error in the migration container's log, instead of restarting the API in
a loop.

The changelog is cumulative and re-runnable. Liquibase records completed
changesets; guarded index/table changes can adopt existing objects. Some
migrations also transform data or add constraints, including the 0.5.0 role,
credential and account changes. Reapplying a completed version is a no-op, but
a failed precondition must be resolved before continuing.

The changelog declares `logicalFilePath="database/changelog.xml"`. Do not change
it: Liquibase identifies a changeset by id, author and this path, and a different
path would make every changeset look new.

To run migrations by hand:

```bash
docker run --rm --network pipelogiq \
  ghcr.io/pipelogiq/pipelogiq-migrate:v0.5.1 \
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

1. Read the release notes' [Breaking changes and upgrade requirements](releases/RELEASE_NOTES_v0.5.1.md#breaking-changes-and-upgrade-requirements).
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

See the release notes and security verification report for the tested upgrade
baselines. Migrations are cumulative; validate the actual deployment on a backup.

## Rollback

Test the previous images and configuration on a restored backup before rolling
back. Version 0.5.0 changes roles, account status, credential storage and session
state; older binaries may not preserve the new access controls. Additive schema
changes alone do not guarantee safe rollback. No automatic downgrade migration
is provided.

## Secrets rotation

| Secret | How | Effect |
|---|---|---|
| `JWT_SECRET` | Change the variable, restart the app | Every dashboard session ends; users sign in again. |
| `SECRETS_ENCRYPTION_KEY` | Re-enter every integration secret in the dashboard **after** changing the key | Values encrypted under the old key cannot be read under the new one. Rotate by re-saving, not by editing the database. |
| API keys | Create a new key in Settings, move the worker to it, disable the old one | Disabled immediately; the worker's next call fails. |
| Administrator password | Account → Password | Every other session of the account ends. |
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
