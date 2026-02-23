SHELL := /bin/bash
.DEFAULT_GOAL := help

GO_DIR := apps/go
WEB_DIR := apps/web
GO ?= go
GOFLAGS ?=
BIN_DIR := bin
BINS := pipeline-api pipeline-worker

COMPOSE_FILE    := infra/compose/docker-compose.yml
COMPOSE_INFRA   := infra/compose/docker-compose.infra.yml
COMPOSE_API     := infra/compose/docker-compose.api.yml
COMPOSE_WORKER  := infra/compose/docker-compose.worker.yml
COMPOSE_WEB     := infra/compose/docker-compose.web.yml
COMPOSE_LATEST  := infra/compose/docker-compose.latest.yml

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS ?= -s -w \
	-X pipelogiq/internal/version.Version=$(VERSION) \
	-X pipelogiq/internal/version.Commit=$(COMMIT) \
	-X pipelogiq/internal/version.Date=$(DATE)

##@ Helpers
.PHONY: help
help: ## Show available targets
	@grep -E '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "%-22s %s\n", $$1, $$2}'

##@ Go
.PHONY: tidy
tidy: ## Sync go.mod/go.sum
	cd $(GO_DIR) && $(GO) mod tidy

.PHONY: build
build: $(BINS:%=$(BIN_DIR)/%) ## Build all binaries

$(BIN_DIR)/%: $(GO_DIR)/cmd/%/main.go
	cd $(GO_DIR) && mkdir -p $(BIN_DIR) && $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$* ./cmd/$*

.PHONY: run-api
run-api: ## Run API from source
	cd $(GO_DIR) && $(GO) run ./cmd/pipeline-api

.PHONY: run-worker
run-worker: ## Run worker from source
	cd $(GO_DIR) && $(GO) run ./cmd/pipeline-worker

.PHONY: run-web
run-web: ## Run React app in dev mode
	cd $(WEB_DIR) && npm run dev

.PHONY: run
run: build ## Run API and worker binaries until interrupted
	@set -e; \
	cd $(GO_DIR); \
	./$(BIN_DIR)/pipeline-api & API_PID=$$!; \
	./$(BIN_DIR)/pipeline-worker & WORKER_PID=$$!; \
	trap 'kill $$API_PID $$WORKER_PID' INT TERM EXIT; \
	wait $$API_PID $$WORKER_PID

.PHONY: test
test: ## Run Go tests
	cd $(GO_DIR) && $(GO) test ./...

.PHONY: fmt
fmt: ## Check Go formatting (exits non-zero if changes needed)
	@test -z "$$(cd $(GO_DIR) && gofmt -l . 2>&1)" || \
		(echo "gofmt: the following files need formatting:" && cd $(GO_DIR) && gofmt -l . && exit 1)

.PHONY: lint
lint: ## Run go vet and web lint
	cd $(GO_DIR) && $(GO) vet ./...
	@if [ -f $(WEB_DIR)/package.json ]; then cd $(WEB_DIR) && npm run lint; fi

.PHONY: migrate-up
migrate-up: ## Apply Liquibase changelog from database/changelog.xml
	cd database && liquibase update

##@ Docker — full stack (build from source)
.PHONY: compose-up
compose-up: ## Start full stack (build all images from source)
	docker network create pipelogiq 2>/dev/null || true
	docker compose -f $(COMPOSE_FILE) up --build

.PHONY: compose-down
compose-down: ## Stop full stack
	docker compose -f $(COMPOSE_FILE) down

##@ Docker — component compose files (build from source)
.PHONY: compose-infra-up
compose-infra-up: ## Start infra services (Postgres, RabbitMQ, Tempo, Grafana)
	docker network create pipelogiq 2>/dev/null || true
	docker compose -f $(COMPOSE_INFRA) up -d

.PHONY: compose-infra-down
compose-infra-down: ## Stop infra services
	docker compose -f $(COMPOSE_INFRA) down

.PHONY: compose-api-up
compose-api-up: ## Build and start pipeline-api
	docker network create pipelogiq 2>/dev/null || true
	docker compose -f $(COMPOSE_API) up --build -d

.PHONY: compose-api-down
compose-api-down: ## Stop pipeline-api
	docker compose -f $(COMPOSE_API) down

.PHONY: compose-worker-up
compose-worker-up: ## Build and start pipeline-worker
	docker network create pipelogiq 2>/dev/null || true
	docker compose -f $(COMPOSE_WORKER) up --build -d

.PHONY: compose-worker-down
compose-worker-down: ## Stop pipeline-worker
	docker compose -f $(COMPOSE_WORKER) down

.PHONY: compose-web-up
compose-web-up: ## Build and start React web dashboard
	docker network create pipelogiq 2>/dev/null || true
	docker compose -f $(COMPOSE_WEB) up --build -d

.PHONY: compose-web-down
compose-web-down: ## Stop React web dashboard
	docker compose -f $(COMPOSE_WEB) down

##@ Docker — pre-built images from ghcr.io
.PHONY: compose-latest-up
compose-latest-up: ## Start full stack using latest images from ghcr.io/pipelogiq
	docker network create pipelogiq 2>/dev/null || true
	docker compose -f $(COMPOSE_LATEST) up -d

.PHONY: compose-latest-down
compose-latest-down: ## Stop pre-built image stack
	docker compose -f $(COMPOSE_LATEST) down

.PHONY: compose-latest-pull
compose-latest-pull: ## Pull latest images from ghcr.io/pipelogiq
	docker compose -f $(COMPOSE_LATEST) pull pipeline-api pipeline-worker pipeline-web

##@ Docker — local dev helpers
.PHONY: dev
dev: ## Start infra only (Postgres, RabbitMQ, Tempo, Grafana)
	docker network create pipelogiq 2>/dev/null || true
	docker compose -f $(COMPOSE_INFRA) up -d
