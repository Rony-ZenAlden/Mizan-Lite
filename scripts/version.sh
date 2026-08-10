#!/usr/bin/env bash
# The single source of the product version.
#
# `git describe --tags --always` was used until Step 1.13, and with no tags in the repository it
# returned a bare commit hash — which produced an installer called "Mizan ERP 2050917 Setup.exe"
# and a Windows file-version field Explorer could not parse.
#
# The rule: VERSION is the number the product claims. A build made exactly on a matching tag is
# a release and says so; anything else is a development build and says THAT, so a binary can
# never be mistaken for a release it is not.
set -euo pipefail
cd "$(dirname "$0")/.."

BASE="$(tr -d '[:space:]' < VERSION)"
SHA="$(git rev-parse --short HEAD 2>/dev/null || echo none)"

if git describe --exact-match --tags HEAD >/dev/null 2>&1; then
  echo "$BASE"
else
  echo "${BASE}-dev.${SHA}"
fi
