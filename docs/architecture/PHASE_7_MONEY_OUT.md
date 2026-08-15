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
