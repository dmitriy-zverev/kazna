SHELL := /bin/sh

GO      ?= go
SQLC    ?= sqlc
COMPOSE ?= docker compose
COMPOSE_FILE := deploy/compose.yaml

MODULE      := github.com/dmitriy-zverev/kazna
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_TIME  ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT) \
	-X $(MODULE)/internal/version.BuildTime=$(BUILD_TIME)

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Compile all binaries into ./bin
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/api ./cmd/api
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/worker ./cmd/worker
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/migrate ./cmd/migrate

.PHONY: test
test: ## Run unit tests
	$(GO) test ./...

.PHONY: test-integration
test-integration: ## Run integration tests against a real PostgreSQL (requires Docker)
	$(GO) test -tags integration -count=1 ./tests/integration/...

.PHONY: keygen
keygen: ## Print a fresh MASTER_ENCRYPTION_KEY
	@openssl rand -hex 32

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: fmt
fmt: ## Format all Go code
	gofmt -w .

.PHONY: fmt-check
fmt-check: ## Fail if any Go file is not formatted
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "unformatted files:"; echo "$$out"; exit 1; fi

.PHONY: lint
lint: vet fmt-check ## Run static checks

.PHONY: tidy
tidy: ## Tidy go.mod / go.sum
	$(GO) mod tidy

.PHONY: generate
generate: ## Regenerate sqlc code from db/queries
	$(SQLC) generate

.PHONY: run-api
run-api: ## Run the API locally (needs DATABASE_URL)
	$(GO) run ./cmd/api

.PHONY: run-worker
run-worker: ## Run the worker locally (needs DATABASE_URL)
	$(GO) run ./cmd/worker

.PHONY: migrate-up
migrate-up: ## Apply all pending migrations
	$(GO) run ./cmd/migrate up

.PHONY: migrate-down
migrate-down: ## Revert the most recent migration
	$(GO) run ./cmd/migrate down

.PHONY: migrate-version
migrate-version: ## Print the current migration version
	$(GO) run ./cmd/migrate version

.PHONY: compose-up
compose-up: ## Build and start the stack
	$(COMPOSE) -f $(COMPOSE_FILE) up -d --build

.PHONY: compose-down
compose-down: ## Stop the stack
	$(COMPOSE) -f $(COMPOSE_FILE) down

.PHONY: compose-logs
compose-logs: ## Follow stack logs
	$(COMPOSE) -f $(COMPOSE_FILE) logs -f

.PHONY: compose-ps
compose-ps: ## Show stack status
	$(COMPOSE) -f $(COMPOSE_FILE) ps

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf bin coverage.out
