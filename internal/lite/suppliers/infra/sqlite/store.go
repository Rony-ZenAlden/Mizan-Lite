// Package sqlite is the suppliers, purchases and payables tables (migration 0012 §4).
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/suppliers"
	"github.com/mizan-erp/mizan/internal/lite/suppliers/domain"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// Store implements suppliers.Store.
type Store struct {
	db  database.DB
	clk clock.Clock
}

// NewStore builds the store.
func NewStore(db database.DB, clk clock.Clock) *Store { return &Store{db: db, clk: clk} }

var _ suppliers.Store = (*Store)(nil)

type scanner interface{ Scan(dest ...any) error }

func corrupt(err error) error {
	return errs.Wrap(err, errs.CategoryInternal, domain.CodeCorrupt, "a supplier row does not read back")
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableID(v id.ID) any { return nullable(v.String()) }

func nullableInt(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func parseTime(raw string) (time.Time, error) {
	t, ok := clock.ParseTimestamp(raw)
	if !ok {
		return time.Time{}, errs.Internal(domain.CodeCorrupt, "a supplier row's time does not parse").WithParam("value", raw)
	}
	return t, nil
}

// ─── Suppliers ─────────────────────────────────────────────────────────────────────────────────────────────────────

const supplierColumns = `id, name, phone, city, note, is_active, row_version, created_at`

func scanSupplier(row scanner) (domain.Supplier, error) {
	var (
		s                 domain.Supplier
		rawID, created    string
		phone, city, note sql.NullString
		active            int
	)
	if err := row.Scan(&rawID, &s.Name, &phone, &city, &note, &active, &s.RowVersion, &created); err != nil {
		return domain.Supplier{}, err
	}
	var err error
	if s.ID, err = id.Parse(rawID); err != nil {
		return domain.Supplier{}, corrupt(err)
	}
	t, err := parseTime(created)
	if err != nil {
		return domain.Supplier{}, err
	}
	s.CreatedAt = t
	s.Phone, s.City, s.Note, s.Active = phone.String, city.String, note.String, active == 1
	return s, nil
}

func (s *Store) InsertSupplier(ctx context.Context, sup domain.Supplier) error {
	now := clock.Format(s.clk.Now())
	_, err := s.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO suppliers (id, name, name_key, phone, city, note, is_active, row_version, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		sup.ID.String(), sup.Name, sup.NameKey(), nullable(sup.Phone), nullable(sup.City), nullable(sup.Note), boolInt(sup.Active),
		clock.Format(sup.CreatedAt), now)
	return s.db.Dialect().TranslateError(err)
}

func (s *Store) UpdateSupplier(ctx context.Context, sup domain.Supplier) (domain.Supplier, error) {
	res, err := s.db.Writer(ctx).ExecContext(ctx, `
		UPDATE suppliers
		   SET name = ?, name_key = ?, phone = ?, city = ?, note = ?, is_active = ?, row_version = row_version + 1, updated_at = ?
		 WHERE id = ? AND row_version = ?`,
		sup.Name, sup.NameKey(), nullable(sup.Phone), nullable(sup.City), nullable(sup.Note), boolInt(sup.Active),
		clock.Format(s.clk.Now()), sup.ID.String(), sup.RowVersion)
	if err != nil {
		return domain.Supplier{}, s.db.Dialect().TranslateError(err)
	}
	if err := database.VersionedUpdateResult(res); err != nil {
		return domain.Supplier{}, err
	}
	sup.RowVersion++
	return sup, nil
}

func (s *Store) Supplier(ctx context.Context, supplierID id.ID) (domain.Supplier, error) {
	sup, err := scanSupplier(s.db.Reader(ctx).QueryRowContext(ctx, `SELECT `+supplierColumns+` FROM suppliers WHERE id = ?`, supplierID.String()))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Supplier{}, domain.ErrNotFound()
	}
	if err != nil {
		return domain.Supplier{}, s.db.Dialect().TranslateError(err)
	}
	return sup, nil
}

func (s *Store) SupplierByNameKey(ctx context.Context, key string) (domain.Supplier, bool, error) {
	sup, err := scanSupplier(s.db.Reader(ctx).QueryRowContext(ctx, `SELECT `+supplierColumns+` FROM suppliers WHERE name_key = ?`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Supplier{}, false, nil
	}
	if err != nil {
		return domain.Supplier{}, false, s.db.Dialect().TranslateError(err)
	}
	return sup, true, nil
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func (s *Store) Search(ctx context.Context, q suppliers.Query) ([]domain.Supplier, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = suppliers.DefaultLimit
	}
	rows, err := s.db.Reader(ctx).QueryContext(ctx, `
		SELECT `+supplierColumns+` FROM suppliers
		 WHERE (? = 1 OR is_active = 1)
		   AND ((? = '' AND ? = '') OR (? <> '' AND name_key LIKE ? ESCAPE '\') OR (? <> '' AND phone LIKE ? ESCAPE '\'))
		 ORDER BY name_key
		 LIMIT ?`,
		boolInt(q.IncludeInactive), q.Text, q.Phone, q.Text, "%"+likeEscaper.Replace(q.Text)+"%", q.Phone, "%"+likeEscaper.Replace(q.Phone)+"%", limit)
	if err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	defer func() { _ = rows.Close() }()
	out := []domain.Supplier{}
	for rows.Next() {
		sup, err := scanSupplier(rows)
		if err != nil {
			return nil, s.db.Dialect().TranslateError(err)
		}
		out = append(out, sup)
	}
	return out, s.db.Dialect().TranslateError(rows.Err())
}

// ─── The payables book ─────────────────────────────────────────────────────────────────────────────────────────────

const entryColumns = `id, supplier_id, currency, seq, business_date, occurred_at, kind, amount_minor, balance_before_minor,
	balance_after_minor, purchase_id, reverses_id, cash_source, supplier_name_snapshot, note`

func scanEntry(row scanner) (domain.Entry, error) {
	var (
		e                                  domain.Entry
		rawID, rawSupplier, occurred, kind string
		purchase, reverses, source, note   sql.NullString
	)
	if err := row.Scan(&rawID, &rawSupplier, &e.Currency, &e.Seq, &e.BusinessDate, &occurred, &kind, &e.AmountMinor,
		&e.BalanceBeforeMinor, &e.BalanceAfterMinor, &purchase, &reverses, &source, &e.SupplierName, &note); err != nil {
		return domain.Entry{}, err
	}
	var err error
	if e.ID, err = id.Parse(rawID); err != nil {
		return domain.Entry{}, corrupt(err)
	}
	if e.SupplierID, err = id.Parse(rawSupplier); err != nil {
		return domain.Entry{}, corrupt(err)
	}
	if purchase.Valid {
		if e.PurchaseID, err = id.Parse(purchase.String); err != nil {
			return domain.Entry{}, corrupt(err)
		}
	}
	if reverses.Valid {
		if e.ReversesID, err = id.Parse(reverses.String); err != nil {
			return domain.Entry{}, corrupt(err)
		}
	}
	t, err := parseTime(occurred)
	if err != nil {
		return domain.Entry{}, err
	}
	e.OccurredAt, e.Kind, e.Source, e.Note = t, domain.Kind(kind), domain.CashSource(source.String), note.String
	return e, nil
}

func (s *Store) Newest(ctx context.Context, supplierID id.ID, currency string) (domain.Place, error) {
	var p domain.Place
	err := s.db.Reader(ctx).QueryRowContext(ctx, `
		SELECT seq, balance_after_minor FROM supplier_entries
		 WHERE supplier_id = ? AND currency = ? ORDER BY seq DESC LIMIT 1`, supplierID.String(), currency).Scan(&p.Seq, &p.BalanceMinor)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Place{}, nil
	}
	return p, s.db.Dialect().TranslateError(err)
}

func (s *Store) InsertEntry(ctx context.Context, e domain.Entry) error {
	_, err := s.db.Writer(ctx).ExecContext(ctx, `INSERT INTO supplier_entries (`+entryColumns+`, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID.String(), e.SupplierID.String(), e.Currency, e.Seq, e.BusinessDate, clock.Format(e.OccurredAt), string(e.Kind),
		e.AmountMinor, e.BalanceBeforeMinor, e.BalanceAfterMinor, nullableID(e.PurchaseID), nullableID(e.ReversesID),
		nullable(string(e.Source)), e.SupplierName, nullable(e.Note), clock.Format(s.clk.Now()))
	return s.db.Dialect().TranslateError(err)
}

func (s *Store) entry(ctx context.Context, where string, args ...any) (domain.Entry, bool, error) {
	e, err := scanEntry(s.db.Reader(ctx).QueryRowContext(ctx, `SELECT `+entryColumns+` FROM supplier_entries WHERE `+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Entry{}, false, nil
	}
	if err != nil {
		return domain.Entry{}, false, s.db.Dialect().TranslateError(err)
	}
	return e, true, nil
}

func (s *Store) Entry(ctx context.Context, entryID id.ID) (domain.Entry, error) {
	e, found, err := s.entry(ctx, `id = ?`, entryID.String())
	if err != nil {
		return domain.Entry{}, err
	}
	if !found {
		return domain.Entry{}, errs.NotFound(domain.CodeEntryNotFound, "no such entry")
	}
	return e, nil
}

func (s *Store) ReversalOf(ctx context.Context, entryID id.ID) (domain.Entry, bool, error) {
	return s.entry(ctx, `reverses_id = ?`, entryID.String())
}

func (s *Store) PurchaseEntry(ctx context.Context, purchaseID id.ID) (domain.Entry, bool, error) {
	return s.entry(ctx, `purchase_id = ? AND kind = 'purchase'`, purchaseID.String())
}

func (s *Store) entries(ctx context.Context, query string, args ...any) ([]domain.Entry, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	defer func() { _ = rows.Close() }()
	out := []domain.Entry{}
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, s.db.Dialect().TranslateError(err)
		}
		out = append(out, e)
	}
	return out, s.db.Dialect().TranslateError(rows.Err())
}

func (s *Store) Chain(ctx context.Context, supplierID id.ID, currency string) ([]domain.Entry, error) {
	return s.entries(ctx, `SELECT `+entryColumns+` FROM supplier_entries WHERE supplier_id = ? AND currency = ? ORDER BY seq`,
		supplierID.String(), currency)
}

func (s *Store) Between(ctx context.Context, from, to string) ([]domain.Entry, error) {
	return s.entries(ctx, `SELECT `+entryColumns+` FROM supplier_entries WHERE business_date BETWEEN ? AND ? ORDER BY occurred_at, seq, id`, from, to)
}

func (s *Store) Balances(ctx context.Context) (map[id.ID]map[string]int64, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx, `
		SELECT e.supplier_id, e.currency, e.balance_after_minor FROM supplier_entries e
		 WHERE e.seq = (SELECT MAX(x.seq) FROM supplier_entries x WHERE x.supplier_id = e.supplier_id AND x.currency = e.currency)`)
	if err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[id.ID]map[string]int64{}
	for rows.Next() {
		var raw, currency string
		var balance int64
		if err := rows.Scan(&raw, &currency, &balance); err != nil {
			return nil, s.db.Dialect().TranslateError(err)
		}
		supplierID, err := id.Parse(raw)
		if err != nil {
			return nil, corrupt(err)
		}
		if out[supplierID] == nil {
			out[supplierID] = map[string]int64{}
		}
		out[supplierID][currency] = balance
	}
	return out, s.db.Dialect().TranslateError(rows.Err())
}

// ─── Purchases ─────────────────────────────────────────────────────────────────────────────────────────────────────

const purchaseColumns = `id, purchase_no, supplier_id, supplier_name_snapshot, business_date, occurred_at, currency,
	local_per_usd_nano, supplier_ref, gross_minor, line_discount_minor, invoice_discount_minor, paid_now_minor, paid_from,
	status, voided_at, void_reason, note, row_version`

const lineColumns = `id, purchase_id, line_no, product_id, name_ar_snapshot, name_en_snapshot, unit_code_snapshot,
	quantity_micro, damaged_micro, unit_cost_micro, discount_percent_micro, gross_minor, line_discount_minor,
	invoice_share_minor, due_minor, stock_ledger_id`

func (s *Store) NextPurchaseNo(ctx context.Context) (int64, error) {
	var next int64
	err := s.db.Reader(ctx).QueryRowContext(ctx, `SELECT COALESCE(MAX(purchase_no), 0) + 1 FROM purchases`).Scan(&next)
	return next, s.db.Dialect().TranslateError(err)
}

func (s *Store) InsertPurchase(ctx context.Context, p domain.Purchase) error {
	w := s.db.Writer(ctx)
	now := clock.Format(s.clk.Now())
	if _, err := w.ExecContext(ctx, `INSERT INTO purchases (`+purchaseColumns+`, due_minor, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		p.ID.String(), p.PurchaseNo, p.SupplierID.String(), p.SupplierName, p.BusinessDate, clock.Format(p.OccurredAt), p.Currency,
		nullableInt(p.RateNano), nullable(p.SupplierRef), p.GrossMinor, p.LineDiscountMinor, p.InvoiceDiscountMinor, p.PaidNowMinor,
		nullable(string(p.PaidFrom)), string(domain.StatusPosted), nil, nil, nullable(p.Note), p.DueMinor(), now); err != nil {
		return s.db.Dialect().TranslateError(err)
	}
	for _, l := range p.Lines {
		if _, err := w.ExecContext(ctx, `INSERT INTO purchase_lines (`+lineColumns+`)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			l.ID.String(), p.ID.String(), l.LineNo, l.ProductID.String(), l.NameAR, nullable(l.NameEN), l.UnitCode,
			l.QuantityMicro, l.DamagedMicro, l.UnitCostMicro, l.DiscountPercentMicro, l.GrossMinor, l.LineDiscountMinor,
			l.InvoiceShareMinor, l.DueMinor(), nullableID(l.StockLedgerID)); err != nil {
			return s.db.Dialect().TranslateError(err)
		}
	}
	return nil
}

func scanPurchase(row scanner) (domain.Purchase, error) {
	var (
		p                                         domain.Purchase
		rawID, rawSupplier, occurred, status      string
		rate                                      sql.NullInt64
		ref, paidFrom, voidedAt, voidReason, note sql.NullString
	)
	if err := row.Scan(&rawID, &p.PurchaseNo, &rawSupplier, &p.SupplierName, &p.BusinessDate, &occurred, &p.Currency, &rate, &ref,
		&p.GrossMinor, &p.LineDiscountMinor, &p.InvoiceDiscountMinor, &p.PaidNowMinor, &paidFrom, &status, &voidedAt, &voidReason,
		&note, &p.RowVersion); err != nil {
		return domain.Purchase{}, err
	}
	var err error
	if p.ID, err = id.Parse(rawID); err != nil {
		return domain.Purchase{}, corrupt(err)
	}
	if p.SupplierID, err = id.Parse(rawSupplier); err != nil {
		return domain.Purchase{}, corrupt(err)
	}
	t, err := parseTime(occurred)
	if err != nil {
		return domain.Purchase{}, err
	}
	p.OccurredAt = t
	if voidedAt.Valid {
		v, err := parseTime(voidedAt.String)
		if err != nil {
			return domain.Purchase{}, err
		}
		p.VoidedAt = v
	}
	p.RateNano, p.SupplierRef, p.PaidFrom = rate.Int64, ref.String, domain.CashSource(paidFrom.String)
	p.Status, p.VoidReason, p.Note = domain.Status(status), voidReason.String, note.String
	return p, nil
}

func scanLine(row scanner) (domain.Line, error) {
	var (
		l                              domain.Line
		rawID, rawPurchase, rawProduct string
		nameEN, ledger                 sql.NullString
		due                            int64
	)
	if err := row.Scan(&rawID, &rawPurchase, &l.LineNo, &rawProduct, &l.NameAR, &nameEN, &l.UnitCode, &l.QuantityMicro, &l.DamagedMicro,
		&l.UnitCostMicro, &l.DiscountPercentMicro, &l.GrossMinor, &l.LineDiscountMinor, &l.InvoiceShareMinor, &due, &ledger); err != nil {
		return domain.Line{}, err
	}
	var err error
	if l.ID, err = id.Parse(rawID); err != nil {
		return domain.Line{}, corrupt(err)
	}
	if l.ProductID, err = id.Parse(rawProduct); err != nil {
		return domain.Line{}, corrupt(err)
	}
	if ledger.Valid {
		if l.StockLedgerID, err = id.Parse(ledger.String); err != nil {
			return domain.Line{}, corrupt(err)
		}
	}
	l.NameEN = nameEN.String
	return l, nil
}

func (s *Store) Purchase(ctx context.Context, purchaseID id.ID) (domain.Purchase, error) {
	p, err := scanPurchase(s.db.Reader(ctx).QueryRowContext(ctx, `SELECT `+purchaseColumns+` FROM purchases WHERE id = ?`, purchaseID.String()))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Purchase{}, domain.ErrPurchaseNotFound()
	}
	if err != nil {
		return domain.Purchase{}, s.db.Dialect().TranslateError(err)
	}
	if p.Lines, err = s.lines(ctx, p.ID); err != nil {
		return domain.Purchase{}, err
	}
	return p, nil
}

func (s *Store) lines(ctx context.Context, purchaseID id.ID) ([]domain.Line, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx, `SELECT `+lineColumns+` FROM purchase_lines WHERE purchase_id = ? ORDER BY line_no`, purchaseID.String())
	if err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	defer func() { _ = rows.Close() }()
	out := []domain.Line{}
	for rows.Next() {
		l, err := scanLine(rows)
		if err != nil {
			return nil, s.db.Dialect().TranslateError(err)
		}
		out = append(out, l)
	}
	return out, s.db.Dialect().TranslateError(rows.Err())
}

func (s *Store) Purchases(ctx context.Context, q suppliers.PurchaseQuery) ([]domain.Purchase, error) {
	var where []string
	var args []any
	if q.SupplierID != "" {
		where, args = append(where, `supplier_id = ?`), append(args, q.SupplierID.String())
	}
	if q.From != "" {
		where, args = append(where, `business_date >= ?`), append(args, q.From)
	}
	if q.To != "" {
		where, args = append(where, `business_date <= ?`), append(args, q.To)
	}
	query := `SELECT ` + purchaseColumns + ` FROM purchases`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, ` AND `)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = suppliers.DefaultLimit
	}
	rows, err := s.db.Reader(ctx).QueryContext(ctx, query+` ORDER BY purchase_no DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	out, err := s.readPurchases(rows)
	if err != nil {
		return nil, err
	}
	// Lines after the list is read: one connection reads at a time, and a query inside an open cursor would wait on it.
	for i := range out {
		if out[i].Lines, err = s.lines(ctx, out[i].ID); err != nil {
			return nil, err
		}
	}
	if out == nil {
		out = []domain.Purchase{}
	}
	return out, nil
}

func (s *Store) VoidPurchase(ctx context.Context, p domain.Purchase) error {
	res, err := s.db.Writer(ctx).ExecContext(ctx, `
		UPDATE purchases SET status = ?, voided_at = ?, void_reason = ?, row_version = row_version + 1
		 WHERE id = ? AND row_version = ? AND status = 'posted'`,
		string(domain.StatusVoided), clock.Format(p.VoidedAt), p.VoidReason, p.ID.String(), p.RowVersion)
	if err != nil {
		return s.db.Dialect().TranslateError(err)
	}
	return database.VersionedUpdateResult(res)
}

// readPurchases reads a list of purchases and closes the cursor, so their lines can be read after it — one connection
// reads at a time, and a query inside an open cursor would wait on it.
func (s *Store) readPurchases(rows *sql.Rows) ([]domain.Purchase, error) {
	defer func() { _ = rows.Close() }()
	var out []domain.Purchase
	for rows.Next() {
		p, err := scanPurchase(rows)
		if err != nil {
			return nil, s.db.Dialect().TranslateError(err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	return out, nil
}
