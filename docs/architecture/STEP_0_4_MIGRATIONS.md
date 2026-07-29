# Step 0.4 — Migration Runner & Platform Schema (Design + implementation record)

> Status: **APPROVED and IMPLEMENTED.** Design approved 2026-07-30. The implementation
> record, and the one decision that was **amended** during implementation, are at **§11**.
> (Permanent Development Protocol, Steps 2–6.)
> Scope: `internal/platform/migrate` — the migration runner and its desktop safety layer —
> plus `migrations/sqlite/0001_platform.sql`, the platform-owned tables.
> **Out of scope:** any business schema (currency lands in Step 0.9; org/identity in Phase 1),
> the settings/metadata registries (Step 0.5), backup/restore *as a user feature* (Phase 9).

This step decides how a customer's database changes shape over the product's life. It is the
one piece of infrastructure whose failure mode is **permanent loss of a business's data**, so
the design is dominated by that risk rather than by convenience.

---

## 1. ANALYSIS

### 1.1 What makes this different from server-side migrations

Mizan is a **desktop application**, and every assumption a server migration tool makes is
false here:

| Server assumption | Desktop reality |
|---|---|
| A DBA runs migrations deliberately | The **user** triggers them by double-clicking an app icon, unaware |
| Failures are noticed and fixed by an operator | Nobody is watching; the shop owner sees a broken app at 8am |
| A backup/restore process exists | Whatever we do is the only safety net |
| Downtime is acceptable and planned | The POS must open, now |
| One canonical database | Every customer has their own, on unknown hardware, possibly on a failing disk |
| Versions roll forward under control | A customer may install an **older** build over a newer database |

The conclusion that shapes everything below: **the runner must assume it will fail on someone's
machine, and must guarantee that when it does, the business's data is still there.**

### 1.2 What this layer must guarantee

1. **Never lose data.** A failed migration leaves the database exactly as it was, and the user
   is told what happened and where their backup is.
2. **Atomic per migration.** Each migration and its version record commit together, or neither.
3. **Deterministic and immutable history.** Migrations are forward-only, numbered, and never
   edited after shipping; a modified historical migration is detected and refused.
4. **Portable.** Migrations are per-dialect files; nothing in the runner is SQLite-specific
   beyond the dialect it is handed (ARCHITECTURE_v1 §10.5).
5. **Honest about state.** The app must refuse to run against a database newer than the binary
   understands, rather than corrupting it.
6. **Observable.** The user sees progress on a long run; a developer sees exactly which
   migration failed and why.

### 1.3 Risks and how the design answers each

| Risk | Consequence if unhandled | Design answer |
|---|---|---|
| Migration fails halfway | Corrupt schema, unusable app, lost business data | **Backup → verify → migrate → auto-restore on failure** (§4) |
| Disk already corrupt before we start | We back up corruption, then make it worse | `PRAGMA integrity_check` **before** anything (§4.1) |
| A shipped migration is edited later | Two customers on "version 3" with different schemas — silent, permanent divergence | **Checksums** recorded and verified on every boot (§3.3) |
| Old binary opens a newer DB | Writes rows the new schema forbids; corrupts data | **Version gate** — refuse to open (§5) |
| Partial DDL under SQLite | SQLite *does* support transactional DDL, but a `PRAGMA`-heavy migration can escape it | Each migration in **one transaction**; no PRAGMA-in-migration (§3.2) |
| Long migration on a big DB | Frozen splash screen; user force-quits mid-write | **Progress reporting** + never force-quit-friendly (§6) |
| Two app instances migrating at once | Interleaved DDL, corruption | **Single-writer pool + an advisory lock** (§3.4) |
| Backups accumulate forever | Disk fills; the app that "protects" data breaks the machine | **Retention policy** on migration backups (§4.4) |

### 1.4 Architectural implications

- The runner sits **above** `platform/database` (it consumes a `*Store`) and **below**
  everything else. It runs once, early, in the composition root (Step 0.10), before any module
  is constructed.
- **Module-owned migrations with global ordering** (ARCHITECTURE_v1 §10.3): each module ships
  its own `.sql` files, but version numbers are globally sequenced so cross-module foreign keys
  always apply in a valid order. The runner is handed one merged, ordered set.
- The **backup mechanism built here is deliberately minimal** — a physical, consistent copy for
  migration safety only. The *user-facing* portable `.mizanbak` backup (logical export, §29) is
  a Phase 9 feature and a different thing. I want that distinction explicit so we don't
  accidentally build the wrong one twice.

### 1.5 Recommendation before design

Use **`pressly/goose` as an embedded library**, wrapped in our own safety layer, rather than
writing a migration runner from scratch. Goose gives transactional migrations, `embed.FS`
support, a version table, and Go-code migrations — all solved, battle-tested problems. What it
does *not* give is the desktop safety layer (§4), the checksum enforcement (§3.3), or the
version gate (§5), which is exactly what we add. Writing our own runner would be ~300 lines of
undifferentiated code to maintain and test forever, for no gain. **D1.**

---

## 2. DESIGN — the shape

```
                 ┌──────────────────────────────────────────────┐
   bootstrap ───▶│  migrate.Runner                              │
                 │                                              │
                 │  1. integrity_check        (abort if corrupt) │
                 │  2. read current version   (version gate)     │
                 │  3. if pending == 0 → return (fast path)      │
                 │  4. BACKUP + verify        (safety net)       │
                 │  5. verify checksums       (history intact)   │
                 │  6. apply each migration   (one tx each)      │
                 │  7. on failure → RESTORE from backup, report  │
                 │  8. prune old backups                         │
                 └──────────────────────────────────────────────┘
                            │                    │
                            ▼                    ▼
                   platform/database        migrations/sqlite/*.sql
                     (*Store, Dialect)         (go:embed)
```

Key property: **steps 4–7 only run when there is something to do.** The overwhelmingly common
launch (no pending migrations) costs one integrity check and one version read — no backup, no
delay.

---

## 3. DESIGN — the runner

### 3.1 Public surface

```go
// internal/platform/migrate
type Runner struct { /* db, migrationsFS, dialect, backupDir, clock, logger, progress */ }

type Options struct {
    FS          fs.FS            // embedded migrations for the active dialect
    BackupDir   string           // where pre-migration backups are written
    KeepBackups int              // retention (default 5)
    Progress    func(Progress)   // optional; drives the UI (§6)
    SkipBackup  bool             // tests only; never set in production
}

func New(db *database.Store, opts Options) (*Runner, error)

// Status reports where the database stands without changing anything.
func (r *Runner) Status(ctx context.Context) (Status, error)

// Up applies all pending migrations with the full safety protocol (§4).
func (r *Runner) Up(ctx context.Context) (Result, error)

type Status struct {
    CurrentVersion int64
    TargetVersion  int64
    Pending        []Migration
    Dirty          bool     // a previous run failed mid-way
}
```

There is deliberately **no `Down()` / rollback API**. Forward-only (ARCHITECTURE_v1 §10.1): a
down-migration that has never been run on customer data is a liability, not a safety net —
correcting a mistake means shipping a new forward migration. Recovery from a *failed* migration
is the backup restore (§4), which is stronger and actually tested.

### 3.2 Applying a migration

Each migration runs inside **one transaction**, on the **writer pool** (`db.WriterPool()`), with
its version row written in that same transaction. Rules enforced on migration content:

- **No `PRAGMA` statements inside migrations.** PRAGMAs are connection state (set by the DSN,
  Step 0.3) and several cannot run inside a transaction — a migration that changes them either
  fails or silently escapes the transaction. This is checked by a lint-style assertion when the
  migration set is loaded, so it fails on the developer's machine, not the customer's.
- **No `VACUUM`, no `ATTACH`.** Same reason.
- SQLite *does* support transactional DDL, which is what makes per-migration atomicity real
  here; PostgreSQL does too. (MySQL does not — noted for the future; the safety layer's
  backup/restore is what covers it there, which is precisely why we built it.)

### 3.3 Checksums — the anti-divergence guarantee

The version table records, per applied migration: `version`, `name`, `checksum` (SHA-256 of the
file bytes), `applied_at`, `duration_ms`.

On **every** boot, before applying anything, the runner recomputes the checksum of each
*already-applied* migration and compares. A mismatch is a **hard startup failure** with a clear
message naming the file.

Why this matters more than it sounds: if migration `0003` is edited after release, customer A
(who migrated before the edit) and customer B (after) both report "schema version 3" while
having *different schemas*. Every subsequent migration then behaves differently on the two
machines, and the divergence is invisible and unrecoverable. Checksums turn a silent, permanent
corruption into a loud, immediate, fixable error. **This is non-negotiable and cheap.**

Note: goose does not enforce this itself, so it is a genuine addition — implemented as our own
column + verification pass alongside goose's version table.

### 3.4 Concurrency — one migrator at a time

Two copies of the app launching simultaneously (a double-click, or a second window) must not
both migrate. Protection is layered:
1. The **single-writer pool** (Step 0.3) already serialises DDL within a process.
2. Across processes, an **advisory lock** — a dedicated row in the platform table taken with an
   `UPDATE … WHERE locked = 0` inside a transaction, with a stale-lock timeout (a crashed
   migrator must not deadlock the app forever).

The lock is released in the same transaction that records completion, and a stale lock older
than the timeout is reclaimed with a logged warning.

### 3.5 Module-owned migrations, globally ordered

Each module exposes `Migrations() fs.FS`. The bootstrap merges them into one ordered set keyed
by the global version number and **fails at startup on a duplicate version** — two modules
claiming `0007` is a developer error that must never reach a build. Files are named
`<version>_<module>_<description>.sql` (e.g. `0002_currency_initial.sql`) so the owner is
obvious from the filename.

---

## 4. DESIGN — the desktop safety layer (the heart of this step)

This is what ARCHITECTURE_v1 §10.4 requires, and the reason we wrap goose instead of calling it.

### 4.1 Pre-flight

1. **`PRAGMA integrity_check`** — if the database is already corrupt, abort *before* touching
   anything. Migrating a corrupt file, or backing it up and calling that a safety net, both make
   things worse. The user is told to restore from a backup.
2. **Free-space check** — refuse to start if free disk space < (database size × 2 + margin). A
   backup that fails halfway because the disk filled is the worst possible outcome, and it is
   trivially preventable.

### 4.2 Backup — physical, consistent, verified

- Uses SQLite's **online backup API** (`VACUUM INTO '<path>'`), not a file copy. A file copy of
  a WAL-mode database while connections are open is **not** a consistent snapshot — this is a
  classic, data-losing mistake, and `VACUUM INTO` produces a single consistent, compacted file.
- Written to `BackupDir/pre-migration-<timestamp>-v<from>-to-v<to>.db`.
- **Verified after creation**: open it, `PRAGMA integrity_check`, and confirm the version table
  reads back the expected version. An unverified backup is a rumour (the same principle proven
  useful in the PulmoSight restore drill).

### 4.3 Failure → automatic restore

If any migration fails:
1. Its transaction rolls back (so the DB *should* already be intact).
2. The runner nonetheless **restores from the verified backup** — belt and braces, because the
   failure mode we cannot rule out is the one we did not predict.
3. Both pools are closed, the file is replaced atomically (write-to-temp + rename), and the app
   reports a typed `errs` error naming the failed migration **and the backup path**.
4. The original DB is retained as `…-failed-<timestamp>.db` for diagnosis, never deleted.

The user-facing outcome: *"Update could not be applied. Your data is unchanged. A backup is at
…"* — an app that refuses to start is bad; an app that starts having eaten a year of invoices is
unrecoverable.

### 4.4 Retention

Keep the most recent `KeepBackups` (default **5**) pre-migration backups; prune older ones after
a *successful* run only. Failed-run artifacts are never auto-pruned. This bounds disk growth
without ever deleting the backup that matters.

---

## 5. DESIGN — the version gate

Before anything else, compare the database's recorded `TargetVersion` against the highest
migration the **running binary** knows:

- `db > binary` → **refuse to open**, with a clear message ("this database was created by a newer
  version of Mizan; please update"). An older build writing through a newer schema is silent
  corruption — this is the only protection against a customer running two installs.
- `db < binary` → migrate (the normal path).
- `db == binary` → nothing to do (the fast path).

---

## 6. DESIGN — progress and observability

- `Progress{Current, Total, Migration, Phase}` is emitted per step (`PhaseBackup`,
  `PhaseMigrating`, `PhaseRestoring`, …). In Step 0.10 the bootstrap forwards these to a Wails
  event so the splash screen shows real progress instead of appearing hung.
- Structured `slog` lines per migration with duration; the version table's `duration_ms` makes a
  slow migration visible after the fact.
- `Status()` is exposed so a diagnostics screen (Phase 9) can show schema state without
  attempting a migration.

---

## 7. DESIGN — `0001_platform.sql` (the platform-owned tables)

Only tables the **platform itself** owns. No business tables — those arrive with their modules.
All columns follow the §8.1 portable type contract (TEXT/BIGINT/SMALLINT+CHECK, CHAR(24) UTC
timestamps, CHAR(36) UUIDv7 keys) and the §8.3 naming rules.

| Table | Purpose | Step that uses it |
|---|---|---|
| `schema_migrations` | version, name, checksum, applied_at, duration_ms | 0.4 (this step) |
| `schema_lock` | single-row advisory migration lock (§3.4) | 0.4 |
| `settings` | scoped typed key/value (§16.2) | 0.5 |
| `feature_flags` | per-scope flag overrides (§17) | 0.5 |
| `outbox_events` | transactional outbox (§23.3) | 0.6 |
| `jobs`, `job_runs` | durable scheduler (§24.1) | 0.7 |
| `translations` | user-content translations (§9.5) | 0.8 |
| `number_series` | document numbering (§9.4) | Phase 1+ |

**Recommendation (D4): create all eight now, in one migration**, rather than one migration per
subsequent step. Reasons: they are platform infrastructure whose shape is already designed and
approved; a single `0001` keeps the initial schema legible as one artifact; and it avoids six
near-empty migrations. The cost — a table existing a step or two before its code — is zero, and
each is exercised by the step that adopts it. `number_series` is included because it is
platform-level (used by every transactional module) and trivial.

Indexes are created with the tables (e.g. `outbox_events` by `status, next_attempt_at`;
`settings` unique on `(scope, scope_id, setting_key)`).

---

## 8. DESIGN — package layout & dependencies

```
internal/platform/migrate/
├── migrate.go        # Runner, Options, Status, Result, Up(), Status()
├── safety.go         # integrity check, free space, backup, verify, restore, retention
├── checksum.go       # checksum computation + verification pass
├── lock.go           # cross-process advisory lock
├── loader.go         # merge module FSs, order, validate (dup versions, forbidden statements)
├── progress.go       # Progress type + phases
└── migrate_test.go   # incl. injected-failure restore drill

migrations/sqlite/
└── 0001_platform.sql
```

**New dependency:** `github.com/pressly/goose/v3` (library use only — no CLI, no init side
effects). It brings a small, well-maintained tree. Everything else is stdlib.

`migrate` imports `platform/database`, `kernel/errs`, `kernel/clock`. Nothing imports `migrate`
except the bootstrap.

---

## 9. TESTING PLAN (Protocol Step 4)

The tests that matter here are the failure-path ones; a runner that works on the happy path is
not the thing we are buying.

| Level | Tests |
|---|---|
| **Unit** | Version ordering & duplicate-version detection; forbidden-statement detection (PRAGMA/VACUUM in a migration); checksum computation; retention pruning arithmetic; progress emission. |
| **Integration — happy path** | Fresh DB → `Up()` → all platform tables exist with the right columns; version recorded; second `Up()` is a no-op; `Status()` accurate at each point. |
| **Integration — the drills** | **(a) Injected failure**: a deliberately invalid migration mid-set → verify the DB is byte-identical to before, the backup exists and verifies, the failed copy is retained, and the error names the migration. **(b) Checksum tamper**: alter an applied migration's bytes → next boot fails loudly. **(c) Version gate**: DB at version N+1 vs a binary knowing N → refuses to open. **(d) Corrupt DB**: truncated/garbage file → aborts at integrity check without writing anything. |
| **Concurrency** | Two runners against one file → exactly one migrates, the other waits and observes the result; a stale lock is reclaimed after the timeout. |
| **Contract** | The whole suite is written against the `Runner` + a `*Store`, so a future PostgreSQL dialect runs the identical drills. |

The **restore drill (b)** is the single most valuable test in this step — it is the one that
proves the promise "a failed update never costs you data".

---

## 10. DECISIONS REQUESTED

1. **D1 — `pressly/goose` as an embedded library**, wrapped in our own safety/checksum/gate
   layer, rather than a hand-written runner. *(Recommended; §1.5.)*
2. **D2 — forward-only; no `Down()` API.** Corrections ship as new forward migrations; recovery
   from failure is the verified backup restore. *(§3.1.)*
3. **D3 — checksum verification of applied migrations on every boot**, mismatch = hard startup
   failure. *(Recommended; §3.3.)*
4. **D4 — create all eight platform tables in `0001_platform.sql` now**, rather than one
   migration per later step. *(Recommended; §7.)*
5. **D5 — `VACUUM INTO` for the pre-migration backup** (consistent snapshot), retention 5, failed
   artifacts never auto-pruned; and this is explicitly *not* the user-facing `.mizanbak` backup
   (Phase 9). *(§4.2, §4.4.)*
6. **D6 — cross-process advisory lock** with a stale-lock timeout, in addition to the
   single-writer pool. *(§3.4.)*

On approval I'll implement in this order, each independently reviewable: `0001_platform.sql` →
loader + checksum → lock → safety (backup/verify/restore) → Runner (`Up`/`Status`) + progress →
the failure drills, tests alongside each, then the self-review and improvement notes
(Protocol Steps 5–6).

---

## 11. IMPLEMENTATION RECORD & AMENDED DECISIONS

### 11.1 D1 — AMENDED: hand-written runner, not `pressly/goose`

**Original D1 (§1.5):** use `pressly/goose/v3` as an embedded library and wrap it.
**As implemented:** a hand-written runner. `goose` is not a dependency.

This reverses an approved decision, so the reasoning is recorded rather than left implicit
(Protocol: *never silently deviate; explain and recommend*).

Why the reversal is the better call:

- **The differentiated part was never goose's.** Goose would have supplied a version table,
  `embed.FS` loading, and transaction-per-migration — roughly 120 lines here. Everything this
  step actually exists for (checksum enforcement §3.3, the version gate §5, integrity check +
  verified backup + auto-restore §4, the cross-process lock §3.4, progress §6) sits *outside*
  goose and had to be written regardless.
- **It would have meant owning two version tables.** Goose maintains `goose_db_version`; our
  checksum columns (§3.3) do not fit it. §3.3 already conceded we would keep "our own column +
  verification pass alongside goose's version table" — two sources of truth for one fact, which
  is precisely the kind of divergence this step is built to prevent.
- **Offline-first (a hard project constraint).** Goose pulls a dependency tree that must be
  vendored and kept current for a 10-year maintenance horizon, in exchange for ~120 lines.
- **Portability stays ours.** The runner drives migrations through the dialect shim (§11.2), so
  the PostgreSQL port is a dialect implementation. Goose's own dialect abstraction would have
  been a second, competing portability seam next to the one Step 0.3 established.

Cost accepted: ~120 lines of loader/apply code we now test and maintain ourselves. Given
§9's failure-path drills had to be written either way, this is a genuinely small delta.

**Reconfirmed unchanged:** D2 (forward-only, no `Down()`), D3 (checksum verification every
boot), D4 (all eight platform tables in `0001`), D5 (`VACUUM INTO` + retention 5), D6
(cross-process advisory lock with stale timeout).

### 11.2 Portability correction — the runner is dialect-driven

The first cut of the implementation reached for SQLite directly in three places: a
`sqlite_master` lookup for the lock table, `PRAGMA integrity_check`, and `VACUUM INTO`. That
contradicts the Step 0.3 rule that **the dialect shim is the entire portability surface**, and
would have made the PostgreSQL port a diff across `migrate` rather than a new dialect.

Three methods were therefore added to `dialect.Dialect`:

| Method | SQLite | Meaning of the zero value |
|---|---|---|
| `TableExistsQuery() string` | `sqlite_master` lookup, one `?` | — |
| `IntegrityCheckStatement() string` | `PRAGMA integrity_check` | `""` → engine has no check; pre-flight skips it |
| `OnlineBackupStatement(dest) string` | `VACUUM INTO '<dest>'` | `""` → no in-engine backup; the safety layer refuses to migrate without an external one |

The zero values matter: they are the honest answer for PostgreSQL/MySQL, and they make the
safety layer's behaviour on those engines explicit rather than accidental.

### 11.3 Design requirements that the first cut had missed

- **Free-space pre-flight (§4.1.2)** was specified but unimplemented (`CodeInsufficientDisk`
  was declared and never used). Now implemented in `diskspace_unix.go` /
  `diskspace_windows.go` (stdlib only, no cgo), injectable for tests.
- **`go:embed` wiring (§8)** — nothing supplied the runner's `fs.FS`. Added as the root
  `migrations` package exposing `SQLite()`.
- **Tests (§9)** — the whole plan, including all four drills, was absent. Now implemented.

### 11.4 Defects found and fixed during self-review (Protocol Step 5)

1. **Lock released against closed pools.** `Up` held the advisory lock via `defer
   releaseLock`, but the failure path calls `restore`, which closes the pools — so the
   deferred release always ran against a closed database. The lock is now released *before*
   restore, while the pools are open, and release is idempotent.
2. **Fragile file-URI construction.** `verifyBackup` built its DSN with
   `strings.ReplaceAll(path, " ", "%20")`, which breaks on `?`, `#`, and `%` — all legal in a
   macOS/Windows path. Replaced with proper `net/url` encoding.
3. **Forbidden-statement scan was line-anchored.** `validateContent` only matched a forbidden
   keyword at the start of a line, so `CREATE TABLE t(...); PRAGMA foo;` passed. It now strips
   `--` and `/* */` comments and checks the leading keyword of every `;`-delimited statement.
4. **`restore` leaves the `Store` closed.** Not a bug but an undocumented postcondition with
   real consequences for the caller. Now stated on `Up` and carried on the returned error: after
   a failed migration the process must reopen the database (or exit), never keep using the Store.

---

*End of Step 0.4. Design approved, implemented, self-reviewed.*
