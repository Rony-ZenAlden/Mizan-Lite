package api_test

import (
	"strconv"
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/api"
	salesdomain "github.com/mizan-erp/mizan/internal/lite/sales/domain"
)

// TestAPartialReturnPutsTheStockBackAndTheMoneyOut is the F8 wizard end to end, through the bindings a screen calls:
// find the sale by its receipt number, tick one of the two litres, and see the stock, the drawer and the day's profit
// all move by the right amounts.
func TestAPartialReturnPutsTheStockBackAndTheMoneyOut(t *testing.T) {
	set, oil := tillShop(t) // olive oil at $3.25, the rate 15,000
	elevate(t, set)
	if r := set.Stock.Opening(api.ReceiveInput{ProductID: oil.ID, Quantity: "10", CostMode: "total", Cost: "20", Currency: "USD"}); !r.OK {
		t.Fatal(r.Error)
	}
	set.Owner.EndElevation()

	cart := api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "2"}}}
	sale := set.Till.Checkout(api.CheckoutInput{Cart: cart, Token: quoted(t, set, cart).Token})
	if !sale.OK {
		t.Fatal(sale.Error)
	}
	onHandAfterSale := levelOf(t, set, oil.ID)

	// The counter has the paper in hand, so the sale is found by the number printed on it.
	found := set.Sales.Returnable(strconv.FormatInt(sale.Data.ReceiptNo, 10))
	if !found.OK {
		t.Fatal(found.Error)
	}
	if len(found.Data.Lines) != 1 || !found.Data.Lines[0].Returnable {
		t.Fatalf("returnable = %+v", found.Data)
	}
	line := found.Data.Lines[0]
	if line.Sold != "2.000" || line.Returned != "0.000" || line.Left != "2.000" {
		t.Fatalf("sold %q, returned %q, left %q", line.Sold, line.Returned, line.Left)
	}

	in := api.ReturnInput{SaleID: found.Data.SaleID, Settlement: "cash", Reason: "الزبون رجّع علبة",
		Lines: []api.ReturnLineInput{{SaleLineID: line.SaleLineID, Quantity: "1", Restock: true}}}

	// Quoting records nothing: the customer is told the figure before the goods change hands.
	quote := set.Sales.QuoteReturn(in)
	if !quote.OK {
		t.Fatal(quote.Error)
	}
	if quote.Data.ID != "" {
		t.Fatalf("a quote claimed an identity: %q", quote.Data.ID)
	}
	if levelOf(t, set, oil.ID) != onHandAfterSale {
		t.Fatal("quoting a return moved the stock")
	}

	recorded := set.Sales.Return(in)
	if !recorded.OK {
		t.Fatal(recorded.Error)
	}
	if recorded.Data.Refund != quote.Data.Refund {
		t.Fatalf("recorded %q but quoted %q", recorded.Data.Refund, quote.Data.Refund)
	}
	// Half of a 97,500 sale.
	if recorded.Data.Refund != "48750" {
		t.Fatalf("refund = %q, want 48,750", recorded.Data.Refund)
	}
	if recorded.Data.ID == "" || recorded.Data.ReturnNo != "1" {
		t.Fatalf("recorded = %+v", recorded.Data)
	}

	// The litre is back on the shelf.
	if back := levelOf(t, set, oil.ID); back != "9.000" {
		t.Fatalf("on hand after the return = %q, want 9.000", back)
	}
	// The sale now says one litre has come back and one is left.
	after := set.Sales.Returnable(strconv.FormatInt(sale.Data.ReceiptNo, 10))
	if !after.OK || after.Data.Lines[0].Returned != "1.000" || after.Data.Lines[0].Left != "1.000" {
		t.Fatalf("after the return = %+v", after.Data.Lines)
	}
	if len(after.Data.Returns) != 1 {
		t.Fatalf("the sale does not carry its return: %+v", after.Data.Returns)
	}

	// The drawer is short by the refund, because a cash return hands notes back over the counter.
	drawer := set.Cash.Drawer("")
	local := drawerOf(t, drawer.Data, "SYP")
	if local.Expected != "48750" {
		t.Fatalf("the drawer expects %q after a 97,500 sale and a 48,750 refund", local.Expected)
	}

	// And the day's profit falls by the MARGIN on what came back, not by the whole refund: the stock is on the shelf.
	elevate(t, set)
	day := set.Reports.Day("")
	set.Owner.EndElevation()
	if !day.OK {
		t.Fatal(day.Error)
	}
	if day.Data.Returns.Count != 1 || day.Data.Returns.Refund.Local != "48750" {
		t.Fatalf("the day's returns = %+v", day.Data.Returns)
	}
	// The delivery was $20 for 10 L, so a litre cost the shop $2 — 30,000 pounds at 15,000.
	if day.Data.Returns.Cost.Local != "30000" {
		t.Fatalf("the cost put back = %q, want 30,000", day.Data.Returns.Cost.Local)
	}
	// So the day lost the margin — 48,750 refunded less 30,000 back on the shelf — and not the whole refund.
	if day.Data.Returns.Profit.Local != "18750" {
		t.Fatalf("profit lost = %q, want 18,750 (the margin, not the refund)", day.Data.Returns.Profit.Local)
	}
}

// TestAReturnAgainstACreditSaleComesOffTheDebtAndNotTheDrawer: no cash crosses the counter, so the drawer must not
// move, and the customer's balance must.
func TestAReturnAgainstACreditSaleComesOffTheDebtAndNotTheDrawer(t *testing.T) {
	set, oil := tillShop(t)
	elevate(t, set)
	if r := set.Stock.Opening(api.ReceiveInput{ProductID: oil.ID, Quantity: "10", CostMode: "total", Cost: "20", Currency: "USD"}); !r.OK {
		t.Fatal(r.Error)
	}
	abu := set.Customers.Create(api.CustomerInput{Name: "أبو محمد"}).Data
	set.Owner.EndElevation()

	cart := api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "2"}},
		Payment: "credit", CustomerID: abu.ID}
	sale := set.Till.Checkout(api.CheckoutInput{Cart: cart, Token: quoted(t, set, cart).Token})
	if !sale.OK {
		t.Fatal(sale.Error)
	}
	before := set.Cash.Drawer("").Data

	found := set.Sales.Returnable(strconv.FormatInt(sale.Data.ReceiptNo, 10))
	if !found.OK || found.Data.CustomerID != abu.ID {
		t.Fatalf("a credit sale did not carry its customer: %+v", found.Data)
	}
	got := set.Sales.Return(api.ReturnInput{SaleID: found.Data.SaleID, Settlement: "debt", Reason: "رجّع علبة",
		Lines: []api.ReturnLineInput{{SaleLineID: found.Data.Lines[0].SaleLineID, Quantity: "1", Restock: true}}})
	if !got.OK {
		t.Fatal(got.Error)
	}

	// The drawer did not move: no notes crossed the counter.
	if after := set.Cash.Drawer("").Data; drawerOf(t, after, "SYP").Expected != drawerOf(t, before, "SYP").Expected {
		t.Fatalf("a debt return moved the drawer: %q → %q",
			drawerOf(t, before, "SYP").Expected, drawerOf(t, after, "SYP").Expected)
	}
	// The balance did: 97,500 owed, 48,750 handed back in goods.
	statement := set.Customers.Statement(api.StatementQueryDTO{CustomerID: abu.ID, Currency: "SYP"})
	if !statement.OK {
		t.Fatal(statement.Error)
	}
	if statement.Data.Balance != "48750" {
		t.Fatalf("balance = %q, want 48,750", statement.Data.Balance)
	}
	// And it is recorded as a return, not as a write-off: the customer has not failed to pay anybody.
	var kinds []string
	for _, e := range statement.Data.Entries {
		kinds = append(kinds, e.Kind)
	}
	if !hasKind(kinds, string(salesdomain.SettleDebt)) && !hasKind(kinds, "sale_return") {
		t.Fatalf("the ledger does not show a return: %v", kinds)
	}
	if hasKind(kinds, "write_off") {
		t.Fatalf("a return was written into the ledger as a write-off: %v", kinds)
	}
}

func levelOf(t *testing.T, set *api.Set, productID string) string {
	t.Helper()
	levels := set.Stock.Levels()
	if !levels.OK {
		t.Fatal(levels.Error)
	}
	for _, l := range levels.Data {
		if l.ProductID == productID {
			return l.OnHand
		}
	}
	t.Fatalf("no level for %s", productID)
	return ""
}

func drawerOf(t *testing.T, d api.DrawerDTO, currency string) api.DrawerCurrencyDTO {
	t.Helper()
	for _, c := range d.Currencies {
		if c.Currency == currency {
			return c
		}
	}
	t.Fatalf("no %s in the drawer: %+v", currency, d.Currencies)
	return api.DrawerCurrencyDTO{}
}

func hasKind(all []string, want string) bool {
	for _, v := range all {
		if v == want {
			return true
		}
	}
	return false
}
