#!/usr/bin/env bash
#
# Generates the upgrade matrix's fixture databases (L8 D-L8.6): one shop per schema a released phase ever shipped, made by
# that phase's own code at its own commit, so a later migration is tested against the data an older build really wrote.
#
# Run once per new schema; the databases are committed under internal/lite/bootstrap/testdata/schemas/. Offline: the module
# cache already holds every version these commits need.
set -euo pipefail
cd "$(dirname "$0")/.."
export GOTOOLCHAIN=local GOFLAGS=-mod=mod

OUT="internal/lite/bootstrap/testdata/schemas"
WORK="$(mktemp -d)"
trap 'for w in "$WORK"/wt-*; do git worktree remove --force "$w" >/dev/null 2>&1 || true; done; rm -rf "$WORK"' EXIT
mkdir -p "$OUT"

# schema commit seeder-flags
PHASES=(
  "1 c2a1d0e -"
  "2 edb4570 -pin 481537"
  "3 407fdbb -pin 481537"
  "4 67d72c8 -pin 481537"
  "5 76463fd -pin 481537"
  "6 97b7875 -pin 481537"
  "7 2c86f17 -pin 481537 -days 7"
  "8 d356598 -pin 481537 -days 7"
  "9 f27a440 -pin 481537 -days 7"
  "10 3e4bb36 -pin 481537 -days 7"
  # 11 is this phase’s own schema: the fixture is made by the build that introduces it.
  "11 7838c6c -pin 481537 -days 7"
  # 12 is 0.10.0's: the build that introduces it, its demo shop keeping a supplier's book.
  "12 8e6b4a7 -pin 481537 -days 7"
)

for line in "${PHASES[@]}"; do
  read -r schema commit flags <<<"$line"
  wt="$WORK/wt-$schema"
  data="$WORK/data-$schema"
  git worktree add --detach "$wt" "$commit" >/dev/null
  mkdir -p "$data"
  if [ "$flags" = "-" ]; then
    # L0 had no seeder: its shop is what its graph creates at first start.
    mkdir -p "$wt/cmd/l0-start"
    cat >"$wt/cmd/l0-start/main.go" <<'GO'
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/paths"
)

func main() {
	p, err := paths.Resolve()
	if err != nil {
		panic(err)
	}
	for _, d := range []string{p.Data, p.Backups, p.Logs, p.WebView} {
		_ = os.MkdirAll(d, 0o700)
	}
	app, err := bootstrap.Start(context.Background(), bootstrap.Options{Paths: p, Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))})
	if err != nil {
		panic(err)
	}
	_ = app.Shutdown(context.Background())
}
GO
    (cd "$wt" && MIZAN_LITE_DATA_DIR="$data" go run ./cmd/l0-start >/dev/null 2>&1)
  else
    # shellcheck disable=SC2086
    (cd "$wt" && MIZAN_LITE_DATA_DIR="$data" go run ./cmd/lite-demoseed $flags >/dev/null)
  fi
  db="$data/mizan-lite.db"
  sqlite3 "$db" "PRAGMA wal_checkpoint(TRUNCATE); VACUUM;" >/dev/null
  got="$(sqlite3 "$db" "SELECT MAX(version) FROM schema_migrations")"
  if [ "$got" != "$schema" ]; then
    echo "schema $schema: $commit produced schema $got" >&2
    exit 1
  fi
  cp "$db" "$OUT/schema-$schema.db"
  echo "✔ schema $schema from $commit: $(du -h "$OUT/schema-$schema.db" | cut -f1)"
done
