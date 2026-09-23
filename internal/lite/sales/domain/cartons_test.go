package domain_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/sales/domain"
)

// TestCartonsAreCountedInTenthsHalfUp: the invoice's طرد column — 6 dozen of a 6-dozen carton is 1.0, 15 sets of a
// 3-set carton 5.0, and a part carton rounds to the nearest tenth, half up.
func TestCartonsAreCountedInTenthsHalfUp(t *testing.T) {
	for _, c := range []struct {
		qty, carton, want int64
	}{
		{6_000_000, 6_000_000, 10},
		{15_000_000, 3_000_000, 50},
		{48_000_000, 24_000_000, 20},
		{7_000_000, 6_000_000, 12}, // 1.1666… → 1.2
		{1_000_000, 4_000_000, 3},  // 0.25 → 0.3, half up
		{1_250_000, 2_500_000, 5},  // 0.5 of a 2.5 kg sack
	} {
		got, ok := domain.Line{QuantityMicro: c.qty, UnitsPerCartonMicro: c.carton}.Cartons()
		if !ok || got != c.want {
			t.Errorf("%d of %d = %d tenths (%v), want %d", c.qty, c.carton, got, ok, c.want)
		}
	}
	if _, ok := (domain.Line{QuantityMicro: 1_000_000}).Cartons(); ok {
		t.Error("a line with no carton size has a carton count")
	}
}

func TestASalesCartonsAreTheSumOfTheLinesThatHaveThem(t *testing.T) {
	sale := domain.Sale{Lines: []domain.Line{
		{QuantityMicro: 6_000_000, UnitsPerCartonMicro: 6_000_000},
		{QuantityMicro: 15_000_000, UnitsPerCartonMicro: 3_000_000},
		{QuantityMicro: 2_000_000}, // no carton size: not counted, not zero
	}}
	if got, ok := sale.Cartons(); !ok || got != 60 {
		t.Fatalf("cartons = %d (%v), want 60 tenths", got, ok)
	}
	if _, ok := (domain.Sale{Lines: []domain.Line{{QuantityMicro: 1}}}).Cartons(); ok {
		t.Fatal("a sale with no carton sizes has a carton count")
	}
}
