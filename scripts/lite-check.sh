#!/usr/bin/env bash
#
# Local, offline CI for Mizan Lite. The canonical way to validate the Lite edition: no GitHub, no
# cloud, no remote runner. After dependencies are fetched once (Go module cache + npm cache), every
# step works with no network.
#
# Usage:
#   scripts/lite-check.sh              # everything
#   scripts/lite-check.sh --go         # Go only
#   scripts/lite-check.sh --frontend   # frontend only (still regenerates the bindings first)
#
set -euo pipefail
cd "$(dirname "$0")/.."
export GOTOOLCHAIN=local

LITE_GO="./internal/lite/... ./apps/lite/ ./cmd/lite-demoseed/"
# platform/database is Mizan's, and Lite depends on the DSN fix made to it in L0.
SHARED_GO="./internal/platform/database/..."

run_go=1
run_fe=1
case "${1:-}" in
  --go)       run_fe=0 ;;
  --frontend) run_go=0 ;;
  "")         ;;
  *) echo "unknown flag: $1" >&2; exit 2 ;;
esac

step() { printf '\n\033[1;36m▶ %s\033[0m\n' "$1"; }
notrun=()

# The generated bindings are the oracle both sides' gates read. Regenerated every run, so a gate never
# compares the source against a stale copy.
step "wails generate module (the bindings every gate reads)"
(cd apps/lite && wails generate module >/dev/null)
test -f apps/lite/frontend/wailsjs/go/api/App.d.ts || { echo "generation produced no bindings" >&2; exit 1; }
echo "  ok"

if [ "$run_go" = 1 ]; then
  step "gofmt"
  unformatted="$(gofmt -l internal/lite apps/lite cmd/lite-demoseed internal/platform/database)"
  if [ -n "$unformatted" ]; then echo "unformatted:"; echo "$unformatted"; exit 1; fi
  echo "  ok"

  step "go vet (this machine)"
  go vet $LITE_GO $SHARED_GO

  # Lite ships on Windows and macOS. Nothing here can RUN a Windows binary, but everything can COMPILE
  # for one: a Windows-only compile error is caught here, not by a shop.
  step "cross-compile: go vet + test binaries for windows/amd64"
  GOOS=windows GOARCH=amd64 go vet $LITE_GO
  tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
  # One binary per package, named by its import path: several packages share a base name (three `domain`,
  # three `sqlite`), and `go test -c -o DIR` refuses to write two binaries called domain.test into one place.
  for pkg in $(GOOS=windows go list $LITE_GO); do
    GOOS=windows GOARCH=amd64 go test -c -o "$tmp/$(echo "$pkg" | tr '/.' '__').test.exe" "$pkg" >/dev/null
  done
  # The demo seeder is a shipped developer tool: it must BUILD for Windows, not only vet.
  GOOS=windows GOARCH=amd64 go build -o "$tmp/lite-demoseed.exe" ./cmd/lite-demoseed
  echo "  ok"

  step "cross-compile: go vet for darwin/amd64 (Intel Macs)"
  GOARCH=amd64 go vet $LITE_GO

  step "architecture rules (archlint)"
  go run ./tools/archlint ./...

  step "architecture rules can fail (planted drills)"
  scripts/lite-arch-drill.sh

  step "tests (race)"
  go test -race -count=1 $LITE_GO $SHARED_GO

  step "golangci-lint (v2)"
  if ! command -v golangci-lint >/dev/null 2>&1; then
    notrun+=("golangci-lint: not installed — make tools")
  elif ! golangci-lint --version 2>/dev/null | grep -qE "version v?2\."; then
    # The config is v2's format, and a v1 binary cannot read Go 1.27 export data anyway — every finding it
    # produced would be false. Reported as NOT RUN, never as passed.
    notrun+=("golangci-lint: $(golangci-lint --version 2>/dev/null | grep -oE 'version v?[0-9.]+') is not v2 — make tools")
  else
    golangci-lint run ./internal/lite/... ./apps/lite/ ./cmd/lite-demoseed/ ./internal/platform/database/...
    echo "  ok"
  fi
fi

if [ "$run_fe" = 1 ]; then
  cd apps/lite/frontend
  if [ ! -d node_modules ]; then
    step "npm ci (offline cache preferred)"
    npm ci --prefer-offline --no-audit --no-fund
  fi
  step "frontend: eslint";            npx eslint .
  step "frontend: typecheck";         npx tsc --noEmit
  step "frontend: tests and gates";   npx vitest run
  step "frontend: production build";  npx vite build >/dev/null && echo "  ok"
  step "frontend: G5 on the built bundle"; npx vitest run --config vitest.bundle.config.ts

  # L8 §3: the built frontend against a real graph in a real browser. The browser is the one the machine already has — Chrome on
  # a Mac, Edge (WebView2's engine) on Windows — and nothing is downloaded. No browser, no run: reported, never passed.
  step "end-to-end journeys and the visual pack (browser)"
  channel="${LITE_E2E_CHANNEL:-chrome}"
  if [ -d "/Applications/Google Chrome.app" ] || [ -n "${LITE_E2E_CHANNEL:-}" ]; then
    npx playwright test -c e2e/playwright.config.ts
  else
    notrun+=("end-to-end journeys: no ${channel} on this machine — install it, or set LITE_E2E_CHANNEL=msedge")
  fi
  cd - >/dev/null
fi

if [ ${#notrun[@]} -gt 0 ]; then
  printf '\n\033[1;33m⚠ checks NOT RUN (not passed):\033[0m\n'
  for n in "${notrun[@]}"; do printf '  - %s\n' "$n"; done
fi
printf '\n\033[1;32m✔ every check that ran passed\033[0m\n'
