# Step 0.3 — Database Platform (Design)

> Status: **APPROVED — all six decisions accepted as recommended. Implemented; see the
> IMPLEMENTATION RECORD at the end.**
> Scope: `internal/platform/database` — connections, dialect shim, Unit of Work, the
> executor abstraction, error translation, and the repository contract test harness.
> **Out of scope:** the migration runner (Step 0.4), any business schema (Step 0.9+).

This is the layer every repository in the system persists through. It is where the
**database-portability contract** (ARCHITECTURE_v1 §8) stops being a document and becomes
enforced code, and where the transaction model that keeps the ledger consistent is defined.
It contains **no business logic and no business tables** — only the machinery the modules
plug into.

---

## 1. ANALYSIS

### 1.1 What this layer must guarantee

1. **Portability is real, not aspirational.** SQLite is the only driver in v1, but no
   repository may contain a SQLite-specific construct. The handful of genuinely
   dialect-specific concerns are funnelled through one small interface; everything else is
   portable SQL against the §8.1 type contract.
2. **Transactions are correct and invisible.** A use case wraps work in one transaction; the
   domain and repositories never see a `*sql.Tx`. On SQLite's single writer, the model must
   never deadlock itself.
3. **Reads and writes are separated at the connection level.** SQLite allows exactly one
   writer; the pool design turns write contention into a bounded queue wait, not an error,
   while reads scale across a separate pool.
4. **Driver errors become typed, portable errors.** "Unique violation" must be catchable by
   business code identically whether the driver is SQLite or PostgreSQL — the SQLite error
   code and the PostgreSQL SQLSTATE are translated to the same `kernel/errs` category.
5. **The contract is testable and dialect-agnostic.** A shared test suite defines what a
   correct repository/DB does; any future dialect must pass the identical suite. This is the
   single mechanism that will keep the PostgreSQL promise honest.
6. **No cgo.** `modernc.org/sqlite` (pure Go), per the approved Step 0.1 decision — one static
   binary, offline builds, no C toolchain.

### 1.2 Risks and how the design answers each

| Risk | Consequence if unhandled | Design answer |
|---|---|---|
| A SQLite-ism leaks into a repo | PostgreSQL migration becomes a rewrite | Dialect shim + portable-SQL discipline + the contract suite that must pass on every dialect |
| `SQLITE_BUSY` under concurrency | Random write failures surface to users | **Single-writer pool** (`MaxOpenConns(1)`) + `busy_timeout`; writes queue, not fail (§2.2) |
| A second `BEGIN` inside a transaction | Deadlock on the single writer | UoW **joins** an in-flight transaction rather than opening a second (§3.3) |
| Threading `*sql.Tx` through business code | Domain coupled to persistence; easy to bypass a transaction | Transaction lives in `context.Context`; repos resolve the executor from context (§3) |
| Foreign keys silently off | Orphaned rows, corrupt ledger | `PRAGMA foreign_keys=ON` **per connection** via the connector hook (§2.2) |
| Driver errors compared by string | Fragile, non-portable error handling | Dialect `TranslateError` maps to typed `kernel/errs` (§4) |
| WAL file not checkpointed on close | Growing `-wal` file; unclean reopen | Ordered close: checkpoint → close pools (§2.4) |

### 1.3 Architectural implications

- Every future module's `infra/sqlite` repository depends on this package and nothing else
  for persistence. Getting the seams right here removes friction from all 18 modules.
- The **executor abstraction** (§3.2) is what lets a repository method be written once and run
  correctly whether or not it is inside a transaction — the difference between a clean data
  layer and one riddled with "with-tx" and "without-tx" duplicates.
- The dialect interface is the **entire** surface a future PostgreSQL port must implement (plus
  a new migrations directory). Keeping it ~12 methods is what makes that port a bounded task.

### 1.4 Future-compatibility check (roadmap in ARCHITECTURE_v1 §26, §8)

| Future need | How this design absorbs it |
|---|---|
| PostgreSQL / MySQL / SQL Server | Implement the `Dialect` interface + a migrations dir; the contract suite validates it. `Writer()`/`Reader()` collapse to one pool on a real client-server DB. |
| Multi-branch / cloud sync | The UoW + (later) outbox write in one transaction; nothing here assumes a single node. |
| Read scaling / reporting | Reader pool is already separate; a read replica later is a Reader that points elsewhere. |
| High write throughput | The single-writer abstraction is SQLite-specific and vanishes on a server DB with no API change. |

### 1.5 Recommendation before design

Build the transaction model on **implicit propagation through `context.Context`**, with an
**executor resolved from context**, rather than passing `*sql.Tx` explicitly through every
repository signature. It is the standard idiom for clean Go data layers, it keeps the domain
and repository interfaces free of persistence types, and it structurally prevents "forgot to
use the transaction" bugs. This is the decision I most want your agreement on (D1).

---

## 2. DESIGN — connections

### 2.1 The `database.DB` surface

```go
// internal/platform/database
type DB interface {
    // Writer is the single-writer pool: all writes and all transactions use it.
    Writer() *sql.DB
    // Reader is the read-only pool for list screens and reports.
    Reader() *sql.DB
    // Dialect exposes the small set of database-specific behaviours (§5).
    Dialect() Dialect
    // Ping verifies connectivity.
    Ping(ctx context.Context) error
    // Close checkpoints the WAL and closes both pools, in order.
    Close() error
}
```

`Writer()`/`Reader()` return `*sql.DB` (not a custom type) so repositories use the standard
library directly; the abstraction is in *which* pool and *how it is configured*, not in
wrapping every call.

### 2.2 SQLite configuration — where correctness is won

Applied on **every** physical connection via a `modernc.org/sqlite` connector hook (PRAGMAs
are per-connection in SQLite, so setting them once on the pool is not enough):

```
PRAGMA journal_mode = WAL;      -- readers don't block the writer
PRAGMA synchronous = NORMAL;    -- safe under WAL, far faster than FULL
PRAGMA foreign_keys = ON;       -- MUST be per-connection
PRAGMA busy_timeout = 5000;     -- wait, don't error, on contention
PRAGMA temp_store = MEMORY;
PRAGMA cache_size = -64000;     -- ~64 MB
PRAGMA journal_size_limit = 67108864;
```

**Two pools, deliberately asymmetric** (ARCHITECTURE_v1 §8.4):

| Pool | `MaxOpenConns` | Mode | Used for |
|---|---|---|---|
| **Writer** | **1** | read-write | all writes, all transactions |
| **Reader** | N (default 4) | read-only (`mode=ro`) | list/report queries outside a transaction |

The single-writer pool is the crux: SQLite serialises writers anyway, so allowing only one
connection converts a would-be `SQLITE_BUSY` into a Go-level queue wait on the pool. Writes
become reliably serial instead of randomly failing under load. On a future PostgreSQL, both
methods return the same pool and the constraint simply disappears — no repository changes.

### 2.3 Boot configuration

The DB is constructed from a small config resolved by `platform/config` (Step 0.5) — database
file path (under the app data dir, `platform/fs`), reader pool size, and a busy-timeout knob.
Nothing here reads global state; the config is passed into the constructor (manual DI, §6 of
the foundation doc).

### 2.4 Lifecycle

- **Open:** create both pools, apply PRAGMAs via the connector, `Ping` to fail fast on a bad
  path or a locked file.
- **Close:** `PRAGMA wal_checkpoint(TRUNCATE)` on the writer, then close reader then writer.
  A desktop app killed mid-write must reopen clean; an unbounded `-wal` file is a support call.

---

## 3. DESIGN — the Unit of Work and the executor

### 3.1 The transaction boundary belongs to the application layer

A use case owns one transaction; the domain never opens one. The interface is deliberately
tiny:

```go
type UnitOfWork interface {
    // Do runs fn inside a single write transaction. It commits on nil error and rolls
    // back on any error or panic. Nested Do calls JOIN the in-flight transaction (§3.3).
    Do(ctx context.Context, fn func(ctx context.Context) error) error
}
```

Usage (illustrative — the sales module, a later phase):

```go
func (h *PostInvoiceHandler) Handle(ctx context.Context, cmd PostInvoice) error {
    return h.uow.Do(ctx, func(ctx context.Context) error {
        inv, err := h.invoices.FindByID(ctx, cmd.ID) // repo reads the tx from ctx
        if err != nil { return err }
        if err := inv.Post(h.clock.Now(), cmd.By); err != nil { return err }
        return h.invoices.Save(ctx, inv)             // same tx, transparently
    })
}
```

The domain object (`inv.Post`) and the repository (`Save`) never mention a transaction. The
`Save` either fully happens or, on any returned error, nothing does.

### 3.2 The executor — one method written once, correct in or out of a transaction

Repositories never touch `Writer()`/`Reader()` directly. They ask the package for an executor
bound to the current context:

```go
// Executor is the subset of *sql.DB / *sql.Tx that repositories use.
type Executor interface {
    ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
    QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
    QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
    PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
}

// Writer returns the active transaction if ctx is inside one, else the writer pool.
func (db) Writer(ctx context.Context) Executor
// Reader returns the active transaction if ctx is inside one (read-your-writes),
// else the read-only pool.
func (db) Reader(ctx context.Context) Executor
```

Both `*sql.DB` and `*sql.Tx` already satisfy `Executor`, so this is zero-cost. The rule for a
repository author is simple and hard to get wrong: **writes and aggregate loads use
`Writer(ctx)`; list/report reads use `Reader(ctx)`.** Inside a `Do`, both resolve to the
transaction, so a use case reads its own uncommitted writes; outside, they split across pools.

The transaction is carried in `context.Context` under an unexported key — inaccessible to
business code, which can neither forge nor extract it. That opacity is the point.

### 3.3 Nested transactions — join, don't nest **[decision D5]**

If `Do` is called while a transaction is already in the context, it **joins** it: runs `fn`
with the same transaction, and commit/rollback happens only at the outermost `Do`. It does
**not** open a second transaction (which would deadlock the single writer) and, in v1, does
**not** create a `SAVEPOINT`.

Rationale: joining is what you want almost always — one user action is one atomic unit even if
it composes several handlers. Partial rollback via savepoints is a real feature but a rare one,
it adds surface, and its semantics differ across dialects. I recommend **join-only in v1**, with
savepoint support added behind an explicit `DoNested`/opt-in if and when a concrete use case
needs partial rollback. Flagging for your call.

### 3.4 Panic safety

`Do` recovers a panic, rolls back, and re-panics after the rollback — a panic must never leave
a dangling open transaction holding the single writer. (This is the one place a recover is
legitimate; the `no-panic` archlint rule already exempts non-business infrastructure.)

---

## 4. DESIGN — portable error translation

Driver errors are not portable: SQLite reports a unique-constraint breach as extended result
code `2067` (`SQLITE_CONSTRAINT_UNIQUE`); PostgreSQL reports SQLSTATE `23505`. Business code
must not care. The dialect translates:

```go
// On the Dialect interface:
TranslateError(err error) error
```

It maps a driver error to a typed `kernel/errs` value with a stable category:

| Condition | Mapped to |
|---|---|
| Unique / primary-key violation | `errs.Conflict(ErrCodeDuplicate, …)` |
| Foreign-key violation | `errs.Conflict(ErrCodeReference, …)` |
| Not-null / check violation | `errs.Invalid(…)` |
| `sql.ErrNoRows` | left as-is; repositories translate to their own `NotFound` (a missing aggregate is a domain concept, not a driver one) |
| anything else | `errs.Internal(…)`, original wrapped for logs |

Repositories call `db.Dialect().TranslateError(err)` at their persistence boundary. The result:
"insert a second product with the same code → `Conflict`" behaves identically on every database,
and the business layer branches on `errs` categories, never on driver specifics.

`kernel/errs` itself is currently a package marker (Step 0.1); **Step 0.3 will implement the
minimal `errs` surface it needs** (typed categories + codes + `Is`/`As`), since the DB layer is
its first real consumer. This is a small, in-scope addition, noted here for transparency.

---

## 5. DESIGN — the dialect shim (the whole portability surface)

One interface, ~12 methods, is the complete set of database-specific behaviour. Nothing else
in the codebase is allowed to be dialect-aware.

```go
type Dialect interface {
    Name() string                                   // "sqlite"

    // SQL construction that genuinely differs between engines:
    Placeholder(n int) string                       // "?"  (SQLite/MySQL) | "$1" (Postgres)
    Rebind(query string) string                     // convert '?' placeholders to native form
    QuoteIdentifier(name string) string             // "table" | `table` | [table]
    LimitOffset(limit, offset int64) string         // "LIMIT ? OFFSET ?" | "OFFSET ? FETCH …"
    Upsert(spec UpsertSpec) string                  // dialect-correct insert-or-update
    CurrentTimestampExpr() string                   // portable "now" for defaults, if ever needed
    ForUpdateClause() string                        // "" (SQLite) | "FOR UPDATE" (Postgres) — §9.4 numbering

    // Semantics:
    TranslateError(err error) error                 // §4
    BooleanLiteral(b bool) string                   // "1"/"0" per the §8.1 boolean contract
    SupportsReturning() bool                         // false (SQLite v1) | true (Postgres) — informs strategy
}
```

- **`Rebind`** lets repositories write one placeholder style (`?`) everywhere; the dialect
  rewrites to `$n` for PostgreSQL. Repositories stay dialect-neutral in the common case.
- **`Upsert`** is abstracted because "insert or update" is the single most divergent portable
  operation (SQLite/Postgres `ON CONFLICT`, MySQL `ON DUPLICATE KEY`, SQL Server `MERGE`).
  §8.2's ban on `INSERT OR REPLACE` is honoured by routing every upsert through here.
- **`ForUpdateClause`** is what makes gapless numbering (§9.4) portable: a no-op on SQLite
  (single writer already serialises), a real `SELECT … FOR UPDATE` on PostgreSQL.

Step 0.3 ships the **`sqlite` implementation only**; the interface existing and being exercised
by the contract suite is what proves it is sufficient before any second dialect is attempted.

---

## 6. DESIGN — repository & read-model split (the pattern, not the queries)

No business tables exist yet, so Step 0.3 fixes the **conventions and helpers** every later
module will follow (ARCHITECTURE_v1 §11, §27):

- **Aggregate repositories** — hand-written SQL, load/save whole aggregates, use `Writer(ctx)`,
  version-checked writes (optimistic concurrency via `row_version`, §6.1). Declared as
  interfaces in each module's `domain`; implemented in `infra/sqlite`.
- **Read models** — flat DTOs for lists/reports, use `Reader(ctx)`, never load an aggregate.
  Hand-written in v1, structured to be swapped to **`sqlc`-generated** code once a schema
  exists (Step 0.9+). `sqlc` is **not** wired in this step — it needs tables to generate
  against — but the read-side is written so adopting it later is mechanical (D4).

Step 0.3 provides small, shared helpers so repositories stay thin and consistent:
- an **optimistic-update helper** (`UPDATE … WHERE id=? AND row_version=?`, returns a typed
  `ErrConcurrentModification` when zero rows change);
- row-scanning helpers and a `pagelist` helper that applies `paging.Page`/`Sort` via the
  dialect's `LimitOffset`;
- a transaction-aware `exists`/`get` helper pair.

### 6.1 Optimistic concurrency

Every business table carries `row_version` (ARCHITECTURE_v1 §9.1). The update helper checks and
increments it in one statement; a zero-row result means someone else wrote first, surfaced as a
typed conflict the use case can retry or report. Two cashiers editing the same document get a
clean error, never a lost update. This helper lives here so all modules get it identically.

---

## 7. DESIGN — the repository contract test suite (the portability guarantee)

This is the most important deliverable of Step 0.3, because it is what makes "portable" a
checked fact rather than a hope.

- A **dialect-agnostic behavioural suite** exercises the DB platform against a **throwaway test
  table** created directly in test setup (not via the migration runner, which is Step 0.4). The
  suite asserts: insert/get/update/delete round-trips; optimistic-concurrency conflict; unique
  violation → `Conflict`; FK violation → `Conflict`; `Do` commits on success and rolls back on
  error and on panic; nested `Do` joins (one atomic unit); `Reader(ctx)` inside `Do` sees
  uncommitted writes; `Reader(ctx)` outside does not; `LimitOffset` paging; `Rebind`/`Upsert`
  correctness.
- The suite is written to take a **`DB` under test**, so the day a PostgreSQL dialect exists it
  runs the *identical* suite by pointing at a test PostgreSQL — the new dialect is correct iff it
  passes what SQLite passes.
- **Test isolation:** each test gets a fresh temporary database file (WAL wants a real file);
  created in the scratch dir, removed on cleanup. Fast enough to run per-test (ARCHITECTURE_v1
  §30).

---

## 8. DESIGN — package layout

```
internal/platform/database/
├── db.go            # DB interface + open/close/ping; pool construction & PRAGMAs
├── config.go        # DBConfig (path, reader pool size, busy timeout)
├── executor.go      # Executor interface + Writer(ctx)/Reader(ctx) resolution
├── uow.go           # UnitOfWork + context tx key + panic-safe Do (join semantics)
├── dialect/
│   ├── dialect.go   # Dialect interface + UpsertSpec
│   └── sqlite.go    # the SQLite implementation (only dialect in v1)
├── dberr.go         # driver-error → kernel/errs translation helpers
├── helpers.go       # optimistic-update, scan, paging, exists/get helpers
└── dbtest/          # the reusable contract suite (imported by tests)
    └── suite.go
```

Plus a minimal implementation of `internal/kernel/errs` (the typed error surface the DB layer
needs — §4). `database` imports `kernel/errs`, `kernel/paging`, and the SQLite driver;
`dialect` is driver-aware; nothing else in the codebase imports the driver (enforceable by the
`import-boundary` rule — I will add a `no-driver-outside-infra`-style entry covering
`platform/database` as the sole allowed importer).

**Dependencies added:** `modernc.org/sqlite` (pure-Go driver). No cgo. No ORM.

---

## 9. TESTING PLAN (Protocol Step 4)

| Level | What |
|---|---|
| **Unit** | Dialect methods (`Rebind` `?`→`$n`, `QuoteIdentifier`, `LimitOffset`, `Upsert`, `BooleanLiteral`); error translation for each mapped condition; executor resolution (tx-in-context vs pools). |
| **Integration** | The full contract suite (§7) against real SQLite temp files: CRUD, optimistic conflict, unique/FK → `Conflict`, `Do` commit/rollback/panic, nested-join atomicity, read-your-writes, paging. |
| **Concurrency** | Parallel writers serialise without `SQLITE_BUSY` errors (proves the single-writer-pool decision); concurrent readers proceed during a write. |
| **Lifecycle** | Open→ping→close leaves no `-wal`; reopening a closed DB errors cleanly. |

Confidence-first, per the agreed standard: the concurrency and transaction-semantics tests
matter far more than line coverage of getters.

---

## 10. DECISIONS REQUESTED

Please confirm (or redirect). D1 and D5 are the substantive ones.

1. **D1 — implicit transactions via `context.Context` + executor-from-context**, rather than
   passing `*sql.Tx` explicitly through repository signatures. *(Recommended; §3.)*
2. **D2 — the `Executor` abstraction** with `Writer(ctx)`/`Reader(ctx)` resolution as the sole
   way repositories obtain a DB handle. *(§3.2.)*
3. **D3 — dialect `TranslateError`** mapping driver errors to typed `kernel/errs` categories,
   and implementing the minimal `kernel/errs` surface now as its first consumer. *(§4.)*
4. **D4 — defer `sqlc`** to when a schema exists (Step 0.9+); hand-write read models now in a
   sqlc-ready shape. *(§6.)*
5. **D5 — nested `Do` joins the in-flight transaction, no `SAVEPOINT` in v1** (partial rollback
   added later only if a concrete need appears). *(§3.3.)*
6. **D6 — reader pool size default 4**, single writer; rely on `busy_timeout` (no app-level
   retry loop in v1). *(§2.2.)*

On approval I'll implement in this order, each independently reviewable: minimal `kernel/errs`
→ `dialect` (interface + SQLite) → `DB` (pools + PRAGMAs + lifecycle) → executor + UoW →
error translation + helpers → the `dbtest` contract suite, tests alongside each, then the
self-review and improvement notes (Protocol Steps 5–6).

---

*End of Step 0.3 design. Awaiting approval per Protocol Step 2 before any implementation.*

---

# IMPLEMENTATION RECORD (Protocol Steps 3–6)

> Status: **IMPLEMENTED and verified.** Full local CI green, offline.

## Step 3 — What was built

- `internal/kernel/errs` — the typed, categorised error surface (Category, Error, builders,
  `Wrap`, `IsCategory`/`CategoryOf`/`CodeOf`). The DB layer is its first consumer.
- `internal/platform/database/dialect` — the `Dialect` interface (the entire portability
  surface) + the SQLite implementation, including `TranslateError` mapping driver codes to
  typed errors, and `Upsert`.
- `internal/platform/database` — `Store` (single-writer + reader pools, per-connection PRAGMAs
  via the modernc DSN), the `Executor` abstraction with `Writer(ctx)`/`Reader(ctx)` resolution,
  the panic-safe join-semantics `Do` (Unit of Work), lifecycle (Ping/Close+WAL checkpoint), and
  the `VersionedUpdateResult` optimistic-lock helper.
- `internal/platform/database/dbtest` — the reusable, dialect-agnostic contract suite.

## Deviations from the approved design (flagged per protocol)

1. **`Writer()`/`Reader()` renamed vs the §2.1 sketch.** The context-aware executor methods are
   `Writer(ctx)`/`Reader(ctx)` (the primary repo API); the raw pool is exposed as
   `WriterPool() *sql.DB` for the migration runner. This avoids a name clash between the two
   concepts the design described separately. Same behaviour, clearer surface.
2. **Reader pool is read-write, not `mode=ro`.** SQLite WAL + a read-only connection needs a
   writable `-shm`/`-wal`, which is fragile. The reader pool opens read-write; "read-only" is a
   usage convention (repos use `Reader(ctx)` for SELECTs). The real value — a single serialised
   writer plus concurrent WAL readers — is unchanged, and on a server DB a true read-only role
   enforces it. Documented in `db.go`.
3. **Dropped `CurrentTimestampExpr` and `SupportsReturning` from the dialect (YAGNI).**
   Timestamps are written from `kernel/clock` (Go side), and repositories take the portable path
   (UUIDs generated in Go, no `RETURNING`), so neither method has a caller. They can be added
   additively when a dialect needs them.

## Step 4 — Testing (confidence-first)

- **Contract suite (13 subtests)** against live SQLite temp files: InsertGet, unique→Conflict,
  FK→Conflict, NOT NULL→Validation, optimistic-concurrency→Conflict, Do commit / rollback-on-error
  / rollback-on-panic (and re-panic), nested-Do join atomicity, read-your-writes + isolation,
  LimitOffset paging, Upsert, and 20-way concurrent writes serialising with **no SQLITE_BUSY**.
- **Dialect unit tests** for the pure string methods and Upsert SQL.
- **errs** unit tests: classification, wrap/unwrap through `fmt.Errorf`, builders, foreign-error
  defaults.
- Aggregate statement coverage ~83%; the remainder is unreachable driver-error paths and trivial
  one-line constructors — effort went to the transaction-semantics and error-mapping behaviour
  that actually matters.

## Step 5 — Self-review findings

- Verified `Do` **never opens a second transaction** when nested (join), and that both the
  error and panic paths roll back (tested) — the single-writer deadlock trap is closed.
- Verified the transaction is **opaque** to callers (unexported context key; no accessor).
- Verified **FK enforcement is actually on** (the FK test fails loudly if `foreign_keys` were
  off — it is applied per connection via the DSN).
- Verified the single-writer pool **serialises** concurrent writers rather than erroring.
- Kept the `uow.go` re-panic (cleanup + re-raise) rather than distorting it to satisfy the
  no-panic rule; added a narrow, documented arch-rule exemption for that one file.

## Step 6 — Improvement notes, risks, opportunities

Recorded, not needed now:
1. **`SAVEPOINT`-based partial rollback** (`DoNested`) — deferred until a real use case needs it
   (D5). The join default is correct for the common case.
2. **`sqlc` for read models** — adopt once a business schema exists (Step 0.9+); the read side is
   written to make this mechanical (D4).
3. **A future PostgreSQL dialect** implements `Dialect` + a migrations dir and must pass the
   identical `dbtest` suite — the portability guarantee is now executable.
4. **`kernel/paging` helper** (`LimitOffset` from a `paging.Page`) — add when `kernel/paging` is
   implemented and the first list query needs it.

## Technical debt

None introduced. `kernel/errs` was implemented minimally but completely for its current
consumers; it will grow additively (e.g. an i18n params renderer) without rework.
