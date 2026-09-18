# Dependabot review — 2026-09-18

The public GitHub API reported 15 open Dependabot pull requests against server
`main`, and none in `pipelogiq-sdk-net`, at review time. This document records
local release preparation and the disposition of those proposals. It does not
assert that any GitHub pull request has been merged or closed.

Close an obsolete proposal only after its replacement reaches `main` and the
current CI checks pass. The old PR checks were recorded against earlier source:
seven PRs exposed passing check runs, #12/#9/#3 had failed Web checks, and
#20/#16/#7/#5/#4 exposed no check runs. They cannot validate this release tree.

## Disposition of all 15 proposals

| PR | Proposed dependency update | Release preparation | Disposition after merge and current CI |
| --- | --- | --- | --- |
| [#21](https://github.com/pipelogiq/pipelogiq/pull/21) | react-resizable-panels 4.6.3 → 4.7.2 | Dependency removed from manifest and lockfile | Close as superseded by removal |
| [#20](https://github.com/pipelogiq/pipelogiq/pull/20) | @eslint/js 9.39.2 → 10.0.1 | Coordinated ESLint 10 migration described below | Close as superseded by the tested compatible set |
| [#19](https://github.com/pipelogiq/pipelogiq/pull/19) | typescript-eslint 8.55.0 → 8.56.1 | Updated to 8.70.0 | Close as superseded |
| [#18](https://github.com/pipelogiq/pipelogiq/pull/18) | react-router-dom 6.30.3 → 7.13.1 | Already updated to 7.18.4 in release preparation | Close as superseded |
| [#17](https://github.com/pipelogiq/pipelogiq/pull/17) | modernc.org/sqlite 1.30.1 → 1.46.1 | Updated to 1.46.2 with matching libc 1.70.0; review and checks below | Close as superseded by the tested patch release |
| [#16](https://github.com/pipelogiq/pipelogiq/pull/16) | OTel SDK 1.40.0 → 1.42.0 | Already updated to 1.46.0 | Close as superseded |
| [#15](https://github.com/pipelogiq/pipelogiq/pull/15) | docker/login-action 3 → 4 | Pinned to the verified v4.6.0 release commit | Close after workflow replacement passes CI |
| [#14](https://github.com/pipelogiq/pipelogiq/pull/14) | docker/build-push-action 6 → 7 | Pinned to the verified v7.4.0 release commit | Close after workflow replacement passes CI |
| [#12](https://github.com/pipelogiq/pipelogiq/pull/12) | react-hook-form 7.71.1 → 7.71.2 | Updated to 7.88.0 | Close as superseded |
| [#9](https://github.com/pipelogiq/pipelogiq/pull/9) | actions/setup-go 5 → 6 | Pinned to the verified v7.0.0 release commit | Close after workflow replacement passes CI |
| [#7](https://github.com/pipelogiq/pipelogiq/pull/7) | pgx 5.5.4 → 5.8.0 | Already updated to 5.9.2 | Close as superseded |
| [#6](https://github.com/pipelogiq/pipelogiq/pull/6) | actions/checkout 4 → 6 | Pinned to the verified v7.0.1 release commit | Close after workflow replacement passes CI |
| [#5](https://github.com/pipelogiq/pipelogiq/pull/5) | AMQP client 1.9.0 → 1.10.0 | Already updated to 1.13.0 | Close as superseded |
| [#4](https://github.com/pipelogiq/pipelogiq/pull/4) | x/crypto 0.33.0 → 0.48.0 | Already updated to 0.57.0 | Close as superseded |
| [#3](https://github.com/pipelogiq/pipelogiq/pull/3) | actions/setup-node 4 → 6 | Pinned to the verified v7.0.0 release commit | Close after workflow replacement passes CI |

The six proposals already superseded before this cleanup were #21, #18, #16,
#7, #5 and #4. The three web proposals are resolved together in the local tree.
The five Actions proposals are replaced by verified release-commit pins in the
local CI and release workflows. The SQLite proposal is superseded by a tested
patch release. All 15 proposals now have a local replacement or removal; closing
them still requires merging those changes and passing current CI.

## Compatible web dependency set

| Package | Previous locked version | New locked version |
| --- | --- | --- |
| react-hook-form | 7.71.1 | 7.88.0 |
| typescript-eslint and its coordinated packages | 8.55.0 | 8.70.0 |
| eslint | 9.39.2 | 10.10.0 |
| @eslint/js | 9.39.2 | 10.0.1 |
| eslint-plugin-react-hooks | 7.0.1 | 7.1.1 |
| eslint-plugin-react-refresh | 0.4.26 | 0.5.7 |

Updating only `@eslint/js` would be incorrect: version 10 requires ESLint 10,
while the old React Hooks plugin only declared support through ESLint 9. The
[current 7.1.1 Hooks package](https://registry.npmjs.org/eslint-plugin-react-hooks/7.1.1)
explicitly supports ESLint 10, as do
[typescript-eslint 8.70.0](https://registry.npmjs.org/typescript-eslint/8.70.0)
and [React Refresh 0.5.7](https://registry.npmjs.org/eslint-plugin-react-refresh/0.5.7).
The selected TypeScript ESLint version also supports the project's TypeScript
5.9.3. [React Hook Form 7.88.0](https://registry.npmjs.org/react-hook-form/7.88.0)
supports the existing React 19 version.

The repository already uses flat ESLint configuration and Node 24, matching the
[ESLint 10 migration requirements](https://eslint.org/blog/2026/02/eslint-v10.0.0-released/).
This update adds no forced peer dependencies, rule suppressions or unrelated
direct dependency upgrades. Project version remains `0.5.0`.

The new lint checks identified existing search and authentication initialization
issues. URL search changes now update the draft and parsed terms before commit,
including clearing an empty query, while keeping independent status/time
filters. Authentication state updates occur in request completion callbacks;
session and request generation checks still reject stale responses. Regression
tests cover URL resets and an initial auth response arriving after expiration.

## SQLite review and replacement for #17

SQLite is not solely a test dependency: `internal/db/db.go` imports the driver
for explicit SQLite connection strings and the optional local fallback. Missing
`DATABASE_URL` falls back only when `ALLOW_SQLITE_FALLBACK=true`; PostgreSQL
remains the supported deployment database. No connector code, connection-pool
settings, pragmas or production schema behavior were changed in this update.

The proposed driver 1.46.1 adds cleanup after failed transaction commits and an
opt-in timestamp metadata option. It embeds SQLite 3.51.2. The selected **1.46.2**
patch embeds SQLite **3.51.3**, which fixes the documented WAL-reset corruption
bug. See the [driver changelog](https://gitlab.com/cznic/sqlite/-/blob/v1.46.2/CHANGELOG.md)
and [SQLite patch notes](https://sqlite.org/releaselog/3_51_3.html).
The new `_texttotime` option is not enabled by this change.

The driver's Go 1.25 minimum is compatible with this repository's Go 1.26.8.
`modernc.org/libc` is pinned to **1.70.0**, matching the driver's
[own go.mod](https://gitlab.com/cznic/sqlite/-/blob/v1.46.2/go.mod), as required by
its [dependency guidance](https://pkg.go.dev/modernc.org/sqlite@v1.46.2).
Related indirect modules were updated or removed by `go mod tidy`; other direct
module versions and the Go toolchain version were retained.

Verification on Go 1.26.8, macOS arm64:

- `go test -race ./internal/store ./internal/api -count=1`: passed.
- `go test -race ./... -count=1`: all Community packages passed.
- `go test -race -tags pro ./... -count=1`: all Pro packages passed.
- `go vet ./...` and `go vet -tags pro ./...`: passed.
- A temporary test overlay exercised the real `db.Connect`: SQLite version,
  foreign keys, one-connection pool, nested database directory creation,
  transaction rollback, persistence after reopen and integrity check all passed.
  It did not modify production sources or a deployed database.
- Govulncheck in both build modes reported zero reachable or imported-package
  vulnerabilities. Its one module-only advisory,
  [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932), concerns unmaintained
  `golang.org/x/crypto/openpgp`, which this application does not import; the
  advisory has no fixed version and is unrelated to SQLite.

These checks do not replace the disposable PostgreSQL upgrade suites and Linux
build checks in CI.

## Verified GitHub Actions replacements

These official release tags were resolved to commit SHAs through GitHub's tag
API, including peeling annotated tags. The applicable occurrences are pinned in
[CI](../.github/workflows/ci.yml) and [release](../.github/workflows/release.yml).
Current CI must validate the resulting workflow changes before publication.

| Action | Release | Commit |
| --- | --- | --- |
| [actions/checkout](https://github.com/actions/checkout/releases/tag/v7.0.1) | v7.0.1 | `3d3c42e5aac5ba805825da76410c181273ba90b1` |
| [actions/setup-go](https://github.com/actions/setup-go/releases/tag/v7.0.0) | v7.0.0 | `b7ad1dad31e06c5925ef5d2fc7ad053ef454303e` |
| [actions/setup-node](https://github.com/actions/setup-node/releases/tag/v7.0.0) | v7.0.0 | `820762786026740c76f36085b0efc47a31fe5020` |
| [docker/login-action](https://github.com/docker/login-action/releases/tag/v4.6.0) | v4.6.0 | `dbcb813823bdd20940b903addbd779551569679f` |
| [docker/build-push-action](https://github.com/docker/build-push-action/releases/tag/v7.4.0) | v7.4.0 | `c3c9e263c25d99ce0380d002d59b67737d91b0dc` |

Keep `cache-dependency-path: apps/go/go.sum` when updating setup-go. These Node 24
Actions require sufficiently recent runners; the workflows use GitHub-hosted
`ubuntu-latest`. Review release notes before applying the same pins to a
self-hosted runner. Build-push 7.4.0 includes a fix for command injection in
metadata logs, so retaining an old proposed major tag alone is insufficient.

## Validation

Web checks use official Node 24.21.0 with npm 11.19.0 on macOS arm64, verified
against the release SHA-256 list. After the regression fixes:

- `npm ci --ignore-scripts --no-audit`: clean installation succeeded.
- Focused search/auth tests: 17 passed.
- `npm test`: 154 tests in 22 files passed.
- `npm run lint`: passed without suppressing the new rules.
- `npm run build`: TypeScript and Vite production build passed; Vite retains its
  advisory warning about a bundle larger than 500 kB.
- `npm audit --json`: zero vulnerabilities at all severity levels.
- `npm ls` confirmed the six selected versions with no peer dependency errors.
- Release version guard: references agree at `0.5.0`.

GitHub's Linux CI must still pass on the commit that is merged and released.

Future weekly minor/patch version updates are grouped by ecosystem in
[dependabot.yml](../.github/dependabot.yml). Major proposals remain separate;
security updates are not placed in those version-update groups. Grouping does
not grant automatic approval or change the requirement for current tests.
