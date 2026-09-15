package domain

import (
	"math/big"
	"sort"
)

// Converted is a figure in both readings. Unconverted counts the figures left out of the other reading because no
// rate was recorded on or before their day — shown as not converted, never guessed (L6 §4.3).
type Converted struct {
	USD         int64
	Local       int64
	Unconverted int
}

// Add adds o.
func (c *Converted) Add(o Converted) {
	c.USD += o.USD
	c.Local += o.Local
	c.Unconverted += o.Unconverted
}

func (c Converted) neg() Converted {
	return Converted{USD: -c.USD, Local: -c.Local, Unconverted: c.Unconverted}
}

// inBoth is an amount in one currency with the other reading at a rate, or unconverted when there is none.
func inBoth(minor int64, currency string, nano int64, found bool, pair Pair) Converted {
	other := int64(0)
	missing := 0
	if found && nano > 0 {
		other = pair.Convert(minor, currency, nano)
	} else {
		missing = 1
	}
	if currency == pair.USD.Code {
		return Converted{USD: minor, Local: other, Unconverted: missing}
	}
	return Converted{USD: other, Local: minor, Unconverted: missing}
}

// Losses are the stock that left without a sale, and what counts found, at the cost each row moved at (D-L6.6).
type Losses struct {
	Spoiled   Converted // damaged, expired
	OwnUse    Converted // own use, gifts
	Other     Converted // other write-offs
	Shortfall Converted // a count found less
	Surplus   Converted // a count found more, an adjustment in
}

// Out is every loss, without the surplus.
func (l Losses) Out() Converted {
	var out Converted
	for _, c := range []Converted{l.Spoiled, l.OwnUse, l.Other, l.Shortfall} {
		out.Add(c)
	}
	return out
}

// Add adds o.
func (l *Losses) Add(o Losses) {
	l.Spoiled.Add(o.Spoiled)
	l.OwnUse.Add(o.OwnUse)
	l.Other.Add(o.Other)
	l.Shortfall.Add(o.Shortfall)
	l.Surplus.Add(o.Surplus)
}

// rowValue is a quantity at a USD unit cost, in dollars rounded once to the cent, and in pounds at a rate rounded once.
func rowValue(quantityMicro, unitCostMicro, nano int64, found bool, pair Pair) Converted {
	exact := mul(quantityMicro, unitCostMicro) // dollars at 10⁻¹²
	c := Converted{USD: roundDiv(new(big.Int).Mul(exact, pow10(pair.USD.Decimals)), pow10(12))}
	if !found || nano <= 0 {
		c.Unconverted = 1
		return c
	}
	c.Local = roundDiv(new(big.Int).Mul(new(big.Int).Mul(exact, big.NewInt(nano)), pow10(pair.Local.Decimals)), pow10(21))
	return c
}

// LossesOn are a business day's losses and gains, the pounds reading at the rate of the day.
func LossesOn(movements []Movement, date string, rate Rate, found bool, pair Pair) Losses {
	var l Losses
	for _, m := range movements {
		if m.BusinessDate != date || (m.Kind != MoveAdjustment && m.Kind != MoveCount) || m.QuantityMicro == 0 {
			continue
		}
		q := m.QuantityMicro
		if q < 0 {
			q = -q
		}
		v := rowValue(q, m.UnitCostMicro, rate.Nano, found, pair)
		switch {
		case m.QuantityMicro > 0:
			l.Surplus.Add(v)
		case m.Kind == MoveCount:
			l.Shortfall.Add(v)
		case m.Reason == ReasonDamaged || m.Reason == ReasonExpired:
			l.Spoiled.Add(v)
		case m.Reason == ReasonOwnUse || m.Reason == ReasonGift:
			l.OwnUse.Add(v)
		default:
			l.Other.Add(v)
		}
	}
	return l
}

// BadDebtsOn are the debts written off on a business day, less write-offs reversed that day, the other reading at the
// rate of the day (L6 §4.4).
func BadDebtsOn(entries []DebtEntry, date string, rate Rate, found bool, pair Pair) Converted {
	var c Converted
	for _, e := range entries {
		if e.BusinessDate != date {
			continue
		}
		if e.Kind == DebtWriteOff || (e.Kind == DebtReversal && e.ReversesKind == DebtWriteOff) {
			c.Add(inBoth(-e.AmountMinor, e.Currency, rate.Nano, found, pair))
		}
	}
	return c
}

// Category is an expense category's total.
type Category struct {
	Category string
	Amount   Converted
}

// ExpensesOn are a business day's expenses, each at the rate it snapshotted, less those reversed that day at theirs.
func ExpensesOn(cash []CashEntry, date string, pair Pair) (Converted, []Category) {
	var total Converted
	by := map[string]*Converted{}
	for _, e := range cash {
		if e.BusinessDate != date {
			continue
		}
		original, sign := e, 1
		if e.Kind == CashReversal && e.Reverses != nil {
			original, sign = *e.Reverses, -1
		}
		if original.Kind != CashExpense {
			continue
		}
		v := inBoth(original.AmountMinor, original.Currency, original.RateNano, true, pair)
		if sign < 0 {
			v = v.neg()
		}
		total.Add(v)
		if by[original.Category] == nil {
			by[original.Category] = &Converted{}
		}
		by[original.Category].Add(v)
	}
	var out []Category
	for k, v := range by {
		out = append(out, Category{Category: k, Amount: *v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Category < out[j].Category })
	return total, out
}

// Takings is what the till did in one currency (L6 §5): the currency sales were charged in, or a debt is kept in.
type Takings struct {
	Currency     string
	Sales        int
	ChargedMinor int64
	CreditSales  int
	CreditMinor  int64
	// DiscountMinor is line and whole-sale discounts given, in the charged currency.
	DiscountMinor int64
	RoundingMinor int64
	Voids         int
	VoidedMinor   int64
	// CollectedMinor, RefundedMinor and WrittenOffMinor are debt payments, refunds and write-offs, each less those
	// reversed the same day.
	CollectedMinor  int64
	RefundedMinor   int64
	WrittenOffMinor int64
}

// Add adds o.
func (t *Takings) Add(o Takings) {
	t.Sales += o.Sales
	t.ChargedMinor += o.ChargedMinor
	t.CreditSales += o.CreditSales
	t.CreditMinor += o.CreditMinor
	t.DiscountMinor += o.DiscountMinor
	t.RoundingMinor += o.RoundingMinor
	t.Voids += o.Voids
	t.VoidedMinor += o.VoidedMinor
	t.CollectedMinor += o.CollectedMinor
	t.RefundedMinor += o.RefundedMinor
	t.WrittenOffMinor += o.WrittenOffMinor
}

func (t Takings) empty() bool { return t == Takings{Currency: t.Currency} }

// TakingsOn is a business day's takings per currency, the dollars first.
func TakingsOn(sales []Sale, entries []DebtEntry, date string, pair Pair) []Takings {
	by := map[string]*Takings{pair.USD.Code: {Currency: pair.USD.Code}, pair.Local.Code: {Currency: pair.Local.Code}}
	at := func(code string) *Takings {
		if by[code] == nil {
			by[code] = &Takings{Currency: code}
		}
		return by[code]
	}
	for _, s := range sales {
		t := at(s.SettlementCurrency)
		if s.BusinessDate == date {
			t.Sales++
			t.ChargedMinor += s.TotalMinor
			if s.Credit {
				t.CreditSales++
				t.CreditMinor += s.CreditMinor
			}
			t.RoundingMinor += s.RoundingMinor
			t.DiscountMinor += inSettlement(s, pair, s.DiscountLocalMinor, s.DiscountUSDMinor)
			for _, l := range s.Lines {
				t.DiscountMinor += inSettlement(s, pair, l.DiscountLocalMinor, l.DiscountUSDMinor)
			}
		}
		if s.Voided && s.VoidBusinessDate == date {
			t.Voids++
			t.VoidedMinor += s.TotalMinor
		}
	}
	for _, e := range entries {
		if e.BusinessDate != date {
			continue
		}
		kind := e.Kind // a reversal's amount is its entry's, negated, so it takes the entry back
		if e.Kind == DebtReversal {
			kind = e.ReversesKind
		}
		t := at(e.Currency)
		switch kind {
		case DebtPayment:
			t.CollectedMinor -= e.AmountMinor
		case DebtRefund:
			t.RefundedMinor += e.AmountMinor
		case DebtWriteOff:
			t.WrittenOffMinor -= e.AmountMinor
		}
	}
	out := []Takings{*by[pair.USD.Code], *by[pair.Local.Code]}
	for code, t := range by {
		if code != pair.USD.Code && code != pair.Local.Code {
			out = append(out, *t)
		}
	}
	return out
}

func inSettlement(s Sale, pair Pair, local, usd int64) int64 {
	if s.SettlementCurrency == pair.USD.Code {
		return usd
	}
	return local
}

// Facts is everything a range's profit and takings are computed from.
type Facts struct {
	Pair      Pair
	Rates     []Rate
	Sales     []Sale
	Movements []Movement
	Debts     []DebtEntry
	Cash      []CashEntry
}

// Day is a business day's statement, read top to bottom: gross profit, losses, bad debts, expenses, net profit; then
// the takings (L6 §10.2).
type Day struct {
	Date       string
	Profit     Profit
	Losses     Losses
	BadDebts   Converted
	Expenses   Converted
	Categories []Category
	Takings    []Takings
	// Rate is the rate of the day, which losses and bad debts are converted at; RateFound is false before any rate.
	Rate      Rate
	RateFound bool
}

// NetUSD is net profit in dollars (D-L6.5).
func (d Day) NetUSD() int64 {
	return d.Profit.ProfitUSD() - d.Losses.Out().USD + d.Losses.Surplus.USD - d.BadDebts.USD - d.Expenses.USD
}

// NetLocal is net profit in pounds.
func (d Day) NetLocal() int64 {
	return d.Profit.ProfitLocal() - d.Losses.Out().Local + d.Losses.Surplus.Local - d.BadDebts.Local - d.Expenses.Local
}

// Unconverted counts the figures of the day left out of a reading for want of a rate.
func (d Day) Unconverted() int {
	return d.Losses.Out().Unconverted + d.Losses.Surplus.Unconverted + d.BadDebts.Unconverted + d.Expenses.Unconverted
}

// Active is true when anything happened that day.
func (d Day) Active() bool {
	if d.Profit != (Profit{}) || d.Losses != (Losses{}) || d.BadDebts != (Converted{}) || d.Expenses != (Converted{}) {
		return true
	}
	for _, t := range d.Takings {
		if !t.empty() {
			return true
		}
	}
	return false
}

// DayOf computes a business day from the facts of a range that contains it.
func DayOf(f Facts, date string) Day {
	d := Day{Date: date, Profit: ProfitOn(f.Sales, date, f.Pair)}
	d.Rate, d.RateFound = RateOfDay(f.Rates, date)
	d.Losses = LossesOn(f.Movements, date, d.Rate, d.RateFound, f.Pair)
	d.BadDebts = BadDebtsOn(f.Debts, date, d.Rate, d.RateFound, f.Pair)
	d.Expenses, d.Categories = ExpensesOn(f.Cash, date, f.Pair)
	d.Takings = TakingsOn(f.Sales, f.Debts, date, f.Pair)
	return d
}

// Total adds days into one statement for a range; its rate is left unset, each day having its own.
func Total(from string, days []Day) Day {
	out := Day{Date: from}
	cats := map[string]*Converted{}
	takings := map[string]*Takings{}
	var order []string
	for _, d := range days {
		out.Profit.Add(d.Profit, 1)
		out.Losses.Add(d.Losses)
		out.BadDebts.Add(d.BadDebts)
		out.Expenses.Add(d.Expenses)
		for _, c := range d.Categories {
			if cats[c.Category] == nil {
				cats[c.Category] = &Converted{}
			}
			cats[c.Category].Add(c.Amount)
		}
		for _, t := range d.Takings {
			if takings[t.Currency] == nil {
				takings[t.Currency] = &Takings{Currency: t.Currency}
				order = append(order, t.Currency)
			}
			takings[t.Currency].Add(t)
		}
	}
	for k, v := range cats {
		out.Categories = append(out.Categories, Category{Category: k, Amount: *v})
	}
	sort.Slice(out.Categories, func(i, j int) bool { return out.Categories[i].Category < out.Categories[j].Category })
	for _, code := range order {
		out.Takings = append(out.Takings, *takings[code])
	}
	return out
}

// Month is a calendar month by business date: a row per day that had anything in it, and the month's totals — the sum
// of its days (Q-L6.8).
type Month struct {
	Month    string
	From, To string
	Days     []Day
	Total    Day
}

// MonthOf computes a month from the facts of its range.
func MonthOf(f Facts, month, from, to string) Month {
	m := Month{Month: month, From: from, To: to}
	var all []Day
	for _, date := range Dates(from, to) {
		d := DayOf(f, date)
		all = append(all, d)
		if d.Active() {
			m.Days = append(m.Days, d)
		}
	}
	m.Total = Total(from, all)
	return m
}
