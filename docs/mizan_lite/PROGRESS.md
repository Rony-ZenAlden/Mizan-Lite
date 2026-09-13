# Mizan Lite — progress

> **The resume point.** Read this first when picking Lite back up.
> **Last updated:** 2026-09-13. **Branch:** `lite/l0-skeleton`.

---

## 1. Where it stands

**L0 is complete and committed.** Next: the L1 design note.

| Phase | Status | Record |
|---|---|---|
| L0 — skeleton and gates | ✅ committed | [phases/L0_SKELETON.md](phases/L0_SKELETON.md) |
| L1 — catalogue, units, owner PIN, demo data | next: design note | — |
| L2–L8 | not started | [DESIGN.md §10](DESIGN.md) |

## 2. How to verify the current state

```bash
make lite-ci              # Go (race), Windows cross-compile, archlint + drills, golangci-lint v2, frontend, bundle gate
make lite-build-macos     # universal .app → apps/lite/build/bin/
make lite-build-windows   # .exe → apps/lite/build/bin/  (builds here; cannot be run here)
```

Last full run: 2026-09-13 — `make lite-ci` green, golangci-lint v2 **0 issues**; Mizan's own `scripts/check.sh`
green after the shared changes.

## 3. Open items

| # | Item | Owner | Since |
|---|---|---|---|
| O1 | **The Windows build has never been run.** No Windows machine or VM is available. | owner: provide a machine, or accept until L8 | L0 |
| O2 | **Nobody has visually confirmed the app** in Arabic and English. Screenshots are blocked by macOS permissions in this environment. | owner: open `apps/lite/build/bin/Mizan Lite.app` | L0 |
| O3 | WebView2 on a Windows 10 machine without it, offline — embed the runtime in the installer. | L8 | L0 |
| O4 | SYP cash-rounding denomination (Q5) — the value depends on the redenomination's status. | L4 design note | approval |

## 4. Findings for Mizan (not fixed in Mizan)

Discovered while building Lite. Each is argued in the phase record that found it.

| # | Finding | Recommendation | Found in |
|---|---|---|---|
| M1 | Mizan's `noWailsGlobals` ESLint rule is evaded by a cast or an alias | Forbid the `go`/`runtime` property outright, as Lite does | L0 F3 |
| M2 | `Info.plist` declares macOS 10.13; Go 1.27 builds for macOS 13 | Declare 13.0 | L0 F4 |
| M3 | The Windows database lives in Roaming AppData | Move to Local AppData (needs a data move) | L0 F5 |
| M4 | The pre-migration snapshot's manifest records an empty app version | Thread the build version into `platform/migrate` | L0 F8 |

**Fixed in shared code during L0** (committed): the `platform/database` DSN defect on paths containing `#` or
`%` (F1); Mizan's error-code gate now skips `internal/lite` (F6); golangci-lint upgraded to v2 for both
editions, with five small Mizan fixes its first honest run found (F2).

## 5. Next

1. Write the L1 design note (catalogue, units, owner PIN, demo data generator) in `phases/`.
2. Stop for approval.
