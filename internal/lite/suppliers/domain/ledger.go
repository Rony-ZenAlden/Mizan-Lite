package domain

import (
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// The payables book: one chain per supplier and currency, each entry moving the balance the shop owes that supplier.
//
// A balance above zero is what the shop owes; below zero, what the supplier owes the shop — goods paid for that arrived
// damaged, or money paid ahead. Every entry records the balance before and after it, and the database refuses a chain
// that does not add up (migration 0012), as the customers' book does.

// Kind is what an entry in the payables book is.
type Kind string

// The kinds.
const (
	// KindOpening is a balance brought over from the paper book, either way.
	KindOpening Kind = "opening"
	// KindPurchase is what a purchase costs the shop, after its discounts and without its damaged goods.
	KindPurchase Kind = "purchase"
	// KindPayment is money the shop paid the supplier.
	KindPayment Kind = "payment"
	// KindRefund is money the supplier paid back, against what they owed the shop.
	KindRefund Kind = "refund"
	// KindReversal undoes an entry made by mistake, with its reason.
	KindReversal Kind = "reversal"
)

// CashSource is where money paid to a supplier came from, or where money from one went (the owner's choice,
// 2026-09-24): the till's drawer — whose expected cash then moves with it — or the owner's own money, which the drawer
// never sees.
type CashSource string

// The sources.
const (
	SourceDrawer CashSource = "drawer"
	SourceOwner  CashSource = "owner"
)

// ParseSource reads a cash source.
func ParseSource(raw string) (CashSource, error) {
	switch s := CashSource(raw); s {
	case SourceDrawer, SourceOwner:
		return s, nil
	}
	return "", errs.Validation(CodeCashSourceInvalid, "the drawer or the owner's own money").
		WithField(FieldSource, CodeCashSourceInvalid, "invalid")
}

// Entry is one line of a supplier's book, in one currency.
type Entry struct {
	ID                 id.ID
	SupplierID         id.ID
	Currency           string
	Seq                int64
	BusinessDate       string
	OccurredAt         time.Time
	Kind               Kind
	AmountMinor        int64
	BalanceBeforeMinor int64
	BalanceAfterMinor  int64
	// PurchaseID is the purchase a purchase entry is, or a payment was made with.
	PurchaseID id.ID
	ReversesID id.ID
	// Source is where the money came from or went: set on a payment and a refund, and on a reversal of one — undoing a
	// payment from the drawer puts the money back in the drawer's expected cash.
	Source       CashSource
	SupplierName string
	Note         string
}

// DrawerMinor is what this entry did to the drawer's cash in its currency: the entry's own amount when the money moved
// through the drawer — a payment of 50 is 50 out of the drawer, a refund of 20 is 20 into it — and nothing otherwise.
func (e Entry) DrawerMinor() int64 {
	if e.Source != SourceDrawer {
		return 0
	}
	return e.AmountMinor
}

// Place is the newest point of one chain: its place number and balance. The zero Place is a chain with no entries.
type Place struct {
	Seq          int64
	BalanceMinor int64
}

// Append places e at the end of the chain p is the newest point of, and refuses what the book's CHECKs refuse, so a
// service error names the rule instead of a constraint.
func (p Place) Append(e Entry) (Entry, error) {
	if e.AmountMinor == 0 {
		return Entry{}, errs.Validation(CodeAmountRequired, "an entry moves a balance").WithField(FieldAmount, CodeAmountRequired, "required")
	}
	after, ok := addChecked(p.BalanceMinor, e.AmountMinor)
	if !ok {
		return Entry{}, errs.Validation(CodeAmountTooLarge, "the amount is out of range").WithField(FieldAmount, CodeAmountTooLarge, "too large")
	}
	switch e.Kind {
	case KindOpening:
		if e.Source != "" {
			return Entry{}, errs.Validation(CodeCashSourceInvalid, "an opening balance moves no cash")
		}
	case KindPurchase:
		if e.AmountMinor < 0 || e.PurchaseID == "" || e.Source != "" {
			return Entry{}, errs.Validation(CodeAmountRequired, "a purchase adds to what the shop owes")
		}
	case KindPayment:
		// A payment may take the balance below zero: money paid ahead, or goods paid for that arrived damaged, is the
		// supplier owing the shop — what a refund then settles.
		if e.AmountMinor > 0 {
			return Entry{}, errs.Validation(CodeAmountRequired, "a payment reduces what the shop owes")
		}
		if _, err := ParseSource(string(e.Source)); err != nil {
			return Entry{}, err
		}
	case KindRefund:
		if p.BalanceMinor >= 0 {
			return Entry{}, errs.Conflict(CodeNothingOwedBack, "the supplier owes the shop nothing in this currency")
		}
		if e.AmountMinor < 0 || after > 0 {
			return Entry{}, errs.Validation(CodeRefundTooLarge, "a refund pays back at most what the supplier owes").
				WithField(FieldAmount, CodeRefundTooLarge, "too large")
		}
		if _, err := ParseSource(string(e.Source)); err != nil {
			return Entry{}, err
		}
	case KindReversal:
		if e.ReversesID == "" {
			return Entry{}, errs.Validation(CodeNotReversible, "a reversal names what it reverses")
		}
	default:
		return Entry{}, errs.Validation("database.constraint_violation", "unknown kind")
	}
	e.Seq, e.BalanceBeforeMinor, e.BalanceAfterMinor = p.Seq+1, p.BalanceMinor, after
	return e, nil
}

// Reverse builds the reversal of an entry: the opposite amount in the same chain, the same cash source, and a reason. A
// purchase is reversed only by voiding the purchase — the stock it brought in has to go back out with it.
func Reverse(original Entry, reason string, byVoid bool) (Entry, error) {
	switch {
	case original.Kind == KindReversal:
		return Entry{}, errs.Conflict(CodeNotReversible, "a reversal is never reversed")
	case original.Kind == KindPurchase && !byVoid:
		return Entry{}, errs.Conflict(CodeNotReversible, "a purchase is reversed by voiding it")
	}
	note, err := Reason(reason)
	if err != nil {
		return Entry{}, err
	}
	return Entry{
		SupplierID: original.SupplierID, Currency: original.Currency, Kind: KindReversal, AmountMinor: -original.AmountMinor,
		ReversesID: original.ID, Source: original.Source, SupplierName: original.SupplierName, Note: note,
	}, nil
}

func addChecked(a, b int64) (int64, bool) {
	s := a + b
	if (b > 0 && s < a) || (b < 0 && s > a) {
		return 0, false
	}
	return s, true
}
