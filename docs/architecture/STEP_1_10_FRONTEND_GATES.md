# Step 1.10 — Frontend: gates, router, login, and the setup wizard (Design + implementation record)

> Status: **IMPLEMENTED.** `make ci` green (144 frontend tests); both mutation drills confirmed (§8.3).
> Scope: the three deferred dependencies (D9); the gate stack `Boot → Setup → Auth → Shell`;
> routing; the login screen; the setup wizard screens.
> **Out of scope:** users, roles, sessions, and the audit viewer (1.11); starting-data import
> (Phase 9).

---

## 1. ANALYSIS

### 1.1 What Phase 1 asked for

§FE.1 specifies the gate stack exactly:

```
BootGate (graph ready?)
  └─ SetupGate   (company exists?)   → wizard
      └─ AuthGate (session valid?)   → login
          └─ AppShell
```

*"Each gate mounts nothing below it until satisfied — the same discipline that keeps the shell
off a mid-restore database."*

That discipline is the design. A gate is not a redirect: it is a **mount boundary**. Nothing
below a `SetupGate` can render, fire an effect, or issue a query until a company exists — so a
screen cannot accidentally call a binding that has no data to answer with.

### 1.2 The deferred dependencies come due — D9 (approved in the phase design)

0.11 D1 deferred TanStack Query, Zustand, and a router *"to Phase 1, where a real list screen
and real routes create the need."* The need is now real, and all three are adopted:
`@tanstack/react-query`, `zustand`, `react-router-dom`.

The deferral did its job. Each arrives with a consumer rather than as speculation, and this
step is where "was that the right call?" gets answered — see §7.

### 1.3 The risk this step carries

**The frontend is not a security boundary.** Every gate, every hidden button, every route guard
here is *cosmetic*: §14.3 is explicit that the backend is the sole enforcement point, and Step
1.5 made that structural.

The risk is that a reviewer three years from now reads `<RequirePermission>` and believes it
protects something. It does not. It exists so the UI does not offer actions that will fail, and
every such component says so at its definition.

### 1.4 Risks

| Risk | Consequence | Answer |
|---|---|---|
| A gate treated as security | Someone removes a backend check because "the UI hides it" | Every gate's doc comment states it is cosmetic (§1.3) |
| A route rendering before its gate resolves | A flash of the shell, then the login screen | Gates render nothing below until resolved (§2.1) |
| Query cache surviving a sign-out | The next user sees the previous user's data | The cache is cleared on sign-out, and that is a test (§3.2) |
| A wizard step losing what was typed | Twelve fields re-entered after a validation error | Wizard state is one object held across steps, submitted once (§5.2) |
| Field errors ignored | The user is told "invalid" with no idea which field | `BindingError.fields` is mapped to inputs (§4.3) |

---

## 2. DESIGN — the gates

### 2.1 A gate is a mount boundary, not a redirect

```tsx
export function SetupGate({ children }: { children: ReactNode }) {
  const status = useSetupStatus();      // suspense-free query
  if (status.isPending) return <FullScreenSpinner />;
  if (status.data?.required) return <SetupWizard />;
  return <>{children}</>;
}
```

`children` is not rendered while the answer is unknown. React would otherwise mount the shell,
fire its queries, and unmount it a tick later — producing a visible flash and a burst of calls
that all fail with `not_ready` or a session error.

### 2.2 `AuthGate` and the `mustChange` case

`AuthGate` renders the login screen when `me()` reports signed out. It also handles
`mustChange`: a user whose password is flagged must change it before anything else mounts.

The backend does not yet expose a change-password binding for one's own account — that arrives
in 1.11 with the user screens. Until then the gate renders an explicit "you must change your
password, and this build cannot yet do it" state rather than silently letting them through.
**Recorded as a gap rather than papered over**, because letting them through would make
`must_change` a lie in exactly the situation it exists for.

### 2.3 Sign-out clears the query cache — D3

`queryClient.clear()` on sign-out. Without it, the next user to sign in at the same terminal
would see the previous user's cached lists until each query refetched — on a shop floor where
one machine is shared by three cashiers, that is a real data leak, and it is a *frontend* one
the backend cannot prevent.

---

## 3. DESIGN — data access

### 3.1 TanStack Query over the 0.11 wrapper

`call()` already throws `BindingError` (0.11 §3.1), which is exactly TanStack Query's error
contract — the two compose with no adapter. Query keys are namespaced per binding
(`["auth","me"]`, `["setup","status"]`).

**Retries are off by default.** A binding call is a local IPC to a process on the same machine:
if it failed, it failed for a reason that will not change in 200ms, and retrying turns one
clear error into three and a delay. Network-style retry policies are a bad fit for a desktop
app with no network in the path.

### 3.2 Zustand for session state — and only that

One store, holding the signed-in principal and the permission set. Deliberately small: server
state belongs in Query (which caches, invalidates, and dedupes it), and putting it in Zustand
too would create two copies that disagree.

The store holds **no token**. The token lives in the Go process (1.5 D3) and never crosses the
boundary.

### 3.3 Permission-aware UI — §FE.3

```tsx
<Can permission="identity.user.manage">…</Can>
```

Cosmetic only. It reads the permission set the backend supplied and hides what would fail. The
`EmptyState` `denied` variant built in 0.11 D6 finally gets its producer: a `BindingError` whose
category is a permission renders it.

---

## 4. DESIGN — the login screen

### 4.1 What it must handle

Wrong credentials, a throttled account (with the `retryAfter` parameter 1.3 D6 attaches), a
deactivated user, and a backend that is not ready. All four arrive as a `BindingError` with a
code, and all four render through `translate(locale, code, params)` — no prose crosses the
boundary (§22.2).

### 4.2 Lockout feedback

§13.1's throttle is "delay, never lock". The screen shows the wait rather than an error the user
cannot act on: the same code carries `retryAfter`, and the submit button is disabled with a
countdown. Telling someone "try again later" without saying how much later is the difference
between a working shop and a support call.

### 4.3 Field errors

`BindingError.fields` maps to the inputs by name. The envelope has carried per-field failures
since 0.11 and nothing has consumed them until now — this is their first real use, which per
this project's pattern is when they will turn out to be subtly wrong if they are.

---

## 5. DESIGN — the setup wizard

### 5.1 The steps

Addendum §C.2's ten, collapsed to seven screens because three of them are single fields that
belong with their neighbours, and one (starting data) is Phase 9:

1. **Language** — affects the wizard immediately
2. **Country** — loads the profile as defaults
3. **Company** — name, legal name, tax number
4. **Currency** — functional and pricing, pre-filled from the country
5. **Business type** — the profile bundle
6. **Locations & fiscal year** — branch, warehouse, first year
7. **Administrator** — username, display name, password (twice)

Step 8 (tax) is omitted rather than shown as a disabled control: no country profile ships a tax
profile (§C.3), so the question has no answer to offer yet. Phase 2 adds it.

### 5.2 One object, submitted once — D5

The wizard holds a single `SetupInput` across all seven steps and calls `Setup.Apply` exactly
once, at the end. Two reasons, and the second is the one that matters:

- The backend applies everything in one transaction (1.9 D3). A stepwise API would have to
  either hold a partial company or invent a draft, and both are states nobody can recover from.
- **A validation failure must not cost the user what they typed.** All seven steps' values are
  in memory, so a rejection returns them to the offending step with everything intact.

### 5.3 The country profile drives the defaults, and never more

Choosing a country pre-fills currency, fiscal-year start, and language. Every one stays editable
— that is §C.1's *"the profile only supplies defaults; it never constrains later
configuration"*, and it is why the wizard shows them rather than applying them invisibly.

### 5.4 After Apply — signing in

1.9 D6: `Apply` does not sign the user in. The wizard therefore ends by calling `login()` with
the credentials just typed, which **proves the account works before the wizard closes**. If that
login fails, the wizard says so on its own last screen rather than dropping the user at a login
form with no explanation.

---

## 6. TESTING PLAN

| Level | Tests |
|---|---|
| **Gate order** | Nothing below a gate mounts until it resolves; a not-ready backend keeps the boot screen |
| **SetupGate** | `required: true` renders the wizard, not the shell |
| **AuthGate** | Signed out renders login; signed in renders children; `mustChange` renders its own state |
| **Sign-out** | The query cache is cleared (D3) |
| **Login** | Wrong password renders the translated code; a throttled attempt renders the wait; field errors land on their inputs |
| **Wizard** | Country selection pre-fills and stays editable; values survive a rejected submit; Apply is called once with everything |
| **Permission UI** | `Can` hides without the permission and shows with it — and is documented as cosmetic |
| **i18n** | Every new key exists in both catalogues (the 0.8 gate, extended) |

**Mutation-verified**: (1) render `children` while a gate is pending → the flash test must fail;
(2) drop `queryClient.clear()` on sign-out → the cache test must fail.

---

## 7. DECISIONS TAKEN

1. **D1 — Gates are mount boundaries, not redirects.** Nothing below renders until the gate
   resolves. *(§2.1.)*
2. **D2 — Every gate and permission component documents that it is cosmetic.** The backend is
   the sole enforcement point (§14.3). *(§1.3.)*
3. **D3 — Sign-out clears the query cache.** A shared terminal must not show the previous
   cashier's data. *(§2.3.)*
4. **D4 — Query retries are off.** A local IPC failure will not resolve itself in 200ms. *(§3.1.)*
5. **D5 — The wizard holds one object and submits once**, matching the backend's single
   transaction and keeping what the user typed. *(§5.2.)*
6. **D6 — `mustChange` renders an explicit dead-end** until 1.11 adds the change-password path,
   rather than letting the user through. *(§2.2.)*
7. **D7 — The tax step is omitted**, not disabled: no country profile ships a tax profile to
   enable. *(§5.1.)*

---

## 8. IMPLEMENTATION RECORD

### 8.1 What was built

| File | What |
|---|---|
| `app/providers/QueryProvider.tsx` | The query client — retries off, focus-refetch off |
| `app/session/session.ts` | The Zustand store, `hasPermission`, `useCan` |
| `app/session/useSignOut.ts` | Sign-out, and the cache clear D3 requires |
| `app/gates/{SetupGate,AuthGate,GateScreen}.tsx` | The §FE.1 stack |
| `modules/auth/LoginScreen.tsx` | Sign-in, lockout countdown, field errors |
| `modules/setup/{SetupWizard,steps,wizardState}` | The seven-step wizard |
| `lib/wails/index.ts` | The `Setup` bindings, typed |
| `App.tsx` | Router + the gate stack |
| `test/appRender.tsx` | The shared harness: real providers, a fresh cache per render |

### 8.2 Three decisions taken during implementation

**`HashRouter`, not `BrowserRouter`.** This is a webview loading from a file; there is no server
to answer a deep path on reload. Hash routing is also honest about what routing means in a
desktop app — in-window navigation, not URLs anyone types.

**React Router's v7 future flags are opted into now.** Both change how navigation resolves, and
discovering that on a version bump — in software meant to be maintained for a decade — is
strictly worse than discovering it here, with two routes and no users. It also removes a warning
that would otherwise appear in every test run, and noise that is always present is noise nobody
reads.

**`toInput` names every field rather than spreading.** The backend's `SetupInput` is the
contract; a `...rest` would send whatever the wizard happened to be holding, which is how
`adminPasswordConfirm` would eventually reach a Go struct that never asked for it.

### 8.3 The mutation drills

**Drill 1 — render `children` while the gate is pending.** `TestSetupGate > renders nothing
below it while the answer is unknown` failed: *"Unable to find role=status"*. The mount-boundary
property (D1) is watched.

**Drill 2 — drop `queryClient.clear()` from sign-out.** The cache test failed:
*"expected [ { id: '1', … } ] to be undefined"*. D3 is watched — and this is the one guarantee
in the step that the **backend cannot provide**: a cached answer never reaches a binding, so no
amount of server-side permission checking prevents the previous cashier's list from being on
screen.

### 8.4 §FE.4's question, answered

§FE.4 said the screens should be buildable from the 0.11 primitives and that *"if one is
[needed], that is a finding worth recording, because it means the Phase 0 set was chosen from
imagination rather than need."*

**No new primitive was needed.** Seven wizard steps, a login form, and three gate states were
built from Button, Input, Select, Checkbox, Alert, and Spinner.

One primitive was **extended**: `Spinner` gained an optional `label`. A `role="status"` element
with no accessible name announces that something changed without saying what, which is worse
than silence because it interrupts. The gates were the first callers to need it — found by a
test failing with *"Found multiple elements with the role status"* when the first attempt
wrapped the spinner in a second status region.

### 8.5 Carried forward

- **`mustChange` is a dead end** (D6). Step 1.11 adds the change-password path; until then a
  user in that state sees an explained stop rather than a silent pass-through.
- **The router has no routes yet.** It is mounted and the future flags are set, but every screen
  in this step is a gate outcome rather than a route. 1.11's admin screens are the first real
  routes — deliberately, since inventing routes now would mean deleting them.
- **The shell is still the 0.11 placeholder** below AuthGate: two tabs, no navigation. 1.11
  replaces it.
- **Still no visual confirmation of the running app** — carried since 0.12, and now the largest
  unverified surface in the project. Every screen here is covered by tests, and none has been
  looked at.
