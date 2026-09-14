package domain_test

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/sales/domain"
)

const (
	u = int64(1_000_000) // one unit, or one major currency unit, at 10⁻⁶
	n = int64(1_000_000_000)
)

var (
	syp = domain.Currency{Code: "SYP", Decimals: 0}
	usd = domain.Currency{Code: "USD", Decimals: 2}
)

// tb is what both testing.T and rapid.T offer.
type tb interface {
	Helper()
	Fatal(args ...any)
}

func newID(t tb) id.ID {
	t.Helper()
	v, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

type shop struct {
	ctx                 domain.Context
	oil, bulgur, jar    id.ID
	halfLitre, uncosted id.ID
}

// newShop is a context at 15,000 pounds a dollar and a 500-pound note: olive oil at $6.50 a litre (12 on hand at $4.68),
// bulgur at 18,000 a kilo (40 on hand at $0.90), molasses at 45,000 a jar (3 on hand at $2.00), and a product never
// received.
func newShop(t tb) shop {
	s := shop{oil: newID(t), bulgur: newID(t), jar: newID(t), halfLitre: newID(t), uncosted: newID(t)}
	s.ctx = domain.Context{
		Local: syp, USD: usd, CashNote: 500,
		Rate: domain.Rate{ID: newID(t), Nano: 15_000 * n, RecordedAt: time.Date(2026, 9, 14, 6, 0, 0, 0, time.UTC)},
		Products: map[id.ID]domain.Product{
			s.oil:       {ID: s.oil, NameAR: "زيت زيتون", NameEN: "Olive oil", UnitCode: "l", UnitDecimals: 3, PriceCurrency: "USD", PriceMicro: 6_500_000, Active: true, RowVersion: 1},
			s.bulgur:    {ID: s.bulgur, NameAR: "برغل", UnitCode: "kg", UnitDecimals: 3, PriceCurrency: "SYP", PriceMicro: 18_000 * u, Active: true, RowVersion: 1},
			s.jar:       {ID: s.jar, NameAR: "دبس رمان", UnitCode: "jar", UnitDecimals: 0, PriceCurrency: "SYP", PriceMicro: 45_000 * u, Active: true, RowVersion: 1},
			s.halfLitre: {ID: s.halfLitre, NameAR: "زيت", UnitCode: "l", UnitDecimals: 3, PriceCurrency: "USD", PriceMicro: 3_330_000, Active: true, RowVersion: 1},
			s.uncosted:  {ID: s.uncosted, NameAR: "شنكليش", UnitCode: "piece", UnitDecimals: 0, PriceCurrency: "SYP", PriceMicro: 12_000 * u, Active: true, RowVersion: 1},
		},
		Stock: map[id.ID]domain.Stocked{
			s.oil:       {OnHandMicro: 12 * u, CostKnown: true, AvgCostMicro: 4_680_000},
			s.bulgur:    {OnHandMicro: 40 * u, CostKnown: true, AvgCostMicro: 900_000},
			s.jar:       {OnHandMicro: 3 * u, CostKnown: true, AvgCostMicro: 2 * u},
			s.halfLitre: {OnHandMicro: 10 * u, CostKnown: true, AvgCostMicro: 2 * u},
		},
	}
	return s
}

func line(p id.ID, qty string) domain.LineInput { return domain.LineInput{ProductID: p, Quantity: qty} }

func price(t *testing.T, in domain.CartInput, c domain.Context) domain.Quote {
	t.Helper()
	q, err := domain.Price(in, c)
	if err != nil {
		t.Fatalf("Price: %v", err)
	}
	return q
}

func code(err error) string { return errs.CodeOf(err) }

func TestALineIsPricedInBothCurrenciesFromOneExactProduct(t *testing.T) {
	s := newShop(t)
	q := price(t, domain.CartInput{Lines: []domain.LineInput{line(s.bulgur, "١٫٧٥٠"), line(s.oil, "2")}}, s.ctx)
	// 1.750 kg at 18,000: 31,500 pounds, and 31,500 ÷ 15,000 = $2.10.
	if b := q.Lines[0]; b.GrossLocalMinor != 31_500 || b.GrossUSDMinor != 210 || b.QuantityMicro != 1_750_000 {
		t.Fatalf("bulgur = %+v", b.Line)
	}
	// 2 L at $6.50: $13.00, and 195,000 pounds.
	if o := q.Lines[1]; o.GrossUSDMinor != 1_300 || o.GrossLocalMinor != 195_000 {
		t.Fatalf("oil = %+v", o.Line)
	}
	// DESIGN §4.4 at 13,000: half a litre at $3.33 is 21,645 pounds, not the 21,710 of rounding twice.
	s.ctx.Rate.Nano = 13_000 * n
	q = price(t, domain.CartInput{Lines: []domain.LineInput{line(s.halfLitre, "0.5")}}, s.ctx)
	if l := q.Lines[0]; l.GrossLocalMinor != 21_645 || l.GrossUSDMinor != 167 {
		t.Fatalf("half a litre = %+v", l.Line)
	}
}

func TestPrintedLinesAddUpToThePrintedTotalExactly(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		s := newShop(rt)
		products := []id.ID{s.oil, s.bulgur, s.jar, s.halfLitre, s.uncosted}
		s.ctx.Rate.Nano = rapid.Int64Range(100*n, 50_000*n).Draw(rt, "rate")
		s.ctx.CashNote = rapid.SampledFrom([]int64{1, 50, 100, 500, 1000}).Draw(rt, "note")
		lines := rapid.IntRange(1, 60).Draw(rt, "lines")
		var in domain.CartInput
		for i := range lines {
			p := products[rapid.IntRange(0, len(products)-1).Draw(rt, fmt.Sprintf("product%d", i))]
			qty := strconv.Itoa(rapid.IntRange(1, 30).Draw(rt, fmt.Sprintf("qty%d", i)))
			pct := strconv.Itoa(rapid.IntRange(0, 100).Draw(rt, fmt.Sprintf("pct%d", i)))
			in.Lines = append(in.Lines, domain.LineInput{ProductID: p, Quantity: qty, DiscountPercent: pct})
		}
		in.Settlement = rapid.SampledFrom([]string{"SYP", "USD"}).Draw(rt, "settlement")
		q, err := domain.Price(in, s.ctx)
		if err != nil {
			rt.Fatal(err)
		}
		var local, dollars int64
		for _, l := range q.Lines {
			if l.DiscountLocalMinor > l.GrossLocalMinor || l.DiscountUSDMinor > l.GrossUSDMinor {
				rt.Fatalf("a discount exceeds its line: %+v", l.Line)
			}
			local += l.NetLocalMinor()
			dollars += l.NetUSDMinor()
		}
		if local != q.LinesLocalMinor || dollars != q.LinesUSDMinor {
			rt.Fatalf("lines %d/%d, totals %d/%d", local, dollars, q.LinesLocalMinor, q.LinesUSDMinor)
		}
		lines64 := q.LinesLocalMinor
		if q.Settlement.Code == "USD" {
			lines64 = q.LinesUSDMinor
			if q.RoundingMinor != 0 {
				rt.Fatal("dollars were cash-rounded")
			}
		}
		if q.TotalMinor != lines64+q.RoundingMinor {
			rt.Fatalf("total %d ≠ lines %d + rounding %d", q.TotalMinor, lines64, q.RoundingMinor)
		}
		if q.Settlement.Code == "SYP" && (q.TotalMinor%s.ctx.CashNote != 0 || 2*abs(q.RoundingMinor) > s.ctx.CashNote) {
			rt.Fatalf("total %d at note %d, rounding %d", q.TotalMinor, s.ctx.CashNote, q.RoundingMinor)
		}
	})
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func TestCashRoundingIsRecordedAndHalfUp(t *testing.T) {
	s := newShop(t)
	// A product at exactly the amounts under test, priced in pounds.
	for _, c := range []struct{ lines, total, rounding int64 }{
		{132_250, 132_500, 250},  // half a note rounds up
		{132_249, 132_000, -249}, // just under rounds down
		{132_500, 132_500, 0},
		{200, 0, -200}, // under half the smallest note
		{250, 500, 250},
	} {
		p := newID(t)
		s.ctx.Products[p] = domain.Product{ID: p, NameAR: "x", UnitCode: "piece", PriceCurrency: "SYP", PriceMicro: c.lines * u, Active: true, RowVersion: 1}
		q := price(t, domain.CartInput{Lines: []domain.LineInput{line(p, "1")}}, s.ctx)
		if q.LinesLocalMinor != c.lines || q.TotalMinor != c.total || q.RoundingMinor != c.rounding || q.CashNoteMinor != 500 {
			t.Errorf("lines %d → total %d rounding %d, want %d %d", c.lines, q.TotalMinor, q.RoundingMinor, c.total, c.rounding)
		}
	}
}

func TestDollarsAreNotCashRounded(t *testing.T) {
	s := newShop(t)
	q := price(t, domain.CartInput{Lines: []domain.LineInput{line(s.jar, "1")}, Settlement: "USD"}, s.ctx)
	// 45,000 ÷ 15,000 = $3.00; the pounds reference is the lines in pounds, unrounded.
	if q.TotalMinor != 300 || q.RoundingMinor != 0 || q.Settlement != usd || q.Other != syp || q.TotalOtherMinor != 45_000 {
		t.Fatalf("quote = %+v", q)
	}
}

// TestChangeIsInPoundsUnlessEverythingIsInDollars is L4 §3.3's table with the owner's answers (Q-L4.2, Q-L4.3).
func TestChangeIsInPoundsUnlessEverythingIsInDollars(t *testing.T) {
	s := newShop(t)
	// A pounds sale of 132,500 exactly.
	p := newID(t)
	s.ctx.Products[p] = domain.Product{ID: p, NameAR: "سلة", UnitCode: "piece", PriceCurrency: "SYP", PriceMicro: 132_500 * u, Active: true, RowVersion: 1}
	cart := func(settle, tenderCur, tendered, changeCur string) domain.CartInput {
		return domain.CartInput{Lines: []domain.LineInput{line(p, "1")}, Settlement: settle, TenderCurrency: tenderCur, Tendered: tendered, ChangeCurrency: changeCur}
	}
	for _, c := range []struct {
		name             string
		in               domain.CartInput
		changeCur        string
		change, tendered int64
	}{
		{"pounds for pounds", cart("SYP", "SYP", "150000", ""), "SYP", 17_500, 150_000},
		{"a $10 note for a pounds total: change in pounds", cart("SYP", "USD", "10", ""), "SYP", 17_500, 1_000},
		{"…or in dollars, when the customer asks", cart("SYP", "USD", "10", "USD"), "USD", 117, 1_000}, // 10 − 8.8333… = 1.1666… → 1.17
		{"a dollar total paid in dollars: change in dollars", cart("USD", "USD", "10", ""), "USD", 117, 1_000},
		{"a dollar total paid in pounds: change in pounds, to the note", cart("USD", "SYP", "150000", ""), "SYP", 17_500, 150_000},
		{"a pounds total paid in pounds, change asked in dollars", cart("SYP", "SYP", "150000", "USD"), "USD", 117, 150_000},
	} {
		q, err := domain.Price(c.in, s.ctx)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if q.Change.Code != c.changeCur || q.ChangeMinor != c.change || q.TenderedMinor != c.tendered || !q.TenderGiven {
			t.Errorf("%s: change %s %d, tendered %d", c.name, q.Change.Code, q.ChangeMinor, q.TenderedMinor)
		}
	}
	// A dollar total paid in dollars gives change strictly in dollars (Q-L4.3).
	if _, err := domain.Price(cart("USD", "USD", "10", "SYP"), s.ctx); code(err) != domain.CodeChangeCurrency {
		t.Fatalf("pounds change for an all-dollar sale: %v", err)
	}
	// Short.
	if _, err := domain.Price(cart("SYP", "USD", "8", ""), s.ctx); code(err) != domain.CodeTenderShort {
		t.Fatalf("$8 for 132,500 at 15,000: %v", err)
	}
	// Exact money.
	q := price(t, cart("SYP", "USD", "", "USD"), s.ctx)
	if q.TenderGiven || q.Tender != syp || q.TenderedMinor != 132_500 || q.ChangeMinor != 0 || q.Change != syp {
		t.Fatalf("exact money = %+v", q)
	}
	if _, err := domain.Price(cart("SYP", "USD", "10.005", ""), s.ctx); code(err) != domain.CodeTenderDecimals {
		t.Fatalf("a tender of a tenth of a cent: %v", err)
	}
}

func TestDiscountsAreKeptInBothCurrencies(t *testing.T) {
	s := newShop(t)
	q := price(t, domain.CartInput{Lines: []domain.LineInput{{ProductID: s.oil, Quantity: "2", DiscountPercent: "10"}}, SaleDiscount: "5000"}, s.ctx)
	o := q.Lines[0]
	// 10% of $13.00 and of 195,000 pounds, each rounded once.
	if o.DiscountPercentMicro != 100_000 || o.DiscountUSDMinor != 130 || o.DiscountLocalMinor != 19_500 || o.NetLocalMinor() != 175_500 {
		t.Fatalf("line = %+v", o.Line)
	}
	// 5,000 pounds off the sale, $0.33 in dollars; 175,500 − 5,000 = 170,500, on a note.
	if q.DiscountLocalMinor != 5_000 || q.DiscountUSDMinor != 33 || q.TotalMinor != 170_500 || q.RoundingMinor != 0 || !q.Discounted {
		t.Fatalf("quote = %+v", q)
	}
	if price(t, domain.CartInput{Lines: []domain.LineInput{line(s.oil, "2")}}, s.ctx).Discounted {
		t.Fatal("a cart with no discount is marked discounted")
	}
	for _, c := range []struct {
		in   domain.CartInput
		code string
	}{
		{domain.CartInput{Lines: []domain.LineInput{{ProductID: s.oil, Quantity: "1", DiscountPercent: "101"}}}, domain.CodeDiscountInvalid},
		{domain.CartInput{Lines: []domain.LineInput{{ProductID: s.oil, Quantity: "1", DiscountPercent: "1.125"}}}, domain.CodeDiscountInvalid},
		{domain.CartInput{Lines: []domain.LineInput{line(s.jar, "1")}, SaleDiscount: "45500"}, domain.CodeDiscountTooLarge},
		{domain.CartInput{Lines: []domain.LineInput{line(s.jar, "1")}, SaleDiscount: "0.5"}, domain.CodeDiscountInvalid},
	} {
		if _, err := domain.Price(c.in, s.ctx); code(err) != c.code {
			t.Errorf("%+v: %v, want %s", c.in, err, c.code)
		}
	}
	// A 100% sale discount in pounds converts to exactly the whole sale in dollars, never a cent more.
	q = price(t, domain.CartInput{Lines: []domain.LineInput{line(s.bulgur, "0.001")}, SaleDiscount: "18"}, s.ctx)
	if q.DiscountUSDMinor > q.LinesUSDMinor || q.TotalMinor != 0 {
		t.Fatalf("whole-sale discount = %+v", q)
	}
}

func TestTheQuoteTokenChangesWithTheRateAPriceOrTheCart(t *testing.T) {
	s := newShop(t)
	in := domain.CartInput{Lines: []domain.LineInput{line(s.oil, "2"), line(s.jar, "1")}, TenderCurrency: "USD", Tendered: "100"}
	base := price(t, in, s.ctx).Token
	if again := price(t, in, s.ctx).Token; again != base || len(base) != 64 {
		t.Fatalf("the same cart gave another token: %s", again)
	}
	changes := map[string]func(domain.CartInput, domain.Context) (domain.CartInput, domain.Context){
		"rate": func(i domain.CartInput, c domain.Context) (domain.CartInput, domain.Context) {
			c.Rate.Nano++
			return i, c
		},
		"rate id": func(i domain.CartInput, c domain.Context) (domain.CartInput, domain.Context) {
			c.Rate.ID = newID(t)
			return i, c
		},
		"price": func(i domain.CartInput, c domain.Context) (domain.CartInput, domain.Context) {
			p := c.Products[s.oil]
			p.PriceMicro++
			c.Products = copyProducts(c.Products, p)
			return i, c
		},
		"row version": func(i domain.CartInput, c domain.Context) (domain.CartInput, domain.Context) {
			p := c.Products[s.oil]
			p.RowVersion++
			c.Products = copyProducts(c.Products, p)
			return i, c
		},
		"quantity": func(i domain.CartInput, c domain.Context) (domain.CartInput, domain.Context) {
			i.Lines = []domain.LineInput{line(s.oil, "3"), line(s.jar, "1")}
			return i, c
		},
		"discount": func(i domain.CartInput, c domain.Context) (domain.CartInput, domain.Context) {
			i.Lines = []domain.LineInput{{ProductID: s.oil, Quantity: "2", DiscountPercent: "5"}, line(s.jar, "1")}
			return i, c
		},
		"tender": func(i domain.CartInput, c domain.Context) (domain.CartInput, domain.Context) {
			i.Tendered = "50"
			return i, c
		},
		"note": func(i domain.CartInput, c domain.Context) (domain.CartInput, domain.Context) {
			c.CashNote = 1000
			return i, c
		},
		"settle": func(i domain.CartInput, c domain.Context) (domain.CartInput, domain.Context) {
			i.Settlement = "USD"
			return i, c
		},
	}
	for name, change := range changes {
		i, c := change(in, s.ctx)
		if price(t, i, c).Token == base {
			t.Errorf("changing the %s left the token unchanged", name)
		}
	}
	// Stock is not in the token: a warning may change, the charge does not.
	c := s.ctx
	c.Stock = map[id.ID]domain.Stocked{}
	if price(t, in, c).Token != base {
		t.Error("the stock level changed the token")
	}
}

func copyProducts(m map[id.ID]domain.Product, p domain.Product) map[id.ID]domain.Product {
	out := map[id.ID]domain.Product{}
	for k, v := range m {
		out[k] = v
	}
	out[p.ID] = p
	return out
}

func TestCostIsSnapshottedAtTheSalesRate(t *testing.T) {
	s := newShop(t)
	q := price(t, domain.CartInput{Lines: []domain.LineInput{line(s.oil, "2")}}, s.ctx)
	// 2 L at $4.68: $9.36, and 140,400 pounds at 15,000.
	if o := q.Lines[0]; !o.CostKnown || o.UnitCostMicro != 4_680_000 || o.CostUSDMinor != 936 || o.CostLocalMinor != 140_400 || q.CostUSDMinor != 936 {
		t.Fatalf("cost = %+v", o.Line)
	}
}

func TestSellingBeyondStockIsWarnedNotRefused(t *testing.T) {
	s := newShop(t)
	q := price(t, domain.CartInput{Lines: []domain.LineInput{line(s.jar, "2"), line(s.jar, "2")}}, s.ctx)
	if len(q.Lines[0].Warnings) != 0 {
		t.Fatalf("2 of 3 jars warned: %v", q.Lines[0].Warnings)
	}
	if w := q.Lines[1].Warnings; len(w) != 1 || w[0] != domain.WarnBeyondStock {
		t.Fatalf("the second line takes the jars to 4 of 3: %v", w)
	}
}

// TestANeverReceivedProductSellsWithUnknownCost is Q-L4.8: sold with a warning, its cost recorded as unknown — zero with
// cost_known false, never a guess that would skew L6's profit.
func TestANeverReceivedProductSellsWithUnknownCost(t *testing.T) {
	s := newShop(t)
	q := price(t, domain.CartInput{Lines: []domain.LineInput{line(s.uncosted, "1")}}, s.ctx)
	u := q.Lines[0]
	if len(u.Warnings) != 2 || u.Warnings[0] != domain.WarnBeyondStock || u.Warnings[1] != domain.WarnNoCost {
		t.Fatalf("warnings = %v", u.Warnings)
	}
	if u.CostKnown || u.CostUSDMinor != 0 || u.CostLocalMinor != 0 || u.UnitCostMicro != 0 || q.CostUSDMinor != 0 || u.GrossLocalMinor != 12_000 {
		t.Fatalf("never received = %+v", u.Line)
	}
}

// TestATenderBelowTheTotalIsRefused compares exactly, across currencies: a dollar tender that shows as the total's rounded
// dollars can still be short of the pounds it must cover.
func TestATenderBelowTheTotalIsRefused(t *testing.T) {
	s := newShop(t)
	p := newID(t)
	s.ctx.Products[p] = domain.Product{ID: p, NameAR: "سلة", UnitCode: "piece", PriceCurrency: "SYP", PriceMicro: 132_500 * u, Active: true, RowVersion: 1}
	cart := func(settle, tenderCur, tendered string) domain.CartInput {
		return domain.CartInput{Lines: []domain.LineInput{line(p, "1")}, Settlement: settle, TenderCurrency: tenderCur, Tendered: tendered}
	}
	for _, c := range []struct {
		name  string
		in    domain.CartInput
		short bool
	}{
		{"a pound short", cart("SYP", "SYP", "132499"), true},
		{"pounds exactly", cart("SYP", "SYP", "132500"), false},
		{"$8.83 for 132,500 — the total's dollars, but 132,450 pounds", cart("SYP", "USD", "8.83"), true},
		{"$8.84 for 132,500", cart("SYP", "USD", "8.84"), false},
		{"a cent short of a dollar total", cart("USD", "USD", "8.82"), true},
		{"a dollar total exactly", cart("USD", "USD", "8.83"), false},
		{"a pound short of a dollar total's 132,450", cart("USD", "SYP", "132449"), true},
	} {
		_, err := domain.Price(c.in, s.ctx)
		if c.short && code(err) != domain.CodeTenderShort {
			t.Errorf("%s: %v, want refused as short", c.name, err)
		}
		if !c.short && err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

func TestACartIsRefusedBeforeAnythingIsPriced(t *testing.T) {
	s := newShop(t)
	inactive := newID(t)
	s.ctx.Products[inactive] = domain.Product{ID: inactive, NameAR: "قديم", UnitCode: "jar", PriceCurrency: "SYP", PriceMicro: u, Active: false, RowVersion: 1}
	many := make([]domain.LineInput, domain.MaxLines+1)
	for i := range many {
		many[i] = line(s.jar, "1")
	}
	for name, c := range map[string]struct {
		in    domain.CartInput
		code  string
		field string
	}{
		"empty":            {domain.CartInput{}, domain.CodeEmptyCart, ""},
		"too many lines":   {domain.CartInput{Lines: many}, domain.CodeTooManyLines, ""},
		"inactive":         {domain.CartInput{Lines: []domain.LineInput{line(s.oil, "1"), line(inactive, "1")}}, domain.CodeInactiveProduct, "lines.2.productId"},
		"unknown":          {domain.CartInput{Lines: []domain.LineInput{line(newID(t), "1")}}, domain.CodeUnknownProduct, "lines.1.productId"},
		"half a jar":       {domain.CartInput{Lines: []domain.LineInput{line(s.jar, "1.5")}}, domain.CodeQuantityDecimals, "lines.1.quantity"},
		"a fourth decimal": {domain.CartInput{Lines: []domain.LineInput{line(s.bulgur, "1.7505")}}, domain.CodeQuantityDecimals, "lines.1.quantity"},
		"nothing":          {domain.CartInput{Lines: []domain.LineInput{line(s.bulgur, "0")}}, domain.CodeQuantityRequired, "lines.1.quantity"},
		"a comma":          {domain.CartInput{Lines: []domain.LineInput{line(s.bulgur, "1,750")}}, "lite.number.grouping", "lines.1.quantity"},
		"a third currency": {domain.CartInput{Lines: []domain.LineInput{line(s.jar, "1")}, Settlement: "EUR"}, domain.CodeUnknownCurrency, ""},
	} {
		_, err := domain.Price(c.in, s.ctx)
		if code(err) != c.code {
			t.Errorf("%s: %v, want %s", name, err, c.code)
			continue
		}
		if c.field != "" {
			if typed, _ := errs.AsError(err); len(typed.Fields) == 0 || typed.Fields[len(typed.Fields)-1].Field != c.field {
				t.Errorf("%s: the refusal names %+v, want %s", name, typed.Fields, c.field)
			}
		}
	}
	s.ctx.Rate = domain.Rate{}
	if _, err := domain.Price(domain.CartInput{Lines: []domain.LineInput{line(s.jar, "1")}}, s.ctx); code(err) != domain.CodeNoRate {
		t.Fatalf("no rate: %v", err)
	}
}
