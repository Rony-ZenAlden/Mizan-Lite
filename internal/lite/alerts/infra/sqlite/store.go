// Package sqlite is the capital_snapshots table: one row per business day, rewritten through the day.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/alerts/domain"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// CodeCorrupt reports a stored row this build cannot read back.
const CodeCorrupt = "lite.alerts.corrupt_row"

// Store implements alerts.Store.
type Store struct{ db database.DB }

// NewStore builds the store.
func NewStore(db database.DB) *Store { return &Store{db: db} }

const columns = `business_date, taken_at, local_currency, local_per_usd_nano, stock_usd_minor, cash_usd_minor,
	cash_local_minor, owed_usd_minor, owed_local_minor`

// Put writes a day's snapshot, replacing the day's earlier one.
func (s *Store) Put(ctx context.Context, snap domain.Snapshot, takenAt time.Time) error {
	_, err := s.db.Writer(ctx).ExecContext(ctx, `INSERT INTO capital_snapshots (`+columns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (business_date) DO UPDATE SET taken_at = excluded.taken_at, local_currency = excluded.local_currency,
			local_per_usd_nano = excluded.local_per_usd_nano, stock_usd_minor = excluded.stock_usd_minor,
			cash_usd_minor = excluded.cash_usd_minor, cash_local_minor = excluded.cash_local_minor,
			owed_usd_minor = excluded.owed_usd_minor, owed_local_minor = excluded.owed_local_minor`,
		snap.BusinessDate, clock.Format(takenAt), snap.LocalCurrency, snap.RateNano, snap.StockUSDMinor,
		snap.CashUSDMinor, snap.CashLocalMinor, snap.OwedUSDMinor, snap.OwedLocalMinor)
	return s.db.Dialect().TranslateError(err)
}

// Get returns one day's snapshot and when it was taken.
func (s *Store) Get(ctx context.Context, businessDate string) (domain.Snapshot, time.Time, bool, error) {
	return s.one(ctx, `WHERE business_date = ?`, businessDate)
}

// OnOrBefore returns the newest snapshot dated on or before a business date.
func (s *Store) OnOrBefore(ctx context.Context, businessDate string) (domain.Snapshot, bool, error) {
	snap, _, found, err := s.one(ctx, `WHERE business_date <= ? ORDER BY business_date DESC LIMIT 1`, businessDate)
	return snap, found, err
}

// First returns the oldest snapshot.
func (s *Store) First(ctx context.Context) (domain.Snapshot, bool, error) {
	snap, _, found, err := s.one(ctx, `ORDER BY business_date ASC LIMIT 1`)
	return snap, found, err
}

func (s *Store) one(ctx context.Context, where string, args ...any) (domain.Snapshot, time.Time, bool, error) {
	var snap domain.Snapshot
	var taken string
	err := s.db.Reader(ctx).QueryRowContext(ctx, `SELECT `+columns+` FROM capital_snapshots `+where, args...).Scan(
		&snap.BusinessDate, &taken, &snap.LocalCurrency, &snap.RateNano, &snap.StockUSDMinor, &snap.CashUSDMinor,
		&snap.CashLocalMinor, &snap.OwedUSDMinor, &snap.OwedLocalMinor)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Snapshot{}, time.Time{}, false, nil
	}
	if err != nil {
		return domain.Snapshot{}, time.Time{}, false, s.db.Dialect().TranslateError(err)
	}
	at, ok := clock.ParseTimestamp(taken)
	if !ok {
		return domain.Snapshot{}, time.Time{}, false, errs.Internal(CodeCorrupt, "a snapshot's time cannot be read").WithParam("value", taken)
	}
	return snap, at, true, nil
}
