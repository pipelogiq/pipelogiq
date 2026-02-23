# Repository Guidelines

## Project Structure & Module Organization
- Go entrypoints: `cmd/pipeline-api` and `cmd/pipeline-worker`; shared logic under `internal/` (config, db, mq, store, api, worker, etc.).
- Frontend (Vue 3 + Vite) lives in `frontend/`; generated API client targets `frontend/src/shared/api/generated`.
- Local infra via `docker-compose.yml` (Postgres, RabbitMQ, API, worker). Persistent volumes in `data/` and `redisdata/`—leave them uncommitted.
- Utility: `scripts/init-admin.sh` seeds the default admin user.

## Build, Test, and Development Commands
- `make build` compiles Go binaries to `bin/`; `make run-api` / `make run-worker` run from source; `make run` starts both built binaries until interrupted.
- `make check` runs `fmt`, `vet`, `golangci-lint`, and `go test`; `make cover` adds coverage; `make tidy` / `make vendor` keep modules in sync.
- `docker-compose up --build` (after copying `.env.example` to `.env`) brings up Postgres (5440), RabbitMQ (5672 / UI 15672), API (8080), worker metrics (9090). Health: `/healthz`, `/readyz`; metrics: `/metrics`.
- Frontend: `cd frontend && npm install`; `npm run dev` for hot reload, `npm run build` for production, `npm run lint` / `npm run format` for quality checks, `npm run preview` to serve the built bundle.

## Coding Style & Naming Conventions
- Go: `gofmt` enforced; keep packages domain-focused and lowercase; exported identifiers PascalCase, locals camelCase. Use structured logging (`logg.Info("msg", "pipelineId", id)`).
- Linting via `golangci-lint` (installed to `.tools/`) should be clean before PRs.
- Frontend: ESLint + Prettier defaults; Vue components PascalCase, composables `useX`; folders/files in `src/` use kebab-case.

## Testing Guidelines
- Add `_test.go` beside Go code you touch; prefer table-driven tests and short timeouts when hitting DB/RabbitMQ.
- Run `make test` (or `make cover`) before a PR. If you add frontend tests (Vitest/Cypress), document the command and results; attach UI screenshots for layout changes.

## Commit & Pull Request Guidelines
- Commit messages follow the existing short sentence-case style (e.g., `changed subscribe channel names`); keep subject <72 chars and group related changes.
- PRs should explain what/why, list local checks run (`make check`, docker-compose smoke, frontend lint/build), link issues, and call out DB/RabbitMQ schema or config changes. Include screenshots/GIFs for UI updates.

## Configuration & Security Tips
- Create `.env` from `.env.example`; never commit secrets. Rotate default `guest/guest` and admin creds outside local dev.
- Avoid logging sensitive payloads (API keys, DSNs); scrub logs before sharing. Prefer the repo-managed toolchain in `.tools/` to keep environments reproducible.
