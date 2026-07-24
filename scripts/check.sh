#!/usr/bin/env bash
#
# Local, offline CI for Mizan ERP. This is the canonical way to validate the project
# — no GitHub, no cloud, no remote runner required. It runs entirely on your machine.
#
# After dependencies have been fetched once (Go module cache + frontend node_modules),
# every step here works with no internet connection.
#
# Usage:
#   scripts/check.sh            # Go core + architecture + frontend
#   scripts/check.sh --go       # Go core only
#   scripts/check.sh --frontend # frontend only
#
set -euo pipefail
cd "$(dirname "$0")/.."

# Keep Go from trying to download a different toolchain (fully offline-friendly).
export GOTOOLCHAIN=local

CORE="./internal/... ./tools/..."
APP="./internal/..."
run_go=1
run_fe=1
case "${1:-}" in
  --go)       run_fe=0 ;;
  --frontend) run_go=0 ;;
  "")         ;;
  *) echo "unknown flag: $1" >&2; exit 2 ;;
esac

step() { printf '\n\033[1;36m▶ %s\033[0m\n' "$1"; }

if [ "$run_go" = 1 ]; then
  step "gofmt (formatting)"
  unformatted="$(gofmt -l internal tools ./*.go 2>/dev/null || true)"
  if [ -n "$unformatted" ]; then
    echo "unformatted files:"; echo "$unformatted"; exit 1
  fi
  echo "  ok"

  step "go build — core (pure Go, no cgo)"
  CGO_ENABLED=0 go build $CORE

  step "architecture rules (archlint)"
  go run ./tools/archlint $APP

  step "go vet"
  go vet $CORE

  step "tests (race + coverage)"
  go test -race -covermode=atomic -coverprofile=coverage.out $CORE

  if command -v golangci-lint >/dev/null 2>&1; then
    step "golangci-lint"
    golangci-lint run $CORE
  else
    echo "  (golangci-lint not installed; skipping — optional)"
  fi
fi

if [ "$run_fe" = 1 ]; then
  if [ -d frontend/node_modules ]; then
    step "frontend typecheck / lint / test"
    (cd frontend && npm run typecheck && npm run lint && npm run test -- --run)
  else
    echo "  (frontend/node_modules missing; run 'cd frontend && npm install' once; skipping)"
  fi
fi

printf '\n\033[1;32m✔ all local checks passed\033[0m\n'
