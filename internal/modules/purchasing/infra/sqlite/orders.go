// Package sqlite is purchasing's storage.
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/purchasing/domain"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// CodeStorage is the coded error a storage fault surfaces as.
const CodeStorage = "purchasing.storage"

// Repos is purchasing's storage.
type Repos struct {
	db  database.DB
	clk clock.Clock
}

// New builds the repositories.
func New(db database.DB, clk clock.Clock) *Repos {
	if clk == nil {
		clk = clock.System()
	}
	return &Repos{db: db, clk: clk}
}

func (r *Repos) now() string { return clock.Format(r.clk.Now()) }

func (r *Repos) wrap(err error, what string) error {
	return errs.Wrap(r.db.Dialect().TranslateError(err), errs.CategoryInternal, CodeStorage, what)
}

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

const orderColumns = `
	id, branch_id, warehouse_id, status, document_number, partner_id, partner_name,
	order_date, expected_date, currency_code, exchange_rate_micro,
	net_minor, tax_minor, discount_minor, total_minor, supplier_reference, notes`

// InsertOrder writes a draft order.
func (r *Repos) InsertOrder(ctx context.Context, o domain.Order, actorID id.ID) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO purchase_orders (
			id, company_id, branch_id, warehouse_id, status, document_number,
			partner_id, partner_name, order_date, expected_date,
			currency_code, exchange_rate_micro,
			net_minor, tax_minor, discount_minor, total_minor,
			supplier_reference, notes, row_version, created_at, created_by, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		string(o.ID), string(o.CompanyID), string(o.BranchID), string(o.WarehouseID),
		string(o.Status), nullable(o.Number),
		string(o.PartnerID), o.PartnerName, o.OrderDate, nullable(o.ExpectedDate),
		o.CurrencyCode, o.ExchangeRateMicro,
		o.NetMinor, o.TaxMinor, o.DiscountMinor, o.TotalMinor,
		nullable(o.SupplierReference), nullable(o.Notes),
		now, nullableID(actorID), now)
	if err != nil {
		return r.wrap(err, "inserting a purchase order")
	}
	return nil
}

// OrderByID reads one order.
func (r *Repos) OrderByID(
	ctx context.Context, orderID id.ID,
) (domain.Order, id.ID, bool, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT company_id, `+orderColumns+` FROM purchase_orders WHERE id = ?`,
		string(orderID))

	order, companyID, err := scanOrder(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Order{}, "", false, nil
	}
	if err != nil {
		return domain.Order{}, "", false, r.wrap(err, "reading a purchase order")
	}
	return order, companyID, true, nil
}

// CompanyOf reports which company an order belongs to.
func (r *Repos) CompanyOf(ctx context.Context, orderID id.ID) (id.ID, error) {
	var companyID string
	err := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT company_id FROM purchase_orders WHERE id = ?`,
		string(orderID)).Scan(&companyID)
	if err != nil {
		return "", r.wrap(err, "reading an order's company")
	}
	return id.ID(companyID), nil
}

// Orders lists a company's orders, newest first, optionally narrowed by status.
func (r *Repos) Orders(
	ctx context.Context, companyID id.ID, status domain.Status,
) ([]domain.Order, error) {
	query := `SELECT company_id, ` + orderColumns + ` FROM purchase_orders WHERE company_id = ?`
	args := []any{string(companyID)}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, string(status))
	}
	query += ` ORDER BY order_date DESC, created_at DESC`

	rows, err := r.db.Reader(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, r.wrap(err, "listing purchase orders")
	}
	defer func() { _ = rows.Close() }()

	orders := make([]domain.Order, 0, 16)
	for rows.Next() {
		order, _, scanErr := scanOrder(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "listing purchase orders")
		}
		orders = append(orders, order)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing purchase orders")
	}
	return orders, nil
}

// UpdateOrder writes an order's mutable fields and bumps its row version.
func (r *Repos) UpdateOrder(ctx context.Context, o domain.Order, actorID id.ID) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE purchase_orders SET
			status = ?, document_number = ?, warehouse_id = ?, expected_date = ?,
			net_minor = ?, tax_minor = ?, discount_minor = ?, total_minor = ?,
			supplier_reference = ?, notes = ?,
			placed_at = CASE WHEN ? = 'placed' AND placed_at IS NULL THEN ? ELSE placed_at END,
			placed_by = CASE WHEN ? = 'placed' AND placed_by IS NULL THEN ? ELSE placed_by END,
			closed_at = CASE WHEN ? = 'closed' AND closed_at IS NULL THEN ? ELSE closed_at END,
			row_version = row_version + 1, updated_at = ?
		WHERE id = ?`,
		string(o.Status), nullable(o.Number), string(o.WarehouseID), nullable(o.ExpectedDate),
		o.NetMinor, o.TaxMinor, o.DiscountMinor, o.TotalMinor,
		nullable(o.SupplierReference), nullable(o.Notes),
		string(o.Status), r.now(), string(o.Status), nullableID(actorID),
		string(o.Status), r.now(), r.now(), string(o.ID))
	if err != nil {
		return r.wrap(err, "updating a purchase order")
	}
	return nil
}

func scanOrder(s scanner) (domain.Order, id.ID, error) {
	var (
		order     domain.Order
		companyID string
		number    any
		expected  any
		reference any
		notes     any
	)
	if err := s.Scan(
		&companyID, &order.ID, &order.BranchID, &order.WarehouseID, &order.Status,
		&number, &order.PartnerID, &order.PartnerName, &order.OrderDate, &expected,
		&order.CurrencyCode, &order.ExchangeRateMicro,
		&order.NetMinor, &order.TaxMinor, &order.DiscountMinor, &order.TotalMinor,
		&reference, &notes,
	); err != nil {
		return domain.Order{}, "", err
	}
	order.CompanyID = id.ID(companyID)
	order.Number = text(number)
	order.ExpectedDate = text(expected)
	order.SupplierReference = text(reference)
	order.Notes = text(notes)
	return order, id.ID(companyID), nil
}

// ── lines ───────────────────────────────────────────────────────────────────────

const lineColumns = `
	id, line_number, product_id, variant_id, product_name, variant_sku, uom_code,
	supplier_code, uom_id, quantity_micro, quantity_stock_micro,
	unit_price_micro, discount_minor, tax_rate_micro, tax_code, tax_amount_minor,
	net_minor, total_minor, received_micro, notes`

// InsertLine writes an order line.
func (r *Repos) InsertLine(ctx context.Context, orderID id.ID, l domain.Line) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO purchase_order_lines (
			id, order_id, line_number, product_id, variant_id,
			product_name, variant_sku, uom_code, supplier_code,
			uom_id, quantity_micro, quantity_stock_micro,
			unit_price_micro, discount_minor, tax_rate_micro, tax_code, tax_amount_minor,
			net_minor, total_minor, received_micro, notes,
			row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		string(l.ID), string(orderID), l.LineNumber, string(l.ProductID), string(l.VariantID),
		l.ProductName, l.VariantSKU, l.UomCode, nullable(l.SupplierCode),
		string(l.UomID), l.QuantityMicro, l.QuantityStockMicro,
		l.UnitPriceMicro, l.DiscountMinor, l.TaxRateMicro, nullable(l.TaxCode),
		l.TaxAmountMinor, l.NetMinor, l.TotalMinor, l.ReceivedMicro, nullable(l.Notes),
		now, now)
	if err != nil {
		return r.wrap(err, "inserting an order line")
	}
	return nil
}

// Lines reads an order's lines, in position order.
func (r *Repos) Lines(ctx context.Context, orderID id.ID) ([]domain.Line, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT `+lineColumns+` FROM purchase_order_lines
		 WHERE order_id = ? ORDER BY line_number`, string(orderID))
	if err != nil {
		return nil, r.wrap(err, "reading order lines")
	}
	defer func() { _ = rows.Close() }()

	lines := make([]domain.Line, 0, 8)
	for rows.Next() {
		line, scanErr := scanLine(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "reading order lines")
		}
		lines = append(lines, line)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "reading order lines")
	}
	return lines, nil
}

// UpdateLine writes a line's computed money back.
func (r *Repos) UpdateLine(ctx context.Context, l domain.Line) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE purchase_order_lines SET
			quantity_micro = ?, quantity_stock_micro = ?,
			unit_price_micro = ?, discount_minor = ?, tax_rate_micro = ?, tax_code = ?,
			tax_amount_minor = ?, net_minor = ?, total_minor = ?, notes = ?,
			row_version = row_version + 1, updated_at = ?
		WHERE id = ?`,
		l.QuantityMicro, l.QuantityStockMicro,
		l.UnitPriceMicro, l.DiscountMinor, l.TaxRateMicro, nullable(l.TaxCode),
		l.TaxAmountMinor, l.NetMinor, l.TotalMinor, nullable(l.Notes),
		r.now(), string(l.ID))
	if err != nil {
		return r.wrap(err, "updating an order line")
	}
	return nil
}

// DeleteLine removes a line from a draft.
func (r *Repos) DeleteLine(ctx context.Context, lineID id.ID) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx,
		`DELETE FROM purchase_order_lines WHERE id = ?`, string(lineID))
	if err != nil {
		return r.wrap(err, "removing an order line")
	}
	return nil
}

// NextLineNumber reports the next free position on an order.
func (r *Repos) NextLineNumber(ctx context.Context, orderID id.ID) (int, error) {
	var highest sql.NullInt64
	err := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT MAX(line_number) FROM purchase_order_lines WHERE order_id = ?`,
		string(orderID)).Scan(&highest)
	if err != nil {
		return 0, r.wrap(err, "reading the next line number")
	}
	return int(highest.Int64) + 1, nil
}

func scanLine(s scanner) (domain.Line, error) {
	var (
		line         domain.Line
		supplierCode any
		taxCode      any
		notes        any
	)
	if err := s.Scan(
		&line.ID, &line.LineNumber, &line.ProductID, &line.VariantID,
		&line.ProductName, &line.VariantSKU, &line.UomCode, &supplierCode,
		&line.UomID, &line.QuantityMicro, &line.QuantityStockMicro,
		&line.UnitPriceMicro, &line.DiscountMinor, &line.TaxRateMicro, &taxCode,
		&line.TaxAmountMinor, &line.NetMinor, &line.TotalMinor, &line.ReceivedMicro, &notes,
	); err != nil {
		return domain.Line{}, err
	}
	line.SupplierCode = text(supplierCode)
	line.TaxCode = text(taxCode)
	line.Notes = text(notes)
	return line, nil
}

// CurrencyDecimals reads a currency's minor-unit scale.
func (r *Repos) CurrencyDecimals(ctx context.Context, code string) (int, error) {
	var decimals int
	err := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT decimal_places FROM currencies WHERE code = ?`, code).Scan(&decimals)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errs.NotFound(CodeStorage, "there is no currency with that code").
			WithParam("code", code)
	}
	if err != nil {
		return 0, r.wrap(err, "reading a currency's scale")
	}
	return decimals, nil
}
