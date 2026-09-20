package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/sales/domain"
)

// The sale_returns tables (2026-09-20). A return is written once and never updated: a mistake in one is corrected by
// returning the rest, or by a reversal of the money it moved, never by rewriting what the shop already handed back.

const returnColumns = `id, return_no, sale_id, business_date, returned_at, settlement, settlement_currency, refund_minor,
	refund_local_minor, refund_usd_minor, cost_usd_minor, cost_local_minor, cost_known, fx_rate_id, local_per_usd_nano,
	customer_id, debt_entry_id, reason`

const returnLineColumns = `id, return_id, sale_line_id, line_no, product_id, quantity_micro, refund_local_minor,
	refund_usd_minor, unit_cost_usd_micro, cost_known, cost_usd_minor, cost_local_minor, restocked`

// NextReturnNo is one more than the highest return number, or 1.
func (s *Store) NextReturnNo(ctx context.Context) (int64, error) {
	var next int64
	err := s.db.Reader(ctx).QueryRowContext(ctx, `SELECT COALESCE(MAX(return_no), 0) + 1 FROM sale_returns`).Scan(&next)
	return next, s.db.Dialect().TranslateError(err)
}

// InsertReturn writes a return and its lines.
func (s *Store) InsertReturn(ctx context.Context, r domain.Return) error {
	w := s.db.Writer(ctx)
	var customer, debtEntry any
	if !r.CustomerID.IsZero() {
		customer = r.CustomerID.String()
	}
	if !r.DebtEntryID.IsZero() {
		debtEntry = r.DebtEntryID.String()
	}
	_, err := w.ExecContext(ctx, `INSERT INTO sale_returns (`+returnColumns+`, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID.String(), r.ReturnNo, r.SaleID.String(), r.BusinessDate, clock.Format(r.ReturnedAt),
		string(r.Settlement), r.SettlementCurrency, r.RefundMinor, r.RefundLocalMinor, r.RefundUSDMinor,
		r.CostUSDMinor, r.CostLocalMinor, boolToInt(r.CostKnown), r.RateID.String(), r.RateNano,
		customer, debtEntry, r.Reason, clock.Format(s.clk.Now()))
	if err != nil {
		return s.db.Dialect().TranslateError(err)
	}
	for _, l := range r.Lines {
		if _, err = w.ExecContext(ctx, `INSERT INTO sale_return_lines (`+returnLineColumns+`)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			l.ID.String(), r.ID.String(), l.SaleLineID.String(), l.LineNo, l.ProductID.String(), l.QuantityMicro,
			l.RefundLocalMinor, l.RefundUSDMinor, l.UnitCostMicro, boolToInt(l.CostKnown),
			l.CostUSDMinor, l.CostLocalMinor, boolToInt(l.Restocked)); err != nil {
			return s.db.Dialect().TranslateError(err)
		}
	}
	return nil
}

// ReturnedOf is how much of each of a sale's lines earlier returns already took back.
func (s *Store) ReturnedOf(ctx context.Context, saleID id.ID) (domain.Returned, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx, `
		SELECT l.sale_line_id, SUM(l.quantity_micro)
		FROM sale_return_lines l JOIN sale_returns r ON r.id = l.return_id
		WHERE r.sale_id = ? GROUP BY l.sale_line_id`, saleID.String())
	if err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	defer func() { _ = rows.Close() }()
	out := domain.Returned{}
	for rows.Next() {
		var raw string
		var total int64
		if err = rows.Scan(&raw, &total); err != nil {
			return nil, s.db.Dialect().TranslateError(err)
		}
		lineID, parseErr := id.Parse(raw)
		if parseErr != nil {
			return nil, corrupt(parseErr)
		}
		out[lineID] = total
	}
	if err = rows.Err(); err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	return out, nil
}

// GetReturn returns one return with its lines.
func (s *Store) GetReturn(ctx context.Context, returnID id.ID) (domain.Return, error) {
	out, err := s.scanReturn(s.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+returnColumns+`, (SELECT receipt_no FROM sales WHERE sales.id = sale_returns.sale_id)
		 FROM sale_returns WHERE id = ?`, returnID.String()))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Return{}, errs.NotFound(domain.CodeReturnNotFound, "no such return")
	}
	if err != nil {
		return domain.Return{}, err
	}
	if out.Lines, err = s.returnLines(ctx, returnID); err != nil {
		return domain.Return{}, err
	}
	return out, nil
}

// ReturnsRange returns the returns recorded on a business date from..to inclusive, by return number, without lines.
func (s *Store) ReturnsRange(ctx context.Context, from, to string) ([]domain.Return, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx,
		`SELECT `+returnColumns+`, (SELECT receipt_no FROM sales WHERE sales.id = sale_returns.sale_id)
		 FROM sale_returns WHERE business_date BETWEEN ? AND ? ORDER BY return_no`, from, to)
	if err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Return
	for rows.Next() {
		r, scanErr := s.scanReturn(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	return out, nil
}

// ReturnsOfSale returns every return recorded against one sale, newest number last, with their lines.
func (s *Store) ReturnsOfSale(ctx context.Context, saleID id.ID) ([]domain.Return, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx,
		`SELECT `+returnColumns+`, (SELECT receipt_no FROM sales WHERE sales.id = sale_returns.sale_id)
		 FROM sale_returns WHERE sale_id = ? ORDER BY return_no`, saleID.String())
	if err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Return
	for rows.Next() {
		r, scanErr := s.scanReturn(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	for i := range out {
		if out[i].Lines, err = s.returnLines(ctx, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) returnLines(ctx context.Context, returnID id.ID) ([]domain.ReturnLine, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx, `
		SELECT l.`+"id, l.sale_line_id, l.line_no, l.product_id, l.quantity_micro, l.refund_local_minor,"+`
		       l.refund_usd_minor, l.unit_cost_usd_micro, l.cost_known, l.cost_usd_minor, l.cost_local_minor, l.restocked,
		       s.name_ar_snapshot, s.name_en_snapshot, s.unit_code_snapshot, s.quantity_micro
		FROM sale_return_lines l JOIN sale_lines s ON s.id = l.sale_line_id
		WHERE l.return_id = ? ORDER BY l.line_no`, returnID.String())
	if err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.ReturnLine
	for rows.Next() {
		var l domain.ReturnLine
		var rawID, rawLine, rawProduct string
		var nameEN sql.NullString
		var costKnown, restocked int
		if err = rows.Scan(&rawID, &rawLine, &l.LineNo, &rawProduct, &l.QuantityMicro, &l.RefundLocalMinor,
			&l.RefundUSDMinor, &l.UnitCostMicro, &costKnown, &l.CostUSDMinor, &l.CostLocalMinor, &restocked,
			&l.NameAR, &nameEN, &l.UnitCode, &l.SoldMicro); err != nil {
			return nil, s.db.Dialect().TranslateError(err)
		}
		if l.ID, err = id.Parse(rawID); err != nil {
			return nil, corrupt(err)
		}
		if l.SaleLineID, err = id.Parse(rawLine); err != nil {
			return nil, corrupt(err)
		}
		if l.ProductID, err = id.Parse(rawProduct); err != nil {
			return nil, corrupt(err)
		}
		l.NameEN, l.CostKnown, l.Restocked = nameEN.String, costKnown == 1, restocked == 1
		out = append(out, l)
	}
	return out, s.db.Dialect().TranslateError(rows.Err())
}

func (s *Store) scanReturn(row scanner) (domain.Return, error) {
	var r domain.Return
	var rawID, rawSale, rawRate, returnedAt, settlement string
	var customer, debtEntry sql.NullString
	var costKnown int
	if err := row.Scan(&rawID, &r.ReturnNo, &rawSale, &r.BusinessDate, &returnedAt, &settlement,
		&r.SettlementCurrency, &r.RefundMinor, &r.RefundLocalMinor, &r.RefundUSDMinor, &r.CostUSDMinor,
		&r.CostLocalMinor, &costKnown, &rawRate, &r.RateNano, &customer, &debtEntry, &r.Reason,
		&r.SaleReceiptNo); err != nil {
		return domain.Return{}, err
	}
	var err error
	if r.ID, err = id.Parse(rawID); err != nil {
		return domain.Return{}, corrupt(err)
	}
	if r.SaleID, err = id.Parse(rawSale); err != nil {
		return domain.Return{}, corrupt(err)
	}
	if r.RateID, err = id.Parse(rawRate); err != nil {
		return domain.Return{}, corrupt(err)
	}
	if customer.Valid {
		if r.CustomerID, err = id.Parse(customer.String); err != nil {
			return domain.Return{}, corrupt(err)
		}
	}
	if debtEntry.Valid {
		if r.DebtEntryID, err = id.Parse(debtEntry.String); err != nil {
			return domain.Return{}, corrupt(err)
		}
	}
	var ok bool
	if r.ReturnedAt, ok = clock.ParseTimestamp(returnedAt); !ok {
		return domain.Return{}, corrupt(errs.Internal(CodeCorrupt, "a return's timestamp cannot be read"))
	}
	r.Settlement, r.CostKnown = domain.Settlement(settlement), costKnown == 1
	return r, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// GetByReceiptNo finds a sale by the number printed on its receipt — how a person holding the paper has it.
func (s *Store) GetByReceiptNo(ctx context.Context, receiptNo int64) (domain.Sale, error) {
	var rawID string
	err := s.db.Reader(ctx).QueryRowContext(ctx, `SELECT id FROM sales WHERE receipt_no = ?`, receiptNo).Scan(&rawID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Sale{}, domain.ErrNotFound()
	}
	if err != nil {
		return domain.Sale{}, s.db.Dialect().TranslateError(err)
	}
	saleID, err := id.Parse(rawID)
	if err != nil {
		return domain.Sale{}, corrupt(err)
	}
	return s.Get(ctx, saleID)
}
