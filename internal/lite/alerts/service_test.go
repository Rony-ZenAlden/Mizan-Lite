package alerts_test

import (
	"context"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/alerts"
	"github.com/mizan-erp/mizan/internal/lite/alerts/alertstest"
	"github.com/mizan-erp/mizan/internal/lite/alerts/domain"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
)

// shop is a fake of every port, set to a shop with one low product, one stale price sold at a loss, a month of history
// in which the pound fell 10%, and a backup taken an hour ago.
type shop struct {
	oil, jar      id.ID
	allowed       bool
	rate          int64
	backupAge     time.Duration
	clk           *clock.Fixed
	stockValueUSD int64
	payableUSD    int64
}

func (s *shop) Products(context.Context) ([]domain.Product, error) {
	return []domain.Product{
		{ID: s.oil, NameAR: "زيت", Active: true, HasReorder: true, ReorderMicro: 5_000_000},
		{ID: s.jar, NameAR: "دبس", Active: true},
	}, nil
}
func (s *shop) OnHand(context.Context) (map[id.ID]int64, error) {
	return map[id.ID]int64{s.oil: 2_000_000, s.jar: 40_000_000}, nil
}
func (s *shop) AvgCostUSD(context.Context) (map[id.ID]int64, error) {
	return map[id.ID]int64{s.jar: 1_000_000}, nil // the jar cost the shop $1.00
}
func (s *shop) ValueUSD(context.Context) (int64, error) { return s.stockValueUSD, nil }
func (s *shop) Stale(context.Context) ([]domain.Stale, error) {
	// Priced at 15,000 when the dollar was 15,000; at 16,500 that is below the 16,500 it now costs to replace.
	return []domain.Stale{{ProductID: s.jar, NameAR: "دبس", Currency: "SYP", PriceMicro: 15_000_000_000,
		ShiftMicro: 10_000_000, ProposedMicro: 16_500_000_000}}, nil
}
func (s *shop) InForce(context.Context) (int64, string, bool, error) { return s.rate, "SYP", true, nil }
func (s *shop) Decimals(context.Context) (int, int, error)           { return 0, 2, nil }
func (s *shop) Expected(context.Context, string) (int64, int64, error) {
	return 0, 15_000_000, nil // 15,000,000 old pounds in the drawer
}
func (s *shop) Owed(context.Context) (int64, int64, error)  { return 0, 0, nil }
func (s *shop) Owing(context.Context) (int64, int64, error) { return s.payableUSD, 0, nil }
func (s *shop) Newest(context.Context) (time.Time, bool, error) {
	return s.clk.Now().Add(-s.backupAge), true, nil
}
func (s *shop) Allowed(context.Context) bool { return s.allowed }

func newShop(t *testing.T) (*shop, *alerts.Service, *alertstest.Fake) {
	t.Helper()
	oil, _ := id.New()
	jar, _ := id.New()
	s := &shop{oil: oil, jar: jar, allowed: true, rate: 16_500_000_000_000, backupAge: time.Hour,
		clk: clock.NewFixed(time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)), stockValueUSD: 100_000}
	store := alertstest.NewFake()
	// A month ago: the same stock and drawer, at 15,000.
	_ = store.Put(context.Background(), domain.Snapshot{BusinessDate: "2026-08-24", RateNano: 15_000_000_000_000,
		LocalCurrency: "SYP", StockUSDMinor: 100_000, CashLocalMinor: 15_000_000}, s.clk.Now().AddDate(0, 0, -30))
	ports := alerts.Ports{Catalogue: s, Stock: s, Prices: s, Money: s, Drawer: s, Receivables: s, Payables: s, Backups: s, Gate: s}
	return s, alerts.NewService(litetest.Immediate{}, store, ports, s.clk, time.UTC), store
}

// TestTheEngineFindsWhatTheOwnerAskedToBeToldAbout: low stock, a price left behind by the rate and sold at a loss, and
// the pound's fall taking part of the shop's worth.
func TestTheEngineFindsWhatTheOwnerAskedToBeToldAbout(t *testing.T) {
	s, svc, _ := newShop(t)
	r, err := svc.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Facts.Low) != 1 || r.Facts.Low[0].Product.ID != s.oil {
		t.Fatalf("low = %+v", r.Facts.Low)
	}
	if len(r.Facts.Stale) != 1 || !r.Facts.Stale[0].Loss || r.Facts.Stale[0].ReplacementMicro != 16_500_000_000 {
		t.Fatalf("stale = %+v — the jar sells at 15,000 and costs 16,500 to replace", r.Facts.Stale)
	}
	if r.Facts.Capital == nil || !r.Facts.Capital.Notify || r.Facts.Capital.DepreciationUSD <= 0 {
		t.Fatalf("capital = %+v", r.Facts.Capital)
	}
	kinds := map[domain.Kind]bool{}
	for _, n := range r.Notifications {
		kinds[n.Kind] = true
	}
	for _, want := range []domain.Kind{domain.KindLowStock, domain.KindStalePrices, domain.KindCapital} {
		if !kinds[want] {
			t.Errorf("no %s notification in %+v", want, r.Notifications)
		}
	}
	if kinds[domain.KindBackupNone] || kinds[domain.KindBackupOld] {
		t.Error("a backup an hour old raised a backup notification")
	}
	if r.OwnerHidden {
		t.Error("figures were hidden from someone allowed to see them")
	}
}

// TestTodaysSnapshotTakesOffWhatTheShopOwesItsSuppliers (0.10.0): today's worth is kept with the payables beside it,
// and the comparison reads them.
func TestTodaysSnapshotTakesOffWhatTheShopOwesItsSuppliers(t *testing.T) {
	s, svc, store := newShop(t)
	s.payableUSD = 25_000
	r, err := svc.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	today, _, found, _ := store.Get(context.Background(), "2026-09-23")
	if !found || today.PayableUSDMinor != 25_000 {
		t.Fatalf("today's snapshot %+v", today)
	}
	if r.Facts.Capital == nil || r.Facts.Capital.NowUSD != today.TotalUSDMinor(0, 2) || r.Facts.Capital.NowUSD >= 190_909 {
		t.Fatalf("capital %+v — $250 owed to a supplier did not come off the shop's worth", r.Facts.Capital)
	}
}

// TestOutsideOwnerModeTheOwnersFiguresAreHeldBack: with PIN protection on and nobody in owner mode, what the shop is
// worth and what its stock costs stay hidden — and the bell cannot count a notification the centre may not show.
func TestOutsideOwnerModeTheOwnersFiguresAreHeldBack(t *testing.T) {
	s, svc, _ := newShop(t)
	s.allowed = false
	r, err := svc.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !r.OwnerHidden {
		t.Fatal("nothing was reported hidden")
	}
	if r.Facts.Capital != nil {
		t.Fatal("the shop's worth was disclosed outside owner mode")
	}
	for _, st := range r.Facts.Stale {
		if st.Loss || st.CostKnown || st.ReplacementMicro != 0 {
			t.Fatalf("a replacement cost was disclosed outside owner mode: %+v", st)
		}
	}
	for _, n := range r.Notifications {
		if n.Owner || n.Kind == domain.KindCapital {
			t.Fatalf("the bell counts a notification the centre may not show: %+v", n)
		}
	}
	// The stale prices themselves stay: a price is on the shelf for anyone to read.
	if len(r.Facts.Stale) != 1 {
		t.Fatal("the stale prices were hidden along with their costs")
	}
}

// TestTheDailySnapshotIsRecordedEvenOutsideOwnerMode: recording what the shop is worth is not showing it to anyone.
// A shop with PIN protection on would otherwise have no history on the day the owner finally looks.
func TestTheDailySnapshotIsRecordedEvenOutsideOwnerMode(t *testing.T) {
	s, svc, store := newShop(t)
	s.allowed = false
	if _, err := svc.Current(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, found, _ := store.Get(context.Background(), "2026-09-23"); !found {
		t.Fatal("today's snapshot was not recorded because nobody was in owner mode")
	}
}

// TestTheSnapshotIsNotRewrittenOnEveryGlance: the bell asks every minute; the day's row is rewritten at most every
// half hour.
func TestTheSnapshotIsNotRewrittenOnEveryGlance(t *testing.T) {
	s, svc, store := newShop(t)
	ctx := context.Background()
	_, _ = svc.Current(ctx)
	_, first, _, _ := store.Get(ctx, "2026-09-23")
	s.stockValueUSD = 999_999
	s.clk.Advance(10 * time.Minute)
	_, _ = svc.Current(ctx)
	snap, again, _, _ := store.Get(ctx, "2026-09-23")
	if !again.Equal(first) || snap.StockUSDMinor == 999_999 {
		t.Fatal("the snapshot was rewritten ten minutes later")
	}
	s.clk.Advance(alerts.SnapshotEvery)
	_, _ = svc.Current(ctx)
	if snap, _, _, _ = store.Get(ctx, "2026-09-23"); snap.StockUSDMinor != 999_999 {
		t.Fatal("the snapshot was not refreshed after half an hour")
	}
}

// TestAStaleBackupIsTheShopsOwnAndNeverTheUSB: only the shop's own backups are judged (D-098.1).
func TestAStaleBackupIsTheShopsOwnAndNeverTheUSB(t *testing.T) {
	s, svc, _ := newShop(t)
	s.backupAge = 40 * time.Hour
	r, _ := svc.Current(context.Background())
	if r.Notifications[0].Kind != domain.KindBackupOld {
		t.Fatalf("a 40-hour-old backup was not first: %+v", r.Notifications[0])
	}
}

// TestANewShopIsToldHowLongUntilItCanCompare: on the first day there is nothing to compare, and the centre can say how
// far the history reaches rather than show a comparison of a day with itself.
func TestANewShopIsToldHowLongUntilItCanCompare(t *testing.T) {
	s, _, _ := newShop(t)
	store := alertstest.NewFake()
	ports := alerts.Ports{Catalogue: s, Stock: s, Prices: s, Money: s, Drawer: s, Receivables: s, Payables: s, Backups: s, Gate: s}
	svc := alerts.NewService(litetest.Immediate{}, store, ports, s.clk, time.UTC)
	for _, want := range []int{0, 1, 6} {
		s.clk.Current = time.Date(2026, 9, 23+want, 9, 0, 0, 0, time.UTC)
		r, err := svc.Current(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if r.HistoryDays != want || r.Facts.Capital != nil {
			t.Fatalf("after %d days: history = %d, capital = %+v", want, r.HistoryDays, r.Facts.Capital)
		}
	}
	// A week on, there is a comparison.
	s.clk.Current = time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	r, _ := svc.Current(context.Background())
	if r.HistoryDays != domain.MinHistoryDays || r.Facts.Capital == nil {
		t.Fatalf("a week on: history = %d, capital = %+v", r.HistoryDays, r.Facts.Capital)
	}
}
