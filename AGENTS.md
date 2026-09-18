# Repository Guidelines

This is the canonical repository guide for contributors and coding assistants.
Use [CONTRIBUTING.md](CONTRIBUTING.md) for local setup and
[docs/architecture.md](docs/architecture.md) for runtime boundaries.

## Layout and toolchains

- `apps/go/`: Go module `pipelogiq`, currently Go **1.26.8**. Entrypoints are
  `cmd/api` and `cmd/worker`; backend packages and adjacent tests are in `internal/`.
- `apps/web/`: **React 19**, TypeScript and Vite dashboard. Use **Node.js 24 LTS**
  and npm; commit `package-lock.json` with dependency changes. Components live in
  `src/components`, route pages in `src/pages`, and API calls in `src/api`.
- `database/changelog.xml`: Liquibase schema and data migrations. The dedicated
  `infra/docker/Dockerfile.migrate` image runs them before the app starts.
- `infra/docker/`, `infra/compose/`, `infra/helm/pipelogiq/`: images, local/deployed
  Compose configurations and Helm chart.
- `examples/dotnet/`: examples targeting **.NET 8**, with project references to
  the sibling `pipelogiq-sdk-net` checkout; see its README before running them.
- `docs/openapi.external.yaml`: contract for SDK-facing HTTP routes.
- `VERSION`: release source of truth; `scripts/check-versions.sh` checks current
  references. Historical changelogs and release notes retain their versions.

## Commands

Run Make targets from the repository root. `make help` lists the actual targets.

| Command | Behavior |
| --- | --- |
| `make build` | Build API and worker into root `bin/api` and `bin/worker`, with version metadata |
| `make run-api` / `make run-worker` | Run the corresponding Go service from source |
| `make run` | Build and run both Go services until interrupted |
| `make test` | Run `go test ./...` in `apps/go` |
| `make fmt` | Check Go formatting; does not rewrite files |
| `make lint` | Go vet, golangci-lint when installed, route/version checks, and web ESLint |
| `make tidy` | Update Go module dependencies |
| `make run-web` | Run the Vite development server |
| `make compose-infra-up` | Start infrastructure and wait for readiness |
| `make compose-up` / `make compose-down` | Build/start or stop the full source Compose stack |
| `make compose-app-up` / `make compose-worker-up` | Start separate app or worker stacks after infrastructure |
| `make compose-latest-up` | Start the registry stack using its pinned release images |
| `make migrate-up` | Run the locally installed Liquibase CLI; requires explicit connection and changelog configuration |

Frontend commands, from `apps/web`: `npm ci`, `npm test`, `npm run lint`,
`npm run build`, `npm run dev`, and `npm run preview`. There is no `npm run format`
or root `make check`, `make cover`, or `make vendor` target.

For manual Liquibase CLI use, provide `LIQUIBASE_COMMAND_URL`,
`LIQUIBASE_COMMAND_USERNAME`, `LIQUIBASE_COMMAND_PASSWORD`, and
`LIQUIBASE_COMMAND_CHANGELOG_FILE=changelog.xml`; `make migrate-up` runs from
`database/`. The Compose migration service is the normal Docker path.

## Runtime and configuration

The combined app container runs nginx on **8080** (published as host **3300**),
the internal Go API on **8088**, and the SDK-facing external API on **8081**.
nginx forwards `/api/*` to the internal API with the prefix removed and forwards
`/ws` for WebSockets. API metrics use a separate listener, normally **9091**;
worker metrics use **9090** when enabled. Registry Compose keeps metrics private.

When running Go directly, the internal API defaults to **8080**. Vite serves
**3300** and proxies `/api` and `/ws` there. Override
`PIPELOGIQ_DEV_API_TARGET` for another backend; see
[apps/web/README.md](apps/web/README.md). HTTP browser requests use same-origin
`/api`; `VITE_API_BASE_URL` only overrides the pipeline WebSocket destination.

Go services require `APP_ID`, PostgreSQL `DATABASE_URL`, and access to RabbitMQ.
The API also requires a unique `JWT_SECRET` of at least 32 characters. Compose
reads root `.env`; direct Go runs use the shell environment. PostgreSQL is the
supported deployment database. `ALLOW_SQLITE_FALLBACK=true` explicitly opts into
SQLite for local experiments; it is not a substitute for PostgreSQL validation.

## Implementation and verification

- Preserve unrelated local edits. Keep generated binaries, dependency directories,
  real `.env` files, credentials and signing keys out of Git.
- Use `gofmt`, explicit errors, context propagation and structured logging in Go.
  Never log passwords, API keys, worker session tokens or secret payload values.
- Use React function components, existing TanStack Query/API patterns and the
  shared UI components. Component names use PascalCase; hooks use `useX`.
- Enforce roles and application scope in the API and store, including WebSockets,
  logs and exports. UI visibility is not authorization. Preserve secret redaction.
- Add focused regression tests for changed behavior. Run the relevant tests, then
  the required checks; report what ran and any skipped external dependencies.
- For backend/security changes, run `go test -race ./...` and
  `go test -tags pro ./...` from `apps/go`. Keep both build modes working.
- Database migrations are append-only once released. Run
  `sh database/test-user-management-upgrade.sh` and
  `sh database/test-reliability-upgrade.sh` for affected upgrade paths. They use
  disposable Docker databases; never point tests at a deployed database.
- Keep external routes and OpenAPI synchronized with
  `./scripts/check-openapi-routes.sh`; run `./scripts/check-versions.sh` for release
  metadata changes. CI also runs vulnerability scans, web tests and image checks.

Use concise Conventional Commit subjects. PRs should describe the resulting
behavior, relevant tests, migrations/configuration changes and screenshots for
UI changes. Follow [RELEASE.md](RELEASE.md) for publishing; preparation does not
require creating or pushing a release tag.
