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
