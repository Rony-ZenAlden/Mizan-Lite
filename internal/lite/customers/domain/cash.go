package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
	"github.com/mizan-erp/mizan/internal/lite/tender"
)

// maxMinor bounds any amount typed into the debt book: ten trillion minor units, far beyond a shop and far inside int64.
const maxMinor = int64(10_000_000_000_000)

// CashContext is what a payment or a refund is worked out against, read in one moment.
type CashContext struct {
	Local, USD Currency
	RateID     id.ID
	RateNano   int64
	CashNote   int64
	CustomerID id.ID
	// Place is the newest point of the chain in the debt's currency.
	Place Place
}

// CashInput is a payment or a refund as typed (L5 §6).
type CashInput struct {
	// Currency is the debt's currency.
	Currency string
	// TenderCurrency is the currency handed over (a payment) or paid out (a refund); "" for the debt's.
	TenderCurrency string
	// Amount is what was handed over or paid out, in TenderCurrency; ignored when All.
	Amount string
	// All settles the whole balance: pay all, refund all (L5 §6.3).
	All bool
	// ChangeCurrency is "" for the default — pounds, unless the debt and the tender are both dollars (Q-L4.2). Payments only.
	ChangeCurrency string
}

// CashQuote is a payment or refund worked out: every figure the screen shows, and the token recording compares.
type CashQuote struct {
	Kind               Kind
	Debt               Currency
	Tender             Currency
	Change             Currency
	TenderedMinor      int64
	ChangeMinor        int64
	SettledMinor       int64
	BalanceBeforeMinor int64
	BalanceAfterMinor  int64
	All                bool
	RateID             id.ID
	RateNano           int64
	CashNote           int64
	Token              string
}

func (c CashContext) currency(code, field string) (Currency, error) {
	switch code {
	case c.Local.Code:
		return c.Local, nil
	case c.USD.Code:
		return c.USD, nil
	}
	e := errs.Validation(CodeUnknownCurrency, "the local currency or USD").WithParam("value", code)
	if field != "" {
		e = e.WithField(field, CodeUnknownCurrency, "unknown")
	}
	return Currency{}, e
}

// PricePayment works out a repayment: what an amount handed over settles at the rate in force, and the change — or, for
// pay all, what to take to clear the balance exactly (L5 §6.2, §6.3, Q-L5.5).
func PricePayment(in CashInput, c CashContext) (CashQuote, error) {
	q, err := c.begin(in, KindPayment)
	if err != nil {
		return CashQuote{}, err
	}
	owed := c.Place.BalanceMinor
	if owed <= 0 {
		return CashQuote{}, errs.Conflict(CodeNothingOwed, "nothing is owed in this currency")
	}
	if q.Change, err = c.changeCurrency(in.ChangeCurrency, q.Debt, q.Tender); err != nil {
		return CashQuote{}, err
	}
	if in.All {
		if q.TenderedMinor, err = c.takeToClear(owed, q.Debt, q.Tender); err != nil {
			return CashQuote{}, err
		}
		q.SettledMinor, q.Change = owed, q.Tender
	} else {
		value, err := c.valueOf(in.Amount, q.Tender, q.Debt, &q)
		if err != nil {
			return CashQuote{}, err
		}
		q.SettledMinor = min(value, owed)
		if value > owed {
			exact := new(big.Rat).Sub(tender.Convert(q.TenderedMinor, tc(q.Tender), tc(q.Change), c.RateNano),
				tender.Convert(owed, tc(q.Debt), tc(q.Change), c.RateNano))
			q.ChangeMinor = max(tender.Round(exact, tender.Increment(tc(q.Change), c.CashNote)), 0)
		}
	}
	q.BalanceBeforeMinor, q.BalanceAfterMinor = owed, owed-q.SettledMinor
	q.Token = cashToken(c, q)
	return q, nil
}

// PriceRefund works out paying back a balance below zero: never past zero, no change (L5 §7.3).
func PriceRefund(in CashInput, c CashContext) (CashQuote, error) {
	q, err := c.begin(in, KindRefund)
	if err != nil {
		return CashQuote{}, err
	}
	owedBack := -c.Place.BalanceMinor
	if owedBack <= 0 {
		return CashQuote{}, errs.Conflict(CodeNothingOwedBack, "the shop owes the customer nothing in this currency")
	}
	q.Change = q.Tender
	if in.All {
		if q.TenderedMinor, err = c.takeToClear(owedBack, q.Debt, q.Tender); err != nil {
			return CashQuote{}, err
		}
		q.SettledMinor = owedBack
	} else {
		value, err := c.valueOf(in.Amount, q.Tender, q.Debt, &q)
		if err != nil {
			return CashQuote{}, err
		}
		if value > owedBack {
			return CashQuote{}, errs.Validation(CodeRefundTooLarge, "a refund pays back at most what is owed").
				WithField(FieldAmount, CodeRefundTooLarge, "too large")
		}
		q.SettledMinor = value
	}
	q.BalanceBeforeMinor, q.BalanceAfterMinor = c.Place.BalanceMinor, c.Place.BalanceMinor+q.SettledMinor
	q.Token = cashToken(c, q)
	return q, nil
}

func (c CashContext) begin(in CashInput, kind Kind) (CashQuote, error) {
	if c.RateNano <= 0 {
		return CashQuote{}, errs.Conflict(CodeNoRate, "set an exchange rate before taking money")
	}
	debt, err := c.currency(in.Currency, "")
	if err != nil {
		return CashQuote{}, err
	}
	q := CashQuote{Kind: kind, Debt: debt, Tender: debt, All: in.All, RateID: c.RateID, RateNano: c.RateNano, CashNote: c.CashNote}
	if in.TenderCurrency != "" {
		if q.Tender, err = c.currency(in.TenderCurrency, FieldTendered); err != nil {
			return CashQuote{}, err
		}
	}
	return q, nil
}

// changeCurrency is L4's rule (Q-L4.2, Q-L4.3): pounds, unless the debt and the tender are both dollars; dollars on
// request; pounds for dollars paid on a dollar debt refused.
func (c CashContext) changeCurrency(raw string, debt, tendered Currency) (Currency, error) {
	bothDollars := debt.Code == c.USD.Code && tendered.Code == c.USD.Code
	switch raw {
	case "":
		if bothDollars {
			return c.USD, nil
		}
		return c.Local, nil
	case c.Local.Code:
		if bothDollars {
			return Currency{}, errs.Validation(CodeChangeCurrency, "dollars paid on a dollar debt give dollar change").
				WithField(FieldChangeCurrency, CodeChangeCurrency, "dollars")
		}
		return c.Local, nil
	case c.USD.Code:
		return c.USD, nil
	}
	return c.currency(raw, FieldChangeCurrency)
}

// takeToClear is what to take in the tender currency to clear an amount in the debt's: the amount itself, or its
// conversion rounded to the note or the cent (L5 §6.3).
func (c CashContext) takeToClear(amount int64, debt, tendered Currency) (int64, error) {
	if tendered.Code == debt.Code {
		return amount, nil
	}
	take := tender.Round(tender.Convert(amount, tc(debt), tc(tendered), c.RateNano), tender.Increment(tc(tendered), c.CashNote))
	if take <= 0 {
		return 0, errs.Validation(CodeBelowNote, "less than the smallest note in that currency").
			WithField(FieldTendered, CodeBelowNote, "below note")
	}
	return take, nil
}

// valueOf reads an amount typed in the tender currency and returns what it is worth in the debt's: exact in the same
// currency, otherwise rounded once to the debt's minor unit (L5 §6.2).
func (c CashContext) valueOf(raw string, tendered, debt Currency, q *CashQuote) (int64, error) {
	minor, err := ParseAmount(raw, tendered, FieldAmount)
	if err != nil {
		return 0, err
	}
	q.TenderedMinor = minor
	if tendered.Code == debt.Code {
		return minor, nil
	}
	value := tender.Round(tender.Convert(minor, tc(tendered), tc(debt), c.RateNano), 1)
	if value <= 0 {
		return 0, errs.Validation(CodeBelowNote, "worth less than the smallest unit of the debt").
			WithField(FieldAmount, CodeBelowNote, "below unit")
	}
	return value, nil
}

// Entry is the book entry a quote records, before it is placed on its chain.
func (q CashQuote) Entry(customerID id.ID, customerName, note string) Entry {
	amount := -q.SettledMinor
	if q.Kind == KindRefund {
		amount = q.SettledMinor
	}
	return Entry{
		CustomerID: customerID, Currency: q.Debt.Code, Kind: q.Kind, AmountMinor: amount, CustomerName: customerName, Note: note,
		Cash: Cash{RateID: q.RateID, RateNano: q.RateNano, CashNoteMinor: q.CashNote, TenderedCurrency: q.Tender.Code,
			TenderedMinor: q.TenderedMinor, ChangeCurrency: q.Change.Code, ChangeMinor: q.ChangeMinor},
	}
}

// ParseAmount reads a positive amount in a currency's decimals, digits in any script, as minor units.
func ParseAmount(raw string, c Currency, field string) (int64, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, errs.Validation(CodeAmountRequired, "enter an amount").WithField(field, CodeAmountRequired, "required")
	}
	normalised, err := numinput.Normalise(raw)
	if err != nil {
		if typed, ok := errs.AsError(err); ok {
			return 0, typed.WithField(field, typed.Code, "invalid")
		}
		return 0, err
	}
	if numinput.Decimals(normalised) > c.Decimals {
		return 0, errs.Validation(CodeAmountDecimals, "too many decimals for the currency").
			WithField(field, CodeAmountDecimals, "decimals").WithParam("currency", c.Code).WithParam("decimals", strconv.Itoa(c.Decimals))
	}
	v, ok := new(big.Rat).SetString(normalised)
	if !ok {
		return 0, errs.Validation(numinput.CodeInvalid, "not a number").WithField(field, numinput.CodeInvalid, "invalid")
	}
	v.Mul(v, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(c.Decimals)), nil)))
	if !v.IsInt() || !v.Num().IsInt64() || v.Num().Int64() > maxMinor {
		return 0, errs.Validation(CodeAmountTooLarge, "the amount is out of range").WithField(field, CodeAmountTooLarge, "too large")
	}
	if v.Num().Int64() <= 0 {
		return 0, errs.Validation(CodeAmountRequired, "enter an amount above zero").WithField(field, CodeAmountRequired, "required")
	}
	return v.Num().Int64(), nil
}

func tc(c Currency) tender.Currency { return tender.Currency(c) }

// cashToken fingerprints everything that decides what money changes hands: the rate, the note, the chain's newest place,
// and what was typed. A payment taken elsewhere on the same debt moves the place, and the quote is stale (L5 §6.4).
func cashToken(c CashContext, q CashQuote) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s|rate=%s:%d|note=%d|customer=%s|debt=%s|place=%d:%d|tender=%s:%d|change=%s:%d|all=%t|settled=%d",
		q.Kind, c.RateID, c.RateNano, c.CashNote, c.CustomerID, q.Debt.Code, c.Place.Seq, c.Place.BalanceMinor,
		q.Tender.Code, q.TenderedMinor, q.Change.Code, q.ChangeMinor, q.All, q.SettledMinor)
	return hex.EncodeToString(h.Sum(nil))
}
