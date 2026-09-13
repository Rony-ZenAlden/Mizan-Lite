// Package sqlite is the catalogue's tables.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/catalog"
	"github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// Store implements catalog.Store.
type Store struct {
	db  database.DB
	clk clock.Clock
}

// NewStore builds the store.
func NewStore(db database.DB, clk clock.Clock) *Store { return &Store{db: db, clk: clk} }

const productColumns = `id, name_ar, name_en, barcode, uom_code, price_currency, sell_price_micro,
	quick_slot, is_active, row_version`

func (s *Store) Units(ctx context.Context) ([]domain.Unit, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx,
		`SELECT code, kind, input_decimals FROM uoms ORDER BY sort_order`)
	if err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Unit
	for rows.Next() {
		var u domain.Unit
		if err := rows.Scan(&u.Code, &u.Kind, &u.InputDecimals); err != nil {
			return nil, s.db.Dialect().TranslateError(err)
		}
		out = append(out, u)
	}
	return out, s.db.Dialect().TranslateError(rows.Err())
}

func (s *Store) Currencies(ctx context.Context) ([]domain.Currency, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx,
		`SELECT code, decimal_places FROM currencies ORDER BY sort_order`)
	if err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Currency
	for rows.Next() {
		var c domain.Currency
		if err := rows.Scan(&c.Code, &c.Decimals); err != nil {
			return nil, s.db.Dialect().TranslateError(err)
		}
		out = append(out, c)
	}
	return out, s.db.Dialect().TranslateError(rows.Err())
}

type scanner interface{ Scan(dest ...any) error }

func scanProduct(row scanner) (domain.Product, error) {
	var (
		p       domain.Product
		rawID   string
		nameEN  sql.NullString
		barcode sql.NullString
		slot    sql.NullInt64
		active  int
	)
	if err := row.Scan(&rawID, &p.NameAR, &nameEN, &barcode, &p.UnitCode, &p.PriceCurrency,
		&p.PriceMicro, &slot, &active, &p.RowVersion); err != nil {
		return domain.Product{}, err
	}
	parsed, err := id.Parse(rawID)
	if err != nil {
		return domain.Product{}, err
	}
	p.ID = parsed
	p.NameEN = nameEN.String
	p.Barcode = barcode.String
	p.QuickSlot = int(slot.Int64)
	p.Active = active == 1
	return p, nil
}

func (s *Store) Get(ctx context.Context, productID id.ID) (domain.Product, error) {
	p, err := scanProduct(s.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+productColumns+` FROM products WHERE id = ?`, productID.String()))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Product{}, domain.ErrNotFound()
	}
	if err != nil {
		return domain.Product{}, s.db.Dialect().TranslateError(err)
	}
	return p, nil
}

func (s *Store) one(ctx context.Context, where string, arg any) (domain.Product, bool, error) {
	p, err := scanProduct(s.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+productColumns+` FROM products WHERE `+where, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Product{}, false, nil
	}
	if err != nil {
		return domain.Product{}, false, s.db.Dialect().TranslateError(err)
	}
	return p, true, nil
}

func (s *Store) ByNameKey(ctx context.Context, key string) (domain.Product, bool, error) {
	return s.one(ctx, `name_key = ?`, key)
}

func (s *Store) ByBarcode(ctx context.Context, barcode string) (domain.Product, bool, error) {
	if barcode == "" {
		return domain.Product{}, false, nil
	}
	return s.one(ctx, `barcode = ?`, barcode)
}

func (s *Store) BySlot(ctx context.Context, slot int) (domain.Product, bool, error) {
	if slot == 0 {
		return domain.Product{}, false, nil
	}
	return s.one(ctx, `quick_slot = ?`, slot)
}

// likeEscaper escapes the characters LIKE treats as wildcards, and the escape character itself. Without it
// a product named "زيت 50% خصم" turns a search for "50%" into "anything containing 50".
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// Search uses LIKE … ESCAPE, which every engine in the portability contract accepts; full-text search is
// engine-specific, and a shop's catalogue is hundreds of rows.
func (s *Store) Search(ctx context.Context, q catalog.Query) ([]domain.Product, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = catalog.DefaultLimit
	}
	includeInactive := 0
	if q.IncludeInactive {
		includeInactive = 1
	}
	rows, err := s.db.Reader(ctx).QueryContext(ctx, `
		SELECT `+productColumns+` FROM products
		 WHERE (is_active = 1 OR ? = 1)
		   AND (? = '' OR search_text LIKE ? ESCAPE '\')
		 ORDER BY CASE WHEN ? <> '' AND barcode = ? THEN 0 ELSE 1 END, name_key
		 LIMIT ?`,
		includeInactive, q.Text, "%"+likeEscaper.Replace(q.Text)+"%", q.Barcode, q.Barcode, limit)
	if err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, s.db.Dialect().TranslateError(err)
		}
		out = append(out, p)
	}
	return out, s.db.Dialect().TranslateError(rows.Err())
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableSlot(slot int) any {
	if slot == 0 {
		return nil
	}
	return slot
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Store) Insert(ctx context.Context, p domain.Product) error {
	now := clock.Format(s.clk.Now())
	_, err := s.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO products (id, name_ar, name_en, name_key, search_text, barcode, uom_code,
		                      price_currency, sell_price_micro, quick_slot, is_active, row_version,
		                      created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID.String(), p.NameAR, nullable(p.NameEN), p.NameKey(), p.SearchText(), nullable(p.Barcode),
		p.UnitCode, p.PriceCurrency, p.PriceMicro, nullableSlot(p.QuickSlot), boolInt(p.Active),
		p.RowVersion, now, now)
	return s.db.Dialect().TranslateError(err)
}

// Update writes every mutable column at once, guarded by the row version. The unit is not among them
// (catalog.UpdateInput explains why).
func (s *Store) Update(ctx context.Context, p domain.Product) (domain.Product, error) {
	res, err := s.db.Writer(ctx).ExecContext(ctx, `
		UPDATE products
		   SET name_ar = ?, name_en = ?, name_key = ?, search_text = ?, barcode = ?,
		       price_currency = ?, sell_price_micro = ?, quick_slot = ?, is_active = ?,
		       row_version = row_version + 1, updated_at = ?
		 WHERE id = ? AND row_version = ?`,
		p.NameAR, nullable(p.NameEN), p.NameKey(), p.SearchText(), nullable(p.Barcode),
		p.PriceCurrency, p.PriceMicro, nullableSlot(p.QuickSlot), boolInt(p.Active),
		clock.Format(s.clk.Now()), p.ID.String(), p.RowVersion)
	if err != nil {
		return domain.Product{}, s.db.Dialect().TranslateError(err)
	}
	if err := database.VersionedUpdateResult(res); err != nil {
		return domain.Product{}, err
	}
	p.RowVersion++
	return p, nil
}
