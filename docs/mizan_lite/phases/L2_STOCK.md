# Mizan Lite — Phase L2: stock, weighted-average cost, opening packages

> **Status: COMPLETE — committed 2026-09-14.** The 14 decisions and this note were approved 2026-09-13 with the
> answers in §12.3; the decisions made while building (§14, D-L2.i1–i14) were approved 2026-09-14. §§1–12 are the approved design, amended only where
> §14 says so; §§13–20 are the implementation record.
> **Date:** 2026-09-13 (design), 2026-09-14 (implementation). **Base:** L1 (`edb4570`). **Design:**
> [../DESIGN.md](../DESIGN.md) §3.3, §4.6, §5 (with §13).

---

## 0. How to read this

| § | Content |
|---|---|
| 1 | Analysis — what L2 must make possible, and seven things harder than they look |
| 2 | Scope — in, out, and three amendments to the approved design |
| 3 | The costing rules — the arithmetic, and every trap Mizan's own costing fell into |
| 4 | The schema — `0003_stock.sql` |
| 5 | The acts: receive, count, adjust, open a package, reverse a receipt, correct a cost |
| 6 | Who may do what — the owner PIN in L2 |
| 7 | The verifier |
| 8 | Bindings, and the screen that calls each one |
| 9 | Architecture rules that change |
| 10 | The demo data |
| 11 | Tests, drills, Definition of Done |
| 12 | **Decisions and questions for approval** |
| 13 | What was built |
| 14 | Decisions made while building |
| 15 | Findings |
| 16 | Evidence |
| 17 | Mutation drills |
| 18 | Not verified |
| 19 | Definition of Done |
| 20 | Next |

---

## 1. ANALYSIS

### 1.1 What L2 must make possible

A pantry shop owner:

1. adopts Lite with **stock already on the shelves** — 12 tins of ghee bought at $17, 40 kg of bulgur — without
   pretending it was a delivery;
2. **receives** a delivery the way the invoice reads: *25 kg of bulgur, 450,000 SYP* — a total, in pounds;
3. writes off **three broken jars** and **spoiled labneh**, and corrects a shelf that was **counted** differently;
4. **opens a 16-litre tin** of olive oil and sells it by the litre, with the cost carried exactly;
5. fixes a delivery typed as **$9.50 instead of $95.00** a tin;
6. sees what is on hand — and, in owner mode, what it cost and what it is worth.

And after all of it, a check that the numbers still agree with themselves.

### 1.2 Seven things harder than they look

**H1 — the scale trap that cost Mizan two phases.** A quantity is 10⁻⁶, a unit cost 10⁻⁶ of the major unit; their
product is **major** units, and money is stored in **minor**. Mizan's costing omitted the conversion and was wrong
by a factor of a hundred for two phases — invisible because every test used SYP, which has no minor unit. Lite holds
cost in **USD, which has two decimals**, so the trap is live from the first receipt. *A scale conversion tested only at
scale 1 is a conversion nobody has tested* (Mizan's `scale_test.go`). §3.4.

**H2 — receiving into negative stock.** Averaging a receipt against a negative on-hand produces nonsense: Mizan's own
example — 10 received at 120 into −5 carrying a phantom average of 999 — gives **−759**. The receipt's cost must win
when on-hand is not positive (DESIGN §4.6). L2 cannot go negative by itself (§5.7), but the rule must be right for the
till in L4, which can (Q4).

**H3 — the seam Mizan had to rebuild.** Mizan built a "revaluation" movement for a caller that did not exist yet; when
the caller came, a `quantity > 0` CHECK made it **structurally unwritable** and the table was rebuilt (Mizan
`KNOWN_GAPS.md` §2). A cost correction moves value without moving quantity. If L2 has one, its CHECK must allow a
zero quantity for that kind **and a test must write one through the domain**, not insert it directly. §4, §5.6.

**H4 — a receipt costed in pounds before rates exist.** DESIGN §4.2 holds cost in USD and converts a pound-costed
receipt at "the receipt's rate snapshot" — but exchange rates arrive in **L3**, after this phase. A shop's labneh and
cheese suppliers are paid in pounds. §5.1, Q-L2.1.

**H5 — invoices state totals, not unit costs.** *25 kg for 450,000* is 18,000 a kilo; *a sack of 50 kg for $63.10* is
$1.262 a kilo — a unit cost with **more decimals than the currency**. A form that asks for the unit cost makes the
shopkeeper divide, and rounding the result to cents before multiplying it back is how a stock value drifts. §5.1, Q-L2.7.

**H6 — opening a package moves value between two products.** One tin at $95.00 becomes 16 litres at $5.9375. At
15 litres it is $6.333333…, which cannot be held exactly; value out and value in can differ by a rounding. The
difference must be **bounded and stated**, not discovered. §3.5.

**H7 — a date is not a timestamp.** "Today's receipts" means the shop's local day. A receipt at 01:00 in Damascus is
22:00 UTC the previous day. And `time.LoadLocation("Asia/Damascus")` **fails on a Windows machine** without Go's
embedded time-zone data — a bug that passes every test on a Mac. §3.7.

---

## 2. SCOPE

### 2.1 In L2

| Area | What |
|---|---|
| Stock levels | on hand and average cost per product, maintained with every movement |
| Movements | opening stock, receipt, receipt reversal, count, adjustment, package out / content in, cost correction (Q-L2.5) |
| Costing | weighted-average cost in USD, per §3 |
| Packages | "this tin opens into 16 L of loose olive oil" — set on the product; the **open package** act (Q8) |
| Kernel | three additions to `kernel/money` (§3.6), with property tests, available to Mizan |
| Owner | guarded acts and one guarded **read** (§6) |
| Verifier | the ledger checked against itself and against the levels (§7) |
| Screens | Stock (levels, acts, history), package fields on the product form |
| Demo data | opening stock, receipts in both currencies, a count, a write-off, a tin opened |

### 2.2 Not in L2

| Not here | Why | Where |
|---|---|---|
| Sales moving stock | there is no till yet | L4 |
| Exchange rates | L3; §5.1 bridges the gap | L3 |
| Suppliers | not in v1 (DESIGN §3.4); a receipt takes a free-text note | — |
| Stock alerts / reorder points | Mizan's `KNOWN_GAPS` §3 reasoning applies unchanged: a threshold nobody maintains is worse than none | — |
| Lots, expiry dates | not asked for; a spoiled item is an adjustment with a reason | — |
| Multi-level packaging (carton → tin → litre) | Q-L2.8 | — |

### 2.3 Three amendments to the approved design

**A-L2.1 — stock gets its own tables; `products` keeps none of it.** DESIGN §5 put `on_hand_micro` and
`avg_cost_usd_micro` on `products`, reasoning that the till reads them constantly. But two modules writing one table —
catalog writing names and prices, stock writing quantities — is exactly what the module-isolation rules exist to
prevent, and a join on a primary key costs nothing at a pantry shop's size. **`stock_levels`** belongs to the stock
module; **`product_packages`** belongs to the catalogue.

**A-L2.2 — every movement records the state before it as well as after.** DESIGN §5 stored only the after-state.
Recording `on_hand_before` and `avg_cost_before` makes a receipt reversal exact (§5.5) and gives the verifier a chain to
walk: each row's *before* must equal the previous row's *after* (§7). A row inserted, deleted or edited out of turn
breaks the chain at the point it happened.

**A-L2.3 — the till's columns arrive with the till.** DESIGN §5's ledger references `sales(id)`, a table that will not
exist until L4. SQLite *can* add that column later with its foreign key (`ADD COLUMN … REFERENCES`, verified) — but it
**cannot change a CHECK constraint**, and the till needs two: the `kind` list must gain `sale` and `sale_void`, and a
sale row must be bound to its `sale_id`. The options are sale kinds and an unbound `sale_id` now (a column nobody writes
for two phases, and a guarantee never enforced), or a planned **rebuild** of `stock_ledger` in L4's migration. **L2
proposes the rebuild** — stated now so it is a decision, not a surprise; Mizan's 6.6 rebuild was the surprise kind. L4's
migration test will rebuild a ledger holding real rows and compare them row for row.

---

## 3. THE COSTING RULES

### 3.1 The state

Per product: **on hand** (10⁻⁶ of the product's unit) and **average cost** (10⁻⁶ USD per unit). Both live in
`stock_levels` and in every ledger row's *after* columns.

### 3.2 The rules

| Movement | On hand | Average cost | Unit cost on the row |
|---|---|---|---|
| **Opening** (first movement only) | + qty | the opening cost | the opening cost |
| **Receipt**, on hand > 0 | + qty | (on hand × avg + qty × cost) ÷ (on hand + qty), rounded once | the receipt's cost |
| **Receipt**, on hand ≤ 0 | + qty | **the receipt's cost** (H2) | the receipt's cost |
| **Count** | set to the counted quantity | **unchanged** — a count says how many, not what they cost | the current average |
| **Adjustment** (±) | ± qty | **unchanged** — an issue never moves a moving average | the current average |
| **Package out** | − packages | unchanged | the package's average |
| **Content in** | + content | as a receipt at the derived cost (§3.5) | the derived cost |
| **Receipt reversal** | − the receipt's qty | **restored** to the receipt's *before* (§5.5) | the receipt's cost |
| **Cost correction** | unchanged (qty 0) | set to the corrected cost | the corrected cost |

**A positive adjustment** (stock found) enters at the current average and so leaves it unchanged. If the product has
**no average yet** (nothing was ever received), a positive adjustment or count is refused with a message to record an
opening or a receipt instead — stock with no cost would make every later profit figure fiction.

### 3.3 Rounding

One rounding per computed average, **HalfUp at 10⁻⁶ USD**, in `big.Int` (on hand × average overflows `int64` at
realistic sizes: 10⁹ micro-units × 10⁸ micro-dollars). HalfUp rather than banker's rounding because nothing here sums
many rounded averages; each is a single state.

### 3.4 Money values, and the scale trap (H1)

A stock **value** — "this shelf is worth $1,140.00" — is quantity × unit cost converted to USD **minor** units. L2 does
not write a new conversion: it uses **`money.LineExtension`**, the kernel's one scale-aware, single-rounding price ×
quantity function, already tested. Every test that asserts a value runs at **0, 2 and 3 decimals**, so a missing scale
conversion cannot hide behind a currency where it makes no difference.

### 3.5 Opening a package (H6)

A package product (a 16 L tin) is linked to a content product (loose olive oil, by the litre) with a content quantity
per package (16). Opening *n* packages:

```
out:  package product   − n
in:   content product   + n × content
unit cost in  =  (package average × n) ÷ (n × content)        one rounding, HalfUp, 10⁻⁶ USD
```

The content product then averages the incoming litres in, like any receipt.

**The rounding bound, stated:** value out is `LineExtension(package avg, n)`; value in is
`LineExtension(unit cost in, n × content)`. With the unit cost held to 10⁻⁶, the two *exact* values differ by less than
`n × content × 0.0000005` of the major unit — below one minor unit while the content is under **2 × 10^(6 − decimals)**
units — so once each is rounded they differ by **at most one minor unit**. For USD, the cost currency: under 20,000 units
of content, at most **one cent**. A property test holds the bound at 0, 2 and 3 decimals (it corrected this paragraph's
first draft, which stated the USD figure as if it held at every scale); the history shows both values, so the difference
is visible, never absorbed silently.

### 3.6 Three kernel additions

DESIGN D6 approved `WeightedAverageUnit`; L2 needs two siblings. All in `kernel/money`, integer-only, one rounding each,
with property tests, and available to Mizan:

| Function | Computes | Used by |
|---|---|---|
| `WeightedAverageUnit(onHand, avg, qty, cost, mode)` | the receipt average (§3.2) | receipt, content in |
| `SplitUnitCost(unitCost, fromQty, toQty, mode)` | a package's cost spread over its content (§3.5) | open package |
| `UnitCostFromTotal(total Money, qty, mode)` | a unit cost from an invoice total (H5), at 10⁻⁶ | receipt, opening |

Plus, for pound-costed receipts (§5.1), `UnitAmount.DivideByRate(localPerUSD Rate, to Currency, mode)`, converting a
local unit cost to USD in one rounding. **Property tests:** the average lies between the old average and the receipt
cost; `SplitUnitCost` then `LineExtension` returns the package value within the §3.5 bound; `UnitCostFromTotal` then
`LineExtension` returns the total within one minor unit.

### 3.7 Business dates (H7)

Every movement carries `business_date` (the shop's local calendar day) and `occurred_at` (UTC). The local day comes
from `time.Local` through an injected `*time.Location`; **nothing calls `time.LoadLocation`**, which needs time-zone
data Windows does not ship. Tests use `time.FixedZone("Damascus", 3*3600)` — a fixed offset, available everywhere — and
include a movement at 01:00 local, which belongs to *today*, not to UTC's yesterday.

---

## 4. THE SCHEMA — `0003_stock.sql`

```sql
-- One row per product that has ever moved. Owned by the stock module (A-L2.1).
CREATE TABLE stock_levels (
  product_id               CHAR(36)  NOT NULL PRIMARY KEY REFERENCES products(id),
  on_hand_micro            BIGINT    NOT NULL,
  avg_cost_usd_micro       BIGINT    NOT NULL CHECK (avg_cost_usd_micro >= 0),
  -- The newest movement: what a receipt reversal checks it is (§5.5), and where the verifier's chain ends.
  last_movement_id         CHAR(36)  NOT NULL REFERENCES stock_ledger(id),   -- FK added in implementation (D-L2.i1)
  row_version              BIGINT    NOT NULL DEFAULT 1,
  updated_at               CHAR(24)  NOT NULL
);

-- LEDGER. Append-only. The only permitted writes are INSERTs (a test scans the store's SQL).
CREATE TABLE stock_ledger (
  id                         CHAR(36)    NOT NULL PRIMARY KEY,
  product_id                 CHAR(36)    NOT NULL REFERENCES products(id),
  seq                        BIGINT      NOT NULL CHECK (seq >= 1),   -- place in the product's ledger (D-L2.i1)
  business_date              CHAR(10)    NOT NULL,     -- the shop's local day (§3.7)
  occurred_at                CHAR(24)    NOT NULL,     -- UTC
  kind                       VARCHAR(20) NOT NULL CHECK (kind IN (
                               'opening', 'receipt', 'receipt_reversal', 'count', 'adjustment',
                               'package_out', 'content_in', 'cost_correction')),
  quantity_micro             BIGINT      NOT NULL,     -- signed
  unit_cost_usd_micro        BIGINT      NOT NULL CHECK (unit_cost_usd_micro >= 0),

  -- A-L2.2: the state before AND after, so a reversal is exact and the verifier can walk the chain.
  on_hand_before_micro       BIGINT      NOT NULL,
  avg_cost_before_usd_micro  BIGINT      NOT NULL CHECK (avg_cost_before_usd_micro >= 0),
  on_hand_after_micro        BIGINT      NOT NULL,
  avg_cost_after_usd_micro   BIGINT      NOT NULL CHECK (avg_cost_after_usd_micro >= 0),

  -- What was typed on a receipt or opening: the currency, the unit cost in it, and (for pounds) the rate (§5.1).
  entered_currency           CHAR(3)     REFERENCES currencies(code),
  entered_unit_cost_micro    BIGINT,
  local_per_usd_nano         BIGINT,

  reason_code                VARCHAR(16),              -- counts and adjustments (Q-L2.6)
  note                       VARCHAR(200),
  reverses_id                CHAR(36)    REFERENCES stock_ledger(id),
  pair_id                    CHAR(36),                 -- links a package_out to its content_in
  created_at                 CHAR(24)    NOT NULL,

  CONSTRAINT ck_ledger_quantity_by_kind CHECK (
    (kind IN ('opening', 'receipt', 'content_in')     AND quantity_micro > 0) OR
    (kind IN ('receipt_reversal', 'package_out')      AND quantity_micro < 0) OR
    (kind = 'adjustment'                              AND quantity_micro <> 0) OR
    -- A count may confirm the shelf (zero); a cost correction moves value, never quantity (H3).
    (kind = 'count') OR
    (kind = 'cost_correction'                         AND quantity_micro = 0)),
  CONSTRAINT ck_ledger_after_follows_before CHECK (on_hand_after_micro = on_hand_before_micro + quantity_micro),
  CONSTRAINT ck_ledger_reason_where_needed CHECK (
    (kind IN ('count', 'adjustment')) = (reason_code IS NOT NULL)),
  CONSTRAINT ck_ledger_reason_known CHECK (
    reason_code IS NULL OR reason_code IN ('count', 'damaged', 'expired', 'own_use', 'gift', 'other')),
  CONSTRAINT ck_ledger_other_needs_note CHECK (reason_code IS NULL OR reason_code <> 'other' OR note IS NOT NULL),
  CONSTRAINT ck_ledger_reversal_links CHECK ((kind = 'receipt_reversal') = (reverses_id IS NOT NULL)),
  CONSTRAINT ck_ledger_package_pairs CHECK ((kind IN ('package_out', 'content_in')) = (pair_id IS NOT NULL)),
  -- Every comparison guarded with IS NOT NULL: in SQL, NULL > 0 is NULL, and a CHECK that evaluates to NULL PASSES.
  -- The first draft of this constraint accepted a pound receipt with no rate for exactly that reason (§4 notes).
  CONSTRAINT ck_ledger_entered_cost CHECK (
    (entered_currency IS NULL AND entered_unit_cost_micro IS NULL AND local_per_usd_nano IS NULL) OR
    (kind IN ('opening', 'receipt') AND entered_unit_cost_micro IS NOT NULL AND entered_unit_cost_micro >= 0 AND (
       (entered_currency IS NOT NULL AND entered_currency = 'USD' AND local_per_usd_nano IS NULL) OR
       (entered_currency IS NOT NULL AND entered_currency <> 'USD' AND
        local_per_usd_nano IS NOT NULL AND local_per_usd_nano > 0)))),
  CONSTRAINT ux_ledger_reversed_once UNIQUE (reverses_id),
  CONSTRAINT ux_ledger_product_seq  UNIQUE (product_id, seq)          -- replaces ix_stock_ledger_product (D-L2.i1)
);
CREATE INDEX ix_stock_ledger_date    ON stock_ledger (business_date);

-- A package and what it opens into. Owned by the catalogue: it is a fact about two products.
CREATE TABLE product_packages (
  package_product_id       CHAR(36)  NOT NULL PRIMARY KEY REFERENCES products(id),
  content_product_id       CHAR(36)  NOT NULL REFERENCES products(id),
  content_quantity_micro   BIGINT    NOT NULL CHECK (content_quantity_micro > 0),  -- per package, in the content's unit
  row_version              BIGINT    NOT NULL DEFAULT 1,
  updated_at               CHAR(24)  NOT NULL,
  CONSTRAINT ck_package_not_itself CHECK (package_product_id <> content_product_id)
);
CREATE INDEX ix_product_packages_content ON product_packages (content_product_id);
```

**Notes.**

- **Verified against SQLite before this note was presented** (on top of 0001 and 0002): every constraint refuses its
  impossible row and accepts the valid ones, including a quantity-0 cost correction (H3). The check **found a defect in
  the first draft**: `local_per_usd_nano > 0` with the rate missing evaluates to NULL, and a CHECK that is NULL passes —
  a pound receipt with no rate was accepted. The first fix repeated the trap one clause over (`entered_currency = 'USD'`
  with no currency is NULL too, so a cost with no currency was accepted), found by the same test. Every comparison now
  carries its own `IS NOT NULL`, and L2's implementation will test each NULL case by raw insert. **The same pattern is in
  DESIGN §5's drafts for `sales` and `debt_entries`**; recorded in [../PROGRESS.md](../PROGRESS.md) for L4 and L5.
- **`ck_ledger_after_follows_before`** makes an arithmetically impossible row unrepresentable, not merely unlikely.
- **One level of packaging** (Q-L2.8) is enforced by the service: a content product may not itself be a package, and a
  package may not be anyone's content. A CHECK cannot see another row, and a trigger is not portable.
- **The `kind` list has no sale kinds** (A-L2.3). L4's migration rebuilds the table to add them with `sale_id` and its
  foreign key.
- **No UPDATE or DELETE** of `stock_ledger` anywhere in the store — `TestLedgerIsInsertOnly` scans the SQL, as DESIGN §9.2
  planned.

---

## 5. THE ACTS

Every act is one transaction: read the level (row-versioned), compute in the domain, append the ledger row(s), write
the level. Refusals happen before anything is written.

### 5.1 Receive

**Input:** product, quantity (normalised; the unit's input decimals), cost as **total or per unit** (Q-L2.7; total by
default), currency, and — when the currency is SYP — the **rate on the receipt** (Q-L2.1), plus an optional note
("from Abu Khalil").

- A **total** becomes a unit cost with `UnitCostFromTotal` at 10⁻⁶ precision — never rounded to cents first (H5).
- A **pound cost** becomes USD with `DivideByRate` in one rounding. The row keeps what was typed: `entered_currency`,
  `entered_unit_cost_micro`, `local_per_usd_nano`. In L3 the form will **pre-fill** the rate from the rate in force;
  the rate on the receipt remains what the shop actually paid at, which is the right figure for cost.
- Refused: an inactive product (reactivate first); a zero or negative quantity; a count unit with a fraction; a rate
  that is not positive; a cost with more than 6 decimals.

### 5.2 Opening stock

Receive's twin for adoption: allowed **only as a product's first movement**, so "stock we already had" never lands in a
month's receipts in L6's reports.

### 5.3 Count

**Input:** product, **the counted quantity** (what is on the shelf — not the difference; Mizan 10.10's lesson), reason
`count`, optional note. The row's quantity is `counted − on hand`, computed in Go at the moment of writing. A count that
matches records a zero movement: "counted, correct" is history worth keeping.

### 5.4 Adjust

**Input:** product, a **signed** quantity, a reason (`damaged`, `expired`, `own_use`, `other` — `other` needs a note).
"Three jars broken" is `−3, damaged`. Refused: taking stock below zero (Q-L2.2).

### 5.5 Reverse a receipt

For a receipt typed wrongly — **only while it is the product's newest movement** (`stock_levels.last_movement_id`).
The reversal row takes the receipt's quantity back out and restores the average to the receipt's recorded *before*
value — exactly, because A-L2.2 stored it. A receipt can be reversed once (`ux_ledger_reversed_once`). After later
movements, the receipt stands and a mistyped **cost** is fixed with §5.6; a mistyped **quantity** with a count.

### 5.6 Correct the average cost (Q-L2.5)

For a cost that is wrong after stock has moved: the owner sets the product's average cost to a stated figure, with a
required note. A `cost_correction` row with **quantity 0**, before and after averages recorded. Written through the
domain by its first test (H3).

### 5.7 Open a package

**Input:** the package product and a whole number of packages (Q-L2.8). Two rows — `package_out` and `content_in` —
sharing a `pair_id`, in one transaction, costed per §3.5. Refused: no package link; the content product inactive; fewer
packages on hand than opened (Q-L2.2).

### 5.8 Below zero, outside the till

Every act that takes stock out — adjustment, package out, reversal — is **refused** if it would leave on hand below
zero (Q-L2.2). The till in L4 is different by the owner's decision (Q4): a customer is waiting. Here nobody is; the
right move is to receive or count first, and a refusal says so.

---

## 6. WHO MAY DO WHAT

| Act | Owner PIN | Why |
|---|---|---|
| Receive, opening stock, open a package | no | adds or moves stock; the ledger shows who could have |
| Count or adjustment that **raises** stock | no | |
| Count or adjustment that **lowers** stock | **yes** (Q-L2.3) | "written off as broken" is how a jar walks out of a shop |
| Reverse a receipt | **yes** | removes stock and restores a cost |
| Correct the average cost | **yes** | changes every later profit figure |
| **See** average cost and stock value | **owner mode** (Q-L2.4) | cost beside price is the margin, and Q7 made profit owner-only |

**A guarded READ is new.** `owner.Require` records an act in the history; viewing costs is not an act, and recording
every glance would bury the acts that matter. L2 adds **`owner.Allowed(ctx) bool`** — the same elevation check, no
record — declared as part of stock's `OwnerGate` port. The cost columns are not merely hidden on screen: `Stock.Valuation`
refuses outside owner mode, and `Stock.Levels` does not carry them at all.

---

## 7. THE VERIFIER

`Stock.Verify` walks every product's ledger in order and reports — **never repairs** (Mizan's rule: a correction that
erased a discrepancy would erase the evidence of what caused it):

| Check | Finds |
|---|---|
| each row's *before* equals the previous row's *after* | a row inserted, removed or altered out of turn |
| the first row's *before* is zero on hand, zero cost | a ledger that does not start from nothing |
| `stock_levels` equals the last row's *after*, and `last_movement_id` is that row | a level written without its movement |
| every `package_out` has its `content_in` partner and vice versa | half an opening |
| every `receipt_reversal` points at a receipt of the same product and quantity | a reversal of the wrong thing |
| a product with a level has at least one movement, and the reverse | orphans |

The result is a list of findings, each with a code and the product; the Stock screen shows a banner in owner mode when
the list is not empty. The demo seeder runs it after seeding and fails if anything is found.

---

## 8. BINDINGS AND THEIR CALLERS

| Façade.Method | Guard | Caller |
|---|---|---|
| **`Stock.Levels`** (query) — quantities only | — | StockScreen |
| **`Stock.Valuation`** (query) — average cost, value | owner mode | StockScreen, owner columns |
| **`Stock.Movements`** (product, limit) | cost fields only in owner mode | MovementHistory |
| **`Stock.Receive`** | — | ReceiveDialog |
| **`Stock.Opening`** | — | ReceiveDialog (opening mode) |
| **`Stock.Count`** | when it lowers | CountDialog |
| **`Stock.Adjust`** | when it lowers | AdjustDialog |
| **`Stock.OpenPackage`** | — | OpenPackageDialog |
| **`Stock.ReverseReceipt`** | yes | MovementHistory |
| **`Stock.CorrectCost`** | yes | CostCorrectionDialog |
| **`Stock.Verify`** | owner mode | StockScreen banner |
| **`Catalog.SetPackage`** / **`Catalog.ClearPackage`** | — | ProductForm, package section |

`ProductDTO` gains `package: { contentProductId, contentQuantity } | null` — built as two flat fields (D-L2.i3). **13 new methods (11 on `Stock`, 2 on
`Catalog`), 34 in total.** Route:
**`/stock`**. Quantities cross as decimal strings formatted to the unit's input decimals by Go (`"12.500"` kg, `"3"` jars).

---

## 9. ARCHITECTURE RULES THAT CHANGE

| Change | Why |
|---|---|
| **`lite-stock-isolated`** (new): `stock` imports no other Lite module | it reaches products and the owner through ports it declares |
| `lite-catalog-isolated`, `lite-owner-isolated`, `lite-settings-isolated`, `lite-setup-reaches-only-its-ports` forbid `stock` | the isolation set grows with the module set |
| `kernel-purity` is unchanged | the three kernel additions import only the kernel |

Each new or changed rule gets a plant in `scripts/lite-arch-drill.sh`.

---

## 10. THE DEMO DATA

`lite-demoseed` grows (DESIGN §9.4): a package link (16 L tin → loose olive oil, 16); **opening stock** for every product,
costs in USD and — for dairy and bulk goods — in pounds at a rate typed on the receipt; three **receipts** entered as
totals; one **count** that finds two jars fewer (through the owner PIN); one **write-off** of spoiled labneh; **one tin
opened**. Then the verifier runs, and the seeder fails if it finds anything.

---

## 11. TESTS, DRILLS, DEFINITION OF DONE

### 11.1 Tests (names are the contract)

| Layer | Tests |
|---|---|
| **kernel** | `TestWeightedAverageLiesBetweenOldAndIncoming` (property) · `TestReceiptIntoNegativeStockTakesTheReceiptCost` (Mizan's −759 case) · `TestSplitUnitCostConservesValueWithinTheBound` (property) · `TestUnitCostFromTotalRoundTripsWithinOneMinorUnit` (property) · `TestDivideByRateRoundsOnce` · **every value assertion at 0, 2 and 3 decimals** |
| **stock/domain** | each row of §3.2 · `TestACountNeverMovesTheAverage` · `TestAnAdjustmentNeverMovesTheAverage` · `TestStockWithNoCostIsRefused` · `TestReversalRestoresTheAverageExactly` · `TestACostCorrectionMovesValueNotQuantity` (written through the domain — H3) · `TestNothingGoesBelowZeroOutsideTheTill` |
| **stock service (fake + contract)** | `StoreContract` on fake and SQLite · `TestOpeningOnlyAsTheFirstMovement` · `TestOnlyTheNewestReceiptCanBeReversedAndOnlyOnce` · `TestOpeningAPackageWritesBothRowsOrNeither` · `TestReceivingIntoAnInactiveProductIsRefused` · `TestLoweringStockNeedsTheOwnerAndRaisingDoesNot` |
| **the database** | `TestLedgerIsInsertOnly` (SQL scan) · `TestACheckRefusesAnImpossibleRow` for each §4 constraint, by raw insert · `TestTheLevelAndItsMovementCommitTogether` (a failure after the ledger insert leaves neither) |
| **verifier** | one planted discrepancy per §7 row, each found and none repaired |
| **dates** | `TestAMovementAt0100DamascusBelongsToToday` · `TestNoLoadLocationAnywhere` (source scan) |
| **catalogue** | package link: not itself, one level, content quantity respects the content unit, clearing a link |
| **bindings** | wire shapes · `Stock.Valuation` refused outside owner mode · `Stock.Levels` carries no cost field (reflection over the DTO) |
| **frontend** | receive by total and by unit; pound receipt asks for the rate; count asks what is there; a lowering count goes through the PIN; cost columns absent outside owner mode and present in it; reversal offered only on the newest receipt; open package shows the resulting litres; every quantity respects its unit's decimals, from the shared fixture |
| **seeder** | the §10 assertions, and the verifier finds nothing |

### 11.2 Drills planned

At least: the scale conversion removed (fails at 2 and 3 decimals, passes at 0 — the drill that proves the decimals
matter); the on-hand ≤ 0 branch removed (the −759 test fails); a count that moves the average; a reversal allowed after
a later movement; a cost correction CHECK that requires quantity > 0 (the H3 drill); package rows written in two
transactions; the verifier's chain check removed; a lowering adjustment without the guard; `Stock.Levels` carrying the
average; `LoadLocation` introduced (the source scan fails).

### 11.3 Definition of Done

> `make lite-ci` green · every invariant drilled · the seeder's stock opens in the packaged app and the verifier finds
> nothing · the Stock screen looked at in Arabic and English (owner, O5) · Windows `.exe` builds ·
> [../PROGRESS.md](../PROGRESS.md), [../DECISIONS.md](../DECISIONS.md) and this document updated.

---

## 12. DECISIONS AND QUESTIONS FOR APPROVAL

### 12.1 Decisions — approve, amend, or reject

| # | Decision | § |
|---|---|---|
| D-L2.1 | Stock owns `stock_levels` and `stock_ledger`; the catalogue owns `product_packages`; `products` gains no stock columns (amends DESIGN §5) | 2.3 |
| D-L2.2 | Every ledger row records before and after; the chain is verifiable (amends DESIGN §5) | 2.3, 7 |
| D-L2.3 | Sale columns arrive with the till, through a planned rebuild of `stock_ledger` in L4 | 2.3 |
| D-L2.4 | Costing per §3.2: receipts average in; counts, adjustments and issues never move the average; on hand ≤ 0 takes the receipt cost | 3.2 |
| D-L2.5 | Values through `money.LineExtension`; every value test at 0, 2 and 3 decimals | 3.4 |
| D-L2.6 | Three kernel additions and `DivideByRate`, each with property tests | 3.6 |
| D-L2.7 | Opening packages conserve value within a stated bound under one cent, shown in the history | 3.5 |
| D-L2.8 | Business dates from `time.Local`; no `LoadLocation` anywhere; tests use a fixed zone | 3.7 |
| D-L2.9 | Opening stock only as a product's first movement | 5.2 |
| D-L2.10 | A count takes the counted quantity; Go computes the difference | 5.3 |
| D-L2.11 | Stock with no cost cannot be raised by a count or adjustment | 3.2 |
| D-L2.12 | Receiving into an inactive product is refused | 5.1 |
| D-L2.13 | `owner.Allowed` for guarded reads, unrecorded; `Require` stays for acts | 6 |
| D-L2.14 | The verifier reports, never repairs; the seeder fails on a finding | 7 |

### 12.2 Questions only you can answer

**Q-L2.1 — How is a delivery paid in pounds costed, before exchange rates exist?** *Recommended: the receipt form asks
for the rate the shop paid at, when the currency is SYP.* It is stored on the receipt; in L3 the field is pre-filled from
the rate in force and stays editable. The alternative — USD-only receipts until L3 — leaves every dairy and bulk delivery
unrecordable for a phase.

**Q-L2.2 — Outside the till, may stock go below zero?** *Recommended: no.* A write-off, an opened tin or a reversal that
would leave a negative shelf is refused with "receive or count first". The till keeps your Q4 answer — sell with a
warning — because a customer is waiting there and nobody is waiting here.

**Q-L2.3 — Does lowering stock need the owner PIN?** *Recommended: yes — any count or adjustment that lowers stock.*
"Written off as broken" is the quiet way stock leaves a shop, and two minutes of owner mode covers an evening's
write-offs. The alternative is no PIN, with every write-off visible in the history for the owner to review.

**Q-L2.4 — Who sees average cost and stock value?** *Recommended: owner mode only.* Cost beside selling price is the
margin, and you made profit owner-only (Q7). Quantities stay visible to everyone.

**Q-L2.5 — How is a mistyped delivery corrected?** *Recommended: both* — reverse the receipt while it is still the
product's newest movement, and an owner-only average-cost correction for when stock has already moved. Without the
correction, *$9.50 instead of $95.00* distorts every sale's profit until the product sells out.

**Q-L2.6 — Are these the write-off reasons?** *Recommended:* count difference, damaged, expired or spoiled, own use, and
other (with a note). Shops sometimes add **gift or sample**; say if it belongs.

**Q-L2.7 — Deliveries entered as a total or a unit cost?** *Recommended: both, total by default* — invoices state totals,
and dividing by hand is where a cost gets rounded wrong.

**Q-L2.8 — How far does opening packages go?** *Recommended: whole packages only, one level.* One tin opens into
16 litres; a carton of 12 tins does not open into tins that open into litres. A second level is a later change with no
schema rewrite (the link table already allows it; only the service refuses).

### 12.3 The owner's answers (2026-09-13)

| Q | Answer | Effect |
|---|---|---|
| Q-L2.1 | Type the rate on a pound receipt; pre-filled once L3 exists | as §5.1 |
| Q-L2.2 | Refuse negative stock for manual entries and adjustments | as §5.8 |
| Q-L2.3 | Owner PIN for anything that lowers stock | as §6 |
| Q-L2.4 | Average cost and stock value in owner mode only | as §6 |
| Q-L2.5 | Both receipt reversal (newest movement) and owner-only cost correction | as §5.5, §5.6 |
| Q-L2.6 | **Add "gift or sample"** | reason code `gift` joins the CHECK list (§4) |
| Q-L2.7 | Total or unit cost, total by default | as §5.1 |
| Q-L2.8 | Single-level package opening | as §5.7 |

---

## 13. WHAT WAS BUILT

| Layer | Packages | What |
|---|---|---|
| Kernel | `kernel/money/costing.go` | `WeightedAverageUnit`, `SplitUnitCost`, `UnitCostFromTotal`, `UnitAmount.DivideByRate` — `big.Int`, one rounding each; `ErrNonPositiveQuantity`, `ErrNegativeCost` |
| Schema | `migrations/sqlite/0003_stock.sql` | `stock_levels`, insert-only `stock_ledger` (every §4 constraint, plus `seq`), `product_packages` |
| Dates | `internal/lite/bizdate` | the shop's calendar day from an injected zone; `TestNoLoadLocationAnywhere` scans every Lite source file |
| Stock | `stock`, `stock/domain`, `stock/infra/sqlite`, `stock/stocktest` | levels, the acts of §5, valuation, history, the verifier; `Catalogue` and `OwnerGate` ports; fake store and fake catalogue; the store contract run on the fake **and** SQLite |
| Owner | `owner.Service.Allowed` | owner mode without a record, for guarded reads (D-L2.13) |
| Catalogue | `catalog/domain/package.go`, `catalog` service + both stores | package links: not itself, sold by count, content decimals, one level; `SetPackage`, `ClearPackage`, `Packages`, `All` |
| Composition | `bootstrap` | `Options.Location` (default `time.Local`); `stockGate` and `stockCatalogue` adapters |
| Bindings | `api/stock.go`, `api/catalog.go` | `Stock` (11 methods) and `Catalog.SetPackage`/`ClearPackage` — **13 new, 34 in total**; `ProductDTO` carries the package link |
| Seeder | `demoseed` + `data/stock.json`, `cmd/lite-demoseed` | package link, 40 opening balances (USD and pounds with a typed rate), 3 deliveries as totals, a lowering count and a write-off through the PIN, one tin opened; fails on any verifier finding; prints the stock value |
| Frontend | `screens/stock/*`, `screens/products/PackageSection.tsx`, `api/client.ts`, `i18n/numbers.ts`, `ui/Dialog.tsx` | `/stock`: quantities for everyone; **Show costs** through `withOwner` with cost, value, total and the verifier's banner; receive (total/unit, pound rate), count, adjust (five reasons, gift included), open package, history with reversal on the one receipt Go names, cost correction; package section on the product form |
| Catalogs | `internal/lite/locales` | +35 error and finding codes, +91 interface strings per language (225 and 94 keys now) |
| Rules | `arch-rules.yml`, `scripts/lite-arch-drill.sh` | `lite-stock-isolated` added; catalog/owner/settings/setup rules forbid stock; `bizdate` joins `lite-pure-text`; **22 rules seen failing on every run** |

## 14. DECISIONS MADE WHILE BUILDING (within the approved design — approved 2026-09-14)

**D-L2.i1 — the ledger is ordered by a per-product sequence, not by time.** §4 indexed `(product_id, occurred_at, id)`
and §7 walked "in order". But `occurred_at` is the shop PC's clock, and a clock set back an hour — a dead CMOS battery
corrected, a timezone fixed — would reorder history, and the verifier would then report a broken chain that is not
broken. Each movement now carries `seq` (1, 2, 3 … per product) under `UNIQUE (product_id, seq)`: the verifier walks by
it, a gap is a finding, and two acts that read the same level cannot both append the next place — a second guard beside
the level's row version. `stock_levels.last_movement_id` also gained its foreign key. The store contract includes "the
walk follows places, not the clock".

**D-L2.i2 — "stock with no cost" stated precisely.** §3.2 said a raise is refused "if nothing was ever received". Two
cases that wording missed: a **zero count before any opening** would create a level and so block the opening forever
(D-L2.9), and a **receipt reversed back to nothing** leaves a product that was received yet has no cost. The rule as
built: a count or adjustment on a product that never moved is refused (`lite.stock.no_cost`), and a raise is refused
while on hand is zero or less **and** the average is zero. A sold-out product keeps its average, so found stock enters at
it.

**D-L2.i3 — `ProductDTO` carries the package link as two flat strings** (`packageContentId`,
`packageContentQuantity`, both empty when unlinked) rather than a nullable object: the DTO stays a comparable value in
Go and a plain object through `Plain<T>` in TypeScript.

**D-L2.i4 — an act's reply is quantities only.** `Receive`, `Opening`, `Count`, `Adjust`, `ReverseReceipt` and
`CorrectCost` answer with `StockLevelDTO` (product and on hand), `OpenPackage` with both products'. Returning the
movement would have carried its costs to a screen outside owner mode.

**D-L2.i5 — costs are cleared in the service, not the binding.** `stock.Service.History` zeroes every cost field
outside owner mode and computes which receipt may be reversed, so no future caller can forget either.

**D-L2.i6 — `Stock.Verify` is owner-guarded;** the seeder calls it in owner mode, and `VerifyUnguarded` exists for tests.

**D-L2.i7 — an adjustment is an unsigned quantity and a direction.** `numinput` refuses signs (a leading `-` is how a
typing slip becomes a write-off), so the dialog asks *take out* or *add*, and Go applies the sign.

**D-L2.i8 — what may be done to an inactive product.** Receiving and opening stock are refused (D-L2.12); opening a
package into an inactive content product is refused (§5.7). Counting and writing off an inactive product's stock are
allowed — the shelf still holds it.

**D-L2.i9 — a movement's time is held to the millisecond** in the service, the ledger's precision, so the movement an
act returns is the movement a read returns, on the fake and on SQLite alike.

**D-L2.i10 — the shared number fixture gained `quantities`.** Go's `stock/domain.ParseQuantity` and the frontend's
`quantityProblem` refuse the same inputs with the same code ("3.0" for jars included), so a dialog names a fourth
decimal of a kilo as it is typed.

**D-L2.i11 — the error-code coverage gate reads `Finding*` constants too.** The verifier's findings reach the owner's
banner the same way error codes do.

**D-L2.i12 — `bizdate` joins `lite-pure-text`.** Every movement's date comes from it; a clock or database reaching it
would make "which day" depend on something other than the instant and the zone.

**D-L2.i13 — the product form's package section saves on its own,** with its own buttons, apart from names and price.

**D-L2.i14 — a guarded stock act records quantities** (before → after, in the unit's decimals) and a cost correction
records the averages, so the owner's history reads "15.000 → 13.000", never an internal code.

## 15. FINDINGS

**R1 — ordering by time was a latent defect in the approved schema** (D-L2.i1), found while writing the store's walk.

**R2 — the no-cost rule as written had two holes** (D-L2.i2), found while writing the domain tests for it.

**R3 — the first CHECK test could pass for the wrong reason.** A reversal pointing at a row that did not exist was
refused by its **foreign key**, not by the constraint the case was about. Every refusal case now names the constraint
that must refuse it and asserts the database said so; a valid receipt is inserted first for the rows that must point at
one.

**R4 — package atomicity was tested only on the fake,** whose transactor runs nothing atomically. A SQLite test now fails
the content's level after both rows were written and requires the ledger and both levels unchanged (drill G6).

**R5 — golangci-lint v2 found 17 issues** (16 shadowed `err`, one identical `||` operands in a test). All fixed; 0 issues.

**R6 — test names drifted from §11.1**, which calls them the contract; five were renamed to match
(`TestWeightedAverageLiesBetweenOldAndIncoming`, `TestSplitUnitCostConservesValueWithinTheBound`,
`TestDivideByRateRoundsOnce`, `TestNoLoadLocationAnywhere`, `TestACountNeverMovesTheAverage`).

**R7 — the "the Arabic catalog is actually Arabic" gate refuses a message made only of placeholders.** A key
`"{action} — {name}"` was dropped before it reached a screen.

**R8 — quitting the packaged app with AppleScript reports "User cancelled (-128)"**, yet the application quits, merges
its write-ahead log and leaves no `-wal`/`-shm`. Observed, not investigated; it does not affect a person closing the window.

## 16. EVIDENCE

| Check | Result |
|---|---|
| `make lite-ci` | **pass** — nothing NOT RUN |
| Go tests (race) | **220** Lite test functions, **415** with subtests, 0 failures (L1: 156 / 293); kernel `money` costing tests at 0, 2 and 3 decimals, property tests included |
| Frontend | **225** tests in 19 files, 0 console warnings; bundle gate 4 (L1: 173) |
| golangci-lint v2 | 0 issues |
| archlint | clean; **22** rules seen failing |
| Windows | every Lite package and its test binary cross-compiled; `Mizan Lite.exe` (GUI) and `lite-demoseed.exe` (console) built |
| **Mizan after L2's kernel change** | `scripts/check.sh` green: 86 Go packages, 337 frontend tests, 0 lint issues |

**The seeder and the packaged app, run for real:**

- the seeder, into `l2 seeded محمد #2`: 40 products, 40 opening balances, 3 deliveries, stock value **1440.57 USD**; the
  ledger holds 40 openings, 3 receipts, 1 count, 1 adjustment, 1 `package_out` and 1 `content_in`; no pound row lacks its
  rate; the first movement at 22:38 UTC carries business date **2026-09-14**, the shop's day at +03:00; a second run was
  refused with `lite.demoseed.already_set_up`;
- **an L1 installation upgraded by the L2 app:** a directory seeded by the L1 build (`edb4570`, in a temporary worktree)
  opened in the packaged L2 app — pre-migration backup written, `migration applied version 3 "stock"`, ready at schema 3,
  `health requested by the frontend`; closed with no `-wal`/`-shm` left;
- the L2-seeded directory in the packaged app: ready, the shell reached Go, closed cleanly.

## 17. MUTATION DRILLS — 28, ALL CAUGHT

| # | Planted defect | Caught by |
|---|---|---|
| G1 | the scale conversion removed from `UnitCostFromTotal` | `TestUnitCostFromTotalAtEveryScale` — **fails at 2 and 3 decimals, passes at 0**, as §11.2 predicted |
| G2 | the on-hand ≤ 0 branch removed | `TestWeightedAverageUnit` and `TestReceiptIntoNegativeStockTakesTheReceiptCost` (−759) |
| G3 | a count moves the average | `TestACountNeverMovesTheAverage` |
| G4 | a reversal allowed after a later movement | `TestOnlyTheNewestReceiptCanBeReversed` (domain and service) |
| G5 | the cost-correction CHECK requiring quantity > 0 (H3) | `TestACheckRefusesAnImpossibleRow`: the quantity-0 row refused |
| G6 | opening a package swallowing the second write | `TestOpeningAPackageWritesBothRowsOrNeither` (fake and SQLite) |
| G7 | the verifier's chain check removed | `TestTheVerifierFindsAPlaceSkipped` |
| G8 | a lowering adjustment without the guard | `TestLoweringStockNeedsTheOwnerAndRaisingDoesNot`, `TestStockReachesTheRealOwnerAndCatalogue` |
| G9 | `StockLevelDTO` carrying the average | `TestLevelsCarryNoCost` |
| G10 | costs left in the history outside owner mode | `TestCostsAreOnlyForTheOwner` |
| G11 | `time.LoadLocation` introduced | `TestNoLoadLocationAnywhere` |
| G12 | the business date taken in UTC | `TestAMovementAt0100DamascusBelongsToToday`, `TestAMovementCarriesTheShopsBusinessDate` |
| G13 | the NULL-rate trap restored in the CHECK | `TestACheckRefusesAnImpossibleRow`: "a pound receipt with no rate (NULL)" |
| G14 | one level of packaging not enforced | `TestAPackageOpensOneLevelOnly` |
| G15 | receiving into an inactive product | `TestReceivingIntoAnInactiveProductIsRefused` |
| G16 | opening stock after a movement | `TestOpeningOnlyAsTheFirstMovement` (domain and service) |
| F1 | cost columns shown outside owner mode | "shows what is on hand … with no cost column outside owner mode" |
| F2 | a reversal offered on every receipt | "offers a reversal only on the receipt Go names, through the PIN" |
| F3 | a count sent without `withOwner` | "a count asks what is on the shelf, and a lowering count goes through the owner PIN" |
| F4 | a pound receipt sent without its rate | "a delivery in pounds asks for the rate it was paid at…" |
| F5 | `quantityProblem` allowing one decimal too many | the shared fixture's `quantities`, in Vitest |
| A1–A7 | stock importing catalog; stock/domain importing owner/domain; catalog, owner, settings and setup importing stock; `bizdate` importing `kernel/clock` | archlint, on every run |

## 18. NOT VERIFIED

1. **The Windows build has not run** (O1, unchanged).
2. **The Stock screen has not been looked at** in either language (O5 extends to it). The logs prove the packaged app opens
   the seeded stock and the shell reaches Go; they do not prove the dialogs lay out well, in RTL especially. **Owner:**
   `MIZAN_LITE_DATA_DIR=~/Desktop/lite-demo go run ./cmd/lite-demoseed`, then open the app with the same variable; the PIN
   is printed by the seeder.
3. **The verifier's cost on a large ledger.** It streams the ledger and holds only receipts, reversals and package pairs
   in memory; not measured beyond the demo's 47 rows. L4's sales will make it the table that grows.

## 19. DEFINITION OF DONE

| Criterion (§11.3) | Status |
|---|---|
| `make lite-ci` green | ✅ |
| Every invariant drilled | ✅ 28 of 28 (§11.2's ten, and eighteen more) |
| The seeder's stock opens in the packaged app and the verifier finds nothing | ✅ |
| The Stock screen looked at in Arabic and English | ⏳ owner (§18.2) |
| Windows `.exe` builds | ✅ built — not run (§18.1) |
| PROGRESS, DECISIONS and this document updated | ✅ |

## 20. NEXT

1. ~~The owner reviews D-L2.i1–i14 and approves committing L2~~ — approved and committed 2026-09-14.
2. The owner looks at the seeded shop's Stock screen in both languages (O5).
3. L3's design note: exchange rates — which will pre-fill the rate this phase's receipt form asks for (Q-L2.1).

