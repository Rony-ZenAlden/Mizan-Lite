# Mizan Lite — progress

> **The resume point.** Read this first when picking Lite back up.
> **Last updated:** 2026-09-25 (0.10.1: dollars only everywhere, figures that stay in their cells, the shop's logo on screen,
> the furniture demo — [DECISIONS.md](DECISIONS.md) D-0101, [DEMO.md](DEMO.md)). Releases 0.9.3 to 0.10.1 are recorded release
> by release in [DECISIONS.md](DECISIONS.md) and listed in [README.md](README.md). The stocktake is designed, not built.
> **Branch:** `lite/l0-skeleton`.

---

## 1. Where it stands

**L0, L1 and L2 are complete and committed** (L2: `407fdbb`; its building decisions D-L2.i1–i14 approved 2026-09-14).
**L3 is complete and committed** (`67d72c8`) — internet and manual exchange rates, **manual mode by default** (owner, 2026-09-14): [phases/L3_RATES.md](phases/L3_RATES.md).
**L4 is complete and committed** (`76463fd`) — the till, sales, receipts, voids, discounts with the PIN, cash rounding to 500: [phases/L4_TILL.md](phases/L4_TILL.md) §14–§21; D-L4.i1–i11 approved 2026-09-14.
**L5 is complete and committed** (`97b7875`) — customers, credit sales, a debt book per customer and currency, repayments at the day's rate, openings, write-offs, refunds, reversals: [phases/L5_CUSTOMERS.md](phases/L5_CUSTOMERS.md) §15–§22; D-L5.i1–i12 approved 2026-09-14.
**L6 is complete and committed** (`2c86f17`) — reports (a day, a month, products, stock value reconciled, the profit on the shelf), a cash book and the cash drawer per currency; a void states the cash to hand back: [phases/L6_REPORTS.md](phases/L6_REPORTS.md) §15–§22; D-L6.i1–i15 approved 2026-09-15.
**L7 is committed** (`d356598`) — export of every report, statements, the debt ledger and the sales history to Excel and A4 PDF through the system's Save dialog; 80 mm thermal receipts and vouchers rendered in Go as Arabic bitmaps, through the printer's driver (default) or raw ESC/POS; verified backups copied to an outside folder, their status on Home, and restore with the owner's PIN, the loss stated and a safety snapshot: [phases/L7_HARDWARE_BACKUP.md](phases/L7_HARDWARE_BACKUP.md) §15–§22. D-L7.1–18, the answers and D-L7.i1–i19 approved 2026-09-15. **Two checks remain with the owner: a receipt printed on the shop's printer and an export opened in Excel** (O14, L7 §22.1).
**L8 is complete and committed** — end-to-end journeys in a real browser against the real Go graph, the Arabic and English polish those journeys found, an Excel import for the first products and stock, the About screen and the support file, the shop's guides rendered into the application, the upgrade matrix over every past schema, and the two installers: [phases/L8_RELEASE.md](phases/L8_RELEASE.md) §15–§22. D-L8.1–20, the owner's five answers and D-L8.i1–i21 approved 2026-09-16. **Release 0.9.0 is the pilot** ([RELEASE.md](RELEASE.md), [phases/PILOT.md](phases/PILOT.md)); three checks remain with the owner — the Windows protocol (O1), a receipt on paper and an export in Excel (O14), and the reading of the Arabic.

**The whole Definition of Done, L0 through L8, is read again in one place:** [phases/L8_DOD_REVIEW.md](phases/L8_DOD_REVIEW.md).

**0.9.1 (2026-09-16)** — the owner's changes after an afternoon in a seeded shop: every product on the till grid, a backup
schedule the shop chooses, **the owner PIN reserved for a restore and for bringing in a backup file** (26 of 28 acts opened
at the counter, every one still recorded in the owner's history), quick pay with the detail fields collapsed, and the shop's
header on A4 reports and workbooks. Decisions D-091.1–7 in [DECISIONS.md](DECISIONS.md).

**0.9.2 (2026-09-17)** — the cost price and profit margin on the product, and **the typed cost as the profit basis**
(D-L9.1): [phases/L9_COST_AND_AUDIT.md](phases/L9_COST_AND_AUDIT.md). **The stocktake the owner asked for alongside it is
designed but NOT built** — its schema waits as a draft for migration 0010, because a shop's database must not carry tables
nothing writes to (L9 §3).

| Phase | Status | Record |
|---|---|---|
| L0 — skeleton and gates | ✅ committed `c2a1d0e` | [phases/L0_SKELETON.md](phases/L0_SKELETON.md) |
| L1 — catalogue, units, owner PIN, demo data | ✅ committed | [phases/L1_CATALOGUE.md](phases/L1_CATALOGUE.md) |
| L2 — stock, weighted-average cost, opening packages | ✅ committed `407fdbb` | [phases/L2_STOCK.md](phases/L2_STOCK.md) |
| L3 — exchange rates and the currency system | ✅ committed `67d72c8` | [phases/L3_RATES.md](phases/L3_RATES.md) |
| L4 — the till: sales, checkout, receipts | ✅ committed `76463fd` | [phases/L4_TILL.md](phases/L4_TILL.md) |
| L5 — customers and debts | ✅ committed `97b7875` | [phases/L5_CUSTOMERS.md](phases/L5_CUSTOMERS.md) |
| L6 — reports: profit, stock value, cash drawer | ✅ committed `2c86f17` | [phases/L6_REPORTS.md](phases/L6_REPORTS.md) |
| L7 — export, printing, backup and restore | ✅ committed `d356598` — printer and Excel checks with the owner (O14) | [phases/L7_HARDWARE_BACKUP.md](phases/L7_HARDWARE_BACKUP.md) |
| L8 — release: E2E, polish, packaging, the shop's documents | ✅ committed, tagged `lite-v0.9.0` — the Windows protocol, paper and Excel with the owner | [phases/L8_RELEASE.md](phases/L8_RELEASE.md) |

## 2. How to verify the current state

```bash
make lite-ci              # Go (race), Windows cross-compile, archlint + drills, golangci-lint v2, frontend, bundle gate, E2E journeys
make lite-e2e             # the browser journeys and the visual pack alone (needs Chrome or Edge)
make lite-release         # CI + the upgrade matrix + both packages + the smoke test + checksums → dist/lite/
make lite-build-macos     # universal .app → apps/lite/build/bin/
make lite-build-windows   # .exe → apps/lite/build/bin/  (builds here; cannot be run here)
```

Last full run: 2026-09-16 (L8, on the tag `lite-v0.9.0`) — see [L8 §19.1](phases/L8_RELEASE.md): 492 Lite Go test functions
(race), 487 frontend tests, 89 architecture rules seen failing, golangci-lint v2 0 issues, and 20 end-to-end journeys plus 6
visual specs in Chrome; nothing NOT RUN. Mizan's `scripts/check.sh` green. `make lite-release` then built both packages and
opened the macOS application **inside the disk image** on a freshly seeded shop: version 0.9.0, schema 8, integrity ok, 0
foreign-key problems, 0 errors, a clean close. Artefacts and their checksums are in `dist/lite/`.

Try the seeded shop:

```bash
MIZAN_LITE_DATA_DIR=~/Desktop/lite-demo go run ./cmd/lite-demoseed      # 30 days of history by default (-days 0 for today only); prints the PIN
make lite-build-macos && MIZAN_LITE_DATA_DIR=~/Desktop/lite-demo "apps/lite/build/bin/Mizan Lite.app/Contents/MacOS/Mizan Lite"
```

## 3. Open items

| # | Item | Owner | Since |
|---|---|---|---|
| O1 | **The Windows build has never been run.** No Windows machine or VM is available. This includes L7's printing through the Windows spooler and GDI, and the Save and folder dialogs. **0.9.0's installer is built and waiting**; the checklist for the machine that runs it is [phases/WINDOWS_PROTOCOL.md](phases/WINDOWS_PROTOCOL.md) (Q-L8.1: the owner runs it). | owner | L0 |
| ~~O2~~ | ~~Nobody has visually confirmed the app~~ — **closed 2026-09-13**: the owner confirmed the interface lays out and runs correctly on macOS | — | L0 |
| O3 | ~~WebView2 on a Windows 10 machine without it, offline~~ — **built 2026-09-16**: the 213 MB standalone runtime is inside `Mizan Lite 0.9.0 Setup.exe` and installed before the application (D-L8.i16). Its *running* is step 3 of the Windows protocol (O1). | owner (with O1) | L0 |
| ~~O4~~ | ~~SYP cash-rounding denomination~~ — **closed 2026-09-14**: 500 pounds, to the nearest (Q-L4.1); the owner can change it with the PIN | — | approval |
| ~~O5~~ | ~~The L1–L7 screens have not been looked at~~ — **closed 2026-09-16**: every screen at two sizes and the main dialogs, in both languages, are in `build/lite-e2e/screens` (65 images), looked at, with structural checks on each in CI. Looking found eight defects (L8 §4, S1–S8), all fixed. The owner's own reading of the Arabic is Q-L8.14, in [phases/L8_DOD_REVIEW.md](phases/L8_DOD_REVIEW.md). | — | L1–L7 |
| O6 | Argon2id verify time on low-end Windows hardware not measured (it holds the single writer while it runs). **`scripts/lite-timing.ps1` measures it**; it needs the owner's machine (with O1). | owner | L1 |
| ~~O7~~ | ~~Nullable comparisons in CHECKs pass on NULL~~ — **closed 2026-09-14**: guarded in `sales` (L4) and `debt_entries` (L5), NULL cases refused by name through the runner; DESIGN §5's debt draft confirmed to accept a NULL amount (L5 R4). | — | L2 note |
| O8 | Cost at scale is partly measured (on this machine; the low-end Windows figures come with O1): who owes what over 50,000 debt entries takes 43 ms; a year of sales reports a month in 32 ms and a year of products in 356 ms (L6). The stock and sales verifiers over a year, `Search`/statements over thousands of customers, and the drawer of a shop never counted over years (it reads every day since the first) are not. | after the pilot | L2 |
| O9 | Quitting the packaged app through AppleScript reports "User cancelled (-128)" although it quits cleanly (L2 R8). Harmless; `scripts/lite-smoke-macos.sh` quits by signal instead and checks the close in the log. | after the pilot | L2 |
| ~~O10~~ | ~~The internet rate is official-style, not the market rate~~ — **closed 2026-09-14**: the owner set manual mode as the default; the internet rate is shown for reference | — | L3 |
| O11 | The free rate providers have no agreement; a change of scale or coverage is caught by the 20% guard and the opt-in live test, not prevented. Manual mode is the default, so a shop is never priced by a provider it did not choose. | after the pilot | L3 |
| ~~O12~~ | **Closed 2026-09-14** — the rebuild ran through the runner in `TestTheLedgerRebuildKeepsEveryRow` and on a real L3 shop in the packaged app, every row intact (L4 §18). L2's planned ledger rebuild (A-L2.3) fails as written — `stock_levels` and the self-reference block the drop under the runner's foreign keys. The working order is designed and verified on real data in L4 §6.2; it must be proven through the runner in L4's migration test. | L4 | L4 note |
| O14 | **Nothing has been printed on paper and no export opened in Excel** — no thermal printer or Excel here. L7's Definition of Done needs both, on the owner's machine (Q-L7.1). | owner | L7 |
| ~~O15~~ | ~~A restore has not been watched in the packaged app~~ — **closed 2026-09-16** by journey J9: the loss stated, the PIN, the safety snapshot, the restart, and the shop's state afterwards read back from Go, in both languages. | — | L7 |
| O16 | Two backups of one reason in the same second share a name, so one is kept (`platform/backup` names by the second). Unreachable in a shop — the reasons are hours apart — and a shared-code change; carried as a finding for Mizan (M5). | after the pilot | L7 |

**Closed in L8:** O3 (built), O5, O15 — see above. **Closed in L6:** O13 — a void's cash return is now derived from the receipt (`Sale.VoidReturn()`), stated in the void form before the PIN and summed by the drawer; receipt 12 of the seeded shop hands back 20,000 SYP (L6 D-L6.i2, §19).

## 4. Findings for Mizan (not fixed in Mizan)

Discovered while building Lite. Each is argued in the phase record that found it.

| # | Finding | Recommendation | Found in |
|---|---|---|---|
| M1 | Mizan's `noWailsGlobals` ESLint rule is evaded by a cast or an alias | Forbid the `go`/`runtime` property outright, as Lite does | L0 F3 |
| M2 | `Info.plist` declares macOS 10.13; Go 1.27 builds for macOS 13 | Declare 13.0 | L0 F4 |
| M3 | The Windows database lives in Roaming AppData | Move to Local AppData (needs a data move) | L0 F5 |
| M4 | The pre-migration snapshot's manifest records an empty app version | Thread the build version into `platform/migrate` | L0 F8 |
| M5 | `platform/backup` names a backup by the second, so two of one reason in the same second collide and one is kept | Name by the second **and a counter**, or refuse the second | L7 O16 |

**Fixed in shared code during L0** (committed): the `platform/database` DSN defect on paths containing `#` or
`%` (F1); Mizan's error-code gate now skips `internal/lite` (F6); golangci-lint upgraded to v2 for both
editions, with five small Mizan fixes its first honest run found (F2).

## 5. Next

**0.9.0 is built and tagged. What stands between it and 1.0.0 is three checks and a pilot week.**

1. Owner **runs the Windows protocol** (O1) on a Windows machine: [phases/WINDOWS_PROTOCOL.md](phases/WINDOWS_PROTOCOL.md) — install
   `Mizan Lite 0.9.0 Setup.exe` (offline, on a machine without WebView2 if possible), set the shop up, sell, print, export, back
   up, restore, then upgrade over the top and check the shop's data survived.
2. Owner **prints on the shop's 80 mm printer and opens an export in Excel** (O14) — the two checks L7 was committed with.
3. Owner **reads the Arabic** in `build/lite-e2e/screens` and in [guide/SHOP_GUIDE.ar.md](guide/SHOP_GUIDE.ar.md) (Q-L8.14):
   the words a shopkeeper would use, not the words a program would.
4. Then the **pilot week** in a real shop: [phases/PILOT.md](phases/PILOT.md). 1.0.0 is cut at its exit criteria.
