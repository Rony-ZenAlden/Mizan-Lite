#!/usr/bin/env bash
#
# Writes internal/lite/notices/THIRD_PARTY_NOTICES.txt — the licence of every Go module the application (and its tests) is built
# from, every npm package in the production bundle, and the OFL of the embedded Arabic font (L8 D-L8.15). Offline: the licences are
# read from the module cache and node_modules. Re-run when a dependency changes; TestEveryModuleHasALicence fails until you do.
set -euo pipefail
cd "$(dirname "$0")/.."
export GOTOOLCHAIN=local

OUT="internal/lite/notices/THIRD_PARTY_NOTICES.txt"
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

licence_of() {
  find "$1" -maxdepth 1 -type f \( -iname 'licen[cs]e*' -o -iname 'copying*' -o -iname 'notice*' \) 2>/dev/null | sort | head -3
}

{
  echo "Mizan Lite — third-party notices"
  echo
  echo "Mizan Lite is built from the software listed below. Each is used under the licence reproduced beneath its name."
  echo
  echo "== IBM Plex Sans Arabic (font) — SIL Open Font License 1.1"
  cat internal/lite/typeset/fonts/OFL.txt
  echo
  # Both systems the application ships on: Windows pulls in modules macOS does not (WebView2, the Windows API).
  for goos in darwin windows; do
    GOOS=$goos go list -deps -test -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}' ./apps/lite
  done | sort -u |
    while read -r path version dir; do
      files="$(licence_of "$dir")"
      if [ -z "$files" ]; then
        echo "no licence file for Go module $path $version in $dir" >&2
        exit 1
      fi
      echo "== $path $version"
      while IFS= read -r f; do cat "$f"; echo; done <<<"$files"
    done
  (cd apps/lite/frontend && npm ls --omit=dev --all --parseable 2>/dev/null) | sed 1d | sort -u |
    while read -r dir; do
      name="$(DIR="$dir" node -p "const p = require(process.env.DIR + '/package.json'); p.name + ' ' + p.version")"
      files="$(licence_of "$dir")"
      if [ -z "$files" ]; then
        echo "no licence file for npm package $name in $dir" >&2
        exit 1
      fi
      echo "== npm $name"
      while IFS= read -r f; do cat "$f"; echo; done <<<"$files"
    done
} >"$tmp"
mv "$tmp" "$OUT"
trap - EXIT
echo "✔ $OUT ($(grep -c '^== ' "$OUT") entries)"
