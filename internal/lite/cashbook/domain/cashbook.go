// Package domain holds Mizan Lite's cash book as values: an expense, a withdrawal, a deposit, a closing count and a
// reversal, and the rules each obeys. No I/O (L6 §7.3).
package domain

import (
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
)

// Stable error codes. They double as i18n keys.
const (
	CodeAmountRequired  = "lite.cashbook.amount_required"
	CodeAmountDecimals  = "lite.cashbook.amount_decimals"
	CodeAmountTooLarge  = "lite.cashbook.amount_too_large"
	CodeUnknownKind     = "lite.cashbook.unknown_kind"
	CodeUnknownCategory = "lite.cashbook.unknown_category"
	CodeUnknownCurrency = "lite.cashbook.unknown_currency"
	CodeReasonRequired  = "lite.cashbook.reason_required"
	CodeNoteTooLong     = "lite.cashbook.note_too_long"
	CodeAlreadyReversed = "lite.cashbook.already_reversed"
	CodeNotReversible   = "lite.cashbook.not_reversible"
	CodeEntryNotFound   = "lite.cashbook.entry_not_found"
	CodeNoRate          = "lite.cashbook.no_rate"
)

// Fields named in validation errors.
const (
	FieldAmount   = "amount"
	FieldCategory = "category"
	FieldNote     = "note"
	FieldReason   = "reason"
	FieldKind     = "kind"
)

// MaxNoteRunes bounds a note or reason, as the schema does.
const MaxNoteRunes = 200

// maxMinor bounds an amount far beyond a shop and far inside int64.
const maxMinor = int64(10_000_000_000_000)

// Kind is what a cash book entry is.
type Kind string

// The kinds (L6 §7.3).
const (
	KindExpense    Kind = "expense"
	KindWithdrawal Kind = "withdrawal"
	KindDeposit    Kind = "deposit"
	KindCount      Kind = "count"
	KindReversal   Kind = "reversal"
)

// Categories are an expense's categories, in display order.
var Categories = []string{"rent", "electricity", "wages", "transport", "supplies", "other"}

// Currency is a currency's code and decimals.
type Currency struct {
	Code     string
	Decimals int
}

// Entry is one line of the cash book.
type Entry struct {
	ID           id.ID
	Seq          int64
	BusinessDate string
	OccurredAt   time.Time
	Kind         Kind
	Currency     string
	// AmountMinor is the money moved (> 0), or for a count what was counted (>= 0).
	AmountMinor int64
	// ExpectedMinor is, for a count, what the drawer was expected to hold when it was counted.
	ExpectedMinor int64
	Category      string
	// FromDrawer is, for an expense, whether it was paid from the drawer.
	FromDrawer bool
	ReversesID id.ID
	RateID     id.ID
	RateNano   int64
	Note       string
}

// Draft is a money entry as typed.
type Draft struct {
	Kind       Kind
	Currency   Currency
	Amount     string
	Category   string
	FromDrawer bool
	Note       string
}

// NewMoney validates an expense, withdrawal or deposit. The rate is the caller's snapshot.
func NewMoney(d Draft) (Entry, error) {
	switch d.Kind {
	case KindExpense, KindWithdrawal, KindDeposit:
	default:
		return Entry{}, errs.Validation(CodeUnknownKind, "an expense, a withdrawal or a deposit").WithField(FieldKind, CodeUnknownKind, "unknown").
			WithParam("value", string(d.Kind))
	}
	amount, err := ParseAmount(d.Amount, d.Currency, false)
	if err != nil {
		return Entry{}, err
	}
	note, err := note(d.Note, FieldNote)
	if err != nil {
		return Entry{}, err
	}
	e := Entry{Kind: d.Kind, Currency: d.Currency.Code, AmountMinor: amount, Note: note}
	if d.Kind == KindExpense {
		if !knownCategory(d.Category) {
			return Entry{}, errs.Validation(CodeUnknownCategory, "choose a category").WithField(FieldCategory, CodeUnknownCategory, "unknown").
				WithParam("value", d.Category)
		}
		e.Category, e.FromDrawer = d.Category, d.FromDrawer
	}
	return e, nil
}

// NewCount validates a closing count against what the drawer was expected to hold.
func NewCount(currency Currency, counted string, expectedMinor int64, rawNote string) (Entry, error) {
	amount, err := ParseAmount(counted, currency, true)
	if err != nil {
		return Entry{}, err
	}
	n, err := note(rawNote, FieldNote)
	if err != nil {
		return Entry{}, err
	}
	return Entry{Kind: KindCount, Currency: currency.Code, AmountMinor: amount, ExpectedMinor: expectedMinor, Note: n}, nil
}

// Reverse undoes an entry once, with a reason. A reversal is never reversed.
func Reverse(original Entry, reason string) (Entry, error) {
	if original.Kind == KindReversal {
		return Entry{}, errs.Conflict(CodeNotReversible, "a reversal is never reversed")
	}
	r, err := note(reason, FieldReason)
	if err != nil {
		return Entry{}, err
	}
	if r == "" {
		return Entry{}, errs.Validation(CodeReasonRequired, "a reason is required").WithField(FieldReason, CodeReasonRequired, "required")
	}
	return Entry{Kind: KindReversal, Currency: original.Currency, AmountMinor: original.AmountMinor, ReversesID: original.ID, Note: r}, nil
}

// Difference is a count's counted less expected: below zero is a shortage.
func (e Entry) Difference() int64 { return e.AmountMinor - e.ExpectedMinor }

// ParseAmount reads an amount in a currency's decimals, digits in any script, as minor units: above zero, or — for a
// count — zero or more.
func ParseAmount(raw string, c Currency, zeroAllowed bool) (int64, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, errs.Validation(CodeAmountRequired, "enter an amount").WithField(FieldAmount, CodeAmountRequired, "required")
	}
	normalised, err := numinput.Normalise(raw)
	if err != nil {
		if typed, ok := errs.AsError(err); ok {
			return 0, typed.WithField(FieldAmount, typed.Code, "invalid")
		}
		return 0, err
	}
	if numinput.Decimals(normalised) > c.Decimals {
		return 0, errs.Validation(CodeAmountDecimals, "too many decimals for the currency").WithField(FieldAmount, CodeAmountDecimals, "decimals").
			WithParam("currency", c.Code).WithParam("decimals", strconv.Itoa(c.Decimals))
	}
	v, ok := new(big.Rat).SetString(normalised)
	if !ok {
		return 0, errs.Validation(numinput.CodeInvalid, "not a number").WithField(FieldAmount, numinput.CodeInvalid, "invalid")
	}
	v.Mul(v, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(c.Decimals)), nil)))
	if !v.IsInt() || !v.Num().IsInt64() || v.Num().Int64() > maxMinor {
		return 0, errs.Validation(CodeAmountTooLarge, "the amount is out of range").WithField(FieldAmount, CodeAmountTooLarge, "too large")
	}
	minor := v.Num().Int64()
	if minor < 0 || (minor == 0 && !zeroAllowed) {
		return 0, errs.Validation(CodeAmountRequired, "enter an amount above zero").WithField(FieldAmount, CodeAmountRequired, "required")
	}
	return minor, nil
}

func note(raw, field string) (string, error) {
	n := strings.TrimSpace(raw)
	if utf8.RuneCountInString(n) > MaxNoteRunes {
		return "", errs.Validation(CodeNoteTooLong, "too long").WithField(field, CodeNoteTooLong, "too long").WithParam("max", strconv.Itoa(MaxNoteRunes))
	}
	return n, nil
}

func knownCategory(c string) bool {
	for _, known := range Categories {
		if c == known {
			return true
		}
	}
	return false
}

// ErrEntryNotFound is the refusal for an entry that does not exist, or an id that names none.
func ErrEntryNotFound() error { return errs.NotFound(CodeEntryNotFound, "no such entry") }
