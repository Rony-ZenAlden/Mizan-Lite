# Phase 2 — The Financial Spine (Design)

> Status: **DESIGN — implemented step by step under the standing autonomous mandate.**
> Scope: the accounting module (chart of accounts, journal entries, fiscal period control,
> balances, table-driven posting rules) and the tax module (jurisdictions, versioned rates,
> groups, exemptions, resolution, calculation).
> **Out of scope:** anything that *produces* a transaction — sales, purchasing, inventory. This
> phase builds the ledger those phases will post into.

---

## 1. ANALYSIS

### 1.1 What this phase is for

§20 opens with the sentence that defines it: accounting is *"built completely from day one,
exposed progressively in the UI"*. Every transaction the product will ever record keeps perfect
double-entry books from the first release, and the UI reveals them over five releases (§20.6)
with **no schema change at any step**.

That is only achievable if the ledger exists before the first invoice does. This is that phase.

### 1.2 The two rules that shape everything

**Debits equal credits, always.** §20.2 calls it *"the one inviolable rule"*. Enforced in the
domain aggregate before persistence, re-verified by the posting service, and checked by a
periodic job. An unbalanced entry cannot be saved — not "should not".

**A posted entry is never modified or deleted.** Corrections are *reversing entries* linked by
`reversal_of_entry_id`. This is what makes a general ledger legally defensible, and it is the
same append-only reasoning the audit trail already follows (§15.1).

### 1.3 Posting is data, not code — the phase's central decision

§20.3 is explicit and it is the most important design call in the phase:

> The naive approach hardcodes "when an invoice is posted, debit AR, credit revenue, credit
> tax." That makes the accounting untestable, un-configurable, and country-specific — and it
> would violate your configuration-over-code principle at the most important layer.

So the accounting module **subscribes to domain events** and evaluates table-driven
`posting_rules` against them. The payoff §20.3 lists is real and worth restating: sales code
will contain **zero** accounting logic; different countries and trades get different books from
seed data; and a mis-mapped account becomes a data fix rather than a patch release.

It also means Phase 5 (Sales) can be written without an accountant in the room.

### 1.4 Tax ships empty — D1 (directive)

§19.5 already designs `tax.enabled = false` as a first-class state: the UI loses tax, every
document resolves to zero, and no tax postings are made. The tables and code paths remain, so
enabling it later needs **no migration**.

v1 ships exactly that: the full tax engine, and **not one rate**. §C.3 forbids asserting
jurisdictional facts, and the directive confirms the permitted option — an empty,
user-configured profile.

This is not a stub. The engine is complete and tested; the *data* is the customer's to supply.

### 1.5 The risk this phase actually carries

Not complexity — **silent wrongness**. A bug in an ERP's identity module produces a login
failure somebody reports. A bug in its ledger produces books that balance and are wrong, and
nobody notices for a year.

Everything below is shaped by that: the invariant is checked in three places, balances are
maintained incrementally *and* rebuildable from source with an equality assertion, and every
posting records why it posted what it did.

### 1.6 Risks

| Risk | Consequence | Answer |
|---|---|---|
| An unbalanced entry reaches the database | The books are wrong and still look right | Domain aggregate + posting service + integrity job (§2.2) |
| A posted entry is edited | The ledger stops being defensible | No update path exists; corrections reverse (§2.2) |
| Incremental balances drift from the ledger | Reports disagree with the general ledger | Rebuild recomputes from `journal_lines` and asserts equality (§2.5) |
| Posting into a closed period | Restated accounts after a period was signed off | Rejected at the service, with the period status as the reason (§2.4) |
| A rate edited in place | Historical invoices silently restate | `tax_versions` — rates are versioned, never edited (§3.1) |
| Rounding scattered across call sites | Line taxes that do not sum to the document tax | One domain service, largest-remainder allocation (§3.3) |
| Accounting logic leaking into sales | Country-specific behaviour welded into a use case | Posting rules are data; sales emits an event (§2.6) |

---

## 2. DESIGN — the accounting module

### 2.1 Chart of accounts

§20.1's shape, with the hierarchy carried by a materialised `path` for subtree aggregation.

Two constraints do real work:

- **Only leaf accounts are postable.** `is_postable` is enforced when a line is written, not
  merely displayed — a posting to a roll-up account makes every parent total double-count.
- **System accounts cannot be deleted.** AR, AP, inventory, COGS, tax payable, FX gain/loss,
  rounding difference, retained earnings, opening balance. The mapping layer references them by
  key, so deleting one would break posting for every future document.

**Templates are seed files** (`seeds/chart_of_accounts/*.json`), loaded through the layered
loader Step 1.8 built. That loader already does discovery, ordering, shipped-is-fatal /
user-is-skipped, and strict decoding — a chart of accounts is exactly the file-shaped,
customer-extensible data it was written for, and this is its second consumer.

### 2.2 Journal entries — the aggregate

```
JournalEntry
  ├─ header: number, date, period, status, source, branch, memo
  └─ lines[]: account, debit XOR credit, currency + original amount + rate, dimensions
```

The balance rule lives in the **domain**, in a constructor that refuses to return an unbalanced
entry. Not a validator someone can forget to call: the aggregate cannot be constructed in an
invalid state, which is the same shape as `domain.NewSession` and `money.Money`.

Three enforcement points, deliberately:

1. the aggregate, at construction;
2. the posting service, before it writes — because a repository is a separate trust boundary;
3. an integrity job, nightly — because the first two only protect the paths that go through
   them, and a restore or a manual database edit does not.

### 2.3 Currency on a line

Every line carries functional-currency `debit_minor`/`credit_minor` **and** the transaction
currency with its original amount and rate (§18.1). The ledger balances in the functional
currency; the original is what an auditor asks about.

FX differences post to the system `FX_GAIN_LOSS` account through the mapping layer, like
everything else.

### 2.4 Fiscal periods

Already in the schema. Step 1.1 created `fiscal_years` and `fiscal_periods` (0.4 D4's precedent:
build the table with its owner, not with its first reader), and the setup wizard already
generates a year with twelve periods.

This phase adds the *control*: `open | closed | locked`, and posting into anything but `open` is
rejected with a typed code naming the period. Year-end closing generates the closing entries
(revenue and expense → retained earnings) as a normal, reversible journal entry — not a special
case, because a special case is one nobody can reverse.

### 2.5 Balances

`account_balances` per (account, period, branch) with opening/debit/credit/closing, maintained
incrementally as entries post. Trial balance and statements read this table, never the full
ledger.

The rebuild job is the safety net §20.5 asks for: recompute from `journal_lines` and **assert
equality**. Cheap, and it converts "the reports look odd" into a specific failing number.

### 2.6 Posting rules

```
posting_rules       event_type, business_profile, sequence, active
posting_rule_lines  side, account_selector, amount_selector, condition, dimension_map
account_mappings    mapping_key → account, per branch / business profile
```

The accounting module subscribes to `Postable` domain events — the same shape as 1.7's
`Auditable`, and for the same reason: the publisher depends on an event type, never on the
accounting service.

**Selectors are a tiny expression language, not Go.** `mapping:AR`, `account:4100`,
`document.total`, `line.tax`. Deliberately small: anything powerful enough to need a parser is
powerful enough to hide a bug in a customer's books.

---

## 3. DESIGN — the tax module

### 3.1 Rates are versioned, never edited

`tax_versions(tax_id, rate, effective_from, effective_to)`. When VAT moves 15% → 16%, a version
is added. Historical invoices keep resolving the old rate.

§19.2 states the alternative plainly: *"editing a rate in place would silently falsify past
documents."* That is the failure mode §1.5 describes — books that balance and are wrong.

### 3.2 Resolution is one function

§19.3's eight-level priority, top to bottom, in one testable function. The resolved group, each
tax, its rate, its base, **and the reason for the resolution** are stored on the document at
posting time — so a tax authority can be shown why an amount was charged, years later.

### 3.3 Calculation

One domain service. Tax-inclusive prices are decomposed with exact integer arithmetic and
largest-remainder allocation, so line taxes sum exactly to the document tax. `money.Allocate`
already implements largest-remainder (0.2) and is the third consumer of it.

The rounding *stage* is configurable — line-level versus document-level produce legally
different totals in different jurisdictions, which is precisely the sort of fact this product
must not hardcode.

---

## 4. STEP SEQUENCE

| Step | Deliverable |
|---|---|
| **2.1** | Accounting module: chart of accounts, seed-file templates, system accounts |
| **2.2** | Journal entries: the aggregate, the balance invariant, the posting service |
| **2.3** | Account balances, incremental maintenance, the rebuild-and-assert job |
| **2.4** | Fiscal period close + year-end closing |
| **2.5** | Posting rules, account mappings, the `Postable` event and its subscriber |
| **2.6** | Tax module: schema, versioned rates, groups, exemptions |
| **2.7** | Tax resolution + calculation, and `tax.enabled = false` as a real state |
| **2.8** | Read-only UI: chart of accounts + trial balance (§20.6's v1.1 tier) |
| **2.9** | Phase 2 Definition-of-Done review |

---

## 5. DEFINITION OF DONE

1. An unbalanced journal entry **cannot be constructed**, cannot be posted, and is caught by the
   integrity job if it somehow exists.
2. A posted entry has no update or delete path; a correction produces a linked reversing entry.
3. Posting into a closed or locked period is rejected, naming the period.
4. Every posting is produced by a **rule row**, not by Go code, and a rule change alters the
   books with no code change.
5. `account_balances` rebuilt from `journal_lines` equals the incrementally-maintained values,
   asserted by a job.
6. A chart of accounts loads from a **seed file**, and a customer's own file overrides it.
7. A tax rate change adds a version; historical documents resolve the old rate.
8. `tax.enabled = false` produces zero tax, no tax postings, and no tax UI — with no migration
   needed to turn it back on.
9. The eight-level resolution priority is table-tested, and the reason is stored on the document.
10. Line taxes sum **exactly** to the document tax, at both rounding stages.
11. Every new binding method has a declared policy (the 1.5 gate), and every audited action
    writes its entry in-transaction (the 1.7 gate).
12. `make ci` green, with the mutation drills each step declares.

---

*Implementation proceeds step by step; each step records its own decisions and drills.*

---

## 6. STEP RECORDS

### Step 2.1 — Chart of accounts ✅

**Built:** migration 0010 (`accounts`, `account_mappings`), `domain/account.go`, the chart
template loader, `generic_trading.json`, the service, the repository, and wiring into the setup
wizard so a fresh install gets a ledger.

**Decisions taken**

1. **The chart is a seed FILE, on 1.8's layered loader.** Second consumer of that loader, and
   the first outside its original purpose — an accountant's own chart in the data directory
   replaces the shipped template with no release. A default chart in a migration would be an
   accounting opinion welded into schema.
2. **`normal_balance` is stored although it is derivable.** Storing a derivable value is
   normally a smell; here it is the point. Re-deriving it in SQL, in Go, in a report, and in an
   export is four chances to disagree about whether an expense is a debit.
3. **A parent stops accepting postings the moment it gains a child**, written immediately rather
   than collected and flushed — so the invariant holds at every point inside the transaction,
   not merely at its edge.
4. **Required mappings are validated at LOAD.** A chart with no receivables account otherwise
   fails when a customer tries to sell something, months later.
5. **`ApplyChart` refuses a second chart.** Two overlapping hierarchies and a mapping layer
   pointing at whichever won is not a state anything could untangle.
6. **The wizard applies the chart with no picker.** One template ships; asking a question with a
   single answer teaches users that the wizard wastes their time. The picker arrives with the
   second chart.

**Mutation drills**

- *A parent keeps accepting postings after it gains a child* → `TestOnlyLeafAccountsArePostable`
  failed on six accounts. The leaf-only rule is watched.
- *A chart may omit a required role* → `TestAChartMissingARequiredMappingIsRefused` failed.

**Carried:** `CreateAccount` (extending a chart after setup) has no caller yet and is not built
— it arrives with the screen that needs it, per the rule this project has repeatedly earned.

### Step 2.2 — Journal entries, the balance invariant, the posting service ✅

**Built:** migration 0011 (`journal_entries`, `journal_lines`), `domain/journal.go` (the
aggregate), the journal repository, and `posting.go` — `Post`, `Reverse`, `SetPeriodStatus`,
`CheckIntegrity`.

**Decisions taken**

1. **The balance rule is a CONSTRUCTOR, not a validator.** `domain.NewEntry` has no valid state
   in which debits and credits differ — the same guarantee `money.Money` gives about currencies.
   A validator is something a caller can forget to run.
2. **Three guards, and they are not redundant.** The aggregate protects what goes through it;
   the posting service is the last refusal before rows exist, and is the only one left the day
   somebody builds an `Entry` literal; `CheckIntegrity` sees entries that met neither — a
   restore, a repair script, a future importer.
3. **The repository offers no update and no delete for a posted entry.** `MarkReversed` is the
   one status change. Append-only enforced by absence, as in the audit module (1.6), and
   asserted on the service surface so adding one is a deliberate act.
4. **A reversal is dated on its own date.** Reversing a March entry in May is a May event;
   back-dating would silently restate a month that may already be closed.
5. **The period is resolved from the entry's DATE, not from today.** Posting into a closed or
   locked period is refused, naming it.
6. **Entry numbers are `MAX+1` inside the transaction.** Safe because SQLite has exactly one
   writer (0.3). On an engine with real concurrency this becomes §9.4's `number_series` row
   taken with a locking read — which arrives with the documents that need gapless numbering.
7. **Every analytical dimension exists from the first release**, although nothing populates most
   of them. Adding a dimension to a ledger with three years of history means those three years
   cannot be analysed by it, and that is not recoverable.

**Mutation drills**

- *Balance rule not enforced at construction* → `TestAnUnbalancedEntryCannotBeConstructed`
  failed.
- *A closed period accepts postings* → `TestPostingIntoAClosedPeriodIsRefused` failed with
  *"a signed-off month was restated"*.

**Also proved:** the integrity check catches corruption applied directly with SQL — the case the
other two guards structurally cannot see.

### Step 2.3 — Account balances and the integrity job ✅

> **Re-ordered.** The plan had period close at 2.3 and balances at 2.4. Year-end closing needs
> account totals to compute what moves to retained earnings, so the dependency runs the other
> way. Swapped rather than worked around.

**Built:** migration 0012 (`account_balances`), incremental maintenance inside the posting
transaction, the trial balance, `RebuildAndVerifyBalances`, and the nightly
`accounting.ledger_integrity` job.

**Decisions taken**

1. **Movement only — a deviation from §20.5, argued.** §20.5 lists "opening/debit/credit/
   closing"; this stores only the period's debit and credit and derives the rest. An opening
   balance is exactly the sum of earlier movements: storing it too means two facts that must
   agree, maintained by different paths, and the day they disagree the reports are wrong and
   still add up. **Storing less means less can drift.** The cost is a running sum bounded by the
   calendar, not by trade volume.
2. **Balances are maintained INSIDE the posting transaction.** Written afterwards they would
   drift the moment a posting rolled back — the failure the rebuild job exists to catch, and a
   far better one to prevent.
3. **Debit-positive throughout.** A liability with a credit balance reads negative; the sign is
   converted once at the presentation edge from `normal_balance`. One convention, so a report
   cannot show revenue upside-down. A correct trial balance therefore sums to **zero**.
4. **An UPSERT, not read-modify-write.** SQLite's single writer makes the race impossible today;
   the portable form costs nothing and survives the move to an engine where it is not (§8).
5. **The job REPORTS rather than repairs.** A rebuild that silently corrects means a bug in the
   incremental path is fixed nightly and reported never — books wrong for exactly one day at a
   time, forever. It compares first, rebuilds second, and fails with the offending entry
   numbers.
6. **Two balance rows per branched line** — the aggregate and the branch's own — so a branch P&L
   uses the same query shape rather than a sum over branches that would omit unbranched lines.

**Mutation drills**

- *The rebuild repairs silently instead of reporting* → `TestRebuildingReportsDriftRatherThanSilentlyFixingIt`
  failed: *"a bug in the incremental path would never be reported"*.
- *Balances are not maintained on post* → the trial balance and carry-forward tests both failed
  with every total at zero.
