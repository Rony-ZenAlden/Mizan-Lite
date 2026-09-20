package domain_test

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/reports/domain"
)

var pair = domain.Pair{Local: domain.Currency{Code: "SYP", Decimals: 0}, USD: domain.Currency{Code: "USD", Decimals: 2}}

const rate15k = 15_000_000_000_000

func pid(n int) id.ID { return id.ID(fmt.Sprintf("00000000-0000-7000-8000-%012d", n)) }

// oilSale is DESIGN C6: oil bought at 3.00 USD a litre, sold at 42,000 SYP when the rate is 15,000.
func oilSale(date string) domain.Sale {
	return domain.Sale{ID: pid(900), BusinessDate: date, SettlementCurrency: "SYP", RateNano: rate15k, TotalMinor: 42_000,
		TenderedCurrency: "SYP", TenderedMinor: 50_000, ChangeCurrency: "SYP", ChangeMinor: 8_000, VoidReturnCurrency: "SYP", VoidReturnMinor: 42_000,
		Lines: []domain.Line{{ProductID: pid(1), NameAR: "زيت", QuantityMicro: 1_000_000, NetLocalMinor: 42_000, NetUSDMinor: 280,
			CostKnown: true, CostUSDMinor: 300, CostLocalMinor: 45_000}}}
}

func TestTheC6TableIsTheReportsTable(t *testing.T) {
	d := domain.DayOf(domain.Facts{Pair: pair, Sales: []domain.Sale{oilSale("2026-09-14")}}, "2026-09-14")
	if d.Profit.ProfitUSD() != -20 || d.Profit.ProfitLocal() != -3_000 || d.NetUSD() != -20 || d.NetLocal() != -3_000 {
		t.Fatalf("C6: %d cents, %d pounds; net %d, %d — want −0.20 USD and −3,000 SYP", d.Profit.ProfitUSD(), d.Profit.ProfitLocal(), d.NetUSD(), d.NetLocal())
	}
	if strings.Contains(fmt.Sprintf("%+v", d), "3000 ") || d.Profit.ProfitLocal() == 3_000 {
		t.Fatal("the phantom +3,000 appears")
	}
	products := domain.ProductsOver([]domain.Sale{oilSale("2026-09-14")}, "2026-09-14", "2026-09-14", pair, nil)
	if products.Rows[0].ProfitLocal() != -3_000 || products.Rows[0].ProfitUSD() != -20 {
		t.Fatalf("per product: %+v", products.Rows[0])
	}
}

func TestAVoidTakesItsProfitBackOnItsOwnDay(t *testing.T) {
	s := oilSale("2026-09-14")
	s.Lines[0].NetLocalMinor, s.Lines[0].NetUSDMinor = 60_000, 400
	s.TotalMinor = 60_000
	s.Voided, s.VoidBusinessDate = true, "2026-10-02"
	f := domain.Facts{Pair: pair, Sales: []domain.Sale{s}}
	sold, voided, between := domain.DayOf(f, "2026-09-14"), domain.DayOf(f, "2026-10-02"), domain.DayOf(f, "2026-09-20")
	if sold.Profit.ProfitUSD() != 100 || sold.Profit.Sales != 1 {
		t.Fatalf("the sale's day keeps it: %+v", sold.Profit)
	}
	if voided.Profit.ProfitUSD() != -100 || voided.Profit.ProfitLocal() != -15_000 || voided.Profit.Sales != -1 || voided.Profit.CostUSD != -300 {
		t.Fatalf("the void's day takes back revenue and cost: %+v", voided.Profit)
	}
	if between.Active() {
		t.Fatal("a day between has something")
	}
	if voided.Takings[1].Voids != 1 || voided.Takings[1].VoidedMinor != 60_000 || sold.Takings[1].Voids != 0 {
		t.Fatalf("takings: %+v / %+v", sold.Takings, voided.Takings)
	}
	sept := domain.MonthOf(f, "2026-09", "2026-09-01", "2026-09-30")
	oct := domain.MonthOf(f, "2026-10", "2026-10-01", "2026-10-31")
	if sept.Total.Profit.ProfitUSD() != 100 || oct.Total.Profit.ProfitUSD() != -100 || len(sept.Days) != 1 || len(oct.Days) != 1 {
		t.Fatalf("a void next month belongs to next month: %d, %d", sept.Total.Profit.ProfitUSD(), oct.Total.Profit.ProfitUSD())
	}
}

func TestLinesOfUnknownCostAreShownApart(t *testing.T) {
	s := oilSale("2026-09-14")
	s.Lines = append(s.Lines, domain.Line{ProductID: pid(2), QuantityMicro: 2_000_000, NetLocalMinor: 30_000, NetUSDMinor: 200})
	s.DiscountLocalMinor, s.DiscountUSDMinor, s.RoundingMinor = 1_000, 7, 500
	p := domain.SaleProfit(s, pair)
	if p.RevenueUSD != 280-7 || p.RevenueLocal != 42_000-1_000+500 || p.CostUSD != 300 {
		t.Fatalf("the unknown line reached revenue or cost: %+v", p)
	}
	if p.Unknown != (domain.Unknown{Lines: 1, QuantityMicro: 2_000_000, NetUSDMinor: 200, NetLocalMinor: 30_000}) {
		t.Fatalf("shown apart: %+v", p.Unknown)
	}
	usd := s
	usd.SettlementCurrency, usd.RoundingMinor = "USD", 0
	if domain.SaleProfit(usd, pair).RoundingLocal != 0 {
		t.Fatal("a dollar sale has pounds rounding")
	}
}

func TestProductsAddUpToTheDayExactly(t *testing.T) {
	rng := rand.New(rand.NewSource(6)) //nolint:gosec // a fixed seed for a reproducible property
	for trial := range 200 {
		var sales []domain.Sale
		for n := range 1 + rng.Intn(20) {
			s := domain.Sale{ID: pid(n), BusinessDate: fmt.Sprintf("2026-09-%02d", 1+rng.Intn(3)), SettlementCurrency: []string{"SYP", "USD"}[rng.Intn(2)],
				DiscountLocalMinor: int64(rng.Intn(3) * 500), DiscountUSDMinor: int64(rng.Intn(40))}
			if s.SettlementCurrency == "SYP" {
				s.RoundingMinor = int64(rng.Intn(500) - 250)
			}
			if rng.Intn(4) == 0 {
				s.Voided, s.VoidBusinessDate = true, fmt.Sprintf("2026-09-%02d", 1+rng.Intn(4))
			}
			for range 1 + rng.Intn(5) {
				s.Lines = append(s.Lines, domain.Line{ProductID: pid(rng.Intn(6)), QuantityMicro: int64(rng.Intn(5_000_000)),
					NetLocalMinor: int64(rng.Intn(90_000)), NetUSDMinor: int64(rng.Intn(600)), CostKnown: rng.Intn(5) > 0,
					CostUSDMinor: int64(rng.Intn(500)), CostLocalMinor: int64(rng.Intn(80_000))})
			}
			sales = append(sales, s)
		}
		f := domain.Facts{Pair: pair, Sales: sales}
		for _, r := range [][2]string{{"2026-09-01", "2026-09-01"}, {"2026-09-02", "2026-09-04"}, {"2026-09-01", "2026-09-30"}} {
			var days []domain.Day
			for _, date := range domain.Dates(r[0], r[1]) {
				days = append(days, domain.DayOf(f, date))
			}
			want := domain.Total(r[0], days).Profit
			got := domain.ProductsOver(sales, r[0], r[1], pair, nil)
			var usd, local, costUSD, costLocal int64
			for _, row := range got.Rows {
				usd += row.RevenueUSD
				local += row.RevenueLocal
				costUSD += row.CostUSD
				costLocal += row.CostLocal
			}
			if usd-got.DiscountUSD != want.RevenueUSD || local-got.DiscountLocal+got.RoundingLocal != want.RevenueLocal ||
				costUSD != want.CostUSD || costLocal != want.CostLocal || got.Total != want {
				t.Fatalf("trial %d %v: products %d/%d with the reconciling row do not make the range's %d/%d", trial, r, usd, local, want.RevenueUSD, want.RevenueLocal)
			}
		}
	}
}

func TestMarginIsExactToOneDecimal(t *testing.T) {
	for _, c := range []struct {
		profit, revenue int64
		want            string
		ok              bool
	}{
		{1, 3, "33.3", true}, {2, 3, "66.7", true}, {1, 8, "12.5", true}, {-1, 8, "-12.5", true}, {1, 2000, "0.1", true},
		{1, 2001, "0.0", true}, {-20, 280, "-7.1", true}, {5, 0, "", false}, {0, 100, "0.0", true}, {-1, -8, "12.5", true},
		{1_000_000_000_000, 1_000_000_000_001, "100.0", true},
	} {
		if got, ok := domain.Margin(c.profit, c.revenue); got != c.want || ok != c.ok {
			t.Errorf("Margin(%d, %d) = %q %v, want %q", c.profit, c.revenue, got, ok, c.want)
		}
	}
}

func TestTheRateOfTheBusinessDayIsNewestOnOrBeforeIt(t *testing.T) {
	rates := []domain.Rate{
		{Seq: 1, Nano: 14_000, BusinessDate: "2026-09-01"},
		{Seq: 2, Nano: 14_500, BusinessDate: "2026-09-03"},
		{Seq: 3, Nano: 14_600, BusinessDate: "2026-09-03"}, // a same-day correction
		{Seq: 4, Nano: 15_000, BusinessDate: "2026-09-10"},
	}
	for date, want := range map[string]int64{"2026-09-01": 14_000, "2026-09-02": 14_000, "2026-09-03": 14_600, "2026-09-09": 14_600, "2026-09-30": 15_000} {
		if r, found := domain.RateOfDay(rates, date); !found || r.Nano != want {
			t.Errorf("rate of %s = %d, want %d", date, r.Nano, want)
		}
	}
	if _, found := domain.RateOfDay(rates, "2026-08-31"); found {
		t.Error("a rate before any was recorded")
	}
	// Places decide, not the order they arrive in.
	shuffled := []domain.Rate{rates[3], rates[2], rates[0], rates[1]}
	if r, _ := domain.RateOfDay(shuffled, "2026-09-05"); r.Seq != 3 {
		t.Errorf("shuffled: seq %d", r.Seq)
	}
}

func TestLossesAndGainsAtTheCostTheyMovedAt(t *testing.T) {
	rates := []domain.Rate{{Seq: 1, Nano: 14_000_000_000_000, BusinessDate: "2026-09-01"}, {Seq: 2, Nano: 16_000_000_000_000, BusinessDate: "2026-09-20"}}
	moves := []domain.Movement{
		{BusinessDate: "2026-09-05", Kind: domain.MoveAdjustment, Reason: domain.ReasonExpired, QuantityMicro: -1_500_000, UnitCostMicro: 2_333_333}, // 3.4999995 → 3.50
		{BusinessDate: "2026-09-05", Kind: domain.MoveAdjustment, Reason: domain.ReasonGift, QuantityMicro: -1_000_000, UnitCostMicro: 1_000_000},
		{BusinessDate: "2026-09-05", Kind: domain.MoveAdjustment, Reason: domain.ReasonOther, QuantityMicro: -1_000_000, UnitCostMicro: 10_000},
		{BusinessDate: "2026-09-05", Kind: domain.MoveCount, Reason: "count", QuantityMicro: -250_000, UnitCostMicro: 4_000_000},
		{BusinessDate: "2026-09-05", Kind: domain.MoveCount, Reason: "count", QuantityMicro: 500_000, UnitCostMicro: 4_000_000},
		{BusinessDate: "2026-09-05", Kind: domain.MoveSale, QuantityMicro: -9_000_000, UnitCostMicro: 4_000_000},
		{BusinessDate: "2026-09-06", Kind: domain.MoveAdjustment, Reason: domain.ReasonDamaged, QuantityMicro: -1_000_000, UnitCostMicro: 1_000_000},
	}
	f := domain.Facts{Pair: pair, Rates: rates, Movements: moves}
	d := domain.DayOf(f, "2026-09-05")
	l := d.Losses
	if l.Spoiled != (domain.Converted{USD: 350, Local: 49_000}) || l.OwnUse != (domain.Converted{USD: 100, Local: 14_000}) ||
		l.Other != (domain.Converted{USD: 1, Local: 140}) || l.Shortfall != (domain.Converted{USD: 100, Local: 14_000}) ||
		l.Surplus != (domain.Converted{USD: 200, Local: 28_000}) {
		t.Fatalf("losses %+v", l)
	}
	if d.NetUSD() != -350-100-1-100+200 || d.NetLocal() != -49_000-14_000-140-14_000+28_000 {
		t.Fatalf("net %d %d", d.NetUSD(), d.NetLocal())
	}
	// Read a month later, when the rate is 16,000: the day's losses stay at the rate of their day.
	f.Rates = append(f.Rates, domain.Rate{Seq: 3, Nano: 20_000_000_000_000, BusinessDate: "2026-10-30"})
	if again := domain.DayOf(f, "2026-09-05"); again.Losses != l {
		t.Fatal("a past day's losses moved with a later rate")
	}
	if before := domain.DayOf(domain.Facts{Pair: pair, Movements: moves}, "2026-09-06"); before.Losses.Spoiled != (domain.Converted{USD: 100, Unconverted: 1}) || before.Unconverted() != 1 {
		t.Fatalf("with no rate the pounds reading is not converted: %+v", before.Losses.Spoiled)
	}
}

func TestBadDebtsAndExpensesInBothReadings(t *testing.T) {
	rates := []domain.Rate{{Seq: 1, Nano: 15_000_000_000_000, BusinessDate: "2026-09-01"}}
	debts := []domain.DebtEntry{
		{BusinessDate: "2026-09-05", Kind: domain.DebtWriteOff, Currency: "USD", AmountMinor: -1_000},
		{BusinessDate: "2026-09-05", Kind: domain.DebtWriteOff, Currency: "SYP", AmountMinor: -75_000},
		{BusinessDate: "2026-09-05", Kind: domain.DebtPayment, Currency: "SYP", AmountMinor: -5_000},
		{BusinessDate: "2026-09-07", Kind: domain.DebtReversal, ReversesKind: domain.DebtWriteOff, Currency: "USD", AmountMinor: 1_000},
		{BusinessDate: "2026-09-07", Kind: domain.DebtReversal, ReversesKind: domain.DebtPayment, Currency: "SYP", AmountMinor: 5_000},
	}
	rent := domain.CashEntry{ID: pid(1), BusinessDate: "2026-09-05", Kind: domain.CashExpense, Currency: "USD", AmountMinor: 10_000, Category: "rent", RateNano: 14_000_000_000_000}
	cash := []domain.CashEntry{
		rent,
		{BusinessDate: "2026-09-05", Kind: domain.CashExpense, Currency: "SYP", AmountMinor: 30_000, Category: "electricity", FromDrawer: true, RateNano: 15_000_000_000_000},
		{BusinessDate: "2026-09-05", Kind: domain.CashWithdrawal, Currency: "SYP", AmountMinor: 99_000, RateNano: 15_000_000_000_000},
		{BusinessDate: "2026-09-07", Kind: domain.CashReversal, Currency: "USD", AmountMinor: 10_000, Reverses: &rent},
	}
	f := domain.Facts{Pair: pair, Rates: rates, Debts: debts, Cash: cash}
	d5, d7 := domain.DayOf(f, "2026-09-05"), domain.DayOf(f, "2026-09-07")
	if d5.BadDebts != (domain.Converted{USD: 1_500, Local: 225_000}) || d7.BadDebts != (domain.Converted{USD: -1_000, Local: -150_000}) {
		t.Fatalf("bad debts %+v / %+v", d5.BadDebts, d7.BadDebts)
	}
	if d5.Expenses.Total != (domain.Converted{USD: 10_200, Local: 1_430_000}) || d7.Expenses.Total != (domain.Converted{USD: -10_000, Local: -1_400_000}) {
		t.Fatalf("expenses at their own rates: %+v / %+v", d5.Expenses, d7.Expenses)
	}
	if len(d5.Categories) != 2 || d5.Categories[0].Category != "electricity" || d5.Categories[1].Amount.USD != 10_000 {
		t.Fatalf("categories %+v", d5.Categories)
	}
	if d5.NetUSD() != -1_500-10_200 || d7.NetUSD() != 11_000 {
		t.Fatalf("net %d / %d — a withdrawal is not an expense", d5.NetUSD(), d7.NetUSD())
	}
	syp5, syp7 := d5.Takings[1], d7.Takings[1]
	if syp5.CollectedMinor != 5_000 || syp5.WrittenOffMinor != 75_000 || syp7.CollectedMinor != -5_000 || d7.Takings[0].WrittenOffMinor != -1_000 {
		t.Fatalf("takings %+v / %+v", d5.Takings, d7.Takings)
	}
}

// ledger builds a product's chain of rows from quantities and costs, each row's before the previous row's after.
func ledger(product int, date string, seq *int64, onHand, avg *int64, rows ...[3]int64) []domain.Movement {
	var out []domain.Movement
	for _, r := range rows { // kind index, quantity, new average
		kinds := []string{domain.MoveReceipt, domain.MoveSale, domain.MoveAdjustment, domain.MoveCostCorrection, domain.MovePackageOut}
		*seq++
		m := domain.Movement{ProductID: pid(product), Seq: *seq, BusinessDate: date, Kind: kinds[r[0]], QuantityMicro: r[1], UnitCostMicro: *avg,
			OnHandBeforeMicro: *onHand, AvgCostBeforeMicro: *avg}
		*onHand += r[1]
		if r[2] > 0 {
			*avg = r[2]
		}
		m.OnHandAfterMicro, m.AvgCostAfterMicro = *onHand, *avg
		out = append(out, m)
	}
	return out
}

func TestStockValueOnAPastDateIsTheLastRowOnOrBeforeIt(t *testing.T) {
	rates := []domain.Rate{{Seq: 1, Nano: 14_000_000_000_000, BusinessDate: "2026-09-01"}, {Seq: 2, Nano: 16_000_000_000_000, BusinessDate: "2026-09-20"}}
	last := []domain.Movement{
		{ProductID: pid(1), BusinessDate: "2026-09-10", OnHandAfterMicro: 2_500_000, AvgCostAfterMicro: 1_333_333}, // 3.3333325 → 3.33
		{ProductID: pid(2), BusinessDate: "2026-09-02", OnHandAfterMicro: 0, AvgCostAfterMicro: 5_000_000},
		{ProductID: pid(3), BusinessDate: "2026-09-02", OnHandAfterMicro: 1_000_000, AvgCostAfterMicro: 0},
	}
	v := domain.ValueOn(last, "2026-09-15", rates, pair, map[id.ID]domain.Product{pid(1): {ID: pid(1), NameAR: "رز"}})
	if v.TotalUSD != 333 || v.TotalLocal != 46_667 || len(v.Lines) != 1 || v.Lines[0].Product.NameAR != "رز" || v.Rate.Seq != 1 {
		t.Fatalf("value %+v", v)
	}
	if len(v.UnknownCost) != 1 || v.UnknownCost[0].Product.ID != pid(3) {
		t.Fatalf("unknown cost %+v", v.UnknownCost)
	}
}

func TestNegativeStockHasNoValue(t *testing.T) {
	last := []domain.Movement{
		{ProductID: pid(1), OnHandAfterMicro: -3_000_000, AvgCostAfterMicro: 2_000_000},
		{ProductID: pid(2), OnHandAfterMicro: 1_000_000, AvgCostAfterMicro: 2_000_000},
	}
	v := domain.ValueOn(last, "2026-09-15", nil, pair, nil)
	if v.TotalUSD != 200 || len(v.BelowZero) != 1 || v.Lines[0].LocalUnrated != true {
		t.Fatalf("value %+v", v)
	}
	// A reconciliation over the sale beyond stock names the value below zero, and rounding stays zero.
	var seq, onHand, avg int64
	opening := ledger(1, "2026-09-01", &seq, &onHand, &avg, [3]int64{0, 2_000_000, 2_000_000})
	period := ledger(1, "2026-09-02", &seq, &onHand, &avg, [3]int64{1, -5_000_000, 0})
	r := domain.Reconcile("2026-09-02", "2026-09-02", opening[len(opening)-1:], period, period[len(period)-1:], pair)
	if r.Opening != 400 || r.Sold != -1_000 || r.NegativeStock != 600 || r.Closing != 0 || r.Rounding != 0 {
		t.Fatalf("reconciliation %+v", r)
	}
}

func TestStockMovementsTelescopeExactly(t *testing.T) {
	rng := rand.New(rand.NewSource(7)) //nolint:gosec // a fixed seed
	for trial := range 300 {
		var before, rows, end []domain.Movement
		var seq int64
		for p := range 1 + rng.Intn(6) {
			onHand, avg := int64(0), int64(0)
			var all []domain.Movement
			for d := 1; d <= 3; d++ {
				var spec [][3]int64
				running := onHand
				for range rng.Intn(4) {
					kind := int64(rng.Intn(5))
					q := int64(rng.Intn(3_000_000) + 1)
					switch kind {
					case 0:
						running += q
						spec = append(spec, [3]int64{0, q, int64(rng.Intn(9_000_000) + 1)})
					case 3:
						spec = append(spec, [3]int64{3, 0, int64(rng.Intn(9_000_000) + 1)})
					default:
						if running-q < 0 {
							q = running
						}
						running -= q
						if q > 0 {
							spec = append(spec, [3]int64{kind, -q, 0})
						}
					}
				}
				day := ledger(p, fmt.Sprintf("2026-09-%02d", d), &seq, &onHand, &avg, spec...)
				all = append(all, day...)
			}
			var lastBefore, lastEnd *domain.Movement
			for i := range all {
				m := all[i]
				if m.BusinessDate < "2026-09-02" {
					lastBefore = &all[i]
				} else {
					rows = append(rows, m)
				}
				lastEnd = &all[i]
			}
			if lastBefore != nil {
				before = append(before, *lastBefore)
			}
			if lastEnd != nil {
				end = append(end, *lastEnd)
			}
		}
		r := domain.Reconcile("2026-09-02", "2026-09-03", before, rows, end, pair)
		sum := r.Opening + r.Received + r.Sold + r.Losses + r.Gains + r.Revaluation + r.Packages + r.NegativeStock + r.Rounding
		if sum != r.Closing || r.NegativeStock != 0 || 2*abs(r.Rounding) > int64(r.Rounded) {
			t.Fatalf("trial %d: %+v (sum %d)", trial, r, sum)
		}
	}
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func TestShelfProfitNamesWhatItLeavesOut(t *testing.T) {
	rates := []domain.Rate{{Seq: 1, Nano: 15_000_000_000_000, BusinessDate: "2026-09-01"}}
	products := []domain.Product{
		{ID: pid(1), NameAR: "رز", PriceCurrency: "USD", PriceMicro: 1_500_000, Active: true},
		{ID: pid(2), NameAR: "زيت", PriceCurrency: "SYP", PriceMicro: 42_000_000_000, Active: true}, // 2.80 USD
		{ID: pid(3), NameAR: "قديم", PriceCurrency: "USD", PriceMicro: 1_000_000, Active: false},
		{ID: pid(4), NameAR: "بلا كلفة", PriceCurrency: "USD", PriceMicro: 1_000_000, Active: true},
		{ID: pid(5), NameAR: "نفد", PriceCurrency: "USD", PriceMicro: 1_000_000, Active: true},
		{ID: pid(6), NameAR: "لم يدخل", PriceCurrency: "USD", PriceMicro: 1_000_000, Active: true},
	}
	last := []domain.Movement{
		{ProductID: pid(1), OnHandAfterMicro: 10_000_000, AvgCostAfterMicro: 1_000_000},
		{ProductID: pid(2), OnHandAfterMicro: 2_500_000, AvgCostAfterMicro: 3_000_000},
		{ProductID: pid(3), OnHandAfterMicro: 1_000_000, AvgCostAfterMicro: 500_000},
		{ProductID: pid(4), OnHandAfterMicro: 1_000_000, AvgCostAfterMicro: 0},
		{ProductID: pid(5), OnHandAfterMicro: -1_000_000, AvgCostAfterMicro: 500_000},
	}
	s := domain.ShelfProfit(last, products, rates, pair)
	if s.TotalUSD != 500-50 || s.TotalLocal != 75_000-7_500 || len(s.Lines) != 2 || s.BelowCost != 1 || !s.Lines[1].BelowCost {
		t.Fatalf("shelf %+v", s)
	}
	reasons := map[id.ID]string{}
	for _, l := range s.LeftOut {
		reasons[l.Product.ID] = l.Reason
	}
	if reasons[pid(3)] != domain.LeftInactive || reasons[pid(4)] != domain.LeftNoCost || reasons[pid(5)] != domain.LeftNoStock || reasons[pid(6)] != domain.LeftNoStock || len(reasons) != 4 {
		t.Fatalf("left out %v", reasons)
	}
	if none := domain.ShelfProfit(last, products[:2], nil, pair); len(none.Lines) != 1 || none.LeftOut[0].Reason != domain.LeftNoRate {
		t.Fatalf("a pounds price with no rate: %+v", none)
	}
}

func TestTheDrawerSumsMoneyInTheCurrencyItMovedIn(t *testing.T) {
	date := "2026-09-14"
	sales := []domain.Sale{
		// A pounds sale paid with a 5-dollar note: dollars in, pounds out as change.
		{BusinessDate: date, SettlementCurrency: "SYP", TotalMinor: 60_000, TenderedCurrency: "USD", TenderedMinor: 500, ChangeCurrency: "SYP", ChangeMinor: 15_000},
		{BusinessDate: date, SettlementCurrency: "USD", TotalMinor: 250, TenderedCurrency: "USD", TenderedMinor: 300, ChangeCurrency: "USD", ChangeMinor: 50},
		{BusinessDate: date, Credit: true, SettlementCurrency: "SYP", TotalMinor: 76_000, CreditMinor: 56_000, TenderedCurrency: "SYP", TenderedMinor: 20_000, ChangeCurrency: "SYP"},
	}
	debts := []domain.DebtEntry{
		{BusinessDate: date, Kind: domain.DebtPayment, Currency: "USD", AmountMinor: -1_000, Cash: domain.DebtCash{TenderedCurrency: "SYP", TenderedMinor: 150_000, ChangeCurrency: "SYP"}},
		{BusinessDate: date, Kind: domain.DebtRefund, Currency: "SYP", AmountMinor: 5_000, Cash: domain.DebtCash{TenderedCurrency: "SYP", TenderedMinor: 5_000, ChangeCurrency: "SYP"}},
	}
	cash := []domain.CashEntry{
		{BusinessDate: date, Kind: domain.CashExpense, Currency: "SYP", AmountMinor: 30_000, FromDrawer: true},
		{BusinessDate: date, Kind: domain.CashExpense, Currency: "USD", AmountMinor: 10_000, FromDrawer: false},
		{BusinessDate: date, Kind: domain.CashWithdrawal, Currency: "USD", AmountMinor: 100},
		{BusinessDate: date, Kind: domain.CashDeposit, Currency: "SYP", AmountMinor: 100_000},
	}
	d := domain.DrawerOf(domain.DrawerFacts{Pair: pair, Sales: sales, Debts: debts, Cash: cash}, date)
	usd, syp := d.Currencies[0], d.Currencies[1]
	if usd.Currency != "USD" || usd.CashSalesIn != 800 || usd.ChangeOut != 50 || usd.WithdrawalOut != 100 || usd.ExpensesOut != 0 || usd.Expected != 650 {
		t.Fatalf("dollars %+v", usd)
	}
	if syp.CashSalesIn != 0 || syp.ChangeOut != 15_000 || syp.CreditPaidIn != 20_000 || syp.RepaymentsIn != 150_000 || syp.RefundsOut != 5_000 ||
		syp.ExpensesOut != 30_000 || syp.DepositsIn != 100_000 || syp.Expected != -15_000+20_000+150_000-5_000-30_000+100_000 {
		t.Fatalf("pounds %+v", syp)
	}
	if len(d.Entries) != 4 {
		t.Fatalf("entries %d", len(d.Entries))
	}
}

func TestAVoidReturnsWhatTheReceiptSaysWasPaid(t *testing.T) {
	credit := domain.Sale{BusinessDate: "2026-09-13", Credit: true, SettlementCurrency: "SYP", TotalMinor: 76_000, CreditMinor: 56_000,
		TenderedCurrency: "USD", TenderedMinor: 200, ChangeCurrency: "SYP", Voided: true, VoidBusinessDate: "2026-09-14",
		VoidReturnCurrency: "SYP", VoidReturnMinor: 20_000}
	d := domain.DrawerOf(domain.DrawerFacts{Pair: pair, Sales: []domain.Sale{credit}}, "2026-09-14")
	if d.Currencies[1].VoidReturns != 20_000 || d.Currencies[0].VoidReturns != 0 || d.Currencies[0].Opening != 200 {
		t.Fatalf("void return in the charged currency, the tender stays in dollars: %+v", d.Currencies)
	}
}

func TestACountClosesItsDay(t *testing.T) {
	sale := func(date string, minor int64) domain.Sale {
		return domain.Sale{BusinessDate: date, SettlementCurrency: "SYP", TotalMinor: minor, TenderedCurrency: "SYP", TenderedMinor: minor, ChangeCurrency: "SYP"}
	}
	sales := []domain.Sale{sale("2026-09-12", 100_000), sale("2026-09-13", 50_000), sale("2026-09-14", 30_000)}
	count13 := domain.CashEntry{Seq: 2, BusinessDate: "2026-09-13", Kind: domain.CashCount, Currency: "SYP", AmountMinor: 140_000, ExpectedMinor: 150_000}
	cash := []domain.CashEntry{
		{Seq: 1, BusinessDate: "2026-09-13", Kind: domain.CashCount, Currency: "SYP", AmountMinor: 1, ExpectedMinor: 150_000},
		count13,
	}
	f := domain.DrawerFacts{Pair: pair, Sales: sales, Cash: cash, LastCounts: map[string]domain.CashEntry{}}
	d13 := domain.DrawerOn(f, "2026-09-13", "SYP")
	if d13.Opening != 100_000 || d13.Expected != 150_000 || d13.Count == nil || d13.Count.Seq != 2 {
		t.Fatalf("the 13th: %+v", d13)
	}
	if diff, ok := d13.Difference(); !ok || diff != -10_000 {
		t.Fatalf("difference %d", diff)
	}
	f.LastCounts["SYP"] = count13
	d14 := domain.DrawerOn(f, "2026-09-14", "SYP")
	if d14.Opening != 140_000 || d14.Expected != 170_000 || d14.Count != nil || d14.OpeningCount.Seq != 2 {
		t.Fatalf("the 14th starts from the count, the shortage is not carried: %+v", d14)
	}
	f.LastCounts = nil
	if never := domain.DrawerOn(f, "2026-09-15", "SYP"); never.Opening != 180_000 || never.OpeningCount != nil {
		t.Fatalf("never counted: %+v", never)
	}
}

func TestDatesAndRanges(t *testing.T) {
	if from, to, err := domain.MonthRange("2028-02"); err != nil || from != "2028-02-01" || to != "2028-02-29" {
		t.Fatalf("%s %s %v", from, to, err)
	}
	for _, bad := range []string{"2026-13", "2026-9", "", "2026-09-01"} {
		if _, _, err := domain.MonthRange(bad); err == nil {
			t.Errorf("month %q accepted", bad)
		}
	}
	for _, bad := range []string{"2026-02-30", "2026-9-01", "", "x"} {
		if _, err := domain.ParseDate(bad); err == nil {
			t.Errorf("date %q accepted", bad)
		}
	}
	if err := domain.ParseRange("2026-09-02", "2026-09-01"); err == nil {
		t.Error("a backwards range accepted")
	}
	if got := domain.Dates("2026-12-30", "2027-01-02"); len(got) != 4 || got[3] != "2027-01-02" {
		t.Fatalf("dates %v", got)
	}
	if got := pair.Convert(1_000, "USD", 14_750_500_000_000); got != 147_505 {
		t.Fatalf("convert %d", got)
	}
	if got := pair.Convert(100_000, "SYP", 15_000_000_000_000); got != 667 {
		t.Fatalf("convert %d", got)
	}
}

// TestACostCorrectionIsARevaluationNotProfit: correcting an average moves the stock's value, not the day's profit, and
// each class of movement lands in its own line (D-L6.5).
func TestACostCorrectionIsARevaluationNotProfit(t *testing.T) {
	var seq, onHand, avg int64
	opening := ledger(1, "2026-09-01", &seq, &onHand, &avg, [3]int64{0, 10_000_000, 2_000_000})
	period := ledger(1, "2026-09-02", &seq, &onHand, &avg,
		[3]int64{3, 0, 2_500_000},  // cost correction: 10 × +0.50
		[3]int64{1, -2_000_000, 0}, // sold 2 at 2.50
		[3]int64{2, -1_000_000, 0}, // written off 1 at 2.50
		[3]int64{0, 4_000_000, 0},  // received 4 at the same average
		[3]int64{4, -1_000_000, 0}, // a package opened out
	)
	r := domain.Reconcile("2026-09-02", "2026-09-02", opening, period, period[len(period)-1:], pair)
	if r.Opening != 2_000 || r.Revaluation != 500 || r.Sold != -500 || r.Losses != -250 || r.Received != 1_000 || r.Packages != -250 ||
		r.Gains != 0 || r.Closing != 2_500 || r.Rounding != 0 {
		t.Fatalf("reconciliation %+v", r)
	}
	d := domain.DayOf(domain.Facts{Pair: pair, Movements: period, Rates: []domain.Rate{{Seq: 1, Nano: rate15k, BusinessDate: "2026-09-01"}}}, "2026-09-02")
	if d.Losses.Out().USD != 250 || d.NetUSD() != -250 {
		t.Fatalf("the day's net %d: only the write-off is a loss, the correction is not profit", d.NetUSD())
	}
}

// TestAReturnReducesTheDayItCameBackOnNotTheDayOfTheSale is the whole reason a return is its own fact: Monday's report
// has been read, counted and possibly printed. Thursday's tin coming back is Thursday's business.
func TestAReturnReducesTheDayItCameBackOnNotTheDayOfTheSale(t *testing.T) {
	// Sold Monday for 900 pounds of stock that cost 750: a margin of 150. Returned Thursday.
	returns := []domain.Return{{
		ReturnNo: 1, BusinessDate: "2026-09-17", Settlement: "cash", SettlementCurrency: "SYP",
		RefundMinor: 900, RefundLocalMinor: 900, RefundUSDMinor: 6,
		CostLocalMinor: 750, CostUSDMinor: 5, CostKnown: true,
	}}
	monday := domain.ReturnsOn(returns, "2026-09-14")
	if monday != (domain.Returns{}) {
		t.Fatalf("the day of the sale was changed by a later return: %+v", monday)
	}
	thursday := domain.ReturnsOn(returns, "2026-09-17")
	if thursday.Count != 1 || thursday.Refund.Local != 900 || thursday.Cost.Local != 750 {
		t.Fatalf("thursday = %+v", thursday)
	}
	// The shop is 150 pounds worse off, not 900: the goods went back on the shelf.
	if p := thursday.Profit(); p.Local != 150 || p.USD != 1 {
		t.Fatalf("profit lost = %+v, want the margin and not the refund", p)
	}
	// A returned line whose cost was never known is counted and reported, not guessed at zero.
	unknown := domain.ReturnsOn([]domain.Return{{
		BusinessDate: "2026-09-17", Settlement: "cash", RefundLocalMinor: 900, CostKnown: false,
	}}, "2026-09-17")
	if unknown.Unknown != 1 {
		t.Fatalf("an unknown cost was not reported: %+v", unknown)
	}
}

// TestAMonthlyExpenseIsToldApartFromTheDaysSmallChange: the day the rent is paid is not a bad day for the shop, and a
// report that adds the two makes the first of the month look like a disaster.
func TestAMonthlyExpenseIsToldApartFromTheDaysSmallChange(t *testing.T) {
	const date = "2026-09-01"
	cash := []domain.CashEntry{
		{BusinessDate: date, Kind: domain.CashExpense, Currency: "SYP", AmountMinor: 12_000, Category: "supplies",
			FromDrawer: true, Recurrence: "once", RateNano: 15_000_000_000_000},
		{BusinessDate: date, Kind: domain.CashExpense, Currency: "SYP", AmountMinor: 250_000, Category: "rent",
			FromDrawer: true, Recurrence: "monthly", RateNano: 15_000_000_000_000},
	}
	got, cats := domain.ExpensesOn(cash, date, pair)
	if got.Daily.Local != 12_000 {
		t.Fatalf("the day's small change = %d, want 12,000", got.Daily.Local)
	}
	if got.Periodic.Local != 250_000 {
		t.Fatalf("the periodic expenses = %d, want 250,000", got.Periodic.Local)
	}
	// Total is unchanged by the split: what left the drawer left the drawer.
	if got.Total.Local != 262_000 {
		t.Fatalf("total = %d, want 262,000", got.Total.Local)
	}
	// The categories are untouched by recurrence — they answer a different question.
	if len(cats) != 2 {
		t.Fatalf("categories = %+v", cats)
	}
	// An expense recorded before recurrence existed reads as the one-off it was.
	old := []domain.CashEntry{{BusinessDate: date, Kind: domain.CashExpense, Currency: "SYP", AmountMinor: 5_000,
		Category: "other", FromDrawer: true, RateNano: 15_000_000_000_000}}
	if was, _ := domain.ExpensesOn(old, date, pair); was.Daily.Local != 5_000 || was.Periodic.Local != 0 {
		t.Fatalf("an expense from before the split = %+v", was)
	}
}
