# Step 1.4 — RBAC: Permissions, Roles & Scope (Design + implementation record)

> Status: **DESIGN — implemented in the same turn under the reviewer's standing approval.**
> Scope: migration `0007`, `Module.Permissions()` joining the contract, the permission sync,
> roles and grants, scope resolution, and the real `config.Authorizer`.
> **Out of scope:** the policy declaration and enforcement decorator (1.5) — this step builds
> the *answer* to "may they?", not the machinery that *asks*; field-level redaction (1.6);
> role management screens (1.11).

---

## 1. ANALYSIS

### 1.1 What this step is and is not

It is easy to conflate two things §14 keeps separate:

- **Can this user do X here?** — a data question about roles and grants. **This step.**
- **Was anyone asked before the operation ran?** — a structural question about every reachable
  entry point. **Step 1.5.**

Building them together would produce an authorizer with one caller and no guarantee that
anything else consults it. Separating them means 1.5's startup check has a working `Can` to
wire, and this step can be judged on whether its answers are right.

### 1.2 The three obvious mistakes

| Mistake | Why it is fatal | Answer |
|---|---|---|
| Deleting a permission the code no longer declares | The grants referencing it vanish silently, and a downgrade hands users rights they should not have | Mark `is_obsolete`, never delete (§3.1) |
| A superuser bypass — `if isAdmin { return true }` | The path everyone uses is not the path that is tested, so authorization bugs become invisible | Administrator holds a `*` grant, resolved by the same code as every other role (§4.3) |
| Caching the permission set | Revoking a role becomes a lie until the cache expires | No cache; one indexed read per check (§4.5) |

### 1.3 Risks

| Risk | Consequence | Design answer |
|---|---|---|
| Two modules declare one permission code | Ambiguous ownership; last-one-wins | Fatal at startup — a code defect (0.10 D3) |
| A grant referencing a permission that no longer exists | Silent loss of access, or silent extra access | Grants reference `permission_code`; obsolete permissions still resolve, and are reported |
| Scope silently ignored | A branch manager voids invoices company-wide | `Can` takes a scope from the first implementation (§4.2) |
| Wildcards accepted on the *check* side | `Can(ctx, "sales.*")` would grant everything | Wildcards are legal only in a **grant** (§4.4) |

---

## 2. DESIGN — `Module.Permissions()` joins the contract

Closes the item 0.9 D5 deferred:

```go
// platform/modules
Permissions() []auth.PermissionDef
```

`auth` here is `internal/modules/identity/contract`, which already holds `Principal` and
`Authenticator` — the package other modules may import. A module declares what it protects:

```go
func (m *Module) Permissions() []contract.PermissionDef {
    return []contract.PermissionDef{
        {Code: "org.company.edit",  Description: "permissions.org.company.edit"},
        {Code: "org.branch.manage", Description: "permissions.org.branch.manage"},
    }
}
```

Currency and org declare theirs; identity declares the user/role/session ones. `Description` is
an i18n key, never prose (§22.2).

---

## 3. DESIGN — schema `0007_rbac.sql`

```sql
permissions (
  id, code VARCHAR(80) NOT NULL, module VARCHAR(40) NOT NULL,
  description VARCHAR(200),                    -- i18n key
  is_obsolete SMALLINT NOT NULL DEFAULT 0,
  + universal, CONSTRAINT ux_permissions_code UNIQUE (code)
);

roles (
  id, company_id NOT NULL REFERENCES companies(id),
  code VARCHAR(40) NOT NULL, name VARCHAR(120) NOT NULL, description VARCHAR(200),
  is_system SMALLINT NOT NULL DEFAULT 0,       -- editable, never deletable
  is_active SMALLINT NOT NULL DEFAULT 1,
  + universal, CONSTRAINT ux_roles_code UNIQUE (company_id, code)
);

role_permissions (
  id, role_id NOT NULL REFERENCES roles(id),
  permission_code VARCHAR(80) NOT NULL,        -- by CODE, not id (§CFG.3)
  + created_at, CONSTRAINT ux_role_permissions UNIQUE (role_id, permission_code)
);

user_roles (
  id, user_id NOT NULL REFERENCES users(id), role_id NOT NULL REFERENCES roles(id),
  scope_kind VARCHAR(10),                      -- NULL = global; 'branch' | 'warehouse'
  scope_id CHAR(36),                           -- NULL when global
  + created_at, CONSTRAINT ux_user_roles UNIQUE (user_id, role_id, scope_kind, scope_id)
);
```

### 3.1 Permissions are marked obsolete, never deleted

A permission the running build no longer declares is a **data leftover**, not a code defect
(0.10 D3): the row is inert once nothing checks it, and deleting it would cascade to
`role_permissions` — silently changing what a role grants. `is_obsolete = 1` keeps the history
and lets a downgrade-then-upgrade restore the permission with its grants intact.

### 3.2 Grants reference the code

`role_permissions.permission_code`, not `permission_id`, so code can depend on
`"sales.invoice.void"` existing without hardcoding a UUID — the §CFG.3 rule that makes seeded
reference data usable from code. It also means a grant survives the permission being
re-synced.

---

## 4. DESIGN — resolution

### 4.1 The surface

```go
// contract — usable by other modules
type Scope struct {
    Kind ScopeKind // Global | Branch | Warehouse
    ID   id.ID     // empty for Global
}

type Authorizer interface {
    Can(ctx context.Context, permission string, scope Scope) bool
    Effective(ctx context.Context) ([]string, error)   // for the UI; cosmetic only
}
```

### 4.2 Scope from the first implementation — §14.2

`Can` takes a scope even though **v1 seeds every grant at global**. §14.2 is right that the
signature must exist from day one: a branch manager who may void invoices *in their branch* is
not expressible later without touching every call site.

Satisfaction rules:

| Assignment | Satisfies |
|---|---|
| Global | any scope |
| Branch B | `Scope{Branch, B}` only |
| Warehouse W | `Scope{Warehouse, W}` only |

**Branch ⊅ Warehouse, deliberately.** A grant at branch B does *not* currently satisfy a check
on a warehouse inside B. That containment is the natural reading and will very likely be
wanted — but deciding it needs a warehouse→branch lookup on every check, and **no
warehouse-scoped permission exists until Phase 4 (inventory)**. Designing the rule now means
designing it against an imagined consumer; the same call 0.9 D1 and 1.1 D5 made. A test pins
the current behaviour so a future change is deliberate rather than accidental.

### 4.3 No superuser bypass

Administrator is seeded holding the `*` grant, and `*` is matched by the ordinary resolver.
There is no `if role == "administrator" { return true }` anywhere.

The reason is not purity: a bypass means the code path every real user exercises is **not** the
code path the administrator exercises, so a bug in resolution stays invisible to whoever tests
as an admin — which is everyone, during development.

### 4.4 Wildcards are a grant-side concept

`*` and `sales.*` are legal in `role_permissions`; a **check** must always name a concrete
permission. `Can(ctx, "sales.*", …)` returning true would let a careless call site grant
everything, so wildcard input to `Can` is refused.

### 4.5 No permission cache

Same reasoning as sessions (1.3): revoking a role must take effect on the next call, and a
cache makes revocation a lie for as long as an entry lives. One indexed query joins
`user_roles → role_permissions`. A cache is a decorator behind the same interface if a
measurement ever justifies one — the 0.9 §4.5 pattern.

---

## 5. DESIGN — the permission sync

At startup, after modules are constructed:

1. Collect every declared permission. **A duplicate code across two modules is fatal** — a code
   defect that must fail on the developer's machine (0.10 D3), reported naming both modules.
2. Insert codes that are not in the table; clear `is_obsolete` on any that reappear.
3. Mark rows that no module declares `is_obsolete = 1` and **report the count**.

Step 3 is reported, not fatal: an inert row must never stop a shop opening.

---

## 6. DESIGN — the `config.Authorizer` stub, replaced

0.5 D8 declared a port and shipped `AllowAll`:

```go
type Authorizer interface{ Can(ctx context.Context, permission string) bool }
```

Note it has **no scope** — deliberately, because a setting is a company-wide fact and there is
no meaningful branch-scoped "may change this setting". So the port stays one-argument, and the
composition root supplies a thin adapter that calls the RBAC authorizer at **global** scope.

Two interfaces rather than one, because they answer different questions and forcing settings to
carry a scope it does not have would be shape without meaning.

---

## 7. TESTING PLAN

| Level | Tests |
|---|---|
| **Sync** | New permissions inserted; a second sync is a no-op; a permission no module declares is marked obsolete and **not deleted**, with its grants intact; a reappearing permission is un-obsoleted; a duplicate code across modules is **fatal**. |
| **Resolution** | A granted permission is allowed; an ungranted one refused; an inactive role grants nothing; a deactivated user is refused everything. |
| **Scope table** | Global/Branch/Warehouse × granted-at-each, asserting the §4.2 table exactly — including that branch does **not** satisfy warehouse. |
| **Wildcards** | `*` grants everything; `sales.*` grants `sales.invoice.void` but not `org.company.edit`; a wildcard **passed to `Can`** is refused. |
| **No bypass** | Administrator's access comes from its `*` grant: removing that grant removes its access. |
| **Revocation** | Removing a role takes effect on the very next `Can` — no cache. |
| **Settings adapter** | A permissioned setting is refused without the permission and allowed with it. |

**Mutation-verified**: *obsolete-not-delete* (delete instead of mark → the grants-intact test
must fail) and *scope enforcement* (ignore the scope argument → the scope table must fail).

---

## 8. DECISIONS TAKEN

1. **D1 — `Module.Permissions()` joins the contract**, returning `contract.PermissionDef`.
   Closes 0.9 D5. *(§2.)*
2. **D2 — Obsolete, never delete.** A permission the build no longer declares is a data
   leftover; deleting it would silently change what roles grant. *(§3.1.)*
3. **D3 — `Can` takes a scope from the first implementation**, with Global ⊃ everything and
   exact match otherwise. **Branch ⊅ Warehouse is deferred** to Phase 4, where the first
   warehouse-scoped permission will define it. *(§4.2.)*
4. **D4 — No superuser bypass.** Administrator holds `*`, resolved by the ordinary path.
   *(§4.3.)*
5. **D5 — Wildcards are grant-side only**; a wildcard passed to `Can` is refused. *(§4.4.)*
6. **D6 — No permission cache**, for the same reason sessions have none. *(§4.5.)*
7. **D7 — `config.Authorizer` stays scope-free**, with an adapter calling RBAC at global scope.
   *(§6.)*

---

## 9. IMPLEMENTATION RECORD (Protocol Steps 3–6)

### 9.1 A mutation drill caught my own reasoning, not just my code

D2 justified "mark obsolete, never delete" as *"deleting would cascade to `role_permissions`
and silently change what every role grants."*

**That is wrong.** Grants reference `permission_code` as a plain string (§CFG.3, deliberately),
so there is no foreign key and nothing cascades. The mutation drill proved it by **passing**
with `DELETE` substituted for the mark — the grants survived exactly as before.

So the test was asserting a property that held for the wrong reason, and the design was
defending against a hazard that does not exist. Both are corrected:

The real reason is **visibility**. A role may still grant a permission the software no longer
implements. The grant survives either way — but only an obsolete *row* lets an administrator
discover that "Manager can revoke sessions" now refers to nothing. A deleted row is invisible,
and it also discards the module attribution and description a settings screen needs to explain
the grant.

The test now asserts the permission row is still present and flagged, and the drill fails
correctly:

```
the undeclared permission was DELETED (0 rows) — a role may still grant it,
and now nobody can find out
```

Worth recording plainly: the drill did its job by refusing to fail. A mutation test that passes
is telling you the test is wrong, and it is only useful if that is treated as a finding rather
than a nuisance.

### 9.2 The archlint rule from 0.5 forced a better design

The first cut put `PermissionDef`, `Scope`, and `Authorizer` in
`internal/modules/identity/contract`, and had `platform/modules` import it so the `Module`
interface could declare `Permissions()`. `make arch` refused it:

```
[import-boundary/platform-independent-of-modules] platform must not import
business modules or the API layer
```

The fix is not a workaround, it is the right architecture surfacing under pressure: **the
authorization types belong to platform, the implementation to identity.** They now live in
`internal/platform/auth`, so a future sales module asks `auth.Authorizer` whether it may void an
invoice and never imports identity — an authorization check should not be a dependency on one
particular implementation of authorization.

Second time a rule added in an earlier step has caught a design error rather than a typo.

### 9.3 Deferring the branch⊃warehouse rule, and pinning it

A grant at branch B does not satisfy a check on a warehouse inside B. That containment is the
natural reading and will very likely be wanted — but it needs a warehouse→branch lookup on
every check, and **no warehouse-scoped permission exists until Phase 4**. Designing it now means
designing against an imagined consumer (0.9 D1, 1.1 D5).

`TestScopeSatisfaction` asserts the current behaviour as a table, including the deferred row,
so changing it later is a deliberate edit to a stated expectation rather than a silent
loosening.

### 9.4 Two naming collisions worth the rename

`Service.Revoke` was declared twice — sessions and grants. Renamed to `RevokeFromRole` /
`GrantToRole`: sessions are revoked too, and a call site reading `svc.Revoke(x, y)` should not
have to guess which. The compiler caught it; the naming was the real problem.

### 9.5 Construction order moved, for a stated reason

`config.Open` takes the `Authorizer` that gates permissioned settings, and the real one is the
identity service — which §BOOT places two steps later. Org and identity are now constructed
**before** settings, because neither reads a setting at construction time (only at call time,
through handles).

The alternative was handing config a mutable holder to fill in later, which is exactly the
late-bound state §6 warns against. The modules themselves are still assembled at step 10.

### 9.6 Verification

**`make ci` green.** 20 new Go tests. Three new error codes, translated in both locales.

**Mutation-verified**:

- *Obsolete-not-delete* — after correcting the test (§9.1), substituting `DELETE` fails it.
- *Scope enforcement* — making `Satisfies` return true unconditionally fails the scope table on
  three rows and the end-to-end scoped-grant test.

### 9.7 Carried forward

- **Nothing calls `Can` in production yet.** The decorator that consults it on every binding is
  Step 1.5 — which is the whole point of the split in §1.1: this step built the answer, 1.5
  builds the guarantee that anyone asks.
- **Cashier and Stock Keeper are seeded with no grants**, deliberately: their permissions are
  defined by modules that do not exist (Phase 4, Phase 5). Seeding imagined codes would seed
  permissions nothing declares, which the sync would immediately mark obsolete.
- **`Effective` returns wildcards unexpanded** — the frontend only needs "should this menu item
  be visible", and expanding would mean listing every permission in the system.
- **Role management screens** are 1.11; **`SeedRoles` is called by the wizard** in 1.9.

---

*End of Step 1.4. Implemented and self-reviewed.*
