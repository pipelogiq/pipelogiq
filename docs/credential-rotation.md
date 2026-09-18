# Rotating Pipelogiq credentials

Removing credentials from Git does not revoke them. This runbook replaces an
API key and the dashboard JWT signing secret, and invalidates previously issued
worker sessions for the affected application. It does not assume that anyone
has used those credentials without authorization. No credential values are
included here, and preparing this document does not perform live rotation.

## Establish the affected deployments

Identify each API instance using the old JWT secret and each SDK/worker or
application using the bootstrap API key. Confirm whether they are local only or
also used by external deployments. Keep an administrator recovery route and
use the existing secret manager or a protected local environment file for new
values. Never put a populated `.env`, exported signing key, or plaintext backup
in source control, terminal history or a support transcript. Record the affected
application's numeric database ID and name; its queue prefix is derived from
that ID. Plan a brief maintenance window with the owners of live consumers.

## Understand what each credential controls

- Disabling an API key blocks subsequent requests that validate that key,
  including bootstrap, HTTP job pulls and HTTP result submissions. It does not
  invalidate existing worker sessions or close RabbitMQ connections.
- Worker sessions are opaque database tokens, normally valid for 24 hours.
  Heartbeat, events and lease requests validate the session without checking the
  original API key. Sessions are associated with an application and instance,
  **not an API-key ID**; revocation cannot select sessions by their issuing key.
- Rebootstrap replaces the previous token for the same application and instance.
  Other instance tokens remain valid. `/workers/shutdown` records stopped status
  but does not revoke the token. There is currently no admin UI/API endpoint to
  revoke all worker sessions; the maintenance step below expires them directly.
- `JWT_SECRET` signs dashboard sessions only. Changing it does not revoke API
  keys, worker session tokens or broker credentials. Reducing
  `WORKER_SESSION_TTL` only changes the expiry of future worker sessions.

## Prepare replacement credentials

1. Sign in as an administrator with access to the affected application. In
   **Settings → API Keys**, create a replacement key for that same application.
   The corresponding dashboard API is `POST /api/apiKeys` through the bundled
   proxy, with `applicationId` and `name`. Preserve the generated key privately;
   it is returned only at creation.
2. Stage the replacement in every relevant SDK/client secret store. Prepare a
   cryptographically random `JWT_SECRET` of at least 32 characters, consistently
   for every affected API replica. Keep the current processes running until the
   drain step; changing a secret file does not update their loaded configuration.
3. If broker credentials were distributed by bootstrap or otherwise need
   rotation, prepare replacement broker credentials with the required queue
   permissions. `WORKER_RABBITMQ_URL` is handed to SDK workers; when unset,
   bootstrap falls back to `RABBITMQ_URL`. In that case its rotation also affects
   the API and internal worker. With broker hand-out disabled, update the SDK
   workers' locally configured broker credentials instead.
4. Remove the old bootstrap value from deployment configuration. On an
   initialized installation, changing `BOOTSTRAP_API_KEY` alone does **not**
   rotate a database key: bootstrap is intentionally a no-op for an existing
   application. Do not change the application name or delete its data to force
   bootstrap. If a bootstrap key is needed for a fresh installation, supply a
   distinct new value from its own secret store.

The `/api` prefix above is stripped by the bundled proxy. Direct internal API
requests use `/apiKeys` and `/apiKeys/disable`. Use the Settings UI or an
authenticated client that preserves the normal session/CSRF protections.

## Drain, revoke and restart

1. Pause new producer traffic and stop consumers from taking new jobs. Let
   in-flight handlers finish and deliver their results, then stop the affected
   SDK consumers. Do this before disabling the key: HTTP result submission still
   needs it. Account for outstanding HTTP acknowledgements before restarting the
   API. If a handler cannot drain, plan its retry/recovery before proceeding.
2. Disable the old key in Settings. The dashboard API uses
   `PUT /api/apiKeys/disable` with `apiKeyId`. Block new external API traffic and
   stop all API replicas that can bootstrap workers for the affected application;
   allow in-flight requests to settle. Keep this boundary closed through the
   database step. Disabling the key alone is insufficient: a bootstrap already
   past its key check could otherwise persist a new session after revocation.
3. In an authorized `psql` session against the affected deployment's database and
   application schema, expire **all** sessions for the verified application ID.
   This also expires legitimate sessions issued with other keys for that
   application, so every affected consumer must bootstrap again. Use the numeric
   database ID, not the `app_<id>` queue prefix. The prompt below contains no
   credential; do not query or print `session_token`.

   ```sql
   \set ON_ERROR_STOP on
   \prompt 'Verified numeric application ID: ' application_id
   BEGIN;
   SET LOCAL TIME ZONE 'UTC';
   UPDATE worker_client
   SET session_expires_at = CURRENT_TIMESTAMP - INTERVAL '1 second',
       updated_at = CURRENT_TIMESTAMP
   WHERE application_id = :'application_id'::integer;
   COMMIT;
   ```

   Record the affected-row count. This operator SQL is not recorded by the
   dashboard audit middleware; retain the maintenance record separately.
4. For broker credentials being rotated, revoke the old credential at RabbitMQ
   and explicitly close its existing connections. Updating a URL or expiring a
   database worker session does not itself disconnect an AMQP consumer. Include
   all services sharing that broker account in the restart plan.
5. Recreate the API replicas with the new `JWT_SECRET` and prepared broker
   configuration. For Compose, use the same project, environment file and
   Compose variant as the running installation; an ordinary container restart
   does not reload changed Compose environment files. Recreate affected services
   and confirm readiness without tearing down unrelated services or volumes.
6. Reopen API access, then start consumers with the replacement key and broker
   configuration. They must bootstrap afresh. Verify their processing before
   resuming producer traffic. Sign in to the dashboard again; every API replica
   must use the new JWT secret for old dashboard sessions to be rejected.

## Verify before ending maintenance

- A valid bootstrap request using the old API key returns an authorization
  failure; the replacement key bootstraps successfully for the same application.
- A previously valid worker session is rejected by a correctly formed heartbeat
  or lease request. Use a controlled pre-rotation session held privately, without
  putting its token in logs, terminal history or this record. A fresh session's
  heartbeat and lease requests succeed.
- A real test job completes with the new configuration, including result delivery
  and acknowledgement over the deployed HTTP or AMQP transport. A green heartbeat
  alone is not sufficient. Check the worker registry and queue backlog.
- Where broker credentials changed, the old login is rejected and no old-account
  connections remain. Confirm new consumers connect and process jobs.
- An old dashboard session is rejected, a new login works, and role/application
  restrictions remain effective. Check service readiness and API-key audit events.

If a consumer still needs the old key, keep its traffic paused, identify it and
complete its configuration update. Do not restore an exposed key as a lasting
rollback.

## Completion evidence

Record deployment identifiers and timestamps, old/new **key IDs** (not values),
successful new-key requests and completed jobs, rejection of old API keys,
worker sessions and dashboard sessions, service health, broker connection checks
and audit entries.
Rerun source/history scans. Retain only an encrypted recovery archive under the
operator's retention policy. Do not record rotation as complete until these
checks pass in every affected deployment.
