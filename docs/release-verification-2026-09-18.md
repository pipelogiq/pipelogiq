# Coordinated 0.5.0 release verification — 2026-09-18

The release target is **0.5.0 without a prerelease suffix** for Pipelogiq,
its dashboard, .NET SDK and Java SDK. This is local release preparation, not a
record of registry publication.

## Version alignment

| Surface | Checked value |
| --- | --- |
| Server `VERSION`, Go source fallback and Make build metadata | `0.5.0` / `v0.5.0` |
| Web package and lockfile root | `0.5.0` |
| OpenAPI, README, current changelog and release notes | `0.5.0` |
| App, worker, migration Docker/Compose defaults | `v0.5.0` |
| Helm chart / appVersion | `0.5.0` / `v0.5.0` |
| Seven .NET packages and their internal dependencies | `0.5.0` |
| .NET assembly/file versions | `0.5.0.0` |
| Java parent, seven libraries and verification modules | `0.5.0` |
| Java installed/embedded Maven metadata and worker diagnostics | `0.5.0` |

The unpublished server `0.6.0-preview.1` draft was replaced with the coordinated
`0.5.0` notes. Existing historical changelog and preview release entries were
preserved. External library/tool versions and example application versions are
independent of the Pipelogiq product version.

## Checks performed

- Server version guard, including exact `v0.5.0` tag: passed. Nine regression tests
  cover wrong/preview tags, incomplete notes, empty changelog, manifest drift and
  stale deployment pins. The checks run in CI.
- Go version tests (including race detection and the Pro build) and package vet:
  passed. Local API/worker builds using Make embed `v0.5.0`; their build records
  retain the commit and dirty-tree status.
- Dashboard production build: passed as `@pipelogiq/web@0.5.0`. Vite retains its
  existing advisory about a JavaScript chunk over 500 kB.
- OpenAPI route coverage, current documentation links and whitespace checks:
  passed.
- Both workflow files passed actionlint 1.7.12, YAML and extracted shell checks.
  Seventeen isolated preflight cases checked tag formats, main ancestry, notes
  and public-key validation. Helm 4.3.0 lint, template and package passed; the
  packaged chart reports `version: 0.5.0` and `appVersion: v0.5.0`.
- Registry, full source, standalone app and standalone worker Compose files:
  rendered without reading the local `.env` or starting services. Version defaults
  matched, and each app configuration waits for successful migration. The
  standalone app's invalid cross-project Postgres dependency was corrected.
- Three server .NET examples: Release builds passed with zero errors/warnings.
  A local HTTP fixture verified bootstrap, heartbeat, event batch and shutdown
  bodies and checked that sample output does not expose broker credentials, API
  keys or worker session tokens.
- .NET: eight release-script regressions, fresh-cache independent package consumer
  and publishing-script dry-run passed. Seven `.nupkg` and seven `.snupkg` files
  were checked for IDs, versions, dependencies, symbols, README and release notes.
  This packaging follow-up did not rerun the full historical .NET functional suite.
- Java: 162 tests passed, 16 infrastructure-gated tests skipped, zero failures or
  errors on JDK 21. Fourteen release-script regressions and two independent package
  consumer tests passed. Seven library artifacts, eight POMs and seven Javadoc
  JARs were checked; all 29 original archived checksums still match. Live Lab and
  cross-language tests were not rerun in this packaging follow-up.

The earlier server functional/security tests are recorded separately in
[the security review](security-review-2026-09-18.md). Do not interpret this release
metadata pass as a new production deployment or live identity-provider test.

## Publication safeguards and remaining steps

The server workflow now validates the exact tag, release notes and licence key,
then runs the reusable full CI suite before preparing binaries, Helm package,
notes and checksums. Image publication follows preparation; only normal releases
update Docker `latest`, and GitHub release creation is last. The worker receives
the same licence key build argument as the app.

.NET publishing requires an explicit opt-in and a clean checkout at the matching
tag. Package checks reject mixed versions and stale files before any push.

Java publication tooling, POM metadata, local signing/bundle checks and the
protected staging workflow are now configured. Actual Central namespace
ownership, publisher credentials/key and GitHub environment protection remain
external prerequisites. See the Java repository's `docs/releasing.md` and the
[later preparation follow-up](release-preparation-2026-09-18.md) for the current
validation results; the checks above describe the earlier metadata pass.

No commits, tags, pushes, registry uploads or production changes were made by
this verification. The repository's existing uncommitted work was preserved.
The hosted release workflow and real repository secret are verified at publication
time; local checks do not establish that the remote release is already available.

See [release notes](releases/RELEASE_NOTES_v0.5.0.md) and
[the release procedure](../RELEASE.md) for the coordinated release.
