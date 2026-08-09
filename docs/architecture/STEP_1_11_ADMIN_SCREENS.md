# Step 1.11 — Administration: users, roles, sessions, and the audit viewer (Design + implementation record)

> Status: **IMPLEMENTED.** `make ci` green (152 frontend tests); both mutation drills confirmed
> (§7.3), and the second changed a test.
> Scope: the `Identity` binding and the service methods it needs; routes and permission-aware
> navigation; the users, roles, sessions, audit, and change-password screens.
> **Out of scope:** company/branch/warehouse settings screens (they edit org data that only the
> wizard writes today — Phase 4 adds multi-branch management); `role_limits` (Phase 5).

---

## 1. ANALYSIS

### 1.1 What this step closes

Phase 1 declared six identity permissions in Step 1.4 and **nothing has ever checked five of
them**. `identity.user.view` gates a few unrelated bindings by convenience; `user.manage`,
`role.view`, `role.manage`, `session.view`, and `session.revoke` have no consumer at all.

This is the step where they get one — which, by this project's repeated experience, is where
they turn out to be subtly wrong if they are.

It also closes the `mustChange` dead end 1.10 (D6) left: there is no way for anyone to change
their own password on either side of the boundary.

### 1.2 A defect found while designing this — D1

`identity.SetPassword` sets `must_change = false`, unconditionally.

That is correct for a user changing their own password and **wrong for an administrator
resetting someone else's** — which is the only way it would ever be called from an
administration screen. The result: an administrator resets a password, tells the person what it
is, and the flag that exists to force them to replace a credential another human knows *stays
off*. `must_change` would be dead code that looks alive.

The method has one name and two meanings. It splits:

| Operation | Requires | `must_change` after |
|---|---|---|
| `ChangeOwnPassword(userID, current, next)` | the **current** password | cleared |
| `ResetPassword(userID, next)` | `identity.user.manage` | **set** |

### 1.3 Changing your own password requires the current one — D2

The session already proves who you are, so the check looks redundant. It is not: the threat is
an **unattended terminal**, which is the normal state of a shop counter. A session left open is
not consent to change the credential that outlives it.

It also stops a stolen session from being converted into permanent access, which is the whole
point of a session being revocable.

### 1.4 Two ways to brick an installation — D6

Both are reachable from these screens, and neither is prevented today:

1. **Deactivating yourself.** `Validate` ends a session whose user is inactive, so the click
   signs you out and — if you were the last administrator — locks everyone out.
2. **Removing the last administrator's role.** The roles screen makes this two clicks.

`CanDeactivate` already refuses the last *active user* (1.2). Neither of these is that rule:
you can be the last administrator among five active users.

Both are refused in the **service**, not the screen. A UI that hides the button is not the
guarantee; §14.3 puts enforcement on the backend, and an importer or a future script must be
refused too.

### 1.5 Risks

| Risk | Consequence | Answer |
|---|---|---|
| An admin reset leaves `must_change` off | The flag is dead code | Split into two named operations (D1) |
| Self-service change with no current password | An unattended terminal becomes permanent access | Current password required (D2) |
| Locking every user out | The shop cannot open, and no screen can repair it | Refused in the service (D6) |
| The role editor invents its permission list | Codes drift from what modules declare | Read from the synced catalogue (D5) |
| Audit payloads shown to someone without the permission | 1.6's redaction defeated at the last step | The DTO's fields are already absent; the UI renders "withheld" (D7) |
| A hidden nav item read as security | A backend check removed later | Every such component documents that it is cosmetic (1.10 D2) |

---

## 2. DESIGN — the service methods

```go
func (s *Service) ChangeOwnPassword(ctx, userID id.ID, current, next string) error
func (s *Service) ResetPassword(ctx, userID id.ID, next string) error
func (s *Service) UserRoles(ctx, userID id.ID) ([]sqlite.Role, error)
func (s *Service) AllActiveSessions(ctx, companyID id.ID) ([]SessionView, error)
func (s *Service) PermissionCatalogue(ctx) ([]PermissionView, error)
```

`SetPassword` is **removed**, not deprecated. Leaving it would leave the wrong default one call
site away, and the whole point of D1 is that the two meanings must be impossible to confuse.

### 2.1 `AllActiveSessions` returns a view, not a domain type

The sessions screen shows *who* — the username beside the device. `domain.Session` carries a
`UserID` and the screen would have to resolve names itself, one query per row. `SessionView`
joins once.

### 2.2 The permission catalogue is read from the database

Not from `declaredPermissions()`. The rows are what the startup sync wrote, so the editor shows
exactly what the running build declares — including anything marked obsolete, which is
information an administrator needs when a grant stops working after an upgrade.

---

## 3. DESIGN — the `Identity` binding

| Method | Policy |
|---|---|
| `Users`, `Roles`, `RoleGrants`, `UserRoles`, `Permissions` | `identity.user.view` / `identity.role.view` |
| `CreateUser`, `SetUserActive`, `ResetPassword` | `identity.user.manage` |
| `GrantToRole`, `RevokeFromRole`, `AssignRole`, `UnassignRole` | `identity.role.manage` |
| `Sessions` | `identity.session.view` |
| `RevokeSession` | `identity.session.revoke` |
| `ChangeMyPassword` | **`policy.Public()`** |

### 3.1 Why `ChangeMyPassword` is Public — D3

It is the one method a user with `must_change` set must be able to call, and that user may hold
no permission at all: `must_change` is typically set on an account created seconds earlier with
no roles assigned yet.

Public here means *"no permission required"*, not *"no session required"*. The method reads the
acting user from the context — it takes **no user id parameter**, so there is no argument to
tamper with, and a caller with no session has no actor and is refused. That is the same shape as
`Auth.Me`, and the pin test in 1.5 records the widened surface deliberately.

---

## 4. DESIGN — the screens

```
/                 → dashboard (the 0.11 system panel, kept)
/admin/users      → identity.user.view
/admin/roles      → identity.role.view
/admin/sessions   → identity.session.view
/admin/audit      → audit.entry.view
/account/password → always
```

The first real routes. 1.10 mounted the router with none, deliberately — inventing routes before
screens would have meant deleting them.

### 4.1 Permission-aware navigation — §FE.3

Nav items are filtered by the effective permission set, and each route additionally renders a
`denied` `EmptyState` if reached without the permission. Both are **cosmetic**: every binding
re-checks, and the guard is the only path to the object graph (1.5 D1).

The `EmptyState` `denied` variant built in 0.11 D6 finally gets its producer.

### 4.2 The audit viewer and redaction — D7

`AuditEntryDTO`'s payload fields are **absent** rather than blank for a caller without
`audit.entry.view_payload` (1.6). The viewer must therefore render three distinct states:

| State | Meaning | Shown as |
|---|---|---|
| key absent | you may not see this | "Withheld" |
| key present, empty | there was no before-state | "—" |
| key present, JSON | the payload | the JSON |

Collapsing the first two would undo the entire point of 1.6: a blank cell cannot be told from a
forbidden one, so nobody ever asks for the permission they are missing.

### 4.3 The users screen and the two bricking rules

Deactivate and "remove administrator" are offered, and refused by the backend with typed codes
the screen renders. The button is **not** hidden — a disabled control with a reason teaches the
rule; a missing one teaches nothing.

---

## 5. TESTING PLAN

| Level | Tests |
|---|---|
| **D1** | An admin reset SETS `must_change`; a self-service change CLEARS it |
| **D2** | The wrong current password is refused; the right one succeeds |
| **D6** | You cannot deactivate yourself; you cannot remove the last administrator |
| **Policies** | Every new method is covered by the 1.5 startup check; `ChangeMyPassword` is pinned as public |
| **Binding** | Each method refuses a caller without its permission |
| **Redaction** | The viewer distinguishes withheld / empty / present |
| **Navigation** | Items are filtered by permission; a denied route renders the `denied` state |
| **Screens** | Users list and create; grants toggle; a session revokes |

**Mutation-verified**: (1) make `ResetPassword` clear `must_change` → the D1 test must fail;
(2) drop the self-deactivation check → the bricking test must fail.

---

## 6. DECISIONS TAKEN

1. **D1 — `SetPassword` splits into `ChangeOwnPassword` and `ResetPassword`**, with opposite
   `must_change` outcomes. The old method is removed, not deprecated. *(§1.2.)*
2. **D2 — Self-service change requires the current password.** An unattended terminal is not
   consent. *(§1.3.)*
3. **D3 — `ChangeMyPassword` is `Public`** (no permission, session still required), because the
   user who most needs it may hold no permission at all. It takes no user id. *(§3.1.)*
4. **D4 — Sessions are listed through a joined view**, so the screen shows who without a query
   per row. *(§2.1.)*
5. **D5 — The role editor reads the synced permission catalogue from the database**, obsolete
   entries included. *(§2.2.)*
6. **D6 — Self-deactivation and removing the last administrator are refused in the service.**
   Both brick an installation, and neither is the existing last-active-user rule. *(§1.4.)*
7. **D7 — The audit viewer renders withheld, empty, and present as three states.** Collapsing
   the first two would undo 1.6. *(§4.2.)*

---

## 7. IMPLEMENTATION RECORD

### 7.1 What was built

| File | What |
|---|---|
| `modules/identity/service.go` | `ChangeOwnPassword`, `ResetPassword`, `writePassword`, the `ActingUser` port, the self-deactivation and last-administrator guards |
| `modules/identity/rbac.go` | `PermissionCatalogue`, `UserRoles`, `CountAdministrators`, the guard in `UnassignRole` |
| `modules/identity/sessions.go` | `SessionView`, `AllActiveSessions` |
| `api/bindings/identity.go` | The fifteen-method administration façade |
| `api/bindings/guard.go` | The Public branch now stamps an actor when a session exists (§7.2) |
| `bootstrap/actors.go` | The `identityActors` adapter |
| `app/session/Can.tsx` | `Can` and `RequirePermission`, both documented as cosmetic |
| `app/shell/{routes,AppShell,SystemPanel}.tsx` | The first real routes, and navigation generated from them |
| `modules/admin/*`, `modules/account/*` | Users, roles, sessions, audit, change-password |

### 7.2 Two defects found, both by first use

**`SetPassword` had one name and two meanings.** It cleared `must_change` unconditionally,
which is right for a self-service change and wrong for the administrator reset that is the only
way an administration screen would ever call it. An administrator would hand someone a password
and the flag meant to force its replacement would never fire — `must_change` was effectively
dead code that looked alive. Split into two named operations; the old one removed outright.

**`policy.Public()` skipped too much.** The guard's public branch returned immediately, without
resolving the session or stamping an actor. That was invisible for three steps because its only
users were `Auth.*` (which handle tokens themselves) and `Setup.*` (which have no actor).
`ChangeMyPassword` is the first public method that needs to know *who is calling*, and it got an
empty context.

Public means "no **permission** required". It never meant "pretend nobody is here". The branch
now resolves a presented session and stamps the actor, ignoring failures — `Auth.Logout` must
still work with a token that has just expired.

That is the **fourth** Phase-0/early-Phase-1 seam to be subtly wrong at its first real use
(0.11 envelope drift, 0.12 lint rules, 1.3 `Module.Jobs`, and now `Public`). The pattern is now
reliable enough to state as a rule: *a seam with no consumer is not built, it is drafted.*

### 7.3 The mutation drills

**Drill 1 — make `ResetPassword` clear `must_change`.** `TestAnAdministratorResetForcesAChange`
failed: *"an administrator-set password does not force a change; the flag is dead code"*.

**Drill 2 — remove the self-deactivation check.** It failed, but with
`code = "identity.last_active_user"` — the *neighbouring* rule was doing the work, because the
fixture had one user. The assertion on the code caught it, but the scenario was unrealistic. The
test now creates a colleague who is also an administrator, so neither the last-active-user rule
nor the last-administrator rule can refuse. Re-run, the drill fails on the right thing entirely:
*"the signed-in administrator deactivated themselves"*.

### 7.4 A latent bug in the query layer

`useMutation({ mutationFn: revokeSession })` passes the binding wrapper by reference — and
TanStack Query calls `mutationFn` with a **second argument** of its own. Our wrappers name their
parameters, so the extra object was discarded; but `call(struct, method, ...args)` spreads, and
the day a wrapper does the same it would serialise Query's internal context across the IPC
boundary.

Found by an assertion that expected `["s-2"]` and received `["s-2", {client, meta, mutationKey}]`.
Every mutation now wraps its call, so only what we decided to send crosses.

### 7.5 Carried forward

- **Branch-scoped role assignment has no screen.** The model supports it (1.4); every Phase 1
  permission is company-wide, so a branch picker would offer a distinction nothing observes. It
  arrives with the Phase 4 multi-branch screens.
- **No company/branch/warehouse settings screens.** They would edit org data only the wizard
  writes today; Phase 4 owns multi-branch management.
- **Audit paging.** The viewer reads a 200-row window with an entity-type filter. §FE.2 called
  the audit log "the first genuinely paginated read" — it is not paginated yet, and will need to
  be before a shop has a year of history.
- **Still no visual confirmation.** Twelve screens now exist and none has been rendered on a
  display. This is the last Phase 1 step before the DoD review, which is where it belongs.
