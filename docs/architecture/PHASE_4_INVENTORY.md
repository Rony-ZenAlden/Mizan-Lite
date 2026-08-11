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
