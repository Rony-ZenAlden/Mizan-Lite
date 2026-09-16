package domain_test

import (
	"testing"

	domain "github.com/mizan-erp/mizan/internal/lite/reports/domain"
)

// The shelf's rate: 15,000 pounds a dollar, as everywhere else in these tests.
var retailRates = []domain.Rate{{Seq: 1, Nano: 15_000_000_000_000, BusinessDate: "2026-09-01"}}

// TestTheShelfIsValuedAtCostAndAtRetail is the owner's request of 2026-09-17: what the stock cost, and what it would fetch
// if it all sold. The two differ by exactly the expected profit already reported, which is what makes them checkable.
func TestTheShelfIsValuedAtCostAndAtRetail(t *testing.T) {
	// Two litres on hand, bought at $2.00, priced at $3.25: cost 4.00, retail 6.50, profit 2.50.
	products := []domain.Product{{ID: pid(801), NameAR: "زيت", PriceCurrency: "USD", PriceMicro: 3_250_000, Active: true}}
	last := []domain.Movement{{ProductID: products[0].ID, OnHandAfterMicro: 2_000_000, AvgCostAfterMicro: 2_000_000}}

	s := domain.ShelfProfit(last, products, retailRates, pair)
	if len(s.Lines) != 1 {
		t.Fatalf("lines = %+v", s.Lines)
	}
	line := s.Lines[0]
	if line.RetailUSD != 650 {
		t.Fatalf("retail in dollars = %d, want 650", line.RetailUSD)
	}
	// 6.50 at 15,000 is 97,500 pounds.
	if line.RetailLocal != 97_500 {
		t.Fatalf("retail in pounds = %d, want 97,500", line.RetailLocal)
	}
	if s.RetailTotalUSD != 650 || s.RetailTotalLocal != 97_500 {
		t.Fatalf("shelf retail = %d / %d", s.RetailTotalUSD, s.RetailTotalLocal)
	}
	// Retail − profit is what the stock cost: 6.50 − 2.50 = 4.00.
	if s.RetailTotalUSD-s.TotalUSD != 400 {
		t.Fatalf("retail %d less profit %d is not the cost 400", s.RetailTotalUSD, s.TotalUSD)
	}
}

// A product priced in pounds values in pounds exactly, and in dollars through the rate.
func TestAPoundPricedShelfLineValuesBothWays(t *testing.T) {
	products := []domain.Product{{ID: pid(802), NameAR: "برغل", PriceCurrency: "SYP", PriceMicro: 18_000 * 1_000_000, Active: true}}
	last := []domain.Movement{{ProductID: products[0].ID, OnHandAfterMicro: 3_000_000, AvgCostAfterMicro: 900_000}}

	s := domain.ShelfProfit(last, products, retailRates, pair)
	line := s.Lines[0]
	// 3 kg at 18,000 = 54,000 pounds, which at 15,000 is $3.60.
	if line.RetailLocal != 54_000 || line.RetailUSD != 360 {
		t.Fatalf("a pound-priced line = %d pounds / %d dollars", line.RetailLocal, line.RetailUSD)
	}
}
