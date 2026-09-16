#!/usr/bin/env bash
# Cross-builds Mizan Lite for Windows and its NSIS installer, from macOS (L8 D-L8.11).
#
# Cross-compiling works because Lite has no cgo: the WebView2 loader is pure Go and L0 chose modernc.org/sqlite over the cgo
# driver. That decision, made for the offline constraint, is what makes a Windows release buildable from a Mac.
set -euo pipefail
cd "$(dirname "$0")/.."

if ! /usr/bin/git --version >/dev/null 2>&1 && [ -d /Library/Developer/CommandLineTools ]; then
  export DEVELOPER_DIR=/Library/Developer/CommandLineTools
fi

# The offline runtime must be present BEFORE anything is built: project.nsi embeds it, and its absence would fail minutes in,
# naming a path rather than the cause. The SIZE check is the one that matters — a 1.7MB bootstrapper here would build an
# installer that looks right, is 200MB smaller, and needs the internet on the machine it installs onto.
WEBVIEW2="build/windows/webview2/MicrosoftEdgeWebView2RuntimeInstallerX64.exe"
if [ ! -f "$WEBVIEW2" ]; then
  echo "error: the offline WebView2 runtime is missing." >&2
  echo "       Run ./scripts/fetch-webview2.sh first (~200MB, once per checkout)." >&2
  exit 1
fi
SIZE=$(wc -c < "$WEBVIEW2" | tr -d ' ')
if [ "$SIZE" -lt 100000000 ]; then
  echo "error: ${WEBVIEW2} is $(( SIZE / 1024 / 1024 ))MB — that is the bootstrapper, not the standalone runtime." >&2
  echo "       The installer it produces would need the internet. Run ./scripts/fetch-webview2.sh." >&2
  exit 1
fi
echo "▶ offline WebView2 runtime: $(( SIZE / 1024 / 1024 ))MB"

VERSION="$(scripts/lite-version.sh)"
DIST="dist/lite"
mkdir -p "$DIST"

echo "▶ building windows/amd64 and its installer (${VERSION})"
# CGO_ENABLED=0 is not decoration: the toolchain would otherwise look for a Windows C compiler that does not exist here, and
# the failure would name a linker rather than the cause.
(cd apps/lite && CGO_ENABLED=0 wails build -clean -platform windows/amd64 -nsis -ldflags "-X main.version=${VERSION}")

BARE="apps/lite/build/bin/Mizan Lite.exe"
SETUP="apps/lite/build/bin/Mizan Lite-amd64-installer.exe"
test -f "$SETUP" || { echo "error: the installer was not produced (is makensis installed? brew install makensis)" >&2; exit 1; }

cp "$BARE" "${DIST}/Mizan Lite ${VERSION}.exe"
cp "$SETUP" "${DIST}/Mizan Lite ${VERSION} Setup.exe"
SETUP_SIZE=$(wc -c < "${DIST}/Mizan Lite ${VERSION} Setup.exe" | tr -d ' ')
if [ "$SETUP_SIZE" -lt 100000000 ]; then
  echo "error: the installer is $(( SETUP_SIZE / 1024 / 1024 ))MB — the WebView2 runtime is not inside it." >&2
  exit 1
fi

echo "✔ $(pwd)/${DIST}/Mizan Lite ${VERSION} Setup.exe ($(( SETUP_SIZE / 1024 / 1024 ))MB, WebView2 inside)"
echo "✔ $(pwd)/${DIST}/Mizan Lite ${VERSION}.exe (the bare application)"
echo
echo "NOTE: unsigned (L8 Q-L8.2). Windows SmartScreen will warn: More info → Run anyway."
