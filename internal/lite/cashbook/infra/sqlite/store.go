// Package sqlite is the cash book's table.
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/cashbook/domain"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// CodeCorrupt reports a stored row this build cannot read back.
const CodeCorrupt = "lite.cashbook.corrupt_row"

// Store implements cashbook.Store. Entries are only ever inserted (a test scans this file).
type Store struct {
	db  database.DB
	clk clock.Clock
}

// NewStore builds the store.
func NewStore(db database.DB, clk clock.Clock) *Store { return &Store{db: db, clk: clk} }

const columns = `id, seq, business_date, occurred_at, kind, currency, amount_minor, expected_minor, category, from_drawer,
	reverses_id, fx_rate_id, local_per_usd_nano, note`

type scanner interface{ Scan(dest ...any) error }

func scan(row scanner) (domain.Entry, error) {
	var (
		e                              domain.Entry
		rawID, occurred, kind          string
		expected, fromDrawer, nano     sql.NullInt64
		category, reverses, rate, note sql.NullString
	)
	if err := row.Scan(&rawID, &e.Seq, &e.BusinessDate, &occurred, &kind, &e.Currency, &e.AmountMinor, &expected, &category, &fromDrawer,
		&reverses, &rate, &nano, &note); err != nil {
		return domain.Entry{}, err
	}
	var err error
	if e.ID, err = id.Parse(rawID); err != nil {
		return domain.Entry{}, corrupt(err)
	}
	for _, link := range []struct {
		raw sql.NullString
		dst *id.ID
	}{{reverses, &e.ReversesID}, {rate, &e.RateID}} {
		if link.raw.Valid {
			if *link.dst, err = id.Parse(link.raw.String); err != nil {
				return domain.Entry{}, corrupt(err)
			}
		}
	}
	var ok bool
	if e.OccurredAt, ok = clock.ParseTimestamp(occurred); !ok {
		return domain.Entry{}, errs.Internal(CodeCorrupt, "a cash entry's time does not parse").WithParam("value", occurred)
	}
	e.Kind = domain.Kind(kind)
	e.ExpectedMinor, e.FromDrawer, e.RateNano = expected.Int64, fromDrawer.Int64 == 1, nano.Int64
	e.Category, e.Note = category.String, note.String
	return e, nil
}

func (s *Store) NextSeq(ctx context.Context) (int64, error) {
	var next int64
	err := s.db.Reader(ctx).QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM cash_entries`).Scan(&next)
	return next, s.db.Dialect().TranslateError(err)
}

func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func (s *Store) Insert(ctx context.Context, e domain.Entry) error {
	var expected, category, fromDrawer, rate, nano any
	switch e.Kind {
	case domain.KindCount:
		expected = e.ExpectedMinor
	case domain.KindExpense:
		category, fromDrawer = e.Category, 0
		if e.FromDrawer {
			fromDrawer = 1
		}
	}
	if e.Kind == domain.KindExpense || e.Kind == domain.KindWithdrawal || e.Kind == domain.KindDeposit {
		rate, nano = e.RateID.String(), e.RateNano
	}
	var reverses any
	if e.ReversesID != "" {
		reverses = e.ReversesID.String()
	}
	_, err := s.db.Writer(ctx).ExecContext(ctx, `INSERT INTO cash_entries (`+columns+`, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID.String(), e.Seq, e.BusinessDate, clock.Format(e.OccurredAt), string(e.Kind), e.Currency, e.AmountMinor, expected, category,
		fromDrawer, reverses, rate, nano, nullable(e.Note), clock.Format(s.clk.Now()))
	return s.db.Dialect().TranslateError(err)
}

func (s *Store) one(ctx context.Context, where string, arg any) (domain.Entry, bool, error) {
	e, err := scan(s.db.Reader(ctx).QueryRowContext(ctx, `SELECT `+columns+` FROM cash_entries WHERE `+where, arg))
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

func (s *Store) ReversalOf(ctx context.Context, entryID id.ID) (domain.Entry, bool, error) {
	return s.one(ctx, `reverses_id = ?`, entryID.String())
}

func (s *Store) many(ctx context.Context, q string, args ...any) ([]domain.Entry, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx, q, args...)
	if err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Entry
	for rows.Next() {
		e, err := scan(rows)
		if err != nil {
			return nil, s.translate(err)
		}
		out = append(out, e)
	}
	return out, s.db.Dialect().TranslateError(rows.Err())
}

func (s *Store) Between(ctx context.Context, from, to string) ([]domain.Entry, error) {
	return s.many(ctx, `SELECT `+columns+` FROM cash_entries WHERE business_date BETWEEN ? AND ? ORDER BY seq`, from, to)
}

func (s *Store) CountsBefore(ctx context.Context, businessDate, currency string) ([]domain.Entry, error) {
	return s.many(ctx, `SELECT `+columns+` FROM cash_entries WHERE kind = 'count' AND currency = ? AND business_date < ? ORDER BY seq`, currency, businessDate)
}

func (s *Store) translate(err error) error {
	if _, typed := errs.AsError(err); typed {
		return err
	}
	return s.db.Dialect().TranslateError(err)
}

func corrupt(err error) error {
	return errs.Wrap(err, errs.CategoryInternal, CodeCorrupt, "a cash book row does not read back")
}
