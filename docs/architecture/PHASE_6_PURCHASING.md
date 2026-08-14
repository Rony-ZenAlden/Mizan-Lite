# Phase 6 — Purchasing

> POs, bills, supplier returns, landed cost.
>
> §32: *"Completes the stock cycle."* Phase 4 could receive goods and Phase 5 could sell them;
> nothing until now could say where the goods came from, what they cost, or who is owed for them.

---

## 1. ANALYSIS

### 1.1 What earlier phases already left here

The sixth application of the rule this project keeps relearning: **before declaring a new place
for a fact to live, look for the one an earlier phase already left.**

| Left by | What | Never called until now |
|---|---|---|
| Phase 2 | `purchase` posting rule (`purchasing.bill.posted`) | ✓ seeded, never fired |
| Phase 2 | `supplier_payment` rule (`purchasing.payment.made`) | ✓ seeded, never fired |
| Phase 3 | `products.purchase_uom_id`, `is_purchased` | read by nothing |
| Phase 3 | `partners.is_supplier`, `partner_role_usage.purchased_from` | written by nothing |
| Phase 4 | `domain.Revaluation` — value without quantity | **no caller** |
| Phase 4 | `domain.Allocate` — largest-remainder (§D.3 trap 4) | tested, **no caller** |
| Phase 4 | `stock_movements.document_type/document_id` | sales only |
| Phase 4 | `inventory_layers`, written on every receipt | written, never read |
| Phase 5 | `number_series` (platform 0001), the allocator | sales series only |

Four of those are seams built for a caller that did not exist yet. Phase 6 is that caller. If any
of them needs reshaping, the seam was guessed rather than designed — which is the test this phase
applies to Phase 4 the way Phase 5 applied it to Phase 2.

### 1.2 The problem purchasing actually solves

Three things happen, at three different times, in three different quantities:

| Event | When | What it changes |
|---|---|---|
| **We order** | first | nothing — an intention |
| **Goods arrive** | days or weeks later | stock, physically |
| **The invoice arrives** | before, with, or after the goods | what we owe |

They are routinely not 1:1. One order delivered in three shipments. One invoice covering two
orders. An invoice that arrives before the lorry. A delivery of 98 where 100 was ordered.

**A design that collapses them into one document asserts they always coincide.** The consequence
is not cosmetic: it means the instant goods arrive you must also know the final price and accept
the debt, which is false, and produces either stock with no value or a liability for goods nobody
has seen.

### 1.3 The three-way match is the control worth having

Ordered vs **received** vs **billed**, on quantity and on price. It is the single mechanism that
stops a business paying for goods it never got — the most common way money leaves a small
business by accident, and the reason purchasing exists as a discipline separate from paying bills.

It is only expressible if the three documents are distinct and linked.

---

## 2. DESIGN

### D1 — Three documents, linked, never collapsed

```
purchase_orders ──┬──▶ goods_receipts ──┬──▶ purchase_bills
   (intention)    │      (physical)     │      (financial)
                  └─────────────────────┴──▶ three-way match
```

- A **purchase order** moves no stock and writes no journal entry. It is what we asked for.
- A **goods receipt** moves stock and posts to GRNI. It is what arrived.
- A **purchase bill** creates the payable and clears GRNI. It is what we owe.

A bill may reference several receipts; a receipt may partly fill several order lines. The links
are on the LINES, not the documents, because that is the grain at which the quantities differ.

### D2 — GRNI: the receipt posts, and it posts to a clearing account

When goods arrive and no invoice has, the business genuinely holds an asset and genuinely owes
somebody for it. Both facts are true before the invoice exists.

```
Goods receipt:   DR Inventory        (at the ORDER's price)
                 CR GRNI                                     ← "goods received not invoiced"

Purchase bill:   DR GRNI             (reversing what the receipt accrued)
                 DR Recoverable tax
                 CR Accounts payable (the total)
```

**The alternative — receipts post nothing, bills post everything — was rejected.** It leaves
stock on the shelf with no value in the books for as long as the invoice takes, so a period that
closes in that window reports a balance sheet missing its goods. A month-end that lands between a
delivery and its invoice is not an edge case; it is twelve times a year.

GRNI is a **new mapping and a new account** (`2150`), because no existing one means this. It is a
liability, and it should be near zero and old-entry-free — a GRNI balance that ages is exactly the
report a purchasing manager needs.

### D3 — The receipt is valued at the ORDER's price, and the bill corrects it

The receipt has to put a number on the goods before anybody has invoiced them. The order's price
is the best available estimate, and it is the number both parties last agreed on.

When the bill disagrees, the difference is real and must go somewhere:

- **Goods still on hand** → the difference REVALUES the stock. Phase 4's `Revaluation` movement
  type — value without quantity, `Neutral` direction — gets its first caller. It was built in 4.2
  for exactly this and has been waiting.
- **Goods already sold** → the difference belongs to the cost of that sale, which has already
  posted. v1 posts it to `INVENTORY_ADJUSTMENT` rather than reopening a closed sale, and says so
  plainly. Retroactively restating COGS requires reopening periods, which §D.4 forbids for costing
  changes and which is the same argument here.

### D4 — Over-receipt is a POLICY, not an error

Suppliers deliver 102 when 100 was ordered. Whether that is acceptable is a business decision,
and the same decision differs by trade: a fastener wholesaler expects tolerance, a pharmacy does
not.

A company setting with a percentage tolerance, checked at receipt. Refusing outright would make
the software wrong for half its market; accepting silently would remove the control the document
exists for.

The setting is declared with the enum already naming the strictest option, so a shop that wants
zero tolerance is a value change rather than a schema change (the §D.4 pattern).

### D5 — Landed cost allocates across a receipt, by a declared basis

Freight, customs, and clearing are part of what stock cost and must reach inventory value; a
business that expenses them understates its stock and overstates its margin.

Allocated across a receipt's lines by **value, weight, volume, or quantity** — the basis is
declared per charge, because freight follows volume and customs follows value, and forcing one
basis makes one of them wrong.

Phase 4's `Allocate` (largest-remainder) is the mechanism and gets its first caller. §D.3's trap
4: naive proportional allocation leaves the total off by a few minor units on every shipment, and
those units land in nobody's account.

### D6 — A supplier return needs a new movement type, and it is not `ReturnIn` reversed

Phase 4 has `ReturnIn` — a customer gives goods back to us. A supplier return is the opposite
direction and a different fact, so it is `ReturnOut`.

It carries `SourceMovementID` for the same reason `ReturnIn` does (§D.3): it must be costed at
**the original receipt's** cost, not today's average. Returning goods bought at last year's price
at this year's average invents a gain or a loss that never happened.

Adding a movement type means one entry in `directions` — the single table Phase 4 built so that
this is one edit rather than three switches to disagree.

### D7 — The seeded `supplier_payment` rule has the defect sales already found

```json
"code": "supplier_payment",
"lines": [{ "side": "debit", "account": "mapping:AP" },
          { "side": "credit", "account": "mapping:CASH" }]
```

**It credits CASH for every method** — a bank transfer to a supplier would reduce the till. This
is precisely the defect found in 5.5 on the receiving side, where payments debited CASH whatever
the method, and it is fixed the same way: four per-method rules, entirely in seed data, with no Go
changed. That the fix is again data-only is the §20.3 design paying for itself twice.

### D8 — Numbering: four series, from the platform's table

`PURCHASE_ORDER`, `GOODS_RECEIPT`, `PURCHASE_BILL`, `SUPPLIER_PAYMENT`. Allocated at POSTING, per
§9.4, exactly as sales does — and reusing sales' allocator rather than a second one.

A goods receipt takes a number when it is CONFIRMED, not when drafted, so an abandoned delivery
note leaves no hole an auditor asks about.

### D9 — The supplier's own product codes

A supplier calls it `ACM-4471`; we call it `WIDGET`. Purchase orders that go out under our code
get filled wrong, and delivery notes that come back under theirs cannot be matched.

A small mapping table, keyed by partner and variant, carrying the supplier's code and their pack
quantity. It is not a price list — Phase 3 owns pricing, and purchase prices live there under the
`Purchase` direction that 3.6 already built and nothing has called.

---

## 3. WHAT THIS PHASE DOES NOT DO

Stated so the boundary is a decision rather than an omission:

- **No requisitions or approval workflows.** A purchase requisition and an approval chain are an
  organisational control for businesses with departments. The target business owner approves a
  purchase by placing it.
- **No supplier quotations or tendering.** Comparing three quotes is a procurement function, not
  a trading one.
- **No foreign-currency purchase revaluation at period end.** §18 covers the transaction; the
  period-end revaluation of open payables belongs with the rest of period close.
- **No drop-shipping.** A purchase that never touches our warehouse is a different stock story and
  earns its own design when something asks for it.

---

## 4. STEP SEQUENCE

| Step | Contents |
|---|---|
| **6.1** | The module, its schema, numbering, and the purchase order |
| **6.2** | Goods receipt: partial deliveries, over-receipt tolerance, GRNI |
| **6.3** | The purchase bill, the three-way match, and posting |
| **6.4** | Price variance: revaluation when the bill disagrees with the order |
| **6.5** | Landed costs |
| **6.6** | Supplier returns and debit notes (`ReturnOut`) |
| **6.7** | Supplier payments, and the per-method seed fix |
| **6.8** | Bindings and screens |
| **6.9** | Phase 6 Definition-of-Done review |

---

## 5. DEFINITION OF DONE

1. A purchase order moves no stock and writes no journal entry.
2. One order can be received in several deliveries, and the outstanding quantity is always the
   ordered quantity less what has actually arrived.
3. A receipt beyond the tolerance is refused; within it, accepted and recorded as an over-receipt.
4. A goods receipt debits inventory and credits GRNI; the bill clears GRNI exactly, leaving no
   residue when quantities and prices agree.
5. A bill whose price differs from the order revalues stock still on hand, through Phase 4's
   `Revaluation` movement — quantity unchanged, value changed.
6. A bill can cover several receipts, and a receipt can be billed only once.
7. Landed costs reach inventory value, allocated by the declared basis, tying to the charge
   exactly (largest-remainder).
8. A supplier return is costed at the ORIGINAL receipt's cost, not today's average.
9. A supplier payment posts to the account its METHOD implies, not to cash for everything.
10. Purchasing contains no accounting logic; every posting goes through Phase 2's rules.
11. A number is allocated only at posting/confirmation; an abandoned draft consumes none.
12. Every new binding has a declared policy; every state change is audited in-transaction.
13. Every seam Phase 4 left (`Revaluation`, `Allocate`, `inventory_layers`, the document link) is
    called by real code, and none of them needed reshaping to serve it.
14. `make ci` green, with the mutation drills each step declares — and a drill that PASSES is
    treated as a defect in the test, the code, or the mutation, per the five outcomes Phase 4
    recorded.

---

*Implementation proceeds step by step; each step records its own decisions and drills.*

---

## Step 6.1 — the module, its schema, numbering, and the purchase order

**Delivered.** `0027_purchasing.sql`, the order domain, the service (draft, add line, remove line,
place, cancel, close), and — unplanned but required — the extraction of the number allocator into
`internal/platform/numbering`.

### The allocator moved, and sales 0023 had already said why

`AddLine` needed a number for a purchase order, and there was no way to get one:

- `sales.allocateNumber` is unexported, deliberately — "no caller outside this module can take a
  number without a document to attach it to."
- `AllocateNumberForTest` lives in `export_test.go`, so it is not even visible outside sales' own
  test binary.
- Purchasing cannot import sales (`module-isolation`).
- A second allocator against the same table would be correct alone and race with the first.

Sales 0023 wrote the answer down a phase early, about the TABLE:

> *"purchasing (Phase 6) and payments will number documents too, and a series table owned by
> sales would make them either import sales or build a second one."*

The allocator has exactly the same property, so it followed its table into platform. Sales now
delegates and its public API is unchanged — **every Phase 5 numbering test passes untouched**,
which is what makes the move safe to believe.

The error codes moved with it (`sales.invalid_series` → `numbering.invalid_series`, and four
more), because a code is an i18n key and a key that says `sales` for a purchase order is a lie the
user reads.

### D1 — three tables, where sales has one

Sales models quotation → order → invoice as one table maturing: same lines, same quantities, one
becoming the next. Purchasing is not that shape, and copying the pattern would be the wrong kind
of consistency. An order, a delivery, and an invoice describe three different events that
routinely disagree — one order in three deliveries, one invoice over two orders, 98 arriving
where 100 were ordered. The links live on the LINES, because the line is the grain at which the
quantities differ.

### D2 — `received_micro` is a maintained projection, not a sum

Like `stock_levels`: the receipt lines are the truth and this is the answer to "what is still
outstanding" without summing them on every screen. It is rebuildable, and 6.2 ships the check.

### D3 — a supplier is required, where a customer is not

A shop sells to whoever walks in and §2.6 requires that a walk-in need no record. Nobody orders
from nobody — the whole content of the document is that a named party has undertaken to deliver.

### Two Phase 3 seams got their first reader

`products.purchase_uom_id` and `products.is_purchased` have existed since 3.2 and nothing has
ever read either. `catalog.PurchaseFacts` now does, extracted from `SaleFacts` so the two share a
body and differ in exactly the one decision that separates them — which unit a caller who names
none gets.

Flour is stocked and sold by the gram and bought by the kilogram. An order for 2 that defaulted to
the sales unit would be 2 grams where the buyer meant 2 kilograms: **wrong by a factor of a
thousand, in the direction nobody notices until the lorry arrives.**

**Mutation drills — 5 run, 1 passed.**

| # | Mutation | Result |
|---|---|---|
| 67 | An order defaults to the SALES unit | fails |
| 68 | `is_purchased` is ignored | fails |
| 69 | The number is allocated FIRST, before anything that can fail | **Passed.** |
| 70 | Outstanding is allowed to go negative | fails |
| 71 | A placed order accepts more lines | fails |

### Drill 69: a comment that overclaimed, in two modules

Moving the allocation to the very top of `Place` — before the pricing that can fail — changed no
test. The reason is that `AdvanceSeries` runs inside `db.Do`: **a rollback takes the counter back
with everything else.** The ordering is not what protects the number; the transaction is.

The comment said otherwise — "§9.4's rule expressed as an ordering rather than as a comment" — and
the same claim sat in `sales/posting.go`, written in 5.3 and believed since. Both are corrected.
The ordering is kept (it avoids a pointless counter write on the failure path) and **no test pins
it**, because a test that passes either way claims something is pinned when nothing is — 4.3's
rule, and the third time this project has taken that particular outcome.

The first bad mutation of the phase came first: the initial D69 moved the call but left it after
the failure point, so it proved nothing. Fifth bad mutation recorded.

---

## Step 6.2 — goods receipt: partial deliveries, over-receipt tolerance, GRNI

**Delivered.** `0028_receipts.sql`, the receipt domain and tolerance rule, the service (draft,
receive line, confirm, cancel, verify), the **GRNI account and mapping**, and the
`goods_receipt` posting rule.

### D1 — a receipt is its own document because somebody signs for it

Marking quantities received on the order itself loses the one thing a delivery note is for: WHICH
delivery. A dispute about a short shipment three months later is settled by *"the second delivery,
on the 14th, signed by Yusuf"* — not by an order line whose received figure has been edited four
times with no record of by whom.

`order_id` is nullable, because every business takes goods against no order: a replacement for
damaged stock, or a cash-and-carry purchase. The three-way match then has one fewer side.

### D2 — the receipt posts, and it posts to GRNI

When goods arrive and no invoice has, the business genuinely holds an asset and genuinely owes
somebody for it. Both facts are true before the invoice exists.

Receipts posting nothing would leave stock on the shelf with no value in the books for as long as
the invoice takes — and a month-end landing in that window is not an edge case, it is twelve times
a year. GRNI (`2150`) is a new account and mapping, because no existing one means this.

The `purchase` rule was reworked to match: the bill now **clears GRNI** rather than debiting
inventory, because the receipt already did that. Both changes are seed data.

### D3 — no tax on a receipt

Tax arrives with the INVOICE. Accruing it when goods arrive would put a recoverable asset on the
books that no document supports, and a revenue authority asking for the invoice behind it would be
told there isn't one.

### D4 — the tolerance is measured against what is OUTSTANDING, not what was ordered

An order for 100 delivered as 60 then 45 is not a 5% over-delivery on the second note; it is 5
more than the 40 still owed, which is 12.5%. Measured against the ordered quantity instead, each
of five deliveries could be 2% over and the total 10% over — with every individual note passing.
That is the arithmetic a supplier who wants to over-ship relies on.

The check runs **at entry**, where somebody is standing at a loading bay and can count again or
telephone the supplier — not at confirmation, after twenty lines have been keyed.

### D5 — `received_micro` is verified, never repaired

4.3's discipline on a second projection. A projection that silently heals hides the bug that broke
it, and the next thing it hides is the one that mattered.

**Mutation drills — 7 run, 4 passed.** All four are now resolved.

| # | Mutation | Result |
|---|---|---|
| 72 | The over-receipt check is skipped | 2 tests fail |
| 73 | The tolerance divides before multiplying | **Passed → test strengthened** |
| 74 | A receipt with no order enters stock unvalued | **Passed (bad mutation) → redone, fails** |
| 75 | The received projection is never advanced | fails |
| 76 | A confirmed delivery posts nothing | 3 tests fail |
| 77 | The receipt rule accrues recoverable tax | **Passed — a genuine no-op** |
| 78 | The verifier counts draft deliveries | **Passed → test written** |

### The defect a drill uncovered by accident

`TestPurchasingNamesNoAccounts` redirected GRNI to another account and asserted that account
received the money. It received nothing — and so did GRNI. **Nothing had posted at all.**

The cause: a receipt against no order took its unit cost from the order line it did not have, so
the cost was zero, so the value was zero, so Phase 2 skipped every rule line and wrote no entry.
**Goods from a cash-and-carry were entering stock worth nothing** — which understates inventory
and overstates margin the day they are sold, surfacing months later as a gross profit nobody can
explain.

`ReceiveLineInput.UnitCostMicro` is now a `*int64`, because nil and zero are different answers and
both are legitimate: nil means "resolve it from the purchase price list", and a pointer to zero
means "these were genuinely FREE", which a warranty replacement is. A plain `int64` collapses the
two, and the collapse has exactly one direction — stock entering at zero because nobody typed a
price.

### Drill 73: whole units hid a real defect

Swapping `outstanding × percent / 10⁶` for `outstanding / 10⁶ × percent` gives the *same answer
for every whole-unit quantity*, and every test ordered whole widgets. They diverge exactly where
tolerance matters most — goods measured rather than counted. 1.5 kg outstanding at 2% allows
0.03 kg; dividing first truncates to 1 kg and allows 0.02, so a delivery the business said was
acceptable is refused at the loading bay.

### Drill 77: two layers, individually inert

A receipt accrues no recoverable tax because the service publishes no tax amount **and** the
seeded rule has no tax line. Breaking either alone changes nothing — Phase 2 skips a rule line
that resolves to zero, so a tax line with no tax behind it is indistinguishable from no tax line.

No extra test was written. One that could fail on a single-layer mutation would have to assert
something neither layer actually promises, and a test that pins nothing is worse than none (4.3).

---

## Step 6.3 — the purchase bill, the three-way match, and posting

**Delivered.** `0029_bills.sql`, the bill domain and the match, the service (draft, add line,
post, cancel, report), and the reworked `purchase` posting rule. Phase 2's
`purchasing.bill.posted` rule fired for the first time.

### D1 — a bill line takes a receipt line IN FULL

Not a partial quantity of one. The receipt line is already the record of what physically arrived
in one delivery, at one time, signed for by one person — it is the natural unit of *"this much was
received"*, and suppliers do not invoice half a delivery line.

Partial take-up would mean tracking how much of each receipt line remains unbilled: a third
projection to maintain and reconcile, in exchange for a case nobody has. Where a supplier really
does split an invoice, the answer is two bills each taking whole lines — which is what the paper
looks like anyway.

**This is what makes GRNI clearing exact rather than approximate.**

### D2 — quantity is guaranteed by CONSTRUCTION; price is reported

The design's most useful outcome, and it emerged from writing the test.

A bill line **must** name a receipt line (`NOT NULL`) and takes its quantity from it. There is no
field through which a different quantity could be expressed, so *"the supplier is invoicing for
goods that never arrived"* is **not a state this schema can hold** — rather than one it validates
against.

A runtime `RequireQuantityMatch` was written first and then **deleted**: it compared the quantity
with the value it had just been assigned from, so it could never fire. An unreachable guard is
worse than none, because a test for it passes whatever the code does (4.3). The test now asserts
the structural property, which is what actually holds.

Price is the opposite. A different price is ordinary — a surcharge, a currency movement, a price
agreed by telephone and never recorded. Refusing it would leave the goods on the shelf, the GRNI
accrued, and no way to close the loop except by editing the order retrospectively, which is worse
than the variance. So it is **booked and reported**, never refused.

### D3 — GRNI is cleared at the ACCRUED figure, never the bill's own net

```
DR GRNI              exactly what the receipts accrued
DR/CR variance       the difference between ordered and charged
DR recoverable tax
CR accounts payable  the total
```

Clearing the bill's net instead would leave the price difference sitting in GRNI forever — and a
GRNI balance nobody can explain is a GRNI balance nobody reads, which costs the business the one
report that says what it has received and not been invoiced for.

### D4 — the variance is two amounts, exactly one non-zero

The posting engine refuses negative amounts, because a negative would silently flip a line to the
other side of the entry — a credit meant to be a debit, balancing perfectly and meaning the
opposite. So `document.variance_over` and `document.variance_under` are two rule lines; the other
resolves to nothing and Phase 2 skips it.

The same shape as Phase 5's `cash_short` and `cash_over`, and the fifth application of: **when the
books must differ, the ACTION differs, never the module's knowledge of accounts.**

### D5 — the supplier's own invoice number is required

Ours is for our filing; theirs is what a payment reference quotes and what a statement
reconciliation matches on. Unique per supplier, because **the same invoice entered twice is a
payment made twice** — and a duplicate number from a *different* supplier is entirely ordinary.

Two layers keep it: a service pre-check that names which invoice and which supplier, and a unique
index that holds when two clerks enter the same invoice at the same moment. Drill 82 confirmed
both — removing the pre-check made the test fail with `purchasing.storage`, which is the index
catching it.

**Mutation drills — 7 run, 0 passed** (one bad mutation, redone).

| # | Mutation | Result |
|---|---|---|
| 79 | GRNI cleared at the bill's net | 2 tests fail |
| 80 | A negative variance reaches a rule line | 2 tests fail |
| 81 | A delivery can be billed twice | fails |
| 82 | The duplicate-invoice pre-check removed | fails — via the index |
| 83 | Posted bills never mark deliveries billed | 2 tests fail |
| 84 | Another supplier's delivery can be billed | fails |
| 85 | The rule drops both variance lines | 2 tests fail |

---

## Step 6.4 — price variance: revaluing what the bill disagrees about

**Delivered.** `inventory.RevalueBy`, `domain.SplitByWhereTheGoodsAre`, the four-way variance
posting, and `0030_revaluation_quantity.sql`.

### The finding that justifies the whole phase

**Phase 4's `Revaluation` movement could not be written.** The type existed since 4.2. The
direction table knew it. The costing strategy had a `revalue` case. It appeared in the schema's
`CHECK` list. And **every path to it ended in a validation error**, because two separate
positive-quantity guards — one in the domain constructor, one in the table's `CHECK` — refused a
movement of zero, which is the only kind a revaluation is.

The seam was built, documented, unit-tested, and structurally unusable. It was found the moment
something needed it.

> **A seam is only proven by a caller.** Everything about this one looked right from the inside.

The phase's Definition of Done asks whether Phase 4's seams needed reshaping to serve a real
caller. For three of them the answer is no. For this one it is **yes**, and the reshaping is a
table rebuild.

### D1 — the caller states a VALUE; inventory decides the average

A bill knows one thing: these goods are worth N more than we thought. It does not know — and must
not learn — whether the business runs weighted average or FIFO, or what the current average is.
That is the costing strategy's business and the whole point of the port Phase 4 built.

`RevalueBy` converts a delta into whatever the strategy needs. Under WAC that is a new average:
the delta spread across what is actually on hand.

### D2 — the split is by WHERE THE GOODS ARE

- **Still on the shelf** → revalue them. Fixes the balance sheet and every future sale.
- **Already sold** → the cost of that sale posted at the old figure, in a period that may be
  closed. §D.4 forbids reopening periods to restate costing and the same argument holds here, so
  the correction goes to an adjustment account: visible and explainable, rather than history
  rewritten.

The proportion is **capped at what this delivery brought in**, because on-hand stock includes
other receipts and revaluing those would correct goods at a price this bill says nothing about.

### D3 — four variance amounts, at most two non-zero

Stock and expense, each over and under. Four names rather than two signed ones, because the
posting engine refuses negatives and a negative would silently flip a line to the other side of
the entry.

### D4 — the revaluation is a MOVEMENT, not only a journal line

The stock ledger carries its own value, and the two must not disagree. A journal-only correction
would leave the trial balance right and the stock valuation wrong — which surfaces at a stock
count, months later, as a discrepancy nobody can trace.

**Mutation drills — 6 run, 2 passed.** Both resolved.

| # | Mutation | Result |
|---|---|---|
| 86 | The whole-on-hand shortcut removed | **Passed — redundant code, deleted** |
| 87 | More than this delivery's quantity is revalued | fails |
| 88 | Both halves rounded separately, losing a unit | fails |
| 89 | A revaluation written against empty stock | **Passed → test written at the inventory layer** |
| 90 | The currency scale ignored in the unit-cost conversion | 2 tests fail |
| 91 | The revaluation exemption removed again | 3 tests fail |

**Drill 86** deleted a `remaining == received` shortcut and nothing failed — because the
proportional path already answers `variance × received / received`, which is the variance. Deleted:
redundant code that makes a drill on the real path ambiguous is worse than no code.

**Drill 89** showed a guard that purchasing can never reach: its own split returns nothing against
stock when nothing is on hand. The guard is real for other callers, so it now has a test at the
inventory layer that can only pass if that layer keeps it — the Phase 3 rule, applied where it
belongs.

---

## Step 6.5 — landed costs

**Delivered.** `0031_landed_costs.sql`, the charge domain and its allocation, the service, the
`landed_cost` posting rule — and **`round.Allocate` promoted to the kernel**.

### The allocator was written four times before it was written once

Phase 6 needed largest-remainder allocation for freight. Three implementations already existed:

| Where | Since | Why it could not be reused |
|---|---|---|
| `money.Money.Allocate` | Phase 0 | requires a currency; column widths and quantities are not money |
| `inventory/domain.Allocate` | Phase 4 | lives in a MODULE; platform and other modules must not import one |
| `printing.Widths` | Phase 5 | its own copy, written after checking the other two |

At the fourth, *"look for the one an earlier phase already left"* stopped meaning **find the copy**
and started meaning **stop making copies**. `round.Allocate` is in the kernel — the one place every
layer can reach — and inventory, printing, and purchasing all delegate to it. The golden print
files were unchanged by the swap, which is what makes the extraction safe to believe.

**The extraction fixed a real defect.** The version it was lifted from implemented the
even-spread case by writing `1` into the caller's slice. A caller that still needed those weights
— to allocate a *second* charge across the same lines, which is exactly what landed costs do —
got them back full of ones, and every charge after the first spread evenly regardless of value.

### D1 — freight is not an expense

A business that books it to one understates what its stock cost and overstates its margin on every
sale. The goods on the shelf really did cost the invoice plus the lorry plus the customs officer,
and a gross margin computed without them looks healthy and is not.

### D2 — the basis is declared per charge

Freight follows volume or weight — a lorry is full when it is full, whatever is in it. Customs
follows value, because that is what duty is charged on. Forcing one basis makes the other wrong,
and a shipment usually carries both kinds of charge at once.

`weight` and `volume` are named in the enum and **refused at runtime**: they need product
dimensions the catalog does not carry. A freight charge spread by value when the operator asked
for weight is wrong in a way nobody would ever notice, so it fails loudly instead.

### D3 — allocations are STORED, not recomputed

The split depends on the receipt's line values *at the moment it was applied*. A later price
correction (6.4) changes those values, and recomputing would silently restate a charge that has
already reached the stock ledger and the books.

It is also the answer to *"why is this item carried at that cost"* — the question a margin nobody
expected always ends in.

**Mutation drills — 6 run, 0 passed.**

| # | Mutation | Result |
|---|---|---|
| 92 | The allocator drops its leftover units | 2 tests fail |
| 93 | One line collects every leftover | 2 kernel tests fail |
| 94 | The allocator rewrites the caller's weights | fails |
| 95 | An unsupported basis is accepted | fails |
| 96 | Every basis behaves like value | fails |
| 97 | A charge can be applied twice | fails |

---

## Step 6.6 — supplier returns

**Delivered.** `0032_returns.sql`, the return domain and its bounds, the service, the
`supplier_return` posting rule, and `ReturnOut` costing in inventory.

### The defect this step uncovered

**Every costed value in the system was in MAJOR units, from fields named `…Minor`.**

`valueOf` multiplies a 10⁻⁶ quantity by a 10⁻⁶ unit cost and divides by 10¹², giving an amount in
major units. Money is stored in **minor** units. The conversion needed the currency's scale and
did not have it.

It survived Phases 4 and 5 because **every test that consumed a costed value used SYP, which has
no minor unit.** At scale 0 the two numbers are identical, so the conversion was only ever
exercised where it could not be wrong. The first test in a two-decimal currency found it
immediately: a return of two items at 10.00 was costed at 20 minor units instead of 2,000.

In production this would have made **every cost of goods sold a hundredth of the truth** for any
business trading in a currency with minor units — which is almost all of them. Gross margin would
have looked extraordinary and the inventory valuation would have been off by two orders of
magnitude.

> **A scale conversion tested only at scale 1 is a conversion nobody has tested.**

The fix reads the functional currency's scale in the service and carries it on the movement, so no
caller can forget it. `scale_test.go` now exercises all three costing paths at 0, 2, and 3
decimals.

### D1 — a debit note, not a negative bill

A negative bill would balance arithmetically and mean the wrong thing everywhere else: the posting
engine refuses negatives because they silently flip a line to the other side of an entry, and a
report filtering `status = 'posted'` would count the return as a purchase. Same reasoning Phase 5
applied to credit notes — **when the books must differ, the ACTION differs.**

### D2 — costed at the original delivery, never today's average

§D.3, in the direction `0030` added `ReturnOut` for. The movement names the receipt's movement as
its source and inventory reads the cost from there. Returning goods bought at last year's price at
this year's average invents a gain or a loss that never happened.

The average of what **remains** moves too: taking goods out at a cost different from the average
changes the average of everything left, and leaving it alone would park the difference in the
valuation of stock that never went anywhere.

### D3 — two bounds, for different reasons

Returning **more than arrived** is a debit note for goods the supplier never sent — the mirror of
being invoiced for goods that never arrived. Returning **the same goods twice** is subtler: each
return looks reasonable alone, and only the running total shows fourteen of ten leaving. Only
POSTED returns count against the bound, or a second genuine return would be refused while the
first was still being typed.

### D4 — an explicit cost is ignored when there is an order

A delivery against an order is worth what the order said until the invoice says otherwise. Letting
whoever keys the delivery note set a cost would put an unagreed number into the valuation with no
document behind it, and the three-way match would have nothing to compare.

**Mutation drills — 5 run, 0 passed.**

| # | Mutation | Result |
|---|---|---|
| 98 | The currency's scale is not read | fails |
| 99 | A supplier return leaves at today's average | fails |
| 100 | More can go back than arrived | 2 tests fail |
| 101 | Draft returns count against what is left | fails |
| 102 | The return names no source movement | 4 tests fail |

---

## Step 6.7 — supplier payments, and the per-method seed fix

**Delivered.** `0033_payments.sql`, the payment domain, the service, and **four per-method posting
rules replacing the one Phase 2 seeded.**

### The defect Phase 2 seeded, on the paying side

```json
"code": "supplier_payment",
"lines": [{ "side": "debit",  "account": "mapping:AP" },
          { "side": "credit", "account": "mapping:CASH" }]
```

**It credited CASH whatever the method.** A bank transfer to a supplier would have reduced the
till — and the till would have been short at every close with no transaction to explain it, while
the bank reconciliation carried a payment the books said never happened.

The identical defect existed on the receiving side and 5.5 found it there. Both are fixed the same
way and **entirely in seed data**: four actions, four rules, and no Go that knows which account any
of them touches. That the fix is data-only twice over is the §20.3 design paying for itself a
third time.

### D1 — the same two-table shape as sales, and not the same table

0024's argument transfers unchanged: `paid_minor` on a bill cannot express one payment settling
several bills, one bill taking several payments, or a prepayment allocated to nothing.

It is **not** `sales_payments` with a direction column. One table would need a nullable foreign key
to each document type and a CHECK that exactly one is set — a discriminated union hand-rolled in
SQL, with a direction filter on every query that somebody eventually writes without. It would also
make sales and purchasing share a table, which `module-isolation` forbids so that neither can
change shape without the other's agreement.

### D2 — a payee is required, where a customer payment's payer is not

A shop takes cash from whoever walks in. Money *leaving* the business goes to somebody, and a
payment with no payee is a hole in the cash position nobody can chase.

### D3 — over-allocation is refused; under-allocation is not

Allocating more than the payment is arithmetic that cannot be true — the money does not exist.
Allocating less is a prepayment with a balance still to assign, which is ordinary: a business
paying a round figure against a statement leaves a few units unassigned, and forcing them to
balance would mean inventing an allocation.

### D4 — what a bill still owes is derived, never stored

A maintained `paid_minor` drifts from the allocations that justify it, and the drift is invisible
until somebody chases a supplier for money already sent.

**Mutation drills — 6 run, 1 passed.**

| # | Mutation | Result |
|---|---|---|
| 103 | Every method credits CASH (the seeded defect) | 3 subtests fail |
| 104 | The posting action ignores the method | 3 subtests fail |
| 105 | A bill can be overpaid | fails |
| 106 | A payment allocates more than it is worth | fails |
| 107 | A draft bill can be paid | fails |
| 108 | Draft payments count as settled | **Passed → test written at the repository** |

**Drill 108** removed the `status = 'posted'` filter from the settled-amount query and nothing
failed — because `Pay` drafts and posts in one transaction, so no draft payment ever carries
allocations through the service. The filter is still right, and the day a draft-payment flow is
added is the day it matters. It is now asserted by writing a draft payment **straight to the
table**, which is the Phase 3 rule applied to a guard the service cannot reach.

---

## Step 6.8 — bindings and screens

**Delivered.** The composition-root wiring, six port adapters, the `Purchasing` façade (23
methods), the typed TypeScript surface, three screens, and a new structural test.

### The module was never wired into the application

Five steps of purchasing existed and **none of it ran**. Every test drove the service directly;
`bootstrap.Start` had never heard of it. This step found that only because a binding needed
`app.Purchasing`.

It is the same shape as drill 44 — a thing registered in one place and absent from another, with
nothing connecting them — and it turned out there were **two** such lists, not one:

| List | Purpose | Symptom when a module is missing |
|---|---|---|
| `bootstrap.Start` | the real graph, with real services | nothing works; the façade has no service |
| `DeclarationModules` | nil-service modules, for enumerating permissions | every binding requiring one of its permissions is **permanently unreachable** |

The second is the nastier one. A permission no module declares cannot be granted to anybody, so
the method is not refused — it is unreachable, which reads as a configuration problem rather than
a missing registration.

`TestEveryModuleTheApplicationRunsAlsoDeclaresItself` now compares the two **in both directions**:
a module that runs must declare itself, and a module that declares must run. The comparison is
against the running application, which is the thing that cannot be forgotten.

### D1 — four grants, not one

Ordering, receiving, billing, and paying are separate permissions. One person who can do all four
can pay a supplier for nothing, and **no amount of matching detects it** because they control
every side. The three-way match is only a control if the sides are held by different people.

### D2 — the GRNI list sits on the bills screen

A bills list answers *"what do we owe"*. The GRNI list answers *"what have we received and not
been invoiced for"* — and a delivery sitting there for weeks is either a missing invoice or goods
nobody charged us for. They are the same question from opposite ends, and separating them means
the second one is never opened.

### D3 — outstanding is shown per LINE

An order is rarely late as a whole. It is late in one line, and a document-level "partly received"
tells a buyer nothing they can act on. The line's outstanding figure is what a telephone call to
the supplier is about.

**Mutation drills — 5 run, 0 passed** (two bad mutations, redone).

| # | Mutation | Result |
|---|---|---|
| 109 | A running module missing from `DeclarationModules` | fails |
| 110 | A declared module never runs | fails |
| 111 | The façade missing from `All()` | fails |
| 112 | The façade never attached | fails |
| 113 | A binding method has no declared policy | fails |
