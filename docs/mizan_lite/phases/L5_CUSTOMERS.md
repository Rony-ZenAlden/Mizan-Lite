# Mizan Lite — Phase L5: customers and debts (العملاء والديون)

> **Status: COMPLETE — committed `97b7875` (2026-09-14).** The owner approved this note, its 18 decisions and five amendments, answered
> §14.2 (recorded in §15), and approved the twelve decisions made while building (§17). §16–§22 are the implementation record.
> **Date:** 2026-09-14. **Base:** L4 (`76463fd`). **Design:** [../DESIGN.md](../DESIGN.md) §3.3 (debt repayment), §4.5
> step 7, §4.7, §5 `customers` and `debt_entries`, C7, §10 (L5), with §13's amendments — Q2 (debts in USD **or** SYP,
> chosen per sale), Q7 (the owner PIN). Prior phases: [L4_TILL.md](L4_TILL.md) A-L4.2 (the customer of a credit sale lives
> on the debt entry), §2.3 (L5's scope), D-L4.i1 (a void counts on its own day); [L3_RATES.md](L3_RATES.md) (the rate a
> repayment converts at); PROGRESS O7 (nullable comparisons in CHECKs).

---

## 0. How to read this

| § | Content |
|---|---|
| 1 | Analysis — what L5 must make possible, and ten things harder than they look |
| 2 | Scope — in, out, the phases after, and five amendments to the approved design |
| 3 | The customer |
| 4 | **The debt ledger** — one chain per customer and currency, balances, "owed since" |
| 5 | **Credit sales at the till** — the debt currency, paid-now, the charge, the receipt, the void |
| 6 | **Repayments** — in the same or the other currency, pay-all, change, the rate |
| 7 | The owner's acts — opening balances, write-offs, refunds, reversals — and who may do what |
| 8 | The schema — `0006_customers.sql`, verified on a copy of the seeded shop |
| 9 | The verifiers — the debt ledger's own, and what the sales verifier learns |
| 10 | Architecture — the `customers` module, its ports, and a shared `tender` package |
| 11 | Bindings and screens |
| 12 | Demo data |
| 13 | Tests, drills, Definition of Done |
| 14 | Decisions and questions for approval |
| 15 | **At approval** — the owner's answers |
| 16–22 | **Implementation record** — built, decisions while building, findings, evidence, drills, not verified, done |

---

## 1. ANALYSIS

### 1.1 What L5 must make possible

A pantry shop sells on credit to people it knows — *اكتبها على الحساب* — and keeps a paper book. L5 replaces the book:

1. **Sell on credit** at the till, to a named customer, with or without part paid now — the owner's brief: *Cash (SYP),
   Cash (USD), or Deferred / Credit attached to a customer profile*.
2. **Know what each customer owes, in each currency, and since when** — without ever adding dollars to pounds.
3. **Take a repayment** in either currency, at any time, whole or in part, and give change.
4. **Adopt the paper book**: enter what each customer already owed before Lite.
5. **Correct and close**: reverse a mistaken entry, forgive a debt that will never be paid, refund a customer the shop owes.
6. **Show a statement** a customer can be read out: every charge and payment, with the balance after each.

### 1.2 Ten things harder than they look

**H1 — A debt in pounds loses value while it is owed (DESIGN C7).** 16.25 USD of goods is 243,750 pounds at 15,000. If the
debt is recorded in pounds and repaid when the dollar is 18,000, the shop receives 243,750 pounds — **13.54 USD**, 2.71 USD
lost. Recorded in dollars, the same customer repays 292,500 pounds. Neither choice is technical; Q2 said *chosen per sale*,
and §5.1 makes the choice one tap at the till. §14.2 Q-L5.1 asks which way the tap defaults.

**H2 — Balances in two currencies must never become one number by accident.** A customer owing 9.58 USD and 150,000 pounds
owes both. A "total" column silently converted at today's rate is a figure that changes every morning with no payment made.
Every balance is kept, stored and shown **per currency** (§4); a converted figure appears only as a labelled reference
with its rate (Q-L5.8).

**H3 — The last cent.** A dollar debt of 9.58 settled in pounds at 15,000 is 143,700 pounds, and the shop hands over no
note smaller than 500. Asking for 143,500 leaves 0.0133 USD owed forever; asking for 144,000 overcharges. §6.3's *pay all*
settles the balance exactly and records the pounds actually taken — the gap is at most half a note, the same bound a
cash sale's rounding already has (D-L4.3).

**H4 — Voiding a credit sale that was partly repaid.** The sale's charge is reversed; the payment stands (cash really came
in). The balance goes **below zero** — the shop now owes the customer. That state must be representable, visible, and
closable (§7.3, Q-L5.6), and a later credit sale must use it up naturally.

**H5 — One transaction across three modules.** A credit sale writes the sale, its stock movements **and** the customer's
charge. A failure in the charge must leave no sale and no stock movement — the atomicity L4 proved for two modules, now
for three (§5.4).

**H6 — The order of a customer's entries cannot come from the clock.** L2 and L3 both learned it: a PC clock set back
reorders a ledger. Entries carry a **place** — `seq` per customer and currency — and each records its balance before and
after, so the chain is checkable row by row (§4.2).

**H7 — "A credit sale without a customer is unrepresentable" (DESIGN §4.5) can no longer be a CHECK.** A-L4.2 moved the
customer off `sales` and onto the debt entry, because rebuilding `sales` — referenced by its lines, the stock ledger and now
the debts — is the most expensive rebuild in the schema. The invariant becomes: every `credit` sale has **exactly one**
charge (a UNIQUE index guarantees at most one; the checkout transaction writes it; the sales verifier finds a missing one).
Amended honestly in A-L5.1 rather than claimed.

**H8 — The sales verifier already written treats every sale as cash.** L4's change check expects `change ≈ tender − total`.
A credit sale with 50,000 paid on a 73,000 total would be reported as wrong the first time one is written — including by
the seeder, which fails on a finding. The verifier must learn credit **in the same change** that allows it (§9.2).

**H9 — Two customers called أبو محمد.** A shop's customers are known by nicknames; the till must never charge the wrong
one. Names are unique after normalisation (the catalogue's `textkey`), the picker shows phone and balances, and the charge
snapshots the name it was given to (§3).

**H10 — Nullable comparisons in CHECKs pass on NULL** (PROGRESS O7). DESIGN §5's `ck_debt_tender_complete` compares
`tendered_minor > 0` on a nullable column: a payment with a NULL amount and a rate passes it. Every nullable comparison in
§8 is guarded with `IS NOT NULL`, and the NULL cases were refused on real SQLite before this note was written.

---

## 2. SCOPE

### 2.1 In L5

| Area | What |
|---|---|
| Customers | create, rename, phone, note, deactivate; search by name or phone; unique names after normalisation |
| The debt ledger | one chain per customer and currency: `opening`, `charge`, `payment`, `write_off`, `refund`, `reversal`; balance before and after each; insert-only |
| Credit sales | at the till: *Cash* or *On credit*; a customer (picked or created on the spot); the debt currency is the currency the sale is charged in; optional *paid now* in either currency; the charge in the checkout transaction; the receipt shows the customer, what was added and the balance after |
| Voiding a credit sale | reverses its charge in the void's transaction |
| Repayments | in either currency at the rate in force; an amount handed over, or *pay all*; change; a quote and a token, as at the till |
| The owner's acts | opening balances from the paper book, write-offs, refunds of a balance below zero, reversal of a mistaken entry — each with the PIN and a reason |
| Reading | who owes what (per currency, owed since, last payment); a customer's statement per currency; the day's credit and payments per currency |
| Verifiers | the debt ledger's chain; the sales verifier learns credit sales and their charges |
| Screens | **Customers** (list, who owes, a customer's statement and actions), the till's credit payment, the receipt's credit block, the Sales screen's *on credit* totals |
| Demo data | eight customers from a paper book, credit sales, repayments both ways, pay-all in pounds, a voided repaid credit sale refunded, a write-off |

### 2.2 Not in L5

| Not here | Why | Where |
|---|---|---|
| Interest, late fees, instalment plans, due dates | none was asked for; a due date without reminders is a field nobody reads | — |
| Credit limits that **refuse** a sale | Q-L5.3 recommends a visible balance instead | — |
| Printing or sending a statement (WhatsApp, SMS) | printing is L7's path; sending needs a network feature nobody reviewed | L7 (printing) |
| Advance payments with no debt (a deposit) | a balance below zero arises only from a void (H4); a deposit is a different promise | — |
| Supplier debts | DESIGN §3.4 | — |
| Profit effect of write-offs | L6 reports them beside profit | L6 |
| The drawer's cash summary including repayments | L6's cash summary per day and currency | L6 |

### 2.3 What remains after L5

| Phase | Covers |
|---|---|
| **L6 — Profit and cash** | daily, monthly, per-product profit in USD and pounds at each sale's rate; expected profit on the shelf (Q3); the cash summary per day and currency — cash sales, **repayments, refunds**, change; **write-offs reported as bad debt**; owner mode only |
| **L7 — Receipts and backups on screen** | printing receipts **and statements**, 80 mm thermal (Q-L4.9); a backups screen |
| **L8 — Release** | installers, the first Windows run (O1), low-end hardware measurements, a pilot shop |

### 2.4 Five amendments to the approved design

**A-L5.1 — "a credit sale without a customer is unrepresentable" becomes "every credit sale has exactly one charge".**
DESIGN §4.5's CHECK cannot exist since A-L4.2 (H7). Enforced by the checkout transaction, a UNIQUE index on the charge's
`sale_id`, and the sales verifier.

**A-L5.2 — the debt currency of a credit sale is the currency the sale is charged in.** Q2's *chosen per sale* is the
till's existing *charge in pounds / dollars* choice (L4 §3.2), not a second currency field: a sale charged in dollars owes
dollars, a sale charged in pounds owes pounds. No conversion between what the receipt says and what the book says. The
default when *On credit* is chosen is Q-L5.1.

**A-L5.3 — `debt_entries` gains a place, a balance chain, a customer-name snapshot and a change; `charge_void` becomes
`reversal`.** DESIGN §5 ordered entries by `occurred_at` and stored no balance. L2 and L3 showed why a ledger needs `seq`
and before/after figures (H6); the receipt needs the name the customer was charged under (D-L4.13); a repayment gives
change like a sale. One `reversal` kind naming the entry it reverses (`reverses_id`, UNIQUE) replaces a `_void` kind per
kind, and `write_off` and `refund` join (§7).

**A-L5.4 — no `debt.default_currency` enum of `local`/`usd`** (DESIGN §6.3 sketch): the setting stores the currency code
(`USD` or the local code), as `currency.local` does, so the redenomination (C8) changes no stored value.

**A-L5.5 — every movement of money snapshots the rate in force; no rate, no payment.** DESIGN §4.7 stored the rate only
for cross-currency payments. A same-currency payment then has no rate, which makes the CHECK conditional and L6's reports
of repayments in the other currency impossible. The till already refuses to sell without a rate (D-L4.6).

---

## 3. THE CUSTOMER

| Field | Rule |
|---|---|
| name | required, 1–100 characters, **unique after normalisation** (`textkey`: Arabic letter variants, diacritics, spaces — the catalogue's rule) so *أبو محمد* and *ابو محمد* are one name; a second person is *أبو محمد — الحلاق* |
| phone | optional; digits as typed in any script, stored in Latin (`numinput.LatinDigits`), up to 20 characters; searched as a substring |
| note | optional, up to 200 characters |
| active | an inactive customer is hidden from the till's picker and **cannot be charged**; can still repay, and keeps every entry |

**Who may:** anyone at the counter may create a customer and edit name, phone and note — a new customer at the till
cannot wait for the owner. Deactivating a customer **who owes money** needs the owner's PIN (it hides a debt from the
picker); deactivating one whose balances are all zero does not. Nothing is ever deleted.

**The till's picker** searches name and phone, shows each match's balances in both currencies, and offers *New customer*
(name and phone) without leaving the sale.

---

## 4. THE DEBT LEDGER

### 4.1 One chain per customer and currency

A customer has **up to two chains** — one in pounds, one in dollars — each with its own places 1, 2, 3 … and its own
balance. An entry belongs to exactly one chain. Nothing sums across chains.

| Kind | Sign | Written by | Cash | Reason |
|---|---|---|---|---|
| `opening` | + | the owner, from the paper book | — | optional note |
| `charge` | + | a credit sale's checkout — never by hand | — | — (names its sale) |
| `payment` | − | anyone, taking a repayment | tender and change, rate | — |
| `write_off` | − | the owner, forgiving a debt | — | **required** |
| `refund` | + | the owner, paying back a balance below zero | tender (paid out), rate | **required** |
| `reversal` | the opposite of the entry it reverses | the owner (opening, payment, write-off, refund) or a sale's void (charge) | — | **required** |

### 4.2 The chain

Every entry records `balance_before` and `balance_after = balance_before + amount` (a CHECK). Place 1 starts from zero;
place *n* starts from place *n − 1*'s after. The place is allocated `MAX(seq) + 1` inside the writing transaction
(single writer; a UNIQUE index backs it), and every read of a balance is **the newest place's `balance_after`** — never a
sum recomputed differently in two queries, never ordered by clock.

**Constraints on the chain (CHECKs, §8):** a payment or write-off never takes a balance below zero; a refund only moves
a balance that is below zero, and never past zero; a reversal may go either way (H4).

### 4.3 Owed since, last payment

Both come from the chain without a stored field:

- **Owed since** — the business date of the newest entry whose `balance_before ≤ 0` and `balance_after > 0`: the day the
  current debt started. A customer who paid down to zero last month and bought on credit yesterday is owed since
  yesterday.
- **Last payment** — the newest `payment` entry's business date.

"Who owes what" lists every customer with a non-zero balance in either currency: name, phone, the pounds balance, the
dollars balance, owed since, last payment — sortable by name or by owed since. **It never sorts by a combined amount.**

---

## 5. CREDIT SALES AT THE TILL

### 5.1 Choosing credit

The till's pay panel gains **Cash / On credit**. On credit:

1. A customer is **required** — picked or created (§3). An inactive customer cannot be charged.
2. The **charge-in** currency (L4's settlement) switches to the credit default (Q-L5.1) and stays changeable: the debt is
   in the currency the sale is charged in (A-L5.2).
3. The tender field becomes **paid now** — optional, in either currency. *Change* is hidden: paying the whole total is a
   cash sale, and the till says so rather than recording a credit sale with nothing owed.
4. The quote shows, all computed by Go: the total; paid now; **added to the debt**; the customer's balance in that currency
   **now** and **after** this sale.

### 5.2 The money

A credit sale's total is computed **exactly as a cash sale's** — the same lines, discounts and cash rounding (D-L4.2,
D-L4.3). One receipt total, whatever the payment. Then:

| Charged in | Paid now in | Added to the debt |
|---|---|---|
| pounds | nothing | the total |
| pounds | pounds | total − paid |
| pounds | dollars | total − paid × R, **rounded to the note**, half up |
| dollars | nothing | the total |
| dollars | dollars | total − paid, to the cent |
| dollars | pounds | total − paid ÷ R, to the cent, half up |

*R* is the sale's rate snapshot. A paid-now worth **at least** the total is refused (`lite.sales.credit_paid_in_full` —
"take it as cash"); a paid-now of zero is allowed. The conversion is the till's (L4 §3.3), moved to a shared package
(§10.2) so the sale and the repayment round identically.

Examples at 15,000, note 500: a 73,000-pound total with 50,000 paid now adds **23,000 SYP**; a 16.25-dollar total with
100,000 pounds paid now (6.6667 USD) adds **9.58 USD**.

### 5.3 What `sales` records

Nothing new in `sales` (A-L4.2 paid for this): `payment = 'credit'`; `tendered_*` is what was paid now (0, in the charged
currency, when nothing was); `change_minor = 0`. The customer, the debt currency and the amount added are on the charge
entry, which names the sale.

### 5.4 One transaction

`Checkout` (L4 §4) gains one step, inside the same transaction after the stock movements: **charge the customer** through
the till's `Debts` port. The charge re-reads the customer (active), allocates the place, snapshots the name and the balance
before, and writes the entry. Any failure — an inactive customer, a place taken, a CHECK — rolls back the sale, its lines,
its stock movements and its receipt number (H5, drilled in §13.2).

**The token** gains the payment kind, the customer id and the paid-now figures. It does **not** include the customer's
balance: a repayment arriving between quote and *Pay* changes what the customer owes, not what this sale charges, and the
receipt's *balance after* is computed inside the transaction.

### 5.5 The receipt

A credit sale's receipt adds a block, read from the charge entry (immutable, D-L4.13): *On credit — {name} · paid now
{paid} · added {amount} · balance {balance after}*, in the debt's currency.

### 5.6 Voiding a credit sale

L4's void (owner PIN, reason) writes, in its transaction, a **reversal** of the sale's charge with the void's reason. The
customer's balance falls by the charge — below zero if payments were made since (H4, §7.3). The void still counts on its
own day (D-L4.i1); the reversal carries that day's business date.

### 5.7 The day's sales

L4's per-currency totals gain **on credit**: the amounts added to debts by sales rung up that day. *Cash in* stays what the
drawer took — for a credit sale, what was paid now. Repayments are not sales; the Customers screen shows the day's
repayments, and L6 joins them into the cash summary.

---

## 6. REPAYMENTS

### 6.1 The rate

A repayment converts at the **rate in force when it is paid**, not the rate of the sale that made the debt. That is the
whole point of a dollar debt (H1): a customer who owes dollars repays their value today. A pounds debt repaid in dollars
likewise converts at today's rate. Q-L5.5 confirms it. With no rate, no repayment (A-L5.5).

### 6.2 An amount handed over

The cashier picks the debt (currency), the currency handed over, and the amount. Go computes:

- the **value** of the amount in the debt's currency — exactly, then rounded once: to the cent for a dollar debt, to the
  **pound** (the minor unit, not the note) for a pounds debt, half up;
- **settled** = the smaller of that value and the balance;
- **change** = what is left over, converted back and given in pounds unless both the debt and the tender are dollars
  (Q-L4.2's rule, the customer may ask otherwise), rounded to the note or the cent.

| Debt | Handed over (R = 15,000) | Settles | Balance after | Change |
|---|---|---|---|---|
| 16.25 USD | 100,000 SYP (6.6667 USD) | 6.67 USD | 9.58 USD | — |
| 9.58 USD | 20 USD | 9.58 USD | 0 | 10.42 USD |
| 250,000 SYP | 10 USD (150,000 SYP) | 150,000 SYP | 100,000 SYP | — |
| 100,000 SYP | 10 USD (150,000 SYP) | 100,000 SYP | 0 | 50,000 SYP |

### 6.3 Pay all

*Pay all* asks Go what to take to clear the balance in the chosen currency: the balance itself in the same currency; in the
other, the converted balance **rounded to the note** (pounds) or the cent (dollars). The entry settles **exactly the
balance**, and records the pounds or dollars actually taken:

| Debt | Pay all in | Asks for | Settles | Balance after |
|---|---|---|---|---|
| 9.58 USD | pounds | 143,700 → **143,500 SYP** | 9.58 USD | **0** |
| 100,000 SYP | dollars | **6.67 USD** | 100,000 SYP | **0** |

The gap between what was taken and the exact value — here 200 pounds, 0.0133 USD — is at most half a note or half a cent,
the bound the verifier holds (§9.1). This is H3's answer: no debt is left at a fraction of a cent, and no customer is
asked for a note the shop cannot change.

### 6.4 Quote and token

A repayment is quoted before it is recorded, as a sale is: the quote carries a token over the rate id, the customer, the
currency, the balance's place (`seq`), the tender and *pay all*. Recording recomputes it and refuses a stale token — so a
rate changed, or another payment taken on the same debt, between the screen and the click is caught (`lite.customers.
payment_stale`). Nothing is guarded: money coming in needs no PIN.

---

## 7. THE OWNER'S ACTS, AND WHO MAY DO WHAT

### 7.1 Opening balances

*From the paper book*: a customer, a currency, an amount, an optional note (*page 12*). Owner PIN — it creates a debt from
nothing. Allowed at any time and more than once (a shop finds another page); a wrong one is reversed, not edited.
Opening balances are not sales and never reach profit (DESIGN §5 notes).

### 7.2 Write-offs

*The customer left the country.* An amount up to the balance, with a required reason. Owner PIN. L6 reports write-offs as
bad debt beside profit. Q-L5.7 asks whether the shop wants this at all.

### 7.3 Balances below zero, and refunds

A balance below zero arises only from a reversal (H4). It is shown in the customer's favour — *الزبون له 50,000* — and:

- **used up by the next credit sale** naturally: the charge adds to −50,000;
- or **refunded**: the owner pays it out in either currency, with the PIN and a reason; the entry records what was paid out
  and never takes the balance past zero.

A payment cannot take a balance below zero (a CHECK): an overpayment is change (§6.2). Q-L5.6 asks whether the shop would
rather refuse a void until the customer has been refunded.

### 7.4 Reversals

Any opening, payment, write-off or refund may be reversed **once**, with the PIN and a reason — not only the newest (unlike
L2's receipts, a balance has no average cost to unwind). A **charge** is reversed only by voiding its sale, so a sale and
its debt can never disagree. A reversal is itself never reversed; the correction of a wrong reversal is a new entry.

### 7.5 The guards

| Act or read | Owner PIN | Why |
|---|---|---|
| Create or edit a customer; search; who owes what; a statement | no | the counter's work; everyone already sees the debt book |
| A credit sale | no | Q-L5.4 — the owner's brief puts credit on the till's one-click payment |
| A repayment | no | money coming in |
| Deactivate a customer who owes money | **yes** | it hides a debt |
| Opening balance | **yes** | a debt from nothing |
| Write-off | **yes** | forgives money |
| Refund | **yes** | money going out |
| Reversal | **yes** | changes the record |
| Void a credit sale | **yes** (L4) | Q7 |

---

## 8. THE SCHEMA — `0006_customers.sql`

No existing table changes (A-L4.2). Two new tables.

```sql
CREATE TABLE customers (
  id            CHAR(36)     NOT NULL PRIMARY KEY,
  name          VARCHAR(100) NOT NULL CHECK (length(trim(name)) > 0),
  name_key      VARCHAR(100) NOT NULL,                    -- textkey.Normalise(name): uniqueness, search (H9)
  phone         VARCHAR(20),                              -- Latin digits
  note          VARCHAR(200),
  is_active     SMALLINT     NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
  row_version   BIGINT       NOT NULL DEFAULT 1,
  created_at    CHAR(24)     NOT NULL,
  updated_at    CHAR(24)     NOT NULL,
  CONSTRAINT ux_customers_name_key UNIQUE (name_key)
);
CREATE INDEX ix_customers_phone ON customers (phone);

-- Insert-only. One chain per customer AND currency, ordered by seq, never by clock (H6).
CREATE TABLE debt_entries (
  id                     CHAR(36)     NOT NULL PRIMARY KEY,
  customer_id            CHAR(36)     NOT NULL REFERENCES customers(id),
  currency               CHAR(3)      NOT NULL REFERENCES currencies(code),
  seq                    BIGINT       NOT NULL CHECK (seq >= 1),
  business_date          CHAR(10)     NOT NULL,
  occurred_at            CHAR(24)     NOT NULL,
  kind                   VARCHAR(10)  NOT NULL
                           CHECK (kind IN ('opening', 'charge', 'payment', 'write_off', 'refund', 'reversal')),
  amount_minor           BIGINT       NOT NULL CHECK (amount_minor <> 0),        -- + owed, − settled
  balance_before_minor   BIGINT       NOT NULL,
  balance_after_minor    BIGINT       NOT NULL,
  sale_id                CHAR(36)     REFERENCES sales(id),                     -- a charge's sale
  reverses_id            CHAR(36)     REFERENCES debt_entries(id),              -- a reversal's entry
  -- Money that changed hands (payment in, refund out): what, in which currency, the change, and the rate (A-L5.5).
  fx_rate_id             CHAR(36)     REFERENCES fx_rates(id),
  local_per_usd_nano     BIGINT,
  tendered_currency      CHAR(3)      REFERENCES currencies(code),
  tendered_minor         BIGINT,
  change_currency        CHAR(3)      REFERENCES currencies(code),
  change_minor           BIGINT,
  customer_name_snapshot VARCHAR(100) NOT NULL,                                 -- the name the receipt shows
  note                   VARCHAR(200),
  created_at             CHAR(24)     NOT NULL,
  CONSTRAINT ux_debt_entries_place    UNIQUE (customer_id, currency, seq),
  CONSTRAINT ux_debt_entries_reverses UNIQUE (reverses_id),
  CONSTRAINT ck_debt_chain CHECK (balance_after_minor = balance_before_minor + amount_minor),
  CONSTRAINT ck_debt_sign_matches_kind CHECK (
    (kind IN ('opening', 'charge', 'refund') AND amount_minor > 0) OR
    (kind IN ('payment', 'write_off')        AND amount_minor < 0) OR
     kind = 'reversal'),
  CONSTRAINT ck_debt_charge_names_its_sale     CHECK ((kind = 'charge')   = (sale_id IS NOT NULL)),
  CONSTRAINT ck_debt_reversal_names_its_entry  CHECK ((kind = 'reversal') = (reverses_id IS NOT NULL)),
  CONSTRAINT ck_debt_cash_moves_only_on_payment_and_refund CHECK (
    (kind IN ('payment', 'refund')
       AND tendered_currency IS NOT NULL AND tendered_minor IS NOT NULL AND tendered_minor > 0
       AND change_currency IS NOT NULL AND change_minor IS NOT NULL AND change_minor >= 0
       AND fx_rate_id IS NOT NULL AND local_per_usd_nano IS NOT NULL AND local_per_usd_nano > 0) OR
    (kind NOT IN ('payment', 'refund')
       AND tendered_currency IS NULL AND tendered_minor IS NULL AND change_currency IS NULL
       AND change_minor IS NULL AND fx_rate_id IS NULL AND local_per_usd_nano IS NULL)),
  CONSTRAINT ck_debt_owner_acts_have_a_reason CHECK (
    kind NOT IN ('write_off', 'refund', 'reversal') OR (note IS NOT NULL AND length(trim(note)) > 0)),
  CONSTRAINT ck_debt_payment_never_below_zero CHECK (kind NOT IN ('payment', 'write_off') OR balance_after_minor >= 0),
  CONSTRAINT ck_debt_refund_only_what_is_owed_back CHECK (
    kind <> 'refund' OR (balance_before_minor < 0 AND balance_after_minor <= 0))
);
CREATE UNIQUE INDEX ux_debt_entries_one_charge_per_sale ON debt_entries (sale_id);   -- A-L5.1; NULLs are distinct
CREATE INDEX ix_debt_entries_business_date ON debt_entries (business_date);
```

The settings module declares `debt.default_currency` (a currency code, A-L5.4).

### 8.1 Verified before this note

The DDL above was applied, in one transaction with foreign keys on, to a **copy of the L4 seeded shop's database**
(`l4 seeded محمد #4`: 10 sales, 64 ledger rows) on 2026-09-14. Then:

- **7 valid rows accepted:** an opening; a charge naming a real sale; a pounds payment of a dollar debt with its rate,
  tender and change; a write-off with a reason; a refund of a negative balance; a reversal; a reversal taking a balance
  below zero.
- **27 invalid rows refused, each by the constraint named above** (asserted by name, as L2's lesson requires): a broken
  chain; a NULL balance; an opening that lowers and a payment that raises; a zero amount; a charge without its sale and an
  opening naming one; a reversal of nothing; a payment with no tender, with a **NULL rate**, with **NULL change**, with zero
  cash; an opening carrying cash; a write-off with a **NULL** and with a blank reason; a payment and a write-off below zero;
  a refund of nothing owed back and one past zero; two entries in one place; a second charge for one sale; reversing an
  entry twice; an unknown customer, kind and currency; a duplicate customer name key; a blank name.
- Two mistakes in the **check script** — one plant broke the chain before reaching the sign rule, one helper row reused a
  place — were found by the "refused by the right constraint" assertion and fixed; the schema did not change.
- **Who owes what** (`balance_after` at each chain's newest place, non-zero) plans as one scan of `debt_entries` with the
  place index for each newest-place lookup. §13.1 adds a timed test at 50,000 entries.

---

## 9. THE VERIFIERS

Reports, never repairs (D-L2.14); the seeder fails on a finding.

### 9.1 The debt ledger's verifier (new)

| Check | Finds |
|---|---|
| each chain's places are 1 … N; place 1 starts at 0; each starts where the previous ended | a lost, inserted or edited entry |
| a reversal names an entry of the same customer and currency, not a reversal, with the opposite amount | a reversal of the wrong thing |
| a payment's tender, less its change, is worth what it settled at its rate, within half a note (pounds) or half a cent (dollars) | a mis-converted repayment |
| a refund's amount is what was paid out, at its rate, within the same bound | a mis-converted refund |
| an entry's customer exists and the charge's name snapshot is non-empty | a charge nobody can be shown |

### 9.2 What the sales verifier learns (H8)

| Check | For |
|---|---|
| a `cash` sale: change ≈ tender − total (L4, unchanged) | cash sales only |
| a `credit` sale: change = 0; paid now worth less than the total; **exactly one charge**, in the settled currency, of total − paid now (the §5.2 rounding) | every credit sale (A-L5.1) |
| a voided credit sale: its charge **reversed** | voided credit sales |
| a charge names a `credit` sale | charges |

The sales verifier reads charges through its `Debts` port, as it reads stock movements through `Stock` (L4).

---

## 10. ARCHITECTURE

### 10.1 The `customers` module

```
internal/lite/customers/
├── domain/          Customer, Entry, Chain (the next entry from the newest), Payment pricing (§6), Verify
├── service.go       Search, Create, Update, SetActive, Statement, Outstanding, QuotePayment, RecordPayment,
│                    Opening, WriteOff, Refund, Reverse, Charge, ReverseCharge, EachCharge, Verify
├── infra/sqlite/    the store: insert-only entries, one UPDATE (customers' own fields)
└── customerstest/   fakes and StoreContract, run on the fake and on SQLite
```

**Ports it declares:** `Rates` (the rate in force), `Settings` (the cash note, the default debt currency), `OwnerGate`.
It imports no other module.

**Ports the till declares, new:** `Debts` — `Customer(id)`, `Balance(customer, currency)`, `Charge(input)` (joins the
caller's transaction), `ReverseCharge(saleID, reason)`, `EachCharge(fn)`. Bootstrap adapts `customers.Service` to it. The
dependency points one way — `sales` → `Debts` port; `customers` knows nothing of sales but the `sale_id` it stores.

### 10.2 A shared `tender` package

The exact conversion between the two currencies at a rate, and rounding to the note (L4 `inMinor`, `roundToNote`), move from
`sales/domain` to **`internal/lite/tender`** — pure: the kernel and `numinput` only — used by `sales/domain` and
`customers/domain`. A repayment and a sale cannot then round differently. `lite-pure-text` and `lite-domain-purity` gain it;
L4's tests of the change table stay green unchanged, which is the proof the move changed nothing.

### 10.3 Rules

| Rule | Holds |
|---|---|
| `lite-customers-isolated` (new) | `customers` imports no other Lite module |
| catalog, owner, settings, stock, fx, setup, **sales** isolation rules | each forbids `customers` |
| `lite-pure-text` | `tender` imports only the kernel and `numinput` |
| every rule | seen failing by a plant in `scripts/lite-arch-drill.sh` |

---

## 11. BINDINGS AND SCREENS

### 11.1 Bindings

| Façade.Method | Guard | Caller |
|---|---|---|
| **`Customers.Search`** (text, owing only, include inactive) — with balances per currency | — | CustomersScreen, the till's picker |
| **`Customers.Create`** / **`Customers.Update`** | — | CustomerForm (screen and till) |
| **`Customers.SetActive`** | owner if a balance is not zero | CustomersScreen |
| **`Customers.Statement`** (customer, currency) — entries with balance after, owed since, last payment | — | CustomerStatement |
| **`Customers.Outstanding`** — who owes what, and today's repayments per currency | — | CustomersScreen |
| **`Customers.QuotePayment`** / **`Customers.RecordPayment`** (with token) | — | PaymentDialog |
| **`Customers.Opening`** | owner | OpeningDialog |
| **`Customers.WriteOff`** / **`Customers.Refund`** / **`Customers.Reverse`** | owner | CustomerStatement |
| `Till.Quote`, `Till.Checkout` | — | gain `payment`, `customerId`; the quote gains paid now, added, balance now and after |
| `Sales.List`, `Sales.Receipt` | — | gain *on credit* totals and the receipt's credit block |

**12 new methods, 62 in total.** Every figure a string formatted by Go; balances never arrive pre-summed across currencies.

### 11.2 Screens

- **Till:** *Cash / On credit*; on credit, a customer field opening the picker (search, balances, *New customer*); *paid now*;
  the added amount and the balance after; *Pay* disabled until a customer is chosen.
- **Receipt:** the credit block (§5.5).
- **Sales:** *on credit* per currency.
- **Customers (`/customers`):** search; *owing only*; a table of name, phone, pounds balance, dollars balance, owed since,
  last payment; today's repayments per currency; *New customer*. A customer opens a **statement** with a tab per currency:
  every entry with its place, date, kind, amount, balance after and receipt number; actions *Take payment*, and in owner
  mode *Opening balance*, *Write off*, *Refund* (when below zero) and *Reverse* on an entry. A balance below zero reads
  *in the customer's favour*.

---

## 12. DEMO DATA

`lite-demoseed` grows, after L4's day at the till: **eight customers**, two sharing a first name told apart by a nickname.
From the paper book: openings in dollars for three, in pounds for two, in both for one. At the till: credit sales charged in
dollars (the default) and one in pounds; one with part paid now in pounds. Repayments: pounds towards a dollar debt; **pay
all** of a dollar debt in pounds (the rounded note, balance zero); a dollar payment with change. A credit sale that was
partly repaid is **voided** — the balance goes below zero — and **refunded**. One **write-off**. Then the stock, sales and
debt verifiers run, and the seeder fails if any finds anything.

---

## 13. TESTS, DRILLS, DEFINITION OF DONE

### 13.1 Tests (names are the contract)

| Layer | Tests |
|---|---|
| **tender** (moved) | L4's change and rounding tests, unchanged and green · `TestConversionRoundsOnceEitherWay` |
| **customers/domain** | `TestTheNextEntryContinuesTheChain` · `TestAPaymentNeverTakesABalanceBelowZero` · `TestAnAmountHandedOverSettlesAtTodaysRate` (the §6.2 table) · `TestPayAllClearsTheBalanceExactly` (the §6.3 table, property-tested across rates and balances: after is 0, gap ≤ half a note) · `TestChangeIsInPoundsUnlessDebtAndTenderAreDollars` · `TestARefundOnlyReturnsWhatIsOwedBack` · `TestOwedSinceIsTheDayTheCurrentDebtStarted` · `TestNamesAreUniqueAfterNormalisation` · verifier: one planted discrepancy per §9.1 row |
| **customers service (fake + contract)** | `StoreContract` · `TestBalancesAreNeverSummedAcrossCurrencies` · `TestAStalePaymentQuoteIsRefusedAndWritesNothing` · `TestNoRateNoPayment` · `TestOwnerActsNeedThePINAndAReason` (opening, write-off, refund, reversal) · `TestAnEntryIsReversedOnceAndAChargeOnlyByItsSale` · `TestDeactivatingACustomerWhoOwesNeedsTheOwner` · `TestAnInactiveCustomerCannotBeChargedButCanPay` · `TestWhoOwesWhatAt50000EntriesIsFast` |
| **sales** | `TestACreditSaleNeedsACustomer` · `TestTheDebtIsInTheCurrencyTheSaleIsChargedIn` · `TestPaidNowReducesTheDebtAndPaidInFullIsCash` (the §5.2 table) · `TestACreditSaleTotalIsTheCashTotal` · `TestTheTokenCoversThePaymentAndTheCustomer` · `TestVoidingACreditSaleReversesItsCharge` · the sales verifier: a credit sale with no charge, with two, with the wrong amount; a voided credit sale whose charge stands |
| **the database** | `TestACheckRefusesAnImpossibleDebtEntry` (§8.1's 27, by name, through the runner) · `TestDebtEntriesAreNeverUpdatedAndCustomersOnlyByTheirOwnFields` (SQL scan) · `TestTheMigrationAddsNothingToExistingTables` (an L4 database upgraded, every L4 row compared) |
| **real graph** | `TestACreditSaleWhoseChargeFailsLeavesNothing` (sale, lines, stock movements, receipt number) · `TestACreditSaleVoidedAfterAPaymentLeavesTheShopOwing` · `TestTheTillReachesTheCustomers` (the adapters) |
| **bindings** | wire shapes · every figure a string · no combined balance field · owner acts refused outside owner mode · `RecordPayment` with a stale token |
| **frontend** | credit needs a customer before *Pay* · the picker finds by phone in Arabic-Indic digits and creates a customer without leaving the sale · paid in full offers cash · the added amount and balance after are Go's · a payment quote goes stale and asks again · *pay all* shows the rounded note · owner acts through the PIN · a balance below zero reads in the customer's favour · the statement per currency, never summed · both languages |
| **seeder** | §12, three verifiers clean |

### 13.2 Drills planned

At least: a debt summed across currencies on the list; a payment converted at the sale's rate instead of today's; *pay
all* leaving the sub-note remainder owed; change computed in the wrong currency; the chain's `balance_before` taken from a
cached figure instead of the newest place; a place allocated outside the transaction; the charge written in its own
transaction (the real-graph rollback test fails); a charge without the UNIQUE index (the verifier's two-charge plant); the
sales verifier's credit rule removed (the seeder fails on its own data); a void not reversing the charge; a write-off
without the guard; a refund past zero (the CHECK); a reversal of a charge by hand; the repayment token check removed;
`customers` importing `sales` (archlint).

### 13.3 Definition of Done

> `make lite-ci` green · every invariant drilled · the seeder's customers open in the packaged app, three verifiers clean ·
> an installation from L4 upgrades with every row intact · the Customers screen, the till's credit and the receipt looked at
> in Arabic and English (owner, O5) · Windows `.exe` builds · Mizan's `scripts/check.sh` green if shared code changes ·
> [../PROGRESS.md](../PROGRESS.md), [../DECISIONS.md](../DECISIONS.md) and this document updated.

---

## 14. DECISIONS AND QUESTIONS FOR APPROVAL

### 14.1 Decisions — approve, amend, or reject

| # | Decision | § |
|---|---|---|
| D-L5.1 | A `customers` module owns `customers` and `debt_entries`, imports no module, and is reached by the till through a `Debts` port | 10.1 |
| D-L5.2 | **One ledger chain per customer and currency**, insert-only, ordered by `seq`, each entry recording its balance before and after; balances are the newest place's figure, **never summed across currencies** | 4.1, 4.2 |
| D-L5.3 | Six kinds — `opening`, `charge`, `payment`, `write_off`, `refund`, `reversal` — with their signs, links and cash fields held by CHECKs, every nullable comparison guarded | 4.1, 8 |
| D-L5.4 | **The debt currency is the currency the sale is charged in**; the credit default is a setting | 5.1, A-L5.2 |
| D-L5.5 | A credit sale's total is the cash total; optional **paid now** in either currency; paid in full is a cash sale | 5.2 |
| D-L5.6 | The charge is written **in the checkout transaction**; every credit sale has exactly one charge (UNIQUE + verifier) | 5.4, A-L5.1 |
| D-L5.7 | Voiding a credit sale reverses its charge in the void's transaction | 5.6 |
| D-L5.8 | **Repayments convert at the rate in force when paid**, round once, give change by L4's rule, and are quoted with a token | 6.1–6.4 |
| D-L5.9 | **Pay all** settles the balance exactly and records the rounded note taken; the gap is bounded by half a note | 6.3 |
| D-L5.10 | A balance below zero arises only from a reversal; it is used up by the next charge or refunded by the owner, never past zero | 7.3 |
| D-L5.11 | Any entry except a charge may be reversed once, with the PIN and a reason; a charge only by voiding its sale | 7.4 |
| D-L5.12 | The guards of §7.5 — PIN for openings, write-offs, refunds, reversals, and deactivating a customer who owes | 7.5 |
| D-L5.13 | Customer names unique after normalisation; phone searchable in any digits; the charge snapshots the name | 3 |
| D-L5.14 | "Owed since" and "last payment" computed from the chain; who owes what never sorts or totals across currencies | 4.3 |
| D-L5.15 | Every money movement snapshots the rate; no rate, no payment | A-L5.5 |
| D-L5.16 | The conversion and note rounding move to a pure `internal/lite/tender` package shared by sales and customers | 10.2 |
| D-L5.17 | A debt verifier (§9.1); the sales verifier learns credit sales **in the same change** that allows them (§9.2) | 9 |
| D-L5.18 | `lite-customers-isolated`; every other isolation rule forbids `customers` | 10.3 |

### 14.2 Questions only you can answer

**Q-L5.1 — When a sale is put on credit, which currency does the till default to?** Q2 said debts may be in either, chosen per
sale; this is only the default, one tap away. *Recommended: US dollars* — a debt that keeps its value while it is owed
(H1: 243,750 pounds today is 13.54 USD at 18,000). The alternative, *pounds*, is what most customers expect to hear.

**Q-L5.2 — May a customer pay part of a credit sale at the counter?** *Recommended: yes* — *paid now*, in either currency,
reducing the debt; paying the whole total is a cash sale.

**Q-L5.3 — Credit limits?** *Recommended: none in v1* — the till shows the customer's balance in both currencies before
*Pay*, and you know your customers. Alternatives: a per-customer limit that **warns**, or one that **refuses** unless you
enter the PIN.

**Q-L5.4 — Who may sell on credit?** *Recommended: whoever is at the counter, without the PIN* — your brief puts credit on the
till's one-click payment. The alternative asks for the owner's PIN on every credit sale.

**Q-L5.5 — At which rate is a debt repaid in the other currency?** *Recommended: the rate on the day it is paid* — that is
what makes a dollar debt keep its value. The alternative, the rate of the original sale, turns every dollar debt back into
a pounds debt in effect.

**Q-L5.6 — A credit sale that was partly repaid is voided: the shop now owes the customer.** *Recommended: allow it; the
balance shows in the customer's favour, is used by their next credit purchase, or is refunded with your PIN.* The
alternative refuses the void until the customer has been refunded.

**Q-L5.7 — Forgiving a debt (write-off)?** *Recommended: yes, with your PIN and a reason*, reported as bad debt in L6. The
alternative keeps every debt on the list until it is paid.

**Q-L5.8 — Show a customer's two balances also as one reference figure?** For example *≈ 16.25 USD in all, at today's
15,000*. *Recommended: yes, on the customer's statement only, labelled with its rate and never stored or sorted by.* The
alternative shows only the two balances.

**Q-L5.9 — Is name, phone and a note enough to know a customer by?** *Recommended: yes*, with names unique — *أبو محمد —
الحلاق*. If you also want an address or a second phone, say so now: adding a column later is a migration.

---

## 15. AT APPROVAL (2026-09-14)

The owner approved this note, D-L5.1–18 and A-L5.1–5, and answered §14.2:

| Q | The owner's answer | In the build |
|---|---|---|
| Q-L5.1 | **US dollars** by default, one tap to pounds | `debt.default_currency = USD`; *On credit* switches the till to dollars |
| Q-L5.2 | **Yes**, part paid at the counter | *Paid now*, in either currency |
| Q-L5.3 | **No credit limits**; show the balance before completing the sale | the quote's *balance now* and *after this sale* |
| Q-L5.4 | **Anyone** at the counter; no PIN for credit sales or repayments | as recommended |
| Q-L5.5 | **The rate in force on the day of the repayment** | as recommended; `TestARepaymentConvertsAtTodaysRate` |
| Q-L5.6 | **Allow the void**; the excess becomes the customer's credit, used or refunded | as recommended |
| Q-L5.7 | **Write-offs**, with the owner PIN and a required reason | as recommended |
| Q-L5.8 | **Each currency's balance separately**, each labelled with its reference at the rate in force | *changed from the recommendation* — no single figure; D-L5.i2 |
| Q-L5.9 | **Name, phone, note**, names unique (*أبو محمد - الحلاق*) | as recommended |

---

## 16. WHAT WAS BUILT

| Layer | What |
|---|---|
| **tender** (new, pure) | `Convert`, `Round`, `Increment`, `WithinHalf` — the till's conversion and note rounding, moved; L4's tests unchanged and green |
| **settings** | `debt.default_currency` (USD, or the local currency) |
| **Migration** | `0006_customers.sql`: `customers`, `debt_entries` (§8, with D-L5.i1); no existing table changes |
| **customers** (new module) | domain: customer rules, the chain (`Place.Append`), `Reverse`, `Summarise` (owed since, last payment), `PricePayment`, `PriceRefund`, `ParseAmount`, the debt verifier; service: `Create`, `Update`, `SetActive`, `Search`, `WithBalancesOf`, `Balances`, `Statement`, `Outstanding`, `QuotePayment`, `RecordPayment`, `Opening`, `WriteOff`, `Refund`, `Reverse`, `Charge`, `ReverseCharge`, `ChargeOf`, `EachCharge`, `Verify`; four ports; `customerstest` fake and `StoreContract` on fake and SQLite; SQLite store |
| **sales** | credit in `Price` (`applyCredit`, `DebtOf`), the `Debts` port, the charge in `Checkout`, its reversal in `Void`, credit on `Receipt` and `Day` (*on credit*); the sales verifier's credit rules |
| **bootstrap** | the debt book built before the till; adapters both ways |
| **api** | `Customers` (12 methods: Search, Create, Update, SetActive, Statement, Outstanding, QuotePayment, RecordPayment, Opening, WriteOff, Refund, Reverse) — **62 bound**; `Till` cart gains `payment`, `customerId`; quote gains the debt and balances; the receipt gains its credit fields; settings gain `debtCurrency` |
| **archlint** | `lite-customers-isolated`; seven module rules forbid `customers`; `tender` joins the pure packages — **50** rules seen failing |
| **i18n** | 37 error codes and findings, 5 owner-history labels, 105 screen strings, in both languages |
| **frontend** | the till's *Cash / On credit*, the customer picker (search in any digits, balances, new customer in place), *paid now*, the debt and balances; the receipt's credit block; *on credit* on the Sales screen; **Customers** (`/customers`): search, owing only, balances per currency with references, today's debt book, the statement with a tab per currency, payments (quote and token, pay all, change), and the owner's opening, write-off, refund and reversal |
| **seeder** | eight customers, seven openings, three credit sales, four payments, a voided repaid credit sale refunded, a write-off; three verifiers |

## 17. DECISIONS MADE WHILE BUILDING — approved 2026-09-14

| # | Decision | Why |
|---|---|---|
| D-L5.i1 | **`debt_entries.cash_note_minor`** on payments and refunds (NULL otherwise, in the cash CHECK) | the verifier's "within half a note" needs the note the change was rounded to — `sales` records it for the same reason |
| D-L5.i2 | **Q-L5.8 as answered:** every balance carries its own reference in the other currency at the rate in force, labelled with the rate; no figure anywhere adds currencies (`TestNoDTOCarriesABalanceSummedAcrossCurrencies`) | the owner's answer |
| D-L5.i3 | A credit quote **without a customer still prices** (`needsCustomer`); checkout refuses it | the total stays on screen while the cashier picks the customer |
| D-L5.i4 | `payment` and `customerId` are part of the **cart** (and so the token), not a separate checkout field | what is charged depends on them |
| D-L5.i5 | Pressing *On credit* switches the till to the default debt currency and opens the picker | Q-L5.1, one tap |
| D-L5.i6 | A payment's value in the other currency rounds to the debt's **minor unit**; change rounds to the **note** | §6.2 as written |
| D-L5.i7 | *Pay all* whose conversion rounds to nothing (a balance under half a note in the other currency) is refused — pay it in the debt's currency | a zero tender cannot be recorded |
| D-L5.i8 | The Sales screen's *on credit* counts credit sales rung up that day, whatever happened since | consistent with D-L4.i1 |
| D-L5.i9 | *Today's debt book* per currency: payments and what they settled, cash in and change out by the currency handed, credit charged, written off | the Customers screen's day, until L6's cash summary |
| D-L5.i10 | The statement lists **newest first**; a charge links to its receipt | read from the top, as a paper book is read from the last page |
| D-L5.i11 | The 50,000-entry timing test allows **10 s under the race detector** (1 s otherwise), its rows generated in SQL | R3 |
| D-L5.i12 | The seeder's *sold* figure is sales charged less refunded, credit included; its summary counts credit sales and both voids | R7 |

## 18. FINDINGS

**R1 — nothing tested that a repayment uses today's rate *value*.** The drill "a payment at a rate other than today's" first
did not compile (refused, redone), then **survived**: every test ran at 15,000, and the stale-quote test proves only that a
changed rate id is refused. Q-L5.5 is the owner's answer that matters most for dollar debts, so
`TestARepaymentConvertsAtTodaysRate` repays a debt made at 15,000 at 18,000 — 180,000 pounds settle 10.00 USD, not 12.00.

**R2 — a frontend drill survived:** the list's balance cell for a balance below zero was untested (only the statement's
header was). A test now holds *in the customer's favour* in the list.

**R3 — the timing test failed under the race detector:** 1.35 s against a 1 s bound, after 60 s of one-by-one inserts. The
query itself takes 43 ms; the race detector instruments the driver. Rows are now generated in one SQL statement and the bound
is 10 s under `-race` (1.5 s measured).

**R4 — confirmed before the note (O7):** DESIGN §5's `ck_debt_tender_complete` accepts a payment with a NULL amount on real
SQLite. `0006` guards every nullable comparison; 23 impossible entries, including five NULL cases, are refused by name through
the runner.

**R5 — the drill "charge written in its own transaction" is caught by a hang**, as L4's stock drill was (L4 R4).

**R6 — golangci-lint found 21 issues** in new code (shadowed `err`, a context not passed, a predeclared name, a switch, a
conversion) and ESLint 2 unused test parameters; all fixed.

**R7 — the seeder's summary under-reported** after credit arrived ("10 sales, 1 voided" with 13 and 2); corrected.

## 19. EVIDENCE

| Check | Result |
|---|---|
| `make lite-ci` | **pass** — nothing NOT RUN (§19.1) |
| Go tests (race) | **376** Lite test functions (L4: 328) |
| Frontend | **351** tests in 25 files, 0 console warnings; bundle gate 4 (L4: 317) |
| golangci-lint v2 | 0 issues |
| archlint | clean; **50** rules seen failing (L4: 39) |
| Timing | who owes what over 50,000 entries: **43 ms** (1.5 s under `-race`) |
| Windows | `Mizan Lite.exe` and `lite-demoseed.exe` built — not run (O1) |
| **Mizan after L5's archlint change** | `scripts/check.sh` green: 100 Go packages, 337 frontend tests, 0 lint issues |

**The packaged macOS app, run for real (2026-09-14):**

- **An L4 shop upgraded:** a copy of `l4 seeded محمد #4` (schema 5) opened in the packaged L5 app — `migration applied version 6
  "customers"`, ready at schema 6, the frontend reached Go, quit clean. `sales` (10), `sale_lines` (15), `stock_ledger` (64),
  `stock_levels` (40), `products` (40), `fx_rates` (3) and `owner_events` (19) **identical row for row**; integrity ok; no
  foreign-key problems. On a further copy a credit sale (1.87 USD), a 20,000-pound payment (1.33 USD) and the sale's void left
  the customer **−1.33 USD in their favour**, and the stock, sales and debt verifiers found nothing.
- **The seeder's debt book** into `l5 seeded محمد #5`: 8 customers, 7 openings, 3 credit sales, 4 payments; 13 sales, 2 voided;
  17 debt entries — 7 openings, 3 charges, 4 payments, a write-off, a reversal, a refund. Opened in the packaged app: ready at
  schema 6, quit clean.

### 19.1 Final runs

After the last change to code and documents, 2026-09-14: `make lite-ci` — wails generate, gofmt, vet, Windows and Intel-Mac
cross-compile, archlint and its 50 planted drills, race tests, golangci-lint v2 (0 issues), ESLint, typecheck, 351 frontend
tests and gates, production build, G5 on the bundle — **every step passed, none NOT RUN**. Mizan's `scripts/check.sh` —
**all local checks passed** (100 Go packages), run after L5's last change to `arch-rules.yml`.

## 20. MUTATION DRILLS — 26 BEHAVIOUR DRILLS, ALL CAUGHT

| # | Planted defect | Caught by |
|---|---|---|
| G1 | a chain read across currencies — balances summed | the store contract on SQLite, `TestTheDebtBookThroughTheBindings` |
| G2 | a payment converted at a rate other than today's | `TestARepaymentConvertsAtTodaysRate` — **did not compile first, then survived** (R1) |
| G3 | pay all leaving the sub-note remainder owed | `TestPayAllClearsTheBalanceExactly` |
| G4 | change in the wrong currency | `TestChangeIsInPoundsUnlessDebtAndTenderAreDollars`, `TestAnAmountHandedOverSettlesAtTodaysRate` |
| G5 | the balance before read from the oldest place | the store contract, `TestConcurrentPaymentsOnOneDebtNeverShareAPlace`, `TestTheTillReachesTheCustomers` |
| G6 | the place read before the payment's transaction | `TestConcurrentPaymentsOnOneDebtNeverShareAPlace` |
| G7 | the charge written in its own transaction | `internal/lite/bootstrap` — **by a hang at the timeout** (R5) |
| G8 | no UNIQUE index on a sale's charge | `TestACheckRefusesAnImpossibleDebtEntry`, the store contract |
| G9 | credit sales verified as cash sales | `TestTheSalesVerifierKnowsCreditSales` and **every seeder test** — the seed fails its own check |
| G10 | a void that does not reverse the charge | `TestVoidingACreditSaleReversesItsCharge`, `TestTheTillReachesTheCustomers`, `TestTheDebtBookThroughTheBindings` |
| G11 | a write-off without the guard | `TestOwnerActsNeedThePINAndAReason`, `TestTheDebtBookThroughTheBindings` |
| G12 | a refund past zero (the CHECK) | `TestACheckRefusesAnImpossibleDebtEntry` |
| G13 | a charge reversed by hand | `TestReversalRules`, `TestAnEntryIsReversedOnceAndAChargeOnlyByItsSale` |
| G14 | the repayment token check removed | `TestAStalePaymentQuoteIsRefusedAndWritesNothing`, `TestTheDebtBookThroughTheBindings` |
| G15 | the cash verifier with no rounding allowed | `TestPayAllClearsTheBalanceExactly` (property), `TestTheVerifier` |
| G16 | migration 0006 updating an existing table | `TestTheMigrationAddsNothingToExistingTables` |
| F1 | credit Pay enabled with no customer | "credit charges in dollars by default, needs a customer before Pay…" |
| F2 | credit sent with no customer | the same test |
| F3 | credit not charged in the default debt currency | the same test |
| F4 | a payment recorded without its quote's token | "quotes a payment in pounds at today's rate and records it with the quote's token", "a payment gone stale…" |
| F5 | a stale payment not quoted again | "a payment gone stale is quoted again and must be recorded again" |
| F6 | an owner's act without the PIN flow | "a write-off asks for the PIN and a reason", "…an entry is reversed through the PIN" |
| F7 | the picker hiding customers who owe nothing | "credit charges in dollars by default…" |
| F8 | the statement tab not switching currency | "the statement has a tab per currency…" |
| F9 | a balance in the customer's favour shown as owed | "a balance below zero reads in the customer's favour in the list" — **survived first** (R2) |
| F10 | the receipt's credit block hidden | "ReceiptView — on credit…", the till's credit test |
| A1–A11 | `customers` importing sales, sales/domain, fx/domain; `tender` importing the kernel's money; sales, catalog, owner, settings, stock, fx and setup importing `customers` | archlint, on every run |

## 21. NOT VERIFIED

1. **The Windows build has not run** (O1).
2. **The Customers screen, the till's credit and the receipt's credit block have not been looked at** in either language (O5).
3. **A year of debts:** who owes what is measured at 50,000 entries (43 ms); a statement or `Search` over thousands of
   customers reads each customer's chains in turn and is unmeasured (O8).
4. **Printing a statement** is L7's.

## 22. DEFINITION OF DONE

| Criterion (§13.3) | Status |
|---|---|
| `make lite-ci` green | ✅ |
| Every invariant drilled | ✅ 26 behaviour drills + 11 architecture; three strengthened after surviving or failing to compile (R1, R2) |
| The seeder's customers open in the packaged app, three verifiers clean | ✅ by the log and the database; ⏳ looked at (§21.2) |
| An installation from L4 upgrades with every row intact | ✅ §19 |
| The Customers screen, the till's credit and the receipt looked at in Arabic and English | ⏳ owner (O5) |
| Windows `.exe` builds | ✅ built — not run |
| Mizan's `scripts/check.sh` green | ✅ |
| PROGRESS, DECISIONS and this document updated | ✅ |

### 22.1 At commit (2026-09-14)

The owner approved D-L5.i1–i12 — naming D-L5.i1 (the smallest note recorded on payments and refunds) and D-L5.i2 (each
balance with its labelled reference) — and the commit. O7 is closed in [../PROGRESS.md](../PROGRESS.md); O1, O5 and O8 remain.

**Next:** L6's design note — reports, profit and cash: [L6_REPORTS.md](L6_REPORTS.md).

