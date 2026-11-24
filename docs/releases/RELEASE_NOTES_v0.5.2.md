# Pipelogiq v0.5.2

Release preparation dated **2026-11-24**. This Community patch adds deployment
under a URL prefix and includes VitaliTs's Dashboard metric-card refresh from
[PR #27](https://github.com/pipelogiq/pipelogiq/pull/27). PR #29 contains the same
commit and is included once. Publication is performed by the tag-triggered release
workflow after its checks pass.

| Component | Version |
| --- | --- |
| Server and dashboard | `0.5.2` |
| .NET SDK packages | `0.5.0` |
| Java SDK modules | `0.5.0` |
| Helm chart | `0.5.2` (app version `v0.5.2`) |
| App, worker and migration image tags | `v0.5.2` |

## Highlights

- Set `PIPELOGIQ_BASE_PATH=/pipelogiq` when starting the combined app image to
  host the dashboard at `https://example.com/pipelogiq`. The same image also works
  at the domain root with an empty setting; no per-customer frontend build is needed.
- Browser navigation, direct links, static assets, API requests and live WebSockets
  retain the prefix. Session cookies use the installation path, allowing separate
  test/live installations on one domain. Container startup rejects invalid prefixes.
- Dashboard KPI cards use responsive container layouts and status accents. Healthy
  zero and unknown values remain neutral; nonzero warnings and errors stand out.
- Docker CI exercises real nginx routing for root, single and nested prefixes,
  including asset/font loading, cookie rewriting and WebSocket upgrades.

- Update Gorilla WebSocket to its fixed commit and OpenTelemetry to 1.47.0
  (HTTP instrumentation 0.72.0), addressing GO-2026-6278, GO-2026-6974 and GO-2026-6975.

## Breaking changes and upgrade requirements

No database migration, worker protocol or SDK change is introduced by this patch.
The default dashboard URL remains `/`. Existing deployments can upgrade without
setting a base path. Official images and binaries remain Community builds.

For a prefixed deployment:

1. Set `PIPELOGIQ_BASE_PATH=/pipelogiq` on the **app container**. Use a leading slash
   and no trailing slash; letters, digits, underscores and hyphens are supported.
2. Route `/pipelogiq` and descendants to app port 8080 **without stripping the prefix**.
   With Helm, set `app.basePath=/pipelogiq`; the ingress and probes follow this value.
3. Use `/pipelogiq/api/healthz` and `/pipelogiq/api/readyz` for browser-port probes.
   Configure `COOKIE_SECURE=true` when TLS terminates at the ingress.
4. Update dashboard links in consuming applications to include the prefix. The
   external SDK API on port 8081 is unchanged. Sign in again after changing paths.

The published `v0.5.1` image does not support this setting. Pull all `v0.5.2` images
only after their publication succeeds. Keep existing database/encryption secrets
and run the migration image before starting app and worker, as in earlier releases.

## Validation and limitations

See [the release verification record](../release-verification-2026-11-24.md) for
checks actually run and remaining hosted/deployment checks. The release workflow
must pass against the final tagged commit before any publication is considered complete.

SDK pairing remains .NET and Java `0.5.0`; the prior consumer compatibility evidence
still applies because this patch does not change the external API. Full SDK suites
are not claimed to have been rerun as part of this frontend patch.

Prefix support is implemented in the combined nginx/app image. The standalone Go
API retains its existing routes. A custom static frontend deployment must supply
an equivalent base element, runtime metadata and reverse-proxy configuration;
simply rewriting the ingress path for an older bundle is insufficient.
