# Mizan Lite — progress

> **The resume point.** Read this first when picking Lite back up.
> **Last updated:** 2026-09-14 (L4 committed; L5 design note next). **Branch:** `lite/l0-skeleton`.

---

## 1. Where it stands

**L0, L1 and L2 are complete and committed** (L2: `407fdbb`; its building decisions D-L2.i1–i14 approved 2026-09-14).
**L3 is complete and committed** (`67d72c8`) — internet and manual exchange rates, **manual mode by default** (owner, 2026-09-14): [phases/L3_RATES.md](phases/L3_RATES.md).
**L4 is complete and committed** — the till, sales, receipts, voids, discounts with the PIN, cash rounding to 500: [phases/L4_TILL.md](phases/L4_TILL.md) §14–§21; D-L4.i1–i11 approved 2026-09-14.

| Phase | Status | Record |
|---|---|---|
| L0 — skeleton and gates | ✅ committed `c2a1d0e` | [phases/L0_SKELETON.md](phases/L0_SKELETON.md) |
| L1 — catalogue, units, owner PIN, demo data | ✅ committed | [phases/L1_CATALOGUE.md](phases/L1_CATALOGUE.md) |
| L2 — stock, weighted-average cost, opening packages | ✅ committed `407fdbb` | [phases/L2_STOCK.md](phases/L2_STOCK.md) |
| L3 — exchange rates and the currency system | ✅ committed `67d72c8` | [phases/L3_RATES.md](phases/L3_RATES.md) |
| L4 — the till: sales, checkout, receipts | ✅ committed | [phases/L4_TILL.md](phases/L4_TILL.md) |
| L5–L8 | not started | [DESIGN.md §10](DESIGN.md); scope restated in [L4_TILL.md §2.3](phases/L4_TILL.md) |

## 2. How to verify the current state

```bash
make lite-ci              # Go (race), Windows cross-compile, archlint + drills, golangci-lint v2, frontend, bundle gate
make lite-build-macos     # universal .app → apps/lite/build/bin/
make lite-build-windows   # .exe → apps/lite/build/bin/  (builds here; cannot be run here)
```

Last full run: 2026-09-14 (L4) — see [L4 §18.1](phases/L4_TILL.md): 328 Lite Go test functions (race), 317 frontend
tests, 39 architecture rules seen failing, golangci-lint v2 0 issues; 23 behaviour drills caught. Mizan's `scripts/check.sh`
green (95 packages, 337 frontend tests) after L4's archlint change. The packaged app upgraded a real L3 shop through the
ledger rebuild with every row intact.

Try the seeded shop:

```bash
MIZAN_LITE_DATA_DIR=~/Desktop/lite-demo go run ./cmd/lite-demoseed      # prints the PIN, recovery code and the day's takings
make lite-build-macos && MIZAN_LITE_DATA_DIR=~/Desktop/lite-demo "apps/lite/build/bin/Mizan Lite.app/Contents/MacOS/Mizan Lite"
```

## 3. Open items

| # | Item | Owner | Since |
|---|---|---|---|
| O1 | **The Windows build has never been run.** No Windows machine or VM is available. | owner: provide a machine, or accept until L8 | L0 |
| ~~O2~~ | ~~Nobody has visually confirmed the app~~ — **closed 2026-09-13**: the owner confirmed the interface lays out and runs correctly on macOS | — | L0 |
| O3 | WebView2 on a Windows 10 machine without it, offline — embed the runtime in the installer. | L8 | L0 |
| ~~O4~~ | ~~SYP cash-rounding denomination~~ — **closed 2026-09-14**: 500 pounds, to the nearest (Q-L4.1); the owner can change it with the PIN | — | approval |
| O5 | **The L1–L4 screens (products, PIN dialog, first run, owner, stock, rates, and now the Till, Sales, receipt and cash rounding) have not been looked at** — screenshots blocked here. | owner | L1–L4 |
| O6 | Argon2id verify time on low-end Windows hardware not measured (it holds the single writer while it runs). | L8 | L1 |
| O7 | **Nullable comparisons in CHECKs pass on NULL.** DESIGN §5's drafts compare nullable columns without `IS NOT NULL` (e.g. `tendered_minor > 0 AND local_per_usd_nano > 0`), so they accept rows they mean to refuse — found in L2, where the pattern accepted a pound receipt with no rate. **Closed for `sales` in L4** (every comparison guarded, NULL cases tested by raw insert); **open for `debt_entries`**, to be applied in L5's schema. | L5 design note | L2 note |
| O8 | The stock and sales verifiers' cost on a large ledger is unmeasured (the seeded day: 64 ledger rows, 10 sales — instant). A year of sales is not. | L6 or L8 | L2 |
| O9 | Quitting the packaged app through AppleScript reports "User cancelled (-128)" although it quits cleanly (L2 R8). | L8 | L2 |
| ~~O10~~ | ~~The internet rate is official-style, not the market rate~~ — **closed 2026-09-14**: the owner set manual mode as the default; the internet rate is shown for reference | — | L3 |
| O11 | The free rate providers have no agreement; a change of scale or coverage is caught by the 20% guard and the opt-in live test, not prevented. | L8 | L3 |
| ~~O12~~ | **Closed 2026-09-14** — the rebuild ran through the runner in `TestTheLedgerRebuildKeepsEveryRow` and on a real L3 shop in the packaged app, every row intact (L4 §18). L2's planned ledger rebuild (A-L2.3) fails as written — `stock_levels` and the self-reference block the drop under the runner's foreign keys. The working order is designed and verified on real data in L4 §6.2; it must be proven through the runner in L4's migration test. | L4 | L4 note |

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

1. L5's design note — customers and debts — written and presented for approval.
2. Owner looks at the seeded shop's till, sales and receipts in both languages (O5): seed with the command in §2, PIN 481537.
