package api_test

import (
	"context"
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/api"
	"github.com/mizan-erp/mizan/internal/lite/moneyfmt"
)

// TestEveryTypedAmountIsTakenBackThroughThePipeline is the INPUT half of the redenomination gate (L10), which 0.9.5
// shipped without.
//
// The output half proves every figure a screen reads has dropped its two noughts. This proves the other direction:
// every amount a person TYPES in the new pound reaches the books as the old pound. Until 0.9.9 the till's tender and
// sale discount, every debt-book amount, and a delivery's cost and rate went straight through — so a cashier typing
// 500 paid-now on a credit sale was recorded as having paid 500 old pounds, and the customer owed a hundred times too
// much. Silently: it was a valid number.
//
// Each case types a figure in the new pound, then reads the books in the old one.
func TestEveryTypedAmountIsTakenBackThroughThePipeline(t *testing.T) {
	set, oil := tillShop(t) // olive oil at $3.25; the rate 15,000 (150 new)
	elevate(t, set)
	abu := set.Customers.Create(api.CustomerInput{Name: "أبو محمد"}).Data
	setDisplay(t, set, moneyfmt.New)

	// ── A delivery in pounds: 10 L for 200 new pounds, at the rate the form shows — 150 — which is 15,000 old.
	if r := set.Stock.Opening(api.ReceiveInput{ProductID: oil.ID, Quantity: "10", CostMode: "total", Cost: "200",
		Currency: "SYP", Rate: "150"}); !r.OK {
		t.Fatalf("a delivery typed in new pounds was refused: %+v", r.Error)
	}
	ctx := context.Background()
	var enteredCost, enteredRate int64
	if err := api.Graph(set).DB.Reader(ctx).QueryRowContext(ctx,
		`SELECT entered_unit_cost_micro, local_per_usd_nano FROM stock_ledger WHERE product_id = ? AND kind = 'opening'`,
		oil.ID).Scan(&enteredCost, &enteredRate); err != nil {
		t.Fatal(err)
	}
	// 200 new pounds for 10 is 20 new = 2,000 old a litre; the rate 150 new = 15,000 old.
	if enteredCost != 2_000_000_000 || enteredRate != 15_000_000_000_000 {
		t.Fatalf("the ledger holds a cost of %d micro at a rate of %d nano — want 2,000 old pounds at 15,000", enteredCost, enteredRate)
	}

	// ── A credit sale of 2 L ($6.50 = 97,500 old = 975 new), 500 new pounds paid now: the debt is 475 new = 47,500 old.
	cart := api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "2"}}, Payment: "credit",
		CustomerID: abu.ID, Tendered: "500", TenderCurrency: "SYP", Settlement: "SYP"}
	sale := set.Till.Checkout(api.CheckoutInput{Cart: cart, Token: quoted(t, set, cart).Token})
	if !sale.OK {
		t.Fatal(sale.Error)
	}
	if got := balanceOf(t, set, abu.ID, "SYP"); got != "475" {
		t.Fatalf("after 500 new pounds paid on a 975 sale the customer owes %q new pounds — want 475", got)
	}

	// ── A whole-sale discount of 10 new pounds is 1,000 old: a 975 cash sale becomes 965.
	discounted := api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "2"}}, SaleDiscount: "10", Settlement: "SYP"}
	if q := set.Till.Quote(discounted); !q.OK || q.Data.Total != "965" {
		t.Fatalf("a 10-pound discount on 975 = %+v, want 965", q)
	}

	// ── A repayment of 75 new pounds: 475 − 75 = 400.
	pay := api.PaymentInput{CustomerID: abu.ID, Currency: "SYP", Amount: "75"}
	quote := set.Customers.QuotePayment(pay)
	if !quote.OK {
		t.Fatal(quote.Error)
	}
	pay.Token = quote.Data.Token
	if r := set.Customers.RecordPayment(pay); !r.OK {
		t.Fatal(r.Error)
	}
	if got := balanceOf(t, set, abu.ID, "SYP"); got != "400" {
		t.Fatalf("after a 75-pound repayment the customer owes %q — want 400", got)
	}

	// ── An opening debt of 100 and a write-off of 50: 400 + 100 − 50 = 450.
	if r := set.Customers.Opening(api.DebtAmountInput{CustomerID: abu.ID, Currency: "SYP", Amount: "100"}); !r.OK {
		t.Fatal(r.Error)
	}
	if r := set.Customers.WriteOff(api.DebtAmountInput{CustomerID: abu.ID, Currency: "SYP", Amount: "50", Note: "سامحناه"}); !r.OK {
		t.Fatal(r.Error)
	}
	if got := balanceOf(t, set, abu.ID, "SYP"); got != "450" {
		t.Fatalf("after an opening of 100 and a write-off of 50 the customer owes %q — want 450", got)
	}

	// ── And the books hold all of it in the old pound.
	setDisplay(t, set, moneyfmt.Legacy)
	if got := balanceOf(t, set, abu.ID, "SYP"); got != "45000" {
		t.Fatalf("the books hold a balance of %q — want 45,000 old pounds", got)
	}
}

// TestAnOpenItemIsPricedAsTypedAndMovesNoStock: the Misc button's first press creates the item; a carrier bag typed at
// 5 new pounds is 500 old, charged exactly, and nothing leaves any shelf.
func TestAnOpenItemIsPricedAsTypedAndMovesNoStock(t *testing.T) {
	set, _ := tillShop(t)
	setDisplay(t, set, moneyfmt.New)
	item := set.Till.OpenItem()
	if !item.OK || !item.Data.OpenPrice {
		t.Fatalf("the Misc item = %+v", item)
	}
	// Asked again, the same item — not a second one.
	if again := set.Till.OpenItem(); again.Data.ID != item.Data.ID {
		t.Fatal("a second Misc item was created")
	}
	cart := api.CartInput{Lines: []api.CartLineInput{{ProductID: item.Data.ID, Quantity: "3", Price: "5"}}, Settlement: "SYP"}
	q := quoted(t, set, cart)
	if q.Total != "15" {
		t.Fatalf("three bags at 5 = %q, want 15", q.Total)
	}
	sale := set.Till.Checkout(api.CheckoutInput{Cart: cart, Token: q.Token})
	if !sale.OK {
		t.Fatal(sale.Error)
	}
	ctx := context.Background()
	var moved int
	if err := api.Graph(set).DB.Reader(ctx).QueryRowContext(ctx,
		`SELECT COUNT(*) FROM stock_ledger WHERE product_id = ?`, item.Data.ID).Scan(&moved); err != nil {
		t.Fatal(err)
	}
	if moved != 0 {
		t.Fatalf("an open-priced sale moved stock %d times", moved)
	}
	// A price typed for a product that has its own is refused, not silently used.
	set.Owner.EndElevation()
	if r := set.Till.Quote(api.CartInput{Lines: []api.CartLineInput{{ProductID: firstStocked(t, set), Quantity: "1", Price: "1"}}}); r.OK {
		t.Fatal("a typed price was accepted for a catalogue-priced product")
	}
	// And the void of such a sale works — there is no stock movement to reverse, and it must not look for one.
	elevate(t, set)
	if v := set.Sales.Void(api.VoidInput{SaleID: sale.Data.ID, Reason: "غلط"}); !v.OK {
		t.Fatalf("voiding an open-priced sale failed: %+v", v.Error)
	}
}

func balanceOf(t *testing.T, set *api.Set, customerID, currency string) string {
	t.Helper()
	st := set.Customers.Statement(api.StatementQueryDTO{CustomerID: customerID, Currency: currency})
	if !st.OK {
		t.Fatal(st.Error)
	}
	return st.Data.Balance
}

func firstStocked(t *testing.T, set *api.Set) string {
	t.Helper()
	list := set.Catalog.Products(api.ProductQueryDTO{})
	for _, p := range list.Data {
		if !p.OpenPrice {
			return p.ID
		}
	}
	t.Fatal("no catalogue-priced product")
	return ""
}
