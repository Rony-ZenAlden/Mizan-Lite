// Package domain holds Mizan Lite's customers and their debt book as values: who a customer is, the chain of entries a
// balance is, what a repayment settles, and the checks the book must pass. No I/O (L5).
package domain

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
	"github.com/mizan-erp/mizan/internal/lite/textkey"
)

// Stable error codes. They double as i18n keys.
const (
	CodeNameRequired     = "lite.customers.name_required"
	CodeNameTooLong      = "lite.customers.name_too_long"
	CodeDuplicateName    = "lite.customers.duplicate_name"
	CodePhoneInvalid     = "lite.customers.phone_invalid"
	CodeNoteTooLong      = "lite.customers.note_too_long"
	CodeNotFound         = "lite.customers.not_found"
	CodeInactive         = "lite.customers.inactive"
	CodeUnknownCurrency  = "lite.customers.unknown_currency"
	CodeAmountRequired   = "lite.customers.amount_required"
	CodeAmountDecimals   = "lite.customers.amount_decimals"
	CodeAmountTooLarge   = "lite.customers.amount_too_large"
	CodeReasonRequired   = "lite.customers.reason_required"
	CodeNothingOwed      = "lite.customers.nothing_owed"
	CodeWriteOffTooLarge = "lite.customers.write_off_too_large"
	CodeNothingOwedBack  = "lite.customers.nothing_owed_back"
	CodeRefundTooLarge   = "lite.customers.refund_too_large"
	CodeBelowNote        = "lite.customers.below_note"
	CodeChangeCurrency   = "lite.customers.change_currency"
	CodeNotReversible    = "lite.customers.not_reversible"
	CodeAlreadyReversed  = "lite.customers.already_reversed"
	CodeEntryNotFound    = "lite.customers.entry_not_found"
	CodePaymentStale     = "lite.customers.payment_stale"
	CodeNoRate           = "lite.customers.no_rate"
	CodeChargeNotFound   = "lite.customers.charge_not_found"
)

// codeStale is platform/database's optimistic-concurrency code, reused so a stale edit reads the same whoever noticed it.
const codeStale = "database.concurrent_modification"

// Bounds.
const (
	MaxNameRunes  = 100
	MaxPhoneRunes = 20
	MaxNoteRunes  = 200
)

// Fields named in validation errors.
const (
	FieldName           = "name"
	FieldPhone          = "phone"
	FieldNote           = "note"
	FieldAmount         = "amount"
	FieldTendered       = "tendered"
	FieldChangeCurrency = "changeCurrency"
	FieldReason         = "reason"
)

// Customer is someone the shop sells to on credit (L5 §3).
type Customer struct {
	ID         id.ID
	Name       string
	Phone      string
	Note       string
	Active     bool
	RowVersion int64
	CreatedAt  time.Time
}

// Draft is a customer as typed.
type Draft struct {
	Name  string
	Phone string
	Note  string
}

// NameKey is the name as compared and searched.
func (c Customer) NameKey() string { return textkey.Normalise(c.Name) }

// NewCustomer validates a draft into an active customer at version 1.
func NewCustomer(customerID id.ID, d Draft) (Customer, error) {
	return Customer{ID: customerID, Active: true, RowVersion: 1}.Edit(d)
}

// Edit sets the name, phone and note.
func (c Customer) Edit(d Draft) (Customer, error) {
	name := strings.TrimSpace(d.Name)
	// A name made only of marks has an empty key, and could never be found or told apart: it is as missing as none.
	if name == "" || textkey.Normalise(name) == "" {
		return c, errs.Validation(CodeNameRequired, "the name is required").WithField(FieldName, CodeNameRequired, "required")
	}
	if utf8.RuneCountInString(name) > MaxNameRunes {
		return c, errs.Validation(CodeNameTooLong, "the name is too long").
			WithField(FieldName, CodeNameTooLong, "too long").WithParam("max", strconv.Itoa(MaxNameRunes))
	}
	phone := numinput.LatinDigits(d.Phone)
	if utf8.RuneCountInString(phone) > MaxPhoneRunes || !phoneShaped(phone) {
		return c, errs.Validation(CodePhoneInvalid, "the phone is not a number").
			WithField(FieldPhone, CodePhoneInvalid, "invalid").WithParam("max", strconv.Itoa(MaxPhoneRunes))
	}
	note := strings.TrimSpace(d.Note)
	if utf8.RuneCountInString(note) > MaxNoteRunes {
		return c, errs.Validation(CodeNoteTooLong, "the note is too long").
			WithField(FieldNote, CodeNoteTooLong, "too long").WithParam("max", strconv.Itoa(MaxNoteRunes))
	}
	c.Name, c.Phone, c.Note = name, phone, note
	return c, nil
}

// phoneShaped accepts digits with the separators people type: spaces, +, -, parentheses.
func phoneShaped(phone string) bool {
	for _, r := range phone {
		if !(r >= '0' && r <= '9' || strings.ContainsRune(" +-()", r)) {
			return false
		}
	}
	return true
}

// ErrNotFound is the refusal for a customer that does not exist.
func ErrNotFound() error { return errs.NotFound(CodeNotFound, "no such customer") }

// ErrStale is the refusal for an edit to a customer that changed since it was read.
func ErrStale() error { return errs.Conflict(codeStale, "the customer changed since it was read") }

// DuplicateName refuses a name another customer already has, naming them.
func DuplicateName(existing Customer) error {
	return errs.Conflict(CodeDuplicateName, "a customer with this name exists").
		WithField(FieldName, CodeDuplicateName, "duplicate").WithParam("existingName", existing.Name)
}
