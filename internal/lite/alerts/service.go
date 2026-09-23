// Package alerts is the notification engine (the owner's request, 2026-09-23): low stock, prices the exchange rate has
// left behind, what the shop is worth in dollars, and the shop's own backups — one list, which the toasts announce, the
// bell counts and the notification centre shows.
//
// It imports no other module. Every fact arrives through a port it declares, satisfied in the composition root by the
// module that owns it — the same shape as the reports, and for the same reason: SQL stays with its owner.
package alerts

import (
	"context"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/alerts/domain"
	"github.com/mizan-erp/mizan/internal/lite/bizdate"
)

// Store keeps the daily snapshots of what the shop is worth. infra/sqlite implements it; alertstest.Fake implements it
// in memory, and one contract suite runs against both.
type Store interface {
	// Put writes a day's snapshot, replacing the day's earlier one.
	Put(ctx context.Context, s domain.Snapshot, takenAt time.Time) error
	// Get returns one day's snapshot and when it was taken, or found=false.
	Get(ctx context.Context, businessDate string) (s domain.Snapshot, takenAt time.Time, found bool, err error)
	// OnOrBefore returns the newest snapshot dated on or before a business date, or found=false.
	OnOrBefore(ctx context.Context, businessDate string) (domain.Snapshot, bool, error)
	// First returns the oldest snapshot, or found=false.
	First(ctx context.Context) (domain.Snapshot, bool, error)
}

// Transactor runs fn atomically.
type Transactor interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// Catalogue is every product, as the engine needs it.
type Catalogue interface {
	Products(ctx context.Context) ([]domain.Product, error)
}

// Stock is what is on the shelf and what it cost.
type Stock interface {
	// OnHand is each product's quantity at 10⁻⁶.
	OnHand(ctx context.Context) (map[id.ID]int64, error)
	// AvgCostUSD is each product's average cost in dollars at 10⁻⁶, for products whose cost is known.
	AvgCostUSD(ctx context.Context) (map[id.ID]int64, error)
	// ValueUSD is the whole stock at average cost, in dollar minor units — computed WITHOUT the owner check, because
	// recording what the shop is worth is not the same as showing it to someone.
	ValueUSD(ctx context.Context) (int64, error)
}

// Prices is the catalogue's list of prices the rate has left behind, with what it would propose for each.
type Prices interface {
	Stale(ctx context.Context) ([]domain.Stale, error)
}

// Money is the rate in force and the two currencies' decimals.
type Money interface {
	InForce(ctx context.Context) (nano int64, local string, found bool, err error)
	Decimals(ctx context.Context) (local, usd int, err error)
}

// Drawer is what the drawer is expected to hold at the end of a business date, per currency.
type Drawer interface {
	Expected(ctx context.Context, businessDate string) (usdMinor, localMinor int64, err error)
}

// Receivables is what customers owe the shop, net, per currency.
type Receivables interface {
	Owed(ctx context.Context) (usdMinor, localMinor int64, err error)
}

// Backups is when the newest of the shop's own backups was taken.
type Backups interface {
	Newest(ctx context.Context) (at time.Time, found bool, err error)
}

// Gate is the owner's PIN: whether an owner's figure may be shown right now.
type Gate interface {
	Allowed(ctx context.Context) bool
}

// Ports is everything the engine reads.
type Ports struct {
	Catalogue   Catalogue
	Stock       Stock
	Prices      Prices
	Money       Money
	Drawer      Drawer
	Receivables Receivables
	Backups     Backups
	Gate        Gate
}

// Service is the notification engine.
type Service struct {
	tx    Transactor
	store Store
	ports Ports
	clk   clock.Clock
	loc   *time.Location
}

// NewService builds the engine.
func NewService(tx Transactor, store Store, ports Ports, clk clock.Clock, loc *time.Location) *Service {
	return &Service{tx: tx, store: store, ports: ports, clk: clk, loc: loc}
}

// SnapshotEvery is how stale today's snapshot may get before it is rewritten. The day's snapshot follows the day, and
// is final once the day is over; rewriting it on every read would put a write behind every glance at the bell.
const SnapshotEvery = 30 * time.Minute

// Report is one pass of the engine: what it found, and the list the screen shows.
type Report struct {
	Facts         domain.Facts
	Notifications []domain.Notification
	// OwnerHidden is true when an owner's figure was held back: PIN protection is on and nobody is in owner mode.
	OwnerHidden bool
	// HistoryDays is how many days the snapshots reach back — 0 on the first day — so the centre can say how long it
	// has been collecting before it can compare (domain.MinHistoryDays).
	HistoryDays   int
	LocalCurrency string
	LocalDecimals int
	USDDecimals   int
}

// Current is what deserves the shop's attention right now.
//
// It computes everything, and only then decides what to disclose. The daily snapshot is recorded whether or not anyone
// is in owner mode — recording what the shop is worth is not showing it to anyone — and the owner's figures are held
// back at the end, where one decision covers all of them.
func (s *Service) Current(ctx context.Context) (Report, error) {
	p := s.ports
	out := Report{}
	rate, local, rateFound, err := p.Money.InForce(ctx)
	if err != nil {
		return out, err
	}
	if out.LocalDecimals, out.USDDecimals, err = p.Money.Decimals(ctx); err != nil {
		return out, err
	}
	out.LocalCurrency = local
	out.Facts.RateNano = rate

	products, err := p.Catalogue.Products(ctx)
	if err != nil {
		return out, err
	}
	onHand, err := p.Stock.OnHand(ctx)
	if err != nil {
		return out, err
	}
	out.Facts.Low = domain.LowStock(products, onHand)

	if rateFound {
		if out.Facts.Stale, err = s.stale(ctx, rate); err != nil {
			return out, err
		}
		if out.Facts.Capital, out.HistoryDays, err = s.capital(ctx, rate, local, out.LocalDecimals, out.USDDecimals); err != nil {
			return out, err
		}
	}

	newest, found, err := p.Backups.Newest(ctx)
	if err != nil {
		return out, err
	}
	out.Facts.BackupFound = found
	if found {
		out.Facts.BackupAgeSeconds = int64(s.clk.Now().Sub(newest) / time.Second)
	}
	out.Facts.Today = bizdate.Date(s.clk.Now(), s.loc)

	out.Notifications = out.Facts.Notifications()
	if !p.Gate.Allowed(ctx) {
		out = withholdOwnerFigures(out)
	}
	return out, nil
}

// stale is the catalogue's stale prices, each marked with whether it now sells below what it costs to replace.
func (s *Service) stale(ctx context.Context, rate int64) ([]domain.Stale, error) {
	items, err := s.ports.Prices.Stale(ctx)
	if err != nil {
		return nil, err
	}
	costs, err := s.ports.Stock.AvgCostUSD(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		avg, known := costs[items[i].ProductID]
		if !known {
			continue
		}
		items[i].CostKnown = true
		items[i].ReplacementMicro = domain.Replacement(avg, rate)
		items[i].Loss = items[i].PriceMicro < items[i].ReplacementMicro
	}
	return items, nil
}

// capital records today's snapshot and compares it with the one from about a month ago.
func (s *Service) capital(ctx context.Context, rate int64, local string, localDec, usdDec int) (*domain.Capital, int, error) {
	now := s.clk.Now().UTC().Truncate(time.Millisecond)
	today := bizdate.Date(now, s.loc)
	snap, err := s.snapshot(ctx, today, rate, local)
	if err != nil {
		return nil, 0, err
	}
	if err = s.keep(ctx, snap, now); err != nil {
		return nil, 0, err
	}

	first, found, err := s.store.First(ctx)
	if err != nil || !found {
		return nil, 0, err
	}
	days := daysBetween(first.BusinessDate, today)
	// The comparison: the snapshot nearest a month back, or the oldest one if the shop is younger than that — but never
	// one from the last week, which says nothing yet.
	then, found, err := s.store.OnOrBefore(ctx, addDays(today, -domain.WindowDays))
	if err != nil {
		return nil, days, err
	}
	if !found {
		then, found = first, true
	}
	if !found || daysBetween(then.BusinessDate, today) < domain.MinHistoryDays {
		return nil, days, nil
	}
	c := domain.CompareCapital(then, snap, localDec, usdDec)
	return &c, days, nil
}

// snapshot is what the shop is worth today.
func (s *Service) snapshot(ctx context.Context, today string, rate int64, local string) (domain.Snapshot, error) {
	p := s.ports
	snap := domain.Snapshot{BusinessDate: today, RateNano: rate, LocalCurrency: local}
	var err error
	if snap.StockUSDMinor, err = p.Stock.ValueUSD(ctx); err != nil {
		return snap, err
	}
	if snap.CashUSDMinor, snap.CashLocalMinor, err = p.Drawer.Expected(ctx, today); err != nil {
		return snap, err
	}
	snap.OwedUSDMinor, snap.OwedLocalMinor, err = p.Receivables.Owed(ctx)
	return snap, err
}

// keep writes today's snapshot if there is none yet, or the one there is has gone stale.
func (s *Service) keep(ctx context.Context, snap domain.Snapshot, now time.Time) error {
	_, taken, found, err := s.store.Get(ctx, snap.BusinessDate)
	if err != nil {
		return err
	}
	if found && now.Sub(taken) < SnapshotEvery {
		return nil
	}
	return s.tx.Do(ctx, func(ctx context.Context) error { return s.store.Put(ctx, snap, now) })
}

// withholdOwnerFigures removes everything that discloses a cost or the shop's worth: the capital comparison, and the
// replacement cost behind a "selling at a loss" flag. The stale prices themselves stay — a price is on the shelf for
// anyone to read.
func withholdOwnerFigures(r Report) Report {
	hidden := r.Facts.Capital != nil
	r.Facts.Capital = nil
	stale := make([]domain.Stale, 0, len(r.Facts.Stale))
	for _, item := range r.Facts.Stale {
		hidden = hidden || item.CostKnown
		item.CostKnown, item.ReplacementMicro, item.Loss = false, 0, false
		stale = append(stale, item)
	}
	r.Facts.Stale = stale
	// Rebuilt from the withheld facts, so the list cannot mention what the centre is not allowed to show.
	r.Notifications = r.Facts.Notifications()
	r.OwnerHidden = hidden
	return r
}

// addDays moves a business date by n days.
func addDays(date string, n int) string {
	t, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return date
	}
	return t.AddDate(0, 0, n).Format(time.DateOnly)
}

// daysBetween is how many days lie from a to b.
func daysBetween(a, b string) int {
	ta, errA := time.Parse(time.DateOnly, a)
	tb, errB := time.Parse(time.DateOnly, b)
	if errA != nil || errB != nil {
		return 0
	}
	return int(tb.Sub(ta).Hours() / 24)
}
