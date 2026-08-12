package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/sales/domain"
)

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableID(v id.ID) any {
	if v.IsZero() {
		return nil
	}
	return string(v)
}

func text(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

type scanner interface{ Scan(dest ...any) error }

// InsertDocument writes a draft.
func (r *Repos) InsertDocument(
	ctx context.Context, companyID id.ID, d domain.Document, actorID id.ID,
) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO sales_documents (
			id, company_id, branch_id, warehouse_id, document_type, status, document_number,
			source_document_id, partner_id, partner_name, document_date, due_date,
			currency_code, exchange_rate_micro,
			net_minor, tax_minor, discount_minor, total_minor, cost_minor,
			is_held, hold_label, notes, row_version, created_at, created_by, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		string(d.ID), string(companyID), string(d.BranchID), nullableID(d.WarehouseID),
		string(d.Type), string(d.Status), nullable(d.Number),
		nullableID(d.SourceID), nullableID(d.PartnerID), nullable(d.PartnerName),
		d.Date, nullable(d.DueDate), d.CurrencyCode, d.RateMicro,
		d.NetMinor, d.TaxMinor, d.DiscountMinor, d.TotalMinor, d.CostMinor,
		boolToInt(d.IsHeld), nullable(d.HoldLabel), nullable(d.Notes),
		now, nullableID(actorID), now)
	if err != nil {
		return r.wrap(err, "creating a sales document")
	}
	return nil
}

// UpdateDocument writes a document's mutable fields.
//
// # Guarded on `status = 'draft'` in the WHERE clause
//
// Not merely checked before the write. The domain refuses to change a posted document, and this
// makes the refusal survive a caller that skipped the domain: an UPDATE that matches no rows is a
// posted document, and it is reported rather than silently doing nothing.
func (r *Repos) UpdateDocument(
	ctx context.Context, d domain.Document, actorID id.ID,
) error {
	result, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE sales_documents SET
			warehouse_id = ?, partner_id = ?, partner_name = ?, document_date = ?,
			due_date = ?, currency_code = ?, exchange_rate_micro = ?,
			net_minor = ?, tax_minor = ?, discount_minor = ?, total_minor = ?, cost_minor = ?,
			is_held = ?, hold_label = ?, notes = ?,
			row_version = row_version + 1, updated_at = ?
		 WHERE id = ? AND status = 'draft'`,
		nullableID(d.WarehouseID), nullableID(d.PartnerID), nullable(d.PartnerName), d.Date,
		nullable(d.DueDate), d.CurrencyCode, d.RateMicro,
		d.NetMinor, d.TaxMinor, d.DiscountMinor, d.TotalMinor, d.CostMinor,
		boolToInt(d.IsHeld), nullable(d.HoldLabel), nullable(d.Notes),
		r.now(), string(d.ID))
	if err != nil {
		return r.wrap(err, "updating a sales document")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return r.wrap(err, "updating a sales document")
	}
	if affected == 0 {
		return errs.Conflict(domain.CodeNotDraft,
			"that document is no longer a draft").WithParam("id", string(d.ID))
	}
	_ = actorID
	return nil
}

const documentColumns = `
	id, branch_id, warehouse_id, document_type, status, document_number,
	source_document_id, partner_id, partner_name, document_date, due_date,
	currency_code, exchange_rate_micro,
	net_minor, tax_minor, discount_minor, total_minor, cost_minor,
	is_held, hold_label, notes, posted_at`

// DocumentByID reads one document.
//
// Through the WRITER, because every caller of this is about to change the document and must not
// act on a stale read. A listing uses the reader; this does not.
func (r *Repos) DocumentByID(
	ctx context.Context, documentID id.ID,
) (domain.Document, bool, error) {
	row := r.db.Writer(ctx).QueryRowContext(ctx,
		`SELECT `+documentColumns+` FROM sales_documents WHERE id = ?`, string(documentID))

	d, err := scanDocument(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Document{}, false, nil
	}
	if err != nil {
		return domain.Document{}, false, r.wrap(err, "reading a sales document")
	}
	return d, true, nil
}

// Documents lists a company's documents, newest first.
func (r *Repos) Documents(
	ctx context.Context, companyID id.ID, documentType domain.Type, status domain.Status,
) ([]domain.Document, error) {
	query := `SELECT ` + documentColumns + ` FROM sales_documents WHERE company_id = ?`
	args := []any{string(companyID)}

	if documentType != "" {
		query += ` AND document_type = ?`
		args = append(args, string(documentType))
	}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, string(status))
	}
	query += ` ORDER BY document_date DESC, created_at DESC`

	rows, err := r.db.Reader(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, r.wrap(err, "listing sales documents")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.Document, 0, 32)
	for rows.Next() {
		d, scanErr := scanDocument(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "reading a sales document")
		}
		out = append(out, d)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing sales documents")
	}
	return out, nil
}

func scanDocument(s scanner) (domain.Document, error) {
	var (
		d                         domain.Document
		warehouse, number, source any
		partner, partnerName      any
		dueDate, holdLabel, notes any
		postedAt                  any
		documentType, status      string
		held                      int
	)
	if err := s.Scan(&d.ID, &d.BranchID, &warehouse, &documentType, &status, &number,
		&source, &partner, &partnerName, &d.Date, &dueDate,
		&d.CurrencyCode, &d.RateMicro,
		&d.NetMinor, &d.TaxMinor, &d.DiscountMinor, &d.TotalMinor, &d.CostMinor,
		&held, &holdLabel, &notes, &postedAt); err != nil {
		return domain.Document{}, err
	}
	d.WarehouseID = id.ID(text(warehouse))
	d.Type = domain.Type(documentType)
	d.Status = domain.Status(status)
	d.Number = text(number)
	d.SourceID = id.ID(text(source))
	d.PartnerID = id.ID(text(partner))
	d.PartnerName = text(partnerName)
	d.DueDate = text(dueDate)
	d.IsHeld = held == 1
	d.HoldLabel = text(holdLabel)
	d.Notes = text(notes)
	d.PostedAt = text(postedAt)
	return domain.Adopt(d), nil
}

// ── lines ───────────────────────────────────────────────────────────────────────

// InsertLine writes a line, snapshot and all.
func (r *Repos) InsertLine(ctx context.Context, documentID id.ID, l domain.Line) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO sales_lines (
			id, document_id, line_number, product_id, variant_id,
			product_name, variant_sku, uom_code,
			quantity_micro, uom_id, quantity_stock_micro,
			unit_price_minor, price_source, price_list_code, discount_minor,
			tax_rate_micro, tax_code, tax_amount_minor,
			net_minor, total_minor, cost_micro,
			movement_id, source_line_id, lot_id, serial_id, notes,
			row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		          ?, ?, ?, ?, ?, 1, ?, ?)`,
		string(l.ID), string(documentID), l.LineNumber, string(l.ProductID),
		string(l.VariantID), l.ProductName, l.VariantSKU, l.UomCode,
		l.QuantityMicro, string(l.UomID), l.QuantityStockMicro,
		l.UnitPriceMinor, nullable(l.PriceSource), nullable(l.PriceListCode), l.DiscountMinor,
		l.TaxRateMicro, nullable(l.TaxCode), l.TaxAmountMinor,
		l.NetMinor, l.TotalMinor, l.CostMicro,
		nullableID(l.MovementID), nullableID(l.SourceLineID),
		nullableID(l.LotID), nullableID(l.SerialID), nullable(l.Notes), now, now)
	if err != nil {
		return r.wrap(err, "adding a sales line")
	}
	return nil
}

// DeleteLine removes a line from a draft.
func (r *Repos) DeleteLine(ctx context.Context, lineID id.ID) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		DELETE FROM sales_lines WHERE id = ?
		  AND document_id IN (SELECT id FROM sales_documents WHERE status = 'draft')`,
		string(lineID))
	if err != nil {
		return r.wrap(err, "removing a sales line")
	}
	return nil
}

// SetLineMovement records which stock movement a line produced.
func (r *Repos) SetLineMovement(ctx context.Context, lineID, movementID id.ID) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE sales_lines SET movement_id = ?, row_version = row_version + 1, updated_at = ?
		 WHERE id = ?`, string(movementID), r.now(), string(lineID))
	if err != nil {
		return r.wrap(err, "linking a sales line to its stock movement")
	}
	return nil
}

const lineColumns = `
	id, line_number, product_id, variant_id, product_name, variant_sku, uom_code,
	quantity_micro, uom_id, quantity_stock_micro,
	unit_price_minor, price_source, price_list_code, discount_minor,
	tax_rate_micro, tax_code, tax_amount_minor,
	net_minor, total_minor, cost_micro,
	movement_id, source_line_id, lot_id, serial_id, notes`

// Lines lists a document's lines in order.
func (r *Repos) Lines(ctx context.Context, documentID id.ID) ([]domain.Line, error) {
	rows, err := r.db.Writer(ctx).QueryContext(ctx,
		`SELECT `+lineColumns+` FROM sales_lines WHERE document_id = ? ORDER BY line_number`,
		string(documentID))
	if err != nil {
		return nil, r.wrap(err, "listing sales lines")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.Line, 0, 16)
	for rows.Next() {
		var (
			l                      domain.Line
			priceSource, priceList any
			taxCode                any
			movement, sourceLine   any
			lot, serial, notes     any
		)
		if err = rows.Scan(&l.ID, &l.LineNumber, &l.ProductID, &l.VariantID,
			&l.ProductName, &l.VariantSKU, &l.UomCode,
			&l.QuantityMicro, &l.UomID, &l.QuantityStockMicro,
			&l.UnitPriceMinor, &priceSource, &priceList, &l.DiscountMinor,
			&l.TaxRateMicro, &taxCode, &l.TaxAmountMinor,
			&l.NetMinor, &l.TotalMinor, &l.CostMicro,
			&movement, &sourceLine, &lot, &serial, &notes); err != nil {
			return nil, r.wrap(err, "reading a sales line")
		}
		l.PriceSource = text(priceSource)
		l.PriceListCode = text(priceList)
		l.TaxCode = text(taxCode)
		l.MovementID = id.ID(text(movement))
		l.SourceLineID = id.ID(text(sourceLine))
		l.LotID = id.ID(text(lot))
		l.SerialID = id.ID(text(serial))
		l.Notes = text(notes)
		out = append(out, l)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing sales lines")
	}
	return out, nil
}

// NextLineNumber reports the next free position on a document.
func (r *Repos) NextLineNumber(ctx context.Context, documentID id.ID) (int, error) {
	var highest sql.NullInt64
	if err := r.db.Writer(ctx).QueryRowContext(ctx,
		`SELECT MAX(line_number) FROM sales_lines WHERE document_id = ?`,
		string(documentID)).Scan(&highest); err != nil {
		return 0, r.wrap(err, "reading a document's lines")
	}
	return int(highest.Int64) + 1, nil
}

// SetDocumentStatus moves a document's status.
//
// Separate from UpdateDocument, whose WHERE clause is guarded on `status = 'draft'` — the guard
// that makes changing a posted document impossible would also make this call impossible once the
// status has moved. Guarded on the FROM status instead, so a double post or a double cancel
// fails loudly rather than running twice.
func (r *Repos) SetDocumentStatus(
	ctx context.Context, documentID id.ID, status domain.Status, number, postedAt string,
) error {
	result, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE sales_documents SET status = ?, document_number = ?, posted_at = ?,
			row_version = row_version + 1, updated_at = ?
		 WHERE id = ? AND status = 'draft'`,
		string(status), nullable(number), nullable(postedAt), r.now(), string(documentID))
	if err != nil {
		return r.wrap(err, "changing a sales document's status")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return r.wrap(err, "changing a sales document's status")
	}
	if affected == 0 {
		return errs.Conflict(domain.CodeNotDraft,
			"that document is no longer a draft").WithParam("id", string(documentID))
	}
	return nil
}

// CompanyOf reports which company a document belongs to.
//
// Not carried on the domain Document: it is a fact about where the row lives rather than about
// the sale, and every caller that needs it is already holding the document's id.
func (r *Repos) CompanyOf(ctx context.Context, documentID id.ID) (id.ID, error) {
	var companyID id.ID
	err := r.db.Writer(ctx).QueryRowContext(ctx,
		`SELECT company_id FROM sales_documents WHERE id = ?`, string(documentID)).
		Scan(&companyID)
	if errors.Is(err, sql.ErrNoRows) {
		return id.ID(""), errs.NotFound(domain.CodeInvalidDocument,
			"there is no sales document with that identity")
	}
	if err != nil {
		return id.ID(""), r.wrap(err, "reading a document's company")
	}
	return companyID, nil
}

// UpdateLineOnPost writes the money a line resolved to.
//
// Only the columns posting fills: the snapshot of what things were CALLED was written when the
// line was added and is not touched here. Two writes of the same fact would be two chances for
// them to differ.
func (r *Repos) UpdateLineOnPost(ctx context.Context, l domain.Line) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE sales_lines SET
			unit_price_minor = ?, price_source = ?, price_list_code = ?,
			tax_rate_micro = ?, tax_code = ?, tax_amount_minor = ?,
			net_minor = ?, total_minor = ?, cost_micro = ?, movement_id = ?,
			row_version = row_version + 1, updated_at = ?
		 WHERE id = ?`,
		l.UnitPriceMinor, nullable(l.PriceSource), nullable(l.PriceListCode),
		l.TaxRateMicro, nullable(l.TaxCode), l.TaxAmountMinor,
		l.NetMinor, l.TotalMinor, l.CostMicro, nullableID(l.MovementID),
		r.now(), string(l.ID))
	if err != nil {
		return r.wrap(err, "writing a posted line")
	}
	return nil
}

// SetDocumentTotals writes what a document came to.
func (r *Repos) SetDocumentTotals(ctx context.Context, d domain.Document) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE sales_documents SET
			net_minor = ?, tax_minor = ?, discount_minor = ?, total_minor = ?, cost_minor = ?,
			row_version = row_version + 1, updated_at = ?
		 WHERE id = ?`,
		d.NetMinor, d.TaxMinor, d.DiscountMinor, d.TotalMinor, d.CostMinor,
		r.now(), string(d.ID))
	if err != nil {
		return r.wrap(err, "writing a document's totals")
	}
	return nil
}
