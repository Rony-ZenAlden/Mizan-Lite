package api_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/api"
	cashbookdomain "github.com/mizan-erp/mizan/internal/lite/cashbook/domain"
	reportsdomain "github.com/mizan-erp/mizan/internal/lite/reports/domain"
)

func TestTheReportsAndTheDrawerThroughTheBindings(t *testing.T) {
	set, oil := tillShop(t) // olive oil at $3.25, 15,000
	elevate(t, set)
	if r := set.Stock.Opening(api.ReceiveInput{ProductID: oil.ID, Quantity: "10", CostMode: "total", Cost: "20", Currency: "USD"}); !r.OK {
		t.Fatal(r.Error)
	}
	set.Owner.EndElevation()

	// 2 L at $3.25 = $6.50 = 97,500 pounds, paid in pounds; the cost is 2 L at $2.00 = $4.00 = 60,000 pounds.
	cart := api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "2"}}}
	sold := set.Till.Checkout(api.CheckoutInput{Cart: cart, Token: quoted(t, set, cart).Token})
	if !sold.OK || sold.Data.VoidReturn != "97500" || sold.Data.VoidReturnCurrency != "SYP" {
		t.Fatalf("the receipt says what a void hands back: %+v", sold)
	}

	// Without owner mode, since 2026-09-16: every report answers, and the drawer is shown in full. The owner asked for the
	// shop's own figures to be readable at the counter without a PIN (owner.ReservedActs).
	if r := set.Reports.Day(""); !r.OK {
		t.Fatalf("Day without owner mode = %+v", r.Error)
	}
	if r := set.Reports.Month(""); !r.OK {
		t.Fatalf("Month without owner mode = %+v", r.Error)
	}
	if r := set.Reports.Products(api.RangeInput{}); !r.OK {
		t.Fatalf("Products without owner mode = %+v", r.Error)
	}
	if r := set.Reports.Stock(api.RangeInput{}); !r.OK {
		t.Fatalf("Stock without owner mode = %+v", r.Error)
	}
	drawer := set.Cash.Drawer("")
	if !drawer.OK || !drawer.Data.OwnerView || drawer.Data.Date != drawer.Data.Today || len(drawer.Data.Categories) != 6 {
		t.Fatalf("the counter's drawer = %+v", drawer)
	}
	usd, syp := drawer.Data.Currencies[0], drawer.Data.Currencies[1]
	if usd.Currency != "USD" || usd.Expected != "0.00" || syp.CashSalesIn != "97500" || syp.Expected != "97500" || syp.Counted {
		t.Fatalf("drawer currencies = %+v", drawer.Data.Currencies)
	}
	count := set.Cash.Count(api.CashCountInput{Currency: "SYP", Counted: "٩٧٠٠٠"})
	if !count.OK || count.Data.Kind != "count" || count.Data.Amount != "97000" || count.Data.Expected != "97500" || count.Data.Difference != "-500" {
		t.Fatalf("a count at the counter = %+v", count)
	}
	expense := api.CashRecordInput{Kind: "expense", Currency: "SYP", Amount: "25000", Category: "electricity", FromDrawer: true, Note: "فاتورة"}
	// (An expense no longer asks for the PIN — owner.ReservedActs, 2026-09-16. Recording it here would put a second entry
	// in the drawer the assertion below counts; see TestEveryOwnersActGoesThroughAtTheCounterWithoutAPIN.)
	if after := set.Cash.Drawer(""); after.Data.Currencies[1].Count != "97000" || after.Data.Currencies[1].Difference != "-500" || len(after.Data.Entries) != 1 {
		t.Fatalf("the drawer after the count = %+v", after.Data)
	}

	elevate(t, set)
	recorded := set.Cash.Record(expense)
	if !recorded.OK || recorded.Data.Amount != "25000" || recorded.Data.Category != "electricity" || !recorded.Data.FromDrawer {
		t.Fatalf("an expense = %+v", recorded)
	}
	if r := set.Cash.Record(api.CashRecordInput{Kind: "loan", Currency: "SYP", Amount: "1"}); codeOf(t, r) != cashbookdomain.CodeUnknownKind {
		t.Fatalf("an unknown kind = %+v", r)
	}
	owner := set.Cash.Drawer("")
	if !owner.Data.OwnerView || owner.Data.Currencies[1].ExpensesOut != "25000" || owner.Data.Currencies[1].Expected != "72500" || len(owner.Data.Entries) != 2 ||
		!owner.Data.Entries[1].Reversible {
		t.Fatalf("the owner's drawer = %+v", owner.Data)
	}
	reversed := set.Cash.Reverse(api.ReverseCashInput{EntryID: recorded.Data.ID, Reason: "مكررة"})
	if !reversed.OK || reversed.Data.Kind != "reversal" || reversed.Data.Amount != "25000" {
		t.Fatalf("a reversal = %+v", reversed)
	}
	if r := set.Cash.Reverse(api.ReverseCashInput{EntryID: "nope", Reason: "x"}); codeOf(t, r) != cashbookdomain.CodeEntryNotFound {
		t.Fatalf("a malformed entry id = %+v", r)
	}

	day := set.Reports.Day("")
	p := day.Data.Profit
	if !day.OK || p.Sales != 1 || p.RevenueUSD != "6.50" || p.CostUSD != "4.00" || p.ProfitUSD != "2.50" || p.MarginUSD != "38.5" ||
		p.RevenueLocal != "97500" || p.CostLocal != "60000" || p.ProfitLocal != "37500" || day.Data.Rate != "15000" || day.Data.NetUSD != "2.50" ||
		day.Data.Expenses.Local != "0" || day.Data.Takings[1].Charged != "97500" {
		t.Fatalf("the day = %+v", day.Data)
	}
	month := set.Reports.Month("")
	if !month.OK || len(month.Data.Days) != 1 || month.Data.Total.Profit.ProfitUSD != "2.50" || month.Data.Total.Rate != "" {
		t.Fatalf("the month = %+v", month.Data)
	}
	products := set.Reports.Products(api.RangeInput{})
	if !products.OK || len(products.Data.Rows) != 1 || products.Data.Rows[0].Quantity != "2.000" || products.Data.Rows[0].NameAR != "زيت زيتون" ||
		products.Data.Total.RevenueLocal != "97500" {
		t.Fatalf("products = %+v", products.Data)
	}
	stockReport := set.Reports.Stock(api.RangeInput{})
	c := stockReport.Data.Reconciliation
	if !stockReport.OK || stockReport.Data.TotalUSD != "16.00" || stockReport.Data.TotalLocal != "240000" || c.Opening != "0.00" || c.Received != "20.00" ||
		c.Sold != "-4.00" || c.Closing != "16.00" || c.Rounding != "0.00" || len(stockReport.Data.Shelf) != 1 || stockReport.Data.Shelf[0].ProfitUSD != "10.00" ||
		stockReport.Data.ShelfTotalLocal != "150000" || stockReport.Data.Shelf[0].AverageCost != "2.00" {
		t.Fatalf("stock = %+v", stockReport.Data)
	}
	if r := set.Reports.Day("2026-02-30"); codeOf(t, r) != reportsdomain.CodeBadDate {
		t.Fatal("a date that does not exist")
	}
	if r := set.Reports.Month("2026-9"); codeOf(t, r) != reportsdomain.CodeBadMonth {
		t.Fatal("a month that is not YYYY-MM")
	}
	if r := set.Reports.Products(api.RangeInput{From: "2026-09-10", To: "2026-09-01"}); codeOf(t, r) != reportsdomain.CodeBadRange {
		t.Fatal("a backwards range")
	}
}

var reportDTOs = []reflect.Type{
	reflect.TypeFor[api.AmountDTO](), reflect.TypeFor[api.ProfitDTO](), reflect.TypeFor[api.LossesDTO](), reflect.TypeFor[api.CategoryDTO](),
	reflect.TypeFor[api.TakingsDTO](), reflect.TypeFor[api.DayReportDTO](), reflect.TypeFor[api.MonthReportDTO](), reflect.TypeFor[api.ProductRowDTO](),
	reflect.TypeFor[api.ProductsReportDTO](), reflect.TypeFor[api.StockLineDTO](), reflect.TypeFor[api.ReconciliationDTO](), reflect.TypeFor[api.ShelfLineDTO](),
	reflect.TypeFor[api.LeftOutDTO](), reflect.TypeFor[api.StockReportDTO](), reflect.TypeFor[api.CashEntryDTO](), reflect.TypeFor[api.DrawerCurrencyDTO](),
	reflect.TypeFor[api.DrawerDTO](),
}

// TestEveryReportFigureIsAString holds DESIGN D9 at L6's boundary: the integers allowed are counts and places.
func TestEveryReportFigureIsAString(t *testing.T) {
	counts := map[string]bool{"Sales": true, "UnknownLines": true, "Unconverted": true, "CreditSales": true, "Voids": true, "BelowCost": true, "Seq": true}
	for _, typ := range reportDTOs {
		for i := range typ.NumField() {
			f := typ.Field(i)
			switch f.Type.Kind() {
			case reflect.String, reflect.Bool, reflect.Slice, reflect.Struct:
			case reflect.Int, reflect.Int64:
				if !counts[f.Name] {
					t.Errorf("%s.%s crosses as a number", typ.Name(), f.Name)
				}
			default:
				t.Errorf("%s.%s is a %s", typ.Name(), f.Name, f.Type.Kind())
			}
		}
	}
}

// TestRefundedIsGoneFromTheWire: the Sales screen's voids are the value voided, named for what they are (A-L6.6).
func TestRefundedIsGoneFromTheWire(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeFor[api.DayTotalsDTO](), reflect.TypeFor[api.DayDTO](), reflect.TypeFor[api.SaleDTO]()} {
		for i := range typ.NumField() {
			if f := typ.Field(i); strings.Contains(strings.ToLower(f.Name+f.Tag.Get("json")), "refund") {
				t.Errorf("%s.%s", typ.Name(), f.Name)
			}
		}
	}
}
