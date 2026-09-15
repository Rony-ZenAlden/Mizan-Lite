package demoseed

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/cashbook"
	cashbookdomain "github.com/mizan-erp/mizan/internal/lite/cashbook/domain"
	"github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	"github.com/mizan-erp/mizan/internal/lite/fx"
	"github.com/mizan-erp/mizan/internal/lite/sales"
	salesdomain "github.com/mizan-erp/mizan/internal/lite/sales/domain"
	"github.com/mizan-erp/mizan/internal/lite/stock"
	stockdomain "github.com/mizan-erp/mizan/internal/lite/stock/domain"
)

// history runs the days before today (L6 §11) under the clock the seeder steps: the owner's rate each morning drifting from
// 14,200 to 15,600 with one same-day correction, a delivery a week, ten to twenty sales a day, three voids, a spoiled
// labneh write-off and a count shortfall, rent and electricity once and transport weekly, and a closing count most days —
// one with a difference. Every act goes through the services, with the PIN where the owner's is needed.
type history struct {
	ctx      context.Context
	app      *bootstrap.App
	pin      string
	clk      *clock.Fixed
	rng      *rand.Rand
	names    []string
	byName   map[string]domain.Product
	units    map[string]int
	res      *Result
	counts   []cashbookdomain.Entry
	rateNano int64
}

func (h *history) owner() error {
	_, err := h.app.Owner.Elevate(h.ctx, h.pin)
	return err
}

func (h *history) wrap(err error, what string) error {
	if err == nil {
		return nil
	}
	return errs.Wrap(err, errs.CategoryInternal, errs.CodeOf(err), "seeding history: "+what)
}

// days runs day 0 (which the openings were entered on) to days−1; today follows in Run.
func (h *history) days(start time.Time, days int) error {
	for d := range days {
		morning := start.AddDate(0, 0, d).Add(8 * time.Hour)
		h.clk.Current = morning
		if err := h.day(d, days, morning); err != nil {
			return err
		}
		h.res.HistoryDays++
	}
	return nil
}

func (h *history) day(d, days int, morning time.Time) error {
	at := func(hours, minutes int) {
		h.clk.Current = morning.Add(time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute)
	}
	// The morning's rate, drifting; on day 12 a same-day correction of a mistyped rate.
	rate := 14_200 + 1_400*d/max(days-1, 1)
	rate -= rate % 50
	if err := h.owner(); err != nil {
		return err
	}
	if _, err := h.app.FX.SetRate(h.ctx, fx.SetRateInput{Rate: strconv.Itoa(rate), Note: "سعر الصباح"}); err != nil {
		return h.wrap(err, "the morning rate")
	}
	if d == 12 {
		at(0, 5)
		if err := h.owner(); err != nil {
			return err
		}
		if _, err := h.app.FX.SetRate(h.ctx, fx.SetRateInput{Rate: strconv.Itoa(rate + 500), Note: "خطأ"}); err != nil {
			return h.wrap(err, "a mistyped rate")
		}
		if _, err := h.app.FX.SetRate(h.ctx, fx.SetRateInput{Rate: strconv.Itoa(rate), Note: "تصحيح"}); err != nil {
			return h.wrap(err, "the correction")
		}
	}
	h.rateNano = int64(rate) * 1_000_000_000

	if d%7 == 3 {
		at(0, 30)
		if err := h.delivery(rate); err != nil {
			return err
		}
	}
	switch d {
	case 1:
		if err := h.expense("USD", "150", "rent", false, "إيجار الشهر"); err != nil {
			return err
		}
	case 2:
		if err := h.expense("SYP", "350000", "electricity", true, "فاتورة الكهرباء"); err != nil {
			return err
		}
	case 4:
		if err := h.writeOff("لبنة بلدية", "1", stockdomain.ReasonExpired, "لبنة منتهية"); err != nil {
			return err
		}
	case 20:
		if err := h.shortfall(); err != nil {
			return err
		}
	}
	if d%7 == 5 {
		if err := h.expense("SYP", "75000", "transport", true, "نقل البضاعة"); err != nil {
			return err
		}
	}

	var today []salesdomain.Sale
	for i := range 10 + h.rng.Intn(11) {
		at(1+i/2, h.rng.Intn(60))
		sale, ok, err := h.sale()
		if err != nil {
			return err
		}
		if ok {
			today = append(today, sale)
		}
	}
	if (d == 5 || d == 16 || d == 24) && len(today) > 0 {
		at(11, 0)
		if err := h.owner(); err != nil {
			return err
		}
		if _, err := h.app.Sales.Void(h.ctx, sales.VoidInput{SaleID: today[h.rng.Intn(len(today))].ID, Reason: "أعاد الزبون البضاعة"}); err != nil {
			return h.wrap(err, "a void")
		}
		h.res.HistoryVoids++
	}

	if d%6 != 4 {
		at(12, 0)
		for _, currency := range []string{"USD", "SYP"} {
			if err := h.count(currency, d == 18 && currency == "SYP"); err != nil {
				return err
			}
		}
	}
	_, err := h.app.Owner.EndElevation(h.ctx)
	return err
}

// quantity is a believable quantity in a unit: weighed goods by the half kilo or litre, the rest whole.
func (h *history) quantity(unit string, whole int) string {
	if h.units[unit] > 0 {
		return fmt.Sprintf("%d.%d", h.rng.Intn(whole), []int{25, 5, 75}[h.rng.Intn(3)])
	}
	return strconv.Itoa(1 + h.rng.Intn(whole))
}

// delivery receives six products at about 72% of their price, entered as invoice totals — pounds with the day's rate.
func (h *history) delivery(rate int) error {
	if err := h.owner(); err != nil {
		return err
	}
	for range 6 {
		p := h.byName[h.names[h.rng.Intn(len(h.names))]]
		qty := 6 + h.rng.Intn(15)
		total := p.PriceMicro * int64(qty) * 72 / 100 // micro of the price currency
		cost := stockdomain.CostInput{Mode: stockdomain.CostTotal, Currency: p.PriceCurrency}
		if p.PriceCurrency == "USD" {
			cost.Amount = fmt.Sprintf("%d.%02d", total/1_000_000, total/10_000%100)
		} else {
			cost.Amount, cost.Rate = strconv.FormatInt(total/1_000_000, 10), strconv.Itoa(rate)
		}
		if _, err := h.app.Stock.Receive(h.ctx, stock.ReceiveInput{ProductID: p.ID, Quantity: strconv.Itoa(qty), Cost: cost, Note: "توريد الأسبوع"}); err != nil {
			return h.wrap(err, "a delivery of "+p.NameEN)
		}
		h.res.HistoryReceipts++
	}
	return nil
}

// sale rings up one cart: mostly pounds, some dollars, some paid with more than the total. A product may be sold beyond its
// shelf, as happens.
func (h *history) sale() (salesdomain.Sale, bool, error) {
	cart := salesdomain.CartInput{Settlement: "SYP"}
	if h.rng.Intn(5) == 0 {
		cart.Settlement = "USD"
	}
	seen := map[string]bool{}
	for range 1 + h.rng.Intn(3) {
		name := h.names[h.rng.Intn(len(h.names))]
		p := h.byName[name]
		if seen[name] || p.UnitCode == "tin" {
			continue
		}
		seen[name] = true
		cart.Lines = append(cart.Lines, salesdomain.LineInput{ProductID: p.ID, Quantity: h.quantity(p.UnitCode, 3)})
	}
	if len(cart.Lines) == 0 {
		return salesdomain.Sale{}, false, nil
	}
	q, err := h.app.Sales.Quote(h.ctx, cart)
	if err != nil {
		return salesdomain.Sale{}, false, h.wrap(err, "a quote")
	}
	if h.rng.Intn(4) == 0 && cart.Settlement == "SYP" {
		// Paid with the next 10,000 above the total.
		cart.Tendered = strconv.FormatInt((q.TotalMinor/10_000+1)*10_000, 10)
		if q, err = h.app.Sales.Quote(h.ctx, cart); err != nil {
			return salesdomain.Sale{}, false, h.wrap(err, "a quote with a tender")
		}
	}
	sale, err := h.app.Sales.Checkout(h.ctx, sales.CheckoutInput{Cart: cart, Token: q.Token})
	if err != nil {
		return salesdomain.Sale{}, false, h.wrap(err, "a sale")
	}
	h.res.HistorySales++
	return sale, true, nil
}

func (h *history) expense(currency, amount, category string, fromDrawer bool, note string) error {
	if err := h.owner(); err != nil {
		return err
	}
	if _, err := h.app.Cashbook.Record(h.ctx, cashbook.RecordInput{Kind: cashbookdomain.KindExpense, Currency: currency, Amount: amount,
		Category: category, FromDrawer: fromDrawer, Note: note}); err != nil {
		return h.wrap(err, "an expense")
	}
	h.res.Expenses++
	return nil
}

func (h *history) writeOff(name, quantity string, reason stockdomain.Reason, note string) error {
	if err := h.owner(); err != nil {
		return err
	}
	p := h.byName[name]
	st, err := h.app.Stock.Stocked(h.ctx, p.ID)
	if err != nil {
		return err
	}
	if st.OnHandMicro < 1_000_000 {
		return errs.Internal(CodeStockInconsistent, "the history has no stock left to write off").WithParam("product", p.NameEN)
	}
	_, err = h.app.Stock.Adjust(h.ctx, stock.AdjustInput{ProductID: p.ID, Direction: stock.DirectionOut, Quantity: quantity, Reason: reason, Note: note})
	return h.wrap(err, "a write-off")
}

// shortfall counts the first product with at least two on the shelf one unit short.
func (h *history) shortfall() error {
	if err := h.owner(); err != nil {
		return err
	}
	for _, name := range h.names {
		p := h.byName[name]
		st, err := h.app.Stock.Stocked(h.ctx, p.ID)
		if err != nil {
			return err
		}
		if whole := st.OnHandMicro / 1_000_000; whole >= 2 && st.OnHandMicro%1_000_000 == 0 {
			_, err = h.app.Stock.Count(h.ctx, stock.CountInput{ProductID: p.ID, Counted: strconv.FormatInt(whole-1, 10), Note: "جرد آخر الشهر"})
			return h.wrap(err, "a count")
		}
	}
	return errs.Internal(CodeStockInconsistent, "the history has no product to count short")
}

// count counts the drawer at closing: exactly what the report expects, or 5,000 pounds short on the day with a difference.
func (h *history) count(currency string, short bool) error {
	date := h.app.Reports.Today()
	expected, err := h.app.Reports.ExpectedCash(h.ctx, date, currency)
	if err != nil {
		return err
	}
	counted := expected
	if short {
		counted -= 5_000
	}
	if counted < 0 {
		counted = 0
	}
	amount := strconv.FormatInt(counted, 10)
	if currency == "USD" {
		amount = fmt.Sprintf("%d.%02d", counted/100, counted%100)
	}
	e, err := h.app.Cashbook.Count(h.ctx, cashbook.CountInput{Currency: currency, Counted: amount, Note: "عدّ آخر النهار"})
	if err != nil {
		return h.wrap(err, "a count of the drawer")
	}
	h.counts = append(h.counts, e)
	h.res.Counts++
	return nil
}
