package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/expenses/domain"
)

const debtColumns = `
	id, company_id, branch_id, status, document_number, direction, kind,
	partner_id, counterparty_name, debt_date, due_date, method, reference,
	description, currency_code, amount_minor`

// InsertDebt writes a draft debt.
func (r *Repos) InsertDebt(ctx context.Context, d domain.Debt, actorID id.ID) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO debts (
			id, company_id, branch_id, status, document_number, direction, kind,
			partner_id, counterparty_name, debt_date, due_date, method, reference,
			description, currency_code, amount_minor,
			row_version, created_at, created_by, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		string(d.ID), string(d.CompanyID), string(d.BranchID), string(d.Status),
		nullable(d.Number), string(d.Direction), string(d.Kind),
		nullableID(d.PartnerID), d.CounterpartyName, d.DebtDate, nullable(d.DueDate),
		string(d.Method), nullable(d.Reference), nullable(d.Description),
		d.CurrencyCode, d.AmountMinor, now, nullableID(actorID), now)
	if err != nil {
		return r.wrap(err, "inserting a debt")
	}
	return nil
}

// DebtByID reads one debt.
func (r *Repos) DebtByID(ctx context.Context, debtID id.ID) (domain.Debt, bool, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+debtColumns+` FROM debts WHERE id = ?`, string(debtID))

	debt, err := scanDebt(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Debt{}, false, nil
	}
	if err != nil {
		return domain.Debt{}, false, r.wrap(err, "reading a debt")
	}
	return debt, true, nil
}

// Debts lists a company's debts, optionally of one kind.
func (r *Repos) Debts(
	ctx context.Context, companyID id.ID, kind domain.Kind,
) ([]domain.Debt, error) {
	query := `SELECT ` + debtColumns + ` FROM debts WHERE company_id = ?`
	args := []any{string(companyID)}
	if kind != "" {
		query += ` AND kind = ?`
		args = append(args, string(kind))
	}
	query += ` ORDER BY debt_date DESC, created_at DESC`

	rows, err := r.db.Reader(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, r.wrap(err, "listing debts")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.Debt, 0, 8)
	for rows.Next() {
		debt, scanErr := scanDebt(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "listing debts")
		}
		out = append(out, debt)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing debts")
	}
	return out, nil
}

// UpdateDebt writes a debt's mutable fields.
func (r *Repos) UpdateDebt(ctx context.Context, d domain.Debt, actorID id.ID) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE debts SET
			status = ?, document_number = ?, due_date = ?, reference = ?, description = ?,
			posted_at = CASE WHEN ? = 'posted' AND posted_at IS NULL THEN ? ELSE posted_at END,
			posted_by = CASE WHEN ? = 'posted' AND posted_by IS NULL THEN ? ELSE posted_by END,
			row_version = row_version + 1, updated_at = ?
		WHERE id = ?`,
		string(d.Status), nullable(d.Number), nullable(d.DueDate),
		nullable(d.Reference), nullable(d.Description),
		string(d.Status), r.now(), string(d.Status), nullableID(actorID),
		r.now(), string(d.ID))
	if err != nil {
		return r.wrap(err, "updating a debt")
	}
	return nil
}

// NetByKind reports the net position of each kind: what has come in less what has gone out.
//
// # Signed on purpose, and only here
//
// The DOCUMENTS keep positive amounts and a direction, because a signed amount makes SUM()
// meaningless. A POSITION is the one place the sign belongs: "we owe 4,000" and "we are owed 300"
// are the two answers this returns, and a reader wants one number per kind.
func (r *Repos) NetByKind(
	ctx context.Context, companyID id.ID,
) (map[domain.Kind]int64, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT kind,
		       SUM(CASE WHEN direction = 'received' THEN amount_minor ELSE -amount_minor END)
		  FROM debts
		 WHERE company_id = ? AND status = 'posted'
		 GROUP BY kind`, string(companyID))
	if err != nil {
		return nil, r.wrap(err, "summing debts by kind")
	}
	defer func() { _ = rows.Close() }()

	out := make(map[domain.Kind]int64, 3)
	for rows.Next() {
		var (
			kind string
			net  int64
		)
		if err = rows.Scan(&kind, &net); err != nil {
			return nil, r.wrap(err, "summing debts by kind")
		}
		out[domain.Kind(kind)] = net
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "summing debts by kind")
	}
	return out, nil
}

func scanDebt(s scanner) (domain.Debt, error) {
	var (
		debt        domain.Debt
		number      any
		partnerID   any
		dueDate     any
		reference   any
		description any
	)
	if err := s.Scan(
		&debt.ID, &debt.CompanyID, &debt.BranchID, &debt.Status, &number,
		&debt.Direction, &debt.Kind, &partnerID, &debt.CounterpartyName,
		&debt.DebtDate, &dueDate, &debt.Method, &reference, &description,
		&debt.CurrencyCode, &debt.AmountMinor,
	); err != nil {
		return domain.Debt{}, err
	}
	debt.Number = text(number)
	debt.PartnerID = id.ID(text(partnerID))
	debt.DueDate = text(dueDate)
	debt.Reference = text(reference)
	debt.Description = text(description)
	return debt, nil
}
