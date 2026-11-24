# Community v0.5.2 verification

Prepared on 2026-11-24 in [PR #31](https://github.com/pipelogiq/pipelogiq/pull/31).
The tested implementation is commit `f2ee97e`; subsequent release-report edits
do not change executable code. This is release preparation, not publication.
Version v0.5.1 is already published and must not be moved.

## Included changes

- Runtime dashboard base paths, including assets, browser routes, API calls,
  WebSockets, cookies, container probes, Compose and Helm configuration.
- VitaliTs's dashboard KPI card refresh, commit `9a7acdd`, merged with its
  authorship preserved. PRs #27 and #29 contain the same change; it is included once.
- WebSocket and OpenTelemetry dependency fixes for the vulnerabilities found
  during release validation, plus deterministic routing smoke-test startup.

## Verification completed

[Hosted CI for the implementation](https://github.com/pipelogiq/pipelogiq/actions/runs/45357982028)
passed all seven jobs: repository hygiene, Go, Web, PostgreSQL integration,
and app, worker and migration Docker builds/scans.

Additional local checks used a clean checkout, excluding unrelated working files:

| Check | Result |
| --- | --- |
| Frontend tests | 164 tests across 24 files passed |
| ESLint and production frontend build | Passed; existing bundle-size warning remains |
| npm dependency audit | No vulnerabilities reported |
| Go race tests and Pro tests | Passed |
| Go vet, Community and Pro | Passed |
| govulncheck, Community and Pro | No vulnerabilities found |
| Version references, OpenAPI coverage and repository/secret guards | Passed |
| Helm lint, prefixed rendering and package | Passed |
| Workflow lint | Passed |
| Actual app-image routing | Root, `/pipelogiq` and nested prefix passed |

The image routing tests cover deep links, assets/fonts, API forwarding, WebSocket
upgrade, cookie paths, invalid prefixes and startup reconfiguration.

A disposable PostgreSQL 16/RabbitMQ stack ran the candidate app, worker and
migration images. Migrations succeeded and a second run applied no changes.
Prefixed login, authenticated user lookup and the v0.5.2 version endpoint passed.
An HTTP worker completed a two-stage pipeline including a retry after a transient
stage failure. Browser login and the refreshed dashboard were also checked under
`/pipelogiq` with synthetic data.

## Publication and remaining deployment checks

No v0.5.2 tag, public release or registry images were published during preparation.
Merge PR #31 using a merge commit to preserve contributor ancestry, then wait for
CI on `main` and follow [RELEASE.md](../RELEASE.md) to publish the version tag.
The release workflow reruns CI for the tagged commit before publication.

After publication, verify the downloadable checksums and both image architectures,
then smoke-test the exact registry images and install the chart in a test namespace.
A live Kubernetes rollout was not performed here. Helm rendering is not evidence
of a successful cluster deployment.

SDK pairing remains Java and .NET 0.5.0. No external API or migration change was
introduced; full SDK consumer suites were not rerun for this patch.

For a shared QMS domain, route `/pipelogiq` without stripping the prefix and set
`PIPELOGIQ_BASE_PATH=/pipelogiq`. The assistant can use `/ai` independently.
Its deployment preparation must only be deployed after all v0.5.2 images exist.
