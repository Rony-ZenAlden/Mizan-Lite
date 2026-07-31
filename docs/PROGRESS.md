# Mizan ERP — Progress & Status

> **Running status / resume-point document.** Read this first when picking the project back up.
> Last updated: **2026-07-30**. Branch: `main`. Everything below is committed and verified
> **offline** (build · archlint · vet · tests+race · golangci-lint · frontend all green).

---

## 1. What Mizan is (one paragraph)

An **offline-first, commercial-grade desktop ERP/POS** — generic across business types and
countries through **configuration/metadata, not code**. Stack: **Go + Wails + React/Vite/TS +
Tailwind + SQLite**. Clean Architecture as a **modular monolith** (module-first folders, layered
inside each module). Portable schema so a future move to PostgreSQL/MySQL/SQL Server needs no
rewrite. Built to be maintained for 10+ years; correctness over speed.

---

## 2. How we work (the Permanent Development Protocol)

Every phase: **Analysis → Design → WAIT FOR APPROVAL → Implementation → Testing → Self-Review →
Improvement Notes → (approval) → next phase.** No implementation code before the design is
approved. Priorities: **Correctness > Maintainability > Scalability > Readability > Simplicity >
Performance.** Partnership mode: challenge weak decisions, recommend better ones with trade-offs,
never sacrifice long-term quality for speed. Full text lives in Claude's project memory.

### Local-first / offline (hard constraint)
No GitHub, no remote CI, no cloud service, no auto-push — all optional future integrations only.
Canonical CI is local: **`scripts/check.sh`** / **`make ci`** (runs fully offline once deps are
fetched). The GitHub Actions workflow is an inert opt-in template under
`ci/optional-github-actions/`. `make vendor` gives fully self-contained Go builds. Local Git only.

---

## 3. Architecture documents (source of truth)

| Document | Purpose |
|---|---|
| `docs/architecture/ARCHITECTURE_v1.md` | System architecture — all 23 areas + tax/accounting/inventory. |
| `docs/architecture/ADDENDUM_v1.1_resolved_design.md` | Variants, UoM, country profiles, costing, currency (rate types, redenomination). |
| `docs/architecture/PHASE_0_FOUNDATION.md` | Phase 0 detailed design + the 12-step sequence + Definition of Done. |
| `docs/architecture/STEP_0_2_NUMERIC_KERNEL.md` | Numeric kernel design + implementation record. |
| `docs/architecture/STEP_0_3_DATABASE_PLATFORM.md` | Database platform design + implementation record. |
| `docs/architecture/STEP_0_4_MIGRATIONS.md` | Migration runner + safety layer design, implementation record, and the amended D1. |
| `docs/architecture/STEP_0_5_CONFIG_REGISTRIES.md` | Settings/flags/strategy/metadata registries — the mandate's mechanism. |
| `docs/architecture/STEP_0_6_EVENTS_OUTBOX.md` | Event bus + transactional outbox, and the amended D3 (per-handler deliveries). |
| `docs/architecture/STEP_0_7_JOB_SCHEDULER.md` | Durable job scheduler, catch-up policies, and the outbox dispatch job. |
| `docs/PROGRESS.md` | **This file** — running status. |

---

## 4. Locked architectural decisions (already approved)

- **Accounting:** double-entry from day one (progressive UI); **table-driven posting rules** —
  modules emit events, accounting subscribes; no hardcoded journal entries.
- **Tax:** fully config-driven, disabled by default, `tax_versions` for rate history. **No country
  hardcoded** — country profiles are seed data. Syria (`SYP`, 0 decimals) is the dev seed example.
- **Currency:** functional (ledger) currency configurable per company; USD only a pricing
  reference + rate pivot. **Multiple rate types** (official/market/custom/manual) as data, bound
  per context (pricing/purchasing/sales/accounting/reporting/tax). **Redenomination** supported
  (successor/predecessor + fixed factor). Currencies are DB entities, never enums.
- **Data model:** single `partners` table; single `sales_documents` (doc_type discriminator).
  Generic **product variants** via attribute system. **Fractional quantities** from day one.
  **UUIDv7** keys (CHAR(36)); money as BIGINT minor units (never float).
- **Inventory:** perpetual, **Weighted Average Cost** default, FIFO-ready schema.
- **Portability:** strict logical type contract; no SQLite-isms; PostgreSQL/MySQL/SQL Server ready.
- **Events:** transactional **outbox** for cross-module events (atomicity + future cloud-sync).
- **Plugins:** staged — internal module registry + extension points now; JSON declarative next;
  out-of-process gRPC only on real demand (Go's native `plugin` is avoided — broken on Windows).
- **Numeric kernel:** checked `int64`, **private representation** (widen to 128-bit/big.Int for
  crypto with no API change); single-rounding rule; typed errors, never panics.
- **Database:** implicit transactions via `context.Context`; executor-from-context; nested `Do`
  joins (no savepoints in v1); dialect shim is the whole portability surface; driver errors →
  typed `kernel/errs`. Driver `modernc.org/sqlite` (no cgo).

---

## 5. Phase 0 — Foundation: step status

The reusable **application kernel** future modules plug into (no business features yet).

| Step | Scope | Status |
|---|---|---|
| **0.1** | Repo scaffold, tooling, archlint framework, CI, Wails shell, frontend foundation | ✅ committed |
| **0.2** | Numeric kernel (money/quantity/rounding) | ✅ committed |
| **0.3** | Database platform (pools, dialect, Unit of Work, contract suite) | ✅ committed |
| **0.4** | Migration runner + desktop safety layer + `0001_platform.sql` | ✅ committed |
| **0.5** | Config registries (settings + metadata) + strategy registry | ✅ committed |
| **0.6** | Event bus + transactional outbox | ✅ committed |
| **0.7** | Durable job scheduler + outbox dispatcher | ✅ committed |
| **0.8** | i18n platform + seed locales | ⬜ next — design first |
| **0.9** | Currency module (schema `0003`, repos, converter, seeds) | ⬜ |
| **0.10** | Composition root wiring the full graph + graceful shutdown | ⬜ |
| **0.11** | Frontend foundation: tokens, primitives, shell, providers, bindings wrapper | ⬜ (base done in 0.1; full design system pending) |
| **0.12** | Phase 0 Definition-of-Done review | ⬜ |

---

## 6. What's built so far (detail)

### Step 0.1 — Scaffold & tooling `cbd68d3`
- Go module `github.com/mizan-erp/mizan` (**placeholder path**), Go 1.26, no-cgo core.
- **`tools/archlint`** — extensible, data-driven architecture-rule framework (`arch-rules.yml`):
  import boundaries, forbidden calls (panic/os.Exit/fmt.*/time.Now by location), no-float. Adding
  a rule = one file + one registry line. Verified it catches planted violations. Scoped to
  production code (`./internal/...`).
- **`.golangci.yml`** (standard linters), **Makefile**, **`scripts/check.sh`** (offline CI),
  optional GitHub Actions template (inert).
- **Wails shell** (`main.go`/`app.go`, builds with cgo → 5.6 MB binary) + **frontend foundation**
  (Vite + React + TS, design tokens light/dark, runtime locale/RTL + theme switch, bindings
  wrapper, i18n key-coverage test, RTL logical-properties ESLint gate).
- **`internal/kernel/*` package markers** (doc.go) + `internal/buildinfo`.

### Step 0.2 — Numeric kernel `ebd263d`
- **`kernel/round`** — 7 rounding modes + one rounding engine (full truth-table tested).
- **`kernel/internal/fixed`** — the single tested home for checked int64 arithmetic + decimal
  parse/format (no duplication).
- **`kernel/money`** — `Currency`, `Money`, `Rate`, `Percent`, `UnitAmount`, and the sole
  `LineExtension` (price × quantity) authority. Integer-only, exact, single-rounding, typed
  errors, allocation that always conserves the whole, private representation (crypto-ready).
- **`kernel/quantity`** — `Unit`, `Quantity`, category-checked conversion (metric/imperial as
  data; fractional-unit rules).
- ~93% coverage with property-based tests (allocation conservation, arithmetic laws, conversion
  round-trips, floor-or-ceil rounding) + boundary/overflow tests.

### Step 0.3 — Database platform `299d075`
- **`kernel/errs`** — typed categorised errors (Validation/NotFound/Conflict/Permission/Internal)
  with stable codes (i18n keys), params, field errors, wrap/unwrap, classification helpers.
- **`platform/database`** — `Store`: single-writer pool (serialises writes, no SQLITE_BUSY) +
  reader pool; portable PRAGMAs per connection; `Executor` + `Writer(ctx)`/`Reader(ctx)`; Unit of
  Work `Do` (panic-safe, nested-join, no single-writer deadlock); `VersionedUpdateResult`
  optimistic lock.
- **`platform/database/dialect`** — the whole portability surface (Upsert, Rebind, LimitOffset,
  ForUpdate, TranslateError). SQLite impl only; driver errors → typed `errs`.
- **`platform/database/dbtest`** — dialect-agnostic **contract suite (13 subtests)**; a future
  PostgreSQL dialect must pass it identically.

### Step 0.4 — Migration runner & platform schema
- **`platform/migrate`** — forward-only runner: `Up()` / `Status()`, one transaction per
  migration with its version row, **SHA-256 checksum verification on every boot**, a
  **version gate** (an older binary refuses a newer database), a **cross-process advisory
  lock** with a 15-minute stale-lock reclaim, and `Progress` phases for the splash screen.
- **Desktop safety layer** — `integrity_check` → **free-space pre-flight** → `VACUUM INTO`
  snapshot → **verify the snapshot** → migrate → **automatic restore on failure**, keeping the
  failed database as `.failed-<ts>` and pruning to the newest 5 backups after success only.
- **Loader** — `<version>_<name>.sql`, numeric ordering, duplicate-version detection, and a
  statement-aware scanner that rejects `PRAGMA`/`VACUUM`/`ATTACH` (comment- and
  string-literal-safe, so `…; PRAGMA x` on one line is caught).
- **`migrations/`** (module root, `go:embed`) — `SQLite()` exposes `0001_platform.sql`, which
  creates all **eight platform tables** (`schema_migrations`, `schema_lock`, `settings`,
  `feature_flags`, `outbox_events`, `jobs`, `job_runs`, `translations`, `number_series`) under
  the portable type contract.
- **Dialect shim extended** — `TableExistsQuery`, `IntegrityCheckStatement`,
  `OnlineBackupStatement`; the runner contains **no engine-specific SQL**, so the PostgreSQL
  port stays a dialect implementation.
- **30 tests, 85.8% coverage**, including the four required drills (injected failure with data
  survival, checksum tamper, version gate, corrupt database) — each **mutation-verified** to
  fail when its protection is removed.
- Decision **D1 was amended**: hand-written runner instead of `pressly/goose`. Reasoning in
  `STEP_0_4_MIGRATIONS.md` §11.1. Zero new dependencies; still stdlib-only.

### Step 0.5 — Configuration & metadata registries
The mechanism behind the mandate. **Three mechanisms, kept deliberately separate** (§CFG.1):
*settings* (typed knobs), *metadata* (kinds of things), *strategies* (named behaviours).
- **`kernel/id`** — UUIDv7 as `CHAR(36)` text; time-ordered for index locality, nil UUID
  rejected. `google/uuid` promoted from indirect to direct — **no new dependency**.
- **`kernel/clock`** — `Format`/`ParseTimestamp`/`TimestampLayout`: the single definition of
  the portable CHAR(24) timestamp (fixed-width UTC, so lexical order = chronological order).
- **`kernel/round.ParseMode`** — the missing inverse of `String()`, which the docs already
  called the storage form. Unknown names rejected, never defaulted.
- **`platform/config`** — settings **declared in code**, read through the typed handle
  declaration returns (`TaxEnabled.Get(ctx)`), so call sites hold no key strings and cannot
  mismatch types. Scope chain **Session ► User ► Branch ► Company ► System ► default**;
  load-all snapshot cache with a version counter; codecs that never route money or int64
  through a float; `Set` validating scope/enum/permission/custom rules through the dialect
  upsert. **Feature flags** share the machinery with their own lifecycle
  (`Stability`, `RemoveBy`, `ExpiredFlags`).
- **Three ports** — `ScopeProvider`, `ChangeNotifier`, `Authorizer` — so config never imports
  `api`, and does not pre-empt the event bus (0.6) or RBAC (Phase 1).
- **`platform/strategy`** — generic `Registry[T]`; duplicate registration is an error, an
  unknown key is a typed NotFound naming the point and key, `Keys()` sorted for admin UIs.
  Nine extension points exported as **names**; typed registries are created by the package
  owning each contract type.
- **`platform/metadata`** — the `code`/`name`/`is_system`/`is_active` contract plus an
  idempotent, **code-keyed** seeder: never deletes, never touches admin-owned rows, and a
  second identical run is a no-op.
- **archlint gained two enforced boundaries** (D6, substituted — see §11.3): platform may not
  import modules or api; modules/api may not import the dialect shim. Both verified by
  planting violations.
- **60 tests, `-race` clean**, run against the real `0001_platform.sql` schema; config 79.4%,
  strategy 97.1%, metadata 85.6%, id 91.7%. Precedence resolution and seed idempotency were
  **mutation-verified**.

### Step 0.6 — Event bus & transactional outbox
The atomicity guarantee under every cross-module side effect. **Two mechanisms, deliberately
separate** (§23.1): synchronous domain events, durable integration events.
- **`kernel/event`** — `Event`/`Envelope` plus correlation & causation carried in the context
  and **stamped automatically**, so causal chains do not depend on every handler author
  remembering to pass them along.
- **`platform/eventbus`** — typed generic `Subscribe`, explicit registration (no reflection
  discovery, so subscriptions are greppable), registration-order delivery. A handler error or
  **panic aborts the publish and rolls the transaction back** — recovered, never swallowed: a
  recovered panic that let the commit proceed would be worse than the crash.
- **`platform/outbox`** — `Publish` writes the event **and one delivery row per registered
  handler inside the caller's transaction**. That single fact is the whole guarantee: a
  rolled-back sale leaves no event.
- **Dispatcher** (`DispatchOnce`, callable; 0.7 schedules it) — compare-and-swap claim,
  **per-(aggregate, handler) head-of-line ordering**, capped exponential backoff with jitter,
  dead-lettering after `MaxAttempts`, and visibility-timeout reclaim of deliveries abandoned
  by a crashed dispatcher. A `dead` delivery halts its stream until a human intervenes.
- **`0002_outbox_deliveries.sql`** — D3 was **rejected in review** in favour of per-handler
  tracking, so a retry re-runs only what failed. Correctness no longer depends on every
  handler being idempotent. **Currency's migration becomes `0003`.**
- **`config.ChangeNotifier` port closed** — `SettingChanged` publishes on the domain bus
  (D7), making "language change without restart" real. A failing subscriber never undoes the
  write.
- **54 tests, `-race` clean**; eventbus 92.7%, outbox 86.7%, config 79.4%. The concurrency
  test caught a **real claim race** (30 deliveries instead of 10) that every per-row assertion
  missed. Atomicity and ordering **mutation-verified**.

---

### Step 0.7 — Durable job scheduler
Persisted, not in-memory, because "a desktop app is closed every evening, so in-memory
scheduling loses work" (§24.1). **No migration** — `jobs`/`job_runs` already existed.
- **Declared in code, reconciled at startup.** A new job inserts its row; an admin's
  `is_enabled` survives upgrades; a row this build no longer declares is **disabled and
  reported, never fatal**.
- **Catch-up policies** (§24.1) — `run_once` (default, collapses missed into one), `skip`,
  `run_all`. **`run_all` is capped per job** (default 50) with the excess logged: a shop closed
  a month with a 30-second job accumulates ~86,000 occurrences, and running them all would make
  the app unusable at 8am.
- **Claiming is a single conditional UPDATE** on `next_run_at` — the same compare-and-swap as
  the outbox, for the same reason.
- **Singleton by default** (`AllowConcurrent` inverted so the zero value is the safe one),
  leased via `job_runs.status` + `jobs.timeout_seconds` — no extra column.
- **Execution:** bounded pool, per-job context timeout, in-process retries with jittered
  backoff, panics recorded as ordinary failures, **catch-up runs sequential** (concurrent ones
  broke both ordering and singleton), graceful drain marking survivors `cancelled`, and
  **startup reclaim** of runs abandoned by a killed process.
- **Two jobs registered:** `outbox.dispatch` (closes 0.6's carried-forward item) and
  `platform.heartbeat`. `RecentRuns`/`JobStates` are the query API the Phase 9 panel will read.
- **28 tests, `-race` clean, 83.7%.** Claim atomicity and catch-up bounding
  **mutation-verified** — the original two-scheduler test passed under mutation, so a
  deterministic one was added.

## 7. Repository map (as built)

```
Mizan ERP/
├── main.go, app.go, wails.json         # Wails desktop shell
├── arch-rules.yml                      # data-driven architecture rules
├── Makefile, scripts/check.sh          # local, offline dev + CI
├── ci/optional-github-actions/         # inert opt-in GH Actions template
├── cmd/, build/                        # (reserved)
├── internal/
│   ├── buildinfo/                      # version metadata
│   ├── kernel/                         # money, quantity, round, errs, clock, id, event,
│   │                                   #   internal/fixed + markers: locale, validation, paging
│   ├── platform/database/              # DB pools, dialect, executor, uow, helpers, dbtest
│   ├── platform/migrate/               # migration runner + desktop safety layer
│   ├── platform/config/                # settings + feature flags (typed handles)
│   ├── platform/strategy/              # generic extension-point registry
│   ├── platform/metadata/              # reference-data contract + idempotent seeder
│   ├── platform/eventbus/              # in-process domain event bus
│   ├── platform/outbox/                # transactional outbox + dispatcher
│   ├── platform/jobs/                  # durable job scheduler + built-in jobs
│   └── api/ (reserved)  modules/ (reserved)  bootstrap/ (reserved)
├── migrations/                         # go:embed'd schema, one dir per dialect
│   └── sqlite/0001_platform.sql, 0002_outbox_deliveries.sql
├── frontend/                           # Vite + React + TS foundation
├── tools/archlint/                     # architecture-rule framework
└── docs/architecture/                  # design docs (+ this PROGRESS.md one level up)
```

---

## 8. Build / test / run (all offline)

```bash
make ci        # full local CI: Go core + archlint + golangci-lint + frontend
make test      # go test (race + coverage) on the core
make arch      # architecture rules only
make dev       # wails dev (needs the wails CLI: make tools)
make build     # production desktop binary
make vendor    # vendor Go deps for fully self-contained builds
```

Prereqs present: Go 1.26, Node 22, golangci-lint. **Not installed locally:** the `wails` CLI
(`make tools` installs it) and `sqlc` (deferred until a schema exists, Step 0.9).

---

## 9. Open items / next

- **Immediate next:** Step 0.8 — i18n platform + seed locales. The `translations` table
  already exists from `0001_platform.sql`, and `kernel/locale` is still a marker package.
  **Design first, await approval** per the protocol.
- **Deferred (tracked):** `sqlc` for read models (0.9+); `kernel/paging` helper; SAVEPOINT-based
  partial rollback (only if needed); the real GitHub/remote integrations (optional, user's call).
- **Carried from 0.4 (deliberate, not gaps):** module-owned migration FS **merging** (§3.5) —
  `Load` reads one FS; merging arrives with the second module that ships migrations (0.9,
  currency). The bootstrap must **reopen or exit** after a failed migration, since `Up` leaves
  the `Store` closed once it restores — wire that in Step 0.10.
- **Carried from 0.5 (deliberate, not gaps):** JSON seed-**file** discovery/ordering across
  modules lands with the first real seed (0.9). The bootstrap must call
  `config.Registry.Validate()` and `config.Bind()` on the root context (0.10) — without the
  bind, every setting silently resolves to its declared default. `ChangeNotifier` and
  `Authorizer` get real implementations in 0.6 and Phase 1 respectively.
  Known edge: `Settings.Set` nested in a business transaction that rolls back needs
  `Reload` (design §11.5).
- **Before Phase 2 (financial spine):** need the first customer's real tax rates / chart of
  accounts, or confirmation to ship an empty, user-configured tax profile.

---

## 10. Commit history (Phase 0)

```
8b769c9  Phase 0 Step 0.7: durable job scheduler + outbox dispatch job
2a7b00b  Phase 0 Step 0.6: event bus + transactional outbox
4160a7a  Phase 0 Step 0.5: configuration & metadata registries
f71384d  Phase 0 Step 0.4: migration runner + desktop safety layer
b763230  docs: add PROGRESS.md — running status / resume-point
299d075  Phase 0 Step 0.3: database platform (pools, dialect, Unit of Work)
ebd263d  Phase 0 Step 0.2: numeric kernel (money, quantity, rounding)
cbd68d3  Phase 0 Step 0.1: repo scaffold, arch-rule framework, Wails shell
```
