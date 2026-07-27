# Mizan ERP — Progress & Status

> **Running status / resume-point document.** Read this first when picking the project back up.
> Last updated: **2026-07-27**. Branch: `main`. Everything below is committed and verified
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
| **0.4** | Migration runner + desktop safety layer + `0001_platform.sql` | ⬜ next — design first |
| **0.5** | Config registries (settings + metadata) + strategy registry | ⬜ |
| **0.6** | Event bus + transactional outbox | ⬜ |
| **0.7** | Durable job scheduler + outbox dispatcher | ⬜ |
| **0.8** | i18n platform + seed locales | ⬜ |
| **0.9** | Currency module (schema `0002`, repos, converter, seeds) | ⬜ |
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

---

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
│   ├── kernel/                         # money, quantity, round, errs, internal/fixed,
│   │                                   #   + markers: id, clock, event, locale, validation, paging
│   ├── platform/database/              # DB pools, dialect, executor, uow, helpers, dbtest
│   └── api/ (reserved)  modules/ (reserved)  bootstrap/ (reserved)
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

- **Immediate next:** Step 0.4 — migration runner (goose, embedded) + the desktop safety layer
  (auto-backup → integrity-check → migrate → auto-restore on failure) + `0001_platform.sql`.
  **Design first, await approval** per the protocol.
- **Deferred (tracked):** `sqlc` for read models (0.9+); `kernel/paging` helper; SAVEPOINT-based
  partial rollback (only if needed); the real GitHub/remote integrations (optional, user's call).
- **Before Phase 2 (financial spine):** need the first customer's real tax rates / chart of
  accounts, or confirmation to ship an empty, user-configured tax profile.

---

## 10. Commit history (Phase 0)

```
299d075  Phase 0 Step 0.3: database platform (pools, dialect, Unit of Work)
ebd263d  Phase 0 Step 0.2: numeric kernel (money, quantity, rounding)
cbd68d3  Phase 0 Step 0.1: repo scaffold, arch-rule framework, Wails shell
```
