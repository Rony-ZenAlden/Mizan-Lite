# Step 0.11 — Frontend Foundation (Design + implementation record)

> Status: **APPROVED and IMPLEMENTED.** Design approved 2026-08-04 — all eight decisions
> (D1–D8) accepted as recommended, including the `main.go` restructure in D2.
> Implementation record at **§11**.
> (Permanent Development Protocol, Steps 2–6.)
> Scope: `frontend/src/**` (design tokens, primitives, app shell, providers, the bindings
> wrapper), `internal/api/bindings` (the binding façades), the `ui.theme` setting, and the
> `main.go` / `app.go` restructure that finally gives startup failure and migration progress
> somewhere to be shown.
> **Out of scope:** business screens and the router (no routes exist until Phase 1), the setup
> wizard (Phase 1), the dead-letter diagnostics UI (Phase 9), locale-aware printing and Hijri
> calendars (§22.5, Phase 9), and virtualized tables (they arrive with the first large grid).

Every step from 0.2 to 0.10 built something a user cannot see. **This is the step where the
foundation acquires a face** — and, less obviously, the step that discovers whether the
boundary those ten steps built actually holds when something consumes it.

It already does not. §1.2 is the finding that shapes this design.

---

## 1. ANALYSIS

### 1.1 What exists after 0.1 and 0.8

Less than the step list suggests, and one part of it is wrong.

| Piece | State |
|---|---|
| Design tokens | Real but thin — 9 semantic colours, 2 radii, a font stack, light/dark. No spacing, typography, elevation, or focus-ring tokens. |
| Tailwind wiring | Correct. Every token is exposed as a utility; nothing hardcodes a colour. |
| RTL lint gate | Real and enforced (`eslint.config.js`, `no-restricted-syntax`). |
| i18n | Real: `messages.ts` imports the shared `locales/` catalogs, keys typed off the English catalog, 9 frontend tests. |
| Bindings wrapper | `lib/health.ts` — **one function, and it is broken (§1.2).** |
| Shell | `App.tsx` — a centred card with two toggles. No layout, no providers, no error boundary. |
| Primitives | None. |
| Component tests | None — vitest runs in the Node environment; there is no DOM. |

So §FE's deliverable list is essentially unbuilt, and the two pieces that *are* built — tokens
and the RTL gate — are the two worth keeping.

### 1.2 The defect the envelope change left behind

Step 0.10 changed `App.Health()` to return `envelope.Result[buildinfo.Info]`. The frontend was
not changed with it. [`health.ts`](../../frontend/src/lib/health.ts) still declares:

```ts
Health?: () => Promise<HealthInfo>;      // wrong since 0.10
```

The real payload is `{ok, data, error}`. So in the packaged desktop app `health.version` is
`undefined`, [`App.tsx`](../../frontend/src/App.tsx) renders `?? "…"`, and the shell shows a
row of ellipses that reads as "still loading" forever.

Three things made it invisible, and each is a lesson this step should act on rather than note:

1. **The browser-dev fallback returns the old flat shape**, so `npm run dev` looks correct.
   A mock that does not match the contract is worse than no mock: it actively certifies the
   broken path.
2. **Nothing tests the wrapper.** `make ci` is green.
3. **There is no single unwrap point.** §FE.4 specifies one — "every call goes through one
   place that unwraps the `Result[T]` envelope" — and with one binding nobody built it, so the
   unwrap lived inline in a type annotation, where drift is silent.

This is the frontend's version of the problem the outbox solved on the backend: a boundary is
only as good as the single place that enforces it. **The bindings wrapper (§3) is therefore
the most load-bearing part of this step**, not the primitives.

### 1.3 The boot-ordering problem 0.10 carried forward

[`main.go`](../../main.go) builds the whole graph *before* `wails.Run`, and says why:

> opening the UI first would mean rendering a shell over a database that might be about to be
> restored.

That reasoning is sound and must survive. But it has two consequences 0.10 explicitly handed
to this step (§9.5): a long migration shows **no window at all**, and a failed start **logs and
exits** with nothing on screen. Step 0.4 emits `migrate.Progress` for exactly this purpose and
nothing consumes it.

The tension is real: the graph must not be *usable* before it is built, but the window must
exist before the graph starts building, or there is nowhere to draw. §2 resolves it.

### 1.4 Locale and theme are client-only, and one of them should not be

`App.tsx` holds locale in `useState`. But Step 0.8 made the active language a **setting** —
`ui.locale`, scoped system/company/user, whose write publishes `SettingChangedEvent` on the
domain bus (0.6 D7), and 0.10 stamps it onto every request context.

So today the frontend toggle and the backend's notion of the active language are two unrelated
facts. They have not disagreed yet only because no backend call renders text. The moment one
does — a printed document, a generated filename — they will, and the bug will present as "the
invoice printed in the wrong language", which is a hard thing to trace back to a `useState`.

Theme has no setting at all, so it resets on every launch.

### 1.5 Dependency policy — D1

§FE.2 specifies Radix primitives; §31 adds TanStack Query and Zustand. None are in the npm
cache. Step 0.7 faced the identical question with `robfig/cron` and deferred it, on the
grounds that Phase 0 did not need it and the decision was better made against a real
requirement.

The same test, applied honestly, splits the three:

| Dependency | Real Phase-0 consumer? | Call |
|---|---|---|
| **Radix** | Yes — Dialog focus trapping, Select keyboard navigation and ARIA | **Adopt.** This is the one thing on the list that is genuinely unsafe to hand-roll: accessibility bugs in focus management are invisible to the developer who wrote them and impossible for a reviewer to catch by eye. |
| **TanStack Query** | No — three bindings, none of which needs caching, invalidation, or background refetch | **Defer to Phase 1**, where the first list screen creates the need. |
| **Zustand** | No — locale and theme, which §4 makes *server* state anyway | **Defer to Phase 1.** |
| **Router** | No — there are no routes | **Defer to Phase 1.** |
| **testing-library + jsdom** | Yes — DoD item 8 requires RTL snapshot tests in both directions, which cannot exist without a DOM | **Adopt.** |

Adopting TanStack Query now would mean wiring a `QueryClientProvider` around three calls and
inventing cache keys for data that does not change — the same speculative fiction 0.9 declined
when it refused to invent a currency event with no subscriber. Deferring costs a provider
insertion later, which is one file.

### 1.6 Risks and how the design answers each

| Risk | Consequence if unhandled | Design answer |
|---|---|---|
| The envelope is unwrapped in more than one place | §1.2 recurs, silently, per screen | **One** wrapper; a lint rule forbids `window.go` outside it (§3.4) |
| The dev mock drifts from the real contract | Green tests certifying a broken app | The mock returns a real `Result<T>`, and a test asserts the mock and the Go JSON shape agree (§3.3) |
| Bindings called before the graph is built | Nil dereference in the Go layer, on a customer's first launch | Façades are attached, and every method guards; unattached is a **typed error**, not a panic (§2.3) |
| A shell renders over a half-built database | The failure 0.10's ordering exists to prevent | The shell does not mount until boot succeeds; only the boot screen renders (§2.2) |
| Startup failure shows English prose | Breaks §22.2, the rule the whole envelope exists for | The failure screen renders `code` + `params` through the catalog like any other error (§2.4) |
| Locale toggle diverges from `ui.locale` | Documents print in the wrong language (§1.4) | Locale is read and written through a binding (§4) |
| Hand-rolled focus management | Keyboard and screen-reader users blocked, undetectably | Radix (D1) |
| Tokens grow ad-hoc per screen | The design system decays into ad-hoc Tailwind, which §31 exists to prevent | Token layer completed **before** primitives, contrast asserted by test (§5) |
| RTL regressions | "Retrofitting is weeks of tedious work" (§22.3) | The existing lint gate **plus** both-direction render tests (§9) |

---

## 2. DESIGN — the boot sequence, inverted

### 2.1 The shape

```
main.go   resolve paths (cheap, no database)
          construct binding façades          ← no graph yet; they are empty shells
          wails.Run(Bind: façades)           ← THE WINDOW EXISTS FROM HERE

OnStartup(ctx)
          go bootstrap.Start(...)            ← migrate, seed, scheduler — off the UI thread
             │  migrate.Progress ──► runtime.EventsEmit("boot:progress")
             ▼
          success → bindings.Attach(built) → EventsEmit("boot:ready")
          failure → EventsEmit("boot:failed", code + params + backupPath)

frontend  BootScreen renders from the first paint
          on "boot:ready"  → mount AppShell
          on "boot:failed" → mount BootFailure  (translated; never exits silently)
```

### 2.2 Why this does not reopen the hazard 0.10 closed

0.10's concern was rendering *a shell* over a database that might be about to be restored.
That concern is preserved exactly: **the shell does not mount until `boot:ready`.** What
renders during migration is a boot screen with no data access and no controls.

The window existing is not the hazard; the *shell* existing is. Separating those two is what
lets both properties hold at once — progress is visible, and nothing touches a database that
is mid-restore.

### 2.3 Binding façades — declared in code, attached at startup — D2

Wails binds a fixed `[]any` at `Run` time, so bindings must be constructible **before** the
graph. The resolution is the pattern this project has already used twice:

> The set of bindings is a property of the built binary, not of a runtime object graph.

That is almost verbatim 0.10 §1.1's justification for the settings and jobs declaration
registries, and 0.7 §3.1 applies it to jobs ("declared in code, reconciled at startup"). A
third application is consistency, not novelty.

```go
// internal/api/bindings
type System struct{ mu sync.RWMutex; app *bootstrap.App }   // Health, Boot status
type Config struct{ mu sync.RWMutex; app *bootstrap.App }   // Locale, Theme, SetLocale, SetTheme
type Ops    struct{ mu sync.RWMutex; app *bootstrap.App }   // JobStates, RecentRuns
type Money  struct{ mu sync.RWMutex; app *bootstrap.App }   // Currencies

func All() []any                                  // the static set handed to Wails
func Attach(built *bootstrap.App)                 // called once, on boot success
```

Every method takes the read lock and returns `envelope.Fail(errs.Unavailable(CodeNotReady))`
when `app` is nil. **Unattached is a typed error, never a nil dereference** — the same
judgement as `strategy.Resolve` returning a typed NotFound rather than a zero value (0.5 §5).

**What this defers, deliberately.** `Module.Bindings() any` (0.9 D5) collects a binding
*instance* from each module, which cannot happen before `Run`. In Phase 0 this path is
**unused** — currency's `Bindings()` returns nil, and `App.Currencies()` lives on the app
struct (0.10 §9.3). Reconciling the module contract with static binding therefore belongs to
Phase 1, with the first module that actually ships one. Designing that reconciliation now,
against zero implementors, is the speculation 0.9 D5 declined.

### 2.4 Failure is a screen, not an exit code

On failure `main.go` no longer calls `os.Exit(1)` with a log line. It emits `boot:failed`
carrying the `errs` code, its params, and — for a migration failure — the backup path that
0.4 §4.3 puts on the error. The frontend renders it through the catalog, so the user reads
*"Update could not be applied. Your data is unchanged. A backup is at …"* in their own
language.

`os.Exit(1)` survives for exactly one case: a failure before `wails.Run`, i.e. an unresolvable
data directory. There is genuinely no window then, and pretending otherwise would mean a
second failure path that cannot be tested.

New error codes (`app.not_ready`, `boot.failed`, …) all require catalog entries in both
locales — 0.8's coverage gate will fail the build until they exist, which is the gate working.

---

## 3. DESIGN — the bindings wrapper — D3

The most important file in this step, per §1.2.

### 3.1 One unwrap, one error type

```ts
// frontend/src/lib/wails/call.ts
export class BindingError extends Error {
  readonly code: string;
  readonly messageKey: string;
  readonly params: Record<string, string>;
  readonly fields: FieldError[];
}

export async function call<T>(invoke: () => Promise<Result<T>>): Promise<T>;
```

`call` unwraps `{ok, data, error}`, returns `data` on success, and **throws `BindingError`** on
failure. Every binding module in `lib/wails/` is a thin typed function over `call`; nothing
else in the codebase reads `window.go`.

Throwing rather than returning a result union is deliberate: a component that forgets to check
a returned union renders `undefined` — which is §1.2 exactly. A thrown error hits the nearest
error boundary and is impossible to ignore silently.

### 3.2 The error is rendered from the code, never from prose

`BindingError` carries no message text, because the envelope carries none (§5.4, §22.2). The
UI renders `translate(locale, err.messageKey, err.params)`. Since 0.8's gate guarantees every
code has an entry in every locale, this always produces real text.

### 3.3 The dev mock must satisfy the same contract

The browser fallback (§1.2 lesson 1) is rewritten to return a genuine `Result<T>`, and gains a
test asserting the mock's shape matches what the Go type serialises to. Without that test the
mock is free to drift again, which is the whole failure this step is correcting.

### 3.4 An enforceable boundary

An ESLint `no-restricted-syntax` rule forbids `window.go` outside `src/lib/wails/**` — the
same mechanism as the RTL gate, for the same reason. §1.2 happened because the rule that "every
call goes through one place" was a sentence in a document; this makes it a failing build.

This mirrors 0.5 §11.3's finding: a rule that *looks* like enforcement while enforcing nothing
is worse than none — so this one is verified by planting a violation, as those two were.

---

## 4. DESIGN — locale and theme as settings — D4

Both become server state, read and written through the `Config` binding:

| Setting | Key | Scopes | Notes |
|---|---|---|---|
| Locale | `ui.locale` | system, company, user | **Already declared** (0.8). The frontend stops holding it in `useState`. |
| Theme | `ui.theme` | system, company, user | **New**, `DeclareEnum` over `light` / `dark` / `system`. |

Writing goes through `Settings.Set` inside a Unit of Work, which publishes
`SettingChangedEvent` (0.6 D7) — so "language change without restart" stops being a frontend
trick and becomes the architecture doing what §16.3 designed it to do.

Two honest limits, stated now rather than discovered:

- **Phase 0 has no identity**, so `appctx.Scopes` returns no user (0.10 §6.2) and writes land
  at **system** scope. The per-user preference the scopes allow becomes real in Phase 1 with no
  frontend change.
- **`ui.theme` includes `system`** (follow the OS) because a desktop app that ignores the OS
  theme feels foreign, and adding the value later would mean migrating stored rows.

`system` is resolved in the frontend via `prefers-color-scheme`; the *setting* stores the
user's choice, not the resolved value. Storing the resolved value would freeze the theme at
whatever the OS was on the day it was set.

---

## 5. DESIGN — completing the token layer — D5

§FE.1 requires tokens **before** primitives, because "retrofitting a design system is a
rewrite" (§31). Today's nine colours are a good start and an incomplete system. Added:

| Group | Tokens |
|---|---|
| Spacing | a 4px-based scale bound to Tailwind's spacing, so no component invents a margin |
| Typography | size/line-height/weight pairs (`--text-xs` … `--text-2xl`), with the Arabic line-height adjustment already in `index.css` generalised per step |
| Elevation | `--shadow-sm/md/lg`, defined separately per theme — a shadow tuned for light backgrounds reads as a smudge on dark |
| Focus | `--ring`, `--ring-offset` — one focus treatment everywhere, keyboard-visible |
| State | `--color-warning`, `--color-info`, and `-subtle` background variants for status surfaces |

**A contrast gate.** A test asserts every foreground/background token pair meets WCAG AA
(4.5:1 for body text, 3:1 for large text and UI borders) **in both themes**. This is the same
species of check as 0.8's error-code coverage gate: a completeness property over data, cheap to
assert, and otherwise discovered by a customer squinting at a receipt total. Dark-theme
contrast in particular is where hand-picked palettes fail, and no reviewer catches it by eye.

---

## 6. DESIGN — primitives — D6

Radix-based, token-styled, in `src/shared/ui/`:

`Button` · `Input` · `Select` · `Checkbox` · `Switch` · `Dialog` · `Sheet` · `Toast` ·
`Tooltip` · `Tabs` · `Table` · `EmptyState` · `Spinner` · `Skeleton` · `Alert`

§FE.2 and §31 require each to have designed **empty / loading / error / permission-denied**
states "from the start", because that is "where professional is actually decided". Two of the
four need care in Phase 0 and two do not yet have a consumer:

- `EmptyState`, `Spinner`, `Skeleton`, `Alert` are the primitives those states are built from,
  and they ship complete.
- **Permission-denied** has no producer until RBAC exists (Phase 1). It ships as a variant of
  `EmptyState` with its own icon and code-driven message, so Phase 1 wires a consumer rather
  than inventing a pattern. Building the *variant* costs nothing; building a *permission
  system* to feed it would be Phase 1 work done blind.

No component hardcodes a colour, spacing value, or physical direction — enforced by the token
layer and the existing lint gate respectively.

---

## 7. DESIGN — the app shell and the job status panel — D7

### 7.1 Shell

A real layout, not a centred card: a sidebar (empty nav region, ready for Phase 1 modules), a
header carrying the locale and theme controls, and a content region. Providers, in order:

```
ErrorBoundary → LocaleProvider (lang/dir on <html>) → ThemeProvider → ToastProvider → Shell
```

`ErrorBoundary` is outermost so a `BindingError` thrown from any screen renders a translated
error state rather than a blank window — the white-screen failure mode that makes a desktop app
feel broken beyond recovery.

### 7.2 The job status panel

DoD item 5 requires a durable job to "report status in the UI panel", and §FE names it in the
Phase 0 deliverable. The backend query API exists and is tested (0.7 §7):
`Scheduler.JobStates` and `Scheduler.RecentRuns`.

The `Ops` binding exposes both as DTOs — flat, `_at` fields as ISO strings, never domain types
(§5.4). The panel lists each job with its schedule, last outcome, and next run, plus recent
runs for the selected job. `RunNow` exists on the scheduler and is **not** exposed: a manual
trigger is an operator action that wants a permission (Phase 1) and a confirmation, and
shipping it unguarded now would be a control we would have to take away.

This is also the first screen that exercises the four states for real — loading (skeleton),
error (`BindingError`), empty (no runs yet), and populated.

---

## 8. DESIGN — layout

```
internal/api/bindings/            # the static façades + Attach (§2.3)
├── bindings.go                   # All(), Attach(), the not-ready guard
├── system.go  config.go  ops.go  money.go
└── dto.go                        # wire shapes; no domain types

frontend/src/
├── app/
│   ├── boot/                     # BootScreen, BootFailure, the event subscription
│   ├── providers/                # ErrorBoundary, Locale, Theme, Toast
│   └── shell/                    # AppShell, Sidebar, Header
├── shared/
│   ├── ui/                       # the primitives (§6)
│   └── hooks/                    # useLocale, useTheme, useAsync
├── lib/wails/                    # call.ts + one typed module per binding — THE ONLY window.go
├── i18n/                         # messages.ts (unchanged) + useTranslation
└── modules/ops/                  # the job status panel (§7.2)
```

`lib/health.ts` is deleted; its replacement is `lib/wails/system.ts`.

---

## 9. TESTING PLAN (Protocol Step 4)

| Level | Tests |
|---|---|
| **Bindings wrapper** | `call` returns `data` on `ok:true`; **throws `BindingError` on `ok:false`** carrying code and params; a missing bridge is a typed error, not a crash; **the regression drill: a `Result`-shaped Health response renders a real version string** — the test §1.2 did not have. |
| **Mock/contract parity** | The dev mock's shape matches the Go type's JSON (asserted against a fixture the Go side generates), so §1.2's second cause cannot recur. |
| **Boundary lint** | Planting `window.go` outside `lib/wails/` fails lint — **verified by planting**, per 0.5 §11.3. |
| **Façades (Go)** | Every method returns a typed not-ready error before `Attach` and real data after; `Attach` is safe under `-race`; **no method panics on a nil graph** (table test over all of them). |
| **Boot sequence (Go)** | Progress events are emitted in phase order; success emits `boot:ready`; an injected migration failure emits `boot:failed` with the code **and the backup path**, and **no English prose** (the 0.10 envelope assertion, applied to boot). |
| **Tokens** | Every foreground/background pair meets WCAG AA **in both themes** (§5); every token referenced by a primitive is declared. |
| **Primitives** | Render in both themes; Dialog traps focus and restores it on close; Select is keyboard-navigable; every primitive is reachable by keyboard with a visible focus ring. |
| **RTL — the gate** | Both-direction render snapshots for the shell and every primitive; `<html dir>` follows locale; the existing physical-class lint rule stays green. |
| **Locale/theme** | Toggling locale calls the binding and updates `<html lang/dir>` **with no reload**; a failed write surfaces as a toast and **does not** leave the UI showing a language the backend did not accept; theme `system` follows `prefers-color-scheme`. |
| **Job panel** | Loading, error, empty, and populated states each render; DTO mapping is exercised against a real scheduler in Go. |
| **i18n** | Existing parity and coverage gates stay green with the new keys (they will fail until the new codes are translated — that is the gate working). |

**Mutation-verified**, per the pattern established in 0.4–0.8:

- *The unwrap* — make `call` return the envelope instead of `data` → the Health regression
  drill must fail. This is the mutation that reproduces §1.2, and it is the one I most want to
  see fail, because the bug shipped once already.
- *The not-ready guard* — remove the nil check from one façade → the pre-`Attach` test must
  fail rather than panic.

---

## 10. DECISIONS REQUESTED

1. **D1 — Adopt Radix and testing-library/jsdom; defer TanStack Query, Zustand, and the
   router to Phase 1**, where a real list screen and real routes create the need. Accessibility
   is the one item genuinely unsafe to hand-roll. *(Confirmed in discussion; §1.5.)*
2. **D2 — Invert the boot sequence**: the window opens first, the graph builds in `OnStartup`,
   and bindings are static façades attached on success. The shell still does not mount until
   the graph is ready, so 0.10's ordering guarantee is preserved. *(Recommended; §2 — **this is
   the decision I most want confirmed**, because it changes `main.go` and touches the one
   ordering rule 0.10 was built to protect.)*
3. **D3 — One bindings wrapper that unwraps the envelope and throws `BindingError`**, with an
   ESLint rule forbidding `window.go` anywhere else, verified by planting a violation. Fixes
   §1.2 and prevents its recurrence. *(Recommended; §3.)*
4. **D4 — Locale and theme become settings** (`ui.locale` exists; `ui.theme` is new, with a
   `system` value), read and written through a binding. Phase 0 writes land at system scope
   until identity exists. *(Recommended; §4.)*
5. **D5 — Complete the token layer before building primitives**, and add a **WCAG AA contrast
   gate** asserted in both themes. *(Recommended; §5.)*
6. **D6 — The fifteen primitives listed**, with permission-denied shipping as an `EmptyState`
   variant awaiting its Phase-1 producer. *(§6.)*
7. **D7 — Ship the job status panel** over the existing `JobStates`/`RecentRuns` API, closing
   DoD item 5. `RunNow` is **not** exposed until it can be permission-gated. *(§7.2.)*
8. **D8 — `Module.Bindings()` reconciliation is deferred to Phase 1**, with the first module
   that actually ships a binding. The path is unused in Phase 0. *(§2.3.)*

On approval I'll implement in this order, each independently reviewable: token layer +
contrast gate → `lib/wails/call.ts` + the Health regression drill (the §1.2 fix lands first) →
binding façades + `Attach` + the not-ready guard → boot inversion in `main.go`/`app.go` +
progress and failure events → BootScreen / BootFailure → providers + shell → primitives →
`ui.theme` setting + locale/theme through bindings → the job status panel → the RTL and
mutation drills, tests alongside each, then the self-review and improvement notes
(Protocol Steps 5–6).

---

## 11. IMPLEMENTATION RECORD (Protocol Steps 3–6)

All eight decisions were implemented as designed. Implementation order was adjusted at the
reviewer's instruction: the §1.2 fix and the bindings wrapper landed **first**, before the
token layer, so no cosmetic work was built on a broken boundary.

### 11.1 The defect was worse than §1.2 recorded

§1.2 found one stale type annotation. Writing the contract fixture found a **second, latent
instance of the same class**: `envelope.Result.Data` carried `json:"data,omitempty"`, and
`encoding/json` omits an empty slice while never omitting a struct.

So the wire shape depended on `T`. `Health()` (a struct) always sent `data`; a list binding
with zero rows sent `{"ok":true}` with **no data key at all**, and the frontend would have
received `undefined` where it expected `[]` and crashed on `.map()`. `Currencies()` was one
empty database away from it.

A boundary whose shape varies with its payload type cannot be consumed by one generic unwrap,
which is the entire point of the envelope. `omitempty` is gone from `Data`; the cost is
`"data":null` on failure results, which no caller reads.

The lesson generalises past this step: **§1.2 was not a mistake, it was a missing gate.** One
stale annotation was the visible instance; the gate now covers the class.

### 11.2 A real accessibility defect, found by a test rather than by review

`TestDialog > moves focus into the dialog and restores it on close` failed with focus on
`<body>`. It was not a jsdom artifact.

Radix returns focus to `Dialog.Trigger` on close. These dialogs are **controlled** and opened
programmatically — from a row action, a menu item, a shortcut — so there is no Trigger,
`triggerRef` is null, and Radix drops focus to the document. A keyboard user pressing Escape
would be silently returned to the top of the page.

This is exactly the class of defect D1 adopted Radix to avoid, and it still occurred, because
the guarantee holds only for the usage Radix expects. Requiring a `Trigger` would have banned
programmatic opens, so `useFocusRestore` captures the previously-focused element on open and
restores it on close. **Invisible to a mouse user, and no reviewer would have caught it.**

### 11.3 The i18n orphan gate had a latent flaw that this step triggered

Adding `app.not_ready` (a genuine Go constant) made `app` an "error prefix" in Step 0.8's
orphan gate, which then swept in `app.title` and `app.tagline` — UI vocabulary from
`common.json` that will never have a Go constant. `make ci` failed.

The gate derived its scope from a **prefix heuristic** over the merged catalog. It now reads
`errors.json` directly, which is both simpler and exact, plus an explicit
`frontendOwnedCodes` allowance for the four codes the TypeScript transport layer produces —
each cross-referenced to `call.ts` and covered by its own gate on the TypeScript side, so the
allowance is not a hole.

Worth recording because the gate was doing its job: it failed loudly on a real ambiguity rather
than quietly accepting a namespace collision.

### 11.4 A bug in the first cut of `Config.SetLocale`, caught while writing it

`read()` reused the context the write had used. `app.Context()` stamps the resolved locale onto
the context, and `i18n.Active` prefers a stamped non-default locale over the setting — so
switching **ar→en would have echoed "ar" back**, and the UI would have reverted to Arabic
immediately after a successful switch. (en→ar worked by accident, because "en" *is* the
default and falls through to the setting.) The read now derives a fresh context.

### 11.5 Deviations from the design

- **`ErrorBoundary` reads the locale from `<html lang>`, not from a prop.** The design passed it
  in; implementing it exposed the circularity — the boundary must render when the provider tree
  it guards is what failed, so it cannot depend on that tree for its language.
- **A `PreferencesProvider`, not separate Locale and Theme providers.** Both come from one
  binding call; splitting them would mean two round trips for two fields of the same row.
- **`initialLocale()` from the OS.** The design did not say what language the *boot screen*
  speaks. It cannot ask the backend — `Config` is guarded until boot completes — so it uses
  `navigator.language`, which on a genuine first launch is also the right answer: no preference
  is stored yet, and a Syrian shop should not be migrated in English.
- **No new spacing scale.** §5 proposed one; Tailwind's default is already 4px-based, and a
  parallel scale would give every component two vocabularies for the same idea. Recorded rather
  than silently skipped.

### 11.6 Verification

**121 frontend tests + 23 binding subtests, `-race` clean, `make ci` green** (build · archlint ·
vet · tests · golangci-lint · frontend typecheck/lint/test).

| Package | Coverage |
|---|---|
| `api/envelope` | 100% |
| `api/bindings` | 89.2% |
| `platform/i18n` | 87.4% |
| `bootstrap` | 68.2% |

**Mutation-verified**, per §9:

- *The unwrap* — returning the envelope instead of `raw.data` fails the regression drill with
  `expected undefined to be '1.2.3'`, which is the shipped bug reproduced exactly.
- *The not-ready guard* — removing it from `Money.Currencies` fails with
  `panicked on a nil graph: invalid memory address or nil pointer dereference` — the
  crash-on-first-launch the guard exists to prevent.
- *The `window.go` boundary* — a planted `window.go` in `App.tsx` fails lint (verified by
  planting, per 0.5 §11.3).
- *The contrast gate* — the classic dark-theme trap (a saturated mid-blue fill with white text)
  fails at **3.68:1**, the exact figure the token comment predicts.
- *The orphan gate* — a planted `sales.invoice.obsolete_code` fails it by name.

### 11.7 Carried forward

- **Step 0.12** is the Phase 0 Definition-of-Done review. DoD items 1, 5, and 8 are closed by
  this step; item 1 wants a real `wails dev` run, which needs the `wails` CLI (`make tools`) —
  the Go and frontend halves are proven separately but have not been run **together** in a
  window on this machine.
- **`Module.Bindings()` reconciliation** (D8) lands in Phase 1 with the first module that ships
  a binding. The path is unused today.
- **TanStack Query, Zustand, and the router** (D1) arrive with the first real list screen and
  the first real route, in Phase 1.
- **`RunNow`** is deliberately unexposed until it can be permission-gated (Phase 1).
- **Virtualized tables** arrive with the first grid large enough to need them; `Table` is the
  seam.
- **Pre-existing dev-tooling advisories**: `npm audit` reports 6 issues in vite/vitest/esbuild/
  brace-expansion. All are dev-only and none was introduced by this step's dependencies; the
  esbuild one affects the dev server, which an offline desktop build does not ship. Worth a
  deliberate toolchain bump in its own change rather than as a side effect of this one.

---

*End of Step 0.11. Design approved, implemented, self-reviewed.*
