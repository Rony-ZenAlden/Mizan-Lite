# Mizan Lite — the Definition of Done, read again across L0–L8

**2026-09-16 · release 0.9.0 (the pilot) · [L8 §12.4](L8_RELEASE.md)**

Every phase closed with a Definition of Done, and several closed with a criterion still open — carried forward rather than
dropped. Before a release goes to a shop, all of them are read again in one place. This is that reading: each phase's criteria,
what closes them today, and what is still owed.

Three things are owed, and all three are the owner's: **the Windows protocol**, **a receipt on paper and an export in Excel**,
and **the reading of the Arabic** by someone in the trade. Nothing here is owed by the code.

---

## 1. The carried-forward criteria, phase by phase

| Phase | Criterion left open at its commit | Where it stands at 0.9.0 |
|---|---|---|
| L0 | Direction before first paint — tested, **not observed** | ✅ **closed.** Every journey starts by reading `<html lang dir>` before the first paint, in both languages; the bridge serves the stored language exactly as the application's middleware does, and 65 screenshots show it |
| L0 | The packaged application looked at, in Arabic and RTL (O2) | ✅ closed 2026-09-13 by the owner |
| L0 | WebView2 on a Windows 10 machine without it, offline (O3) | ✅ **closed in the build:** the 213 MB standalone runtime is inside `Mizan Lite 0.9.0 Setup.exe` and installed before the application; ⏳ its *running* is part of the Windows protocol (O1) |
| L1 | The packaged application looked at in Arabic and English | ✅ **closed** by the visual pack — every screen at 1366×768 and 1024×768, both languages |
| L1–L7 | Windows `.exe` builds — **not run** (O1) | ⏳ **still owed.** It builds, it is structurally checked, its installer is held to what it must do by tests; no machine here can run it. [WINDOWS_PROTOCOL.md](WINDOWS_PROTOCOL.md) |
| L2 | The Stock screen looked at in both languages | ✅ closed by the visual pack |
| L3 | The seeded rates in the packaged application, the header's corrected rate looked at | ✅ closed — and the rate line was **wrong when looked at** (L8 S3) and is now `1 USD = 15,000 SYP` in both languages |
| L3 | The live providers answer (NOT RUN in CI by design) | ✅ unchanged: run by hand 2026-09-14; the opt-in test still exists; the 20% guard still holds (O11) |
| L4 | The Till and Sales screens looked at | ✅ closed by the visual pack — and **three defects were found by looking** (L8 R1) |
| L5 | Customers, the till's credit and the receipt looked at | ✅ closed by the visual pack and J4's voucher |
| L6 | Reports and Cash looked at | ✅ closed by the visual pack; the drawer's count date was wrong (S4) and is fixed |
| L6 | A year of sales within the timing bounds | ✅ unchanged (L6 §19); the low-end Windows measurement is O6/O8, `scripts/lite-timing.ps1` |
| L7 | **A receipt printed on the owner's thermal printer** (Q-L7.1) | ⏳ **still owed** (O14). The bytes are golden-tested and the images looked at; paper is paper |
| L7 | **An export opened in Excel** | ⏳ **still owed** (O14). The workbooks open in Numbers and QuickLook here; Excel's own reading of an RTL sheet is the check |
| L7 | A restore watched in the packaged application (O15) | ✅ **closed** by J9: a restore through the PIN, the safety snapshot, the restart, and the shop's state afterwards read back from Go |
| L7 | Mizan's `scripts/check.sh` green | ✅ re-run after L8's archlint change |
| L8 | Every criterion of [L8 §22](L8_RELEASE.md#22-definition-of-done) | see that table — green but for the three owed above |

## 2. The criteria every phase shared

| Criterion | At 0.9.0 |
|---|---|
| `make lite-ci` green | ✅ — now including the browser journeys and the visual pack |
| Every invariant mutation-drilled | ✅ 40 behaviour drills (L7) still caught; **89 architecture rules** each seen failing |
| golangci-lint v2 | ✅ 0 issues |
| The seeded shop opens in the packaged application and every verifier is clean | ✅ from the mounted disk image: schema 8, integrity ok, 0 foreign-key problems, 0 errors, a clean close |
| An installation from the previous phase upgrades with every row intact | ✅ and stronger: **schemas 1–8**, each written by that phase's own commit, upgrade on every CI run; a newer database is refused without being written |
| PROGRESS, DECISIONS and the phase record updated | ✅ |

## 3. What a release is allowed to ship with

L8 §12.5's gate says a release may ship with a criterion open only when it is **named**, **owed to a person, not to the code**,
and **recoverable if it fails**. The three owed here meet all three:

1. **The Windows protocol** — if it fails, the pilot is macOS-only until it passes; no shop is affected, because no shop has it.
2. **Paper and Excel** — if a receipt prints wrongly, the shop writes by hand for a day and L7 reopens; the sales are in the
   database either way, and the receipt can be reprinted (L7 §5.4).
3. **The Arabic** — if a word is wrong, it is a catalog entry and a re-render of one guide.

None of the three can lose a shop's money or its books, which is the line the gate draws.

## 4. What this review changed

Reading the criteria again is what produced §4 of the L8 note: **S1–S8 are defects found by looking at screens that earlier
phases had marked "⏳ looked at"** rather than by any test. The date that read `15/09/2026` as something else, the rate line,
the reference line, the drawer's count date, two navigation items called the same thing — every one of them had passed CI
for weeks. That is the argument for the visual pack being part of CI from now on, and not a thing done once at the end.

---

**Next:** the pilot ([PILOT.md](PILOT.md)). 1.0.0 is cut when the three owed checks pass and the pilot's exit criteria hold.
