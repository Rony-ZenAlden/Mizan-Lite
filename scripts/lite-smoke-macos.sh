#!/usr/bin/env bash
# Opens a packaged Mizan Lite on a copy of a real shop, waits for the frontend to reach Go, and quits it (L8 D-L8.5).
#
# What L4–L7 did by hand at every phase. It runs against the INSTALLED application — the one in the disk image or in
# /Applications — not the build folder, because what ships is what must open.
#
#   scripts/lite-smoke-macos.sh "/Volumes/Mizan Lite 0.9.0/Mizan Lite.app" [a shop's data directory to copy]
set -euo pipefail
cd "$(dirname "$0")/.."

APP="${1:-apps/lite/build/bin/Mizan Lite.app}"
FIXTURE="${2:-}"
BIN="${APP}/Contents/MacOS/Mizan Lite"
test -x "$BIN" || { echo "error: ${BIN} is not there" >&2; exit 1; }

DATA="$(mktemp -d)/Mizan Lite"
mkdir -p "$DATA"
if [ -n "$FIXTURE" ]; then
  cp -R "$FIXTURE/." "$DATA/"
  echo "▶ a copy of ${FIXTURE}"
else
  echo "▶ a new shop"
fi
LOG="$DATA/logs/mizan-lite.log"

# The application holds a single-instance lock (apps/lite/options.go): a second launch hands itself to the copy already
# running and exits before it has logged a line. A shop's own copy left open — the 0.9.9 release met one — would otherwise
# read as "the package does not start".
RUNNING="$(pgrep -f "Mizan Lite.app/Contents/MacOS/Mizan Lite" | tr '\n' ' ' || true)"
if [ -n "$RUNNING" ]; then
  echo "error: Mizan Lite is already running (pid ${RUNNING% }) and its single-instance lock would take this launch — quit it and run again" >&2
  exit 1
fi

echo "▶ opening ${APP}"
MIZAN_LITE_DATA_DIR="$DATA" "$BIN" >/dev/null 2>&1 &
PID=$!
trap 'kill "$PID" 2>/dev/null || true' EXIT

# A shop that is set up reaches Go from the shell (App.Health, logged); a brand-new one shows the first-run screen instead, and
# the proof there is that the graph started and the window opened.
WANTED="health requested by the frontend"
[ -n "$FIXTURE" ] || WANTED="mizan lite ready"
for _ in $(seq 1 60); do
  [ -f "$LOG" ] && grep -q "$WANTED" "$LOG" && break
  sleep 1
done
if ! grep -q "$WANTED" "${LOG:-/dev/null}" 2>/dev/null; then
  echo "error: the packaged application never logged \"${WANTED}\"" >&2
  [ -f "$LOG" ] && tail -20 "$LOG" >&2
  exit 1
fi

kill -TERM "$PID" 2>/dev/null || true
for _ in $(seq 1 30); do kill -0 "$PID" 2>/dev/null || break; sleep 1; done

VERSION="$(sed -n 's/.*"mizan lite ready".*"version":"\([^"]*\)".*/\1/p' "$LOG" | tail -1)"
SCHEMA="$(sqlite3 "$DATA/mizan-lite.db" "SELECT MAX(version) FROM schema_migrations")"
INTEGRITY="$(sqlite3 "$DATA/mizan-lite.db" "PRAGMA integrity_check")"
FK="$(sqlite3 "$DATA/mizan-lite.db" "PRAGMA foreign_key_check" | wc -l | tr -d ' ')"
ERRORS="$(grep -c '"level":"ERROR"' "$LOG" || true)"
BACKUPS="$(ls "$DATA/backups"/*.db 2>/dev/null | wc -l | tr -d ' ')"

echo "  version:   ${VERSION:-unknown}"
echo "  schema:    ${SCHEMA}"
echo "  integrity: ${INTEGRITY}, ${FK} foreign-key problems"
echo "  backups:   ${BACKUPS}"
echo "  errors:    ${ERRORS}"
[ "$INTEGRITY" = "ok" ] && [ "$FK" = "0" ] && [ "$ERRORS" = "0" ] || { echo "error: the smoke test found problems above" >&2; exit 1; }
echo "✔ the packaged application opened, reached Go, and closed cleanly"
