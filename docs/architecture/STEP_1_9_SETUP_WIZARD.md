# Step 1.9 — The setup wizard backend (Design + implementation record)

> Status: **IMPLEMENTED.** `make ci` green; both mutation drills confirmed (§8.3), and the first
> drill changed the design (§8.2).
> Scope: `internal/api/setup` (the orchestration), the `Setup` binding and its structural gate,
> and the cross-module test that closes the currency gap Step 1.8 opened.
> **Out of scope:** the wizard screens (1.10–1.11); importing starting data (§C.2 step 10 —
> Phase 9); tax profile seeding (Phase 2, and no country profile ships one).

---

## 1. ANALYSIS

### 1.1 The bootstrap paradox

§WIZ.1 states it exactly: no user exists, so nobody can authenticate; but creating the first
administrator requires a call, and every call requires authorization. Something must give.

The phase design's answer, approved: **setup bindings are `Public`, and are callable only while
no company exists.** Once a company row exists, every mutating `Setup.*` method fails forever.

The alternative — a magic bootstrap principal — is worse in a specific way: it would then exist
for the life of the system, as an identity with no password, no audit trail, and no way to
revoke. A row-count check has none of those properties, and it is trivially testable.

### 1.2 "Callable only while no company exists" must be structural

This is a **public, unauthenticated method that creates an administrator.** If the check is one
`if` a method must remember, then a future `Setup.Reset` that forgets it is a remote
account-creation hole reachable from an unauthenticated surface.

Step 1.5 (D1) already solved the shape of this problem: the guard is the *accessor*, so a method
that skips it has no graph and is non-functional rather than unprotected. This step applies the
same trick one level in — `setupGuard` returns the graph **only** while setup is required, so a
mutating Setup method that forgets it cannot reach the database at all.

### 1.3 Two gates, not one

`Status` and `Apply` need different rules, and conflating them would break the frontend:

| Method | Gate | Why |
|---|---|---|
| `Status` | Public, callable **always** | The shell asks "do I route to the wizard?" on every launch, including after setup. A gate that closed would make the answer unobtainable. |
| `Apply` | Public, callable **only while unprovisioned** | It creates an administrator. |

After setup, `Status` returns `{required: false}` and **nothing else** — no country list, no
profile catalogue. A public method should stop answering as soon as its purpose is over.

### 1.4 Risks

| Risk | Consequence | Answer |
|---|---|---|
| A forgotten check on a public method | Unauthenticated administrator creation | `setupGuard` is the accessor (§1.2, D2) |
| A half-configured company | A company with no branch: no screen can recover it | One transaction (D3) |
| The wizard becomes a hidden source of truth | Values that can only be set once | The wizard writes settings and rows, nothing else (§3.4) |
| A profile names a currency nobody seeded | Provisioning fails on a foreign key mid-wizard | Fixed, and a test now asserts the invariant (§5) |
| The wizard finishes but the account does not work | The customer is locked out of a fresh install | Apply does not auto-sign-in; the frontend logs in, which proves the credential (D6) |

---

## 2. DESIGN — where the orchestration lives

### 2.1 Not a module — D1

Setup orchestrates **org, identity, profile, and currency** in one transaction. A package under
`internal/modules` may reach another module only through its `contract` (`module-isolation`,
1.1), so a `modules/setup` is structurally impossible — correctly so: setup is not a domain, it
is a one-time composition of four of them.

It lives in **`internal/api/setup`**, as an application service:

- The API layer is already the only layer permitted to see several modules at once; every
  binding does it.
- Keeping it out of `bindings` keeps the binding thin — DTOs and the guard — and makes the
  orchestration testable without constructing a Wails façade.
- It is not the composition root either. `bootstrap` starts the system; it should not also own
  a business use case.

### 2.2 The service

```go
type Service struct { db, org, identity, profile, settings, bus }

func (s *Service) Required(ctx) (bool, error)
func (s *Service) Options(ctx) (Options, error)   // countries, profiles, currencies, locales
func (s *Service) Apply(ctx, Input) (Result, error)
```

Dependencies are the individual services, named explicitly, rather than `*bootstrap.App`.
Taking the App would make every future field of the graph an invisible dependency of setup.

---

## 3. DESIGN — `Apply`

### 3.1 One transaction — D3

```
Do:
  org.Provision            → company, branch, warehouse, fiscal year
  identity.SeedRoles       → the default roles
  identity.CreateUser      → the administrator (must_change = 0)
  identity.AssignRoleByCode→ administrator
  profile.Apply            → the business bundle's settings and flags
  settings.Set             → the wizard's explicit choices
  publish Auditable        → setup.completed
```

Every inner call opens its own `Do`, which **joins** the caller's (0.3 join semantics), so this
is one transaction with one commit. A failure anywhere leaves **no company at all** — which is
recoverable by running the wizard again — rather than a company with no administrator, which no
screen in the system can repair.

### 3.2 Order matters, in one specific place

The business profile is applied **before** the wizard's explicit settings. The user chose their
language on screen; the bundle merely suggests defaults for a trade. Whoever writes last wins,
and it must be the person.

### 3.3 Validation before anything is written

`Apply` is public and unauthenticated, so its input is untrusted. Everything is checked before
the transaction opens: the country must exist in the profile registry, the business profile must
exist, currency codes must be seeded, and the administrator's password must satisfy the policy
(identity enforces this; the wizard does not duplicate the rule).

### 3.4 The wizard writes only what can be rewritten — §C.2

Every value `Apply` sets is a settings row or a table column that a Settings screen can change
later. The wizard contains **no logic that cannot be reproduced by editing configuration** —
otherwise it becomes a hidden source of truth, which §C.2 explicitly forbids and which is the
trap that makes first-run wizards unmaintainable.

### 3.5 The actor is `system` — D5

Setup is performed by nobody: there is no authenticated user, and the administrator does not
exist until part-way through the transaction. Every audit entry from `Apply` therefore records
`source: system` with no actor.

The alternative — stamping the just-created administrator as the actor for the later half —
would produce a trail where the company was created by "system" and the profile applied by
"Nadia", implying a sequence of decisions that never happened. **An unattributed entry is
honest; a fabricated one is not** (1.7). Who was created is in the payload, where it is a fact
rather than an attribution.

### 3.6 Apply does not sign the user in — D6

The wizard finishes signed out, and the frontend logs in with the credentials just typed.

That is one extra screen, and it buys something worth more: **the credential is proved before
the wizard closes.** A wizard that completes and leaves an unusable login is the worst possible
outcome on a fresh install — the customer has a configured system they cannot enter. It also
keeps session-minting in exactly one place.

---

## 4. DESIGN — the binding gate

```go
func setupPolicies() map[string]policy.Policy {
    return map[string]policy.Policy{
        "Status": policy.Public(),
        "Apply":  policy.Public(),
    }
}

// setupGuard is guard PLUS "setup is still required".
func (s *Setup) setupGuard(method string) (context.Context, *bootstrap.App, error)
```

`Apply` calls `setupGuard`; `Status` calls the ordinary `guard`. A method that calls neither has
no graph, so the failure mode of forgetting is a method that does nothing — not one that runs
unprotected (D2).

The check is `org.IsProvisioned`, the same row count the service uses. Two enforcement points on
purpose, and they answer different questions: the binding gate is about **reachability** (keep
the wizard off a configured install), and `org.Provision`'s own check is about **truth** (a
second company would make every scope in the system ambiguous, so a future importer calling the
service directly must be refused too).

---

## 5. The Step 1.8 defect this step closes

Step 1.8 shipped `sa.json`, `ae.json`, and `eg.json` naming SAR, AED, and EGP. Currency seeds
only SYP, USD, EUR, and TRY, and `companies.functional_currency` carries a foreign key to
`currencies(code)`.

**Choosing Saudi Arabia in the wizard would have failed at provisioning with a constraint
error**, mid-transaction, on a fresh install — the exact class of defect that only appears at
first real use. It is the same pattern this project has now hit at every phase boundary: a seam
built one step ahead of its consumer is subtly wrong until the consumer arrives.

Fixed by seeding the three currencies, and — more importantly — by a test asserting that **every
shipped country profile's currencies exist in the seeded currency table.** The next profile
cannot reintroduce the gap.

---

## 6. TESTING PLAN

| Level | Tests |
|---|---|
| **Required** | True on a fresh install; false afterwards |
| **The gate (D2)** | `Apply` fails once a company exists — and keeps failing |
| **Atomicity (D3)** | A failure mid-apply leaves **no** company, **no** user, and no audit entries |
| **Ordering (§3.2)** | The wizard's explicit language beats the bundle's |
| **Validation** | Unknown country, unknown profile, unseeded currency, weak password — each refused before anything is written |
| **No default password** | The administrator is created with what the user typed and `must_change = 0` (§13.1) |
| **Cross-module (§5)** | Every shipped country profile's currencies are seeded |
| **Options** | Returns countries/profiles/currencies while required; empty afterwards |
| **End to end** | After `Apply`, the administrator can log in and holds administrator permissions |

**Mutation-verified**: (1) let `Apply` use the ordinary guard instead of `setupGuard` → the "not
after setup" test must fail; (2) apply the wizard's settings before the bundle → the ordering
test must fail.

---

## 7. DECISIONS TAKEN

1. **D1 — The orchestration is `internal/api/setup`**, not a module (blocked by
   `module-isolation`, and rightly) and not the composition root. *(§2.1.)*
2. **D2 — `setupGuard` is the accessor for mutating Setup methods**, so forgetting the check
   yields a non-functional method rather than an unprotected one. *(§1.2, §4.)*
3. **D3 — `Apply` is one transaction.** A half-configured company is unrecoverable; an
   unconfigured one is just the wizard again. *(§3.1.)*
4. **D4 — `Status` stays callable forever, and answers less after setup.** *(§1.3.)*
5. **D5 — Setup's audit entries record `system` with no actor.** Fabricating an attribution
   would imply decisions nobody made. *(§3.5.)*
6. **D6 — `Apply` does not sign the user in.** The login that follows proves the credential
   before the wizard closes. *(§3.6.)*
7. **D7 — SAR, AED, and EGP are seeded**, and a test pins the profile↔currency invariant. *(§5.)*

---

## 8. IMPLEMENTATION RECORD

### 8.1 What was built

| File | What |
|---|---|
| `api/setup/setup.go` | `Required`, `Options`, `Apply`, and the input normalisation |
| `api/bindings/setup.go` | The `Setup` façade, its DTOs, and `setupGuard` |
| `api/bindings/bindings.go` | `Setup` registered in the façade set |
| `modules/currency/seeds.go` | SAR, AED, EGP — the gap §5 describes |
| `modules/identity/permissions.go` | `RoleAdministrator` exported; the wizard and the seed list had the string twice |
| `bootstrap` | `app.Setup` wired from the individual services |

### 8.2 The first drill changed the design

**Drill 1 failed to fail.** Replacing `setupGuard` with the ordinary `guard` in `Apply` broke
nothing: the second call was refused anyway, by an `IsProvisioned` check I had written *inside*
`setup.Apply`.

Three enforcement points existed where the design described two, and the redundant one was
doing real harm: **it made the binding gate untestable.** A guard whose removal is invisible is
a guard that has already stopped working and nobody can tell.

The third check was deleted, with the reasoning at the site. Two remain, and they answer
different questions — the binding's `setupGuard` (reachability) and `org.Provision`'s own check
(truth, for any caller including a future importer). Re-run, the drill now fails correctly:
`code = "org.already_provisioned", want "setup.already_complete"` — the error code is what
distinguishes which layer caught it, so the test pins the gate rather than the outcome.

This is the second time a drill has reported a *reasoning* error rather than a code error (the
first was 1.4's cascade claim). Both times the passing drill was the useful signal.

### 8.3 The second drill, and a test that proved nothing

**Drill 2 — apply the wizard's settings before the bundle.** The ordering test failed as
required: `locale = "ar", want the language the user chose in the wizard`.

But it only failed because the drill was run against a fixture written for it. As originally
committed the test used `general_retail`, whose bundle is **empty** (1.8 D8) — so it would have
passed under either ordering, proving nothing. The fixture now drops a bundle that really sets
`ui.locale`, through the **overlay layer 1.8 shipped**, so a test fixture and a customer's own
profile take exactly the same path into the system.

Fourth occurrence of "a passing test that tests nothing" in this project. The pattern is
consistent: the assertion's success path and its failure path are reachable by different
routes, so nothing forces them apart.

### 8.4 The Step 1.8 defect, closed

`sa`/`ae`/`eg` named SAR/AED/EGP; currency seeded only SYP/USD/EUR/TRY; `companies.functional_currency`
has a foreign key to `currencies(code)`. Choosing Saudi Arabia would have failed mid-transaction
on a fresh install.

Two tests now hold the line. `TestEveryShippedCountrysCurrenciesAreSeeded` checks the invariant
against the real seeded database, and `TestEveryShippedCountryCanCompleteSetup` **runs the whole
wizard once per shipped country** and signs in afterwards — which does not reason about what
would happen, it does it.

### 8.5 A fifth count-based test broke

`TestSecondBootIsClean` asserted `currencies == 4`. Adding three currencies broke it, while the
property it guards — seeding is idempotent — was never in question. Rewritten to compare the
count after the second boot against the count after the first, which is what idempotency
actually means.

### 8.6 Carried forward

- **`Apply` needs `settings.Reload`** afterwards, because the rows are written inside a
  transaction the in-memory cache was loaded before. The 0.5 design flagged exactly this for a
  `Set` nested in a business transaction; this is its first real occurrence.
- **The wizard has no screens** (1.10–1.11). Everything above is reachable only from a test.
- **Starting data import** (§C.2 step 10) is Phase 9; the wizard's last step will read "start
  empty" until then.
- **Tax is not a wizard step yet.** §C.2 step 8 offers enable/disable, and no country profile
  ships a tax profile to enable (§C.3). It arrives with Phase 2.
