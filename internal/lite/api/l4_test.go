package api_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/api"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	ownerdomain "github.com/mizan-erp/mizan/internal/lite/owner/domain"
	salesdomain "github.com/mizan-erp/mizan/internal/lite/sales/domain"
	settingsdomain "github.com/mizan-erp/mizan/internal/lite/settings/domain"
)

// tillShop is a shop after first run (15,000) with olive oil by the litre at $3.25, barcoded.
func tillShop(t *testing.T) (*api.Set, api.ProductDTO) {
	t.Helper()
	set, _ := attached(t, litetest.Logger())
	firstRun(t, set)
	oil := set.Catalog.CreateProduct(api.CreateProductInput{NameAR: "زيت زيتون", UnitCode: "l", PriceCurrency: "USD", Price: "3.25", Barcode: "6291041500213"})
	if !oil.OK {
		t.Fatal(oil.Error)
	}
	return set, oil.Data
}

func quoted(t *testing.T, set *api.Set, in api.CartInput) api.CartQuoteDTO {
	t.Helper()
	q := set.Till.Quote(in)
	if !q.OK {
		t.Fatalf("Quote(%+v) = %+v", in, q.Error)
	}
	return q.Data
}

func TestTheTillThroughTheBindings(t *testing.T) {
	set, oil := tillShop(t)

	scan := set.Till.Scan("٦٢٩١٠٤١٥٠٠٢١٣")
	if !scan.OK || !scan.Data.Found || scan.Data.ProductID != oil.ID || scan.Data.UnitDecimals != 3 || scan.Data.OnHand != "0.000" {
		t.Fatalf("Scan in Arabic-Indic digits = %+v", scan)
	}
	if unknown := set.Till.Scan("123"); !unknown.OK || unknown.Data.Found {
		t.Fatalf("an unknown barcode = %+v", unknown)
	}

	// 1.5 L at $3.25 = $4.875 → $4.88; 73,125 pounds → 73,000 on the 500 note; paid with a $5 note, change in pounds.
	cart := api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "1٫5"}}, TenderCurrency: "USD", Tendered: "5"}
	q := quoted(t, set, cart)
	line := q.Lines[0]
	if q.Settlement != "SYP" || q.Total != "73000" || q.Rounding != "-125" || q.CashNote != "500" || q.LinesUSD != "4.88" ||
		q.OtherCurrency != "USD" || q.TotalOther != "4.88" || q.Tendered != "5.00" || q.ChangeCurrency != "SYP" || q.Change != "2000" ||
		q.Rate != "15000" || q.Discounted || q.Token == "" || q.LocalCurrency != "SYP" {
		t.Fatalf("Quote = %+v", q)
	}
	if line.Quantity != "1.500" || line.UnitPrice != "3.25" || line.GrossLocal != "73125" || line.GrossUSD != "4.88" ||
		line.NetLocal != "73125" || line.DiscountPercent != "" || !slices.Contains(line.Warnings, "beyond_stock") || !slices.Contains(line.Warnings, "no_cost") {
		t.Fatalf("line = %+v", line)
	}

	if r := set.Till.Checkout(api.CheckoutInput{Cart: cart, Token: "not-the-token"}); codeOf(t, r) != salesdomain.CodeQuoteStale {
		t.Fatal("a checkout with a stale token went through")
	}
	onCredit := api.CartInput{Lines: cart.Lines, Payment: "credit"}
	creditQuote := quoted(t, set, onCredit)
	if !creditQuote.NeedsCustomer || creditQuote.Debt == "" {
		t.Fatalf("a credit quote with no customer = %+v", creditQuote)
	}
	if r := set.Till.Checkout(api.CheckoutInput{Cart: onCredit, Token: creditQuote.Token}); codeOf(t, r) != salesdomain.CodeCustomerRequired {
		t.Fatal("a credit sale with no customer")
	}
	sold := set.Till.Checkout(api.CheckoutInput{Cart: cart, Token: q.Token})
	if !sold.OK || sold.Data.ReceiptNo != 1 || sold.Data.Status != "posted" || sold.Data.Total != "73000" || sold.Data.Change != "2000" ||
		sold.Data.ShopName != "بقالية المونة" || sold.Data.Lines[0].Quantity != "1.500" || sold.Data.VoidedAt != "" {
		t.Fatalf("Checkout = %+v", sold)
	}
	if levels := set.Stock.Levels(); !levels.OK || levels.Data[0].OnHand != "-1.500" {
		t.Fatalf("stock after selling what was never received = %+v", levels)
	}

	// A discount needs the owner at checkout; the quote shows it.
	discounted := api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "1", DiscountPercent: "10"}}}
	dq := quoted(t, set, discounted)
	if !dq.Discounted || dq.Lines[0].DiscountPercent != "10" || dq.Lines[0].DiscountUSD != "0.33" {
		t.Fatalf("discounted quote = %+v", dq)
	}
	if r := set.Till.Checkout(api.CheckoutInput{Cart: discounted, Token: dq.Token}); codeOf(t, r) != ownerdomain.CodeRequired {
		t.Fatal("a discount went through without the owner")
	}

	// The day, per currency: pounds charged and given back, dollars taken.
	day := set.Sales.List("")
	if !day.OK || len(day.Data.Sales) != 1 || len(day.Data.Totals) != 2 {
		t.Fatalf("List = %+v", day)
	}
	syp, usd := day.Data.Totals[0], day.Data.Totals[1]
	if syp != (api.DayTotalsDTO{Currency: "SYP", Sales: 1, Charged: "73000", CashIn: "0", ChangeOut: "2000", Voided: "0", OnCredit: "0"}) ||
		usd != (api.DayTotalsDTO{Currency: "USD", Charged: "0.00", CashIn: "5.00", ChangeOut: "0.00", Voided: "0.00", OnCredit: "0.00"}) {
		t.Fatalf("totals = %+v", day.Data.Totals)
	}
	if r := set.Sales.List("14/09/2026"); codeOf(t, r) != "lite.sales.invalid_date" {
		t.Fatal("a date that is not YYYY-MM-DD")
	}

	// Voids, the verifier and the note are the owner's.
	void := api.VoidInput{SaleID: sold.Data.ID, Reason: "خطأ في الكمية"}
	if r := set.Sales.Void(void); codeOf(t, r) != ownerdomain.CodeRequired {
		t.Fatal("a void outside owner mode")
	}
	if r := set.Sales.Verify(); codeOf(t, r) != ownerdomain.CodeRequired {
		t.Fatal("the verifier answered outside owner mode")
	}
	if r := set.Till.SetCashNote("1000"); codeOf(t, r) != ownerdomain.CodeRequired {
		t.Fatal("the cash note changed outside owner mode")
	}
	if note := set.Till.CashNote(); !note.OK || note.Data != (api.CashNoteDTO{Currency: "SYP", Note: "500"}) {
		t.Fatalf("CashNote = %+v", note)
	}

	elevate(t, set)
	if sold.Data.VoidReturn != "73000" || sold.Data.VoidReturnCurrency != "SYP" {
		t.Fatalf("a cash sale's void hands back its total = %+v", sold.Data)
	}
	if r := set.Sales.Void(api.VoidInput{SaleID: sold.Data.ID}); codeOf(t, r) != salesdomain.CodeVoidReasonRequired {
		t.Fatal("a void without a reason")
	}
	voided := set.Sales.Void(void)
	if !voided.OK || voided.Data.Status != "voided" || voided.Data.VoidReason != void.Reason || voided.Data.VoidedAt == "" {
		t.Fatalf("Void = %+v", voided)
	}
	if r := set.Sales.Void(void); codeOf(t, r) != salesdomain.CodeAlreadyVoided {
		t.Fatal("a sale voided twice")
	}
	if levels := set.Stock.Levels(); levels.Data[0].OnHand != "0.000" {
		t.Fatalf("stock after the void = %+v", levels)
	}
	if receipt := set.Sales.Receipt(sold.Data.ID); !receipt.OK || receipt.Data.Status != "voided" {
		t.Fatalf("Receipt = %+v", receipt)
	}
	if r := set.Sales.Receipt("no-such-sale"); codeOf(t, r) != salesdomain.CodeSaleNotFound {
		t.Fatal("a receipt for a malformed id")
	}
	if r := set.Till.Checkout(api.CheckoutInput{Cart: discounted, Token: dq.Token}); !r.OK || r.Data.ReceiptNo != 2 || r.Data.Lines[0].NetUSD != "2.92" {
		t.Fatalf("the discount in owner mode = %+v", r)
	}
	if findings := set.Sales.Verify(); !findings.OK || len(findings.Data) != 0 {
		t.Fatalf("Verify = %+v", findings)
	}
	if note := set.Till.SetCashNote("١٠٠٠"); !note.OK || note.Data.Note != "1000" {
		t.Fatalf("SetCashNote = %+v", note)
	}
	if r := set.Till.SetCashNote("0"); codeOf(t, r) != settingsdomain.CodeInvalidCashNote {
		t.Fatal("a note of nothing")
	}
}

// TestARateChangedAfterTheQuoteRefusesTheCheckout is the stale token for real: the owner's new rate lands between the
// cashier seeing a total and pressing Pay.
func TestARateChangedAfterTheQuoteRefusesTheCheckout(t *testing.T) {
	set, oil := tillShop(t)
	cart := api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "1"}}}
	before := quoted(t, set, cart)
	elevate(t, set)
	if r := set.FX.SetRate(api.SetRateInput{Rate: "15500"}); !r.OK {
		t.Fatal(r.Error)
	}
	if r := set.Till.Checkout(api.CheckoutInput{Cart: cart, Token: before.Token}); codeOf(t, r) != salesdomain.CodeQuoteStale {
		t.Fatal("a sale at the old rate")
	}
	after := quoted(t, set, cart)
	if after.Total == before.Total || after.Rate != "15500" {
		t.Fatalf("the new quote = %+v", after)
	}
	if r := set.Till.Checkout(api.CheckoutInput{Cart: cart, Token: after.Token}); !r.OK || r.Data.Rate != "15500" {
		t.Fatalf("checkout at the new rate = %+v", r)
	}
}

// TestADollarSaleGivesDollarChange is Q-L4.3: a dollar total paid in dollars is not cash-rounded and its change is dollars.
func TestADollarSaleGivesDollarChange(t *testing.T) {
	set, oil := tillShop(t)
	cart := api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "1"}}, Settlement: "USD", TenderCurrency: "USD", Tendered: "10"}
	q := quoted(t, set, cart)
	if q.Total != "3.25" || q.Rounding != "0.00" || q.CashNote != "" || q.ChangeCurrency != "USD" || q.Change != "6.75" || q.TotalOther != "48750" {
		t.Fatalf("a dollar quote = %+v", q)
	}
	pounds := cart
	pounds.ChangeCurrency = "SYP"
	if r := set.Till.Quote(pounds); codeOf(t, r) != salesdomain.CodeChangeCurrency {
		t.Fatal("pound change for dollars paid in dollars")
	}
}

func TestNoRateMeansNoSale(t *testing.T) {
	set, app := attached(t, litetest.Logger())
	if _, err := app.Owner.SetUp(t.Context(), testPIN); err != nil {
		t.Fatal(err)
	}
	p := create(t, set, "زيت", "l")
	if r := set.Till.Quote(api.CartInput{Lines: []api.CartLineInput{{ProductID: p.ID, Quantity: "1"}}}); codeOf(t, r) != salesdomain.CodeNoRate {
		t.Fatal("a quote with no rate")
	}
	if r := set.Till.Quote(api.CartInput{Lines: []api.CartLineInput{{ProductID: "nope", Quantity: "1"}}}); codeOf(t, r) != salesdomain.CodeUnknownProduct {
		t.Fatal("a malformed product id")
	}
}

// saleDTOs are every DTO a sale or a cart crosses the boundary in.
var saleDTOs = []reflect.Type{
	reflect.TypeFor[api.CartLineDTO](), reflect.TypeFor[api.CartQuoteDTO](), reflect.TypeFor[api.ScanDTO](),
	reflect.TypeFor[api.SaleDTO](), reflect.TypeFor[api.SaleLineDTO](), reflect.TypeFor[api.DayTotalsDTO](),
}

// TestASaleCarriesNoCost holds the till and sales DTOs free of cost: cost is the owner's and reaches a screen with
// profit (Q-L2.4, L6).
func TestASaleCarriesNoCost(t *testing.T) {
	for _, typ := range saleDTOs {
		for i := range typ.NumField() {
			f := typ.Field(i)
			name := strings.ToLower(f.Name + f.Tag.Get("json"))
			for _, forbidden := range []string{"cost", "profit", "margin", "avg", "average"} {
				if strings.Contains(name, forbidden) {
					t.Errorf("%s.%s carries %s to the till", typ.Name(), f.Name, forbidden)
				}
			}
		}
	}
}

// TestEveryTillFigureIsAString holds the frontend out of arithmetic: no figure crosses as a number (DESIGN D9). The
// integers allowed are counts and receipt numbers.
func TestEveryTillFigureIsAString(t *testing.T) {
	counts := map[string]bool{"ReceiptNo": true, "LineNo": true, "Sales": true, "Voids": true, "UnitDecimals": true}
	for _, typ := range saleDTOs {
		for i := range typ.NumField() {
			f := typ.Field(i)
			switch f.Type.Kind() {
			case reflect.String, reflect.Bool, reflect.Slice:
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
