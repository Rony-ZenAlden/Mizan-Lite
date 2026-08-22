#!/usr/bin/env bash
# Package the built .app into a distributable .dmg.
#
# `hdiutil` is macOS's own tool, so this needs nothing installed — which matters because the
# alternative (create-dmg, appdmg) would put an npm or brew dependency between a clean checkout
# and a release.
#
# The layout is the convention every Mac user already knows: the application on the left, a
# symlink to /Applications on the right, drag one onto the other. Getting this wrong is the
# difference between an install and a support call, and it costs one `ln -s`.
set -euo pipefail

cd "$(dirname "$0")/.."

APP_NAME="Mizan"
APP="build/bin/${APP_NAME}.app"
VERSION="$(scripts/version.sh)"
DIST="dist"
DMG="${DIST}/${APP_NAME} ${VERSION}.dmg"

if [ ! -d "$APP" ]; then
  echo "error: ${APP} not found — run 'make build-macos' first" >&2
  exit 1
fi

mkdir -p "$DIST"
rm -f "$DMG"

# Signing is a no-op until an identity exists, deliberately: an unsigned build must still be
# producible on a clean machine, or nobody can test the packaging without a paid account.
#
# The APP is signed here, before it is copied into the image. Signing it afterwards would leave
# the image carrying the unsigned copy — the signature would be on a bundle nobody ships.
if [ -n "${MIZAN_MACOS_IDENTITY:-}" ]; then
  echo "▶ signing the app with ${MIZAN_MACOS_IDENTITY}"
  codesign --force --deep --options runtime --timestamp \
    --sign "${MIZAN_MACOS_IDENTITY}" "$APP"
  codesign --verify --strict --verbose=2 "$APP"
fi

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

cp -R "$APP" "$STAGE/"
ln -s /Applications "$STAGE/Applications"

echo "▶ building ${DMG}"
# UDZO is zlib-compressed and read-only: the smallest thing to download and the only sensible
# state for a disk image someone is meant to copy out of, not write into.
hdiutil create \
  -volname "${APP_NAME}" \
  -srcfolder "$STAGE" \
  -ov -format UDZO \
  "$DMG" >/dev/null

if [ -n "${MIZAN_MACOS_IDENTITY:-}" ]; then
  echo "▶ signing the image"
  codesign --force --sign "${MIZAN_MACOS_IDENTITY}" "$DMG"
  echo "✔ ${DMG} — signed. Notarize with:"
  echo "    xcrun notarytool submit \"${DMG}\" --keychain-profile mizan --wait"
  echo "    xcrun stapler staple \"${DMG}\""
  exit 0
fi

echo "✔ ${DMG}"
echo
echo "NOTE: this image is UNSIGNED and NOT NOTARIZED."
echo "  Gatekeeper will refuse it on another Mac until an Apple Developer ID signs and"
echo "  notarizes it. Set MIZAN_MACOS_IDENTITY to a 'Developer ID Application: …' identity"
echo "  and re-run to sign; see docs/architecture/STEP_1_13_PACKAGING.md §4."
