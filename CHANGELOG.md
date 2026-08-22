# Changelog

Notable changes to Mizan. Newest first.

This file records what changed and **why it was decided that way**. Where a decision has a
trade-off, the trade-off is stated rather than the outcome alone — a changelog that only lists
features is a changelog nobody can use to understand a regression.

---

## 1.0.0 — 2026-08-23

The first release. Mizan is an offline-first desktop ERP and point of sale for small and medium
businesses: Go and SQLite behind a React interface, packaged as a single installer for Windows and
macOS, in English and Arabic with full right-to-left support.

**It runs with no network, ever.** Not "works offline too" — there is no server, no account, and
no telemetry. The Windows installer carries the WebView2 runtime inside it, so a machine that has
never been online can install and run Mizan from a USB stick.

### What a shop can do

| Area | What ships |
|------|-----------|
| **Point of sale** | A till with held sales, shift open/close and cash reconciliation, PIN sign-in for counter staff, and receipt printing on thermal or A4 |
| **Sales** | Quotations, orders, invoices, credit notes, returns, payments and settlement, customer statements |
| **Purchasing** | Purchase orders, goods receipts with over-receipt tolerance, bills with a three-way match, price variance, landed costs, supplier returns and payments |
| **Inventory** | A stock ledger with weighted-average costing, transfers between warehouses, lots, serials and expiry, stock counts |
| **Catalogue** | Categories, products, variants with attribute generation, barcodes, packagings, units of measure with conversions, price lists |
| **Accounting** | A chart of accounts, journal entries, period close and year end, and posting rules that are **data rather than code** |
| **Money out** | Expenses, debts, partner balances, and a verifier that reconciles the subsidiary ledger to the control account |
| **Reporting** | Profit and loss, balance sheet, trial balance, sales and purchase analysis, stock valuation, global search, a dashboard |
| **Operations** | Automated backups, verified restore, CSV import and export, a notice centre, job diagnostics |

### Decisions worth knowing about

These are the ones most likely to surprise somebody reading the code or the books.

- **Money never becomes a float.** It crosses the JavaScript boundary as a string of minor units,
  and quantities are integers scaled by 10⁶. A number that cannot survive a round trip is a number
  that will eventually be wrong on an invoice.
- **Every posted document line snapshots its own price, discount, tax rate and cost.** Renaming a
  product, changing a price or superseding a tax rate cannot reach backwards into a document a
  customer is holding.
- **A tax rate is never updated — it is superseded from a date.** The old version's window closes
  and a new one opens, so every document already issued keeps resolving what it was charged at.
- **Discrepancies are reported, never repaired.** Where the stock valuation and the general ledger
  disagree, or a partner balance and its control account disagree, Mizan says so and stops. A job
  that silently corrected the difference would erase the evidence of what caused it.
- **Permission checks in the interface are cosmetic.** They hide what would refuse, so nobody
  learns to ignore errors. Every check that matters is on the Go side, where the guard is the only
  route to the object graph — a method that skips it has no database and no context.
- **The session token never crosses into the frontend.** It lives in the Go process, so there is
  nothing in the browser context for an injected script to steal.

### Data safety

Backups are automatic and need nobody to remember them:

- a **daily** snapshot, which runs immediately on a fresh install rather than a day later;
- a snapshot **when Mizan closes**, so a day's trading is never left with only the live database
  as its copy — skipped when one under an hour old already exists, so opening and closing the
  window repeatedly does not fill the disk;
- a snapshot **before every migration** and **before every restore**;
- **retention of seven per reason**, so a busy day's close-time snapshots can never evict the
  daily ones or the pre-upgrade one a support conversation is about.

Every snapshot is opened, integrity-checked and read back before it is listed. One that fails any
of that is deleted rather than left on disk — a file in the backup directory is a promise, and one
that cannot be opened is a promise discovered broken at the worst moment.

### Known limits

Stated plainly, because the alternative is somebody discovering them.

- **Both artefacts are unsigned.** macOS Gatekeeper will refuse the `.dmg` and Windows SmartScreen
  will warn, until a Developer ID and an Authenticode certificate are bought and applied. The
  hooks exist and are tested; `docs/RELEASE.md` §4 is the procedure. Proven the hard way: an
  ad-hoc signature verifies and Gatekeeper **still** rejects it, so signing is necessary and not
  sufficient — notarization is required.
- **The Windows installer has never been executed.** No Windows machine, VM or Wine runtime exists
  in the build environment. It is structurally correct and the WebView2 runtime is verifiably
  embedded, but that it *installs* can only be proven on a real machine. `docs/RELEASE.md` §3 is
  the checklist.
- **Nobody has traded on this.** Every guarantee is proven by a test, and tests prove what somebody
  thought to check.
- **"Out of stock" is not "low stock".** The alert counts levels at or below zero. A threshold per
  product needs a reorder point, which the schema does not carry — see
  `docs/architecture/KNOWN_GAPS.md` §3 for what closing it would cost.
- **No tax question in the setup wizard.** No country profile ships a tax profile to enable, so
  the question has no answer to offer, and a control that cannot do anything teaches people to
  distrust controls.

Two further gaps — a nullable `price_list_items.variant_id` and a Phase 4 seam that needed
reshaping — are recorded in full in `docs/architecture/KNOWN_GAPS.md`, with the reasoning rather
than just the verdict.

### Built on

35 migrations · 1,270 Go tests · 307 frontend tests · English and Arabic · Windows 10+ and
macOS (Universal, Apple Silicon and Intel).
