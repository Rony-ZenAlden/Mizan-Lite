package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/sales/domain"
)

// InsertPayment writes a payment.
func (r *Repos) InsertPayment(
	ctx context.Context, companyID id.ID, p domain.Payment, actorID id.ID,
) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO sales_payments (
			id, company_id, branch_id, partner_id, partner_name, document_number,
			payment_date, method, reference, currency_code, exchange_rate_micro,
			amount_minor, status, notes, row_version, created_at, created_by, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		string(p.ID), string(companyID), string(p.BranchID), nullableID(p.PartnerID),
		nullable(p.PartnerName), nullable(p.Number), p.Date, string(p.Method),
		nullable(p.Reference), p.CurrencyCode, p.RateMicro,
		p.AmountMinor, string(p.Status), nullable(p.Notes),
		now, nullableID(actorID), now)
	if err != nil {
		return r.wrap(err, "creating a payment")
	}
	return nil
}

const paymentColumns = `
	id, branch_id, partner_id, partner_name, document_number, payment_date,
	method, reference, currency_code, exchange_rate_micro, amount_minor, status,
	notes, posted_at`

// PaymentByID reads one payment.
func (r *Repos) PaymentByID(
	ctx context.Context, paymentID id.ID,
) (domain.Payment, bool, error) {
	row := r.db.Writer(ctx).QueryRowContext(ctx,
		`SELECT `+paymentColumns+` FROM sales_payments WHERE id = ?`, string(paymentID))

	var (
		p                    domain.Payment
		partner, partnerName any
		number, reference    any
		notes, postedAt      any
		method, status       string
	)
	err := row.Scan(&p.ID, &p.BranchID, &partner, &partnerName, &number, &p.Date,
		&method, &reference, &p.CurrencyCode, &p.RateMicro, &p.AmountMinor, &status,
		&notes, &postedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Payment{}, false, nil
	}
	if err != nil {
		return domain.Payment{}, false, r.wrap(err, "reading a payment")
	}
	p.PartnerID = id.ID(text(partner))
	p.PartnerName = text(partnerName)
	p.Number = text(number)
	p.Method = domain.Method(method)
	p.Reference = text(reference)
	p.Status = domain.Status(status)
	p.Notes = text(notes)
	p.PostedAt = text(postedAt)
	return p, true, nil
}

// SetPaymentPosted marks a payment posted, guarded on it still being a draft.
func (r *Repos) SetPaymentPosted(
	ctx context.Context, paymentID id.ID, number, postedAt string,
) error {
	result, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE sales_payments SET status = 'posted', document_number = ?, posted_at = ?,
			row_version = row_version + 1, updated_at = ?
		 WHERE id = ? AND status = 'draft'`,
		number, postedAt, r.now(), string(paymentID))
	if err != nil {
		return r.wrap(err, "posting a payment")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return r.wrap(err, "posting a payment")
	}
	if affected == 0 {
		return errs.Conflict(domain.CodePaymentNotDraft,
			"that payment is no longer a draft").WithParam("id", string(paymentID))
	}
	return nil
}

// InsertAllocation records a payment settling a document.
func (r *Repos) InsertAllocation(
	ctx context.Context, allocationID, paymentID id.ID, a domain.Allocation,
) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO sales_payment_allocations (
			id, payment_id, document_id, amount_minor, created_at
		) VALUES (?, ?, ?, ?, ?)`,
		string(allocationID), string(paymentID), string(a.DocumentID),
		a.AmountMinor, r.now())
	if err != nil {
		return r.wrap(err, "allocating a payment")
	}
	return nil
}

// Allocations lists what a payment settles.
func (r *Repos) Allocations(
	ctx context.Context, paymentID id.ID,
) ([]domain.Allocation, error) {
	rows, err := r.db.Writer(ctx).QueryContext(ctx, `
		SELECT id, document_id, amount_minor FROM sales_payment_allocations
		 WHERE payment_id = ? ORDER BY created_at`, string(paymentID))
	if err != nil {
		return nil, r.wrap(err, "listing allocations")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.Allocation, 0, 4)
	for rows.Next() {
		var a domain.Allocation
		if err = rows.Scan(&a.ID, &a.DocumentID, &a.AmountMinor); err != nil {
			return nil, r.wrap(err, "reading an allocation")
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SettledOn reports how much has been allocated to a document, across every POSTED payment.
//
// Posted only: a draft payment has not been received, and counting it would show an invoice as
// settled by money nobody has handed over.
func (r *Repos) SettledOn(ctx context.Context, documentID id.ID) (int64, error) {
	var settled sql.NullInt64
	err := r.db.Writer(ctx).QueryRowContext(ctx, `
		SELECT SUM(a.amount_minor)
		  FROM sales_payment_allocations a
		  JOIN sales_payments p ON p.id = a.payment_id
		 WHERE a.document_id = ? AND p.status = 'posted'`, string(documentID)).Scan(&settled)
	if err != nil {
		return 0, r.wrap(err, "reading what a document has been paid")
	}
	return settled.Int64, nil
}
