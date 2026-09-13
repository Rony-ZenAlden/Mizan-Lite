#!/usr/bin/env bash
#
# Proves every Mizan Lite architecture rule can FAIL.
#
# A rule nobody has watched fail may match nothing — a mistyped glob passes silently forever. So this
# plants one violation per rule, requires archlint to report THAT rule, and removes the plant. It runs
# on every `make lite-ci`, so a later edit to arch-rules.yml that breaks a pattern is caught the same
# day rather than never.
set -euo pipefail
cd "$(dirname "$0")/.."
export GOTOOLCHAIN=local

planted=()
# Ends in `return 0`: an EXIT trap's last status becomes the script's, and `[ -n "" ] && …` on an empty
# list is false — which made a fully successful run exit 1.
cleanup() {
  local f
  for f in "${planted[@]:-}"; do
    if [ -n "$f" ]; then rm -rf "$f"; fi
  done
  return 0
}
trap cleanup EXIT

# plant PATH CONTENT — write a Go file and remember to remove it (or its new directory).
plant() {
  local path="$1" content="$2" dir
  dir="$(dirname "$path")"
  if [ ! -d "$dir" ]; then mkdir -p "$dir"; planted+=("$dir"); else planted+=("$path"); fi
  printf '%s\n' "$content" > "$path"
}

drill() {
  local rule="$1" expect="$2"; shift 2
  plant "$@"
  local out
  if out="$(go run ./tools/archlint ./... 2>&1)"; then
    echo "SURVIVED: archlint passed with a planted '$rule' violation" >&2
    exit 1
  fi
  if ! grep -q -- "$expect" <<<"$out"; then
    echo "WRONG RULE: planted '$rule' but archlint reported something else:" >&2
    echo "$out" >&2
    exit 1
  fi
  echo "  caught  $rule"
  cleanup; planted=()
}

M=github.com/mizan-erp/mizan

drill lite-edition-boundary "Mizan Lite may import only" \
  internal/lite/zzdrill/drill.go "package zzdrill
import _ \"$M/internal/platform/outbox\""

drill mizan-independent-of-lite "Mizan must not import Mizan Lite" \
  internal/platform/zzdrill/drill.go "package zzdrill
import _ \"$M/internal/lite/paths\""

drill lite-app-entry "apps/lite may import only" \
  apps/lite/zz_drill.go "package main
import _ \"$M/internal/platform/config\""

drill lite-domain-purity "Lite domain packages may import only" \
  internal/lite/settings/domain/zz_drill.go "package domain
import _ \"$M/internal/platform/database\""

drill dialect-in-lite "only platform/database may be dialect-aware" \
  internal/lite/zzdrill/drill.go "package zzdrill
import _ \"$M/internal/platform/database/dialect\""

# A CALL. forbid-call matches call expressions, so `f := time.Now` (a function value, called later)
# would evade it — a limit of the rule kind, recorded rather than papered over; review is the defence.
drill time-now-in-lite "take a kernel/clock.Clock" \
  internal/lite/zzdrill/drill.go "package zzdrill
import \"time\"
var _ = time.Now()"

drill no-float-in-lite "float32/float64 are forbidden" \
  internal/lite/zzdrill/drill.go "package zzdrill
var _ float64"

drill no-sql-in-lite-service "SQL belongs to infra/platform" \
  internal/lite/settings/zz_drill.go "package settings
const drillQuery = \"SELECT value FROM settings WHERE setting_key = ?\""

echo "every Lite architecture rule was seen failing"
