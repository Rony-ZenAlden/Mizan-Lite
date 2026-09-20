package domain

// DrawerTerms is what should be in the drawer in one currency at the end of a business day, and how it was made up:
// money summed in the currency it physically moved in, nothing converted (D-L6.12, L6 §7.1).
type DrawerTerms struct {
	Currency string
	// Opening is the count that closed the last counted day, plus what moved on the days after it before this one.
	Opening int64
	// OpeningCount is the count Opening starts from, or nil when the drawer was never counted before this day.
	OpeningCount *CashEntry

	CashSalesIn  int64
	CreditPaidIn int64
	RepaymentsIn int64
	DepositsIn   int64
	ChangeOut    int64
	RefundsOut   int64
	VoidReturns  int64
	// ReturnsOut is cash handed back over the counter for goods returned (2026-09-20). A return settled against a
	// customer's debt moves no cash and is not here.
	ReturnsOut    int64
	ExpensesOut   int64
	WithdrawalOut int64

	Expected int64
	// Count is the day's count — the newest not reversed — or nil.
	Count *CashEntry
}

// Moved is what the day's terms add to the opening.
func (t DrawerTerms) Moved() int64 {
	return t.CashSalesIn + t.CreditPaidIn + t.RepaymentsIn + t.DepositsIn -
		t.ChangeOut - t.RefundsOut - t.VoidReturns - t.ReturnsOut - t.ExpensesOut - t.WithdrawalOut
}

// Difference is the day's count less what it was expected to hold when counted: below zero is a shortage.
func (t DrawerTerms) Difference() (int64, bool) {
	if t.Count == nil {
		return 0, false
	}
	return t.Count.AmountMinor - t.Count.ExpectedMinor, true
}

// DrawerFacts is what a drawer is computed from: the facts from the day after the oldest last count to the date.
type DrawerFacts struct {
	Pair    Pair
	Sales   []Sale
	Debts   []DebtEntry
	Cash    []CashEntry
	Returns []Return
	// LastCounts is, per currency, the newest count not reversed on a day before the date.
	LastCounts map[string]CashEntry
}

func in(code, currency string, minor int64) int64 {
	if code == currency {
		return minor
	}
	return 0
}

// cashMoves adds to t what moved in its currency on business dates from..to.
func cashMoves(t *DrawerTerms, f DrawerFacts, from, to string) {
	c := t.Currency
	within := func(date string) bool { return date >= from && date <= to }
	for _, s := range f.Sales {
		if within(s.BusinessDate) {
			if s.Credit {
				t.CreditPaidIn += in(s.TenderedCurrency, c, s.TenderedMinor)
			} else {
				t.CashSalesIn += in(s.TenderedCurrency, c, s.TenderedMinor)
			}
			t.ChangeOut += in(s.ChangeCurrency, c, s.ChangeMinor)
		}
		if s.Voided && within(s.VoidBusinessDate) {
			t.VoidReturns += in(s.VoidReturnCurrency, c, s.VoidReturnMinor)
		}
	}
	for _, r := range f.Returns {
		// Only a cash return takes notes out of the drawer; one settled against a debt moves no cash.
		if within(r.BusinessDate) && r.Settlement == SettlementCash {
			t.ReturnsOut += in(r.SettlementCurrency, c, r.RefundMinor)
		}
	}
	for _, e := range f.Debts {
		if !within(e.BusinessDate) {
			continue
		}
		kind, cash, sign := e.Kind, e.Cash, int64(1)
		if e.Kind == DebtReversal { // a reversed payment or refund takes its cash back on the reversal's day
			kind, cash, sign = e.ReversesKind, e.ReversesCash, -1
		}
		switch kind {
		case DebtPayment:
			t.RepaymentsIn += sign * in(cash.TenderedCurrency, c, cash.TenderedMinor)
			t.ChangeOut += sign * in(cash.ChangeCurrency, c, cash.ChangeMinor)
		case DebtRefund:
			t.RefundsOut += sign * (in(cash.TenderedCurrency, c, cash.TenderedMinor) - in(cash.ChangeCurrency, c, cash.ChangeMinor))
		}
	}
	for _, e := range f.Cash {
		if !within(e.BusinessDate) {
			continue
		}
		entry, sign := e, int64(1)
		if e.Kind == CashReversal && e.Reverses != nil {
			entry, sign = *e.Reverses, -1
		}
		amount := sign * in(entry.Currency, c, entry.AmountMinor)
		switch entry.Kind {
		case CashExpense:
			if entry.FromDrawer {
				t.ExpensesOut += amount
			}
		case CashWithdrawal:
			t.WithdrawalOut += amount
		case CashDeposit:
			t.DepositsIn += amount
		}
	}
}

// DrawerOn is the drawer in one currency at the end of a business date. A count closes its day: the next day starts from
// it, so a difference is found once, on the day it happened (D-L6.15).
func DrawerOn(f DrawerFacts, date, currency string) DrawerTerms {
	t := DrawerTerms{Currency: currency}
	opening := DrawerTerms{Currency: currency}
	from := ""
	if last, ok := f.LastCounts[currency]; ok {
		count := last
		t.OpeningCount = &count
		opening.Opening = last.AmountMinor
		from = AddDays(last.BusinessDate, 1)
	}
	cashMoves(&opening, f, from, AddDays(date, -1))
	t.Opening = opening.Opening + opening.Moved()
	cashMoves(&t, f, date, date)
	t.Expected = t.Opening + t.Moved()
	for i := len(f.Cash) - 1; i >= 0; i-- {
		e := f.Cash[i]
		if e.Kind == CashCount && e.Currency == currency && e.BusinessDate == date && !e.Reversed {
			if t.Count == nil || e.Seq > t.Count.Seq {
				count := e
				t.Count = &count
			}
		}
	}
	return t
}

// Drawer is a business day's drawer per currency, dollars first, and the day's cash book.
type Drawer struct {
	Date       string
	Currencies []DrawerTerms
	Entries    []CashEntry
}

// DrawerOf computes the day's drawer in both currencies.
func DrawerOf(f DrawerFacts, date string) Drawer {
	d := Drawer{Date: date}
	for _, c := range []string{f.Pair.USD.Code, f.Pair.Local.Code} {
		d.Currencies = append(d.Currencies, DrawerOn(f, date, c))
	}
	for _, e := range f.Cash {
		if e.BusinessDate == date {
			d.Entries = append(d.Entries, e)
		}
	}
	return d
}
