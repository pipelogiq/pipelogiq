# Pipelogiq v0.5.1

Release preparation dated **2026-11-19**. This Community release replaces the
incomplete v0.5.0 publication. Availability of release assets and registry images
must be verified after the release workflow succeeds.

| Component | Version |
| --- | --- |
| Server and dashboard | `0.5.1` |
| .NET SDK packages | `0.5.0` |
| Java SDK modules | `0.5.0` |
| Helm chart | `0.5.1` (app version `v0.5.1`) |
| App, worker and migration image tags | `v0.5.1` |

## Highlights

- Official binaries and Docker images ship Community without Pro implementations
  or a licence verification key. `LICENSE_PUBLIC_KEY` is no longer required or
  read by the release workflow. Pro launch is deferred; its source and CI coverage
  remain available for future development.
- Fix PostgreSQL startup when adopting application access from unchanged legacy
  demo accounts into the configured administrator account. The SQL explicitly
  types the administrator identifier used in the membership transfer.
- Release validation allows independently versioned SDKs while still requiring
  explicit SDK versions in release notes. This server patch does not require a
  new .NET or Java SDK release and makes no external API contract change beyond
  updating the reported version.
- Preserve the original v0.5.0 tag. The replacement is published through the
  normal CI-gated release workflow under v0.5.1.

## Breaking changes and upgrade requirements

No new database migration or external API breaking change is introduced relative
to the v0.5.0 source. Pro functionality is not available in these artifacts;
installations using a custom Pro build must account for that before switching.

For upgrades from preview versions, all [v0.5.0 upgrade requirements](RELEASE_NOTES_v0.5.0.md#breaking-changes-and-upgrade-requirements)
still apply: back up the database and deployment configuration, preserve
encryption keys, configure administrator access, and run migrations before the
API and worker. The old notes' requirement for `LICENSE_PUBLIC_KEY` and their
description of Pro release builds are superseded by this Community release.

Keep .NET and Java SDK consumers on `0.5.0`. Verify a representative pipeline
with the deployed workers after upgrading the server.

After all three images have been published and confirmed publicly accessible:

```bash
PIPELOGIQ_VERSION=v0.5.1 docker compose --env-file .env \
  -f infra/compose/docker-compose.registry.yml pull
PIPELOGIQ_VERSION=v0.5.1 docker compose --env-file .env \
  -f infra/compose/docker-compose.registry.yml up -d
docker logs pipelogiq-migrate
curl -fsS http://localhost:3300/api/readyz
curl -fsS http://localhost:3300/api/version
```

Confirm migrations completed successfully, the reported version is `v0.5.1`,
administrator access works, and a test pipeline completes. Follow
[operations](../operations.md#upgrade) for environment-specific upgrade steps.

## Validation and limitations

The Community workflow change was checked locally with actionlint, release
preflight without `LICENSE_PUBLIC_KEY`, all eight cross-platform binary builds,
`go test -race ./...`, and `go test -tags pro ./...`. Version guard tests cover
independent SDK versions and reject missing, duplicate or invalid SDK entries.

Full CI, including PostgreSQL integration, frontend, vulnerability and container
checks, must pass on the release commit before publication. The existing
[0.5.0 verification report](../release-verification-2026-09-18.md) describes prior
SDK verification; the full .NET and Java consumer suites have not been rerun for
this patch. No registry publication or deployment smoke test is implied by local
release preparation.

Verify anonymous access to all three GHCR packages, including the migration
image, and verify downloaded assets against `SHA256SUMS`. Rollback is not an
automatic database downgrade: test it on a restored backup as described in
[operations](../operations.md).
