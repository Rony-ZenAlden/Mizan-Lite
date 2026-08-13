package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/purchasing/domain"
)

const receiptColumns = `
	id, company_id, branch_id, warehouse_id, order_id, status, document_number,
	partner_id, partner_name, receipt_date, delivery_note_reference, received_by_name,
	currency_code, value_minor, billed_at, bill_id, notes`

// InsertReceipt writes a draft delivery.
func (r *Repos) InsertReceipt(ctx context.Context, g domain.Receipt, actorID id.ID) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO goods_receipts (
			id, company_id, branch_id, warehouse_id, order_id, status, document_number,
			partner_id, partner_name, receipt_date, delivery_note_reference, received_by_name,
			currency_code, value_minor, billed_at, bill_id, notes,
			row_version, created_at, created_by, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		string(g.ID), string(g.CompanyID), string(g.BranchID), string(g.WarehouseID),
		nullableID(g.OrderID), string(g.Status), nullable(g.Number),
		string(g.PartnerID), g.PartnerName, g.ReceiptDate,
		nullable(g.DeliveryNoteReference), nullable(g.ReceivedByName),
		g.CurrencyCode, g.ValueMinor, nullable(g.BilledAt), nullableID(g.BillID),
		nullable(g.Notes), now, nullableID(actorID), now)
	if err != nil {
		return r.wrap(err, "inserting a goods receipt")
	}
	return nil
}

// ReceiptByID reads one delivery.
func (r *Repos) ReceiptByID(
	ctx context.Context, receiptID id.ID,
) (domain.Receipt, bool, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+receiptColumns+` FROM goods_receipts WHERE id = ?`, string(receiptID))

	receipt, err := scanReceipt(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Receipt{}, false, nil
	}
	if err != nil {
		return domain.Receipt{}, false, r.wrap(err, "reading a goods receipt")
	}
	return receipt, true, nil
}

// Receipts lists a company's deliveries, newest first.
func (r *Repos) Receipts(
	ctx context.Context, companyID, orderID id.ID, status domain.ReceiptStatus,
) ([]domain.Receipt, error) {
	query := `SELECT ` + receiptColumns + ` FROM goods_receipts WHERE company_id = ?`
	args := []any{string(companyID)}
	if !orderID.IsZero() {
		query += ` AND order_id = ?`
		args = append(args, string(orderID))
	}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, string(status))
	}
	query += ` ORDER BY receipt_date DESC, created_at DESC`

	rows, err := r.db.Reader(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, r.wrap(err, "listing goods receipts")
	}
	defer func() { _ = rows.Close() }()

	receipts := make([]domain.Receipt, 0, 8)
	for rows.Next() {
		receipt, scanErr := scanReceipt(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "listing goods receipts")
		}
		receipts = append(receipts, receipt)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing goods receipts")
	}
	return receipts, nil
}

// UpdateReceipt writes a delivery's mutable fields.
func (r *Repos) UpdateReceipt(ctx context.Context, g domain.Receipt, actorID id.ID) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE goods_receipts SET
			status = ?, document_number = ?, value_minor = ?,
			delivery_note_reference = ?, received_by_name = ?, notes = ?,
			billed_at = ?, bill_id = ?,
			confirmed_at = CASE WHEN ? = 'confirmed' AND confirmed_at IS NULL
			                    THEN ? ELSE confirmed_at END,
			confirmed_by = CASE WHEN ? = 'confirmed' AND confirmed_by IS NULL
			                    THEN ? ELSE confirmed_by END,
			row_version = row_version + 1, updated_at = ?
		WHERE id = ?`,
		string(g.Status), nullable(g.Number), g.ValueMinor,
		nullable(g.DeliveryNoteReference), nullable(g.ReceivedByName), nullable(g.Notes),
		nullable(g.BilledAt), nullableID(g.BillID),
		string(g.Status), r.now(), string(g.Status), nullableID(actorID),
		r.now(), string(g.ID))
	if err != nil {
		return r.wrap(err, "updating a goods receipt")
	}
	return nil
}

func scanReceipt(s scanner) (domain.Receipt, error) {
	var (
		receipt    domain.Receipt
		orderID    any
		number     any
		reference  any
		receivedBy any
		billedAt   any
		billID     any
		notes      any
	)
	if err := s.Scan(
		&receipt.ID, &receipt.CompanyID, &receipt.BranchID, &receipt.WarehouseID,
		&orderID, &receipt.Status, &number,
		&receipt.PartnerID, &receipt.PartnerName, &receipt.ReceiptDate,
		&reference, &receivedBy, &receipt.CurrencyCode, &receipt.ValueMinor,
		&billedAt, &billID, &notes,
	); err != nil {
		return domain.Receipt{}, err
	}
	receipt.OrderID = id.ID(text(orderID))
	receipt.Number = text(number)
	receipt.DeliveryNoteReference = text(reference)
	receipt.ReceivedByName = text(receivedBy)
	receipt.BilledAt = text(billedAt)
	receipt.BillID = id.ID(text(billID))
	receipt.Notes = text(notes)
	return receipt, nil
}

// ── receipt lines ───────────────────────────────────────────────────────────────

const receiptLineColumns = `
	id, line_number, order_line_id, product_id, variant_id,
	product_name, variant_sku, uom_code, uom_id,
	quantity_micro, quantity_stock_micro, unit_cost_micro, value_minor,
	lot_id, serial_id, movement_id, notes`

// InsertReceiptLine writes a delivery line.
func (r *Repos) InsertReceiptLine(
	ctx context.Context, receiptID id.ID, l domain.ReceiptLine,
) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO goods_receipt_lines (
			id, receipt_id, line_number, order_line_id, product_id, variant_id,
			product_name, variant_sku, uom_code, uom_id,
			quantity_micro, quantity_stock_micro, unit_cost_micro, value_minor,
			lot_id, serial_id, movement_id, notes, row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		string(l.ID), string(receiptID), l.LineNumber, nullableID(l.OrderLineID),
		string(l.ProductID), string(l.VariantID),
		l.ProductName, l.VariantSKU, l.UomCode, string(l.UomID),
		l.QuantityMicro, l.QuantityStockMicro, l.UnitCostMicro, l.ValueMinor,
		nullableID(l.LotID), nullableID(l.SerialID), nullableID(l.MovementID),
		nullable(l.Notes), now, now)
	if err != nil {
		return r.wrap(err, "inserting a receipt line")
	}
	return nil
}

// ReceiptLines reads a delivery's lines.
func (r *Repos) ReceiptLines(
	ctx context.Context, receiptID id.ID,
) ([]domain.ReceiptLine, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT `+receiptLineColumns+` FROM goods_receipt_lines
		 WHERE receipt_id = ? ORDER BY line_number`, string(receiptID))
	if err != nil {
		return nil, r.wrap(err, "reading receipt lines")
	}
	defer func() { _ = rows.Close() }()

	lines := make([]domain.ReceiptLine, 0, 8)
	for rows.Next() {
		line, scanErr := scanReceiptLine(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "reading receipt lines")
		}
		lines = append(lines, line)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "reading receipt lines")
	}
	return lines, nil
}

// UpdateReceiptLine writes a line's computed value and its movement link.
func (r *Repos) UpdateReceiptLine(ctx context.Context, l domain.ReceiptLine) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE goods_receipt_lines SET
			unit_cost_micro = ?, value_minor = ?, lot_id = ?, serial_id = ?, movement_id = ?,
			row_version = row_version + 1, updated_at = ?
		WHERE id = ?`,
		l.UnitCostMicro, l.ValueMinor, nullableID(l.LotID), nullableID(l.SerialID),
		nullableID(l.MovementID), r.now(), string(l.ID))
	if err != nil {
		return r.wrap(err, "updating a receipt line")
	}
	return nil
}

// DeleteReceiptLine removes a line from a draft delivery.
func (r *Repos) DeleteReceiptLine(ctx context.Context, lineID id.ID) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx,
		`DELETE FROM goods_receipt_lines WHERE id = ?`, string(lineID))
	if err != nil {
		return r.wrap(err, "removing a receipt line")
	}
	return nil
}

// NextReceiptLineNumber reports the next free position on a delivery.
func (r *Repos) NextReceiptLineNumber(ctx context.Context, receiptID id.ID) (int, error) {
	var highest sql.NullInt64
	err := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT MAX(line_number) FROM goods_receipt_lines WHERE receipt_id = ?`,
		string(receiptID)).Scan(&highest)
	if err != nil {
		return 0, r.wrap(err, "reading the next receipt line number")
	}
	return int(highest.Int64) + 1, nil
}

// AddReceived moves an order line's received figure.
//
// A single UPDATE rather than read-modify-write, so two deliveries confirmed at once cannot each
// read 40 and each write 45. The projection and the receipt lines that justify it are written in
// one transaction, which is what makes `VerifyReceived` a check rather than a repair.
func (r *Repos) AddReceived(ctx context.Context, orderLineID id.ID, deltaMicro int64) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE purchase_order_lines
		   SET received_micro = received_micro + ?, row_version = row_version + 1,
		       updated_at = ?
		 WHERE id = ?`,
		deltaMicro, r.now(), string(orderLineID))
	if err != nil {
		return r.wrap(err, "recording what was received")
	}
	return nil
}

// ReceivedFromLines sums what confirmed deliveries actually recorded against an order's lines.
//
// The rebuild half of the projection check: `purchase_order_lines.received_micro` is maintained,
// and this is what it should equal. Reports, never repairs — the same discipline Phase 4 applies
// to `stock_levels`.
func (r *Repos) ReceivedFromLines(
	ctx context.Context, orderID id.ID,
) (map[id.ID]int64, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT l.order_line_id, COALESCE(SUM(l.quantity_micro), 0)
		  FROM goods_receipt_lines l
		  JOIN goods_receipts g ON g.id = l.receipt_id
		 WHERE g.status = 'confirmed' AND l.order_line_id IN (
		       SELECT id FROM purchase_order_lines WHERE order_id = ?)
		 GROUP BY l.order_line_id`, string(orderID))
	if err != nil {
		return nil, r.wrap(err, "recomputing what was received")
	}
	defer func() { _ = rows.Close() }()

	totals := make(map[id.ID]int64, 8)
	for rows.Next() {
		var (
			lineID string
			total  int64
		)
		if err = rows.Scan(&lineID, &total); err != nil {
			return nil, r.wrap(err, "recomputing what was received")
		}
		totals[id.ID(lineID)] = total
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "recomputing what was received")
	}
	return totals, nil
}

// OrderLineByID reads one order line.
func (r *Repos) OrderLineByID(
	ctx context.Context, lineID id.ID,
) (domain.Line, id.ID, bool, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+lineColumns+`, order_id FROM purchase_order_lines WHERE id = ?`,
		string(lineID))

	var (
		line         domain.Line
		supplierCode any
		taxCode      any
		notes        any
		orderID      string
	)
	err := row.Scan(
		&line.ID, &line.LineNumber, &line.ProductID, &line.VariantID,
		&line.ProductName, &line.VariantSKU, &line.UomCode, &supplierCode,
		&line.UomID, &line.QuantityMicro, &line.QuantityStockMicro,
		&line.UnitPriceMicro, &line.DiscountMinor, &line.TaxRateMicro, &taxCode,
		&line.TaxAmountMinor, &line.NetMinor, &line.TotalMinor, &line.ReceivedMicro, &notes,
		&orderID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Line{}, "", false, nil
	}
	if err != nil {
		return domain.Line{}, "", false, r.wrap(err, "reading an order line")
	}
	line.SupplierCode = text(supplierCode)
	line.TaxCode = text(taxCode)
	line.Notes = text(notes)
	return line, id.ID(orderID), true, nil
}

func scanReceiptLine(s scanner) (domain.ReceiptLine, error) {
	var (
		line        domain.ReceiptLine
		orderLineID any
		lotID       any
		serialID    any
		movementID  any
		notes       any
	)
	if err := s.Scan(
		&line.ID, &line.LineNumber, &orderLineID, &line.ProductID, &line.VariantID,
		&line.ProductName, &line.VariantSKU, &line.UomCode, &line.UomID,
		&line.QuantityMicro, &line.QuantityStockMicro, &line.UnitCostMicro, &line.ValueMinor,
		&lotID, &serialID, &movementID, &notes,
	); err != nil {
		return domain.ReceiptLine{}, err
	}
	line.OrderLineID = id.ID(text(orderLineID))
	line.LotID = id.ID(text(lotID))
	line.SerialID = id.ID(text(serialID))
	line.MovementID = id.ID(text(movementID))
	line.Notes = text(notes)
	return line, nil
}
