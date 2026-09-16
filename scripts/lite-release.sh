#!/usr/bin/env bash
# One command for a Mizan Lite release (L8 D-L8.14): everything that can be checked here, then both packages, then the packaged
# macOS application opened on a real shop, then the checksums and a manifest that says what was NOT verified.
#
#   make lite-release
#
# It stops at the first failure. A release built anywhere but on a `lite-v<version>` tag marks itself a development build.
set -euo pipefail
cd "$(dirname "$0")/.."

if ! /usr/bin/git --version >/dev/null 2>&1 && [ -d /Library/Developer/CommandLineTools ]; then
  export DEVELOPER_DIR=/Library/Developer/CommandLineTools
fi

VERSION="$(scripts/lite-version.sh)"
DIST="dist/lite"
echo "▶ Mizan Lite ${VERSION}"

if [ -n "$(git status --porcelain --untracked-files=no)" ]; then
  echo "  ⚠ the working tree has uncommitted changes — this release is not reproducible from the repository"
fi

# The release folder holds exactly this release: a leftover artefact of another build would otherwise be checksummed into
# SHA256SUMS alongside it (a development build's name contains the release's number).
rm -rf "$DIST"

echo "▶ the full local CI"
scripts/lite-check.sh

echo "▶ the guides are the documents they are rendered from"
go run ./cmd/lite-guides >/dev/null
if [ -n "$(git status --porcelain internal/lite/guide/pdf)" ]; then
  echo "error: the shipped guides differ from docs/mizan_lite/guide — commit the regenerated PDFs" >&2
  exit 1
fi

echo "▶ every past schema upgrades to this release"
go test -count=1 -run "TestEveryPastSchemaUpgradesToThisRelease|TestAnOlderBinaryRefusesANewerDatabase" ./internal/lite/bootstrap/

scripts/lite-package-macos.sh
scripts/lite-package-windows.sh

# The application under test is the one INSIDE the disk image, not the one in the build folder: what ships is what must open,
# and the Windows build above cleans the build folder out from under it anyway.
echo "▶ the packaged application opens a real shop"
SHOP="$(mktemp -d)/seeded"
MIZAN_LITE_DATA_DIR="$SHOP" go run ./cmd/lite-demoseed -days 3 >/dev/null
MOUNT="$(mktemp -d)"
hdiutil attach "${DIST}/Mizan Lite ${VERSION}.dmg" -mountpoint "$MOUNT" -nobrowse -quiet
trap 'hdiutil detach "$MOUNT" -quiet 2>/dev/null || true' EXIT
scripts/lite-smoke-macos.sh "${MOUNT}/Mizan Lite.app" "$SHOP"
# The application has only just closed, and macOS can still hold the image for a moment: hdiutil answers 16, "resource
# busy", and an impatient detach would fail a release whose artefacts are already built and verified.
detach() {
  for _ in 1 2 3 4 5; do
    if hdiutil detach "$MOUNT" -quiet 2>/dev/null; then
      return 0
    fi
    sleep 2
  done
  hdiutil detach "$MOUNT" -force -quiet 2>/dev/null || true
}
detach
trap - EXIT

echo "▶ checksums"
(cd "$DIST" && shasum -a 256 -- *"${VERSION}"* > "SHA256SUMS-${VERSION}.txt" && cat "SHA256SUMS-${VERSION}.txt")

cat > "${DIST}/MANIFEST-${VERSION}.txt" <<MANIFEST
Mizan Lite ${VERSION}
built    $(date -u +%Y-%m-%dT%H:%M:%SZ) on $(sw_vers -productName 2>/dev/null || uname -s) $(sw_vers -productVersion 2>/dev/null || uname -r)
commit   $(git rev-parse HEAD 2>/dev/null || echo none)
go       $(go version | cut -d' ' -f3)
wails    $(cd apps/lite && wails version 2>/dev/null | head -1)

Verified here
  the full local CI (Go with the race detector, archlint and its drills, lint, the frontend, the end-to-end journeys)
  every past schema (1-8) upgrades to this release with every row intact
  the macOS application INSIDE the disk image opened a seeded shop, reached Go and closed cleanly

NOT verified here — needs the owner's machines (L8 §7, §12.4)
  the Windows installer has never been run: no Windows machine exists in this build environment
  printing on a thermal printer, and an export opened in Excel
  Gatekeeper and SmartScreen: both artefacts are UNSIGNED (L8 Q-L8.2)
MANIFEST
echo
echo "✔ dist/lite:"
ls -la "$DIST" | sed 's/^/    /'
