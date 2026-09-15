# Mizan Lite — progress

> **The resume point.** Read this first when picking Lite back up.
> **Last updated:** 2026-09-15 (L7 committed). **Branch:** `lite/l0-skeleton`.

---

## 1. Where it stands

**L0, L1 and L2 are complete and committed** (L2: `407fdbb`; its building decisions D-L2.i1–i14 approved 2026-09-14).
**L3 is complete and committed** (`67d72c8`) — internet and manual exchange rates, **manual mode by default** (owner, 2026-09-14): [phases/L3_RATES.md](phases/L3_RATES.md).
**L4 is complete and committed** (`76463fd`) — the till, sales, receipts, voids, discounts with the PIN, cash rounding to 500: [phases/L4_TILL.md](phases/L4_TILL.md) §14–§21; D-L4.i1–i11 approved 2026-09-14.
**L5 is complete and committed** (`97b7875`) — customers, credit sales, a debt book per customer and currency, repayments at the day's rate, openings, write-offs, refunds, reversals: [phases/L5_CUSTOMERS.md](phases/L5_CUSTOMERS.md) §15–§22; D-L5.i1–i12 approved 2026-09-14.
**L6 is complete and committed** (`2c86f17`) — reports (a day, a month, products, stock value reconciled, the profit on the shelf), a cash book and the cash drawer per currency; a void states the cash to hand back: [phases/L6_REPORTS.md](phases/L6_REPORTS.md) §15–§22; D-L6.i1–i15 approved 2026-09-15.
**L7 is committed** — export of every report, statements, the debt ledger and the sales history to Excel and A4 PDF through the system's Save dialog; 80 mm thermal receipts and vouchers rendered in Go as Arabic bitmaps, through the printer's driver (default) or raw ESC/POS; verified backups copied to an outside folder, their status on Home, and restore with the owner's PIN, the loss stated and a safety snapshot: [phases/L7_HARDWARE_BACKUP.md](phases/L7_HARDWARE_BACKUP.md) §15–§22. D-L7.1–18, the answers and D-L7.i1–i19 approved 2026-09-15. **Two checks remain with the owner: a receipt printed on the shop's printer and an export opened in Excel** (O14, L7 §22.1).

| Phase | Status | Record |
|---|---|---|
| L0 — skeleton and gates | ✅ committed `c2a1d0e` | [phases/L0_SKELETON.md](phases/L0_SKELETON.md) |
| L1 — catalogue, units, owner PIN, demo data | ✅ committed | [phases/L1_CATALOGUE.md](phases/L1_CATALOGUE.md) |
| L2 — stock, weighted-average cost, opening packages | ✅ committed `407fdbb` | [phases/L2_STOCK.md](phases/L2_STOCK.md) |
| L3 — exchange rates and the currency system | ✅ committed `67d72c8` | [phases/L3_RATES.md](phases/L3_RATES.md) |
| L4 — the till: sales, checkout, receipts | ✅ committed `76463fd` | [phases/L4_TILL.md](phases/L4_TILL.md) |
| L5 — customers and debts | ✅ committed `97b7875` | [phases/L5_CUSTOMERS.md](phases/L5_CUSTOMERS.md) |
| L6 — reports: profit, stock value, cash drawer | ✅ committed `2c86f17` | [phases/L6_REPORTS.md](phases/L6_REPORTS.md) |
| L7 — export, printing, backup and restore | ✅ committed — printer and Excel checks with the owner (O14) | [phases/L7_HARDWARE_BACKUP.md](phases/L7_HARDWARE_BACKUP.md) |
| L8 — release | not started | [DESIGN.md §10](DESIGN.md) |

## 2. How to verify the current state

```bash
make lite-ci              # Go (race), Windows cross-compile, archlint + drills, golangci-lint v2, frontend, bundle gate
make lite-build-macos     # universal .app → apps/lite/build/bin/
make lite-build-windows   # .exe → apps/lite/build/bin/  (builds here; cannot be run here)
```

Last full run: 2026-09-15 (L7) — see [L7 §19.1](phases/L7_HARDWARE_BACKUP.md): 480 Lite Go test functions (race), 471 frontend
tests, 85 architecture rules seen failing, golangci-lint v2 0 issues; 40 behaviour drills caught; a year of sales history to
Excel in 1.2 s and to PDF in 2.7 s. Mizan's `scripts/check.sh` green (114 packages) after L7's archlint change. The packaged app
upgraded the seeded L6 month to schema 8 with every row intact.

Try the seeded shop:

```bash
MIZAN_LITE_DATA_DIR=~/Desktop/lite-demo go run ./cmd/lite-demoseed      # 30 days of history by default (-days 0 for today only); prints the PIN
make lite-build-macos && MIZAN_LITE_DATA_DIR=~/Desktop/lite-demo "apps/lite/build/bin/Mizan Lite.app/Contents/MacOS/Mizan Lite"
```

## 3. Open items

| # | Item | Owner | Since |
|---|---|---|---|
| O1 | **The Windows build has never been run.** No Windows machine or VM is available. This includes L7's printing through the Windows spooler and GDI, and the Save and folder dialogs. | owner: provide a machine, or accept until L8 | L0 |
| ~~O2~~ | ~~Nobody has visually confirmed the app~~ — **closed 2026-09-13**: the owner confirmed the interface lays out and runs correctly on macOS | — | L0 |
| O3 | WebView2 on a Windows 10 machine without it, offline — embed the runtime in the installer. | L8 | L0 |
| ~~O4~~ | ~~SYP cash-rounding denomination~~ — **closed 2026-09-14**: 500 pounds, to the nearest (Q-L4.1); the owner can change it with the PIN | — | approval |
| O5 | **The L1–L7 screens (products, PIN dialog, first run, owner, stock, rates, the Till, Sales, receipt and cash rounding, Customers, the statement, payments and the till's credit, Reports, the Cash drawer and the void form's *Hand back*, and now Printer settings, Backups, Home's backup status, the export buttons and the receipt's *As printed* tab) have not been looked at** — screenshots blocked here. L7's printouts and exports themselves were rendered and looked at (L7 §19). | owner | L1–L7 |
| O6 | Argon2id verify time on low-end Windows hardware not measured (it holds the single writer while it runs). | L8 | L1 |
| ~~O7~~ | ~~Nullable comparisons in CHECKs pass on NULL~~ — **closed 2026-09-14**: guarded in `sales` (L4) and `debt_entries` (L5), NULL cases refused by name through the runner; DESIGN §5's debt draft confirmed to accept a NULL amount (L5 R4). | — | L2 note |
| O8 | Cost at scale is partly measured: who owes what over 50,000 debt entries takes 43 ms; a year of sales reports a month in 32 ms and a year of products in 356 ms (L6). The stock and sales verifiers over a year, `Search`/statements over thousands of customers, and the drawer of a shop never counted over years (it reads every day since the first) are not. | L8 | L2 |
| O9 | Quitting the packaged app through AppleScript reports "User cancelled (-128)" although it quits cleanly (L2 R8). | L8 | L2 |
| ~~O10~~ | ~~The internet rate is official-style, not the market rate~~ — **closed 2026-09-14**: the owner set manual mode as the default; the internet rate is shown for reference | — | L3 |
| O11 | The free rate providers have no agreement; a change of scale or coverage is caught by the 20% guard and the opt-in live test, not prevented. | L8 | L3 |
| ~~O12~~ | **Closed 2026-09-14** — the rebuild ran through the runner in `TestTheLedgerRebuildKeepsEveryRow` and on a real L3 shop in the packaged app, every row intact (L4 §18). L2's planned ledger rebuild (A-L2.3) fails as written — `stock_levels` and the self-reference block the drop under the runner's foreign keys. The working order is designed and verified on real data in L4 §6.2; it must be proven through the runner in L4's migration test. | L4 | L4 note |
| O14 | **Nothing has been printed on paper and no export opened in Excel** — no thermal printer or Excel here. L7's Definition of Done needs both, on the owner's machine (Q-L7.1). | owner | L7 |
| O15 | A restore has not been watched in the packaged app (proven in Go: the graph restarts on the restored file and the webview reload is called). | owner / L8 | L7 |
| O16 | Two backups of one reason in the same second share a name, so one is kept (`platform/backup` names by the second). | L8 | L7 |

**Closed in L6:** O13 — a void's cash return is now derived from the receipt (`Sale.VoidReturn()`), stated in the void form before the PIN and summed by the drawer; receipt 12 of the seeded shop hands back 20,000 SYP (L6 D-L6.i2, §19).

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

1. Owner **prints on the shop's 80 mm printer** (O14): Printer settings → choose it → *Print a test page*; then a sale's receipt and a payment's voucher. If the driver path's receipt is scaled or cut, try *Straight to the printer (ESC/POS)*.
2. Owner **opens an export in Excel** (O14): Reports → a tab → *Excel*; check the sheet reads right to left and the sums work.
3. L8's design note: end-to-end testing, Arabic and English polish, release packaging, the shop's documentation.
