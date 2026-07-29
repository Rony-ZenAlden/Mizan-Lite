package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// staleLockAfter is how long a held lock may persist before it is treated as abandoned.
// A migrator that crashed (or a machine that lost power mid-run) must not lock the
// application out forever.
const staleLockAfter = 15 * time.Minute

// acquireLock takes the single-row cross-process advisory lock.
//
// The single-writer pool (Step 0.3) already serialises DDL within one process; this
// covers the case the pool cannot see — two app instances launched at once, each
// wanting to migrate the same file.
func (r *Runner) acquireLock(ctx context.Context) error {
	owner := fmt.Sprintf("pid=%d host=%s", os.Getpid(), hostname())
	now := r.clock.Now().UTC()
	nowStr := formatTimestamp(now)
	staleBefore := formatTimestamp(now.Add(-staleLockAfter))

	// One statement, evaluated inside the writer's serialised access: claim the lock if
	// it is free OR if the existing hold is stale. Timestamps are ISO-8601 UTC, so a
	// lexical comparison is a chronological one.
	res, err := r.db.WriterPool().ExecContext(ctx, `
		UPDATE schema_lock
		   SET is_locked = 1, locked_at = ?, locked_by = ?
		 WHERE lock_id = 1
		   AND (is_locked = 0 OR locked_at IS NULL OR locked_at < ?)`,
		nowStr, owner, staleBefore)
	if err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeLockUnavailable, "acquiring migration lock")
	}
	n, err := res.RowsAffected()
	if err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeLockUnavailable, "acquiring migration lock")
	}
	if n == 0 {
		return errs.Conflict(CodeLockUnavailable,
			"another Mizan instance is currently migrating this database")
	}
	return nil
}

// releaseLock frees the advisory lock. Best-effort: a stale lock is reclaimable, so a
// failed release degrades to a delay, never a permanent lockout.
func (r *Runner) releaseLock(ctx context.Context) {
	_, _ = r.db.WriterPool().ExecContext(ctx,
		`UPDATE schema_lock SET is_locked = 0, locked_at = NULL, locked_by = NULL WHERE lock_id = 1`)
}

// lockTableExists reports whether the platform bookkeeping tables are present. On a
// brand-new database they are not, so the first migration runs without a lock — there
// is no history to protect and nothing for a second instance to corrupt beyond the
// single-writer serialisation already in force.
func (r *Runner) lockTableExists(ctx context.Context) (bool, error) {
	var name string
	err := r.db.WriterPool().QueryRowContext(ctx,
		r.db.Dialect().TableExistsQuery(), "schema_lock").Scan(&name)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, errs.Wrap(err, errs.CategoryInternal, CodeLoadFailed, "checking for schema_lock")
	default:
		return true, nil
	}
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

// formatTimestamp renders the portable CHAR(24) ISO-8601 UTC form (§7.5).
func formatTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}
