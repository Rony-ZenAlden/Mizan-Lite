package domain

import (
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Kind is what an entry in the debt book is (L5 §4.1).
type Kind string

// The kinds.
const (
	KindOpening  Kind = "opening"
	KindCharge   Kind = "charge"
	KindPayment  Kind = "payment"
	KindWriteOff Kind = "write_off"
	KindRefund   Kind = "refund"
	KindReversal Kind = "reversal"
	// KindSaleReturn takes goods handed back off what a customer owes (2026-09-20).
	//
	// It is not a write-off: a write-off is the shop forgiving a debt it does not expect to collect, and the reports
	// count one as a BAD DEBT. A customer returning goods has not failed to pay anybody.
	KindSaleReturn Kind = "sale_return"
	// KindConversion moves a balance from one currency to another when the shop goes over to dollars only (0.10.0):
	// the pounds off the pound chain, the same sum at the day's rate onto the dollar chain. Either sign, no cash, a note.
	//
	// Not an opening, which is always above nought, and not a write-off, which the reports count as a bad debt: a
	// customer whose debt was re-counted in dollars has neither started owing nor failed to pay.
	KindConversion Kind = "conversion"
)

// Currency is a currency's code and decimals.
type Currency struct {
	Code     string
	Decimals int
}

// Cash is money that changed hands on a payment or a refund, with the rate and the note it was worked out at.
type Cash struct {
	RateID           id.ID
	RateNano         int64
	CashNoteMinor    int64
	TenderedCurrency string
	TenderedMinor    int64
	ChangeCurrency   string
	ChangeMinor      int64
}

// Entry is one line of a customer's debt book, in one currency.
type Entry struct {
	ID                 id.ID
	CustomerID         id.ID
	Currency           string
	Seq                int64
	BusinessDate       string
	OccurredAt         time.Time
	Kind               Kind
	AmountMinor        int64
	BalanceBeforeMinor int64
	BalanceAfterMinor  int64
	SaleID             id.ID
	ReversesID         id.ID
	Cash               Cash
	CustomerName       string
	Note               string
}

// Place is the newest point of one chain: its place number and balance. The zero Place is a chain with no entries.
type Place struct {
	Seq          int64
	BalanceMinor int64
}

// Append places e at the end of the chain p is the newest point of — the next seq, balance before and after — and
// refuses what the book's CHECKs refuse, so a service error names the rule instead of a constraint (L5 §4.2).
func (p Place) Append(e Entry) (Entry, error) {
	if e.AmountMinor == 0 {
		return Entry{}, errs.Validation(CodeAmountRequired, "an entry moves a balance").WithField(FieldAmount, CodeAmountRequired, "required")
	}
	after, ok := addChecked(p.BalanceMinor, e.AmountMinor)
	if !ok {
		return Entry{}, errs.Validation(CodeAmountTooLarge, "the amount is out of range").WithField(FieldAmount, CodeAmountTooLarge, "too large")
	}
	switch e.Kind {
	case KindOpening, KindCharge:
		if e.AmountMinor < 0 {
			return Entry{}, errs.Validation(CodeAmountRequired, "an opening or charge adds to a debt")
		}
	case KindPayment:
		if p.BalanceMinor <= 0 {
			return Entry{}, errs.Conflict(CodeNothingOwed, "nothing is owed in this currency")
		}
		if e.AmountMinor > 0 || after < 0 {
			return Entry{}, errs.Validation(CodeAmountTooLarge, "a payment settles at most the balance")
		}
	case KindWriteOff:
		if p.BalanceMinor <= 0 {
			return Entry{}, errs.Conflict(CodeNothingOwed, "nothing is owed in this currency")
		}
		if e.AmountMinor > 0 || after < 0 {
			return Entry{}, errs.Validation(CodeWriteOffTooLarge, "a write-off forgives at most the balance").
				WithField(FieldAmount, CodeWriteOffTooLarge, "too large")
		}
	case KindRefund:
		if p.BalanceMinor >= 0 {
			return Entry{}, errs.Conflict(CodeNothingOwedBack, "the shop owes the customer nothing in this currency")
		}
		if e.AmountMinor < 0 || after > 0 {
			return Entry{}, errs.Validation(CodeRefundTooLarge, "a refund pays back at most what is owed").
				WithField(FieldAmount, CodeRefundTooLarge, "too large")
		}
	case KindSaleReturn:
		// Goods coming back reduce the balance, and MAY take it below zero: a customer who owes 2,000 and returns
		// 5,000 of goods is owed 3,000, which is what a refund then pays back. Refusing it would leave the shop
		// unable to record what plainly happened.
		if e.AmountMinor > 0 {
			return Entry{}, errs.Validation(CodeAmountRequired, "a return reduces a balance")
		}
	case KindReversal, KindConversion:
	default:
		return Entry{}, errs.Validation("database.constraint_violation", "unknown kind")
	}
	e.Seq, e.BalanceBeforeMinor, e.BalanceAfterMinor = p.Seq+1, p.BalanceMinor, after
	return e, nil
}

// Reverse builds the reversal of an entry: the opposite amount, in the same chain, with a reason (L5 §7.4). A charge is
// reversed only by voiding its sale — the service says which it is doing.
func Reverse(original Entry, reason string, bySale bool) (Entry, error) {
	switch {
	case original.Kind == KindReversal:
		return Entry{}, errs.Conflict(CodeNotReversible, "a reversal is never reversed")
	case original.Kind == KindCharge && !bySale:
		return Entry{}, errs.Conflict(CodeNotReversible, "a charge is reversed by voiding its sale")
	case original.Kind == KindConversion:
		// Half a conversion undone would leave the balance in both currencies at once.
		return Entry{}, errs.Conflict(CodeNotReversible, "a conversion moved a balance and is never reversed")
	}
	note, err := Reason(reason)
	if err != nil {
		return Entry{}, err
	}
	return Entry{
		CustomerID: original.CustomerID, Currency: original.Currency, Kind: KindReversal, AmountMinor: -original.AmountMinor,
		ReversesID: original.ID, CustomerName: original.CustomerName, Note: note,
	}, nil
}

// Summary is what a chain says about a debt without a stored field (L5 §4.3).
type Summary struct {
	Currency     string
	BalanceMinor int64
	// OwedSince is the business date the current debt started, "" when nothing is owed.
	OwedSince string
	// LastPayment is the business date of the newest payment, "".
	LastPayment string
	Place       Place
}

// Summarise reads a chain's entries, in place order.
func Summarise(currency string, entries []Entry) Summary {
	s := Summary{Currency: currency}
	for _, e := range entries {
		if e.BalanceBeforeMinor <= 0 && e.BalanceAfterMinor > 0 {
			s.OwedSince = e.BusinessDate
		}
		if e.BalanceAfterMinor <= 0 {
			s.OwedSince = ""
		}
		if e.Kind == KindPayment {
			s.LastPayment = e.BusinessDate
		}
		s.BalanceMinor, s.Place = e.BalanceAfterMinor, Place{Seq: e.Seq, BalanceMinor: e.BalanceAfterMinor}
	}
	return s
}

func addChecked(a, b int64) (int64, bool) {
	s := a + b
	if (b > 0 && s < a) || (b < 0 && s > a) {
		return 0, false
	}
	return s, true
}
