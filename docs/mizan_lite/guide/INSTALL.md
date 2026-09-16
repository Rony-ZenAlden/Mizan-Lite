# Installing Mizan Lite

For whoever installs the application in the shop. No step needs the internet.

---

## What is needed

| | Minimum |
|---|---|
| Windows | 10 (22H2) or 11, 64-bit |
| macOS | 13 or later |
| Memory | 4 GB |
| Screen | 1366×768 or larger |
| Printer | an 80 mm thermal receipt printer (optional) |

## Installing on Windows

1. Copy `Mizan Lite … Setup.exe` from a USB drive to the computer.
2. Run it. **"Windows protected your PC"** appears because the application is not signed: press **More info**, then **Run anyway**.
3. Choose the installer's language and continue. The installer asks for administrator rights once.
4. If the computer has no **WebView2**, the installer installs it from a copy inside itself — no internet. Do nothing; wait for it to finish.
5. When it finishes, a **Mizan Lite** shortcut is on the desktop and in the Start menu.

**The shop's data** is kept in `%LOCALAPPDATA%\Mizan Lite`, and uninstalling does not delete it.

## Installing on macOS

1. Open `Mizan Lite ….dmg` and drag **Mizan Lite** into **Applications**.
2. Open it the first time: the system refuses it because it is unsigned. Go to **System Settings → Privacy & Security**, scroll to the Mizan Lite message, press **Open Anyway**, then **Open**.
3. **The shop's data** is kept in `~/Library/Application Support/Mizan Lite`.

## First run

Follow "The first day" in the shop guide: the shop's name, the language, the owner PIN, the exchange rate — then write the recovery code on paper.

## Setting up the printer

1. Install the printer in the operating system first (USB cable and the maker's driver), and check it prints a test page from the system.
2. In the application: **Printer** → choose the printer and the paper width (usually 80 mm), then **Print a test page**.
3. If the page comes out distorted or cut off, change "How receipts reach the printer" to **Straight to the printer (ESC/POS)** and test again.
4. Type the shop's phone, address and thank-you line to appear on every receipt.

## The outside backup folder

**Backups → Choose a folder…**: pick a USB drive that stays plugged in, or a folder that is synced. Every backup is copied there and checked. Leave the drive in; if it is pulled out, the application says the last outside copy is old.

## Installing a newer version

Close the application first — the installer refuses to install over a running copy — then run the new installer over the old one. The data stays as it is, and a backup is taken before any database update.

## Moving the shop to another computer

1. On the old computer: **Backups → Back up now**, then **Save a copy…** to a USB drive.
2. On the new computer: install the application and complete first run with any details.
3. **Backups → Restore from a file…**: choose the file on the drive and restore it. The application restarts on the shop's data.

## Uninstalling

Windows: **Settings → Apps → Mizan Lite → Uninstall**. macOS: delete the application from Applications. In both cases **the shop's data stays** where it is; delete it by hand only if you are sure, and only after taking a copy.
