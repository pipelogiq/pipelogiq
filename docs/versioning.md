# Versioning and compatibility

This document is the promise behind the version number. It says what is covered by
semantic versioning, what is not, how long a deprecated element keeps working, and
what an upgrade guarantees.

## What is covered

The **stability contract** applies to:

1. **The external API** on `:8081`, exactly as described in
   [`docs/openapi.external.yaml`](openapi.external.yaml). Every route in the server
   is in that file — CI fails otherwise — so the file *is* the contract.
2. **The HTTP worker protocol** in [`docs/http-workers.md`](http-workers.md).
3. **The AMQP wire messages** a worker consumes and produces (`StageNext`,
   `StageResult`, `StageSetStatus`, `StageUpdated`) as carried by
   the .NET and Java SDKs.
4. **Configuration**: environment variable names and their meaning, and the
   Docker image entrypoints.
5. **Database upgrades**: migrations are re-runnable and applied before new code
   starts. Data changes and constraints that require operator action are listed
   in release notes; rollback compatibility must be verified separately.
6. **The .NET and Java SDK public surfaces**, versioned in their respective
   repositories; compatibility requirements are documented per SDK.

## What is not covered

- **The internal API** on `:8080`. It exists to serve the dashboard that ships in
  the same image. Anything can change there between any two releases. Do not
  integrate against it.
- **The dashboard's HTML, routes, and behaviour.**
- **Prometheus metric names and labels.** They are operational, not contractual,
  and will be renamed when clarity demands. Renames are listed in the changelog.
- **Log message text.**
- **Anything marked `preview` or `experimental` in the OpenAPI description** —
  the Pro feature gates named as "next release" fall here until they ship.

## Version numbers

`MAJOR.MINOR.PATCH`, with an optional `-preview.N` or `-rc.N` pre-release suffix.

| Change | Bump |
|---|---|
| Removing or renaming anything in the covered set | MAJOR |
| Changing the meaning of a covered field, status code, or environment variable | MAJOR |
| A schema migration that is not additive or not re-runnable | MAJOR |
| Adding a route, field, message, option, or environment variable | MINOR |
| A change that requires action at upgrade time even if compatible (a new required variable, a moved port) | MINOR, with an **Upgrade notes** section in the release notes |
| Bug fixes, performance, dependency updates | PATCH |

While the version is `0.x`, MINOR may carry breaking changes; each is listed
under a **Breaking changes** heading in the changelog. From `1.0.0` that
heading only ever appears in a MAJOR release.

## Deprecation

Before `1.0.0`, minor releases may remove or change APIs with an explicit breaking
change and migration instructions. The following deprecation policy is the
contract for the future `1.x` line:

1. The element is marked `deprecated: true` in OpenAPI (or `[Obsolete]` in the SDK)
   and named in the changelog, with its replacement.
2. It keeps working for **at least two MINOR releases and 180 days**, whichever is
   longer.
3. It is removed in the next MAJOR release.

Deprecations are recorded in the contract and changelog so callers can identify
the replacement before upgrading.

## Supported versions

- Before `1.0.0`, fixes target the **current release line**, as stated in the
  [security policy](../SECURITY.md). Older lines do not have a separate maintenance
  guarantee.
- Pre-release versions (`-preview.N`, `-rc.N`) are supported only until the next
  pre-release or the release they precede.

## Upgrade guarantees

- **Upgrade coverage is recorded per release.** Migrations are cumulative, but
  passing a schema upgrade test does not certify every previous deployment or
  configuration. Follow the release-specific prerequisites and verification.
- **Migrations run before the new code starts** and are re-runnable: an
  interrupted upgrade can be retried by starting the same version again.
- **Rollback requires verification on a backup.** New constraints, credential
  storage and role changes can affect older binaries. An additive schema alone
  does not establish rollback safety; there are no automatic downgrade migrations.
- **Server first, then SDK.** A newer SDK may call routes an older server does
  not have. An older SDK keeps working against a newer server; it does not
  participate in features it does not know about.

## Server and SDK pairing

The server, .NET SDK and Java SDK are versioned in separate repositories.
The coordinated current release is **0.5.0 for all three**. Each server release
names the SDK version it was verified with in its release notes; each SDK release
documents its minimum server version in its compatibility/release guide. Matching
MINOR numbers are a convenience, not a requirement.

## How this is enforced

- `scripts/check-openapi-routes.sh` — a route without a contract entry fails CI.
- `VERSION` defines the server release target. `scripts/check-versions.sh` checks
  the dashboard manifests, Go default, image/Compose defaults, Helm chart, README,
  changelog, OpenAPI, current notes and deployment examples against it. Passing a
  tag also requires an exact match, so a mistyped tag cannot publish blank notes.
- `database/test-reliability-upgrade.sh` — the migration set is applied to a
  copy of the previous release's schema in CI.
- The tag-triggered release workflow validates the exact tag and runs the reusable
  CI suite before publication. It packages artifacts before publishing images and
  creates the GitHub release last. Tags themselves are created by a maintainer.
