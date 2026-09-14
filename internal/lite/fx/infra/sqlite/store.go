// Package sqlite is the exchange-rate tables: fx_rates and fx_fetches, both insert-only.
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/fx/domain"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// CodeCorrupt reports a stored row this build cannot read back.
const CodeCorrupt = "lite.fx.corrupt_row"

// Store implements fx.Store. Both tables are INSERT-only: TestRatesAreInsertOnly reads this file.
type Store struct{ db database.DB }

// NewStore builds the store.
func NewStore(db database.DB) *Store { return &Store{db: db} }

const rateColumns = `id, local_currency, seq, local_per_usd_nano, source, fetch_id, business_date, recorded_at, note`

type scanner interface{ Scan(dest ...any) error }

func scanRate(row scanner) (domain.Rate, error) {
	var (
		r                 domain.Rate
		rawID, source, at string
		fetchID, note     sql.NullString
	)
	if err := row.Scan(&rawID, &r.LocalCurrency, &r.Seq, &r.Nano, &source, &fetchID, &r.BusinessDate, &at, &note); err != nil {
		return domain.Rate{}, err
	}
	var err error
	if r.ID, err = id.Parse(rawID); err != nil {
		return domain.Rate{}, corrupt(err)
	}
	if r.FetchID, err = optionalID(fetchID); err != nil {
		return domain.Rate{}, corrupt(err)
	}
	var ok bool
	if r.RecordedAt, ok = clock.ParseTimestamp(at); !ok {
		return domain.Rate{}, errs.Internal(CodeCorrupt, "a rate's time does not parse").WithParam("value", at)
	}
	r.Source = domain.Source(source)
	r.Note = note.String
	return r, nil
}

func (s *Store) InForce(ctx context.Context, local string) (domain.Rate, bool, error) {
	r, err := scanRate(s.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+rateColumns+` FROM fx_rates WHERE local_currency = ? ORDER BY seq DESC LIMIT 1`, local))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Rate{}, false, nil
	}
	if err != nil {
		return domain.Rate{}, false, s.db.Dialect().TranslateError(err)
	}
	return r, true, nil
}

func (s *Store) Rates(ctx context.Context, local string, limit int) ([]domain.Rate, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx,
		`SELECT `+rateColumns+` FROM fx_rates WHERE local_currency = ? ORDER BY seq DESC LIMIT ?`, local, limit)
	if err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Rate
	for rows.Next() {
		r, err := scanRate(rows)
		if err != nil {
			return nil, s.db.Dialect().TranslateError(err)
		}
		out = append(out, r)
	}
	return out, s.db.Dialect().TranslateError(rows.Err())
}

func (s *Store) AppendRate(ctx context.Context, r domain.Rate) error {
	_, err := s.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO fx_rates (`+rateColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID.String(), r.LocalCurrency, r.Seq, r.Nano, string(r.Source), nullableText(r.FetchID.String()),
		r.BusinessDate, clock.Format(r.RecordedAt), nullableText(r.Note))
	return s.db.Dialect().TranslateError(err)
}

const fetchColumns = `id, local_currency, seq, attempted_at, business_date, outcome, provider, local_per_usd_nano,
	against_rate_id, error_code`

func scanFetch(row scanner) (domain.Fetch, error) {
	var (
		f                       domain.Fetch
		rawID, at, outcome      string
		provider, against, code sql.NullString
		nano                    sql.NullInt64
	)
	if err := row.Scan(&rawID, &f.LocalCurrency, &f.Seq, &at, &f.BusinessDate, &outcome, &provider, &nano, &against, &code); err != nil {
		return domain.Fetch{}, err
	}
	var err error
	if f.ID, err = id.Parse(rawID); err != nil {
		return domain.Fetch{}, corrupt(err)
	}
	if f.AgainstRateID, err = optionalID(against); err != nil {
		return domain.Fetch{}, corrupt(err)
	}
	var ok bool
	if f.AttemptedAt, ok = clock.ParseTimestamp(at); !ok {
		return domain.Fetch{}, errs.Internal(CodeCorrupt, "a fetch's time does not parse").WithParam("value", at)
	}
	f.Outcome = domain.Outcome(outcome)
	f.Provider, f.Nano, f.ErrorCode = provider.String, nano.Int64, code.String
	return f, nil
}

func (s *Store) LastFetch(ctx context.Context, local string) (domain.Fetch, bool, error) {
	f, err := scanFetch(s.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+fetchColumns+` FROM fx_fetches WHERE local_currency = ? ORDER BY seq DESC LIMIT 1`, local))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Fetch{}, false, nil
	}
	if err != nil {
		return domain.Fetch{}, false, s.db.Dialect().TranslateError(err)
	}
	return f, true, nil
}

func (s *Store) Fetch(ctx context.Context, fetchID id.ID) (domain.Fetch, error) {
	f, err := scanFetch(s.db.Reader(ctx).QueryRowContext(ctx, `SELECT `+fetchColumns+` FROM fx_fetches WHERE id = ?`, fetchID.String()))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Fetch{}, errs.NotFound(domain.CodeFetchNotFound, "no such fetch")
	}
	if err != nil {
		return domain.Fetch{}, s.db.Dialect().TranslateError(err)
	}
	return f, nil
}

func (s *Store) AppendFetch(ctx context.Context, f domain.Fetch) error {
	var nano any
	if f.Outcome != domain.OutcomeFailed {
		nano = f.Nano
	}
	_, err := s.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO fx_fetches (`+fetchColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID.String(), f.LocalCurrency, f.Seq, clock.Format(f.AttemptedAt), f.BusinessDate, string(f.Outcome),
		nullableText(f.Provider), nano, nullableText(f.AgainstRateID.String()), nullableText(f.ErrorCode))
	return s.db.Dialect().TranslateError(err)
}

func nullableText(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func optionalID(v sql.NullString) (id.ID, error) {
	if !v.Valid {
		return "", nil
	}
	return id.Parse(v.String)
}

func corrupt(err error) error {
	return errs.Wrap(err, errs.CategoryInternal, CodeCorrupt, "a rate row does not read back")
}
