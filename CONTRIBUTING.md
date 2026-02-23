# Contributing to Pipelogiq

Thank you for considering a contribution. This guide covers the development workflow, coding standards, and PR process.

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md). By participating you agree to uphold it.

## Development Setup

### Prerequisites

- Go 1.22+
- Node.js 20+ and npm
- Docker and Docker Compose

### Running Locally

Start infrastructure (Postgres, RabbitMQ, Tempo, Grafana):

```bash
docker network create pipelogiq 2>/dev/null || true
cp .env.example .env
make dev
```

In separate terminals, run the services:

```bash
# Terminal 1: API server
export APP_ID=PipelogiqTest
export DATABASE_URL=postgres://pipelogiq:pipelogiq@localhost:5441/pipelogiq?sslmode=disable
export RABBITMQ_URL=amqp://guest:guest@localhost:5672/
make run-api

# Terminal 2: Worker
make run-worker

# Terminal 3: Frontend (dev server at :3300)
cd apps/web && npm install && npm run dev
```

Or start the full stack in Docker (build from source):

```bash
make compose-up
```

Or use pre-built images from GHCR (no local build):

```bash
make compose-latest-up
```

### Running Tests

```bash
make test        # Go tests
make lint        # go vet + ESLint
make fmt         # Check Go formatting
```

Run a single Go test:

```bash
cd apps/go && go test ./internal/observability/repo/ -run TestName
```

### Formatting and Linting

Go code must pass `gofmt`. The CI workflow enforces this.

```bash
# Check formatting (exits non-zero if changes needed)
make fmt

# Apply formatting
cd apps/go && gofmt -w .
```

Frontend:

```bash
cd apps/web && npm run lint
```

## Pull Request Process

1. Fork the repo and create a branch from `main`.
2. Make your changes. Add or update tests as appropriate.
3. Run `make test` and `make lint` locally.
4. If you changed the Docker setup, verify `make compose-up` and `make compose-latest-up` work.
5. Open a PR against `main`.

### PR Checklist

- [ ] Tests pass (`make test`)
- [ ] Linting passes (`make lint`)
- [ ] Formatting is correct (`make fmt`)
- [ ] Breaking changes are documented
- [ ] Database schema changes (Liquibase) are noted
- [ ] New environment variables are added to `.env.example`
- [ ] UI changes include a screenshot

### Commit Messages

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add stage timeout enforcement
fix: prevent second stage from starting when first stage failed
docs: update architecture diagram
refactor: extract policy repository interface
chore: bump Go to 1.22.12
```

Keep the subject line under 72 characters. Use the body for context on *why*, not *what*.

## What to Contribute

- Bug fixes and test coverage improvements
- Documentation corrections and examples
- Performance improvements with benchmarks
- New features (open an issue first to discuss scope)

## Reporting Issues

Use [GitHub Issues](https://github.com/pipelogiq/pipelogiq/issues). For security vulnerabilities, see [SECURITY.md](SECURITY.md).
