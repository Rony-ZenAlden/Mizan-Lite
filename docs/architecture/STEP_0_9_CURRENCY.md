# Step 0.9 — Currency Module (Design + implementation record)

> Status: **APPROVED and IMPLEMENTED.** Design approved 2026-07-30 — all eight decisions
> (D1–D8) accepted as recommended, including D3's correction to the approved schema.
> Implementation record at **§9**.
> (Permanent Development Protocol, Steps 2–6.)
> Scope: `migrations/sqlite/0003_currency.sql`, `internal/modules/currency/**`, the `Module`
> contract, and the currency/rate-type seeds.
> **Out of scope:** the rate-preview batch workflow and provider configuration UI (Phase 5 —
> see D1), network rate providers (offline-first; D7), FX gain/loss posting (Phase 2,
> accounting), and per-currency psychological rounding applied to prices (§18.6/§G.3, Phase 5).

This is the **first business module**, and it is in Phase 0 for the reason §CUR gives: `Money`,
rate types, and redenomination are kernel-adjacent, so every later module depends on them.

It is also the first real exercise of three mechanisms built earlier — the metadata seeder and
`code`/`is_system` contract (0.5), the `translations` table (0.8), and the strategy registry
(0.5) — which is the point of building the foundation first.

---

## 1. ANALYSIS

### 1.1 Three currency roles, not one

§18.1 is the decision this module exists to honour. "Store prices in USD and calculate local
currency" is right for *pricing* and insufficient for *accounting*:

| Role | Meaning | Configured as |
|---|---|---|
| **Pricing** | What catalogue prices are authored in | `currency.pricing` (may be USD) |
| **Functional / ledger** | What the general ledger is kept in — usually mandated by law | `currency.functional` |
| **Transaction** | What a specific document was actually transacted in | Per document |

The consequence for every future monetary row (§18.1):

```sql
amount_minor            BIGINT  NOT NULL,   -- transaction currency
currency_code           CHAR(3) NOT NULL,
exchange_rate_nano      BIGINT  NOT NULL,   -- to functional, ×10⁹
functional_amount_minor BIGINT  NOT NULL    -- computed and STORED, never recomputed
```

**Storing the functional amount rather than recomputing it is the load-bearing part.** A report
run next year must not silently change because a rate was later corrected. It is also what
makes FX gain/loss computable at all.

No documents exist yet, so 0.9 does not create those columns — it provides the `Converter`
that returns *both* the converted amount and the rate used, so the module that first stores a
monetary row has everything it needs and no excuse to recompute later.

### 1.2 What makes this different from a textbook currency table

Two properties, both driven by the target market (§G.1, §G.2):

- **More than one live rate per pair.** In economies with currency controls, an official rate
  and a market rate coexist and are *legitimately* used for different purposes — official for
  tax and statutory accounts, market for pricing and purchasing. Without this, users pick one
  and manually adjust everywhere else, destroying the accuracy the subsystem exists for.
- **Redenomination happens.** High-inflation economies remove zeros. An invoice for 500,000
  old units must reprint as 500,000 forever, while a report spanning the change needs
  consistent figures.

Both are handled by data, not code: rate types are rows, and a redenomination is a new
currency row with a fixed factor to its predecessor.

### 1.3 Risks and how the design answers each

| Risk | Consequence if unhandled | Design answer |
|---|---|---|
| A rate correction cannot be recorded | The books keep a known-wrong rate | **The approved UNIQUE constraint forbids this — §3.2, D3** |
| Functional amounts recomputed at read time | Last year's report changes silently | `Convert` returns the rate so callers store it (§4.2) |
| No rate for a pair | A sale cannot be rung up | Identity → direct → inverse → pivot, then **last known with its age** (§4.3, D4) |
| Silent fabrication of a rate | Wrong money, no trace | A fallback rate is always returned *with* its age and source; never invented |
| Currency codes treated as an ISO enum | Redenomination becomes a data migration under time pressure | Codes are data; non-ISO allowed (§3.1) |
| Redenomination chain loops | Infinite loop resolving an amount | Cycle detection with a hop bound (§4.4) |
| A currency in use gets deleted | Documents reference a missing currency | `is_system` protection + never hard-delete (§3.1) |
| Rate lookups are hot | Every invoice line hits the database | Resolution is a single indexed query; caching deferred until measured (§4.5) |

---

## 2. DESIGN — the module contract, first implementation

§MOD requires the `Module` interface to be finalised in Phase 0 so Phase 1 has a stable
target. Currency is the first implementor.

```go
// internal/modules/contract  (or internal/platform/modules)
type Module interface {
    Name() string
    DependsOn() []string
    Migrations() fs.FS
    Settings() []config.Definition
    FeatureFlags() []config.FlagDef
    Metadata() []metadata.SeedSet
    RegisterStrategies(*strategy.Registries) error
    Subscribe(*eventbus.Bus, *outbox.Subscribers) error
    Jobs() []jobs.Def
    Bindings() any
}
```

Two deliberate departures from §MOD's sketch:

- **`Permissions()` is absent.** `auth.PermissionDef` does not exist until Phase 1, and
  declaring a method returning a type we would invent now is exactly the speculative fiction
  0.6 avoided with placeholder event types. It is added when RBAC lands. **D5.**
- **`Subscribe` takes both buses**, because 0.6 established two distinct mechanisms and a
  module may use either.

**The registry that consumes this — validating no dependency cycles, no duplicate setting or
flag keys across modules, namespaced strategy keys — belongs to the composition root in Step
0.10.** Building the validator here, with exactly one module to validate, would be designing
against an imagined second implementor. **D5.**

---

## 3. DESIGN — schema `0003_currency.sql`

### 3.1 `currencies`

```sql
currencies (
  id                    CHAR(36)    NOT NULL PRIMARY KEY,
  code                  CHAR(3)     NOT NULL,          -- UNIQUE; the business key
  name                  VARCHAR(80) NOT NULL,          -- base; translated via `translations`
  symbol                VARCHAR(12) NOT NULL,
  decimal_places        SMALLINT    NOT NULL,
  symbol_position       VARCHAR(8)  NOT NULL DEFAULT 'before',
  rounding_mode         VARCHAR(20) NOT NULL DEFAULT 'half_away_from_zero',
  succeeded_by_code     CHAR(3),                       -- redenomination successor (§G.2)
  redenomination_factor BIGINT,                        -- ×10⁹, fixed, never expires
  is_historical         SMALLINT    NOT NULL DEFAULT 0,
  is_system             SMALLINT    NOT NULL DEFAULT 0,
  is_active             SMALLINT    NOT NULL DEFAULT 1,
  created_at, updated_at, row_version,
  CONSTRAINT ux_currencies_code UNIQUE (code)
)
```

**`id` in addition to `code` — D2.** §18.2 says `code(PK)`. But the metadata seeder built in
0.5 requires the contract columns `id`, `code`, `name`, `is_system`, `is_active`, and every
other table in the system carries a surrogate `id`. Giving `currencies` both means the seeder
works unchanged and the uniformity holds; `code` stays the stable key that foreign keys and
documents reference. Cost: one column. The alternative — special-casing the seeder for one
table — trades a column for a permanent exception.

Foreign keys reference `currencies(code)`, which is legal against a UNIQUE column on every
target engine.

**Codes are data, not an enum.** `CHAR(3)` is a width, not an ISO membership claim: a
redenominated or customer-defined currency is a row like any other. Nothing hard-deletes a
currency — `is_system` protects the seeded set, and a currency that appears in any document
must survive forever.

**`rounding_mode`** stores the `round.RoundingMode` names made durable in 0.8's
`round.ParseMode` — the parser exists precisely so this column round-trips.

### 3.2 `exchange_rates` — and a contradiction in the approved schema (D3)

```sql
exchange_rates (
  id            CHAR(36)    NOT NULL PRIMARY KEY,
  from_currency CHAR(3)     NOT NULL REFERENCES currencies(code),
  to_currency   CHAR(3)     NOT NULL REFERENCES currencies(code),
  rate_type_id  CHAR(36)    NOT NULL REFERENCES rate_types(id),
  rate_nano     BIGINT      NOT NULL,        -- ×10⁹
  valid_from    CHAR(10)    NOT NULL,        -- date
  valid_to      CHAR(10),
  source        VARCHAR(32) NOT NULL,        -- 'manual' | 'provider:<key>'
  batch_id      CHAR(36),                    -- reserved for the Phase 5 preview
  created_at, updated_at, row_version
)
```

§CUR.1 specifies `UNIQUE (from_currency, to_currency, rate_type_id, valid_from)` **and** that
rates are "append-only — a correction is a new row". **These two rules contradict each other.**

If today's market rate is entered wrongly and corrected an hour later, the correction is a row
with the same `(from, to, rate_type, valid_from)` — which the constraint rejects. The only ways
out are updating in place (breaking append-only and destroying the history the design exists to
provide) or back-dating the correction (falsifying when it was known).

**Recommendation: the uniqueness includes `created_at`**, so a correction is a legal new row,
and resolution takes the newest:

```sql
CONSTRAINT ux_exchange_rates UNIQUE (from_currency, to_currency, rate_type_id, valid_from, created_at)
ORDER BY valid_from DESC, created_at DESC, id DESC   -- resolution
```

The constraint still blocks an exact double-insert, history stays complete, and "the rate we
believed on date X, as at time T" remains answerable — which is what an auditor asks.

### 3.3 `rate_types`

Straight metadata-contract table: `id`, `code` UNIQUE, `name`, `is_system`, `is_active`, plus
universal columns. Seeded with `official`, `market`, `custom`, `manual` — all `is_system`, so
code can depend on `RATE_TYPE_OFFICIAL` existing without hardcoding a UUID, which is exactly
what §CFG.3's `is_system` rule was built for.

Adding a rate type is inserting a row. **No migration** — satisfying "add rate types without
changing the schema" literally.

### 3.4 What `0003` deliberately does NOT create — D1

§CUR.1 lists five tables. This migration creates **three**.

`exchange_rate_batches` and `rate_provider_config` are deferred to the phase that builds the
preview workflow. The 0.4 precedent (D4: create all eight platform tables at once) does not
transfer: those had shapes fixed by approved design and each was adopted within a step or two.
These two would sit unused until Phase 5, and §G.3 says the preview needs bulk actions,
category filters, threshold rules, and psychological rounding — requirements that will very
likely change the batch schema. Creating it now buys nothing and risks migrating it anyway.

`exchange_rates.batch_id` **is** included, because back-filling a nullable column onto
historical rate rows later is the more expensive half.

---

## 4. DESIGN — the domain

```
internal/modules/currency/
├── domain/          # Currency, RateType, ExchangeRate, Converter, redenomination — pure
├── app/             # use cases: SetRate, ListCurrencies, Convert…
├── infra/sqlite/    # repositories
├── contract/        # what other modules may use: Converter, CurrencyCatalog
└── module.go        # the Module implementation
```

### 4.1 The kernel type stays the value type

`money.Currency` already exists in the kernel (code, decimals, rounding mode) and is what
`Money` carries. The module owns **persistence and lookup**, not the value:

```go
type Catalog interface {
    Get(ctx, code string) (money.Currency, error)   // → the kernel value type
    List(ctx) ([]CurrencyInfo, error)               // + name, symbol, position, flags
    Functional(ctx) (money.Currency, error)         // the ledger currency
    Pricing(ctx) (money.Currency, error)
}
```

Keeping `money.Currency` as the boundary means no module ever holds a currency *row*, so
adding a column here cannot ripple outward.

### 4.2 The converter

```go
type Converter interface {
    Convert(ctx, amt money.Money, to money.Currency, at Date, rateType string) (Result, error)
    ConvertForContext(ctx, amt money.Money, to money.Currency, at Date, kind ContextKind) (Result, error)
    ResolveRedenomination(ctx, amt money.Money, at Date) (money.Money, error)
}

type Result struct {
    Amount   money.Money
    Rate     money.Rate      // store this on the document — never recompute (§18.1)
    RateType string
    Source   string          // 'manual' | 'provider:<key>' | 'inverse' | 'pivot' | 'identity'
    AsOf     Date            // the rate's valid_from
    Age      time.Duration   // how stale — surfaced in the POS (§G.3)
}
```

`Result` carries the *provenance*, not just a number. §G.3 is explicit that "selling at a
three-day-old rate during rapid movement is a real loss", so staleness has to be a first-class
return value rather than something a caller could forget to ask for.

### 4.3 Rate resolution order — D4

1. **Identity** — `from == to` → rate 1, no lookup. A single-currency company never touches
   any other path, which is what §18.1 means by "every conversion path collapses to identity
   with no special-case code".
2. **Direct** — newest row for `(from, to, rateType)` valid at the date.
3. **Inverse** — newest `(to, from, rateType)`, inverted. Storing both directions would double
   the rows and let them disagree.
4. **Pivot** — `from → pivot → to`, where the pivot currency is a setting (USD by default,
   per §18.1's "exchange-rate pivot"). Two lookups, one multiplication, **one rounding at the
   end** per the kernel's single-rounding rule.
5. **Last known** — no rate valid at the date → the most recent rate *before* it, returned with
   its `AsOf` and `Age`. Offline is the normal case (§18.3), not an error.
6. **Fail** — no rate has ever existed for the pair. A typed error, never a fabricated 1.0.

### 4.4 Redenomination

`ResolveRedenomination` walks `succeeded_by_code` / `redenomination_factor` so a report
spanning a changeover is consistent while stored documents keep their original amounts.

The walk is **cycle-detected and hop-bounded**: a data-entry mistake making A succeed B and B
succeed A would otherwise hang the report that discovered it. A cycle is a typed error naming
both codes.

Factors are exact `×10⁹` integers and compose by multiplication with a single final rounding —
never a float, per the numeric kernel's rule.

### 4.5 No cache yet

Rate resolution is one indexed query. A cache would need invalidating on every rate write and
on redenomination, and the read volume is unknown until documents exist.

Deliberately deferred, consistent with 0.8's translation cache being *added* only where the
N+1 was demonstrable. The seam is the `RateRepository` interface, so a cache is a decorator
when a measurement justifies it.

---

## 5. DESIGN — settings, seeds, and the strategy port

### 5.1 Settings

| Key | Default | Scopes |
|---|---|---|
| `currency.functional` | `SYP` (dev seed) | system, company |
| `currency.pricing` | `USD` | system, company |
| `currency.pivot` | `USD` | system |
| `currency.rate_type.pricing` | `market` | system, company |
| `currency.rate_type.purchasing` | `market` | system, company |
| `currency.rate_type.sales` | `market` | system, company |
| `currency.rate_type.accounting` | `official` | system, company |
| `currency.rate_type.reporting` | `official` | system, company |
| `currency.rate_type.tax` | `official` | system, company |

Six contexts resolved independently from day one, exactly as §CUR.2 requires — **a company
with one rate sets them all the same and never encounters the concept.** They are typed
handles (0.5, D1), so no module reads a rate type by string.

### 5.2 Seeds — D6

`currencies` and `rate_types` are seeded through the **0.5 metadata seeder**: idempotent,
matched by `code`, `is_system` rows protected, admin edits to non-system rows never reverted.
This is that machinery's first real use.

Seed set: `SYP`, `USD`, `EUR`, `TRY` (regional relevance), plus the four rate types.

**Arabic names are seeded into the `translations` table** — the first real use of 0.8's
user-content resolver, and the demonstration that adding a language to reference data is a
data operation.

**Rates are never seeded.** They are the customer's data, and a shipped rate would be wrong
the day it shipped.

### 5.3 The provider port — D7

§18.3's abstraction, registered through the `strategy` registry under the `rate_provider`
extension point that 0.5 already named:

```go
type RateProvider interface {
    Key() string
    Fetch(ctx, base money.Currency, targets []money.Currency) ([]FetchedRate, error)
}
```

**No network provider ships.** Offline is a hard project constraint, and a provider needs
credentials, a schedule, and a preview workflow — all Phase 5. Defining the port now costs a
file and means Phase 5 adds an implementation rather than retrofitting a seam.

---

## 6. DESIGN — what this module does NOT publish

No events, and no subscriptions.

0.6 declined to invent a fake `currency-created` event for exactly this reason: an event with
no subscriber is a guess about what someone will need. The real consumer is Phase 5 price
recalculation, which will know what it wants. `Module.Subscribe` returns nil and the seam is
already there. **D8.**

---

## 7. TESTING PLAN (Protocol Step 4)

| Level | Tests |
|---|---|
| **Schema** | `0003` applies on top of `0001`+`0002`; portable-contract audit (no SQLite-isms, CHECKs present); a currency referenced by a rate cannot be deleted. |
| **Catalog** | `Get` maps a row to `money.Currency` with the right decimals and rounding mode; unknown code is a typed NotFound; `Functional`/`Pricing` read their settings. |
| **Conversion — table test** | Identity; direct; inverse; pivot; **a pivot conversion rounds once, not twice**; unknown pair is a typed error, never 1.0. |
| **Temporal** | A rate valid at a past date resolves to the rate *of that date*, not today's; a correction recorded later supersedes the original for the same `valid_from`; `valid_to` expiry is respected. |
| **The correction drill (D3)** | Recording a corrected rate for a date that already has one **succeeds**, both rows survive, and resolution returns the newer. This is the test the approved UNIQUE constraint would fail. |
| **Staleness** | With no rate valid at the date, the last known rate is returned with a correct `AsOf` and non-zero `Age`; with no rate ever, a typed error. |
| **Redenomination** | A chain of two resolves through both factors exactly; a cycle is a typed error naming the codes; a hop bound stops a long chain; a document's stored amount is untouched. |
| **Rate types** | Each of the six contexts resolves its own bound type; changing one context's setting does not affect the others; a company with all six the same behaves identically to a single-rate system. |
| **Seeds** | Seeding twice changes nothing (idempotency drill, as in 0.5); an admin's edit to a non-system currency survives re-seeding; a system currency cannot be deleted; Arabic names resolve through the 0.8 resolver. |
| **Money safety** | Property test: converting an amount and converting it back through the inverse rate stays within one minor unit — no float, no drift. |

**Mutation-verified**: *single rounding in pivot conversion* (round at each hop → the
double-rounding test must fail) and *temporal resolution* (order by `valid_from` only, ignoring
`created_at` → the correction drill must fail).

---

## 8. DECISIONS REQUESTED

1. **D1 — `0003` creates three tables, not five.** `exchange_rate_batches` and
   `rate_provider_config` are deferred to Phase 5, whose UX requirements (§G.3) will shape
   them. `exchange_rates.batch_id` is included now. *(Recommended; §3.4.)*
2. **D2 — `currencies` gets `id CHAR(36)` as well as `code CHAR(3)`**, so the 0.5 metadata
   seeder and the universal-column convention apply unchanged; foreign keys reference `code`.
   *(Recommended; §3.1.)*
3. **D3 — The approved `UNIQUE (from, to, rate_type, valid_from)` contradicts append-only
   corrections.** Include `created_at` in the uniqueness and resolve newest-first.
   *(Recommended; §3.2 — **this is a genuine defect in the approved schema, and the decision I
   most want confirmed**.)*
4. **D4 — Resolution order** identity → direct → inverse → pivot → last-known-with-age →
   typed error. A rate is never fabricated. *(§4.3.)*
5. **D5 — The `Module` interface is defined here without `Permissions()`** (Phase 1 adds it);
   the validating registry lands with the composition root in 0.10. *(§2.)*
6. **D6 — Seeds through the 0.5 metadata seeder**, with Arabic names in `translations`. Rates
   are never seeded. *(§5.2.)*
7. **D7 — `RateProvider` port + strategy registration, no network provider.** *(§5.3.)*
8. **D8 — No currency events yet**, since no subscriber exists; the seam is in place. *(§6.)*

On approval I'll implement in this order, each independently reviewable: `0003_currency.sql` →
domain types + `Catalog` → rate repository + temporal resolution → `Converter` (identity →
direct → inverse → pivot → stale) → redenomination → settings + context bindings → seeds
(currencies, rate types, Arabic names) → the `Module` implementation → the correction and
rounding drills, tests alongside each, then the self-review and improvement notes (Protocol
Steps 5–6).

---

## 9. IMPLEMENTATION RECORD (Protocol Steps 3–6)

### 9.1 A test exposed a real gap: staleness was only reported on the fallback path

`Result.Stale` was set only when resolution fell through to `LastKnown`. But a rate with no
explicit `valid_to` **never formally expires** — so a three-day-old rate resolved as a
perfectly current "direct" hit, with `Stale` false and `Age` zero.

§G.3 is explicit that "selling at a three-day-old rate during rapid movement is a real loss".
What a caller needs is the age of the rate *actually applied*, not whether the resolver had to
fall back. `Age` is now stamped on **every** path, and `Stale` means "the rate predates the
requested date" — with the threshold that matters left to the caller, since a POS and a
monthly report care very differently.

The design said the right thing about staleness and the first implementation quietly did not.

### 9.2 Module-owned migrations, and 0.4's carried-forward promise

The design put `0003_currency.sql` in the shared `migrations/sqlite/` tree, but that would have
made `Module.Migrations() fs.FS` a formality — the module would be returning someone else's
files. The SQL now lives in `internal/modules/currency/migrations/` and is embedded by the
module.

That required `migrate.Merge`, which is exactly the item **Step 0.4 carried forward**:
*"module-owned migration FS merging — merging arrives with the second module that ships
migrations (0.9, currency)."* Duplicate-version detection stays in `Load`, so a module
colliding with the platform and two modules colliding with each other produce the identical
error.

### 9.3 Three repositories, not one

`CurrencyRepository` and `RateTypeRepository` both declare `ByCode`, so one type cannot satisfy
both. Renaming a method to dodge that would have made the domain interfaces read worse to serve
an implementation detail, so the infra layer has three small types instead.

### 9.4 Honest limits of the mutation testing

**Temporal resolution: verified.** Ordering by `valid_from` alone, ignoring `created_at` — the
pre-D3 behaviour — makes the correction drill fail with `amount = 13000, want 15000`. The D3
fix is genuinely pinned.

**Single rounding: NOT mutation-verified.** Two attempts both passed, and the reason is
instructive: perturbing the combined *rate* barely moves the result, because nano is already
the storage precision. The real hazard is rounding the *amount* at an intermediate currency —
€0.01 → $0.0033 → $0.00 → 0 SYP, versus 10 SYP when the rates are combined first. Modelling
that faithfully needs the converter restructured to do sequential money conversion, which is a
larger change than the mutation is worth.

So `TestPivotRoundsOnceNotTwice` asserts the correct behaviour directly (€0.01 → 10 SYP via
pivot, which only holds if no intermediate rounding occurs) but rests on that assertion rather
than on mutation evidence. Recorded rather than glossed, because "mutation-verified" has been
a real signal in this project and should not be diluted.

### 9.5 Verification

**23 currency tests + 4 new merge tests, `-race` clean, `make ci` green.**
`modules/currency` 73.9%, `platform/migrate` back to 85.5%.

Everything runs against the real merged schema — `0001`, `0002`, and the module's `0003`
applied through the Step 0.4 runner — so the foreign keys, the CHECK constraints, and the D3
uniqueness are genuinely exercised.

Three earlier mechanisms got their first real use, which was the point of building them first:

- **The 0.5 metadata seeder** — currencies and rate types seeded by `code`, `is_system`
  protected, and the idempotency drill confirms a second run changes nothing.
- **The 0.8 translations table** — Arabic currency and rate-type names as data. `SYP` reads
  "Syrian Pound" in English and "ليرة سورية" in Arabic with no `name_ar` column anywhere.
- **`round.ParseMode`**, added in 0.8 as "the other half of the storage contract", is what
  makes `currencies.rounding_mode` round-trip.

### 9.6 Carried forward

- **Step 0.10** wires the module through the composition root: the validating registry (no
  dependency cycles, no duplicate setting keys across modules), and `Module.Bindings()` gets a
  real value once 0.11 defines the frontend contract.
- **Phase 5** builds `exchange_rate_batches` and `rate_provider_config` (D1), the preview
  workflow, psychological rounding, and the first network `RateProvider`.
- **Phase 2** (accounting) is the first consumer of the three-column monetary convention from
  §18.1; `Result` already carries everything such a row needs.
- **`Permissions()`** joins the `Module` interface with RBAC in Phase 1.

---

*End of Step 0.9. Design approved, implemented, self-reviewed.*
