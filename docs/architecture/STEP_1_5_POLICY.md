# Step 1.5 — The Policy Mechanism & Enforcement (Design + implementation record)

> Status: **DESIGN — implemented in the same turn under the reviewer's standing approval.**
> **D1 strengthens the phase design's D6** into a structural guarantee rather than a
> declarative one. That is the decision to read hardest.
> Scope: `internal/api/policy`, the guard inside every binding façade, the `Auth` binding, the
> startup policy-coverage check, and the current-session holder.
> **Out of scope:** field-level redaction (1.6); audit (1.7); the login screen (1.10).

Step 1.4 built the *answer* to "may they?". This step builds the guarantee that **anyone asks**.

---

## 1. ANALYSIS

### 1.1 The phase design's D6, and where it fell short

Phase 1 §POL.2 proposed: declare a policy per binding method, and validate at startup by
reflection over the static binding set that **every exported method has one**. A method with no
policy is a fatal startup error.

That is good, and it was approved. But it guarantees only that a policy is **declared** — not
that it is **consulted**. A method could carry a perfect policy entry and simply never check
it, and the startup validation would pass. §POL.2 admitted as much ("the honest limitation").

Implementing it revealed that the honest limitation is unnecessary.

### 1.2 The observation that closes the gap — D1

Every graph-backed binding method already begins the same way:

```go
app, ok := m.resolve()
if !ok { return envelope.Fail[T](notReady()) }
```

`resolve()` is how a method reaches the object graph. **A method that does not call it has no
database, no services, and no context** — it cannot do anything at all.

So the guard does not need to be a decorator a method might skip. It can *be* the accessor:

```go
ctx, app, err := m.guard("Currencies")
if err != nil { return envelope.Fail[T](err) }
```

`guard` looks up the method's policy, resolves the session, checks the permission, stamps the
actor onto the context, and only then hands back the graph. A method that forgets it is not
unprotected — it is **non-functional**, which a reviewer and the compiler both notice
immediately.

That turns §14.3's promise from *"a use case without a declared policy fails to start"* into
something stronger and simpler to state:

> **A binding method cannot reach the database without passing its policy.**

The startup check still exists, and still matters: it catches the *other* failure — a method
that calls `guard` with a name nobody declared a policy for.

### 1.3 Where the session token comes from

A Wails binding is called directly from JavaScript; there is no request object to carry a
token, and threading one through every call signature would put an opaque string in every
frontend call site.

**A desktop ERP has exactly one user at the machine and one window.** So the process holds one
current session: `Auth.Login` sets it, `Auth.Logout` clears it, and every guarded method reads
it. This is not a shortcut around multi-tenancy — there is no multi-tenancy inside the process
to work around (§2 of ARCHITECTURE_v1: single-process desktop application, no HTTP layer).

Sessions remain per-row and revocable in the database exactly as 1.3 built them; this is only
about which token the *current window* is presenting.

### 1.4 Risks

| Risk | Consequence | Answer |
|---|---|---|
| A new binding method ships unguarded | An unauthenticated caller reaches the database | It cannot reach the graph without `guard` (§1.2) |
| A method guards under the wrong name | The wrong policy applies, silently | Startup rejects a `guard` name with no policy; a test asserts every declared name is used |
| Public methods proliferate | The surface quietly becomes unauthenticated | `Public` is explicit, never a default, and enumerated in a test |
| Permission denied looks like a crash | Users cannot tell "not allowed" from "broken" | A typed `Permission` category the frontend renders as the denied state (0.11 D6) |
| The guard runs before boot | Nil dereference on first launch | Not-ready is checked first, as today |

---

## 2. DESIGN — `internal/api/policy`

```go
type Policy struct {
    Permission string
    Scope      auth.ScopeKind
    Public     bool
}

func Requires(permission string) Policy          // global scope by default
func (p Policy) InScope(k auth.ScopeKind) Policy
func Public() Policy                              // explicit, never a default
```

`Public` is a separate constructor rather than an empty `Policy{}`, so the zero value is not a
valid unauthenticated policy. An accidentally-empty map entry must fail, not open a hole.

---

## 3. DESIGN — the guard

```go
func (g *graph) guard(method string) (context.Context, *bootstrap.App, error)
```

In order:

1. **Not ready** → typed `app.not_ready` (unchanged from 0.11).
2. **Policy lookup.** No policy for this method name is a **fatal-class internal error**, not a
   denial — startup should have caught it, so reaching here means the name is misspelled.
3. **Public** → return the enriched context with no actor. The setup wizard and login run here.
4. **Session.** No current token, or a token that fails 1.3's `Validate`, → typed
   `identity.session_invalid`.
5. **Actor stamped** onto the context (`appctx.WithActor`), which is what makes settings resolve
   at user scope (1.3 §6) and what `Can` reads.
6. **Authorize.** `Can(ctx, policy.Permission, scope)` — false is a typed `Permission` error the
   frontend renders as the permission-denied state built in 0.11 D6.

Scope comes from the session's branch when the policy asks for branch scope, and is Global
otherwise. A policy asking for warehouse scope has no consumer yet and is rejected at startup
rather than silently treated as global.

---

## 4. DESIGN — the `Auth` binding

```go
Auth.Login(username, password, remember) → Result[SessionDTO]   // Public
Auth.Logout()                            → Result[bool]          // Public
Auth.Me()                                → Result[SessionDTO]    // Public
```

All three are `Public`, and each for a reason worth stating:

- **Login** must be reachable by an unauthenticated caller; that is its entire purpose.
- **Logout** is public because a session that has *just* expired must still be able to clear
  itself. Requiring a valid session to log out would strand the frontend holding a dead token.
- **Me** answers "am I signed in, and as whom?" — the question the frontend asks *before* it
  knows the answer. It returns a signed-out result rather than an error when there is no
  session, because "not signed in" is not a failure.

`SessionDTO` carries the display name, the permission set from `Effective` (cosmetic — the
backend remains the only enforcement point, §14.3), and `mustChange`. **It never carries the
token**: the token lives in the process, not in the frontend, so it cannot be logged, stored in
`localStorage`, or read by an injected script.

---

## 5. DESIGN — the startup coverage check

```go
func (s *Set) ValidatePolicies() error
```

Reflects over every struct in `All()`, enumerates its **exported methods**, and requires a
policy entry for each. Anything missing is a fatal startup error naming the struct and method.

Also checked, because each is a way the mechanism could rot:

- A policy declared for a method that **does not exist** (a rename left it behind).
- A policy naming a permission **no module declares** — it could never be granted, so the
  method would be permanently unreachable.
- A policy with **neither** `Public` nor a permission — the empty-value hole.

Reflection is used once, at boot, over a set fixed at compile time. Not at call time, and not
to discover behaviour.

---

## 6. TESTING PLAN

| Level | Tests |
|---|---|
| **Coverage** | Every exported method on every binding has a policy; **removing one entry fails startup** naming it; a policy for a nonexistent method fails; a policy naming an undeclared permission fails. |
| **Guard** | Not-ready before attach; a guarded method with no session is refused; with a valid session and the permission, allowed; with a session but **without** the permission, refused with the `Permission` category. |
| **Structural** | A guarded method cannot reach the graph without `guard` — asserted by the fact that `resolve` no longer exists (a compile-level guarantee, stated in the test as documentation). |
| **Public** | Login/Logout/Me work with no session; the set of public methods is **pinned by a test**, so making something public is a visible diff. |
| **Auth** | Login returns a principal and permissions; a wrong password returns the 1.2 code; Logout clears the session; Me reports signed-out cleanly. |
| **Token** | The token never appears in any DTO's JSON. |
| **Scope** | A branch-scoped policy checks against the session's branch. |

**Mutation-verified**: *coverage* (delete a policy entry → startup must fail) and
*authorization* (make `guard` skip the `Can` call → the without-permission test must fail).

---

## 7. DECISIONS TAKEN

1. **D1 — The guard IS the graph accessor**, strengthening the phase design's D6: a method that
   skips it is non-functional rather than unprotected. *(§1.2.)*
2. **D2 — One current session per process**, set by Login and read by the guard. A desktop app
   has one user at the machine; there is no in-process multi-tenancy to work around. *(§1.3.)*
3. **D3 — The token never crosses to the frontend.** It lives in the process, so it cannot be
   logged or stored by the webview. *(§4.)*
4. **D4 — `Public` is an explicit constructor**, never the zero value. *(§2.)*
5. **D5 — Logout and Me are public**, so an expired session can still clear itself and the
   frontend can ask "am I signed in?" before it knows. *(§4.)*
6. **D6 — The coverage check also rejects orphaned and unsatisfiable policies**, not only
   missing ones. *(§5.)*

---

## 8. IMPLEMENTATION RECORD (Protocol Steps 3–6)

### 8.1 The guarantee got stronger than the phase design asked for

Phase 1 §POL.2 admitted an "honest limitation": startup validation proves a policy is
*declared*, not *consulted*. Implementing it showed the limitation was avoidable.

Every graph-backed method already began with `resolve()` — the only way to reach the object
graph. Making the guard **be** that accessor means a method which skips it has no database, no
services, and no context. The guarantee is now:

> A binding method cannot reach the database without passing its policy.

Not "should not". *Cannot.* `resolve()` no longer exists as a way to get a usable graph without
a policy check.

### 8.2 The 0.11 test suite failing was the proof

Converting the façades broke seven assertions in `bindings_test.go` — all of the form
*"System.Health failed after Attach: identity.session_invalid"*.

That is the step working. Those tests were written when attaching the graph was enough; the
whole point of 1.5 is that **the graph being ready says nothing about who is asking.** The test
is now split: `TestGuardedMethodsStillNeedASessionAfterAttach` (every method refuses) and
`TestGuardedMethodsWorkForASignedInUser` (the positive half, through a real `Auth.Login`).

### 8.3 Both drills, and what mutation 1 revealed

*Authorization skipped* — making `guard` return without calling `Can`:

```
a signed-in user with NO permissions reached a guarded method —
the guard authenticates but does not authorize
```

and the companion test printed the leak in full: a complete list of scheduled jobs, their
schedules and next-run times, returned to a user holding no grants. Authentication without
authorization is not a smaller version of security; it is a different thing that looks like it.

*Policy coverage* — renaming a policy key caught **both** failure modes at once:

```
System declares a policy for Healthz, which is not an exported method (renamed or removed?)
System.Health has no declared policy
```

The first attempt at this mutation deleted the map entry and broke the build on an unused
import — a reminder that a mutation which does not compile proves nothing, and has to be
reshaped until it does.

### 8.4 A bug in my own conversion, caught before it shipped

`Config.set` read the preference back through `app.Context()` — a fresh context with **no
actor**. Since preferences now resolve at *user* scope, that would have read the system-scope
value and echoed the wrong theme straight back to the user who just changed it.

Fixed by re-guarding to get a context that is both fresh *and* actor-stamped. Worth recording
because it is the second time this exact shape has bitten in this file: 0.11 §9 fixed the
locale half of it, and the scope change re-opened it from a different direction.

### 8.5 One deliberate linter suppression

`Auth.currentPrincipal` discards the error from `Validate` and reports "signed out". `nilerr`
flagged it, correctly — and the suppression carries the reason: an expired session is a
**state**, not a failure, and returning an error would make "your session ended"
indistinguishable from "the app is broken" in the frontend.

### 8.6 Verification

**`make ci` green.** 16 new Go tests. Three new error codes, translated in both locales. The
frontend gained the `Auth` binding and a matching mock — kept contract-accurate because 0.11
§1.2 was caused by a mock that was not.

**Mutation-verified**: authorization skipping and policy coverage (§8.3).

### 8.7 Carried forward

- **The public surface is three methods** — `Auth.Login`, `Logout`, `Me` — pinned by a test so
  widening it is a visible diff. The setup wizard's bindings (1.9) will be the next entries,
  and each will need the same argument made for it.
- **Nothing enforces `mustChange` yet**; the 1.10 login flow does.
- **Field-level redaction** is 1.6: a permission can currently gate a whole method, not a field
  within its response.
- **Audit** (1.7) is where login, logout, denial, and revocation get recorded.
- **`Boot` is the single policy-exempt binding**, because it reports whether the graph exists
  and therefore cannot require one. It holds no graph at all, so there is nothing to protect.

---

*End of Step 1.5. Implemented and self-reviewed.*
