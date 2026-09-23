// Package sqlite is the sales tables. A sale is updated only from posted to voided, and its lines never:
// TestSalesAreUpdatedOnlyToVoidAndLinesNever reads this file.
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/sales/domain"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// CodeCorrupt reports a stored row this build cannot read back.
const CodeCorrupt = "lite.sales.corrupt_row"

// Store implements sales.Store.
type Store struct {
	db  database.DB
	clk clock.Clock
}

// NewStore builds the store.
func NewStore(db database.DB, clk clock.Clock) *Store { return &Store{db: db, clk: clk} }

const saleColumns = `id, receipt_no, business_date, sold_at, status, payment, local_currency, fx_rate_id, local_per_usd_nano,
	rate_recorded_at, settlement_currency, lines_local_minor, lines_usd_minor, discount_local_minor, discount_usd_minor,
	cash_increment_minor, rounding_minor, total_minor, tendered_currency, tendered_minor, change_currency, change_minor,
	cost_usd_minor, shop_name_snapshot, voided_at, void_business_date, void_reason, row_version`

const lineColumns = `id, sale_id, line_no, product_id, name_ar_snapshot, name_en_snapshot, unit_code_snapshot, quantity_micro,
	price_currency, unit_price_micro, gross_local_minor, gross_usd_minor, discount_percent_micro, discount_local_minor,
	discount_usd_minor, unit_cost_usd_micro, cost_known, cost_usd_minor, cost_local_minor, open_price, units_per_carton_micro`

type scanner interface{ Scan(dest ...any) error }

func scanSale(row scanner) (domain.Sale, error) {
	var (
		s                                       domain.Sale
		rawID, rawRate, sold, rateAt, status, p string
		voidedAt, voidDate, voidReason          sql.NullString
	)
	if err := row.Scan(&rawID, &s.ReceiptNo, &s.BusinessDate, &sold, &status, &p, &s.LocalCurrency, &rawRate, &s.RateNano,
		&rateAt, &s.SettlementCurrency, &s.LinesLocalMinor, &s.LinesUSDMinor, &s.DiscountLocalMinor, &s.DiscountUSDMinor,
		&s.CashNoteMinor, &s.RoundingMinor, &s.TotalMinor, &s.TenderedCurrency, &s.TenderedMinor, &s.ChangeCurrency,
		&s.ChangeMinor, &s.CostUSDMinor, &s.ShopName, &voidedAt, &voidDate, &voidReason, &s.RowVersion); err != nil {
		return domain.Sale{}, err
	}
	var err error
	if s.ID, err = id.Parse(rawID); err != nil {
		return domain.Sale{}, corrupt(err)
	}
	if s.RateID, err = id.Parse(rawRate); err != nil {
		return domain.Sale{}, corrupt(err)
	}
	var ok bool
	if s.SoldAt, ok = clock.ParseTimestamp(sold); !ok {
		return domain.Sale{}, badTime(sold)
	}
	if s.RateRecordedAt, ok = clock.ParseTimestamp(rateAt); !ok {
		return domain.Sale{}, badTime(rateAt)
	}
	if voidedAt.Valid {
		if s.VoidedAt, ok = clock.ParseTimestamp(voidedAt.String); !ok {
			return domain.Sale{}, badTime(voidedAt.String)
		}
	}
	s.Status, s.Payment = domain.Status(status), domain.Payment(p)
	s.VoidBusinessDate, s.VoidReason = voidDate.String, voidReason.String
	return s, nil
}

func scanLine(row scanner) (domain.Line, id.ID, error) {
	var (
		l                       domain.Line
		rawID, rawSale, rawProd string
		nameEN                  sql.NullString
		costKnown, open         int
		carton                  sql.NullInt64
	)
	if err := row.Scan(&rawID, &rawSale, &l.LineNo, &rawProd, &l.NameAR, &nameEN, &l.UnitCode, &l.QuantityMicro, &l.PriceCurrency,
		&l.UnitPriceMicro, &l.GrossLocalMinor, &l.GrossUSDMinor, &l.DiscountPercentMicro, &l.DiscountLocalMinor,
		&l.DiscountUSDMinor, &l.UnitCostMicro, &costKnown, &l.CostUSDMinor, &l.CostLocalMinor, &open, &carton); err != nil {
		return domain.Line{}, "", err
	}
	var err error
	if l.ID, err = id.Parse(rawID); err != nil {
		return domain.Line{}, "", corrupt(err)
	}
	saleID, err := id.Parse(rawSale)
	if err != nil {
		return domain.Line{}, "", corrupt(err)
	}
	if l.ProductID, err = id.Parse(rawProd); err != nil {
		return domain.Line{}, "", corrupt(err)
	}
	l.NameEN, l.CostKnown, l.OpenPrice, l.UnitsPerCartonMicro = nameEN.String, costKnown == 1, open == 1, carton.Int64
	return l, saleID, nil
}

func (s *Store) NextReceiptNo(ctx context.Context) (int64, error) {
	var next int64
	err := s.db.Reader(ctx).QueryRowContext(ctx, `SELECT COALESCE(MAX(receipt_no), 0) + 1 FROM sales`).Scan(&next)
	return next, s.db.Dialect().TranslateError(err)
}

func (s *Store) Insert(ctx context.Context, sale domain.Sale) error {
	w := s.db.Writer(ctx)
	now := clock.Format(s.clk.Now())
	_, err := w.ExecContext(ctx, `INSERT INTO sales (`+saleColumns+`, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)`,
		sale.ID.String(), sale.ReceiptNo, sale.BusinessDate, clock.Format(sale.SoldAt), string(domain.StatusPosted),
		string(sale.Payment), sale.LocalCurrency, sale.RateID.String(), sale.RateNano, clock.Format(sale.RateRecordedAt),
		sale.SettlementCurrency, sale.LinesLocalMinor, sale.LinesUSDMinor, sale.DiscountLocalMinor, sale.DiscountUSDMinor,
		sale.CashNoteMinor, sale.RoundingMinor, sale.TotalMinor, sale.TenderedCurrency, sale.TenderedMinor, sale.ChangeCurrency,
		sale.ChangeMinor, sale.CostUSDMinor, sale.ShopName, nil, nil, nil, now)
	if err != nil {
		return s.db.Dialect().TranslateError(err)
	}
	for _, l := range sale.Lines {
		costKnown := 0
		if l.CostKnown {
			costKnown = 1
		}
		var nameEN any
		if l.NameEN != "" {
			nameEN = l.NameEN
		}
		open := 0
		if l.OpenPrice {
			open = 1
		}
		var carton any
		if l.UnitsPerCartonMicro > 0 {
			carton = l.UnitsPerCartonMicro
		}
		if _, err = w.ExecContext(ctx, `INSERT INTO sale_lines (`+lineColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			l.ID.String(), sale.ID.String(), l.LineNo, l.ProductID.String(), l.NameAR, nameEN, l.UnitCode, l.QuantityMicro,
			l.PriceCurrency, l.UnitPriceMicro, l.GrossLocalMinor, l.GrossUSDMinor, l.DiscountPercentMicro, l.DiscountLocalMinor,
			l.DiscountUSDMinor, l.UnitCostMicro, costKnown, l.CostUSDMinor, l.CostLocalMinor, open, carton); err != nil {
			return s.db.Dialect().TranslateError(err)
		}
	}
	return nil
}

func (s *Store) Get(ctx context.Context, saleID id.ID) (domain.Sale, error) {
	sale, err := scanSale(s.db.Reader(ctx).QueryRowContext(ctx, `SELECT `+saleColumns+` FROM sales WHERE id = ?`, saleID.String()))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Sale{}, domain.ErrNotFound()
	}
	if err != nil {
		return domain.Sale{}, s.db.Dialect().TranslateError(err)
	}
	lines, err := s.lines(ctx, `WHERE sale_id = ?`, saleID.String())
	if err != nil {
		return domain.Sale{}, err
	}
	sale.Lines = lines[saleID]
	return sale, nil
}

func (s *Store) Day(ctx context.Context, businessDate string) ([]domain.Sale, error) {
	return s.sales(ctx, `WHERE business_date = ? OR void_business_date = ?`, businessDate, businessDate)
}

func (s *Store) Range(ctx context.Context, from, to string) ([]domain.Sale, error) {
	return s.sales(ctx, `WHERE business_date BETWEEN ? AND ? OR void_business_date BETWEEN ? AND ?`, from, to, from, to)
}

func (s *Store) Each(ctx context.Context, fn func(domain.Sale) error) error {
	all, err := s.sales(ctx, "")
	if err != nil {
		return err
	}
	for _, sale := range all {
		if err := fn(sale); err != nil {
			return err
		}
	}
	return nil
}

// sales reads the sales a condition selects, with their lines, by receipt number.
func (s *Store) sales(ctx context.Context, where string, args ...any) ([]domain.Sale, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx, `SELECT `+saleColumns+` FROM sales `+where+` ORDER BY receipt_no`, args...)
	if err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	out, err := func() ([]domain.Sale, error) {
		// Closed before the lines are read: SQLite's reader pool may hold one connection, and the lines need it.
		defer func() { _ = rows.Close() }()
		var read []domain.Sale
		for rows.Next() {
			sale, scanErr := scanSale(rows)
			if scanErr != nil {
				return nil, scanErr
			}
			read = append(read, sale)
		}
		return read, rows.Err()
	}()
	if err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	lineWhere := `WHERE sale_id IN (SELECT id FROM sales ` + where + `)`
	lines, err := s.lines(ctx, lineWhere, args...)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Lines = lines[out[i].ID]
	}
	return out, nil
}

func (s *Store) lines(ctx context.Context, where string, args ...any) (map[id.ID][]domain.Line, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx, `SELECT `+lineColumns+` FROM sale_lines `+where+` ORDER BY sale_id, line_no`, args...)
	if err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[id.ID][]domain.Line{}
	for rows.Next() {
		l, saleID, err := scanLine(rows)
		if err != nil {
			return nil, s.db.Dialect().TranslateError(err)
		}
		out[saleID] = append(out[saleID], l)
	}
	return out, s.db.Dialect().TranslateError(rows.Err())
}

// Void is the one UPDATE of a sale: posted to voided, guarded by its version.
func (s *Store) Void(ctx context.Context, sale domain.Sale) (domain.Sale, error) {
	res, err := s.db.Writer(ctx).ExecContext(ctx, `
		UPDATE sales SET status = 'voided', voided_at = ?, void_business_date = ?, void_reason = ?, row_version = row_version + 1
		 WHERE id = ? AND row_version = ? AND status = 'posted'`,
		clock.Format(sale.VoidedAt), sale.VoidBusinessDate, sale.VoidReason, sale.ID.String(), sale.RowVersion)
	if err != nil {
		return domain.Sale{}, s.db.Dialect().TranslateError(err)
	}
	if err := database.VersionedUpdateResult(res); err != nil {
		return domain.Sale{}, err
	}
	sale.RowVersion++
	return sale, nil
}

func corrupt(err error) error {
	return errs.Wrap(err, errs.CategoryInternal, CodeCorrupt, "a sale row does not read back")
}

func badTime(v string) error {
	return errs.Internal(CodeCorrupt, "a sale's time does not parse").WithParam("value", v)
}
