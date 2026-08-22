# Known gaps

Two criteria were recorded as unmet or excepted during the phase reviews, and both survive into
the release. This document states each one, why it stands, what would close it, and what closing
it would cost.

It exists so the next person deciding whether to act on them has the reasoning rather than the
verdict.

---

## 1. `price_list_items.variant_id` is nullable

**Recorded:** Phase 3, criterion 2 — *"`variant_id` is `NOT NULL` everywhere it appears"* (§1.2).
**Status:** deliberate exception, standing since Phase 3. Four phases have passed and nothing has
come to depend on it.

### What the criterion exists to prevent

A nullable `variant_id` meaning *"the product itself"*. If NULL is a third state, every query,
stock lookup, price resolution and report must handle two cases forever — and the one that forgets
produces a price for a variant that has its own.

### Why the exception stands

3.5 decided a price may target either a product or a variant, because a shop pricing "olive oil"
once should not have to price six bottle sizes separately.

Three things make it narrower than the criterion fears:

- **The feared state is unrepresentable, not merely discouraged.** `ck_price_item_targets_one`
  refuses any row that is not *exactly one* of the two kinds. There is no row where both are set
  and none where neither is.
- **Exactly one function reads both cases** — `domain.Resolve`, whose entire job is choosing
  between granularities. No stock lookup, no document line and no report touches the column.
- **No second reader has appeared** in four phases, including Phase 8's reporting work, which
  read almost everything else.

### What would close it

A `price_list_item_variants` join, or a nullable-free design where a product-level price is
materialised onto every variant.

```sql
-- Sketch. Not written, not tested, and not recommended without a reason.
CREATE TABLE price_list_item_targets (
  id             CHAR(36) NOT NULL PRIMARY KEY,
  price_item_id  CHAR(36) NOT NULL REFERENCES price_list_items(id) ON DELETE CASCADE,
  variant_id     CHAR(36) NOT NULL REFERENCES product_variants(id),
  CONSTRAINT ux_price_item_target UNIQUE (price_item_id, variant_id)
);
```

### What it would cost

- **A migration that fans out** every product-level price across its variants, and a backfill that
  must run inside the same transaction as the schema change.
- **A new invariant nobody currently has to maintain:** creating a variant would have to create
  its price rows, and every price list would grow by the variant count rather than the product
  count. A shop with 2,000 products averaging six variants goes from 2,000 rows to 12,000.
- **The resolution order in §2.6 would need restating**, because "the product's price" would no
  longer exist as a row to fall back to.

### Recommendation

**Leave it.** Revisit only if a second reader of the column appears — that is the condition under
which the criterion's reasoning starts to apply, and until then the CHECK constraint is doing the
work the `NOT NULL` was asked to do.

---

## 2. Phase 4's `Revaluation` seam needed reshaping

**Recorded:** Phase 6, criterion 13 — *"Every seam Phase 4 left has a caller **and none needed
reshaping**"*.
**Status:** **permanently false.** No later work can make it true.

### What happened

Phase 4 built four seams for callers that did not yet exist: `inventory_layers`, the movement's
document link, `round.Allocate`, and `Revaluation`.

Three served their first caller unchanged. `Revaluation` did not. When Phase 6 came to apply a
landed cost — the feature `Revaluation` was built for — it turned out to be **structurally
unwritable**: a positive-quantity guard in the domain AND a `CHECK (quantity_micro > 0)` in the
schema, both of which a revaluation must violate, because it changes value without changing
quantity.

6.6 rebuilt the table:

```sql
-- 0030_revaluation_quantity.sql, already applied.
CHECK (quantity_micro > 0 OR (quantity_micro = 0 AND movement_type = 'revaluation'))
```

### Why this is not a gap to close

**The code is correct today.** Landed costs apply, revaluations post, and
`TestFreightReachesTheCostOfTheGoodsItBroughtIn` proves it.

The criterion is a claim about the DESIGN PROCESS, not about the code: it asked whether Phase 4's
seams were designed or guessed. One was guessed, the guess was wrong, and the cost was a table
rebuild in a later phase. Rewording the criterion to make it pass would erase the only evidence
that the question was worth asking.

### What it actually recommends

Not a migration — a practice:

- **A seam built for a caller that does not exist yet should be exercised by a test that writes
  through it**, not merely one that reads it back. Phase 4's tests read revaluations that were
  inserted directly; none constructed one through the domain, which is why both guards survived
  review.
- **When a phase leaves a seam, the phase that consumes it should report back.** Phase 6 did, and
  that is why this is written down rather than discovered by a customer.

### Recommendation

**Leave the criterion false.** It is a historical record. If Phase 4's DoD is ever restated, the
honest form is *"three of four seams served their first caller unchanged; the fourth was rebuilt
in 6.6"*.

---

## Three further gaps, not schema

Carried from the Phase 10 review, listed here so one document holds them all.

| Gap | Why it stands | What would close it |
|-----|---------------|---------------------|
| **Nobody has used this.** Every guarantee is proven by a test; no shopkeeper has opened it. | Tests prove what somebody thought to check. | A pilot install, and a week of somebody's real trading. |
| **The performance ceiling catches structural regressions, not drift.** A report going from 46ms to five seconds still passes. | A tighter bound fails on a loaded build machine — measured, not assumed (10.2 D2). | Record each report's time per run and alert on a trend, which needs somewhere to keep the history. |
| **`runEveryReport` lists the phase's reads by hand** (9.7). | Reflection over the `Insight` façade needs a session and a permission set. | Extend the reflection the façade-coverage test already does, and drive it with a granted-everything session. |

---

## 3. "Out of stock" is not "low stock"

**Recorded:** Phase 10.16, while building the KPI tiles.
**Status:** deliberate. The tile counts what can be counted; the tile that was asked for needs a
schema change.

### What ships

`inventory.out_of_stock` counts stock levels at or below zero — a fact, read straight from
`stock_levels`. At or below rather than exactly zero, because a negative level means stock went
out that was never booked in, and a shop with that problem most needs to see it.

### Why not "low stock"

"Low" needs a threshold per product, and no such column exists. Any number invented here is right
for nothing: five is a week of cement and a year of engine blocks. A single global threshold would
alarm constantly on fast movers and stay silent on the slow ones that actually run out.

### What would close it

A `reorder_point_micro` on the variant — per warehouse, since a shop and its back store do not
need the same cover — plus a screen to set it and a column on the stock list to show it.

```sql
-- Sketch. Not written, not tested.
ALTER TABLE stock_levels ADD COLUMN reorder_point_micro INTEGER NOT NULL DEFAULT 0;
```

### What it would cost

- **A number somebody has to maintain.** A reorder point that is never revisited is worse than
  none: it fires on products the shop stopped selling and stays quiet on the ones it now sells
  ten times more of. The feature is the screen and the habit, not the column.
- **A decision about who sets it.** Derived from sales velocity, it becomes a forecast — which is
  a different feature with different failure modes, and one that needs history before it can say
  anything.

### Recommendation

**Leave it.** Out-of-stock is the alert a shop acts on today, and it is honest about what it
counts. Revisit when somebody using Mizan asks for the threshold — at which point they will also
say what they want it derived from, which is the part that cannot be guessed from here.

---

## Signing

Unsigned on both platforms, by Phase 0's decision.

- **macOS:** the `.dmg` is unsigned and un-notarized. Gatekeeper will refuse it on another Mac
  until an Apple Developer ID signs and notarizes it. The hook is in
  `scripts/package-macos.sh` and activates when `MIZAN_MACOS_IDENTITY` is set.
- **Windows:** the `.exe` is unsigned. SmartScreen will warn on first run until an Authenticode
  certificate signs it.

Both are opt-in rather than absent, so a build on a clean machine with no certificate still
produces a working artefact — which is what makes the packaging testable at all.
