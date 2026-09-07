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
   `pipelogiq-sdk-net`.
4. **Configuration**: environment variable names and their meaning, and the
   Docker image entrypoints.
5. **The database schema's forward compatibility**: every release's migration is
   additive and re-runnable; a release N-1 binary can run against a schema
   migrated to N.
6. **The .NET SDK public surface** — the public types in `PipelogiqSDK.Core`,
   `PipelogiqSDK`, and `PipelogiqSDK.Http`, versioned independently of the server.

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

Nothing in the covered set is removed without first being deprecated:

1. The element is marked `deprecated: true` in OpenAPI (or `[Obsolete]` in the SDK)
   and named in the changelog, with its replacement.
2. It keeps working for **at least two MINOR releases and 180 days**, whichever is
   longer.
3. It is removed in the next MAJOR release.

A deprecated route logs one warning per calling application per day, so an
operator finds out before the removal, not after.

## Supported versions

- The **current MINOR** receives fixes.
- The **previous MINOR** receives security fixes for 180 days after the current
  one is released.
- Pre-release versions (`-preview.N`, `-rc.N`) are supported only until the next
  pre-release or the release they precede.

## Upgrade guarantees

- **N-1 → N is always supported** without an intermediate step. Skipping more
  than one MINOR is expected to work, because migrations are cumulative and
  additive, but is tested only across adjacent versions.
- **Migrations run before the new code starts** and are re-runnable: an
  interrupted upgrade can be retried by starting the same version again.
- **Rollback to N-1** is supported by pinning the previous image; the schema is
  not rolled back, because N-1 ignores the columns N added. Data written by
  features that only exist in N (a schedule, a lease) stays in the database and
  is inert under N-1.
- **Server first, then SDK.** A newer SDK may call routes an older server does
  not have. An older SDK keeps working against a newer server; it does not
  participate in features it does not know about.

## Server and SDK pairing

The server and the .NET SDK are versioned independently. Each server release
names the SDK version it was verified with in its release notes; each SDK release
names the minimum server version it needs in `docs/compatibility.md`. Matching
MINOR numbers are a convenience, not a requirement.

## How this is enforced

- `scripts/check-openapi-routes.sh` — a route without a contract entry fails CI.
- `scripts/check-versions.sh` — the README badge, changelog heading, OpenAPI
  `info.version` and quickstart pinning must agree.
- `database/test-reliability-upgrade.sh` — the migration set is applied to a
  copy of the previous release's schema in CI.
- The release workflow refuses to tag a build whose tests are red.
