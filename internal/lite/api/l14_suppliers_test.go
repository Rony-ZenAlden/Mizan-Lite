package api_test

import (
	"context"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/api"
	"github.com/mizan-erp/mizan/internal/lite/moneyfmt"
)

// TestSuppliersThroughTheBindings is the payables book as the screen drives it (0.10.0), in a shop reading the new pound:
// what is typed is the new figure and is kept in the old one, and what comes back is the new figure again.
func TestSuppliersThroughTheBindings(t *testing.T) {
	set, oil := tillShop(t) // olive oil, the rate 15,000
	setDisplay(t, set, moneyfmt.New)
	marwa := set.Suppliers.Create(api.SupplierInput{Name: "المروى", Phone: "0988 703 785", City: "حلب"})
	if !marwa.OK {
		t.Fatal(marwa.Error)
	}
	supplierID := marwa.Data.ID

	// Four litres at 300 new pounds, 20 off the line, at 150 to the dollar; 500 paid from the drawer.
	in := api.PurchaseInput{SupplierID: supplierID, Currency: "SYP", Rate: "150", SupplierRef: "2970", PaidNow: "500", PaidFrom: "drawer",
		Lines: []api.PurchaseLineInput{{ProductID: oil.ID, Quantity: "4", UnitCost: "300", DiscountAmount: "20"}}}
	q := set.Suppliers.QuotePurchase(in)
	if !q.OK {
		t.Fatal(q.Error)
	}
	p := q.Data.Purchase
	if p.Gross != "1200" || p.LineDiscount != "20" || p.Due != "1180" || p.PaidNow != "500" || p.Rate != "150" ||
		q.Data.BalanceBefore != "0" || q.Data.BalanceAfter != "680" || p.Lines[0].NetUnitCost != "295" || p.Lines[0].Good != "4.000" {
		t.Fatalf("the quote = %+v, before %s after %s", p, q.Data.BalanceBefore, q.Data.BalanceAfter)
	}
	recorded := set.Suppliers.RecordPurchase(in)
	if !recorded.OK || recorded.Data.PurchaseNo != 1 || recorded.Data.Status != "posted" {
		t.Fatalf("RecordPurchase = %+v", recorded)
	}

	// The books hold old pounds: 118,000 for four litres at 15,000 — $1.966667 a litre.
	ctx := context.Background()
	graph := api.Graph(set)
	oilID, err := id.Parse(oil.ID)
	if err != nil {
		t.Fatal(err)
	}
	level, err := graph.Stock.Stocked(ctx, oilID)
	if err != nil || level.OnHandMicro != 4_000_000 || level.AvgCostMicro != 1_966_667 {
		t.Fatalf("stock %+v, %v — the new pounds typed were not taken back into old ones", level, err)
	}
	var due int64
	if err = graph.DB.Reader(ctx).QueryRowContext(ctx, `SELECT amount_minor FROM supplier_entries WHERE kind = 'purchase'`).Scan(&due); err != nil || due != 118_000 {
		t.Fatalf("the book holds %d (%v), want 118000 old pounds", due, err)
	}

	st := set.Suppliers.Statement(api.SupplierStatementQueryDTO{SupplierID: supplierID, Currency: "SYP"})
	if !st.OK || st.Data.Balance != "680" || len(st.Data.Entries) != 2 {
		t.Fatalf("Statement = %+v", st)
	}
	if paid, bought := st.Data.Entries[0], st.Data.Entries[1]; paid.Kind != "payment" || paid.Amount != "-500" || paid.Source != "drawer" ||
		!paid.Reversible || bought.Kind != "purchase" || bought.PurchaseNo != 1 || bought.SupplierRef != "2970" || bought.Reversible {
		t.Fatalf("entries %+v", st.Data.Entries)
	}
	list := set.Suppliers.List(api.SupplierQueryDTO{})
	if !list.OK || len(list.Data.Suppliers) != 1 || list.Data.Rate != "150" || len(list.Data.Totals) != 1 || list.Data.Totals[0].Balance != "680" {
		t.Fatalf("List = %+v", list)
	}

	// 180 from the owner's own pocket, reversed; an opening balance in dollars the other way, refunded into the drawer.
	pay := set.Suppliers.Pay(api.SupplierMoneyInput{SupplierID: supplierID, Currency: "SYP", Amount: "180", Source: "owner"})
	if !pay.OK || pay.Data.BalanceAfter != "500" {
		t.Fatalf("Pay = %+v", pay)
	}
	if r := set.Suppliers.Reverse(api.SupplierReverseInput{EntryID: pay.Data.ID, Reason: "دفعة مكررة"}); !r.OK || r.Data.BalanceAfter != "680" {
		t.Fatalf("Reverse = %+v", r)
	}
	opening := set.Suppliers.Opening(api.SupplierMoneyInput{SupplierID: supplierID, Currency: "USD", Amount: "25", InShopsFavour: true})
	if !opening.OK || opening.Data.BalanceAfter != "-25.00" {
		t.Fatalf("Opening = %+v", opening)
	}
	if r := set.Suppliers.Refund(api.SupplierMoneyInput{SupplierID: supplierID, Currency: "USD", Amount: "25", Source: "drawer"}); !r.OK || r.Data.BalanceAfter != "0.00" {
		t.Fatalf("Refund = %+v", r)
	}

	// The void takes the four litres back out, and the 500 paid is the supplier's to give back.
	voided := set.Suppliers.VoidPurchase(api.VoidPurchaseInput{PurchaseID: recorded.Data.ID, Reason: "فاتورة مكررة"})
	if !voided.OK || voided.Data.Status != "voided" || voided.Data.VoidReason != "فاتورة مكررة" {
		t.Fatalf("VoidPurchase = %+v", voided)
	}
	if st = set.Suppliers.Statement(api.SupplierStatementQueryDTO{SupplierID: supplierID, Currency: "SYP"}); st.Data.Balance != "-500" {
		t.Fatalf("after the void the book reads %s", st.Data.Balance)
	}
	if all := set.Suppliers.Purchases(api.PurchaseQueryDTO{SupplierID: supplierID}); !all.OK || len(all.Data) != 1 || all.Data[0].Status != "voided" {
		t.Fatalf("Purchases = %+v", all)
	}
	if one := set.Suppliers.Purchase(recorded.Data.ID); !one.OK || one.Data.Lines[0].NameAR != "زيت زيتون" {
		t.Fatalf("Purchase = %+v", one)
	}
	if missing := set.Suppliers.Purchase("not-an-id"); missing.OK || missing.Error.Code != "lite.suppliers.purchase_not_found" {
		t.Fatalf("an unreadable purchase id = %+v", missing)
	}
}

// TestWithThePINOnTheSuppliersScreenIsTheOwners: a counter with the PIN switched on reads no supplier, and the owner in
// owner mode reads them all.
func TestWithThePINOnTheSuppliersScreenIsTheOwners(t *testing.T) {
	set, _ := tillShop(t)
	if r := set.Suppliers.Create(api.SupplierInput{Name: "المروى"}); !r.OK {
		t.Fatal(r.Error)
	}
	on := true
	if r := set.Settings.Update(api.SettingsInput{PINRequired: &on}); !r.OK {
		t.Fatal(r.Error)
	}
	set.Owner.EndElevation()
	if r := set.Suppliers.List(api.SupplierQueryDTO{}); r.OK || r.Error.Code != "lite.owner.required" {
		t.Fatalf("List outside owner mode = %+v", r)
	}
	elevate(t, set)
	if r := set.Suppliers.List(api.SupplierQueryDTO{}); !r.OK || len(r.Data.Suppliers) != 1 {
		t.Fatalf("List in owner mode = %+v", r)
	}
}
