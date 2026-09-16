#!/usr/bin/env bash
# Builds Mizan Lite for macOS and packages the .app into a .dmg (L8 D-L8.12).
#
# hdiutil is macOS's own tool, so a clean checkout needs nothing installed. The layout is the one every Mac user knows: the
# application on one side, a link to /Applications on the other, drag one onto the other.
set -euo pipefail
cd "$(dirname "$0")/.."

# A macOS update can leave Xcode's licence unaccepted, which breaks /usr/bin/git and the compiler drivers. The Command Line
# Tools carry their own copies and no licence gate, so a build never waits for an administrator.
if ! /usr/bin/git --version >/dev/null 2>&1 && [ -d /Library/Developer/CommandLineTools ]; then
  export DEVELOPER_DIR=/Library/Developer/CommandLineTools
  echo "▶ Xcode's licence is not accepted; building with the Command Line Tools"
fi

VERSION="$(scripts/lite-version.sh)"
APP="apps/lite/build/bin/Mizan Lite.app"
DIST="dist/lite"
DMG="${DIST}/Mizan Lite ${VERSION}.dmg"

echo "▶ building the universal application (${VERSION})"
(cd apps/lite && wails build -clean -platform darwin/universal -ldflags "-X main.version=${VERSION}")
test -d "$APP" || { echo "error: ${APP} was not built" >&2; exit 1; }

# Signing is opt-in (L8 D-L8.13): an unsigned build must still be producible on a machine with no certificate, or nobody can
# test the packaging. The app is signed BEFORE it is copied into the image, so the image carries the signed bundle.
if [ -n "${MIZAN_LITE_MACOS_IDENTITY:-}" ]; then
  echo "▶ signing the application with ${MIZAN_LITE_MACOS_IDENTITY}"
  codesign --force --deep --options runtime --timestamp --sign "${MIZAN_LITE_MACOS_IDENTITY}" "$APP"
  codesign --verify --strict --verbose=2 "$APP"
fi

mkdir -p "$DIST"
rm -f "$DMG"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
cp -R "$APP" "$STAGE/"
ln -s /Applications "$STAGE/Applications"
# The install guide travels inside the image: the person installing reads it before the shop does.
cp internal/lite/guide/pdf/install-ar.pdf "$STAGE/دليل التثبيت.pdf" 2>/dev/null || true
cp internal/lite/guide/pdf/install-en.pdf "$STAGE/Installation guide.pdf" 2>/dev/null || true

echo "▶ building ${DMG}"
hdiutil create -volname "Mizan Lite ${VERSION}" -srcfolder "$STAGE" -ov -format UDZO "$DMG" >/dev/null

if [ -n "${MIZAN_LITE_MACOS_IDENTITY:-}" ]; then
  codesign --force --sign "${MIZAN_LITE_MACOS_IDENTITY}" "$DMG"
  echo "✔ $(cd "$(dirname "$DMG")" && pwd)/$(basename "$DMG") — signed. Notarize with:"
  echo "    xcrun notarytool submit \"${DMG}\" --keychain-profile mizan-lite --wait"
  echo "    xcrun stapler staple \"${DMG}\""
else
  echo "✔ $(cd "$(dirname "$DMG")" && pwd)/$(basename "$DMG")"
  echo
  echo "NOTE: unsigned and not notarized (L8 Q-L8.2 — the pilot ships unsigned)."
  echo "  On this Mac it opens after: System Settings → Privacy & Security → Open Anyway."
fi
