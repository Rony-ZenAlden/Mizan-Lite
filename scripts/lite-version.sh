#!/usr/bin/env bash
# Mizan Lite's version (L8 D-L8.10): the number in apps/lite/wails.json, which the installer, the .app's Info.plist, the About
# screen, the log's first line and every backup manifest all take. A build made exactly on its tag (lite-v<version>) is a
# release and says so; anything else is a development build and says THAT.
set -euo pipefail
cd "$(dirname "$0")/.."

BASE="$(sed -n 's/.*"productVersion": *"\([^"]*\)".*/\1/p' apps/lite/wails.json)"
if [ -z "$BASE" ]; then
  echo "lite-version: no productVersion in apps/lite/wails.json" >&2
  exit 1
fi
SHA="$(git rev-parse --short HEAD 2>/dev/null || echo none)"
if git describe --exact-match --tags --match "lite-v*" HEAD >/dev/null 2>&1; then
  echo "$BASE"
else
  echo "${BASE}-dev.${SHA}"
fi
