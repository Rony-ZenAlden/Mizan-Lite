package domain_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/reports/domain"
)

// TestTheLossReportReadsWhatWasLostAtWhatItCost (0.10.0): write-offs out and counts short, each at its own movement's
// cost and its own day's rate; stock put back and a count found over are not losses.
func TestTheLossReportReadsWhatWasLostAtWhatItCost(t *testing.T) {
	labneh, oil := id.ID("p-labneh"), id.ID("p-oil")
	products := map[id.ID]domain.Product{
		labneh: {ID: labneh, NameAR: "لبنة بلدية", UnitCode: "kg", UnitDecimals: 3},
		oil:    {ID: oil, NameAR: "زيت زيتون", UnitCode: "l", UnitDecimals: 3},
	}
	rates := []domain.Rate{
		{Seq: 1, Nano: 15_000_000_000_000, BusinessDate: "2026-09-01"},
		{Seq: 2, Nano: 16_000_000_000_000, BusinessDate: "2026-09-20"},
	}
	movements := []domain.Movement{
		// 1.5 kg of labneh gone bad at $4.00 a kilo: $6.00, 90,000 pounds at 15,000.
		{ID: "m1", ProductID: labneh, BusinessDate: "2026-09-10", Kind: domain.MoveAdjustment, Reason: domain.ReasonSpoiled,
			QuantityMicro: -1_500_000, UnitCostMicro: 4_000_000, Note: "من الحر"},
		// Two litres broken at $3.00, after the rate moved: $6.00, 96,000 pounds at 16,000.
		{ID: "m2", ProductID: oil, BusinessDate: "2026-09-21", Kind: domain.MoveAdjustment, Reason: domain.ReasonDamaged,
			QuantityMicro: -2_000_000, UnitCostMicro: 3_000_000},
		// A count one litre short.
		{ID: "m3", ProductID: oil, BusinessDate: "2026-09-22", Kind: domain.MoveCount, Reason: "count",
			QuantityMicro: -1_000_000, UnitCostMicro: 3_000_000},
		// Not losses: stock put back, a count over, a sale, and a write-off outside the period.
		{ID: "m4", ProductID: oil, BusinessDate: "2026-09-22", Kind: domain.MoveAdjustment, Reason: domain.ReasonOther, QuantityMicro: 1_000_000, UnitCostMicro: 3_000_000},
		{ID: "m5", ProductID: oil, BusinessDate: "2026-09-23", Kind: domain.MoveCount, Reason: "count", QuantityMicro: 500_000, UnitCostMicro: 3_000_000},
		{ID: "m6", ProductID: oil, BusinessDate: "2026-09-23", Kind: domain.MoveSale, QuantityMicro: -1_000_000, UnitCostMicro: 3_000_000},
		{ID: "m7", ProductID: oil, BusinessDate: "2026-08-31", Kind: domain.MoveAdjustment, Reason: domain.ReasonExpired, QuantityMicro: -1_000_000, UnitCostMicro: 3_000_000},
	}
	arrival := []domain.ArrivalDamage{{BusinessDate: "2026-09-05", PurchaseNo: 1, SupplierName: "المروى", ProductID: oil, DamagedMicro: 2_000_000, Currency: "USD", ValueMinor: 600}}
	r := domain.LossesBetween(movements, products, rates, pair, "2026-09-01", "2026-09-30", arrival)

	if len(r.Lines) != 3 || r.Lines[0].MovementID != "m3" || r.Lines[2].MovementID != "m1" {
		t.Fatalf("lines %+v", r.Lines)
	}
	spoiled := r.Lines[2]
	if spoiled.NameAR != "لبنة بلدية" || spoiled.QuantityMicro != 1_500_000 || spoiled.Value.USD != 600 || spoiled.Value.Local != 90_000 || spoiled.Note != "من الحر" {
		t.Fatalf("the spoiled labneh %+v", spoiled)
	}
	if r.Lines[1].Value.Local != 96_000 {
		t.Fatalf("the broken oil at the rate of its own day: %+v", r.Lines[1])
	}
	want := []domain.LossTotal{
		{Reason: domain.ReasonDamaged, Lines: 1, Value: domain.Converted{USD: 600, Local: 96_000}},
		{Reason: domain.ReasonSpoiled, Lines: 1, Value: domain.Converted{USD: 600, Local: 90_000}},
		{Reason: domain.LossShortfall, Lines: 1, Value: domain.Converted{USD: 300, Local: 48_000}},
	}
	if len(r.ByReason) != len(want) {
		t.Fatalf("by reason %+v", r.ByReason)
	}
	for i := range want {
		if r.ByReason[i] != want[i] {
			t.Fatalf("by reason %d = %+v, want %+v", i, r.ByReason[i], want[i])
		}
	}
	if r.Total != (domain.Converted{USD: 1_500, Local: 234_000}) {
		t.Fatalf("total %+v", r.Total)
	}
	if len(r.Arrival) != 1 || r.Arrival[0].ValueMinor != 600 {
		t.Fatalf("arrival %+v — goods the supplier did not charge for are shown, not lost", r.Arrival)
	}
}
