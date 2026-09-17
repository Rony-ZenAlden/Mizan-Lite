package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"
	"unicode"

	"github.com/mizan-erp/mizan/internal/lite/api"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/lite/moneyfmt"
)

// setDisplay puts the shop into one of the three readings of its currency.
func setDisplay(t *testing.T, set *api.Set, mode moneyfmt.Display) {
	t.Helper()
	value := string(mode)
	if r := set.Settings.Update(api.SettingsInput{MoneyDisplay: &value}); !r.OK {
		t.Fatalf("Settings.Update(%s) = %+v", mode, r.Error)
	}
}

// TestTheRedenominationReachesTheTillTheReportsAndTheReceipt is L10's whole point: a shop that drops two noughts sees them
// dropped everywhere at once, and a figure the books hold is never itself changed.
func TestTheRedenominationReachesTheTillTheReportsAndTheReceipt(t *testing.T) {
	set, oil := tillShop(t) // olive oil at $3.25, the rate 15,000
	elevate(t, set)
	if r := set.Stock.Opening(api.ReceiveInput{ProductID: oil.ID, Quantity: "10", CostMode: "total", Cost: "20", Currency: "USD"}); !r.OK {
		t.Fatal(r.Error)
	}
	set.Owner.EndElevation()

	// A sale of 2 L: $6.50, which at 15,000 is 97,500 old pounds.
	cart := api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "2"}}}
	sale := set.Till.Checkout(api.CheckoutInput{Cart: cart, Token: quoted(t, set, cart).Token})
	if !sale.OK {
		t.Fatal(sale.Error)
	}
	saleID := sale.Data.ID

	// The old pound, which is what a shop reads until it says otherwise.
	if got := set.Sales.Receipt(saleID); got.Data.Total != "97500" {
		t.Fatalf("legacy total = %q, want 97500", got.Data.Total)
	}

	// The new pound: the same sale, two noughts fewer, on the receipt DTO...
	setDisplay(t, set, moneyfmt.New)
	receipt := set.Sales.Receipt(saleID)
	if receipt.Data.Total != "975" {
		t.Fatalf("new total = %q, want 975", receipt.Data.Total)
	}
	// ...in the day's report...
	day := set.Reports.Day("")
	if day.Data.Profit.RevenueLocal != "975" {
		t.Fatalf("the day's revenue = %q, want 975", day.Data.Profit.RevenueLocal)
	}
	// ...in the drawer...
	drawer := set.Cash.Drawer("")
	if drawer.Data.Currencies[1].Expected != "975" {
		t.Fatalf("the drawer expected = %q, want 975", drawer.Data.Currencies[1].Expected)
	}
	// ...and on the paper the shop actually prints, which is built from those DTOs.
	doc, err := api.PrintDocument(set, "sale", saleID)
	if err != nil {
		t.Fatal(err)
	}
	printed := textOf(doc)
	if !strings.Contains(printed, "975") || strings.Contains(printed, "97500") {
		t.Fatalf("the receipt still prints the old pound:\n%s", printed)
	}

	// The dollar is untouched throughout: the redenomination is the pound's.
	if day.Data.Profit.RevenueUSD != "6.50" {
		t.Fatalf("the dollar revenue changed to %q", day.Data.Profit.RevenueUSD)
	}

	// Dual carries both figures, the new one first.
	setDisplay(t, set, moneyfmt.Dual)
	dual := set.Sales.Receipt(saleID)
	if !strings.HasPrefix(dual.Data.Total, "975") || !strings.Contains(dual.Data.Total, "97500") {
		t.Fatalf("dual total = %q, want both figures", dual.Data.Total)
	}

	// And back to the old pound: the books never moved, so the original figure returns exactly.
	setDisplay(t, set, moneyfmt.Legacy)
	if got := set.Sales.Receipt(saleID); got.Data.Total != "97500" {
		t.Fatalf("back in the old pound the total is %q — the stored figure was changed", got.Data.Total)
	}
}

// TestADualReceiptCarriesNothingAPrinterCannotDraw is the owner's report of 2026-09-17, kept from coming back.
//
// A dual reading used to divide its two figures with an ASCII unit separator. Every place that printed the figure
// without knowing to split it — the receipt, the A4 page, the rate line at the top of the screen, a workbook cell —
// drew a control character, which a webview and a printer both show as a broken box: "1 USD = 137 ☒ 13700 SYP". The
// separator is now brackets, which need no cooperation from whoever draws them.
func TestADualReceiptCarriesNothingAPrinterCannotDraw(t *testing.T) {
	set, oil := tillShop(t) // olive oil at $3.25, the rate 15,000
	cart := api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "2"}}}
	sale := set.Till.Checkout(api.CheckoutInput{Cart: cart, Token: quoted(t, set, cart).Token})
	if !sale.OK {
		t.Fatal(sale.Error)
	}
	setDisplay(t, set, moneyfmt.Dual)

	doc, err := api.PrintDocument(set, "sale", sale.Data.ID)
	if err != nil {
		t.Fatal(err)
	}
	printed := textOf(doc)
	for _, r := range printed {
		// Bidi isolates are the one control character a printed line is allowed (L7 H3); nothing else may reach paper.
		if unicode.IsControl(r) && !strings.ContainsRune("\u2066\u2067\u2068\u2069\u200e\u200f\n\t", r) {
			t.Fatalf("the receipt carries %U, which a printer draws as a broken box:\n%s", r, printed)
		}
	}
	// The rate line reads as the owner asked: the new figure, then the old one in brackets, each grouped.
	if !strings.Contains(printed, "150 (15,000)") {
		t.Fatalf("the rate line does not read \"150 (15,000)\":\n%s", printed)
	}
	// And the total, 97,500 old pounds, reads the same way.
	if !strings.Contains(printed, "975 (97,500)") {
		t.Fatalf("the total does not read \"975 (97,500)\":\n%s", printed)
	}
}

// TestTheStoredFigureIsNeverRedenominated reads the database itself: whatever the shop chooses to read, the books hold the
// pounds they always held.
func TestTheStoredFigureIsNeverRedenominated(t *testing.T) {
	set, oil := tillShop(t)
	cart := api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "2"}}}
	sale := set.Till.Checkout(api.CheckoutInput{Cart: cart, Token: quoted(t, set, cart).Token})
	if !sale.OK {
		t.Fatal(sale.Error)
	}
	for _, mode := range []moneyfmt.Display{moneyfmt.New, moneyfmt.Dual, moneyfmt.Legacy} {
		setDisplay(t, set, mode)
		var totalMinor int64
		ctx := context.Background()
		row := api.Graph(set).DB.Reader(ctx).QueryRowContext(ctx, "SELECT total_minor FROM sales WHERE id = ?", sale.Data.ID)
		if err := row.Scan(&totalMinor); err != nil {
			t.Fatal(err)
		}
		if totalMinor != 97_500 {
			t.Fatalf("%s: the stored figure is %d — a display setting changed the books", mode, totalMinor)
		}
	}
}

// TestNoLocalMoneyFieldEscapesThePipeline is the gate the redenomination rests on.
//
// It reads the same seeded shop twice — once in the old pound, once in the new — and compares every figure every binding
// sends. A field carrying local money MUST differ between the two readings. One that does not either is not money (a
// quantity, a dollar, a percentage, a date, a count) and belongs on the list below, or it is a leak: a figure that will
// show a shop 15,000 on a screen that says 150 everywhere else.
//
// When this fails with a new field name, the question to ask is "is this local money?" — and the answer decides whether it
// goes through moneyShop or onto the list.
func TestNoLocalMoneyFieldEscapesThePipeline(t *testing.T) {
	set, oil := tillShop(t)
	elevate(t, set)
	if r := set.Stock.Opening(api.ReceiveInput{ProductID: oil.ID, Quantity: "10", CostMode: "total", Cost: "20", Currency: "USD"}); !r.OK {
		t.Fatal(r.Error)
	}
	abu := set.Customers.Create(api.CustomerInput{Name: "أبو محمد"}).Data
	set.Owner.EndElevation()
	cart := api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "2"}}}
	if r := set.Till.Checkout(api.CheckoutInput{Cart: cart, Token: quoted(t, set, cart).Token}); !r.OK {
		t.Fatal(r.Error)
	}

	// Every figure a screen reads, in both readings of the currency.
	read := func() map[string]string {
		out := map[string]string{}
		collect := func(name string, v any) {
			raw, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			flatten(t, name, json.RawMessage(raw), out)
		}
		collect("till.quote", quoted(t, set, cart))
		collect("sales.list", set.Sales.List("").Data)
		collect("catalog.products", set.Catalog.Products(api.ProductQueryDTO{}).Data)
		collect("stock.valuation", set.Stock.Valuation().Data)
		collect("reports.day", set.Reports.Day("").Data)
		collect("reports.month", set.Reports.Month("").Data)
		collect("reports.stock", set.Reports.Stock(api.RangeInput{}).Data)
		collect("cash.drawer", set.Cash.Drawer("").Data)
		collect("customers.outstanding", set.Customers.Outstanding().Data)
		collect("customers.statement", set.Customers.Statement(api.StatementQueryDTO{CustomerID: abu.ID, Currency: "USD"}).Data)
		// The rate screens: added after FX.Current was found reading 15,000 beside prices that read 150 (L10).
		collect("fx.current", set.FX.Current().Data)
		collect("fx.history", set.FX.History(10).Data)
		collect("stock.levels", set.Stock.Levels().Data)
		collect("sales.receipt", set.Sales.List("").Data)
		return out
	}

	setDisplay(t, set, moneyfmt.Legacy)
	legacy := read()
	setDisplay(t, set, moneyfmt.New)
	redenominated := read()

	if os.Getenv("LITE_MONEY_PATHS") == "1" {
		var paths []string
		for path, before := range legacy {
			paths = append(paths, fmt.Sprintf("%s = %q -> %q", path, before, redenominated[path]))
		}
		sort.Strings(paths)
		t.Log("\n" + strings.Join(paths, "\n"))
	}

	var leaks []string
	for path, before := range legacy {
		after, seen := redenominated[path]
		if !seen || before == after {
			continue // unchanged: either not money, or on the list below
		}
		// It changed — prove it changed by exactly the two noughts, not by something else moving underneath.
		if want := shiftLeftTwo(before); after != want {
			leaks = append(leaks, fmt.Sprintf("%s: %q became %q, want %q", path, before, after, want))
		}
	}
	sort.Strings(leaks)
	if len(leaks) > 0 {
		t.Fatalf("figures that moved by something other than two noughts:\n  %s", strings.Join(leaks, "\n  "))
	}

	// The half of the gate that catches a leak: every non-zero figure that stayed still must be one we have classified as
	// not being local money. A new one means somebody added a figure that bypassed the pipeline.
	var stayed []string
	for path, before := range legacy {
		if redenominated[path] == before && before != "0" && before != "0.00" && before != "0.000" {
			stayed = append(stayed, path)
		}
	}
	sort.Strings(stayed)
	if !slices.Equal(stayed, notLocalMoney) {
		t.Errorf("the figures that did not move are not the ones audited as \"not local money\".\n"+
			"A new name here is a figure that bypassed the pipeline — classify it, or route it through moneyShop.\n"+
			"got:\n  %s\nwant:\n  %s", strings.Join(stayed, "\n  "), strings.Join(notLocalMoney, "\n  "))
	}

	// And the other half: the figures we know are local money DID move. A pipeline that transformed nothing would pass
	// the loop above in silence.
	for _, path := range []string{
		"reports.day.profit.revenueLocal", "reports.day.takings.1.charged",
		"reports.month.total.profit.revenueLocal", "reports.stock.totalLocal",
		"cash.drawer.currencies.1.expected",
	} {
		if legacy[path] == redenominated[path] {
			t.Errorf("%s did not change: it is local money and should read two noughts shorter (%q)", path, legacy[path])
		}
	}
}

// notLocalMoney is every non-zero figure that does NOT move when the shop drops two noughts, audited by hand on
// 2026-09-17. Each is a dollar amount, a quantity, a percentage or a barcode — none of them is local money.
//
// The list is frozen because it is the half of the gate that catches a LEAK. A new field carrying local money that
// forgets the pipeline does not move, so it appears here, and this test fails asking to be classified. Without the list a
// leak is invisible: nothing changed, and nothing complained.
var notLocalMoney = []string{
	"catalog.products.0.barcode",
	"catalog.products.0.price",
	"reports.day.netUsd",
	"reports.day.profit.costUsd",
	"reports.day.profit.marginLocal",
	"reports.day.profit.marginUsd",
	"reports.day.profit.profitUsd",
	"reports.day.profit.revenueUsd",
	"reports.month.days.0.netUsd",
	"reports.month.days.0.profit.costUsd",
	"reports.month.days.0.profit.marginLocal",
	"reports.month.days.0.profit.marginUsd",
	"reports.month.days.0.profit.profitUsd",
	"reports.month.days.0.profit.revenueUsd",
	"reports.month.total.netUsd",
	"reports.month.total.profit.costUsd",
	"reports.month.total.profit.marginLocal",
	"reports.month.total.profit.marginUsd",
	"reports.month.total.profit.profitUsd",
	"reports.month.total.profit.revenueUsd",
	"reports.stock.lines.0.averageCost",
	"reports.stock.lines.0.onHand",
	"reports.stock.lines.0.valueUsd",
	"reports.stock.reconciliation.closing",
	"reports.stock.reconciliation.received",
	"reports.stock.reconciliation.sold",
	"reports.stock.retailTotalUsd",
	"reports.stock.shelf.0.averageCost",
	"reports.stock.shelf.0.onHand",
	"reports.stock.shelf.0.price",
	"reports.stock.shelf.0.profitUsd",
	"reports.stock.shelfTotalUsd",
	"reports.stock.totalUsd",
	"sales.list.sales.0.lines.0.grossUsd",
	"sales.list.sales.0.lines.0.netUsd",
	"sales.list.sales.0.lines.0.quantity",
	"sales.list.sales.0.lines.0.unitPrice",
	"sales.list.sales.0.linesUsd",
	"sales.receipt.sales.0.lines.0.grossUsd",
	"sales.receipt.sales.0.lines.0.netUsd",
	"sales.receipt.sales.0.lines.0.quantity",
	"sales.receipt.sales.0.lines.0.unitPrice",
	"sales.receipt.sales.0.linesUsd",
	"stock.levels.0.onHand",
	"stock.valuation.lines.0.averageCost",
	"stock.valuation.lines.0.onHand",
	"stock.valuation.lines.0.value",
	"stock.valuation.total",
	"till.quote.lines.0.grossUsd",
	"till.quote.lines.0.netUsd",
	"till.quote.lines.0.onHand",
	"till.quote.lines.0.quantity",
	"till.quote.lines.0.unitPrice",
	"till.quote.linesUsd",
	"till.quote.totalOther",
}

// flatten walks a JSON document into dotted paths so every leaf figure can be compared by name.
func flatten(t *testing.T, prefix string, raw json.RawMessage, out map[string]string) {
	t.Helper()
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) == nil {
		for key, value := range object {
			flatten(t, prefix+"."+key, value, out)
		}
		return
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil {
		for i, value := range list {
			flatten(t, fmt.Sprintf("%s.%d", prefix, i), value, out)
		}
		return
	}
	var text string
	if json.Unmarshal(raw, &text) == nil && looksLikeAFigure(text) {
		out[prefix] = text
	}
}

// looksLikeAFigure is a decimal with an optional sign: the shape every money figure crosses the boundary in (DESIGN D9).
func looksLikeAFigure(text string) bool {
	if text == "" {
		return false
	}
	body := strings.TrimPrefix(text, "-")
	if body == "" {
		return false
	}
	seenPoint := false
	for _, r := range body {
		switch {
		case r >= '0' && r <= '9':
		case r == '.' && !seenPoint:
			seenPoint = true
		default:
			return false
		}
	}
	return true
}

// shiftLeftTwo is what dropping two noughts does to a decimal, computed independently of the package under test so the
// gate cannot agree with a bug in it.
func shiftLeftTwo(text string) string {
	negative := strings.HasPrefix(text, "-")
	body := strings.TrimPrefix(text, "-")
	whole, fraction, _ := strings.Cut(body, ".")
	digits := whole + fraction
	point := len(whole) - 2
	for point <= 0 {
		digits = "0" + digits
		point++
	}
	whole, fraction = digits[:point], digits[point:]
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	fraction = strings.TrimRight(fraction, "0")
	out := whole
	if fraction != "" {
		out += "." + fraction
	}
	if negative {
		return "-" + out
	}
	return out
}

// TestWhatAShopTypesInNewPoundsIsStoredInOldOnes is the input half of L10. A shop reading the new pound types 150 and the
// books record 15,000 — so the redenomination is a way of reading, never a change to what the shop owns.
func TestWhatAShopTypesInNewPoundsIsStoredInOldOnes(t *testing.T) {
	set, _ := attached(t, litetest.Logger())
	firstRun(t, set)
	setDisplay(t, set, moneyfmt.New)
	elevate(t, set)

	// A price typed as 300 new pounds is 30,000 old ones — and reads back as 300.
	made := set.Catalog.CreateProduct(api.CreateProductInput{NameAR: "رز", UnitCode: "kg", PriceCurrency: "SYP", Price: "300"})
	if !made.OK {
		t.Fatal(made.Error)
	}
	if made.Data.Price != "300" {
		t.Fatalf("a price typed as 300 reads back as %q", made.Data.Price)
	}
	setDisplay(t, set, moneyfmt.Legacy)
	legacy := set.Catalog.Products(api.ProductQueryDTO{Text: "رز"})
	if legacy.Data[0].Price != "30000" {
		t.Fatalf("the books hold %q, want 30000", legacy.Data[0].Price)
	}

	// The exchange rate: typed as 150, held as 15,000.
	setDisplay(t, set, moneyfmt.New)
	if r := set.FX.SetRate(api.SetRateInput{Rate: "152", ConfirmLargeChange: true}); !r.OK {
		t.Fatal(r.Error)
	}
	if got := set.FX.Current(); got.Data.Rate != "152" {
		t.Fatalf("the rate reads back as %q, want 152", got.Data.Rate)
	}
	setDisplay(t, set, moneyfmt.Legacy)
	if got := set.FX.Current(); got.Data.Rate != "15200" {
		t.Fatalf("the books hold the rate as %q, want 15200", got.Data.Rate)
	}

	// The smallest note: typed as 5, held as 500.
	setDisplay(t, set, moneyfmt.New)
	if r := set.Till.SetCashNote("5"); !r.OK {
		t.Fatal(r.Error)
	}
	if got := set.Till.CashNote(); got.Data.Note != "5" {
		t.Fatalf("the note reads back as %q, want 5", got.Data.Note)
	}
	setDisplay(t, set, moneyfmt.Legacy)
	if got := set.Till.CashNote(); got.Data.Note != "500" {
		t.Fatalf("the books hold the note as %q, want 500", got.Data.Note)
	}

	// And an expense typed as 250 new pounds is 25,000 old ones in the drawer.
	setDisplay(t, set, moneyfmt.New)
	if r := set.Cash.Record(api.CashRecordInput{Kind: "expense", Currency: "SYP", Amount: "250", Category: "electricity", FromDrawer: true}); !r.OK {
		t.Fatal(r.Error)
	}
	setDisplay(t, set, moneyfmt.Legacy)
	drawer := set.Cash.Drawer("")
	if drawer.Data.Currencies[1].ExpensesOut != "25000" {
		t.Fatalf("the drawer holds the expense as %q, want 25000", drawer.Data.Currencies[1].ExpensesOut)
	}
}
