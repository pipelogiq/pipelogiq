# Pipelogiq v0.6.0-preview.1

**Released:** 2026-09-07
**Pairs with:** `pipelogiq-sdk-net 0.5.0-preview.1`
**Images:** `pipelogiq-app`, `pipelogiq-worker`, `pipelogiq-migrate` at `v0.6.0-preview.1` (linux/amd64 and linux/arm64)

This release is a security and operations pass toward 1.0. It closes the
authentication and credential gaps, moves migrations and the licence seam into
the release pipeline, and makes the deployment shape production-appropriate.

The minor version moves because it changes how the product is deployed and can
refuse to start on a configuration that worked before. Read
[Before you upgrade](#before-you-upgrade).

---

## Highlights

### Credentials and sessions

- **API keys are stored hashed** (SHA-256). The dashboard shows an 8-character
  prefix; the clear key is shown once, when it is created. Existing keys are
  backfilled and keep working.
- **Sign-in is rate-limited** per address and per account, and answers `429`
  with `Retry-After`. An unknown account burns a bcrypt comparison, so it is not
  distinguishable by timing.
- **Sessions are revocable.** Every JWT carries a token version; logout and a
  password change rotate it, so a stolen or shared token stops working at logout
  instead of at its seven-day expiry.
- **Password change** (`POST /auth/password`, and a form in Settings) verifies
  the current password and ends every other session of the account.
- **The session cookie's `Secure` flag** honours `X-Forwarded-Proto` behind a
  TLS-terminating proxy; `COOKIE_SECURE=always|never` overrides.

### Secrets and the broker

- **Integration secrets are encrypted at rest** (AES-256-GCM under
  `SECRETS_ENCRYPTION_KEY`) and **masked in API responses**; a form re-saved
  with the mask keeps the stored value.
- **`GET /rabbitmq/connection` is removed.** AMQP workers get the broker address
  at bootstrap; `WORKER_RABBITMQ_URL` sets a least-privilege address, and
  `WORKER_BROKER_HANDOUT=false` hands out none.

### Audit

- **An audit log** records sign-ins, password changes and every state-changing
  administrative call with route, status and address. Settings → Audit, and
  `GET /audit`.

### The commercial seam actually ships

- **The release build carries the Pro code and the licence public key.** A
  release image can now verify a licence; the previous pipeline built
  Community-only images that never could. A release job asserts the built binary
  reports the Pro seam compiled in.

### Deployment

- **Migrations run in their own image** (`pipelogiq-migrate`) before the app,
  so a failed migration stops the rollout instead of looping the API.
- **The app runs unprivileged** and builds for **amd64 and arm64**; the JRE and
  in-image Liquibase are gone.
- **A Helm chart** with a pre-upgrade migration job.

### The rest

- **Action policies live in the database**, not a JSON file.
- **Stage names are validated** — unique per pipeline, no commas or control
  characters, because dependencies are resolved by name.
- **The whole external API is in `docs/openapi.external.yaml`**; CI fails when a
  route is added without documenting it.
- **The dashboard distinguishes 401, 403 and errors** instead of drawing them as
  empty data, gates admin screens by role, and shows an explicit refusal.

---

## Before you upgrade

Read the full checklist in [operations.md](../operations.md#upgrade). The steps
that change from a working install:

1. **`DATABASE_URL` is now required.** The silent SQLite fallback is gone. Set
   `ALLOW_SQLITE_FALLBACK=true` only for local experiments.
2. **Set `SECRETS_ENCRYPTION_KEY`** (`openssl rand -base64 32`) on the app *and*
   the worker before saving any integration token. Without it, tokens are stored
   in clear and a warning is logged.
3. **Run the migration image first.** Compose and Helm do this for you; a custom
   deployment must run `pipelogiq-migrate` before the app and stop the rollout if
   it fails.
4. **The app port changed** — nginx listens on `8080`, not `80`. Compose maps
   `3300:8080`; a custom deployment must republish.
5. **Everyone signs in again.** Sessions from before the upgrade are invalid.
6. **Configure the release secret.** The release workflow needs a
   `LICENSE_PUBLIC_KEY` repository secret, or it refuses to build a
   Community-only image.

## Upgrade

```bash
PIPELOGIQ_VERSION=v0.6.0-preview.1 docker compose --env-file .env \
  -f infra/compose/docker-compose.registry.yml up -d
```

Confirm the migration finished and the app is healthy:

```bash
docker logs pipelogiq-migrate | grep -E "Run:|ERROR"
curl -fsS http://localhost:3300/api/readyz
```

## Rollback

The schema is additive; pin the previous image and restart. Do not roll the
schema back. See [operations.md](../operations.md#rollback).

## Known limitations

Unchanged from the reliability guide: no exactly-once external side effects, the
dispatch confirm-persist race can duplicate a delivery, and a lease excludes
competing workers only while valid.

## Not in this release

SSO is implemented behind the Pro `sso` gate but has not been verified against a
live Entra ID or Google tenant. Published load figures and a transactional
outbox remain future work.
