package api_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/api"
)

// TestGoingOverToDollarsOnlyThroughTheBindings (0.10.0, "convert everything"): the plan the owner reads, the switch
// that writes it, and a shop that then takes pounds nowhere — until it leaves dollars-only again.
func TestGoingOverToDollarsOnlyThroughTheBindings(t *testing.T) {
	set, oil := tillShop(t) // olive oil at $3.25, the rate 15,000
	elevate(t, set)
	labneh := set.Catalog.CreateProduct(api.CreateProductInput{NameAR: "لبنة", UnitCode: "kg", PriceCurrency: "SYP", Price: "60000", CostPrice: "45000"})
	if !labneh.OK {
		t.Fatal(labneh.Error)
	}
	samir := set.Customers.Create(api.CustomerInput{Name: "سمير"}).Data
	if r := set.Customers.Opening(api.DebtAmountInput{CustomerID: samir.ID, Currency: "SYP", Amount: "150000"}); !r.OK {
		t.Fatal(r.Error)
	}

	// A plain settings change cannot reach dollars-only: going over converts first.
	usd := "usd"
	if r := set.Settings.Update(api.SettingsInput{MoneyDisplay: &usd}); r.OK || r.Error.Code != api.CodeUSDOnlyByConversion {
		t.Fatalf("usd as a plain setting = %+v", r)
	}
	plan := set.Settings.USDOnlyPlan()
	if !plan.OK {
		t.Fatal(plan.Error)
	}
	p := plan.Data
	if p.LocalCurrency != "SYP" || p.Rate != "15000" || len(p.Prices) != 1 || p.Prices[0].USDPrice != "4.00" || p.Prices[0].USDCost != "3.00" ||
		p.Prices[0].LocalPrice != "60000" || len(p.Customers) != 1 || p.Customers[0].Dollars != "10.00" || p.Token == "" {
		t.Fatalf("the plan = %+v", p)
	}
	if r := set.Settings.SwitchToUSDOnly("another plan"); r.OK || r.Error.Code != "lite.usdmode.plan_changed" {
		t.Fatalf("a confirmation of another plan = %+v", r)
	}
	switched := set.Settings.SwitchToUSDOnly(p.Token)
	if !switched.OK || switched.Data.MoneyDisplay != "usd" {
		t.Fatalf("SwitchToUSDOnly = %+v", switched)
	}
	if rate := set.FX.Current(); !rate.OK || !rate.Data.USDOnly {
		t.Fatalf("the rate reading does not say dollars only: %+v", rate)
	}

	// The till sells in dollars by default and takes no pounds.
	cart := api.CartInput{Lines: []api.CartLineInput{{ProductID: labneh.Data.ID, Quantity: "1"}}}
	q := set.Till.Quote(cart)
	if !q.OK || q.Data.Settlement != "USD" || q.Data.Total != "4.00" {
		t.Fatalf("a dollars-only quote = %+v", q)
	}
	cart.Settlement = "SYP"
	if r := set.Till.Quote(cart); r.OK || r.Error.Code != "lite.usdmode.local_currency_off" {
		t.Fatalf("a pound sale in a dollars-only shop = %+v", r)
	}
	// Pounds refused at every other door, too.
	for name, r := range map[string]bool{
		"a pound price": set.Catalog.CreateProduct(api.CreateProductInput{NameAR: "جبنة", UnitCode: "kg", PriceCurrency: "SYP", Price: "1000"}).OK,
		"a pound delivery": set.Stock.Receive(api.ReceiveInput{ProductID: oil.ID, Quantity: "1", CostMode: "total", Cost: "15000", Currency: "SYP",
			Rate: "15000"}).OK,
		"a pound expense":  set.Cash.Record(api.CashRecordInput{Kind: "expense", Currency: "SYP", Amount: "1000", Category: "other", FromDrawer: true, Note: "x"}).OK,
		"a pound opening":  set.Customers.Opening(api.DebtAmountInput{CustomerID: samir.ID, Currency: "SYP", Amount: "1000"}).OK,
		"a pound purchase": set.Suppliers.QuotePurchase(api.PurchaseInput{Currency: "SYP"}).OK,
		"a pound count":    set.Cash.Count(api.CashCountInput{Currency: "SYP", Counted: "0"}).OK,
	} {
		if r {
			t.Errorf("%s was taken in a dollars-only shop", name)
		}
	}
	// A product created with no currency named is a dollar product.
	if made := set.Catalog.CreateProduct(api.CreateProductInput{NameAR: "جبنة", UnitCode: "kg", Price: "5"}); !made.OK || made.Data.PriceCurrency != "USD" {
		t.Fatalf("a new product in a dollars-only shop = %+v", made)
	}
	if statement := set.Customers.Statement(api.StatementQueryDTO{CustomerID: samir.ID, Currency: "USD"}); !statement.OK ||
		statement.Data.Balance != "10.00" || statement.Data.Entries[0].Kind != "conversion" || statement.Data.Entries[0].Reversible {
		t.Fatalf("Samir's dollar statement = %+v", statement)
	}

	// Leaving dollars-only is a plain setting: every figure is in dollars already.
	legacy := "legacy"
	if r := set.Settings.Update(api.SettingsInput{MoneyDisplay: &legacy}); !r.OK {
		t.Fatalf("leaving dollars-only = %+v", r)
	}
	if r := set.Cash.Record(api.CashRecordInput{Kind: "expense", Currency: "SYP", Amount: "1000", Category: "other", FromDrawer: true, Note: "x"}); !r.OK {
		t.Fatalf("pounds again after leaving = %+v", r)
	}
}
