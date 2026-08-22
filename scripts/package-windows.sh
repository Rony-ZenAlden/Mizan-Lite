#!/usr/bin/env bash
# Cross-build the Windows application and its NSIS installer, from macOS.
#
# # Why this works at all
#
# Wails refuses to cross-compile for Linux and does not refuse for Windows, and the reason is
# cgo. Windows needs none: the WebView2 loader is pure Go, and Step 0.3 chose modernc.org/sqlite
# — a pure-Go driver — over the cgo one. That decision, made for the offline constraint, is what
# makes a Windows release buildable from a Mac two phases later.
set -euo pipefail

cd "$(dirname "$0")/.."

# ── the offline runtime must be present BEFORE anything is built ──────────────
#
# `project.nsi` embeds it, so NSIS would fail on a missing file — but it would fail minutes in,
# after the Go build and the frontend bundle, naming a path rather than the cause. Checking here
# costs nothing and fails in a second with the command to fix it.
#
# The SIZE check is the one that matters. A bootstrapper sitting at this path would build an
# installer that looks correct, is 200MB smaller, and needs the internet on the machine it is
# installing onto — which is the exact failure the offline runtime exists to prevent, made
# invisible.
WEBVIEW2="build/windows/webview2/MicrosoftEdgeWebView2RuntimeInstallerX64.exe"
if [ ! -f "$WEBVIEW2" ]; then
  echo "error: the offline WebView2 runtime is missing." >&2
  echo "       Run ./scripts/fetch-webview2.sh first (~200MB, once per checkout)." >&2
  exit 1
fi
WEBVIEW2_SIZE=$(wc -c < "$WEBVIEW2" | tr -d ' ')
if [ "$WEBVIEW2_SIZE" -lt 100000000 ]; then
  echo "error: ${WEBVIEW2} is $(( WEBVIEW2_SIZE / 1024 / 1024 ))MB — that is the bootstrapper," >&2
  echo "       not the standalone runtime. The installer it produces would need the internet." >&2
  echo "       Run ./scripts/fetch-webview2.sh to replace it." >&2
  exit 1
fi
echo "▶ offline WebView2 runtime: $(( WEBVIEW2_SIZE / 1024 / 1024 ))MB"

VERSION="$(scripts/version.sh)"
LDFLAGS="-X github.com/mizan-erp/mizan/internal/buildinfo.Version=${VERSION} \
-X github.com/mizan-erp/mizan/internal/buildinfo.Commit=$(git rev-parse --short HEAD 2>/dev/null || echo none) \
-X github.com/mizan-erp/mizan/internal/buildinfo.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)"

echo "▶ building windows/amd64"
# CGO_ENABLED=0 is not decoration: the Go toolchain would otherwise try to find a Windows C
# compiler that does not exist on this machine, and the failure names a linker rather than the
# cause.
CGO_ENABLED=0 wails build -platform windows/amd64 -ldflags "$LDFLAGS" -o "Mizan.exe"

mkdir -p dist
cp "build/bin/Mizan.exe" "dist/Mizan ${VERSION}.exe"
echo "✔ dist/Mizan ${VERSION}.exe"

if ! command -v makensis >/dev/null 2>&1; then
  echo
  echo "NOTE: makensis not found, so only the bare .exe was produced."
  echo "  brew install makensis    # then re-run for the installer"
  exit 0
fi

echo "▶ building the NSIS installer"
CGO_ENABLED=0 wails build -platform windows/amd64 -nsis -ldflags "$LDFLAGS" -o "Mizan.exe"

INSTALLER="$(ls -t build/bin/*installer*.exe 2>/dev/null | head -1 || true)"
if [ -z "$INSTALLER" ]; then
  echo "error: wails reported success but produced no installer" >&2
  exit 1
fi
cp "$INSTALLER" "dist/Mizan ${VERSION} Setup.exe"
echo "✔ dist/Mizan ${VERSION} Setup.exe"

echo
echo "NOTE: this installer is UNSIGNED."
echo "  Windows SmartScreen will warn on first run until an Authenticode certificate signs it."
echo "  See docs/architecture/STEP_1_13_PACKAGING.md §4."
