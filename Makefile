# Mizan ERP — developer tasks.
# Pure-Go core (no cgo) for data/logic; the Wails desktop shell uses cgo on the GUI layer.

MODULE      := github.com/mizan-erp/mizan
# The pure-Go core (no cgo). The desktop shell (root package) uses cgo/webkit and is
# built only via the wails CLI or the desktop CI job, so core tooling excludes it.
CORE        := ./internal/... ./tools/...
# Architecture rules govern the application packages (internal/), not the lint tool
# itself or the cgo entrypoint.
APP         := ./internal/...
LDFLAGS     := -X $(MODULE)/internal/buildinfo.Version=$(shell ./scripts/version.sh) \
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
build: ## Build the desktop binary for THIS machine (requires the wails CLI)
	wails build -ldflags "$(LDFLAGS)"

# ── release ─────────────────────────────────────────────────────────────────────
#
# Two targets, both supported, no others. Linux was dropped deliberately (Step 1.13, D1): it is
# the only one of the three that cannot be cross-compiled — it needs cgo and webkit2gtk headers,
# so shipping it would mean a second build machine or a container for a platform nobody has
# asked for.
#
# Windows cross-builds from macOS because nothing in the stack needs cgo there; macOS builds
# natively as a universal binary. One machine produces both releases.

.PHONY: icons
icons: ## Regenerate every platform icon from scripts/icon.py
	./scripts/icons.sh

.PHONY: build-windows
build-windows: ## Cross-build the Windows .exe and NSIS installer into dist/
	./scripts/package-windows.sh

.PHONY: build-macos
build-macos: ## Build the macOS universal .app into build/bin/
	# -o names the BUNDLE. Without it Wails falls back to wails.json's `name` and produces
	# "mizan.app", which is what a user would then see in Finder and in Applications — the one
	# place the product's name is least negotiable.
	wails build -platform darwin/universal -ldflags "$(LDFLAGS)" -o "Mizan"

.PHONY: package-macos
package-macos: build-macos ## Build the macOS .app and wrap it in a .dmg in dist/
	./scripts/package-macos.sh

.PHONY: release
release: ci build-windows package-macos ## Everything shippable, both platforms
	@echo
	@ls -lh dist/

.PHONY: tools
tools: ## Install developer tools that must be global
	go install github.com/wailsapp/wails/v2/cmd/wails@latest
	@# v2, built by THIS Go toolchain: a golangci-lint built by an older Go cannot read newer export data
	@# (v1.64.8 failed on every package under Go 1.27 — Mizan Lite L0, finding F2).
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
	@command -v makensis >/dev/null 2>&1 || { \
		echo "makensis is needed for the Windows installer:"; \
		echo "  brew install makensis"; }

# ── Mizan Lite ───────────────────────────────────────────────────────────────────
# A second Wails application in apps/lite, built against this module (docs/mizan_lite/DESIGN.md §2).
LITE_VERSION := $(shell sed -n 's/.*"productVersion": *"\([^"]*\)".*/\1/p' apps/lite/wails.json)
LITE_LDFLAGS := -X main.version=$(LITE_VERSION)

.PHONY: lite-ci
lite-ci: ## Mizan Lite: full local CI — Go, cross-compile, archlint + drills, frontend, bundle gate
	scripts/lite-check.sh

.PHONY: lite-e2e
lite-e2e: ## Mizan Lite: the end-to-end journeys and the visual pack (needs Chrome, or LITE_E2E_CHANNEL=msedge)
	cd apps/lite/frontend && npm run build >/dev/null && npx playwright test -c e2e/playwright.config.ts

.PHONY: lite-guides
lite-guides: ## Mizan Lite: render docs/mizan_lite/guide/*.md to the PDFs the application ships
	go run ./cmd/lite-guides

.PHONY: lite-package-macos
lite-package-macos: ## Mizan Lite: build and package the macOS .dmg into dist/lite/
	scripts/lite-package-macos.sh

.PHONY: lite-package-windows
lite-package-windows: ## Mizan Lite: cross-build the Windows installer (WebView2 inside) into dist/lite/
	scripts/lite-package-windows.sh

.PHONY: lite-release
lite-release: ## Mizan Lite: a release from a clean tree — CI, packages, smoke, checksums
	scripts/lite-release.sh

.PHONY: lite-dev
lite-dev: ## Mizan Lite: run with hot reload (requires the wails CLI)
	cd apps/lite && wails dev

.PHONY: lite-build-macos
lite-build-macos: ## Mizan Lite: build the macOS universal .app into apps/lite/build/bin/
	cd apps/lite && wails build -clean -platform darwin/universal -ldflags "$(LITE_LDFLAGS)"

.PHONY: lite-build-windows
lite-build-windows: ## Mizan Lite: cross-build the Windows .exe into apps/lite/build/bin/
	cd apps/lite && wails build -platform windows/amd64 -ldflags "$(LITE_LDFLAGS)"
