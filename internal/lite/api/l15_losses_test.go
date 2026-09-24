package api_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/api"
	"github.com/mizan-erp/mizan/internal/lite/moneyfmt"
)

// TestSpoilageAndItsReportThroughTheBindings (0.10.0): stock written off as spoiled through the stock book's own
// write-off, read back on the loss report at cost in both currencies, beside goods that arrived damaged from a supplier.
func TestSpoilageAndItsReportThroughTheBindings(t *testing.T) {
	set, oil := tillShop(t) // olive oil, the rate 15,000
	elevate(t, set)
	if r := set.Stock.Opening(api.ReceiveInput{ProductID: oil.ID, Quantity: "10", CostMode: "total", Cost: "20", Currency: "USD"}); !r.OK {
		t.Fatal(r.Error)
	}
	if r := set.Stock.Adjust(api.AdjustInput{ProductID: oil.ID, Direction: "out", Quantity: "1.5", Reason: "spoiled", Note: "من الحر"}); !r.OK {
		t.Fatal(r.Error)
	}
	marwa := set.Suppliers.Create(api.SupplierInput{Name: "المروى"}).Data
	if r := set.Suppliers.RecordPurchase(api.PurchaseInput{SupplierID: marwa.ID, Currency: "USD",
		Lines: []api.PurchaseLineInput{{ProductID: oil.ID, Quantity: "4", Damaged: "1", UnitCost: "2.50"}}}); !r.OK {
		t.Fatal(r.Error)
	}
	setDisplay(t, set, moneyfmt.New)

	report := set.Reports.Losses(api.RangeInput{})
	if !report.OK {
		t.Fatal(report.Error)
	}
	r := report.Data
	// 1.5 L at $2.00: $3.00, which at 15,000 is 45,000 old pounds — 450 new.
	if len(r.Lines) != 1 || r.Lines[0].Reason != "spoiled" || r.Lines[0].Quantity != "1.500" || r.Lines[0].Value.USD != "3.00" ||
		r.Lines[0].Value.Local != "450" || r.Lines[0].Note != "من الحر" || r.Lines[0].NameAR != "زيت زيتون" {
		t.Fatalf("lines %+v", r.Lines)
	}
	if len(r.ByReason) != 1 || r.ByReason[0].Reason != "spoiled" || r.Total.USD != "3.00" {
		t.Fatalf("by reason %+v, total %+v", r.ByReason, r.Total)
	}
	if len(r.Arrival) != 1 || r.Arrival[0].Damaged != "1.000" || r.Arrival[0].Value != "2.50" || r.Arrival[0].SupplierName != "المروى" {
		t.Fatalf("arrival %+v", r.Arrival)
	}
	if bad := set.Stock.Adjust(api.AdjustInput{ProductID: oil.ID, Direction: "out", Quantity: "1", Reason: "rotten"}); bad.OK || bad.Error.Code != "lite.stock.unknown_reason" {
		t.Fatalf("an unknown reason = %+v", bad)
	}
}
