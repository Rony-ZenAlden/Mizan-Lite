package bootstrap_test

import (
	"context"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	catalogdomain "github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	customersdomain "github.com/mizan-erp/mizan/internal/lite/customers/domain"
	ownerdomain "github.com/mizan-erp/mizan/internal/lite/owner/domain"
	"github.com/mizan-erp/mizan/internal/lite/sales"
	salesdomain "github.com/mizan-erp/mizan/internal/lite/sales/domain"
	"github.com/mizan-erp/mizan/internal/lite/stock"
	stockdomain "github.com/mizan-erp/mizan/internal/lite/stock/domain"
	"github.com/mizan-erp/mizan/internal/lite/suppliers"
	suppliersdomain "github.com/mizan-erp/mizan/internal/lite/suppliers/domain"
	"github.com/mizan-erp/mizan/internal/lite/usdmode"
	usdmodedomain "github.com/mizan-erp/mizan/internal/lite/usdmode/domain"
)

// TestAShopGoesOverToDollarsOnly proves the conversion's adapters on the real graph (0.10.0, "convert everything"): a
// pound price and its cost, the Misc item, a customer owing pounds, a supplier owed pounds and the pounds in the drawer —
// every one in dollars at the rate in force, the setting last, one owner's act, and every verifier clean after.
func TestAShopGoesOverToDollarsOnly(t *testing.T) {
	ctx := context.Background()
	app, oil := startFast(t) // olive oil at $3.25; the rate 15,000
	if usdmode.CodeOwnerRequired != ownerdomain.CodeRequired {
		t.Fatal("the conversion's refusal is not the owner's")
	}
	labneh, err := app.Catalog.Create(ctx, catalogdomain.Draft{NameAR: "لبنة", UnitCode: "kg", PriceCurrency: "SYP", Price: "60000", Cost: "45000"})
	if err != nil {
		t.Fatal(err)
	}
	misc, err := app.Catalog.OpenItem(ctx, "SYP")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.Stock.Opening(ctx, stock.ReceiveInput{ProductID: labneh.ID, Quantity: "10",
		Cost: stockdomain.CostInput{Amount: "450000", Currency: "SYP", Rate: "15000"}}); err != nil {
		t.Fatal(err)
	}
	// A cash sale of 2 kg in pounds — 120,000 in the drawer — and 1 kg on credit to Samir, in pounds.
	cash := salesdomain.CartInput{Lines: []salesdomain.LineInput{{ProductID: labneh.ID, Quantity: "2"}}}
	q, err := app.Sales.Quote(ctx, cash)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.Sales.Checkout(ctx, sales.CheckoutInput{Cart: cash, Token: q.Token}); err != nil {
		t.Fatal(err)
	}
	samir, err := app.Customers.Create(ctx, customersdomain.Draft{Name: "سمير"})
	if err != nil {
		t.Fatal(err)
	}
	credit := salesdomain.CartInput{Lines: []salesdomain.LineInput{{ProductID: labneh.ID, Quantity: "1"}}, Payment: salesdomain.PaymentCredit,
		CustomerID: samir.ID, Settlement: "SYP", TenderCurrency: "SYP", Tendered: "0"}
	if q, err = app.Sales.Quote(ctx, credit); err != nil {
		t.Fatal(err)
	}
	if _, err = app.Sales.Checkout(ctx, sales.CheckoutInput{Cart: credit, Token: q.Token}); err != nil {
		t.Fatal(err)
	}
	marwa, err := app.Suppliers.Create(ctx, suppliersdomain.Draft{Name: "المروى"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.Suppliers.Opening(ctx, suppliers.MoneyInput{SupplierID: marwa.ID, Currency: "SYP", Amount: "300000"}); err != nil {
		t.Fatal(err)
	}

	plan, err := app.USDMode.Plan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Prices) != 1 || plan.Prices[0].USDPriceMicro != 4_000_000 || plan.Prices[0].USDCostMicro != 3_000_000 ||
		len(plan.OpenItems) != 1 || len(plan.Customers) != 1 || plan.Customers[0].USDMinor != 400 ||
		len(plan.Suppliers) != 1 || plan.Suppliers[0].USDMinor != 2_000 || plan.Drawer.LocalMinor != 120_000 || plan.Drawer.USDMinor != 800 {
		t.Fatalf("the plan %+v", plan)
	}
	if _, err = app.USDMode.Apply(ctx, "not-the-plan"); errs.CodeOf(err) != usdmodedomain.CodePlanChanged {
		t.Fatalf("a confirmation of another plan: %v", err)
	}
	if _, err = app.USDMode.Apply(ctx, plan.Token); err != nil {
		t.Fatal(err)
	}

	after, _ := app.Catalog.Get(ctx, labneh.ID)
	if after.PriceCurrency != "USD" || after.PriceMicro != 4_000_000 || !after.HasCost || after.CostMicro != 3_000_000 {
		t.Fatalf("labneh after %+v — 60,000 and 45,000 pounds are $4.00 and $3.00", after)
	}
	if m, _ := app.Catalog.Get(ctx, misc.ID); m.PriceCurrency != "USD" || !m.OpenPrice {
		t.Fatalf("the Misc item after %+v", m)
	}
	if o, _ := app.Catalog.Get(ctx, oil.ID); o.PriceCurrency != "USD" || o.PriceMicro != 3_250_000 {
		t.Fatalf("a dollar price was touched: %+v", o)
	}
	if balances, _ := app.Customers.Balances(ctx, samir.ID); balances["SYP"] != 0 || balances["USD"] != 400 {
		t.Fatalf("Samir after %+v", balances)
	}
	if owed, _ := app.Suppliers.Payables(ctx); owed["SYP"] != 0 || owed["USD"] != 2_000 {
		t.Fatalf("payables after %+v", owed)
	}
	today := app.Reports.Today()
	if local, _ := app.Reports.ExpectedCash(ctx, today, "SYP"); local != 0 {
		t.Fatalf("pounds left in the drawer: %d", local)
	}
	if usd, _ := app.Reports.ExpectedCash(ctx, today, "USD"); usd != 800 {
		t.Fatalf("dollars in the drawer: %d", usd)
	}
	if s, _ := app.Settings.Get(ctx); s.MoneyDisplay != "usd" {
		t.Fatalf("the setting after: %q", s.MoneyDisplay)
	}
	events, _ := app.Owner.Events(ctx, 200)
	switched := 0
	for _, e := range events {
		if e.Kind == ownerdomain.EventGuardedAct && e.Action == usdmode.ActSwitch {
			switched++
		}
	}
	if switched != 1 {
		t.Fatalf("the going-over recorded %d times", switched)
	}
	for name, check := range map[string]func(context.Context) (int, error){
		"sales": func(ctx context.Context) (int, error) { f, verr := app.Sales.VerifyUnguarded(ctx); return len(f), verr },
		"stock": func(ctx context.Context) (int, error) { f, verr := app.Stock.VerifyUnguarded(ctx); return len(f), verr },
		"customers": func(ctx context.Context) (int, error) {
			f, verr := app.Customers.VerifyUnguarded(ctx)
			return len(f), verr
		},
	} {
		if n, verifyErr := check(ctx); verifyErr != nil || n != 0 {
			t.Errorf("%s verifier after the going-over: %d findings, %v", name, n, verifyErr)
		}
	}
	if _, err = app.USDMode.Plan(ctx); errs.CodeOf(err) != usdmodedomain.CodeAlreadyUSDOnly {
		t.Fatalf("a second going-over: %v", err)
	}
}
