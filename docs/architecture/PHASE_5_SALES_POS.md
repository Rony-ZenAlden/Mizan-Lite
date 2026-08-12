# Phase 5 — Sales & POS (Design)

> Status: **DESIGN — implemented step by step under the standing autonomous mandate.**
> Scope: number series, sales documents (quotation → order → invoice), returns, payments, the
> POS terminal with shifts and held sales, and printing.
> **Out of scope:** purchasing (Phase 6), expenses and debts (Phase 7). This phase builds the
> money coming *in*.

---

## 1. ANALYSIS

### 1.1 This phase is mostly composition, and that is the point

Every mechanism a sale needs already exists:

| What a sale needs | Built in | Waiting since |
|---|---|---|
| A price, and why | Pricing resolution (3.5) | Has never had a caller |
| Tax on that price | Tax engine (2.6) | Has never had a caller |
| Stock leaving | `inventory.Move` with `Issue` (4.1) | `qty_reserved` written by nobody |
| Cost of what left | Costing port (4.2) | — |
| The journal entry | `sales.invoice.posted` posting rules | **Seeded in Phase 2, never fired** |
| Who the customer is | Partners (3.4) | Credit limit never checked |
| What was sold | Catalog + variants (3.2) | — |

So Phase 5 is the phase that finds out whether those seams were built right. If they were, this
is assembly. If any of them was designed for an imagined caller rather than a real one, this is
where it shows — and the honest thing is to report that when it happens rather than quietly
reshaping the seam to fit.

The two genuinely new mechanisms are **number series** and the **document snapshot**.

### 1.2 The snapshot rule — §9.3

> Every posted document line stores a **snapshot** of everything used to compute it: unit price,
> discount, tax rate applied, exchange rate used, cost at time of sale. Never a reference to a
> mutable current value. Reprinting a two-year-old invoice must reproduce it byte-identically
> after prices, tax rates and exchange rates have all changed.

This is the single most important rule in the phase, and it inverts the instinct a developer
brings to a schema. Normalisation says: store `variant_id` and join for the price. Correctness
says: store the price, because the join answers *today's* question and an invoice is a record of
what was agreed *then*.

The test that matters is not "does the total add up" but "does this invoice still print the same
after somebody changes the price list, the tax rate, and the product's name".

### 1.3 Numbers are allocated at POSTING — §9.4

Not at draft creation. An abandoned draft must not consume a number, or a shop that starts and
cancels twenty sales in a morning ends the day at invoice 400 having issued twenty.

Allocation is transactional: `next_value` is read and incremented inside the posting
transaction. SQLite's single writer makes this naturally safe, which is a property to *rely on
deliberately* rather than to accidentally depend on — the dialect shim is where it becomes
`SELECT … FOR UPDATE` elsewhere.

**Unique sequential, not gapless** (§33.4). `is_gapless` is a per-series flag defaulting to 0, so
a jurisdiction that later requires it is a configuration change.

### 1.4 A posted document is immutable

The same rule the journal and the stock ledger follow, for the same reason. A posted invoice is
corrected by a **credit note**, never by editing. Editing would silently restate a period whose
books are closed, and would leave the stock movement and the journal entry describing a document
that no longer says what they were made from.

### 1.5 Dual quantity on every line — §B.3

```
quantity_micro        as entered, in the document's unit
uom_id                the unit the customer bought in
quantity_stock_micro  converted, in the product's stock unit
```

A reprinted invoice says "2 rolls" while inventory correctly moved 200 metres — and neither
figure depends on a conversion factor that might be edited later. Storing only one and converting
on read makes the invoice a function of today's UoM table.

### 1.6 What makes POS different from an invoice screen

Not the data — a POS sale *is* an invoice. What differs is the failure model:

- **Speed matters more than completeness.** A queue forms. The till must take a payment in
  seconds without a customer record, a delivery address, or a due date.
- **Interruption is normal.** A customer forgets their wallet; the sale is **held** and the next
  one starts. Held sales are not drafts of a different kind — they are the same document, parked.
- **The cash drawer must reconcile.** A **shift** opens with a float, takes payments, and closes
  with a count. The difference is a fact somebody must explain, and it is the POS equivalent of
  the stock reconciliation Phase 4 built.
- **Users switch constantly.** §979 asks for PIN login: a short credential on an already-unlocked
  device, scoped to POS permissions and rate-limited.

### 1.7 Risks

| Risk | Consequence | Answer |
|---|---|---|
| A line stores a reference, not a snapshot | Reprints change as prices change | §1.2, tested by mutating the source data |
| Numbers allocated at draft | Gaps on every abandoned sale | Allocated at posting (§1.3) |
| Two terminals post at once | Duplicate invoice number | Transactional allocation, plus a UNIQUE index |
| A posted invoice is edited | Closed period restated; stock and books describe a document that changed | Immutable; corrected by credit note (§1.4) |
| Stock issued but no journal entry | Goods gone, books silent | One transaction, as Phase 4 established |
| Payment recorded twice | Customer credited twice | Idempotency on the payment, not on the click |
| Shift never reconciled | Cash difference invisible | Shift close requires a count (§1.6) |
| Sale posted without stock | Sells what does not exist | Phase 4's refusal, now with a caller |

---

## 2. DESIGN

### 2.1 One document table, discriminated by type

```
sales_documents   quotation | order | invoice | credit_note
sales_lines
```

Not four tables. A quotation that becomes an order that becomes an invoice is the ordinary path,
and copying rows between tables at each step is how a line's history gets lost. The `type` column
plus a `source_document_id` chain records the progression.

### 2.2 The lifecycle

```
draft ──▶ posted ──▶ (paid | partly_paid)
  │           │
  └─▶ cancelled └─▶ returned (via a credit note that references it)
```

`posted` is the irreversible step: it allocates the number, issues the stock, computes the tax,
writes the journal entry, and freezes the snapshot. Everything before it is editable; nothing
after it is.

### 2.3 The snapshot columns

Every line carries, frozen at posting:

```
unit_price_minor        the price that was agreed
price_source            which list answered (§2.6 of Phase 3)
discount_minor
tax_rate_micro          the RATE, not the tax group
tax_amount_minor
cost_micro              what the goods cost us, from the costing port
exchange_rate_micro     when the document is not in the functional currency
product_name            what the product was CALLED at the time
uom_code                what the unit was called
```

The last two look redundant next to `variant_id`. They are the difference between a reprint that
says what the customer bought and one that says what that product is called now.

### 2.4 Payments are their own documents

A payment is not a field on an invoice. One payment settles several invoices; one invoice takes
several payments; a payment may be a deposit against nothing yet. `sales_payments` and
`sales_payment_allocations` express all three; a `paid_minor` column on the invoice cannot.

### 2.5 The POS shift

```
pos_shifts    opened_by, opening_float_minor, closed_at, counted_minor, expected_minor
```

`expected` is derived from the payments taken; `counted` is what the drawer held. The difference
is recorded, not hidden — the same reasoning as the stock count's variance, and the same refusal
to let a system quietly absorb a discrepancy somebody should explain.

### 2.6 Held sales are parked documents, not a new concept

A held sale is a draft with `is_held = 1` and a label. It occupies no number, moves no stock, and
is resumable at any terminal in the branch.

---

## 3. STEP SEQUENCE

| Step | Deliverable |
|---|---|
| **5.1** | Number series: allocation at posting, transactional |
| **5.2** | The sales document: draft, lines, dual quantity, totals |
| **5.3** | Posting: price, tax, stock, cost, GL, number — one transaction |
| **5.4** | Returns and credit notes |
| **5.5** | Payments, allocation, and settlement |
| **5.6** | POS: shifts, held sales, PIN login |
| **5.7** | Screens: POS terminal, invoice list and detail |
| **5.8** | Printing: receipts and invoices |
| **5.9** | Phase 5 Definition-of-Done review |

---

## 4. DEFINITION OF DONE

1. A posted line reproduces its invoice byte-identically after the price list, the tax rate, the
   product name, and the unit name have all changed.
2. A number is allocated only at posting; an abandoned draft consumes none.
3. Two documents can never share a number, enforced by the schema as well as the allocator.
4. A posted document cannot be edited; correction is by credit note.
5. Posting a sale issues stock, computes tax, records cost, and writes the journal entry — all in
   one transaction, or none of it.
6. A sale that would take stock below zero is refused unless the warehouse permits it.
7. A credit note returns stock at the **original sale's cost**, not today's average.
8. Price resolution records which list answered, and the answer is stored on the line.
9. One payment can settle several invoices, and one invoice can take several payments.
10. A shift's expected cash is derived from its payments, and a close records the counted
    difference rather than absorbing it.
11. A held sale occupies no number and moves no stock.
12. Sales contains no accounting logic; every posting goes through Phase 2's rules.
13. Every new binding has a declared policy; every state change is audited in-transaction.
14. `make ci` green, with the mutation drills each step declares — and a drill that PASSES is
    treated as a defect in the test, the code, or the mutation, per the five outcomes Phase 4
    recorded.

---

*Implementation proceeds step by step; each step records its own decisions and drills.*

---

## STEP RECORDS

### Step 5.1 — Number series

**Delivered.** The numbering domain (`Series`, `Format`, `Next`), the repository, the service
(`CreateSeries`, `allocateNumber`, `PreviewNumber`), and the sales module wired into the
composition root.

**D1 — the table already existed, and this step deleted the one it had written.**

`0023_numbering.sql` was written, and the migration failed on `table number_series already
exists`: `migrations/sqlite/0001_platform.sql` creates it, commented *"platform-level; used by
every transactional module"*. That is the right home — purchasing (Phase 6) and payments will
number documents too, and a series table owned by sales would make them either import sales or
build a second one.

**Second occurrence of the 4.4 lesson**, and this time the schema said so out loud before any code
depended on the duplicate. Two consequences shaped the domain rather than the reverse:

- The platform table has **no `company_id`**. A series is identified by code + branch + fiscal
  year, and a branch already belongs to a company. Adding a column to a platform table from a
  module is the ownership violation §10.3 exists to prevent.
- It has **no `is_active`**, so `Series` has no such field. A field with no storage is a lie that
  reads as a feature; retiring a series is expressible by changing the code a document type
  resolves to.

**D2 — `Format` is separated from `Next`.** A screen can show "this will be INV-000124" without
consuming it, and the padding rules have one implementation rather than two.

**D3 — `Next` returns the advanced series rather than mutating.** The counter is part of the
answer, so there is no path where a number is taken and the counter is left alone. The drill for
this fails four tests.

**D4 — `allocateNumber` is unexported and takes a transaction context.** No caller outside this
module can take a number without a document to attach it to. That is the structural form of
§9.4's "allocated at posting", rather than a comment asking people to remember it.

**Mutation drills — 6 run, all now fail as required.**

| # | Mutation | Result |
|---|---|---|
| 1 | The counter is never advanced | **Passed twice — both mutations were wrong.** See below. |
| 2 | The series read through `Reader` | **Passed — my comment was wrong.** See below. |
| 3 | The `next_value <` guard dropped | **Passed — defensive, kept and documented.** |
| 4 | A branch series no longer wins | `…BeatsTheCompanyWideOne` fails |
| 5 | Padding ignored | 4 tests fail |
| 6 | `PreviewNumber` consumes a number | `…DoesNotConsumeIt` fails |

**Drill 1 was a bad mutation, twice, and the second one is instructive.** The first left an unused
variable and did not compile. The second changed `if err = advance(); err != nil` to
`if err = advance(); false` — which still **executes the call**, because Go evaluates an `if`
statement's init clause before its condition. Removing the call outright then failed four tests
immediately. The 4.5 rule stands and now has a sharper edge: *check that the mutation changed
behaviour, not merely text.*

**Drill 2 corrected a comment that asserted a danger which does not exist.** The comment claimed
the counter had to be read through `Writer`, because "reading from the reader would let two
terminals see the same `next_value`". Swapping to `Reader` broke nothing — because
`Store.Reader(ctx)` and `Store.Writer(ctx)` **both resolve to the live transaction** when one is
present (0.3). Inside `db.Do` they are the same executor. The real requirement is that a
transaction be present at all, which the unexported signature already enforces. The comment now
says that.

This is a new category: not a weak test, not redundant code, but **a comment describing a
mechanism the platform had already made unnecessary**. Documentation can be wrong in exactly the
way code can, and a drill is one of the few things that checks it.

**Drill 3 is 4.3's outcome again**, applied without hesitation this time: the `next_value < ?`
guard cannot fire on SQLite, where writers are serialised and the allocation sits inside the
transaction. It is kept as insurance for the engines §8 says this must survive moving to, the
comment says exactly that, and **no test was written for it** — because one that passes either way
would claim something is pinned when nothing is.

### Step 5.2 — The sales document

**Delivered.** `0023_sales.sql` (`sales_documents`, `sales_lines`), the document domain, the
repositories, the service (`Draft`, `AddLine`, `RemoveLine`, `Hold`, `Resume`, `Cancel`,
`Document`, `Documents`), and a `Catalog` port satisfied by catalog.

**D1 — the snapshot is taken when a LINE IS ADDED, not at posting.** Posting would be almost as
good, and "almost" is the gap through which a product renamed between drafting and posting changes
what the customer sees on the invoice they were quoted. The test renames the product, the SKU, and
the unit after the line exists, and asserts the line still says what was sold.

**D2 — the conversion happens in CATALOG, not in sales.** Unit arithmetic belongs to the module
that owns units. Sales could fetch two units and multiply, and it would be right today — but there
would then be two implementations of "convert a quantity", and the day one stops rounding the way
the other does is the day "2 rolls" and "200 metres" stop agreeing on an invoice already printed.

This gave **3.1's refusals their first caller**: selling half a widget now fails with
`catalog.fractional_not_allowed`, from the domain written in Phase 3, unchanged. The seam held.

**D3 — `AddLineInput` has no price field, deliberately.** The caller says what and how many. Price
comes from resolution at posting, tax from the engine, cost from the costing port. A till operator
who can type a price is a discount nobody approved, and an input struct with a price field is an
invitation to build that screen.

**D4 — `MovesStock()` lives on the TYPE.** A quotation is a promise and an order is an intention;
neither takes anything off a shelf. Putting it on the type means a report, a screen, and the poster
cannot disagree. A quotation naming a warehouse is *refused* rather than ignored, because a screen
showing it would be telling the operator something untrue.

**D5 — a held sale is the SAME document, parked.** It occupies no number and moves no stock.
Modelling it as a different kind of thing would mean resuming had to convert one into the other,
and a conversion is a place for a line to get lost.

**D6 — `UpdateDocument` is guarded on `status = 'draft'` in its WHERE clause**, not merely checked
beforehand. An UPDATE matching no rows is a posted document, and it is reported rather than
silently doing nothing. `SetDocumentStatus` is separate for exactly this reason — the guard that
protects a posted document would otherwise make cancelling a draft impossible too.

**Mutation drills — 7 run, all now fail as required.**

| # | Mutation | Result |
|---|---|---|
| 7 | The line stores no snapshot | 2 tests fail |
| 8 | The stock quantity is not converted | `…BothTheSoldAndTheStockQuantity` fails |
| 9 | A posted document becomes editable | `…CannotBeChanged` fails |
| 10 | The discount guard removed | `…CannotExceedItsLine` fails |
| 11 | The line rounds twice | 2 tests fail |
| 12 | The draft/posted number CHECK dropped | **Passed — no test wrote to the table.** |
| 13 | The number uniqueness index weakened | **Passed — same reason.** |

**Drills 12 and 13 are the fifth occurrence of the Phase 3 rule**, and this time the cause was
structural rather than an oversight: *posting does not exist yet*, so no test could produce a
posted document through the service, and the CHECK guarding the draft/posted split had nothing
exercising it. Two tests now write straight to the table.

Worth noting what those tests assert, because it is more than "a constraint exists": a **draft
holding a number** means somebody consumed one and abandoned it, and a **posted document without
one** means a posting skipped allocation entirely. Both halves of §9.4, made unrepresentable.

### Step 5.3 — Posting

**Delivered.** Four narrow ports (`Pricing`, `Tax`, `Stock`, `Credit`), their composition-root
adapters, and `Post` — one transaction doing eight things.

**This is the step the phase was built for, and every earlier seam held.** Phase 2's
`sale_revenue` and `sale_cost` rules, seeded three phases ago and never fired, now write a journal
entry from a sale. Phase 3's price resolution and Phase 4's costing port both got their first
caller. Nothing had to be reshaped to fit.

**D1 — the order of the eight steps is not arbitrary.**

```
1 refuse anything that is not a postable draft
2 resolve each line's price, and record which list answered
3 compute each line's tax, and record the RATE
4 issue the stock, and take back what it cost us
5 total the document from its lines
6 check the credit limit against the total
7 allocate the number          ← LAST of the writes
8 publish the event the posting rules turn into an entry
```

Price before tax, because tax is computed on the net. Stock before totals, because the cost comes
back from the movement. Credit after totals, because the limit is checked against what the
customer will actually owe. **The number last**, so a failure anywhere earlier consumes none —
§9.4's rule expressed as an ordering rather than as a comment.

**D2 — the ports are narrower than the services behind them.** `Pricing` here is one method;
`pricing.Service` has eight. A port mirroring the service would make every future change there a
change here. It also makes posting — the most complex transaction in the system — testable
against fakes of a few lines each, which is why its failure modes are reachable without standing
up five modules.

**D3 — the cost comes back from the stock movement.** Only inventory knows it: the costing port
(4.2) decides whether the answer is an average or a layer, and sales must not learn which. Faking
inventory in these tests would have made the most important number in the transaction a constant
the test chose, so the real service is wired.

**D4 — a credit note is a separate ACTION, not an invoice with negative amounts.** The posting
engine refuses negatives — a negative would flip a line's side silently, and "debit XOR credit,
both non-negative" is what makes an entry checkable.

**Mutation drills — 7 run, 6 fail as required.**

| # | Mutation | Result |
|---|---|---|
| 14 | The number is allocated first | 2 tests fail |
| 15 | The credit limit is never checked | `…RollsBackTheStockAndTheNumber` fails |
| 16 | The cost is not taken from the movement | 3 tests fail |
| 17 | The price reason is not recorded | `…WhichPriceListAnswered…` fails |
| 18 | The journal entry is never published | 4 tests fail |
| 19 | A quotation moves stock | `…MovesNoStock` fails |
| 20 | A credit note publishes the invoice action | **Passed — deferred to 5.4.** |

**Drill 20 passed for a structural reason, and it is recorded rather than papered over.** Credit
notes are 5.4's step; nothing posts one yet, so the branch choosing `ActionCreditNotePosted` has
no caller and no test can distinguish it. Writing a half-test now to satisfy the drill would be
pinning a mechanism whose real use does not exist. **5.4 must close it**, and the DoD review will
check that it did.

**One test fixture was wrong, and the code was right.** `receive` first put stock on the shelf
with no document, and inventory correctly posted it as a stock increase (Phase 4.3's rule) — so a
test expecting inventory at −120 found 480, because the receipt had debited 600 first. The
fixture now receives against a purchase bill, which is both realistic and what isolates the sale.

### Step 5.4 — Returns and credit notes

**Delivered.** `DraftReturn`, the source-line chain that makes §D.3 reachable, the
already-credited sum, and **two new posting rules in the seed file**.

**Drill 20's obligation is discharged.** 5.3 recorded that a credit note's action had no caller
and no test could distinguish it. It now does: the drill removing the branch fails
`TestACreditNotePostsItsOwnActionAndReversesTheSale`.

**D1 — the missing rules were a SEED FILE, not code.**

Posting the first credit note produced *no journal entry at all* — Phase 2 seeded
`sales.invoice.posted` and never a credit-note counterpart, so nothing matched. The fix was two
rules in `generic_trading.json`:

```
sale_credit_revenue   debit SALES, debit TAX_PAYABLE, credit AR
sale_credit_cost      debit INVENTORY, credit COGS
```

**No Go changed.** That is §20.3's promise tested rather than asserted: a country that books
returns differently is a different file. It is also a gap that only a real caller could have
found — the rules had looked complete for three phases.

**D2 — §D.3's chain, walked at posting.** A credit-note line names the invoice line it reverses;
that line names the stock movement it produced; that movement knows what the goods cost when they
left. Returning an item sold at 60 when today's average is 300 credits inventory with **60**.

The movement id is resolved at posting rather than copied onto the credit-note line, because a
copy of a fact that can be looked up is a copy that can be wrong. The domain's `Line` carries no
`sourceMovementID` field, and says so.

**D3 — the snapshot is COPIED from the invoice, never re-fetched.** A credit note must describe
what was *sold*, including what the product was called then. Re-fetching would make a return of a
renamed product say something the original invoice does not.

**D4 — already-credited quantities are summed ACROSS credit notes.** Two returned today and two
more tomorrow is the same overreturn as four at once, and a per-note check would miss it.

**D5 — a partial return scales the stock quantity from the ORIGINAL line's two figures**, rather
than re-converting. Returning 1 of 3 rolls returns a third of the metres, using the factor the
sale used — even if the unit's conversion has been edited since. A whole-line return copies the
figure outright: no arithmetic, no rounding, no drift.

**Mutation drills — 6 run, all fail as required.**

| # | Mutation | Result |
|---|---|---|
| 20 | A credit note publishes the invoice action | `…PostsItsOwnAction…` fails *(was deferred from 5.3)* |
| 21 | The source movement is ignored | 6 tests fail |
| 22 | The snapshot is re-fetched | `…CopiesTheInvoicesSnapshot…` fails |
| 23 | Already-credited quantities ignored | 2 tests fail |
| 24 | A draft invoice can be credited | `…OnlyAPostedInvoice…` fails |
| 25 | A credit note can be credited | `…CannotBeCredited` fails |
