# Contributing to Pipelogiq

[AGENTS.md](AGENTS.md) is the canonical repository guide; this document covers
setup and the review workflow.

## Prerequisites

- Go **1.26.8**, matching `apps/go/go.mod` and the build images.
- Node.js **24 LTS** and npm for `apps/web`.
- Docker with Docker Compose v2 for PostgreSQL, RabbitMQ and migration tests.
- .NET **8 SDK** only when running `examples/dotnet`; those examples reference
  the sibling `pipelogiq-sdk-net` source checkout.

## Full stack from source

Run from the repository root:

```bash
# Create local configuration once; keep existing values on later runs.
test -f .env || cp .env.example .env
openssl rand -base64 48
```

Set `JWT_SECRET` in `.env` to your generated value before starting. The API rejects
the published placeholder. Review the local credentials and administrator
settings in `.env.example`; never commit your `.env`.

```bash
make compose-up
```

This target creates the shared Docker network, builds the app, worker and
migration images, and starts the stack. The dedicated `pipelogiq-migrate` service
must complete successfully before the app starts. nginx serves the dashboard at
`http://localhost:3300`; SDK clients use `http://localhost:8081`.
A new bootstrap administrator without a configured password hash receives a
one-time password in the API startup log. Treat that log entry as a credential.

Use `make compose-down` to stop the source stack. `make compose-latest-up` and
`make compose-latest-pull` use the pinned registry release, which must already be
published. See [Quick Start](docs/quickstart.md) for component deployments.

## Local Go and Vite development

Start infrastructure, then apply migrations without starting the app container:

```bash
make compose-infra-up
docker compose --env-file .env -f infra/compose/docker-compose.app.yml run --build --rm pipelogiq-migrate
```

In the terminal that runs Go, export the local connection settings. The values
below match the infrastructure defaults; adjust them if you changed `.env`.
Direct Go processes do not automatically load the root `.env` file.

```bash
export APP_ID=PipelogiqGo
export DATABASE_URL='postgres://pipelogiq:pipelogiq@localhost:5441/pipelogiq?sslmode=disable'
export RABBITMQ_URL='amqp://guest:guest@localhost:5672/'
export JWT_SECRET="$(openssl rand -base64 48)"
export ADMIN_EMAIL=admin@pipelogiq.local
export HTTP_ADDR=:8080
export EXTERNAL_HTTP_ADDR=:8081
make run
```

`make run` builds root `bin/api` and `bin/worker`, then runs both until interrupted.
You can instead use `make run-api` and `make run-worker` in separate terminals
with the relevant environment exported in each. Configure `METRICS_ADDR=:9090`
only for the worker if you need its metrics; the API defaults to a separate
metrics listener on `:9091`.

In another terminal:

```bash
cd apps/web
npm ci
npm run dev
```

Vite serves `http://localhost:3300` and proxies `/api` and `/ws` to the direct API
on port 8080. If that port is occupied, set Go's `HTTP_ADDR=:8088` and start Vite
with `PIPELOGIQ_DEV_API_TARGET=http://127.0.0.1:8088 npm run dev`. When developing
against a running app container, use its `/api` URL and a different Vite port;
see [the web README](apps/web/README.md).

Infrastructure exposes PostgreSQL on `5441`, RabbitMQ on `5672` (management UI
`15672`), Tempo on `3200`/`4317`/`4318`, and Grafana on host `3100`.
Stop it with `make compose-infra-down` when finished.

## Checks

Run from the repository root:

```bash
make fmt
make test
make lint
./scripts/check-openapi-routes.sh
./scripts/check-versions.sh
python3 -m unittest discover -s scripts -p 'test_check_versions.py' -v
```

`make fmt` checks formatting; it does not modify files. Apply `gofmt` to changed
Go files before committing. `make lint` runs Go vet, route/version checks, web
ESLint, and golangci-lint when installed. CI requires golangci-lint 2.13.2, so a
local message saying it was skipped is not a successful static-analysis run.

Backend checks and a focused test, from `apps/go`:

```bash
go test -race ./...
go vet -tags pro ./...
go test -tags pro ./...
go test ./internal/api -run TestAuditHistoryValidatesFiltersAndRemainsAdminOnly -count=1
```

Web checks, from `apps/web`:

```bash
npm test
npm run lint
npm run build
```

For database or access-control changes, use the disposable PostgreSQL upgrade
and regression suites from the repository root:

```bash
sh database/test-user-management-upgrade.sh
sh database/test-reliability-upgrade.sh
```

They require Docker and create and remove their own test infrastructure. CI runs
these checks plus dependency scans and image scanning. Production PostgreSQL
behavior must be tested against PostgreSQL, even when a unit test uses SQLite.

## Dependency updates

Dependabot groups weekly minor/patch version updates by ecosystem. Major changes
remain separate, and security updates are not held inside those version groups.
Review compatibility and current CI results before merging; a historical green
check on an old PR does not validate a changed base branch.

When a dependency has been removed or upgraded beyond an old PR's target, first
merge the replacement and verify the default branch, then close the obsolete PR
with a reference to that change. Do not mass-close remaining updates. Keep npm
peer dependencies compatible without `--force` or `--legacy-peer-deps`, and run
backend/database coverage for database-driver changes.

## Pull requests and commits

1. Create a branch from `main` and keep the change focused.
2. Add regression tests for changed behavior and run the relevant checks above.
3. Describe the resulting behavior, tests, migrations and new environment variables.
4. Include screenshots for UI changes and document breaking changes.
5. Open a PR against `main` and wait for its current CI checks.

Use concise [Conventional Commits](https://www.conventionalcommits.org/), for
example `fix: enforce application scope when listing logs`. Keep subjects under
72 characters. Never commit generated binaries, private keys, credentials or
local configuration. Report vulnerabilities using [SECURITY.md](SECURITY.md).

Release preparation and publishing follow [RELEASE.md](RELEASE.md).

## Source and credential checks

Enable the repository pre-commit hook and run the [source hygiene checks](docs/source-hygiene.md) before committing. CI repeats these checks.
