package domain_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/alerts/domain"
)

func pid(t *testing.T) id.ID {
	t.Helper()
	v, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// TestLowStockListsOnlyWhatTheShopAskedToBeToldAbout: "low" is a comparison, and only the shop knows the second number.
func TestLowStockListsOnlyWhatTheShopAskedToBeToldAbout(t *testing.T) {
	rice, oil, bread, bag, gone := pid(t), pid(t), pid(t), pid(t), pid(t)
	products := []domain.Product{
		{ID: rice, NameAR: "رز", Active: true, HasReorder: true, ReorderMicro: 10_000_000},  // 12 of 10: fine
		{ID: oil, NameAR: "زيت", Active: true, HasReorder: true, ReorderMicro: 20_000_000},  // 2 of 20: very low
		{ID: bread, NameAR: "خبز", Active: true, HasReorder: true, ReorderMicro: 4_000_000}, // 2 of 4: low
		{ID: bag, NameAR: "كيس", Active: true, OpenPrice: true, HasReorder: true, ReorderMicro: 1_000_000},
		{ID: gone, NameAR: "قديم", Active: false, HasReorder: true, ReorderMicro: 5_000_000},
	}
	onHand := map[id.ID]int64{rice: 12_000_000, oil: 2_000_000, bread: 2_000_000, bag: 0, gone: 0}
	low := domain.LowStock(products, onHand)
	if len(low) != 2 {
		t.Fatalf("low = %+v, want oil and bread only", low)
	}
	// Emptiest relative to the level first: 2 of 20 before 2 of 4.
	if low[0].Product.ID != oil || low[1].Product.ID != bread {
		t.Fatalf("order = %s, %s — want oil (10%% of its level) before bread (50%%)", low[0].Product.NameAR, low[1].Product.NameAR)
	}
	// Exactly at the level is low: "tell me at ten" means ten is when to buy.
	products[0].ReorderMicro = 12_000_000
	if got := domain.LowStock(products, onHand); len(got) != 3 {
		t.Fatalf("at the level was not low: %d listed", len(got))
	}
}

// TestReplacementIsTheDollarCostAtTodaysRate, without overflowing on old-pound figures.
func TestReplacementIsTheDollarCostAtTodaysRate(t *testing.T) {
	// A jar that cost the shop $1.00 costs 16,500 old pounds to replace at 16,500.
	if got := domain.Replacement(1_000_000, 16_500_000_000_000); got != 16_500_000_000 {
		t.Fatalf("replacement = %d, want 16,500 pounds", got)
	}
	if got := domain.Replacement(0, 16_500_000_000_000); got != 0 {
		t.Fatalf("an unknown cost produced a replacement: %d", got)
	}
}

// TestCapitalSeparatesAFallingPoundFromABadMonth is the insight the owner asked for: a shop can look flat in pounds
// while the pounds it held lost a tenth of their value. The two numbers must be told apart.
func TestCapitalSeparatesAFallingPoundFromABadMonth(t *testing.T) {
	then := domain.Snapshot{BusinessDate: "2026-08-24", RateNano: 15_000_000_000_000,
		StockUSDMinor: 100_000, CashLocalMinor: 15_000_000, OwedLocalMinor: 0} // $1,000 of stock + 15,000,000 old pounds = $1,000
	now := domain.Snapshot{BusinessDate: "2026-09-23", RateNano: 16_500_000_000_000,
		StockUSDMinor: 100_000, CashLocalMinor: 15_000_000} // the same things held, the pound down 10%
	c := domain.CompareCapital(then, now, 0, 2)
	if c.ThenUSD != 200_000 {
		t.Fatalf("then = %d cents, want $2,000", c.ThenUSD)
	}
	// 15,000,000 ÷ 16,500 = $909.09 — so $2,000 became $1,909.09.
	if c.NowUSD != 190_909 {
		t.Fatalf("now = %d cents, want $1,909.09", c.NowUSD)
	}
	// All of the fall is the pound's: the shop held the same things.
	if c.DepreciationUSD != 9_091 {
		t.Fatalf("lost to the rate = %d cents, want $90.91", c.DepreciationUSD)
	}
	if c.RateShiftMicro != 10_000_000 {
		t.Fatalf("rate shift = %d, want 10%%", c.RateShiftMicro)
	}
	if !c.Notify {
		t.Fatal("the pound took 4.5% of the shop's worth and the owner was not told")
	}
}

// TestAQuietMonthDoesNotNotify: small moves are the ordinary noise of a shop.
func TestAQuietMonthDoesNotNotify(t *testing.T) {
	then := domain.Snapshot{RateNano: 15_000_000_000_000, StockUSDMinor: 100_000, CashUSDMinor: 50_000}
	now := domain.Snapshot{RateNano: 15_100_000_000_000, StockUSDMinor: 102_000, CashUSDMinor: 50_000}
	if c := domain.CompareCapital(then, now, 0, 2); c.Notify {
		t.Fatalf("a 1.3%% rise notified: %+v", c)
	}
}

// TestTheBellCountsWhatDeservesIt, and a notification that is merely still true keeps its fingerprint.
func TestTheBellCountsWhatDeservesIt(t *testing.T) {
	oil := pid(t)
	f := domain.Facts{
		Low:         []domain.Low{{Product: domain.Product{ID: oil, NameAR: "زيت"}, OnHandMicro: 1}},
		Stale:       []domain.Stale{{ProductID: pid(t), Loss: true}},
		RateNano:    16_500_000_000_000,
		BackupFound: false,
	}
	list := f.Notifications()
	if len(list) != 3 {
		t.Fatalf("notifications = %+v", list)
	}
	if list[0].Kind != domain.KindBackupNone {
		t.Fatalf("an unprotected shop is not first: %v", list[0].Kind)
	}
	// A new rate is a new stale-price notification: that is the moment the owner asked to be told.
	before := f.Notifications()[1].Fingerprint
	f.RateNano = 17_000_000_000_000
	if after := f.Notifications()[1].Fingerprint; after == before {
		t.Fatal("a new exchange rate did not make the stale-price notification new")
	}
	// Owner mode shows the losses and leaving it hides them; neither is news about the prices.
	shown := f.Notifications()[1].Fingerprint
	f.Stale[0].Loss = false
	if hidden := f.Notifications()[1].Fingerprint; hidden != shown {
		t.Fatal("hiding the losses changed the stale-price notification")
	}
	// The low-stock notification names its product, so reading it once is reading it once.
	if list[2].Key != "low_stock:"+oil.String() || list[2].Fingerprint != "low" {
		t.Fatalf("low stock key = %q / %q", list[2].Key, list[2].Fingerprint)
	}
}

// TestAnUnprotectedShopIsRemindedOnceADay: the same day is read once; the next day without a backup is new again.
func TestAnUnprotectedShopIsRemindedOnceADay(t *testing.T) {
	for _, f := range []domain.Facts{
		{BackupFound: false, Today: "2026-09-23"},
		{BackupFound: true, BackupAgeSeconds: domain.BackupTooOldSeconds + 1, Today: "2026-09-23"},
	} {
		today := f.Notifications()[0]
		if again := f.Notifications()[0]; again.Fingerprint != today.Fingerprint {
			t.Fatalf("%s: the same day changed the fingerprint", today.Kind)
		}
		f.Today = "2026-09-24"
		if tomorrow := f.Notifications()[0]; tomorrow.Key != today.Key || tomorrow.Fingerprint == today.Fingerprint {
			t.Fatalf("%s: a second day without a backup was not a new reminder", today.Kind)
		}
	}
}

// TestCapitalIsTheOwnersAndBandsInFives: it discloses the shop's worth, and drifting within a band is not news.
// TestWhatTheShopOwesItsSuppliersComesOffItsWorth (0.10.0): a delivery on credit adds stock and adds a debt of the same
// size — the shop is no richer — and pounds owed to a supplier are pounds a falling pound makes cheaper.
func TestWhatTheShopOwesItsSuppliersComesOffItsWorth(t *testing.T) {
	then := domain.Snapshot{BusinessDate: "2026-08-24", RateNano: 15_000_000_000_000, StockUSDMinor: 100_000}
	now := then
	now.BusinessDate, now.StockUSDMinor, now.PayableUSDMinor = "2026-09-23", 150_000, 50_000
	if c := domain.CompareCapital(then, now, 0, 2); c.NowUSD != 100_000 || c.ChangeMicro != 0 || c.Notify {
		t.Fatalf("a delivery bought on credit made the shop richer: %+v", c)
	}
	// Owing 15,000,000 old pounds and holding 3,000,000: the pound falling 10% is the shop's gain, not its loss.
	then = domain.Snapshot{BusinessDate: "2026-08-24", RateNano: 15_000_000_000_000, StockUSDMinor: 200_000,
		CashLocalMinor: 3_000_000, PayableLocalMinor: 15_000_000}
	now = then
	now.BusinessDate, now.RateNano = "2026-09-23", 16_500_000_000_000
	c := domain.CompareCapital(then, now, 0, 2)
	if c.ThenUSD != 120_000 || c.DepreciationUSD >= 0 {
		t.Fatalf("then %d cents, lost to the rate %d — owing pounds is not holding them", c.ThenUSD, c.DepreciationUSD)
	}
}

func TestCapitalIsTheOwnersAndBandsInFives(t *testing.T) {
	mk := func(change int64) domain.Facts {
		return domain.Facts{BackupFound: true, Capital: &domain.Capital{Notify: true, ChangeMicro: change}}
	}
	six, seven, eleven := mk(-6_000_000).Notifications()[0], mk(-7_000_000).Notifications()[0], mk(-11_000_000).Notifications()[0]
	if !six.Owner {
		t.Fatal("the capital notification is not marked as the owner's")
	}
	if six.Fingerprint != seven.Fingerprint {
		t.Fatal("drifting from −6% to −7% made a new notification")
	}
	if six.Fingerprint == eleven.Fingerprint {
		t.Fatal("crossing −10% did not make a new notification")
	}
}

// TestAShopHoldingDollarsIsNotAlarmedByAFallingPound: the depreciation is on what was held in POUNDS. A shop that keeps
// its value in dollar stock and dollar cash loses nothing to the rate, and must not be told it did.
func TestAShopHoldingDollarsIsNotAlarmedByAFallingPound(t *testing.T) {
	then := domain.Snapshot{RateNano: 15_000_000_000_000, StockUSDMinor: 180_000, CashUSDMinor: 20_000}
	now := domain.Snapshot{RateNano: 16_500_000_000_000, StockUSDMinor: 180_000, CashUSDMinor: 20_000}
	c := domain.CompareCapital(then, now, 0, 2)
	if c.DepreciationUSD != 0 || c.Notify {
		t.Fatalf("a shop holding only dollars was told it lost %d cents to the pound", c.DepreciationUSD)
	}
}
