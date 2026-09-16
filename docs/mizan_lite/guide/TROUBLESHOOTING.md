# Troubleshooting — Mizan Lite

---

## A blank window at startup (Windows)

The **WebView2** component is missing. Run `Setup.exe` again; it carries a copy and installs it with no internet.

## "The application is already running"

Mizan Lite opens one window only, so there is one set of books. If you cannot find the window, close the application in Task Manager and open it again.

## The owner PIN is forgotten

Open **Owner → Forgot the PIN**, and enter the **recovery code** written down on setup day. A new code is given — write it down.

**If the recovery code is lost too**: nobody can open what the PIN protects — not us, not anyone; that is what makes it protection. Selling and taking payments still work without it. The only way back is a new shop restored from a backup… which also needs the code. Write the code down and keep it.

## The printer does not print

1. **Printer → Print a test page**. If no paper comes out:
2. Check the printer is on, has paper, the cable is in, and it prints a test page from the operating system.
3. Check the printer chosen in the application is the one that is connected.
4. Try **Straight to the printer (ESC/POS)**; some printers do not print well through their driver.
5. A sale is always recorded even when the receipt does not print; open it from Sales and press **Print again**.

## The receipt is cut off or full of strange characters

The paper width in **Printer** must match the real paper (80 or 58 mm). Strange characters instead of Arabic mean the printer is printing text instead of an image: choose "Straight to the printer".

## The exchange rate is old or wrong

**Exchange rate** → write today's rate. Each sale keeps the rate it was sold at, so changing today's rate does not change yesterday's profit.

## The drawer count differs

The difference is recorded as it is. Look at the **cash book** for the day: was an expense recorded? Was money handed back on a void? The reports show the differences in detail.

## "The last outside copy is old"

The USB drive is unplugged, or its letter changed. Plug it back in, or **Backups → Change the folder…**. Local backups continue either way.

## The application is slow

- Close other programs; 4 GB is the minimum.
- A whole year's report takes seconds on an old computer; the day's report is immediate.
- If the slow moment is entering the owner PIN, that is deliberate: the check is meant to be a little slow, to protect the PIN.

## The computer went off during a sale

Open the application. Everything completed before it went off is saved; a sale that was never paid was never recorded. Count the drawer to be sure.

## Where is my data?

**About** shows the data folder and a **Show in folder** button. It holds the database, a `backups` folder and a `logs` folder.

## I need help

**About → Save a support file…**: a zip with the last week's logs and the application's state. Include a copy of the database only if you are asked to and you trust who receives it — it holds customers' names and debts.
