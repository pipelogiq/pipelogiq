# Rotating exposed Pipelogiq credentials

Removing credentials from Git does not revoke them. The September 18 preparation
removed an environment backup from unpublished history, but the JWT signing
secret and bootstrap API key still match the running local installation.
The deployment/consumer scope must be established before completing rotation.
No credential values are included in this document.

## Establish the affected deployments

Identify each API instance using the exposed JWT secret and each SDK/worker or
application using the bootstrap API key. Confirm whether they are local only or
also used by external deployments. Keep an administrator recovery route and
use the existing secret manager or a protected local environment file for new
values. Never put a populated `.env`, exported signing key, or plaintext backup
in source control, terminal history or a support transcript.

## Replace the API key

1. Sign in as an administrator with access to the affected application. In
   **Settings → API Keys**, create a replacement key for that same application.
   The corresponding dashboard API is `POST /api/apiKeys` through the bundled
   proxy, with `applicationId` and `name`. Preserve the generated key privately;
   it is returned only at creation.
2. Update the relevant SDK/client secret stores and restart those consumers.
   Verify an authenticated request and a pipeline run with the replacement.
3. Disable the old key in Settings. The dashboard API uses
   `PUT /api/apiKeys/disable` with `apiKeyId`. Verify the old key returns an
   authorization failure and the new key still works. Check the audit events.
4. Remove the exposed bootstrap value from deployment configuration. On an
   initialized installation, changing `BOOTSTRAP_API_KEY` alone does **not**
   rotate a database key: bootstrap is intentionally a no-op for an existing
   application. Do not change the application name or delete its data to force
   bootstrap. If a bootstrap key is needed for a fresh installation, supply a
   distinct new value from its own secret store.

The `/api` prefix above is stripped by the bundled proxy. Direct internal API
requests use `/apiKeys` and `/apiKeys/disable`. Use the Settings UI or an
authenticated client that preserves the normal session/CSRF protections.

## Replace the JWT signing secret

Generate a cryptographically random value of at least 32 characters and replace
`JWT_SECRET` consistently on every affected API instance. Recreate/restart those
instances with the new environment; an ordinary container restart does not
reload changed Compose environment files. Existing login sessions will be
invalidated. Sign in again and verify that an old session is rejected, a new
session works and role/application restrictions remain effective.

For Compose, use the same project, environment file and Compose variant as the
running installation. Recreate only the affected app service and confirm its
health; do not tear down unrelated services or volumes. Stop the rollout if a
consumer still depends on the old key, identify that consumer and complete its
configuration update. Do not restore a known exposed key as a lasting rollback.

## Completion evidence

Record deployment identifiers and timestamps, old/new **key IDs** (not values),
successful new-key requests, old-key/session rejection, service health and audit
entries. Rerun source/history scans. Retain only an encrypted recovery archive
under the operator's retention policy. This preparation did not perform live
rotation, and it must not be recorded as complete until the rejection checks
pass in every affected deployment.
