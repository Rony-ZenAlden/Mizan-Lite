// Package migrate applies schema migrations with a desktop-grade safety protocol.
//
// Mizan is a desktop application: the "operator" is a shop owner who double-clicked an
// icon, nobody is watching when a migration fails, and whatever safety net exists here
// is the only one there is. The runner therefore assumes it WILL fail on someone's
// machine and guarantees that when it does, the business's data is still intact:
//
//	integrity check → version gate → backup → verify → migrate → restore on failure
//
// Migrations are forward-only, numbered, immutable, and checksum-verified on every boot
// (see ARCHITECTURE_v1 §10 and docs/architecture/STEP_0_4_MIGRATIONS.md).
package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// Options configures a Runner.
type Options struct {
	// FS holds the migrations for the active dialect (embedded).
	FS fs.FS
	// DBPath is the live database file (needed to restore over it).
	DBPath string
	// BackupDir is where pre-migration snapshots are written.
	BackupDir string
	// KeepBackups is how many pre-migration backups to retain. Default 5.
	KeepBackups int
	// Progress, if set, receives run progress for the UI.
	Progress func(Progress)
	// Clock is injected for deterministic tests. Defaults to the system clock.
	Clock clock.Clock
	// Logger, if nil, uses slog.Default().
	Logger *slog.Logger
	// SkipBackup disables the safety snapshot. TESTS ONLY — never set in production.
	SkipBackup bool
}

// Runner applies migrations.
type Runner struct {
	db          *database.Store
	migrations  []Migration
	dbPath      string
	backupDir   string
	keepBackups int
	progress    func(Progress)
	clock       clock.Clock
	log         *slog.Logger
	skipBackup  bool
	// freeSpace reports free bytes on the volume holding dir. A field rather than a
	// direct call so the pre-flight gate is testable without filling a real disk.
	freeSpace func(dir string) (uint64, error)
}

// Status reports where the database stands, without changing anything.
type Status struct {
	CurrentVersion int64
	TargetVersion  int64
	Pending        []Migration
}

// Result summarises a completed run.
type Result struct {
	FromVersion int64
	ToVersion   int64
	Applied     int
	BackupPath  string // empty when nothing needed applying
	Duration    time.Duration
}

// New validates and loads the migration set.
func New(db *database.Store, opts Options) (*Runner, error) {
	if db == nil {
		return nil, errs.Internal(CodeLoadFailed, "migrate: db is required")
	}
	if opts.FS == nil {
		return nil, errs.Internal(CodeLoadFailed, "migrate: Options.FS is required")
	}
	if opts.DBPath == "" {
		return nil, errs.Internal(CodeLoadFailed, "migrate: Options.DBPath is required")
	}
	ms, err := Load(opts.FS)
	if err != nil {
		return nil, err
	}
	if opts.KeepBackups == 0 {
		opts.KeepBackups = 5
	}
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Runner{
		db:          db,
		migrations:  ms,
		dbPath:      opts.DBPath,
		backupDir:   opts.BackupDir,
		keepBackups: opts.KeepBackups,
		progress:    opts.Progress,
		clock:       opts.Clock,
		log:         opts.Logger,
		skipBackup:  opts.SkipBackup,
		freeSpace:   availableBytes,
	}, nil
}

// Status returns the current and target versions and any pending migrations.
func (r *Runner) Status(ctx context.Context) (Status, error) {
	applied, err := r.appliedRecords(ctx)
	if err != nil {
		return Status{}, err
	}
	st := Status{TargetVersion: r.targetVersion()}
	for _, m := range r.migrations {
		if _, ok := applied[m.Version]; ok {
			if m.Version > st.CurrentVersion {
				st.CurrentVersion = m.Version
			}
			continue
		}
		st.Pending = append(st.Pending, m)
	}
	// A database migrated by a NEWER binary can hold versions this build does not know.
	for v := range applied {
		if v > st.CurrentVersion {
			st.CurrentVersion = v
		}
	}
	return st, nil
}

// Up applies every pending migration under the full safety protocol.
//
// IMPORTANT postcondition on failure: when a migration fails and the database is
// restored from the safety snapshot, the *database.Store is CLOSED before Up returns
// (restore must replace the file, which requires no open handles). The caller must then
// reopen the database or terminate — it must not keep using the Store. On success, and
// on every error raised before the apply loop, the Store is left open and usable.
func (r *Runner) Up(ctx context.Context) (Result, error) {
	start := r.clock.Now()
	r.emit(Progress{Phase: PhaseChecking})

	// 1. Never migrate an already-corrupt database.
	if err := integrityCheck(ctx, r.db.WriterPool(), r.db.Dialect().IntegrityCheckStatement()); err != nil {
		return Result{}, err
	}

	// 2. Where do we stand?
	st, err := r.Status(ctx)
	if err != nil {
		return Result{}, err
	}

	// 3. Version gate: an older binary must never write through a newer schema.
	if st.CurrentVersion > st.TargetVersion {
		return Result{}, errs.Conflict(CodeDatabaseTooNew, fmt.Sprintf(
			"this database is at schema version %d but this version of Mizan only understands %d; "+
				"please update the application", st.CurrentVersion, st.TargetVersion))
	}

	// 4. History must be intact (checksums) — a silent divergence is unrecoverable.
	if err = r.verifyChecksums(ctx); err != nil {
		return Result{}, err
	}

	// 5. Fast path: nothing to do. No backup, no delay — the common launch.
	if len(st.Pending) == 0 {
		r.emit(Progress{Phase: PhaseDone})
		return Result{FromVersion: st.CurrentVersion, ToVersion: st.CurrentVersion}, nil
	}

	// 6. Serialise across processes (skipped on a virgin database — no tables yet).
	var hasLock bool
	if hasLock, err = r.lockTableExists(ctx); err != nil {
		return Result{}, err
	}
	lockHeld := false
	if hasLock {
		if err = r.acquireLock(ctx); err != nil {
			return Result{}, err
		}
		lockHeld = true
		// Guarded rather than a bare defer: the failure path releases the lock early,
		// because restore() closes the pools and a later release would run against a
		// closed database and silently leave the lock set.
		defer func() {
			if lockHeld {
				r.releaseLock(ctx)
			}
		}()
	}

	// 7. Disk pre-flight. Checked here, not on every launch: it only matters once we
	// know we are going to write, and the no-op fast path above must stay cheap.
	if !r.skipBackup {
		if err = r.checkFreeSpace(); err != nil {
			return Result{}, err
		}
	}

	// 8. Safety snapshot, verified before it is trusted.
	var backupPath string
	if !r.skipBackup && r.backupDir != "" {
		r.emit(Progress{Phase: PhaseBackup, Total: len(st.Pending)})
		backupPath, err = r.backup(ctx, st.CurrentVersion, st.TargetVersion)
		if err != nil {
			return Result{}, err
		}
		r.log.InfoContext(ctx, "pre-migration backup created", slog.String("path", backupPath))
	}

	// 9. Apply.
	for i, m := range st.Pending {
		r.emit(Progress{Phase: PhaseMigrating, Current: i + 1, Total: len(st.Pending), Name: m.FileName})
		if applyErr := r.apply(ctx, m); applyErr != nil {
			// Release while the pools are still open — handleFailure closes them.
			if lockHeld {
				r.releaseLock(ctx)
				lockHeld = false
			}
			return r.handleFailure(ctx, m, backupPath, applyErr)
		}
	}

	r.pruneBackups(r.keepBackups)
	r.emit(Progress{Phase: PhaseDone, Current: len(st.Pending), Total: len(st.Pending)})

	return Result{
		FromVersion: st.CurrentVersion,
		ToVersion:   st.TargetVersion,
		Applied:     len(st.Pending),
		BackupPath:  backupPath,
		Duration:    r.clock.Now().Sub(start),
	}, nil
}

// apply runs one migration and records it — both in a single transaction, so a
// migration and its version row commit together or neither does.
func (r *Runner) apply(ctx context.Context, m Migration) error {
	began := r.clock.Now()

	tx, err := r.db.WriterPool().BeginTx(ctx, nil)
	if err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeMigrationFailed, "beginning migration "+m.FileName)
	}
	defer func() { _ = tx.Rollback() }() // no-op after a successful commit

	// The whole file executes as one statement batch; no fragile SQL splitting.
	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeMigrationFailed,
			"applying migration "+m.FileName)
	}

	elapsed := r.clock.Now().Sub(began)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name, checksum, applied_at, duration_ms)
		 VALUES (?, ?, ?, ?, ?)`,
		m.Version, m.Name, m.Checksum, formatTimestamp(r.clock.Now()), elapsed.Milliseconds(),
	); err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeMigrationFailed,
			"recording migration "+m.FileName)
	}

	if err := tx.Commit(); err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeMigrationFailed,
			"committing migration "+m.FileName)
	}
	r.log.InfoContext(ctx, "migration applied",
		slog.Int64("version", m.Version),
		slog.String("name", m.Name),
		slog.Int64("duration_ms", elapsed.Milliseconds()))
	return nil
}

// handleFailure restores the database from the verified backup and reports an error
// that names both the failed migration and the backup path.
func (r *Runner) handleFailure(ctx context.Context, m Migration, backupPath string, cause error) (Result, error) {
	r.log.ErrorContext(ctx, "migration failed",
		slog.Int64("version", m.Version),
		slog.String("name", m.FileName),
		slog.Any("error", cause))

	if backupPath == "" {
		// No snapshot was taken (tests, or no backup dir): the migration's own
		// transaction already rolled back, so the database is unchanged.
		return Result{}, cause
	}

	r.emit(Progress{Phase: PhaseRestoring, Name: m.FileName})
	// restore is deliberately context-free: it closes the pools and swaps files on
	// disk. Those steps must complete even if ctx is already cancelled — abandoning a
	// restore half-way is precisely the data loss this layer exists to prevent.
	//nolint:contextcheck // see above: cancellation must not interrupt a restore
	failedCopy, restoreErr := r.restore(backupPath)
	if restoreErr != nil {
		return Result{}, errs.Wrap(restoreErr, errs.CategoryInternal, CodeRestoreFailed, fmt.Sprintf(
			"migration %s failed AND the automatic restore failed; your data is in the backup at %s",
			m.FileName, backupPath))
	}

	msg := fmt.Sprintf(
		"migration %s failed; your data was restored and is unchanged (backup: %s",
		m.FileName, backupPath)
	if failedCopy != "" {
		msg += "; the failed database was kept at " + failedCopy
	}
	msg += ")"
	return Result{}, errs.Wrap(cause, errs.CategoryConflict, CodeMigrationFailed, msg).
		WithParam("migration", m.FileName).
		WithParam("backup", backupPath)
}

// verifyChecksums re-hashes every already-applied migration this build still ships and
// refuses to continue on a mismatch.
//
// If a shipped migration is edited later, a customer who migrated before the edit and
// one who migrated after both report the same version while having DIFFERENT schemas.
// Every later migration then behaves differently on the two machines, invisibly and
// unrecoverably. This check turns that into a loud, immediate, fixable error.
func (r *Runner) verifyChecksums(ctx context.Context) error {
	applied, err := r.appliedRecords(ctx)
	if err != nil {
		return err
	}
	for _, m := range r.migrations {
		rec, ok := applied[m.Version]
		if !ok {
			continue
		}
		if rec.checksum != m.Checksum {
			return errs.Conflict(CodeChecksumMismatch, fmt.Sprintf(
				"migration %s (version %d) has changed since it was applied; "+
					"applied migrations are immutable — ship a new migration instead",
				m.FileName, m.Version))
		}
	}
	return nil
}

type appliedRecord struct {
	checksum string
	name     string
}

// appliedRecords reads the history table, tolerating its absence on a fresh database.
func (r *Runner) appliedRecords(ctx context.Context) (map[int64]appliedRecord, error) {
	out := make(map[int64]appliedRecord)

	rows, err := r.db.WriterPool().QueryContext(ctx,
		`SELECT version, name, checksum FROM schema_migrations`)
	if err != nil {
		if isMissingTable(err) {
			return out, nil // virgin database: nothing applied yet
		}
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeMissingHistory, "reading migration history")
	}
	defer rows.Close()

	for rows.Next() {
		var v int64
		var name, sum string
		if err := rows.Scan(&v, &name, &sum); err != nil {
			return nil, errs.Wrap(err, errs.CategoryInternal, CodeMissingHistory, "scanning migration history")
		}
		out[v] = appliedRecord{checksum: sum, name: name}
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeMissingHistory, "reading migration history")
	}
	return out, nil
}

func (r *Runner) targetVersion() int64 {
	var highest int64
	for _, m := range r.migrations {
		if m.Version > highest {
			highest = m.Version
		}
	}
	return highest
}

// isMissingTable reports whether err is the driver's "no such table" — the expected
// condition on a virgin database that has never been migrated.
func isMissingTable(err error) bool {
	if err == nil || errors.Is(err, sql.ErrNoRows) {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "no such table")
}
