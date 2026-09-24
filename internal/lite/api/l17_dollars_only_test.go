package api_test

import (
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/api"
	"github.com/mizan-erp/mizan/internal/lite/moneyfmt"
	"github.com/mizan-erp/mizan/internal/lite/sheets"
)

// poundWords are what any pound figure or pound column is read by, in both languages.
var poundWords = []string{"ل.س", "ليرة", "SYP", "pound"}

// pounds returns the pound words a text carries.
func pounds(text string) []string {
	var found []string
	for _, w := range poundWords {
		if strings.Contains(text, w) {
			found = append(found, w)
		}
	}
	return found
}

func sheetText(book []sheets.Sheet) string {
	var b strings.Builder
	for _, sh := range book {
		for _, row := range sh.Rows {
			for _, c := range row {
				b.WriteString(c.Text + " | ")
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// TestADollarsOnlyShopReadsNoPounds (0.10.1, the owner's report of 2026-09-24): once a shop has gone over to dollars
// only, nothing Go sends a screen or prints carries a pound reading of a dollar figure — not a product's price, a
// balance's reference, the stock's value, the losses, the end of the day, the report exports or a shelf label. Each is
// seen WITH its pounds first, so the test cannot pass on a shop that had none to hide.
func TestADollarsOnlyShopReadsNoPounds(t *testing.T) {
	set, oil := tillShop(t) // olive oil at $3.25, the rate 15,000
	elevate(t, set)
	if r := set.Stock.Opening(api.ReceiveInput{ProductID: oil.ID, Quantity: "10", CostMode: "total", Cost: "20", Currency: "USD"}); !r.OK {
		t.Fatal(r.Error)
	}
	if r := set.Stock.Adjust(api.AdjustInput{ProductID: oil.ID, Direction: "out", Quantity: "1", Reason: "spoiled", Note: "انكسرت"}); !r.OK {
		t.Fatal(r.Error)
	}
	samir := set.Customers.Create(api.CustomerInput{Name: "سمير"}).Data
	if r := set.Customers.Opening(api.DebtAmountInput{CustomerID: samir.ID, Currency: "USD", Amount: "12"}); !r.OK {
		t.Fatal(r.Error)
	}
	saleOnTheTill(t, set, oil, api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "2"}}, Settlement: "USD"})
	today := set.Reports.Day("").Data.Date

	type reading struct {
		name string
		text func(t *testing.T) string
	}
	readings := []reading{
		{"the product's price", func(t *testing.T) string {
			products := set.Catalog.Products(api.ProductQueryDTO{})
			if !products.OK {
				t.Fatal(products.Error)
			}
			var b strings.Builder
			for _, p := range products.Data {
				b.WriteString(p.ConvertedPrice + " " + p.ConvertedCurrency + "\n")
			}
			return b.String()
		}},
		{"a balance's reference", func(t *testing.T) string {
			out := set.Customers.Outstanding()
			if !out.OK {
				t.Fatal(out.Error)
			}
			var b strings.Builder
			for _, c := range out.Data.Customers {
				for _, bal := range c.Balances {
					b.WriteString(bal.Reference + " " + bal.ReferenceCurrency + "\n")
				}
			}
			return b.String()
		}},
		{"the stock's value", func(t *testing.T) string {
			v := set.Stock.Valuation()
			if !v.OK {
				t.Fatal(v.Error)
			}
			text := v.Data.TotalLocal + " " + v.Data.LocalCurrency
			for _, l := range v.Data.Lines {
				text += " " + l.ValueLocal
			}
			return text
		}},
		{"the losses", func(t *testing.T) string {
			r := set.Reports.Losses(api.RangeInput{})
			if !r.OK {
				t.Fatal(r.Error)
			}
			text := r.Data.LocalCurrency + " " + r.Data.Total.Local
			for _, l := range r.Data.Lines {
				text += " " + l.Value.Local
			}
			for _, x := range r.Data.ByReason {
				text += " " + x.Value.Local
			}
			return text
		}},
		{"the end-of-day report", func(t *testing.T) string {
			doc, err := api.PrintDocument(set, "zreport", today)
			if err != nil {
				t.Fatal(err)
			}
			return textOf(doc)
		}},
		{"a shelf label", func(t *testing.T) string {
			doc, err := api.PrintDocument(set, "label", oil.ID)
			if err != nil {
				t.Fatal(err)
			}
			return textOf(doc)
		}},
	}
	for _, kind := range []string{"day", "month", "products", "stock"} {
		in := api.ExportReportInput{Kind: kind, Date: today, Month: today[:7], From: today, To: today}
		readings = append(readings,
			reading{"the " + kind + " report's PDF", func(t *testing.T) string {
				doc, err := api.ExportDocument(set, kind, in)
				if err != nil {
					t.Fatal(err)
				}
				return textOf(doc)
			}},
			reading{"the " + kind + " report's workbook", func(t *testing.T) string {
				book, err := api.ExportSheets(set, in)
				if err != nil {
					t.Fatal(err)
				}
				return sheetText(book)
			}})
	}

	// Before going over, every one of them reads pounds — or the test below proves nothing.
	for _, r := range readings {
		text := r.text(t)
		if r.name == "the product's price" || r.name == "a balance's reference" || r.name == "the stock's value" || r.name == "the losses" {
			if !strings.Contains(text, "SYP") {
				t.Fatalf("%s carries no pounds before going over: %q", r.name, text)
			}
			continue
		}
		if len(pounds(text)) == 0 {
			t.Fatalf("%s carries no pounds before going over:\n%s", r.name, text)
		}
	}

	plan := set.Settings.USDOnlyPlan()
	if !plan.OK {
		t.Fatal(plan.Error)
	}
	if r := set.Settings.SwitchToUSDOnly(plan.Data.Token); !r.OK {
		t.Fatal(r.Error)
	}
	for _, r := range readings {
		text := r.text(t)
		if found := pounds(text); len(found) > 0 {
			t.Errorf("%s still reads pounds in a dollars-only shop (%v):\n%s", r.name, found, text)
		}
	}
	// And the dollars are all still there.
	if doc, _ := api.PrintDocument(set, "zreport", today); !strings.Contains(textOf(doc), "دولار") {
		t.Errorf("the end-of-day report has no dollars:\n%s", textOf(doc))
	}
	if doc, _ := api.ExportDocument(set, "day", api.ExportReportInput{Kind: "day", Date: today}); !strings.Contains(textOf(doc), "6.50") {
		t.Errorf("the day's statement lost its revenue of $6.50:\n%s", textOf(doc))
	}
}

// TestADualZeroIsNotPrinted (0.10.1): a shop reading both pounds formats a zero as "0 (0)". A sale with no discount
// printed "Discount (0) 0" on its receipt and its invoice until the check learned the dual shape — found drawing the
// invoice of 2026-09-24.
func TestADualZeroIsNotPrinted(t *testing.T) {
	set, oil := tillShop(t)
	elevate(t, set)
	if r := set.Stock.Opening(api.ReceiveInput{ProductID: oil.ID, Quantity: "10", CostMode: "total", Cost: "20", Currency: "USD"}); !r.OK {
		t.Fatal(r.Error)
	}
	setDisplay(t, set, moneyfmt.Dual)
	sale := saleOnTheTill(t, set, oil, api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "2"}}, Settlement: "SYP"})
	if sale.DiscountLocal != "0 (0)" {
		t.Fatalf("the dual zero this test is about reads %q", sale.DiscountLocal)
	}
	for _, kind := range []string{"sale", "invoice"} {
		doc, err := api.PrintDocument(set, kind, sale.ID)
		if err != nil {
			t.Fatal(err)
		}
		if text := textOf(doc); strings.Contains(text, "الحسم") {
			t.Errorf("the %s prints a discount of nothing:\n%s", kind, text)
		}
	}
}
