package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/purchasing/domain"
)

const paymentColumns = `
	id, company_id, branch_id, partner_id, partner_name, document_number, payment_date,
	method, reference, currency_code, exchange_rate_micro, amount_minor, status, notes`

// InsertPayment writes a draft payment.
func (r *Repos) InsertPayment(ctx context.Context, p domain.Payment, actorID id.ID) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO supplier_payments (
			id, company_id, branch_id, partner_id, partner_name, document_number, payment_date,
			method, reference, currency_code, exchange_rate_micro, amount_minor, status, notes,
			row_version, created_at, created_by, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		string(p.ID), string(p.CompanyID), string(p.BranchID), string(p.PartnerID),
		p.PartnerName, nullable(p.Number), p.PaymentDate, string(p.Method),
		nullable(p.Reference), p.CurrencyCode, p.ExchangeRateMicro, p.AmountMinor,
		string(p.Status), nullable(p.Notes), now, nullableID(actorID), now)
	if err != nil {
		return r.wrap(err, "inserting a supplier payment")
	}
	return nil
}

// PaymentByID reads one payment.
func (r *Repos) PaymentByID(
	ctx context.Context, paymentID id.ID,
) (domain.Payment, bool, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+paymentColumns+` FROM supplier_payments WHERE id = ?`, string(paymentID))

	payment, err := scanPayment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Payment{}, false, nil
	}
	if err != nil {
		return domain.Payment{}, false, r.wrap(err, "reading a supplier payment")
	}
	return payment, true, nil
}

// Payments lists a company's supplier payments.
func (r *Repos) Payments(
	ctx context.Context, companyID id.ID, status domain.PaymentStatus,
) ([]domain.Payment, error) {
	query := `SELECT ` + paymentColumns + ` FROM supplier_payments WHERE company_id = ?`
	args := []any{string(companyID)}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, string(status))
	}
	query += ` ORDER BY payment_date DESC, created_at DESC`

	rows, err := r.db.Reader(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, r.wrap(err, "listing supplier payments")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.Payment, 0, 8)
	for rows.Next() {
		payment, scanErr := scanPayment(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "listing supplier payments")
		}
		out = append(out, payment)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing supplier payments")
	}
	return out, nil
}

// UpdatePayment writes a payment's mutable fields.
func (r *Repos) UpdatePayment(ctx context.Context, p domain.Payment, actorID id.ID) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE supplier_payments SET
			status = ?, document_number = ?, reference = ?, notes = ?,
			posted_at = CASE WHEN ? = 'posted' AND posted_at IS NULL THEN ? ELSE posted_at END,
			posted_by = CASE WHEN ? = 'posted' AND posted_by IS NULL THEN ? ELSE posted_by END,
			row_version = row_version + 1, updated_at = ?
		WHERE id = ?`,
		string(p.Status), nullable(p.Number), nullable(p.Reference), nullable(p.Notes),
		string(p.Status), r.now(), string(p.Status), nullableID(actorID),
		r.now(), string(p.ID))
	if err != nil {
		return r.wrap(err, "updating a supplier payment")
	}
	return nil
}

// InsertAllocationFor records a payment settling part of a bill.
func (r *Repos) InsertAllocationFor(
	ctx context.Context, paymentID id.ID, a domain.Allocation,
) error {
	identifier, err := id.New()
	if err != nil {
		return err
	}
	if _, err = r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO supplier_payment_allocations (
			id, payment_id, bill_id, amount_minor, created_at
		) VALUES (?, ?, ?, ?, ?)`,
		string(identifier), string(paymentID), string(a.BillID),
		a.AmountMinor, r.now()); err != nil {
		return r.wrap(err, "allocating a supplier payment")
	}
	return nil
}

// AllocationsOfPayment reads what a payment settled.
func (r *Repos) AllocationsOfPayment(
	ctx context.Context, paymentID id.ID,
) ([]domain.Allocation, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT bill_id, amount_minor FROM supplier_payment_allocations WHERE payment_id = ?`,
		string(paymentID))
	if err != nil {
		return nil, r.wrap(err, "reading payment allocations")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.Allocation, 0, 4)
	for rows.Next() {
		var allocation domain.Allocation
		if err = rows.Scan(&allocation.BillID, &allocation.AmountMinor); err != nil {
			return nil, r.wrap(err, "reading payment allocations")
		}
		out = append(out, allocation)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "reading payment allocations")
	}
	return out, nil
}

// SettledMinor sums what POSTED payments have already put against a bill.
//
// Derived on read, never stored. A maintained `paid_minor` drifts from the allocations that
// justify it, and the drift is invisible until somebody chases a supplier for money already paid.
func (r *Repos) SettledMinor(ctx context.Context, billID id.ID) (int64, error) {
	var total sql.NullInt64
	err := r.db.Reader(ctx).QueryRowContext(ctx, `
		SELECT SUM(a.amount_minor)
		  FROM supplier_payment_allocations a
		  JOIN supplier_payments p ON p.id = a.payment_id
		 WHERE a.bill_id = ? AND p.status = 'posted' `,
		string(billID)).Scan(&total)
	if err != nil {
		return 0, r.wrap(err, "summing what has been paid")
	}
	return total.Int64, nil
}

func scanPayment(s scanner) (domain.Payment, error) {
	var (
		payment   domain.Payment
		number    any
		reference any
		notes     any
	)
	if err := s.Scan(
		&payment.ID, &payment.CompanyID, &payment.BranchID, &payment.PartnerID,
		&payment.PartnerName, &number, &payment.PaymentDate, &payment.Method,
		&reference, &payment.CurrencyCode, &payment.ExchangeRateMicro,
		&payment.AmountMinor, &payment.Status, &notes,
	); err != nil {
		return domain.Payment{}, err
	}
	payment.Number = text(number)
	payment.Reference = text(reference)
	payment.Notes = text(notes)
	return payment, nil
}
