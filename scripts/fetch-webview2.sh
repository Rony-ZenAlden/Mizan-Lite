#!/usr/bin/env bash
# Fetch the WebView2 Evergreen Standalone Installer, for a Windows installer that needs no network.
#
# # Why this is a separate script and not part of the build
#
# `wails build` downloads the ~1.7MB BOOTSTRAPPER on every run and embeds that — which fetches the
# real runtime from Microsoft at INSTALL time. On an air-gapped machine that fails, and the
# application installs, starts, and shows a blank window.
#
# Replacing the bootstrapper in place does not work: the next `wails build` overwrites it. So the
# offline runtime lives here, outside anything Wails manages, and `project.nsi` embeds it.
#
# It is roughly 200MB and is NOT committed. A binary that size in version control is a repository
# nobody can clone on a slow connection, and it is reproducible from one URL with a checksum.
set -euo pipefail

cd "$(dirname "$0")/.."

DEST="build/windows/webview2/MicrosoftEdgeWebView2RuntimeInstallerX64.exe"
# Microsoft's permanent link to the x64 Evergreen Standalone Installer.
URL="https://go.microsoft.com/fwlink/?linkid=2124701"

mkdir -p "$(dirname "$DEST")"

if [ -f "$DEST" ]; then
  SIZE=$(wc -c < "$DEST" | tr -d ' ')
  # A bootstrapper is ~1.7MB; the standalone is ~200MB. Anything small is the wrong file, and the
  # wrong file here produces an installer that looks right and needs the internet.
  if [ "$SIZE" -gt 100000000 ]; then
    echo "✔ ${DEST} already present ($(( SIZE / 1024 / 1024 ))MB)"
    exit 0
  fi
  echo "▶ replacing ${DEST}: $(( SIZE / 1024 / 1024 ))MB is too small to be the standalone runtime"
fi

echo "▶ downloading the WebView2 standalone runtime (~200MB)"
curl -fL --progress-bar -o "${DEST}.partial" "$URL"

SIZE=$(wc -c < "${DEST}.partial" | tr -d ' ')
if [ "$SIZE" -lt 100000000 ]; then
  rm -f "${DEST}.partial"
  echo "error: downloaded ${SIZE} bytes — that is the bootstrapper, not the standalone runtime." >&2
  echo "       Microsoft may have changed the link. See https://developer.microsoft.com/microsoft-edge/webview2/" >&2
  exit 1
fi

# Windows executables only. A redirect to an error page would otherwise be embedded silently.
if ! head -c 2 "${DEST}.partial" | grep -q "MZ"; then
  rm -f "${DEST}.partial"
  echo "error: the download is not a Windows executable." >&2
  exit 1
fi

mv "${DEST}.partial" "$DEST"
echo "✔ ${DEST} ($(( SIZE / 1024 / 1024 ))MB)"
echo
echo "  sha256: $(shasum -a 256 "$DEST" | cut -d' ' -f1)"
echo
echo "  This file is NOT committed. Re-run this script on a fresh checkout before"
echo "  ./scripts/package-windows.sh, or the installer will refuse to build."
