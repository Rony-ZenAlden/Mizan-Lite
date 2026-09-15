# Mizan Lite — Edition Design

> **Status: APPROVED 2026-09-13**, with the owner's answers and two added requirements recorded in §13.
> L0 is implemented — see [`phases/L0_SKELETON.md`](phases/L0_SKELETON.md). Each later phase opens with its own
> design note in [`phases/`](phases/); the index is [`README.md`](README.md).
>
> **Audience:** the owner deciding whether and how to build Lite, and whoever implements it.
> **Date:** 2026-09-13. **Base:** Mizan v1.0.0 + Phase 10.19 (`1a413cd`).

---

## 0. How to read this document

| § | Content |
|---|---------|
| 1 | **Analysis** — what the brief gets right, and nine corrections it needs before anything is built |
| 2 | **D1, the fork strategy** — the decision everything else depends on |
| 3 | Scope: what Lite keeps from Mizan, what it strips, and what the brief left out |
| 4 | Domain rules — the invariants the code must hold |
| 5 | SQLite schema (full DDL) |
| 6 | Go architecture: layout, contracts, and the checkout algorithm |
| 7 | **The binding bridge** — six gates that make an unconnected binding a build failure |
| 8 | Bilingual handling: RTL, digits, and number input |
| 9 | Testing strategy and the demo seeder |
| 10 | Roadmap: phases L0–L8, each with a Definition of Done |
| 11 | Decisions and questions (as put for approval) |
| 13 | **Amendments after approval** — answers, added requirements, and changes made while building L0 |

Code in this document is **contract-level**: types, signatures, and the algorithms whose
correctness is the point. It is not the implementation.

---

## 1. ANALYSIS

### 1.1 What the brief gets right

- **A narrow product for a real shop type.** A pantry shop sells by the kilo, the litre and the tin,
  in two currencies, often on credit. Mizan serves it, but through a surface built for a
  wholesaler with purchasing, three-way matching and double-entry books.
- **Naming the defects as requirements.** "No unbound methods, no unrouted screens, no mock in
  packaged builds" is Mizan's history turned into acceptance criteria. That is the right lesson.
- **Historical FX snapshots at the moment of sale.** Mizan §9.3, applied correctly.
- **A seeder as an end-to-end test.** Mizan's untracked `cmd/demoseed` already proves the idea:
  drive the real services, never INSERT rows.

### 1.2 Nine corrections before anything is built

These are not style preferences. Each one, left as written, produces a defect this project has
either already shipped once or would ship on day one.

#### C1 — The frontend is Vite + React, not Next.js, and Lite should stay that way

Mizan's `frontend/package.json` is **Vite 5, React 18, react-router 6, TanStack Query**. There is
no Next.js anywhere in the repository.

Next.js is the wrong tool inside Wails. Its value is server rendering, file-system routing on a
server, and API routes — and a Wails webview has no server. What remains is a static export mode
that disables most of the framework, plus a build pipeline Wails does not integrate with. Lite
keeps **Vite + React + react-router**, which is proven in this repository, packaged, and tested.

#### C2 — `window.go.main` is the exact bug that shipped in 10.17

Wails publishes a bound struct under its **Go package name**. Mizan's bindings live in package
`bindings`, so the runtime publishes `window.go.bindings.*`. `bridge.ts` read `window.go.main`,
and **every packaged build silently served mock data**. That is recorded in
`frontend/src/lib/wails/namespace.test.ts`.

The brief hardcodes `window.go.main...` as the contract. Lite must not type a namespace string
anywhere: the frontend imports the files Wails **generates** (`wailsjs/go/<pkg>/<Struct>.js`), so
TypeScript fails to compile when a Go method is renamed or removed. See §7.

#### C3 — Core code does not belong in `cmd/app/`

`cmd/` holds entry points: `package main`, a few lines, a call into a composition root. Putting
the POS service, inventory engine and FX sync there has three costs:

1. **`package main` cannot be imported**, so the services can only be tested from inside `main` —
   no integration test package, no seeder reusing them.
2. **Bound structs in `package main` publish under `window.go.main`**, which couples the
   frontend's namespace to where an entry point happens to live — the C2 defect's precondition.
3. It defeats the architecture rules archlint enforces everywhere else in the repository.

Lite's services live in `internal/lite/…`; `cmd/` and the Wails project hold only `main` (§6.1).

#### C4 — An automatic FX fetcher conflicts with two facts, and must propose rather than apply

**The product promise.** Mizan's 1.0.0 changelog says: *"It runs with no network, ever… no
server, no account, and no telemetry."* A background HTTP call changes that promise. It can be
changed, but deliberately, and a shop must be able to see that it is off.

**The Syrian rate.** In a market with a parallel rate, the number a shop can buy dollars at can
differ widely from the official one. Mizan §G.1 and 10.18 bind pricing and sales to the **market**
rate for exactly this reason: *"defaulting to `official` would reprice a shop's whole day at a rate
nobody can actually buy dollars at."* Most general-purpose free FX APIs report an official or
interbank figure, and availability from inside Syria cannot be assumed.

A fetcher that **silently applies** whatever it downloads can therefore reprice every product in
the shop, mid-day, at a wrong rate, with nobody having touched anything.

**Design (D5):** the fetcher is **off by default** and must be enabled in settings. It writes a
**proposal**; the header shows *"Fetched: 14,850 — Accept?"* beside the rate in force; one click
applies it. The manual rate is never overwritten by a background process. The source is an
adapter behind a port — **I am not choosing a provider**, because which rate a shop trusts is the
owner's knowledge, not mine (Q1).

#### C5 — "Receipt bindings present but disabled in the UI" contradicts "zero unconnected bindings"

A binding with no caller is the defect the brief bans. Both requirements can hold if the words
mean this: **the caller exists and is gated by a setting.** The "Print receipt" button is written,
tested and routed; it renders only when `receipts.enabled` is true. The reachability gate sees a
real caller; the shopkeeper sees nothing until they switch it on. "Disabled" is a setting value,
never a missing caller.

#### C6 — "Profit = (Sell − Cost) × Qty" is ambiguous when the pound moves, and the wrong reading shows profit that is not there

Suppose olive oil was bought at **$3.00/L** when the rate was 13,000, and sold today at
**42,000 SYP/L** when the rate is 15,000.

| Reading | Cost used | Profit per litre | What it says |
|---|---|---|---|
| Cost frozen in SYP at purchase | 39,000 SYP | **+3,000 SYP** | "We made money" |
| Cost at today's rate | 45,000 SYP | **−3,000 SYP** | "We sold below cost" |
| In USD | $3.00 vs $2.80 | **−$0.20** | "We sold below cost" |

The first reading is **phantom profit**: the shop is liquidating inventory it cannot replace at
the price it charged. In a devaluing currency that is how a shop that looks profitable runs out of
stock money. **Design (D4):** unit cost is held in **USD**; every sale line snapshots the cost and
the rate; profit is reported in **USD and in SYP at the sale's own rate**, so the two readings
always agree.

"Expected vs. Actual profit" also has two plausible meanings, and they need different data (Q3).

#### C7 — A debt recorded in SYP loses value while it is owed

A customer who takes 150,000 SYP of goods on credit and pays it back three months later may repay
a fraction of the dollar value. Many shops in this situation record credit in dollars. This is a
business decision, not a technical one (Q2), so the schema supports **both currencies per
customer** and the default is a setting.

#### C8 — "SYP" must not be welded into the schema

Syria has announced a redenomination of the pound removing two zeros. **Confirm its current status
before building** — I am not certain of its timeline or of the code the new currency will use.
Mizan already models redenomination (successor currency + fixed factor, Step 0.9). Lite keeps the
currency code as **data in columns**, never as a column name or an enum in Go, so a new currency
code is a migration and a seed row, not a rewrite. Column names say `local` and `usd`, not `syp`.

#### C9 — "Decimal precision" must never mean floating point

`1.750 kg` and `0.5 L` are exact in Mizan today: quantities are `int64` scaled by 10⁶
(`kernel/quantity.Scale`), prices are `money.UnitAmount` at 10⁻⁶ of the major unit, and money
crosses into JavaScript as a **string**. Lite reuses these unchanged. A JavaScript `Number`
never holds a price, a quantity or a total — in Lite, **the frontend performs no money arithmetic
at all** (§6.4).

#### A finding while reading, for Mizan rather than Lite

Mizan's sales and purchasing documents store `exchange_rate_micro` (10⁻⁶) while the currency
engine stores `rate_nano` (10⁻⁹). If a document ever stores a rate in the USD-per-SYP direction,
micro precision gives a rounding error of up to ~0.65% at today's magnitudes (1/13,000 ≈ 77 micro, so one micro is 1/77 of the value). **Not verified which
direction is stored** — worth one check, separately from this work. Lite avoids it by storing one
canonical direction only (§4.3).

---

## 2. D1 — THE FORK STRATEGY

Everything below depends on this. There are three ways to build Lite.

### Option A — A "pantry profile" over full Mizan

Mizan's mandate is *configuration, not code*, and 10.19 already added workspaces (`ui.landing`).
A pantry business profile plus a simplified shell would be Lite with zero new backend.

**Why I do not recommend it:** simplicity here is not only the screens. A credit sale in Mizan
posts through number series, posting rules, journal entries and the outbox; a USD cash tender at
the till, per-line USD profit and dual-currency credit would all require changes inside Mizan's
sales and accounting modules — reaching into a released product to serve a different one. The
bug surface of the till would stay the bug surface of a full ERP.

### Option B — A second app in the same Go module, reusing the kernel and platform *(recommended)*

A new Wails project in `apps/lite/`, its own lean business modules in `internal/lite/`, its own
schema and frontend — importing Mizan's **kernel** and **platform** packages directly.

**Why:** the parts of Mizan that are hardest to get right and are domain-agnostic — exact money,
quantities, single rounding, the database platform, migrations with backup-before-migrate,
verified backups — carry **the drills that caught the six defects** listed in `PHASE_10_POLISH.md`,
including a costing conversion wrong by a factor of a hundred. Rewriting them for "simplicity"
would discard that evidence. What Lite rewrites is only the business layer, which is where it
genuinely differs.

**Verified while writing this:** no `internal/kernel/*` or `internal/platform/*` package imports a
business module, the API layer or bootstrap. `platform/backup` imports only `kernel/clock` and
`kernel/errs`. The boundary archlint has held since Phase 0 is what makes this option possible.

**Why same module:** Go's `internal/` rule forbids importing `github.com/mizan-erp/mizan/internal/…`
from any other module. Same module is the only way to reuse without copying or restructuring.

**Costs, stated:**
- A kernel change must pass **both** editions' suites. That is a benefit for correctness and a
  cost in effort.
- **Lite has no double-entry books.** No balance sheet, no trial balance. A shop that outgrows Lite
  migrates to Mizan through export/import, not by switching a setting.
- **Two Wails projects in one repository is unproven here.** Wails reads `wails.json` from the
  current directory and builds with the Go toolchain, which resolves `go.mod` upwards — so it
  should work, but **L0 opens with a spike to prove it** before anything else is built.

### Option C — A separate repository

Clean separation, independent release cadence. **Cost:** Go's `internal/` rule means the kernel
must be copied (and then drift) or moved out of `internal/` into a public package (a refactor of a
released product). Every drill protecting the copied code becomes a copy too, maintained twice.
Worth it only if Lite will be sold, versioned and supported by a different team.

### Recommendation

**Option B.** It keeps the proven foundation, rewrites only what differs, and the edition boundary
is enforced by archlint rather than by discipline (§6.2).

---

## 3. SCOPE

### 3.1 Reused from Mizan, unmodified

| Package | Why Lite needs it |
|---|---|
| `kernel/money` | `Money`, `UnitAmount`, `Rate`, `LineExtension` — exact, single-rounding |
| `kernel/quantity` | 10⁶-scaled quantities, `Unit` with a fractional flag, `Parse` |
| `kernel/round` | Seven rounding modes, `ParseMode` |
| `kernel/errs` | Typed, categorised errors |
| `kernel/id`, `kernel/clock` | UUIDv7 keys, the portable CHAR(24) timestamp |
| `kernel/locale` | Tag handling, RTL detection, fallback chain |
| `platform/database` | Single-writer pool, WAL, Unit of Work, dialect shim |
| `platform/migrate` | Forward-only runner, checksums, **backup before every migration** |
| `platform/backup` | Daily + on-close snapshots, verification, retention |
| `platform/paths` | OS app-data directory, `MIZAN_DATA_DIR` override |
| `platform/i18n` | `LoadFS(fs.FS)` only — Lite passes its own catalog. The package also links `platform/config`, `eventbus` and Mizan's `locales` transitively; harmless at runtime, and the reason §6.2's rule governs **direct** imports |
| `api/envelope` | `Result[T]` / `APIError` — no English prose crosses the boundary |

**Two kernel additions (D6):** `money.LineExtensionConverted(price, qty, rate, to, mode)` — see
§4.4 for why the existing functions cannot be composed without rounding twice — and
`money.WeightedAverageUnit` (§6.6). Both carry property tests, and both are available to Mizan.

### 3.2 Stripped

| Mizan capability | Why Lite drops it |
|---|---|
| Double-entry accounting, posting rules, periods, year end | A pantry shop needs profit and debts, not a balance sheet |
| Tax engine | Not required by the brief; Mizan ships it disabled anyway |
| Purchasing documents (PO, GRN, bills, 3-way match, landed cost) | Replaced by a single **Receive stock** act (§3.3) |
| RBAC, roles, policies, field redaction | Single operator in v1 (Q7) |
| Multi-company, branches, warehouses | One shop, one stock location |
| Variants, attributes, packagings, price lists | One product = one unit = one price |
| Number series | A receipt number is `MAX + 1` inside the single writer |
| POS shifts, PIN login | Replaced by a daily cash summary per currency |
| Event bus, outbox, notifications | One transaction per act; no cross-module events |
| Config scope chain, feature flags, profiles | A typed settings row; see §6.3 |
| Setup wizard | A first-run screen: shop name, language, opening rate |
| CSV import, global search registry, dashboard | Search is the POS search box; profit reports replace the dashboard |

### 3.3 Missing from the brief, and required for it to work

1. **Receiving stock.** Without it, stock can only go down and cost is unknown, so neither the
   inventory ledger nor profit can be computed. Lite adds one act: product, quantity, unit cost
   (in USD or SYP), done.
2. **Stock adjustment.** Dairy expires, jars break, counts disagree. A signed quantity with a
   required reason.
3. **Debt repayment.** A debts table that can only grow is a list, not a ledger.
4. **Voiding a sale.** A till mistake has to be correctable. A void **records** a reversal —
   stock back, debt reversed — and never deletes or edits the sale.

### 3.4 Deliberately not in v1

Break-bulk (buying a 16 L tin and selling it by the litre — see Q8), multiple users and PINs,
suppliers and supplier balances, barcode label printing, cloud sync, and a third language.
Barcode **scanning** costs nothing: a USB scanner types into the POS search box.

---

## 4. DOMAIN RULES

### 4.1 Units of measure

| Code | Kind | Input decimals | Example |
|---|---|---|---|
| `kg` | mass | 3 | 1.750 kg |
| `l` | volume | 3 | 0.500 L |
| `piece` | count | 0 | 3 pieces |
| `jar` | count | 0 | 2 jars |
| `container` | count | 0 | 1 container |
| `tin` | count | 0 | 1 tin |

- Units are **seed data** with names in the i18n catalog (`uom.kg`), not user content.
- Stored as `quantity_micro` (10⁶). **Input decimals** limit what the operator may type; storage
  precision is always 10⁶, so a later change to input decimals never requires a migration.
- A count unit refuses a fractional quantity **in the domain**, via `quantity.Unit.AllowsFractional`
  — not only in the input mask.
- A product has **exactly one** unit and it never changes after the first stock movement (the
  ledger's quantities would change meaning).

### 4.2 Money and currencies

- Two currencies in v1: the **local currency** (SYP today, a code in data) and **USD**.
- Each product has a **price currency**: priced in USD and converted at the till, or priced in
  local currency directly. Both are common in the same shop.
- **Unit cost is always held in USD** (D4). A stock receipt entered in local currency is converted
  once, at the receipt's rate snapshot, and the original amount and rate are kept on the row.
- **Local-currency cash rounding** is a setting (Q5): the smallest amount the shop actually hands
  over. The rounding difference is **recorded on the sale** as its own column, so
  `Σ lines + rounding = total` always holds and is asserted.

### 4.3 Exchange rates

- **One canonical direction: local currency per 1 USD**, stored as `local_per_usd_nano` (10⁹).
  At 15,000 that is 1.5 × 10¹³, far inside `int64`. The inverse is **never stored** — it is
  computed at the point of use, in the same big-integer expression as the amount (§4.4).
- `fx_rates` is **append-only**. The rate in force is the row with the latest `effective_at ≤ now`,
  ties broken by `created_at DESC` — Mizan 0.9's correction, because a same-day fix must win over
  the mistake it fixes.
- Every rate read returns its **age**. The till shows a warning when the rate in force is older
  than `fx.stale_after_hours` (Mizan 0.9: a rate with no expiry never expires by itself).
- Fetched quotes go to `fx_fetch_log`. **Only an operator action** inserts a fetched quote into
  `fx_rates` (C4).
- A sale **snapshots the rate value**, not only the rate's id, so the sale can be read without
  joining, and a deleted-by-mistake rate row could never change a posted receipt.

### 4.4 The one calculation that must round once

For a USD-priced product sold in local currency, the obvious composition rounds twice:

```
0.5 L × $3.33/L = $1.665   → LineExtension rounds (half-up) to $1.67
$1.67 × 13,000             = 21,710 SYP     ✗
exact: 0.5 × 3.33 × 13,000 = 21,645 SYP     ✓   (65 SYP per line, accumulating down a receipt)
```

`LineExtensionConverted` computes `qty_micro × price_micro × rate_nano` as one `big.Int` and
rounds **once**, to the target currency's minor unit. Every line computes **both** currencies from
the same exact product, each rounded once. The two totals are therefore not `usd × rate` of each
other to the last unit — **by design**, and the test says so. The **settlement currency** figure
is what was charged; the other is the reference.

### 4.5 A sale

A sale is **one database transaction** (`database.Do`), or nothing:

1. Resolve the rate in force; snapshot its value.
2. For each line: load product → validate quantity against unit → compute line values in both
   currencies (§4.4) → snapshot name, unit, price, price currency and **unit cost**.
3. Apply cash rounding to the settlement total; record the difference.
4. Allocate the next receipt number.
5. Insert `sales` and `sale_items`.
6. For each line: append a `stock_ledger` row (negative quantity) and update the product's
   `on_hand_micro` in the same statement set.
7. If the payment is **credit**: append a `debt_entries` charge for the customer, in the debt
   currency (Q2).
8. Commit.

**Invariants the tests hold:**
- `products.on_hand_micro = Σ stock_ledger.quantity_micro` for every product (a verifier, reported
  never repaired — Mizan's rule).
- A credit sale without a customer is **unrepresentable** (CHECK constraint).
- A failure at any step leaves **zero** rows from any step (the rollback drill).
- A posted sale is never updated except `status`/`voided_*`, and never deleted.

### 4.6 Stock and weighted-average cost

On a receipt of `q` at unit cost `c` (USD) into `on_hand` at average `a`:

```
if on_hand > 0:  a' = (on_hand × a + q × c) / (on_hand + q)     one rounding, HALF_UP, at 10⁻⁶ USD
else:            a' = c                                           no history worth averaging
```

The `on_hand ≤ 0` branch matters: averaging against a **negative** quantity produces a meaningless
and possibly negative cost. A sale snapshots `a` at the moment of sale. A void reverses with the
**snapshotted** cost, not the current average.

**Negative stock** is Q4: refusing a sale at the till because a delivery was not booked in stops
trade; allowing it means a cost based on the last known average.

### 4.7 Debts

- `debt_entries` is an append-only ledger per customer **and currency**.
- A balance is `Σ amount_minor` per currency. **Balances in different currencies are never summed**
  into one figure without the operator asking for a conversion at a stated rate.
- A repayment may be in either currency. Repaying a USD debt in local cash records both the local
  amount received and the USD amount it settles, at the rate snapshot.

### 4.8 Business date

Every sale and ledger row carries `business_date CHAR(10)` (`YYYY-MM-DD` in the **shop's local
time zone**), stamped at write. Timestamps stay UTC. Daily profit groups by `business_date`,
because grouping a UTC timestamp by date puts a sale at 1 a.m. in Damascus on the previous day.

---

## 5. SQLITE SCHEMA

One migration, `internal/lite/migrations/sqlite/0001_lite.sql`, run by `platform/migrate`.

**Correction found while writing this section:** the runner does NOT create its own bookkeeping.
`schema_migrations` and `schema_lock` are created by Mizan's `0001_platform.sql`, and the runner
expects them to exist with that exact shape once version 1 is applied — and `platform/backup`
reads `schema_migrations` for its manifest. Mizan's `0001` cannot be split to share them, because
an applied migration's checksum is verified on every boot. So Lite's `0001` **carries the two
tables verbatim**, and a test asserts their statements are identical to Mizan's (comments aside), so the runner's
expectations cannot drift between editions.

Conventions inherited from Mizan's portability contract:
UUIDv7 `CHAR(36)` keys, `BIGINT` integers for every amount, `CHAR(24)` UTC timestamps from
`kernel/clock.Format`, booleans as `SMALLINT` with a CHECK, no `AUTOINCREMENT`, no SQLite-only
types, and **no UPDATE or DELETE on the four ledgers** (enforced by a test over the repository
SQL, §9.2, rather than by triggers whose syntax is not portable).

Suffixes carry the scale, so a reader never has to guess: `_minor` = currency minor units,
`_micro` = 10⁻⁶, `_nano` = 10⁻⁹.

```sql
-- ============================================================================================
-- 0001_lite.sql — Mizan Lite schema
-- ============================================================================================

-- Migration bookkeeping — statements VERBATIM from Mizan's 0001_platform.sql, asserted by a test.
CREATE TABLE schema_migrations (
  version      BIGINT       NOT NULL PRIMARY KEY,
  name         VARCHAR(200) NOT NULL,
  checksum     CHAR(64)     NOT NULL,               -- SHA-256 of the migration bytes
  applied_at   CHAR(24)     NOT NULL,
  duration_ms  BIGINT       NOT NULL DEFAULT 0
);

CREATE TABLE schema_lock (
  lock_id      INTEGER      NOT NULL PRIMARY KEY,
  is_locked    SMALLINT     NOT NULL DEFAULT 0,
  locked_at    CHAR(24),
  locked_by    VARCHAR(200),
  CONSTRAINT ck_schema_lock_single CHECK (lock_id = 1),
  CONSTRAINT ck_schema_lock_bool   CHECK (is_locked IN (0, 1))
);

INSERT INTO schema_lock (lock_id, is_locked) VALUES (1, 0);

-- Settings are DECLARED in Go (typed handles); a row exists only once a value is changed.
-- An unknown key is logged and ignored at load, never fatal (Mizan 0.5 D3).
CREATE TABLE settings (
  key         VARCHAR(64)  NOT NULL PRIMARY KEY,
  value       TEXT         NOT NULL,
  updated_at  CHAR(24)     NOT NULL
);

-- Currency codes are DATA (C8). Exactly two rows in v1; which one is "local" is a setting.
CREATE TABLE currencies (
  code            CHAR(3)      NOT NULL PRIMARY KEY,
  decimal_places  SMALLINT     NOT NULL CHECK (decimal_places BETWEEN 0 AND 4),
  rounding_mode   VARCHAR(16)  NOT NULL,              -- kernel/round.ParseMode
  created_at      CHAR(24)     NOT NULL
);

-- Units are seed data; display names live in the i18n catalog under "uom.<code>".
CREATE TABLE uoms (
  code            VARCHAR(16)  NOT NULL PRIMARY KEY,
  kind            VARCHAR(8)   NOT NULL CHECK (kind IN ('mass', 'volume', 'count')),
  input_decimals  SMALLINT     NOT NULL CHECK (input_decimals BETWEEN 0 AND 6),
  sort_order      SMALLINT     NOT NULL,
  CONSTRAINT ck_uom_count_is_whole CHECK (kind <> 'count' OR input_decimals = 0)
);

CREATE TABLE products (
  id                   CHAR(36)     NOT NULL PRIMARY KEY,
  name_ar              VARCHAR(200) NOT NULL,
  name_en              VARCHAR(200),                   -- NULL displays name_ar; never invented
  barcode              VARCHAR(64),
  uom_code             VARCHAR(16)  NOT NULL REFERENCES uoms(code),
  price_currency       CHAR(3)      NOT NULL REFERENCES currencies(code),
  sell_price_micro     BIGINT       NOT NULL CHECK (sell_price_micro >= 0),  -- per unit, 10⁻⁶ major
  avg_cost_usd_micro   BIGINT       NOT NULL DEFAULT 0 CHECK (avg_cost_usd_micro >= 0),
  on_hand_micro        BIGINT       NOT NULL DEFAULT 0,  -- cache of Σ stock_ledger; verified
  quick_slot           SMALLINT,                        -- NULL = not on the quick grid
  is_active            SMALLINT     NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
  row_version          BIGINT       NOT NULL DEFAULT 1,
  created_at           CHAR(24)     NOT NULL,
  updated_at           CHAR(24)     NOT NULL,
  CONSTRAINT ux_products_barcode    UNIQUE (barcode),     -- see portability note below
  CONSTRAINT ux_products_quick_slot UNIQUE (quick_slot)
);
CREATE INDEX ix_products_active_name ON products (is_active, name_ar);

CREATE TABLE customers (
  id           CHAR(36)     NOT NULL PRIMARY KEY,
  name         VARCHAR(200) NOT NULL,
  phone        VARCHAR(32),
  notes        VARCHAR(500),
  is_active    SMALLINT     NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
  row_version  BIGINT       NOT NULL DEFAULT 1,
  created_at   CHAR(24)     NOT NULL,
  updated_at   CHAR(24)     NOT NULL
);
CREATE INDEX ix_customers_active_name ON customers (is_active, name);

-- LEDGER 1 of 4. Append-only. Canonical direction: local currency per ONE US dollar (§4.3).
CREATE TABLE fx_rates (
  id                  CHAR(36)     NOT NULL PRIMARY KEY,
  local_currency      CHAR(3)      NOT NULL REFERENCES currencies(code),
  local_per_usd_nano  BIGINT       NOT NULL CHECK (local_per_usd_nano > 0),
  effective_at        CHAR(24)     NOT NULL,
  source              VARCHAR(16)  NOT NULL CHECK (source IN ('manual', 'fetched', 'seed')),
  fetch_log_id        CHAR(36)     REFERENCES fx_fetch_log(id),
  note                VARCHAR(200),
  created_at          CHAR(24)     NOT NULL,
  CONSTRAINT ck_fx_fetched_has_log CHECK (source <> 'fetched' OR fetch_log_id IS NOT NULL)
);
-- Resolution order: effective_at DESC, created_at DESC — a same-day correction wins (Mizan 0.9 D3).
CREATE INDEX ix_fx_rates_resolution ON fx_rates (local_currency, effective_at, created_at);

-- What the fetcher saw. Never read by pricing. Only an operator's Accept copies a row to fx_rates.
CREATE TABLE fx_fetch_log (
  id                  CHAR(36)     NOT NULL PRIMARY KEY,
  attempted_at        CHAR(24)     NOT NULL,
  provider            VARCHAR(32)  NOT NULL,
  outcome             VARCHAR(16)  NOT NULL CHECK (outcome IN ('ok', 'failed', 'rejected')),
  local_per_usd_nano  BIGINT,
  quoted_at           CHAR(24),                          -- the provider's timestamp, if any
  error_code          VARCHAR(64),                       -- an errs code, never provider prose
  CONSTRAINT ck_fetch_ok_has_rate CHECK (outcome <> 'ok' OR local_per_usd_nano > 0)
);
CREATE INDEX ix_fx_fetch_log_attempted ON fx_fetch_log (attempted_at);

CREATE TABLE sales (
  id                        CHAR(36)    NOT NULL PRIMARY KEY,
  receipt_no                BIGINT      NOT NULL,
  business_date             CHAR(10)    NOT NULL,         -- shop-local YYYY-MM-DD (§4.8)
  sold_at                   CHAR(24)    NOT NULL,
  status                    VARCHAR(8)  NOT NULL CHECK (status IN ('posted', 'voided')),
  payment                   VARCHAR(12) NOT NULL CHECK (payment IN ('cash_local', 'cash_usd', 'credit')),
  customer_id               CHAR(36)    REFERENCES customers(id),
  settlement_currency       CHAR(3)     NOT NULL REFERENCES currencies(code),

  -- The rate snapshot: the VALUE, not only the id (§4.3).
  fx_rate_id                CHAR(36)    NOT NULL REFERENCES fx_rates(id),
  local_currency            CHAR(3)     NOT NULL REFERENCES currencies(code),
  local_per_usd_nano        BIGINT      NOT NULL CHECK (local_per_usd_nano > 0),

  lines_local_minor         BIGINT      NOT NULL,         -- Σ sale_items.line_local_minor
  lines_usd_minor           BIGINT      NOT NULL,         -- Σ sale_items.line_usd_minor
  cash_rounding_minor       BIGINT      NOT NULL DEFAULT 0, -- in settlement_currency (§4.2)
  total_settlement_minor    BIGINT      NOT NULL,         -- what was charged
  cost_usd_minor            BIGINT      NOT NULL,         -- Σ sale_items.cost_usd_minor

  voided_at                 CHAR(24),
  void_reason               VARCHAR(200),
  created_at                CHAR(24)    NOT NULL,
  row_version               BIGINT      NOT NULL DEFAULT 1,

  CONSTRAINT ux_sales_receipt_no      UNIQUE (receipt_no),
  CONSTRAINT ck_sale_credit_has_party CHECK (payment <> 'credit' OR customer_id IS NOT NULL),
  CONSTRAINT ck_sale_void_is_complete CHECK (
    (status = 'posted' AND voided_at IS NULL     AND void_reason IS NULL) OR
    (status = 'voided' AND voided_at IS NOT NULL AND void_reason IS NOT NULL)),
  CONSTRAINT ck_sale_cash_usd_settles_usd CHECK (payment <> 'cash_usd' OR settlement_currency = 'USD')
);
CREATE INDEX ix_sales_business_date ON sales (business_date, status);
CREATE INDEX ix_sales_customer      ON sales (customer_id);

-- Every column a later edit to the product could change is SNAPSHOTTED here (Mizan §9.3).
CREATE TABLE sale_items (
  id                     CHAR(36)     NOT NULL PRIMARY KEY,
  sale_id                CHAR(36)     NOT NULL REFERENCES sales(id),
  line_no                SMALLINT     NOT NULL CHECK (line_no > 0),
  product_id             CHAR(36)     NOT NULL REFERENCES products(id),
  name_ar_snapshot       VARCHAR(200) NOT NULL,
  name_en_snapshot       VARCHAR(200),
  uom_code_snapshot      VARCHAR(16)  NOT NULL,
  quantity_micro         BIGINT       NOT NULL CHECK (quantity_micro > 0),
  price_currency         CHAR(3)      NOT NULL,
  list_price_micro       BIGINT       NOT NULL CHECK (list_price_micro >= 0),  -- catalogue price then
  unit_price_micro       BIGINT       NOT NULL CHECK (unit_price_micro >= 0),  -- price charged (Q3)
  line_local_minor       BIGINT       NOT NULL,   -- §4.4: both computed from ONE exact product,
  line_usd_minor         BIGINT       NOT NULL,   --       each rounded once
  unit_cost_usd_micro    BIGINT       NOT NULL CHECK (unit_cost_usd_micro >= 0),
  cost_usd_minor         BIGINT       NOT NULL,
  cost_local_minor       BIGINT       NOT NULL,   -- cost at the SALE's rate, so profit agrees (C6)
  CONSTRAINT ux_sale_items_line UNIQUE (sale_id, line_no)
);
CREATE INDEX ix_sale_items_product ON sale_items (product_id);

-- LEDGER 2 of 4. Append-only. products.on_hand_micro is a cache of Σ quantity_micro per product.
CREATE TABLE stock_ledger (
  id                         CHAR(36)     NOT NULL PRIMARY KEY,
  product_id                 CHAR(36)     NOT NULL REFERENCES products(id),
  business_date              CHAR(10)     NOT NULL,
  occurred_at                CHAR(24)     NOT NULL,
  kind                       VARCHAR(12)  NOT NULL
                               CHECK (kind IN ('opening', 'receipt', 'sale', 'sale_void', 'adjustment')),
  quantity_micro             BIGINT       NOT NULL CHECK (quantity_micro <> 0),   -- signed
  unit_cost_usd_micro        BIGINT       NOT NULL CHECK (unit_cost_usd_micro >= 0),
  avg_cost_after_usd_micro   BIGINT       NOT NULL CHECK (avg_cost_after_usd_micro >= 0),
  on_hand_after_micro        BIGINT       NOT NULL,

  -- A receipt entered in local currency keeps what was TYPED and the rate used (§4.2).
  entered_currency           CHAR(3)      REFERENCES currencies(code),
  entered_unit_cost_micro    BIGINT,
  local_per_usd_nano         BIGINT,

  sale_id                    CHAR(36)     REFERENCES sales(id),
  reason                     VARCHAR(200),
  created_at                 CHAR(24)     NOT NULL,

  CONSTRAINT ck_stock_sign_matches_kind CHECK (
    (kind IN ('opening', 'receipt', 'sale_void') AND quantity_micro > 0) OR
    (kind = 'sale'                               AND quantity_micro < 0) OR
    (kind = 'adjustment')),
  CONSTRAINT ck_stock_sale_links_sale CHECK ((kind IN ('sale', 'sale_void')) = (sale_id IS NOT NULL)),
  CONSTRAINT ck_stock_adjustment_has_reason CHECK (kind <> 'adjustment' OR reason IS NOT NULL),
  CONSTRAINT ck_stock_entered_cost_complete CHECK (
    (entered_currency IS NULL AND entered_unit_cost_micro IS NULL AND local_per_usd_nano IS NULL) OR
    (entered_currency IS NOT NULL AND entered_unit_cost_micro IS NOT NULL AND local_per_usd_nano > 0))
);
CREATE INDEX ix_stock_ledger_product ON stock_ledger (product_id, occurred_at);
CREATE INDEX ix_stock_ledger_date    ON stock_ledger (business_date);

-- LEDGER 3 of 4. Append-only, per customer AND currency. Balances are never summed across currencies.
CREATE TABLE debt_entries (
  id                  CHAR(36)     NOT NULL PRIMARY KEY,
  customer_id         CHAR(36)     NOT NULL REFERENCES customers(id),
  business_date       CHAR(10)     NOT NULL,
  occurred_at         CHAR(24)     NOT NULL,
  kind                VARCHAR(12)  NOT NULL
                        CHECK (kind IN ('opening', 'charge', 'charge_void', 'payment')),
  currency            CHAR(3)      NOT NULL REFERENCES currencies(code),
  amount_minor        BIGINT       NOT NULL CHECK (amount_minor <> 0),   -- + owed, − paid

  -- A payment made in the OTHER currency keeps what was handed over and the rate (§4.7).
  tendered_currency   CHAR(3)      REFERENCES currencies(code),
  tendered_minor      BIGINT,
  local_per_usd_nano  BIGINT,

  sale_id             CHAR(36)     REFERENCES sales(id),
  note                VARCHAR(200),
  created_at          CHAR(24)     NOT NULL,

  CONSTRAINT ck_debt_sign_matches_kind CHECK (
    (kind IN ('opening', 'charge') AND amount_minor > 0) OR
    (kind IN ('charge_void', 'payment') AND amount_minor < 0)),
  CONSTRAINT ck_debt_charge_links_sale CHECK ((kind IN ('charge', 'charge_void')) = (sale_id IS NOT NULL)),
  CONSTRAINT ck_debt_tender_complete CHECK (
    (tendered_currency IS NULL AND tendered_minor IS NULL AND local_per_usd_nano IS NULL) OR
    (tendered_currency IS NOT NULL AND tendered_minor > 0 AND local_per_usd_nano > 0))
);
CREATE INDEX ix_debt_entries_customer ON debt_entries (customer_id, currency, occurred_at);
```

**Ledger 4 of 4** is `sales` itself: its only permitted update is `posted → voided`, asserted by
the same SQL scan.

**Notes on the schema:**

- **`opening` kinds exist on purpose.** A shop adopting Lite already has stock on the shelves and
  a paper debt book. Without an opening kind, existing debts would have to be entered as fake
  sales and existing stock as fake receipts — both of which corrupt profit.
- **Portability note — nullable UNIQUE.** `ux_products_barcode` and `ux_products_quick_slot` allow
  many NULLs in SQLite and PostgreSQL; SQL Server treats NULLs as equal in a UNIQUE constraint and
  would allow only one. Recorded rather than solved, because SQL Server is not on the roadmap.
- **`fx_rates` references `fx_fetch_log`**, so the migration creates `fx_fetch_log` first; the
  listing above is in reading order, not execution order.
- **No `debts` table holding a balance.** A stored balance is a second source of truth that can
  disagree with the entries. It is computed, and indexed so computing it is cheap.
- **`products.on_hand_micro` IS a stored balance**, deliberately, because the POS grid reads it on
  every keystroke. It is written only in the same transaction as its ledger row, and a verifier
  compares it to the ledger (§4.5).

---

## 6. GO ARCHITECTURE

### 6.1 Layout

```
apps/lite/                          Wails project (spike in L0 proves it builds from here)
├── wails.json
├── main.go                         package main — ~30 lines: bootstrap.Run(), wails.Run(Bind: …)
├── build/                          icons, Info.plist, NSIS config
└── frontend/                       Vite + React + TS + Tailwind (its own package.json)
    ├── wailsjs/                    GENERATED by Wails — the only oracle for the namespace
    └── src/
        ├── api/client.ts           the ONLY importer of wailsjs/ (§7)
        ├── api/client.gates.test.ts
        ├── app/routes.ts           routes as data: path, screen, nav group
        ├── screens/…               one folder per route
        ├── ui/…                    primitives (copied from Mizan at L0 — D7)
        └── i18n/                   loader; catalogs live in ../../locales-lite

cmd/lite-demoseed/main.go           package main — drives the REAL services (§9.4)

internal/lite/
├── bootstrap/                      the composition root: open DB, migrate, build services, Bind list
├── api/                            package api → window.go.api — thin façades, envelopes only
├── catalog/                        products, units
├── stock/                          ledger, weighted-average cost, verifier
├── fx/                             rates, resolution, fetch log, Source port
│   └── httpsource/                 the optional HTTP adapter (D5, Q1)
├── customers/                      customers + debt ledger
├── pos/                            checkout, void, quote — the ONE orchestrator
├── reports/                        profit, cash summary (read-only)
├── receipt/                        receipt rendering (thermal / A4)
├── settings/                       typed settings over the settings table
└── migrations/sqlite/0001_lite.sql go:embed'd

locales-lite/{ar,en}/{common,errors,uom}.json   go:embed'd AND imported by Vite (Mizan 0.8)
```

Each module is layered as Mizan's are: `domain` (pure types and rules, no I/O) → the service
(orchestrates, owns transactions) → `infra/sqlite` (the only package holding SQL).

### 6.2 Boundaries, enforced by archlint

New rules in `arch-rules.yml`:

| Rule | Why |
|---|---|
| `internal/lite/**` may import only `internal/kernel/**`, the §3.1 platform packages, `internal/api/envelope`, and `internal/lite/**` | Lite cannot quietly grow a dependency on a Mizan business module |
| Nothing outside `internal/lite/**`, `apps/lite`, `cmd/lite-demoseed` may import `internal/lite/**` | Mizan cannot depend on Lite |
| Within Lite: **only `pos`** may import `catalog`, `stock`, `fx`, `customers`; those four import none of each other | One orchestrator. Mizan uses ports between all modules; Lite has one caller, and a port with one implementation and one caller is ceremony |
| `reports` may import any Lite module's **read** interfaces only | Reports cannot write |
| SQL strings only in `infra/sqlite` packages | Mizan's `no-sql` rule, reapplied |

Each rule is **verified by planting** a violation during L0 — Mizan's practice, because a rule
never seen failing may not match anything.

### 6.3 Settings

Mizan's `platform/config` resolves through a six-level scope chain with authorization and change
notification ports. Lite has one shop and one operator, so it declares typed handles over the
`settings` table directly — **the same idea (Mizan 0.5 D1: call sites hold no key strings), without
the scope chain.**

```go
package settings

var (
    Locale            = declare("ui.locale", EnumOf("ar", "en"), "ar")
    Digits            = declare("ui.digits", EnumOf("arabic_indic", "latin"), "latin") // Q6
    LocalCurrency     = declare("currency.local", CurrencyCode, "SYP")               // C8: data
    CashIncrement     = declare("currency.local_cash_increment_minor", PositiveInt, 1) // Q5
    DebtCurrency      = declare("debt.default_currency", EnumOf("local", "usd"), "usd") // Q2
    AllowNegativeStock = declare("stock.allow_negative", Bool, true)                // Q4
    StaleAfterHours   = declare("fx.stale_after_hours", PositiveInt, 24)
    FetchEnabled      = declare("fx.fetch.enabled", Bool, false)                    // C4: OFF
    FetchProvider     = declare("fx.fetch.provider", String, "")                    // Q1
    ReceiptsEnabled   = declare("receipts.enabled", Bool, false)                    // C5
    ReceiptFormat     = declare("receipts.format", EnumOf("thermal_80", "a4"), "thermal_80")
)
```

The `settings` binding reads and writes a **typed struct**, not key/value pairs, so the frontend
also holds no key strings.

### 6.4 The contracts

The binding surface is small enough to list completely — **and it must be listed**, because this
list is what §7's gates compare against.

```go
package api // window.go.api — every method returns envelope.Result[T]

type App      struct{ … } // Health, FirstRunStatus, CompleteFirstRun
type Settings struct{ … } // Get, Update
type Catalog  struct{ … } // Units, Products, Product, CreateProduct, UpdateProduct, SetQuickSlot, DeactivateProduct
type Stock    struct{ … } // Receive, Adjust, Ledger, Verify
type FX       struct{ … } // Current, History, SetManual, FetchNow, AcceptFetched
type POS      struct{ … } // QuickGrid, Search, Quote, Checkout, Void, Sale, SalesOn
type Customers struct{ … } // List, Create, Update, Statement, RecordPayment, RecordOpening, Outstanding
type Reports  struct{ … } // DailyProfit, MonthlyProfit, ProductProfit, CashSummary
type Receipts struct{ … } // Render
type Backups  struct{ … } // List, TakeNow, Restore
```

**43 methods.** Every one has a client function, and every client function has a screen caller
(§7).

**DTOs cross the boundary as strings.** A money amount is `{ currency: "SYP", minor: "21645" }`; a
quantity is `{ uom: "kg", micro: "1750000" }`. `Quote` exists so that **the frontend computes
nothing**: the cart sends product ids and quantity strings, Go returns every figure the till
displays. Mizan 10.19 computes a dual-currency display line in BigInt in the browser as a
convenience; Lite removes the need, so there is no JavaScript money arithmetic to get wrong. A
quote round-trip is a local IPC call, and the till debounces it.

### 6.5 The checkout, in code

```go
package pos

type Payment string

const (
    PayCashLocal Payment = "cash_local"
    PayCashUSD   Payment = "cash_usd"
    PayCredit    Payment = "credit"
)

type CheckoutInput struct {
    Lines      []LineInput // product id + quantity string ("1.750"), already digit-normalised (§8.3)
    Payment    Payment
    CustomerID *id.ID      // required iff Payment == PayCredit
    QuoteToken string      // the rate + prices the operator SAW; see below
}

// Checkout records a sale, moves stock and — on credit — charges the customer, atomically.
//
// QuoteToken carries the rate id and each product's row_version from the Quote the operator saw.
// If the rate in force or any price changed between the quote and the click, Checkout refuses with
// errs.CodeQuoteStale and the till re-quotes. Without it, an operator accepts a fetched rate while
// a cashier's cart is open, and the customer is charged a total nobody showed them.
func (s *Service) Checkout(ctx context.Context, in CheckoutInput) (Receipt, error) {
    var receipt Receipt
    err := s.db.Do(ctx, func(ctx context.Context) error {
        rate, err := s.fx.InForce(ctx)                      // 1. rate + its age
        if err != nil {
            return err
        }
        lines, err := s.priceLines(ctx, in.Lines, rate)     // 2. §4.4, one rounding per currency
        if err != nil {
            return err
        }
        if err := verifyQuote(in.QuoteToken, rate, lines); err != nil {
            return err
        }
        sale, err := s.assemble(ctx, in, rate, lines)       // 3. totals + cash rounding, recorded
        if err != nil {
            return err
        }
        if sale.ReceiptNo, err = s.sales.NextReceiptNo(ctx); err != nil { // 4. single writer
            return err
        }
        if err := s.sales.Insert(ctx, sale); err != nil {   // 5.
            return err
        }
        for _, line := range sale.Lines {                   // 6.
            if err := s.stock.ApplySale(ctx, sale, line); err != nil {
                return err
            }
        }
        if in.Payment == PayCredit {                        // 7.
            if err := s.customers.Charge(ctx, *in.CustomerID, sale); err != nil {
                return err
            }
        }
        receipt = toReceipt(sale)
        return nil
    })                                                       // 8. commit, or nothing at all
    return receipt, err
}
```

### 6.6 The weighted average, in code

```go
package domain // internal/lite/stock/domain

// AverageAfterReceipt returns the weighted-average USD unit cost after receiving qty at unitCost.
//
// When nothing is on hand — or less than nothing, because a sale ran ahead of a delivery — there
// is no history worth averaging and the receipt's own cost becomes the average. Averaging against
// a negative quantity produces a cost with no meaning, and possibly a negative one.
func AverageAfterReceipt(onHand, avg, qty, unitCost int64) (int64, error) {
    if qty <= 0 {
        return 0, errs.Validation(CodeReceiptQuantity, "receipt quantity must be positive")
    }
    if onHand <= 0 {
        return unitCost, nil
    }
    // (onHand×avg + qty×unitCost) / (onHand+qty) — in big.Int, rounded ONCE, HalfUp.
    return money.WeightedAverageUnit(onHand, avg, qty, unitCost, round.HalfUp)
}
```

`money.WeightedAverageUnit` **does not exist yet** — it is the second kernel addition (D6),
written in L2 over `kernel/internal/fixed`, whose exported surface today is checked `int64`
arithmetic, `big.Int` conversion and scaled parsing. It belongs in the kernel rather than in Lite
because Mizan's own weighted-average costing performs the same division, and one implementation
with one property test is the kernel's reason to exist.

### 6.7 The FX fetcher

```go
package fx

// Source is one provider's view of the rate. It knows nothing about the database, and nothing it
// returns is used for pricing until an operator accepts it.
type Source interface {
    Name() string
    Fetch(ctx context.Context) (Quote, error)
}

type Quote struct {
    LocalPerUSDNano int64
    QuotedAt        time.Time // the provider's own timestamp; zero if it gives none
}
```

- **No `Source` is registered unless `fx.fetch.enabled` is true.** The composition root does not
  construct `httpsource` at all otherwise — so "the app makes no network call" is a property of
  the object graph, not of an `if` inside a loop, and a test can assert it (§9.2).
- The fetch runs on `platform/jobs`… **or not**: `jobs` reads and writes Mizan's `jobs` and
  `job_runs` tables, which Lite's schema does not carry. The choice is between adding those two
  tables (with the same verbatim-and-asserted treatment as the migration bookkeeping) and a single
  goroutine with a ticker, capped backoff and a context. Lite has two periodic tasks — the fetch
  and the daily backup — so reusing `jobs` is my current expectation, because its catch-up policy
  and panic handling are already proven (§11, D8).
- **A plausibility band:** a quote more than `fx.fetch.max_jump_pct` (default 20%) away from the
  rate in force is logged as `rejected` and shown as a warning, never offered as a one-click
  accept. A provider returning the official rate, or a decimal-point error, cannot be accepted by
  reflex.
- Every attempt, including failures, is one `fx_fetch_log` row with an error **code**, never
  provider prose — the envelope rule.

---

## 7. THE BINDING BRIDGE — SIX GATES

The brief's requirement is "strict 1:1 mapping". Mizan's history shows what that has to mean in
practice, because each of its connection defects slipped through a **different** gap:

| Mizan defect | The gap it went through | Lite gate |
|---|---|---|
| 10.17: `window.go.main` vs `window.go.bindings` | A namespace **string** asserted by nobody | **G1** |
| 10.13: seven write bindings with no client function | Only a manual audit looked | **G2** |
| 10.18: the FX engine and export bindings with a client function but **no screen** | Mizan's gate checks Go→client, not client→screen | **G3** |
| 10.9: eleven screens with no route | Routes and screens listed separately | **G4** |
| 10.17: a packaged build silently serving mock data | The fallback was unconditional | **G5** |

And one gap no Mizan gate covers: **a bound struct left out of Wails' `Bind:` list generates no
files, so a gate that reads the generated files passes while checking nothing.** Mizan named that
failure in 7.6 (D162: *"a gate whose input is missing passes while checking nothing"*). **G0**
closes it.

### G0 — every façade is bound (Go test)

Parses `internal/lite/api` for exported struct types with methods, parses `bootstrap`'s `Bind`
slice, and fails unless the two sets are equal. It also fails if it finds fewer than ten structs,
so a broken parse cannot pass.

### G1 — nothing types a namespace (lint + TypeScript)

- `src/api/client.ts` is the **only** file that imports from `wailsjs/`. An ESLint
  `no-restricted-imports` rule forbids `wailsjs/**` everywhere else.
- `window.go` is forbidden **everywhere, including `client.ts`** (Mizan's `noWailsGlobals` rule,
  without Mizan's exemption for `bridge.ts`). The generated files reach `window.go`; our code never
  does, so there is no string to get wrong.
- Because `client.ts` imports **typed functions**, renaming or removing a Go method **fails
  `tsc`**, before any test runs. Mizan's `call<T>("Catalog", "UpdateProduct")` is a string pair
  that compiles regardless.

### G2 — every Go method has a client function (Go test)

For every exported method of every bound struct — **reads included**, where Mizan's gate checks
only methods starting with a write verb — the test requires `client.ts` to reference it.

```go
// internal/lite/api/gates_test.go
func TestEveryBoundMethodHasAClientFunction(t *testing.T) {
    methods := boundMethods(t)              // from the AST of internal/lite/api, e.g. "POS.Checkout"
    if len(methods) < 40 {
        t.Fatalf("found %d bound methods; the parser is not matching", len(methods))
    }
    client := read(t, "apps/lite/frontend/src/api/client.ts")
    for _, m := range methods {
        // client.ts imports each façade as a namespace: import * as POS from "../../wailsjs/go/api/POS"
        // and calls POS.Checkout(...). Both halves are matched, so a method name shared by two
        // façades cannot satisfy the check for the wrong one.
        if !regexp.MustCompile(`\b` + m.Struct + `\.` + m.Name + `\(`).MatchString(client) {
            t.Errorf("%s is bound and nothing in client.ts calls it — unreachable or dead", m)
        }
    }
}
```

### G3 — every client function has a screen caller (Vitest, TypeScript compiler API)

Uses the `typescript` package already in the toolchain — no new dependency — to list every
export of `client.ts` and find its references under `src/screens/` and `src/app/`. An export with
no reference fails. **This is the gate Mizan does not have**, and the one that would have caught
10.18's FX engine: it had a client function for two phases before it had a screen.

The receipt button (C5) satisfies this gate the honest way: the caller exists, rendered behind
`receipts.enabled`.

### G4 — every screen is routed, every route is in navigation (Vitest)

Routes are data (`src/app/routes.ts`), and the router and the sidebar render from the same list —
Mizan's 10.13 design. The gate adds the other direction: every component under `src/screens/*/`
exporting a `*Screen` must appear in `routes.ts`.

### G5 — no mock can reach a packaged build (Vitest over the built bundle)

- Lite ships **no runtime mock at all**. Component tests inject a fake client through a React
  context; there is no fallback path in `client.ts` to take.
- If `window.go` is absent, `client.ts` throws `CODE_BRIDGE_UNAVAILABLE`, and the app shows a full
  screen error. **An error is a bug report; fake data is every number on screen quietly wrong**
  (Mizan 10.17).
- A test builds the frontend (`vite build`) and fails if the bundle contains the sentinel string
  planted in the test-only fake client — proof by artefact, not by reading `import.meta.env`.

### 7.1 The client, concretely

```ts
// apps/lite/frontend/src/api/client.ts — the ONLY importer of wailsjs/.
import * as POS from "../../wailsjs/go/api/POS";
import * as FX from "../../wailsjs/go/api/FX";
// … one namespace import per façade; G2 fails if one is missing.
import type { api } from "../../wailsjs/go/models";
import { unwrap } from "./envelope";

/** A money amount as it crosses the boundary: never a JavaScript number. */
export type Amount = { currency: string; minor: string };

export const client = {
  pos: {
    quote: (cart: api.QuoteInput) => unwrap(POS.Quote(cart)),
    checkout: (input: api.CheckoutInput) => unwrap(POS.Checkout(input)),
    void: (saleId: string, reason: string) => unwrap(POS.Void(saleId, reason)),
    // …
  },
  fx: {
    current: () => unwrap(FX.Current()),
    setManual: (input: api.SetRateInput) => unwrap(FX.SetManual(input)),
    acceptFetched: (fetchLogId: string) => unwrap(FX.AcceptFetched(fetchLogId)),
    // …
  },
} as const;
```

`unwrap` is Mizan's `call<T>` without the lookup: it takes the promise Wails returns, turns a
transport failure into `CODE_CALL_FAILED`, a non-envelope into `CODE_MALFORMED_RESPONSE`, and an
`ok: false` envelope into a thrown `BindingError` carrying only a **code** — the message is looked
up in the catalog, so no English crosses the boundary.

---

## 8. BILINGUAL HANDLING

### 8.1 Direction

- **Arabic is the default locale and RTL the default direction.** `<html lang dir>` is set from
  `ui.locale` **before first paint**, from a value Go passes at startup — not after an async
  settings call, which would flash an LTR layout first.
- **Logical properties only** (`ms-`/`me-`, `ps-`/`pe-`, `text-start`). Mizan's
  `noPhysicalDirection` ESLint rule is copied as-is; it has held since 10.3.
- **Icons with direction** (back, next, a cart arrow) flip via one `rtl:-scale-x-100` utility in
  one `DirectionalIcon` component; icons without direction never flip.
- Every screen is mounted under **both** directions in tests (Mizan 10.3 D2: proves it mounts, not
  that it looks right — §10's DoD includes looking at it).

### 8.2 Mixed-direction text

A receipt line reads *"زيت زيتون 16L — 2 × 185,000 ل.س"*. Without isolation, the Unicode bidi
algorithm can reorder `16L`, `2 ×` and the amount into a line that reads wrong **while every
character is correct**. Every number, quantity, unit symbol and product name rendered inside
translated text passes through one `<Bidi>` component (`<bdi>`), and the receipt renderer in Go
wraps the same runs in FSI/PDI (U+2068 / U+2069).

### 8.3 Digits and number input — the most likely bug in this product

A phone or a keyboard set to Arabic produces **Arabic-Indic digits** (`١٫٧٥٠`) and the **Arabic
decimal separator** `٫` (U+066B). Persian keyboards produce **Extended Arabic-Indic** digits
(`۱۲۳`). An input that parses only `0-9` and `.` will reject `١٫٧٥٠`, or worse, a lenient parser
will read `1` and drop the rest.

**Rule:** every quantity, price and rate input passes through one `normaliseNumber` function
before it becomes a string DTO, and **Go normalises again** — a boundary must not trust its caller:

| Input | Normalised |
|---|---|
| `١٫٧٥٠` | `1.750` |
| `۱.۵` | `1.5` |
| `1,750` in an Arabic locale | **refused** — `,` and `٬` are grouping separators, and guessing turns 1,750 kg into 1.75 kg |
| `1.7500` for a `kg` product | refused — exceeds the unit's input decimals |
| `0.5` for a `jar` product | refused — count units are whole (§4.1) |

The function has a property test over all three digit sets, and the refusal cases are fixed
examples.

**Display** is a setting (Q6): Arabic-Indic or Latin digits in Arabic. Formatting works on the
**integer string** from Go — group, place the decimal point by the currency's or unit's scale, then
map digits — never through `Number`, and `Intl.NumberFormat` is used only for the grouping and
separator **symbols** of the locale, never for the value.

### 8.4 Catalogs and the "no hardcoded text" gate

- `locales-lite/{ar,en}/{common,errors,uom}.json`, embedded by Go and imported by Vite — Mizan
  0.8's single catalog, so a receipt printed by Go and a screen rendered by React cannot use
  different words.
- **Key parity gate:** both locales have exactly the same keys.
- **Error coverage gate:** every `Code*` constant under `internal/lite` has a translation in both
  locales — Mizan 0.8's AST walk, which caught four missing codes on its first run.
- **Literal text gate (new):** a Vitest check using the TypeScript compiler API fails on any JSX
  text node or string-literal `aria-label`/`title`/`placeholder` containing a letter in
  `src/screens` or `src/ui`. Mizan has no equivalent — its protection is review.
- **No pluralisation machinery in v1**, for Mizan 0.8's reason: Arabic has six plural forms, and a
  naive `count === 1` is silently wrong. Strings are written to avoid needing plurals.

---

## 9. TESTING STRATEGY

### 9.1 Principles carried from Mizan

- **Mutation drills:** every invariant test is proven able to fail by temporarily breaking the
  code it guards. Mizan ran 255; about one in seven passed first time, and every such pass changed
  something. In 7.4, **two tests were calling `t.Skipf` on every run** — green and asserting
  nothing.
- **Fixtures that exercise the failing case.** Mizan 10.16's receivables tile was wrong in a way
  its fixture could not show until it held an over-settled invoice. Every Lite fixture names the
  case it exists to exercise.
- **Tests of checks must include the case the check exists to find.**

### 9.2 Go

| Area | Tests (names are the contract) |
|---|---|
| **Kernel additions** | `TestLineExtensionConvertedRoundsOnce` (the §4.4 example) · property: Σ of N converted lines vs. the whole, bounded by N × one minor unit · `TestWeightedAverageUnit` property: result between old average and receipt cost |
| **Catalog** | `TestCountUnitRefusesFractionalQuantity` · `TestUnitCannotChangeAfterFirstMovement` · `TestRenameDoesNotRewriteSaleSnapshot` |
| **Stock** | `TestReceiptUpdatesAverageAndOnHandInOneTransaction` · `TestReceiptAfterNegativeStockTakesReceiptCost` · `TestLocalCurrencyReceiptKeepsEnteredCostAndRate` · `TestVerifierReportsDriftAndDoesNotRepair` (fixture plants a drifted `on_hand_micro`) |
| **FX** | `TestSameDayCorrectionWins` · `TestInForceReportsAgeOnEveryPath` · `TestFetchedQuoteIsNeverInForceUntilAccepted` · `TestQuoteOutsidePlausibilityBandIsRejected` · **`TestDisabledFetcherMakesNoNetworkCall`** — the transport is an `http.RoundTripper` that fails the test if dialled |
| **POS** | `TestCheckoutIsAtomic` — a customer charge forced to fail leaves zero sales, items, ledger rows and on-hand changes · `TestCreditSaleWithoutCustomerIsUnrepresentable` (the CHECK, hit with a raw insert) · `TestCashRoundingIsRecordedNotLost` · `TestStaleQuoteIsRefused` · `TestVoidReversesStockAtSnapshottedCost` · `TestVoidIsRecordedNeverDeleted` · `TestReceiptNumbersAreGapFreeUnderConcurrentCheckouts` |
| **Customers** | `TestBalancesAreNeverSummedAcrossCurrencies` · `TestPaymentInOtherCurrencyKeepsTender` · `TestOpeningBalanceIsNotASale` |
| **Reports** | `TestUsdAndLocalProfitAgreeAtTheSaleRate` (the C6 table as a fixture) · `TestDailyProfitGroupsByBusinessDateNotUTC` (a sale at 01:00 Damascus) · `TestVoidedSalesAreExcluded` · `TestReportsAreReadOnly` |
| **Structure** | G0 · G2 · `TestLedgersHaveNoUpdateOrDelete` (scans `infra/sqlite` SQL) · `TestMigrationBookkeepingMatchesMizan` · the error-code coverage gate · archlint rules planted and seen failing |

All database tests run against a real SQLite file through `platform/database`, never a mock.

### 9.3 Frontend

| Area | Tests |
|---|---|
| **Gates** | G1 (lint + `tsc`) · G3 · G4 · G5 · key parity · literal text |
| **Numbers** | `normaliseNumber` property test over three digit sets · refusal table (§8.3) · formatting from integer strings at 0, 2 and 3 decimals |
| **POS** | quick-grid click adds a line · weight product opens the quantity pad, count product adds one · checkout disabled while a quote is in flight · stale-quote error re-quotes · credit requires a customer · receipt button absent when `receipts.enabled` is false and present when true |
| **Direction** | every routed screen mounts under `ar`/RTL and `en`/LTR · `<html dir>` is correct on the first render |
| **Header** | rate shows age · stale warning past the threshold · fetched proposal shows Accept · rejected proposal shows a warning and no Accept |

### 9.4 The demo seeder

`cmd/lite-demoseed`, built on the reasoning in Mizan's untracked `cmd/demoseed`: **it boots the
real composition root and calls the services**, never INSERTs, so every figure on every screen was
reached the way a shop's would be — and the verifiers agree with it.

- **Refuses a database that has been set up.** Re-seeding means deleting the directory.
- **Deterministic:** `-seed 42` produces the same shop every time, so a screenshot or a bug report
  can be reproduced.
- **What it builds:** ~40 bilingual pantry products (olive oil loose by L and in 16 L tins,
  pomegranate molasses, carob and grape syrups, labneh, white cheese, ghee, za'atar, sumac,
  bulgur, freekeh, lentils, chickpeas, jarred makdous and pickles) · opening stock with USD costs ·
  30 days of receipts and sales at a **rate that drifts** across the month, so the USD and local
  profit figures visibly diverge from a naïve reading · 8 customers, with credit sales, partial
  repayments in both currencies and opening balances · a same-day rate correction · two voided
  sales · one fetched-and-rejected quote.
- **It grows with every phase.** Each phase's DoD includes "the seeder exercises it and the
  verifiers pass afterwards", so the seeder is an end-to-end test that runs the way a shop does.

---

## 10. ROADMAP

Each phase follows the protocol: design note → **approval** → implementation → tests and drills →
self-review → approval for the next. Every phase's Definition of Done includes the same four lines:

> `make lite-ci` green (Go, archlint, lint, `tsc`, Vitest, all gates) · every new invariant
> mutation-drilled · the seeder exercises the phase and the verifiers pass afterwards ·
> **the packaged app was opened and the phase's screens were looked at, in Arabic and English**.

The last line is there because Mizan carried *"sixteen screens, none looked at"* as an open item
from Phase 0 to Phase 10, and the one defect that mattered most — 10.17 — was visible the moment
somebody opened the packaged app.

| Phase | Scope | Definition of Done (beyond the four lines) |
|---|---|---|
| **L0 — Skeleton and gates** | **Spike first:** two Wails projects, one Go module. Composition root, `0001_lite.sql`, own data directory (D10), archlint rules, **G0–G5 built against a one-method surface** (`App.Health`), catalogs + parity gates, direction before first paint, backups wired, `make lite-ci` | Each gate and archlint rule **seen failing** by planting. The packaged macOS app shows `Health` **from Go**, in Arabic, RTL. |
| **L1 — Catalogue and units** | Units seed, products CRUD, quick-grid slots, `normaliseNumber` (TS + Go), product screens | Count unit refuses `0.5` at both layers; Arabic-Indic input accepted |
| **L2 — Stock** | Opening, receipt, adjustment, ledger, `WeightedAverageUnit` (kernel), verifier, stock screens | Verifier drill: a planted drift is reported and not repaired |
| **L3 — Exchange rates (manual)** | `fx_rates`, resolution with age, header rate + manual override, history screen, `LineExtensionConverted` (kernel) | Same-day correction wins; §4.4 example is a passing test |
| **L4 — The till** | Quote, checkout (cash local, cash USD), cash rounding, receipt numbers, void, quick grid, search, today's sales | Atomicity drill; stale-quote drill; a 60-line receipt's totals agree with a whole-receipt computation |
| **L5 — Customers and debts** | Credit sales, repayments in either currency, opening balances, statements, outstanding list | Balances never summed across currencies; the paper-book adoption path works end to end |
| **L6 — Profit** | Daily, monthly and per-product profit in USD and local currency; daily cash summary per payment kind | The C6 table is a fixture and passes; business date, not UTC |
| **L7 — Fetcher and receipts** | Only if Q1 names a source: `httpsource`, proposal + accept, plausibility band. Receipt rendering, thermal 80 mm and A4, behind `receipts.enabled` | No-network drill with the fetcher disabled; bidi isolation on an Arabic receipt with a Latin unit |
| **L8 — Release** | Installers (Windows NSIS, macOS DMG), seeder complete, a DoD review in Mizan's form, **one pilot shop for one week** | The review names every criterion it failed rather than rewording it |

**The order is deliberate.** Gates come before features (L0), because every Mizan connection
defect was cheaper to prevent than to find. Rates come before the till (L3 before L4), because a
till built against a hardcoded rate hides every conversion defect until rates arrive. The fetcher
comes last (L7), because the product is fully usable with a manual rate and the fetcher is the one
feature that depends on an answer nobody has yet.

---

## 11. DECISIONS AND QUESTIONS AWAITING APPROVAL

### 11.1 Decisions — approve, amend, or reject each

| # | Decision | Recommendation | § |
|---|---|---|---|
| **D1** | Fork strategy | **Option B** — a second app in the same Go module, reusing kernel and platform | 2 |
| **D2** | Scope | Strip per §3.2; **add** receive, adjust, repay, void and opening balances per §3.3 | 3 |
| **D3** | Language | Arabic default and RTL default; `name_ar` required, `name_en` optional and never invented | 5, 8 |
| **D4** | Cost and profit | Unit cost held in **USD**; profit reported in USD **and** local currency at the sale's own rate | 1.2 C6, 4.6 |
| **D5** | FX fetcher | **Off by default**; writes proposals; one-click accept; ±20% plausibility band; no provider chosen by me | 1.2 C4, 6.7 |
| **D6** | Kernel additions | `LineExtensionConverted` and `WeightedAverageUnit` in `kernel/money`, available to Mizan | 4.4, 6.6 |
| **D7** | UI primitives | **Copy** Mizan's primitives into Lite at L0 rather than share them. Sharing across two Vite projects means workspace tooling and a coupled Tailwind config; Lite's till needs larger touch targets anyway. **Cost:** two copies that will drift, accepted because the two UIs serve different hands | 6.1 |
| **D8** | Scheduling | Reuse `platform/jobs`, carrying its two tables verbatim with an equivalence test | 6.7 |
| **D9** | Frontend arithmetic | **None.** Every displayed figure comes from Go through `Quote` | 6.4 |
| **D10** | Data directory | Lite uses its **own** app-data directory and database file, so Mizan and Lite can be installed on one machine without touching each other's data. `platform/paths` is to be checked in L0 for an app-name parameter | 6.1 |

### 11.2 Questions only you can answer

**Q1 — Which exchange rate does a shop trust, and where does it come from?** Name a source for the
market rate, or confirm **manual only for v1**. Until a source is named, L7's fetcher is not built:
an adapter written against a guessed provider is a feature that reprices a shop from a website
nobody chose. *My recommendation: manual only for v1.*

**Q2 — What currency are debts recorded in?** USD (protects the shop against devaluation), local
currency (what the customer expects to hear), or chosen per sale with a default? *The schema
supports all three; the default is the question.*

**Q3 — What does "Expected profit vs. Actual profit" mean?** Two readings, needing different things:
- **(a) List price vs. charged price.** Expected is what the sale would have earned at the
  catalogue price; actual is what was charged. They differ only if the till allows **changing a
  price at the counter** — so: may the operator change a price at the till?
- **(b) Stock potential vs. realised.** Expected is what the stock on the shelf would earn if sold
  at today's prices; actual is what sales earned.

**Q4 — May the till sell more than is recorded in stock?** *Recommended: yes, with a visible
warning* — refusing a sale because a delivery was not booked stops trade. The cost used is the last
known average.

**Q5 — What is the smallest amount of local currency actually handed over?** That is the cash
rounding increment. **And: what is the current status of the pound's redenomination** (C8)? The
answer decides the seed's increment and whether L0 seeds one local currency or two.

**Q6 — Digits in Arabic: Latin (`1,750`) or Arabic-Indic (`١٬٧٥٠`)?** Input accepts both
regardless (§8.3); this is display only.

**Q7 — Is one operator enough?** v1 as designed has no users or PINs. That means **anyone at the
counter can void a sale, change the rate, and see cost and profit.** If an employee ever works the
till, an **owner PIN** on those three acts is the minimum I would recommend — small to build, and
far cheaper than retrofitting after a shop discovers why it was needed.

**Q8 — Do shops open a tin and sell it loose?** Buying olive oil in 16 L tins and selling by the
litre is common in pantry shops. v1 treats a tin and loose oil as separate products, so opening a
tin is two stock adjustments with the cost carried by hand. If it is a daily act, it needs its own
operation — one tin out, 16 L in, cost carried exactly — and it belongs in L2, not later.

**Q9 — Repository housekeeping before Lite starts.** All of Mizan's work since 2026-07-31 lives on
`step-0.11-frontend-foundation`, **109 commits ahead of `main`**. Lite should branch from a `main`
that is true. *Recommendation: fast-forward `main` to the current branch first* (it is strictly
ahead, so no merge conflicts), and decide separately whether to commit `cmd/demoseed/main.go` —
**without** the 13 MB compiled `demoseed` binary beside it, which should be added to `.gitignore`.

---

## 12. WHAT THIS DOCUMENT DOES NOT CONTAIN

**The implementation code.** The brief asks for full Go implementations of the POS service, the
inventory engine, FX sync and the seeder. Under the development protocol, implementation begins
after this design is approved — and several answers above change that code materially:
Q2 changes the debt service's signatures, Q3 decides whether a line carries an override, Q7 adds a
guard to three services, and Q8 may add an operation to the stock engine. Code written before those
answers would be written twice.

On approval, L0 begins with the Wails spike, because it is the one assumption in this document
that could invalidate the layout in §6.1.

---

## 13. AMENDMENTS AFTER APPROVAL

Recorded rather than edited into the sections above, so the document shows both what was proposed and what
changed.

### 13.1 The owner's answers

| Q | Answer | What it changes | Lands in |
|---|---|---|---|
| Q1 | Manual exchange rate for v1 | The fetcher (§6.7, D5) is **not built in v1**; `fx_fetch_log` and `httpsource` leave L7's scope. **Superseded 2026-09-14** by the owner's dual-mode requirement (§13.4) | L3 |
| Q2 | Debts in USD **or** SYP, chosen per sale | A credit sale carries its debt currency; the §5 ledger already keys by currency | L5 |
| Q3 | Expected = potential profit of the stock on the shelf; actual = profit at the price charged | Reading (b). "Expected" is a stock report (on hand × (list price − average cost)), not a column on a sale line; `list_price_micro` on `sale_items` is re-examined in L4 | L6 |
| Q4 | Sell beyond stock, with a visible warning | `stock.allow_negative` is not a setting; the till warns and proceeds. §4.6's `on_hand ≤ 0` branch applies | L2, L4 |
| Q5 | Round to the nearest standard paper denomination in SYP | The cash increment is the smallest note in circulation. **The value itself is confirmed in L4's design note**, because the pound's redenomination (C8) changes the notes | L4 |
| Q6 | "Arabic digits (123)" | **Read as Latin digits 0–9**, following the example given — the design's Arabic-Indic option was `١٢٣`. Implemented in L0 (`formatInteger` pins `nu-latn`). If Arabic-Indic was meant, it is a one-line change | L0 ✅ |
| Q7 | Owner PIN for voiding a sale, changing the exchange rate, and viewing profit reports | A PIN mechanism before the first guarded act. **L1's design note proposes it**, including whether `platform/crypto` joins the edition boundary's allow-list (§6.2) for Argon2id | L1 (mechanism), L3/L4/L6 (use) |
| Q8 | Support opening packages and selling loose | An "open package" stock operation: one packaged unit out, its content in, cost carried exactly | L2 |
| Q9 | Fast-forward `main`; ignore the `demoseed` binary | **Done.** `main` fast-forwarded to `252e959`; `/demoseed` ignored and committed | ✅ |

### 13.2 Two requirements added at approval

**R1 — Windows and macOS, equally.** Applied in every phase, not only in packaging:

- `lite-ci` cross-compiles every Lite package and its test binaries for `windows/amd64`, and vets for
  `darwin/amd64`, on every run.
- Every OS-dependent decision takes an injected environment so its Windows branch is a test on any machine
  (L0: `paths.Env`).
- Every file-handling path is tested for the Windows behaviour even when running on macOS: files closed
  before they are removed or renamed; paths carrying Arabic, spaces, `#` and `'`.
- **What this cannot do** is run the Windows binary; nothing here can. Each phase's record says which Windows
  behaviours are proven by construction and which only by running it. The first real Windows run is an open
  item until a machine is available.

**R2 — nothing moves forward without every test passing.** Applied as:

- unit tests on domain values; service tests over fakes held honest by a shared contract suite run against
  the real store too (§13.3 A6); integration tests on a real SQLite file; binding tests on the envelope's wire
  shape; frontend component, routing, direction and gate tests; a gate on the built bundle;
- **every invariant mutation-drilled**, and a drill whose mutation does not compile is refused, not counted;
- a check that could not run is reported as **NOT RUN, never as passed** — L0's golangci-lint (`phases/L0_SKELETON.md`, F2).

One honest limit, stated once: tests make runtime errors rare and specific; they cannot make them impossible.
What they guarantee is that every property somebody thought to check stays checked.

### 13.3 Changes made while building L0

**A1 — Lite resolves its own data directory** instead of reusing `platform/paths`: own override
(`MIZAN_LITE_DATA_DIR`), own file (`mizan-lite.db`), and **Local rather than Roaming AppData** on Windows (L0 F5).

**A2 — the catalogs live in `internal/lite/locales/`**, not a root-level `locales-lite/`: one Go package beside
the code that embeds it, reached by Vite through an `@locales` alias.

**A3 — one migration per phase, not one `0001` for all of §5.** Q7 and Q8 both change tables §5 had already
drawn, and editing an applied migration is exactly what the runner's checksum refuses. §5 remains the target
schema; each phase's migration implements its slice with that phase's knowledge.

**A4 — `0001` also carries `jobs` and `job_runs`, verbatim from Mizan** — D8's reuse of `platform/jobs`, with
the same equivalence test as the bookkeeping tables. And the settings key column is `setting_key`: `key` is
reserved in MySQL.

**A5 — a setting is declared only when something reads it.** §6.3 sketched a dozen; L0 declares one
(`ui.locale`).

**A6 — "DB mock tests" (R2) are fakes plus a contract**, never a mock of SQL (L0 D-L0.3).

**A7 — G1 forbids the property, not the `window` object.** A drill showed a `window`-keyed rule misses a cast
and an alias (L0 F3).

**A8 — the language is applied before the first paint on the Go side** (L0 D-L0.1): `PeekLocale` plus an
asset-server middleware. §8.1's "a value Go passes at startup" is now a mechanism.

**A9 — macOS 13 is the minimum**, because Go 1.27 builds for it (L0 F4).

**A10 — the "only `pos` orchestrates" import rule (§6.2) is deferred to L1.** A rule about modules importing each
other cannot be seen failing while only one module exists. It is written, and planted, the moment a second
module does.

**A11 — primitives are built when a screen needs them**, not copied as a set (D7 amended; L0 D-L0.7).

### 13.4 Amendments made in later phases — where they are recorded

From L1 on, each phase records its amendments to this design in its own note, beside the reasoning, and the register
[DECISIONS.md](DECISIONS.md) lists their approval. This index says where to look; the sections above are not edited.

| Phase | Amendments | Changes to this design |
|---|---|---|
| L1 | [L1 §14–§20](phases/L1_CATALOGUE.md) | the owner PIN mechanism (Q7), nine units, 24 quick buttons, shop name required |
| L2 | [L2 §2.3](phases/L2_STOCK.md) A-L2.1–3 | stock in its own tables; movements record before and after; the till's ledger columns deferred to L4 |
| L3 | [L3 §2.3, §14](phases/L3_RATES.md) A-L3.1–3, R-L3 | the rate in force is the newest recorded (`seq`); **dual rate modes — internet fetch and the owner's manual rate, manual by default — supersede Q1**; D6's conversion split into `LineExtensionMulRate`/`DivRate` |
| L4 | [L4 §2.4, §14, §16](phases/L4_TILL.md) A-L4.1–6 | the ledger rebuild in dependency order; no `customer_id` on `sales` (`credit` refused until L5); one price per line; tender and change replace `cash_local`/`cash_usd`; no `products.on_hand_micro`; receipts printed in L7; **Q5's value: 500 pounds, to the nearest**; **discounts with the owner PIN** (item percent, sale amount); a void counts on the day it is made |
| L5 | [L5 §2.4, §15, §17](phases/L5_CUSTOMERS.md) A-L5.1–5 | "a credit sale without a customer is unrepresentable" becomes "exactly one charge per credit sale"; the debt currency is the currency the sale is charged in (**USD by default**); `debt_entries` gains a place, a balance chain, a name snapshot, change and a note, with one `reversal` kind and `write_off`/`refund`; the default debt currency stored as a code; every money movement snapshots the rate; **repayments at the day's rate**; each balance shown separately with a labelled reference |
| L6 | [L6 §2.4, §15, §17](phases/L6_REPORTS.md) A-L6.1–6 | the daily cash summary per payment kind becomes **a drawer per currency**; **a cash book** (`cash_entries`: expenses, withdrawals, deposits, closing counts, reversals); events without a rate convert at **the rate of their business day**; reports computed in Go from facts each module supplies (a read-only `reports` module); **a void states the cash to hand back**; the Sales screen's `refunded` is `voided`; profit on credit counts when sold; expenses with the owner PIN, the count at the counter |
| L7 | [L7 §2.4, §15, §17](phases/L7_HARDWARE_BACKUP.md) A-L7.1–6 | **export in scope** (Excel and A4 PDF, supersedes Q-L6.9); documents laid out and rendered in Go with an embedded Arabic font — the webview's print dialog not used; receipts as 1-bit bitmaps **through the printer's driver by default**, raw ESC/POS a setting; `receipts.enabled` becomes "a printer is chosen"; `print_jobs` and shop-wide `voucher_numbers`; backups daily, on close, before migration and restore and on demand, **copied to an outside folder and verified**; restore with the PIN, the loss counted and a safety snapshot, applied at start before the database opens; go-text/typesetting and x/image admitted for `typeset` only |
