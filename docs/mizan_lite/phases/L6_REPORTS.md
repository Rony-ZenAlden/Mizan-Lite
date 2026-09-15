# Mizan Lite — Phase L6: reports — profit, stock value and cash (التقارير والأرباح والتدفق النقدي)

> **Status: COMPLETE — committed 2026-09-15.** The owner approved this note, its 18 decisions and six amendments, answered
> §14.2 (recorded in §15), and approved the fifteen decisions made while building (§17). §16–§22 are the implementation record.
> **Date:** 2026-09-14. **Base:** L5 (`97b7875`). **Design:** [../DESIGN.md](../DESIGN.md) C6 (profit when the pound
> moves), Q3 (expected profit is the stock on the shelf), Q7 (profit reports are the owner's), §9.4 (30 days of demo
> history), §10 (L6). Prior phases: [L2_STOCK.md](L2_STOCK.md) (the ledger's costs), [L3_RATES.md](L3_RATES.md) (rates and
> their business dates), [L4_TILL.md](L4_TILL.md) (what a sale records; D-L4.i1 — a void counts on its own day),
> [L5_CUSTOMERS.md](L5_CUSTOMERS.md) (credit, repayments, write-offs; §2.3 names what L6 owes it).

---

## 0. How to read this

| § | Content |
|---|---|
| 1 | Analysis — what the owner must be able to read, and ten things harder than they look, **two of them defects found in L4/L5** |
| 2 | Scope — in, out, the phases after, and six amendments to the approved design |
| 3 | **Profit** — revenue, cost and profit of a sale, in dollars and in pounds; the C6 table as a fixture |
| 4 | **Net profit** — stock losses, bad debts and expenses, and the rate of the business day |
| 5 | **Takings** — a day and a month; voids on their own day; credit sold and credit collected |
| 6 | **Stock value** — now, at any past date, a period's movements at cost, and the profit waiting on the shelf |
| 7 | **The cash drawer** — what should be in it, per currency; counts, expenses, withdrawals |
| 8 | The schema — `0007_cashbook.sql`, and why nothing else changes |
| 9 | Architecture — a read-only `reports` module, a `cashbook` module, and the facts each module supplies |
| 10 | Bindings and screens |
| 11 | Demo data — thirty days with a drifting rate |
| 12 | Tests, reconciliations, drills, Definition of Done |
| 13 | (reserved for the record) |
| 14 | **Decisions and questions for approval** |

---

## 1. ANALYSIS

### 1.1 What the owner must be able to read

1. **Did I make money today, this month, on this product?** In dollars — the currency the shop's costs are kept in — and
   in pounds at each sale's own rate, the two readings agreeing in sign (DESIGN C6).
2. **What is my stock worth**, now and on any past date, and **what would it earn** at today's prices (Q3)?
3. **What did the till take**, per day and per month, per currency — cash sales, credit sold, credit collected, voids on the
   day they happened.
4. **What should be in the drawer**, in pounds and in dollars, and **what was actually there** at closing.
5. **What did I lose** — goods spoiled, given away or missing at a count; debts forgiven; and, if the shop records them,
   **what it spent** (rent, electricity, wages).

### 1.2 Ten things harder than they look

**H1 — Profit in pounds has three readings, and one of them is false (C6).** Oil bought at **3.00 USD/L** when the rate was
13,000 and sold at **42,000 SYP/L** when it is 15,000:

| Reading | Revenue | Cost | Profit | True? |
|---|---|---|---|---|
| Dollars | 2.80 USD (at the sale's rate) | 3.00 USD | **−0.20 USD** | yes |
| Pounds at **the sale's** rate | 42,000 | 45,000 (3.00 × 15,000) | **−3,000 SYP** | yes — agrees in sign |
| Pounds with cost **frozen at purchase** | 42,000 | 39,000 (3.00 × 13,000) | **+3,000 SYP** | **no** — phantom profit |

A sale line already stores its cost in both currencies at the sale's rate (L4 `cost_usd_minor`, `cost_local_minor`). L6
reads those, never recomputes cost from a receipt's pounds, and holds this table as a test fixture.

**H2 — A line whose cost is unknown makes every margin wrong.** L4 sells a never-received product with `cost_known = 0` and
cost zero (Q-L4.8). Counted as it is, its whole price is "profit". Q-L6.2.

**H3 — A void on a later day.** D-L4.i1: a closed day never changes, and a void counts on its own day. So a day's profit is
*the sales rung up that day* less *the sales voided that day* — their revenue **and** their cost, since a void returns the
goods to stock at the cost they left at (L4 §5). A month's profit is the sum of its days, and a void in the next month
belongs to the next month.

**H4 — Profit on credit is not cash.** A credit sale's profit is earned when the goods leave (Q-L6.1); its cash arrives with
the repayments, maybe next month, maybe never (a write-off). The profit report and the drawer report must therefore be two
reports, and both must say which they are.

**H5 — Events with no rate of their own.** A sale, a repayment and a refund snapshot their rate. A stock write-off, a count
shortfall, a cost correction and a debt write-off do not (L2, L5). Converting them "at today's rate" would move last month's
losses every morning. L6 converts them at **the rate of their business day** — the newest rate recorded on or before that
day (`fx_rates.business_date`, highest `seq`) — which is fixed once the day is past, and never read from a clock (§4.3).

**H6 — Discounts and cash rounding belong to a sale, not a product.** A whole-sale discount (L4 §14.1) and the note rounding
are figures of the sale. A per-product report that spreads them would invent numbers; one that ignores them would not add
up to the day. Per-product figures use each line's net, and the report carries **one reconciling row** — *discounts on whole
sales and cash rounding* — so the products add up exactly to the day (§3.4).

**H7 — Stock value on a past date is not stored.** Only the value *now* is (`stock_levels`). But every ledger row keeps the
quantity and average cost **after** it (L2 A-L2.2), so the value on any date is the last row on or before that date, per
product — reconstructed from facts already recorded, not estimated (§6.2).

**H8 — Found in L4 and L5: a void's cash is recorded nowhere, and a credit sale's "paid now" has no defined fate.** A void
marks the sale and returns the stock; the money handed back to the customer is neither shown to the cashier nor recorded.
For a credit sale it is worse: voiding **receipt 12 in the seeded shop** (76,000 pounds, 20,000 paid now, 56,000 charged)
reversed the 56,000 charge and later refunded the customer's 30,000 repayment — and the **20,000 paid at the counter is
recorded nowhere**: not refunded, not credited, not counted as returned. Verified on `l5 seeded محمد #5`. L6 must decide
what a void returns (Q-L6.5), derive it from the sale — every figure needed is already recorded — and **tell the cashier the
amount in the void dialog** (A-L6.5). No stored row is wrong; the gap is the missing statement of what left the drawer.

**H9 — Found in L4: the Sales screen's "voids" amount is the value voided, not cash returned.** L4 adds a voided sale's whole
total — 76,000 for receipt 12 — although only 20,000 of it was ever cash. Right as a *sales* figure, wrong as a *cash* one.
The drawer report computes returns from §7.2's rule and never reuses it; the DTO field `refunded` is renamed `voided` so
nobody else mistakes it (A-L6.6).

**H10 — "Net profit" needs expenses the application does not have.** Rent, electricity and wages are not in any ledger. Without
them L6 can honestly report *profit before expenses*; with a small expense book (Q-L6.3), *net profit*. The note designs both
and lets the owner choose.

---

## 2. SCOPE

### 2.1 In L6

| Area | What |
|---|---|
| Profit | per day, per month, per product, for any date range; in dollars and in pounds at each sale's rate; lines of unknown cost shown apart; voids on their own day |
| Net profit | gross profit less stock losses (at cost), bad debts written off, and — if Q-L6.3 — expenses; each at its own rate or the rate of its business day |
| Takings | per day and month, per currency: cash sales, credit sold, credit collected, discounts, cash rounding, voids |
| Stock | value now and at any past date, in dollars and pounds; a period's movements at cost (received, sold, losses by reason, count gains, cost corrections) reconciled from opening to closing value; **expected profit on the shelf** (Q3) |
| Cash drawer | expected cash per currency for a day — from sales, repayments, refunds, void returns and the cash book — and, if Q-L6.4, the closing count and its difference, expenses paid from the drawer, withdrawals and deposits |
| The till | the void dialog says **how much to hand back**, in which currency (A-L6.5) |
| Screens | **Reports** (owner): Day, Month, Products, Stock; **Cash drawer** (`/cash`) |
| Demo data | thirty days of history with a drifting rate, deliveries, losses, debts, expenses and counts |

### 2.2 Not in L6

| Not here | Why | Where |
|---|---|---|
| Printing or exporting reports (PDF, CSV) | printing is one path for receipts, statements and reports | L7 (printing); export not planned |
| Supplier accounts, purchase invoices owed | DESIGN §3.4 | — |
| Tax | none was asked for | — |
| Budgets, forecasts, charts | numbers first; a chart of a wrong number is worse than the number | — |
| Several drawers or shifts | one counter, one operator (DESIGN Q7) | — |
| A balance sheet or double entry | DESIGN §3.2 | — |

### 2.3 What remains after L6

| Phase | Covers |
|---|---|
| **L7 — Printing and backups** | receipts (80 mm, Q-L4.9), statements and reports printed; a backups screen |
| **L8 — Release** | installers, the first Windows run (O1), low-end measurements (O6, O8), a pilot shop for one week |

### 2.4 Six amendments to the approved design

**A-L6.1 — "a daily cash summary per payment kind" becomes a cash drawer per currency.** DESIGN §10's payment kinds
(`cash_local`, `cash_usd`) no longer exist (L4 A-L4.4): a sale records the currencies handed over and given back. Cash is
summed by the currency it physically moved in.

**A-L6.2 — a cash book** (`0007_cashbook.sql`): expenses, withdrawals, deposits and closing counts, if Q-L6.3 and Q-L6.4 are
answered yes. DESIGN had no table for them.

**A-L6.3 — the rate of the business day** converts events that snapshot no rate (H5).

**A-L6.4 — reports are computed in Go from facts each module supplies**, not by one SQL module reading every table (§9).

**A-L6.5 — the void dialog states the cash to hand back** (H8). A change to L4's screen, not its schema.

**A-L6.6 — the Sales screen's `refunded` becomes `voided`** (H9): the same figure, named for what it is.

---

## 3. PROFIT

### 3.1 A sale's revenue, cost and profit

Every figure is read from what the sale stored; nothing is re-priced.

| Figure | In dollars | In pounds |
|---|---|---|
| **Lines** | Σ line `gross − discount` (USD) | Σ line `gross − discount` (local) |
| less **whole-sale discount** | `discount_usd_minor` | `discount_local_minor` |
| plus **cash rounding** | 0 (dollars are not cash-rounded) | `rounding_minor` when charged in pounds |
| = **Revenue** | | |
| **Cost** | Σ line `cost_usd_minor` (known cost only) | Σ line `cost_local_minor` (at the sale's rate) |
| = **Gross profit** | revenue − cost | revenue − cost |

A pounds sale's cash rounding is revenue in pounds; the dollar reading leaves it out and the report shows it as its own
line in pounds — at most half a note a sale, never converted (§3.4).

### 3.2 Lines of unknown cost (H2, Q-L6.2)

*Recommended:* a line with `cost_known = 0` is **left out of revenue, cost and margin** and shown apart: *"3 lines,
48,000 SYP / 3.20 USD, sold with no recorded cost — not in profit"*. The owner sees the gap and can fix its cause (an
opening or a delivery never entered). The alternative counts their revenue at zero cost.

### 3.3 A day, a month, a range (H3)

For business dates *D*:

```
gross profit(D) = Σ profit of sales rung up on D  −  Σ profit of sales voided on D
```

A month is the sum of its days; any range is the sum of its days. The day a sale was rung up keeps it; the day of its void
takes it back. Credit sales count when sold (Q-L6.1).

### 3.4 Per product, adding up exactly (H6)

For a range: per product, quantity sold (net of voided lines), revenue, cost, gross profit and margin, in both currencies,
from line nets. Then:

| Row | |
|---|---|
| Σ products | from line nets |
| **Discounts on whole sales** | − Σ `discount_*` of the range's sales, net of voids |
| **Cash rounding** (pounds) | Σ `rounding_minor`, net of voids |
| **= the range's revenue** | equal to §3.3's, **exactly** — a test holds it |

Margin is `profit ÷ revenue`, computed exactly and shown to one decimal, half up; empty when revenue is zero.

### 3.5 The C6 fixture

§1.2 H1's table is a test through the real modules: a receipt of oil at 3.00 USD entered at 13,000, the rate set to 15,000,
a sale at 42,000 SYP. The day's report must show **−0.20 USD** and **−3,000 SYP**, and no figure anywhere may show +3,000.

---

## 4. NET PROFIT

### 4.1 From gross to net

```
net profit = gross profit
           − stock losses          (§4.2)
           + stock count gains     (§4.2)
           − bad debts             (debt write-offs, L5 §7.2)
           − expenses              (if Q-L6.3, §7.3)
```

In both readings. Opening stock and opening debts are **not** profit (DESIGN §5); a cost correction is a **revaluation** of
stock, shown in the stock report (§6.3), not profit; a package opening moves cost between two products and nets to zero.

### 4.2 Stock losses and gains, at cost

From the stock ledger, per business date, at each row's `unit_cost_usd_micro` (the average when it moved, L2):

| Row kind | Effect | Shown as |
|---|---|---|
| `adjustment` out — `damaged`, `expired` | loss | *spoiled* |
| `adjustment` out — `own_use`, `gift` | cost of goods not sold | *own use and gifts* |
| `adjustment` out — `other` | loss | *other write-offs* |
| `count` lower | loss | *count shortfall* |
| `count` higher, `adjustment` in | gain | *count surplus* |

Each row's value is rounded once to the cent (`quantity × unit cost`, half up) and converted to pounds at the rate of its
business day (§4.3), rounded once per row.

### 4.3 The rate of the business day (H5, A-L6.3)

For a business date *D*: the `fx_rates` row with the highest `seq` among those whose `business_date ≤ D`. Deterministic,
stamped at write, independent of the reading machine's clock. With no such rate (a shop upgraded from L2 that sold nothing
before its first rate), the pounds reading of those rows is shown as *not converted* rather than guessed.

### 4.4 Bad debts

A write-off is in its debt's currency; the other reading converts it at the rate of its business day. Reversing a
write-off (L5 §7.4) gives it back on the reversal's day.

---

## 5. TAKINGS

A day and a month, per currency — what the till did, independent of cost:

| Line | From |
|---|---|
| Sales rung up (count, charged) | `sales` by `business_date` |
| of which **on credit** (added to debts) | the charges (L5) |
| **Discounts** given (line and whole-sale) | `sale_lines`, `sales` |
| **Cash rounding** | `sales.rounding_minor` |
| **Voids** made that day (count, value) | `sales` by `void_business_date` |
| **Credit collected** | debt payments settled, by the debt's currency |
| **Bad debts** written off | debt write-offs |

This is L4's *Sales* totals and L5's *today's debt book*, for any day or month, in one place — owner only when shown beside
profit; the Sales screen keeps its own counter view.

---

## 6. STOCK VALUE

### 6.1 Now

L2's valuation, as it is (owner only): per product and in total, in dollars and in pounds at the rate in force.

### 6.2 On a past date (H7)

For each product, the ledger row with the highest `seq` whose `business_date ≤ D`: its `on_hand_after × avg_cost_after`,
rounded once to the cent; **negative stock counts as zero value**; a product whose cost is unknown is listed apart. Pounds at
the rate of that business day.

### 6.3 A period's movements, reconciled

For a range [from, to], in dollars at cost:

| | |
|---|---|
| **Value at the start** (the day before *from*) | §6.2 |
| + received (openings, deliveries, less reversed deliveries) | ledger rows × their unit cost |
| − sold (sales less voids) | the lines' `cost_usd_minor` |
| − losses, + gains (§4.2) | |
| ± cost corrections (revaluation) | `(avg after − avg before) × on hand` |
| ± packages opened (nets to zero) | |
| **= expected value at the end** | |
| **Value at the end** (§6.2) | |
| **Difference** | shown, **with its causes named**: per-row rounding (bounded), sales beyond stock (their cost left the value below zero), lines of unknown cost |

A test runs random ledgers through the real services and requires the difference to be within the rounding bound whenever
no stock went below zero and every cost was known.

### 6.4 Expected profit on the shelf (Q3)

For every **active** product with stock above zero and a known cost:

```
expected profit = on hand × (list price in dollars − average cost)
```

A pounds price converts at the rate in force (as the product screen shows it, L3 §3.3), rounded once per product; the
pounds reading converts the dollar figure the same way. Products left out — no cost, no stock, inactive, priced below
cost — are counted and named, never silently dropped.

---

## 7. THE CASH DRAWER

### 7.1 What should be in it, per currency

For a business date *D* and each currency *c*:

```
expected(D, c) = the count closing the day before (or 0 if the drawer was never counted)
               + cash sales:        Σ tendered in c − Σ change in c
               + credit paid now:    Σ tendered in c
               + debt repayments:    Σ tendered in c − Σ change in c
               − debt refunds:       Σ paid out in c
               − void returns:       §7.2
               − expenses paid from the drawer, − withdrawals, + deposits   (§7.3)
```

Every term is money that physically moved in *c*, read from the sale, the debt entry or the cash book. Nothing is converted:
dollars handed over for a pounds total are **dollars in** and the change is **pounds out** (L4 §3.3).

### 7.2 What a void returns (H8, Q-L6.5)

*Recommended:* **the money the receipt says was paid, in the currency the receipt was charged in** — for a cash sale its
total; for a credit sale what was paid now, as its value in the charged currency (total − the debt it added). Derived, not
stored: every voided sale since L4 is reported the same way. The void dialog shows it before the PIN: *"Hand back 20,000
SYP."* The alternative — handing back the exact notes taken and taking back the change — is exact but rarely what happens at a
counter.

### 7.3 The cash book (Q-L6.3, Q-L6.4)

| Kind | What | Who |
|---|---|---|
| `expense` | money spent — category *rent, electricity, wages, transport, supplies, other* — from the drawer or not | owner |
| `withdrawal` | money the owner takes out of the drawer | owner |
| `deposit` | money put in — a float, change from the bank | owner |
| `count` | what was counted at closing, per currency; the expected figure and the difference are recorded with it | anyone at the counter |
| `reversal` | undoes one mistaken entry, with a reason | owner |

An expense **not** paid from the drawer (rent paid from a bank) counts in net profit but not in the drawer. A count closes
its business day: the next day's expected cash starts from it, so a difference is found once, on the day it happened, and
does not carry forward. The newest count of a day and currency is the day's count.

---

## 8. THE SCHEMA — `0007_cashbook.sql`

Profit, takings and stock value need **no new column**: every figure in §3–§6 is already recorded. Only the cash book is new
(if Q-L6.3 and Q-L6.4 are yes):

```sql
CREATE TABLE cash_entries (
  id                  CHAR(36)     NOT NULL PRIMARY KEY,
  seq                 BIGINT       NOT NULL CHECK (seq >= 1),                    -- the book's order, never the clock
  business_date       CHAR(10)     NOT NULL,
  occurred_at         CHAR(24)     NOT NULL,
  kind                VARCHAR(10)  NOT NULL CHECK (kind IN ('expense', 'withdrawal', 'deposit', 'count', 'reversal')),
  currency            CHAR(3)      NOT NULL REFERENCES currencies(code),
  amount_minor        BIGINT       NOT NULL,          -- money: > 0; a count: what was counted, >= 0
  expected_minor      BIGINT,                         -- a count: what the report expected when it was counted
  category            VARCHAR(12),                    -- an expense: rent, electricity, wages, transport, supplies, other
  from_drawer         SMALLINT     CHECK (from_drawer IN (0, 1)),
  reverses_id         CHAR(36)     REFERENCES cash_entries(id),
  fx_rate_id          CHAR(36)     REFERENCES fx_rates(id),
  local_per_usd_nano  BIGINT       CHECK (local_per_usd_nano IS NULL OR local_per_usd_nano > 0),
  note                VARCHAR(200),
  created_at          CHAR(24)     NOT NULL,
  CONSTRAINT ux_cash_entries_seq      UNIQUE (seq),
  CONSTRAINT ux_cash_entries_reverses UNIQUE (reverses_id),
  CONSTRAINT ck_cash_money_is_positive CHECK (kind = 'count' OR amount_minor > 0),
  CONSTRAINT ck_cash_count_is_complete CHECK (
    (kind = 'count' AND amount_minor >= 0 AND expected_minor IS NOT NULL) OR (kind <> 'count' AND expected_minor IS NULL)),
  CONSTRAINT ck_cash_expense_is_complete CHECK (
    (kind = 'expense' AND category IS NOT NULL
       AND category IN ('rent', 'electricity', 'wages', 'transport', 'supplies', 'other') AND from_drawer IS NOT NULL) OR
    (kind <> 'expense' AND category IS NULL AND from_drawer IS NULL)),
  CONSTRAINT ck_cash_reversal_names_its_entry CHECK ((kind = 'reversal') = (reverses_id IS NOT NULL)),
  CONSTRAINT ck_cash_reversal_has_a_reason CHECK (kind <> 'reversal' OR (note IS NOT NULL AND length(trim(note)) > 0)),
  CONSTRAINT ck_cash_rate_on_money CHECK (
    (kind IN ('expense', 'withdrawal', 'deposit') AND fx_rate_id IS NOT NULL AND local_per_usd_nano IS NOT NULL) OR
    (kind IN ('count', 'reversal') AND fx_rate_id IS NULL AND local_per_usd_nano IS NULL))
);
CREATE INDEX ix_cash_entries_day ON cash_entries (business_date, currency);
```

Every nullable comparison is guarded (PROGRESS O7's lesson). An expense snapshots its rate, so net profit in the other
currency never needs §4.3 for it.

### 8.1 Verified before this note

The DDL above was applied, in one transaction with foreign keys on, to a **copy of the L5 seeded shop's database**
(`l5 seeded محمد #5`) on 2026-09-14:

- **6 valid rows accepted:** an expense paid from the bank, one from the drawer, a withdrawal, a dollar deposit, a count of
  zero, a reversal with its reason.
- **22 impossible rows refused, each by the constraint named** — a zero expense; a negative withdrawal; a count with no
  expected figure (NULL) and a negative count; an expense carrying an expected figure; an expense with no category (NULL), an
  unknown category, no *from drawer* (NULL), and `from_drawer = 2`; a withdrawal with a category; a reversal of nothing, with
  no reason (NULL) and with a blank one; an expense with no rate (NULL) and a NULL rate value; a zero rate; a count carrying a
  rate; an unknown kind and currency; `seq` 0; a place taken; an entry reversed twice.

---

## 9. ARCHITECTURE

### 9.1 A read-only `reports` module

```
internal/lite/reports/
├── domain/     pure: Profit, Takings, StockValue, Reconcile, ShelfProfit, Drawer, RateOfDay, Margin — from facts, no I/O
├── service.go  Day, Month, Range, Products, StockAt, StockMovements, Shelf, Drawer — owner-guarded except Drawer
└── reportstest fakes of every facts port, and fixtures (C6, the seeded day)
```

**It owns no table and writes nothing.** Each module supplies its own facts through a port `reports` declares, implemented
by that module's service and store — so SQL stays with the table's owner (A-L6.4):

| Port | Supplied by | Facts |
|---|---|---|
| `Sales` | sales | sales rung up or voided in a range, with lines; each credit sale's charge amount |
| `Stock` | stock | ledger rows in a range; each product's last row on or before a date |
| `Debts` | customers | debt entries in a range |
| `Rates` | fx | rates in `seq` order with their business dates; the rate in force |
| `Catalogue` | catalog | products: names, units, prices, active |
| `CashBook` | cashbook | cash entries in a range; the last count before a date |
| `OwnerGate` | owner | `Allowed` |

Aggregation happens in Go over the range's facts: one implementation of each rule, testable against fakes, with integer
sums of stored minor units and one rounding per converted row. §12 measures it at a year of sales before it is accepted —
if a year is too slow, the facts ports gain summed queries and the domain stays as it is.

### 9.2 A `cashbook` module

Owns `cash_entries`: record an expense, withdrawal, deposit or count, reverse one, list a range. Its ports: `Rates` (the rate
snapshot), `OwnerGate`, and — for a count's *expected* figure — the reports service's drawer computation, reached through a
port the composition root satisfies.

### 9.3 Rules

| Rule | Holds |
|---|---|
| `lite-reports-isolated` | `reports` imports no module; only ports |
| `lite-cashbook-isolated` | `cashbook` imports no module |
| every other isolation rule | forbids `reports` and `cashbook` |
| `lite-reports-read-only` (new kind of check) | a test scans `reports` for `Insert`, `Update`, `Record`, `Void` calls on its ports and finds none |

---

## 10. BINDINGS AND SCREENS

### 10.1 Bindings

| Façade.Method | Guard | Caller |
|---|---|---|
| **`Reports.Day`** (date) — profit, net profit, takings, losses, bad debts, expenses, unknown-cost lines | owner | ReportsScreen · Day |
| **`Reports.Month`** (YYYY-MM) — a row per day and the month's totals | owner | ReportsScreen · Month |
| **`Reports.Products`** (from, to) — per product, with the reconciling row | owner | ReportsScreen · Products |
| **`Reports.Stock`** (date) — value on that date, the period's reconciled movements, expected profit on the shelf | owner | ReportsScreen · Stock |
| **`Cash.Drawer`** (date) — expected per currency, its terms, the day's cash book, the count and difference | counter (Q-L6.7) | CashScreen |
| **`Cash.Record`** (expense, withdrawal or deposit) | owner | CashScreen |
| **`Cash.Count`** (currency, counted) | counter | CashScreen |
| **`Cash.Reverse`** (entry, reason) | owner | CashScreen |
| `Sales.Receipt` | — | gains `voidReturn` and its currency (A-L6.5) |
| `Sales.List` | — | `refunded` renamed `voided` (A-L6.6) |

**8 new methods, 70 in total.** Every figure a string formatted by Go; every report states the rate each pounds figure was
converted at.

### 10.2 Screens

- **Reports (`/reports`, owner):** tabs *Day · Month · Products · Stock*.
  - *Day* — a statement read top to bottom: revenue, cost, **gross profit**, losses, bad debts, expenses, **net profit**, in
    two columns, *Dollars* and *Pounds at each sale's rate*; below, takings; a banner for lines of unknown cost.
  - *Month* — one row per day (sales, revenue, gross profit, net profit, both currencies) and the totals; a day opens *Day*.
  - *Products* — sortable by profit, quantity or margin; the reconciling row last.
  - *Stock* — value on a date, the period's movements from opening to closing value with any difference and its causes,
    and the profit waiting on the shelf with what was left out.
- **Cash drawer (`/cash`):** for today (or a date), a card per currency — *expected*, how it was made up, *counted*,
  *difference* — the day's cash book, and *Count*, *Expense*, *Withdrawal*, *Deposit*.
- **Receipt:** the void form reads *"Hand back 20,000 SYP"* (A-L6.5).
- **Sales:** *Voids* shows the value voided, as today, under its right name.

---

## 11. DEMO DATA

`lite-demoseed` builds **thirty days of history** before today, running the services under a clock the seeder steps day by
day (the graph already takes an injected clock): the owner's rate each morning drifting from 14,200 to 15,600 with one
same-day correction; a delivery a week; ten to twenty sales a day from the catalogue's pattern, some weighed, some beyond
stock, two or three voids; the paper book adopted on day one and credit and repayments through the month; a spoiled-labneh
write-off and a count shortfall; rent and electricity once, transport weekly; a closing count most days, one with a
difference. Today is L4's and L5's day, as now. Then every verifier runs **and every reconciliation of §12.2**, and the
seeder fails on any finding — so the USD and pounds profits visibly diverge across the month, as DESIGN §9.4 asked.

---

## 12. TESTS, RECONCILIATIONS, DRILLS, DEFINITION OF DONE

### 12.1 Tests (names are the contract)

| Layer | Tests |
|---|---|
| **reports/domain** | `TestTheC6TableIsTheReportsTable` · `TestAVoidTakesItsProfitBackOnItsOwnDay` · `TestLinesOfUnknownCostAreShownApart` · `TestProductsAddUpToTheDayExactly` · `TestMarginIsExactToOneDecimal` · `TestTheRateOfTheBusinessDayIsNewestOnOrBeforeIt` · `TestLossesAndGainsAtTheCostTheyMovedAt` · `TestStockValueOnAPastDateIsTheLastRowOnOrBeforeIt` · `TestNegativeStockHasNoValue` · `TestShelfProfitNamesWhatItLeavesOut` · `TestTheDrawerSumsMoneyInTheCurrencyItMovedIn` · `TestAVoidReturnsWhatTheReceiptSaysWasPaid` · `TestACountClosesItsDay` |
| **reconciliations** (property, through the real services on SQLite) | `TestAMonthIsTheSumOfItsDays` · `TestStockMovementsReconcileOpeningToClosingValue` (no negative stock, known costs: difference within the rounding bound) · `TestTheDrawerAgreesWithEveryCashMovement` · `TestNetProfitDollarsAndPoundsAgreeInSignForASingleRate` |
| **cashbook** (fake + contract) | `StoreContract` · `TestMoneyEntriesNeedTheOwner` · `TestACountNeedsNoPIN` · `TestAnEntryIsReversedOnceWithAReason` · `TestAnExpenseSnapshotsItsRate` |
| **the database** | `TestACheckRefusesAnImpossibleCashEntry` (every §8 constraint, by name, NULL cases) · `TestCashEntriesAreInsertOnly` · `TestTheMigrationAddsNothingToExistingTables` |
| **performance** | `TestAYearOfSalesReportsInTime` — 36,500 sales and 110,000 lines: a month under 300 ms, a year of products under 2 s (bounds × 10 under `-race`) |
| **bindings** | every figure a string · reports refused outside owner mode · the drawer visible to the counter · `refunded` gone from the wire |
| **frontend** | the day's statement reads top to bottom in both currencies · unknown-cost banner · month rows open a day · products sort and reconcile · stock difference names its causes · the drawer's terms and difference · count without PIN, expense through PIN · the void dialog says what to hand back · both languages |
| **seeder** | §11: thirty days, every verifier and reconciliation clean |

### 12.2 Drills planned

At least: pounds cost frozen at purchase (the C6 fixture fails); a void's profit taken back on the sale's day; unknown-cost
revenue counted as profit; the reconciling row left out (products stop adding up); losses converted at today's rate
instead of the business day's; a rate of the day read by clock instead of `seq`; negative stock valued below zero; a cost
correction counted as profit; dollars handed over for a pounds total added to the pounds drawer; a void return in the
tender's currency; a count carried into the next day's difference; an expense without the guard; a report readable outside
owner mode; `reports` calling a writing method (the scan); `reports` importing `sales` (archlint).

### 12.3 Definition of Done

> `make lite-ci` green · every rule drilled and every reconciliation holding on the seeded month · the C6 fixture passes
> through the real modules · a year of sales within §12.1's bounds · an installation from L5 upgrades with every row intact ·
> the Reports and Cash screens looked at in Arabic and English (owner, O5) · Windows `.exe` builds · Mizan's `scripts/check.sh`
> green if shared code changes · [../PROGRESS.md](../PROGRESS.md), [../DECISIONS.md](../DECISIONS.md) and this document updated.

---

## 13. (RESERVED)

The at-approval section (§15) and the implementation record (§16–§22) follow §14.

---

## 14. DECISIONS AND QUESTIONS FOR APPROVAL

### 14.1 Decisions — approve, amend, or reject

| # | Decision | § |
|---|---|---|
| D-L6.1 | Profit is read from what each sale stored — lines' nets, whole-sale discount, rounding, and cost in both currencies at the sale's rate — never re-priced; the C6 table is a fixture | 3.1, 3.5 |
| D-L6.2 | Dollars are the primary reading; pounds at each sale's own rate beside them; a pounds sale's cash rounding is pounds revenue only | 3.1 |
| D-L6.3 | A day's profit is the sales rung up that day less the sales voided that day; a month or range is the sum of its days | 3.3 |
| D-L6.4 | Per-product figures from line nets, with one reconciling row for whole-sale discounts and rounding, adding up exactly | 3.4 |
| D-L6.5 | Net profit = gross profit − stock losses + count gains − bad debts − expenses; openings are not profit; cost corrections are revaluations; package openings net to zero | 4.1 |
| D-L6.6 | Stock losses and gains at the unit cost each ledger row moved at, by reason | 4.2 |
| D-L6.7 | Events without a rate convert at the rate of their business day (highest `seq` with `business_date ≤ D`), never today's, never by clock | 4.3, A-L6.3 |
| D-L6.8 | Takings per day and month per currency: sold, on credit, discounts, rounding, voids, collected, written off | 5 |
| D-L6.9 | Stock value on a past date from each product's last ledger row on or before it; negative stock has no value | 6.2 |
| D-L6.10 | A period's stock movements reconciled from opening to closing value, the difference shown with its causes | 6.3 |
| D-L6.11 | Expected profit on the shelf per Q3, naming every product it leaves out | 6.4 |
| D-L6.12 | The drawer per currency sums money in the currency it moved in; nothing converted | 7.1, A-L6.1 |
| D-L6.13 | A void's cash return is derived from the sale and shown in the void dialog (the answer to Q-L6.5) | 7.2, A-L6.5 |
| D-L6.14 | A cash book of expenses, withdrawals, deposits, counts and reversals, insert-only, ordered by `seq`, each money entry snapshotting its rate (if Q-L6.3/Q-L6.4) | 7.3, 8 |
| D-L6.15 | A count closes its day; the next day's expected cash starts from it | 7.3 |
| D-L6.16 | A read-only `reports` module computing in Go from facts each module supplies through ports; a `cashbook` module; both isolated | 9 |
| D-L6.17 | `refunded` on the Sales screen renamed `voided` | 1.2 H9, A-L6.6 |
| D-L6.18 | A year of sales within stated bounds before L6 is done; summed fact queries only if measured too slow | 9.1, 12.1 |

### 14.2 Questions only you can answer

**Q-L6.1 — When does a credit sale's profit count: when the goods are sold, or when the customer pays?** *Recommended: when
sold* — the goods have left the shelf and their cost is spent; the drawer report shows the cash separately, and a debt that
is never paid comes off as a bad debt when you write it off.

**Q-L6.2 — Sales of goods whose cost was never recorded?** *Recommended: shown apart and left out of profit and margins*, with
a banner naming them, so a missing delivery entry cannot look like profit. The alternative counts them at zero cost.

**Q-L6.3 — Should the shop record its expenses (rent, electricity, wages, transport) in the app?** *Recommended: yes, with your
PIN* — without them the reports can only show *profit before expenses*. Each expense has a category, a currency, and whether
it was paid from the drawer.

**Q-L6.4 — Should the app keep the cash drawer: expected cash per currency, a count at closing with its difference, and money
you take out or put in?** *Recommended: yes* — a drawer that is never counted cannot show a shortage.

**Q-L6.5 — When a sale is voided, what does the cashier hand back?** *Recommended: what the receipt says was paid, in the
currency the receipt was charged in* — a cash sale's total; a credit sale's *paid now*. The void dialog will say it. This
also settles what happens to money paid at the counter on a credit sale that is later voided (H8), which today is recorded
nowhere.

**Q-L6.6 — Which currency leads the reports?** *Recommended: dollars first, pounds at each sale's rate beside them* — dollars
are what the shop's costs are kept in, so dollar profit is the one that cannot be inflated by the pound falling (C6).

**Q-L6.7 — Who may see what?** *Recommended:* profit, costs, stock value and expenses — **owner only** (Q7); the drawer's
expected cash and the closing count — **anyone at the counter**, because the cashier counts it. The alternative puts the drawer
behind the PIN too.

**Q-L6.8 — What is a month?** *Recommended: the calendar month, by the shop's business date.* Say so if the shop closes its
books on another day (for example the 25th).

**Q-L6.9 — Printing or exporting reports?** *Recommended: on screen in L6; printed with receipts and statements in L7; no
spreadsheet export in v1.*

---

## 15. AT APPROVAL (2026-09-14)

The owner approved this note, D-L6.1–18 and A-L6.1–6, and answered §14.2:

| Q | The owner's answer | In the build |
|---|---|---|
| Q-L6.1 | Credit sale profit counts **when the goods are sold** | as recommended; a credit sale is in its day's profit like a cash sale |
| Q-L6.2 | Items without a recorded cost **shown separately**, excluded from profit and margin | as recommended; the Day banner, the Products column *sold with no cost*; `TestLinesOfUnknownCostAreShownApart` |
| Q-L6.3 | Expenses **recorded in the app**, owner PIN | `Cash.Record`, `cashbook.expense` in the owner's history |
| Q-L6.4 | **Track expected cash, closing counts and the variance** | the Cash drawer screen; a count records its expected figure and difference |
| Q-L6.5 | **Always clearly state the exact cash to hand back on a void**, matching what was actually paid at the counter | the void form reads *Hand back 30,000 SYP* before the PIN — see D-L6.i2 for the rule as built |
| Q-L6.6 | **Dollars first**, pounds beside at each sale's historic rate | every report's first column |
| Q-L6.7 | Profit, margins, item costs, stock values and expenses **owner only**; the basic cash count **at the counter** | reports refused outside owner mode; the drawer and the count open to the counter, expenses shown there only as *taken out by the owner* (D-L6.i4) |
| Q-L6.8 | **Calendar months** by business date | `Reports.Month(YYYY-MM)` |
| Q-L6.9 | **Screen only**; printing in L7; no export | as recommended |

**Q-L6.5, as interpreted.** "What was actually paid at the counter" is read as the receipt's own figures: a cash sale hands
back its total; a credit sale hands back what was paid now — the total less the debt it added — both **in the currency the
sale was charged in**. The dollar note a customer handed over for a pounds total is not what is handed back: *30,000 SYP*,
not *$2*. Say so if the counter hands back the notes themselves; it is one function, `Sale.VoidReturn()`.

---

## 16. WHAT WAS BUILT

| Layer | What |
|---|---|
| **Migration** | `0007_cashbook.sql`: `cash_entries` (§8, with D-L6.i1); no existing table changes (`TestTheMigrationAddsNothingToExistingTables`) |
| **cashbook** (new module) | domain: `NewMoney`, `NewCount`, `Reverse`, `ParseAmount`, `Difference`; service: `Record` (owner), `Count` (counter), `Reverse` (owner), `Between`, `LastCountBefore`, `Entry`; ports `Rates`, `Currencies`, `OwnerGate`, `Expected`; `cashbooktest` fake and `StoreContract` on fake and SQLite; SQLite store, insert-only |
| **reports** (new module, read-only) | domain: `SaleProfit`, `ProfitOn`, `ProductsOver`, `LossesOn`, `BadDebtsOn`, `ExpensesOn`, `TakingsOn`, `DayOf`, `MonthOf`, `Total`, `RateOfDay`, `Margin`, `ValueOn`, `Reconcile`, `ShelfProfit`, `DrawerOn`, `DrawerOf`; service: `Day`, `Month`, `Products`, `Stock`, `Drawer`, `ExpectedCash`, `Today`, `Pair`; seven facts ports; `reportstest` facts |
| **sales** | `Sale.VoidReturn()`; `Store.Range`, `Service.Facts`; `RefundedMinor` → `VoidedMinor` |
| **stock** | `Store.Between`, `Store.LastOnOrBefore` and their service reads; both in the store contract |
| **fx / customers** | `AllRates` (place order, business dates); `EntriesBetween` with each reversal's entry |
| **bootstrap** | the cash book and the reports built last; six facts adapters, the cash book's lazy `Expected` adapter |
| **api** | `Reports` (Day, Month, Products, Stock) and `Cash` (Drawer, Record, Count, Reverse) — **70 bound**; `SaleDTO` gains `voidReturn`, `voidReturnCurrency`; `DayTotalsDTO.refunded` → `voided` |
| **archlint** | `lite-reports-isolated`, `lite-cashbook-isolated`; eight module rules forbid both — **65** rules seen failing (L5: 50); `TestTheReportsModuleWritesNothing` scans for writing calls |
| **i18n** | 17 error codes, 4 owner-history labels, 173 screen strings, both languages |
| **frontend** | **Reports** (`/reports`, owner): Day statement, Month rows opening a day, Products sortable with the reconciling row, Stock with value, reconciliation, shelf and what it leaves out; **Cash drawer** (`/cash`): a card per currency with its terms, count and difference, the cash book, Count (no PIN), Expense / Take money out / Put money in and Reverse (PIN); the void form's *Hand back*; Sales' *voids* from `voided` |
| **seeder** | `-days 30` (default): thirty days before today under a stepped clock — a drifting rate with one same-day correction, weekly deliveries, 10–20 sales a day, three voids, a spoiled-labneh write-off, a count shortfall, rent, electricity and weekly transport, closing counts with one 5,000-pound shortage — then every verifier and the reports' reconciliations |

## 17. DECISIONS MADE WHILE BUILDING — approved 2026-09-15

| # | Decision | Why |
|---|---|---|
| D-L6.i1 | **`ck_cash_money_is_positive` amended**: `amount_minor >= 0 AND (kind IN ('count', 'reversal') OR amount_minor > 0)` — §8's draft said `kind = 'count' OR amount_minor > 0` | a reversal copies its entry's amount, and a count may be zero: the draft refused reversing a mistaken zero count (R6). The 22 refusals of §8.1 still hold by name, plus the zero-count reversal accepted |
| D-L6.i2 | **Q-L6.5 as built**: a cash sale hands back its total, a credit sale total − the debt added, in the charged currency; one function, shown before the PIN, summed by the drawer | §15's interpretation; derived, so every void since L4 reports the same way |
| D-L6.i3 | A **reversed debt payment or refund** moves its cash back on the reversal's day | a reversal says the payment did not happen; like a void, it counts on its own day (D-L4.i1) |
| D-L6.i4 | **At the counter** the drawer shows expenses paid from the drawer together with withdrawals as *taken out by the owner*, and lists only counts; categories, notes and expenses apart are the owner's | Q-L6.7: expenses owner only, the count at the counter — the expected figure still has to add up in front of the cashier |
| D-L6.i5 | A count **records the expected figure at the moment it is made**; the day's count is the newest not reversed; a reversed count no longer opens the next day | the difference is found once, against the figure the cashier saw |
| D-L6.i6 | `Reports.Stock` takes a **range** (month to date by default): value on its last day, the range reconciled, the shelf now | a reconciliation needs a start; §10.1 named only a date |
| D-L6.i7 | The reconciliation's classes are **exact value changes** (on hand × average after − before, in big integers) that telescope; so its only possible differences are **stock below zero** and **rounding**, the rounding bounded by half a cent per rounded figure and the count shown | §6.3 also named *lines of unknown cost* as a cause; a product never costed carries average 0 and moves no value, so it cannot make a difference |
| D-L6.i8 | Shelf profit **counts products priced below cost** — negative, marked and counted — and leaves out inactive products, no stock, no cost, and pounds prices with no rate | a below-cost price is exactly what the owner must see in the figure; §6.4 had it among those left out |
| D-L6.i9 | Reports read their facts **without a transaction** | SQLite has one writer; a year's report inside `tx.Do` would hold the till. A sale rung up during a report may show in one figure and not another until it is read again |
| D-L6.i10 | A month lists **only days with activity**; its totals add every day | 31 empty rows teach nothing; `TestAMonthIsTheSumOfItsDays` holds the totals |
| D-L6.i11 | Takings **net each reversal on the reversal's day** — collected, refunded, written off | D-L4.i1 |
| D-L6.i12 | Reports use **their own fact types**; the composition root copies records, and `TestTheReportsAndCashBookReachTheRealOwner` holds every kind string equal to its module's | `lite-reports-isolated` |
| D-L6.i13 | The Cash screen shows a **past day read-only**; counts and entries are made on today | a count records *now* against today's figure |
| D-L6.i14 | The seeder's **history has no credit**: L5's paper book is adopted today, as before (§11 put credit through the month) | L5's seeded debt book and its tests stay as approved; recorded as a deviation |
| D-L6.i15 | The year-of-sales bounds are **×10 under the race detector**, rows generated in SQL | as D-L5.i11 |

## 18. FINDINGS

**R1 — nothing classified the reconciliation's rows.** Planning drill G8 (a cost correction counted with the losses) showed
it would survive: the telescoping property checks only that the classes sum. `TestACostCorrectionIsARevaluationNotProfit`
now holds each class and that a correction is not the day's profit; the drill is caught by it.

**R2 — the seeder's history silently skipped a loss.** The spoiled-labneh write-off was planned on day 9, when the labneh had
already sold out, and the helper returned nil. Found by the seeder test asserting the month has spoiled goods. The write-off
moved to day 4, the shortfall now picks a product with stock, and both **refuse loudly** instead of skipping.

**R3 — a wall-clock dependency in L5's binding test.** `TestTheDebtBookThroughTheBindings` asserted `OwedSince: "2026-09-14"`
while the application ran on the system clock; it failed when the date became 2026-09-15 during this build. It now reads the
business date from the response. The Reports screen's *owner mode ends* test was also timing-sensitive on real timers and now
drives the countdown with fake timers.

**R4 — archlint refused the seeder command importing `kernel/clock`** (`lite-cmd-entry`); the fixed clock is built by
`demoseed.NewClock()`.

**R5 — the translation gate caught `lite.demoseed.reports_inconsistent`** with no message; translated.

**R6 — §8's verified CHECK was too strict** for a reversal of a zero count (D-L6.i1).

**R7 — two drill plants did not compile or transform** (G10, F10) and were re-planted; a run that fails to compile is never
counted as a catch.

**R8 — golangci-lint v2: 7 shadowed `err`**; ESLint's RTL rule matched `left-` inside a test id. Fixed.

## 19. EVIDENCE

| Check | Result |
|---|---|
| `make lite-ci` | **pass** — nothing NOT RUN (§19.1) |
| Go tests (race) | **424** Lite test functions (L5: 376) |
| Frontend | **391** tests in 27 files; bundle gate 4 (L5: 351) |
| golangci-lint v2 | 0 issues |
| archlint | clean; **65** rules seen failing (L5: 50) |
| Timing — a year of sales (36,500 sales, 109,500 lines) | a month **32 ms** (bound 300 ms), a year of products **356 ms** (bound 2 s), a day **1 ms**; under `-race` 0.9 s, 10.4 s and 31 ms (bounds ×10) |
| Windows | `Mizan Lite.exe` and `lite-demoseed.exe` built — not run (O1) |
| **Mizan after L6's archlint change** | `scripts/check.sh` green: 106 Go packages, 337 frontend tests, 0 lint issues |

**The packaged macOS app, run for real (2026-09-15):**

- **An L5 shop upgraded:** a copy of `l5 seeded محمد #5` (schema 6) opened in the packaged L6 app — `migration applied version 7
  "cashbook"`, ready at schema 7, the frontend reached Go, quit. All 15 existing tables — `sales` (13), `sale_lines` (19),
  `stock_ledger` (69), `stock_levels` (40), `products` (40), `fx_rates` (3), `customers` (8), `debt_entries` (17),
  `owner_events` (33) and the rest — **identical row for row**; `cash_entries` added, empty; integrity ok; no foreign-key problems.
- **L6 over that real data** (a further copy, through the graph): receipt 12 — the credit sale of O13 — **hands back 20,000 SYP**,
  receipt 8 its 150,000; the pounds drawer's void returns 170,000, expected 1,142,500. The day's gross profit 29.74 USD /
  446,568 SYP, net 13.08 USD / 196,663 SYP after an 8.00 USD write-off and 8.66 USD of stock losses. The day's stock
  reconciliation names **0.91 USD of stock below zero** (L4's sale beyond the shelf) and 1 cent of rounding (bound 22 cents),
  and its closing value is the stock screen's less that negative line. The next day's count recorded its expected figure net
  of an expense. Sales, stock and debt verifiers: nothing.
- **The seeder's month** into `l6 seeded محمد #6` (`-days 30`): 439 sales (426 of history), 5 voided, 816 lines, 900 ledger rows,
  35 rates from 14,200 to 15,600, 50 counts, 6 expenses, 17 debt entries; every verifier and reconciliation held; net profit
  880.64 USD and 13,192,257 SYP — the two readings diverging as the pound moved. Opened in the packaged app: ready at
  schema 7, quit; integrity ok.

### 19.1 Final runs

After the last change to code, 2026-09-15: `make lite-ci` — wails generate, gofmt, vet, Windows and Intel-Mac cross-compile,
archlint and its 65 planted drills, race tests, golangci-lint v2 (0 issues), ESLint, typecheck, 391 frontend tests and gates,
production build, G5 on the bundle — **every step passed, none NOT RUN**. The first two runs failed and were fixed (R4, R5).
Mizan's `scripts/check.sh` — **all local checks passed**, after L6's change to `arch-rules.yml`. Only documents changed
afterwards.

## 20. MUTATION DRILLS — 35 BEHAVIOUR DRILLS, ALL CAUGHT

| # | Planted defect | Caught by |
|---|---|---|
| G1 | pounds cost frozen at purchase (C6) | `TestTheC6FixtureThroughTheRealModules`, `TestNetProfitDollarsAndPoundsAgreeInSignForASingleRate` |
| G2 | a void's profit taken back on the sale's day | `TestAVoidTakesItsProfitBackOnItsOwnDay`, `TestProductsAddUpToTheDayExactly` |
| G3 | unknown-cost revenue counted as profit | `TestLinesOfUnknownCostAreShownApart`, `TestProductsAddUpToTheDayExactly`, `TestAMonthIsTheSumOfItsDays` |
| G4 | the reconciling row left out | `TestProductsAddUpToTheDayExactly`, `TestAMonthIsTheSumOfItsDays` |
| G5 | losses converted at today's rate | `TestLossesAndGainsAtTheCostTheyMovedAt` |
| G6 | the rate of the day by arrival, not by place | `TestTheRateOfTheBusinessDayIsNewestOnOrBeforeIt` |
| G7 | negative stock valued below zero | `TestNegativeStockHasNoValue` |
| G8 | a cost correction counted with the losses | `TestACostCorrectionIsARevaluationNotProfit` — **would have survived** (R1) |
| G9 | dollars handed over for a pounds total added to the pounds drawer | `TestTheDrawerSumsMoneyInTheCurrencyItMovedIn`, `TestTheDrawerAgreesWithEveryCashMovement` and the scenario's count check |
| G10 | a void return in the tender's currency | `TestTheDrawerAgreesWithEveryCashMovement` and the scenario's count check — **did not compile first** (R7) |
| G11 | a count carried into the next day's difference | `TestACountClosesItsDay`, `TestTheDrawerAgreesWithEveryCashMovement` |
| G12 | an expense without the guard | `TestMoneyEntriesNeedTheOwner`, `TestTheReportsAndCashBookReachTheRealOwner`, `TestTheReportsAndTheDrawerThroughTheBindings` |
| G13 | a report readable outside owner mode | `TestReportsAreTheOwners`, `TestTheReportsAndTheDrawerThroughTheBindings` |
| G14 | the reports calling a writing method | `TestTheReportsModuleWritesNothing` |
| G15 | the counter shown the owner's expenses | `TestTheDrawerIsTheCountersWithoutTheOwnersDetails` |
| G16 | a count measured against the machine's date | `TestACountNeedsNoPIN` |
| G17 | a reversal with no reason (NULL through the CHECK) | `TestACheckRefusesAnImpossibleCashEntry` |
| G18 | a margin truncated, not rounded | `TestMarginIsExactToOneDecimal` |
| G19 | a write-off reversal not giving the bad debt back | `TestBadDebtsAndExpensesInBothReadings` |
| G20 | an expense reversed at today's rate | `TestBadDebtsAndExpensesInBothReadings` |
| G21 | a reversed payment's cash left in the drawer | `TestTheDrawerAgreesWithEveryCashMovement` and the scenario's count check |
| F1 | the void dialog stating the tender, not what was paid | "says the cash to hand back before the PIN: a credit sale's paid now…" |
| F2 | reports read without the PIN flow | "asks for the PIN, then reads the day's statement…", "a cancelled PIN shows no figures…" |
| F3 | figures kept when owner mode ends | "clears the figures when owner mode ends" |
| F4 | a month's row opening another day | "a month lists its days with the totals, and a day opens its statement" |
| F5 | products sorted by profit whatever is chosen | "products sort by profit, quantity or margin…" |
| F6 | the reconciling row left off | the same test |
| F7 | a stock difference's cause not named | "the stock report reconciles opening to closing value and names a difference's cause" |
| F8 | the unknown-cost banner hidden | "shows sales of goods with no recorded cost apart, as a banner" |
| F9 | a count sent in the wrong currency | "the counter counts without a PIN and sees the difference" |
| F10 | an expense without the PIN flow | "an expense goes through the owner's PIN…" — **did not transform first** (R7) |
| F11 | a drawer term showing another figure | "shows per currency what should be in the drawer…", "reads in Arabic" |
| F12 | Reverse offered on every entry | "the owner sees the cash book and reverses an entry with a reason" |
| F13 | a past day offering a count | "a past day is shown as it was, with no count or entry buttons" |
| F14 | the Sales screen's voids read from another field | "lists the day's sales with Go's totals per currency" |
| A1–A15 | `reports` importing sales, stock/domain, cashbook; `cashbook` importing reports, fx/domain; catalog, owner, settings, setup, stock, fx, sales and customers importing `reports` or `cashbook` | archlint, on every run |

## 21. NOT VERIFIED

1. **The Windows build has not run** (O1).
2. **The Reports and Cash screens and the void form have not been looked at** in either language (O5).
3. **A drawer on a shop never counted** reads every day's facts since the first; it is fast on the seeded month and unmeasured
   over years (O8).
4. **Concurrent reads:** a report reads each module's facts separately (D-L6.i9); nothing tests a sale landing between them.
5. **Printing reports** is L7's.

## 22. DEFINITION OF DONE

| Criterion (§12.3) | Status |
|---|---|
| `make lite-ci` green | ✅ |
| Every rule drilled, every reconciliation holding on the seeded month | ✅ 35 behaviour drills + 15 architecture; the seeder fails on any reconciliation and passed |
| The C6 fixture passes through the real modules | ✅ `TestTheC6FixtureThroughTheRealModules` |
| A year of sales within §12.1's bounds | ✅ §19 |
| An installation from L5 upgrades with every row intact | ✅ §19 |
| The Reports and Cash screens looked at in Arabic and English | ⏳ owner (O5) |
| Windows `.exe` builds | ✅ built — not run |
| Mizan's `scripts/check.sh` green | ✅ |
| PROGRESS, DECISIONS and this document updated | ✅ |

### 22.1 At commit (2026-09-15)

The owner approved D-L6.i1–i15 — naming D-L6.i2 (what a void hands back), D-L6.i4 (the counter's view of expenses) and
D-L6.i1 (the amended cash book CHECK) — and the commit. O13 is closed in [../PROGRESS.md](../PROGRESS.md); O1, O5 and O8
remain.

**Next:** L7's design note — export (Excel and PDF), 80 mm thermal printing, backup and restore.
