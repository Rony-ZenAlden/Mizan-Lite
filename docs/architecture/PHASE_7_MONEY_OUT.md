# Phase 7 — Money out

> Expenses, debts, settlements.
>
> §32's shortest phase description, and the one that needs the most analysis before anything is
> built — because two of its three words turn out to mean something different from what the
> original table planned.

---

## 1. ANALYSIS

### 1.1 The `payments` module in §7.2's table no longer exists

The architecture's module table lists:

| Module | Tables |
|---|---|
| payments | `payment_methods`, `payment_transactions`, `settlements`, `settlement_allocations` |
| expenses | `expense_categories`, `expenses` |

That table was written before Phases 5 and 6 built payments. Both of them declined a shared
payments table, twice and independently, with the same argument:

> *"One table would need a nullable foreign key to each document type and a CHECK that exactly one
> is set — a discriminated union hand-rolled in SQL, with a direction filter on every query that
> somebody eventually writes without. It would also make sales and purchasing share a table, which
> `module-isolation` forbids precisely so neither can change shape without the other's agreement."*

So `sales_payments` + `sales_payment_allocations` and `supplier_payments` +
`supplier_payment_allocations` already exist, each owned by the module whose documents they
settle. **`payment_transactions`, `settlements` and `settlement_allocations` are subsumed.**

`payment_methods` is subsumed too, and by something better: the method became part of the posting
ACTION, so a business whose cheques clear through a separate account edits a seed file rather than
a row (§20.3, proven twice).

**This is worth recording rather than quietly skipping.** The original table was a reasonable plan
made before the shape was known; two phases of building found a better one. A phase that ticked
"payments module: done" by creating the tables anyway would be following a plan instead of a
design.

### 1.2 What is genuinely missing

Three things, and none of them is a payments table.

| Missing | Why it matters | Served by nothing today |
|---|---|---|
| **Expenses** | Money out that buys no stock — rent, wages, fuel, utilities. Most of what a small business spends. | ✓ |
| **Partner balances** | §7.13 invariant 5: *"Partner balances equal the sum of their open documents minus settlements."* Nothing computes a partner's position across sales AND purchases. | ✓ |
| **Debts** | Money in or out with no trade document: an owner putting money in, a loan from a relative, an advance to an employee. Ordinary in the target market and unrepresentable today. | ✓ |

### 1.3 Why expenses cannot just be purchase bills

The instinct is to reuse the bill: it has a supplier, a date, a total, and it posts to payables.

It fails on the line. A purchase bill line **must** name a goods receipt line (`NOT NULL`), because
that is what makes the three-way match structural rather than validated. An electricity bill has no
delivery, no stock movement, and nothing to match against — so it would need that column made
nullable, and the moment it is nullable the match becomes a rule somebody remembered to write
rather than a state the schema cannot hold.

**Phase 6's strongest guarantee would be traded for one table's reuse.** An expense is its own
document.

---

## 2. DESIGN

### D1 — An expense is a document with a CATEGORY, not a product

A purchase line points at a variant; an expense line points at an expense category, which carries
the account it posts to. That is the whole structural difference, and it is why the two cannot
share a table.

Categories are **reference data seeded per country profile**, because what a business must report
separately is a tax question that differs by jurisdiction — and adding one must not need a release.

### D2 — An expense may have a supplier, and usually does not

The electricity company is a partner if you want a statement from them and a name on a report if
you do not. `partner_id` is nullable, and an expense with none posts to the cash or bank account
its payment method implies rather than to payables.

That gives two shapes from one document, which is right: an expense **paid now** (fuel, bought
with cash) and an expense **owed** (rent, invoiced monthly) are the same business fact at different
moments.

### D3 — Partner balances are DERIVED, and verified rather than maintained

§7.13 invariant 5 names them, and the temptation is a `partner_balances` table updated on every
posting. Phases 2 and 4 both rejected that shape for the same reason and both were right: a
maintained projection that is never checked is a number nobody should trust.

So a partner's balance is a **query** across their open sales documents, purchase bills, and the
settlements against both — and a **verifier** reports drift the way `Reconcile` does for stock. If
performance ever demands a projection, it will be one that can be rebuilt and is checked, which is
Phase 2's shape and not a new decision.

### D4 — A debt is not a partner document

An owner putting money into the business, a loan from a relative, an advance against wages: money
moves, nothing is bought or sold, and no invoice exists.

Modelling these as sales or purchase documents would put them in revenue and cost reports where
they are not revenue or cost. They get their own document with an explicit **direction** — money
in or money out — and their own posting rules, so where they land is a seed-file decision like
everything else.

**A debt is settled by a payment, and reuses the settlement shape** rather than inventing a third:
the same allocation table pattern, owned by this module.

### D5 — Recurring expenses are a TEMPLATE, not a schedule

Rent is the same every month. The instinct is a scheduler that posts it automatically.

Rejected for v1: an expense that appears in the books without a person deciding it did is an
expense nobody checked, and the failure mode — a standing order posted after the lease ended — is
silent and compounding. A template that pre-fills a form somebody confirms costs one click a month
and keeps a human in the loop.

The schema carries `is_template` and a period, so making it automatic later is a job registration
rather than a migration.

---

## 3. WHAT THIS PHASE DOES NOT DO

- **No payroll.** Wages as an expense, yes. Calculating them — tax bands, social insurance,
  end-of-service — is a domain the size of this whole application and jurisdiction-specific in
  every detail.
- **No fixed assets or depreciation.** An expense that buys a van is a capital purchase, and
  depreciation schedules belong with period-end.
- **No budgets.** Comparing spend against a plan is an *insight* question (Phase 8), and it needs
  the spend to exist first.
- **No bank reconciliation.** Matching a statement against recorded payments is Phase 9's
  import/export territory.

---

## 4. STEP SEQUENCE

| Step | Contents |
|---|---|
| **7.1** | The expenses module: categories, the expense document, posting |
| **7.2** | Expenses paid now vs owed, and settling what is owed |
| **7.3** | Debts: money in and out with no trade document |
| **7.4** | Partner balances and the statement, with a verifier |
| **7.5** | Bindings and screens |
| **7.6** | Phase 7 Definition-of-Done review |

---

## 5. DEFINITION OF DONE

1. An expense posts to the account its CATEGORY names, and purchasing/sales code is not involved.
2. An expense paid immediately never touches payables; one that is owed does.
3. Expense categories are seed data — adding one needs no release and no migration.
4. A partner's balance equals their open sales documents, less their open purchase bills, less
   settlements against both — computed, and verified against the documents that justify it.
5. A debt moves money without appearing in revenue or cost.
6. A debt can be settled in parts, and what remains is derived from its settlements.
7. Money out contains no accounting logic; every posting goes through Phase 2's rules.
8. A number is allocated only at posting; an abandoned draft consumes none.
9. Every new binding has a declared policy; every state change is audited in-transaction.
10. Every module that runs also declares itself, and every façade is exported and attached
    (6.8's checks, extended to this phase's module).
11. Amounts are exact at every currency scale, including one with minor units — 6.6's lesson
    applied from the start rather than discovered.
12. `make ci` green, with the mutation drills each step declares — and a drill that PASSES is
    treated as a defect in the test, the code, or the mutation.

---

*Implementation proceeds step by step; each step records its own decisions and drills.*

---

## Step 7.1 — the expenses module

**Delivered.** `0034_expenses.sql`, the domain, the repository, the service, ten seeded
categories, five posting rules, the composition-root wiring — and a **third account-selector kind
in the rules engine**.

### `document:accounts` — the narrow exception to §20.3

Every posting until now knew which accounts it touched. A sale moves revenue, receivables and tax;
a rule can name all three, and that is what makes "the module names no accounts" a real guarantee.

**An expense does not.** A business has forty categories, each pointing at its own account, and no
rule can enumerate them. Three ways out were considered:

| Option | Why not |
|---|---|
| Forty mappings | Mappings name concepts the RULES know about; there are a handful by design. Forty would make the mapping table the chart of accounts. |
| One "expenses" account | Destroys the only thing an expense report is for. |
| The module reads the account and posts directly | Names accounts in Go — precisely what §20.3 forbids, and where every "just this once" starts. |

So the **document** carries the debits — one amount per account, snapshotted on its lines — and the
rule says `document:accounts`. The rule still decides the **other** side, which is what differs by
payment method and is exactly what §20.3 exists for.

This is a narrow exception and is documented as one. A module using it for anything a mapping
could express would be naming accounts in Go with extra steps.

### D1 — this is not a purchase bill, and one column says why

A purchase bill line **must** name a goods receipt line, which is what makes the three-way match
structural rather than validated. An electricity bill has no delivery. Reusing the table would mean
making that column nullable — and the moment it is nullable, Phase 6's strongest guarantee becomes
a rule somebody remembered to write.

### D2 — one document, two moments

`settlement` gives an expense **paid now** (fuel, cash) and one **owed** (rent, invoiced) from one
table. They are the same business fact at different moments, and the method becomes part of the
posting action so which account the money left is a seed-file decision.

A `CHECK` keeps the two consistent: paid-now must say how, on-account must not — naming a method
for something unpaid would let a "what left the bank" report count it.

### D3 — recoverability is per LINE

Most jurisdictions disallow reclaiming tax on entertainment and allow it on fuel, and one card
statement routinely carries both. Deciding it per document would force somebody to split a receipt
in two, which is how a rule gets ignored.

Blocked tax is **not lost**: it becomes part of what the thing cost and lands in the expense
account, because a business that claimed it would be making a claim a revenue authority disallows.

### D4 — a recurring expense is a TEMPLATE, not a schedule

An expense that appears in the books without a person deciding it did is an expense nobody checked,
and a standing order posted after the lease ended is silent and compounding. A template pre-fills a
form somebody confirms; the schema carries `is_template` and a period so making it automatic later
is a job registration rather than a migration.

### Two Phase 6 lessons applied from the start

- **Audit actions and posting keys are different strings.** The Phase 6 review found them declared
  apart and given identical values; here they differ by construction, and a test asserts it.
- **The test fixture uses a two-decimal currency.** 6.6's scale defect survived two phases because
  every test that consumed a costed value used a currency with no minor unit.

**Mutation drills — 7 run, 0 passed** (one bad mutation, redone).

| # | Mutation | Result |
|---|---|---|
| 120 | Blocked tax is claimed anyway | 2 tests fail |
| 121 | The posting action ignores the method | fails |
| 122 | An expense on account posts as if paid | fails |
| 123 | The document carries no per-account debits | 3 tests fail |
| 124 | The whole tax is claimed, recoverable or not | 2 tests fail |
| 125 | A template can be recorded | fails |
| 126 | Document-named lines are emitted in map order | fails |

**Drill 126** needed a test written for it. The debits come from a **map**, and Go randomises map
iteration deliberately — so without a sort, one expense produces its journal lines in a different
order on every run. Nothing else in this application posts from a map, so this is the only place
the problem arises and the only place a test can catch it.

---

## Step 7.2 — settling what is owed

**Delivered.** `0035_settlements.sql`, expense settlements with allocations, four posting rules —
and **`kernel/settle`**, the settlement arithmetic extracted at its third caller.

### The third caller, and what actually moved

Sales settles invoices, purchasing settles bills, and this settles expenses left on account. Three
copies of "payment + allocations" — which by Phase 6's own recorded rule looks like a mistake:

> **The moment a fact needs a third home, the second home was the wrong one.**

The rule applies, and the precision matters. **What moved is the ARITHMETIC**: what over-allocation
means, what outstanding means, and that neither may go negative. That was genuinely copied twice
and now lives in `kernel/settle`, the same resolution `round.Allocate` got.

**What did not move is the DATA.** Phases 5 and 6 each rejected a shared table twice and
independently, and the argument still holds: a shared allocation table would need a nullable
foreign key to sales documents, purchase bills, *and* expenses, plus a `CHECK` that exactly one is
set — a discriminated union hand-rolled in SQL, with a filter on every query that somebody
eventually writes without.

**A real foreign key to a real table is worth more than one fewer table.** Three small tables with
integrity beat one wide table with a `CHECK` standing in for it.

The extraction is proven the way the others were: sales and purchasing delegate to the new package
and **their tests pass unchanged**.

### D1 — the codes stay in the modules

`kernel/settle` returns sentinel errors, because it sits below the vocabulary of categories and
codes and reaching up for them would invert the layering. Each module translates into its own
codes, because a code is an i18n key and a screen renders it.

The kernel decides what the **rule** is; the module decides what the **user is told**.

### D2 — settling touches the expense's category account not at all

The expense reached its category account when it was **recorded**. Settling moves only what was
owed and where the money came from — posting it again would double the cost and nothing downstream
would look wrong.

### D3 — an expense paid on the spot cannot be settled again

It is already gone. Settling it would credit the bank twice for one payment, and the only symptom
would be a bank balance quietly short.

**Mutation drills — 6 run, 1 passed.**

| # | Mutation | Result |
|---|---|---|
| 127 | Outstanding is allowed to go negative | fails |
| 128 | A document can be over-settled | 2 tests fail |
| 129 | A payment allocates more than it is worth | 2 tests fail |
| 130 | An expense paid on the spot is settled again | fails |
| 131 | The settlement action ignores the method | 2 tests fail |
| 132 | Draft settlements count as paid | **Passed → test written at the repository** |

**Drill 132 is drill 108 again**, in the module written after it. `Settle` drafts and posts in one
transaction, so no draft ever carries allocations through the service and the `posted`-only filter
is unreachable from the API. It is still right for the day a draft flow exists, and is now
asserted by writing a draft settlement straight to the table.

That the *same* unreachable-guard shape appeared in two modules is itself the finding: **any
service that creates and commits a document in one call has a "drafts do not count" filter that
its own API cannot exercise.**

---

## Step 7.3 — debts

**Delivered.** `0036_debts.sql`, the debt domain, the service, three accounts and mappings, and
**eight posting rules**.

### D1 — a debt is not a sale or a purchase

An owner putting money in, a loan from a relative, an advance against wages: money moves, nothing
is bought or sold, no invoice exists.

Modelling them as trade documents would put them in **revenue and cost reports where they are
neither**. A business whose "sales" included the owner's own capital would show a month it never
had — and the mistake is invisible, because the totals all add up. They are just about the wrong
thing.

### D2 — a closed set of kinds, so mappings and not 7.1's exception

Expense categories are open-ended — forty of them, each with its own account — which is why 7.1
introduced `document:accounts`. Debt kinds are **three**, and three can be mappings.

Using the exception here would be *naming accounts in Go with extra steps*, which is exactly the
boundary 7.1 drew around it. The exception stays narrow because the second thing that could have
used it did not.

### D3 — direction and method name the ACTION; the kind is an AMOUNT

Putting the kind in the action too would give **twenty-four rules where eight say the same thing**.
So the action is `debts.{received|paid}.{method}` and each rule carries three obligation lines of
which exactly one is ever non-zero — the same shape as the variance split (6.3) and cash
short/over (5.6).

### D4 — a sign belongs in a POSITION, never in a document

The documents keep positive amounts and a `direction` column, because a signed amount makes
`SUM(amount)` meaningless and every query a minefield — the discipline stock movements and payments
already keep.

`DebtPositions` is the one place a sign is right: "we owe 2,500" and "we are owed 300" are the two
answers, and a reader wants one number per kind.

### D5 — why these live in the expenses module

**Not because they are expenses.** Because a module is a unit of ownership, and a separate `debts`
module owning two small tables would earn a migration range, a permission set, a service, a
binding façade, and a place in three registration lists — in exchange for keeping apart two things
only ever used together.

§10 draws module boundaries around what changes together. If debts grow a life of their own —
schedules, interest, terms — they earn their own module then, and the tables move with a migration
rather than being split now on a guess.

### D6 — recorded and posted in one call

An expense is entered, checked, and approved, often by different people — which is why drafting and
posting are separate grants. A debt is a **single act somebody performs with the money in their
hand**: the owner takes 4,000 out of the till, and there is no intermediate state in which that has
half-happened.

The permission is deliberately its own: money moving with no trade document behind it is the shape
every misappropriation takes, and *"the owner drew 40,000"* is a sentence somebody should have had
to be authorised to write.

**Mutation drills — 6 run, 0 passed.**

| # | Mutation | Result |
|---|---|---|
| 133 | The posting action ignores the direction | 2 tests fail |
| 134 | The posting action ignores the method | 2 subtests fail |
| 135 | Every debt posts as a loan payable | 3 subtests fail |
| 136 | An unknown obligation kind is accepted | fails |
| 137 | The net position ignores direction | fails |
| 138 | The rules drop the owner-equity line | 2 tests fail |

---

## Step 7.4 — partner balances, statements, and the verifier

**Delivered.** `BalanceOf`, `StatementFor`, `Age`, `VerifyBalances`, `PartnerControlBalance`, and
the three composition-root adapters that let a module compute something spanning three others it
may not import.

### D1 — three ports, satisfied where the graph is assembled

A partner's position spans sales, purchasing and expenses. No module owns it and none may import
another, so each contributes through a port: `Receivables`, `Payables`, `ControlLedger`.

The ports are about MONEY, not documents. This module never learns what an invoice is — it asks
*"what does this partner owe, and for what"*, and every answer has the same shape whatever
produced it.

### D2 — the ports are attached AFTER construction

A balance needs sales, purchasing and expenses; every one of those needs partner, to check credit
limits and name a counterparty. Passing the ports into `NewService` would need all four built
before any of them.

The cycle is broken where cycles always are: the module is built able to do less and gains the
rest once its neighbours exist. Nothing between the two points can ask for a balance, because the
composition root is the only caller and does both in one function.

### D3 — invariant 5 is a REPORT, not a repair

§7.13: *"Partner balances equal the sum of their open documents minus settlements."*

The subsidiary ledger and the control account are computed by **completely different paths** — one
sums documents and allocations, the other sums journal lines written by posting rules. When they
disagree, one is wrong in a way nothing else will surface: an invoice posted against the wrong
partner, a payment allocated across companies, a rule edited after the fact. Both sides are
internally consistent; **only comparing them finds it**.

It reports and does not repair, for 4.3's reason about stock.

### D4 — a net figure, with both halves kept

Netting them away hides the case that matters most: a partner who is both customer and supplier,
owing 5,000 and owed 4,900, **is not the same risk** as one who simply owes 100.

### D5 — ageing is as at a DATE

An analysis printed for a month end must say what it said at that month end. One that quietly
re-ages itself on reopening is a report nobody can file.

**Mutation drills — 6 run, 3 passed.** All resolved.

| # | Mutation | Result |
|---|---|---|
| 139 | Everything is aged as current | 3 tests fail |
| 140 | An undated debt is never due | **Passed → test strengthened** |
| 141 | A verifier with no control ledger reports success | fails |
| 142 | A balance with no ledgers answers zero | **Passed → test strengthened** |
| 143 | Draft journal entries count toward a control balance | **Passed → tests were SKIPPING** |
| 144 | The verifier never compares receivables | **Passed → drift test written** |

### The worst finding of this step: two tests that skipped

D143 kept passing after several attempts to make it fail. The cause was not the mutation —
**both balance tests were calling `t.Skipf` on every run**, because the bootstrap fixture has no
provisioned company and I had written the skip as a convenience.

They were green. They asserted nothing. That is worse than a missing test, because a skip occupies
the place where a real check would go and the suite counts it as coverage.

A test about partner balances *needs* a provisioned company. If provisioning breaks, it should fail
loudly — so the skip became a fixture that provisions, and if that fails the test fails.

### Two more tests that passed for the wrong reason

- **D140** used an undated item from January. An empty due date sorts before every real date, so it
  lands in "older" whether or not the fallback exists. The fallback is load-bearing for a *recent*
  item: an invoice raised five days ago with no terms is five days over, not a year over.
- **D142** asserted only that *an* error came back — and one did, because the partner identifier
  was invented and the lookup failed first. It now uses a real partner and names the code.
- **D144** tested the verifier only on a clean company, where deleting the comparison changes
  nothing. It now introduces real drift.

**Four of six drills in this step found a test that could not fail.** That is the highest
proportion in the project, and the reason is worth recording: this step's subject is *a check*, and
tests of checks are unusually easy to write in a form that never exercises the failing case.

---

## Step 7.5 — bindings and screens

**Delivered.** The `Expenses` façade (13 methods), partner statements and the balance verifier on
`Partners`, the typed TypeScript surface, three screens, and nine frontend tests.

### D1 — three grants, and the third is the strict one

Recording an expense, paying one, and recording a **debt** are separate permissions. Money moving
with no trade document behind it is the shape every misappropriation takes, and *"the owner drew
40,000"* is a sentence somebody should have had to be authorised to write.

### D2 — the ageing date comes from the OPERATOR

`Statement(partnerID, asAt)` takes the date; empty means today, decided in Go. A statement printed
for a month end must say what it said at that month end, and the frontend never bakes a date in.

### D3 — outstanding is absent, not zero, on an expense paid when recorded

`"0.00"` reads as *a debt that was settled*. An expense paid on the spot owes nothing and never
did, which is a different fact — so the field is blank and the screen shows a dash.

### D4 — every debt position is shown, including the empty ones

A screen listing only the kinds with activity would silently change shape as a business used it,
and *"the owner has taken nothing out"* is an answer somebody wants to see stated. The binding
returns all three in a fixed order.

### D5 — receivable and payable stay apart on a statement

A statement sent to a customer shows what they owe; one sent to a supplier shows what is owed to
them. Merging them into one signed list produces a document nobody can send to either — and the
same partner is frequently both.

Both halves sit beside the net, because netting them away hides the case that matters most: owing
5,000 and being owed 4,900 **is not the same risk** as owing 100.

**Mutation drills — 3 run, 0 passed** (one bad mutation, redone).

| # | Mutation | Result |
|---|---|---|
| 145 | The expenses façade missing from `All()` | fails |
| 146 | The façade never attached | fails |
| 147 | Recording a debt has no declared policy | fails |

The two structural checks written in 5.8 and 6.8 caught the first two immediately, which is what a
structural test is for: **the third and fourth façade to be added cost nothing to get right.**
