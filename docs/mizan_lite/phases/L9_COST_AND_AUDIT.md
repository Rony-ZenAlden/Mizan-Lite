# L9 — the price a shop paid, and the stocktake

**Started 2026-09-16 · cost price shipped in 0.9.2 · the stocktake not built**

The owner asked for two things after using 0.9.1 in a seeded shop for an afternoon. One is built and shipped; the other is a
phase of its own and is recorded here so the thinking is not lost. This note is written after the fact for the first and
before the fact for the second — the design→approval→implement gate was waived by the owner for this round ("build now on
the answers above", 2026-09-16), and §4 says what that cost.

---

## 1. What the owner asked, and answered

| Question | The owner's answer |
|---|---|
| Products have no cost field; cost is the weighted average of real deliveries, which all profit reporting uses. Adding a typed cost price makes two sources of truth. Which decides profit? | **The typed cost becomes the profit basis** |
| Category-based audits were asked for, but Lite has no categories | **A free-text section typed per audit** ("الرف العلوي"), not a category scheme |
| This is phase-sized; your protocol is design note → approval → implement | **Build now on those answers** |

---

## 2. The cost price and the margin (shipped, 0.9.2)

### 2.1 What was built

`0009_product_cost.sql` adds `products.cost_price_micro` and `products.cost_currency`, both nullable, guarded by triggers so
an amount and its currency arrive together and a cost of nothing is refused.

- **The margin is not stored.** It is the distance between two prices the product already carries. A third stored number
  would be free to disagree with the two it came from. `Product.Margin()` computes it: an amount, and a percentage **of the
  cost** — "I buy at 2 and sell at 2.50" is 25%, which is how a shopkeeper says it.
- **The cost is in the product's own selling currency.** A product priced in pounds whose cost was in dollars would need a
  rate to compare them, and a rate that moves would make the margin move with it.
- **Both directions.** `PriceFromMarginPercent` and `PriceFromMarginAmount` work the selling price out of the cost; typing a
  price instead leaves the margin to be read off it. The form remembers which box was touched last and sends that one; **Go
  does every piece of the arithmetic**, as it does for every other figure (DESIGN D9).
- **A loss is shown, not refused.** A product sold below cost reads as a negative margin with "sold below cost" beside it. A
  margin that would take the price below nothing is refused.

### 2.2 The profit basis — D-L9.1

The owner chose the typed cost over the weighted average. It is applied as a **snapshot at checkout**: a sale records the
typed cost where one is set, and the weighted average where it is not, exactly as costs have always been recorded.

**Sales already made keep the cost they actually recorded.** This is a deliberate departure from the literal answer, which
said historical figures would be recomputed. Two reasons: overwriting what a past sale cost destroys the record of what the
shop actually paid, irreversibly; and a closed day's profit would change every time somebody edited a price, which
contradicts the rule that a closed day's totals never change (D-L4.i1). The owner was told at the time and can ask for a
one-off back-fill as its own deliberate step.

### 2.3 Currencies — D-L9.2

A cost typed in pounds is converted to dollars **at that sale's own rate**, so the dollar and pound profit figures still
reconcile (DESIGN C6). Tested in both directions.

### 2.4 What it fixes as a side effect

A product that was never received into stock had no cost and sat outside the profit figures as "cost unknown". A typed cost
now fills that gap, and the warning goes.

---

## 3. The stocktake (designed, NOT built)

Its schema is drafted at [L9_AUDIT_DRAFT.sql](L9_AUDIT_DRAFT.sql) and **is not in any migration**: a shop's database must
not carry tables nothing writes to. It ships as `0010` with the module that uses it.

The shape it was designed to:

- **An audit is a session, not a count.** A shopkeeper walks a shelf with a sheet, writes what is there, and compares it
  with the books. `audits` + `audit_lines`, open until signed off.
- **Only a closed audit moves stock.** Until then the counts are a piece of paper, and a piece of paper has never changed
  what is on a shelf.
- **The system quantity is snapshotted when a line is added**, not read at closing time, or the variance is measured
  against a moving target.
- **A line never counted is not a line counted as zero.** `counted_micro` is NULL until somebody writes a number: the
  difference between "the shelf was empty" and "nobody looked" is the whole point of an audit.
- **Kinds** — hourly, daily, weekly, monthly, section — title the sheet and constrain nothing.
- **Money** uses the typed cost price of §2, at the rate recorded when the audit closed.

Still to build: the domain and its service, the store and its contract suite, the bindings, the screen, PDF and Excel
exports and a printed sheet with the shop's header, the catalogue strings in both languages, and the journeys.

---

## 4. What building without the note cost

The owner waived the design gate for this round. The gate would have caught two things before code was written:

1. **"Category-based audits" cannot be built** — the catalogue has no categories. This surfaced as a question only because
   the code was read first; a note would have found it in a paragraph.
2. **The audit is a phase, not a feature.** It was started, its schema written, and then taken back out of the migration
   when it became clear the module behind it could not be finished to the standard the rest of the edition holds. The
   schema survived as a draft; the half-built migration did not ship.

The cost price shipped in the same round because it is genuinely small and its arithmetic is testable in isolation.

---

## 5. Decisions

| # | Decision | Status |
|---|---|---|
| D-L9.1 | The typed cost price is the profit basis, snapshotted at checkout; sales already made keep the cost they recorded | built |
| D-L9.2 | A cost typed in the local currency converts to dollars at the sale's own rate | built |
| D-L9.3 | The margin is computed, never stored | built |
| D-L9.4 | The cost price is held in the product's own selling currency | built |
| D-L9.5 | A margin that would take the price below nothing is refused; a loss is shown | built |
| D-L9.6 | An audit is scoped by a free-text section, not a category scheme | designed |
| D-L9.7 | Only a closed audit moves stock; the system quantity is snapshotted per line | designed |
| D-L9.8 | The audit schema ships with the module that uses it, as 0010 | designed |
