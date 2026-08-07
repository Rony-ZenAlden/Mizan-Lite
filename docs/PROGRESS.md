# Mizan ERP — Progress & Status

> **Running status / resume-point document.** Read this first when picking the project back up.
> Last updated: **2026-08-07** (Step 1.9). Branch: `main`. Everything below is committed and verified
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
| `docs/architecture/STEP_0_8_I18N.md` | i18n platform, the single shared catalog, and the error-code coverage gate. |
| `docs/architecture/STEP_0_9_CURRENCY.md` | Currency module, rate types, redenomination, and the corrected rate uniqueness. |
| `docs/architecture/STEP_0_10_COMPOSITION_ROOT.md` | Composition root, startup ordering, the error taxonomy, and shutdown. |
| `docs/architecture/STEP_0_11_FRONTEND_FOUNDATION.md` | Frontend foundation: the bindings wrapper, boot inversion, tokens, primitives, shell. |
| `docs/architecture/STEP_0_12_PHASE_0_DOD_REVIEW.md` | **Phase 0 Definition-of-Done review** — the ten criteria, evidence, and the two gaps. |
| `docs/architecture/PHASE_1_CORE_DATA.md` | **Phase 1 detailed design** — org, identity, RBAC, the policy mechanism, audit, setup. |
| `docs/architecture/STEP_1_1_ORG_AND_RULES.md` | Step 1.1: module-isolation + no-sql rules, and the org module. |
| `docs/architecture/STEP_1_2_IDENTITY.md` | Step 1.2: Argon2id credentials, users, password policy, `Authenticator`. |
| `docs/architecture/STEP_1_3_SESSIONS.md` | Step 1.3: sessions, throttling, the sweep job, and the real AppContext. |
| `docs/architecture/STEP_1_4_RBAC.md` | Step 1.4: permissions, roles, scope resolution, the real `Authorizer`. |
| `docs/architecture/STEP_1_5_POLICY.md` | Step 1.5: policies per binding, the guard, and the startup coverage check. |
| `docs/architecture/STEP_1_6_REDACTION.md` | Step 1.6: field-level redaction and the audit read path. |
| `docs/architecture/STEP_1_7_AUDIT_WRITE.md` | Step 1.7: the `Auditable` event, in-transaction subscribers, atomicity. |
| `docs/architecture/STEP_1_8_PROFILES.md` | Step 1.8: country/business profiles and layered seed-file discovery. |
| `docs/architecture/STEP_1_9_SETUP_WIZARD.md` | Step 1.9: the setup wizard backend and its structural gate. |
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
| **0.8** | i18n platform + seed locales | ✅ committed |
| **0.9** | Currency module (schema `0003`, repos, converter, seeds) | ✅ committed |
| **0.10** | Composition root wiring the full graph + graceful shutdown | ✅ committed |
| **0.11** | Frontend foundation: bindings wrapper, boot inversion, tokens, primitives, shell | ✅ committed |
| **0.12** | Phase 0 Definition-of-Done review | ✅ committed — **Phase 0 COMPLETE** |

## 5b. Phase 1 — Core Data: step status

Design: `docs/architecture/PHASE_1_CORE_DATA.md` (approved, incl. D6 policy-at-the-boundary
and D7 synchronous in-transaction audit).

| Step | Scope | Status |
|---|---|---|
| **1.1** | `module-isolation` + `no-sql` rules; org module (company/branch/warehouse/fiscal) | ✅ committed |
| **1.2** | Identity: credentials (Argon2id), `Authenticator` port, users | ✅ committed |
| **1.3** | Sessions, lockout, login attempts; `appctx` reads a real session | ✅ committed |
| **1.4** | RBAC: permissions sync, roles, grants, scope resolution | ✅ committed |
| **1.5** | The policy mechanism + enforcement decorator + startup coverage check | ✅ committed |
| **1.6** | Field-level redaction (+ audit schema & read path) | ✅ committed |
| **1.7** | Audit WRITE path: `Auditable` events, in-transaction subscribers (D7) | ✅ committed |
| **1.8** | Country + business profiles, seed-file discovery | ✅ committed |
| **1.9** | Setup wizard backend | ✅ committed |
| **1.10–1.11** | Frontend: router, gates, login, wizard, admin screens | ⬜ next — design first |
| **1.12** | Phase 1 Definition-of-Done review | ⬜ |

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

### Step 0.8 — i18n platform & seed locales
Two problems that share a word and must not share a mechanism (§22.4): UI strings shipped in
the binary, user content typed by the customer. **No migration** — `translations` already
existed.
- **One catalog** (§22.1): `locales/{en,ar}/{common,errors}.json` at the module root,
  `go:embed`'d by Go and imported directly by Vite. The placeholder dictionary in
  `frontend/src/i18n/locales.ts` is **deleted**, and a test fails if it reappears.
- **`kernel/locale`** — any well-formed tag (not an enum of two), RTL by language subtag,
  fallback chain `ar-SY → ar → en`, context carrier. A missing key **returns the key itself**:
  visible and greppable beats a blank label.
- **Error-code coverage gate** — an AST walk over `internal/**` collects every `Code*`
  constant and asserts each has a translation in **every** locale. It caught four missing
  codes on its first run — ones this step had just introduced. **65 keys per locale, 57 of
  them error codes.**
- **User content** resolves via the generic `translations` table with a lazy
  per-`(entity_type, locale)` cache and `ResolveMany` to avoid the grid N+1. It **never falls
  back across languages** — the owner's own text beats a stray English translation.
- **Locale is a setting** (`ui.locale`, scoped system/company/user); `OnLocaleChanged` filters
  the existing `SettingChangedEvent` rather than adding a second mechanism.
- **No pluralisation**, deliberately: Arabic has six forms and a naive `count == 1` fallback
  would be wrong in front of every Arabic customer.
- **30 Go + 9 frontend tests**, `-race` clean; locale 93.9%, i18n 87.4%. Fallback chain and
  the coverage gate **mutation-verified**.

### Step 0.9 — Currency module
The **first business module**, in Phase 0 because Money, rate types, and redenomination are
kernel-adjacent (§CUR). Schema `0003`, owned by the module and merged into the globally
ordered set via the new `migrate.Merge` — which closes the item **0.4 carried forward**.
- **Three currency roles** (§18.1): pricing, functional/ledger, transaction — all settings, so
  a single-currency company sets them equal and every conversion collapses to identity.
- **A contradiction in the approved schema, corrected (D3).** `UNIQUE (from, to, rate_type,
  valid_from)` and "append-only, a correction is a new row" cannot both hold: correcting
  today's rate an hour later is rejected. `created_at` joins the key; resolution takes the
  newest. The correction drill is **mutation-verified**.
- **Resolution:** identity → direct → inverse → pivot (one rounding) → last-known-with-age →
  typed error. **A rate is never fabricated.** `Result` carries the rate, source, `AsOf` and
  `Age` so the caller stores what §18.1 requires and the POS can show staleness.
- **Six rate-type contexts** resolved independently (§G.1/§CUR.2) — official for tax and
  statutory, market for pricing and purchasing.
- **Redenomination** (§G.2) walks the successor chain with cycle detection and a hop bound;
  stored documents keep their original amounts forever.
- **First real use of three earlier mechanisms:** the 0.5 metadata seeder (idempotency drill
  passes), the 0.8 `translations` table (Arabic currency names as data, no `name_ar` column),
  and `round.ParseMode` for `rounding_mode` round-tripping.
- **`Module` contract** defined in `platform/modules` and implemented here; no `Permissions()`
  (Phase 1) and no events (no subscriber exists).
- **23 tests + 4 merge tests, `-race` clean**, currency 73.9%. Temporal resolution
  mutation-verified; single-rounding asserted directly but **not** mutation-verified — see
  §9.4 of the step doc for why.

### Step 0.10 — Composition root & full graph wiring
The step where everything built so far has to start **together**, in the right order, and stop
again without losing anything. Manual constructor injection, one root, no container (§6).
- **`platform/paths`** closes a real gap: `database.Config.Path` was required and nothing
  supplied it. OS app-data dirs, `MIZAN_DATA_DIR` override, `0o700`, and a **write check at
  startup** — "cannot write to the data folder" is fixable at launch and a disaster mid-sale.
- **Module registry**: topological order (deterministic for independent modules), cycle
  detection, unknown dependencies, and cross-module duplicate setting/flag keys — all **fatal**.
- **The error taxonomy, now written down (D3):** *code* defects are fatal at startup; *data*
  leftovers are reported and ignored. Applied consistently since 0.4; the composition root is
  where it is enforced.
- **`api/envelope`** enforces §5.4: every binding returns `Result[T]`, and **no English prose
  crosses the boundary** — a test asserts developer text never appears in the JSON.
  `api/appctx` stamps locale (closing 0.8's carried item) and a per-call correlation id.
- **Ordering:** migrate before anything reads a table; subscriptions before the scheduler
  starts. **Seeding runs every boot** (D4) — no first-run flag, and it self-heals a deleted
  system row.
- **Shutdown is politeness, not durability** (D6): bounded, best-effort, and correctness comes
  from the outbox, job reclaim, and the WAL.
- `main.go`/`app.go` now start through the root, with `Health()` and `Currencies()` as the
  envelope's first consumers.
- **38 tests, `-race` clean**; envelope 100%, modules 95.6%, bootstrap 70.3%. Cycle detection
  **mutation-verified** (stack overflow without the guard). The ordering test is honestly
  scoped — see step doc §9.1.

### Step 0.11 — Frontend foundation
The step where the boundary ten steps built finally got a consumer — and turned out to have
been leaking since 0.10.
- **The defect that shaped the step.** `App.Health()` began returning `Result[T]` in 0.10; the
  frontend wrapper still declared the flat shape, so the packaged app showed "…" forever.
  Invisible because the dev mock returned the *old* shape (certifying the broken path), nothing
  tested the wrapper, and §FE.4's "one unwrap point" was a sentence rather than a build gate.
- **A second, latent instance of the same class**: `Result.Data` had `omitempty`, which omits
  empty slices — so a list binding with zero rows sent no `data` key and the frontend would
  have crashed on `.map()`. `Currencies()` was one empty database away.
- **`lib/wails`** is now the single unwrap point: `call()` returns data or **throws**
  `BindingError` (a union a caller can forget to check is how §1.2 happened). An ESLint rule
  forbids `window.go` anywhere else, **verified by planting**; a Go-generated fixture pins the
  wire shape so neither language can change it alone.
- **Boot inverted (D2).** The window opens first; the graph builds in `OnStartup`; bindings are
  **static façades attached on success** — "declared in code, reconciled at startup", the same
  pattern as settings (0.5) and jobs (0.7). Every guarded method returns a typed not-ready
  error, never a nil dereference. 0.10's ordering guarantee is preserved because the *shell*
  still does not mount until ready — only a boot screen does. Migration `Progress`, emitted
  since 0.4 and never consumed, finally has somewhere to draw.
- **A failed start is now a screen**, rendered from code + params with the backup path — not a
  log line and `os.Exit(1)`. `os.Exit` survives only for an unresolvable data directory, where
  there is genuinely no window.
- **Locale and theme became settings** (`ui.theme` is new, with a `system` value). The shell's
  private `useState` locale and the backend's `ui.locale` were two unrelated facts that had not
  yet disagreed only because nothing on the backend rendered text.
- **Tokens before primitives**, with a **WCAG AA contrast gate** over both themes — 52
  assertions parsed from the real `index.css`. The classic dark-theme trap fails it at 3.68:1.
- **Radix primitives** (D1: Radix yes, TanStack/Zustand/router deferred to a real consumer).
  A focus test found a genuine defect: a *controlled* Radix dialog has no Trigger, so Radix
  dropped focus to `<body>` on close, stranding keyboard users. Fixed in the primitive.
- **Job status panel** over the 0.7 query API, closing DoD item 5. `RunNow` deliberately
  unexposed until it can be permission-gated.
- **121 frontend tests + 23 binding subtests**, `-race` clean; envelope 100%, bindings 89.2%.
  Five mutation drills, including the shipped bug reproduced exactly.

### Step 0.12 — Phase 0 Definition-of-Done review
A review, not a build. Ten criteria, each backed by a **live run of the packaged app** or a
**named test** — nothing asserted from reading code.
- **The end-to-end gap 0.11 flagged is closed.** `make tools` installed wails v2.13.0;
  `make build` produced `mizan.app`; it was launched against an empty data directory and
  migrated a fresh database. `mizan window ready` logs **before** any migration line — 0.11's
  boot inversion (D2) working on real hardware, previously provable only by unit test. Second
  boot applied no migrations (the idempotent fast path).
- **DoD 5 live**: 43 job runs across dispatch (5s) and heartbeat (60s); stopped, left down
  ~3 minutes, restarted → `run_once` collapsed three missed heartbeats into **exactly one**.
- **DoD 9 live**: after shutdown, **0** runs left `running`, 47 succeeded, and `mizan.db-wal`
  at **0 bytes** — the checkpoint ran. Drain → flush → checkpoint → close, observed.
- **Two honest gaps, written down rather than smoothed over:**
  1. **Two of REPO.3's seven lint rules are not enforced.** `module-isolation` (rule 2) is
     documented as planned and is vacuously true today with one module — it must land early in
     Phase 1, when a second module makes it both possible and necessary. Rule 7 ("no raw SQL
     outside infra") was written before `platform/*` packages owned tables; its *intent* holds
     (no SQL in domain/app/api) but the wording needs restating. No rule is being violated:
     `time.Now()` outside `kernel/clock` and float arithmetic both have **zero** occurrences.
  2. **Nobody has visually confirmed the rendered shell.** macOS screen-recording permission
     was declined, so rendering rests on a live WebKit content process plus 121 component tests
     (including both-direction RTL renders) — not on anyone having looked. One manual launch
     closes it.
- **312 Go tests + 121 frontend tests**, `-race` clean, `make ci` green.
- **Verdict: Phase 0 is complete.** Phase 1 (org, identity + RBAC, settings, audit) can begin.

### Step 1.1 — Architecture rules & the org module
Two jobs together, deliberately: **the rules land before the imports they forbid are written.**
- **`module-isolation` enforced at last** (§3.2). Deferred three times on the assumption it
  needed a whole-program graph pass — it does not: the owning module of a file and of each
  import are both prefix arithmetic. Its first draft mis-read Go's synthetic `foo_test`
  packages as a module; it now walks each *file's* imports so `skipPos` exempts test files.
- **`no-sql`** replaces REPO.3 rule 7, whose wording ("outside infra") predated the platform
  packages that own tables. Requires **two** SQL tokens, so `"update the display name"` stays
  legal — a rule that cries wolf gets switched off.
- **Both verified by planting**, in both directions (violation *and* allowed case).
- **`modules/org`** — schema `0004`, domain, repositories, `Provision`. Company, branches,
  warehouses, and a fiscal calendar whose periods are **generated, never hand-entered**: a
  boundary one day out means a Phase 2 posting belonging to no period or to two.
- **D1 corrected the phase design.** §SEQ said 1.1 would *seed* a default company; §WIZ.1 says
  the wizard creates it. Both cannot hold — a seeded placeholder makes the wizard's "no company
  exists yet" invariant false before it runs. **Nothing is provisioned at boot**; the
  one-company invariant lives in the service, not only on the binding.
- **The topological sort finally has something to order**: org depends on currency (FK on
  `currencies(code)`), and the composition root hands them over deliberately reversed.
- 16 new tests; mutation-verified on the one-company invariant and fiscal tiling.

### Step 1.2 — Identity: credentials & users
The first step whose defects are *security* defects — several are invisible in testing because
the system behaves correctly right up until someone attacks it.
- **Argon2id** in `platform/crypto`, at OWASP's 19 MiB / 2-pass setting (chosen over 46 MiB /
  1-pass because a POS switches users constantly on a shop's hardware). `golang.org/x/crypto`
  was **already an indirect dependency** — promoting it added nothing and resolves offline.
- **One PHC string** carries the salt and every parameter (D2). The phase design's separate
  `params_json` was dropped as a second copy of one fact — 0.4 rejected goose over that, and
  0.7 D2 refused it for job state.
- **The timing drill is the test that matters.** Removing the constant-work verification for an
  unknown username: *"returned in 27.5µs against 15.028917ms for a known one"* — a 545×
  oracle for enumerating accounts, with the system returning the correct answer either way.
- **Every authentication failure is the same error** — unknown user, wrong password, inactive
  account. Anything else is a username oracle.
- **Rehash on login** when stored parameters fall below policy: the mechanism that makes
  §13.1's "raise the cost without invalidating passwords" real rather than aspirational.
- **Password policy follows NIST SP 800-63B** (D3): 12 characters, **no forced rotation**, no
  composition rules, 5-password history. Rotation produces `Summer2026!` → `Summer2027!`; the
  default is what a well-run system does, and an auditor's requirement is one setting away.
  Minimum length is floored at 8 in the domain — a setting is not a licence to disable
  security (§CFG.5).
- **The leak drill asserts on marshalled JSON**, not struct fields. Credentials live in their
  own table so no ordinary user query *can* return one.
- **Identity ships the first `contract` package**; it reaches org through a one-method port it
  declares itself, so org owes identity nothing.
- 25 new tests; mutation-verified on constant-work verification and history enforcement.

### Step 1.3 — Sessions, throttling & the real AppContext
- **Throttle, never permanently lock (D2).** This product has no password-reset email, no help
  desk, and on a fresh install one administrator. A permanent lock is not a security feature —
  it is an outage an attacker triggers with five wrong guesses against a username printed on
  the shop's own invoices, recoverable only by hand-editing the database. The delay is capped
  and always elapses; `TestTheThrottleAlwaysElapses` is the guarantee.
- **Idle timeout is 60 minutes, revising the 30 I had proposed (D1).** A shop is quiet for
  stretches, and re-typing a twelve-character passphrase after every lull drives users to
  shorter passwords or switching the timeout off — the same failure the password policy avoids.
  A till wants a PIN-resumed screen lock (Phase 5), not a shorter timeout.
- **Tokens are 256-bit random, stored as SHA-256** (D3) — a backup must not be a set of working
  logins. Not Argon2: a high-entropy token has no dictionary to attack. Not a UUIDv7: v7 is
  time-ordered, which is right for a key and wrong for a bearer credential.
- **Absolute expiry is checked before idle**, so activity cannot mask it. Removing the check
  fails with *"a session survived past its absolute expiry because activity kept extending it"*.
- **No session cache**: revocation must take effect on the very next call, not when a cache
  entry expires.
- **`last_seen_at` written at minute granularity** (D4), so session upkeep does not put a write
  on every call behind SQLite's single writer.
- **A latent contract defect, found and fixed.** `Module.Jobs()` returned declarations with no
  handler and the composition root registered them as `Register(def, nil)` — which `Register`
  rejects. **No module could ever have declared a job**; identity's sweep is the first to try.
  Now `jobs.Registration{Def, Handler}`. An unused seam is an untested seam.
- **`appctx` reads a real session**, closing the item 0.10 §6.2 and 0.11 D4 both carried:
  settings now resolve at *user* scope, so two people sharing a machine get their own language.
- 21 new tests; mutation-verified on absolute expiry and throttle clearing.

### Step 1.4 — RBAC: permissions, roles & scope
Builds the **answer** to "may they?"; Step 1.5 builds the guarantee that anyone asks.
- **`Module.Permissions()` joins the contract**, closing the item 0.9 D5 deferred. Permissions
  are code-defined and reconciled at startup, which is what stops the list drifting from what
  the code actually checks. A duplicate code across modules is fatal; a row nothing declares is
  marked obsolete and reported.
- **A mutation drill caught my reasoning, not my code.** D2 justified obsolete-not-delete as
  "deleting would cascade to role_permissions" — **wrong**: grants reference the code as a plain
  string, so there is no FK and nothing cascades. The drill proved it by *passing*. The real
  reason is visibility: the grant survives either way, but only an obsolete row lets an
  administrator discover a role still grants something the software no longer implements.
  Test and design both corrected. *A mutation test that passes is telling you the test is wrong.*
- **The 0.5 archlint rule forced a better design.** Putting the auth types in identity's
  contract made `platform/modules` import a module, which the rule refused. They now live in
  `internal/platform/auth`: the **types are platform, the implementation is identity**, so a
  future sales module asks `auth.Authorizer` and never imports identity.
- **No superuser bypass.** Administrator holds `*`, resolved by the ordinary path — removing
  the grant removes the access. A bypass would hide resolution bugs from everyone testing as
  an admin, which during development is everyone.
- **Scope from the first implementation** (§14.2), with a table test pinning all nine cases —
  including that **branch does not cover warehouse**, deferred to Phase 4 where the first
  warehouse-scoped permission will define it.
- **Wildcards are grant-side only**: `Can(ctx, "sales.*")` is refused, or one typo unprotects a
  module.
- **`config.Authorizer`'s AllowAll stub is gone**, replaced by an adapter calling RBAC at global
  scope — the settings port stays scope-free because a setting is a company-wide fact.
- 20 new tests; mutation-verified on obsolete-not-delete and scope enforcement.

### Step 1.5 — The policy mechanism & enforcement
Step 1.4 built the **answer** to "may they?"; this builds the guarantee that **anyone asks**.
- **The guarantee is structural, not declarative (D1).** The phase design proposed declaring a
  policy per binding method and validating at startup that each has one — which proves a policy
  is *declared*, not *consulted*. Implementing it showed that was avoidable: every method
  already reached the graph through `resolve()`, so the **guard became the accessor**. A method
  that skips it has no database, no services, no context. Not "should not" — *cannot*.
- **The 0.11 test suite failing was the proof.** Seven assertions broke with
  *"failed after Attach: identity.session_invalid"* — exactly right: the graph being ready says
  nothing about who is asking. Split into a refuses-without-session test and a positive one
  that signs in through a real `Auth.Login`.
- **One session per process (D2).** A Wails binding has no request object; threading a token
  through every JS signature would put a credential in every call site. A desktop ERP has one
  user at the machine, so the process holds the token — and **it never crosses to the frontend**
  (D3), so it cannot be logged, stored in localStorage, or read by an injected script.
- **The public surface is three methods**, pinned by a test so widening it is a visible diff.
  Logout is public so a *just-expired* session can still clear itself; Me is public because
  "am I signed in?" is asked before the answer is known.
- **Preferences now resolve at user scope**, closing 0.11 D4's system-scope limitation — two
  people sharing a machine keep their own language and theme.
- Mutation-verified: skipping the `Can` call leaked the full job list to a permissionless user;
  renaming a policy key caught both the uncovered method and the orphaned entry at once.
- 16 new tests.

### Step 1.6 — Field-level redaction
A permission can now gate a **field within a response**, not just a whole method (§14.2).
- **Populate only if permitted, never populate-then-blank (D1).** The restricted fields are
  pointers with `omitempty`, and `redact.Visible` is the only thing that sets them. The property
  that buys: **forgetting the redaction call hides data rather than leaking it.** A blank-it-out
  design has the opposite failure mode, where every new code path is another place to forget and
  forgetting is silent.
- **Absent, not blank.** A blank `beforeJson` is indistinguishable from an entry that genuinely
  had no before-state, so the user cannot tell "nothing here" from "something here you may not
  see" — and only the second sends someone to ask.
- **The audit schema and read path land here**, because redaction needed a real consumer;
  writing entries stays in 1.7. Append-only is enforced by there being **no** Update or Delete
  method, not by a comment.
- **Manager is seeded with `audit.entry.view` but not `view_payload`** (D4), so the split is
  live in the default configuration rather than only in a test.
- **The two gates compose without knowing about each other**: the 1.5 guard decides whether you
  see the list, redaction decides how much of each row.
- Mutation-verified twice: removing the check leaked the salary values in full; dropping
  `omitempty` (keeping the pointer, isolating exactly the absent-vs-blank decision) failed with
  *"the safe state is not the default, so forgetting the call would LEAK"*.
- A count-based binding test broke for the **third** time in this codebase and was rewritten to
  assert by presence — a test that counts a growing collection is a maintenance tax.
- 12 new Go tests, 2 new frontend tests (123 total).

### Step 1.7 — The audit write path
An audited change and its audit record now **commit together or not at all** (phase D7).
- **Atomicity is inherited, not implemented.** A module publishes `Auditable` on the synchronous
  bus; the subscriber writes through `db.Writer(ctx)`, which inside a Unit of Work *is* the live
  transaction. Three Phase-0 guarantees compose and this step wrote almost no machinery of its
  own — which is what a foundation is for.
- **A failed audit write aborts the operation (D3).** Stated as a cost, not hidden: a shop that
  cannot record what it is doing should stop, not continue silently. The abort drill drops the
  audit table and asserts the old password still authenticates.
- **A service with no publisher fails at the first audited write**, rather than recording
  nothing quietly. An empty trail is a missing trail.
- **Every identity and org test now wires the audit subscriber**, not just the audit tests — the
  write happens inside all of those transactions, so ~40 tests exercise the D7 path instead of
  two. The §1.2 "an unused seam is an untested seam" lesson, applied in advance.
- **Payloads are hand-written projections**; the argon2 encoding and the session token are both
  in scope at publish sites and both asserted absent from the trail.
- `audit.DependsOn` corrected to **none** (D5): 1.6 declared `["identity"]`, but `audit_log`
  carries no foreign key to `users` — deliberately, so history survives a deleted account.
- Mutation-verified twice. Writing on a connection of its own **deadlocked** the rollback drill
  rather than merely failing it (SQLite has one writer) — that mistake cannot be shipped.
  Swallowing the subscriber error failed both abort drills.
- A count-based test broke for the **fourth** time and was rewritten to filter, not count.
- **Known gap, carried:** nothing forces a module to publish. A new write path that forgets
  leaves a silent hole, and it is not catchable by the enumeration trick that made 1.5's
  guarantee structural.
- 14 new Go tests.

### Step 1.8 — Country & business profiles, seed-file discovery
The mechanism §16.4 calls *"the concrete mechanism that satisfies generic ERP through
configuration, not code"*.
- **Closes the 0.5 D7 item**, deferred to 0.9, carried again at 0.9 and 0.12. Profiles are the
  first genuinely file-shaped seed, so this is where the loader gets built — and
  `Module.Metadata()` needed **no contract change**, the first Phase-0 seam to fit at first use.
- **Two layers**: what the binary embeds, and `<dataDir>/seeds/`. That second layer is what
  makes Addendum §C's "adding a country is dropping in a JSON file — no code, no release" true.
  Overrides are **whole-file**, never merged: a merged document has no legible author.
- **A broken shipped file is fatal; a broken user file is reported and skipped (D4).** A bug in
  our build must fail on our machine; a typo in a customer's file must not close their shop.
- **Country profiles are files, never a stored table (D1)** — read once at setup, then copied
  into settings. A stored row would be a second answer to "what is this company's date format?",
  which is exactly the hidden source of truth §C.2 forbids.
- **Business profiles split into a catalogue table and a bundle file (D2)**: the list is
  metadata the UI shows; the bundle is a script run once. `Apply` writes at company scope in one
  audited transaction.
- **Bundles are validated against the settings registry at LOAD (D5)**, not at apply — the same
  defect, caught where it is still cheap to fix.
- **Strict JSON**: an unknown key is reported, never ignored. A silently-ignored key is a value
  the customer believes they configured and the system never saw.
- `sy` (per §C.3) plus `sa`, `ae`, `eg`. **No shipped file carries a tax rate or a chart of
  accounts**, and a test asserts it. The non-registry defaults are unverified and flagged.
- Both business bundles are **empty on purpose**: no module has yet declared a setting that
  differs by trade. The mechanism ships; the data fills in Phase 3–5.
- Mutation-verified twice: making a user-file failure fatal broke the "a bad file must not stop
  the shop" tests at both levels; dropping `DisallowUnknownFields` broke the strictness test.
- **A test was passing for the wrong reason** — the settings registry is populated by package
  `init`, so a focused test declared fewer keys than production and a wrong-type check was never
  reached. Third occurrence of this pattern in the project.
- 24 new Go tests.

### Step 1.9 — The setup wizard backend
§WIZ's answer to the bootstrap paradox, built: no user exists, so setup bindings are **public
and callable only while no company does**.
- **`internal/api/setup`, not a module.** Setup composes org, identity, profile, and currency in
  one transaction, which `module-isolation` forbids from inside `internal/modules` — correctly,
  because setup owns no entities and is not a domain.
- **`setupGuard` is the accessor**, applying the 1.5 D1 trick one level in: a mutating Setup
  method that skips it has no graph. Forgetting produces a method that does nothing, not one
  that creates administrators unauthenticated.
- **One transaction.** A failure leaves **no company at all** — recoverable by running the
  wizard again — rather than a company with no administrator, which no screen can repair.
- **`Status` stays callable forever and answers less afterwards**; `Apply` refuses forever.
- **The business profile applies before the user's explicit choices**, so the person beats the
  trade's default.
- **Apply does not sign the user in** (D6): the login that follows proves the credential before
  the wizard closes. No default password ships, and a test tries five common ones.
- **Setup's audit entries record `system`** with no actor. Fabricating an attribution would
  imply decisions nobody made.
- **Closes a defect Step 1.8 shipped**: `sa`/`ae`/`eg` named SAR/AED/EGP, which currency never
  seeded, and companies carry an FK to `currencies(code)`. Choosing Saudi Arabia would have
  failed mid-transaction. Now seeded, and **the wizard is run once per shipped country** in test.
- **The first mutation drill changed the design.** Removing `setupGuard` broke nothing, because
  a third redundant check inside `setup.Apply` caught the call instead — a guard whose removal
  is invisible has already stopped working. The redundant check was deleted; the drill now fails
  correctly on the error code, which is what distinguishes the layers.
- **A test that proved nothing**, again (fourth time): the ordering test used an empty bundle, so
  it would have passed under either order. Its fixture now drops a real bundle through 1.8's
  overlay layer.
- **A fifth count-based test broke** (`currencies == 4`) and was rewritten to compare boots,
  which is what idempotency actually means.
- 20 new Go tests.

---

## 7. Repository map (as built)

```
Mizan ERP/
├── main.go, shell.go, wails.json       # Wails desktop shell (boot inversion, 0.11)
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
│   ├── platform/i18n/                  # message catalog + user-content translations
│   ├── platform/modules/               # the Module contract every business module implements
│   ├── modules/currency/               # first business module (own migrations, domain, infra)
│   ├── modules/org/                    # company, branches, warehouses, fiscal calendar
│   ├── modules/identity/               # users, credentials, password policy, contract
│   ├── platform/crypto/                # Argon2id password hashing
│   ├── platform/auth/                  # permission, scope, Authorizer port
│   ├── platform/paths/                 # OS app-data locations
│   ├── platform/ui/                    # presentation preferences (ui.theme)
│   ├── api/envelope/, api/appctx/      # result envelope + per-request context
│   ├── api/bindings/                   # static Wails façades + the policy guard
│   ├── api/policy/                     # what each binding method requires
│   ├── api/redact/                     # field-level redaction (absent, not blank)
│   ├── modules/audit/                  # the append-only trail (read path)
│   ├── bootstrap/                      # the composition root
│   └── api/ (reserved)  modules/ (reserved)  bootstrap/ (reserved)
├── locales/{en,ar}/                     # go:embed'd catalogs, shared with the frontend
├── migrations/                         # go:embed'd schema, one dir per dialect
│   └── sqlite/0001_platform.sql, 0002_outbox_deliveries.sql  (+ module-owned 0003)
├── frontend/                           # Vite + React + TS + Tailwind + Radix
│   └── src/{app,shared/ui,lib/wails,modules}/   # shell, primitives, bindings, screens
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

- **Immediate next: Steps 1.10–1.11 — the frontend**: router, auth gates, the login screen, the
  setup wizard screens, and the first admin screens. **Design first, await approval.**
- **`Apply` needs `settings.Reload`** after its transaction, because the cache was loaded before
  the rows were written. First real occurrence of the case 0.5 flagged.
- **The shipped country defaults are unverified** (1.8 §3.3): date format, first day of week,
  and pricing currency for `sy`/`sa`/`ae`/`eg`. Each is one line in one file and is shown to the
  user during setup. Confirm or correct before the first customer install.
- **Both business bundles are empty** and fill as modules declare trade-specific settings.
- **Nothing forces a module to publish `Auditable`** (1.7 §6). A new write path that forgets
  leaves a silent hole in the trail. The mitigations that would work are a review checklist item
  now and a per-module "state changes publish" test later; neither is built.
- **Built but not yet reached by any production path:** sessions. `Validate` is fully tested
  and nothing calls it — the binding decorator that stamps `appctx.WithActor` is Step 1.5, and
  the login screen is 1.10. Same for `must_change`, carried from 1.2.
- **Closed in Step 1.1:** REPO.3 rule 2 (`module-isolation`) and rule 7 (restated as `no-sql`)
  are both enforced and verified by planting. **All seven REPO.3 rules now hold.**
- **One manual launch** to visually confirm the shell before Phase 1 UI work.
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
(pending) Phase 1 Step 1.7: audit write path — in-transaction subscribers
(pending) Phase 1 Step 1.6: field-level redaction and the audit read path
(pending) Phase 1 Step 1.5: the policy mechanism and enforcement
(pending) Phase 1 Step 1.4: RBAC — permissions, roles, and scope
(pending) Phase 1 Step 1.3: sessions, throttling, and the real AppContext
(pending) Phase 1 Step 1.2: identity — Argon2id credentials and users
(pending) Phase 1 Step 1.1: architecture rules + the org module
(pending) Phase 0 Step 0.12: Definition-of-Done review — Phase 0 complete
(pending) Phase 0 Step 0.11: frontend foundation + bindings wrapper
5ac657c  Phase 0 Step 0.10: composition root + full graph wiring
a4cd7ff  Phase 0 Step 0.9: currency module
6b3b773  Phase 0 Step 0.8: i18n platform + seed locales
8b769c9  Phase 0 Step 0.7: durable job scheduler + outbox dispatch job
2a7b00b  Phase 0 Step 0.6: event bus + transactional outbox
4160a7a  Phase 0 Step 0.5: configuration & metadata registries
f71384d  Phase 0 Step 0.4: migration runner + desktop safety layer
b763230  docs: add PROGRESS.md — running status / resume-point
299d075  Phase 0 Step 0.3: database platform (pools, dialect, Unit of Work)
ebd263d  Phase 0 Step 0.2: numeric kernel (money, quantity, rounding)
cbd68d3  Phase 0 Step 0.1: repo scaffold, arch-rule framework, Wails shell
```
