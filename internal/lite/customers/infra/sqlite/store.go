// Package sqlite is the debt book's tables.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/customers"
	"github.com/mizan-erp/mizan/internal/lite/customers/domain"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// CodeCorrupt reports a stored row this build cannot read back.
const CodeCorrupt = "lite.customers.corrupt_row"

// Store implements customers.Store. Debt entries are only ever inserted (a test scans this file); customers are
// updated only in their own fields.
type Store struct {
	db  database.DB
	clk clock.Clock
}

// NewStore builds the store.
func NewStore(db database.DB, clk clock.Clock) *Store { return &Store{db: db, clk: clk} }

type scanner interface{ Scan(dest ...any) error }

const customerColumns = `id, name, phone, note, city, is_active, row_version, created_at`

func scanCustomer(row scanner) (domain.Customer, error) {
	var (
		c              domain.Customer
		rawID, created string
		phone, note    sql.NullString
		city           sql.NullString
		active         int
	)
	if err := row.Scan(&rawID, &c.Name, &phone, &note, &city, &active, &c.RowVersion, &created); err != nil {
		return domain.Customer{}, err
	}
	var err error
	if c.ID, err = id.Parse(rawID); err != nil {
		return domain.Customer{}, corrupt(err)
	}
	var ok bool
	if c.CreatedAt, ok = clock.ParseTimestamp(created); !ok {
		return domain.Customer{}, errs.Internal(CodeCorrupt, "a customer's time does not parse").WithParam("value", created)
	}
	c.Phone, c.Note, c.City, c.Active = phone.String, note.String, city.String, active == 1
	return c, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Store) InsertCustomer(ctx context.Context, c domain.Customer) error {
	now := clock.Format(s.clk.Now())
	_, err := s.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO customers (id, name, name_key, phone, note, city, is_active, row_version, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		c.ID.String(), c.Name, c.NameKey(), nullable(c.Phone), nullable(c.Note), nullable(c.City), boolInt(c.Active), clock.Format(c.CreatedAt), now)
	return s.db.Dialect().TranslateError(err)
}

func (s *Store) UpdateCustomer(ctx context.Context, c domain.Customer) (domain.Customer, error) {
	res, err := s.db.Writer(ctx).ExecContext(ctx, `
		UPDATE customers
		   SET name = ?, name_key = ?, phone = ?, note = ?, city = ?, is_active = ?, row_version = row_version + 1, updated_at = ?
		 WHERE id = ? AND row_version = ?`,
		c.Name, c.NameKey(), nullable(c.Phone), nullable(c.Note), nullable(c.City), boolInt(c.Active), clock.Format(s.clk.Now()), c.ID.String(), c.RowVersion)
	if err != nil {
		return domain.Customer{}, s.db.Dialect().TranslateError(err)
	}
	if err := database.VersionedUpdateResult(res); err != nil {
		return domain.Customer{}, err
	}
	c.RowVersion++
	return c, nil
}

func (s *Store) Customer(ctx context.Context, customerID id.ID) (domain.Customer, error) {
	c, err := scanCustomer(s.db.Reader(ctx).QueryRowContext(ctx, `SELECT `+customerColumns+` FROM customers WHERE id = ?`, customerID.String()))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Customer{}, domain.ErrNotFound()
	}
	if err != nil {
		return domain.Customer{}, s.translate(err)
	}
	return c, nil
}

func (s *Store) CustomerByNameKey(ctx context.Context, key string) (domain.Customer, bool, error) {
	c, err := scanCustomer(s.db.Reader(ctx).QueryRowContext(ctx, `SELECT `+customerColumns+` FROM customers WHERE name_key = ?`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Customer{}, false, nil
	}
	if err != nil {
		return domain.Customer{}, false, s.translate(err)
	}
	return c, true, nil
}

// likeEscaper escapes LIKE's wildcards and the escape character itself.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func (s *Store) Search(ctx context.Context, q customers.Query) ([]domain.Customer, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = customers.DefaultLimit
	}
	rows, err := s.db.Reader(ctx).QueryContext(ctx, `
		SELECT `+customerColumns+` FROM customers
		 WHERE (is_active = 1 OR ? = 1)
		   AND ((? = '' AND ? = '')
		        OR (? <> '' AND name_key LIKE ? ESCAPE '\')
		        OR (? <> '' AND phone LIKE ? ESCAPE '\'))
		 ORDER BY name_key
		 LIMIT ?`,
		boolInt(q.IncludeInactive), q.Text, q.Phone, q.Text, "%"+likeEscaper.Replace(q.Text)+"%", q.Phone, "%"+likeEscaper.Replace(q.Phone)+"%", limit)
	if err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Customer
	for rows.Next() {
		c, err := scanCustomer(rows)
		if err != nil {
			return nil, s.translate(err)
		}
		out = append(out, c)
	}
	return out, s.db.Dialect().TranslateError(rows.Err())
}

const entryColumns = `id, customer_id, currency, seq, business_date, occurred_at, kind, amount_minor, balance_before_minor,
	balance_after_minor, sale_id, reverses_id, fx_rate_id, local_per_usd_nano, cash_note_minor, tendered_currency,
	tendered_minor, change_currency, change_minor, customer_name_snapshot, note`

func scanEntry(row scanner) (domain.Entry, error) {
	var (
		e                                          domain.Entry
		rawID, rawCustomer, occurred, kind         string
		sale, reverses, rate, tenderCur, changeCur sql.NullString
		nano, note, tendered, change               sql.NullInt64
		text                                       sql.NullString
	)
	if err := row.Scan(&rawID, &rawCustomer, &e.Currency, &e.Seq, &e.BusinessDate, &occurred, &kind, &e.AmountMinor,
		&e.BalanceBeforeMinor, &e.BalanceAfterMinor, &sale, &reverses, &rate, &nano, &note, &tenderCur, &tendered, &changeCur,
		&change, &e.CustomerName, &text); err != nil {
		return domain.Entry{}, err
	}
	var err error
	if e.ID, err = id.Parse(rawID); err != nil {
		return domain.Entry{}, corrupt(err)
	}
	if e.CustomerID, err = id.Parse(rawCustomer); err != nil {
		return domain.Entry{}, corrupt(err)
	}
	for _, link := range []struct {
		raw sql.NullString
		dst *id.ID
	}{{sale, &e.SaleID}, {reverses, &e.ReversesID}, {rate, &e.Cash.RateID}} {
		if link.raw.Valid {
			if *link.dst, err = id.Parse(link.raw.String); err != nil {
				return domain.Entry{}, corrupt(err)
			}
		}
	}
	var ok bool
	if e.OccurredAt, ok = clock.ParseTimestamp(occurred); !ok {
		return domain.Entry{}, errs.Internal(CodeCorrupt, "an entry's time does not parse").WithParam("value", occurred)
	}
	e.Kind = domain.Kind(kind)
	e.Cash.RateNano, e.Cash.CashNoteMinor, e.Cash.TenderedMinor, e.Cash.ChangeMinor = nano.Int64, note.Int64, tendered.Int64, change.Int64
	e.Cash.TenderedCurrency, e.Cash.ChangeCurrency, e.Note = tenderCur.String, changeCur.String, text.String
	return e, nil
}

func (s *Store) Newest(ctx context.Context, customerID id.ID, currency string) (domain.Place, error) {
	var p domain.Place
	err := s.db.Reader(ctx).QueryRowContext(ctx, `
		SELECT seq, balance_after_minor FROM debt_entries
		 WHERE customer_id = ? AND currency = ?
		 ORDER BY seq DESC LIMIT 1`, customerID.String(), currency).Scan(&p.Seq, &p.BalanceMinor)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Place{}, nil
	}
	return p, s.db.Dialect().TranslateError(err)
}

func nullableID(v id.ID) any {
	if v == "" {
		return nil
	}
	return v.String()
}

func (s *Store) InsertEntry(ctx context.Context, e domain.Entry) error {
	cash := []any{nil, nil, nil, nil, nil, nil, nil}
	if e.Kind == domain.KindPayment || e.Kind == domain.KindRefund {
		cash = []any{e.Cash.RateID.String(), e.Cash.RateNano, e.Cash.CashNoteMinor, e.Cash.TenderedCurrency, e.Cash.TenderedMinor,
			e.Cash.ChangeCurrency, e.Cash.ChangeMinor}
	}
	args := []any{e.ID.String(), e.CustomerID.String(), e.Currency, e.Seq, e.BusinessDate, clock.Format(e.OccurredAt), string(e.Kind),
		e.AmountMinor, e.BalanceBeforeMinor, e.BalanceAfterMinor, nullableID(e.SaleID), nullableID(e.ReversesID)}
	args = append(args, cash...)
	args = append(args, e.CustomerName, nullable(e.Note), clock.Format(s.clk.Now()))
	_, err := s.db.Writer(ctx).ExecContext(ctx, `INSERT INTO debt_entries (`+entryColumns+`, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, args...)
	return s.db.Dialect().TranslateError(err)
}

func (s *Store) one(ctx context.Context, where string, args ...any) (domain.Entry, bool, error) {
	e, err := scanEntry(s.db.Reader(ctx).QueryRowContext(ctx, `SELECT `+entryColumns+` FROM debt_entries WHERE `+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Entry{}, false, nil
	}
	if err != nil {
		return domain.Entry{}, false, s.translate(err)
	}
	return e, true, nil
}

func (s *Store) Entry(ctx context.Context, entryID id.ID) (domain.Entry, error) {
	e, found, err := s.one(ctx, `id = ?`, entryID.String())
	if err == nil && !found {
		return domain.Entry{}, errs.NotFound(domain.CodeEntryNotFound, "no such entry")
	}
	return e, err
}

func (s *Store) ChargeOf(ctx context.Context, saleID id.ID) (domain.Entry, bool, error) {
	return s.one(ctx, `sale_id = ? AND kind = 'charge'`, saleID.String())
}

func (s *Store) ReversalOf(ctx context.Context, entryID id.ID) (domain.Entry, bool, error) {
	return s.one(ctx, `reverses_id = ?`, entryID.String())
}

func (s *Store) many(ctx context.Context, query string, args ...any) ([]domain.Entry, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, s.translate(err)
		}
		out = append(out, e)
	}
	return out, s.db.Dialect().TranslateError(rows.Err())
}

func (s *Store) Chain(ctx context.Context, customerID id.ID, currency string) ([]domain.Entry, error) {
	return s.many(ctx, `SELECT `+entryColumns+` FROM debt_entries WHERE customer_id = ? AND currency = ? ORDER BY seq`,
		customerID.String(), currency)
}

func (s *Store) ChainCurrencies(ctx context.Context, customerID id.ID) ([]string, error) {
	return s.strings(ctx, `SELECT DISTINCT currency FROM debt_entries WHERE customer_id = ? ORDER BY currency`, customerID.String())
}

// Owing reads each chain's newest place through the place index (L5 §8.1).
func (s *Store) Owing(ctx context.Context) ([]id.ID, error) {
	raw, err := s.strings(ctx, `
		SELECT DISTINCT d.customer_id FROM debt_entries d
		 WHERE d.seq = (SELECT MAX(x.seq) FROM debt_entries x WHERE x.customer_id = d.customer_id AND x.currency = d.currency)
		   AND d.balance_after_minor <> 0
		 ORDER BY d.customer_id`)
	if err != nil {
		return nil, err
	}
	out := make([]id.ID, 0, len(raw))
	for _, r := range raw {
		v, err := id.Parse(r)
		if err != nil {
			return nil, corrupt(err)
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Store) strings(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, s.db.Dialect().TranslateError(err)
		}
		out = append(out, v)
	}
	return out, s.db.Dialect().TranslateError(rows.Err())
}

func (s *Store) Day(ctx context.Context, businessDate string) ([]domain.Entry, error) {
	return s.many(ctx, `SELECT `+entryColumns+` FROM debt_entries WHERE business_date = ? ORDER BY customer_id, currency, seq`, businessDate)
}

func (s *Store) Each(ctx context.Context, fn func(domain.Entry) error) error {
	entries, err := s.many(ctx, `SELECT `+entryColumns+` FROM debt_entries ORDER BY customer_id, currency, seq`)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := fn(e); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) translate(err error) error {
	if _, typed := errs.AsError(err); typed {
		return err
	}
	return s.db.Dialect().TranslateError(err)
}

func corrupt(err error) error {
	return errs.Wrap(err, errs.CategoryInternal, CodeCorrupt, "a debt book row does not read back")
}
