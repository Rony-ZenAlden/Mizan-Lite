# Mizan ERP — developer tasks.
# Pure-Go core (no cgo) for data/logic; the Wails desktop shell uses cgo on the GUI layer.

MODULE      := github.com/mizan-erp/mizan
# The pure-Go core (no cgo). The desktop shell (root package) uses cgo/webkit and is
# built only via the wails CLI or the desktop CI job, so core tooling excludes it.
CORE        := ./internal/... ./tools/...
# Architecture rules govern the application packages (internal/), not the lint tool
# itself or the cgo entrypoint.
APP         := ./internal/...
LDFLAGS     := -X $(MODULE)/internal/buildinfo.Version=$(shell git describe --tags --always 2>/dev/null || echo dev) \
               -X $(MODULE)/internal/buildinfo.Commit=$(shell git rev-parse --short HEAD 2>/dev/null || echo none) \
               -X $(MODULE)/internal/buildinfo.BuildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: tidy
tidy: ## Sync go.mod/go.sum
	go mod tidy

.PHONY: build-core
build-core: ## Build the pure-Go core (no GUI) — used by CI
	CGO_ENABLED=0 go build $(CORE)

.PHONY: arch
arch: ## Run the Mizan architecture-rule framework
	go run ./tools/archlint $(APP)

.PHONY: lint
lint: arch ## Run golangci-lint + architecture rules
	golangci-lint run $(CORE)

.PHONY: test
test: ## Run tests with race detector and coverage
	go test -race -covermode=atomic -coverprofile=coverage.out $(CORE)

.PHONY: vet
vet: ## go vet
	go vet $(CORE)

.PHONY: check
check: lint vet test ## Everything CI runs (Go side)

.PHONY: ci
ci: ## Full local, offline CI (Go core + architecture + frontend). No GitHub needed.
	./scripts/check.sh

.PHONY: vendor
vendor: ## Vendor Go dependencies for fully offline, self-contained builds
	go mod vendor
	@echo "Vendored. Builds now work with no module downloads (go build -mod=vendor)."

.PHONY: dev
dev: ## Run the desktop app with hot reload (requires the wails CLI)
	wails dev

.PHONY: build
build: ## Build the production desktop binary (requires the wails CLI)
	wails build -ldflags "$(LDFLAGS)"

.PHONY: tools
tools: ## Install developer tools that must be global
	go install github.com/wailsapp/wails/v2/cmd/wails@latest
