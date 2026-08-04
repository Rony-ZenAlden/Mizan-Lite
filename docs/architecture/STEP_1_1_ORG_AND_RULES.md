# Step 1.1 — Architecture Rules & the Org Module (Design + implementation record)

> Status: **DESIGN — implemented in the same turn at the reviewer's explicit instruction.**
> This collapses the design→approval gate the protocol normally holds per step. The decisions
> below are therefore flagged rather than silently taken, and **D1 in particular corrects the
> phase-level design** — it is the one I would most have wanted reviewed first.
> Scope: two new archlint rule kinds (`module-isolation`, `no-sql`), and `internal/modules/org`
> — schema `0004`, domain, repositories, and the provisioning operation.
> **Out of scope:** identity (1.2), the `Setup.*` bindings and wizard UI (1.9/1.10), the org
> settings screens (1.11), and anything that reads a fiscal period (Phase 2).

Two jobs in one step, and the pairing is deliberate: **the rules must land before the imports
they forbid have been written.** Phase 1 adds three modules to a codebase that has had exactly
one, so this is the last cheap moment to turn `module-isolation` on.

---

## 1. ANALYSIS

### 1.1 A correction to the phase design — D1

`PHASE_1_CORE_DATA.md` §SEQ describes step 1.1 as *"…and seeding of the default
company/branch/warehouse."* That is **wrong**, and it contradicts the same document's §WIZ.1,
where `Setup.Apply` creates exactly those rows inside one transaction.

Both cannot hold. If 1.1 seeds a company at boot, then by the time the wizard runs, a company
already exists — so either the wizard's "no company exists" invariant (§WIZ.1) is dead on
arrival, or the wizard must reconcile itself with a placeholder company it did not create and
whose name, country, and currency are wrong.

The source of the error is §26.1's phrasing — *"companies, branches, warehouses tables exist
and are **seeded** with one default row each"* — which describes the **end state after setup**,
not a metadata seeder running at boot. Step 1.1 read it as the latter.

**Recommendation: Step 1.1 creates nothing at boot.** It delivers the schema, the domain, and a
`Provision` operation; §WIZ.1's wizard calls it in 1.9. The "exactly one company" invariant
lives in the service, not only in the binding, so it holds regardless of who calls.

This is the decision I would most have wanted reviewed before implementing, and it is recorded
first for that reason.

### 1.2 `module-isolation` is cheaper than the docs assumed — D2

`arch-rules.yml` lists `module-isolation` under *"Planned rule kinds… The following require a
whole-program package-graph pass (tools/archlint -graph)"*, and `PHASE_0_FOUNDATION` §REPO.3
inherited that assumption. Step 0.12 repeated it.

**It is not true.** The rule is *"a module may import another module only via its `/contract`"*,
and deciding it needs only two facts, both local to the file being examined:

1. which module the file is in — from its own package path;
2. which module each import belongs to — from the import path.

No graph, no whole-program pass. It is an ordinary per-file rule, the same shape as
`import-boundary`. The graph-pass caveat is genuinely required for `max-dependency-depth` and
`no-package-cycles`, and it was over-applied to this one.

Worth recording because it changes the cost of a rule the project has now deferred three times
on the strength of that assumption.

### 1.3 REPO.3 rule 7, restated — D3

Step 0.12 found rule 7 (*"No raw SQL string outside `infra` packages"*) unenforced and its
wording too narrow: SQL legitimately lives in eight `platform/*` packages that own their own
tables. The intent — **no SQL in `domain`, `app`, or `api`** — holds and is worth enforcing.

A new `no-sql` rule kind scans string literals for a leading SQL verb inside the scoped
packages. It checks the thing the rule is actually about, rather than a proxy such as "does
this package import the database".

### 1.4 Risks and how the design answers each

| Risk | Consequence if unhandled | Design answer |
|---|---|---|
| A rule that looks like enforcement but enforces nothing | Worse than no rule (0.5 §11.3) | Both rules **verified by planting violations**, as 0.5 and 0.11 were (§8) |
| `no-sql` fires on prose containing "select" | The gate gets disabled | Matches a leading SQL **verb followed by a keyword**, and is scoped to three layers (§3.2) |
| Two companies created | Every scope becomes ambiguous | `Provision` is rejected when any company exists — in the **service**, not the binding (§5.3) |
| Provisioning partially applied | A company with no branch is unusable and unrecoverable by any screen | One Unit of Work; the caller's transaction (§5.3) |
| Deactivating the last branch | An organisation with nowhere to trade | Domain invariant (§4.3) |
| Fiscal periods with gaps or overlaps | Phase 2 postings land nowhere, or twice | Generated, never hand-entered; tiling asserted (§4.4) |
| `created_by` FK to a table that does not exist yet | Migration 0004 cannot apply before identity | Nullable, **no FK** — and the reason is not just ordering (§3.3) |

---

## 2. DESIGN — the `module-isolation` rule

```yaml
module-isolation:
  enabled: true
  modules_root: "self/internal/modules"
  contract_dir: "contract"
  message: "a module may import another module only through its /contract package"
```

For a file in `internal/modules/X/...` importing `internal/modules/Y/...`:

| Case | Verdict |
|---|---|
| `Y == X` | allowed — a module's own internals |
| `Y != X`, import is `…/modules/Y/contract` or below | **allowed** — the published channel (§3.2) |
| `Y != X`, anything else | **violation** |

Imports from outside `internal/modules/**` are not this rule's business:
`platform-independent-of-modules` (0.5) already stops platform reaching in, and the composition
root is *supposed* to import every module.

**Why `contract` and not the module root:** §3.2 is explicit that a module "exports a small
`contract` package containing interfaces and DTOs. It never exports its aggregates. Sales must
not be able to construct an `inventory.StockLevel`." Allowing the module root would allow
exactly that.

Currency has no `contract` package and imports no other module, so the rule is **green on the
current tree and constrains everything Phase 1 adds** — which is the point of landing it now.

---

## 3. DESIGN — the `no-sql` rule

### 3.1 Configuration

```yaml
no-sql:
  enabled: true
  applies_to:
    - "self/internal/modules/*/domain/**"
    - "self/internal/modules/*/app/**"
    - "self/internal/api/**"
  message: "SQL belongs to infra/platform — domain, app, and api must not contain it"
```

### 3.2 Detection

A string literal is SQL if, ignoring leading whitespace and case, it **starts with** one of
`SELECT` `INSERT` `UPDATE` `DELETE` `CREATE` `ALTER` `DROP` and contains a second SQL keyword
(`FROM`, `INTO`, `SET`, `WHERE`, `VALUES`, `TABLE`, `JOIN`).

The two-token requirement is what keeps the rule usable: `"update the display name"` is prose
and stays legal; `"UPDATE branches SET name = ?"` is not. A single-keyword match would fire on
ordinary English and the gate would be switched off within a week — which is the failure mode
0.5 §11.3 warns about from the other direction.

### 3.3 What it deliberately does not cover

Concatenated or `fmt.Sprintf`-built SQL evades a literal scan. That is accepted: the rule is a
**tripwire against the easy mistake**, not a proof. The structural defence remains
`domain-purity` (domain cannot import the database at all) and code review for `app`.
Overstating what a rule proves is worse than a narrow rule honestly described.

---

## 4. DESIGN — the org domain

### 4.1 Schema — `0004_org.sql`

Currency owns `0003` (module-owned, 0.9 §9.2), so org owns `0004`, embedded in
`internal/modules/org/migrations/`. Portable type contract throughout (§8.1).

```sql
companies (
  id CHAR(36) PK, code VARCHAR(40) NOT NULL, name VARCHAR(200) NOT NULL,
  legal_name VARCHAR(200), tax_number VARCHAR(60),
  country_code CHAR(2) NOT NULL,
  functional_currency CHAR(3) NOT NULL REFERENCES currencies(code),
  pricing_currency CHAR(3) REFERENCES currencies(code),      -- NULL → = functional
  logo_ref VARCHAR(255),
  is_active SMALLINT NOT NULL DEFAULT 1,
  created_at, created_by, updated_at, updated_by, row_version,
  CONSTRAINT ux_companies_code UNIQUE (code)
);

branches (
  id, company_id NOT NULL REFERENCES companies(id),
  code VARCHAR(40) NOT NULL, name VARCHAR(200) NOT NULL,
  is_default SMALLINT NOT NULL DEFAULT 0,
  address_json TEXT,                       -- shaped by the country profile (§C.1)
  is_active SMALLINT NOT NULL DEFAULT 1, + universal,
  CONSTRAINT ux_branches_code UNIQUE (company_id, code)
);

warehouses (
  id, branch_id NOT NULL REFERENCES branches(id),
  code, name, is_default,
  allows_negative_stock SMALLINT NOT NULL DEFAULT 0,   -- read from Phase 4 (§21.2)
  is_active, + universal,
  CONSTRAINT ux_warehouses_code UNIQUE (branch_id, code)
);

fiscal_years (
  id, company_id NOT NULL REFERENCES companies(id),
  code VARCHAR(20) NOT NULL, start_date CHAR(10) NOT NULL, end_date CHAR(10) NOT NULL,
  status VARCHAR(10) NOT NULL DEFAULT 'open',   -- open|closed|locked
  + universal,
  CONSTRAINT ux_fiscal_years UNIQUE (company_id, code)
);

fiscal_periods (
  id, fiscal_year_id NOT NULL REFERENCES fiscal_years(id),
  sequence INTEGER NOT NULL, start_date CHAR(10) NOT NULL, end_date CHAR(10) NOT NULL,
  status VARCHAR(10) NOT NULL DEFAULT 'open',
  + universal,
  CONSTRAINT ux_fiscal_periods UNIQUE (fiscal_year_id, sequence)
);
```

**FK to `currencies(code)`** is legal against a UNIQUE column on every target engine (0.9 §3.1)
and means a company cannot name a currency that does not exist.

### 4.2 `created_by` / `updated_by` carry no foreign key — D4

§9.1 lists them as `CHAR(36)` and does **not** give them a foreign key. Two reasons to keep it
that way, and the second matters more than the ordering one:

1. `users` does not exist until 1.2, so an FK from `0004` would be a forward reference.
2. Even after 1.2, the audit trail snapshots `actor_name_snapshot` precisely so a five-year-old
   record stays readable (§15.1). Binding these columns to a live row would make the *identity*
   table load-bearing for the *history*, which is the coupling snapshots exist to avoid.

### 4.3 Invariants enforced in the domain

- A company's `code`, `name`, `country_code` (2 chars) and `functional_currency` are required.
- **Exactly one default branch per company**, and one default warehouse per branch.
- **The last active branch cannot be deactivated**, nor the last active warehouse of a branch.
  A business with nowhere to trade is a state no screen can recover from.
- `pricing_currency` NULL means "same as functional" — the §18.1 collapse-to-identity path, not
  a missing value.

### 4.4 Fiscal year generation — D6

Fiscal periods are **generated, never hand-entered**: given a start month and a calendar year,
`GenerateFiscalYear` produces twelve consecutive monthly periods that tile the year exactly —
no gaps, no overlaps, last period ending the day before the next year starts.

Hand entry would let a period boundary land a day out, which in Phase 2 means a posting that
belongs to no period or to two. Generation makes that unrepresentable rather than validated.

Month-length and leap-year handling come from `time`, not from arithmetic on day counts.

---

## 5. DESIGN — the service

### 5.1 Surface

```go
type Service struct{ /* repos, clock */ }

func (s *Service) IsProvisioned(ctx) (bool, error)
func (s *Service) Provision(ctx, in ProvisionInput) (ProvisionResult, error)

func (s *Service) Company(ctx) (domain.Company, error)      // the single company
func (s *Service) Branches(ctx) ([]domain.Branch, error)
func (s *Service) DefaultBranch(ctx) (domain.Branch, error)
func (s *Service) Warehouses(ctx, branchID id.ID) ([]domain.Warehouse, error)
func (s *Service) FiscalYears(ctx) ([]domain.FiscalYear, error)
```

### 5.2 `ProvisionInput`

```go
type ProvisionInput struct {
    Company   CompanyInput      // code, name, legalName, taxNumber, countryCode, currencies
    Branch    BranchInput       // code, name
    Warehouse WarehouseInput    // code, name
    FiscalYearStartMonth time.Month
    FiscalYearStartYear  int
}
```

### 5.3 The one-company invariant lives here — not in the binding

`Provision` fails with a typed `Conflict` if any company row exists. §WIZ.1 puts the same check
on the `Setup.*` bindings; having it in the service too is deliberate defence in depth — the
binding check is about *reachability*, this one is about *truth*, and a future importer or
migration tool that calls the service directly gets the same guarantee.

`Provision` executes inside the **caller's** Unit of Work (0.3 join semantics), so the wizard
can wrap company creation and the first administrator in one transaction, exactly as §WIZ.1
requires.

### 5.4 No `contract` package yet — D5

`module-isolation` makes `contract` the only legal cross-module channel, so it is tempting to
create one now. **Deferred to 1.2**, where identity becomes the first real consumer and can say
what it actually needs.

This is 0.9 D5's reasoning applied unchanged: a contract designed against an imagined consumer
is a guess that the real consumer then has to live with. Nothing is lost by waiting one step —
and the new rule guarantees the omission is caught the moment someone reaches across.

### 5.5 The `Module` implementation

Follows currency exactly: `Name() = "org"`, `DependsOn() = ["currency"]` (the FK on
`functional_currency` is a genuine dependency, and the topological sort from 0.10 now has
something real to order), `Migrations()` from the embedded FS, and `nil` for settings, flags,
metadata, subscriptions, jobs, and bindings.

`Permissions()` does not exist on the interface until 1.4.

---

## 6. DESIGN — wiring

`bootstrap.Start` constructs the org service and module alongside currency. **Nothing is
provisioned at boot** (D1): `Start` succeeds against an empty, unprovisioned database, which is
exactly the state a fresh install is in before the wizard runs.

`App.Org` joins the graph so 1.9's `Setup` binding and 1.3's `appctx` can reach it.

---

## 7. TESTING PLAN

| Level | Tests |
|---|---|
| **`module-isolation`** | A module importing another's `domain`/`infra`/root is a violation; importing its `contract` is not; importing its **own** packages is not; a non-module importer is unaffected. **Verified by planting** a real violation in the tree. |
| **`no-sql`** | A `SELECT … FROM` literal in `domain`/`app`/`api` is a violation; the same literal in `infra` or `platform` is not; prose containing "update"/"select" is **not** flagged; SQL in a `_test.go` is not flagged. Verified by planting. |
| **Schema** | `0004` applies on top of `0001`–`0003`; portable-contract audit (no SQLite-isms, CHECKs present); a company cannot reference a missing currency; a branch cannot outlive its company. |
| **Domain** | Company/branch/warehouse construction rejects empty code, empty name, and a malformed country code; `pricing_currency` NULL resolves to functional. |
| **Default rows** | Exactly one default branch per company and one default warehouse per branch; promoting a new default demotes the previous one **in the same transaction**. |
| **Last-active guard** | Deactivating the only active branch is a typed error; deactivating one of two succeeds. |
| **Fiscal generation** | Twelve periods; they tile the year with **no gap and no overlap**; a February in a leap year is 29 days; a July start wraps into the following calendar year correctly. |
| **Provisioning** | A fresh database provisions company + branch + warehouse + fiscal year in one transaction; a **second** `Provision` is a typed conflict; a provision that fails midway leaves **zero** company rows. |
| **Boot** | `bootstrap.Start` succeeds on an unprovisioned database and reports `IsProvisioned() == false`; after provisioning, a second boot reports `true` and creates nothing. |

**Mutation-verified**: the *one-company invariant* (drop the existence check → the second-provision
test must fail) and *fiscal tiling* (off-by-one the period end → the no-gap assertion must fail).

---

## 8. DECISIONS TAKEN

Recorded rather than requested, since implementation followed immediately. **D1 is a correction
to an approved document** and is the one to review hardest.

1. **D1 — Step 1.1 provisions nothing at boot.** It delivers schema, domain, and a `Provision`
   operation the wizard calls in 1.9. This corrects `PHASE_1_CORE_DATA.md` §SEQ, which
   contradicted §WIZ.1. *(§1.1.)*
2. **D2 — `module-isolation` is an ordinary per-file rule**, not a whole-program pass, and
   lands now, before the imports it forbids exist. *(§1.2, §2.)*
3. **D3 — REPO.3 rule 7 restated as `no-sql` over `domain`/`app`/`api`**, honestly described as
   a tripwire rather than a proof. *(§1.3, §3.)*
4. **D4 — `created_by`/`updated_by` are nullable with no foreign key**, so history does not
   depend on a live identity row. *(§4.2.)*
5. **D5 — No `contract` package until 1.2**, when identity is a real consumer. *(§5.4.)*
6. **D6 — Fiscal periods are generated, never hand-entered.** *(§4.4.)*

---

## 9. IMPLEMENTATION RECORD (Protocol Steps 3–6)

### 9.1 The new rules found a real gap in their own first draft

`module-isolation` fired immediately — on Go's own synthetic test packages. An external test
package for `modules/currency` has the import path `…/modules/currency_test`, which the first
implementation read as *a module named `currency_test` importing module `currency`*.

The fix is not a special case for `_test`: the rule now walks each **file's** imports rather
than the package's, so `skipPos` — the framework's existing "architecture rules govern
production source" helper — exempts test files and the build cache uniformly. It also improved
the diagnostics, since reporting now lands on the offending import line instead of the file
head.

Worth recording because the first draft would have *passed* on the current tree while being
wrong: currency has no cross-module imports, so nothing but the test-package artefact exposed
it.

### 9.2 Both rules verified by planting, in both directions

Per the 0.5 §11.3 precedent, neither rule is trusted until watched to fail:

| Rule | Planted | Result |
|---|---|---|
| `module-isolation` | module A imports module B's `domain` | **violation**, naming both modules and the exact line |
| `module-isolation` | same import via B's `/contract` | **allowed** (0 violations) |
| `no-sql` | `"SELECT code, symbol FROM currencies…"` in a `domain` package | **violation** |
| `no-sql` | `"update the display name before saving"` | **not** flagged |
| `no-sql` | `"select a currency"` | **not** flagged |

The two prose strings are the ones that matter: a single-keyword match would have flagged both,
and a rule that cries wolf on ordinary English gets switched off.

Own-module imports needed no planted case — currency imports its own `domain` and
`infra/sqlite` on a green tree, which proves the allowance.

### 9.3 The `Do` method is not on `database.DB`

`Provision` needs a Unit of Work, and `Do` lives on `database.UnitOfWork`, not on
`database.DB`. Rather than reaching for the concrete `*database.Store`, the module declares a
two-line `Database` interface embedding both — naming the two capabilities it actually uses.
`*Store` satisfies it, and a test double would only need those.

### 9.4 A pre-existing test was brittle in a way this step exposed

`TestStartBuildsAWorkingGraph` asserted `len(app.Modules) != 1 … want [currency]`. Adding org
broke it — exactly the failure Step 0.6 §11.2 fixed for the migration count, with the same
diagnosis: *a test that must be edited every time the system legitimately grows is a
maintenance tax, not a safety net.* It now asserts by **presence**, which matters because Phase
1 adds three more modules and would otherwise break it three more times.

### 9.5 Two bugs caught while writing the harness

- `t.TempDir()` returns a **new** directory on every call, so opening the pool at one path and
  pointing the migration runner at another silently migrated a different, empty database. Named
  once now.
- The first harness referenced a `migrationsfortest` package that did not exist. Replaced with
  the real merged set — `migrations.SQLite()` + currency's `0003` + org's `0004`, assembled
  exactly as the composition root assembles it — so the foreign key from
  `companies.functional_currency` to `currencies(code)` is genuinely exercised. It is:
  `TestProvisionRejectsAnUnknownCurrency` passes because the database refuses the row.

### 9.6 The topological sort finally has something to order

Org depends on currency (the FK above), so the composition root hands
`modules.Order` the two modules **deliberately in the wrong order** — org first — and
`TestModulesAreOrderedByDependency` asserts currency comes out ahead. Step 0.10 built the sort
against a single node, where it was trivially correct and entirely unexercised.

### 9.7 Verification

**`make ci` green**: gofmt · build · archlint · vet · tests (race) · golangci-lint · frontend.
16 new Go tests across `modules/org` and `modules/org/domain`, plus two in `bootstrap`.

**Mutation-verified**, as promised in §7:

- *One-company invariant* — removing the `IsProvisioned` guard from `Provision` fails
  `TestProvisionIsRejectedTwice` with *"a second Provision succeeded; every scope is now
  ambiguous"*.
- *Fiscal tiling* — moving a period end one day earlier fails
  `TestFiscalPeriodsTileTheYear` with *"gap of 48h0m0s between periods, want exactly one day"*
  for every period, in all four tested start months.

### 9.8 Carried forward

- **`contract` package** (D5) lands in 1.2, when identity becomes the first real consumer and
  can say what it needs. `module-isolation` now guarantees the omission is caught rather than
  worked around.
- **`Auditable` events** from org land in 1.7 with the audit module.
- **`Setup` bindings** wrap `Provision` in 1.9; **org settings screens** in 1.11.
- **Fiscal period status transitions** (open → closed → locked) are Phase 2's, with the first
  code that posts into a period.
- **Ten new error codes** are translated in both locales; the 0.8 coverage gate enforced it.

---

*End of Step 1.1. Implemented and self-reviewed. Per the protocol, the Step 1.2 design will be
delivered for approval before any identity code is written.*
