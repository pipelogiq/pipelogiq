# New-feature security review — 2026-09-17

Reviewed the current working tree: Users/roles/application membership, Settings
access, user activity audit, pipeline run details/logs/context, session handling,
WebSockets and the Pro audit export. Confirmed application issues were fixed and
covered by regression tests. The dependency findings below describe the September 17
baseline; see the [September 18 follow-up](security-review-2026-09-18.md) for
updated dependencies, additional hardening and verification.

No application database was modified and no service was deployed. PostgreSQL
verification used disposable Docker databases created by the test script.

## Confirmed issues fixed

| Priority | Finding | Result and regression evidence |
| --- | --- | --- |
| High | An in-flight password change could overwrite a later administrator reset or logout and mint a new session. | Atomic update checks the verified password hash, request token version and active status. API regression rejects the revoked request; SQLite and PostgreSQL concurrent password changes have one winner. |
| High | Pro stage-audit exports included sensitive values masked elsewhere. | Export masks current and historical sensitive values, fails on lookup errors, and reads logs/masks in one repeatable-read transaction. PostgreSQL regression commits a new secret/log between the two reads and verifies it cannot leak into the export. |
| High | Historical migrations left accounts with publicly shipped demo password hashes active when bootstrap was not configured. | Additive migration disables exact unchanged hashes, including renamed accounts, and revokes sessions. Bootstrap preserves identity/history and real accounts with changed passwords. Login rejects those demo hashes even if an account is reactivated. |
| Medium | An administrator could update an application outside their assigned membership. | Application update enforces the same membership check as other application operations; out-of-scope targets return 404. |
| Medium | Keyword suggestions exposed keys belonging to other applications, including to accounts with no memberships. | Suggestions require a pipeline or application-log link within the caller's applications. Empty membership returns an empty list. |
| Medium | Established WebSockets remained usable past JWT expiration. | Verified expiry is required at upgrade, checked on broadcast/write, and an expiry timer closes idle connections. |
| Medium | Login IP throttling could be bypassed with a new TCP port or forged forwarding headers. | Limiter normalizes IPv4/IPv6 addresses. API trusts only a single valid X-Real-IP from a loopback proxy; bundled nginx overwrites it and strips True-Client-IP. Other peers retain their transport address. |
| Medium | Oversized/invalid target text could prevent PostgreSQL from recording a failed operation. | Audit fields are bounded by character count and made valid UTF-8 without NUL. API and actual PostgreSQL regressions cover long ASCII, Unicode and malformed targets. |
| Client state | Late login/auth-check responses could restore logged-out state or stale administrator UI permissions. | Session generation and latest-request guards ignore obsolete completions. Server authorization remains the enforcement boundary. |

Additional hardening: login/password JSON is limited to 64 KiB and one strict
object; JWTs require the expected issuer and expiration; unknown-account bcrypt
work matches the default cost of newly created accounts. Tests also cover hostile
markup rendered as text, administrative route guards, cache isolation, last-admin
protection, role filters and the new dropdown controls.

Bootstrap accepts only valid non-demo bcrypt hashes. A replacement configured
hash can reactivate a disabled administrator and revokes older sessions when
security state changes. Unchanged restarts preserve sessions; no replacement
hash leaves a disabled account disabled. Memberships of unchanged demos are
copied to the configured administrator without deleting historical identities.

## Verification

- Web: **150 tests across 21 files passed**; ESLint, TypeScript and production
  build passed. The existing bundle-size warning remains.
- Go: full `go test -race -count=1 ./...` passed: **622 tests/subtests passed**,
  with 6 PostgreSQL-only tests exercised separately rather than relying on
  their normal skipped results.
- Pro: `go test -race -tags pro ./internal/pro -count=1` passed, including export
  masking and failure behavior.
- PostgreSQL: **12 top-level tests / 47 including subtests passed** against the
  actual Liquibase schema and isolated test schemas, with the race detector.
  Coverage includes audit filters/text bounds/export snapshot, membership
  filters, user uniqueness, concurrent demotion/password changes, API-key
  hashing, idempotency and execution fencing.
- Upgrade: role normalization, token revocation, retained memberships, duplicate
  email rejection/recovery, renamed demo credentials, preserved changed
  passwords, and migration re-application passed.
- Go vet/build for Community and Pro, Go formatting, nginx configuration syntax,
  external OpenAPI route coverage and version-reference checks passed.

Reproduce the database checks with:

```sh
sh database/test-user-management-upgrade.sh
```

CI now discovers every PostgreSQL test using `-run Postgres`; the previous
`TestPostgres_` expression omitted newer security tests. The upgrade script is
also wired into CI.

## Dependency findings still open

Compatible npm security updates and Vitest 4.1.11 reduced `npm audit` from
**22 affected packages (including high/critical) to 2 moderate packages**:
`react-router` and `react-router-dom` in the same dependency chain. The installed
6.x line still carries redirect/SSR-hydration advisories; the reported full fix
requires upgrading to 7.x. This application uses BrowserRouter/createRoot,
without SSR hydration or a user-controlled redirect query parameter; no
application exploit for these remaining advisories was demonstrated. See the
[redirect advisory](https://github.com/advisories/GHSA-wrjc-x8rr-h8h6) and
[SSR advisory](https://github.com/advisories/GHSA-337j-9hxr-rhxg).

Updated `github.com/golang-jwt/jwt/v5` from 5.2.0 to 5.2.2 for
[GO-2025-3553](https://pkg.go.dev/vuln/GO-2025-3553). Removed use of chi's
unconditionally trusting RealIP middleware.

The final Go vulnerability scan still reports **27 unique advisories with
reachable symbols** under the local Go 1.26.1 toolchain. This is static
reachability, not 27 proven application exploits; some reports have additional
platform/configuration preconditions. These findings were not hidden or waived.

| Component scanned | Installed | Reported fixed version for remaining reachable reports | Example advisory |
| --- | --- | --- | --- |
| Go standard library | 1.26.1 locally | 1.26.6 covers the 20 reported stdlib advisories on this line | [GO-2026-6090](https://pkg.go.dev/vuln/GO-2026-6090) |
| pgx/v5 | 5.5.4 | 5.9.2 | [GO-2026-5004](https://pkg.go.dev/vuln/GO-2026-5004) |
| amqp091-go | 1.9.0 | 1.13.0 | [GO-2026-6372](https://pkg.go.dev/vuln/GO-2026-6372) |
| grpc | 1.71.0 | 1.83.1 | [GO-2026-6348](https://pkg.go.dev/vuln/GO-2026-6348) |
| OpenTelemetry | 1.40.0 | 1.41.0 | [GO-2026-5506](https://pkg.go.dev/vuln/GO-2026-5506) |
| OTLP HTTP trace exporter | 1.35.0 | 1.43.0 | [GO-2026-4985](https://pkg.go.dev/vuln/GO-2026-4985) |
| x/text | 0.28.0 | 0.39.0 | [GO-2026-5970](https://pkg.go.dev/vuln/GO-2026-5970) |

Go release Dockerfiles still select the 1.24 line, so the local scan does not
certify those release images. Updating Go and the runtime dependencies requires
aligning Docker/CI toolchains and repeating the worker/telemetry/database checks.
Treat that dependency upgrade and the Router migration as remaining work before
claiming a clean dependency scan.

## Operational limits

Apply `database/changelog.xml` before rolling out this code. The added demo
credential migration has not been applied to any running application database
by this review. New JWTs retain the existing issuer; correctly issued current
tokens remain compatible with the stricter parser.

Audit persistence is still best effort rather than transactional with every
mutation. Login limits are per API process. Remote reverse proxies are treated
as their transport IP; only the bundled loopback proxy gets forwarded client-IP
trust. Browser automation of a live production deployment, an infrastructure
penetration test, and a release-image/container vulnerability scan were outside
this review.
