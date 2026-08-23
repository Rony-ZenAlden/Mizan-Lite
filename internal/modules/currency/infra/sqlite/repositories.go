// Package sqlite implements the currency module's repositories.
//
// It is the only place in the module that knows SQL. Everything portable-schema-related — the
// CHAR(24)/CHAR(10) timestamp and date forms, the dialect shim — is used here rather than
// leaking into the domain.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/round"
	"github.com/mizan-erp/mizan/internal/modules/currency/domain"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// Three repository types rather than one, because CurrencyRepository and RateTypeRepository
// both declare ByCode — a single type cannot satisfy both, and renaming one method to dodge
// that would make the domain interfaces read worse to serve an implementation detail.

// CurrencyRepo reads the currencies table.
type CurrencyRepo struct{ db database.DB }

// RateTypeRepo reads the rate_types table.
type RateTypeRepo struct{ db database.DB }

// RateRepo reads and appends exchange rates.
type RateRepo struct {
	db  database.DB
	clk clock.Clock
}

// Repos bundles the currency module's repositories over one database.
type Repos struct {
	Currencies *CurrencyRepo
	RateTypes  *RateTypeRepo
	Rates      *RateRepo
}

// New builds the repositories.
func New(db database.DB, clk clock.Clock) *Repos {
	if clk == nil {
		clk = clock.System()
	}
	return &Repos{
		Currencies: &CurrencyRepo{db: db},
		RateTypes:  &RateTypeRepo{db: db},
		Rates:      &RateRepo{db: db, clk: clk},
	}
}

var (
	_ domain.CurrencyRepository = (*CurrencyRepo)(nil)
	_ domain.RateTypeRepository = (*RateTypeRepo)(nil)
	_ domain.RateRepository     = (*RateRepo)(nil)
)

const currencyColumns = `id, code, name, symbol, decimal_places, symbol_position,
	rounding_mode, succeeded_by_code, redenomination_factor,
	is_historical, is_system, is_active`

// ByCode returns one currency.
func (r *CurrencyRepo) ByCode(ctx context.Context, code string) (domain.Info, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+currencyColumns+` FROM currencies WHERE code = ?`, code)

	info, err := scanCurrency(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Info{}, errs.NotFound(domain.CodeUnknownCurrency,
			"no such currency").WithParam("code", code)
	}
	if err != nil {
		return domain.Info{}, errs.Wrap(r.db.Dialect().TranslateError(err), errs.CategoryInternal,
			domain.CodeUnknownCurrency, "reading currency "+code)
	}
	return info, nil
}

// List returns currencies ordered by code.
func (r *CurrencyRepo) List(ctx context.Context, includeInactive bool) ([]domain.Info, error) {
	query := `SELECT ` + currencyColumns + ` FROM currencies`
	if !includeInactive {
		query += ` WHERE is_active = 1`
	}
	query += ` ORDER BY code`

	rows, err := r.db.Reader(ctx).QueryContext(ctx, query)
	if err != nil {
		return nil, errs.Wrap(r.db.Dialect().TranslateError(err), errs.CategoryInternal,
			domain.CodeUnknownCurrency, "listing currencies")
	}
	defer rows.Close()

	var out []domain.Info
	for rows.Next() {
		info, scanErr := scanCurrency(rows)
		if scanErr != nil {
			return nil, errs.Wrap(scanErr, errs.CategoryInternal, domain.CodeUnknownCurrency,
				"scanning currency")
		}
		out = append(out, info)
	}
	return out, rows.Err()
}

type scanner interface{ Scan(dest ...any) error }

func scanCurrency(s scanner) (domain.Info, error) {
	var (
		info          domain.Info
		rawID         string
		decimals      int64
		roundingName  string
		successor     *string
		factor        *int64
		hist, sys, on int64
	)
	if err := s.Scan(&rawID, &info.Code, &info.Name, &info.Symbol, &decimals,
		&info.SymbolPosition, &roundingName, &successor, &factor,
		&hist, &sys, &on); err != nil {
		return domain.Info{}, err
	}
	info.ID = id.ID(rawID)
	info.DecimalPlaces = uint8(decimals) //nolint:gosec // constrained to 0..9 by a CHECK
	// round.ParseMode is the other half of RoundingMode.String()'s storage contract, added in
	// Step 0.8 precisely so this column round-trips.
	mode, ok := round.ParseMode(roundingName)
	if !ok {
		return domain.Info{}, errs.Validation(domain.CodeInvalidCurrency,
			"currency has an unknown rounding mode").
			WithParam("code", info.Code).WithParam("rounding_mode", roundingName)
	}
	info.RoundingMode = mode
	if successor != nil {
		info.SucceededBy = *successor
	}
	if factor != nil {
		info.RedenominationFactor = *factor
	}
	info.IsHistorical = hist != 0
	info.IsSystem = sys != 0
	info.IsActive = on != 0
	return info, nil
}

// ── rate types ──────────────────────────────────────────────────────────────────

// ByCode returns one rate type.
func (r *RateTypeRepo) ByCode(ctx context.Context, code string) (domain.RateType, error) {
	var (
		rt       domain.RateType
		rawID    string
		sys, act int64
	)
	err := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT id, code, name, is_system, is_active FROM rate_types WHERE code = ?`, code).
		Scan(&rawID, &rt.Code, &rt.Name, &sys, &act)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.RateType{}, errs.NotFound(domain.CodeUnknownRateType,
			"no such rate type").WithParam("code", code)
	}
	if err != nil {
		return domain.RateType{}, errs.Wrap(r.db.Dialect().TranslateError(err),
			errs.CategoryInternal, domain.CodeUnknownRateType, "reading rate type "+code)
	}
	rt.ID = id.ID(rawID)
	rt.IsSystem = sys != 0
	rt.IsActive = act != 0
	return rt, nil
}

// List returns every rate type, ordered by code.
func (r *RateTypeRepo) List(ctx context.Context) ([]domain.RateType, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT id, code, name, is_system, is_active FROM rate_types ORDER BY code`)
	if err != nil {
		return nil, errs.Wrap(r.db.Dialect().TranslateError(err), errs.CategoryInternal,
			domain.CodeUnknownRateType, "listing rate types")
	}
	defer rows.Close()

	var out []domain.RateType
	for rows.Next() {
		var (
			rt       domain.RateType
			rawID    string
			sys, act int64
		)
		if scanErr := rows.Scan(&rawID, &rt.Code, &rt.Name, &sys, &act); scanErr != nil {
			return nil, errs.Wrap(scanErr, errs.CategoryInternal, domain.CodeUnknownRateType,
				"scanning rate type")
		}
		rt.ID = id.ID(rawID)
		rt.IsSystem = sys != 0
		rt.IsActive = act != 0
		out = append(out, rt)
	}
	return out, rows.Err()
}

// ── exchange rates ──────────────────────────────────────────────────────────────

const rateColumns = `id, from_currency, to_currency, rate_type_id, rate_nano,
	valid_from, valid_to, source, batch_id, created_at`

// newestOrder is the resolution order that makes append-only corrections work (Step 0.9, D3).
//
// valid_from DESC picks the rate in force on the date; created_at DESC then picks the most
// recently RECORDED of several rates carrying that same date — which is exactly what a
// correction is. id DESC only breaks a tie between rows written in the same millisecond.
const newestOrder = ` ORDER BY valid_from DESC, created_at DESC, id DESC LIMIT 1`

// Newest returns the rate in force on `at`, respecting an explicit valid_to.
func (r *RateRepo) Newest(
	ctx context.Context, from, to string, rateTypeID id.ID, at time.Time,
) (domain.Rate, bool, error) {
	day := clock.FormatDate(at)
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+rateColumns+` FROM exchange_rates
		  WHERE from_currency = ? AND to_currency = ? AND rate_type_id = ?
		    AND valid_from <= ?
		    AND (valid_to IS NULL OR valid_to >= ?)`+newestOrder,
		from, to, rateTypeID.String(), day, day)
	return scanRateRow(row)
}

// LastKnown ignores valid_to, returning the most recent rate at or before `at`.
//
// This is the offline fallback: §18.3 says missing a live rate is normal, not an error, so a
// rate that has formally expired is still better information than nothing — as long as the
// caller is told how old it is.
func (r *RateRepo) LastKnown(
	ctx context.Context, from, to string, rateTypeID id.ID, at time.Time,
) (domain.Rate, bool, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+rateColumns+` FROM exchange_rates
		  WHERE from_currency = ? AND to_currency = ? AND rate_type_id = ?
		    AND valid_from <= ?`+newestOrder,
		from, to, rateTypeID.String(), clock.FormatDate(at))
	return scanRateRow(row)
}

func scanRateRow(row *sql.Row) (domain.Rate, bool, error) {
	var (
		rate                 domain.Rate
		rawID, rawType       string
		validFrom, createdAt string
		validTo, batchID     *string
	)
	err := row.Scan(&rawID, &rate.From, &rate.To, &rawType, &rate.Nano,
		&validFrom, &validTo, &rate.Source, &batchID, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Rate{}, false, nil
	}
	if err != nil {
		return domain.Rate{}, false, errs.Wrap(err, errs.CategoryInternal, domain.CodeNoRate,
			"reading exchange rate")
	}
	rate.ID = id.ID(rawID)
	rate.RateTypeID = id.ID(rawType)
	rate.ValidFrom, _ = clock.ParseDate(validFrom)
	rate.CreatedAt, _ = clock.ParseTimestamp(createdAt)
	if validTo != nil {
		if t, ok := clock.ParseDate(*validTo); ok {
			rate.ValidTo = &t
		}
	}
	if batchID != nil {
		rate.BatchID = *batchID
	}
	return rate, true, nil
}

// Append records a rate. Rates are never updated in place — a correction is a new row.
func (r *RateRepo) Append(ctx context.Context, rate domain.Rate) error {
	if rate.Nano <= 0 {
		return errs.Validation(domain.CodeInvalidRate, "a rate must be positive").
			WithParam("from", rate.From).WithParam("to", rate.To)
	}
	if rate.From == rate.To {
		return errs.Validation(domain.CodeInvalidRate,
			"a rate cannot convert a currency to itself; identity is computed, never stored").
			WithParam("code", rate.From)
	}

	newID, err := id.New()
	if err != nil {
		return err
	}
	now := clock.Format(r.clk.Now())

	var validTo any
	if rate.ValidTo != nil {
		validTo = clock.FormatDate(*rate.ValidTo)
	}
	var batchID any
	if rate.BatchID != "" {
		batchID = rate.BatchID
	}
	source := rate.Source
	if source == "" {
		source = "manual"
	}

	if _, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO exchange_rates
		  (id, from_currency, to_currency, rate_type_id, rate_nano, valid_from, valid_to,
		   source, batch_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		newID.String(), rate.From, rate.To, rate.RateTypeID.String(), rate.Nano,
		clock.FormatDate(rate.ValidFrom), validTo, source, batchID, now, now,
	); err != nil {
		return errs.Wrap(r.db.Dialect().TranslateError(err), errs.CategoryInternal,
			domain.CodeInvalidRate, "recording exchange rate "+rate.From+"→"+rate.To)
	}
	return nil
}

// History lists the most recently RECORDED rates for a pair, newest first.
//
// # Why recorded order rather than effective order
//
// Rates are append-only: a correction is a new row carrying the same `valid_from` as the one it
// corrects (Step 0.9, D3). A screen ordered by `valid_from` alone would show the correction and
// the mistake adjacent and indistinguishable, and the reader would have no way to tell which of
// the two the system is actually using.
//
// Ordered the same way resolution orders — valid_from, then created_at — the row the engine will
// pick is always the first one shown for its date. What the screen says and what the till charges
// cannot disagree.
//
// # Why the rate TYPE is part of the query
//
// A market rate and an official rate for the same pair are different numbers, both current, and
// used by different parts of the system: §G.1 binds trade to `market` and anything the state
// reads to `official`. Listing them together would put two rows on screen that are both correct
// and only one of which any given document will use — and no ordering can make that legible.
func (r *RateRepo) History(
	ctx context.Context, from, to string, rateTypeID id.ID, limit int,
) ([]domain.Rate, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT `+rateColumns+` FROM exchange_rates
		  WHERE from_currency = ? AND to_currency = ? AND rate_type_id = ?
		  ORDER BY valid_from DESC, created_at DESC, id DESC
		  LIMIT ?`, from, to, rateTypeID.String(), limit)
	if err != nil {
		return nil, errs.Wrap(err, errs.CategoryInternal, domain.CodeNoRate,
			"listing exchange rates")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.Rate, 0, limit)
	for rows.Next() {
		var (
			rate                 domain.Rate
			rawID, rawType       string
			validFrom, createdAt string
			validTo, batchID     *string
		)
		if err = rows.Scan(&rawID, &rate.From, &rate.To, &rawType, &rate.Nano,
			&validFrom, &validTo, &rate.Source, &batchID, &createdAt); err != nil {
			return nil, errs.Wrap(err, errs.CategoryInternal, domain.CodeNoRate,
				"reading an exchange rate")
		}
		rate.ID = id.ID(rawID)
		rate.RateTypeID = id.ID(rawType)
		rate.ValidFrom, _ = clock.ParseDate(validFrom)
		rate.CreatedAt, _ = clock.ParseTimestamp(createdAt)
		if validTo != nil {
			if t, ok := clock.ParseDate(*validTo); ok {
				rate.ValidTo = &t
			}
		}
		if batchID != nil {
			rate.BatchID = *batchID
		}
		out = append(out, rate)
	}
	return out, rows.Err()
}
