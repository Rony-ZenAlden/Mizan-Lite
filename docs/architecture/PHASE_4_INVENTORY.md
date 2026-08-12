# Phase 4 — Inventory & Costing (Design)

> Status: **DESIGN — implemented step by step under the standing autonomous mandate.**
> Scope: the stock ledger, the costing strategy port with Weighted Average Cost, receipts,
> issues, adjustments, transfers, lots and serials, stock counts, and the screens that read them.
> **Out of scope:** anything that sells or buys (Phases 5–6). This phase builds *how much of a
> thing there is and what it cost*; the next builds *the documents that move it*.

---

## 1. ANALYSIS

### 1.1 What makes this phase risky

Phase 2's danger was books that balance and are wrong. Phase 3's was a schema that works and
cannot grow. **This phase has both, plus a third: numbers that are wrong in a way nobody can see
until a stock count.**

Inventory is where the two halves of the system meet. A stock movement is simultaneously a
quantity fact and a money fact, and they must agree forever: the value of what is on the shelf
must equal the balance of the inventory account, on every day, without anybody reconciling them
by hand. If they drift, the first symptom is a gross-margin figure that is quietly wrong — and by
then the drift is months deep.

So this phase is built the way Phase 2 was, and for the same reason: **the invariant is a
property of construction, and a job exists whose only purpose is to prove it still holds.**

### 1.2 The source of truth is the movement ledger — §21.2

```
stock_movements   APPEND-ONLY. Every in, out, adjustment, transfer.
stock_levels      A maintained PROJECTION. Rebuildable, and verified against the ledger.
```

Exactly the shape Phase 2 gave the general ledger: `journal_lines` is the truth,
`account_balances` is a projection, and `RebuildAndVerifyBalances` proves they agree. That
pattern is reused rather than reinvented, and the reasoning transfers unchanged — a projection
that cannot be rebuilt is a number nobody can defend, and one that is never *checked* is a number
nobody should trust.

### 1.3 Layers are written even when nothing reads them — §D.2

`inventory_layers` gets a row on **every receipt**, in average-cost mode as much as in FIFO.

Under WAC those rows are recorded and unused. That costs one insert per receipt and buys the
thing §33.3 asked for: switching a company to FIFO is a configuration change plus a recompute,
**not a migration**. If layers were created only once FIFO was switched on, the switch would
require reconstructing purchase history from movements — approximate at best, and impossible once
opening balances exist.

This is the phase's clearest case of paying a small, certain cost now to avoid an unbounded one
later, which is the §26 argument that has run through every phase.

### 1.4 Costing is a port, not an if-statement — §D.1

`CostingStrategy` is an interface with one implementation in v1. Nothing outside it knows which
method is in use: sales asks for the cost of an issue and does not care whether the answer came
from an average or a layer.

The temptation is to write WAC inline and "extract the interface when FIFO arrives". That fails
for a specific reason: the call sites are what would need changing, and by then there are call
sites in sales, purchasing, adjustments, transfers, counts, and three reports. The port costs
almost nothing today and is the difference between a configuration change and a rewrite.

### 1.5 The four arithmetic traps — §D.3

Naive implementations get all four wrong, and each is silent:

| Case | Naive result | What must happen |
|---|---|---|
| Receipt when on-hand is zero or negative | Division by a non-positive quantity | The new cost **is** the receipt cost |
| Issue with insufficient stock (where permitted) | Cost silently absorbed | Issue at current average, record a `cost_variance` so it is visible and postable |
| Return of an item bought last year | Costed at *today's* average → phantom profit | Costed at the **original issue's** cost, via `source_movement_id` |
| Landed costs across a receipt's lines | Rounding leaves the total off by pennies | Largest-remainder allocation, so it ties exactly |

The third is why a return document line must carry its source line — designed in from the start,
because retrofitting it means every historical return has no source and no correct cost.

### 1.6 Inventory contains ZERO accounting logic

Phase 2 built table-driven posting (§20.3) precisely so that a module which moves goods never
learns debits and credits. Inventory publishes a `Postable` event with amounts; the posting rules
decide which accounts. A stock adjustment that hits a different expense account in a different
country is a seed-file change.

This is already proven — sales will do the same in Phase 5 — and this phase must not be the one
that breaks it.

### 1.7 Risks

| Risk | Consequence | Answer |
|---|---|---|
| Levels drift from movements | Valuation wrong, nobody notices until a count | Rebuild-and-verify job, as Phase 2's ledger has (§2.2) |
| Stock value ≠ inventory account balance | Gross margin quietly wrong for months | An integrity check that reconciles the two (§2.7) |
| Layers only under FIFO | Switching method needs a migration | Written always (§1.3) |
| Costing inline | FIFO becomes a rewrite | Strategy port (§1.4) |
| Return costed at today's average | Phantom profit | `source_movement_id` on the return (§1.5) |
| Negative stock silently allowed | Sells what does not exist | Configurable, blocked by default, variance recorded when permitted |
| `stock_uom` changed after movement | Every historical quantity restated | Already refused in Phase 3 (3.2 D-notes) |
| Concurrent issues oversell | Two sales, one item | The level row is the lock; movements serialise on it |

---

## 2. DESIGN

### 2.1 Movements — the append-only ledger

```
stock_movements
  id, company_id, warehouse_id, variant_id, product_id
  movement_type      receipt|issue|adjustment|transfer_out|transfer_in|count|revaluation
  quantity_micro     ALWAYS POSITIVE — direction comes from the type
  unit_cost_micro    the cost applied to THIS movement
  value_minor        quantity × unit cost, the money this movement moved
  balance_after      the resulting on-hand, recorded at write time
  source_movement_id for a return, the issue it reverses
  document_type/id   what caused it (Phase 5/6 fill these)
  reason, occurred_at, created_at
```

**`quantity_micro` is always positive** and the type carries the direction. A signed quantity
makes `SUM(quantity)` meaningful and every other query a minefield: "how much did we receive
this month" becomes a filtered sum that somebody eventually writes without the filter.

**`balance_after` is recorded, not derived.** The same reasoning as `normal_balance` in Phase 2:
storing a derivable value is normally a smell, and here it is the point — it is what lets the
verifier catch a projection that has drifted *and say where it started*.

### 2.2 Levels — the projection

```
stock_levels  variant_id, warehouse_id → qty_on_hand, qty_reserved, avg_cost_micro
              (qty_available is derived: on_hand − reserved)
```

Rebuildable from movements, and **verified** by a job that replays the ledger and compares. The
Phase 2 precedent is exact, including that the job reports rather than silently repairs: a
projection that heals itself hides the bug that broke it.

`qty_reserved` exists now and is written by Phase 5. It is on the level row rather than computed
from open orders because the till needs "can I sell this" in one indexed read.

### 2.3 The costing port

```go
type Strategy interface {
    Key() string
    OnReceipt(state State, m Movement) (Result, error)
    OnIssue(state State, m Movement) (Result, error)
    OnAdjustment(state State, m Movement) (Result, error)
    OnReturn(state State, m Movement, original Movement) (Result, error)
}
```

Pure — no database, no clock — so the arithmetic is testable against a table, exactly as the
tax engine and the unit conversion are. The service loads the state, calls the strategy, and
writes what it returns.

`Result` carries `ValueDelta`, which is what drives the GL posting. The strategy decides the
money; the posting rules decide the accounts; neither knows the other.

### 2.4 WAC arithmetic

```
new_average = (on_hand × current_average + received × receipt_cost) ÷ (on_hand + received)
```

128-bit intermediate, rounded once. The four traps of §1.5 are each a named test.

### 2.5 Negative stock

A company setting, **blocked by default**. Some businesses genuinely need it — a workshop that
issues components before the delivery note is entered — and most should not have it.

When permitted, an issue that takes stock negative is costed at the current average and records
the shortfall as a variance, so the money is visible and postable rather than absorbed.

### 2.6 Lots and serials

Tables from day one, behind feature flags (§21.2). A pharmacy needs lot expiry; a furniture shop
does not, and must never see the concept. The movement carries an optional `lot_id`, and the
domain enforces that a lot-tracked product cannot move without one.

### 2.7 The integrity check

A job, in the shape Phase 2's `CheckIntegrity` established:

1. Replay `stock_movements` per variant and warehouse; compare with `stock_levels`.
2. Sum stock value across levels; compare with the inventory account's balance.
3. Report discrepancies — never silently repair.

Step 2 is the one that matters. It is the only thing standing between "our margin looks fine"
and a year of quietly wrong profit.

---

## 3. STEP SEQUENCE

| Step | Deliverable |
|---|---|
| **4.1** | The stock ledger: movements, levels, rebuild-and-verify |
| **4.2** | The costing port and Weighted Average Cost |
| **4.3** | Receipts, issues, adjustments — with GL posting through Phase 2's rules |
| **4.4** | Transfers between warehouses |
| **4.5** | Lots, serials, and expiry |
| **4.6** | Stock counts and reconciliation |
| **4.7** | Screens: stock on hand, movement history, adjustments |
| **4.8** | Phase 4 Definition-of-Done review |

---

## 4. DEFINITION OF DONE

1. `stock_movements` is append-only; nothing updates or deletes a movement.
2. `stock_levels` can be rebuilt from movements and is verified against them, reporting rather
   than repairing.
3. Stock value reconciles to the inventory account balance, proven by a job.
4. Costing is reached only through the strategy port; no caller knows the method.
5. `inventory_layers` is written on every receipt, in average-cost mode as much as FIFO.
6. WAC handles all four §1.5 cases correctly, each with a named test.
7. A return is costed at its original issue's cost, not the current average.
8. Negative stock is blocked by default and, where permitted, records a visible variance.
9. A lot-tracked product cannot move without a lot; an untracked one never sees the concept.
10. A transfer moves quantity and value between warehouses without changing total stock value.
11. Inventory contains no accounting logic; every posting goes through Phase 2's rules.
12. Every new binding has a declared policy; every state change is audited in-transaction.
13. `make ci` green, with the mutation drills each step declares — and a drill that PASSES is
    treated as a defective test, per the rule Phase 3 established.

---

*Implementation proceeds step by step; each step records its own decisions and drills.*

---

## STEP RECORDS

### Steps 4.1–4.2 — The stock ledger, the costing port, and Weighted Average Cost

**Delivered.** A new `inventory` module: `0020_inventory.sql` (`stock_movements`,
`stock_levels`, `inventory_layers`), the movement domain, the costing port with WAC, the
repositories, the service with `Move` / `VerifyLedger` / `RebuildLevels`, and the
composition-root wiring. 4.1 and 4.2 shipped together because the ledger cannot record a
movement without costing it, and a costing port with no ledger has nothing to price.

**D1 — direction lives on the TYPE, and quantity is always positive.** A signed quantity makes
`SUM(quantity)` meaningful and every other query a minefield: "how much did we receive this
month" becomes a filtered sum that somebody eventually writes without the filter, and the answer
looks perfectly plausible. One `directions` table is consulted everywhere, so a service, a
verifier, and a report cannot disagree about whether a count adds or replaces.

**D2 — one fold, used by both the projection and its own verification.** `Apply` is the only
place a movement changes a quantity. If the maintainer and the verifier each carried their own
switch over the type, they would eventually disagree — and the verifier would then certify the
bug.

**D3 — `Verify` reports, `RebuildLevels` repairs, and they are separate calls behind separate
permissions.** A projection that heals itself hides the bug that broke it; the next drift is
silent too, and by the time anybody looks the evidence has been overwritten. `balance_after` on
every movement is what turns "the total is wrong" into "it went wrong here".

**D4 — layers are written on every receipt and drawn down on every issue, under WAC as much as
FIFO** (§D.2). Nothing reads them in v1. That costs one insert per receipt and buys the thing
§33.3 asked for: switching a company to FIFO is a configuration change plus a recompute, not a
migration that would have to reconstruct purchase history from movements.

**D5 — costing is a port with one implementation.** The temptation to write WAC inline and
"extract the interface when FIFO arrives" fails because the *call sites* are what would change,
and by then they are in sales, purchasing, adjustments, transfers, counts, and three reports.
An unknown method is **refused**, not silently costed by the default — valuing stock by a method
nobody chose is exactly the kind of wrong number that survives a year.

**D6 — the level row is the lock.** It is read through the *writer* connection inside the
transaction, so two concurrent issues of the last item serialise. Reading from the reader would
let the second see a stale on-hand and oversell, and both movements would record a plausible
balance.

**The four §D.3 traps, each with a named test:**

| Trap | Handled |
|---|---|
| Receipt into zero or negative on-hand | The new average **is** the receipt cost |
| Issue exceeding stock | Refused by default; where permitted, the shortfall is a recorded **variance**, not absorbed |
| Return of an item bought last year | Costed at the **original issue's** cost via `source_movement_id` |
| Landed costs across lines | Largest-remainder allocation, so the parts tie exactly |

**Mutation drills — 12 run, all now fail as required.**

| # | Mutation | Result |
|---|---|---|
| 1 | Trap 1 guard removed | **Passed at first.** See below. |
| 2 | Over-issue silently permitted | Domain and service tests fail |
| 2b | Shortfall absorbed, not recorded | `…RecordsAVisibleVariance` fails |
| 3 | Return costed at today's average | Domain and service tests fail |
| 4 | Largest-remainder pass dropped | 3 allocation tests fail |
| 6 | An issue moves the average | Domain and service tests fail |
| 7 | A count adjusts instead of replacing | Domain and service tests fail |
| 8 | The `Revaluation` case removed | **Passed at first.** See below. |
| 9 | `Verify` stops detecting drift | 3 tests fail |
| 10 | Layers only under FIFO | 2 tests fail |
| 11 | Layers not drawn down | `…ConsumedInReceiptOrder` fails |
| 12 | Caller-supplied balance recorded | `…RecordsTheBalanceItProduced` fails |
| 13 | Rebuild discards reservations | `…DoesNotDiscardReservations` fails |

**Two drills passed, and both taught something — the fifth and sixth occurrences of Phase 3's
lesson, now finding redundant *code* rather than merely weak tests.**

**Drill 1** removed the `on_hand <= 0` guard and the empty-stock test still passed. Working the
arithmetic out showed why: at zero on-hand the general formula *already* yields the receipt cost,
because the first term vanishes for any stale average whatsoever. Only the **negative** half is
load-bearing — receiving 10 at 120 into a stock of −5 carrying a phantom average of 999 yields
**−759**, a negative unit cost that then values the whole warehouse below nothing. The guard is
kept for clarity, but both the code comment and the test now say plainly which half does the work
and which is defensive.

**Drill 8** removed the explicit `case Revaluation:` from `Apply` and nothing failed — because
`Revaluation`'s direction is `Neutral`, and `Neutral × anything` is already nothing. That was not
a weak test; it was **a second mechanism restating what the first already said**. The case was
deleted. The direction table is now the only thing that decides, and the re-run drill (changing
`Revaluation` to `Inward` in that table) fails two tests as it should.

The rule Phase 3 wrote down has now earned a corollary: *when a drill passes, the second
mechanism is sometimes the one to delete — not the test to strengthen.*

### Step 4.3 — GL posting through Phase 2's rules

**Delivered.** `posting.go` in the inventory module, a `5600 Stock adjustments` account and an
`INVENTORY_ADJUSTMENT` mapping in the shipped chart, and two posting rules — all of it data
except the sixty lines that decide *whether* to publish.

**D1 — an increase and a decrease are DIFFERENT actions.** Phase 2's engine refuses a negative
amount, deliberately: a negative would flip a line's side silently, and "a line is debit XOR
credit, both non-negative" is what makes an entry checkable at all. So `inventory.stock.increased`
and `inventory.stock.decreased` are separate events with separate rules — exactly as a credit
note is a separate event from an invoice. The alternative, one action carrying a signed amount,
would push the sign into every rule an accountant writes.

**D2 — the rule that prevents double-posting.** A movement caused by a *document* is posted by
that document's module, as one entry that also records the payable or the revenue. A movement
with no document has nobody else to post it.

```
DocumentType set   → somebody else posts it; inventory publishes nothing.
DocumentType empty → inventory posts it.
Transfer           → nothing posts it, either way.
```

Without this, every purchase would debit inventory twice. The transfer case is its own: the
company owns the same goods in a different place, so total inventory value is unchanged and
posting one would be an entry whose two sides are the same account.

**D3 — the posting happens before the audit entry, and both inside the movement's
transaction.** A rule that refuses — a closed period, a missing mapping — must stop the
movement, not leave a stock change the ledger never heard about. `TestAMovementIsRolledBackWhenItsPostingFails`
deletes a mapping and asserts that neither the level nor the ledger moved.

**D4 — stock value reconciles to the inventory account exactly**, which is DoD criterion 3 and
the reason any of this exists. `TestStockValueReconcilesToTheInventoryAccount` runs four
movements including two that change the average, and compares the shelf against the account to
the minor unit.

**Mutation drills — 5 run.**

| # | Mutation | Result |
|---|---|---|
| 14 | Documented movements posted too | 5 tests fail |
| 15 | A transfer posts | `TestATransferPostsNothing` fails |
| 16 | A decrease publishes the increase action | 4 tests fail |
| 17 | A costless movement posts a zero entry | **Passed — and stayed passed.** See below. |
| 18 | Posting skipped entirely | 7 tests fail |

**Drill 17 is the third passing drill of this phase, and the first that resolved to "keep it and
say so".** Publishing a zero-amount event changes no balance, because Phase 2's engine already
skips lines that resolve to zero. I then checked whether it at least left a stray journal *header*
— it does not; the engine declines to create an entry with no lines. So the guard is genuinely
defensive: what it saves is a publish and a rules lookup on every costless movement, which is
real work but not a different answer.

It is kept, `postingFor` now says exactly that, and **no assertion was written for it** — an
assertion that passes either way would tell the next reader something is pinned when nothing is.
That is the third possible outcome of a passing drill, alongside 3.x's "strengthen the test" and
4.1's "delete the redundant code":

> **Keep the code, document that it saves work rather than changing outcomes, and write no test
> that pretends otherwise.**

### Step 4.4 — Transfers between warehouses

**Delivered.** `Transfer`, `moveWithin`, and the `WarehouseAllowsNegative` read. `Move` now
delegates to `moveWithin`, so the recording body exists once rather than twice.

**D1 — the inbound leg is costed at the SOURCE's average.** This is the whole subtlety of the
step. If warehouse A holds stock at 100 and B at 200, moving five units and costing the arrival
at B's average would credit A with 500 and debit B with 1000 — **creating 500 of inventory value
by moving a box across town**. The destination receives at what the goods actually cost where
they came from and blends that into its own average, so total company inventory value is
unchanged. That is DoD criterion 10, and it is what makes a transfer post nothing to the ledger.

**D2 — both legs in one transaction, through `moveWithin`.** A crash between them would produce
the worst state this module can reach: stock that has left one warehouse and arrived at none. The
ledger would be internally consistent per warehouse, and the company would simply own less. Note
that `moveWithin` takes an existing transaction rather than opening one — nesting `db.Do` would
either deadlock on the single writer or commit the first leg independently, which is exactly the
half-transfer being prevented.

**D3 — a transfer to the same warehouse is refused**, not treated as a no-op. It would write two
movements that cancel, leaving the ledger noisier and the stock unchanged.

**D4 — negative stock is read from the WAREHOUSE, and the company setting I first wrote was
deleted.**

This is the most useful thing the step found. I declared an `inventory.allow_negative_stock`
setting at company scope, noted in a comment that per-warehouse "would arguably be righter", and
moved on. Writing the transfer test then required inserting a warehouse row — and the `warehouses`
table already had:

```sql
-- §21.2: some businesses genuinely need negative stock, most should not. Read from Phase 4.
allows_negative_stock SMALLINT NOT NULL DEFAULT 0,
```

Phase 1 had left the seam, labelled with the phase that was meant to read it, and I had built a
second one at the wrong grain without looking. The setting is gone; the column is the authority.
It is also simply *righter* — a bonded store may tolerate what the shop floor must not, which a
company setting cannot express.

**The lesson generalises past this instance:** before declaring a new place for a fact to live,
look for the one an earlier phase already left. This codebase writes those down in the schema, in
comments that name the phase.

**Mutation drills — 5 run, all fail as required.**

| # | Mutation | Result |
|---|---|---|
| 19 | Arrival costed at the destination's average | 3 tests fail, incl. value conservation |
| 20 | Source cost read as a stale zero | 3 tests fail |
| 21 | Same-warehouse transfer allowed | `…ToTheSameWarehouseIsRefused` fails |
| 22 | Warehouse flag ignored — always permissive | 3 tests fail |
| 23 | Warehouse flag ignored — never permissive | `…WarehouseThatPermitsItCanIssueBelowZero` fails |

Drills 22 and 23 are deliberately a pair: a flag read in only one direction is a flag half-tested,
and the default being `false` means a single test could pass while the read did nothing at all.

### Step 4.5 — Lots, serials, and expiry

**Delivered.** `0021_lots_serials.sql` (`stock_lots`, `stock_lot_levels`, `stock_serials`, and
`lot_id`/`serial_id` on movements), the tracking domain, the repositories, the service methods,
two feature flags, and a `Products` port satisfied by catalog in the composition root.

**D1 — FEFO for lots, FIFO for cost layers, and they are not the same ordering.** Cost layers are
consumed in *receipt* order (§D.2); physical lots in *expiry* order. Conflating them is how a
wholesaler ships the batch that expires next week while holding one that expires next year, and
then writes the second one off. Lots with no expiry sort **last** — a batch that never expires can
wait.

**D2 — expiry is checked at ISSUE, not at receipt.** A batch expiring in a month is perfectly good
today, and refusing to receive it would stop a delivery for nothing. What must not happen is
shipping it after the date. A lot expiring *today* is still good today — off by one here throws
away a day's stock, every day.

**D3 — an unusable lot is SKIPPED, not refused.** The caller asked for a quantity, not for a
particular batch; skipping and taking from the next is what a picker does. When the usable lots
cannot cover the request, the shortfall names what is **usable** — "you have 3 of the 10 you
asked for" is actionable, "lot 47 is expired" is not.

**D4 — the tracking rule runs in BOTH directions.** A lot-tracked product moving without a lot is
the obvious failure. The reverse is the one that gets forgotten: a movement carrying a lot for a
product that is *not* lot-tracked has recorded something nothing reads, while implying
traceability that does not exist. A furniture shop must never see the concept, so its data must
never carry it.

**D5 — a serial is an IDENTITY, not a quantity.** A serialised movement moves exactly one unit;
two of them is two movements. A sold serial keeps its row, because the warranty claim two years
later needs to find it.

**D6 — `Products` is a port, not an import of catalog.** Inventory needs one fact — how finely a
product is tracked — and §10.3 says a module reaches another only through its contract package.
The port is two lines to fake in a test and is satisfied from catalog in the composition root.

**`nilerr` earned its keep for the second time.** The port's first draft returned "quantity" for
*any* error, and the linter caught it — as it caught Phase 2's tax repository reading a database
fault as "this company charges no tax". The failure prevented here: a transient database error
would report a lot-tracked product as quantity-tracked, and the movement would be accepted
**without a lot** — the batch becoming untraceable at exactly the moment traceability was being
recorded. A product that does not *exist* still reads as `quantity` (the FK is about to fail with
a clearer message); anything else propagates.

**Mutation drills — 7 run, all now fail as required.**

| # | Mutation | Result |
|---|---|---|
| 24 | The FEFO sort removed | **Passed at first — the mutation was wrong.** See below. |
| 25 | A lot with no expiry sorts first | `TestALotWithNoExpirySortsLast` fails |
| 26 | Expired lots picked anyway | 4 tests fail |
| 27 | A lot expiring today treated as expired | `TestALotIsGoodOnItsExpiryDate` fails |
| 28 | The untracked direction unchecked | Domain and service tests fail |
| 29 | Lot-number index weakened | **Passed at first — the test was wrong.** |
| 30 | Serials unique per variant, not per company | **Passed at first — the test was wrong.** |

**Drill 24 was a bad mutation, not a bad test.** Inserting a `return false` comparator *before*
the real `SliceStable` call left the real one running afterwards, so nothing changed. Replacing
the comparator's body made it fail two domain tests immediately. Worth recording because it is a
distinct failure mode from the others: **a drill that passes may mean the mutation missed**, and
checking that the change actually took effect is the first thing to do — as 3.1 also learned when
a patch silently failed to apply.

**Drills 29 and 30 are the Phase 3 lesson, fourth occurrence.** `CreateLot` looks up the number
and refuses a duplicate before the index is consulted, so weakening the index changed nothing a
test could see. And the serial test used one variant, so it could not tell `(company_id, …)` from
`(variant_id, …)`. Both now have tests that can: one writes straight to the table, the other uses
**two variants**. The rule holds — *a test must name the mechanism it is about* — and it is now
cheap to apply, because I recognised the shape before the drill finished running.

### Step 4.6 — Stock counts

**Delivered.** `0022_counts.sql` (`stock_counts`, `stock_count_lines`), the count domain with its
lifecycle, the repositories, and the service: `OpenCount`, `BeginCounting`, `RecordCount`,
`SubmitCount`, `PlanCount`, `ApplyCount`, `CancelCount`.

**D1 — a count is a DOCUMENT, not a movement.** A movement is an instant; a count is a process
that takes hours — a sheet is generated, people walk the aisles, figures come back, somebody
reviews the surprising ones, and only then does stock change. Modelling it as a bare movement
loses every part of that except the last, and the parts it loses are the ones an auditor asks
about.

**D2 — the VARIANCE is applied, never the counted figure. This is the correctness point of the
step.**

Stock moves while people count. If applying the count *set* the balance to what was counted,
every movement made during it would be silently undone — a sale shipped at 11am reappearing when
the count is applied at 4pm, leaving the ledger showing stock that had already left the building.

So the expectation is **frozen at the snapshot** and never refreshed, and the count contributes
what the counter actually established: `counted − expected`. Two fewer than the sheet said is two
fewer, whatever else happened meanwhile. The test walks exactly this: receive 10, count 8, sell 3
during the count, apply → **5**, not 8.

**D3 — an uncounted line is not a zero.** Lines nobody reached are left alone. Treating them as
zero would write off the entire stock of everything the counter did not get to, which on a
partial count is most of the warehouse. A zero somebody *entered* is a real finding and is
applied; `CountedMicro` is a pointer precisely so the two are different values.

**D4 — blind by default, and the hiding happens at the READ.** Showing the counter what to expect
is how a count comes back agreeing with the system on every line while the shelves say otherwise
— not through dishonesty, but because a tired person looking at "10" finds ten. The expectation
is still stored (the apply needs it); it is simply not handed out while counting. A screen cannot
show what it was never given. In *review* it returns, because there the variance is the whole
point. The unsafe option is named `ShowExpected`, so it must be asked for.

**D5 — applied from REVIEW only.** A line reading 3 where 300 was expected is a miscount far more
often than a theft, and applying it unseen writes off stock sitting on the shelf.

**D6 — `StockCount`, not `Count`.** `Count` is already a movement type in that package. The third
collision of this shape — after `Auditable.EventType` (1.7) and the i18n catalogue versus the
catalog module (3.1) — and resolved the same way each time: rename the newcomer to say what it
is, rather than shortening the incumbent.

**Mutation drills — 6 run, all fail as required.**

| # | Mutation | Result |
|---|---|---|
| 31 | The counted figure applied instead of the variance | Domain and service tests fail |
| 32 | An uncounted line treated as zero | Domain and service tests fail |
| 33 | Applied straight from counting | 3 tests fail |
| 34 | An applied count applied again | Domain test fails |
| 35 | A blind count shows the expectation | `…DoesNotTellTheCounterWhatToExpect` fails |
| 36 | Not blind by default | `TestACountIsBlindByDefault` fails |

Drill 34 fails only at the domain level, and that is correct rather than a gap: with the terminal
check removed, the review check still refuses — two guards, both firing. The **domain** test
asserts the specific code and so distinguishes them, which is exactly what the Phase 3 rule asks
for.
