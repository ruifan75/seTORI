.DEFAULT_GOAL := help

GO ?= go
GOFLAGS ?=
LDFLAGS ?=

BINARY ?= bin/server
CMD ?= ./cmd/server
HOST ?= 0.0.0.0
PORT ?= 8080
API_URL ?= http://localhost:$(PORT)

COMPOSE ?= docker compose
COMPOSE_FILE ?= ../docker/docker-compose.yml
DB_SERVICE ?= postgres

.PHONY: help run build serve test test-race fmt fmt-check vet check tidy download \
	db-up db-down db-logs health clean

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*## "; printf "Usage: make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_-]+:.*## / {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

run: ## Run the API server (PORT=8080 HOST=0.0.0.0)
	HOST=$(HOST) PORT=$(PORT) $(GO) run $(GOFLAGS) $(CMD)

build: ## Build the server binary into bin/server
	@mkdir -p $(dir $(BINARY))
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)

serve: build ## Build and run the server binary
	HOST=$(HOST) PORT=$(PORT) $(BINARY)

test: ## Run all Go tests
	$(GO) test $(GOFLAGS) ./...

test-race: ## Run all Go tests with the race detector
	$(GO) test $(GOFLAGS) -race ./...

fmt: ## Format all Go packages
	$(GO) fmt ./...

fmt-check: ## Fail when Go files are not gofmt-formatted
	@files="$$(find . -type f -name '*.go' -not -path './vendor/*' -exec gofmt -l {} +)"; \
	if [ -n "$$files" ]; then printf 'Unformatted files:\n%s\n' "$$files"; exit 1; fi

vet: ## Run go vet
	$(GO) vet $(GOFLAGS) ./...

check: fmt-check vet test ## Run formatting, vet, and tests

tidy: ## Update go.mod and go.sum
	$(GO) mod tidy

download: ## Download Go module dependencies
	$(GO) mod download

db-up: ## Start PostgreSQL with Docker Compose
	$(COMPOSE) -f $(COMPOSE_FILE) up -d $(DB_SERVICE)

db-down: ## Stop Docker Compose services
	$(COMPOSE) -f $(COMPOSE_FILE) down

db-logs: ## Follow PostgreSQL logs
	$(COMPOSE) -f $(COMPOSE_FILE) logs -f $(DB_SERVICE)

health: ## Call the backend health endpoint
	curl --fail --silent --show-error $(API_URL)/health
	@printf '\n'

clean: ## Remove Go build artifacts and the server binary
	$(GO) clean
	rm -f "$(BINARY)"
	@rmdir "$(dir $(BINARY))" 2>/dev/null || true
