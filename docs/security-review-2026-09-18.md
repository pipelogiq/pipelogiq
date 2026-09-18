# Dependency security follow-up — 2026-09-18

Continues the [September 17 feature/security review](security-review-2026-09-17.md).
All changes remain local; no running application database was modified and no
service was deployed.

## Dependencies and toolchains

- React Router upgraded to **7.18.4** using its supported compatibility package.
  Existing declarative routes remain intact; obsolete v6 test-router future
  flags were removed. Full `npm audit` now reports **0 vulnerabilities**,
  including development dependencies.
- Go module and Docker builders now require **1.26.8**. CI and release jobs
  select the module's version, so local automatic toolchain selection and image
  builds use the same security patch level.
- Updated pgx **5.9.2**, AMQP client **1.13.0**, chi **5.3.2**, gRPC **1.83.2**,
  OpenTelemetry core/exporters **1.46.0**, HTTP instrumentation **0.71.0**, and
  the x/crypto, x/net and x/text dependency chains.
- Web CI and Docker builds use **Node 24 LTS**. The application runtime uses
  nginx **1.30.5**. The repository now has its own `.dockerignore`, excluding
  local environment files, data, node_modules and build outputs from images.
- Migrated golangci-lint configuration and CI to **2.13.2**, preserving enabled
  checks and previous exclusions. Corrected the Trivy action reference and pinned
  its commit, with scanner **0.74.0**. CI now audits all npm dependencies at
  moderate severity and scans both Community and Pro Go builds.

The final Linux/amd64 `govulncheck` scans for Community and Pro report **0
reachable vulnerabilities and 0 vulnerabilities in imported packages**. The
only remaining module-level advisory is
[GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932), covering the unmaintained
`golang.org/x/crypto/openpgp` package. Pipelogiq imports bcrypt/blowfish from that
module, not OpenPGP; the affected package is absent from the application's
dependency graph. This advisory has no fixed version and was not suppressed.

## Additional corrections

- API and metrics listeners have a five-second header timeout. Metrics shutdown
  has a bounded grace period and closes remaining connections on failure.
- Logout, invalid-token and revoked-session responses delete cookies using the
  same HttpOnly, SameSite and configurable Secure policy as session issuance.
- RabbitMQ cancellation now uses the actual registered consumer tag and handles
  cancellation errors. Passive queue inspection uses the supported client API
  with equivalent wire flags.
- Removed unused helpers and overwritten assignments flagged by the updated
  linter. Narrow gosec annotations explain existing sanitized serialization,
  configurable local-HTTP cookies and the local Compose broker default; no
  security check was globally disabled for this follow-up.

## Application verification

- Web: **150 tests / 21 files**, ESLint, TypeScript and production build passed.
  The existing large-bundle warning remains; Router migration added approximately
  5.8 KiB gzip.
- Community: **638 passing tests/subtests** with the race detector.
- Pro: **658 passing tests/subtests** with the race detector.
- Both editions: vet and build passed; full golangci-lint reports **0 issues**.
- Both normal test suites skip six PostgreSQL-dependent tests; these are
  exercised separately against disposable PostgreSQL databases.
- New telemetry tests send actual OTLP HTTP and gRPC spans to local test
  collectors and verify configured headers/resource attributes. AMQP propagation
  tests cover traceparent, tracestate, baggage and malformed inputs.
- New cookie-removal and consumer-cancellation regressions passed.
- Formatting, diff whitespace, OpenAPI route coverage and version-reference
  checks and CI workflow validation passed.

## Release images and migrations

The old migration image had **32 fixable High/Critical findings**. Merely changing
its Liquibase tag did not resolve them. The final PostgreSQL migration image uses
Liquibase **4.33.0 OSS**, refreshed Temurin **21.0.12**, OS security updates and
SHA-256-pinned pgJDBC **42.7.12**. Unused commercial extensions, non-PostgreSQL
drivers and the LPM package manager were removed; the CLI still runs as its
unprivileged user. The driver version includes the current
[channel-binding security fix](https://jdbc.postgresql.org/security/).

Trivy **0.74.0**, with vulnerability database timestamp **2026-09-18T01:11:25Z**,
reports **0 fixable High/Critical findings** in each final linux/amd64 image:
application, worker and migration. OS and language dependencies were included;
application/worker builds included the Pro tag. No advisory ignore list was
added. This severity/fix-availability result does not claim absence of all
lower-severity or unfixed issues. nginx configuration validation passed under
the image's unprivileged user. The migration image also passed its linux/arm64
build, CLI/JRE smoke check and equivalent Trivy scan.

Functional verification of the actual migration image passed:

- Fresh PostgreSQL database: all **53 changesets** applied; a second update ran
  **0 changesets**.
- Upgrade from the previously shipped Liquibase **4.25** engine: role/session
  migration, membership retention, duplicate-email rejection/recovery, unchanged
  demo credential disabling, and preservation of changed passwords passed.
- Reliability upgrade from the pre-idempotency schema preserved historical rows
  and added the expected defaults/indexes. Its baseline now uses the named
  changeset boundary instead of assuming these are the last two changesets.
- **12 PostgreSQL tests / 47 including subtests** passed with the race detector,
  including concurrent account changes and the audit-export redaction snapshot.

CI builds and tests the same migration Dockerfile used for release. Both upgrade
scripts build it by default, with optional `LIQUIBASE_IMAGE` and
`BASELINE_LIQUIBASE_IMAGE` overrides. All database checks used disposable
containers/networks, which were cleaned up.

```sh
sh database/test-user-management-upgrade.sh
sh database/test-reliability-upgrade.sh
```

The OSS-only Liquibase image prints a nonfatal warning about its absent Pro
license service. Pipelogiq's PostgreSQL changelog uses no Liquibase Pro features;
the warning does not prevent migrations and is separate from Pipelogiq licensing.

## Scope and operations

Dependency scans describe the current source and inspected build artifacts, not
all possible application exploits. Operational limitations from the earlier
review still apply: audit writes are best effort, login throttling is per API
process, and only the bundled loopback proxy is trusted for forwarded client IPs.
Production deployments must configure broker credentials and the intended
Secure-cookie/TLS policy. No live deployment penetration test was performed.
