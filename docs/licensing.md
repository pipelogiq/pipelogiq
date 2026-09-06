# Licensing

Pipelogiq has an Apache-2.0 core and a commercial tier next to it.

The rule that decides which side a feature falls on is one sentence:
**what one engineer needs to run and debug a pipeline is free; what an
organization needs to govern, comply, and scale is Pro.**

The core is complete for a single developer. Pipelines, stages, retries, leases,
fencing, cancellation, schedules, the HTTP worker protocol, the dashboard, the
observability bridge, and every API in `docs/openapi.external.yaml` are
Apache-2.0 and stay that way. Adding a commercial tier never relicenses existing
code.

## Three guarantees

1. **A licence never stops a pipeline.** No code path in execution consults the
   licence. Gating happens at the API surface, before work is created. An
   expired licence means new Pro *requests* answer `402`; everything already in
   the system runs to completion.
2. **A licence check never touches the network.** Verification is a local
   Ed25519 signature check against a public key compiled into the binary. There
   is no activation call, no heartbeat, and no telemetry — by construction, not
   by configuration.
3. **A licensing problem degrades, it does not fail.** Missing, malformed,
   expired, unreadable: every failure resolves to Community and logs why. A
   deployment never refuses to start because of a licence.

## The token

`PIPELOGIQ_LICENSE` holds a signed JWT (Ed25519, `EdDSA`). Its claims:

| Claim | Meaning |
|---|---|
| `sub` | The licensed company |
| `tier` | `pro` or `enterprise` |
| `engineers` | The engineer band ceiling — a commercial term, not an enforced limit |
| `features` | The explicit list of gate names this token unlocks |
| `iat`, `exp` | Issued-at and expiry |

`features` is an explicit list, not a consequence of the tier. A tier can
therefore be extended without shipping a new binary, and cannot be widened by
accident: a `pro` token that does not name `retention` does not unlock it.

The signing algorithm is pinned to `EdDSA`, so a token cannot ask to be verified
with a weaker one — `alg: none` and an HMAC downgrade are both rejected.

## Expiry and the grace period

An expired token keeps every feature for **14 days**, with a banner in the
dashboard and a warning in the startup log. After that the deployment is
Community. Pipelines are unaffected at every stage of this.

## The public key

`apps/go/internal/license/pubkey.go` holds the verification key. Publishing it is
intentional and safe: it verifies signatures and cannot create them.

The open repository ships an all-zero placeholder, so a build from a clone is
Community-only whatever token it is given. A release build supplies the real key
at link time:

```bash
go build -ldflags "-X pipelogiq/internal/license.PublicKeyBase64=<base64>" ./cmd/api
```

The matching private key lives only in `cmd/licensegen`, a small signing tool
kept in a separate private repository and never shipped in an image.

## The build seam

Pro code is compiled only under the `pro` build tag.

| Build | What is linked | Who does it |
|---|---|---|
| `go build ./...` | `internal/pro/stub.go` — every call returns `ErrNotLicensed` | Public CI, and anyone cloning the repository |
| `go build -tags pro ./...` | The real implementations | The release image |

So there is **one image**, not two. The tag decides what is compiled; the licence
decides what is allowed. A Community deployment of the released image behaves
exactly like a build without the tag, because every gate is closed.

Public CI builds and tests both ways, so the open repository cannot drift into
requiring private code.

## Gating

A handler gates with one call and returns:

```go
if !s.requireFeature(w, r, license.FeatureRetention) {
    return
}
```

A closed gate answers `402` with `application/problem+json`:

```json
{
  "type": "https://api.pipelogiq.dev/errors/license-required",
  "title": "Licence required",
  "status": 402,
  "detail": "Scheduled purge of finished pipelines, stage IO and logs requires a Pro licence. no licence configured",
  "errors": { "feature": ["retention"] }
}
```

The body names the gate, so an operator knows exactly what to buy.

## Feature gates

Gates that have code behind them today:

| Gate | What it adds |
|---|---|
| `sso` | OIDC login with group-to-role mapping. Local accounts remain for break-glass access. |
| `audit.export` | Signed NDJSON export of the audit trail per application and date range. |
| `retention` | Configurable purge of finished pipelines, stage IO, and logs, with optional archive before purge. |

Gates named now, implemented in a later release. They exist so the pricing page
and the code cannot drift apart:

`schedules.advanced` · `alerting.oncall` · `approvals.slack` ·
`ha.multi-instance` · `agent.cost-caps`

The dashboard renders every gate, locked or not, with an explanation and a link
to pricing. Hiding a feature would only make an operator wonder whether it
exists.

## Reading the status

```http
GET /license
```

Returns the tier, the company, the engineer band, expiry, grace state, the
reason for a Community result, whether the Pro code is compiled in, and the full
feature catalogue.

`GET /version` also carries `tier`, so an operator can read it from the same
place they read the version.

## Configuring the Pro features

### SSO

| Variable | Meaning |
|---|---|
| `SSO_ISSUER` | OIDC issuer URL. Unset means licensed but not configured. |
| `SSO_CLIENT_ID`, `SSO_CLIENT_SECRET` | Client credentials |
| `SSO_SCOPES` | Default `openid profile email` |
| `SSO_GROUP_CLAIM` | Default `groups` |
| `SSO_GROUP_ROLE_MAP` | `group=Role` pairs, e.g. `pipelogiq-admins=Admin,pipelogiq-viewers=Viewer` |

A login whose groups match nothing in the map is **refused**, not defaulted. A
group added by accident cannot grant access. Local accounts always keep working,
so a misconfigured provider cannot lock an operator out.

Entra ID and Google need no special code path — they are ordinary OIDC issuers.

### Audit export

```http
GET /audit/export?applicationId=1&from=2026-01-01T00:00:00Z&to=2026-02-01T00:00:00Z
```

Streams NDJSON: one `record` object per audit row, then a `trailer` carrying the
count and a signature over the SHA-256 of everything above it. A consumer can
verify an export without the database, and cannot silently drop a record from the
middle of a file.

Set `AUDIT_EXPORT_SIGNING_KEY` (base64 of a 32-byte Ed25519 seed) to sign
exports. Without it the trailer says `"algorithm": "none"` and carries no
signature — visibly unsigned rather than silently so.

### Retention

```http
GET  /retention/{applicationId}
PUT  /retention/{applicationId}     { "keepDays": 90, "enabled": true, "archiveTarget": "s3://bucket/prefix" }
POST /retention/{applicationId}/purge
```

`keepDays` is 30, 90, or 365. An arbitrary value is rejected rather than rounded,
so what an operator picked is what runs.

A purge deletes finished pipelines past the window, and their stages, stage IO,
stage options, logs, context, and keywords. **It never touches an unfinished
pipeline**, whatever the window says: a pipeline that has been running for a year
is a problem to investigate, not data to delete.

Community keeps the current unbounded behaviour. To trim manually:

```sql
-- Inspect first.
SELECT count(*) FROM pipeline
WHERE application_id = 1 AND is_completed = true AND finished_at < now() - interval '90 days';

-- Then delete children before parents, in one transaction.
BEGIN;
CREATE TEMP TABLE purge_pipeline AS
  SELECT id FROM pipeline
  WHERE application_id = 1 AND is_completed = true AND finished_at < now() - interval '90 days';
CREATE TEMP TABLE purge_stage AS
  SELECT id FROM stage WHERE pipeline_id IN (SELECT id FROM purge_pipeline);

DELETE FROM stage_log     WHERE stage_id IN (SELECT id FROM purge_stage);
DELETE FROM stage_io      WHERE stage_id IN (SELECT id FROM purge_stage);
DELETE FROM stage_options WHERE stage_id IN (SELECT id FROM purge_stage);
DELETE FROM stage         WHERE pipeline_id IN (SELECT id FROM purge_pipeline);
DELETE FROM pipeline_context_item WHERE pipeline_id IN (SELECT id FROM purge_pipeline);
DELETE FROM pipeline_keyword      WHERE pipeline_id IN (SELECT id FROM purge_pipeline);
DELETE FROM pipeline      WHERE id IN (SELECT id FROM purge_pipeline);
COMMIT;
```

Take a backup first and run it in a transaction you are willing to roll back.

## Commercial terms

See [LICENSE-PRO.md](../LICENSE-PRO.md).
