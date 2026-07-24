# Mizan ERP — System Architecture (v1.1)

> Status: **Core architecture APPROVED. Foundational decisions resolved — see §33.**
> No production code until Phase 0 detailed design is approved.
> Scope: architecture only. Implementation follows the approved design, module by module.
>
> **Companion document:** [`ADDENDUM_v1.1_resolved_design.md`](./ADDENDUM_v1.1_resolved_design.md)
> contains the detailed designs your decisions require: product variants, units of measure &
> fractional quantities, country/localization profiles, the costing abstraction, and four new
> recommendations arising from the Syria default market.

---

## 0. How to read this document

Your request listed 23 architectural areas. This document covers all of them plus several
areas that cannot be deferred without causing a schema redesign later. Mapping:

| Your item | Section |
|---|---|
| 1. Clean Architecture | §3, §5 |
| 2. Folder structure | §4 |
| 3. Package structure | §4, §5 |
| 4. Domain layer | §5.1 |
| 5. Application layer | §5.2 |
| 6. Infrastructure layer | §5.3 |
| 7. Dependency injection | §6 |
| 8. SQLite schema | §8, §9 |
| 9. Migration strategy | §10 |
| 10. Repository interfaces | §11 |
| 11. Service layer | §12 |
| 12. Authentication | §13 |
| 13. Authorization (RBAC) | §14 |
| 14. Audit system | §15 |
| 15. Settings system | §16 |
| 16. Currency architecture | §18 |
| 17. Internationalization | §22 |
| 18. Plugin architecture | §25 |
| 19. Feature flags | §17 |
| 20. Background workers | §24 |
| 21. Event bus | §23 |
| 22. Multi-branch | §26 |
| 23. Multi-warehouse | §26 |
| *(added)* Tax engine | §19 |
| *(added)* Accounting / GL | §20 |
| *(added)* Inventory costing | §21 |
| *(added)* Reporting, search, backup, testing, frontend | §27–§31 |

Sections marked **[RECOMMENDATION]** are places where I am proposing something different
from, or beyond, what you specified. Each states the reasoning and the cost of not doing it.
Section §33 lists the decisions I still need from you.

---

## 1. Guiding principles

1. **Configuration over code.** A new business type must be onboarded by seeding data and
   toggling flags — never by branching code. Any `if businessType == "pharmacy"` in the
   codebase is a design failure.
2. **The database outlives the application.** Schema decisions are the hardest to reverse,
   so they get the most conservatism: portable types, explicit constraints, no vendor tricks.
3. **Financial records are append-only.** Posted documents and journal entries are never
   mutated. Corrections happen by reversal. This is what makes the audit log trustworthy.
4. **Money is never a floating-point number.** Anywhere. Ever.
5. **The domain layer knows nothing about SQLite, Wails, React, or JSON.** It is plain Go
   with zero third-party imports beyond the standard library.
6. **Simplicity is a constraint, not an aspiration.** If a mechanism cannot be explained to a
   new engineer in five minutes, it needs justification.
7. **Every feature is off by default.** Feature flags gate everything; a minimal install is
   a clean POS, and complexity is opt-in.

---

## 2. System topology

Mizan is a **single-process desktop application**. There is no server, no network hop, and
no HTTP layer in v1.

```
┌─────────────────────────────────────────────────────────┐
│                    Wails Application                    │
│                                                         │
│  ┌───────────────────────┐    ┌──────────────────────┐  │
│  │  WebView (Chromium /  │    │      Go Runtime      │  │
│  │  WebKit)              │    │                      │  │
│  │                       │◄──►│  Bindings (§5.4)     │  │
│  │  React + Vite + TS    │ IPC│  Application (§5.2)  │  │
│  │  Tailwind             │    │  Domain (§5.1)       │  │
│  │  Presentation only    │    │  Infrastructure(§5.3)│  │
│  └───────────────────────┘    │  Workers (§24)       │  │
│                               └──────────┬───────────┘  │
└──────────────────────────────────────────┼──────────────┘
                                           ▼
                              ┌────────────────────────┐
                              │ SQLite (WAL) + backups │
                              │ mizan.db               │
                              └────────────────────────┘
```

**Consequences that shape everything below:**

- The frontend is untrusted in the same sense a browser is untrusted: it is a rendering
  surface. **All validation, authorization, calculation and money arithmetic happen in Go.**
  The frontend may pre-validate for UX, never for correctness.
- There is no request/response lifecycle to hang scoping off. Session, current branch,
  locale and permissions live in an explicit, in-process `AppContext` passed to use cases.
- Background work runs in goroutines inside the same process and must survive app restarts,
  so job state is persisted (§24).

**[RECOMMENDATION] Keep an HTTP transport seam even though v1 has no server.** The binding
layer (§5.4) should be a thin adapter over the same use cases an HTTP handler would call.
This costs nothing now and is what makes future cloud sync, a mobile companion app, or a
headless branch server possible without touching business logic.

---

## 3. Architectural style: modular monolith + Clean Architecture

### 3.1 The decision **[RECOMMENDATION]**

Clean Architecture is usually drawn as four horizontal layers, and many Go projects
translate that literally into top-level `domain/`, `usecase/`, `repository/`, `delivery/`
folders. **I recommend against that for Mizan**, and instead propose a **modular monolith**:
the top-level partition is by *business module* (bounded context), and Clean Architecture
layering happens *inside* each module.

```
❌ Layer-first                      ✅ Module-first (recommended)

internal/                           internal/modules/
├── domain/                         ├── sales/
│   ├── product.go                  │   ├── domain/
│   ├── invoice.go                  │   ├── app/
│   ├── journal.go                  │   ├── infra/
│   └── ... 80 more files           │   └── module.go
├── usecase/                        ├── inventory/
│   └── ... 200 more files          │   ├── domain/
└── repository/                     │   ├── app/
    └── ... 80 more files           │   ├── infra/
                                    │   └── module.go
                                    └── accounting/ ...
```

**Why this is the better decision for a 20-module ERP:**

- **Change locality.** A change to "how sales returns work" touches one directory. In the
  layer-first layout it touches four directories and you must reconstruct the feature in
  your head every time.
- **Enforceable boundaries.** Module-to-module dependencies become visible as package
  imports and can be linted. In a layer-first layout, everything is in one package soup and
  coupling grows silently — this is the single most common way an ERP becomes unmaintainable.
- **It is the only path to a real plugin system (§25).** A module that is already a
  self-contained unit with an explicit registration surface can later be extracted; a
  feature smeared across four layer folders cannot.
- **It is the only path to future service extraction** if you ever need a central server.

The Clean Architecture dependency rule is preserved exactly — it just applies within and
between modules:

```
   api/bindings ──┐
                  ▼
        modules/*/app  ──►  modules/*/domain  ◄── (depends on nothing)
                  ▲                  ▲
   modules/*/infra┘──────────────────┘
   (implements interfaces defined inward)
```

**Dependencies point inward, always.** `domain` imports nothing from `app`, `infra`, or
`api`. `infra` implements interfaces that `domain`/`app` declare.

### 3.2 Inter-module communication rules

Modules must not reach into each other's internals. Three legal channels only:

| Channel | Use for | Example |
|---|---|---|
| **Published contract** (`modules/x/contract`) | Synchronous queries and commands another module legitimately needs | Sales asks Inventory to reserve stock |
| **Domain events** (§23) | Reacting to something that happened; no reply needed | Sales posts an invoice → Accounting creates the journal entry |
| **Shared kernel** (`internal/kernel`) | Universal value objects only | `Money`, `Quantity`, `ID`, `Locale` |

A module exports a small `contract` package containing interfaces and DTOs. It never
exports its aggregates. Sales must not be able to construct an `inventory.StockLevel`.

**Preferred direction:** downstream modules react to events rather than upstream modules
calling them. Sales does not call Accounting; Sales emits `InvoicePosted` and Accounting
subscribes. This keeps the accounting module removable and the sales module ignorant of
double-entry — which is exactly what lets you expose accounting progressively.

---

## 4. Folder structure

```
mizan/
├── cmd/
│   └── mizan/
│       └── main.go                  # entrypoint: parse flags, call bootstrap, run Wails
│
├── internal/                        # nothing here is importable by external code
│   │
│   ├── bootstrap/                   # THE COMPOSITION ROOT (§6)
│   │   ├── bootstrap.go             # builds the whole object graph, in order
│   │   ├── modules.go               # registers every module
│   │   ├── bindings.go              # collects Wails-exposed structs
│   │   └── shutdown.go              # ordered graceful shutdown
│   │
│   ├── kernel/                      # shared domain primitives — NO business logic
│   │   ├── money/                   # Money, Currency, rounding, allocation
│   │   ├── quantity/                # Quantity + unit of measure
│   │   ├── id/                      # UUIDv7 generation & parsing
│   │   ├── clock/                   # Clock interface (real + fake for tests)
│   │   ├── locale/                  # Locale, TextDirection
│   │   ├── errs/                    # typed domain errors + error codes
│   │   ├── event/                   # DomainEvent interface, envelope
│   │   ├── validation/              # rule combinators
│   │   └── paging/                  # Page, Sort, Filter
│   │
│   ├── platform/                    # technical infrastructure — NO business logic
│   │   ├── database/
│   │   │   ├── db.go                # connection pools (1 writer, N readers) §8.4
│   │   │   ├── dialect/             # portability shim §8.5
│   │   │   ├── uow.go               # Unit of Work / transaction manager §11.3
│   │   │   └── sqlbuilder/          # thin, typed query helpers
│   │   ├── migrate/                 # migration runner §10
│   │   ├── eventbus/                # in-process bus §23
│   │   ├── outbox/                  # transactional outbox §23.3
│   │   ├── jobs/                    # scheduler + durable worker pool §24
│   │   ├── i18n/                    # catalog loading, message resolution §22
│   │   ├── crypto/                  # Argon2id, random tokens, hashing
│   │   ├── logging/                 # structured logging (slog), redaction
│   │   ├── config/                  # boot config: paths, db location, log level
│   │   ├── telemetry/               # local-only metrics & timing (opt-in)
│   │   ├── fs/                      # app data dirs, atomic file writes
│   │   └── printing/                # render pipeline for receipts/documents
│   │
│   ├── modules/                     # THE BUSINESS. One folder per bounded context.
│   │   ├── org/                     # company, branches, warehouses, fiscal setup
│   │   ├── identity/                # users, roles, permissions, sessions §13 §14
│   │   ├── settings/                # layered settings + feature flags §16 §17
│   │   ├── audit/                   # append-only audit trail §15
│   │   ├── currency/                # currencies, rates, providers §18
│   │   ├── tax/                     # tax codes, groups, exemptions, engine §19
│   │   ├── accounting/              # CoA, journals, GL, periods, posting rules §20
│   │   ├── catalog/                 # products, variants, categories, UoM, barcodes
│   │   ├── partners/                # customers, suppliers, contacts
│   │   ├── inventory/               # stock, movements, valuation, transfers §21
│   │   ├── sales/                   # POS, invoices, returns, quotations
│   │   ├── purchasing/              # purchase orders, bills, returns
│   │   ├── payments/                # receipts, payments, debts, settlements
│   │   ├── expenses/                # expense entries and categories
│   │   ├── reporting/               # read models & report queries §27
│   │   ├── search/                  # search abstraction §28
│   │   ├── backup/                  # backup/restore/verify §29
│   │   └── notifications/           # in-app notification centre
│   │
│   └── api/                         # the Wails-facing adapter layer §5.4
│       ├── bindings/                # one struct per module, exposed to JS
│       ├── dto/                     # wire-shaped types (never domain types)
│       ├── mapper/                  # domain ⇄ dto
│       ├── envelope/                # Result<T> / error envelope
│       └── appctx/                  # session, branch, locale resolution
│
├── migrations/
│   └── sqlite/                      # 0001_init.sql, 0002_….sql  (§10)
│       └── (future) postgres/ mysql/ sqlserver/
│
├── seeds/                           # portable seed data (JSON)
│   ├── chart_of_accounts/           # per-country CoA templates
│   ├── business_profiles/           # furniture.json, pharmacy.json, …
│   ├── tax_profiles/                # per-country tax setups
│   └── currencies.json
│
├── locales/                         # SINGLE source of truth for both Go and React §22
│   ├── en/  common.json, sales.json, errors.json, …
│   └── ar/  common.json, sales.json, errors.json, …
│
├── frontend/                        # React + Vite + TS §31
│   ├── src/
│   │   ├── app/                     # router, providers, shell
│   │   ├── modules/                 # mirrors internal/modules
│   │   ├── shared/                  # design system, hooks, utils
│   │   ├── i18n/
│   │   └── lib/wails/               # generated bindings wrapper
│   └── ...
│
├── docs/
│   ├── architecture/                # this document + ADRs
│   ├── modules/                     # per-module design docs (written before each build)
│   └── decisions/                   # ADR-0001…N
│
├── test/
│   ├── fixtures/
│   ├── integration/
│   └── e2e/
│
└── build/                           # Wails build assets, icons, installers
```

### 4.1 Anatomy of a module

Every module has the same shape. This uniformity is deliberate — you should be able to open
any module and know where everything is.

```
internal/modules/sales/
├── domain/                          # pure Go, no imports outside kernel + stdlib
│   ├── invoice.go                   # the aggregate root + invariants
│   ├── invoice_line.go
│   ├── status.go                    # value objects / enums
│   ├── events.go                    # InvoicePosted, InvoiceVoided, …
│   ├── errors.go                    # ErrInvoiceAlreadyPosted, …
│   ├── repository.go                # InvoiceRepository interface (declared inward)
│   ├── numbering.go                 # domain service: document numbering
│   └── invoice_test.go              # pure unit tests, no DB
│
├── app/                             # orchestration
│   ├── commands/
│   │   ├── create_invoice.go        # CreateInvoiceHandler
│   │   ├── post_invoice.go
│   │   └── void_invoice.go
│   ├── queries/
│   │   ├── list_invoices.go         # reads via read models, not aggregates
│   │   └── get_invoice.go
│   ├── ports.go                     # interfaces this module needs from elsewhere
│   │                                #   (TaxEngine, StockReserver, RateProvider)
│   └── policies.go                  # permission requirements per use case
│
├── infra/
│   ├── sqlite/
│   │   ├── invoice_repository.go    # implements domain.InvoiceRepository
│   │   ├── invoice_readmodel.go     # implements query-side interfaces
│   │   └── mapping.go               # row ⇄ entity
│   └── subscribers/                 # event handlers this module owns
│
├── contract/                        # what OTHER modules may use
│   ├── contract.go                  # e.g. SalesQuery interface
│   └── events.go                    # re-exported event types
│
└── module.go                        # registration surface (see below)
```

`module.go` is the module's only registration point:

```go
// Illustrative shape only — not final code.
type Module struct{ /* deps */ }

func (m *Module) Name() string                     { return "sales" }
func (m *Module) Permissions() []identity.PermissionDef  // declares its own permissions
func (m *Module) FeatureFlags() []settings.FlagDef       // declares its own flags
func (m *Module) Migrations() fs.FS                      // owns its schema files
func (m *Module) Subscribe(bus event.Bus)                // registers event handlers
func (m *Module) Jobs() []jobs.Definition                // registers background jobs
func (m *Module) Bindings() any                          // exposes the Wails binding struct
func (m *Module) Seed(ctx, tx) error                     // seeds reference data
```

This interface is the seam that later becomes the plugin contract (§25).

---

## 5. Layer contracts

### 5.1 Domain layer

**Contains:** entities, aggregate roots, value objects, domain events, domain errors,
domain services (logic that doesn't belong to a single entity), and repository *interfaces*.

**Rules:**
- Imports only the Go standard library and `internal/kernel`. No SQL, no JSON tags, no
  Wails, no context-carrying frameworks. `context.Context` is allowed only in repository
  interface signatures.
- Aggregates are **always constructed valid**. There is no public zero-value entity that
  can be filled in field-by-field; construction goes through a factory that enforces
  invariants and returns a typed error.
- Fields are unexported with intention-revealing methods. `invoice.AddLine(...)` — not
  `invoice.Lines = append(...)`. This is what prevents a use case from bypassing an
  invariant two years from now.
- State changes append domain events to the aggregate; the application layer collects and
  publishes them after a successful commit.

**Example of the invariant style** (illustrative):

```go
// Invoice is an aggregate root. It is the ONLY way invoice lines may be modified.
type Invoice struct {
    id        id.ID
    status    Status
    lines     []Line
    events    []event.DomainEvent
    // ...
}

// Post transitions a draft invoice to posted. A posted invoice is immutable:
// corrections are made by issuing a credit note, never by editing.
func (i *Invoice) Post(at time.Time, by id.ID) error {
    if i.status != StatusDraft {
        return errs.Conflict(ErrCodeInvoiceNotDraft, "invoice is not a draft")
    }
    if len(i.lines) == 0 {
        return errs.Invalid(ErrCodeInvoiceEmpty, "invoice has no lines")
    }
    i.status = StatusPosted
    i.record(InvoicePosted{InvoiceID: i.id, At: at, By: by, Total: i.total})
    return nil
}
```

### 5.2 Application layer

**Contains:** use cases (one type per use case, not fat services), input/output DTOs,
ports (interfaces for things the module needs from outside), and per-use-case policies.

**Rules:**
- **One use case = one struct with one `Handle` method.** Not a `SalesService` with 40
  methods. This keeps dependencies honest — a use case declares exactly what it needs — and
  makes each unit trivially testable.
- The use case owns the transaction boundary (§11.3). Domain objects never open transactions.
- Authorization is enforced here, declaratively (§14.3), not inside the domain.
- **Commands go through aggregates; queries do not.** Reads use read models and
  purpose-built SQL (§27). Loading a 400-line invoice aggregate to render a list row is
  the classic way ERP screens become slow.

### 5.3 Infrastructure layer

**Contains:** repository implementations, read-model queries, external service adapters
(exchange-rate providers, printers), file system access, and anything touching a driver.

**Rules:**
- Implements interfaces declared inward. Never declares interfaces others must implement.
- Contains **no business rules**. If a repository is deciding something, the decision is in
  the wrong place. A repository maps and persists; it does not compute totals or check
  permissions.
- SQL lives here and nowhere else, in explicit, readable statements. No ORM (§8.6).

### 5.4 API / bindings layer

**Contains:** Wails-exposed structs, wire DTOs, mappers, the result envelope, and app
context resolution.

**Rules:**
- Bindings are thin: resolve context → map DTO → call use case → map result → return.
  A binding method that is longer than ~15 lines is doing work that belongs in a use case.
- **Domain types are never exposed to JavaScript.** Always DTOs. This decouples the UI from
  domain refactoring and prevents accidental leakage of internal fields.
- Every call returns a uniform envelope so the frontend has one error path:

```go
type Result[T any] struct {
    OK    bool       `json:"ok"`
    Data  T          `json:"data,omitempty"`
    Error *APIError  `json:"error,omitempty"`
}

type APIError struct {
    Code       string            `json:"code"`        // "sales.invoice.not_draft"
    MessageKey string            `json:"messageKey"`  // i18n key — UI translates
    Params     map[string]string `json:"params"`      // interpolation values
    Fields     []FieldError      `json:"fields"`      // per-field validation errors
}
```

Note the consequence: **the backend never returns a human-readable English string.** It
returns a code and parameters; the frontend renders it in the active locale. This is what
makes "no restart on language change" actually true, including for error messages (§22).

---

## 6. Dependency injection

**[RECOMMENDATION] Manual constructor injection with a single composition root. No DI
container, no reflection, no code generation.**

Reflection-based containers (`dig`, `fx`) trade compile-time safety for convenience and
turn a missing dependency into a runtime panic during app startup on a customer's machine.
`google/wire` is safe but adds a code-generation step and an indirection for a problem we
do not have at this size. Explicit wiring in one file is boring, greppable, and the compiler
verifies it.

`internal/bootstrap/bootstrap.go` builds the graph in strict order:

```
1. Config & paths            (where is the DB, where are logs)
2. Logging
3. Clock
4. Database connections      (writer + reader pools, PRAGMAs)
5. Migration runner          → run pending migrations (backup first §10.4)
6. Event bus + outbox
7. Platform services         (crypto, i18n catalogs, jobs scheduler)
8. Module construction       (dependency order: kernel modules first)
9. Cross-module wiring       (event subscriptions, port bindings)
10. Seeding                  (first-run only)
11. Job registration + scheduler start
12. Binding collection       → hand to Wails
13. Shutdown hooks registered in reverse order
```

Module construction order is explicit and acyclic:

```
org → identity → settings → audit → currency → tax → accounting
    → catalog → partners → inventory → purchasing → sales
    → payments → expenses → reporting → search → backup → notifications
```

If a cycle ever appears, the fix is an event (§23), never a shared mutable singleton.

**No global variables. No `init()` side effects. No singletons.** Everything a component
needs arrives through its constructor. This is the single most effective rule for keeping a
large Go codebase testable.

---

## 7. Cross-cutting primitives

These are decided once, in `internal/kernel`, and used everywhere. Getting them wrong is
expensive; getting them right removes a whole class of bugs permanently.

### 7.1 Money **[CRITICAL]**

```go
type Money struct {
    minor    int64   // amount in minor units (e.g. cents)
    currency Currency
}
```

- **Never `float64`.** Ever. `0.1 + 0.2 != 0.3` is not acceptable in an accounting system.
- Stored in the DB as `BIGINT` (minor units) + `CHAR(3)` currency code.
- Scale (decimal places) is a property of the currency, from the `currencies` table —
  not hardcoded to 2. JOD/KWD/BHD use 3; JPY uses 0.
- Arithmetic between different currencies returns an error, not a silent conversion.
- **Allocation, not division.** Splitting 100.00 three ways gives 33.34 / 33.33 / 33.33 —
  the largest-remainder algorithm guarantees the parts sum exactly to the whole. This is
  required for discount distribution, tax apportionment, and instalment plans.
- Rounding mode is explicit and configurable per context (half-up, half-even/banker's).

### 7.2 Quantity

`int64` scaled by 10^6, plus a unit of measure. Supports fractional units (0.25 kg, 1.5 m)
without float error, and supports piece-based products with the same type.

### 7.3 Rates and percentages

Exchange rates: `int64` scaled by **10^9**. Tax rates: `int64` scaled by 10^6 (so 15% is
`150000`). Percentages are never floats for the same reason money isn't.

### 7.4 Identifiers **[RECOMMENDATION]**

**UUIDv7 for all primary keys**, stored as `CHAR(36)`.

Rationale — this is a decision forced by your own roadmap:
- Auto-increment integers **cannot** be reconciled across branches. The moment you have two
  branches creating invoices offline and syncing, integer IDs collide. Retrofitting UUIDs
  after data exists is a migration nightmare across every foreign key.
- UUIDv7 is **time-ordered**, so unlike UUIDv4 it does not destroy B-tree index locality —
  it keeps insert performance close to sequential integers.
- `CHAR(36)` over `BINARY(16)`: costs ~20 bytes per key but is portable across all four
  target databases without vendor-specific types, and is legible in logs and support
  sessions. For a desktop ERP's data volumes this is the right trade. If a specific table
  ever proves hot enough to matter, it can be changed in isolation.

**Human-facing document numbers are a separate concern** and are *not* the primary key —
see §9.4 number series.

### 7.5 Time

- All timestamps stored **UTC**, ISO-8601 with milliseconds: `2026-07-24T18:52:00.000Z`,
  as `CHAR(24)`. Lexicographic order equals chronological order, it is unambiguous, and it
  maps cleanly to `TIMESTAMP`/`DATETIME2` on migration.
- Business dates (invoice date, accounting period date) are stored **separately** as
  `CHAR(10)` (`2026-07-24`) — a business date is not an instant, and timezone-shifting an
  invoice into the previous fiscal period is a real and costly bug.
- A `Clock` interface everywhere; `time.Now()` is never called in domain or application code.

### 7.6 Errors

Typed, coded errors with a category that maps to UI behaviour:

| Category | Meaning | UI treatment |
|---|---|---|
| `Validation` | Bad input, field-level | Inline field errors |
| `NotFound` | Missing entity | Empty state |
| `Conflict` | Invariant/state violation | Toast + explanation |
| `Permission` | RBAC denial | Blocked with reason |
| `Internal` | Bug or infrastructure fault | Generic message + logged detail |

Every error carries a stable code (`sales.invoice.not_draft`) that doubles as the i18n key.
Codes are part of the public contract and are never renamed once shipped.

---

## 8. Database portability contract

You require that nothing block a future move to PostgreSQL, MySQL, or SQL Server. That is a
real constraint with real consequences, so it is written down here as an enforceable contract.

### 8.1 The type contract

Every column uses one of these logical types. Nothing else is permitted.

| Logical type | SQLite (v1) | PostgreSQL | MySQL | SQL Server |
|---|---|---|---|---|
| ID | `CHAR(36)` | `CHAR(36)`/`uuid` | `CHAR(36)` | `CHAR(36)` |
| Short text | `VARCHAR(n)` | `VARCHAR(n)` | `VARCHAR(n)` | `NVARCHAR(n)` |
| Long text | `TEXT` | `TEXT` | `TEXT` | `NVARCHAR(MAX)` |
| Money (minor units) | `BIGINT` | `BIGINT` | `BIGINT` | `BIGINT` |
| Quantity (×10⁶) | `BIGINT` | `BIGINT` | `BIGINT` | `BIGINT` |
| Rate (×10⁹) | `BIGINT` | `BIGINT` | `BIGINT` | `BIGINT` |
| Integer | `INTEGER` | `INTEGER` | `INT` | `INT` |
| Boolean | `SMALLINT` + `CHECK (c IN (0,1))` | `SMALLINT` | `TINYINT` | `TINYINT` |
| Timestamp (UTC) | `CHAR(24)` | `CHAR(24)`→`TIMESTAMPTZ` | `CHAR(24)` | `CHAR(24)` |
| Date | `CHAR(10)` | `CHAR(10)`→`DATE` | `CHAR(10)` | `CHAR(10)` |
| Enum | `VARCHAR(32)` + `CHECK` | same | same | same |
| JSON blob | `TEXT` (validated in Go) | `TEXT`→`JSONB` | `TEXT`→`JSON` | `NVARCHAR(MAX)` |
| Binary | `BLOB` | `BYTEA` | `BLOB` | `VARBINARY(MAX)` |

### 8.2 Forbidden (SQLite-specific or non-portable)

- ❌ `AUTOINCREMENT` / rowid aliasing / `WITHOUT ROWID` / `rowid` references
- ❌ Relying on SQLite's dynamic typing — every column has one type and one meaning
- ❌ `INTEGER PRIMARY KEY` as an implicit rowid alias
- ❌ Partial/filtered indexes (MySQL has no equivalent)
- ❌ Expression indexes and functional indexes in core schema
- ❌ `strftime`, `julianday`, `datetime()` and other SQLite date functions in SQL
- ❌ Business logic in triggers (audit, totals, and stock are computed in Go)
- ❌ FTS5 virtual tables in the core schema (isolated behind the search port, §28)
- ❌ `REAL`/`FLOAT`/`DOUBLE` for any monetary or quantity value
- ❌ Multi-statement DDL relying on SQLite's permissive `ALTER TABLE`
- ❌ `INSERT OR REPLACE`, `UPSERT` syntax variants — use explicit select/insert/update
  inside a transaction, or a dialect-abstracted upsert helper

### 8.3 Naming conventions (portable, no reserved words)

- Tables: `snake_case`, **plural** — `sales_invoices`, `journal_entries`
- Columns: `snake_case`, singular — `customer_id`, `posted_at`
- Primary key: always `id`
- Foreign key: `<singular_table>_id`
- Booleans: `is_`/`has_` prefix — `is_active`, `has_tax`
- Timestamps: `_at` suffix; dates: `_date` suffix
- Indexes: `ix_<table>_<cols>`; unique: `ux_<table>_<cols>`; FK: `fk_<table>_<ref>`;
  check: `ck_<table>_<rule>`
- Never use `order`, `group`, `user`, `key`, `value`, `check`, `transaction`, `index` as
  identifiers — reserved in at least one target DB. Use `sales_orders`, `users` (plural is
  safe), `setting_key`, `setting_value`.
- Maximum identifier length 30 characters (Oracle-safe, and comfortably within all four).

### 8.4 SQLite runtime configuration (v1)

Applied at connection open, isolated in `platform/database`:

```
PRAGMA journal_mode = WAL;         -- concurrent readers during writes
PRAGMA synchronous = NORMAL;       -- safe with WAL, much faster than FULL
PRAGMA foreign_keys = ON;          -- MUST be set per connection
PRAGMA busy_timeout = 5000;
PRAGMA temp_store = MEMORY;
PRAGMA cache_size = -64000;        -- 64 MB
```

**Connection strategy — this matters.** SQLite permits exactly one writer. The pool is
therefore split:

- **Writer pool: `MaxOpenConns(1)`.** All writes and all transactions go here. This turns
  potential `SQLITE_BUSY` errors into simple queue waits.
- **Reader pool: `MaxOpenConns(N)`** (N ≈ 4–8), read-only, for reports and list screens.

This split is expressed as `db.Writer()` / `db.Reader()` in the platform layer. On
PostgreSQL both simply return the same pool — the abstraction survives the migration.

### 8.5 The dialect shim

A small `dialect` interface (~10 methods) isolates the handful of genuinely
database-specific concerns: placeholder style (`?` vs `$1`), identifier quoting, upsert
syntax, `LIMIT/OFFSET` vs `OFFSET/FETCH`, boolean literals, and current-timestamp. Nothing
else is allowed to be dialect-aware. Repositories write plain SQL against the contract above.

### 8.6 No ORM **[RECOMMENDATION]**

Use `database/sql` with explicit SQL, plus `sqlx`-style scanning helpers or `sqlc` for
type-safe generated queries.

Reasoning: ORMs (GORM et al.) generate unpredictable SQL, hide N+1 queries, encourage
leaking persistence concerns into entities via struct tags, and their portability promise
breaks down exactly where it matters (upserts, window functions, locking). For a system
where query performance and financial correctness are non-negotiable, explicit SQL is both
simpler and safer. The cost — writing mapping code — is mechanical and reviewable.

**Recommended:** `sqlc` for read models and reports (compile-time-checked SQL, generated
structs), hand-written SQL for aggregate repositories where mapping is nontrivial.

---

## 9. Schema design

Full DDL will be delivered per module during implementation. This section fixes the shape,
the relationships, and the decisions that are expensive to reverse.

### 9.1 Universal columns

Every business table carries:

```sql
id            CHAR(36)  NOT NULL PRIMARY KEY,   -- UUIDv7
created_at    CHAR(24)  NOT NULL,
created_by    CHAR(36),
updated_at    CHAR(24)  NOT NULL,
updated_by    CHAR(36),
row_version   INTEGER   NOT NULL DEFAULT 1,     -- optimistic concurrency
```

Transactional tables additionally carry the dimensions required for the multi-branch future
(§26) — **present from migration 0001, never nullable**:

```sql
branch_id     CHAR(36)  NOT NULL,
```

Soft deletes: **used sparingly and deliberately.** Master data (products, customers) uses
`is_active` rather than deletion. Transactional documents are **never deleted** — they are
voided or reversed. There is no global `deleted_at` column, because a soft-delete column on
every table silently breaks every unique constraint and every join.

Optimistic concurrency: `row_version` is checked on update (`WHERE id = ? AND row_version = ?`)
and incremented. Two cashiers editing the same document produce a clean conflict error, not
a lost update.

### 9.2 Module ownership map

| Module | Owns tables |
|---|---|
| org | `companies`, `branches`, `warehouses`, `fiscal_years`, `fiscal_periods` |
| identity | `users`, `roles`, `permissions`, `role_permissions`, `user_roles`, `sessions`, `login_attempts` |
| settings | `settings`, `feature_flags`, `business_profiles` |
| audit | `audit_log` |
| currency | `currencies`, `exchange_rates`, `exchange_rate_batches`, `rate_provider_config` |
| tax | `taxes`, `tax_versions`, `tax_groups`, `tax_group_items`, `tax_exemptions`, `tax_jurisdictions` |
| accounting | `accounts`, `account_types`, `journal_entries`, `journal_lines`, `posting_rules`, `account_mappings`, `account_balances` |
| catalog | `products`, `product_variants`, `categories`, `units_of_measure`, `uom_conversions`, `barcodes`, `price_lists`, `price_list_items`, `translations` |
| partners | `partners`, `partner_addresses`, `partner_contacts`, `partner_balances` |
| inventory | `stock_levels`, `stock_movements`, `inventory_layers`, `stock_counts`, `stock_transfers`, `lots`, `serials` |
| sales | `sales_documents`, `sales_document_lines`, `sales_document_taxes`, `sales_payments` |
| purchasing | `purchase_documents`, `purchase_document_lines`, `purchase_document_taxes` |
| payments | `payment_methods`, `payment_transactions`, `settlements`, `settlement_allocations` |
| expenses | `expense_categories`, `expenses` |
| platform | `schema_migrations`, `outbox_events`, `jobs`, `job_runs`, `number_series` |

**[RECOMMENDATION] One `partners` table, not separate `customers` and `suppliers`.** In real
businesses the same entity is frequently both (you buy fabric from a company that also buys
furniture from you). Two tables means duplicated contacts, split balances, and a painful
merge later. Use one table with role flags (`is_customer`, `is_supplier`) and role-specific
extension tables for terms/credit limits. The UI still presents "Customers" and "Suppliers"
as separate screens — this is a storage decision, not a UX decision.

**[RECOMMENDATION] One `sales_documents` table for quotation / order / invoice / return,
discriminated by `doc_type`,** rather than four near-identical table sets. They share ~95% of
their columns and all of their line structure. This makes conversion (quote → order →
invoice) a cheap operation with a clean lineage chain (`source_document_id`) instead of a
cross-table copy, and it halves the reporting surface. Same pattern for purchasing.

### 9.3 Document immutability

Every financial document has a lifecycle:

```
draft ──► posted ──► (paid / partially_paid) 
             │
             ├──► voided        (same-period cancellation, reversing entry)
             └──► reversed_by   (credit note / debit note, new document)
```

`draft` is freely editable. **`posted` is immutable** — no UPDATE of amounts, lines, taxes,
dates, or partner. Only status and settlement links may change. Enforced in the domain
layer, verified by integration tests, and visible in the audit trail.

Every posted document line stores a **snapshot** of everything used to compute it: unit
price, discount, tax rate applied, exchange rate used, cost at time of sale. Never a
reference to a mutable current value. Reprinting a two-year-old invoice must reproduce it
byte-identically even after prices, tax rates and exchange rates have all changed.

### 9.4 Number series

Human-readable document numbers are generated from a configurable series, independent of
the UUID primary key:

```sql
number_series (
  id, code,               -- 'SALES_INVOICE'
  branch_id,              -- per-branch sequences
  fiscal_year_id,         -- optional per-year reset
  prefix, suffix,         -- 'INV-', ''
  padding,                -- 6 → INV-000123
  next_value,
  is_gapless,             -- legal requirement in some jurisdictions
  ...
)
```

**DECIDED (§33.4): v1 uses unique sequential numbering, not gapless.** `is_gapless` remains a
per-series flag defaulting to `0`, so strict gapless numbering becomes a configuration change
for jurisdictions that require it — no migration.

Two rules apply regardless of mode, and both are needed for correctness:

1. **The number is allocated at posting, never at draft creation.** Abandoned drafts must not
   consume numbers. This keeps the sequence clean in normal operation even without a gapless
   guarantee.
2. **Allocation is transactional.** `next_value` is read and incremented inside the posting
   transaction (SQLite's single writer makes this naturally safe; on PostgreSQL it becomes a
   `SELECT … FOR UPDATE`, isolated in the dialect shim). Two terminals posting simultaneously
   can never produce a duplicate number.

Uniqueness is enforced by `UNIQUE (series_id, branch_id, fiscal_year_id, document_number)`.

### 9.5 Translatable content **[RECOMMENDATION]**

Do **not** add `name_ar` / `name_en` columns to products, categories, and accounts. Adding a
third language would then require a migration on every table, and every query would need to
know about language columns.

Instead: a base `name` column (fallback) plus a generic side table:

```sql
translations (
  id, entity_type, entity_id, field_name, locale, value, ...
  UNIQUE (entity_type, entity_id, field_name, locale)
)
```

Loaded through a small resolver with an in-memory cache. Adding Kurdish, French, or Turkish
becomes a data operation, not a schema change — which is exactly what your i18n requirement
("architecture must allow adding future languages") demands.

---

## 10. Migration strategy

### 10.1 Rules

1. **Forward-only, numbered, immutable.** `0001_init.sql`, `0002_add_tax_groups.sql`. A
   migration that has shipped is never edited — correcting it means a new migration.
2. **Embedded** via `go:embed`. The binary is self-sufficient; no files to ship alongside.
3. **Transactional.** Each migration runs inside a transaction and is recorded in
   `schema_migrations` (version, name, checksum, applied_at, duration_ms) in the same
   transaction. A failed migration leaves no partial state.
4. **Checksummed.** On startup, applied migrations are verified against their recorded
   checksum. A modified historical migration is a hard startup failure — this catches the
   "someone edited 0003 after release" class of corruption.
5. **Dialect-partitioned directories** from day one: `migrations/sqlite/`. When PostgreSQL
   arrives, `migrations/postgres/` sits beside it with the same version numbers.
6. **Data migrations are separate from schema migrations** and are idempotent.
7. **Seeds are not migrations.** Reference data (chart of accounts templates, currencies,
   business profiles) lives in `seeds/` as versioned JSON, applied idempotently.

### 10.2 Tooling **[RECOMMENDATION]**

Use **`pressly/goose`** as an embedded library (not the CLI) rather than writing a custom
migrator. It supports `embed.FS`, transactional migrations, Go-code migrations for data
transformations, and is battle-tested. Writing our own would be ~300 lines of code we'd have
to maintain and test for a solved problem. The one thing we add on top is §10.4.

### 10.3 Module-owned migrations

Each module owns its migration files, but versions are **globally sequenced** to keep
ordering deterministic across modules with foreign keys between them. The bootstrap
collects module FSs into one ordered set.

### 10.4 Desktop-specific safety **[CRITICAL]**

This is a desktop app: the user *is* the DBA, there is no ops team, and a failed migration
on a customer machine means lost business data.

1. **Automatic backup before any migration run**, to a timestamped file, verified readable.
2. **Integrity check** (`PRAGMA integrity_check`) before migrating.
3. On failure: transaction rollback, then **automatic restore** from the pre-migration
   backup, then a clear user-facing error with the backup path.
4. **Version gate:** a database newer than the running binary refuses to open (prevents an
   older installer from corrupting a newer database).
5. Migration progress is shown in the UI for long runs — never a frozen splash screen.

---

## 11. Repository interfaces & transactions

### 11.1 Interface style

Repositories are declared in `domain/`, named after the aggregate, and are deliberately
narrow. **A repository is a collection of aggregates, not a query API.**

```go
// internal/modules/sales/domain/repository.go
type InvoiceRepository interface {
    NextID(ctx context.Context) (id.ID, error)
    FindByID(ctx context.Context, id id.ID) (*Invoice, error)
    Save(ctx context.Context, inv *Invoice) error     // insert or update, version-checked
    ExistsByNumber(ctx context.Context, n string) (bool, error)
}
```

What repositories deliberately do **not** have: `FindAll`, `Search`, `FindByFilter`,
`GetPage`, or anything returning a projection. Those are read-model queries (§27) and live
in separate, query-side interfaces. Mixing them is how repository interfaces grow to 40
methods and become untestable.

### 11.2 Aggregate boundaries

An aggregate is loaded and saved whole. `Invoice` includes its lines and tax lines;
`JournalEntry` includes its lines. References across aggregates are by ID only — an
`Invoice` holds a `customerID`, never a `*Partner`. This keeps transactions small,
prevents accidental deep loading, and is what allows the modules to stay decoupled.

### 11.3 Unit of Work

Transactions are owned by the application layer and propagated implicitly through
`context.Context`, so repositories don't take a `*sql.Tx` parameter and business code
doesn't thread it manually:

```go
func (h *PostInvoiceHandler) Handle(ctx context.Context, cmd PostInvoice) error {
    return h.uow.Do(ctx, func(ctx context.Context) error {
        inv, err := h.invoices.FindByID(ctx, cmd.InvoiceID)   // uses tx from ctx
        // ... domain operations ...
        if err := h.invoices.Save(ctx, inv); err != nil { return err }
        return h.outbox.Publish(ctx, inv.PullEvents()...)      // same transaction §23.3
    })
}
```

Guarantees: nested `Do` calls join the existing transaction rather than opening a second
one (which would deadlock on SQLite's single writer); domain events are published to the
outbox **inside** the transaction and dispatched **after** commit.

---

## 12. Service layer

Three distinct kinds of "service", kept separate because conflating them is how a
`SalesService` grows to 3,000 lines:

| Kind | Lives in | Purpose | Example |
|---|---|---|---|
| **Domain service** | `domain/` | Logic spanning several entities of the same module, no I/O | Invoice total calculation, stock valuation math |
| **Application service (use case)** | `app/commands`, `app/queries` | Orchestrates one operation: transaction, permissions, repositories, events | `PostInvoiceHandler` |
| **Infrastructure service** | `infra/` | Technical capability behind a port | `ECBRateProvider`, `ThermalPrinter` |

The use case pipeline is uniform, implemented as composable decorators rather than repeated
boilerplate in every handler:

```
Binding
  └─ Recover (panic → Internal error)
      └─ Logging + correlation ID
          └─ Authenticate (session valid?)          §13
              └─ Authorize (permission + scope?)    §14
                  └─ Validate (input DTO)
                      └─ FeatureFlag (enabled?)     §17
                          └─ Transaction (UoW)      §11.3
                              └─ HANDLER (business logic only)
                          ── commit ──
                      └─ Dispatch events            §23
                  └─ Audit                          §15
```

The handler itself therefore contains business orchestration and nothing else — no
permission checks, no logging, no transaction management, no audit calls.

---

## 13. Authentication

### 13.1 Model

Local-first, no external identity provider in v1, designed so one can be added.

- **Password hashing: Argon2id**, parameters stored per-hash so they can be increased over
  time without invalidating existing passwords. (Argon2id over bcrypt: memory-hard, current
  best practice, and pure-Go implementations are readily available with no cgo.)
- **PIN login for POS.** Cashiers switch users dozens of times per shift; a full password is
  unusable there. A PIN is a *second, weaker* credential, allowed only when a device is
  already unlocked by a full login, scoped to POS-only permissions, rate-limited, and
  separately hashed. This is a deliberate, bounded trade-off, configurable per deployment.
- **Sessions** are in-process objects backed by a `sessions` table so they survive restart if
  "stay signed in" is enabled. Configurable idle timeout and absolute expiry.
- **Lockout:** configurable failed-attempt threshold, exponential backoff, all attempts
  recorded in `login_attempts` and surfaced in the audit log.
- **Password policy** is configuration (§16): length, complexity, expiry, reuse history.
- **First-run setup** creates the initial administrator; there is no default password shipped
  in the binary.

### 13.2 What is explicitly deferred (but designed for)

Windows Hello / Touch ID unlock, LDAP/Active Directory, and cloud SSO all plug in behind a
single `Authenticator` port. No schema change required.

### 13.3 Database encryption

**[RECOMMENDATION] Do not use SQLCipher in v1.** It is SQLite-specific (violating your
portability requirement), complicates the build with cgo, and breaks standard tooling. For
data-at-rest protection, prefer OS-level disk encryption plus **field-level encryption** for
the few genuinely sensitive columns (via a `crypto` port), which is portable across all four
databases. Revisit if a specific compliance requirement demands full-file encryption.

---

## 14. Authorization (RBAC)

### 14.1 Model

```
users ──< user_roles >── roles ──< role_permissions >── permissions
                           │
                    (optionally scoped to branch — §26)
```

- **Permissions are code-defined, not user-created.** Each module declares its permissions in
  `module.go`; a startup sync inserts new ones and marks removed ones obsolete. This means the
  permission list can never drift from what the code actually checks.
- **Roles are user-created** and freely editable, seeded with sensible defaults
  (Administrator, Manager, Cashier, Accountant, Stock Keeper, Viewer).
- **Permission naming:** `<module>.<resource>.<action>` —
  `sales.invoice.create`, `sales.invoice.void`, `accounting.journal.post`,
  `reports.profit.view`, `settings.company.edit`.
- Wildcards supported for role definition only: `sales.*`, `*`.

### 14.2 Beyond simple permissions **[RECOMMENDATION]**

Pure RBAC is insufficient for a real ERP and adding the missing pieces later means reworking
every check. Include from day one:

1. **Scope.** A permission is granted *within a scope*: global, branch, or warehouse. A
   branch manager has `sales.invoice.void` in their branch only. The check signature is
   `Can(ctx, permission, scope)` from the very first implementation, even while only the
   global scope exists.
2. **Field-level restrictions.** "Can see cost price" and "can see profit margin" are
   permissions, not screens. Sales staff routinely must not see purchase cost. This is
   enforced at the DTO-mapping layer so restricted fields are never serialized to the
   frontend at all.
3. **Approval limits.** "May discount up to 10%", "may void invoices under $500". Modelled as
   typed limits attached to roles, evaluated by the domain, and overridable by a supervisor
   approval flow. Deferring this means retrofitting an approval concept into every
   transactional use case later.

### 14.3 Enforcement

Declarative, adjacent to the use case, never buried inside it:

```go
func (h *VoidInvoiceHandler) Policy() app.Policy {
    return app.Requires("sales.invoice.void").
        InScope(app.ScopeBranch).
        WithLimit(app.LimitVoidAmount)
}
```

The authorization decorator (§12) reads the policy. **A use case without a declared policy
fails to start the application** — a compile-time-adjacent guarantee that nothing ships
unprotected by accident. The frontend receives the effective permission set to hide
unavailable actions, but that is purely cosmetic; the backend is the only enforcement point.

---

## 15. Audit system

### 15.1 Design

Append-only `audit_log`, written from event subscribers rather than sprinkled calls:

```sql
audit_log (
  id, occurred_at, actor_user_id, actor_name_snapshot,
  branch_id, session_id, correlation_id,
  action,                    -- 'sales.invoice.posted'
  entity_type, entity_id, entity_label_snapshot,
  before_json, after_json, changed_fields,
  source,                    -- 'ui' | 'job' | 'import' | 'system'
  device_info,
  prev_hash, row_hash,       -- tamper-evident chain (§15.2)
  ...
)
```

- **Never updated, never deleted** by application code. Retention/archival is an explicit
  administrative operation that itself writes an audit record.
- `before_json` / `after_json` store diffs, not full entities, for anything large.
- **Snapshots of labels** (`actor_name_snapshot`) so a five-year-old audit entry remains
  readable after the user is renamed or deactivated.
- `correlation_id` ties every record produced by one user action together — essential when
  posting an invoice cascades into stock movements and journal entries.
- Sensitive values (password hashes, tokens) are redacted at write time by a field policy.

### 15.2 Tamper evidence — deferred to a future enterprise edition

**DECIDED (§33.8): hash chaining is excluded from v1.**

One structural detail is required now to keep that door genuinely open. The `prev_hash` and
`row_hash` columns are **created in migration 0001 as nullable, and left NULL in v1.**

The reason is not convenience — it is that a hash chain **cannot be meaningfully backfilled.**
Computing hashes retroactively over rows that were never protected proves nothing about the
period before protection existed; an attacker who edited a row could simply recompute the
chain. A chain has value only from its activation point forward.

So the design is: columns reserved now, and when the enterprise edition enables chaining it
writes a **genesis record** marking the activation instant, chains forward from there, and the
verifier reports the protected range explicitly. Historical rows remain honestly unverified
rather than falsely certified. Cost today: two nullable columns.

### 15.3 What is audited

All writes to financial and master data, all authentication events, all permission and role
changes, all settings changes, all exchange-rate applications, all backups/restores, all
data imports/exports, and all report exports containing cost or profit data.

---

## 16. Settings system

### 16.1 Layered resolution

```
Code defaults  ◄─  Business profile  ◄─  Company  ◄─  Branch  ◄─  User  ◄─  Session
   (lowest)                                                              (highest)
```

Resolution walks from the most specific scope outward to the first defined value. A branch
can override VAT-inclusive pricing; a user can override language and theme; neither can
override anything not declared overridable at that scope.

### 16.2 Storage & typing

```sql
settings (
  id, scope,            -- 'system'|'company'|'branch'|'user'
  scope_id,             -- NULL for system
  setting_key,          -- 'sales.default_tax_group'
  setting_value,        -- TEXT (JSON-encoded)
  value_type,           -- 'string'|'int'|'bool'|'money'|'json'|'enum'
  UNIQUE (scope, scope_id, setting_key)
)
```

But application code never touches this table directly. Every setting is **declared in code**
with its key, type, default, allowed values, scope levels, and whether changing it requires
special permission. Access is through typed accessors:

```go
cfg.Sales.DefaultTaxGroup(ctx)   // returns id.ID, never a string lookup
cfg.Tax.Enabled(ctx)             // returns bool
```

Consequences: no typos in setting keys, no untyped parsing at call sites, a settings UI that
can be **generated** from the registry rather than hand-built, and a startup validation pass
that rejects unknown or malformed settings.

### 16.3 Caching and reactivity

Settings are cached in memory with a version counter; a write invalidates and emits a
`SettingChanged` event. Components that must react (locale, currency display, feature flags)
subscribe. This is the mechanism behind "changing language does not require a restart" (§22).

### 16.4 Business profiles

A business profile is a named bundle of settings + feature flags + seed data
(`seeds/business_profiles/pharmacy.json`): which modules are visible, default tax behaviour,
whether lot/expiry tracking is on, default units, receipt layout, terminology overrides.

Selecting "Pharmacy" at setup applies the bundle — and every value remains individually
editable afterwards. **This is the concrete mechanism that satisfies "generic ERP through
configuration, not code."**

---

## 17. Feature flags

- Declared in code by each module (`FlagDef`: key, description, default, stability, whether
  user-toggleable), stored in `feature_flags`, overridable per company and per branch.
- Three uses: **progressive delivery** (accounting UI revealed over releases — exactly your
  §2 requirement), **business-type tailoring** (restaurant tables off for a hardware store),
  and **future licensing/editions** (Basic / Professional / Enterprise) without a separate
  build.
- Enforced in **three places**: navigation (hide the menu item), use case (reject the call —
  a hidden UI is not security), and migration/seed (don't seed data for disabled features).
- Flags have a declared lifecycle (`experimental` → `stable` → `deprecated` → removed) and a
  removal target, so the codebase doesn't accumulate permanent dead branches.

---

## 18. Currency architecture

### 18.1 The important correction **[RECOMMENDATION — needs your decision]**

Your spec says "store prices in USD; calculate local currency automatically." That is right
for *pricing*. It is **not** sufficient for *accounting*, and conflating them will force a
schema redesign the moment you have real books.

An ERP needs **three distinct currency roles**:

| Role | Meaning | Example |
|---|---|---|
| **Pricing currency** | What catalogue prices are authored in | USD (per your spec) |
| **Functional / ledger currency** | What the general ledger and financial statements are kept in — usually mandated by law | Local currency |
| **Transaction currency** | What a specific document was actually transacted in | Whatever the customer paid |

Every monetary row therefore stores **both** the transaction amount and the functional-currency
amount, plus the rate used:

```sql
amount_minor            BIGINT   NOT NULL,   -- transaction currency
currency_code           CHAR(3)  NOT NULL,
exchange_rate           BIGINT   NOT NULL,   -- ×10⁹, to functional currency
functional_amount_minor BIGINT   NOT NULL,   -- computed and STORED, never recomputed
```

Storing the functional amount rather than recomputing it is essential: a report run next
year must not silently change because a rate was later corrected. It also makes FX gain/loss
computable, which is a genuine accounting requirement when you invoice in USD and collect in
local currency at a different rate.

This costs three columns and gives you a legally usable ledger.

**DECIDED (§33.1):** the functional currency is **configurable per company**, selected in the
setup wizard, and the general ledger is kept in it. USD is demoted to two optional roles: a
**pricing reference** (catalogue authoring, per your original spec) and an **exchange-rate
pivot**. Neither role is hardcoded — a company operating entirely in one currency simply sets
pricing currency = functional currency, and every conversion path collapses to identity with
no special-case code.

See the addendum §G.1 for the related recommendation on **rate types** (official vs market
rates), which matters directly for the Syria default market.

### 18.2 Schema

```
currencies            code(PK, CHAR(3)), name, symbol, decimal_places,
                      rounding_mode, symbol_position, is_active
exchange_rates        id, from_currency, to_currency, rate(BIGINT ×10⁹),
                      valid_from(date), valid_to(date), source, provider_id,
                      batch_id, created_by
exchange_rate_batches id, status(draft|previewed|applied|rejected), fetched_at,
                      applied_at, applied_by, note      -- §18.4 preview
rate_provider_config  id, provider_key, is_enabled, priority, schedule_cron,
                      credentials_ref, last_success_at, last_error
```

Rates are **temporal and never updated in place** — a correction is a new row. This gives you
the required rate history for free and makes historical documents reproducible.

### 18.3 Provider abstraction

```go
type RateProvider interface {
    Key() string
    Fetch(ctx context.Context, base Currency, targets []Currency) ([]Rate, error)
}
```

Providers are registered with a priority chain and per-provider failure handling. Offline is
the normal case, not an error: when no provider succeeds, the last known rate is used and the
UI shows its age. A **manual** provider is always present and always wins when used.

### 18.4 The preview workflow

Your requirement for "preview before applying" is modelled as a staged batch, which is the
right shape for it:

1. Fetch → create `exchange_rate_batch` in `draft`.
2. Compute the impact: for each affected product, old local price → new local price, delta %,
   and a summary of how many products cross a psychologically significant threshold.
3. User reviews, can exclude individual items, then **applies** or **rejects** the batch.
4. Applying writes the rates and an audit record; a batch can be **rolled back** as a unit.

### 18.5 Fixed-price products

A product may set `price_mode = 'fixed_local'`, pinning its local price regardless of rate
movement. Rate application skips these, and the preview reports them separately so nobody is
surprised by an unchanged price.

### 18.6 Rounding

Per-currency rounding rules (nearest 0.05, nearest 50, nearest 1000 — common in
high-inflation markets), applied at the configured stage (line, subtotal, or grand total),
with the rounding difference posted to a dedicated GL account so the books still balance.

---

## 19. Tax architecture

### 19.1 Principles

Fully configuration-driven, per your §3. **No tax rate, threshold, or rule is ever written in
Go code.** The code contains a resolution algorithm; the rates and rules are data.

### 19.2 Schema

```
tax_jurisdictions   id, country_code, region_code, name, is_active
taxes               id, code, name, jurisdiction_id, tax_type(vat|sales|excise|withholding),
                    calculation(percentage|fixed_per_unit|fixed_per_document),
                    is_compound, is_recoverable,
                    payable_account_id, receivable_account_id   -- links to GL §20
tax_versions        id, tax_id, rate(BIGINT ×10⁶), effective_from(date),
                    effective_to(date)          -- rates are versioned, never edited
tax_groups          id, code, name, is_price_inclusive, is_default
tax_group_items     id, tax_group_id, tax_id, sequence, is_compound_on_previous
tax_exemptions      id, partner_id, tax_id NULL, reason_code, certificate_number,
                    valid_from, valid_to, evidence_ref
```

- **`tax_versions` is the critical piece.** When VAT changes from 15% to 16%, you add a
  version with an effective date. Historical invoices continue to resolve the old rate, and
  the change is auditable. Editing a rate in place would silently falsify past documents.
- **`tax_groups`** handle multiple simultaneous taxes (VAT + municipal tax), including
  **compound** taxes (a tax computed on a base that already includes another tax), ordered by
  `sequence`.
- **Inclusive vs exclusive pricing** is a property of the group. Both are needed: retail
  markets usually quote tax-inclusive; B2B quotes exclusive.

### 19.3 Resolution algorithm

A single, testable function resolves the applicable tax group by priority:

```
1. Explicit override on the document line          (highest)
2. Explicit override on the document header
3. Partner exemption                                → zero-rated, reason recorded
4. Partner tax group (e.g. export customer)
5. Product tax group
6. Product category tax group
7. Branch default tax group
8. Company default tax group                        (lowest)
9. If tax feature disabled → no tax                 (§19.5)
```

The resolved group, each individual tax, its rate, its base, and the reason for the
resolution are all **stored on the document** at posting time (`sales_document_taxes`), so a
tax authority can be shown exactly why a given amount was charged years later.

### 19.4 Calculation

- Computed by a domain service, per line, with the rounding stage configurable
  (line-level vs document-level rounding produce legally different totals in different
  jurisdictions).
- Tax-inclusive prices are decomposed with exact integer arithmetic and largest-remainder
  allocation so line taxes sum exactly to the document tax.
- Every tax computation produces a GL posting via §20's mapping — tax payable is a liability,
  recoverable input tax is an asset.

### 19.5 Disabling tax completely

`tax.enabled = false` (feature flag + setting) removes tax from the UI, resolves every
document to zero tax, and skips tax GL postings. The tables and code paths remain — turning
it back on requires no migration. Businesses in tax-free jurisdictions get a genuinely
simpler UI, which is the point.

---

## 20. Accounting architecture

Built completely from day one, exposed progressively in the UI — exactly as you specified.

### 20.1 Chart of Accounts

```sql
accounts (
  id, code, name, parent_id,
  account_type,        -- asset|liability|equity|revenue|expense
  account_subtype,     -- current_asset|fixed_asset|cogs|operating_expense|…
  normal_balance,      -- 'debit' | 'credit'  (derived from type, stored for clarity)
  currency_code,       -- NULL = functional currency; set for FX-denominated accounts
  is_postable,         -- only leaf accounts accept postings
  is_system,           -- system accounts cannot be deleted
  is_active, branch_id NULL,   -- NULL = shared across branches
  ...
)
```

- Hierarchical, unlimited depth, with materialized `path` for fast subtree aggregation.
- **Only leaf accounts are postable.** Parent accounts are roll-up only.
- Country-specific CoA templates seeded from `seeds/chart_of_accounts/` — another
  configuration-not-code decision.
- **System accounts** (AR, AP, inventory, COGS, tax payable, FX gain/loss, rounding
  difference, retained earnings, opening balance) are seeded, protected, and referenced by
  the mapping layer.

### 20.2 Journal entries — the core invariant

```sql
journal_entries (
  id, entry_number, entry_date(date), fiscal_period_id,
  status,              -- draft|posted|reversed
  source_module,       -- 'sales' | 'purchasing' | 'manual' | …
  source_document_type, source_document_id,     -- traceability back to the invoice
  reversal_of_entry_id, memo, branch_id,
  posted_at, posted_by, ...
)

journal_lines (
  id, journal_entry_id, line_number, account_id,
  debit_minor  BIGINT NOT NULL DEFAULT 0,       -- functional currency
  credit_minor BIGINT NOT NULL DEFAULT 0,       -- functional currency
  currency_code, original_amount_minor, exchange_rate,   -- transaction currency §18.1
  -- analytical dimensions:
  branch_id, warehouse_id, partner_id, product_id, cost_center_id, project_id,
  memo, ...
  CHECK (debit_minor >= 0 AND credit_minor >= 0),
  CHECK (NOT (debit_minor > 0 AND credit_minor > 0))     -- a line is debit XOR credit
)
```

**The one inviolable rule:** for every entry, `SUM(debit_minor) = SUM(credit_minor)` in the
functional currency. Enforced in the domain aggregate before persistence, re-verified by a
posting service, and checked by a periodic integrity job. An unbalanced entry cannot be saved.

**Immutability:** a posted entry is never modified or deleted. Corrections create a
**reversing entry** linked via `reversal_of_entry_id`. This is what makes the general ledger
legally defensible.

### 20.3 Posting rules — the key design decision **[RECOMMENDATION]**

The naive approach hardcodes "when an invoice is posted, debit AR, credit revenue, credit
tax." That makes the accounting untestable, un-configurable, and country-specific — and it
would violate your configuration-over-code principle at the most important layer.

Instead, **posting is table-driven**:

```sql
posting_rules (
  id, event_type,        -- 'sales.invoice.posted'
  business_profile_id,   -- NULL = applies to all
  sequence, description, is_active
)

posting_rule_lines (
  id, posting_rule_id, line_number,
  side,                  -- 'debit' | 'credit'
  account_selector,      -- 'mapping:AR' | 'account:4100' | 'partner:receivable_account'
  amount_selector,       -- 'document.total' | 'line.net' | 'line.tax' | 'line.cost'
  condition_expr,        -- optional guard, e.g. 'document.has_tax'
  dimension_map          -- how to populate branch/warehouse/partner/product
)

account_mappings (
  id, mapping_key,       -- 'AR' | 'AP' | 'INVENTORY' | 'COGS' | 'TAX_PAYABLE' | …
  account_id, branch_id NULL, business_profile_id NULL
)
```

The accounting module subscribes to domain events from sales/purchasing/inventory/payments,
looks up the applicable rule, evaluates it, and produces a balanced journal entry.

The payoff is large:
- Sales code contains **zero** accounting logic — it just emits `InvoicePosted`.
- Different countries and business types get different posting behaviour through seed data.
- Accounting can be completely disabled in v1's UI while still recording perfect books.
- Posting rules are unit-testable in isolation, and a mis-mapped account is a data fix rather
  than a patch release.

### 20.4 Fiscal periods

`fiscal_years` and `fiscal_periods` with `open | closed | locked` status. Posting into a
closed period is rejected. Year-end closing generates the closing entries (revenue and
expense accounts → retained earnings) as a normal, reversible journal entry.

### 20.5 Balances and performance

`account_balances` (account, fiscal_period, branch, opening/debit/credit/closing) maintained
incrementally on posting. Trial balance and financial statements read this table, not the
full ledger. A rebuild job recomputes from `journal_lines` and asserts equality — a cheap,
strong correctness check to run nightly and after restore.

### 20.6 Progressive UI exposure

| Release | Exposed in UI | Recorded internally |
|---|---|---|
| v1.0 | Nothing — books keep themselves | Full double-entry for every transaction |
| v1.1 | Chart of Accounts (read-only), Trial Balance | " |
| v1.2 | General Ledger, account statements | " |
| v1.3 | Manual journal entries, period close | " |
| v1.4 | P&L, Balance Sheet, Cash Flow | " |

No schema change at any step. This is precisely the outcome your §2 asked for.

---

## 21. Inventory & costing

**DECIDED (§33.3): perpetual inventory with Weighted Average Cost (WAC) as the default,
FIFO-ready schema.** The costing abstraction that keeps FIFO a configuration change rather
than a redesign is specified in addendum §D.

### 21.1 Perpetual inventory, moving weighted average cost

- **Perpetual**, not periodic: COGS is recorded at the moment of sale, so profit is correct
  in real time — which your dashboard and profit-analysis requirements imply.
- **Moving weighted average** as the v1 default: simple to explain to shop owners, cheap to
  compute, immune to the layer-tracking complexity of FIFO, and sufficient for most retail
  and wholesale.
- **Schema is FIFO-ready:** an `inventory_layers` table (receipt date, qty remaining, unit
  cost) is created from migration 0001 and populated even in average-cost mode. Switching a
  company to FIFO later is then a configuration change plus a recompute job, not a migration.
- `costing_method` is a per-company setting with values `average` (v1) and `fifo`, `standard`
  (later).

### 21.2 Stock model

```
stock_levels     product_id, variant_id, warehouse_id → qty_on_hand, qty_reserved,
                 qty_available (derived), avg_cost_minor
stock_movements  APPEND-ONLY ledger: every in/out/adjustment/transfer with
                 qty, unit_cost, resulting balance, source document, reason
```

`stock_movements` is the source of truth; `stock_levels` is a maintained projection that can
always be rebuilt and verified against it. Negative stock is permitted or blocked by
configuration (some businesses genuinely need it; most should not).

Lot/batch and serial tracking tables exist from day one, gated by feature flags — pharmacy
and electronics need them, a furniture store does not.

---

## 22. Internationalization

### 22.1 Single source of truth

`locales/{en,ar}/*.json` is consumed by **both** Go (via `go:embed`) and React (imported at
build time). One set of files, one key namespace, no drift between backend error messages and
frontend labels.

### 22.2 Backend rule

**The backend never produces user-facing prose.** It returns error codes and parameters
(§5.4). This is what makes instant language switching complete rather than partial — with
translated strings coming from the server, a language change would leave stale English text
in every already-rendered error.

### 22.3 Frontend

- React context + a `useTranslation` hook; the locale lives in a store.
- Changing locale updates `<html lang>` and `<html dir>` and re-renders. **No reload, no
  restart** — this falls out of the architecture rather than being a special feature.
- **Tailwind logical properties everywhere**: `ps-4`/`pe-4`, `ms-2`/`me-2`, `text-start`,
  `border-s` — never `pl-4`, `ml-2`, `text-left`. This is a lint-enforced rule; it is the
  single biggest determinant of whether RTL support actually works, and retrofitting it
  across a finished UI is weeks of tedious work.
- Icons and directional affordances (chevrons, progress, charts) mirror via a `dir`-aware
  utility. Numbers, dates, and currency format through `Intl` with the active locale.
- Fonts: a properly hinted Arabic face (e.g. IBM Plex Sans Arabic / Noto Naskh) bundled
  offline, with per-locale line-height and letter-spacing adjustments — Arabic needs more
  vertical breathing room than Latin at the same size.

### 22.4 Data vs UI translation

UI strings → JSON catalogs. User-entered content (product names, category names, account
names) → the `translations` table (§9.5). These are different problems and must not share a
mechanism.

### 22.5 Printing

Document templates are locale-aware, including RTL layout, Arabic-Indic vs Western digit
selection (configurable — this varies by country and by customer preference), and per-locale
date and calendar formatting (Gregorian/Hijri display).

---

## 23. Event bus

### 23.1 Two kinds of events, deliberately separated

| | **Domain events** | **Integration events** |
|---|---|---|
| Scope | Within a module | Across modules |
| Delivery | Synchronous, in the same transaction | Asynchronous, after commit, via outbox |
| Failure | Rolls back the whole operation | Retried independently |
| Example | Invoice recalculates its totals | Accounting posts the journal entry |

Conflating these is a classic source of either lost side effects or transactions that roll
back because an unrelated subscriber failed.

### 23.2 Bus

In-process, typed, with explicit subscriber registration at bootstrap (no reflection-based
discovery — subscriptions must be greppable). Handlers declare idempotency; the dispatcher
enforces ordering per aggregate and isolates panics.

### 23.3 Transactional outbox **[RECOMMENDATION]**

Cross-module events are **not** published directly to the bus. They are written to an
`outbox_events` table **inside the same transaction** as the business change, then dispatched
by a background worker after commit.

```sql
outbox_events (
  id, occurred_at, event_type, aggregate_type, aggregate_id,
  payload_json, correlation_id, causation_id, branch_id,
  status,            -- pending | processing | done | failed
  attempts, next_attempt_at, last_error, processed_at
)
```

Why this matters enough to build now:

- **Atomicity.** Today, an app crash between "invoice saved" and "journal entry created"
  would leave the books wrong. With an outbox this is impossible: either both happen or
  neither does.
- **Retries and poison-message handling** come for free.
- **It is the foundation of the cloud sync in your roadmap.** The outbox is already an ordered,
  durable, causally-linked change log — which is exactly what a sync protocol needs. Building
  sync without it later would mean inventing change tracking from scratch, or resorting to
  triggers (which the portability contract forbids).
- Cost: one table and roughly 150 lines of dispatcher.

---

## 24. Background workers & scheduling

### 24.1 Durable jobs

A desktop app is closed every evening, so in-memory scheduling loses work. Jobs are persisted:

```sql
jobs      (id, job_key, schedule_cron, is_enabled, last_run_at, next_run_at,
           timeout_seconds, max_attempts, is_singleton, ...)
job_runs  (id, job_id, started_at, finished_at, status, attempt,
           error, output_json, triggered_by)
```

On startup the scheduler reconciles: overdue jobs run according to a per-job **catch-up
policy** (`run_once` / `skip` / `run_all`) rather than a blanket rule — a missed backup should
run; three missed rate fetches should collapse to one.

### 24.2 Execution

Bounded worker pool, per-job concurrency limits, singleton enforcement (no two backups at
once), timeouts, cancellation via context, exponential backoff with jitter, and graceful
drain on shutdown. Every run is recorded and surfaced in a UI panel — invisible background
failures are how customers lose backups without knowing.

### 24.3 Initial jobs

Outbox dispatch, exchange-rate fetch, scheduled backup, account-balance rebuild/verify,
stock-level reconciliation, audit-chain verification, report cache warming, notification
generation (low stock, overdue debts, expiring lots), old-log cleanup.

### 24.4 UI responsiveness

No long operation blocks the UI thread. Long jobs report progress through Wails events, are
cancellable, and the app remains usable — a POS that freezes during a report is not
acceptable.

---

## 25. Plugin architecture **[RECOMMENDATION — significant deviation]**

### 25.1 The honest constraint

Go's built-in `plugin` package is **not viable** for this product: it does not work on
Windows at all, requires exact toolchain and dependency version matching between host and
plugin, and cannot be unloaded. Any design that assumes it will fail on your primary
platform. I want to state this plainly rather than design around a mechanism that won't work.

### 25.2 Recommended three-stage path

**Stage 1 — v1: internal module registry + extension points.** This is the real foundation
and delivers most of the value.

- Every module is already a self-contained unit with a registration interface (§4.1).
- Define **named extension points** where behaviour can be contributed:
  payment methods, tax calculators, exchange-rate providers, print templates, report
  definitions, export formats, barcode symbologies, dashboard widgets, document validators,
  posting rules.
- Each is a registry mapping a key to an implementation. Adding a payment method is
  registering one struct — no changes to sales code.
- **This is what makes the system genuinely extensible.** Dynamic loading is a distribution
  mechanism, not an architecture.

**Stage 2 — declarative/data plugins (no code).** A large fraction of real customization is
not code at all: custom fields on entities, custom document templates, custom report
definitions, custom posting rules, custom validation rules, workflow/status configuration.
Delivered as signed JSON packages, these cover most integrator needs with zero security or
compatibility risk. Schema support: a `custom_field_definitions` + `custom_field_values`
pair, designed now.

**Stage 3 — out-of-process plugins (post-v1, only if commercially needed).** If third parties
must ship real code, use `hashicorp/go-plugin` (gRPC over a subprocess): works on Windows,
crash-isolated, independently versioned, language-agnostic, and sandboxable. Slower per call,
which is fine for the integration-shaped work plugins actually do.

Embedded scripting (goja/JavaScript or Lua) is a viable Stage-2.5 for customer-written hooks
if you want it; it is safer than native plugins and easier than gRPC. I'd hold it until a
concrete need appears.

### 25.3 Designing for it now, at near-zero cost

- Keep the module registration interface stable and documented.
- Route every extension point through a registry rather than a `switch` statement.
- Give every plugin-facing type a stable versioned contract.
- Namespace all keys (`plugin_key.entity`) to prevent collisions.
- Reserve schema space: `custom_field_*` tables and a `plugin_registry` table from 0001.

---

## 26. Multi-branch & multi-warehouse

Both are v2 features that **must be in the v1 schema**, because retrofitting a dimension onto
every transactional table and every report is effectively a rewrite.

### 26.1 What v1 does

- `companies`, `branches`, `warehouses` tables exist and are seeded with one default row each.
- **Every** transactional table has `branch_id NOT NULL`; every stock-bearing table has
  `warehouse_id NOT NULL`. Journal lines carry both as dimensions.
- Every query, index, and report is written branch-aware from the start — even though there
  is only one branch. An index that starts `(branch_id, …)` costs nothing today and is
  correct tomorrow.
- The `AppContext` always carries a current branch and warehouse.
- Number series (§9.4) are already per-branch.
- RBAC scope (§14.2) already accepts branch scope.

### 26.2 What v2 adds

Branch switching UI, inter-branch stock transfers (as a two-sided document with in-transit
stock), per-branch pricing and tax defaults, consolidated vs per-branch reporting, and
branch-scoped permissions — all additive, no migration of existing rows.

### 26.3 Warehouse detail

Stock is always tracked per warehouse. A warehouse belongs to a branch. Transfers move stock
through an in-transit state so quantities are never in two places or nowhere. Locations
(bin/shelf) within a warehouse are deferred, but `location_id` is reserved on movements.

---

## 27. Reporting & read models **[RECOMMENDATION]**

**Separate the read path from the write path (CQRS-lite, without event sourcing).**

- **Writes** go through aggregates and repositories, with full invariant enforcement.
- **Reads** for lists, dashboards, and reports go through purpose-built SQL returning flat
  DTOs — never by loading aggregates.

This is not architectural fashion; it is the difference between a POS list screen that
returns in 20 ms and one that loads 500 aggregates with their lines. It also lets report
queries be optimized and indexed independently of the domain model.

- Report definitions are **data** (`report_definitions`) where possible: columns, filters,
  grouping, permissions — so new reports and customer-specific variants don't require code.
- Heavy aggregates (daily sales, stock valuation, profit by product) are **materialized
  snapshot tables** refreshed by background jobs, with a "data as of" indicator in the UI.
- All exports (Excel/CSV/PDF) run as background jobs with progress, and exports containing
  cost or profit data are permission-gated and audited.

---

## 28. Search

Behind a port from day one:

```go
type SearchIndex interface {
    Index(ctx, doc Document) error
    Delete(ctx, docType, id string) error
    Query(ctx, q Query) (Results, error)
}
```

- v1 implementation: SQLite FTS5 in a **separate index table**, never mixed into the core
  schema (per §8.2), kept current by outbox subscribers.
- Because it is a port, moving to PostgreSQL full-text or Bleve later is an infrastructure
  swap with no domain impact.
- **Arabic search needs explicit handling** and it should be designed now, not discovered
  later: diacritic (tashkeel) stripping, alef/hamza normalization (أ إ آ → ا), teh marbuta
  ↔ heh, and Arabic-Indic ↔ Western digit folding. Without normalization, users cannot find
  their own products. A `search_normalized` column stores the folded form.
- Barcode scanning is a distinct, exact-match fast path — not a full-text query.

---

## 29. Backup & restore

- **Portable format, not a raw file copy.** A `.mizanbak` archive = compressed logical export
  (schema version, table data, checksums, manifest) + optional attachments. Rationale: a raw
  SQLite file copy is not restorable onto PostgreSQL later, and would make your migration
  path depend on the backup format. It is also unsafe to copy while WAL is active.
- Backups are **verified after creation** (restore into a temporary database, run integrity
  checks and an accounting balance assertion).
- Scheduled, retained by policy (N daily / M weekly / K monthly), with configurable
  destination and a visible "last successful backup" indicator in the UI.
- **Restore requires elevated permission, is fully audited, always backs up the current
  database first**, and refuses backups from a newer schema version.
- An encrypted-backup option (passphrase, portable) for off-site copies.

---

## 30. Testing strategy

| Level | Scope | Speed | Where |
|---|---|---|---|
| **Unit** | Domain entities, value objects, calculators (money, tax, costing) — no I/O | ms | Alongside code |
| **Integration** | Use case + real SQLite in a temp file, per-test fresh migration | ~100 ms | `test/integration` |
| **Contract** | Every repository implementation against its interface's shared test suite | ~s | Per module |
| **Property-based** | Money allocation, tax decomposition, journal balance | ~s | Domain |
| **Golden-file** | Print templates, exports, report output | ms | Per module |
| **E2E** | Critical flows through Wails bindings | ~min | `test/e2e` |

Non-negotiable invariant tests, run in CI and as a runtime integrity job:

1. Every posted journal entry balances to zero.
2. `stock_levels` equals the sum of `stock_movements` for every product/warehouse.
3. `account_balances` equals the recomputation from `journal_lines`.
4. Document totals equal the sum of their lines plus taxes minus discounts.
5. Partner balances equal the sum of their open documents minus settlements.
6. The audit hash chain is unbroken.

**A shared test-suite per repository interface** is what protects the portability promise:
when PostgreSQL support arrives, the new repositories must pass the identical suite.

---

## 31. Frontend architecture (summary)

Detailed UI/UX design is a separate document; this fixes the structural decisions.

- **Mirrors the backend modules** (`frontend/src/modules/sales/...`) so a feature is one
  folder on each side.
- **Layers per module:** `api/` (generated binding wrappers), `hooks/` (data access +
  caching), `components/`, `pages/`, `types/`.
- **Server state via TanStack Query** over Wails bindings — caching, invalidation, optimistic
  updates, and background refetch are solved problems. **Client state via Zustand** for
  session, locale, branch, and UI preferences. No Redux; the boilerplate is not earned here.
- **A real design system first**, not ad-hoc Tailwind: design tokens (spacing, color, radius,
  typography, elevation), then primitives (Button, Input, Select, Table, Dialog, Sheet,
  Toast, EmptyState), then composites. Radix primitives for accessibility. Building the token
  layer before screens is what produces the Linear/Notion consistency you asked for.
- **Every screen has designed empty, loading, error, and permission-denied states.** These are
  where "professional" is actually decided.
- **Keyboard-first POS**: the entire sale flow completable without a mouse, with a documented
  and configurable shortcut map.
- **Virtualized lists** for large tables; route-level code splitting.
- **RTL is verified by automated snapshot tests in both directions**, not by manual checking.

---

## 32. Proposed build order

Each phase follows your process: design doc → architecture review → schema review → your
approval → implementation → review → refactor.

| Phase | Contents | Why here |
|---|---|---|
| **0. Foundation** | Repo scaffolding, kernel (money/id/time/errors), platform (db, migrate, eventbus, outbox, jobs, i18n), bootstrap, design system tokens | Everything else depends on it |
| **1. Core data** | org (company/branch/warehouse), identity + RBAC, settings, feature flags, audit | Nothing can be built safely without auth + audit |
| **2. Financial spine** | currency, tax, accounting (CoA, journals, posting rules, periods) | Must exist before any transaction is recorded |
| **3. Master data** | catalog (products, categories, UoM, barcodes), partners, price lists | Prerequisite for transactions |
| **4. Inventory** | stock levels, movements, valuation, adjustments, counts | Sales depends on stock |
| **5. Sales & POS** | POS screen, invoices, returns, payments, printing | The product's core value |
| **6. Purchasing** | POs, bills, supplier returns, landed cost | Completes the stock cycle |
| **7. Money out** | expenses, debts, settlements | |
| **8. Insight** | dashboard, reports, profit analysis, search | Needs real data to be meaningful |
| **9. Operations** | backup/restore, import/export, notifications | |
| **10. Polish** | performance, accessibility, RTL audit, installers, docs | |

Phases 0–2 are the ones worth being slow and careful about. Everything after them is
comparatively mechanical if those are right.

---

## 33. Resolved foundational decisions

All nine open items are decided. This section is the authoritative record; the addendum
carries the detailed designs that follow from them.

| # | Decision | Outcome | Detail |
|---|---|---|---|
| 1 | **Functional currency** | Configurable per company, selected in setup. Ledger kept in it. USD is an optional pricing reference and rate pivot only — never hardcoded. | §18.1, Addendum §G.1 |
| 2 | **Target country** | **No country hardcoded.** Selected in the setup wizard from country profiles. Taxes, currency, number/date formats, calendar and accounting defaults are all seed data. Syria (`sy`) is the development seed example. | Addendum §C |
| 3 | **Costing method** | Weighted Average Cost, perpetual. FIFO addable later via a costing-strategy port and layers populated from day one — no redesign. | §21, Addendum §D |
| 4 | **Document numbering** | Unique sequential, **not** gapless, in v1. `is_gapless` is a per-series flag for jurisdictions that later require it. Numbers allocated at posting, transactionally. | §9.4 |
| 5 | **Fractional quantities** | Supported from day one: kg, litre, metre, fabric, timber, cable. Full unit-of-measure system with per-unit fractional permission and rounding precision. | Addendum §B |
| 6 | **Product variants** | Supported in v1 via a generic attribute system (colour, size, material, capacity, model). No product-type-specific columns. | Addendum §A |
| 7 | **Plugin strategy** | Staged as proposed: internal module registry + extension points in v1; JSON declarative extensions next; out-of-process gRPC only on real commercial demand. | §25 |
| 8 | **Audit hash chaining** | Excluded from v1. Columns reserved nullable; chain activates with a genesis record in a future enterprise edition — never backfilled. | §15.2 |
| 9 | **Generalized data model** | Confirmed: single `partners` table, single `sales_documents` table discriminated by `doc_type`. Generalized ERP model preferred throughout. | §9.2 |

### 33.1 Remaining open questions (non-blocking)

None of these block Phase 0. They are needed by the phase noted.

| # | Question | Needed by |
|---|---|---|
| 1 | Is **lot / batch / expiry tracking** active in v1 (pharmacy, food, cosmetics), or built-but-flag-off? | Phase 4 (Inventory) |
| 2 | Does v1 need **multiple price lists** (wholesale/retail/customer-specific), or one price with per-line discounts? | Phase 3 (Catalog) |
| 3 | **Receipt printing targets**: thermal ESC/POS (58/80mm), A4/A5 documents, or both? Drives the print pipeline. | Phase 5 (Sales) |
| 4 | Does the setup wizard need **data import** from an existing system in v1 (Excel/CSV of products, customers, opening balances)? | Phase 9 |
| 5 | **Dual calendar display** (Gregorian + Hijri) — required for the Syrian market, or Gregorian only? | Phase 1 (Settings) |

---

## 34. Summary of recommendations that differ from or extend your brief

| # | Recommendation | Cost of not doing it |
|---|---|---|
| 1 | Modular monolith (module-first), not layer-first folders | Coupling grows silently; plugins become impossible |
| 2 | Separate functional currency from pricing currency | Ledger is not legally usable; FX gain/loss impossible; schema redesign |
| 3 | UUIDv7 keys, not auto-increment | Multi-branch sync breaks; retrofit touches every FK |
| 4 | Transactional outbox for cross-module events | Crash between save and posting corrupts books; no foundation for cloud sync |
| 5 | Table-driven posting rules, not hardcoded entries | Accounting untestable, country-specific, un-configurable |
| 6 | Versioned tax rates (`tax_versions`) | Editing a rate silently falsifies historical invoices |
| 7 | Scope + field-level permissions + approval limits in RBAC from day one | Retrofitting scope into every check later |
| 8 | Translations side table, not per-language columns | Every new language is a migration on every table |
| 9 | Single `partners` and single `sales_documents` tables | Duplicated data, painful merges, doubled reporting surface |
| 10 | Perpetual inventory + moving average, FIFO-ready schema | Profit figures wrong or unfixable without migration |
| 11 | Staged plugin strategy; no Go `plugin` package | A dynamic-plugin design that cannot ship on Windows |
| 12 | CQRS-lite read models for lists and reports | Slow screens that cannot be fixed without rework |
| 13 | Logical backup format, not raw file copy | Backups unusable after any database migration |
| 14 | No ORM; explicit SQL (+ `sqlc`) | Unpredictable queries, hidden N+1, broken portability |
| 15 | Manual DI, no container | Runtime startup panics on customer machines |
| 16 | Audit hash chain | No defence against direct database tampering |
| 17 | Tailwind logical properties enforced by lint | RTL retrofit costs weeks and is never fully correct |
| 18 | Arabic search normalization designed in | Users cannot find their own Arabic-named products |
| 19 | Backup + auto-restore around every migration | A failed migration destroys a customer's business data |
| 20 | Keep an HTTP-shaped seam at the binding layer | Cloud sync / mobile companion requires reworking the boundary |

---

*End of v1 architecture. Awaiting your review, the answers in §33, and approval to proceed to
Phase 0 detailed design.*
