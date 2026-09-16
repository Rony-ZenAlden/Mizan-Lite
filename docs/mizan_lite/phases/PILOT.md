# The pilot week — Mizan Lite

> **Status: not started.** It begins when the release gate is green, the Windows protocol has passed on the shop's machine, and
> a receipt has printed on its printer (L8 §8, §12.4).

One shop, seven trading days, version 0.9.0. The point is not to find defects in a laboratory: it is to see whether a shop can
open, sell, take debts, count its drawer and close, on its own hardware, for a week, without losing money or time.

## Before the first day

- [ ] `make lite-release` green, and the manifest read.
- [ ] [WINDOWS_PROTOCOL.md](WINDOWS_PROTOCOL.md) passed on the shop's own computer.
- [ ] A receipt printed on the shop's printer; an export opened in Excel.
- [ ] The owner has read the shop guide; the counter card is printed and beside the till.
- [ ] The shop's products entered (typed, or imported from Excel) and the opening stock counted in.
- [ ] The paper debt book adopted: every customer and every balance.
- [ ] An outside backup folder chosen — a USB drive that stays in the machine.
- [ ] A way to reach whoever supports the shop, written on the counter card.

## Every day

1. Morning: open the application, set the day's rate.
2. Sell through the application, all day. **For the first three days keep the paper book as well**, and compare the two at
   closing (Q-L8.4).
3. Closing: count the drawer in each currency; write the difference down.
4. Check the outside copy is fresh (Home says so); leave the drive in.
5. One line in the table below.

| Day | Sales | Difference at the count | Anything odd |
|---|---|---|---|
| 1 | | | |
| 2 | | | |
| 3 | | | |
| 4 | | | |
| 5 | | | |
| 6 | | | |
| 7 | | | |

## If something goes wrong

- **Stop issues** — money recorded wrongly, data lost, the till unusable: go back to the paper book for the rest of the day,
  keep the backup, and send a support file (About → Save a support file). A fix release follows before the week continues.
- **Everything else** — a word that reads badly, a button in the wrong place, a slow screen: write it in the day's row and carry
  on. It is fixed after the week.

## Exiting the pilot

All of these, or the week repeats:

- [ ] Seven trading days.
- [ ] No stop issue open.
- [ ] Every day's verifiers clean (the seeder's checks run by the support file's diagnostics).
- [ ] Every drawer difference explained.
- [ ] The first three days' paper book and the application agree.
- [ ] The owner says the shop is better off with it than without it.

Then `lite-v1.0.0` is tagged from the pilot's commit plus its fixes, and the gate is run again.
