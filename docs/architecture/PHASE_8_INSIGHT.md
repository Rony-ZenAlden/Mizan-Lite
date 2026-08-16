# Phase 8 — Insight

> Dashboard, reports, profit analysis, search.
>
> The first phase that adds no new document and owns no new table of its own — and the analysis
> below is mostly about how much of that is true.

---

## 1. ANALYSIS

### What exists today

Seven phases have built a system that records things faithfully and reports almost nothing. The
complete list of read surfaces:

| Surface | Owner | Shape |
|---------|-------|-------|
| `TrialBalance` | accounting | every account's position at the end of a period |
| `BalanceOfMapping` | accounting | one mapped account's balance |
| `BalanceOf`, `StatementFor`, `Age` | partner | one partner's receivable and payable |
| `Levels`, `StockOf` | inventory | quantity on hand |
| `DebtPositions` | expenses | net position per debt kind |

No profit and loss. No balance sheet. No sales figures. No margin. No stock valuation. No
dashboard. No search.

A shopkeeper using this today can post a sale and cannot answer *"did I make money this month?"*

### The question this phase turns on

Where does a report live when it spans modules?

`module-isolation` forbids a module importing another's internals. A reporting module that read
every table directly would violate it by construction — and would be the single place that breaks
every time any module changes a column, which is the coupling the rule exists to prevent.

Three candidate answers, and the phase's first job is to find out which questions actually need
which:

1. **The module that owns the data owns the report.** No new mechanism.
2. **Cross-module reports are assembled through PORTS in the composition root** — the shape
   Phase 7 already used for partner balances, where three ports satisfied at the root let the
   partner module compute a balance from sales and purchasing documents it cannot see.
3. **A read model** — a projection table maintained by events, queried by a reporting module.

### The finding that shrinks this phase

**Almost nothing here is cross-module, because every document already snapshots what it needs.**

This is §9.3's rule paying a second dividend. It was written for reprint fidelity — *"reprinting a
two-year-old invoice must reproduce it byte-identically"* — and the consequence for reporting was
not the reason for it:

- **Profit and loss** is revenue and expense accounts summed over a period range. Every posting
  from every module is already in the ledger, put there by Phase 2's rules. Accounting alone.
- **Balance sheet** is asset, liability and equity accounts, cumulative to a date. Accounting
  alone.
- **Margin per product** needs the sale price and the cost of goods sold. `sales_document_lines`
  stores `cost_micro` — *"what the goods cost us AT THE MOMENT OF SALE, frozen"* — beside the
  price it charged. Sales alone.
- **Stock valuation** is quantity times cost, both owned by inventory. Inventory alone.
- **Purchase analysis** reads purchasing's own documents. Purchasing alone.

So answer 1 covers five of the six things this phase must produce, and the two mechanisms that
would have needed inventing are not needed.

What is genuinely cross-module is exactly two things:

- **The dashboard**, which is several modules' numbers on one screen. That is an ASSEMBLY, not a
  computation: each figure is one module's answer, and nothing combines two modules' data into a
  third number. The composition root already assembles.
- **Search**, which must find a product, a partner, an invoice and a bill from one box. Genuinely
  cross-module, genuinely a new mechanism.

### Why no read model

Answer 3 is the one to refuse explicitly, because it is what an ERP is expected to grow.

A projection table is worth its cost when the query is too slow to run live and the staleness is
acceptable. Neither holds here. A shop's ledger is hundreds of thousands of journal lines, not
hundreds of millions; `account_balances` already exists as a per-period projection and is already
rebuilt and VERIFIED (§20.5). And a shopkeeper asking "what did I sell today" will not accept an
answer that is an hour old.

A second projection layer would also need its own rebuild, its own verifier, and its own drift
report — three mechanisms whose absence is currently impossible to notice, because they do not
exist. **The moment a fact needs a third home, the second home was the wrong one.**

If a report is later found to be too slow, the answer is an index, and then a projection with a
verifier — in that order, driven by a measurement rather than by an expectation.

### What "profit analysis" has to mean

§32 names it separately from "reports", so it is not just the P&L. Three questions a shopkeeper
actually asks:

- Which products make money? (margin per product, sorted)
- Which customers are worth having? (revenue and margin per partner)
- Is this month better than last? (a period comparison)

All three are sales' own data. The third needs no new query shape — a report over a date range,
run twice.

The trap is the word "profit". Gross margin (revenue less cost of goods) is a SALES figure and is
exact. Net profit is a LEDGER figure, because it includes rent, wages and depreciation that no
sale knows about. Reporting either one as "profit" without saying which is how a shopkeeper
concludes they are doing well while losing money. **Both are reported, both are named, and neither
is called "profit" alone.**

---

## 2. DESIGN

### D1 — reports live with their data, and there is no reporting module

Five of the six report families are computed by the module that owns the data, exposed as ordinary
service methods, and bound through that module's existing façade.

This is not a new decision so much as declining to make one. It follows the rule recorded in 4.4
and applied in every phase since: *before declaring a new place for a fact to live, look for the
one an earlier phase already left.*

The cost is that no single package can be pointed at and called "the reports". The benefit is that
a report cannot drift from the data it reports on, because it is compiled against it — and that a
column rename breaks the report in the same commit rather than at runtime three months later.

### D2 — a report is a QUERY, never a document

Nothing in this phase writes. No report is numbered, audited as a state change, or posted.

This has a structural consequence worth stating: every method added here is safe to call twice,
and the permission that guards it is a `view` permission. It also means this phase adds no
migration for a document table — the only schema it may add is an INDEX, and each one must be
justified by a query that exists.

### D3 — every financial statement is derived from `account_balances`, and says so

The P&L and balance sheet read the same projection the trial balance does. They do not read
`journal_lines`.

Two reasons. The projection is already rebuilt and verified against the ledger, so a statement
built on it inherits that guarantee; building a second path to the same numbers would mean two
answers to one question and no rule for choosing. And §20.5's reasoning applies unchanged: a shop
with three years of history has hundreds of thousands of lines and about two hundred accounts.

The consequence is that **a wrong balance projection produces a wrong P&L silently**, which is
exactly the risk `RebuildAndVerifyBalances` exists to catch — and which this phase must make
reachable from the reports screen, not only from a scheduled job.

### D4 — a statement is a TREE, because a flat list of two hundred accounts is not a report

`accounts` has a parent (`0010`), and a P&L that lists every leaf account is a trial balance with
a filter. The statement builds the account tree, sums each subtree, and returns levels — so
"Revenue 45,000" can be opened to find "Sales of goods 40,000, Services 5,000".

The subtotal is computed from the CHILDREN, never stored. A stored subtotal is a second home for
a fact whose first home is the balances it sums.

### D5 — every report is bounded by a date range, and the range is explicit

No report defaults to "everything". A shop's third year of trading should not silently produce a
report over three years because nobody passed a date.

Balance-sheet accounts are cumulative and P&L accounts are periodic — so the same range means two
different things depending on the account type, and the report says which it applied.

### D6 — search is a registry the composition root fills

The one genuinely new mechanism. Each module that has something findable contributes a searcher;
the composition root collects them; one binding fans out.

It follows the shape the module contract already uses for jobs and permissions — a module declares
what it offers, the root assembles — rather than a package that imports every module.

A search result carries a KIND, an id, a label and a subtitle. Deliberately not a typed union:
the caller navigates by kind, and a union would need extending in three places every time a
module became searchable.

### D7 — the dashboard asks each module its own question

No cross-module arithmetic. Every tile is one module's answer, labelled with which module answered
and over what range.

A tile that needed two modules' data multiplied together would be a report, and would belong to
whichever module owns the multiplication — which is D1 again.

---

## 3. WHAT THIS PHASE DOES NOT DO

- **No report designer.** Configurable report layouts are a §25 plugin concern. The reports here
  are fixed, and adding one is a code change.
- **No scheduled or emailed reports.** Phase 9 owns notifications.
- **No export.** Phase 9 owns import/export. A report screen may print (Phase 5's printing
  platform), and that is the whole of it.
- **No budget or forecast.** Comparing actuals to a budget needs a budget, which is a document
  nobody has asked for.
- **No consolidated multi-company reporting.** One company at a time, as every phase so far.
- **No read model, no reporting module** — see the analysis.

---

## 4. STEP SEQUENCE

| Step | What |
|------|------|
| 8.1 | Financial statements — P&L and balance sheet, from the balances projection |
| 8.2 | Sales analysis — by period, product, partner; and gross margin |
| 8.3 | Stock valuation and movement history |
| 8.4 | Purchase analysis, and the supplier side of the same questions |
| 8.5 | Global search — the registry, and each module's searcher |
| 8.6 | The dashboard — assembly in the composition root |
| 8.7 | Bindings and screens |
| 8.8 | Phase 8 Definition-of-Done review |

---

## 5. DEFINITION OF DONE

1. A profit and loss statement for a date range, as a tree, whose total ties to the ledger.
2. A balance sheet as at a date that BALANCES — assets equal liabilities plus equity — and whose
   failure to balance is reported rather than hidden.
3. Gross margin and net profit are both reported, both named, and neither is called "profit"
   alone.
4. Sales can be analysed by period, by product and by partner, and each uses the cost SNAPSHOTTED
   at the time of sale rather than today's cost.
5. Stock is valued at the same cost the ledger carries it at, and a disagreement is reported.
6. One search box finds a product, a partner, and a document, and a module that becomes searchable
   needs no change to the search mechanism.
7. Every dashboard tile names the module that answered and the range it covers.
8. No report writes anything: every method in this phase is safe to call twice.
9. No new document table, and every index added is justified by a query that exists.
10. Every new binding has a declared policy, and every report permission is a `view` permission.
11. A report over an empty company returns an empty report, not an error and not a zero that
    looks like an answer.
12. `make ci` green, with the mutation drills each step declares — and a drill that PASSES is
    treated as a defect in the test, the code, or the mutation.

---

*Implementation proceeds step by step; each step records its own decisions and drills.*

---

## Step 8.1 — financial statements

A profit and loss and a balance sheet, both built on `account_balances` — the projection the
trial balance already reads, and the one §20.5 already rebuilds and verifies.

Two service methods, two view permissions, one new query file. No migration, no index: both
queries filter on `accounts.company_id` and `account_balances.fiscal_period_id`, which
`ix_accounts_path` and 0012's own index already cover.

### D1 — a statement widens to whole periods, and SAYS which ones

`account_balances` is per-period, so the finest resolution any statement built on it can have is
one fiscal period. A request for 1–15 March cannot be answered exactly. The three honest options
are to refuse, to widen silently, or to widen and report both ranges.

Refusing makes the common case fail for anyone whose fiscal calendar does not start on the 1st.
Widening silently reports March's figures under a fortnight's heading, which is how somebody
concludes their sales doubled.

So `ProfitAndLoss` carries `RequestedFrom`/`RequestedTo` alongside `Covered`, and a screen that
finds them different can say so. **A report that cannot answer the question asked should say what
question it answered.**

The period filter is OVERLAP, not containment. `start >= from AND end <= to` reads naturally and
silently drops the period a mid-month date falls in — the whole month, not part of it.

### D2 — the balance sheet carries the unclosed result, and that is the step's real content

Assets equal liabilities plus equity only once revenue and expense have been closed out. Until
the year end the profit so far sits in accounts the balance sheet does not show, so a sheet that
ignored them would be out of balance **by exactly the profit, every day of every year except
one**.

It is computed from the revenue and expense positions rather than stored, because closing has not
happened and a stored figure would be a second home for what those accounts already say. It is
also folded INTO `EquityMinor` rather than left beside it: a reader wants one equity figure that
ties, not two they have to add up.

`TestTheUnclosedResultIsTheSameFigureBothStatementsReport` pins the two statements to one answer.

### D3 — out of balance is REPORTED, never enforced

A statement that refused to render when the books are wrong would hide the evidence needed to
find out why. That is 7.4 D3's rule — invariant 5 is a report, not a repair — applied to a screen
instead of a job.

### D4 — the subtotal is summed from the children

A parent's figure is the sum of its children, never the parent's own balance. Both give the same
answer today, because a posting to a non-postable account is refused — but nothing in the schema
promises it, and a restore or a repair script does not go through the posting path.

`TestASubtotalIsTheSumOfItsChildrenAndNotAStoredFigure` writes exactly that row straight to
`account_balances` and requires the subtotal not to move.

### D5 — the presentation edge is here, and only here

The ledger is debit-positive throughout: revenue holds a negative balance, correctly, and an
accountant who sees it stops trusting the report. 0012 named this the presentation edge and said
the conversion happens once. `presentationSign` is that once.

### The drills

D163–D170, eight. Two passed, and each cost a change:

- **D167** — reporting the EARLIEST period end instead of the latest changed nothing, because
  every covered-range assertion used a single period, where the two are the same date. A
  three-period range was added. *A range tested only at length one is not a range.*
- **D169** — hardcoding `OutOfBalanceMinor = 0` left every balance-sheet test green, because they
  all assert it IS zero. **A detector tested only where it should stay silent is a detector
  nobody has heard.** `TestABalanceSheetThatDoesNotBalanceSaysSo` writes an unmatched balance
  straight to the table and requires the statement to report it — and to still render.

D169 is the one worth keeping: it is the same shape as 8.1's own D3 decision. Deciding that
something is reported rather than enforced obliges you to test that it is actually reported.

---

## Step 8.2 — sales analysis and gross margin

Three cuts of one question — by period, by product, by partner — plus the margin figure Phase 8's
analysis warned about naming carelessly.

One permission for all three. A grant that let somebody see revenue per customer but not per
product would protect nothing; they could add the products up.

### D1 — the lines come back raw and the arithmetic happens in Go

The obvious query sums `quantity_stock_micro * cost_micro` per product in SQL. That would be a
SECOND implementation of `domain.CostOfLine`, in a language with different integer division,
rounding at a different point.

Phase 6 already priced that mistake. `valueOf` produced values in major units from fields named
`…Minor` for two phases, invisible because every consuming test used a zero-decimal currency —
and the defect was not that the arithmetic was hard, but that two places believed they agreed.

So `SoldLinesInRange` returns rows and the service sums them with the function the document
itself used. A year of a shop's sales is tens of thousands of lines. If that is ever too slow the
answer is an index, then a projection with a verifier, in that order.

### D2 — the credit-note sign lives beside the query, and is applied once

A credit note's lines carry POSITIVE quantities; `NewLine` refuses a negative one, because a
return is a document rather than a negative row. An analysis that summed both document types
would report a returned sale as two sales, and **a shop with heavy returns would read as its own
best month**.

`SoldLine.Sign()` is a method on the row, so no caller can forget it and no caller can apply it
twice. Three separate `GROUP BY` queries would have been three places to forget it — which is
also why the three analyses share one aggregation with the grouping passed in.

### D3 — revenue is net of discount and before tax

Tax is collected on behalf of a government and owed to it. A margin computed against a
tax-inclusive figure flatters every product by the tax rate, which here is 15% — more than most
shops make.

### D4 — a ranking and a sequence are different reports

"Which products make money" is a ranking. "Is this month better than last" is a sequence. A
period report sorted by margin puts July before March whenever July did better, answering a
question nobody asked and hiding the one they did. Ties break on the key in both, so a printed
copy cannot disagree with the screen it came from.

### D5 — the walk-in row exists

Most of a shop's trade names no partner. Dropping those lines would make the partner analysis
total less than the other two — and a shopkeeper WILL add the rows up and compare.
`TestTheThreeAnalysesAddUpToTheSameTotal` is what holds the three to one answer.

### The drills

D171–D176, six. Two passed, and both for the same underlying reason: **the test data could not
tell the mutation apart from the truth.**

- **D172** — deleting `status = 'posted'` changed nothing. A draft line has no money on it at
  all: price, tax and cost are resolved at posting, so a counted draft contributes zero revenue
  either way. The QUANTITY is set when the line is added, and is the one field a draft can leak;
  the test now asserts on it.
- **D174** — removing the chronological branch changed nothing, because the test put the BIGGER
  sale on the earlier day. Ranking and sorting by date gave the same answer. The data was
  inverted so the two orders disagree, and the test now checks that they do before relying on it.

Both are the same lesson in different clothes, and it is worth stating plainly: **a test whose
fixture cannot distinguish the right answer from the wrong one is not evidence, however carefully
it is written.** D174's original even had a comment claiming the later day earned less — the
comment described the test that should have been written.

---

## Step 8.3 — stock valuation, and whether the books agree

What is on the shelf, what it is worth, and whether the general ledger says the same.

### D1 — the valuation reads levels, not movements

`stock_levels` carries `avg_cost_micro`, maintained by the same costing the ledger's entries were
computed from, and 4.6's `VerifyLedger` already proves the levels reconcile to the movements.
Recomputing from the movement history here would be a second costing implementation — which 8.2
declined to write in SQL for the reason Phase 6 paid for.

`valueOf` was EXPORTED at its third caller, the same threshold `round.Allocate` was promoted to
the kernel at. A valuation writing `quantity * cost / scale` itself would be a third
implementation of the conversion whose own comment records finding it wrong in its second.

### D2 — inventory asks the ledger, through a port

The shape Phase 7 used for partner balances. Inventory cannot import accounting, so it declares
one method — `StockValueMinor` — and the composition root satisfies it from
`BalanceOfMapping(…, "INVENTORY")`.

The mapping, not an account code. A company that renumbered its chart is still checkable, and the
port cannot start making accounting decisions (§20.3).

### D3 — reported, never repaired; and refused when nobody asked

`Valuation` returns the difference and still renders. `VerifyValuation` REFUSES when no control
ledger is attached rather than returning zero — a scheduled check that only reports failures would
read a zero as "the books agree" when they were never asked.

### D4 — zero-stock rows are not valuation lines

A shop that has ever stocked a thousand products has a level row for each. Nine hundred lines
worth nothing bury the ninety that matter. `VerifyLedger` covers the empty ones and reads every
row.

### The drills

D177–D182, six. Two passed:

- **D179** — the first attempt left `errs` unused and did not compile, so it was not a drill at
  all. Rewritten to return zero instead of refusing, it fails.
- **D181** — pointing the composition root's adapter at the COGS mapping instead of INVENTORY left
  **every inventory test green**, because the module's own tests use a fake ledger. A fake proves
  the arithmetic and the reporting and says nothing about whether the real port asks the right
  question.

D181 is Phase 6's central finding arriving again: **a seam is only proven by a caller.**
`TestTheStockValuationReconcilesToTheGeneralLedger` is that caller, and it lives in
`internal/bootstrap` because that is where the two sides meet.

Writing it turned up two things the fixture had to learn. Applying the chart is not enough — the
posting RULES have to be applied too, or every movement fires an event that matches nothing and
the ledger sits at zero, which reads as a reconciliation failure rather than as a fixture that
never wired the books up. And a documentless ISSUE deliberately posts nothing (4.3), so using one
would have reported a designed state as a defect; the test writes stock off with an adjustment
instead.

---

## Step 8.4 — purchase analysis

Spend by period, by product, by supplier. Structurally 8.2's mirror, with three differences worth
recording rather than three that were copied.

### D1 — spend is BILLS, not orders and not receipts

An order is an intention; a receipt is goods arriving. Neither is money owed. The bill is where
the supplier states the price — which is the whole reason 6.5 made it a separate document — and a
spend report built on orders would count what was asked for rather than what was charged.

`TestAnOrderIsNotSpendAndNeitherIsADelivery` posts a full delivery and requires the report to stay
empty.

### D2 — one UNION, not two queries

Bills and supplier returns live in different tables with different date columns. Reading them in
one query rather than merging two in the service puts `status = 'posted'` and the sign in one
place each — which is 8.2's finding approached from the other side.

### D3 — Spend is not sales' Figures, and the reason is not module-isolation

The two shapes are nearly identical and `module-isolation` would forbid sharing anyway, but that
is the weaker half of the argument. The stronger half: **a sale has a cost and therefore a margin;
a purchase does not, because the cost IS the purchase.** A shared type would carry a
`GrossMarginMinor` that means nothing here, and a reader would have to work out which fields
apply.

Promotion to a shared kernel type is the answer at the THIRD caller — where `round.Allocate` and
`domain.ValueOf` were both promoted. Two is not a pattern.

### D4 — no walk-in row

A bill's supplier is NOT NULL: money is owed to somebody, and 6.5 made that a schema constraint
rather than a convention. So unlike the sales side there is no unattributed bucket, and a row
without a partner would be a defect — which the test asserts rather than assumes.

### The drills

D183–D187, five, all failed on the first attempt.

That is worth a sentence, because it is not a boast. The two lessons 8.2's passing drills produced
were applied while writing these tests rather than after: the draft test asserts on the QUANTITY,
which is the field a draft can leak, and the period test puts the SMALLER bill on the earlier day
so that ranking and chronology disagree. Both assertions exist because a drill in the previous
step found their absence.

---

## Step 8.5 — global search

The phase's one genuinely new mechanism, and the only thing in it that no single module can
answer.

### D1 — a registry the composition root fills

The alternatives were a package importing every module — which `module-isolation` forbids, and
which would be the single file that breaks whenever any module changes a column — or a search
index maintained by events, which is a projection carrying every obligation this phase's analysis
refused: its own rebuild, its own verifier, its own drift report.

Each module contributes a `Searcher`; the root collects them. A module that becomes searchable
adds one method and nothing in `platform/search` changes.

### D2 — assembled after the graph, unlike Series() and Permissions()

Worth stating because it looks inconsistent. `Series()` and `Permissions()` are on the module
CONTRACT, readable with no database and no services — which is what lets them be enumerated
before the graph exists, and it is why `runMigrations` can work at all.

A searcher needs its module's SERVICE. It can only be collected once the graph is built, so it is
wired in the composition root beside the ports rather than declared on the interface.

### D3 — one broken searcher does not break the search

A search box that fails entirely because one module's query is broken fails for reasons the user
cannot see or fix. `Response.Failed` names who did not answer, and the rest is returned — 7.4 D3
again: report rather than refuse, so the evidence survives. Finding nothing and failing are
different answers, and a caller deciding between "nothing found" and "search is having trouble"
needs to tell them apart.

### D4 — the bound is PER SEARCHER

A shop searching "AH" must not get two hundred products and no partners purely because the
catalogue was asked first. "Find me Ahmad" is the query a shared budget fails on.

### D5 — grouped by kind and ranked within it, never one global score

Comparing "how well does this product match" with "how well does this invoice match" needs a scale
neither module knows about. Inventing one would make the order look meaningful when it is
arbitrary.

Within a kind the rank is real and it is what makes search usable: a shopkeeper with a scanner
searches by SKU or code, both exact, and without a rank the product whose NAME merely contains the
same characters appears above the one they meant.

### D6 — `Like` escapes, and the ESCAPE clause is its other half

A query containing `%` matches everything; one containing `_` matches any character. So a customer
searching for a product called "50%" gets the whole catalogue.

SQLite honours the escape only when the query says `ESCAPE '\'`, which means a searcher can use
the helper, look correct, and have escaping that does nothing.
`TestEverySearcherEscapesItsWildcards` is what catches that, and it is written against real data —
a product actually named "50% off bundle".

### The drills

D188–D191, four. Two passed:

- **D189** — subtracting what had already been found from each searcher's budget changed nothing,
  because both stubs returned nothing at all. A shared budget and a separate one are the same
  number when nobody has found anything. The stubs now return results.
- **D190** — the first two attempts did not remove the `ESCAPE` clauses at all; the escaping in
  the Python replacement did not match what was in the file. **A mutation that does not apply is
  not a passing drill, it is no drill** — and the only way to know the difference was to count
  the occurrences afterwards, which is now part of how these are run.

D191 is the one the end-to-end test exists for: removing `app.Partner.Searcher()` from the
registry left every unit test in `platform/search` green. A searcher that compiles, is never
registered, and silently contributes nothing is invisible to everything except a caller.

---

## Step 8.6 — the dashboard

Six tiles from three modules, assembled in the composition root.

### D1 — an assembly, and a structural test that keeps it one

Phase 8's analysis called the dashboard an assembly rather than a computation: every tile is one
module's own answer, and nothing combines two modules' data into a third number. A tile that
multiplied two modules together would be a REPORT, belonging to whichever module owns the
multiplication — and this file would have quietly become the reporting module the analysis
refused.

A comment saying so is not a check. `TestTheDashboardDoesNoCrossModuleArithmetic` PARSES
`dashboard.go` and requires every tile's amount to be a single selector, literal, or call — not
`x - y`. It is a blunt rule, and being blunt is what makes it hold: the moment a tile needs
arithmetic, it fails, and somebody has to decide where the work belongs rather than adding one
more line.

The scan also asserts it found at least four tiles, because a parser that matched nothing would
pass while checking nothing — 7.6's D162 in a different shape.

### D2 — `Periodic` exists because two kinds of figure share one screen

Sales is a figure over a range; stock value is a position as at now. Rendering them side by side
without saying which is which is how "stock value 40,000" gets read as this month's purchases.

The test requires BOTH kinds to be present, so the distinction is exercised rather than being a
field nobody sets differently.

### D3 — a broken tile is marked, not fatal

A home screen that renders nothing because one query broke fails entirely for a reason nobody can
see. Same choice as `search.Response.Failed`, same reason.

The test breaks it with a backwards date range — which sales and purchasing both refuse and
inventory does not read — so one source fails while another still answers, with no fake involved.

### D4 — the range is the caller's

No hidden default of "this month". A screen showing figures for a period the user did not choose
is a screen whose numbers cannot be checked; 8.1 D5 made the same call for statements.
`MonthToDate` exists so a caller can ASK for the common range rather than have it assumed, and it
reads the injected clock so a test can make "today" deterministic.

### The drills

D192–D195, four, all failed on the first attempt.
