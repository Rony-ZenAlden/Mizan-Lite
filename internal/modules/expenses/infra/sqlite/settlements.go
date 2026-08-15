package sqlite

import (
	"context"
	"database/sql"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/expenses/domain"
)

const paymentColumns = `
	id, company_id, branch_id, partner_id, payee_name, document_number, payment_date,
	method, reference, currency_code, amount_minor, status`

// InsertPayment writes a settlement.
func (r *Repos) InsertPayment(ctx context.Context, p domain.Payment, actorID id.ID) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO expense_payments (
			id, company_id, branch_id, partner_id, payee_name, document_number, payment_date,
			method, reference, currency_code, amount_minor, status,
			row_version, created_at, created_by, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		string(p.ID), string(p.CompanyID), string(p.BranchID), nullableID(p.PartnerID),
		p.PayeeName, nullable(p.Number), p.PaymentDate, string(p.Method),
		nullable(p.Reference), p.CurrencyCode, p.AmountMinor, string(p.Status),
		now, nullableID(actorID), now)
	if err != nil {
		return r.wrap(err, "inserting an expense settlement")
	}
	return nil
}

// UpdatePayment writes a settlement's mutable fields.
func (r *Repos) UpdatePayment(ctx context.Context, p domain.Payment, actorID id.ID) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE expense_payments SET
			status = ?, document_number = ?,
			posted_at = CASE WHEN ? = 'posted' AND posted_at IS NULL THEN ? ELSE posted_at END,
			posted_by = CASE WHEN ? = 'posted' AND posted_by IS NULL THEN ? ELSE posted_by END,
			row_version = row_version + 1, updated_at = ?
		WHERE id = ?`,
		string(p.Status), nullable(p.Number),
		string(p.Status), r.now(), string(p.Status), nullableID(actorID),
		r.now(), string(p.ID))
	if err != nil {
		return r.wrap(err, "updating an expense settlement")
	}
	return nil
}

// Payments lists a company's expense settlements.
func (r *Repos) Payments(ctx context.Context, companyID id.ID) ([]domain.Payment, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT `+paymentColumns+` FROM expense_payments
		  WHERE company_id = ? ORDER BY payment_date DESC, created_at DESC`,
		string(companyID))
	if err != nil {
		return nil, r.wrap(err, "listing expense settlements")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.Payment, 0, 8)
	for rows.Next() {
		var (
			payment   domain.Payment
			partnerID any
			number    any
			reference any
		)
		if err = rows.Scan(
			&payment.ID, &payment.CompanyID, &payment.BranchID, &partnerID,
			&payment.PayeeName, &number, &payment.PaymentDate, &payment.Method,
			&reference, &payment.CurrencyCode, &payment.AmountMinor, &payment.Status,
		); err != nil {
			return nil, r.wrap(err, "listing expense settlements")
		}
		payment.PartnerID = id.ID(text(partnerID))
		payment.Number = text(number)
		payment.Reference = text(reference)
		out = append(out, payment)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing expense settlements")
	}
	return out, nil
}

// InsertAllocation records a settlement clearing part of an expense.
func (r *Repos) InsertAllocation(
	ctx context.Context, paymentID, expenseID id.ID, amountMinor int64,
) error {
	identifier, err := id.New()
	if err != nil {
		return err
	}
	if _, err = r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO expense_payment_allocations (
			id, payment_id, expense_id, amount_minor, created_at
		) VALUES (?, ?, ?, ?, ?)`,
		string(identifier), string(paymentID), string(expenseID),
		amountMinor, r.now()); err != nil {
		return r.wrap(err, "allocating an expense settlement")
	}
	return nil
}

// SettledMinor sums what POSTED settlements have put against an expense.
//
// Only posted ones count. A draft settlement is somebody typing, and counting it would show an
// expense cleared by money that has not left.
func (r *Repos) SettledMinor(ctx context.Context, expenseID id.ID) (int64, error) {
	var total sql.NullInt64
	err := r.db.Reader(ctx).QueryRowContext(ctx, `
		SELECT SUM(a.amount_minor)
		  FROM expense_payment_allocations a
		  JOIN expense_payments p ON p.id = a.payment_id
		 WHERE a.expense_id = ? AND p.status = 'posted'`,
		string(expenseID)).Scan(&total)
	if err != nil {
		return 0, r.wrap(err, "summing what has been settled")
	}
	return total.Int64, nil
}

// UnsettledExpenses lists what is still owed, oldest due first.
//
// The report somebody opens to decide what to pay this week. Ordered by due date because that is
// the question — not by when it was entered.
func (r *Repos) UnsettledExpenses(
	ctx context.Context, companyID id.ID,
) ([]domain.Expense, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT `+expenseColumns+`
		  FROM expenses e
		 WHERE e.company_id = ? AND e.status = 'posted' AND e.settlement = 'on_account'
		   AND e.total_minor > COALESCE((
		         SELECT SUM(a.amount_minor)
		           FROM expense_payment_allocations a
		           JOIN expense_payments p ON p.id = a.payment_id
		          WHERE a.expense_id = e.id AND p.status = 'posted'), 0)
		 ORDER BY COALESCE(e.due_date, e.expense_date), e.created_at`,
		string(companyID))
	if err != nil {
		return nil, r.wrap(err, "listing unsettled expenses")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.Expense, 0, 8)
	for rows.Next() {
		expense, scanErr := scanExpense(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "listing unsettled expenses")
		}
		out = append(out, expense)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing unsettled expenses")
	}
	return out, nil
}
