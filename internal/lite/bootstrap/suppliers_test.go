package bootstrap_test

import (
	"context"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	ownerdomain "github.com/mizan-erp/mizan/internal/lite/owner/domain"
	"github.com/mizan-erp/mizan/internal/lite/suppliers"
	suppliersdomain "github.com/mizan-erp/mizan/internal/lite/suppliers/domain"
)

// TestAPurchaseIsReceivedIntoTheRealStockBook proves the payables book's adapters on the real graph (0.10.0): the good
// units of a delivery received at what they cost after every discount, in dollars and in pounds at the purchase's rate;
// the drawer short by what it paid the supplier; the notification engine's worth less what the shop owes; and a void
// taking the goods back out while it still can.
func TestAPurchaseIsReceivedIntoTheRealStockBook(t *testing.T) {
	ctx := context.Background()
	app, oil := startFast(t) // olive oil, sold by the litre
	if suppliers.CodeOwnerRequired != ownerdomain.CodeRequired {
		t.Fatal("the payables book's refusal is not the owner's")
	}
	marwa, err := app.Suppliers.Create(ctx, suppliersdomain.Draft{Name: "المروى", City: "حلب"})
	if err != nil {
		t.Fatal(err)
	}

	// Ten litres at $2.00, two broken, 10% off the line and 40 cents off the invoice: eight litres for $14.00 — $1.75 each.
	// Five dollars paid from the drawer on the spot.
	first, err := app.Suppliers.RecordPurchase(ctx, suppliersdomain.Input{SupplierID: marwa.ID, Currency: "USD", SupplierRef: "2970",
		InvoiceDiscount: "0.40", PaidNow: "5", PaidFrom: "drawer",
		Lines: []suppliersdomain.LineInput{{ProductID: oil.ID, Quantity: "10", Damaged: "2", UnitCost: "2", DiscountPercent: "10"}}})
	if err != nil {
		t.Fatal(err)
	}
	if first.DueMinor() != 1_400 {
		t.Fatalf("the purchase came to %d cents, want $14.00", first.DueMinor())
	}
	level, err := app.Stock.Stocked(ctx, oil.ID)
	if err != nil || level.OnHandMicro != 8_000_000 || level.AvgCostMicro != 1_750_000 {
		t.Fatalf("stock after the delivery: %+v, %v — want 8 L at $1.75", level, err)
	}
	today := app.Reports.Today()
	if cash, _ := app.Reports.ExpectedCash(ctx, today, "USD"); cash != -500 {
		t.Fatalf("the drawer expects %d cents — the $5.00 paid to the supplier left it", cash)
	}

	// Four litres at 30,000 pounds at 15,000 to the dollar: $2.00 each, on credit.
	second, err := app.Suppliers.RecordPurchase(ctx, suppliersdomain.Input{SupplierID: marwa.ID, Currency: "SYP", Rate: "15000",
		Lines: []suppliersdomain.LineInput{{ProductID: oil.ID, Quantity: "4", UnitCost: "30000"}}})
	if err != nil {
		t.Fatal(err)
	}
	level, _ = app.Stock.Stocked(ctx, oil.ID)
	if level.OnHandMicro != 12_000_000 || level.AvgCostMicro <= 1_750_000 || level.AvgCostMicro >= 2_000_000 {
		t.Fatalf("stock after the pounds delivery: %+v", level)
	}
	payables, _ := app.Suppliers.Payables(ctx)
	if payables["USD"] != 900 || payables["SYP"] != 120_000 {
		t.Fatalf("payables %+v", payables)
	}
	if _, err = app.Alerts.Current(ctx); err != nil {
		t.Fatal(err)
	}
	var payableUSD, payableLocal int64
	if err = app.DB.Reader(ctx).QueryRowContext(ctx, `SELECT payable_usd_minor, payable_local_minor FROM capital_snapshots WHERE business_date = ?`,
		today).Scan(&payableUSD, &payableLocal); err != nil || payableUSD != 900 || payableLocal != 120_000 {
		t.Fatalf("today's worth kept payables of %d and %d (%v)", payableUSD, payableLocal, err)
	}

	// The pounds delivery is the oil's newest movement, so it can be voided; the dollar one no longer can.
	if _, err = app.Suppliers.VoidPurchase(ctx, second.ID, "entered twice"); err != nil {
		t.Fatal(err)
	}
	level, _ = app.Stock.Stocked(ctx, oil.ID)
	if level.OnHandMicro != 8_000_000 || level.AvgCostMicro != 1_750_000 {
		t.Fatalf("stock after the void: %+v", level)
	}
	_, err = app.Suppliers.VoidPurchase(ctx, first.ID, "wrong supplier")
	if typed, _ := errs.AsError(err); errs.CodeOf(err) != suppliersdomain.CodeLineNotReversible || typed.Params["line"] != "1" {
		t.Fatalf("voiding a delivery whose stock has moved since: %v", err)
	}
	if st, _ := app.Suppliers.StatementOf(ctx, marwa.ID, "SYP"); st.BalanceMinor != 0 || len(st.Entries) != 2 {
		t.Fatalf("the pounds book after the void: %+v", st)
	}
}
