# Mizan ERP — Phase 1: Core Data (Detailed Design)

> Companion to [`ARCHITECTURE_v1.md`](./ARCHITECTURE_v1.md),
> [`ADDENDUM_v1.1_resolved_design.md`](./ADDENDUM_v1.1_resolved_design.md) and
> [`PHASE_0_FOUNDATION.md`](./PHASE_0_FOUNDATION.md).
> Status: **DRAFT — awaiting approval. No implementation until this is signed off**
> (Permanent Development Protocol, Step 2).
> Per-step designs follow this document, each with its own approval gate, exactly as Phase 0
> ran (0.2 … 0.12).

Phase 0 built a kernel with no users, no owner, and no memory of who did what. **Phase 1 gives
the system an identity: whose data it is, who may touch it, and what happened.**

§32 puts it second for a blunt reason — *"nothing can be built safely without auth + audit."*
Every module after this one records an actor and checks a permission. Retrofitting either into
twelve modules is the kind of work that does not get done.

## Contents

| § | Section |
|---|---|
| **SCOPE** | What Phase 1 includes and explicitly excludes |
| **ORG** | Company, branches, warehouses, fiscal calendar |
| **IDN** | Authentication: credentials, sessions, lockout |
| **RBAC** | Permissions, roles, scope |
| **POL** | The policy mechanism — how "nothing ships unprotected" is *enforced* |
| **AUD** | The audit trail, and a contradiction in the approved architecture |
| **PROF** | Country profiles, business profiles, seed-file discovery |
| **WIZ** | First-run setup, and the bootstrap paradox |
| **FE** | Login, session, routing, permission-aware UI |
| **DEBT** | The Phase 0 carried-forward items this phase closes |
| **TEST** | Testing strategy |
| **DOD** | Definition of Done for Phase 1 |
| **SEQ** | Step sequence |
| **DEC** | Decisions requested |

---

## SCOPE — what Phase 1 is and is not

**In scope:**

- `modules/org` — companies, branches, warehouses, fiscal years and periods
- `modules/identity` — users, credentials, sessions, login attempts, roles, permissions
- `modules/audit` — the append-only audit trail and its subscribers
- The **authorization mechanism**: policy declaration, the enforcement decorator, scope, and
  field-level redaction
- **Country and business profiles** as seed data, plus the JSON seed-file discovery deferred
  from 0.5 and 0.9
- The **first-run setup wizard**, backend and frontend
- Frontend: login, session handling, routing, and permission-aware navigation

**Explicitly excluded (later phases):** products, partners, inventory, sales, purchasing,
accounting postings, tax rules, reports. **PIN login** (Phase 5, with the POS that needs it —
§IDN.5). **Approval limits** (Phase 5, §RBAC.5). **Audit hash chaining** (enterprise edition,
decision 8 — columns stay reserved and NULL). **External identity providers** (behind the
`Authenticator` port, no implementation).

Phase 1 ends with an application that a real business can be configured into: a company with a
branch and a warehouse, an administrator who logged in, roles that actually block actions, and
an audit trail recording all of it.

---

## ORG — the organisational spine

### ORG.1 Why this is first

Every scope in the system resolves against it. Settings resolve `company` and `branch` (0.5);
RBAC grants are scoped to a branch (§14.2); every transactional table will carry
`branch_id NOT NULL` (§26.1). None of that is expressible until the rows exist.

### ORG.2 Schema

```sql
companies (
  id, code, name, legal_name, tax_number,
  country_code        CHAR(2)  NOT NULL,      -- drives the country profile (§PROF)
  functional_currency CHAR(3)  NOT NULL,      -- §18.1; the ledger currency
  pricing_currency    CHAR(3),                -- NULL → same as functional
  logo_ref            VARCHAR(255),
  is_active, + universal columns
);

branches (
  id, company_id, code, name,
  is_default          SMALLINT NOT NULL DEFAULT 0,   -- exactly one per company
  address_json        TEXT,                          -- shaped by the country profile
  is_active, + universal columns,
  CONSTRAINT ux_branches_code UNIQUE (company_id, code)
);

warehouses (
  id, branch_id, code, name,
  is_default          SMALLINT NOT NULL DEFAULT 0,
  allows_negative_stock SMALLINT NOT NULL DEFAULT 0,  -- §21.2, read from Phase 4
  is_active, + universal columns,
  CONSTRAINT ux_warehouses_code UNIQUE (branch_id, code)
);

fiscal_years (
  id, company_id, code, start_date CHAR(10), end_date CHAR(10),
  status VARCHAR(10) NOT NULL,            -- open | closed | locked
  + universal columns
);

fiscal_periods (
  id, fiscal_year_id, sequence INTEGER, start_date, end_date,
  status VARCHAR(10) NOT NULL,            -- open | closed | locked
  + universal columns
);
```

**One company in v1, schema-ready for more.** `companies` is a table rather than a settings
row because multi-company is a plausible future and retrofitting a `company_id` onto every
table is precisely the §26 argument. v1 creates exactly one and the UI never shows a picker.

**`fiscal_years`/`fiscal_periods` are created here, unused until Phase 2.** They belong to org
by §9.2, the wizard sets the fiscal year start (§C.2 step 6), and generating twelve periods
from a start month is trivial. This is the §7/0.4-D4 precedent — a table whose shape is already
fixed by approved design — and *not* the 0.9-D1 case, where deferral was right because the
requirements would move.

### ORG.3 The default row rule

Setup creates one company, one branch, one warehouse. `is_default` marks each. The domain
enforces **exactly one default per parent** and forbids deactivating the last active branch or
warehouse — an organisation with nowhere to trade is a state no screen can recover from.

---

## IDN — authentication

### IDN.1 Credentials

**Argon2id**, per §13.1, with parameters stored **per hash** so they can be raised over time
without invalidating existing passwords:

```sql
users (
  id, company_id, username VARCHAR(60) NOT NULL,
  display_name, email, locale, is_active,
  is_system SMALLINT NOT NULL DEFAULT 0,     -- protects the setup administrator
  + universal columns,
  CONSTRAINT ux_users_username UNIQUE (company_id, username)
);

user_credentials (
  id, user_id, credential_type VARCHAR(12) NOT NULL,   -- 'password' | 'pin'  (§IDN.5)
  hash TEXT NOT NULL,                                   -- encoded Argon2id string
  algorithm VARCHAR(20) NOT NULL, params_json TEXT NOT NULL,
  must_change SMALLINT NOT NULL DEFAULT 0,
  expires_at CHAR(24), + universal columns
);

password_history (id, user_id, hash, created_at);       -- reuse policy (§13.1)
```

Credentials are a **separate table** from `users` so a password hash is never selected by a
routine user query and never lands in a DTO by accident. That is cheap insurance against the
single worst leak in the system.

**Dependency check (D1).** `golang.org/x/crypto/argon2` is the natural choice and is pure Go.
Whether it is already in the module cache **must be confirmed before Step 1.2 begins**; if it
is not, this is a one-time fetch, the same call made for Radix in 0.11 (offline means *builds
and runs* offline once deps are fetched, not that dependencies may never be added). The
alternative — hand-writing Argon2id — is categorically not acceptable for a password hash.

### IDN.2 Sessions

```sql
sessions (
  id, user_id, branch_id,                    -- the branch this session is acting in
  created_at, last_seen_at, expires_at,
  absolute_expires_at,                        -- separate from idle expiry (§13.1)
  ended_at, end_reason VARCHAR(20),           -- logout | idle | absolute | revoked
  device_info VARCHAR(200), + universal columns
);
```

A desktop application has one user at the machine, so a session is an in-process object backed
by a row — the row exists so "stay signed in" survives a restart and so an administrator can
see and revoke sessions. **Idle timeout and absolute expiry are separate settings**, because
they answer different questions ("walked away" vs "this credential has been alive too long").

### IDN.3 Lockout

```sql
login_attempts (id, username, user_id NULL, succeeded, ip_or_device, attempted_at, reason);
```

Configurable threshold with exponential backoff (§13.1), every attempt recorded, surfaced in
the audit log. `user_id` is nullable because **an attempt against a username that does not
exist must still be recorded** — that is the shape of an attack, and discarding it discards the
evidence.

**Failure responses are identical** for "no such user" and "wrong password". A distinguishable
response is a username oracle.

### IDN.4 The `Authenticator` port

```go
type Authenticator interface {
    Authenticate(ctx context.Context, c Credentials) (Principal, error)
}
```

Local password authentication is the only implementation. Windows Hello, Touch ID, LDAP, and
SSO (§13.2) plug in behind this with no schema change. Defining the port costs one file.

### IDN.5 PIN login is deferred — D2

§13.1 specifies PIN login for POS: a second, weaker credential, allowed only on an
already-unlocked device, POS-scoped, rate-limited, separately hashed.

**Recommendation: schema now (`credential_type` already carries `'pin'`), implementation in
Phase 5.** A PIN is meaningless without the POS screen that switches cashiers, and its rules —
which permissions a PIN session may hold, what "device already unlocked" means in the UI — are
decisions the POS design will make properly and this one would guess. The column costs nothing;
guessing costs a rewrite. Same judgement as 0.9's deferral of the rate-preview tables.

---

## RBAC — authorization

### RBAC.1 Schema

```sql
permissions (
  id, code VARCHAR(80) NOT NULL,            -- 'sales.invoice.void'  — CODE-DEFINED
  module VARCHAR(40) NOT NULL, description VARCHAR(200),   -- i18n key
  is_obsolete SMALLINT NOT NULL DEFAULT 0,
  CONSTRAINT ux_permissions_code UNIQUE (code)
);

roles (
  id, company_id, code, name, description,
  is_system SMALLINT NOT NULL DEFAULT 0,    -- seeded roles: editable, never deletable
  is_active, + universal columns
);

role_permissions (
  id, role_id, permission_code VARCHAR(80) NOT NULL,   -- by CODE, not id (§CFG.3)
  scope VARCHAR(12) NOT NULL DEFAULT 'global',         -- global | branch | warehouse
  CONSTRAINT ux_role_permissions UNIQUE (role_id, permission_code, scope)
);

user_roles (
  id, user_id, role_id,
  scope_type VARCHAR(12), scope_id CHAR(36),   -- NULL/NULL = everywhere
  CONSTRAINT ux_user_roles UNIQUE (user_id, role_id, scope_type, scope_id)
);
```

**Permissions are code-defined and synced at startup** (§14.1) through `Module.Permissions()`,
which finally joins the `Module` interface — the item 0.9 D5 deferred. A permission the code no
longer declares is marked `is_obsolete`, **never deleted**: deleting it would silently drop the
grants referencing it, and a downgrade would then hand users permissions they should not have.
Obsolete-and-reported is the data-leftover side of 0.10's D3 taxonomy.

**Grants reference `permission_code`, not `permissions.id`** — the §CFG.3 rule that lets code
depend on a permission existing without hardcoding a UUID.

### RBAC.2 Scope from day one — D3

§14.2 is emphatic, and it is right: the check signature is
`Can(ctx, permission, scope)` **from the first implementation, even while only the global scope
exists.** A branch manager who may void invoices *in their branch* is not expressible later
without touching every call site.

```go
type Scope struct {
    Kind ScopeKind    // Global | Branch | Warehouse
    ID   id.ID        // empty for Global
}

type Authorizer interface {
    Can(ctx context.Context, permission string, scope Scope) bool
    Effective(ctx context.Context) PermissionSet   // for the UI (cosmetic only)
}
```

v1 seeds every grant at `global`. The resolution algorithm handles branch and warehouse scopes
correctly and is tested at all three, so Phase 2's branch-scoped roles are configuration.

### RBAC.3 Seeded roles

Administrator, Manager, Cashier, Accountant, Stock Keeper, Viewer (§14.1) — `is_system`, freely
editable, never deletable. They are seeded through the 0.5 metadata seeder, by `code`, so an
admin's edits survive upgrades.

**Administrator is not a superuser bypass.** It holds a `*` grant evaluated by the same
resolver as every other role. A hardcoded "if admin, allow" branch is how authorization bugs
become invisible — the code path everyone uses must be the code path that is tested.

### RBAC.4 Field-level restrictions — D4

§14.2(2): "can see cost price" and "can see profit margin" are permissions, not screens,
**enforced at the DTO-mapping layer so restricted fields are never serialized to the frontend
at all.** Hiding a field in the UI is not security; the value still crossed the boundary.

Phase 1 builds the mechanism in the binding façades (0.11) and proves it on a **real Phase-1
field**: `audit.entry.view` shows the audit list; `audit.entry.view_payload` is required for
`before_json`/`after_json`. Without the second permission the payload is **absent from the JSON
object**, not blanked.

That is a genuine consumer, not a fixture — audit payloads contain exactly the kind of detail
(old salary, old price, old credit limit) that a junior user should not read.

### RBAC.5 Approval limits are deferred — D5

§14.2(3) wants typed limits ("may discount up to 10%", "may void invoices under $500"), warning
that deferring means retrofitting an approval concept into every transactional use case.

**Recommendation: keep the seam, defer the table.** `Policy` gains a `WithLimit(...)` builder
that no policy uses yet; `role_limits` is **not** created.

The reasoning is 0.9 D1's, applied honestly: Phase 1 has no transactional use case, and the
shape of a limit — what is limited, in which currency, whose approval overrides it, whether
approval is a workflow or a supervisor PIN — will be decided by the first real one in Phase 5.
A table created now would very likely be migrated anyway. What §14.2 actually warns against is
losing the *concept*, and the concept survives in the policy builder and in this paragraph.

---

## POL — how "nothing ships unprotected" is enforced

This is the most important design decision in the phase, and the one I most want scrutinised.

### POL.1 The requirement

§14.3: *"A use case without a declared policy fails to start the application"* — a
compile-time-adjacent guarantee that nothing ships unprotected by accident.

The requirement is excellent. The mechanism is unspecified, and the obvious readings do not
work:

- **A use-case registry** every handler registers with. But a handler that is never registered
  is exactly the one that is unprotected, so the check misses the case it exists for.
- **An archlint rule.** "This struct has a `Handle` method and no `Policy` method" is
  expressible, but a policy that exists and is never *consulted* still passes.

### POL.2 The recommendation — D6

**Declare policies against the binding surface, and validate it at startup by reflection over
the static binding set.**

Step 0.11 made this possible: `bindings.Set` is a fixed, statically-declared list of structs
(D2), and every call from JavaScript enters through an exported method on one of them. That
surface **is** the untrusted boundary.

```go
// internal/api/policy
type Policy struct {
    Permission string
    Scope      ScopeKind
    // Public marks a method reachable without a session. Explicit, never a default.
    Public     bool
}

// Declared beside the binding it guards.
func (o *Ops) Policies() map[string]policy.Policy {
    return map[string]policy.Policy{
        "Jobs": policy.Requires("platform.jobs.view").InScope(policy.ScopeGlobal),
        "Runs": policy.Requires("platform.jobs.view").InScope(policy.ScopeGlobal),
    }
}
```

At startup the composition root reflects over every bound struct, enumerates its **exported
methods**, and asserts each has a policy entry. A method with none is a **fatal startup error
naming the struct and method**. Reflection is used once, at boot, over a set fixed at compile
time — not at call time, and not to discover behaviour.

Why this is the right seam:

- **It cannot be forgotten.** Adding a binding method without a policy fails on the developer's
  machine at first boot. Forgetting to register a handler is no longer a way to become invisible
  to the check, because the check enumerates the surface rather than a registry.
- **It is where enforcement must happen anyway** — §12's pipeline authorizes between the
  session check and the transaction.
- **It reuses a Phase 0 invariant** rather than inventing a parallel one.

**The honest limitation**, stated rather than discovered: this protects the *boundary*, not
every use case. A use case invoked from a background job, a subscriber, or another module is
not re-checked. That is the correct model — those callers are system-initiated or have already
crossed a checked boundary — but it means "a use case without a policy fails to start" becomes
**"a reachable-from-outside operation without a policy fails to start."** That is the guarantee
worth having, and it should be written down in those words rather than the looser original.

### POL.3 The enforcement decorator

§12's pipeline, made real for the first time:

```
binding method
  └─ recover (panic → Internal)
      └─ correlation id + locale        (0.10 appctx)
          └─ authenticate                (session valid? §IDN.2)
              └─ authorize               (policy + scope §RBAC.2)
                  └─ feature flag        (0.5)
                      └─ transaction     (0.3 UoW)
                          └─ HANDLER
                      ── commit ──
                  └─ audit               (§AUD — but see AUD.2)
```

Written once, applied to every binding, so a handler contains business orchestration and
nothing else.

---

## AUD — the audit trail

### AUD.1 Schema

Exactly §15.1, with the reserved chain columns from decision 8:

```sql
audit_log (
  id, occurred_at, actor_user_id, actor_name_snapshot,
  branch_id, session_id, correlation_id,
  action VARCHAR(80),                     -- 'sales.invoice.posted'
  entity_type, entity_id, entity_label_snapshot,
  before_json TEXT, after_json TEXT, changed_fields TEXT,
  source VARCHAR(10),                     -- ui | job | import | system
  device_info,
  prev_hash CHAR(64) NULL, row_hash CHAR(64) NULL,   -- reserved, NULL in v1 (§15.2)
  + created_at
);
```

Never updated, never deleted by application code. Label snapshots so a five-year-old entry
stays readable after a user is renamed. Sensitive fields redacted at write time by a field
policy (password hashes, tokens — §15.1).

### AUD.2 A contradiction in the approved architecture — D7

**§15.1 and §12 disagree about when the audit record is written**, and the difference matters.

- **§15.1**: *"written from event subscribers rather than sprinkled calls."*
- **§12**: the pipeline places `Audit` **after** `commit`.

Those are not the same design. After commit, a process that dies in the gap leaves a business
change with **no audit record** — and the audit trail's entire value is that it is complete.
"The invoice exists but its audit entry is still pending" is a sentence no one wants to say to
an auditor.

Three options:

| Option | Atomic? | Cost |
|---|---|---|
| **A. Domain bus, inside the transaction** | **Yes** | An audit failure aborts the business operation |
| B. Outbox, after commit | Intent is atomic; the row is eventual | At-least-once ⇒ needs dedupe; a window where the change is unaudited |
| C. Decorator after commit (§12 as written) | **No** | A crash in the gap loses the record permanently |

**Recommendation: Option A.** Modules publish a domain event carrying before/after; the audit
module subscribes on the **synchronous** bus (0.6), so the audit row commits in the same
transaction as the change. Nothing is ever unaudited, and there are no duplicates to reconcile.

The cost is real and I want it on the record: **if the audit write fails, the business operation
fails.** For a system whose §1.3 principle is "financial records are append-only" and whose
audit log is meant to be legally defensible, that is the correct direction — the same reasoning
that made 0.6 reject D3 in favour of correctness over convenience. A shop that cannot record
what it is doing should stop, not continue silently.

This amends §12's pipeline diagram. Flagged loudly because it changes an approved document.

**On the "domain events are within a module" rule (§23.1):** audit subscribing to other
modules' events looks like a violation. It is not, and the distinction is worth stating: audit
is **cross-cutting infrastructure**, like logging or metrics — it does not participate in
business decisions and never calls back into a module. Modules publish a typed `Auditable`
event; the audit module is its only subscriber. Greppable, typed, and no module knows audit
exists.

### AUD.3 What is audited

Per §15.3: all writes to financial and master data, all authentication events, all permission
and role changes, all settings changes, all backups/restores, all imports/exports, and all
report exports containing cost or profit data. In Phase 1 that means org changes, identity
changes, RBAC changes, settings changes, and login/logout/lockout.

---

## PROF — country and business profiles

### PROF.1 Country profiles

Addendum §C.1: `seeds/country_profiles/<iso2>.json` — locale, currencies, CoA template, tax
profile, fiscal year start, date/number formats, calendar, address format, rounding rule.
Everything is a **default**, editable after setup; adding a country is dropping in a file.

`sy.json` is the development seed. Per §C.3 **no Syrian tax rate or chart of accounts is
encoded** — those are jurisdictional facts to be supplied by the customer or their accountant
before Phase 2, or shipped as an empty, user-configured tax profile.

### PROF.2 Business profiles

§16.4: a named bundle of settings + feature flags + seed data (`furniture.json`,
`pharmacy.json`, …). Selecting one at setup **writes company-scope setting rows** — it is a
seeding operation, not a resolution tier (0.5 §3.3 settled this), which is what keeps every
value individually editable afterwards.

```sql
business_profiles (id, code, name, description, is_system, is_active, + universal columns);
```

### PROF.3 Seed-file discovery — closing a Phase 0 item

0.5 D7 deferred "JSON seed-**file** discovery and ordering across modules" to the first real
seed; 0.9 seeded from Go literals instead, carrying it forward again. Country and business
profiles are genuinely file-shaped, editable by a non-programmer, and numerous — this is that
first real consumer. The loader reads a module's embedded `seeds/` FS, orders deterministically,
and applies through the existing idempotent `code`-keyed seeder.

---

## WIZ — first-run setup

### WIZ.1 The bootstrap paradox — D8

No user exists, so nobody can authenticate; but creating the first administrator requires a
call, and every call requires authorization. Something must give.

**Recommendation: setup bindings are `Public`, and are callable only while no company exists.**

```
Setup.Status()    → { required: bool, step: string }
Setup.Apply(...)  → creates company, branch, warehouse, fiscal year, admin, roles, seeds
```

Once a company row exists, every `Setup.*` method returns a typed error **forever**. The
invariant is a single row-count check — trivially testable, impossible to leave half-open, and
it does not require a magic bootstrap principal that would then exist for the life of the
system. `Setup.Apply` runs in **one transaction**: a half-configured company is worse than an
unconfigured one.

The first administrator is created with `must_change = 0` and a password the user typed. **No
default password ships** (§13.1).

### WIZ.2 Flow

Addendum §C.2's ten steps, unchanged: language → country → company → currency → business
profile → fiscal setup → branch & warehouse → tax on/off → administrator → optional starting
data (deferred to Phase 9; the step shows "start empty").

Every step is revisitable in Settings afterwards. The wizard writes settings and seeds and
contains no logic that cannot be reproduced by editing configuration later — otherwise it
becomes a hidden source of truth, which §C.2 explicitly warns against.

---

## FE — frontend

### FE.1 The gate above the shell

0.11's `BootGate` decides between boot / failure / shell. Phase 1 inserts a second gate:

```
BootGate (graph ready?)
  └─ SetupGate   (company exists?)   → wizard
      └─ AuthGate (session valid?)   → login
          └─ AppShell
```

Each gate mounts nothing below it until satisfied — the same discipline that keeps the shell off
a mid-restore database.

### FE.2 The deferred dependencies come due — D9

0.11 D1 deferred TanStack Query, Zustand, and a router *"to Phase 1, where a real list screen
and real routes create the need."* That need is now real: user lists, role editors, an audit log
viewer, and several routes.

**Recommendation: adopt all three now.** The deferral did its job — they arrive with consumers
rather than as speculation.

- **TanStack Query** over the 0.11 bindings wrapper: `BindingError` maps to its error channel,
  and the audit log is the first genuinely paginated read.
- **Zustand** for session and current-branch state.
- **react-router** for routes and route-level code splitting (§31).

### FE.3 Permission-aware UI

The frontend receives the effective permission set and hides unavailable actions — **cosmetic
only; the backend is the sole enforcement point** (§14.3). The `EmptyState` `denied` variant
built in 0.11 D6 finally gets its producer: a `BindingError` with a `Permission` category
renders it.

### FE.4 Screens

Login (with lockout feedback), the setup wizard, company/branch/warehouse settings, users,
roles and grants, sessions, and the audit log viewer with filters. Built from the 0.11
primitives; **no new primitive should be needed** — if one is, that is a finding worth
recording, because it means the Phase 0 set was chosen from imagination rather than need.

---

## DEBT — Phase 0 items this phase closes

Tracked explicitly so none is quietly carried a third time.

| Item | Origin | Closed by |
|---|---|---|
| `Module.Permissions()` joins the contract | 0.9 D5 | §RBAC.1 |
| `config.Authorizer` real implementation | 0.5 D8 | §RBAC.2 |
| `appctx.Scopes` reads a real session | 0.10 §6.2 | §IDN.2 |
| Settings write at **user** scope | 0.11 D4 | §IDN.2 (a user finally exists) |
| `Module.Bindings()` reconciled with static façades | 0.11 D8 | §POL.2 |
| Permission-denied `EmptyState` gets a producer | 0.11 D6 | §FE.3 |
| TanStack Query / Zustand / router | 0.11 D1 | §FE.2 |
| JSON seed-**file** discovery | 0.5 D7, 0.9 | §PROF.3 |
| **archlint `module-isolation`** | 0.12 §5 | **Step 1.1 — see below** |
| archlint rule 7 restated ("no SQL in domain/app/api") | 0.12 §5 | Step 1.1 |
| Visual confirmation of the shell | 0.12 | Before Step 1.8 |

**`module-isolation` lands in Step 1.1, not later.** Phase 1 adds three modules to a codebase
that has had one. It is the rule that stops modules quietly fusing, and the cheapest moment to
turn it on is *before* the imports it forbids have been written.

---

## TEST — testing strategy

Inheriting the Phase 0 harness (§30, §TEST) and its drills. The Phase-1-specific work:

- **Security tests are first-class**, not a category:
  - a wrong password and an unknown username are **indistinguishable** in response and timing
    class;
  - lockout triggers, backs off, and records every attempt;
  - a session past idle expiry and one past absolute expiry are both rejected;
  - a revoked session stops working immediately;
  - **a binding with no declared policy fails startup** (the §POL.2 drill);
  - a permission removed from a role takes effect on the next call, not the next login.
- **Scope resolution table test** across global / branch / warehouse × granted / not granted,
  the same shape as 0.5's precedence table.
- **Field-level redaction**: a user without `view_payload` receives a JSON object with the key
  **absent**, asserted on the marshalled bytes — not on a Go struct field.
- **Audit atomicity drill**: a business change whose audit write fails **rolls back the
  business change** (the D7 guarantee), and a rolled-back change leaves no audit row.
- **Setup idempotency**: `Setup.Apply` twice is rejected; a failed apply leaves **no** partial
  company.
- **Mutation-verified**, per the pattern established in 0.4–0.11: the policy-coverage check
  (delete one policy entry → startup must fail) and audit atomicity (write the audit row after
  commit → the drill must fail).

---

## DOD — Definition of Done for Phase 1

1. A fresh install launches into the **setup wizard**, and completing it produces a company,
   branch, warehouse, fiscal year, seeded roles, and an administrator — in one transaction.
2. That administrator can **log in**; a wrong password is indistinguishable from an unknown
   user; repeated failures lock the account and every attempt is recorded.
3. A session expires on **both** idle and absolute timeout, survives a restart when "stay
   signed in" is set, and can be revoked by an administrator.
4. **Every binding method has a declared policy**, and removing one makes the application fail
   to start with a message naming the method.
5. A role without a permission is **actually blocked at the backend**, not merely hidden in the
   UI; the denial renders as a translated permission-denied state.
6. Scope resolution is correct at **global, branch, and warehouse**, with tests, even though
   v1 seeds only global grants.
7. A restricted field is **absent from the serialized JSON** for a user lacking its permission.
8. Every audited action writes an `audit_log` row **in the same transaction**, with actor,
   correlation id, and label snapshots; a failed audit write rolls the change back.
9. Country and business profiles apply from **seed files**, and every value they set remains
   individually editable afterwards.
10. `module-isolation` is enforced and green; no module imports another module's non-`contract`
    package.
11. Settings resolve at **user** scope for a signed-in user (closing 0.11's system-scope
    limitation), and locale/theme follow the user.
12. The Phase 0 carried-forward list in §DEBT is empty.

---

## SEQ — step sequence

Ordered by dependency; each is separately designed, reviewed, and approved.

| Step | Deliverable |
|---|---|
| **1.1** | `module-isolation` + restated SQL rule; `modules/org` schema, domain, seeding of the default company/branch/warehouse |
| **1.2** | Identity: credentials (Argon2id), `Authenticator` port, users, password policy |
| **1.3** | Sessions, lockout, login attempts; `appctx` reads a real session |
| **1.4** | RBAC: permissions sync via `Module.Permissions()`, roles, grants, scope resolution, `config.Authorizer` |
| **1.5** | The policy mechanism (§POL) + the enforcement decorator + startup coverage check |
| **1.6** | Field-level redaction, proven on audit payloads |
| **1.7** | Audit module: schema, `Auditable` events, subscribers, redaction policy, atomicity drill |
| **1.8** | Country + business profiles, seed-file discovery, fiscal year generation |
| **1.9** | Setup wizard: backend `Setup.*` + the bootstrap-paradox invariant |
| **1.10** | Frontend: router, TanStack Query, Zustand, Setup/Auth gates, login, wizard UI |
| **1.11** | Frontend: users, roles, sessions, audit viewer; permission-aware navigation |
| **1.12** | Phase 1 Definition-of-Done review |

---

## DEC — decisions requested

1. **D1 — `golang.org/x/crypto/argon2` for password hashing**, confirming module-cache presence
   before Step 1.2 and treating a fetch as acceptable if absent (offline means *builds and runs*
   offline, per the 0.11 precedent). Hand-writing Argon2id is not on the table. *(§IDN.1.)*
2. **D2 — PIN login deferred to Phase 5**, with `credential_type` reserved now. *(§IDN.5.)*
3. **D3 — `Can(ctx, permission, scope)` from the first implementation**, with all three scope
   kinds resolved and tested though v1 seeds only global grants. *(§RBAC.2.)*
4. **D4 — Field-level restrictions enforced at DTO mapping**, proven on audit payloads, with
   restricted keys **absent** from the JSON rather than blanked. *(§RBAC.4.)*
5. **D5 — Approval limits: keep the policy seam, defer the `role_limits` table** to Phase 5,
   where the first real limit will decide its shape. *(§RBAC.5.)*
6. **D6 — Policies are declared per binding method and validated at startup by reflection over
   the static binding set**; a method without one is a fatal startup error. The guarantee is
   restated honestly as *"a reachable-from-outside operation without a policy fails to start."*
   *(Recommended; §POL.2 — **the decision I most want scrutinised**.)*
7. **D7 — Audit is written inside the business transaction via the synchronous domain bus**,
   amending §12's after-commit pipeline. A failed audit write aborts the operation.
   *(Recommended; §AUD.2 — **this amends an approved document and I want it confirmed
   explicitly**.)*
8. **D8 — Setup bindings are public but callable only while no company exists**, and
   `Setup.Apply` runs in one transaction. *(§WIZ.1.)*
9. **D9 — Adopt TanStack Query, Zustand, and react-router now**, their 0.11 deferral having
   done its job. *(§FE.2.)*
10. **D10 — `fiscal_years`/`fiscal_periods` are created in Phase 1** though unused until
    Phase 2, per the 0.4 D4 precedent. *(§ORG.2.)*

### Open questions (non-blocking, needed within Phase 1)

| # | Question | Needed by |
|---|---|---|
| 1 | **Dual calendar display** (Gregorian + Hijri) — required for the first customer, or Gregorian only? §33.1(5) marks this as needed by Phase 1 Settings. Storage is Gregorian either way (§G.4); this decides whether a secondary display is built now. | Step 1.8 |
| 2 | **Password policy defaults** — minimum length, complexity, expiry, reuse history. I will propose conservative defaults (12 characters, no forced rotation, 5-password history) unless you have a requirement. | Step 1.2 |
| 3 | **Session timeouts** — proposed 30 minutes idle, 12 hours absolute, "stay signed in" off by default. A shop floor may want something very different. | Step 1.3 |
| 4 | **Which business profiles ship first?** Furniture is the known case; each is a seed file, so more can follow at any time. | Step 1.8 |

---

*End of Phase 1 detailed design. Awaiting your approval of this document — and specifically of
D6 and D7 — before the Step 1.1 design. Per the agreed process: design → review → approve →
implement → review → refactor, step by step.*
