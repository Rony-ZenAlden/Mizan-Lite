package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/sales/domain"
)

// InsertShift opens a till.
func (r *Repos) InsertShift(
	ctx context.Context, companyID id.ID, s domain.Shift,
) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO pos_shifts (
			id, company_id, branch_id, terminal, opened_at, opened_by,
			opening_float_minor, status, notes, row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		string(s.ID), string(companyID), string(s.BranchID), s.Terminal,
		s.OpenedAt, nullableID(s.OpenedBy), s.OpeningFloatMinor,
		string(s.Status), nullable(s.Notes), now, now)
	if err != nil {
		return r.wrap(err, "opening a shift")
	}
	return nil
}

const shiftColumns = `
	id, branch_id, terminal, opened_at, opened_by, opening_float_minor,
	closed_at, closed_by, expected_minor, counted_minor, difference_minor, status, notes`

// ShiftByID reads one shift.
func (r *Repos) ShiftByID(ctx context.Context, shiftID id.ID) (domain.Shift, bool, error) {
	row := r.db.Writer(ctx).QueryRowContext(ctx,
		`SELECT `+shiftColumns+` FROM pos_shifts WHERE id = ?`, string(shiftID))

	s, err := scanShift(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Shift{}, false, nil
	}
	if err != nil {
		return domain.Shift{}, false, r.wrap(err, "reading a shift")
	}
	return s, true, nil
}

// OpenShift finds the shift a till is currently trading under.
func (r *Repos) OpenShift(
	ctx context.Context, branchID id.ID, terminal string,
) (domain.Shift, bool, error) {
	row := r.db.Writer(ctx).QueryRowContext(ctx,
		`SELECT `+shiftColumns+` FROM pos_shifts
		  WHERE branch_id = ? AND terminal = ? AND status = 'open'`,
		string(branchID), terminal)

	s, err := scanShift(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Shift{}, false, nil
	}
	if err != nil {
		return domain.Shift{}, false, r.wrap(err, "reading the open shift")
	}
	return s, true, nil
}

func scanShift(sc scanner) (domain.Shift, error) {
	var (
		s                            domain.Shift
		openedBy, closedAt, closedBy any
		expected, counted, diff      sql.NullInt64
		notes                        any
		status                       string
	)
	if err := sc.Scan(&s.ID, &s.BranchID, &s.Terminal, &s.OpenedAt, &openedBy,
		&s.OpeningFloatMinor, &closedAt, &closedBy,
		&expected, &counted, &diff, &status, &notes); err != nil {
		return domain.Shift{}, err
	}
	s.OpenedBy = id.ID(text(openedBy))
	s.ClosedAt = text(closedAt)
	s.ClosedBy = id.ID(text(closedBy))
	s.ExpectedMinor = expected.Int64
	s.CountedMinor = counted.Int64
	s.DifferenceMinor = diff.Int64
	s.Status = domain.ShiftStatus(status)
	s.Notes = text(notes)
	return s, nil
}

// CloseShift writes the reconciliation, guarded on the shift still being open.
func (r *Repos) CloseShift(
	ctx context.Context, s domain.Shift, closedBy id.ID,
) error {
	result, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE pos_shifts SET status = 'closed', closed_at = ?, closed_by = ?,
			expected_minor = ?, counted_minor = ?, difference_minor = ?, notes = ?,
			row_version = row_version + 1, updated_at = ?
		 WHERE id = ? AND status = 'open'`,
		s.ClosedAt, nullableID(closedBy),
		s.ExpectedMinor, s.CountedMinor, s.DifferenceMinor, nullable(s.Notes),
		r.now(), string(s.ID))
	if err != nil {
		return r.wrap(err, "closing a shift")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return r.wrap(err, "closing a shift")
	}
	if affected == 0 {
		return errs.Conflict(domain.CodeShiftClosed,
			"that shift is already closed").WithParam("id", string(s.ID))
	}
	return nil
}

// CashTakenIn reports the cash a shift took.
//
// CASH only, and POSTED only. A card payment does not put money in the drawer, and a draft
// payment has not been received — counting either would make a balanced till look short by
// exactly that amount.
func (r *Repos) CashTakenIn(ctx context.Context, shiftID id.ID) (int64, error) {
	var taken sql.NullInt64
	err := r.db.Writer(ctx).QueryRowContext(ctx, `
		SELECT SUM(amount_minor) FROM sales_payments
		 WHERE shift_id = ? AND method = 'cash' AND status = 'posted'`,
		string(shiftID)).Scan(&taken)
	if err != nil {
		return 0, r.wrap(err, "reading a shift's cash")
	}
	return taken.Int64, nil
}

// AttachPaymentToShift records which till took a payment.
func (r *Repos) AttachPaymentToShift(
	ctx context.Context, paymentID, shiftID id.ID,
) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx,
		`UPDATE sales_payments SET shift_id = ? WHERE id = ?`,
		string(shiftID), string(paymentID))
	if err != nil {
		return r.wrap(err, "attaching a payment to a shift")
	}
	return nil
}
