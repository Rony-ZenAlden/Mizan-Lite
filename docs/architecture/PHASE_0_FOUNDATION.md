# Mizan ERP — Phase 0: Foundation (Detailed Design)

> Companion to [`ARCHITECTURE_v1.md`](./ARCHITECTURE_v1.md) and
> [`ADDENDUM_v1.1_resolved_design.md`](./ADDENDUM_v1.1_resolved_design.md).
> Status: **DRAFT — awaiting approval.** No production code until this is signed off.

Phase 0 builds the load-bearing layer that every business module depends on and that is the
most expensive to change once modules exist. It contains **no business logic** — no products,
no invoices, no accounting. It contains the kernel, the platform, the configuration
architecture, the composition root, and the frontend foundation.

**The discipline for this phase:** if it isn't needed by at least two future modules, it
doesn't belong in Phase 0. Everything here is infrastructure that earns its place by being
shared.

## Contents

| § | Section |
|---|---|
| **SCOPE** | What Phase 0 includes and explicitly excludes |
| **CFG** | Configuration & Metadata Architecture (the mandate, formalized) |
| **REPO** | Repository scaffolding, Go module, tooling |
| **KRN** | The numeric & primitive kernel |
| **DB** | Database platform: connections, dialect shim, Unit of Work |
| **MIG** | Migration runner |
| **BUS** | Event bus + transactional outbox |
| **JOB** | Durable job scheduler |
| **I18N** | i18n platform |
| **CUR** | Currency subsystem foundation (rate types, redenomination) |
| **MOD** | The module contract |
| **BOOT** | Composition root |
| **FE** | Frontend foundation (design tokens, providers, bindings) |
| **TEST** | Testing foundation |
| **DOD** | Definition of Done for Phase 0 |

---

## SCOPE — what Phase 0 is and is not

**In scope (infrastructure only):**

- Repository skeleton, Go module, build/lint/test tooling, Wails app that launches to a shell
- `internal/kernel/*` — money/numeric types, ID, clock, errors, events, locale, validation, paging
- `internal/platform/*` — database, migrate, eventbus, outbox, jobs, i18n, crypto, logging, config, fs
- The **configuration registry** and **metadata registry** mechanisms (§CFG)
- The **module contract** interface and registry (§MOD)
- The **composition root** wiring an empty-but-complete graph (§BOOT)
- Currency **primitives and schema** (§CUR) — the one place Phase 0 touches a "business" table,
  because Money and rate types are kernel-level and every module depends on them
- Frontend **design-token layer, providers, i18n, and the bindings wrapper** (§FE)
- Testing harness and the first invariant/property tests (§TEST)

**Explicitly excluded (later phases):** users, roles, products, variants, partners,
inventory, sales, accounting posting, tax rules, reports. The setup wizard is Phase 1.
Phase 0 ends with an application that boots, migrates an almost-empty database, resolves
locale and settings, runs a background job, and renders a themed, translated, RTL-capable
empty shell. Nothing a user would call a feature — everything a developer needs to build one.

---

## CFG — Configuration & Metadata Architecture

Your overarching mandate gets its own subsystem, because "prefer configuration over code" only
holds up if there is a disciplined mechanism for it. Ad-hoc config scattered across modules
decays into exactly the hardcoding it was meant to avoid. Phase 0 establishes three distinct,
deliberately separated mechanisms.

### CFG.1 The three kinds of "configuration" — kept separate

| Mechanism | What it is | Changes | Example | Store |
|---|---|---|---|---|
| **Settings** | Typed key/value knobs resolved by scope | At runtime, by users | "default tax group", "language", "rounding stage" | `settings` table (§16) |
| **Metadata / reference data** | Rows that define *kinds of things* the system operates on | At runtime, by admins; seeded initially | rate types, payment methods, UoM, number series, print templates, tax codes, accounts | dedicated tables |
| **Registered strategies** | Named code behaviours selected by data | At build time (code) + runtime (selection) | costing strategy, rate provider, print renderer, barcode symbology, posting-rule evaluator | in-code registry keyed by string |

The rule that makes this coherent:

> **Data selects behaviour; code provides behaviour.** A business rule's *parameters* and
> *which* behaviour applies are always data. The *mechanism* of a genuinely algorithmic
> behaviour is code, registered under a stable key that data points to.

So a tax *rate* is data; the tax *resolution algorithm* is code. A payment *method* is data; a
payment *provider integration* is a registered strategy. This is what lets Mizan adapt to a
new country by editing seed data, while keeping the code paths finite, testable, and safe.

### CFG.2 The Setting Registry

Settings are **declared in code** (key, type, default, allowed values, scopes, permission),
never invented as loose strings at call sites. Illustrative shape:

```go
// platform/config
type SettingDef struct {
    Key          string            // "sales.rounding_stage"
    Type         ValueType         // String|Int|Bool|Money|Enum|JSON|ID|Duration
    Default      any
    Enum         []string          // for Type == Enum
    Scopes       []Scope           // which scopes may override it
    Permission   string            // permission required to change it (optional)
    Description  string            // i18n key
    Validate     func(any) error   // optional extra validation
}

type Settings interface {
    String(ctx, key string) (string, error)
    Bool(ctx, key string) (bool, error)
    Int(ctx, key string) (int64, error)
    Money(ctx, key string) (money.Money, error)
    ID(ctx, key string) (id.ID, error)
    Enum(ctx, key string) (string, error)
    Set(ctx, scope Scope, scopeID id.ID, key string, val any) error
}
```

Each module registers its `SettingDef`s in `module.go`. At startup the registry: validates all
declared defaults, rejects unknown keys found in the database (catches typos and removed
settings), and can **generate the settings UI** from the registry rather than anyone
hand-coding a settings screen. Typed accessors mean a call site can never fetch a bool as a
string. A `SettingChanged` event drives live reactivity (§16.3) — this is the mechanism behind
"language change without restart" and "rate-type change takes effect immediately."

### CFG.3 The Metadata Registry

Reference data (rate types, payment methods, print templates, UoM, number series, tax codes,
accounts, report definitions) shares a common contract so the system treats "kinds of things"
uniformly:

- Every metadata table carries `code` (stable, machine key), `name` (translatable label),
  `is_system` (protected from deletion), `is_active`, and the universal columns.
- **`is_system` rows are seeded, referenced by code, and cannot be deleted** — only
  deactivated. This is what lets code depend on `RATE_TYPE_OFFICIAL` existing without
  hardcoding its UUID.
- User-created rows are freely editable and deletable (subject to referential use).
- Seeds are idempotent JSON, versioned, applied by `code`, never by primary key.

This gives you the mandate's payoff: adding a payment method, a print template, a rate type, a
unit, or a document series is a **data operation**, available to an admin at runtime, with no
code change and no release.

### CFG.4 Registered strategies (extension points)

The in-code registry from §25.2, formalized as the third mechanism:

```go
type Registry[T any] struct { /* key → factory */ }
func (r *Registry[T]) Register(key string, impl T)
func (r *Registry[T]) Resolve(key string) (T, error)
func (r *Registry[T]) Keys() []string
```

Phase 0 ships the generic registry plus these named registries, empty and ready for later
phases to populate: `costing`, `rate_provider`, `print_renderer`, `barcode_symbology`,
`payment_provider`, `report`, `export_format`, `posting_evaluator`, `import_source`. A
`switch` statement on a business "kind" anywhere in the codebase is a design defect — it goes
through a registry instead.

### CFG.5 The boundary — where configuration stops

Being honest about this prevents the mandate from becoming dogma that harms the product:

- **Invariants are never configurable.** Double-entry must balance; a posted document is
  immutable; quantities can't be negative where a unit forbids it. These are code, always.
- **Safety and integrity rules are never configurable.** No setting disables authorization,
  audit, or transactional integrity.
- **Configuration is not a scripting language.** When flexibility genuinely needs logic
  (custom validation, complex workflows), that is the *plugin* path (§25), not an ever-growing
  settings table. Overloading settings into a pseudo-language is its own maintenance disaster.

The test I apply: *"Would a competent admin in another country reasonably need this different,
and can it be expressed as parameters + a choice among safe behaviours?"* If yes → config. If
it needs new logic → strategy or plugin. If changing it would break a financial or safety
invariant → code, not configurable.

---

## REPO — repository scaffolding

### REPO.1 Layout (Phase 0 materialized subset)

Follows the tree in `ARCHITECTURE_v1.md` §4. Phase 0 creates the skeleton and fills only the
foundation:

```
mizan/
├── go.mod                      module github.com/<org>/mizan   (Go 1.23+)
├── Makefile / Taskfile.yml     build, lint, test, migrate, generate, wails-dev
├── wails.json
├── .golangci.yml               lint config incl. the custom rules below
├── .editorconfig
├── cmd/mizan/main.go
├── internal/
│   ├── bootstrap/              composition root (§BOOT)
│   ├── kernel/                 (§KRN)  — fully built in Phase 0
│   ├── platform/               (§DB–§I18N) — fully built in Phase 0
│   ├── modules/
│   │   └── currency/           the only module in Phase 0 (§CUR)
│   └── api/
│       ├── envelope/           Result[T], APIError (§5.4)
│       ├── appctx/             session/branch/locale context
│       └── bindings/           health/system bindings only in Phase 0
├── migrations/sqlite/          0001_platform.sql, 0002_currency.sql
├── seeds/currencies.json       currencies + rate types (system rows)
├── locales/{en,ar}/            common.json, errors.json  (seed set)
├── frontend/                   (§FE)
├── test/                       fixtures, integration harness
└── docs/                       these documents + ADRs
```

### REPO.2 Toolchain

| Concern | Choice | Note |
|---|---|---|
| Go | 1.23+ | generics for the kernel & registries |
| SQLite driver | `modernc.org/sqlite` (pure Go, **no cgo**) | keeps Windows cross-compilation from macOS trivial (proved in Step 1.13); cgo is a build-and-support tax a desktop app should avoid |
| Migrations | `pressly/goose` as a library | §MIG |
| SQL / read models | `sqlc` (generated) + `database/sql` for aggregates | §8.6 |
| UUIDv7 | `google/uuid` (v7 support) | §7.4 |
| Password hashing | `golang.org/x/crypto/argon2` | pure Go |
| Logging | stdlib `log/slog` | structured, no dependency |
| Config | stdlib + small loader | boot config only |
| Lint | `golangci-lint` + custom analyzers | REPO.3 |
| Test | stdlib `testing` + `testify` + `rapid` (property tests) | §TEST |
| Frontend | Vite + React 18 + TS strict + Tailwind + Radix + TanStack Query + Zustand | §FE |

`modernc.org/sqlite` over `mattn/go-sqlite3` is a deliberate call: no-cgo means one static
binary per platform, reproducible builds, and no C toolchain on the build machine or the
customer's. For a desktop product shipped as an installer, that is worth the marginally lower
raw throughput, which is irrelevant at desktop data volumes.

### REPO.3 Enforced architectural rules (lint, not convention)

Rules become custom `go/analysis` analyzers or `depguard` config, so violations fail CI
rather than relying on reviewer memory:

1. `internal/kernel` and every `modules/*/domain` package may import **only** stdlib and
   `internal/kernel`. No SQL driver, no Wails, no JSON, no other module.
2. No package imports another module's non-`contract` package.
3. No `float64`/`float32` in `domain` or in any type named `*Money|*Price|*Cost|*Rate|*Qty`.
4. No `time.Now()` outside `platform/clock`; domain/app take a `Clock`.
5. No `panic` in library code paths (recover boundary is the binding decorator only).
6. Frontend: no physical-direction Tailwind classes (`pl-`, `ml-`, `text-left`, `float-left`)
   — ESLint rule; only logical properties (`ps-`, `ms-`, `text-start`) pass. (§22.3)
7. No raw SQL string outside `infra` packages.

These seven rules are what keep the architecture *actually* clean two years in, rather than
clean only in this document.

---

## KRN — the numeric & primitive kernel

Pure Go, stdlib + `google/uuid` only, exhaustively unit- and property-tested. This is the most
correctness-critical code in the system; it is small, and it is worth writing slowly.

### KRN.1 The five numeric types (§E made concrete)

```go
// kernel/money
type Currency struct {                 // value object; the row lives in `currencies` (§CUR)
    Code     string   // "SYP", "USD" — data, never an enum (§CUR.2)
    Decimals int      // minor-unit scale: SYP? USD 2, KWD 3, JPY 0
}

type Money struct {                    // exact; totals, GL, payments, balances
    minor    int64
    currency Currency
}
func NewMoney(minor int64, c Currency) Money
func (m Money) Add(o Money) (Money, error)        // error on currency mismatch
func (m Money) Sub(o Money) (Money, error)
func (m Money) MulQty(q quantity.Quantity) Money  // full-precision → single rounding
func (m Money) Allocate(weights []int64) []Money  // largest-remainder; parts sum to whole
func (m Money) Round(mode RoundingMode) Money

type UnitAmount struct { micro int64; currency Currency }   // unit prices/costs, 10⁻⁶ major
type Quantity   struct { micro int64; uom UoMRef }          // 10⁻⁶, category-aware
type Rate       struct { nano  int64 }                      // 10⁻⁹, fx & uom factors
type Percent    struct { micro int64 }                      // 10⁻⁶  (15% == 150000)
```

**The single-rounding rule (§E), implemented once and only here:**

```go
// LineExtension = round( unitPrice × quantity ), 128-bit intermediate, ONE rounding.
func LineExtension(p UnitAmount, q Quantity, mode RoundingMode) (Money, error)
```

Uses `math/big.Int` for the intermediate to guarantee no overflow, converts to `Money` with
exactly one rounding operation. **No other code in the entire system multiplies a price by a
quantity.** Discounts and taxes then operate on the resulting `Money` via `Allocate`.

Property-based tests (rapid) assert: allocation parts always sum to the whole; conversion
round-trips within one ulp; `a+b-b == a`; commutativity; no panic on `int64` extremes;
currency-mismatch always errors.

### KRN.2 Other kernel primitives

| Package | Provides |
|---|---|
| `kernel/id` | UUIDv7 generate/parse/validate; `ID` type; `Nil` |
| `kernel/clock` | `Clock` interface; `System` and `Fixed` (test) implementations |
| `kernel/errs` | Typed errors with category (§7.6) + stable code + i18n key + params + field errors; `Is`/`As` helpers |
| `kernel/event` | `DomainEvent` interface, envelope (id, type, occurredAt, aggregate, correlation, causation) |
| `kernel/locale` | `Locale` (`ar`,`en`), `TextDirection` (RTL/LTR), fallback chain |
| `kernel/validation` | Rule combinators returning field-level `errs` |
| `kernel/paging` | `Page`, `Sort`, `Filter`, `PageResult[T]` — the shared read-side shape |
| `kernel/quantity` | `Quantity`, UoM reference, category-checked conversion |

No package here imports anything outside stdlib + `google/uuid`. This is verified by lint rule
REPO.3(1) and is what lets the entire kernel be reasoned about in isolation.

---

## DB — database platform

### DB.1 Connections (§8.4)

```go
type DB interface {
    Writer() *sql.DB          // MaxOpenConns(1) — all writes & transactions
    Reader() *sql.DB          // MaxOpenConns(N) — read-only queries
    Dialect() Dialect
    Close() error
}
```

WAL, `foreign_keys=ON`, `busy_timeout`, and the other PRAGMAs (§8.4) are applied on every
connection open via a driver connect hook. The single-writer pool converts SQLite's write
contention into a bounded queue wait rather than `SQLITE_BUSY` errors surfacing to users.

### DB.2 Dialect shim (§8.5)

```go
type Dialect interface {
    Name() string                          // "sqlite"
    Placeholder(n int) string              // "?"  → "$1" on postgres
    QuoteIdent(s string) string
    Upsert(table string, cols, conflict, update []string) string
    Limit(limit, offset int) string        // "LIMIT ? OFFSET ?" vs "OFFSET ? FETCH ..."
    BoolLiteral(b bool) string
    NowExpr() string
    ForUpdate() string                     // "" on sqlite; "FOR UPDATE" on pg (§9.4)
}
```

~10 methods, the only place aware of a specific database. Repositories write portable SQL
against the §8.1 type contract and route these few concerns through the shim. Phase 0 ships
`SQLiteDialect`; the interface is proven by the currency repository using it.

### DB.3 Unit of Work (§11.3)

```go
type UoW interface {
    Do(ctx context.Context, fn func(ctx context.Context) error) error
}
```

Stores the active `*sql.Tx` in the context. Repositories retrieve it transparently, so
business code never threads a transaction handle. Nested `Do` calls **join** the outer
transaction (critical on SQLite's single writer — a second real transaction would deadlock).
On success it commits, then hands the aggregate's pulled events to the outbox writer *within*
the same transaction (so publish is atomic with the change), then dispatches after commit.

### DB.4 Repository & read-model split

Phase 0 establishes the two shapes so every later module follows them (§11.1, §27):

- **Aggregate repositories** — hand-written SQL, load/save whole aggregates, version-checked.
- **Read models** — `sqlc`-generated, flat DTOs, never touch aggregates.

A **shared repository contract test-suite** harness (§30) is built now: any repository
implementation runs the same behavioural suite. This is the mechanism that will protect the
PostgreSQL migration — the new driver's repositories must pass the identical suite.

---

## MIG — migration runner

`pressly/goose` embedded (§10.2), wrapped with the desktop-safety layer that is the actual
reason we don't just call goose directly (§10.4):

1. `PRAGMA integrity_check` before running.
2. **Automatic verified backup** to a timestamped file.
3. Run pending migrations, each transactional, recorded with checksum in `schema_migrations`.
4. **Checksum verification** of already-applied migrations on every boot — a modified
   historical migration is a hard startup failure.
5. On failure: rollback, **auto-restore** from the pre-migration backup, clear user-facing
   error with the backup path.
6. **Version gate**: a database newer than the binary refuses to open.
7. Progress surfaced to the UI for long runs (Wails events).

Phase 0 migrations: `0001_platform.sql` (schema_migrations already handled by goose;
`outbox_events`, `jobs`, `job_runs`, `number_series`, `settings`, `feature_flags`,
`translations`) and `0002_currency.sql` (§CUR). Module-owned migration FSs are collected in
global version order by the bootstrap (§10.3).

---

## BUS — event bus + transactional outbox

### BUS.1 In-process bus (§23.2)

Typed, synchronous for **domain** events (same transaction), with explicit subscriber
registration at bootstrap — no reflection-based discovery, so every subscription is greppable.
Panics in handlers are isolated and logged; ordering is per-aggregate.

### BUS.2 Transactional outbox (§23.3)

The reason this is Phase 0 and not later: it is the atomicity guarantee under every
cross-module side effect, and every module will depend on it from its first commit.

- `outbox_events` table (§23.3 schema) written **inside** the business transaction.
- A dispatcher (running as a durable job, §JOB) polls `pending`, dispatches to integration
  subscribers, marks `done`/`failed`, retries with exponential backoff + jitter, and routes
  exhausted messages to a dead-letter state surfaced in the UI.
- Idempotency: handlers declare it; the dispatcher records processed event IDs.
- Ordered and causally linked (`correlation_id`, `causation_id`) — already the shape a future
  cloud-sync protocol consumes (§23.3).

Phase 0 proves the whole path end-to-end with a trivial event (currency-created → a no-op
logging subscriber), so the machinery is exercised before any module relies on it.

---

## JOB — durable job scheduler

Persisted jobs (§24), because a desktop app is closed nightly and in-memory schedules lose
work.

- `jobs` + `job_runs` tables; cron-style schedule; singleton enforcement; per-job timeout,
  max attempts, catch-up policy (`run_once`/`skip`/`run_all`).
- Bounded worker pool, context cancellation, graceful drain on shutdown, backoff with jitter.
- Startup reconciliation runs overdue jobs per their catch-up policy (a missed backup runs;
  three missed rate-fetches collapse to one).
- Every run recorded and surfaced in a UI panel — silent background failure is how customers
  lose backups without knowing.

Phase 0 registers exactly one real job — the **outbox dispatcher** — plus a heartbeat job that
proves scheduling, catch-up, and the UI panel work.

---

## I18N — internationalization platform

- `locales/{en,ar}/*.json` embedded via `go:embed`, the single source shared with the
  frontend (§22.1). Phase 0 ships `common.json` and `errors.json` seed sets.
- Message resolver: key + locale + params → string, with fallback chain (`ar → en → key`).
- The backend still returns **codes and params**, not prose (§22.2); the resolver exists for
  the few backend-rendered artifacts (logs, printed documents, generated file names).
- `translations` table + resolver for user content (§9.5), with an in-memory cache invalidated
  on write.
- Locale is carried in `AppContext` and changeable at runtime; a `LocaleChanged` event lets
  subscribers react without a restart.

---

## CUR — currency subsystem foundation

The one "business" area in Phase 0, because `Money`, rate types, and redenomination are
kernel-adjacent — every future module depends on them, so they must exist before Phase 1.

### CUR.1 Schema (finalized, per approved §G.1/§G.2)

```sql
currencies (
  code                  CHAR(3)  NOT NULL PRIMARY KEY,   -- data, not an enum; non-ISO allowed
  name                  VARCHAR(80) NOT NULL,            -- translated via `translations`
  symbol                VARCHAR(12) NOT NULL,
  decimal_places        INTEGER  NOT NULL,               -- minor-unit scale (0/2/3…)
  symbol_position       VARCHAR(8) NOT NULL DEFAULT 'before',
  rounding_mode         VARCHAR(12) NOT NULL DEFAULT 'half_up',
  -- redenomination (§G.2):
  succeeded_by_code     CHAR(3),                         -- old → new successor
  redenomination_factor BIGINT,                          -- ×10⁹, fixed, never expires
  is_historical         SMALLINT NOT NULL DEFAULT 0,
  is_system             SMALLINT NOT NULL DEFAULT 0,
  is_active             SMALLINT NOT NULL DEFAULT 1,
  -- + universal columns
  CONSTRAINT ck_currencies_hist CHECK (is_historical IN (0,1))
);

rate_types (                                             -- DATA, extensible without migration
  id, code, name, is_system, is_active, ...              -- 'official','market','custom','manual'
  CONSTRAINT ux_rate_types_code UNIQUE (code)
);

exchange_rates (
  id                    CHAR(36) NOT NULL PRIMARY KEY,
  from_currency         CHAR(3)  NOT NULL,
  to_currency           CHAR(3)  NOT NULL,
  rate_type_id          CHAR(36) NOT NULL,               -- §G.1
  rate_nano             BIGINT   NOT NULL,               -- ×10⁹
  valid_from            CHAR(10) NOT NULL,               -- date; temporal, never updated
  valid_to              CHAR(10),
  source                VARCHAR(20) NOT NULL,            -- 'manual'|'provider:<key>'
  provider_id           CHAR(36),
  batch_id              CHAR(36),                        -- §18.4 preview batches
  -- + universal columns
  CONSTRAINT ux_exchange_rates UNIQUE (from_currency, to_currency, rate_type_id, valid_from)
);

exchange_rate_batches ( ... status draft|previewed|applied|rejected ... );   -- §18.4
rate_provider_config  ( ... provider_key, priority, schedule_cron, ... );    -- §18.3
```

Rates are temporal and append-only — a correction is a new row, giving free history and
reproducible historical documents.

### CUR.2 Rate-type context bindings (your six contexts)

Which rate type applies is **settings**, one per context, so v1 can expose a single rate while
the backend already resolves six independently:

```
currency.rate_type.pricing      → 'market'   (default)
currency.rate_type.purchasing   → 'market'
currency.rate_type.sales        → 'market'
currency.rate_type.accounting   → 'official'
currency.rate_type.reporting    → 'official'
currency.rate_type.tax          → 'official'
```

A single-rate company sets all six to the same value and never encounters the concept. New
rate types are inserted as `rate_types` rows — **no schema change**, satisfying your "add rate
types without changing the schema" requirement literally.

### CUR.3 Conversion service

```go
type Converter interface {
    Convert(ctx, amt Money, to Currency, at Date, rateType RateTypeCode) (Money, Rate, error)
    ConvertForContext(ctx, amt Money, to Currency, at Date, ctxKind ContextKind) (Money, Rate, error)
    ResolveRedenomination(ctx, amt Money, at Date) (Money, error)   // §G.2
}
```

- Resolves the rate valid at `at` for the requested type; returns both the converted amount
  **and** the rate used, so callers can store the rate on the document (§18.1) — never
  recomputed later.
- Offline is normal: missing a live rate falls back to the last known rate and reports its age
  (§18.3); the manual rate always wins when set.
- `ResolveRedenomination` walks `succeeded_by_code`/`redenomination_factor` chains so a report
  spanning a redenomination stays consistent while stored documents keep original amounts.

### CUR.4 Seeds

`seeds/currencies.json`: a starter currency set (incl. `SYP`, `USD`, `EUR`) and the four
system `rate_types`. All `is_system`, referenced by `code`, protected from deletion. Rates
themselves are **not** seeded — they are the customer's data.

---

## MOD — the module contract

The interface every future module implements and the registry the bootstrap drives (§4.1).
Finalized in Phase 0 so Phase 1 has a stable target:

```go
type Module interface {
    Name() string
    DependsOn() []string                       // enforced acyclic at bootstrap
    Migrations() fs.FS                          // globally version-ordered (§10.3)
    Settings() []config.SettingDef              // (§CFG.2)
    FeatureFlags() []flags.FlagDef              // (§17)
    Permissions() []auth.PermissionDef          // (§14.1) — Phase 1 defines auth; contract ready
    Metadata() []meta.SeedSet                   // reference-data seeds (§CFG.3)
    RegisterStrategies(reg *Registries)         // extension points (§CFG.4)
    Subscribe(bus event.Bus)                    // event handlers
    Jobs() []jobs.Definition
    Bindings() any                              // Wails binding struct (DTOs only)
}
```

The registry validates: no dependency cycles, no duplicate setting/permission/flag keys across
modules, every strategy key namespaced, every binding method returning the standard envelope.
A module that violates these **fails application startup** — the misconfiguration surfaces at
boot on the developer's machine, never as a runtime surprise on a customer's.

---

## BOOT — composition root

`internal/bootstrap` builds the graph in the strict order from §6, manually, no container. In
Phase 0 it wires a complete graph containing only the currency module, proving the sequence:

```
config → logging → clock → db(writer/reader/dialect) → backup+integrity
      → migrate(run) → eventbus → outbox → settings → i18n → jobs
      → metadata/strategy registries → modules[currency].register
      → cross-wiring (subscriptions, strategy resolution) → seed(first-run)
      → jobs.start(outbox dispatcher + heartbeat) → collect bindings → Wails.Run
      → shutdown hooks (reverse order): drain jobs → flush outbox → close db
```

Shutdown is ordered and graceful: stop accepting work, drain the job pool, flush the outbox,
checkpoint WAL, close pools. A desktop app closed mid-write must reopen clean.

---

## FE — frontend foundation

No business screens. The foundation that makes later screens fast to build and consistent —
built before any feature, because retrofitting a design system is a rewrite (§31).

### FE.1 Design tokens first

CSS-variable token layer — spacing, radius, typography scale, elevation, and a semantic color
system (`--color-surface`, `--color-text`, `--color-primary`, `--color-danger`…) defined for
**both** light and dark themes and switchable at runtime. Tailwind configured to consume the
tokens, so no component hardcodes a raw color or spacing value. This token layer is what
produces the Linear/Notion consistency the brief asks for.

### FE.2 Primitives

Radix-based accessible primitives styled with tokens: Button, Input, Select, Checkbox, Radio,
Switch, Dialog, Sheet, Toast, Tooltip, Table, Tabs, EmptyState, Spinner, Skeleton. Each with
designed **empty / loading / error / permission-denied** states from the start (§31).

### FE.3 App shell & providers

- Providers: theme, locale/`dir`, TanStack Query client, Zustand stores (session, locale,
  branch, UI prefs), toast, error boundary.
- `<html lang>` + `<html dir>` driven by locale; switching locale re-renders with **no
  reload** (§22.3) — proven in Phase 0 with the en/ar toggle in the shell.
- RTL correctness enforced by the logical-properties lint rule (REPO.3(6)) and verified by
  snapshot tests in both directions.
- Bundled offline Arabic font with per-locale typographic adjustments.

### FE.4 Bindings wrapper

`frontend/src/lib/wails/` wraps generated Wails bindings so every call goes through one place
that unwraps the `Result[T]` envelope, maps `APIError.messageKey` + params to a translated
message, and surfaces typed errors to TanStack Query. The UI therefore has exactly one error
path and never sees a raw backend string.

Phase 0 frontend deliverable: the app launches to a themed, translated, RTL-capable empty
shell with a working language toggle, a theme toggle, a system/health screen reading through a
binding, and a visible background-job status panel. Proof the whole stack is connected — no
features.

---

## TEST — testing foundation

The harness and the first tests, established now so every later phase inherits them (§30):

- **Unit**: kernel numeric types — exhaustive, plus **property-based** (rapid) for money
  allocation, conversion round-trips, and the single-rounding rule.
- **Integration harness**: spin a temp SQLite file, run full migrations, run a test, discard.
  Fast enough to run per-test.
- **Repository contract suite**: the shared behavioural suite (§DB.4) — the currency
  repository is its first client and the template for all future ones.
- **Migration tests**: apply all up; verify checksum enforcement; verify the backup+auto-
  restore path triggers on an injected failure.
- **Frontend**: component render tests + RTL/LTR snapshot tests + the i18n key-coverage check
  (no missing keys, no orphan keys).
- **CI gates**: lint (incl. the seven custom rules), `go test ./...`, `sqlc vet`, frontend
  test + typecheck, and an i18n completeness check. All green is required to merge.

---

## DOD — Definition of Done for Phase 0

Phase 0 is complete when all of the following hold. This is the checklist we review against
before Phase 1 begins.

1. `wails dev` launches an app that migrates a fresh database and renders the themed,
   translated, RTL-capable shell.
2. Kernel numeric types pass their full unit + property suites; the seven lint rules are
   enforced in CI and green.
3. Migrations run transactionally with checksum verification; the pre-migration backup and
   auto-restore-on-failure path is tested and works.
4. The outbox delivers an event end-to-end atomically; a forced handler failure retries and
   dead-letters visibly.
5. A durable job runs on schedule, survives an app restart with correct catch-up, and reports
   status in the UI panel.
6. Settings resolve by scope through typed accessors; a change emits `SettingChanged` and a
   subscriber reacts live.
7. The currency module converts across rate types and resolves a redenomination chain
   correctly, with tests.
8. Locale switches at runtime with no reload; RTL snapshot tests pass in both directions;
   i18n key coverage is complete.
9. The composition root builds the full graph and shuts down gracefully (drain → flush →
   checkpoint → close) with no data loss on mid-write close.
10. The module contract, both config registries, and the strategy registry exist, are
    documented, and are exercised by the currency module.

---

## Proposed sequence within Phase 0

Ordered by dependency; each step reviewable on its own.

| Step | Deliverable |
|---|---|
| 0.1 | Repo scaffold, Go module, tooling, lint rules, CI, empty Wails shell |
| 0.2 | Kernel: numeric types + primitives, with full unit/property tests |
| 0.3 | Platform DB: connections, dialect shim, UoW, repository/read-model split + contract suite |
| 0.4 | Migration runner + desktop safety layer + `0001_platform.sql` |
| 0.5 | Config registries (settings + metadata) and strategy registry (§CFG) |
| 0.6 | Event bus + transactional outbox, proven end-to-end |
| 0.7 | Durable job scheduler + outbox dispatcher + heartbeat |
| 0.8 | i18n platform + seed locale files |
| 0.9 | Currency module: schema `0002`, repositories, converter, seeds, tests |
| 0.10 | Composition root wiring the full graph + graceful shutdown |
| 0.11 | Frontend foundation: tokens, primitives, shell, providers, bindings wrapper |
| 0.12 | Phase 0 DoD review |

---

*End of Phase 0 detailed design. Awaiting your approval of this design before implementation
of step 0.1. Per the agreed process: design → review → approve → implement → review →
refactor, module by module.*