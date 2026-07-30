# Step 0.5 — Configuration & Metadata Registries (Design + implementation record)

> Status: **APPROVED and IMPLEMENTED.** Design approved 2026-07-30 — all eight decisions
> (D1–D8) accepted as recommended. Implementation record at **§11**.
> (Permanent Development Protocol, Steps 2–6.)
> Scope: `internal/kernel/id`, `internal/platform/config` (settings + feature flags),
> `internal/platform/strategy`, `internal/platform/metadata`.
> **Out of scope:** business settings/flags (they arrive with their modules), the settings
> UI (Phase 1+), RBAC enforcement (Phase 1), the event bus itself (0.6), business metadata
> tables such as rate types (0.9).

This step builds the machinery behind the project's overriding mandate — *generic across
business types and countries via configuration, not code*. PHASE_0_FOUNDATION §CFG states the
mandate plainly: it "only holds up if there is a disciplined mechanism for it. Ad-hoc config
scattered across modules decays into exactly the hardcoding it was meant to avoid."

Everything else in Phase 0 has been infrastructure that a competent team would build for any
Go desktop app. **This step is the one that makes Mizan the product it claims to be**, so the
design is dominated by keeping the three mechanisms genuinely separate and keeping call sites
incapable of drifting back into strings and `switch` statements.

---

## 1. ANALYSIS

### 1.1 The three mechanisms, and why the separation is load-bearing

PHASE_0_FOUNDATION §CFG.1 defines three deliberately distinct things. Collapsing any two is
the failure mode this step exists to prevent:

| Mechanism | What it is | Who changes it | Stored in |
|---|---|---|---|
| **Settings** | Typed key/value knobs resolved by scope | Users, at runtime | `settings` |
| **Metadata** | Rows defining *kinds of things* | Admins, at runtime; seeded initially | dedicated tables |
| **Strategies** | Named code behaviours selected by data | Developers (code) + data (selection) | in-code registry |

The governing rule, quoted from §CFG.1:

> **Data selects behaviour; code provides behaviour.**

A tax *rate* is data; the tax *resolution algorithm* is code. A payment *method* is data; the
payment *provider integration* is a registered strategy. This is what allows a new country to
be supported by editing seed data while the code paths stay finite, testable, and safe.

### 1.2 What actually constrains this step

Unlike 0.4, the hard part here is not a failure mode — it is **dependency reality**. Four
things this design needs do not exist yet:

| Needed | Status | Consequence for this design |
|---|---|---|
| `kernel/id` (UUIDv7) | **Marker only** — `doc.go`, no code | `settings.id` is `CHAR(36)`; we cannot write a row without it. **Must be built here** (§2). |
| Event bus (`SettingChanged`, §16.3) | Step 0.6 | Settings must not import a bus that does not exist. Needs a port (§3.5). |
| `appctx` (current company/branch/user) | Step 0.10 / `internal/api` | `config` is *platform*; importing `api` would invert the layering. Needs a port (§3.3). |
| RBAC (`SettingDef.Permission`) | Phase 1 | Declare the field now, enforce through a port that defaults to permit (§3.7). |

Three ports, each with a trivial Phase-0 implementation, replaced later without touching call
sites. That is the whole architectural shape of this step.

### 1.3 Risks and how the design answers each

| Risk | Consequence if unhandled | Design answer |
|---|---|---|
| Settings accessed by loose string keys | Typos become silent wrong behaviour; a renamed key breaks at runtime, in production, on a customer's machine | **Typed handles** — declaration returns the accessor (§3.1) |
| A setting read as the wrong type | `Bool("sales.tax_rate")` compiles and returns nonsense | Handles are generic; the type is fixed at declaration (§3.1) |
| Two modules declare the same key | Last-one-wins, nondeterministic by init order | Duplicate detection at declaration, reported by startup validation (§3.2) |
| Config becomes a scripting language | The settings table grows into an unmaintainable pseudo-DSL | §CFG.5 boundary is written into the doc *and* the review checklist (§8) |
| `switch` on a business "kind" | The mandate quietly dies one switch at a time | Strategy registry + it is **a stated design defect** to switch (§5) |
| A removed setting's row remains after a downgrade | App refuses to start over a stale row | Unknown keys are reported, not fatal (§3.6) — **deviates from §CFG.2, D3** |
| Seeds re-applied on every boot | Duplicate metadata rows, or admin edits silently reverted | Idempotent, `code`-keyed, `is_system`-aware seeding (§6) |
| Cache races | Torn reads under concurrent settings writes | Single load-all snapshot behind `RWMutex` + version counter (§3.5) |

---

## 2. DESIGN — prerequisite: `kernel/id`

`settings` and `feature_flags` both have `id CHAR(36)` primary keys, so this step cannot write
a row without identifiers. `kernel/id` is listed as Phase-0 kernel work (§KRN) but was not part
of 0.2, which was numeric-only.

```go
// internal/kernel/id
type ID string                    // CHAR(36) canonical UUID text

func New() (ID, error)            // UUIDv7 — time-ordered
func Parse(s string) (ID, error)  // validates; rejects the nil UUID
func (i ID) IsZero() bool
```

- **UUIDv7** per ARCHITECTURE_v1 §7.4: time-ordered, so it is index-friendly on a B-tree
  primary key, while remaining collision-free across branches for future sync.
- `github.com/google/uuid v1.6.0` is **already in the module graph** (indirect, via Wails) and
  provides `NewV7`. This step promotes it to a direct dependency — **no new dependency is
  added**, and archlint's `kernel-purity` rule already allows `github.com/google/uuid`.
- Stored as text, not 16 bytes: the portable type contract (§8.1) fixes `CHAR(36)`, and a
  human-readable key in a support conversation is worth more than 20 bytes a row.
- `New()` returns an error rather than panicking (entropy failure is possible, and archlint
  forbids `panic` in production code).

---

## 3. DESIGN — the Setting Registry

### 3.1 Typed handles — a recommended improvement on §CFG.2

§CFG.2 sketches a string-keyed interface:

```go
cfg.Bool(ctx, "tax.enabled")     // illustrative shape from PHASE_0_FOUNDATION
```

**I recommend declaring a typed handle instead, and reading through it. D1.**

```go
// internal/platform/config — declared once, in the owning module's config.go
var TaxEnabled = config.DeclareBool(config.Def{
    Key:         "tax.enabled",
    Default:     false,
    Scopes:      []config.Scope{config.ScopeSystem, config.ScopeCompany},
    Description: "settings.tax.enabled",   // i18n key, never prose (§22.2)
})

// at the call site
if TaxEnabled.Get(ctx) { ... }
```

Why this is better than the illustrative shape, and worth deviating for:

- **The key cannot be mistyped at a call site**, because call sites contain no key. A typo
  becomes a compile error rather than a silently-wrong default at a customer's till.
- **The type cannot be mismatched.** `TaxEnabled.Get(ctx)` returns `bool` by construction;
  there is no `Bool(ctx, key)` that can be pointed at a money setting.
- **Find-usages works.** Renaming or removing a setting is a refactor the compiler checks,
  instead of a grep across string literals — this matters over a 10-year maintenance horizon.
- **`Get` needs no error return.** Every failure mode (undeclared key, bad type, malformed
  stored value) is either impossible by construction or resolved to the declared default with
  a logged warning. A settings read at a call site should never force error handling; that is
  what pushes people to ignore it.

Cost: settings must be declared in a package the caller can import, which is exactly the
discipline §CFG.2 asks for anyway. The string-keyed form is **still needed** for the generated
settings UI and for `Set`, so the registry retains a dynamic path (§3.7) — but application code
uses handles.

Constructors: `DeclareBool`, `DeclareString`, `DeclareInt`, `DeclareEnum`, `DeclareMoney`,
`DeclareID`, `DeclareDuration`, `DeclareJSON[T]` — matching the `value_type` CHECK constraint
already in `0001_platform.sql`.

### 3.2 Declaration without panics

`var X = config.DeclareBool(...)` at package level cannot return an error, and archlint forbids
`panic` in production code. So `Declare*` **records** problems (duplicate key, invalid default,
empty key, enum default not in the enum) into the package registry, and:

```go
func (r *Registry) Validate() error   // called once by the bootstrap, before anything runs
```

returns them as a single aggregated `errs` error at startup. A duplicate key is a developer
error that fails on the developer's machine at first boot — never a silent last-one-wins.

### 3.3 Scope resolution

ARCHITECTURE_v1 §16.1 defines the chain. Resolution walks from most specific to least and takes
the first value present:

```
Session  ►  User  ►  Branch  ►  Company  ►  System  ►  Code default
(ctx only)                                            (never absent)
```

Two reconciliations with the schema, which only permits `('system','company','branch','user')`:

- **Session** is request-scoped and never persisted — an in-memory override carried in `ctx`
  (used for "preview in another currency/locale" flows). It is deliberately absent from the
  table.
- **Business profile** (§16.4) is *not* a resolution layer. Applying the "Pharmacy" profile
  **writes company-scope rows**, which is what keeps every value individually editable
  afterwards. It is a seeding operation, not a lookup tier. Documenting this now prevents a
  fifth scope being added to the table later.

A setting is only overridable at the scopes its `Def.Scopes` lists; a write to any other scope
is a validation error. Since `config` is platform and must not import `api`:

```go
// implemented by api/appctx in Step 0.10; a fixed no-scope stub in Phase 0
type ScopeProvider interface {
    CompanyID(ctx context.Context) (id.ID, bool)
    BranchID(ctx context.Context) (id.ID, bool)
    UserID(ctx context.Context) (id.ID, bool)
}
```

### 3.4 Storage & codecs

`setting_value` is `TEXT`, JSON-encoded, with one codec per `value_type`. Two rules:

- **Money is never a float, in JSON or anywhere else** (§7.1, and archlint's `no-float` rule).
  It is encoded as `{"amount": <minor units int64>, "currency": "SYP"}` — a JSON number for
  minor units is exact for every value `int64` holds.
- `Duration` is stored as an integer of nanoseconds, not a Go duration string, so a future
  non-Go reader of the database is not required to parse `"1h30m"`.

Codecs are symmetric and round-trip-tested (§9), because a codec that silently loses precision
on write is indistinguishable from a correct one until a customer's money is wrong.

### 3.5 Cache and reactivity

**Load-all into an immutable snapshot at startup; swap the snapshot on write. D5.**

Settings are a small, bounded set (hundreds of rows at most, on a single-user desktop
database). Per-key lazy caching would add invalidation complexity and a database round-trip on
the first read of every key during boot, for no measurable gain. A whole-table load is one
query, and reads afterwards are map lookups under an `RWMutex`, with a `version` counter
incremented on every swap.

Reactivity (§16.3, "language change without restart") needs an event the bus will carry — but
the bus is Step 0.6. So `config` defines the port and 0.6 supplies the implementation:

```go
type ChangeNotifier interface {
    SettingChanged(ctx context.Context, key string, scope Scope, scopeID id.ID)
}
```

Phase 0 ships a no-op notifier. This keeps 0.5 from either importing a non-existent package or
inventing a second, competing event mechanism that would have to be deleted in 0.6.

### 3.6 Startup validation, and what to do about unknown keys

The registry validates at startup that every declared default is type-correct and enum-legal,
and that every *stored* row corresponds to a declared key.

§CFG.2 says the registry "rejects unknown keys found in the database". **I recommend softening
that to report-and-ignore. D3.**

The scenario that decides it: a customer runs 1.4, which removed `sales.legacy_rounding`. Their
database still has the row. Under a hard rejection, **the application refuses to start** — the
shop cannot open — over a row that no code reads. That is a strictly worse outcome than the
one it prevents.

This is not the same judgement as the migration version gate (0.4 §5), where refusing to start
was right: there, continuing would have *corrupted data*. Here, continuing is harmless. The
distinction is worth stating explicitly, because "fail loudly" is a good instinct that has to
be spent where it buys something.

So: unknown keys are logged at `WARN`, surfaced in `Validate()`'s report for the diagnostics
screen, and otherwise ignored. A **malformed value for a known key** is likewise logged and
falls back to the declared default rather than failing the read.

### 3.7 Writes

```go
func (s *Settings) Set(ctx context.Context, scope Scope, scopeID id.ID, key string, v any) error
```

- Runs inside the caller's Unit of Work (Step 0.3 `Do`) so a settings change participates in
  the surrounding transaction.
- Validates: key declared, scope permitted by the `Def`, value type-correct, enum member legal,
  and `Def.Validate` if supplied.
- Uses the dialect's `Upsert` on `(scope, scope_id, setting_key)` — the unique constraint
  already in `0001_platform.sql`.
- Checks permission through a port, so RBAC lands in Phase 1 without touching this code:

```go
type Authorizer interface{ Can(ctx context.Context, permission string) bool }
```

Phase 0 ships a permit-all implementation. It is a *port*, not a stub with a TODO — the
Phase-1 implementation is a constructor argument change.

---

## 4. DESIGN — feature flags

Flags (§17) are declared in code, stored in `feature_flags`, and resolved by scope — the same
shape as settings, so they **share the resolution and cache machinery and live in the same
package**, as a distinct type. D4.

```go
var AccountingUI = config.DeclareFlag(config.FlagDef{
    Key:         "accounting.ui",
    Default:     false,
    Stability:   config.Experimental,   // → Stable → Deprecated → removed
    RemoveBy:    "v1.4",                // §17: no permanent dead branches
    Scopes:      []config.Scope{config.ScopeSystem, config.ScopeCompany},
    Description: "flags.accounting.ui",
})
```

Why the same package rather than a separate one: the scope-resolution chain, the cache, the
codecs, and the change notification are identical, and duplicating them would guarantee they
drift. Why a distinct type rather than a bool setting: flags carry a **lifecycle**
(`Stability`, `RemoveBy`) that settings do not, and §17 requires enforcement in three places
(navigation, use case, seeding) that will want to enumerate flags specifically.

Step 0.5 delivers declaration and resolution. The three enforcement points arrive with the
modules and the API layer that have something to enforce.

---

## 5. DESIGN — the strategy registry

§CFG.4, generic over the contributed type:

```go
// internal/platform/strategy
type Registry[T any] struct{ /* key → T, guarded */ }

func New[T any](name string) *Registry[T]
func (r *Registry[T]) Register(key string, impl T) error  // duplicate key = error
func (r *Registry[T]) Resolve(key string) (T, error)      // missing = typed NotFound
func (r *Registry[T]) Keys() []string                     // sorted; drives admin UIs
```

Phase 0 ships the generic registry and the named, empty registries §CFG.4 lists: `costing`,
`rate_provider`, `print_renderer`, `barcode_symbology`, `payment_provider`, `report`,
`export_format`, `posting_evaluator`, `import_source`.

Two design points worth being explicit about:

- **`Resolve` returns a typed `errs` NotFound, not a zero value.** Data selects behaviour, and
  data can be wrong — a `payment_methods` row pointing at an uninstalled provider must produce
  a clear, translatable error naming the key, not a nil interface panic three frames later.
- **`Keys()` is sorted.** It drives admin dropdowns; map iteration order would reshuffle the
  UI on every launch.

A `switch` on a business "kind" anywhere in `internal/modules/**` is, per §CFG.4, **a design
defect**. §8 proposes making that enforceable rather than aspirational.

---

## 6. DESIGN — the metadata contract and seeder

Metadata (§CFG.3) is a *contract* that reference tables conform to, plus the mechanism that
seeds them. Phase 0 has no business metadata tables yet — the first are rate types and
currencies in 0.9 — so this step builds the contract and the seeder and proves them against a
fixture table in tests.

**The contract.** Every metadata table carries:

| Column | Rule |
|---|---|
| `code` | stable machine key, unique; **all references in code and seeds are by `code`, never by id** |
| `name` | translatable label (via `translations`, §9.5) |
| `is_system` | seeded, protected: **may be deactivated, never deleted** |
| `is_active` | soft availability |
| + universal columns | `id`, `created_at`, `updated_at`, `row_version` |

`is_system` is what lets code depend on `RATE_TYPE_OFFICIAL` existing without hardcoding a
UUID — the single most important property here.

**The seeder.** Idempotent, `code`-keyed, versioned JSON:

```go
func Seed(ctx context.Context, db database.DB, spec SeedSpec) (SeedReport, error)
```

- Matches existing rows **by `code`**, never by primary key, so the same seed file applies
  identically to a fresh install and a three-year-old database.
- Re-running is a no-op: re-applying the same seed version changes nothing and reports zero
  writes. This is asserted by test, because a seeder that quietly reverts an admin's edits on
  every launch is a support nightmare that looks like data corruption.
- **Never reverts admin edits to non-`is_system` rows**, and never deletes.
- Runs inside the caller's Unit of Work, using the dialect `Upsert`.

Deliberately **not** built here: JSON *file* discovery and ordering across modules. That is
one small piece of the module contract, and it belongs with the first module that ships a seed
(0.9). Building a loader now, with nothing to load, would be speculation.

---

## 7. DESIGN — package layout & dependencies

```
internal/kernel/id/                 # UUIDv7 (§2) — kernel, stdlib + google/uuid only
internal/platform/config/
├── def.go                          # Def, FlagDef, Scope, ValueType, Stability
├── declare.go                      # Declare* constructors → typed handles
├── registry.go                     # declaration registry + Validate()
├── settings.go                     # resolution, cache/snapshot, Get/Set
├── flags.go                        # feature-flag resolution
├── codec.go                        # value_type ⇄ JSON, money- and duration-safe
└── ports.go                        # ScopeProvider, ChangeNotifier, Authorizer
internal/platform/strategy/         # Registry[T] + the named registries
internal/platform/metadata/         # the contract + idempotent seeder
```

Dependencies: `config` imports `kernel/{id,errs,money,clock}` and `platform/database`.
`strategy` imports `kernel/errs` only. **Nothing imports `api`**, which is what the three ports
buy. `metadata` imports `platform/database` and `kernel/errs`.

---

## 8. DESIGN — keeping the mandate enforceable

§CFG.5 draws the boundary — invariants, safety, and integrity are never configurable, and
configuration is not a scripting language. A boundary in a document decays; **I recommend
making two parts of it mechanical. D6.**

1. **An archlint rule `forbid-call` on `switch` over known kind-columns** is not expressible
   today (archlint matches calls and imports, not statements). What *is* expressible now, and
   worth adding: a `forbid-call` entry preventing `internal/modules/**` from importing
   `platform/config` internals directly rather than through declared handles.
2. **A `Registry.Keys()`-driven test** in each later module asserting every persisted "kind"
   value resolves to a registered strategy — catching a seed that names a provider nobody
   registered, at test time rather than at a till.

Item 1 is one line of `arch-rules.yml`. Item 2 is a pattern later steps follow, recorded here
so it is not reinvented.

---

## 9. TESTING PLAN (Protocol Step 4)

| Level | Tests |
|---|---|
| **`kernel/id`** | v7 version/variant bits correct; monotonic ordering across a batch; `Parse` round-trip; nil-UUID rejected; uniqueness across concurrent goroutines (race). |
| **Declaration** | Duplicate key reported by `Validate`, not last-one-wins; invalid default (enum member, wrong type, empty key) reported; a valid registry validates clean. |
| **Resolution** | A precedence **table test** over the full chain — session/user/branch/company/system/default — asserting the first-present wins at every combination, including "declared but absent at that scope". |
| **Scopes** | Writing to a scope the `Def` does not permit is a validation error. |
| **Codecs** | Round-trip per `value_type`; **money property test** (random int64 minor units survive encode→decode exactly, never via float); duration precision preserved. |
| **Cache** | A write is visible to the next read; version counter increments; concurrent readers during a write see a consistent snapshot (`-race`). |
| **Degradation** | Unknown stored key → logged, ignored, reported by `Validate`, app still starts. Malformed value for a known key → falls back to the default, read still succeeds. |
| **Flags** | Resolution matches settings semantics; lifecycle fields survive round-trip; `RemoveBy` in the past is reported by `Validate` (so dead branches surface). |
| **Strategy** | Duplicate registration is an error; missing key is a typed NotFound naming the key; `Keys()` is sorted; concurrent `Register`/`Resolve` under `-race`. |
| **Metadata** | Seeding twice changes nothing the second time (**the idempotency drill**); an admin edit to a non-system row survives re-seeding; an `is_system` row cannot be deleted; matching is by `code` when ids differ. |
| **Integration** | Against a real migrated database from `0001_platform.sql`, through `database.Store` and a Unit of Work — not a mock, so the `UNIQUE (scope, scope_id, setting_key)` constraint and the upsert are genuinely exercised. |

Mutation-verification, as in 0.4, for the two guarantees that matter most: precedence
resolution and seed idempotency.

---

## 10. DECISIONS REQUESTED

1. **D1 — Typed handles** (`TaxEnabled.Get(ctx)`) as the application-facing API, instead of
   §CFG.2's string-keyed `cfg.Bool(ctx, key)`. The dynamic path is retained for the generated
   settings UI and `Set`. *(Recommended; §3.1.)*
2. **D2 — Build `kernel/id` in this step** (UUIDv7 via `google/uuid`, already in the module
   graph — no new dependency). It is a hard prerequisite for writing a settings row. *(§2.)*
3. **D3 — Unknown stored keys are reported, not fatal** — softening §CFG.2's "rejects", so a
   downgrade cannot brick a customer's install over a row no code reads. Malformed values fall
   back to the declared default. *(Recommended; §3.6.)*
4. **D4 — Feature flags share the settings machinery** in `platform/config` as a distinct type
   with lifecycle fields, rather than a separate package or a plain bool setting. *(§4.)*
5. **D5 — Load-all snapshot cache** with a version counter and `RWMutex`, rather than per-key
   lazy caching. *(§3.5.)*
6. **D6 — Make part of the §CFG.5 boundary mechanical** — one new `arch-rules.yml` entry now,
   plus the registry-coverage test pattern for later modules. *(§8.)*
7. **D7 — Metadata contract + idempotent `code`-keyed seeder now; JSON seed-file discovery
   deferred to 0.9**, where the first real seed exists. *(§6.)*
8. **D8 — Three ports** (`ScopeProvider`, `ChangeNotifier`, `Authorizer`) with trivial Phase-0
   implementations, so `platform/config` never imports `api` and does not pre-empt the event
   bus (0.6) or RBAC (Phase 1). *(§1.2, §3.3, §3.5, §3.7.)*

On approval I'll implement in this order, each independently reviewable: `kernel/id` →
codecs → declaration registry + `Validate` → resolution + cache → `Set` + ports → flags →
strategy registry → metadata contract + seeder → the integration and mutation drills, tests
alongside each, then the self-review and improvement notes (Protocol Steps 5–6).

---

## 11. IMPLEMENTATION RECORD (Protocol Steps 3–6)

All eight decisions were approved as recommended and implemented as designed. What follows
is what the design did **not** anticipate.

### 11.1 Two kernel gaps found while building

Both are halves of contracts that already existed and were incomplete:

- **`round.ParseMode`.** `RoundingMode.String()` is documented as "suitable for storage and
  settings", but nothing could read it back. A money setting stores its currency's rounding
  mode (§3.4), so the parser had to exist. Unknown names are **rejected, not defaulted**: a
  typo in a seed that silently changed how money rounds would be nearly untraceable from the
  symptom.
- **`clock.Format` / `clock.ParseTimestamp` / `clock.TimestampLayout`.** Step 0.4 had grown a
  private `formatTimestamp` inside `platform/migrate`, and this step needed the identical
  CHAR(24) format. A second copy is how two subtly different timestamp formats end up in one
  database, so the definition now lives once in the kernel.

### 11.2 `MustParse` written, then removed

`id.MustParse` was written for seed literals, then deleted before commit. Because archlint
forbids `panic` in production code it could not panic, so it returned the zero ID on
failure — a function named `Must…` that silently yields an invalid primary key is a trap for
whoever reads it next. Nothing used it. Removing it also lifted `kernel/id` coverage from
68.8% to 91.7%, which is the more honest number.

### 11.3 D6 — the archlint rule, substituted

The design proposed "a `forbid-call` entry preventing `internal/modules/**` from importing
`platform/config` internals". On implementation this turned out **not to be expressible**:
archlint matches imports and call selectors, and "bypassed a typed handle" is neither. A rule
written to look like enforcement while enforcing nothing is worse than no rule, so two
genuinely enforceable boundaries were added instead, both serving the same layering intent:

| Rule | What it forbids |
|---|---|
| `platform-independent-of-modules` | `internal/platform/**` importing `internal/modules/**` or `internal/api/**` |
| `dialect-is-platform-only` | `internal/modules/**` and `internal/api/**` importing the dialect shim |

The first protects exactly what this step built: a module type leaking into `platform/config`
would invert the architecture and make the configuration mechanisms un-reusable. The second
enforces ARCHITECTURE_v1 §8.5.

**Both were verified by planting violations** and confirming archlint fails on each, then
removing them. The registry-coverage test pattern (design §8 item 2) stands as written, for
the later modules that will have strategies to cover.

### 11.4 Deviation: extension points are named, not pre-typed

§CFG.4 says Phase 0 ships "these named registries, empty". `Registry[T]` is generic, and the
contract types (`CostingStrategy`, `RateProvider`, …) do not exist until the modules that
define them. Declaring placeholder `interface{}` types now would be a fiction every later
phase must replace, and would let anything at all register in the meantime.

So `platform/strategy` exports the extension-point **names** as constants (`PointCosting`,
`PointRateProvider`, …, with `Points()` listing all nine), and the typed registry is created
by the package owning the contract: `strategy.New[CostingStrategy](strategy.PointCosting)`.
The names cannot collide and are discoverable in one place, which was the actual goal.

### 11.5 Known limitation: cache patch versus a joined rollback

`Settings.Set` runs in a Unit of Work and patches the snapshot after `Do` returns. Step 0.3's
`Do` **joins** an outer transaction rather than opening a second one, so a `Set` nested inside
a larger business transaction that later rolls back leaves the snapshot holding a value the
database does not.

This is documented on `Set`, and `Reload` is the recovery. It is not fixed in code because
every fix is worse: detecting the join would leak transaction state into the config layer, and
reloading on every read would defeat the cache entirely. Settings changes are their own use
case in practice, so the path is an edge, not the norm. Revisit if a module ever needs to
change a setting inside a business transaction.

### 11.6 Verification

**60 tests across six packages, `-race` clean, `make ci` green** (build · archlint · vet ·
tests · golangci-lint · frontend).

| Package | Tests | Coverage |
|---|---|---|
| `kernel/id` | 8 | 91.7% |
| `kernel/clock` | 6 | 80.0% |
| `kernel/round` (ParseMode) | 7 | 100% |
| `platform/config` | 23 | 79.4% |
| `platform/strategy` | 8 | 97.1% |
| `platform/metadata` | 8 | 85.6% |

The config tests run against the **real `0001_platform.sql` schema** through the Step 0.4
migration runner, so the `UNIQUE (scope, scope_id, setting_key)` constraint and the
`value_type` CHECK are genuinely exercised rather than mocked.

**Mutation-verified**, as promised in §9:

- Reversing `resolutionOrder` → the precedence table fails on 5 of its 9 cases.
- Forcing `sameValues` to report a difference → the idempotency drill fails, reporting 2
  rows updated on a second identical seed.

One test was also found to pass **for the wrong reason** during review: "non-system scope
without an id" used a setting that permitted only system scope, so it was rejected by the
scope check before ever reaching the identifier check. It now uses a company-scoped setting
and asserts the intended error.

---

*End of Step 0.5. Design approved, implemented, self-reviewed.*
