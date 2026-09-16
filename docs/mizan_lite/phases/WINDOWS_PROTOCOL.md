# Windows verification protocol — Mizan Lite

> **Status: not run.** No Windows machine exists in the build environment (PROGRESS O1). Every Windows path in Lite is compiled
> and cross-vetted on every CI run; none has been executed. This is the checklist for the machine that can, and the results
> table below is filled in and committed when it is.

Run it on the shop's own computer if possible, **with the network cable unplugged from step 2**, from the installer in
`dist/lite/`. A failure in steps 2–9 blocks the release (L8 §12.4).

| # | Step | Expected | Result |
|---|---|---|---|
| 1 | Note the machine: Windows edition, RAM, CPU, screen size | — | |
| 2 | Copy `Mizan Lite <version> Setup.exe` from a USB drive and run it | SmartScreen warns (unsigned) → More info → Run anyway; the installer asks for administrator once | |
| 3 | Watch the WebView2 step | either "already installed" or it installs from the bundled copy **with no network** | |
| 4 | Finish; open Mizan Lite from the Start menu | the window opens in Arabic, right to left, Arabic letters joined — never a blank window | |
| 5 | Complete first run; write the recovery code down | the Till appears, the scan field has focus | |
| 6 | `powershell -ExecutionPolicy Bypass -File scripts\lite-smoke-windows.ps1` | data under `%LOCALAPPDATA%\Mizan Lite`, integrity ok, a backup taken, no errors in the log | |
| 7 | Open a second copy from the shortcut | the first window comes forward; no second window, no second database | |
| 8 | Sell: scan, F9, the receipt; print a test page, then a receipt; try both paths | paper comes out, Arabic reads correctly, the amounts match the screen | |
| 9 | Export a report to Excel onto the Desktop and onto a USB drive; open both in Excel | the sheet reads right to left, the figures are numbers, `=SUM()` over a money column works | |
| 10 | Backups → choose the USB drive as the outside folder → Back up now → pull the drive out → reopen Backups | the copy is made and verified; with the drive out, "the last outside copy is old" | |
| 11 | Restore the newest backup | the loss is stated, the PIN is asked, the application restarts on the restored data, and the safety snapshot is listed first | |
| 12 | Close and reopen | an `on_close` backup appears; nothing is lost | |
| 13 | `powershell -File scripts\lite-timing.ps1` | the PIN check, a month's report and a year of sales history — record the seconds (PROGRESS O6, O8) | |
| 14 | With the network back: `npm run build` then `LITE_E2E_CHANNEL=msedge npx playwright test -c e2e/playwright.config.ts` | the journeys pass in Edge — WebView2's own engine | |
| 15 | Install the next build over this one **while it is running** | the installer refuses until the application is closed; afterwards the data is intact | |
| 16 | Uninstall from Settings → Apps | the program goes; `%LOCALAPPDATA%\Mizan Lite` stays, and the uninstaller says so | |

**After the run:** paste the results into this table, note anything surprising in L8 §18, and close PROGRESS O1 and O3 — or say
plainly what failed.
