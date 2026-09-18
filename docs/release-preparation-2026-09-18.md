# Coordinated 0.5.0 release preparation — 2026-09-18

Pipelogiq, .NET SDK and Java SDK source are aligned on **0.5.0**. This report
records the local preparation checkpoint before commit/push. Consult Git and
GitHub Actions for subsequent commit/push/CI status; preparation does not claim
that registries or deployed credentials have been changed.

## Resolution of the seven reported risks

| Risk | Completed preparation | Remaining release/operations step |
| --- | --- | --- |
| Uncommitted work | Encrypted, verified source/Git recovery backups; source guards; all intended source prepared for review | Review and create the three source commits; push only reviewed content |
| Environment backup in unpublished history | Removed from both unpublished server branches and working tree; obsolete objects/reflogs pruned; complete reachable-history secret scan passes | Rotate the two exposed credentials wherever used; live values still match the local installation |
| Extensionless executables in Git | API/worker removed from the index; Make outputs to ignored root `bin/`; magic-byte, size and generated-output guards run in hooks/CI | Public historical binaries remain; no disruptive public-history rewrite was performed |
| Inconsistent HEAD and version | Unpublished server HEAD message corrected; release branch named `release/v0.5.0`; current manifests/docs/notes use 0.5.0; unsupported draft compatibility mapping clarified | Historical release notes remain historical; do not publish old preview metadata |
| Publication prerequisites | Full CI release gates, checked package identities/versions/hashes, protected SDK publication, Central POM/signing/bundle configuration | Actual account ownership, namespace, publisher credentials and GitHub environment protection must be configured |
| Stale developer docs | Server AGENTS/CLAUDE/contributing/architecture updated; current .NET guides are English; historical audit documents have an English index | Maintain docs alongside future changes |
| Fifteen stale Dependabot PRs | Every proposal has a tested local replacement or removal; Actions pinned to verified commits; future routine updates grouped | Close obsolete PRs only after replacements reach remote main and current CI passes |

See [Dependabot disposition](dependabot-review-2026-09-18.md),
[source hygiene](source-hygiene.md), and [credential rotation](credential-rotation.md).
The guard tests prove that a real credential added to an allowed template fails;
allowlists do not suppress whole test, configuration or documentation files.

## Git state and commit preparation

Local server and .NET `main` were fast-forwarded to their existing prepared
history, preserving the complete source/index state. Remote `origin/main` was
not changed. The server remains a fast-forward descendant of the unchanged
public base; only unpublished descendants were rewritten to remove the secret.
The old credential-bearing object is no longer readable from the local Git
object database. The encrypted recovery archive is outside all three repos.

Java now has the exact requested origin
`git@github.com:pipelogiq/pipelogiq-sdk-java.git`. SSH repository access succeeds;
the remote is empty and the local `main` still awaits its initial commit.
All three clones have `core.hooksPath=.githooks` enabled.

Suggested source commit subjects after review:

- Server: `feat: prepare Pipelogiq 0.5.0 with access management and audit`
- .NET: `feat: prepare .NET SDK 0.5.0 and guarded NuGet release`
- Java: `feat: initial Java SDK 0.5.0 with Central release tooling`

Review `git diff --cached --stat` and `git diff --cached`, then execute the checked-in
pre-commit hook before committing. Generated artifacts, actual environment files,
private keys and test-service data must remain ignored. Do not push all local
branches/tags: publish the reviewed main branch and later only its intended tag.

## Current validation

- Source, complete reachable Git history, and repository hygiene checks pass in
  all three repositories. Ten shared security regressions pass per repository.
  Java has no committed history yet; its source scan covers the initial files.
- Server version guard and its nine regressions pass. Make builds API/worker
  under root `bin/`, embeds `v0.5.0`, and records dirty-tree provenance.
- Web: 154 tests in 22 files, lint, production build and peer checks pass on Node
  24.21.0. npm audit reports zero vulnerabilities. The existing bundle-size
  advisory remains.
- Go: complete Community and Pro race suites and vet pass after SQLite 1.46.2 /
  libc 1.70.0. A real SQLite connection/transaction/reopen/integrity probe passes.
  Module verification passes; govulncheck reports no reachable or imported-package
  vulnerabilities. The unimported, unmaintained OpenPGP module advisory remains
  documented without suppression.
- .NET: fresh disposable server/broker/store run passes 232 tests in Debug and
  232 in Release with zero skips/failures. All 53 migrations apply; a second
  update applies zero. Seven NuGet packages and seven symbol packages retain
  version 0.5.0; independent package/assembly checks pass. Thirty-one release
  script regressions pass. The Go contract/race suite also passes twice against
  this temporary PostgreSQL server; NuGet audit reports zero vulnerabilities.
- Java: JDK 17 and 21 each pass 162 reactor tests and two independent consumer
  tests. Sixteen Lab-dependent cases are explicitly skipped in each normal
  reactor run. Thirty-three Python tests pass. Both an unsigned Central-format
  preview and a locally signed preview validate 29 payload artifacts; all 29
  signatures were verified using a temporary test key. This does not validate a
  real publisher key or claim that Lab-dependent cases ran.
- Workflow syntax is checked with actionlint. Independent cross-review fixed
  the Java checkout-origin check, complete Maven-coordinate validation and the
  .NET publishing-environment approval check.

Earlier migration/image and application security evidence is retained in
[security verification](security-review-2026-09-18.md) and
[the initial release verification](release-verification-2026-09-18.md).
Those previously scanned images are not described as rebuilt after this later
dependency update. The release CI must test and scan the final committed build.

## Gates before publication

1. Complete [credential rotation](credential-rotation.md). First establish
   whether the exposed JWT/bootstrap key is used only locally or also by external
   servers/SDK clients. Changing the bootstrap environment variable alone does
   not revoke an existing database key.
2. Confirm the actual NuGet owner/Trusted Publisher and the Central
   **io.pipelogiq** namespace. Configure the protected `nuget` / `maven-central`
   environments and their documented credentials/signing identity. Source files
   do not establish account permissions. Confirm the server licence public key
   used by the release workflow.
3. Review and commit all three repositories, push the intended main branches,
   wait for current hosted CI, and create the corresponding annotated `v0.5.0`
   tags on those reviewed commits. Do not tag the pre-commit working tree.
4. Publish through each repository's release process. Server tag publication is
   automatic after its CI gates. NuGet requires an explicit manual publish flag;
   six new package IDs require the scoped bootstrap flag. Java stages a signed
   deployment with automatic public release disabled and waits for Portal validation.
5. Verify the published package/image bytes and installation, then close the 15
   superseded Dependabot PRs. Local success does not substitute for hosted CI or
   registry availability.

At this preparation checkpoint, working source changes had not yet been
committed, pushed, tagged or published.
Live credential rotation, registry account configuration and remote PR closure
are intentionally recorded as incomplete rather than silently treated as done.
