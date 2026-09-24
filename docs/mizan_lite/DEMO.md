# The furniture demo — الكردي

> **Reader:** the owner, showing Mizan Lite to a client. **Made:** 0.10.1, 2026-09-25 — decisions D-0101.5–8 in
> [DECISIONS.md](DECISIONS.md).

A shop to present Mizan Lite with: **الكردي — مفروشات وأدوات منزلية**, in Aleppo, selling in US dollars only from its first
day, with a month and a half of trade behind it. Everything in it was entered through the application's own services and
passes its own checks — the stock, sales and debt verifiers and every reconciliation of the reports — so it is a shop the
application could have produced, not rows written by hand.

## What is in it

| | |
|---|---|
| **The shop** | name, phone, city and address in Settings → Store information, and its logo at the head of the screen, every receipt and every A4 invoice |
| **Money** | US dollars only (Settings → How money is shown). No pound figure appears anywhere |
| **Catalogue** | 51 products — living room, bedroom, dining, office and children's furniture, kitchenware, decor, rugs and textiles, lighting, bath — each with its price, its cost and a barcode; household goods sold by the carton carry their carton size (the invoice's **طرد** column). 24 on the till's quick buttons |
| **Suppliers** | 5, with balances from the paper book, 8 purchase invoices on credit and in cash (supplier discounts, a few items broken on arrival and not charged) and 5 payments, from the drawer and from the owner's own money |
| **Customers** | 11 — a hotel, a restaurant, an office, a kindergarten, a charity, a newlywed paying a bedroom set off by the month, and walk-in regulars — with credit sales, repayments and balances from the paper book |
| **History** | 45 days of walk-in sales (some discounted by the owner, some paid with a larger note and given change), a void, a return, three items written off, rent, wages, electricity and delivery, and the drawer counted each evening |

**Owner PIN: 481537.** The PIN switch is off, as on a new installation: the owner's screens open without it, and the acts
that are always the owner's — a restore, bringing in a backup file — ask for it.

The receipt printer is set to *Demo XP-80*, a name no computer has, and nothing prints by itself: a presentation is never
interrupted by a printer that is not there. Choose a real printer on the Printer screen to print; the A4 invoice can always be
saved as a PDF.

## On the Mac

The installed application opens the demo. The pantry test shop that was there before (بقالية المونة) was **moved aside whole
and untouched** to `~/Library/Application Support/Mizan Lite (pantry test shop, 2026-09-25)`.

To go back to it: quit Mizan Lite, then

```bash
cd ~/Library/Application\ Support
mv "Mizan Lite" "Mizan Lite (Al-Kurdi demo)"
mv "Mizan Lite (pantry test shop, 2026-09-25)" "Mizan Lite"
```

and open the application. The same two lines the other way round bring the demo back.

## On a Windows till

The demo travels as one file, `al-kurdi-demo.db`, in the `Al-Kurdi demo` folder beside the installer — a verified Mizan Lite
backup.

1. Install `Mizan Lite 0.10.1 Setup.exe` and complete the first run with any name, a PIN of your own and today's rate — this
   shop is replaced in a moment.
2. **النسخ الاحتياطية (Backups) → استعادة من ملف… (Restore from a file…)**, the PIN of step 1, and choose `al-kurdi-demo.db`. It
   is checked and added to the list.
3. Choose it in the list → **استعادة (Restore)**, the PIN again. The application restarts as الكردي; from then on the PIN is
   **481537**.

The shop from step 1 is kept as the snapshot taken before the restore, in the same list.

Sales made during a presentation go into the demo's books. To start clean again, restore the file again.

## Making it again

```bash
MIZAN_LITE_DATA_DIR=~/Desktop/kurdi-demo go run ./cmd/lite-demoseed -profile home \
    -logo assets/brands/al-kurdi/al-kurdi-logo-800.png -export ~/Desktop/al-kurdi-demo.db
```

The folder must be new — the seeder refuses a shop that has been set up. Seeded before the shop opens (09:30), today is empty;
later in the day, today has a few sales and a repayment. Nothing is ever dated after the moment it ran.

## The brand kit

`assets/brands/al-kurdi/` — the SVG files are the masters, with the lettering drawn as outlines from IBM Plex Sans Arabic (the
face the application's documents use, under the SIL Open Font License), so they open the same in any editor and at a print
shop; the PNGs are rendered from them with a clear background.

| File | For |
|---|---|
| `al-kurdi-logo.png` · `.svg` | the logo — the emblem, the name and "مفروشات وأدوات منزلية" (2400 px) |
| `al-kurdi-logo-800.png` | the same at the 800 px the application keeps — what the demo was given |
| `al-kurdi-logo-bilingual.png` · `.svg` | with "AL-KURDI · FURNITURE & HOME" beneath, for signs and the web |
| `al-kurdi-logo-stacked.png` · `.svg` | the emblem above the name, for a square: a profile picture, a sticker |
| `al-kurdi-emblem.png` · `.svg` | the emblem alone: an armchair under a roof on a walnut tile |
| `al-kurdi-logo-mono.png` · `.svg` | one colour, black: stamps, engraving, a thermal printer that should print it solid |

The application's own logo is the Arabic lockup without the Latin line: at a receipt's size the Latin letters print as specks.
