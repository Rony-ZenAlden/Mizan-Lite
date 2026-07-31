# Step 0.7 — Durable Job Scheduler & Outbox Dispatch Job (Design + implementation record)

> Status: **APPROVED and IMPLEMENTED.** Design approved 2026-07-30 — all seven decisions
> (D1–D7) accepted as recommended, with `MaxCatchUp` made **configurable per job** at the
> reviewer's request. Implementation record at **§11**.
> (Permanent Development Protocol, Steps 2–6.)
> Scope: `internal/platform/jobs` — declaration, reconciliation, scheduling, execution,
> run recording — plus the two jobs Phase 0 registers.
> **Out of scope:** the job-monitoring UI panel (Phase 9 diagnostics; this step provides the
> query API it will read), the business jobs from §24.3 (rate fetch, backup, balance rebuild —
> they arrive with their modules), and Wails progress wiring (Step 0.10).

ARCHITECTURE_v1 §24.1 gives the reason this cannot be an in-memory ticker: **"a desktop app is
closed every evening, so in-memory scheduling loses work."** A server process runs for months;
Mizan is switched off at 8pm and opened at 8am, and anything scheduled in between simply did
not happen.

§24.2 gives the reason runs are recorded: **"invisible background failures are how customers
lose backups without knowing."**

---

## 1. ANALYSIS

### 1.1 What already exists

Like 0.6, most of the schema landed in `0001_platform.sql`:

```
jobs      (id, job_key UNIQUE, schedule_cron, is_enabled, is_singleton,
           catch_up_policy ∈ (run_once|skip|run_all), timeout_seconds, max_attempts,
           last_run_at, next_run_at, created_at, updated_at, row_version)
job_runs  (id, job_id → jobs(id), started_at, finished_at,
           status ∈ (running|succeeded|failed|timeout|cancelled),
           attempt, error, output_json, triggered_by ∈ (schedule|manual|startup))
ix_jobs_due (is_enabled, next_run_at)      ix_job_runs_job (job_id, started_at)
```

`outbox.Dispatcher.DispatchOnce` was deliberately built in 0.6 as a callable unit with no
loop of its own, precisely so this step supplies the loop. **No migration is required** — see
§4.2, which is the one place that was nearly untrue.

### 1.2 The desktop scheduling problem, stated precisely

| Server assumption | Desktop reality |
|---|---|
| The process runs continuously | It runs 09:00–20:00, if the shop opens that day |
| A missed tick is an incident | A missed tick is *every night*, by design |
| Time moves forward smoothly | The laptop sleeps, resumes, crosses DST, and the clock can be corrected backwards |
| Someone watches the logs | Nobody does |

The consequence that shapes the design: **"what should happen to occurrences that were missed
while the app was closed?" is the central question**, not an edge case. §24.1 answers it with a
per-job **catch-up policy** rather than a blanket rule — "a missed backup should run; three
missed rate fetches should collapse to one."

### 1.3 Risks and how the design answers each

| Risk | Consequence if unhandled | Design answer |
|---|---|---|
| App closed for a month, `run_all` job | 43,000 queued dispatches on open; the POS is unusable at 8am | Catch-up is **bounded** with a logged warning (§5.3, **D4**) |
| Two instances run the same job | Two backups over one file; two rate fetches | CAS claim on `next_run_at`, plus a run lease (§4.2, **D2**) |
| Crash mid-run | `job_runs` row stuck `running` forever; the job never runs again | Startup reclaim of expired runs (§6.3, **D5**) |
| A job hangs | A worker is consumed permanently; the queue stalls | Per-job `timeout_seconds` via context (§6.2) |
| Long job blocks the UI | "A POS that freezes during a report is not acceptable" (§24.4) | Jobs run on the pool, never the UI thread; progress callback (§6.4) |
| Job removed from code, row remains | Scheduler tries to run a handler that no longer exists | Orphan rows disabled and reported, never fatal (§3.2, **D3**) |
| Clock moved backwards | `next_run_at` far in the future; the job silently stops | Schedules recomputed from `now`, never by accumulating deltas (§5.1) |
| Silent failure | The customer loses backups without knowing | Every attempt recorded in `job_runs`; query API for the panel (§7) |

### 1.4 The cron question — and an offline-first constraint

§24.1 says "cron-style schedule", and the column is named `schedule_cron`. A correct cron
parser is genuinely fiddly: ranges, steps, the OR semantics between day-of-month and
day-of-week, DST transitions. Getting it subtly wrong means a nightly backup that silently
never runs — data-loss-adjacent.

The obvious answer is `github.com/robfig/cron/v3`. **It is not in the module cache**, so
adding it requires a network fetch, and "the project must build, test, and run fully offline"
is a hard project constraint, not a preference.

Weighed against that: **Phase 0 needs no cron at all.** Both jobs it registers are intervals
(dispatch every N seconds, heartbeat every N minutes). The first genuinely cron-shaped job is
the nightly backup, which is Phase 9.

So the recommendation (**D1**) is a `Schedule` interface with one implementation now:

```go
type Schedule interface{ Next(after time.Time) time.Time }

jobs.Every(30 * time.Second)   // stored as "@every 30s"
```

`@every 30s` is stored in `schedule_cron` — it is not a fudge: that is
**robfig/cron's own syntax** for interval schedules. So when the first cron job arrives, adding
either the dependency or a hand-written 5-field parser is additive, existing rows stay valid,
and the decision gets made with a real requirement in hand rather than speculatively today.

---

## 2. DESIGN — the shape

```
        declarations (code)                      jobs table (durable)
                │                                       │
                ▼                                       ▼
        Registry.Register(def, handler)  ──►  Reconcile at startup (upsert by job_key)
                                                        │
                                                        ▼
                          ┌──────────────────────────────────────────┐
        Scheduler.Start ─►│ tick: claim due jobs (CAS on next_run_at)│
                          │  ├─ catch-up policy for missed occurrences│
                          │  ├─ dispatch to bounded worker pool       │
                          │  ├─ run with timeout + attempt recording  │
                          │  └─ retry w/ backoff, or give up          │
                          └──────────────────────────────────────────┘
                                                        │
        Scheduler.Stop ─► stop claiming, drain in-flight, mark cancelled
```

---

## 3. DESIGN — declaration and reconciliation

### 3.1 Jobs are declared in code

Same principle as settings (0.5): the set of jobs is a property of the built binary, and the
table is where their *state* lives.

```go
type Def struct {
    Key         string        // "outbox.dispatch" — stable; part of the durable record
    Schedule    Schedule
    Timeout     time.Duration // default 5m
    MaxAttempts int           // default 3
    Singleton   bool          // default true
    CatchUp     CatchUpPolicy // RunOnce (default) | Skip | RunAll
    Description string        // i18n key, never prose (§22.2)
}

type Handler func(ctx context.Context, run RunContext) error
```

`RunContext` carries the attempt number, the trigger, and a `Progress(percent int, note string)`
callback (§6.4).

### 3.2 Reconciliation at startup

`Reconcile(ctx)` upserts one row per declared job, keyed by `job_key`:

- **New declaration** → row inserted, `next_run_at` computed from now.
- **Existing** → schedule/timeout/attempts refreshed from code; `is_enabled` and
  `next_run_at` are **preserved**, because an admin may have disabled a job and a release must
  not silently re-enable it.
- **Row with no declaration** (a job removed in a new release) → **disabled and reported**,
  never fatal. Same reasoning as unknown settings keys (0.5, D3): a customer who downgrades
  must still be able to open their shop, and the row is inert once disabled. **D3.**

---

## 4. DESIGN — claiming work without a migration

### 4.1 The due query

`is_enabled = 1 AND next_run_at <= now`, ordered by `next_run_at` — exactly the
`ix_jobs_due` index built in 0.4.

### 4.2 Claiming: CAS on `next_run_at`, plus a run lease

Selecting due jobs and then marking them is the same two-statement race that produced 30
deliveries instead of 10 in 0.6. It is not repeated here.

**Claim is one conditional UPDATE**, guarded on the value just read:

```sql
UPDATE jobs
   SET last_run_at = ?, next_run_at = ?, row_version = row_version + 1, updated_at = ?
 WHERE id = ? AND is_enabled = 1 AND next_run_at = ?   -- the value we read
```

`RowsAffected() = 1` means this scheduler owns the occurrence; `0` means someone else took it.
Advancing `next_run_at` as part of the claim also means an occurrence cannot be claimed twice.

**Singleton** (`is_singleton = 1`) needs more than that, because a long run can still be
in flight when the next occurrence falls due. A job is considered running when it has a
`job_runs` row with `status = 'running'` whose lease has not expired, where the lease is
`started_at + jobs.timeout_seconds`. A due singleton with a live run is skipped this tick.

**This is why no migration is needed (D2):** `job_runs.status`, `job_runs.started_at`, and
`jobs.timeout_seconds` already express the lease. The obvious alternative — adding
`jobs.is_running` / `jobs.locked_by` — would be a second source of truth for a fact `job_runs`
already records, and the two would drift.

---

## 5. DESIGN — schedules and catch-up

### 5.1 Next-run computation is absolute, never accumulated

`next = schedule.Next(now)`, computed from the current instant — not `previous + interval`.

This matters on a desktop: after the laptop sleeps for 14 hours, accumulating deltas produces
a backlog of ticks; and if the user corrects a wrong system clock *backwards*, an accumulated
schedule pushes `next_run_at` months into the future and the job silently stops forever.
Recomputing from `now` self-heals in both directions.

### 5.2 Catch-up policies (§24.1)

| Policy | Behaviour when occurrences were missed | Fits |
|---|---|---|
| `run_once` *(default)* | Run once now, then resume the normal schedule | Backup, balance rebuild |
| `skip` | Do not run; just schedule the next occurrence | Cache warming, cleanup |
| `run_all` | Run once per missed occurrence, in order | Anything where each occurrence has distinct work |

### 5.3 `run_all` must be bounded — D4

A shop closed for a month with a 30-second job accumulates ~86,000 occurrences. Faithfully
running them all would make the app unusable at 8am — the opposite of what a scheduler is for.

So `run_all` is capped (`MaxCatchUp`, default **50**) and the excess is **skipped with a
logged warning** naming the job and the number dropped. Silently discarding them would hide a
real problem; faithfully running them would *be* one.

---

## 6. DESIGN — execution

### 6.1 Bounded worker pool

A fixed pool (default `min(4, NumCPU)`), because this shares a machine with the UI and a
single-writer SQLite database. Unbounded goroutines would let a burst of catch-up work starve
the POS.

### 6.2 Per-run lifecycle

1. Insert `job_runs` row: `status='running'`, `attempt=N`, `triggered_by`.
2. Run the handler under `context.WithTimeout(ctx, timeout_seconds)`.
3. Record the outcome: `succeeded`, `failed` (+ `error`), `timeout`, or `cancelled`.
4. On failure with attempts remaining, schedule a retry with capped exponential backoff and
   jitter — the same rationale as 0.6 §5.5, so a job failing for an environmental reason does
   not retry in lockstep forever.

`output_json` records a structured result (the dispatcher's `Report`, for instance), which is
what makes the Phase 9 panel useful rather than a list of green ticks.

### 6.3 Crash recovery — D5

A desktop app killed mid-run leaves `job_runs` rows at `running` forever, and a singleton job
would then never run again.

At startup, runs whose lease has expired (`started_at + timeout_seconds < now`) are marked
`timeout` with an explanatory error. Same reasoning and mechanism as the migration lock (0.4)
and the outbox visibility timeout (0.6) — a pattern now used three times, deliberately.

### 6.4 Graceful shutdown and UI responsiveness

`Stop(ctx)` stops claiming, cancels in-flight run contexts, waits for the pool to drain within
a grace period, and marks anything still unfinished `cancelled`. Nothing is left `running`,
so the next startup has no phantom leases to reclaim.

Handlers report progress through `RunContext.Progress`, which Step 0.10 forwards to Wails
events (§24.4). No job ever runs on the UI thread.

---

## 7. DESIGN — observability

`Scheduler` exposes read APIs for the Phase 9 panel — recent runs per job, currently running
jobs, last outcome per job, and consecutive-failure counts. Built now because they cost
little and because §24.2 is explicit that unrecorded background failure is how customers lose
backups without noticing.

---

## 8. DESIGN — the two Phase 0 jobs (D7)

§JOB: "Phase 0 registers exactly one real job — the **outbox dispatcher** — plus a heartbeat
job that proves scheduling, catch-up, and the UI panel work."

1. **`outbox.dispatch`** — `@every 5s`, singleton, `CatchUp: Skip` (a missed dispatch tick has
   no distinct work; the pending rows are still there), timeout 2m. Wraps
   `Dispatcher.DispatchOnce` and returns its `Report` as `output_json`. **This closes the item
   0.6 carried forward.**
2. **`platform.heartbeat`** — `@every 1m`, records a timestamp. It exists to prove the whole
   path end-to-end and to give the diagnostics panel something to show on a healthy install.

Package layout:

```
internal/platform/jobs/
├── def.go          # Def, Handler, RunContext, CatchUpPolicy, Schedule, Every
├── registry.go     # declaration + Reconcile
├── scheduler.go    # tick loop, claim, catch-up, Start/Stop
├── worker.go       # bounded pool, timeout, retry/backoff
├── runs.go         # job_runs recording, reclaim, query API
└── builtin.go      # the outbox dispatch + heartbeat jobs
```

`jobs` imports `kernel/{clock,errs,id}` and `platform/database`; `builtin.go` additionally
imports `platform/outbox`. Nothing imports `jobs` except the bootstrap.

---

## 9. TESTING PLAN (Protocol Step 4)

| Level | Tests |
|---|---|
| **Schedules** | `Every` computes from `now`, not by accumulation; a backwards clock jump self-heals rather than freezing the job; `@every` text round-trips through `schedule_cron`. |
| **Reconciliation** | New declaration inserts a row; an existing row keeps admin-set `is_enabled`; a row with no declaration is disabled and reported, **not** fatal; a re-run changes nothing (idempotent, as in 0.5's seeder). |
| **Claiming** | Two schedulers over one database run a due job **exactly once** (`-race`) — the drill that caught the 0.6 bug; a claim on a stale `next_run_at` affects zero rows. |
| **Catch-up — table test** | For each policy, with N missed occurrences: `run_once` → 1 run; `skip` → 0 runs and the schedule advances; `run_all` → N runs in order; **`run_all` beyond `MaxCatchUp` → capped, warned, and the app still starts promptly**. |
| **Singleton** | A due occurrence is skipped while a live run holds the lease; once the lease expires it runs; a non-singleton job may overlap. |
| **Execution** | Success → `succeeded` + `finished_at` + `output_json`; error → `failed` + `error` recorded; a handler that ignores cancellation → `timeout` at `timeout_seconds`; retries increment `attempt` and back off; exhausted attempts stop retrying. |
| **Crash recovery — the drill** | A `running` row left by a killed process, with an expired lease, is marked `timeout` at startup and the job runs again. A row still inside its lease is left alone. |
| **Shutdown** | `Stop` drains in-flight runs; nothing is left `running`; a slow handler past the grace period is `cancelled`, not abandoned. |
| **Integration** | `outbox.dispatch` actually drains a published event end-to-end — event published in a Unit of Work, scheduler tick, subscriber invoked, delivery `done`. This is 0.6 + 0.7 proven together. |

**Mutation-verified**, per the pattern now established: *claim atomicity* (drop the
`next_run_at` guard → the two-scheduler test must fail) and *catch-up bounding* (remove the
cap → the `run_all` test must blow past `MaxCatchUp`).

---

## 10. DECISIONS REQUESTED

1. **D1 — `@every <duration>` schedules now; standard cron deferred** to the first job that
   needs it, keeping the build fully offline. `@every` is robfig/cron's own syntax, so the
   stored values stay valid if that dependency is later adopted. *(Recommended; §1.4.)*
2. **D2 — No migration.** Singleton leasing uses `job_runs.status`/`started_at` plus
   `jobs.timeout_seconds`; claiming is a CAS on `jobs.next_run_at`. Adding `is_running` /
   `locked_by` columns would duplicate a fact `job_runs` already holds. *(§4.2.)*
3. **D3 — Jobs declared in code, reconciled into the table at startup.** Admin-set
   `is_enabled` survives upgrades; an orphan row is disabled and reported, never fatal.
   *(§3.2.)*
4. **D4 — `run_all` catch-up is capped** (`MaxCatchUp`, default 50) with a logged warning for
   the excess. *(Recommended; §5.3 — the alternative makes the app unusable after a holiday.)*
5. **D5 — Startup reclaim of expired runs**, marking them `timeout`. *(§6.3.)*
6. **D6 — Bounded worker pool** (`min(4, NumCPU)`), per-job context timeout, graceful drain
   marking survivors `cancelled`. *(§6.1, §6.4.)*
7. **D7 — Phase 0 registers exactly two jobs**: `outbox.dispatch` (real, closing 0.6's
   carried-forward item) and `platform.heartbeat` (proves the machinery). *(§8.)*

On approval I'll implement in this order, each independently reviewable: `Schedule` + `Every`
→ `Def`/registry + `Reconcile` → claim (CAS) + due query → catch-up policies → worker pool,
timeout, retry → run recording + startup reclaim → `Start`/`Stop` drain → the two built-in
jobs → the integration and mutation drills, tests alongside each, then the self-review and
improvement notes (Protocol Steps 5–6).

---

## 11. IMPLEMENTATION RECORD (Protocol Steps 3–6)

### 11.1 Three bugs the tests caught, all in one run

**Defaults were applied after validation.** `Register` called `def.validate()` on the raw
struct, where `CatchUp` is `""` — which `Valid()` rejects. Every job declared the ordinary way
(leaving defaults alone) was refused with "unknown catch-up policy". Fifteen tests failed at
once. Defaults are now applied *before* validation: **a zero value that cannot be used is not
a default, it is a trap.**

**The catch-up cap was applied while counting, destroying its own reporting.** `Tick` called
`missedOccurrences(..., def.MaxCatchUp+1)`, so with `MaxCatchUp = 5` the count saturated at 6
and `Dropped` reported **1 instead of 3595**. The cap and the enumeration guard had been
conflated. They are now separate: `enumerationGuard` (1,000,000) exists only so a pathological
schedule cannot spin, while the catch-up cap is applied in `planRuns` against the true figure.
Without this, D4's promise — that the excess is *reported* rather than hidden — was silently
false.

**Catch-up runs were launched concurrently.** Each occurrence got its own pooled goroutine,
which broke two guarantees simultaneously: `RunAll` promises occurrences "in order", and a
**singleton job would have had N copies of itself running at once** — precisely what
`is_singleton` exists to prevent. `launchSeries` now runs a job's occurrences sequentially in
one goroutine, holding a single pool slot for the series.

### 11.2 Two deliberate departures from the design

**`Def.AllowConcurrent` replaces `Def.Singleton`.** The design specified `Singleton bool //
default true`, but Go's zero value for a bool is `false` — so a job declared without thinking
about concurrency would have been non-singleton, the *unsafe* reading. Inverting the field
makes the zero value the safe one.

**The heartbeat uses `RunOnce`, not `Skip`.** §8 specified `Skip` on the reasoning that a
missed heartbeat carries no information. In practice `Skip` drops the occurrence whenever a
tick arrives a full interval late — which is exactly when the machine is loaded and a
heartbeat is *most* worth having. `RunOnce` collapses missed beats into a single "still alive,
now", which is what a liveness signal actually means. The outbox dispatcher is `RunOnce` for
the same reason: after a night closed there are pending deliveries waiting, and they should go
out at 8am rather than one tick later.

### 11.3 A test that was weaker than it looked

`TestTwoSchedulersRunADueJobExactlyOnce` passed even with the compare-and-swap guard removed.
The single-writer pool serialises the two ticks, so the second scheduler's `loadDueJobs`
already saw the advanced `next_run_at` and found nothing due — the race never occurred.

`TestOnlyOneSchedulerWinsASimultaneousClaim` was added to force the real interleaving: both
schedulers read the due row *first*, then both claim. It fails deterministically under the
mutation with "both schedulers claimed the same occurrence". The original test is kept — it
would still catch gross duplication — but it is documented as timing-dependent rather than
trusted as the guard.

The lesson generalises: **a passing concurrency test proves nothing until you have watched it
fail.**

### 11.4 Verification

**28 tests, `-race` clean, 83.7% coverage, `make ci` green** (build · archlint · vet · tests ·
golangci-lint · frontend).

Against the real schema — `0001` and `0002` applied through the Step 0.4 runner — so the
`jobs`/`job_runs` CHECK constraints and the foreign key are genuinely exercised.

**Mutation-verified**, per §9:

- *Claim atomicity*: replacing the `next_run_at = ?` guard with a tautology → the simultaneous
  claim test fails with both schedulers winning.
- *Catch-up bounding*: disabling the cap → `run_all is capped` reports 86,400 runs instead of
  50, and the end-to-end test starts 3,601 runs instead of 5.

The integration test proves 0.6 and 0.7 together: an event published inside a Unit of Work,
drained by the scheduled `outbox.dispatch` job, subscriber invoked once, delivery `done`, and
the dispatcher's `Report` recorded in `job_runs.output_json`.

### 11.5 Carried forward

- **Step 0.10** wires `Start`/`Stop` into the composition root and forwards
  `RunContext.Progress` to Wails events (§24.4). Until then the scheduler is started only by
  tests.
- **Phase 9** builds the panel over `RecentRuns` and `JobStates`, which exist and are tested.
- **Cron** arrives with the first job that needs it (nightly backup). `ParseSchedule` is the
  single place to add it, and `@every` values stay valid.
- **Retries are in-process**, so a retry interrupted by the app closing is lost. Deliberate:
  these jobs are periodic, so the next occurrence does the same work, and persisting retry
  state would mean a second scheduling mechanism alongside `next_run_at`.

---

*End of Step 0.7. Design approved, implemented, self-reviewed.*
