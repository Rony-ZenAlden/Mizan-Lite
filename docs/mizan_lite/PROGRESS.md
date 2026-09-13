# Mizan Lite — progress

> **The resume point.** Read this first when picking Lite back up.
> **Last updated:** 2026-09-14 (L2 implemented). **Branch:** `lite/l0-skeleton`.

---

## 1. Where it stands

**L0, L1 and L2 are complete and committed.** L2's building decisions D-L2.i1–i14 were approved 2026-09-14.

| Phase | Status | Record |
|---|---|---|
| L0 — skeleton and gates | ✅ committed `c2a1d0e` | [phases/L0_SKELETON.md](phases/L0_SKELETON.md) |
| L1 — catalogue, units, owner PIN, demo data | ✅ committed | [phases/L1_CATALOGUE.md](phases/L1_CATALOGUE.md) |
| L2 — stock, weighted-average cost, opening packages | ✅ committed | [phases/L2_STOCK.md](phases/L2_STOCK.md) |
| L3–L8 | not started | [DESIGN.md §10](DESIGN.md) |

## 2. How to verify the current state

```bash
make lite-ci              # Go (race), Windows cross-compile, archlint + drills, golangci-lint v2, frontend, bundle gate
make lite-build-macos     # universal .app → apps/lite/build/bin/
make lite-build-windows   # .exe → apps/lite/build/bin/  (builds here; cannot be run here)
```

Last full run: 2026-09-14 (L2) — `make lite-ci` green: 220 Lite Go test functions (race), 225 frontend tests, 4 bundle
gates, 22 architecture rules seen failing, golangci-lint v2 0 issues; 28 mutation drills caught. Mizan's
`scripts/check.sh` green (86 packages, 337 frontend tests) after L2's kernel additions.

Try the seeded shop:

```bash
MIZAN_LITE_DATA_DIR=~/Desktop/lite-demo go run ./cmd/lite-demoseed      # prints the PIN and recovery code
make lite-build-macos && MIZAN_LITE_DATA_DIR=~/Desktop/lite-demo "apps/lite/build/bin/Mizan Lite.app/Contents/MacOS/Mizan Lite"
```

## 3. Open items

| # | Item | Owner | Since |
|---|---|---|---|
| O1 | **The Windows build has never been run.** No Windows machine or VM is available. | owner: provide a machine, or accept until L8 | L0 |
| ~~O2~~ | ~~Nobody has visually confirmed the app~~ — **closed 2026-09-13**: the owner confirmed the interface lays out and runs correctly on macOS | — | L0 |
| O3 | WebView2 on a Windows 10 machine without it, offline — embed the runtime in the installer. | L8 | L0 |
| O4 | SYP cash-rounding denomination (Q5) — the value depends on the redenomination's status. | L4 design note | approval |
| O5 | **The L1 and L2 screens (products, PIN dialog, first run, owner, stock and its dialogs) have not been looked at** — screenshots blocked here. | owner | L1, L2 |
| O6 | Argon2id verify time on low-end Windows hardware not measured (it holds the single writer while it runs). | L8 | L1 |
| O7 | **DESIGN §5's drafts for `sales` and `debt_entries` compare nullable columns in CHECKs without `IS NOT NULL`** (e.g. `tendered_minor > 0 AND local_per_usd_nano > 0`). A CHECK that evaluates to NULL passes, so those drafts accept rows they mean to refuse. Found verifying L2's schema, where the same pattern accepted a pound receipt with no rate. | L4, L5 design notes | L2 note |
| O8 | The stock verifier's cost on a large ledger is unmeasured (it streams; demo ledger is 47 rows). Re-measure once L4's sales write to it. | L4 | L2 |
| O9 | Quitting the packaged app through AppleScript reports "User cancelled (-128)" although it quits cleanly (L2 R8). | L8 | L2 |

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

1. L3 design note — exchange rates and the currency system, which pre-fill the rate L2's receipt form asks for (Q-L2.1).
2. Owner looks at the seeded shop, the Stock screen included (O5).
