# Pipelogiq v0.5.0

Release notes prepared on **2026-09-18**. This document describes the release
target; registry availability is checked separately during publication.

| Component | Version |
| --- | --- |
| Server and dashboard | `0.5.0` |
| .NET SDK packages | `0.5.0` |
| Java SDK modules | `0.5.0` |
| Helm chart | `0.5.0` (app version `v0.5.0`) |
| App, worker and migration image tags | `v0.5.0` |

This is a release without a preview suffix. As a pre-1.0 project, future minor
releases can still introduce documented breaking changes.

## Highlights

- **Users and access:** manage accounts, Admin/Operator/Viewer roles, application
  memberships, account status and password resets in Settings. Permissions are
  enforced on the server, including application-scoped live updates. Custom roles
  and email invitations are not included.
- **User activity audit:** filter by acting user, action and outcome. Pipeline and
  stage operations, including rerun and skip, schedule actions, administrative
  changes and authentication are recorded. Secret values are redacted in history
  and Pro exports. See [audit coverage](../audit.md).
- **Pipeline run workspace:** a dedicated page brings together stages, payloads,
  searchable logs and a context table. Open it with **View details** from the run
  panel. Shared dropdowns replace native filters throughout the dashboard.
- **Settings:** API keys, Users, Audit, Licence and Security have separate tabs.
  The licence view focuses on the installed tier, status and available features.
- **Schedules and workers:** cron/interval/once schedules, HTTP and AMQP workers,
  execution leases, retries and result fencing. The coordinated .NET and Java
  SDK release target is `0.5.0`.

## Security and operations

- API keys are stored as hashes; existing keys are backfilled and remain valid.
  A new key's clear value is shown only at creation.
- Sign-in is rate-limited per address and account. Unknown accounts also perform
  a password-hash comparison. Limits are per API process; proxy trust boundaries
  are described in [Users and access](../user-management.md#proxy-addresses-and-login-throttling).
- Account edits, deactivation and resets revoke sessions. Live streams revalidate
  access and close on expiry. Concurrent password changes cannot overwrite a
  later administrator reset or session change.
- Integration secrets can be encrypted with `SECRETS_ENCRYPTION_KEY`; audit,
  context and API responses mask sensitive values.
- Migrations run in a separate image before the app. The app runs unprivileged
  with nginx on port `8080`. Release images target Linux amd64 and arm64.
- The standalone app Compose configuration waits for migration completion, and
  the registry pull command includes the migration image alongside app and worker.
- Release validation checks version consistency and runs the full CI suite before
  publication. Pro builds receive the licence public key; prereleases do not
  overwrite stable `latest` tags. Release assets include binaries, the Helm chart
  and checksums.

## Breaking changes and upgrade requirements

Read [operations](../operations.md#upgrade) and back up the database and deployment
configuration before upgrading from a preview release.

1. Set `DATABASE_URL`. SQLite fallback requires explicit
   `ALLOW_SQLITE_FALLBACK=true` and is intended for local experiments.
2. Set `SECRETS_ENCRYPTION_KEY` on the app and worker before storing integration
   credentials (`openssl rand -base64 32` generates a key). Preserve existing
   encryption keys across upgrades.
3. Apply **all migrations before starting the new API and worker**. Compose and
   Helm run the migration service/job first. The app no longer reads
   `LIQUIBASE_ENABLED`, `LIQUIBASE_USERNAME`, `LIQUIBASE_PASSWORD` or
   `LIQUIBASE_CHANGELOG_FILE`.
4. Review [user migration details](../user-management.md#upgrade). Legacy roles
   normalize to Admin, Operator and Viewer, existing memberships remain, and
   sessions are revoked. Duplicate trimmed, case-insensitive emails must be
   resolved before migration can finish; accounts are not merged automatically.
5. Accounts retaining shipped demo password hashes are disabled, even if renamed.
   Configure a valid replacement `ADMIN_PASSWORD_HASH` for a disabled bootstrap
   administrator. Accounts with changed passwords retain their history and access.
   Verify administrator sign-in and memberships after migration.
6. Publish the app's nginx port `8080` in custom deployments (Compose uses
   `3300:8080`); the former port `80` is no longer used.
7. Update old integrations: `GET /rabbitmq/connection`, duplicate
   `/pipelines/stages/{id}` and `/pipelines/context/{id}` routes, and `APPENV__*` /
   `CONNECTIONSTRINGS__*` aliases are removed. AMQP workers receive broker details
   at bootstrap; `WORKER_RABBITMQ_URL` can provide a restricted broker account.
8. Upgrade the server first, then SDK consumers to `0.5.0`, and restart workers.
   Stage names must be unique within a pipeline and contain no commas or control
   characters. Action policies are now stored in the database.

Once the release images are published:

```bash
PIPELOGIQ_VERSION=v0.5.0 docker compose --env-file .env \
  -f infra/compose/docker-compose.registry.yml up -d
docker logs pipelogiq-migrate
curl -fsS http://localhost:3300/api/readyz
curl -fsS http://localhost:3300/api/version
```

Check the migration container exited successfully, `/version` reports `v0.5.0`,
and a test run completes with the upgraded SDK. Release maintainers also need
the `LICENSE_PUBLIC_KEY` repository secret; operators using published images do
not need access to this repository secret.

## Rollback

Do not assume an older binary preserves the new authentication and access
controls. Role normalization, disabled demo accounts, session revocation and
hashed credentials change data as well as schema. Test rollback on a restored
backup with the previous images and configuration before using it in production.
No automatic downgrade migration is provided.

## Validation and limitations

The [security verification report](../security-review-2026-09-18.md) records
Community/Pro Go tests, frontend checks, PostgreSQL upgrades and concurrency
regressions, vulnerability scans and container checks. SDK repositories record
their own package-consumer verification for `0.5.0`. The coordinated
[version and packaging check](../release-verification-2026-09-18.md) records this
release preparation and the remaining publication prerequisites.

- External side effects remain at least once: handlers must be idempotent. A
  publish-confirm/persist race can duplicate dispatch; leases exclude competing
  workers only while valid. A transactional outbox is not included.
- SSO is behind the Pro gate and has not been verified against a live Entra ID or
  Google tenant. No published load certification is included.
- Packaging checks do not prove that NuGet, Maven Central or GitHub/GHCR artifacts
  have been published. Follow each repository's release procedure and verify the
  resulting registries after publication.
