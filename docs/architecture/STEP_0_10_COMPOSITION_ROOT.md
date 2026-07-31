# Step 0.10 — Composition Root & Full Graph Wiring (Design + implementation record)

> Status: **APPROVED and IMPLEMENTED.** Design approved 2026-07-31 — all eight decisions
> (D1–D8) accepted as recommended. Implementation record at **§9**.
> (Permanent Development Protocol, Steps 2–6.)
> Scope: `internal/bootstrap`, `internal/platform/paths`, the module registry in
> `internal/platform/modules`, `internal/api/envelope`, `internal/api/appctx`, and rewriting
> `main.go` / `app.go` to start through the composition root.
> **Out of scope:** the frontend shell, design tokens, and the bindings wrapper (Step 0.11);
> the setup wizard (Phase 1); real module bindings beyond the health probe.

Every step so far built a component and proved it in isolation. **This is the step where they
have to start together, in the right order, on a real machine — and stop again without losing
anything.**

That makes the two interesting questions ordering and failure. A graph wired in the wrong
order fails in ways unit tests cannot see: a scheduler started before its subscribers are
registered will dispatch events to handlers that do not exist yet, and skip them.

---

## 1. ANALYSIS

### 1.1 Manual wiring, and why

ARCHITECTURE_v1 §6 is unambiguous: **manual constructor injection, one composition root, no
container, no reflection, no code generation.**

The reasoning is worth restating because it is the opposite of what a large Go codebase is
often told to do: a reflection container turns a missing dependency into a **runtime panic
during startup on a customer's machine**, which is precisely the class of failure a desktop
ERP cannot afford. Explicit wiring in one file is boring, greppable, and checked by the
compiler.

The rules that come with it — no globals, no `init()` side effects, no singletons — have been
honoured throughout: every component so far takes its dependencies as constructor arguments.
The one deliberate exception is the settings/jobs **declaration** registries, which are
package-level because the set of declared settings is a property of the built binary, not of a
runtime object graph.

### 1.2 The order, and what each step depends on

§BOOT gives the sequence. What matters is *why* each edge exists:

| Step | Must come after | Because |
|---|---|---|
| paths | — | everything below needs to know where the database is |
| logging, clock | paths | logs need a directory |
| database | paths | the DSN is a file path |
| **migrate** | database | and **before everything else** — no other component may touch a schema that has not been brought up to date |
| eventbus, outbox | migrate | the outbox writes to tables `0001`/`0002` create |
| settings | migrate, eventbus | it reads its table and publishes `SettingChanged` |
| i18n | settings | the active locale is a setting |
| modules | all platform services | a module's constructor takes them |
| **subscriptions** | modules | every handler must be registered… |
| **scheduler start** | subscriptions | …**before** the dispatcher runs, or deliveries are skipped as "no registered handler" (0.6) |
| seeding | modules | seeds come from `Module.Metadata()` |
| bindings | modules | collected from `Module.Bindings()` |

The subscriptions→scheduler edge is the one a careless implementation gets wrong, and §7's
integration test exists to catch it.

### 1.3 What does not exist yet

Two gaps this step has to fill, both currently implicit:

- **Nobody knows where the database lives.** `database.Config.Path` is required, and its
  doc comment says it "should live under the OS app-data directory (resolved by
  platform/fs)" — a package that does not exist. Tests have been passing `t.TempDir()`.
- **`main.go` builds nothing.** It constructs an `App` with a health probe and no graph.

### 1.4 A rule that has been applied consistently and should now be written down

Across five steps, two kinds of problem have been treated very differently:

| | Fatal at startup | Reported, not fatal |
|---|---|---|
| Kind | A **code** defect | A **data** leftover |
| Examples | duplicate setting key (0.5), duplicate job key (0.7), duplicate migration version (0.4), malformed catalog (0.8) | unknown stored setting key (0.5, D3), orphan job row (0.7), unknown stored flag (0.5) |
| Why | Can only reach a customer through a bad build; the developer must see it on their own machine | The row is inert; refusing to start would keep a shop closed over something harmless |

**I propose stating this explicitly as the startup-validation rule (D3)**, because the
composition root is where it is enforced and because the next person adding a check will need
to know which side of the line they are on.

### 1.5 Risks and how the design answers each

| Risk | Consequence if unhandled | Design answer |
|---|---|---|
| Scheduler starts before subscribers register | Events dispatched to "no registered handler" and skipped — silent data loss | Strict ordering + an end-to-end test (§2, §7) |
| Bootstrap continues after a failed migration | Every later step operates on a **closed** `Store` (0.4's postcondition) | Migration failure is fatal and structured (§4.1, **D2**) |
| Seeding runs before migration | No tables | Order; and seeding is driven from the same sequence |
| Data directory unwritable | A panic on a customer's first launch | Checked and reported as a typed error (§3) |
| Shutdown hangs | The app will not close; the user force-quits mid-write | Every drain step is bounded (§5) |
| Shutdown treated as a correctness guarantee | A power cut loses data | Durability lives in the outbox, job reclaim, and WAL — shutdown is politeness (§5) |
| A module cycle | Infinite construction loop, or an arbitrary order | Topological sort with cycle detection, fatal (§4.2) |
| Two modules claim one setting key | Whichever wins depends on init order | Cross-module key validation, fatal (§4.2) |

---

## 2. DESIGN — the sequence

```go
// internal/bootstrap
func Start(ctx context.Context, opts Options) (*App, error)
func (a *App) Shutdown(ctx context.Context) error
```

```
 1. paths           resolve + create the data directory
 2. logging, clock
 3. database        writer + reader pools, PRAGMAs
 4. migrate         merge platform + module migrations, run with the safety protocol
 5. eventbus        in-process domain bus
 6. outbox          store + subscriber registry + dispatcher
 7. settings        registry validation, load snapshot, bus-backed notifier
 8. i18n            catalog + user-content translations
 9. jobs            scheduler (NOT started)
10. modules         construct in topological order, validate the set
11. wiring          subscriptions, then job registration
12. seeding         module metadata, idempotently
13. scheduler.Start reconcile, reclaim, begin ticking
14. bindings        collect non-nil Module.Bindings() + the health probe
```

`App` holds what the shell needs — the bindings slice and the shutdown hooks — and nothing
else. There is no service locator: a component that needs another got it in its constructor.

---

## 3. DESIGN — `platform/paths`

```go
type Paths struct {
    Data    string // the app-data root
    DBFile  string // Data/mizan.db
    Backups string // Data/backups   — the migration safety layer writes here
    Logs    string // Data/logs
}

func Resolve(appName string) (Paths, error)
```

Per OS, stdlib only (`os.UserConfigDir`, `os.UserHomeDir`):

| OS | Location |
|---|---|
| macOS | `~/Library/Application Support/Mizan` |
| Windows | `%AppData%\Mizan` |
| Linux | `$XDG_DATA_HOME/mizan`, else `~/.local/share/mizan` |

**`MIZAN_DATA_DIR` overrides it**, which is what makes a portable install, a second test
instance, and a developer's throwaway database possible without touching code.

Directories are created with `0o700` and **write access is verified at startup**, because
"cannot write to the data directory" discovered at the first sale is far worse than at launch.

---

## 4. DESIGN — the module registry

### 4.1 Migration failure is fatal — D2

Step 0.4 established the postcondition, and Step 0.9 depends on it: when a migration fails and
the database is restored, **the `Store` is closed**. Every subsequent step in the sequence
would then be operating on a closed handle.

So `Start` returns a typed fatal error carrying the failed migration and **the backup path**,
and builds nothing further. It deliberately does **not** reopen and continue: a database that
has just failed a migration is a database a human should look at, and the message the user
needs is "your data is safe, here is where" — not a half-started application.

### 4.2 Validating the module set

```go
func Validate(mods []Module) error   // in platform/modules
func Order(mods []Module) ([]Module, error)
```

Three checks, all **fatal** (code defects, per D3):

- **Dependency cycles** — topological sort; a cycle names the modules in it. §6 is explicit
  that the fix for a cycle "is an event, never a shared mutable singleton", so the error says
  so.
- **Unknown dependency** — a module depending on one that is not registered.
- **Duplicate setting or flag keys across modules** — the config registry already rejects
  duplicates within a process, but this reports it as *which two modules collided*, which is
  the useful form.

Migration version collisions are already caught by `migrate.Load` (0.9), and deliberately stay
there so a platform/module clash and a module/module clash produce the identical error.

With one module the sort is trivial. It is built now because §MOD requires the contract
finalised in Phase 0, and because a topological sort written against one node is easier to get
right than one retrofitted against twelve.

---

## 5. DESIGN — shutdown

```
scheduler.Stop(grace)     stop claiming, drain in-flight, mark survivors cancelled
outbox final DispatchOnce bounded; deliver what is already committed
settings/i18n             nothing to flush
database.Close()          WAL checkpoint, close both pools
```

Each step is bounded. If a drain exceeds its budget the app still exits — an ERP that will not
close is a worse bug than one that leaves a job to be re-run.

**Framing that matters:** shutdown is *politeness, not a durability mechanism.* A power cut or
a force-quit skips all of it, and the system is still correct — because the outbox is
committed with its business transaction (0.6), abandoned job runs are reclaimed on next start
(0.7), and the WAL recovers (0.3). Shutdown makes the next launch faster and quieter; it is
never what makes it correct. Anything that *would* depend on graceful shutdown for correctness
is a design error, and stating this now is what stops one being introduced later.

---

## 6. DESIGN — the API layer

### 6.1 The result envelope

§5.4's contract, built now with one real consumer rather than none:

```go
// internal/api/envelope
type Result[T any] struct {
    OK    bool      `json:"ok"`
    Data  T         `json:"data,omitempty"`
    Error *APIError `json:"error,omitempty"`
}

type APIError struct {
    Code       string            `json:"code"`
    MessageKey string            `json:"messageKey"`
    Params     map[string]string `json:"params"`
    Fields     []FieldError      `json:"fields"`
}

func Ok[T any](v T) Result[T]
func Fail[T any](err error) Result[T]   // maps kernel/errs → APIError
```

`Fail` maps an `errs.Error` straight through: `Code` and `Params` are already the shape the
frontend needs, and Step 0.8 guaranteed every code has a translation. **No English prose
crosses the boundary** — that rule is what makes language switching complete rather than
partial, and the envelope is where it is enforced rather than merely intended.

`App.Health()` becomes its first consumer, so the whole path is proven end-to-end before any
module has a screen.

### 6.2 `api/appctx`

Stamps the request context: locale from the resolved `ui.locale` setting (closing the item 0.8
carried forward), and correlation id for the event chain (0.6).

It also implements `config.ScopeProvider` — returning **no** company/branch/user, because
identity does not exist until Phase 1. That is the honest Phase-0 answer, and it is a
constructor argument to swap rather than a code change.

---

## 7. TESTING PLAN (Protocol Step 4)

| Level | Tests |
|---|---|
| **paths** | Resolves under a temp `MIZAN_DATA_DIR`; creates the tree; an unwritable directory is a typed error, not a panic; the override wins over the OS default. |
| **Registry** | Topological order respects `DependsOn`; a cycle (A→B→A, and self-dependency) is a fatal error naming the modules; an unknown dependency is fatal; duplicate setting keys across two modules are fatal and name both. |
| **Boot — the happy path** | `Start` on an empty data dir produces a running app: migrations applied (platform + currency), seeds present, settings readable, the heartbeat job registered, bindings collected. |
| **Boot is idempotent** | A second `Start` on the same directory applies no migrations, re-seeds nothing, and starts clean — the "customer reopens the app" case. |
| **Ordering — the drill** | An integration test publishes an outbox event through a Unit of Work and asserts the scheduled dispatcher **delivers** it. If the scheduler were started before subscriptions were registered, the delivery is skipped as "no registered handler" and this fails. |
| **Migration failure** | A deliberately broken migration makes `Start` return a fatal error naming the backup path, with no partially-built graph and no goroutines left running. |
| **Shutdown** | `Shutdown` drains the scheduler (nothing left `running`), flushes pending outbox deliveries, and closes the database; a second `Shutdown` is safe; a slow job does not prevent exit. |
| **Envelope** | `Ok` carries data; `Fail` maps an `errs.Error` to code + params and **contains no English prose**; a non-`errs` error still produces a usable code. |
| **appctx** | Locale is stamped from the setting and overridable per request; `ScopeProvider` returns no scopes in Phase 0. |

**Mutation-verified**: *ordering* (start the scheduler before registering subscriptions → the
delivery drill must fail) and *cycle detection* (skip the visit check → the cycle test must
hang or pass wrongly, so the test asserts a bounded failure).

---

## 8. DECISIONS REQUESTED

1. **D1 — `platform/paths` resolves the OS app-data directory**, with `MIZAN_DATA_DIR` as an
   override, `0o700` permissions, and a startup write check. *(§3.)*
2. **D2 — A failed migration is a fatal startup error** carrying the backup path; bootstrap
   does not reopen and continue. *(§4.1.)*
3. **D3 — Write down the startup-validation rule**: code defects are fatal, data leftovers are
   reported. It has been applied consistently since 0.4; the composition root is where it
   becomes explicit. *(§1.4.)*
4. **D4 — Seed on every boot, with no first-run flag.** The 0.5 seeder is idempotent and 0.9
   proved it, so a flag would add state that can only get out of sync — and always seeding
   self-heals a deleted system row. *(Recommended; §2 step 12.)*
5. **D5 — Strict ordering: subscriptions before `scheduler.Start`**, with the delivery drill
   as the regression test. *(§1.2, §7.)*
6. **D6 — Shutdown is bounded and best-effort, never a durability mechanism.** Correctness
   comes from the outbox, job reclaim, and the WAL. *(§5.)*
7. **D7 — Build `api/envelope` now with `App.Health()` as its first consumer**, so the
   no-English-prose boundary is proven before any module has a screen. *(§6.1.)*
8. **D8 — `api/appctx` stamps locale and correlation id**, and implements `ScopeProvider`
   returning no scopes until identity exists in Phase 1. *(§6.2.)*

On approval I'll implement in this order, each independently reviewable: `platform/paths` →
module registry validation + ordering → `api/envelope` → `api/appctx` → `bootstrap.Start`
(sequence) → shutdown → `main.go`/`app.go` over the composition root → the ordering and
migration-failure drills, tests alongside each, then the self-review and improvement notes
(Protocol Steps 5–6).

---

## 9. IMPLEMENTATION RECORD (Protocol Steps 3–6)

### 9.1 The ordering drill does not prove what its name claimed

§7 promised a mutation-verified ordering test. It is **not** verified, and the reason matters
more than the omission.

The test registers its observer *after* `Start` returns, so it exercises whether the outbox
store, subscriber registry, dispatcher, and scheduler share one graph — which it does prove,
end to end, through the scheduled job rather than a direct dispatcher call. But **no Phase-0
module registers a subscription**: currency's `Subscribe` returns nil (0.9, D8). So reordering
`Subscribe` and `scheduler.Start` inside `bootstrap.go` changes nothing observable, and the
first mutation attempt failed for an unrelated reason — it double-registered the dispatch job
and tripped `jobs.duplicate_key`.

The test is renamed `TestOutboxIsDeliveredThroughTheScheduledJob`, and its comment now states
the limit. The ordering itself is enforced by the sequence in `bootstrap.go` and becomes
mutation-testable the moment a module subscribes for real — accounting, in Phase 2.

Second step running into the same wall (0.9 §9.4 was the first). The pattern is worth naming:
**a guarantee with no consumer cannot be mutation-tested**, and pretending otherwise dilutes a
signal that has genuinely caught bugs.

*Cycle detection is verified*: disabling the `visiting` guard makes `TestOrderDetectsCycles`
die with `fatal error: stack overflow` — an infinite recursion, exactly what the guard exists
to prevent.

### 9.2 A test caught an error that could not be read

`TestValidateRejectsDuplicateSettingKeysAcrossModules` asserts the message names both
colliding modules. It failed — because `errs.Error.Error()` renders Category, Code, and
Message but **not Params**, and the module names were carried only as parameters.

That is fine for errors crossing the API boundary, where the frontend reads `Params`
structurally. It is useless for a **startup error a developer reads in a log**. The detail now
goes in the message text for these code-defect errors, which never reach a customer by
definition (D3).

A general lesson for the error taxonomy: *where* an error is going determines whether params
are enough.

### 9.3 Two bindings, not one

The design named `App.Health()` as the envelope's first consumer. `App.Currencies()` was added
alongside it, because Health proves only the envelope's happy path — it touches no context, no
settings, no module, and no translation.

`Currencies()` exercises the whole graph in one call: the per-request context, the
settings-bound locale, the currency module, and the i18n resolver. It is also the first real
DTO boundary — `CurrencyDTO` rather than `domain.Info`, per §5.4's rule that domain types
never reach JavaScript.

### 9.4 Verification

**38 tests, `-race` clean, `make ci` green.** `api/envelope` 100%, `platform/modules` 95.6%,
`bootstrap` 70.3%, `platform/paths` 64.9%.

The bootstrap suite boots a **real graph in a temporary data directory** — migrations merged
across platform and module, seeds applied, settings loaded, jobs reconciled, scheduler
constructed — and then shuts it down. What it covers:

- a second boot on the same directory is clean and re-seeds nothing;
- deleting a seeded system currency and rebooting **restores it** (the reason D4 has no
  first-run flag);
- a corrupt database file makes `Start` return a typed fatal error and **no partial graph**;
- shutdown leaves no job run `running`, flushes pending outbox deliveries, and is safe twice;
- a currency conversion works through the built graph, with locale and correlation id stamped.

### 9.5 Carried forward

- **Step 0.11** builds the frontend shell over these bindings, and gives the startup-failure
  path and the migration `Progress` events somewhere to be shown. Today a failed start logs
  and exits, which is honest but not friendly.
- **Phase 1** replaces `appctx.Scopes` with a session-reading implementation and adds
  `Permissions()` to the `Module` interface.
- **Phase 2** is when the subscribe-before-start ordering becomes mutation-testable (§9.1).
- The **module construction order** in §6 lists a dozen modules; the topological sort handles
  them without a hardcoded list when they arrive.

---

*End of Step 0.10. Design approved, implemented, self-reviewed.*
