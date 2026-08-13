package numbering

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// Stable codes for the allocator.
const (
	CodeStorage         = "numbering.storage"
	CodeDuplicateSeries = "numbering.duplicate_series"
	CodeUnknownSeries   = "numbering.unknown_series"
)

// Allocator issues document numbers from shared series.
type Allocator struct {
	db  database.DB
	clk clock.Clock
}

// New builds an allocator.
func New(db database.DB, clk clock.Clock) *Allocator {
	if clk == nil {
		clk = clock.System()
	}
	return &Allocator{db: db, clk: clk}
}

func (r *Allocator) now() string { return clock.Format(r.clk.Now()) }

func (r *Allocator) wrap(err error, what string) error {
	return errs.Wrap(r.db.Dialect().TranslateError(err), errs.CategoryInternal, CodeStorage, what)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Create adds a series.
func (r *Allocator) Create(ctx context.Context, s Series) error {
	existing, found, err := r.SeriesFor(ctx, s.BranchID, s.Code)
	if err != nil {
		return err
	}
	if found && existing.BranchID == s.BranchID {
		return errs.Conflict(CodeDuplicateSeries,
			"a number series with that code already exists").WithParam("code", s.Code)
	}
	return r.InsertSeries(ctx, s)
}

// Allocate takes the next number from a series, INSIDE the caller's transaction.
//
// # Why it must be the caller's transaction
//
// §9.4's guarantee is that a number is unique and sequential, not that it is gapless. Taking the
// number in the same transaction that writes the document is what makes a failed document give
// its number back — the two either both happen or neither does.
//
// A separate transaction here would hand out a number, commit it, and then watch the document
// fail: a permanent hole, and one an auditor will ask about.
func (r *Allocator) Allocate(
	ctx context.Context, branchID id.ID, code string,
) (string, error) {
	series, found, err := r.SeriesFor(ctx, branchID, code)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errs.NotFound(CodeUnknownSeries,
			"there is no number series for that kind of document").WithParam("code", code)
	}

	number, advanced, err := series.Next()
	if err != nil {
		return "", err
	}
	if err = r.AdvanceSeries(ctx, advanced.ID, advanced.NextValue); err != nil {
		return "", err
	}
	return number, nil
}

// Preview reports what the next number WOULD be, without consuming it.
//
// For a screen that wants to show "this will be INV-000124". It reads and does not advance, so
// two callers previewing at once both see the same answer — which is correct, because neither has
// taken anything.
func (r *Allocator) Preview(
	ctx context.Context, branchID id.ID, code string,
) (string, error) {
	series, found, err := r.SeriesFor(ctx, branchID, code)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errs.NotFound(CodeUnknownSeries,
			"there is no number series for that kind of document").WithParam("code", code)
	}
	return series.Format(series.NextValue), nil
}

// InsertSeries writes a number series.
//
// The table is the PLATFORM's (0001), shared with every transactional module. Its branch and
// fiscal-year columns are NOT NULL with an empty-string default rather than nullable, which is
// the better shape: "" means "all of them" without a COALESCE in every index and query.
func (r *Allocator) InsertSeries(ctx context.Context, s Series) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO number_series (
			id, code, branch_id, fiscal_year_id, prefix, suffix, padding,
			next_value, is_gapless, created_at, updated_at, row_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		string(s.ID), s.Code, string(s.BranchID), string(s.FiscalYearID),
		s.Prefix, s.Suffix, s.Padding, s.NextValue, boolToInt(s.IsGapless), now, now)
	if err != nil {
		return r.wrap(err, "creating a number series")
	}
	return nil
}

// SeriesFor finds the series that should number a document.
//
// # Resolution: the most specific series wins
//
// A branch's own series beats the company-wide one, exactly as a branch's account mapping beats
// the company's (Phase 2) and a partner's price list beats the default (Phase 3). One resolution
// shape, reused, rather than a third rule for a reader to learn.
//
// # It must be called inside a TRANSACTION, and that is the whole requirement
//
// The first version of this comment claimed the read had to go through `Writer` specifically,
// because "reading from the reader would let two terminals see the same next_value". A mutation
// drill swapping `Writer` for `Reader` changed nothing — and the reason is worth recording:
// `Store.Reader(ctx)` and `Store.Writer(ctx)` BOTH resolve to the live transaction when one is
// present (0.3). Inside `db.Do` they are the same executor, and the danger the comment described
// does not exist.
//
// What is actually required is that a transaction be present at all — which `allocateNumber`
// being unexported and taking a transaction context already enforces structurally. `Writer` is
// kept because it says "this is part of a write" to whoever reads it next, not because swapping
// it would break anything.
func (r *Allocator) SeriesFor(
	ctx context.Context, branchID id.ID, code string,
) (Series, bool, error) {
	row := r.db.Writer(ctx).QueryRowContext(ctx, `
		SELECT id, code, branch_id, fiscal_year_id, prefix, suffix, padding,
		       next_value, is_gapless
		  FROM number_series
		 WHERE code = ? AND (branch_id = '' OR branch_id = ?)
		 ORDER BY CASE WHEN branch_id = '' THEN 1 ELSE 0 END
		 LIMIT 1`,
		code, string(branchID))

	var (
		s       Series
		gapless int
	)
	err := row.Scan(&s.ID, &s.Code, &s.BranchID, &s.FiscalYearID, &s.Prefix, &s.Suffix,
		&s.Padding, &s.NextValue, &gapless)
	if errors.Is(err, sql.ErrNoRows) {
		return Series{}, false, nil
	}
	if err != nil {
		return Series{}, false, r.wrap(err, "reading a number series")
	}
	s.IsGapless = gapless == 1
	return s, true, nil
}

// AdvanceSeries writes the counter back.
//
// # The `next_value < ?` guard is portability insurance, not a live check
//
// It refuses to move the counter backwards or sideways, so a concurrent advance loses loudly
// rather than silently overwriting. On SQLite it cannot fire: writers are serialised and the
// allocation happens inside the transaction, so no second allocation can interleave — a mutation
// drill removing the condition broke no test, and none was added, because a test that passes
// either way claims something is pinned when nothing is (4.3's rule).
//
// It is kept for the engine §8 says this schema must survive moving to, where `db.Do` maps to a
// transaction whose isolation level does NOT serialise two allocators. There it is what stops
// two terminals both believing they took number 123 — and the cost of carrying it until then is
// one comparison.
func (r *Allocator) AdvanceSeries(
	ctx context.Context, seriesID id.ID, nextValue int64,
) error {
	result, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE number_series SET next_value = ?, row_version = row_version + 1, updated_at = ?
		 WHERE id = ? AND next_value < ?`,
		nextValue, r.now(), string(seriesID), nextValue)
	if err != nil {
		return r.wrap(err, "advancing a number series")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return r.wrap(err, "advancing a number series")
	}
	if affected == 0 {
		// The counter did not move, which means somebody else already took this number. Failing
		// LOUDLY is the point: the alternative is two documents with one number, discovered by
		// a customer.
		return errs.Conflict(CodeNoSeries,
			"that number has already been issued").WithParam("next_value", itoa(nextValue))
	}
	return nil
}
