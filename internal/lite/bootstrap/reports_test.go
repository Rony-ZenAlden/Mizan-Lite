package bootstrap_test

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/bizdate"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/cashbook"
	cashbookdomain "github.com/mizan-erp/mizan/internal/lite/cashbook/domain"
	catalogdomain "github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	"github.com/mizan-erp/mizan/internal/lite/customers"
	customersdomain "github.com/mizan-erp/mizan/internal/lite/customers/domain"
	"github.com/mizan-erp/mizan/internal/lite/fx"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	ownerdomain "github.com/mizan-erp/mizan/internal/lite/owner/domain"
	"github.com/mizan-erp/mizan/internal/lite/owner/ownertest"
	"github.com/mizan-erp/mizan/internal/lite/reports"
	reportsdomain "github.com/mizan-erp/mizan/internal/lite/reports/domain"
	"github.com/mizan-erp/mizan/internal/lite/sales"
	salesdomain "github.com/mizan-erp/mizan/internal/lite/sales/domain"
	"github.com/mizan-erp/mizan/internal/lite/setup"
	"github.com/mizan-erp/mizan/internal/lite/stock"
	stockdomain "github.com/mizan-erp/mizan/internal/lite/stock/domain"
)

var damascus = time.FixedZone("Damascus", 3*3600)

// shop is a real graph under a clock the test steps, and an independent tally of every piece of cash that moved.
type shop struct {
	t        *testing.T
	ctx      context.Context
	app      *bootstrap.App
	clk      *clock.Fixed
	rng      *rand.Rand
	products []catalogdomain.Product
	unknown  catalogdomain.Product // sold, never received
	people   []customersdomain.Customer
	posted   []salesdomain.Sale
	payments []customersdomain.Entry
	money    []cashbookdomain.Entry
	// tally is the cash that moved, per business date and currency, counted from each recorded object.
	tally map[string]map[string]int64
	// counts is each count recorded, in order.
	counts []cashbookdomain.Entry
	// rateFixed keeps the rate where setup put it.
	rateFixed bool
}

func openShop(t *testing.T, seed int64) *shop {
	t.Helper()
	ctx := context.Background()
	clk := clock.NewFixed(time.Date(2026, 8, 1, 6, 0, 0, 0, time.UTC))
	app, err := bootstrap.Start(ctx, bootstrap.Options{Paths: dataDir(t), Logger: litetest.Logger(), PINHasher: ownertest.Hasher(), Clock: clk, Location: damascus})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Shutdown(ctx) })
	if _, err = app.Setup.Run(ctx, setup.Input{ShopName: "المونة", Locale: "ar", PIN: "246813", Rate: "15000"}); err != nil {
		t.Fatal(err)
	}
	s := &shop{t: t, ctx: ctx, app: app, clk: clk, rng: rand.New(rand.NewSource(seed)), tally: map[string]map[string]int64{}} //nolint:gosec // a fixed seed
	s.owner()
	for _, d := range []catalogdomain.Draft{
		{NameAR: "رز", UnitCode: "kg", PriceCurrency: "USD", Price: "1.50"},
		{NameAR: "زيت", UnitCode: "l", PriceCurrency: "SYP", Price: "42000"},
		{NameAR: "سكر", UnitCode: "kg", PriceCurrency: "SYP", Price: "20000"},
		{NameAR: "شاي", UnitCode: "piece", PriceCurrency: "USD", Price: "2.25"},
	} {
		p, createErr := app.Catalog.Create(ctx, d)
		if createErr != nil {
			t.Fatal(createErr)
		}
		s.products = append(s.products, p)
	}
	if s.unknown, err = app.Catalog.Create(ctx, catalogdomain.Draft{NameAR: "جبنة", UnitCode: "kg", PriceCurrency: "USD", Price: "6"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"أبو محمد", "أم علي", "سمير"} {
		c, err := app.Customers.Create(ctx, customersdomain.Draft{Name: name})
		if err != nil {
			t.Fatal(err)
		}
		s.people = append(s.people, c)
	}
	return s
}

func (s *shop) owner() {
	s.t.Helper()
	if _, err := s.app.Owner.Elevate(s.ctx, "246813"); err != nil {
		s.t.Fatal(err)
	}
}

func (s *shop) today() string { return bizdate.Date(s.clk.Now(), damascus) }

func (s *shop) move(date, currency string, minor int64) {
	if s.tally[date] == nil {
		s.tally[date] = map[string]int64{}
	}
	s.tally[date][currency] += minor
}

func (s *shop) must(err error) {
	s.t.Helper()
	if err != nil {
		s.t.Fatal(err)
	}
}

// onHand is a product's stock now.
func (s *shop) onHand(productID id.ID) int64 {
	st, err := s.app.Stock.Stocked(s.ctx, productID)
	s.must(err)
	return st.OnHandMicro
}

func (s *shop) sell() {
	var lines []salesdomain.LineInput
	for range 1 + s.rng.Intn(3) {
		if s.rng.Intn(6) == 0 {
			lines = append(lines, salesdomain.LineInput{ProductID: s.unknown.ID, Quantity: "0.25"})
			continue
		}
		p := s.products[s.rng.Intn(len(s.products))]
		whole := s.onHand(p.ID) / 1_000_000
		for _, l := range lines {
			if l.ProductID == p.ID {
				whole = 0
			}
		}
		if whole < 1 {
			continue
		}
		line := salesdomain.LineInput{ProductID: p.ID, Quantity: strconv.FormatInt(1+s.rng.Int63n(min(whole, 3)), 10)}
		if s.rng.Intn(8) == 0 {
			line.DiscountPercent = "10"
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return
	}
	in := salesdomain.CartInput{Lines: lines, Settlement: []string{"SYP", "USD"}[s.rng.Intn(2)]}
	if s.rng.Intn(6) == 0 {
		in.SaleDiscount = map[string]string{"SYP": "500", "USD": "0.05"}[in.Settlement]
	}
	switch s.rng.Intn(4) {
	case 0:
		in.Payment, in.CustomerID = salesdomain.PaymentCredit, s.people[s.rng.Intn(len(s.people))].ID
		if s.rng.Intn(2) == 0 {
			in.TenderCurrency, in.Tendered = "SYP", "1000"
		}
	case 1:
		in.TenderCurrency, in.Tendered = "USD", "100"
	}
	s.owner()
	q, err := s.app.Sales.Quote(s.ctx, in)
	if err != nil {
		s.t.Fatalf("quote %+v: %v", in, err)
	}
	sale, err := s.app.Sales.Checkout(s.ctx, sales.CheckoutInput{Cart: in, Token: q.Token})
	if errs.CodeOf(err) == salesdomain.CodeCreditPaidInFull {
		return
	}
	s.must(err)
	s.move(sale.BusinessDate, sale.TenderedCurrency, sale.TenderedMinor)
	s.move(sale.BusinessDate, sale.ChangeCurrency, -sale.ChangeMinor)
	s.posted = append(s.posted, sale)
}

func (s *shop) void() {
	if len(s.posted) == 0 {
		return
	}
	i := s.rng.Intn(len(s.posted))
	sale := s.posted[i]
	s.posted = append(s.posted[:i], s.posted[i+1:]...)
	receipt, err := s.app.Sales.Receipt(s.ctx, sale.ID)
	s.must(err)
	s.owner()
	voided, err := s.app.Sales.Void(s.ctx, sales.VoidInput{SaleID: sale.ID, Reason: "خطأ"})
	s.must(err)
	back := receipt.TotalMinor // the till's rule, counted independently: a cash sale's total, a credit sale's paid now
	if receipt.Payment == salesdomain.PaymentCredit {
		back -= receipt.Credit.AmountMinor
	}
	s.move(voided.VoidBusinessDate, receipt.SettlementCurrency, -back)
}

func (s *shop) debts() {
	c := s.people[s.rng.Intn(len(s.people))]
	balances, err := s.app.Customers.Balances(s.ctx, c.ID)
	s.must(err)
	for currency, balance := range balances {
		switch {
		case balance > 0 && s.rng.Intn(2) == 0:
			in := customersdomain.CashInput{Currency: currency, TenderCurrency: []string{"SYP", "USD"}[s.rng.Intn(2)], All: s.rng.Intn(3) == 0, Amount: "1"}
			if in.TenderCurrency == "SYP" {
				in.Amount = "5000"
			}
			q, err := s.app.Customers.QuotePayment(s.ctx, c.ID, in)
			if err != nil {
				continue
			}
			e, err := s.app.Customers.RecordPayment(s.ctx, customers.PaymentInput{CustomerID: c.ID, Cash: in, Token: q.Token})
			s.must(err)
			s.move(e.BusinessDate, e.Cash.TenderedCurrency, e.Cash.TenderedMinor)
			s.move(e.BusinessDate, e.Cash.ChangeCurrency, -e.Cash.ChangeMinor)
			s.payments = append(s.payments, e)
		case balance > 0 && s.rng.Intn(5) == 0:
			s.owner()
			_, err := s.app.Customers.WriteOff(s.ctx, customers.AmountInput{CustomerID: c.ID, Currency: currency, All: true, Note: "سافر"})
			s.must(err)
		case balance < 0:
			s.owner()
			e, err := s.app.Customers.Refund(s.ctx, customers.RefundInput{CustomerID: c.ID, Cash: customersdomain.CashInput{Currency: currency, All: true}, Reason: "رد"})
			s.must(err)
			s.move(e.BusinessDate, e.Cash.TenderedCurrency, -e.Cash.TenderedMinor)
			s.move(e.BusinessDate, e.Cash.ChangeCurrency, e.Cash.ChangeMinor)
		}
	}
	if len(s.payments) > 0 && s.rng.Intn(12) == 0 {
		e := s.payments[len(s.payments)-1]
		s.payments = s.payments[:len(s.payments)-1]
		s.owner()
		r, err := s.app.Customers.Reverse(s.ctx, e.ID, "دفعة خطأ")
		if err == nil {
			s.move(r.BusinessDate, e.Cash.TenderedCurrency, -e.Cash.TenderedMinor)
			s.move(r.BusinessDate, e.Cash.ChangeCurrency, e.Cash.ChangeMinor)
		} else if errs.CategoryOf(err) != errs.CategoryConflict && errs.CategoryOf(err) != errs.CategoryValidation {
			s.must(err)
		}
	}
}

func (s *shop) stockDay() {
	p := s.products[s.rng.Intn(len(s.products))]
	s.owner()
	switch s.rng.Intn(6) {
	case 0:
		_, err := s.app.Stock.Receive(s.ctx, stock.ReceiveInput{ProductID: p.ID, Quantity: strconv.Itoa(5 + s.rng.Intn(20)),
			Cost: stockdomain.CostInput{Amount: strconv.Itoa(7 + s.rng.Intn(40)), Currency: "USD"}})
		s.must(err)
	case 1:
		_, err := s.app.Stock.Receive(s.ctx, stock.ReceiveInput{ProductID: p.ID, Quantity: "10",
			Cost: stockdomain.CostInput{Amount: strconv.Itoa(150_000 + s.rng.Intn(300_000)), Currency: "SYP", Rate: "14750"}})
		s.must(err)
	case 2:
		if s.onHand(p.ID) >= 1_000_000 {
			_, err := s.app.Stock.Adjust(s.ctx, stock.AdjustInput{ProductID: p.ID, Direction: stock.DirectionOut, Quantity: "1",
				Reason: []stockdomain.Reason{stockdomain.ReasonDamaged, stockdomain.ReasonGift, stockdomain.ReasonOther}[s.rng.Intn(3)], Note: "تالف"})
			s.must(err)
		}
	case 3:
		if h := s.onHand(p.ID); h > 0 {
			counted := strconv.FormatInt(h/1_000_000+int64(s.rng.Intn(3)-1), 10)
			if _, err := s.app.Stock.Count(s.ctx, stock.CountInput{ProductID: p.ID, Counted: counted}); err != nil && errs.CategoryOf(err) != errs.CategoryConflict &&
				errs.CategoryOf(err) != errs.CategoryValidation {
				s.must(err)
			}
		}
	case 4:
		if s.onHand(p.ID) > 0 {
			_, err := s.app.Stock.CorrectCost(s.ctx, stock.CorrectCostInput{ProductID: p.ID, AverageCost: fmt.Sprintf("%d.%02d", 1+s.rng.Intn(4), s.rng.Intn(100)), Note: "تصحيح"})
			if err != nil && errs.CategoryOf(err) != errs.CategoryConflict && errs.CategoryOf(err) != errs.CategoryValidation {
				s.must(err)
			}
		}
	}
}

func (s *shop) cashDay(closing bool) {
	s.owner()
	switch s.rng.Intn(6) {
	case 0:
		e, err := s.app.Cashbook.Record(s.ctx, cashbook.RecordInput{Kind: cashbookdomain.KindExpense, Currency: "SYP", Amount: strconv.Itoa(10_000 + s.rng.Intn(90_000)),
			Category: cashbookdomain.Categories[s.rng.Intn(len(cashbookdomain.Categories))], FromDrawer: s.rng.Intn(2) == 0})
		s.must(err)
		if e.FromDrawer {
			s.move(e.BusinessDate, e.Currency, -e.AmountMinor)
		}
		s.money = append(s.money, e)
	case 1:
		e, err := s.app.Cashbook.Record(s.ctx, cashbook.RecordInput{Kind: cashbookdomain.KindWithdrawal, Currency: "USD", Amount: "5"})
		s.must(err)
		s.move(e.BusinessDate, e.Currency, -e.AmountMinor)
		s.money = append(s.money, e)
	case 2:
		e, err := s.app.Cashbook.Record(s.ctx, cashbook.RecordInput{Kind: cashbookdomain.KindDeposit, Currency: "SYP", Amount: "50000"})
		s.must(err)
		s.move(e.BusinessDate, e.Currency, e.AmountMinor)
		s.money = append(s.money, e)
	case 3:
		if len(s.money) > 0 {
			e := s.money[len(s.money)-1]
			s.money = s.money[:len(s.money)-1]
			r, err := s.app.Cashbook.Reverse(s.ctx, e.ID, "خطأ")
			s.must(err)
			sign := int64(1)
			if e.Kind == cashbookdomain.KindDeposit {
				sign = -1
			}
			if e.Kind != cashbookdomain.KindExpense || e.FromDrawer {
				s.move(r.BusinessDate, e.Currency, sign*e.AmountMinor)
			}
		}
	}
	if closing {
		for _, currency := range []string{"USD", "SYP"} {
			if s.rng.Intn(3) == 0 {
				continue
			}
			want := s.expected(s.today(), currency)
			counted := want + int64(s.rng.Intn(3)-1)*map[string]int64{"USD": 100, "SYP": 1_000}[currency]
			if counted < 0 {
				counted = 0
			}
			decimals := map[string]int{"USD": 2, "SYP": 0}[currency]
			e, err := s.app.Cashbook.Count(s.ctx, cashbook.CountInput{Currency: currency, Counted: format(counted, decimals)})
			s.must(err)
			if e.ExpectedMinor != want || e.AmountMinor != counted {
				s.t.Fatalf("a count on %s recorded %d expected, %d counted; the tally says %d, %d", e.BusinessDate, e.ExpectedMinor, e.AmountMinor, want, counted)
			}
			s.counts = append(s.counts, e)
		}
	}
}

func format(minor int64, decimals int) string {
	if decimals == 0 {
		return strconv.FormatInt(minor, 10)
	}
	return fmt.Sprintf("%d.%02d", minor/100, minor%100)
}

// expected is the tally's drawer: the newest count on a day before the date, plus every movement after that day.
func (s *shop) expected(date, currency string) int64 {
	from, total := "", int64(0)
	for _, c := range s.counts {
		if c.Currency == currency && c.BusinessDate < date {
			from, total = c.BusinessDate, c.AmountMinor
		}
	}
	for d, moves := range s.tally {
		if d > from && d <= date {
			total += moves[currency]
		}
	}
	return total
}

// run trades for n days, the rate drifting unless fixed.
func (s *shop) run(days int) []string {
	var dates []string
	s.owner()
	for _, p := range s.products {
		_, err := s.app.Stock.Opening(s.ctx, stock.ReceiveInput{ProductID: p.ID, Quantity: "40", Cost: stockdomain.CostInput{Amount: strconv.Itoa(30 + s.rng.Intn(60)), Currency: "USD"}})
		s.must(err)
	}
	for day := range days {
		s.clk.Current = time.Date(2026, 8, 1+day, 5, 0, 0, 0, time.UTC)
		dates = append(dates, s.today())
		if !s.rateFixed && day > 0 && s.rng.Intn(3) == 0 {
			s.owner()
			_, err := s.app.FX.SetRate(s.ctx, fx.SetRateInput{Rate: strconv.Itoa(14_500 + s.rng.Intn(1_000))})
			s.must(err)
		}
		for step := range 12 {
			s.clk.Advance(time.Duration(1+s.rng.Intn(40)) * time.Minute)
			switch s.rng.Intn(5) {
			case 0, 1:
				s.sell()
			case 2:
				if s.rng.Intn(3) == 0 {
					s.void()
				}
			case 3:
				if !s.rateFixed {
					s.stockDay()
				}
				s.debts()
			default:
				s.cashDay(step == 11)
			}
		}
		s.clk.Advance(time.Hour)
		s.cashDay(true)
	}
	return dates
}

// TestAMonthIsTheSumOfItsDays: a random month through the real graph — the month's totals are its days, and each of its
// rows is that day's report.
func TestAMonthIsTheSumOfItsDays(t *testing.T) {
	s := openShop(t, 61)
	dates := s.run(31)
	s.owner()
	month, err := s.app.Reports.Month(s.ctx, "2026-08")
	s.must(err)
	var days []reportsdomain.Day
	rows := map[string]reportsdomain.Day{}
	for _, d := range month.Days {
		rows[d.Date] = d
	}
	for _, date := range reportsdomain.Dates("2026-08-01", "2026-08-31") {
		s.owner()
		d, dayErr := s.app.Reports.Day(s.ctx, date)
		s.must(dayErr)
		days = append(days, d)
		if row, ok := rows[date]; ok && fmt.Sprintf("%+v", row) != fmt.Sprintf("%+v", d) {
			t.Fatalf("the month's row for %s is not the day's report:\n%+v\n%+v", date, row, d)
		} else if !ok && d.Active() {
			t.Fatalf("%s had activity and no row", date)
		}
	}
	sum := reportsdomain.Total("2026-08-01", days)
	if fmt.Sprintf("%+v", sum) != fmt.Sprintf("%+v", month.Total) || sum.NetUSD() != month.Total.NetUSD() || sum.NetLocal() != month.Total.NetLocal() {
		t.Fatalf("the month is not the sum of its days:\n%+v\n%+v", month.Total, sum)
	}
	if month.Total.Profit.Sales == 0 || month.Total.Profit.Unknown.Lines == 0 || month.Total.Losses == (reportsdomain.Losses{}) ||
		month.Total.Expenses.Total.Local == 0 || month.Total.BadDebts == (reportsdomain.Converted{}) {
		t.Fatalf("the month exercised too little: %+v", month.Total)
	}
	// And the products over the month add up to the month.
	s.owner()
	products, err := s.app.Reports.Products(s.ctx, "2026-08-01", "2026-08-31")
	s.must(err)
	var usd, local int64
	for _, r := range products.Rows {
		usd += r.RevenueUSD
		local += r.RevenueLocal
	}
	if usd-products.DiscountUSD != month.Total.Profit.RevenueUSD || local-products.DiscountLocal+products.RoundingLocal != month.Total.Profit.RevenueLocal {
		t.Fatalf("products %d/%d with the reconciling row are not the month's %d/%d", usd, local, month.Total.Profit.RevenueUSD, month.Total.Profit.RevenueLocal)
	}
	t.Logf("%d dates, %d sales, month net %d cents / %d pounds", len(dates), month.Total.Profit.Sales, month.Total.NetUSD(), month.Total.NetLocal())
}

// TestStockMovementsReconcileOpeningToClosingValue: through the real stock module, no stock ever below zero and every
// known cost — the classes add up from the opening value to the closing value within the rounding bound, and the closing
// value on today is the stock screen's valuation.
func TestStockMovementsReconcileOpeningToClosingValue(t *testing.T) {
	for seed := int64(1); seed <= 3; seed++ {
		s := openShop(t, seed)
		dates := s.run(12)
		for _, r := range [][2]string{{dates[0], dates[0]}, {dates[1], dates[5]}, {dates[3], dates[11]}, {dates[0], dates[11]}} {
			s.owner()
			report, err := s.app.Reports.Stock(s.ctx, r[0], r[1])
			s.must(err)
			c := report.Reconciliation
			sum := c.Opening + c.Received + c.Sold + c.Losses + c.Gains + c.Revaluation + c.Packages + c.NegativeStock + c.Rounding
			if sum != c.Closing || c.NegativeStock != 0 || 2*absolute(c.Rounding) > int64(c.Rounded) || report.Value.TotalUSD != c.Closing {
				t.Fatalf("seed %d %v: %+v", seed, r, c)
			}
			if c.Sold >= 0 || c.Received <= 0 {
				t.Fatalf("seed %d %v exercised too little: %+v", seed, r, c)
			}
		}
		s.owner()
		valuation, err := s.app.Stock.Valuation(s.ctx)
		s.must(err)
		report, err := s.app.Reports.Stock(s.ctx, dates[0], dates[11])
		s.must(err)
		if valuation.TotalMinor != report.Reconciliation.Closing {
			t.Fatalf("seed %d: the stock screen's value %d, the report's %d", seed, valuation.TotalMinor, report.Reconciliation.Closing)
		}
	}
}

func absolute(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// TestTheDrawerAgreesWithEveryCashMovement: every day's expected cash, per currency, is the independent tally's — every
// sale's tender and change, every void's return, repayment, refund, reversal and cash book entry — and each count
// recorded the figure the tally expected at that moment (checked as the counts were made).
func TestTheDrawerAgreesWithEveryCashMovement(t *testing.T) {
	s := openShop(t, 62)
	dates := s.run(20)
	if len(s.counts) < 10 {
		t.Fatalf("only %d counts", len(s.counts))
	}
	for _, date := range append(dates, "2026-08-25") {
		for _, owner := range []bool{false, true} {
			if owner {
				s.owner()
			} else {
				_, _ = s.app.Owner.EndElevation(s.ctx)
			}
			d, err := s.app.Reports.Drawer(s.ctx, date)
			s.must(err)
			for _, terms := range d.Currencies {
				if want := s.expected(date, terms.Currency); terms.Expected != want {
					t.Fatalf("%s %s (owner %v): the drawer expects %d, the tally %d: %+v", date, terms.Currency, owner, terms.Expected, want, terms)
				}
			}
		}
	}
}

// TestNetProfitDollarsAndPoundsAgreeInSignForASingleRate: at one rate, each day's pounds profit is its dollar profit at that
// rate within the rounding each figure carries — so the two agree in sign whenever the profit is larger than the rounding.
func TestNetProfitDollarsAndPoundsAgreeInSignForASingleRate(t *testing.T) {
	s := openShop(t, 63)
	s.rateFixed = true
	dates := s.run(15)
	for _, date := range dates {
		s.owner()
		d, dayErr := s.app.Reports.Day(s.ctx, date)
		s.must(dayErr)
		sales, factsErr := s.app.Sales.Facts(s.ctx, date, date)
		s.must(factsErr)
		// Per sale: half a 500 note of cash rounding, and 76 pounds for each rounded cent — gross, discount and cost of each
		// line, and the whole-sale discount.
		var bound int64
		for _, sale := range sales {
			bound += 250 + 76*int64(1+3*len(sale.Lines))
		}
		bound += 76 * int64(len(d.Categories)+10)
		gap := d.NetLocal() - 150*d.NetUSD()
		if absolute(gap) > bound {
			t.Fatalf("%s: net %d pounds against %d cents × 150 — a gap of %d beyond the rounding bound %d", date, d.NetLocal(), d.NetUSD(), gap, bound)
		}
		if absolute(150*d.NetUSD()) > bound && (d.NetLocal() > 0) != (d.NetUSD() > 0) {
			t.Fatalf("%s: net %d pounds and %d cents disagree in sign", date, d.NetLocal(), d.NetUSD())
		}
	}
}

// TestTheC6FixtureThroughTheRealModules is DESIGN C6 end to end: oil received at 3.00 USD when the rate was 13,000, sold at
// 42,000 pounds at 15,000. The day shows −0.20 USD and −3,000 SYP, never +3,000.
func TestTheC6FixtureThroughTheRealModules(t *testing.T) {
	s := openShop(t, 64)
	oil := s.products[1]
	s.owner()
	if _, err := s.app.Stock.Receive(s.ctx, stock.ReceiveInput{ProductID: oil.ID, Quantity: "1",
		Cost: stockdomain.CostInput{Amount: "39000", Currency: "SYP", Rate: "13000"}}); err != nil {
		t.Fatal(err)
	}
	in := salesdomain.CartInput{Lines: []salesdomain.LineInput{{ProductID: oil.ID, Quantity: "1"}}, Settlement: "SYP"}
	q, err := s.app.Sales.Quote(s.ctx, in)
	s.must(err)
	if _, err = s.app.Sales.Checkout(s.ctx, sales.CheckoutInput{Cart: in, Token: q.Token}); err != nil {
		t.Fatal(err)
	}
	d, err := s.app.Reports.Day(s.ctx, "")
	s.must(err)
	if d.Profit.ProfitUSD() != -20 || d.Profit.ProfitLocal() != -3_000 || d.NetUSD() != -20 || d.NetLocal() != -3_000 {
		t.Fatalf("C6 through the modules: %d cents, %d pounds", d.Profit.ProfitUSD(), d.Profit.ProfitLocal())
	}
	p, err := s.app.Reports.Products(s.ctx, "", "")
	s.must(err)
	if len(p.Rows) != 1 || p.Rows[0].ProfitLocal() != -3_000 || p.Rows[0].NameAR != "زيت" {
		t.Fatalf("per product %+v", p.Rows)
	}
}

// TestTheReportsAndCashBookReachTheRealOwner proves the new adapters: a money entry in the owner's history, a count against
// the reports' own expected figure, and the module constants the adapters copy.
//
// Since 2026-09-16 neither a report nor an expense asks for the PIN (owner.ReservedActs), so the proof that the adapters
// reach the REAL owner service is the record they leave, not a refusal.
func TestTheReportsAndCashBookReachTheRealOwner(t *testing.T) {
	s := openShop(t, 65)
	if reports.CodeOwnerRequired != ownerdomain.CodeRequired || cashbook.CodeOwnerRequired != ownerdomain.CodeRequired {
		t.Fatal("a refusal code is not the owner's")
	}
	_, _ = s.app.Owner.EndElevation(s.ctx)
	if _, err := s.app.Reports.Day(s.ctx, ""); err != nil {
		t.Fatalf("a report at the counter: %v", err)
	}
	countedBefore := guardedActs(t, s.app)
	if _, err := s.app.Cashbook.Record(s.ctx, cashbook.RecordInput{Kind: cashbookdomain.KindExpense, Currency: "SYP", Amount: "1000", Category: "rent"}); err != nil {
		t.Fatalf("an expense at the counter: %v", err)
	}
	if guardedActs(t, s.app) != countedBefore+1 {
		t.Fatal("the expense is not in the owner's history")
	}
	before := guardedActs(t, s.app)
	s.owner()
	if _, err := s.app.Cashbook.Record(s.ctx, cashbook.RecordInput{Kind: cashbookdomain.KindDeposit, Currency: "SYP", Amount: "70000"}); err != nil {
		t.Fatal(err)
	}
	if guardedActs(t, s.app) != before+1 {
		t.Fatal("the deposit is not in the owner's history")
	}
	_, _ = s.app.Owner.EndElevation(s.ctx)
	c, err := s.app.Cashbook.Count(s.ctx, cashbook.CountInput{Currency: "SYP", Counted: "69000"})
	if err != nil || c.ExpectedMinor != 70_000 || c.Difference() != -1_000 {
		t.Fatalf("a count at the counter: %+v, %v", c, err)
	}
	for _, pair := range [][2]string{
		{reportsdomain.MoveOpening, string(stockdomain.KindOpening)}, {reportsdomain.MoveReceipt, string(stockdomain.KindReceipt)},
		{reportsdomain.MoveReceiptReversal, string(stockdomain.KindReceiptReversal)}, {reportsdomain.MoveCount, string(stockdomain.KindCount)},
		{reportsdomain.MoveAdjustment, string(stockdomain.KindAdjustment)}, {reportsdomain.MovePackageOut, string(stockdomain.KindPackageOut)},
		{reportsdomain.MoveContentIn, string(stockdomain.KindContentIn)}, {reportsdomain.MoveCostCorrection, string(stockdomain.KindCostCorrection)},
		{reportsdomain.MoveSale, string(stockdomain.KindSale)}, {reportsdomain.MoveSaleVoid, string(stockdomain.KindSaleVoid)},
		{reportsdomain.ReasonDamaged, string(stockdomain.ReasonDamaged)}, {reportsdomain.ReasonExpired, string(stockdomain.ReasonExpired)},
		{reportsdomain.ReasonOwnUse, string(stockdomain.ReasonOwnUse)}, {reportsdomain.ReasonGift, string(stockdomain.ReasonGift)},
		{reportsdomain.ReasonOther, string(stockdomain.ReasonOther)},
		{reportsdomain.DebtOpening, string(customersdomain.KindOpening)}, {reportsdomain.DebtCharge, string(customersdomain.KindCharge)},
		{reportsdomain.DebtPayment, string(customersdomain.KindPayment)}, {reportsdomain.DebtWriteOff, string(customersdomain.KindWriteOff)},
		{reportsdomain.DebtRefund, string(customersdomain.KindRefund)}, {reportsdomain.DebtReversal, string(customersdomain.KindReversal)},
		{reportsdomain.CashExpense, string(cashbookdomain.KindExpense)}, {reportsdomain.CashWithdrawal, string(cashbookdomain.KindWithdrawal)},
		{reportsdomain.CashDeposit, string(cashbookdomain.KindDeposit)}, {reportsdomain.CashCount, string(cashbookdomain.KindCount)},
		{reportsdomain.CashReversal, string(cashbookdomain.KindReversal)},
	} {
		if pair[0] != pair[1] {
			t.Errorf("the reports read %q where the module stores %q", pair[0], pair[1])
		}
	}
	if len(stockdomain.Kinds) != 10 {
		t.Errorf("stock has %d kinds; the reports classify 10", len(stockdomain.Kinds))
	}
}
