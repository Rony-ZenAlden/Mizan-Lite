#!/usr/bin/env bash
# Regenerate every platform icon from the one source in scripts/icon.py.
#
# macOS ships the two tools this needs — `sips` for resampling and `iconutil` for the .icns
# container — so the whole pipeline runs offline with nothing installed. The Windows .ico is
# packed by icon.py, because macOS ships nothing that writes one.
set -euo pipefail

cd "$(dirname "$0")/.."
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "▶ rendering the master"
python3 scripts/icon.py

# macOS needs nothing more: Wails derives Contents/Resources/iconfile.icns from appicon.png
# during the build. A first draft also produced build/darwin/icon.icns — verified afterwards to
# be a file Wails never reads, so it went. Two .icns files and no way to tell which one ships is
# exactly the sort of thing that wastes an afternoon in three years.

echo "▶ Windows .ico"
ICO="$WORK/ico"
mkdir -p "$ICO"
# 256 is the largest the format's one-byte dimension field can name; 16 through 48 are what
# Explorer, the taskbar, and Alt-Tab actually reach for.
for size in 16 24 32 48 64 128 256; do
  sips -Z "$size" build/appicon.png --out "$ICO/$size.png" >/dev/null
done
python3 scripts/icon.py --ico "$ICO"

echo "✔ icons regenerated"
