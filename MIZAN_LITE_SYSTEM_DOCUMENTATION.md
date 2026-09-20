# Mizan Lite — System Documentation

**Official architecture and functional reference.**

| | |
|---|---|
| **Release** | 0.9.6 (tag `lite-v0.9.6`, commit `eb4d1a6`) |
| **Schema version** | 9 |
| **Document revised** | 2026-09-20 |
| **Runtime** | Go 1.27.1 · Wails v2.13.0 · React 18.3 · SQLite |
| **Module** | `github.com/mizan-erp/mizan` — application in `apps/lite`, domain in `internal/lite` |
| **Targets** | macOS 13+ (universal `.dmg`) · Windows 10+ amd64 (NSIS installer + portable `.exe`) |
| **Status** | Pilot. Not yet 1.0.0 — see [§14 What is not verified](#14-what-is-not-verified-here) |

---

## Contents

1. [What Mizan Lite is](#1-what-mizan-lite-is)
2. [Architecture](#2-architecture)
3. [Data model](#3-data-model)
4. [The binding surface — every backend route](#4-the-binding-surface--every-backend-route)
5. [Money: the chokepoint pipeline](#5-money-the-chokepoint-pipeline)
6. [Redenomination and dual currency display](#6-redenomination-and-dual-currency-display)
7. [The FX engine](#7-the-fx-engine)
8. [Point of sale — the till](#8-point-of-sale--the-till)
9. [Catalogue and inventory](#9-catalogue-and-inventory)
10. [Customers, debt and the cash drawer](#10-customers-debt-and-the-cash-drawer)
11. [Reports, receipts and exports](#11-reports-receipts-and-exports)
12. [Security and configuration](#12-security-and-configuration)
13. [Build, packaging and CI](#13-build-packaging-and-ci)
14. [What is not verified here](#14-what-is-not-verified-here)
15. [Appendix A — architecture rules](#appendix-a--architecture-rules-enforced)
16. [Appendix B — file map](#appendix-b--file-map)

---

## 1. What Mizan Lite is

A **single-computer, fully offline point-of-sale and stock system for a Syrian pantry shop**. It is the small edition of
Mizan ERP: one shop, one computer, no server, no network account, no login at the counter.

Three facts shape every design decision in it:

1. **It is offline.** There is no server to authenticate against and no remote database. The only network traffic the
   application ever makes is the exchange-rate fetch (§7), and the application works completely without it.
2. **It is Arabic-first and right-to-left.** Arabic is the default; English is a full second language, not a fallback.
   Every figure in an Arabic sentence is bidi-isolated so it is not printed backwards (§12.5).
3. **It handles two currencies with an unstable one among them.** Prices may be set in US dollars or Syrian pounds;
   the pound has been redenominated by two noughts; the market rate moves daily and differs from the published one.

### 1.1 Functional scope

| Area | Included | Deliberately absent |
|---|---|---|
| Sales | Cash and credit sales, discounts, voids, rounding to the smallest note, reprints | Multi-till, shifts, cashier accounts |
| Catalogue | Products, barcodes, units, packages, prices in USD or SYP, cost price and margin | Categories, variants, serial numbers |
| Stock | Levels, deliveries, counts, adjustments, average cost, valuation, movement ledger | Multi-warehouse, transfers, purchase orders |
| Customers | Customer records, credit sales, repayments, statements, write-offs, refunds | Suppliers, payables, purchase ledger |
| Cash | A day's drawer per currency, expected vs counted, expenses, deposits, withdrawals | Bank accounts, reconciliation |
| Reports | Day (Z-report), month, arbitrary period, per-product, stock valuation | Consolidated multi-shop reporting |
| Output | 80 mm thermal receipts, A4 PDFs, Excel workbooks | Email, cloud sync, e-invoicing |

---

## 2. Architecture

### 2.1 Process model

```
┌──────────────────────────────── one OS process ────────────────────────────────┐
│                                                                                 │
│   ┌───────────────────────────┐        ┌──────────────────────────────────┐    │
│   │  WEBVIEW (WKWebView /     │        │  GO                              │    │
│   │  WebView2)                │        │                                  │    │
│   │                           │        │   apps/lite/main.go              │    │
│   │   React 18 + TypeScript   │◀──────▶│     └ internal/lite/api   ← bindings   │
│   │   Vite bundle, EMBEDDED   │  Wails │        └ internal/lite/bootstrap  │    │
│   │   in the binary           │  IPC   │           └ services (catalog,   │    │
│   │   HashRouter              │        │              stock, sales, fx,   │    │
│   │   Tailwind CSS            │        │              customers, cashbook,│    │
│   │                           │        │              reports, owner,     │    │
│   │   NO business arithmetic  │        │              backups, printing)  │    │
│   └───────────────────────────┘        │              └ domain (pure)     │    │
│                                        │              └ infra/sqlite      │    │
│                                        └──────────────────┬───────────────┘    │
└───────────────────────────────────────────────────────────┼────────────────────┘
                                                            ▼
                                        ┌──────────────────────────────────────┐
                                        │  SQLite  (WAL, foreign keys ON)      │
                                        │  ~/Library/Application Support/      │
                                        │      Mizan Lite/            (macOS)  │
                                        │  %LOCALAPPDATA%\Mizan Lite\ (Windows)│
                                        │    ├ mizan-lite.db                   │
                                        │    ├ backups/                        │
                                        │    ├ logs/mizan-lite.log             │
                                        │    └ exports/                        │
                                        └──────────────────────────────────────┘
```

There is no HTTP server in the shipped application. The frontend calls Go through Wails' generated bindings; the
`internal/lite/e2e` loopback bridge exists **only in test builds** and is held out of the shipped binary by an
architecture rule (`lite-e2e-test-only`).

### 2.2 Layering

Each feature module is four layers, and the dependency arrow only ever points inwards:

```
api (bindings, DTOs)  →  service (use cases, transactions)  →  domain (pure rules)
                              ↓
                       infra/sqlite (SQL)
```

- **`domain`** — pure Go. No database, no clock, no filesystem, no `net/http`. All arithmetic lives here.
- **`service`** — orchestrates the domain inside a transaction, talks to ports (interfaces) it declares itself.
  Every port has both a SQLite implementation and an in-memory fake, and **one contract suite runs against both**,
  so a fake cannot quietly behave differently from the database it stands in for.
- **`infra/sqlite`** — SQL only.
- **`api`** — the Wails-facing layer. Converts domain values to DTOs of **strings**, and wraps everything in an envelope.

These boundaries are not conventions — they are compiled rules checked by `archlint` on every build (Appendix A).

### 2.3 The wire contract

Every binding returns the same envelope:

```jsonc
{ "ok": true,  "data": { /* DTO */ } }
{ "ok": false, "error": {
    "code":       "lite.fx.large_change",   // stable, doubles as an i18n key
    "messageKey": "lite.fx.large_change",
    "params":     { "from": "15000", "to": "19000" },
    "fields":     [ { "field": "rate", "code": "...", "message": "..." } ]
} }
```

The frontend **never** builds an error message. It looks the code up in the locale catalogue, which is held complete by
a gate test: any error code a screen can receive that is missing a translation in either language fails the build.

### 2.4 DESIGN D9 — money crosses the boundary as text

**No monetary or quantity value is ever a JavaScript `number`.** Go formats every figure into an exact decimal string
before it crosses the bridge, and parses every typed figure back on the Go side.

| Concept | Stored as | Crosses as |
|---|---|---|
| Money | `int64` minor units, or micros (10⁻⁶) for prices and costs | `"15000"`, `"3.25"` |
| Quantity | `int64` micros (10⁻⁶ of a unit) | `"2.500"` |
| Exchange rate | `int64` nano (10⁻⁹) | `"15000"`, `"136.75"` |
| Percentage | `int64` micros of a percentage point | `"25"`, `"12.5"` |

The frontend's only numeric operation is **grouping for display** (via `BigInt`, never `Number`) and adding **one** to a
whole count in the cart. Arithmetic that could overflow `int64` — margins on old-pound prices in the millions, rate
adjustments — is done in `math/big`. An `archlint` rule named `no-float` forbids `float32`/`float64` anywhere in
`internal/lite` outside the two packages that measure pages in points for a rasteriser.

---

## 3. Data model

### 3.1 Migrations

Migrations are forward-only, numbered, and each is applied in a transaction. A **backup is taken automatically before
any migration runs**. Every past schema is proved to upgrade to the current release, with every row intact, as a release
gate (§13.3).

| # | File | Introduces |
|---|---|---|
| 0001 | `0001_lite_platform.sql` | `settings`, `schema_migrations`, `schema_lock`, `jobs`, `job_runs` |
| 0002 | `0002_catalogue_owner.sql` | `products`, `uoms`, `currencies`, `owner_credentials`, `owner_events` |
| 0003 | `0003_stock.sql` | `stock_levels`, `stock_ledger`, `product_packages` |
| 0004 | `0004_fx.sql` | `fx_fetches`, `fx_rates` |
| 0005 | `0005_till.sql` | `sales`, `sale_lines`; rebuilds `stock_ledger` and `stock_levels` to carry a sale reference |
| 0006 | `0006_customers.sql` | `customers`, `debt_entries` |
| 0007 | `0007_cashbook.sql` | `cash_entries` |
| 0008 | `0008_printing.sql` | `print_jobs`, `voucher_numbers` |
| 0009 | `0009_product_cost.sql` | `products.cost_micro`, `products.has_cost` |

### 3.2 Tables

| Table | Holds |
|---|---|
| `settings` | Key/value. Every setting in §12.4 |
| `products` | Name (AR/EN), barcode, unit, price currency and price micros, cost micros, quick slot, active flag, row version |
| `product_packages` | A product sold both loose and in a pack, with the pack's factor |
| `uoms`, `currencies` | Reference data: unit input decimals, currency decimals |
| `stock_levels` | On-hand micros and average cost per product |
| `stock_ledger` | Every movement: delivery, sale, count, adjustment, reversal — append-only |
| `sales`, `sale_lines` | Each sale with its rate, discount, rounding, tender, change; each line with its snapshotted cost |
| `customers`, `debt_entries` | Customer records, and the append-only debt ledger (charge, payment, write-off, refund, reversal) |
| `voucher_numbers` | Per-series counters for receipt and voucher numbering |
| `cash_entries` | Drawer movements: deposits, withdrawals, expenses, counts |
| `fx_rates` | Every rate ever in force, with source and business date |
| `fx_fetches` | Every fetch attempt, its provider, outcome and error code — successful or not |
| `owner_credentials` | Argon2id PIN hash, recovery hash, failed-attempt count, lockout time |
| `owner_events` | The owner's history: every guarded act, PIN change, elevation. Never holds a PIN or a hash |
| `print_jobs` | What was printed, when, to which printer |
| `jobs`, `job_runs` | The scheduler's record: what ran, when, and what it returned |
| `schema_migrations`, `schema_lock` | Migration bookkeeping and the single-writer lock |

`stock_levels_copy` and `stock_ledger_new` appear in migration 0005 as rebuild scaffolding and do not exist in the
finished schema.

### 3.3 Invariants the schema enforces

- **Money is never a float.** Every amount column is `INTEGER`, with a `CHECK` on currency decimals (0–4).
- **Ledgers are append-only.** A mistake is corrected by a reversing entry, never by an update or a delete. Both the
  stock ledger and the debt ledger work this way, so history always reconstructs.
- **Business date, not wall clock.** A sale belongs to a business day (`internal/lite/bizdate`) resolved in the shop's
  own timezone, so a sale at 00:30 lands where the shopkeeper expects it.
- **Row versions.** Every editable row carries one; a stale write is refused with a conflict, not silently applied.

---

## 4. The binding surface — every backend route

Fifteen binding objects, 99 methods. These are the complete set of calls the frontend can make. All return
`envelope.Result[T]`.

### `App` — lifecycle
| Method | Purpose |
|---|---|
| `BootStatus()` | Whether the database opened, migrated and is usable |
| `FirstRunStatus()` | Whether the shop has been set up |
| `CompleteFirstRun(FirstRunInput)` | Shop name, language, PIN (twice), first rate |
| `Health()` | Proof the webview reached Go; written to the log |
| `About()` | Version, commit, schema, data directory, integrity check |
| `SaveGuide(name)` | Writes a shipped PDF guide to disk |
| `SaveSupportFile(includeDatabase)` | Diagnostic bundle. Including the database is a guarded act |

### `Settings`
| Method | Purpose |
|---|---|
| `Get()` | Every resolved setting |
| `Update(SettingsInput)` | Partial change: locale, shop name, money display, PIN switch, rate source, local endpoint |

### `Owner` — the PIN
| Method | Purpose |
|---|---|
| `Status()` | Set up? locked for how long? elevated for how long? |
| `Elevate(PINInput)` | Enter owner mode (2 minutes, in memory only) |
| `EndElevation()` | Leave owner mode now |
| `ChangePIN(ChangePINInput)` | Takes the current PIN itself |
| `Recover(RecoverInput)` | Recovery code → new PIN |
| `Events(limit)` | The owner's history, newest first |

### `Catalog`
| Method | Purpose |
|---|---|
| `Products(ProductQueryDTO)` · `Product(id)` | List and read |
| `CreateProduct` · `UpdateProduct` | Create and edit |
| `SetPrice(SetPriceInput)` | Price, cost price, or a price computed from a margin — **guarded** |
| `SetActive` | Deactivate/reactivate — **guarded** |
| `SetQuickSlot` | Pin to the till grid |
| `SetPackage` · `ClearPackage` | The pack a loose product also sells in |
| `Units()` · `Currencies()` | Reference data |
| `ImportTemplate()` · `ImportPreview()` · `ImportApply(...)` | Excel import: template out, dry-run, apply — **guarded** |

### `Stock`
| Method | Purpose |
|---|---|
| `Levels()` · `Movements(query)` | On-hand and the ledger |
| `Valuation()` | Stock value at average cost — **owner-gated read** |
| `Receive(ReceiveInput)` · `Opening(ReceiveInput)` | A delivery, and the first-ever quantity |
| `Count(CountInput)` | A stocktake line. Lowering stock is **guarded** |
| `Adjust(AdjustInput)` | A write-off. Lowering is **guarded** |
| `CorrectCost(CorrectCostInput)` | Fix an average cost — **guarded** |
| `ReverseReceipt(...)` | Undo a delivery — **guarded** |
| `OpenPackage(...)` | Break a pack into loose units |
| `Verify()` | Re-derives every level from the ledger and reports disagreements |

### `Till` — the counter
| Method | Purpose |
|---|---|
| `Scan(code)` | Barcode → product, with its on-hand quantity |
| `Quote(CartInput)` | Prices a cart and returns a **token** |
| `Checkout(CheckoutInput)` | Records the sale, if the token still matches |
| `CashNote()` · `SetCashNote(note)` | The smallest note totals round to — **guarded** |

### `Sales`
| Method | Purpose |
|---|---|
| `List(businessDate)` · `Receipt(saleID)` | A day's sales; one receipt |
| `Void(VoidInput)` | Reverses a sale and states the cash to hand back — **guarded** |
| `Verify()` | Re-prices every recorded sale and reports any that no longer add up |

### `Customers`
| Method | Purpose |
|---|---|
| `Search(query)` · `Create` · `Update` · `SetActive` | The customer record |
| `Statement(query)` | The debt ledger for one customer |
| `Outstanding()` | Everyone who owes |
| `QuotePayment(PaymentInput)` · `RecordPayment(PaymentInput)` | A repayment, quoted then recorded |
| `Opening(DebtAmountInput)` | A debt the shop is carrying in — **guarded** |
| `WriteOff(DebtAmountInput)` · `Refund(RefundInput)` · `Reverse(...)` | **Guarded** |

### `Cash` — the drawer
| Method | Purpose |
|---|---|
| `Drawer(date)` | A day's drawer per currency: expected, counted, difference, entries |
| `Record(CashRecordInput)` | Deposit, withdrawal, expense — **guarded** |
| `Count(CashCountInput)` | What is actually in the drawer |
| `Reverse(ReverseCashInput)` | **Guarded** |

### `FX`
| Method | Purpose |
|---|---|
| `Current()` | Rate in force, its age, whether it is stale, the mode |
| `History(limit)` | Every rate and every fetch attempt |
| `SetRate(SetRateInput)` | A rate typed by hand — **guarded** |
| `SetMode(mode)` | `automatic` / `manual` — **guarded** |
| `SetAdjustPercent(percent)` | The shop's margin on the internet's rate — **guarded** |
| `Refresh()` · `FetchQuote()` | Fetch now |
| `AcceptProposal(fetchID)` | Accept a fetch the 20% guard held back — **guarded** |

### `Reports`
| Method | Purpose |
|---|---|
| `Day(date)` | The Z-report (§11.3) — **owner-gated read** |
| `Month(month)` · `Period(RangeInput)` | The same shape over a longer span |
| `Products(RangeInput)` | Per-product revenue, cost, profit, margin |
| `Stock(RangeInput)` | Valuation and movement summary |

### `Export`
| Method | Purpose |
|---|---|
| `Report(ExportReportInput)` | `day` · `month` · `products` · `stock` · `drawer`, as `xlsx` or `pdf` |
| `Statement(ExportStatementInput)` | One customer's statement |
| `DebtLedger(ExportRangeInput)` · `SalesHistory(ExportRangeInput)` | Ledger and sales history |
| `ShowInFolder(path)` | Reveals the written file |

### `Print`
| Method | Purpose |
|---|---|
| `Sale(saleID)` · `Entry(entryID)` | A receipt, or a debt voucher |
| `Preview(PreviewInput)` | The rendered bitmap, without printing |

### `Printers`
| Method | Purpose |
|---|---|
| `List()` | The printers the OS knows |
| `Settings()` · `Save(PrinterSettingsInput)` | Printer, paper width, path, header, footer — **guarded** |
| `Test()` | A test page |

### `Backups`
| Method | Purpose |
|---|---|
| `List()` · `Status()` | Backups held; whether the outside copy is stale |
| `TakeNow()` | A manual backup |
| `SetBackupEvery(every)` | `daily` · `weekly` · `monthly` · `manual` — **guarded** |
| `SetOutsideFolder(stop)` | A USB or synced folder — **guarded** |
| `SaveCopy(name)` | **Guarded** |
| `LossPreview(name)` | What restoring this backup would discard |
| `Restore(name)` · `RestoreFromFile()` | **Always require the PIN** (§12.2) |

---

## 5. Money: the chokepoint pipeline

### 5.1 The problem it solves

Before this pipeline existed, the application formatted money in **eight** places and parsed it in **twenty-one**. Adding
a redenomination would have meant editing about ninety call sites and hoping. One missed site shows a shop `13,675`
where it means `136.75` — not a cosmetic fault but a wrong price on a customer's receipt.

### 5.2 The design

Every money figure passes through **one package**, `internal/lite/moneyfmt`, at exactly the moment integers become text:

```
     BOOKS                    CHOKEPOINT                      SCREEN / PAPER
  int64 minor  ──▶ numinput.FormatFixed ──▶ Shop.Display() ──▶  "150 (15000)"
                                                                      │
  int64 minor  ◀── domain parse         ◀── Shop.Base()     ◀──  what is typed
```

```go
type Shop struct {
    Mode  Display  // legacy | new | dual
    Local string   // the currency code the redenomination applies to
}

func (s Shop) Display(text, currency string) string  // books  → person
func (s Shop) Base(typed, currency string) string    // person → books
```

The single Go-side chokepoint is `tillView.money` in `internal/lite/api/till.go`:

```go
func (v tillView) money(minor int64, code string) string {
    d := v.ref.Currencies[code].Decimals
    return v.shop.Display(numinput.FormatFixed(minor, d, d), code)
}
```

Because **documents are built from the very DTOs the screen receives** (D-L7.6), receipts, A4 pages, Excel cells,
the drawer and every report move together with the screens. There is no second formatting path to forget.

### 5.3 Why decimal text, not integer arithmetic

Every formatter already produces exact decimal text, because of D9. Dividing that by a hundred is **moving the decimal
point two places** — exact, needing no rounding rule, unable to lose a unit, unable to overflow. Converting back to
integers and dividing would introduce a rounding question the shop's books never asked.

### 5.4 The gate that holds it

`TestNoLocalMoneyFieldEscapesThePipeline` is a two-sided proof. It reads one seeded shop twice — once in the old pound,
once in the new — flattens **every field of every binding's JSON** to dotted paths, and asserts:

1. every figure that moved, moved by **exactly two noughts** (checked against an independent implementation); and
2. the set of non-zero figures that did **not** move equals a frozen, hand-audited 55-entry list.

A new field that carries local money and skips the pipeline fails this test by name. The gate caught three real leaks in
its own author's work: nine exchange-rate fields still reading 15,000 beside prices reading 150; the stock valuation's
local total and per-line value; and `FX.Current` returning an unconverted rate.

---

## 6. Redenomination and dual currency display

Syria removed two noughts from the pound. A shop may want to read the new figure, the old one, or both — and whichever
it picks must be true on the till, the shelf label, the receipt, the reports and the workbooks **at once**.

### 6.1 The three modes

Set under **Settings → Currency → How money is shown** (`currency.display`).

| Mode | Key | A stored `15000` reads as | A person types |
|---|---|---|---|
| **Legacy** (default) | `legacy` | `15,000 ل.س` | the old pound |
| **New** | `new` | `150 ل.س` | the **new** pound |
| **Dual** | `dual` | `150 ل.س (15,000)` | the **new** pound |

Three properties hold in all three modes:

- **The books never move.** `SELECT total_minor FROM sales` returns the same integer whichever mode is set. The mode
  changes presentation and input interpretation only. A test reads the raw table in each mode to prove it.
- **Dollars are never redenominated.** $3.25 is $3.25 whichever pound the shop reads. Only the configured local currency
  shifts, which is why every entry point takes a currency code.
- **What is typed comes back unchanged.** Typed → stored → displayed is the identity, in every mode. This is the property
  that matters most: a shop that types 150 and later reads 1.50 has been lied to about its own money.

### 6.2 The dual reading format

A dual reading is the new figure with the old one after it in brackets:

```
moneyfmt.Display("15000", "SYP")  →  "150 (15000)"
```

Rendered, that becomes `150 ل.س (15,000)` where a component knows it carries two figures, and
`150 (15,000) ل.س` where it does not. Both are correct readings.

> **Design note — why brackets and not a separator character.**
> The first implementation divided the two figures with an ASCII unit separator (`U+001F`), on the reasoning that it
> could never be part of a figure. That was true and useless. Every place that printed money without knowing to split
> it drew a control character: the webview showed a broken box — `1 USD = 137 ☒ 13700 SYP` — and the thermal printer
> **dropped the character entirely**, fusing `975` and `97500` into `97597500` on a customer's receipt.
>
> A separator that must be understood to be readable *will* be misunderstood somewhere, because there are more places
> that draw money than places that know about `moneyfmt`. Brackets need nobody's cooperation: the worst a naïve site
> can do with them is show the reading a person wanted anyway. The shape is unambiguous because a figure this
> application produces is digits, at most one point and at most one leading minus — never a bracket.

Four consumers implement the shape, and a gate holds them together:

| Consumer | Behaviour |
|---|---|
| `moneyfmt.DualOpen`/`DualClose`, `SplitDual()` | The definition, and the way back apart |
| `documents.Group()` (receipts, A4) | Groups each half: `150 (15,000)` |
| `numbers.ts` `formatDecimal`/`splitDual` | Groups each half in the webview's locale |
| `<Money>` component | Draws the old figure small and dimmed beside the new one |

**The gate:** `TestADualReceiptCarriesNothingAPrinterCannotDraw` builds a real receipt in dual mode and fails on any
character a printer cannot draw, bidi isolates excepted. Run against the old separator, it fails and prints `97597500`.

### 6.3 Input

In New and Dual mode a person types the **new** figure. Dual shows the old figure for recognition, not for typing — a
shop allowed to type either would have no way to say which it meant. `Shop.Base()` shifts the typed figure back up by
two noughts before the domain parses it.

---

## 7. The FX engine

One rate, USD → local currency, stored in **nano** (10⁻⁹) as `int64`. At 15,000 that is 1.5 × 10¹³; after the
redenomination, 1.5 × 10¹¹. Both are far inside `int64`.

### 7.1 Sources, in the order they are tried

The shop chooses a **source** under Settings → Exchange rate:

**`standard`** — the published providers, tried in order:

| Order | Provider | Endpoint | Scale |
|---|---|---|---|
| 1 | `currency-api-jsdelivr` | `cdn.jsdelivr.net/npm/@fawazahmed0/currency-api@latest/v1/currencies/usd.json` | ×1 |
| 2 | `currency-api-pages` | `latest.currency-api.pages.dev/v1/currencies/usd.json` | ×1 |
| 3 | `exchangerate-api-open` | `open.er-api.com/v6/latest/USD` | **×100** |

> The scale is explicit and deliberate. Checked 2026-09-14: the first reports 13,007.54 (the old pound) while the third
> reports 121.96 — *the redenominated pound under the same currency code*. Left to inference, one of them silently
> prices the shop a hundredfold wrong.

**`local`** — the Damascus market, then the standard providers behind it:

| Provider | Endpoint | Notes |
|---|---|---|
| `sp-today-damascus` | `https://sp-today.com/` | **Built in and pre-configured.** Scrapes the Damascus USD buy/sell pair out of the page's embedded JSON. No configuration required |
| *(the shop's own)* | any HTTPS JSON endpoint | Optional. A dotted path (`usd.sell`, default `rate`) names the number inside the response |
| `standard` providers | as above | Automatic fallback when the market source cannot be reached |

The local endpoint's URL is validated when it is saved — where the shop can be told about it — rather than at fetch
time, where the only sign would be a rate that never arrives.

### 7.2 Margin and manual override

- **Percentage offset** (`fx.adjust_percent`) — a signed margin the shop puts on the internet's figure:
  `nano × (1 + percent/100)`, rounded half away from zero, never below 1. It applies **only in automatic mode** and
  **never to a rate typed by hand**. The arithmetic is `math/big`: a nano rate on an old-pound figure is already in the
  tens of trillions, and multiplying by 10⁸ before dividing overflows `int64` and returns a plausible wrong number.
  The fetch row keeps the **published** figure, so the log always says what the provider actually answered; the margin
  and the effective rate are derived when the row is read.
- **Manual override** (`fx.mode = manual`) — nothing is fetched at all. The owner's rate stands until they change it.

### 7.3 What a fetch decides

`Refresh()` runs on a timer (`RefreshInterval` = 1 minute) **outside any transaction**, so it holds no lock. Each
attempt is recorded in `fx_fetches` whether it succeeded or not, with its provider and error code. The decision:

| Condition | Outcome | Effect |
|---|---|---|
| Mode is `manual` | `held_mode` | Nothing applied |
| No rate has ever been recorded | `proposed` | The first rate is always a person's |
| A manual rate was set **today** | `held_today` | The owner's rate holds for the rest of its day |
| The change exceeds **20%** | `proposed` | Waits for the owner to accept |
| Identical to today's rate | `unchanged` | Nothing written |
| Otherwise | `applied` | Becomes the rate in force |

A proposal is accepted with `FX.AcceptProposal(fetchID)` — a guarded act. A proposal made stale by a newer rate is
refused rather than applied late.

### 7.4 Rate rules

| Rule | Value |
|---|---|
| Maximum decimals | 4. Also what refuses a rate typed in the wrong direction — 0.0000667 dollars per pound has seven |
| Large-change guard | 20% |
| Stale | The rate's business date is not today. Shown in the header in red |
| History kept | 500 rates and fetches |
| Provider timeout | 10 s, response capped at 1 MB |

`internal/lite/fx/infra/httpsource` is the **only** package in `internal/lite` permitted to import `net/http`, enforced
by the `lite-network-only-in-httpsource` rule.

---

## 8. Point of sale — the till

### 8.1 Quote and checkout

The till is a two-phase transaction, and the phases are bound by a **token**:

```
  Till.Quote(cart)  →  { every figure the cashier sees, token: "…" }
                          │
                          │  the cashier reads the total aloud, takes the money
                          ▼
  Till.Checkout(cart, token)  →  the sale, or a conflict
```

The token fingerprints **everything that decides what the customer is charged**: the exchange rate, and each line's
product at its price and row version. If a price or the rate changes between the quote and the click, checkout is
refused rather than recording a different total from the one the cashier said out loud.

### 8.2 The cart

- **Auto-aggregation.** Scanning a **counted** product already in the cart, on a line with no discount typed, raises
  that line's quantity by one. A **weighed** product always takes a new line — two weighings are two lines, as on a
  scale's ticket. The `+1` is the only number the frontend computes, in `BigInt`, and only for a whole count.
- **Per-line discount**, and a **whole-sale discount** — a discount is a **guarded act**.
- **Rounding** to the smallest note in circulation (`currency.local_cash_note`), applied to the local total only, and
  shown as its own line on the receipt.

### 8.3 Barcode and search

`Till.Scan(code)` resolves a barcode against the catalogue and returns the product **with its on-hand quantity**, so the
cashier sees immediately if they are selling something the books say is not there. A code that matches nothing falls
through to a name search in both languages.

### 8.4 Keyboard

The counter is a keyboard-first screen; the cashier's hands need not leave it.

| Key | Action |
|---|---|
| *(type / scan)* | Scan field is focused on load and refocused after every action |
| `Enter` on an empty scan field | Pay |
| `F9` | Quick pay — the whole sale, cash, in the local currency, no change |
| `F4` | Switch between cash and credit |
| `F2` | Jump to the last line's quantity and select it |
| `+` / `−` on an empty scan field | Raise/lower the last counted line by one |
| `Esc` | Clear the search results |

Multi-currency tender, credit terms and change are **collapsed by default** and only appear when chosen — and closing
them clears them, so nothing hidden can price a sale.

### 8.5 Payment

| Kind | Behaviour |
|---|---|
| Cash | Tendered in USD or the local currency; change computed at the rate in force and rounded to the smallest note |
| Credit | Charged to a customer in the default debt currency; the balance before and after both print |
| Mixed | Part tendered, remainder to credit |

Voiding a sale reverses its stock and its debt entries and **states the cash to hand back before it is confirmed**.

---

## 9. Catalogue and inventory

### 9.1 Products

| Field | Notes |
|---|---|
| Name (AR / EN) | Arabic required |
| Barcode | Optional, unique |
| Unit | From `uoms`, with its own **input decimals** — a counted unit takes 0, a weighed one 3 |
| Price | In **USD or the local currency**, held to that currency's decimals |
| Cost price | In the product's **own** selling currency (see below) |
| Quick slot | Pins the product to the front of the till grid |
| Package | A pack the loose product also sells in, with its factor |

**Cost price and margin.** The cost is typed in the product's own selling currency: a product priced in pounds whose
cost is in dollars would need a rate to compare them, and a moving rate would make the margin move with it.

The margin is **computed, never stored** — storing it would be a third number free to disagree with the two it came
from. It is a percentage **of the cost**, which is how a shopkeeper says it ("I buy at 2 and sell at 2.50" is 25%).
The form works in both directions: cost + margin % → price, and cost + margin money → price. A margin that would take
the price below nothing is refused; **selling below cost is shown, not refused** — a shop doing it needs to see it.

### 9.2 Stock

| Movement | Effect |
|---|---|
| Opening | The first-ever quantity, with its cost |
| Delivery (`Receive`) | Raises on-hand and recomputes the weighted average cost |
| Sale | Lowers on-hand; **snapshots the cost onto the sale line** |
| Count | A stocktake. Lowering stock is a guarded act |
| Adjustment | A write-off. Lowering is guarded |
| Reversal | Undoes a delivery |
| Open package | Converts a pack into loose units |

Cost is **snapshotted at checkout** (D-L9.1): a sale already made keeps the cost it recorded, so last month's profit does
not change because this month's delivery cost more.

`Stock.Verify()` re-derives every level from the ledger and reports any disagreement — the ledger is the truth, the
level is a cache of it.

### 9.3 Excel import

`ImportTemplate()` → the shop fills it in → `ImportPreview()` shows exactly what would change, row by row, with errors
named per cell → `ImportApply()` writes it, as a guarded act. Nothing is written until the preview has been seen.

---

## 10. Customers, debt and the cash drawer

### 10.1 The debt ledger

`debt_entries` is **append-only**. Every entry is one of: charge (a credit sale), payment, opening balance, write-off,
refund, or a reversal of any of those. A balance is the sum of the ledger, never a stored field that could drift.

- Debt is carried in the **default debt currency** (`debt.default_currency`) — USD or the local currency.
- A repayment is **quoted then recorded**, so the customer is told the figure before it is written.
- Every repayment and every write-off produces a numbered **voucher**, printable.
- `Statement(customer)` is the full ledger; `Outstanding()` is everyone who owes.
- Write-off, refund, opening balance, reversal and deactivating a customer who owes are all **guarded acts**.

### 10.2 The cash drawer

A day's drawer, **per currency**:

| Figure | Meaning |
|---|---|
| Expected | Opening + takings + deposits − withdrawals − expenses |
| Counted | What the cashier actually counted |
| Difference | Over or short |

The counting screen says plainly, in Arabic, what to type:
> «أدخل المبلغ الفعلي الموجود حالياً في الدرج لمطابقته مع المبيعات واكتشاف أي زيادة أو نقص في الصندوق.»

Day navigation has previous/next arrows and a **"اليوم"** (Today) button.

`OwnerView` is a flag on the drawer DTO: at the counter, expenses are shown together with withdrawals as *money taken*,
without the categories and figures an owner may not want a helper reading. With the PIN switch on (§12), the owner view
requires owner mode.

---

## 11. Reports, receipts and exports

### 11.1 One document model, three renderers

```
   DTO (the same one the screen reads)
        │
        ▼
   documents.Document  ── titles, pairs, tables, paragraphs, rules, stamps
        │
        ├──▶  A4 PDF          (internal/lite/documents — its own PDF writer)
        ├──▶  1-bit bitmap    ──▶ ESC/POS bytes or the printer's driver
        └──▶  sheets.Workbook ──▶ .xlsx
```

`documents` is **pure**: no database, no clock, no operating system, no module imports. Its figures are strings Go
already formatted for the screen. Because a printout is built from the DTO the screen received, **a receipt and a screen
cannot disagree** (D-L7.6).

### 11.2 Thermal receipts

- **80 mm** (configurable paper width), rendered as a 1-bit bitmap so Arabic shapes and ligatures are correct regardless
  of the printer's own font support.
- Two delivery paths: **through the printer's driver** (the default) or **straight to the printer as raw ESC/POS**
  through the print queue. CUPS via `os/exec` on macOS, the spooler and GDI via `syscall` on Windows.
- `internal/lite/printers` is the **only** package permitted to call the operating system, enforced by
  `lite-os-calls-only-in-printers`.
- The receipt carries the shop's header and footer, the sale's lines, discount, rounding, total, tender, change or the
  debt added and the balance after, and the exchange rate line.
- Every print is recorded in `print_jobs`.

### 11.3 The day's report (Z-report)

`Reports.Day(date)` — an owner-gated read:

| Group | Figures |
|---|---|
| Profit | Sales count; revenue, cost, profit, margin — **in both USD and the local currency** |
| Discounts | Given, in both currencies |
| Rounding | The local rounding total |
| Losses | Stock written off |
| Bad debts | Written off, converted at the day's rate |
| Expenses | From the cash book, by category |
| Takings | Per currency |
| Net | USD and local |
| Unknown | Lines whose cost was never recorded — counted and totalled, never hidden |
| Rate | The rate losses and bad debts were converted at |

`Month(month)` and `Period(from, to)` return the same shape over a longer span. `Products(range)` is the same profit
breakdown per product; `Stock(range)` is valuation and movement.

**Unknown lines are reported, not suppressed.** A shop with products that never had a cost recorded is told how many
and how much, rather than being shown a profit figure that quietly excludes them.

### 11.4 Exports

| Export | Formats |
|---|---|
| `day`, `month`, `products`, `stock`, `drawer` | `.xlsx`, `.pdf` |
| Customer statement | `.xlsx`, `.pdf` |
| Debt ledger (range) | `.xlsx`, `.pdf` |
| Sales history (range) | `.xlsx`, `.pdf` |

Workbooks are written by `internal/lite/sheets` — a minimal, dependency-free `.xlsx` writer. Numbers are written as
**numbers** with an explicit format, so Excel sums them; text stays text. A4 PDFs carry the shop's name, address and
telephone in the page header. `ShowInFolder(path)` reveals the written file in Finder/Explorer.

> **Known limitation.** In **Dual** display mode, a money cell in a workbook is written as the combined text
> `150 (15,000)` in a text cell rather than as two numeric columns. Excel will not sum such a column. The PDF and the
> receipt render dual correctly. Shops that export to Excel for arithmetic should use Legacy or New mode.

### 11.5 Backups

| Trigger | Reason recorded |
|---|---|
| The shop asks | `on_demand` |
| The scheduler | `scheduled` — daily (default), weekly, monthly, or manual only |
| Before any migration | `before_migration` |
| Before any restore | `before_restore` |
| On close | `on_close` |
| Brought in from a file | `imported` |

The three automatic ones — on close, before a migration, before a restore — **ignore the shop's frequency setting**.
They are the ones that save a shop from what is about to happen.

An **outside folder** (a USB drive or a synced folder) receives a copy of every backup; the application warns on every
screen if the last outside copy is more than **48 hours** old. `LossPreview(name)` states what restoring a given backup
would discard before it is restored. A restore records itself in the owner's history of the database it restored into —
the restore removed the history that guarded it, so it is written again at first start.

---

## 12. Security and configuration

### 12.1 The model

There is **no login and no JWT**. A single-computer offline application has no server to authenticate against, and the
counter must never be locked out mid-sale. What exists instead:

| Mechanism | Detail |
|---|---|
| Owner PIN | **6–12 digits**, hashed with **Argon2id**, set once at first run |
| Recovery code | Generated once at first run, shown once, hashed. Recovers a forgotten PIN |
| Lockout | Failed attempts are counted and committed **even though the attempt fails** — a refusal returned from inside the transaction would roll the counter back and lockout would never engage |
| Owner mode | **2 minutes** (`domain.ElevationWindow`), counted from entry, held **in memory only** — never a token in JavaScript, never a row in the database. A restart ends it |
| Owner history | `owner_events`. Every guarded act, whether the gate stopped it or not |

### 12.2 The master PIN switch

**Settings → Security → "PIN verification required for sensitive actions"** (`security.pin_required`, default **off**).

| Switch | Guarded acts (voids, discounts, price changes, stock corrections, cash book, settings) | Guarded reads (profit, cost, stock value, owner drawer view) | `backups.restore`, `backups.import` |
|---|---|---|---|
| **Off** (default) | Allowed, **recorded** | Open | **PIN required** |
| **On** | **PIN required** | **PIN required** | **PIN required** |

Four properties:

1. **Every act is recorded either way.** The owner's history is what survives the gate, and is what an owner actually
   reads afterwards.
2. **The switch is guarded *before* it is written.** The guard judges the setting *in force*, not the one being set.
   Turning the PIN **on is free**; turning it **off costs the PIN**. Guarded afterwards, the protection would have let
   itself be switched off — the one direction that must not be free.
3. **A switch that cannot be read is treated as off**, and logged. Failing closed would stop a shopkeeper mid-sale over
   a database hiccup for a setting they never turned on; failing open is what every shop ran before the switch existed.
4. **Restore and import ask regardless.** A switch stored in the settings a restore would overwrite is not a place to
   put the shop's last defence.

The guard itself is one method, and every module reaches it through a port it declares:

```go
func (s *Service) Require(ctx context.Context, act Act) error {
    if ReservedActs[act.Action] || s.guarding(ctx) {
        if s.elevatedFor(s.clk.Now()) <= 0 {
            return errs.Permission(domain.CodeRequired, "the owner's PIN is required")
        }
    }
    return s.event(ctx, domain.EventGuardedAct, act)   // the record
}
```

When Go returns `lite.owner.required`, the frontend's `withOwner` wrapper opens the PIN dialog and retries the act
**once**. A screen writes a guarded act the way it writes any act — it does not know it is guarded — so a future
guarded act cannot forget to ask.

### 12.3 The guarded acts

| Module | Acts |
|---|---|
| Sales | `sales.sale.void`, `sales.discount`, `sales.cash_note.set` |
| Catalogue | `catalog.price.change`, `catalog.product.deactivate`, `catalog.import` |
| Stock | `stock.adjust.lower`, `stock.count.lower`, `stock.cost.correct`, `stock.receipt.reverse` |
| Customers | `customers.write_off`, `customers.refund`, `customers.opening`, `customers.reverse`, `customers.deactivate_owing` |
| Cash book | `cashbook.deposit`, `cashbook.withdrawal`, `cashbook.expense`, `cashbook.reverse` |
| FX | `fx.rate.set`, `fx.mode.set`, `fx.rate.accept`, `fx.adjust.set`, `fx.source.set` |
| Settings | `settings.shop_name.set`, `settings.money_display.set`, `security.pin_required.set` |
| Printing | `printers.settings` |
| Backups | `backups.restore`*, `backups.import`*, `backups.save_copy`, `backups.outside_folder`, `backups.every` |
| Support | `support.database` |

\* Always require the PIN, whatever the switch says.

> **Note on the language.** Changing the language is deliberately **not** a guarded act. It changes nothing about the
> shop's money, and a helper who reads one language should not need the owner's PIN to read the screen at all.

### 12.4 Settings

All settings live in the `settings` table as key/value rows and resolve through one domain function, which **logs and
ignores** a damaged value rather than failing to start.

| Key | Meaning | Default |
|---|---|---|
| `ui.locale` | `ar` / `en` | `ar` |
| `shop.name` | Printed on every receipt and report | empty until first run |
| `currency.local` | The currency the rate prices | `SYP` |
| `currency.display` | `legacy` / `new` / `dual` | `legacy` |
| `currency.local_cash_note` | Smallest note local totals round to | `500` |
| `debt.default_currency` | `USD` or the local code | `USD` |
| `security.pin_required` | The master PIN switch | `false` |
| `fx.mode` | `automatic` / `manual` | `manual` |
| `fx.source` | `standard` / `local` | `standard` |
| `fx.local_url`, `fx.local_field` | The shop's own market endpoint | empty (built-in sp-today used) |
| `fx.adjust_percent` | Signed margin on the internet's rate | `0` |
| `receipt.header`, `receipt.footer` | Receipt text | shop defaults |
| *(printer keys)* | Printer name, paper width, path | driver path |
| *(backup keys)* | Frequency, outside folder | `daily` |

### 12.5 The Settings screen, and the header

Consolidated into **four groups**, in the order a shop thinks about them:

| Group | Holds |
|---|---|
| **Language and general preferences** | Language, the shop's name |
| **Currency** | Local currency, cash note, debt currency (read-only, with where each is set) · **How money is shown** |
| **Exchange rate** | Source, the local endpoint and field · points at the Rates screen for the margin and the manual rate |
| **Security** | **The master PIN switch**, what it covers, and the two acts that always ask |

Each group **says where the settings it does not hold are set**, rather than duplicating a control that lives elsewhere
— two places to change one figure is how the two disagree.

**The header** carries the shop's name, the exchange rate (clickable, red when stale or unset, with its age), and the
owner-mode countdown with a Lock button. It carries **nothing else**. The two language buttons that used to sit beside
the till were removed in 0.9.6: the language is set once, in Settings, not from a control a cashier can press by
accident mid-sale.

### 12.6 Arabic, RTL and bidi

- Digits are **Latin (0–9) in both languages**, pinned in the locale tag (`ar-u-nu-latn`), never left to the platform.
- Typed Arabic-Indic digits (`١٢٣`) and the Arabic decimal separator (`٫`) are accepted and normalised. The TypeScript
  and Go normalisers are held to **the same fixture file**, so the two cannot disagree.
- Every figure inside an Arabic sentence is wrapped in a bidi isolate (`U+2066 … U+2069`) or `<bdi dir="ltr">`, so
  `1,234.50` is never printed as `50.234,1`. A test lists every text in every document that carries a digit outside an
  isolate and **holds that list empty**.
- The locale catalogues are held complete in both languages by a gate: an error code a screen can receive with no
  translation fails the build.

---

## 13. Build, packaging and CI

### 13.1 Make targets

| Target | Does |
|---|---|
| `make lite-dev` | Hot-reload development |
| `make lite-build-macos` | Universal `.app` into `apps/lite/build/bin/` |
| `make lite-build-windows` | Cross-built `.exe` into `apps/lite/build/bin/` |
| `make lite-package-macos` | `.dmg` into `dist/lite/` |
| `make lite-package-windows` | NSIS installer + portable `.exe` into `dist/lite/` |
| `make lite-guides` | Renders the shop's Markdown guides to the PDFs the app ships |
| `make lite-ci` | The full local CI (below) |
| `make lite-release` | A release from a clean tree: CI, packages, smoke test, checksums |

### 13.2 `make lite-ci`

Fails on the first problem:

1. `wails generate module` — the bindings every gate reads
2. `gofmt`
3. `go vet` (this machine)
4. Cross-compile: `go vet` **and test binaries** for `windows/amd64`
5. Cross-compile: `go vet` for `darwin/amd64` (Intel Macs)
6. **archlint** — the architecture rules (Appendix A)
7. **archlint drills** — 90 planted violations, each proving its rule *can* fail. A rule that cannot fail is not a rule
8. `go test -race ./...`
9. `golangci-lint` v2
10. Frontend: `eslint`, `tsc --noEmit`, `vitest run` (546 tests), production build, bundle gate
11. **Playwright**: 10 journeys × 2 locales + 6 visual/structure specs, against the built bundle through a test-only bridge

### 13.3 `make lite-release`

Additionally, and refusing to run on a dirty tree:

1. Version from the git tag — `scripts/lite-version.sh`. A build off the tag is clean (`0.9.6`); anything else names
   itself `<version>-dev.<sha>` **by design**, so an unofficial build cannot be mistaken for a release
2. The shipped guide PDFs are proved identical to the Markdown they are rendered from
3. **Every past schema (1–8) is upgraded to this release and every row checked intact**
4. `dist/lite` is cleared, then both packages built
5. **Smoke test**: the `.dmg` is *mounted* and the application *inside it* is opened against a seeded shop — it must
   reach Go, report its version and schema, pass an integrity check and close cleanly
6. `SHA256SUMS-<version>.txt` and `MANIFEST-<version>.txt` written

### 13.4 Artefacts

| Artefact | Contents |
|---|---|
| `Mizan Lite <v>.dmg` | Universal macOS app (arm64 + amd64), macOS 13+ |
| `Mizan Lite <v> Setup.exe` | NSIS installer with the **WebView2 runtime bundled** — installs offline on a machine that has never had it |
| `Mizan Lite <v>.exe` | The bare portable application |
| `SHA256SUMS-<v>.txt`, `MANIFEST-<v>.txt` | Hashes; and the build's commit, toolchain, what was verified and what was not |

**Both artefacts are unsigned.** macOS Gatekeeper and Windows SmartScreen will warn.

### 13.5 Post-build cleanup

Run after every packaging:

```bash
go clean -cache                      # the Go build cache (~4 GB)
go clean -testcache
rm -rf apps/lite/build/bin           # the Wails staging directory
rm -rf build/lite-e2e                # Playwright screenshots and fixtures
rm -rf apps/lite/frontend/node_modules/.vite
rm -rf "$TMPDIR"/tmp.*               # release staging temporaries
```

The Go **module** cache (`$GOMODCACHE`, ~5.7 GB) is kept by default; purging it forces a full re-download of every
dependency on the next build.

---

## 14. What is not verified here

Stated plainly, because a document that implies otherwise is worse than no document.

| Not verified | Why |
|---|---|
| **The Windows installer has never been run** | No Windows machine exists in the build environment. It is cross-compiled and packaged, never installed or launched |
| **Thermal printing on real hardware** | Never sent to a physical 80 mm printer |
| **An export opened in Excel** | The workbooks are written and parsed back by the project's own reader, never opened in Excel itself |
| **Gatekeeper / SmartScreen** | Both artefacts are unsigned |
| **Arabic read by a native shopkeeper** | The wording has not been reviewed by the person who will use it |
| **A pilot week in a real shop** | 1.0.0 is cut at the pilot's exit criteria, not before |

Additional known limitations:

- **Dual mode in Excel exports** writes the combined text into a text cell (§11.4).
- **Suppliers and payables do not exist.** There is no supplier entity; "payables" in any report means customer credit
  only.
- **Product categories do not exist.** Report grouping is by product, not category.

---

## Appendix A — architecture rules (enforced)

`archlint` runs on every build against `arch-rules.yml`, and 90 planted drills prove each rule can actually fail.

| Rule | What it holds |
|---|---|
| `lite-domain-purity` | A domain package may import only the standard library, `kernel/errs`, and the named pure helpers |
| `lite-pure-text` | `textkey`, `numinput`, `bizdate`, `tender` and `moneyfmt` may import almost nothing |
| `no-float` | No `float32`/`float64` in `internal/lite`, outside the two page-measuring packages |
| `lite-documents-pure` | `documents` and `sheets` may import only the standard library, `kernel/errs` and `typeset` |
| `lite-network-only-in-httpsource` | Only the rate fetcher may import `net/http` |
| `lite-os-calls-only-in-printers` | Only `printers` may use `os/exec`, `syscall` or `unsafe` |
| `lite-e2e-test-only` | The loopback test bridge can never reach the shipped binary |
| `lite-support-pure`, `lite-guide-pure` | The support bundle and the guides stay dependency-free |

## Appendix B — file map

```
apps/lite/
  main.go                         the Wails application
  wails.json                      product name, version, build config
  frontend/
    src/
      app/          Shell, Root, routes, FirstRun
      api/          the typed client over the generated bindings
      i18n/         LocaleProvider, numbers.ts (formatDecimal, splitDual), figures.ts
      owner/        OwnerProvider, withOwner, PinDialog
      rates/        RateProvider
      screens/      till · sales · cash · customers · products · stock · rates
                    reports · printer · backups · settings · owner · about
      ui/           Button, Field, Checkbox, Alert, Spinner, …
    e2e/            Playwright journeys and the visual pack
internal/lite/
  api/              the binding surface and its DTOs  ← the chokepoint (till.go)
  bootstrap/        the composition root: every service wired, every port satisfied
  moneyfmt/         the money pipeline (Display / Base / SplitDual)
  catalog/ stock/ sales/ customers/ cashbook/ fx/ reports/
  owner/            the PIN, owner mode, the guard and its history
  settings/         every setting and its validation
  backups/ printing/ printers/ documents/ sheets/ guide/ support/
  numinput/ bizdate/ tender/ textkey/ typeset/   the pure helpers
  locales/          ar/ en/ · common.json + errors.json (993 + 240 keys each, held complete)
  migrations/sqlite/  0001 … 0009
scripts/
  lite-check.sh     make lite-ci
  lite-release.sh   make lite-release
  lite-arch-drill.sh  the 90 planted violations
  lite-version.sh   the tag → version rule
docs/mizan_lite/
  README.md · DESIGN.md · DECISIONS.md · PROGRESS.md · GLOSSARY.md · RELEASE.md
  phases/           L0 … L10, the Windows protocol, the pilot plan
  guide/            the shop's own documents, Arabic first
```

---

*Generated from the Mizan Lite source tree at commit `eb4d1a6` (tag `lite-v0.9.6`). Where this document and the code
disagree, the code is right — and the disagreement is a bug in this document.*
