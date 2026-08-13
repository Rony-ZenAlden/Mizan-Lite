package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/purchasing/domain"
)

const billColumns = `
	id, company_id, branch_id, status, document_number, partner_id, partner_name,
	bill_date, due_date, supplier_invoice_number, currency_code, exchange_rate_micro,
	net_minor, tax_minor, discount_minor, total_minor, accrued_minor, variance_minor, notes`

// InsertBill writes a draft bill.
func (r *Repos) InsertBill(ctx context.Context, b domain.Bill, actorID id.ID) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO purchase_bills (
			id, company_id, branch_id, status, document_number, partner_id, partner_name,
			bill_date, due_date, supplier_invoice_number, currency_code, exchange_rate_micro,
			net_minor, tax_minor, discount_minor, total_minor, accrued_minor, variance_minor,
			notes, row_version, created_at, created_by, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		string(b.ID), string(b.CompanyID), string(b.BranchID), string(b.Status),
		nullable(b.Number), string(b.PartnerID), b.PartnerName,
		b.BillDate, nullable(b.DueDate), b.SupplierInvoiceNumber,
		b.CurrencyCode, b.ExchangeRateMicro,
		b.NetMinor, b.TaxMinor, b.DiscountMinor, b.TotalMinor,
		b.AccruedMinor, b.VarianceMinor, nullable(b.Notes),
		now, nullableID(actorID), now)
	if err != nil {
		return r.wrap(err, "inserting a purchase bill")
	}
	return nil
}

// BillByID reads one bill.
func (r *Repos) BillByID(ctx context.Context, billID id.ID) (domain.Bill, bool, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+billColumns+` FROM purchase_bills WHERE id = ?`, string(billID))

	bill, err := scanBill(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Bill{}, false, nil
	}
	if err != nil {
		return domain.Bill{}, false, r.wrap(err, "reading a purchase bill")
	}
	return bill, true, nil
}

// Bills lists a company's supplier invoices.
func (r *Repos) Bills(
	ctx context.Context, companyID id.ID, status domain.BillStatus,
) ([]domain.Bill, error) {
	query := `SELECT ` + billColumns + ` FROM purchase_bills WHERE company_id = ?`
	args := []any{string(companyID)}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, string(status))
	}
	query += ` ORDER BY bill_date DESC, created_at DESC`

	rows, err := r.db.Reader(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, r.wrap(err, "listing purchase bills")
	}
	defer func() { _ = rows.Close() }()

	bills := make([]domain.Bill, 0, 8)
	for rows.Next() {
		bill, scanErr := scanBill(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "listing purchase bills")
		}
		bills = append(bills, bill)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing purchase bills")
	}
	return bills, nil
}

// UpdateBill writes a bill's mutable fields.
func (r *Repos) UpdateBill(ctx context.Context, b domain.Bill, actorID id.ID) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE purchase_bills SET
			status = ?, document_number = ?, due_date = ?,
			net_minor = ?, tax_minor = ?, discount_minor = ?, total_minor = ?,
			accrued_minor = ?, variance_minor = ?, notes = ?,
			posted_at = CASE WHEN ? = 'posted' AND posted_at IS NULL THEN ? ELSE posted_at END,
			posted_by = CASE WHEN ? = 'posted' AND posted_by IS NULL THEN ? ELSE posted_by END,
			row_version = row_version + 1, updated_at = ?
		WHERE id = ?`,
		string(b.Status), nullable(b.Number), nullable(b.DueDate),
		b.NetMinor, b.TaxMinor, b.DiscountMinor, b.TotalMinor,
		b.AccruedMinor, b.VarianceMinor, nullable(b.Notes),
		string(b.Status), r.now(), string(b.Status), nullableID(actorID),
		r.now(), string(b.ID))
	if err != nil {
		return r.wrap(err, "updating a purchase bill")
	}
	return nil
}

func scanBill(s scanner) (domain.Bill, error) {
	var (
		bill    domain.Bill
		number  any
		dueDate any
		notes   any
	)
	if err := s.Scan(
		&bill.ID, &bill.CompanyID, &bill.BranchID, &bill.Status, &number,
		&bill.PartnerID, &bill.PartnerName, &bill.BillDate, &dueDate,
		&bill.SupplierInvoiceNumber, &bill.CurrencyCode, &bill.ExchangeRateMicro,
		&bill.NetMinor, &bill.TaxMinor, &bill.DiscountMinor, &bill.TotalMinor,
		&bill.AccruedMinor, &bill.VarianceMinor, &notes,
	); err != nil {
		return domain.Bill{}, err
	}
	bill.Number = text(number)
	bill.DueDate = text(dueDate)
	bill.Notes = text(notes)
	return bill, nil
}

// ── bill lines ──────────────────────────────────────────────────────────────────

const billLineColumns = `
	id, line_number, receipt_line_id, product_id, variant_id,
	product_name, variant_sku, uom_code, quantity_micro,
	unit_price_micro, accrued_unit_cost_micro, discount_minor,
	tax_rate_micro, tax_code, tax_amount_minor, net_minor, accrued_minor, total_minor, notes`

// InsertBillLine writes a bill line.
func (r *Repos) InsertBillLine(ctx context.Context, billID id.ID, l domain.BillLine) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO purchase_bill_lines (
			id, bill_id, line_number, receipt_line_id, product_id, variant_id,
			product_name, variant_sku, uom_code, quantity_micro,
			unit_price_micro, accrued_unit_cost_micro, discount_minor,
			tax_rate_micro, tax_code, tax_amount_minor, net_minor, accrued_minor, total_minor,
			notes, row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		string(l.ID), string(billID), l.LineNumber, string(l.ReceiptLineID),
		string(l.ProductID), string(l.VariantID),
		l.ProductName, l.VariantSKU, l.UomCode, l.QuantityMicro,
		l.UnitPriceMicro, l.AccruedUnitCostMicro, l.DiscountMinor,
		l.TaxRateMicro, nullable(l.TaxCode), l.TaxAmountMinor,
		l.NetMinor, l.AccruedMinor, l.TotalMinor, nullable(l.Notes), now, now)
	if err != nil {
		return r.wrap(err, "inserting a bill line")
	}
	return nil
}

// BillLines reads a bill's lines.
func (r *Repos) BillLines(ctx context.Context, billID id.ID) ([]domain.BillLine, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT `+billLineColumns+` FROM purchase_bill_lines
		 WHERE bill_id = ? ORDER BY line_number`, string(billID))
	if err != nil {
		return nil, r.wrap(err, "reading bill lines")
	}
	defer func() { _ = rows.Close() }()

	lines := make([]domain.BillLine, 0, 8)
	for rows.Next() {
		line, scanErr := scanBillLine(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "reading bill lines")
		}
		lines = append(lines, line)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "reading bill lines")
	}
	return lines, nil
}

// UpdateBillLine writes a line's computed money back.
func (r *Repos) UpdateBillLine(ctx context.Context, l domain.BillLine) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE purchase_bill_lines SET
			unit_price_micro = ?, discount_minor = ?, tax_rate_micro = ?, tax_code = ?,
			tax_amount_minor = ?, net_minor = ?, total_minor = ?,
			row_version = row_version + 1, updated_at = ?
		WHERE id = ?`,
		l.UnitPriceMicro, l.DiscountMinor, l.TaxRateMicro, nullable(l.TaxCode),
		l.TaxAmountMinor, l.NetMinor, l.TotalMinor, r.now(), string(l.ID))
	if err != nil {
		return r.wrap(err, "updating a bill line")
	}
	return nil
}

// DeleteBillLine removes a line from a draft bill.
func (r *Repos) DeleteBillLine(ctx context.Context, lineID id.ID) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx,
		`DELETE FROM purchase_bill_lines WHERE id = ?`, string(lineID))
	if err != nil {
		return r.wrap(err, "removing a bill line")
	}
	return nil
}

// NextBillLineNumber reports the next free position on a bill.
func (r *Repos) NextBillLineNumber(ctx context.Context, billID id.ID) (int, error) {
	var highest sql.NullInt64
	err := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT MAX(line_number) FROM purchase_bill_lines WHERE bill_id = ?`,
		string(billID)).Scan(&highest)
	if err != nil {
		return 0, r.wrap(err, "reading the next bill line number")
	}
	return int(highest.Int64) + 1, nil
}

// ReceiptLineByID reads one delivery line, with the receipt it belongs to.
func (r *Repos) ReceiptLineByID(
	ctx context.Context, lineID id.ID,
) (domain.ReceiptLine, id.ID, bool, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+receiptLineColumns+`, receipt_id FROM goods_receipt_lines WHERE id = ?`,
		string(lineID))

	var (
		line        domain.ReceiptLine
		orderLineID any
		lotID       any
		serialID    any
		movementID  any
		notes       any
		receiptID   string
	)
	err := row.Scan(
		&line.ID, &line.LineNumber, &orderLineID, &line.ProductID, &line.VariantID,
		&line.ProductName, &line.VariantSKU, &line.UomCode, &line.UomID,
		&line.QuantityMicro, &line.QuantityStockMicro, &line.UnitCostMicro, &line.ValueMinor,
		&lotID, &serialID, &movementID, &notes, &receiptID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ReceiptLine{}, "", false, nil
	}
	if err != nil {
		return domain.ReceiptLine{}, "", false, r.wrap(err, "reading a receipt line")
	}
	line.OrderLineID = id.ID(text(orderLineID))
	line.LotID = id.ID(text(lotID))
	line.SerialID = id.ID(text(serialID))
	line.MovementID = id.ID(text(movementID))
	line.Notes = text(notes)
	return line, id.ID(receiptID), true, nil
}

// MarkReceiptsBilled links every receipt a bill took up.
func (r *Repos) MarkReceiptsBilled(
	ctx context.Context, billID id.ID, receiptIDs []id.ID,
) error {
	now := r.now()
	for _, receiptID := range receiptIDs {
		_, err := r.db.Writer(ctx).ExecContext(ctx, `
			UPDATE goods_receipts
			   SET billed_at = ?, bill_id = ?, row_version = row_version + 1, updated_at = ?
			 WHERE id = ?`,
			now, string(billID), now, string(receiptID))
		if err != nil {
			return r.wrap(err, "marking a receipt billed")
		}
	}
	return nil
}

func scanBillLine(s scanner) (domain.BillLine, error) {
	var (
		line    domain.BillLine
		taxCode any
		notes   any
	)
	if err := s.Scan(
		&line.ID, &line.LineNumber, &line.ReceiptLineID, &line.ProductID, &line.VariantID,
		&line.ProductName, &line.VariantSKU, &line.UomCode, &line.QuantityMicro,
		&line.UnitPriceMicro, &line.AccruedUnitCostMicro, &line.DiscountMinor,
		&line.TaxRateMicro, &taxCode, &line.TaxAmountMinor,
		&line.NetMinor, &line.AccruedMinor, &line.TotalMinor, &notes,
	); err != nil {
		return domain.BillLine{}, err
	}
	line.TaxCode = text(taxCode)
	line.Notes = text(notes)
	return line, nil
}

// BillBySupplierInvoice finds a bill already entered under a supplier's own invoice number.
func (r *Repos) BillBySupplierInvoice(
	ctx context.Context, companyID, partnerID id.ID, invoiceNumber string,
) (id.ID, bool, error) {
	var billID string
	err := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT id FROM purchase_bills
		  WHERE company_id = ? AND partner_id = ? AND supplier_invoice_number = ?`,
		string(companyID), string(partnerID), invoiceNumber).Scan(&billID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, r.wrap(err, "looking for a duplicate supplier invoice")
	}
	return id.ID(billID), true, nil
}
