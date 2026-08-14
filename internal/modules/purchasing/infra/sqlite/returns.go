package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/purchasing/domain"
)

const returnColumns = `
	id, company_id, branch_id, warehouse_id, status, document_number,
	partner_id, partner_name, return_date, reason, supplier_reference,
	currency_code, net_minor, tax_minor, total_minor, cost_minor`

// InsertReturn writes a draft return.
func (r *Repos) InsertReturn(
	ctx context.Context, s domain.SupplierReturn, actorID id.ID,
) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO supplier_returns (
			id, company_id, branch_id, warehouse_id, status, document_number,
			partner_id, partner_name, return_date, reason, supplier_reference,
			currency_code, net_minor, tax_minor, total_minor, cost_minor,
			row_version, created_at, created_by, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		string(s.ID), string(s.CompanyID), string(s.BranchID), string(s.WarehouseID),
		string(s.Status), nullable(s.Number), string(s.PartnerID), s.PartnerName,
		s.ReturnDate, nullable(s.Reason), nullable(s.SupplierReference),
		s.CurrencyCode, s.NetMinor, s.TaxMinor, s.TotalMinor, s.CostMinor,
		now, nullableID(actorID), now)
	if err != nil {
		return r.wrap(err, "inserting a supplier return")
	}
	return nil
}

// ReturnByID reads one return.
func (r *Repos) ReturnByID(
	ctx context.Context, returnID id.ID,
) (domain.SupplierReturn, bool, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+returnColumns+` FROM supplier_returns WHERE id = ?`, string(returnID))

	out, err := scanReturn(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.SupplierReturn{}, false, nil
	}
	if err != nil {
		return domain.SupplierReturn{}, false, r.wrap(err, "reading a supplier return")
	}
	return out, true, nil
}

// Returns lists a company's supplier returns.
func (r *Repos) Returns(
	ctx context.Context, companyID id.ID, status domain.ReturnStatus,
) ([]domain.SupplierReturn, error) {
	query := `SELECT ` + returnColumns + ` FROM supplier_returns WHERE company_id = ?`
	args := []any{string(companyID)}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, string(status))
	}
	query += ` ORDER BY return_date DESC, created_at DESC`

	rows, err := r.db.Reader(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, r.wrap(err, "listing supplier returns")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.SupplierReturn, 0, 8)
	for rows.Next() {
		item, scanErr := scanReturn(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "listing supplier returns")
		}
		out = append(out, item)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing supplier returns")
	}
	return out, nil
}

// UpdateReturn writes a return's mutable fields.
func (r *Repos) UpdateReturn(
	ctx context.Context, s domain.SupplierReturn, actorID id.ID,
) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE supplier_returns SET
			status = ?, document_number = ?, reason = ?, supplier_reference = ?,
			net_minor = ?, tax_minor = ?, total_minor = ?, cost_minor = ?,
			posted_at = CASE WHEN ? = 'posted' AND posted_at IS NULL THEN ? ELSE posted_at END,
			posted_by = CASE WHEN ? = 'posted' AND posted_by IS NULL THEN ? ELSE posted_by END,
			row_version = row_version + 1, updated_at = ?
		WHERE id = ?`,
		string(s.Status), nullable(s.Number), nullable(s.Reason),
		nullable(s.SupplierReference),
		s.NetMinor, s.TaxMinor, s.TotalMinor, s.CostMinor,
		string(s.Status), r.now(), string(s.Status), nullableID(actorID),
		r.now(), string(s.ID))
	if err != nil {
		return r.wrap(err, "updating a supplier return")
	}
	return nil
}

const returnLineColumns = `
	id, line_number, receipt_line_id, product_id, variant_id,
	product_name, variant_sku, uom_code, quantity_micro, quantity_stock_micro,
	unit_price_micro, unit_cost_micro, tax_rate_micro, tax_code, tax_amount_minor,
	net_minor, cost_minor, total_minor, movement_id, notes`

// InsertReturnLine writes a return line.
func (r *Repos) InsertReturnLine(
	ctx context.Context, returnID id.ID, l domain.ReturnLine,
) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO supplier_return_lines (
			id, return_id, line_number, receipt_line_id, product_id, variant_id,
			product_name, variant_sku, uom_code, quantity_micro, quantity_stock_micro,
			unit_price_micro, unit_cost_micro, tax_rate_micro, tax_code, tax_amount_minor,
			net_minor, cost_minor, total_minor, movement_id, notes,
			row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		string(l.ID), string(returnID), l.LineNumber, string(l.ReceiptLineID),
		string(l.ProductID), string(l.VariantID),
		l.ProductName, l.VariantSKU, l.UomCode, l.QuantityMicro, l.QuantityStockMicro,
		l.UnitPriceMicro, l.UnitCostMicro, l.TaxRateMicro, nullable(l.TaxCode),
		l.TaxAmountMinor, l.NetMinor, l.CostMinor, l.TotalMinor,
		nullableID(l.MovementID), nullable(l.Notes), now, now)
	if err != nil {
		return r.wrap(err, "inserting a return line")
	}
	return nil
}

// ReturnLines reads a return's lines.
func (r *Repos) ReturnLines(
	ctx context.Context, returnID id.ID,
) ([]domain.ReturnLine, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT `+returnLineColumns+` FROM supplier_return_lines
		 WHERE return_id = ? ORDER BY line_number`, string(returnID))
	if err != nil {
		return nil, r.wrap(err, "reading return lines")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.ReturnLine, 0, 8)
	for rows.Next() {
		line, scanErr := scanReturnLine(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "reading return lines")
		}
		out = append(out, line)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "reading return lines")
	}
	return out, nil
}

// UpdateReturnLine writes a line's computed money and its movement link.
func (r *Repos) UpdateReturnLine(ctx context.Context, l domain.ReturnLine) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE supplier_return_lines SET
			tax_rate_micro = ?, tax_code = ?, tax_amount_minor = ?,
			net_minor = ?, cost_minor = ?, total_minor = ?, movement_id = ?,
			row_version = row_version + 1, updated_at = ?
		WHERE id = ?`,
		l.TaxRateMicro, nullable(l.TaxCode), l.TaxAmountMinor,
		l.NetMinor, l.CostMinor, l.TotalMinor, nullableID(l.MovementID),
		r.now(), string(l.ID))
	if err != nil {
		return r.wrap(err, "updating a return line")
	}
	return nil
}

// NextReturnLineNumber reports the next free position.
func (r *Repos) NextReturnLineNumber(ctx context.Context, returnID id.ID) (int, error) {
	var highest sql.NullInt64
	err := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT MAX(line_number) FROM supplier_return_lines WHERE return_id = ?`,
		string(returnID)).Scan(&highest)
	if err != nil {
		return 0, r.wrap(err, "reading the next return line number")
	}
	return int(highest.Int64) + 1, nil
}

// AlreadyReturnedMicro sums what POSTED returns have already sent back from a delivery line.
//
// Only posted ones count. A draft return is somebody typing, and counting it would refuse a
// second genuine return while the first is still being entered.
func (r *Repos) AlreadyReturnedMicro(
	ctx context.Context, receiptLineID id.ID,
) (int64, error) {
	var total sql.NullInt64
	err := r.db.Reader(ctx).QueryRowContext(ctx, `
		SELECT SUM(l.quantity_micro)
		  FROM supplier_return_lines l
		  JOIN supplier_returns s ON s.id = l.return_id
		 WHERE l.receipt_line_id = ? AND s.status = 'posted' `,
		string(receiptLineID)).Scan(&total)
	if err != nil {
		return 0, r.wrap(err, "summing what has already gone back")
	}
	return total.Int64, nil
}

func scanReturn(s scanner) (domain.SupplierReturn, error) {
	var (
		out       domain.SupplierReturn
		number    any
		reason    any
		reference any
	)
	if err := s.Scan(
		&out.ID, &out.CompanyID, &out.BranchID, &out.WarehouseID, &out.Status, &number,
		&out.PartnerID, &out.PartnerName, &out.ReturnDate, &reason, &reference,
		&out.CurrencyCode, &out.NetMinor, &out.TaxMinor, &out.TotalMinor, &out.CostMinor,
	); err != nil {
		return domain.SupplierReturn{}, err
	}
	out.Number = text(number)
	out.Reason = text(reason)
	out.SupplierReference = text(reference)
	return out, nil
}

func scanReturnLine(s scanner) (domain.ReturnLine, error) {
	var (
		line       domain.ReturnLine
		taxCode    any
		movementID any
		notes      any
	)
	if err := s.Scan(
		&line.ID, &line.LineNumber, &line.ReceiptLineID, &line.ProductID, &line.VariantID,
		&line.ProductName, &line.VariantSKU, &line.UomCode,
		&line.QuantityMicro, &line.QuantityStockMicro,
		&line.UnitPriceMicro, &line.UnitCostMicro, &line.TaxRateMicro, &taxCode,
		&line.TaxAmountMinor, &line.NetMinor, &line.CostMinor, &line.TotalMinor,
		&movementID, &notes,
	); err != nil {
		return domain.ReturnLine{}, err
	}
	line.TaxCode = text(taxCode)
	line.MovementID = id.ID(text(movementID))
	line.Notes = text(notes)
	return line, nil
}
