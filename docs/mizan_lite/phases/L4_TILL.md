# Mizan Lite — Phase L4: the till — sales, checkout, and receipts

> **Status: COMPLETE — committed 2026-09-14.** The owner approved this note, its 16 decisions and the stock-ledger migration
> path, answered §13.2 (recorded in §14, which amends §3, §6 and §13 where they differ), and approved the eleven decisions
> made while building (§16). §15–§21 are the implementation record.
> **Date:** 2026-09-14. **Base:** L3 (`67d72c8`). **Design:** [../DESIGN.md](../DESIGN.md) §4.4–§4.6, §5 `sales` and
> `sale_items`, §6.5, §10 (L4), with §13's amendments — Q3 (profit reading b), Q4 (sell beyond stock with a warning),
> Q5 (cash rounding to a note), Q7 (owner PIN to void). Prior phases: [L2_STOCK.md](L2_STOCK.md) A-L2.3 (the planned
> ledger rebuild), [L3_RATES.md](L3_RATES.md) (the rate a sale snapshots).

---

## 0. How to read this

| § | Content |
|---|---|
| 1 | Analysis — what L4 must make possible, and nine things harder than they look |
| 2 | Scope — in, out, the phases after, and six amendments to the approved design |
| 3 | The money of a sale — lines, totals, cash rounding, tender and change |
| 4 | Quote and checkout — the operator is charged what the screen showed |
| 5 | Stock at the till — selling beyond stock, cost snapshots, voids |
| 6 | The schema — `0005_till.sql`, and the ledger rebuild, verified on real data |
| 7 | Receipts — numbers, the immutable snapshot, reprint |
| 8 | Voids, who may do what, and the sales verifier |
| 9 | Architecture — the `sales` module and its ports |
| 10 | Bindings and screens |
| 11 | Demo data |
| 12 | Tests, drills, Definition of Done |
| 13 | Decisions and questions for approval |
| 14 | **At approval** — the owner's answers, and what they changed (discounts, change currency) |
| 15–21 | **Implementation record** — built, decisions while building, findings, evidence, drills, not verified, done |

---

## 1. ANALYSIS

### 1.1 What L4 must make possible

A cashier at a pantry shop:

1. rings up a sale fast: taps **quick buttons**, **scans** barcodes, **types** a weight for loose goods (1.750 kg of
   bulgur), and sees every line priced in **pounds and dollars**;
2. sees the **total in pounds rounded to a note** the shop can actually hand over (Q5), or totals in **dollars** for a
   dollar customer;
3. takes **pounds or dollars** — a customer paying a 132,500-pound total with a $10 note — and is told the **change**;
4. **completes the sale**: stock moves, a **receipt number** is issued, and the receipt is shown and can be shown again;
5. is **warned, not stopped**, when a product is short on the shelf (Q4);
6. with the owner's PIN, **voids** a sale that was wrong, returning its stock (Q7);
7. sees **today's sales** and what came in, per currency.

### 1.2 Nine things harder than they look

**H1 — the total the customer was shown is not the total they are charged.** The rate can change between the quote on
screen and the click on *Pay* (the owner types a new rate in the next room, or — in automatic mode — the hourly fetch
applies one), and so can a price. DESIGN §6.5 named the answer: a **quote token**. §4.

**H2 — rounding twice across currencies, down a whole receipt.** L3 proved one line (21,645 not 21,710). A receipt of
sixty lines adds sixty of those; the printed lines must add up to the printed total **exactly**, and each line's pounds
and dollars must come from one exact product. §3.1.

**H3 — cash rounding is money, and it has to be somewhere.** A total of 132,380 pounds charged as 132,500 is 120 pounds
the shop took that no line explains. It is recorded on the sale as its own figure, and `lines + rounding = total` is a
CHECK constraint (DESIGN §4.2). §3.2.

**H4 — two currencies in one hand.** A pounds total paid with a $10 note, change in pounds: the tender is converted at
the **sale's** rate, the change is rounded to a note, and the drawer gains dollars and loses pounds. The cash summary in
L6 is only right if all four figures are kept. §3.3.

**H5 — selling what the shelf does not have.** Q4 says sell with a warning. L2's stock domain refuses below zero for
every act; the till is the one place it must not. And a product that was **never received** has no average cost: its
profit would be the whole price. §5.

**H6 — the ledger rebuild L2 planned does not work as written.** A-L2.3 planned to rebuild `stock_ledger` in L4 to add
sale kinds. **Tested for this note on a copy of the real seeded shop:** dropping the old table fails its foreign keys —
`stock_levels.last_movement_id` points at it, and a receipt reversal points at the table itself — and SQLite cannot turn
foreign keys off inside the migration's transaction (`defer_foreign_keys` fails at commit instead). Mizan's own rebuild
never had another table pointing at its ledger. §6.2 gives the order that works, verified.

**H7 — a receipt that changes after it was issued.** A product renamed, repriced or deactivated next week must not
change last week's receipt. Every figure and every name on a receipt is **snapshotted** at checkout (Mizan §9.3), and
the sale is never updated except to void it. §7.

**H8 — voiding is not deleting.** A voided sale keeps its receipt number, returns its stock at the cost it left at (not
today's average), and is recorded on the day it was voided — which may not be the day it was sold. §8.

**H9 — the till is the screen that is used most, and the one a keyboard mistake costs most on.** A barcode scanner types
digits and Enter into whatever has focus, in whatever keyboard layout is active (L1 H2). A weight typed as `1,750` is
refused by `numinput` rather than read as 1,750 kg. The till must keep the scan field focused and never compute money in
the browser (DESIGN D9). §10.

---

## 2. SCOPE

### 2.1 In L4

| Area | What |
|---|---|
| Sales | a `sales` module: quote, checkout, receipt numbers, the receipt snapshot, void, the day's sales, a verifier |
| Money | lines in both currencies from one exact product; cash rounding to a note; tender in either currency; change |
| Stock | `sale` and `sale_void` movements; selling beyond stock with a warning; cost snapshots; the ledger rebuilt in the order that works |
| Rates | every sale snapshots the rate in force (its value, id and age); no rate, no sale |
| Owner | void with the PIN; the cash rounding note set with the PIN |
| Screens | **Till** (quick grid, scan and search, cart, totals, tender, change, pay), **Receipt** (after pay and on reprint), **Sales** (the day's sales, per-currency totals, void, verifier banner) |
| Demo data | a day of sales: pounds, dollars, dollars for a pounds total, a sale beyond stock, a void |

### 2.2 Not in L4

| Not here | Why | Where |
|---|---|---|
| Credit sales, customers, debts | a customer and a debt ledger are L5; the schema is ready for it (§6, A-L4.2) | L5 |
| Profit, cash-drawer reports over days | the sale keeps every figure they need | L6 |
| **Printing** receipts (thermal 80 mm, A4) | Q-L4.7 | L7 (recommended) |
| Discounts, price changes at the till | Q3 chose reading (b); Q-L4.6 | — |
| Returning part of a sale | Q-L4.5 | — |
| Several open carts, parked sales | one counter, one operator (DESIGN Q7) | — |

### 2.3 What remains after L4

| Phase | Covers |
|---|---|
| **L5 — Customers and debts** | customers; credit sales (the `credit` payment this schema already allows); a debt ledger per customer **and currency**; repayments in either currency at the day's rate; opening balances from the paper book; statements; who owes what |
| **L6 — Profit and cash** | daily, monthly and per-product profit in USD **and** pounds at each sale's own rate (C6); expected profit on the shelf (Q3); a cash summary per day and currency, voids on their own day; owner mode only (Q7) |
| **L7 — Receipts and backups on screen** | receipt printing, thermal 80 mm and A4, behind `receipts.enabled`, with Arabic/Latin bidi isolation; a backups screen — list, take now, restore — for the snapshots L0 already takes (DESIGN §6.4 lists them; no phase had claimed them) |
| **L8 — Release** | Windows installer (NSIS, with WebView2) and macOS DMG; the first real Windows run (O1); low-end hardware measurements (O6); the full seeder; a Definition-of-Done review; one pilot shop for one week |

### 2.4 Six amendments to the approved design

**A-L4.1 — the ledger rebuild also rebuilds `stock_levels`, in dependency order** (H6, §6.2). It amends A-L2.3's plan,
not its decision.

**A-L4.2 — `sales` has no `customer_id`; `payment` allows `credit` from L4.** A table referenced by `sale_lines`, the
stock ledger and — in L5 — the debt ledger is the worst table to rebuild, as H6 just showed for a table referenced
once. So the `payment` CHECK includes `credit` now, the service refuses it until L5, and the customer of a credit sale is
recorded where it belongs: on L5's debt entry, which names the sale. DESIGN §5's `customer_id` column and its CHECK move
there.

**A-L4.3 — one price per line.** Q3 chose reading (b) — expected profit is a stock report — and prices change only with
the owner's PIN (L1), so `list_price_micro` and `unit_price_micro` would always be equal. One column.

**A-L4.4 — tender and change replace `cash_local` / `cash_usd`.** DESIGN's two payment kinds cannot record a pounds total
paid in dollars (H4). A sale records its **settlement currency** (what the total is fixed in), the **tender currency and
amount**, and the **change currency and amount**.

**A-L4.5 — no `products.on_hand_micro`.** DESIGN §5's cached balance on `products` became L2's `stock_levels`; the till
reads levels through the stock module.

**A-L4.6 — receipts are issued in L4 and printed in L7** (recommended; Q-L4.7). Issuing — the number, the snapshot, the
receipt on screen, reprint — is what makes a sale a sale. Printing differs by platform: the WebView2 control on Windows
prints from the page, macOS's WebKit view does not print from JavaScript, so a cross-platform print path (a document
rendered in Go and handed to the system) is its own work.

---

## 3. THE MONEY OF A SALE

### 3.1 A line

For a product with price **P** (10⁻⁶ of its price currency), quantity **Q** (10⁻⁶ of its unit), and the rate **R**
(pounds per dollar, 10⁻⁹):

| Product priced in | Line in pounds | Line in dollars |
|---|---|---|
| pounds | `LineExtension(P, Q)` | `LineExtensionDivRate(P, Q, R, USD)` |
| dollars | `LineExtensionMulRate(P, Q, R, SYP)` | `LineExtension(P, Q)` |

Both from the exact product, each rounded once (L3's kernel additions; DESIGN §4.4). The two figures are not each
other's conversion to the last unit — by design, and a test says so.

**Cost**, snapshotted per line: `unit_cost_usd_micro` = the product's average at the moment of sale; `cost_usd_minor` =
`LineExtension(avg, Q)`; `cost_local_minor` = `LineExtensionMulRate(avg, Q, R, SYP)` — at the **sale's** rate, so L6's two
profit figures agree (C6).

### 3.2 Totals and cash rounding (H3)

- `lines_local` = Σ lines in pounds; `lines_usd` = Σ lines in dollars — the printed lines add up to these **exactly**.
- **Settled in pounds:** `total = round(lines_local, to the nearest cash note)`, half up; `rounding = total − lines_local`
  is recorded (it may be negative). The note is a setting, `currency.local_cash_increment`, changed with the PIN — its
  value is Q-L4.1.
- **Settled in dollars:** `total = lines_usd`, to the cent, no cash rounding (Q5 concerns the pound).
- The schema holds `total = lines_in_settlement_currency + rounding` and `rounding = 0` for dollars (§6).

### 3.3 Tender and change (H4)

| Settlement | Tendered in | Change given in | Change |
|---|---|---|---|
| pounds | pounds | pounds | `tendered − total` |
| pounds | dollars | pounds | `tendered × R` in pounds, minus `total`, **rounded to the note** |
| dollars | dollars | dollars | `tendered − total`, to the cent |
| dollars | pounds | pounds | `tendered − total × R` in pounds, **rounded to the note** |

The rule behind the table (Q-L4.2): **change is given in pounds unless both the total and the tender are in dollars.**
Every conversion uses the sale's rate, in the kernel, rounded once; the change is rounded **to the nearest note, half
up**, and the verifier holds the rounding to within half a note of the exact change. A tender below the total — in value,
at the sale's rate — is refused before anything is written. Tendering is optional: an empty tender means *exact money*,
recorded as tender = total in the settlement currency, change 0.

---

## 4. QUOTE AND CHECKOUT (H1)

**`Till.Quote`** — the cart (product ids, quantities as typed), the settlement currency, and the tender (currency and
amount, optional) — returns **every figure the till displays**: each line in both currencies with its warnings, the
totals, the rounding, the total to pay, the change, the rate with its age and "not updated today", and a **quote token**.
The frontend computes nothing (D9); it re-quotes, debounced, whenever the cart or the tender changes.

**`Till.Checkout`** takes the same input **and the token**. Inside one transaction it:

1. reads the rate in force — **refuses with no rate** (L3 §4.4: nothing converts at 1);
2. prices every line from the catalogue as it is now, and reads each product's stock level;
3. recomputes the token and **refuses `lite.sales.quote_stale`** if it differs — the till re-quotes and shows the new
   total with *"the rate or a price changed — check the total"*; nothing was written;
4. assembles the sale: lines, totals, rounding, tender, change, cost snapshots, the shop's name;
5. allocates the next **receipt number** (§7.1);
6. inserts the sale and its lines;
7. appends one `sale` stock movement per line, in the stock module's own act, **in the same transaction**;
8. commits — or nothing at all.

**The token** is a SHA-256 digest of what determines the charge: the rate's id, each line's product id, row version,
price and price currency, the quantities, the settlement and tender currencies and amounts, and the cash note. It is not
a secret — one operator, one machine — only a fingerprint of what was shown.

**Refused before anything is written:** an empty cart; more than 200 lines; a quantity that is zero, negative, or has
more decimals than its unit (L1's shared fixture); an inactive product (it may have been deactivated while in the cart);
a product that does not exist; a tender below the total; `credit` (until L5).

---

## 5. STOCK AT THE TILL (H5, H8)

**A sale** is a stock movement of kind `sale`: quantity −Q, the average **unchanged**, unit cost = the average. Unlike every
L2 act, it is **allowed below zero** (Q4): the quote warns *"only 0.500 kg on the shelf"*, the cashier sells, and the level
goes negative until a delivery or a count corrects it — which L2's rule (§3.2 H2) already costs correctly.

**A product never received** (no level, or no cost: L2 D-L2.i2's condition) can still be sold (Q-L4.8), with the warning
*"no cost is recorded for this product — its profit will be wrong"*. The line records `cost_known = 0` and zero cost, so L6
can show those lines as unknown rather than as pure profit.

**A void** appends, per line, a `sale_void` movement: quantity +Q, reversing the sale movement (`reverses_id`), at the
**snapshotted** unit cost. It is costed like a receipt of the returned goods — it averages in, and into zero or negative
stock it takes that cost — because the goods come back at what they cost when they left, not at today's average (DESIGN
§4.6).

**The stock verifier grows** (L2 §7): a `sale_void` must reverse a `sale` of the same product and quantity; a `sale` or
`sale_void` must name a sale line that names the same product and quantity.

---

## 6. THE SCHEMA — `0005_till.sql`

### 6.1 Sales and their lines

```sql
CREATE TABLE sales (
  id                    CHAR(36)     NOT NULL PRIMARY KEY,
  receipt_no            BIGINT       NOT NULL CHECK (receipt_no >= 1),
  business_date         CHAR(10)     NOT NULL,
  sold_at               CHAR(24)     NOT NULL,
  status                VARCHAR(8)   NOT NULL CHECK (status IN ('posted', 'voided')),
  payment               VARCHAR(8)   NOT NULL CHECK (payment IN ('cash', 'credit')),     -- credit refused until L5 (A-L4.2)
  local_currency        CHAR(3)      NOT NULL REFERENCES currencies(code),
  fx_rate_id            CHAR(36)     NOT NULL REFERENCES fx_rates(id),                  -- the rate snapshot:
  local_per_usd_nano    BIGINT       NOT NULL CHECK (local_per_usd_nano > 0),           --   its value,
  rate_recorded_at      CHAR(24)     NOT NULL,                                          --   and how old it was
  settlement_currency   CHAR(3)      NOT NULL REFERENCES currencies(code),
  lines_local_minor     BIGINT       NOT NULL CHECK (lines_local_minor >= 0),
  lines_usd_minor       BIGINT       NOT NULL CHECK (lines_usd_minor >= 0),
  cash_increment_minor  BIGINT       NOT NULL CHECK (cash_increment_minor >= 1),        -- the note in force at the sale
  rounding_minor        BIGINT       NOT NULL,                                          -- settlement currency, signed
  total_minor           BIGINT       NOT NULL CHECK (total_minor >= 0),                 -- what was charged
  tendered_currency     CHAR(3)      NOT NULL REFERENCES currencies(code),
  tendered_minor        BIGINT       NOT NULL CHECK (tendered_minor >= 0),
  change_currency       CHAR(3)      NOT NULL REFERENCES currencies(code),
  change_minor          BIGINT       NOT NULL CHECK (change_minor >= 0),
  cost_usd_minor        BIGINT       NOT NULL CHECK (cost_usd_minor >= 0),
  shop_name_snapshot    VARCHAR(100) NOT NULL,
  voided_at             CHAR(24),
  void_business_date    CHAR(10),                                                      -- the day the void belongs to (H8)
  void_reason           VARCHAR(200),
  created_at            CHAR(24)     NOT NULL,
  row_version           BIGINT       NOT NULL DEFAULT 1,
  CONSTRAINT ux_sales_receipt_no UNIQUE (receipt_no),
  CONSTRAINT ck_sale_local_is_not_usd CHECK (local_currency <> 'USD'),
  CONSTRAINT ck_sale_settles_in_its_two_currencies CHECK (settlement_currency IN (local_currency, 'USD')),
  CONSTRAINT ck_sale_tender_in_its_two_currencies CHECK (tendered_currency IN (local_currency, 'USD')),
  CONSTRAINT ck_sale_change_in_its_two_currencies CHECK (change_currency IN (local_currency, 'USD')),
  CONSTRAINT ck_sale_total_is_lines_plus_rounding CHECK (total_minor =
    CASE WHEN settlement_currency = 'USD' THEN lines_usd_minor ELSE lines_local_minor END + rounding_minor),
  CONSTRAINT ck_sale_dollars_are_not_cash_rounded CHECK (settlement_currency <> 'USD' OR rounding_minor = 0),
  CONSTRAINT ck_sale_void_is_complete CHECK (
    (status = 'posted' AND voided_at IS NULL     AND void_business_date IS NULL     AND void_reason IS NULL) OR
    (status = 'voided' AND voided_at IS NOT NULL AND void_business_date IS NOT NULL AND void_reason IS NOT NULL))
);
CREATE INDEX ix_sales_business_date      ON sales (business_date, status);
CREATE INDEX ix_sales_void_business_date ON sales (void_business_date);

-- Every figure a later edit to the product could change is SNAPSHOTTED (H7).
CREATE TABLE sale_lines (
  id                   CHAR(36)     NOT NULL PRIMARY KEY,
  sale_id              CHAR(36)     NOT NULL REFERENCES sales(id),
  line_no              SMALLINT     NOT NULL CHECK (line_no >= 1),
  product_id           CHAR(36)     NOT NULL REFERENCES products(id),
  name_ar_snapshot     VARCHAR(200) NOT NULL,
  name_en_snapshot     VARCHAR(200),
  unit_code_snapshot   VARCHAR(16)  NOT NULL,
  quantity_micro       BIGINT       NOT NULL CHECK (quantity_micro > 0),
  price_currency       CHAR(3)      NOT NULL REFERENCES currencies(code),
  unit_price_micro     BIGINT       NOT NULL CHECK (unit_price_micro >= 0),               -- one price (A-L4.3)
  line_local_minor     BIGINT       NOT NULL CHECK (line_local_minor >= 0),
  line_usd_minor       BIGINT       NOT NULL CHECK (line_usd_minor >= 0),
  unit_cost_usd_micro  BIGINT       NOT NULL CHECK (unit_cost_usd_micro >= 0),
  cost_known           SMALLINT     NOT NULL CHECK (cost_known IN (0, 1)),
  cost_usd_minor       BIGINT       NOT NULL CHECK (cost_usd_minor >= 0),
  cost_local_minor     BIGINT       NOT NULL CHECK (cost_local_minor >= 0),
  CONSTRAINT ux_sale_lines_line UNIQUE (sale_id, line_no),
  CONSTRAINT ck_sale_line_unknown_cost_is_zero CHECK (
    cost_known = 1 OR (unit_cost_usd_micro = 0 AND cost_usd_minor = 0 AND cost_local_minor = 0))
);
CREATE INDEX ix_sale_lines_product ON sale_lines (product_id);
```

`sales` is updated only `posted → voided`; `sale_lines` never — a test scans the store's SQL, as for the ledger.

### 6.2 The stock ledger rebuild, in the order that works (H6, A-L4.1)

The new `stock_ledger` is L2's with three changes: `kind` gains `sale` and `sale_void`; it gains `sale_id REFERENCES
sales(id)` and `sale_line_id REFERENCES sale_lines(id)`; and `reverses_id` now links `sale_void` as well as
`receipt_reversal` (so a sale line is voided at most once, by `ux_ledger_reversed_once`). New constraints:

```sql
  CONSTRAINT ck_ledger_quantity_by_kind CHECK (
    (kind IN ('opening', 'receipt', 'content_in', 'sale_void') AND quantity_micro > 0) OR
    (kind IN ('receipt_reversal', 'package_out', 'sale')      AND quantity_micro < 0) OR …as L2),
  CONSTRAINT ck_ledger_reversal_links CHECK ((kind IN ('receipt_reversal', 'sale_void')) = (reverses_id IS NOT NULL)),
  CONSTRAINT ck_ledger_sale_links CHECK ((kind IN ('sale', 'sale_void')) = (sale_id IS NOT NULL AND sale_line_id IS NOT NULL)),
  CONSTRAINT ck_ledger_sale_links_both_or_neither CHECK ((sale_id IS NULL) = (sale_line_id IS NULL)),
…
CREATE UNIQUE INDEX ux_stock_ledger_sale_line_kind ON stock_ledger (sale_line_id, kind);  -- one sale and one void per line
```

**The migration's order**, in one transaction with foreign keys on, as the runner runs it:

1. copy `stock_levels` aside and drop it — it points at the ledger;
2. create `stock_ledger_new`, whose `reverses_id` references **`stock_ledger_new(id)`** — referencing the old name makes
   the old table's drop break the new table's copied reversal rows;
3. copy every row, with `sale_id` and `sale_line_id` NULL;
4. drop `stock_ledger`; rename `stock_ledger_new` to `stock_ledger` — SQLite rewrites the self-reference to the new name;
5. recreate `stock_levels` exactly as 0003 did, copy it back, drop the copy; recreate the ledger's indexes.

**Verified before this note was presented**, with Python's `sqlite3` on a copy of the database the L3 demo seeder built
(47 ledger rows, 40 levels), plus a receipt and its reversal added so the self-reference was exercised:

| Check | Result |
|---|---|
| L2's plan as written (drop the ledger with `stock_levels` in place) | **fails**: `FOREIGN KEY constraint failed` — with and without `defer_foreign_keys` |
| the first corrected attempt (new table referencing the old name) | **fails** on the reversal row — step 2's reason |
| the order above | **commits**; ledger rows identical (every column compared), levels identical, `foreign_key_check` empty, `integrity_check` ok, the self-reference reads `REFERENCES "stock_ledger"(id)` |
| 35 constraint cases on the migrated database | all 26 impossible rows refused (`sales` 12, `sale_lines` 5, ledger 9) and all 9 valid rows accepted — a pounds total with dollars tendered, a dollar settlement, a void, a `credit` sale (schema only), a line with unknown cost, a sale movement, a sale below zero (Q4), and a sale void |

Two refusals came from a different constraint than the draft predicted (a third currency is refused by the CHECK before
the foreign key) — the rows were refused; the prediction, not the schema, was wrong. The implementation's migration test
runs this rebuild through the real runner on a database holding every L2 movement kind, and compares every row.

---

## 7. RECEIPTS

### 7.1 Numbers

One continuous sequence, **1, 2, 3 …**, never reset and never reused: the next number is `MAX(receipt_no) + 1` inside the
checkout's transaction, under the unique constraint and SQLite's single writer, so a checkout that rolls back consumes no
number. A voided sale keeps its number. Shown as *No. 124*, with its business date and time.

### 7.2 The receipt is the sale

There is no separate receipt table: the receipt **is** the stored sale and its lines — the shop's name, the date and time,
the receipt number, every line's name, unit, quantity, unit price and currency, the line in pounds and in dollars, the
totals, the rounding, the rate and its date, the tender and the change. Nothing on it is read from the catalogue, the
settings or the rate table at display time (H7). A voided receipt shows *VOIDED*, when, and why.

### 7.3 On screen

After *Pay*, the receipt opens over the till with **New sale** focused (Enter starts the next customer). From the Sales
screen, any receipt reopens. Printing is L7 (A-L4.6, Q-L4.7).

---

## 8. VOIDS, WHO MAY DO WHAT, AND THE SALES VERIFIER

### 8.1 A void

A whole sale (Q-L4.5), with a required reason, with the owner's PIN (Q7). One transaction: the sale becomes `voided` with
`voided_at`, `void_business_date` (the shop's day of the **void**) and the reason; each line's stock returns (§5); the
owner's history records *sale voided — No. 124 — 132,500 SYP*. A sale already voided is refused. Any posted sale may be voided
on any later day (Q-L4.4); L6 counts the void on its own day.

### 8.2 The guards

| Act or read | Owner PIN | Why |
|---|---|---|
| Quote, checkout, reprint, the day's sales | no | the counter's work |
| Sell beyond stock or with no recorded cost | no — warned | Q4 |
| **Void a sale** | **yes** | Q7 |
| **Change the cash rounding note** | **yes** | it changes what every customer is charged |
| Costs on a sale | not shown in L4 | profit is L6's, in owner mode (Q7) |
| The sales verifier's findings | owner mode | as L2's stock verifier |

### 8.3 The sales verifier

Reports, never repairs (D-L2.14), and the demo seeder fails on a finding:

| Check | Finds |
|---|---|
| Σ lines = `lines_local` and `lines_usd`; `total = lines + rounding` | a sale whose receipt does not add up |
| the rounding is within half a note; the change is within half a note of `tender − total` at the sale's rate | a mis-rounded total or change |
| every line has exactly one `sale` movement of its product and quantity, and a voided sale one `sale_void` per line | stock that did not move with its sale |
| no stock movement names a sale that does not have that line | stock that moved without a sale |
| receipt numbers 1 … N with no gap | a lost or deleted sale |

---

## 9. ARCHITECTURE

### 9.1 The `sales` module

`internal/lite/sales` — domain (a quote, a sale, lines, rounding, tender and change, the token, the verifier), service
(quote, checkout, list, receipt, void, verify, the cash note), `infra/sqlite`, `salestest` (fakes and the store contract,
run on the fake **and** SQLite).

It reaches every other module through ports it declares, satisfied in the composition root — the pattern since L1:

| Port | What it needs | Satisfied by |
|---|---|---|
| `Catalogue` | a product by id or barcode: names, unit and its decimals, price and currency, active, row version; currency decimals | `catalog.Service` |
| `Stock` | levels for warnings; **record a sale movement** and **record a void movement**, each in the caller's transaction | `stock.Service` (two new acts) |
| `Rates` | the rate in force with its id, value, recorded time and staleness | `fx.Service` |
| `OwnerGate` | `Require` for void and the cash note | `owner.Service` |
| `Settings` | the shop's name; the cash note (read and, guarded, write) | `settings.Service` |

The stock module gains `RecordSale` and `RecordSaleVoid` — acts with no owner guard and no below-zero refusal, that take
a sale id and line id and join their caller's transaction (L2 D-L2.i1's places and before/after chain apply unchanged).

### 9.2 Rules

| Change | Why |
|---|---|
| **`lite-sales-isolated`** (new): `sales` imports no other Lite module | ports, as above |
| every other isolation rule forbids `sales` | the set grows with the modules |
| the no-network rule applies to `sales` | nothing at the till reaches the internet |

Each with a plant in `scripts/lite-arch-drill.sh`.

---

## 10. BINDINGS AND SCREENS

### 10.1 Bindings

| Façade.Method | Guard | Caller |
|---|---|---|
| **`Till.Scan`** (code) — the product for a barcode, digits normalised, with its stock warning | — | TillScreen scan field |
| **`Till.Quote`** (cart, settlement, tender) | — | TillScreen, debounced |
| **`Till.Checkout`** (cart, settlement, tender, token) → receipt | — | TillScreen *Pay* |
| **`Till.CashNote`** / **`Till.SetCashNote`** | set: owner | RatesScreen, a *Cash rounding* section beside the rate |
| **`Sales.List`** (business date) — sales and per-currency totals | — | SalesScreen |
| **`Sales.Receipt`** (sale id) | — | ReceiptView (SalesScreen) |
| **`Sales.Void`** (sale id, reason) | owner | ReceiptView *Void* |
| **`Sales.Verify`** | owner mode | SalesScreen banner |

**9 new methods, 50 in total.** Every figure crosses as a decimal string formatted by Go.

### 10.2 The Till (`/till`)

- **Left in Arabic, right in English — mirrored by direction:** the **quick grid** (24 buttons, L1) and the **scan and
  search** field; the other side: the **cart** — each line's name, quantity (editable, the unit's decimals enforced as
  typed from the shared fixture), price, line in pounds and dollars, its warning, and remove.
- **Adding:** a scan or a tap adds 1 of a counted product, or increases its line; a weighed or measured product opens a
  **quantity** prompt first (1.750 kg), with Arabic-Indic digits and `٫` accepted.
- **Totals:** the **total to pay** large, in the settlement currency; the other currency beside it; the rounding if any;
  the rate and its age, amber when *not updated today*.
- **Pay:** settlement *Pounds / Dollars*; tender *currency and amount* (optional); the **change** as Go computed it;
  **Pay** (Enter from the tender field). With no rate, *Pay* is disabled and the till says *set an exchange rate*, linking to it.
- **Focus:** the scan field keeps focus after every add, so a scanner never types into a quantity.
- **Stale quote:** the new total is shown with *"the rate or a price changed — check the total"* and *Pay* must be pressed
  again.

### 10.3 Sales (`/sales`)

The day's sales (today by default, a date picker for earlier days): receipt number, time, total, tender, change, status.
Per currency: sales, total charged, cash in, change out, voids. Opening a sale shows its receipt, with **Void** (PIN,
reason). In owner mode, the verifier's banner if it found anything.

---

## 11. DEMO DATA

`lite-demoseed` grows: after L3's rates, **a day of sales** through the services, as a person would — ten sales mixing
quick buttons and scanned barcodes; pounds totals paid in pounds; a dollar customer settling in dollars; **a pounds total
paid with a $20 note, change in pounds**; weighed goods (1.750 kg of bulgur); a sale of more labneh than the shelf holds
(the warning, and a negative level); and **one void** with the PIN. Then the stock verifier **and** the sales verifier run,
and the seeder fails if either finds anything.

---

## 12. TESTS, DRILLS, DEFINITION OF DONE

### 12.1 Tests (names are the contract)

| Layer | Tests |
|---|---|
| **sales/domain** | `TestALineIsPricedInBothCurrenciesFromOneExactProduct` (priced in pounds, and in dollars) · `TestPrintedLinesAddUpToThePrintedTotalExactly` (a 60-line receipt, property) · `TestCashRoundingIsRecordedAndHalfUp` · `TestDollarsAreNotCashRounded` · `TestChangeIsInPoundsUnlessEverythingIsInDollars` (the §3.3 table) · `TestATenderBelowTheTotalIsRefused` · `TestTheQuoteTokenChangesWithTheRateAPriceOrTheCart` · `TestCostIsSnapshottedAtTheSalesRate` · `TestANeverReceivedProductSellsWithUnknownCost` |
| **sales service (fake + contract)** | `StoreContract` · `TestCheckoutRefusesAStaleQuoteAndWritesNothing` · `TestNoRateNoSale` · `TestReceiptNumbersAreContinuousAndARolledBackCheckoutUsesNone` · `TestAnInactiveProductInTheCartIsRefused` · `TestVoidNeedsTheOwnerAndReturnsTheStockAtTheSnapshottedCost` · `TestAVoidBelongsToTheDayItWasMade` · `TestCreditIsRefusedUntilCustomersExist` |
| **stock** | `TestASaleMayTakeStockBelowZero` · `TestASaleNeverMovesTheAverage` · `TestAVoidAveragesInAtTheCostTheGoodsLeftAt` · verifier plants for a void of the wrong movement and a sale movement with no line |
| **the database** | `TestTheLedgerRebuildKeepsEveryRow` (every L2 kind, a reversal and a package pair, compared column by column through the real runner) · `TestACheckRefusesAnImpossibleSale` (every §6 constraint, by raw insert, naming the constraint, NULL cases) · `TestSalesAreUpdatedOnlyToVoidAndLinesNever` (SQL scan) · `TestACheckoutThatFailsAfterTheStockMovementLeavesNothing` (real transaction) |
| **verifier** | one planted discrepancy per §8.3 row, found and not repaired |
| **bindings** | wire shapes · `Sales.Void` refused outside owner mode · `Till.Checkout` with a stale token · every figure a string |
| **frontend** | the grid adds and increments · a scan with an Arabic layout's digits finds the product and keeps focus · a weighed product asks for a quantity and refuses a fourth decimal · the till shows Go's totals and change and computes none · a stale quote shows the new total and needs a second *Pay* · no rate disables *Pay* · the warning beyond stock · the receipt after pay, and reprint · void through the PIN · the day's totals per currency |
| **seeder** | §11, both verifiers clean |

### 12.2 Drills planned

At least: a line's pounds computed from its rounded dollars (the §3.1 test fails); the rounding left off the sale (the
CHECK and the domain fail); change computed from a rate inverted first; the token check removed (the stale-quote test
fails); the receipt number taken outside the transaction; the stock movement written in its own transaction (the
rollback test fails); a sale refused below zero (Q4's test fails); a void at today's average instead of the snapshot; a void
without the guard; the ledger rebuild in L2's original order (the migration test fails); a receipt read from the
catalogue instead of the snapshot (rename the product, the receipt test fails); `credit` accepted.

### 12.3 Definition of Done

> `make lite-ci` green · every invariant drilled · the seeder's day of sales opens in the packaged app, both verifiers
> clean · an installation from L3 upgrades through the rebuild with its ledger intact · the Till and Sales screens looked at
> in Arabic and English (owner, O5) · Windows `.exe` builds · Mizan's `scripts/check.sh` green if shared code changes ·
> [../PROGRESS.md](../PROGRESS.md), [../DECISIONS.md](../DECISIONS.md) and this document updated.

---

## 13. DECISIONS AND QUESTIONS FOR APPROVAL

### 13.1 Decisions — approve, amend, or reject

| # | Decision | § |
|---|---|---|
| D-L4.1 | A `sales` module owns `sales` and `sale_lines`, reaching catalogue, stock, rates, owner and settings through ports | 9 |
| D-L4.2 | Every line priced in both currencies from one exact product; the printed lines add up to the printed total exactly | 3.1, 3.2 |
| D-L4.3 | Cash rounding to a note, half up, recorded on the sale; dollars not cash-rounded; `total = lines + rounding` is a CHECK | 3.2 |
| D-L4.4 | Tender in either currency; change in pounds unless total and tender are both in dollars; change rounded to the note | 3.3 |
| D-L4.5 | A quote token: checkout recomputes it and refuses a stale quote, writing nothing | 4 |
| D-L4.6 | No rate, no sale | 4 |
| D-L4.7 | Selling beyond stock is allowed with a warning; a never-received product sells with `cost_known = 0` | 5 |
| D-L4.8 | Cost snapshotted per line at the sale's rate; a void returns stock at that cost | 3.1, 5 |
| D-L4.9 | The ledger rebuild in dependency order, with `stock_levels` rebuilt too (amends A-L2.3's plan) | 6.2, A-L4.1 |
| D-L4.10 | `payment` allows `credit` now, refused until L5; the customer lives on L5's debt entry, not on `sales` | A-L4.2 |
| D-L4.11 | One price per line (Q3 reading b) | A-L4.3 |
| D-L4.12 | One continuous receipt sequence, allocated in the checkout transaction; voids keep their numbers | 7.1 |
| D-L4.13 | The receipt is the stored sale, fully snapshotted; no display-time lookups | 7.2 |
| D-L4.14 | A whole-sale void with the PIN and a reason, recorded on the day it is made | 8.1 |
| D-L4.15 | A sales verifier that reports, never repairs; the seeder fails on a finding | 8.3 |
| D-L4.16 | `lite-sales-isolated`; every other isolation rule forbids `sales` | 9.2 |

### 13.2 Questions only you can answer

**Q-L4.1 — What is the smallest pound note the shop hands over, and which way does a total round?** DESIGN Q5 said *round to
the nearest standard note*, and left the value to this note. With the dollar at around 15,000 old pounds, the notes in
daily use start somewhere between 100 and 1,000. *Recommended: 500 pounds, to the nearest (250 rounds up), changeable by
the owner with the PIN.* If your shop always rounds **down** in the customer's favour, say so — it is one line.

**Q-L4.2 — When a customer pays a pounds total with dollars, is the change given in pounds?** *Recommended: yes — change is in
pounds unless both the total and the payment are in dollars.* The till then shows, for a 132,500 total paid with $10 at
15,000: *change 17,500 pounds*.

**Q-L4.3 — May a customer settle the whole sale in dollars?** *Recommended: yes* — the total is then the sum of the lines in
dollars, to the cent, not rounded, and the change is in dollars when paid in dollars.

**Q-L4.4 — Which sales may be voided?** *Recommended: any posted sale, on any later day, with the PIN and a reason*, counted on
the day of the void. The alternative, *today's sales only*, is stricter and makes a mistake found tomorrow uncorrectable.

**Q-L4.5 — Returning part of a sale?** *Recommended: not in v1* — void the sale and ring up what the customer keeps. A partial
return is a second document kind (a refund) with its own receipt and cash-out, and nothing yet needs it.

**Q-L4.6 — Discounts at the till?** *Recommended: none in v1.* Cash rounding covers the small change, prices change only with
the PIN (L1), and a discount at the counter is the same power as a price change. If shops haggle, the answer is a
**discount with the owner's PIN**, recorded on the sale — say so and it joins L4.

**Q-L4.7 — Is printing receipts needed now, or is the receipt on screen enough until L7?** *Recommended: on screen in L4,
printing in L7.* Printing needs a separate path per platform (§2.4 A-L4.6) and the choice of printer — which leads to…

**Q-L4.8 — Will you sell a product that was never received into stock?** *Recommended: yes, with the warning and the cost
recorded as unknown*, because a shop that has not yet entered its opening stock must still be able to trade. The
alternative refuses the sale until an opening or a delivery is recorded.

**Q-L4.9 — What printer, if any, does the shop use?** An 80 mm thermal receipt printer (the common kind, USB), a normal A4
printer, or none. Not needed to build L4 — it decides L7's design.

---

## 14. AT APPROVAL (2026-09-14)

The owner approved §13.1's sixteen decisions and the ledger migration path of §6.2 (A-L4.1), and answered §13.2:

| Q | The owner's answer | What it means in the build |
|---|---|---|
| Q-L4.1 | **500 pounds**, to the **nearest** 500 | `settings` key `currency.local_cash_note`, default 500; half up (250 → 500); changed with the PIN (1 … 1,000,000) |
| Q-L4.2 | Change in **pounds**, unless the customer asks otherwise | the till's *Change in* defaults to pounds and offers dollars |
| Q-L4.3 | **Yes** — a whole sale in dollars, change **strictly** in dollars when paid in dollars | settlement USD; a pounds change for dollars paid on a dollar total is refused (`lite.sales.change_currency`) |
| Q-L4.4 | **Any** past sale, with the PIN and a **required** reason | as recommended; the void counts on its own day |
| Q-L4.5 | **No** partial returns — void and re-ring | as recommended |
| Q-L4.6 | **Item and total discounts, only with the owner PIN** | *changed from the recommendation* — §14.1 |
| Q-L4.7 | Receipts **on screen** in L4, printing in L7 | as recommended |
| Q-L4.8 | **Yes**, with a clear warning, cost recorded as unknown | as recommended |
| Q-L4.9 | **80 mm thermal** | the on-screen receipt is laid out at 80 mm; L7 prints it |

### 14.1 Discounts (Q-L4.6)

- **A line discount is a percentage**, 0–100 with at most two decimals, applied to the line's gross in **each** currency and
  rounded once per currency — so the lines still add up exactly in both.
- **A sale discount is an amount in the settlement currency**, converted once to the other currency and capped at the lines
  in that currency (the conversion's one rounding could otherwise exceed them by a minor unit).
- **Any discount needs the owner at checkout** — one guarded act `sales.discount`, recorded as *gross → total* (e.g.
  *90000 SYP → 80000 SYP*). The quote says `discounted`, so the till warns before *Pay*; the discounts are part of the
  quote token, so a discount changed after the quote is a stale quote.
- Schema additions to §6.1: `sales.discount_local_minor`/`discount_usd_minor` and `ck_sale_discount_within_lines`;
  `ck_sale_total_is_lines_less_discount_plus_rounding` replaces `total = lines + rounding`; `sale_lines.gross_*_minor`,
  `discount_percent_micro`, `discount_*_minor`, `ck_sale_line_discount_within_gross` and
  `ck_sale_line_no_percent_no_discount`.

### 14.2 The change currency (Q-L4.2, Q-L4.3)

A cart carries `changeCurrency`: empty for the default (pounds; dollars when the total **and** the tender are dollars), or
either currency on the customer's request — except pounds for dollars paid on a dollar total, which Q-L4.3 forbids. An
empty tender is **exact money**: tender = total, change 0. Schema: `sales.change_currency` with
`ck_sale_change_in_its_two_currencies`.

---

## 15. WHAT WAS BUILT

| Layer | What |
|---|---|
| **Migration** | `0005_till.sql`: `sales`, `sale_lines` (§6.1 with §14), and the ledger rebuild in the verified order (kinds `sale`/`sale_void`, `sale_id`, `sale_line_id`, `ux_stock_ledger_sale_line_kind`, `stock_levels` recreated) |
| **stock** | domain `Sale` (below zero allowed, average untouched, unit cost the average if known) and `SaleVoid` (averaged in at the sale's cost); verifier checks a void reverses a sale of the same line, product and quantity; service `RecordSale`, `RecordSaleVoid` (refuses a second void of a line), `Stocked`, `EachSaleMovement`; store and fake `SaleMovement`, `EachSaleMovement` |
| **sales** (new module) | domain `Price` (lines, discounts, cash rounding, tender, change, warnings, the token), `Assemble`, `Sale.Void`, `Verify` (six findings); service `Scan`, `Quote`, `Checkout`, `Day`, `Receipt`, `Void`, `CashNote`, `SetCashNote`, `Verify`; five ports; `salestest` fakes and `StoreContract` on fake and SQLite; SQLite store |
| **settings, catalog, numinput** | the cash note setting; `catalog.ByBarcode` (digits in any script); `numinput.FormatFixed`, shared by stock, fx and sales |
| **bootstrap** | the till wired last, with five adapters |
| **api** | `Till` (Scan, Quote, Checkout, CashNote, SetCashNote) and `Sales` (List, Receipt, Void, Verify) — **9 methods, 50 bound**; every figure a string; no cost field |
| **archlint** | `lite-sales-isolated`; the six other module rules forbid `sales` — **39** rules seen failing |
| **i18n** | 31 error codes and findings (the seeder's `sales_inconsistent` included), 3 owner-history labels, 86 screen strings, in both languages; a new gate: every `Act*` has an `owner.action.*` label |
| **frontend** | `/till` (scan and search, quick grid, weighed-quantity prompt, cart with line discounts, settlement, sale discount, tender and change currency, Go's totals and change, stale quote, no rate); `ReceiptView` (80 mm, void with PIN and reason); `/sales` (the day, per-currency totals, the verifier's banner in owner mode); the *Cash rounding* section on `/rates`; stock history names `sale` and `sale_void` |
| **seeder** | a day at the till (§11): ten sales, one void, both verifiers must be clean |

## 16. DECISIONS MADE WHILE BUILDING — approved 2026-09-14

| # | Decision | Why |
|---|---|---|
| D-L4.i1 | **A day's sales stay as rung up; a void counts on the day it is made.** `Day` counts every sale sold that day in *charged* (even if voided later) and every void made that day in *refunded*; takings = charged − refunded | a closed day's totals never change (§8.1, D-L4.14) — see R2 |
| D-L4.i2 | Discounts as §14.1: percent per line, amount on the sale, one guarded act at checkout | Q-L4.6 |
| D-L4.i3 | Change currency as §14.2; an empty tender is exact money | Q-L4.2, Q-L4.3 |
| D-L4.i4 | The cash note is a setting (1 … 1,000,000), set through `Till.SetCashNote` with the PIN, shown on the rate screen | Q-L4.1; beside the rate because both change what customers pay |
| D-L4.i5 | Till and sales DTOs carry **no cost** — held by `TestASaleCarriesNoCost` | Q-L2.4; cost reaches a screen with profit in L6 |
| D-L4.i6 | `Till.Scan` answers `found: false` for an unknown code; the till then searches by name | a mistyped code is not an error to translate |
| D-L4.i7 | A counted product added again goes up by one on its line — the only number the till changes itself, a whole count in BigInt; each weighing is its own line; a line with a discount typed is not merged | the scanner's rhythm, without arithmetic on money or weights (D9) |
| D-L4.i8 | Every `Act*` constant must have an `owner.action.<act>` label in both catalogs (`TestEveryGuardedActIsNamedInTheOwnersHistory`) | the till's three acts would otherwise have shown as raw keys; seen failing |
| D-L4.i9 | `ui/Field.TextField` forwards its ref; `CellInput` for inputs in table cells | the scan field keeps focus; cart cells need names without visible labels |
| D-L4.i10 | `bootstrap.TillWithStock` (test-only export) builds the till with a wrapped stock port | to fail a real transaction after a real stock movement |
| D-L4.i11 | The seeder's beyond-stock sale is white cheese (6.5 kg of 6.25), not labneh | the seeded shop has 17 kg of labneh; a 17.5 kg sale is not believable |

## 17. FINDINGS

**R1 — the till was wired before the stock service existed.** `bootstrap` built `sales` with `salesStock{stock: app.Stock}`
one line before `app.Stock` was assigned. Every fake-based test passed; the first real-graph test
(`TestTheTillReachesTheRealModules`) panicked on the nil service. The till is now built last, with a comment saying why.

**R2 — `Day` changed a closed day retroactively.** A sale voided the next day disappeared from its own day's *charged*,
contradicting §8.1 and D-L4.14. Found while mapping tests to §12.1's contract names; fixed (D-L4.i1) and held by
`TestAVoidBelongsToTheDayItWasMade`.

**R3 — the drill "receipt number taken outside the transaction" survived as first planted.** The writer pool is one
connection, so whole checkouts serialise; a number read on the reader pool *inside* `tx.Do` is still consistent. The real
hazard is a number read **before** the transaction begins — re-planted that way, it is caught by
`TestConcurrentCheckoutsNeverShareAReceiptNumber` (eight concurrent checkouts on the real graph). §12.2's wording is
answered here rather than edited.

**R4 — the drill "stock movement in its own transaction" is caught by a hang, not an assertion.** A second transaction on
the single writer connection waits forever; the package fails at the 90 s timeout. Caught, but slowly; recorded honestly.

**R5 — three frontend drills survived first**, each a test that could not fail: Pay's no-rate guard was masked because Go
also refuses the quote; focus was checked only after typing into the scan field, never after a quick button; out-of-order
quote answers were untested. Three tests added; all three drills caught.

**R6 — §12.1's names, where the build differs:** `TestReceiptNumbersAreContinuousAndARolledBackCheckoutUsesNone` is covered by
`TestReceiptNumbersAreContinuous` (service), `TestACheckoutThatFailsAfterInsertingLeavesNothing` (SQLite),
`TestACheckoutThatFailsAfterTheStockMovementLeavesNothing` (real graph) and `TestConcurrentCheckoutsNeverShareAReceiptNumber`;
`TestVoidNeedsTheOwnerAndReturnsTheStockAtTheSnapshottedCost` is a real-graph test (the fake stock has no cost) beside the
service's `TestVoidNeedsTheOwnerAndReturnsTheStock`; `StoreContract` is `salestest.StoreContract`, run on fake and SQLite.

**R7 — golangci-lint found 10 issues** in new code (8 shadowed `err`, a `Close` not deferred, a switch), and ESLint 7
unused test parameters that stopped the first `make lite-ci`; all fixed.

**R8 — O12 closed.** The rebuild in the verified order ran through the real runner in `TestTheLedgerRebuildKeepsEveryRow`
and on a real L3 shop (§18).

## 18. EVIDENCE

| Check | Result |
|---|---|
| `make lite-ci` | **pass** — nothing NOT RUN (§18.1) |
| Go tests (race) | **328** Lite test functions, **580** with subtests, 0 failures (L3: 268 / 499) |
| Frontend | **317** tests in 24 files, 0 console warnings; bundle gate 4 (L3: 256) |
| golangci-lint v2 | 0 issues |
| archlint | clean; **39** rules seen failing |
| Windows | `Mizan Lite.exe` (GUI) and `lite-demoseed.exe` (console) built — not run (O1) |
| **Mizan after L4's archlint change** (`arch-rules.yml` is shared) | `scripts/check.sh` green: 95 Go packages, 337 frontend tests, 0 lint issues |

**The packaged macOS app, run for real (2026-09-14):**

- **An L3 shop upgraded:** a copy of `l3 seeded محمد #3` (schema 4; 47 ledger rows, 40 levels) opened in the packaged L4
  app — pre-migration backup written, `migration applied version 5 "till"` in 2 ms, ready at schema 5, the frontend
  reached Go, quit with no `-wal`/`-shm` left. Against the pre-migration backup, **all 20 original columns of all 47 ledger
  rows are identical**, the 40 levels identical, the two new columns NULL; `PRAGMA integrity_check` ok,
  `foreign_key_check` empty. On a further copy a sale of three products (63,000 SYP) and its void went through, and both
  verifiers found nothing.
- **The seeder's day at the till** into `l4 seeded محمد #4`: 40 products, stock value 1370.24 USD, **10 sales, 1 voided,
  taken 1,179,000 SYP and 16.25 USD**; 15 `sale` and 2 `sale_void` movements; white cheese at −0.250 kg; a second run
  refused. Opened in the packaged app: ready at schema 5, the frontend reached Go, the rate job `held_mode`, quit clean.

### 18.1 Final runs

After the last change to code and documents, 2026-09-14: `make lite-ci` — wails generate, gofmt, vet, Windows and Intel-Mac
cross-compile, archlint and its 39 planted drills, race tests, golangci-lint v2 (0 issues), ESLint, typecheck, 317 frontend
tests and gates, production build, G5 on the bundle — **every step passed, none NOT RUN**. Mizan's `scripts/check.sh` —
**all local checks passed**.

## 19. MUTATION DRILLS — 23, ALL CAUGHT

| # | Planted defect | Caught by |
|---|---|---|
| G1 | a line's pounds computed from its rounded dollars | `TestALineIsPricedInBothCurrenciesFromOneExactProduct` |
| G2 | the rounding left off the sale | `TestCashRoundingIsRecordedAndHalfUp`, `TestPrintedLinesAddUpToThePrintedTotalExactly` |
| G3 | change computed from an inverted rate | `TestChangeIsInPoundsUnlessEverythingIsInDollars`, `TestATenderBelowTheTotalIsRefused` and three more |
| G4 | the token check removed | `TestCheckoutRefusesAStaleQuoteAndWritesNothing`, `TestARateChangedAfterTheQuoteRefusesTheCheckout`, `TestTheTillThroughTheBindings` |
| G5 | the receipt number read before the transaction | `TestConcurrentCheckoutsNeverShareAReceiptNumber` — **survived first as planted inside it** (R3) |
| G6 | the stock movement in its own transaction | `internal/lite/bootstrap` — **by a hang at the 90 s timeout** (R4) |
| G7 | a sale refused below zero | `TestASaleMayTakeStockBelowZero`, `TestANeverReceivedProductSellsBelowZeroWithItsCostUnknown` and two more |
| G8 | a void at today's average instead of the snapshot | `TestAVoidAveragesInAtTheCostTheGoodsLeftAt`, `TestASaleAndItsVoidMoveStockAtTheSnapshottedCost`, `TestVoidNeedsTheOwnerAndReturnsTheStockAtTheSnapshottedCost` |
| G9 | a void without the guard | `TestVoidNeedsTheOwnerAndReturnsTheStock`, `TestTheTillThroughTheBindings` |
| G10 | the ledger rebuild in L2's original order | `TestTheLedgerRebuildKeepsEveryRow` |
| G11 | a receipt read from the catalogue instead of the snapshot | `TestAReceiptIsTheSaleAsRecordedNotTheCatalogueNow` |
| G12 | `credit` accepted | `TestCreditIsRefusedUntilCustomersExist`, `TestTheTillThroughTheBindings` |
| F1 | Pay sends no token | "pays with the token of the quote on screen…", "a quote gone stale…" |
| F2 | a stale quote not priced again | "a quote gone stale is priced again and Pay must be pressed again" |
| F3 | Pay enabled with no rate | "with no rate in force Pay stays disabled even if a quote answered" — **survived first** (R5) |
| F4 | a weighed product added without asking | "a weighed product from a quick button asks how much first" |
| F5 | the scan field losing focus after an add | "a counted product from a quick button… focus returns" — **survived first** (R5) |
| F6 | a discount paid without the owner flow | "a discount asks for the owner PIN at Pay and the sale is retried once" |
| F7 | an older quote's answer overwriting a newer one | "an answer to an older cart arriving late…" — **survived first** (R5) |
| F8 | a void without the owner flow | "voids a whole sale with the owner's PIN and a reason, and reloads the day" |
| F9 | the day not reloaded after a void | the same test |
| F10 | money shown unformatted | nine tests across till, sales and receipt |
| F11 | the cash note saved without the owner flow | "shows the smallest note and changes it with the owner's PIN" |
| A1–A9 | `sales` importing stock, catalog/domain, fx/domain; catalog, owner, settings, stock, fx and setup importing `sales` | archlint, on every run |
| I1 | an owner-history label removed | `TestEveryGuardedActIsNamedInTheOwnersHistory` |

## 20. NOT VERIFIED

1. **The Windows build has not run** (O1).
2. **The Till, Sales and receipt screens have not been looked at** in either language (O5) — only rendered in tests and
   reached through the packaged app's log.
3. **The stock verifier's and sales verifier's cost at scale** (O8): both ran on the seeded day (64 ledger rows, 10 sales)
   instantly; a year of sales is unmeasured.
4. **An 80 mm receipt on paper.** The layout is sized to 80 mm on screen; whether it prints so is L7's.

## 21. DEFINITION OF DONE

| Criterion (§12.3) | Status |
|---|---|
| `make lite-ci` green | ✅ |
| Every invariant drilled | ✅ 23 behaviour drills + 9 architecture + 1 gate; four strengthened after surviving (R3, R5) |
| The seeder's day of sales opens in the packaged app, both verifiers clean | ✅ by the log and the database; ⏳ looked at (§20.2) |
| An installation from L3 upgrades through the rebuild with its ledger intact | ✅ §18 |
| The Till and Sales screens looked at in Arabic and English | ⏳ owner (O5) |
| Windows `.exe` builds | ✅ built — not run |
| Mizan's `scripts/check.sh` green | ✅ |
| PROGRESS, DECISIONS and this document updated | ✅ |

### 21.1 At commit (2026-09-14)

The owner approved D-L4.i1–i11 — naming D-L4.i1, a void recorded on the day it is made so that a closed day's totals never
change — and the commit. O4, O7 (for `sales`) and O12 are closed in [../PROGRESS.md](../PROGRESS.md); O1, O5 and O8 remain.

**Next:** L5's design note — customers and debts (`L5_CUSTOMERS.md`).

