// Package domain holds Mizan Lite's suppliers and what the shop owes them as values: who a supplier is, the chain of
// entries a balance is, what a purchase costs after its discounts and its damage, and the checks the book must pass.
// No I/O (0.10.0).
//
// # Why its own book, and not the customers' turned around
//
// The owner asked for suppliers "completely separate from the customer debt module" (2026-09-24), and the separation
// is right on its merits. A customer's balance is what is owed TO the shop and a supplier's what the shop owes; a report
// that added the two would net a debt against a receivable and show neither. Kept apart, each book has its own sign
// rule, its own acts and its own history, and neither can be misread as the other.
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
	CodeNameRequired      = "lite.suppliers.name_required"
	CodeNameTooLong       = "lite.suppliers.name_too_long"
	CodeDuplicateName     = "lite.suppliers.duplicate_name"
	CodePhoneInvalid      = "lite.suppliers.phone_invalid"
	CodeCityTooLong       = "lite.suppliers.city_too_long"
	CodeNoteTooLong       = "lite.suppliers.note_too_long"
	CodeNotFound          = "lite.suppliers.not_found"
	CodeInactive          = "lite.suppliers.inactive"
	CodeReasonRequired    = "lite.suppliers.reason_required"
	CodeUnknownCurrency   = "lite.suppliers.unknown_currency"
	CodeAmountRequired    = "lite.suppliers.amount_required"
	CodeAmountDecimals    = "lite.suppliers.amount_decimals"
	CodeAmountTooLarge    = "lite.suppliers.amount_too_large"
	CodeNothingOwedBack   = "lite.suppliers.nothing_owed_back"
	CodeRefundTooLarge    = "lite.suppliers.refund_too_large"
	CodeCashSourceInvalid = "lite.suppliers.cash_source_invalid"
	CodeNotReversible     = "lite.suppliers.not_reversible"
	CodeAlreadyReversed   = "lite.suppliers.already_reversed"
	CodeEntryNotFound     = "lite.suppliers.entry_not_found"
	CodeCorrupt           = "lite.suppliers.corrupt_row"
)

// codeStale is platform/database's optimistic-concurrency code, reused so a stale edit reads the same whoever noticed it.
const codeStale = "database.concurrent_modification"

// Bounds.
const (
	MaxNameRunes  = 100
	MaxPhoneRunes = 20
	MaxCityRunes  = 60
	MaxNoteRunes  = 200
)

// Fields named in validation errors.
const (
	FieldName   = "name"
	FieldPhone  = "phone"
	FieldCity   = "city"
	FieldNote   = "note"
	FieldAmount = "amount"
	FieldReason = "reason"
	FieldSource = "source"
)

// Supplier is someone the shop buys from.
type Supplier struct {
	ID         id.ID
	Name       string
	Phone      string
	City       string
	Note       string
	Active     bool
	RowVersion int64
	CreatedAt  time.Time
}

// Draft is a supplier as typed.
type Draft struct {
	Name, Phone, City, Note string
}

// NameKey is the name as compared and searched: two spellings a reader calls the same are one supplier.
func (s Supplier) NameKey() string { return textkey.Normalise(s.Name) }

// NewSupplier validates a draft into an active supplier at version 1.
func NewSupplier(supplierID id.ID, d Draft) (Supplier, error) {
	return Supplier{ID: supplierID, Active: true, RowVersion: 1}.Edit(d)
}

// Edit sets the name, phone, city and note.
func (s Supplier) Edit(d Draft) (Supplier, error) {
	name := strings.TrimSpace(d.Name)
	if name == "" || textkey.Normalise(name) == "" {
		return s, errs.Validation(CodeNameRequired, "the name is required").WithField(FieldName, CodeNameRequired, "required")
	}
	if utf8.RuneCountInString(name) > MaxNameRunes {
		return s, errs.Validation(CodeNameTooLong, "the name is too long").
			WithField(FieldName, CodeNameTooLong, "too long").WithParam("max", strconv.Itoa(MaxNameRunes))
	}
	phone := numinput.LatinDigits(d.Phone)
	if utf8.RuneCountInString(phone) > MaxPhoneRunes || !phoneShaped(phone) {
		return s, errs.Validation(CodePhoneInvalid, "the phone is not a number").
			WithField(FieldPhone, CodePhoneInvalid, "invalid").WithParam("max", strconv.Itoa(MaxPhoneRunes))
	}
	city := strings.TrimSpace(d.City)
	if utf8.RuneCountInString(city) > MaxCityRunes {
		return s, errs.Validation(CodeCityTooLong, "the city is too long").
			WithField(FieldCity, CodeCityTooLong, "too long").WithParam("max", strconv.Itoa(MaxCityRunes))
	}
	note, err := Note(d.Note)
	if err != nil {
		return s, err
	}
	s.Name, s.Phone, s.City, s.Note = name, phone, city, note
	return s, nil
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

// Reason reads the reason an owner's correction must carry.
func Reason(raw string) (string, error) {
	reason := strings.TrimSpace(raw)
	if reason == "" {
		return "", errs.Validation(CodeReasonRequired, "a reason is required").WithField(FieldReason, CodeReasonRequired, "required")
	}
	if utf8.RuneCountInString(reason) > MaxNoteRunes {
		return "", errs.Validation(CodeNoteTooLong, "the reason is too long").
			WithField(FieldReason, CodeNoteTooLong, "too long").WithParam("max", strconv.Itoa(MaxNoteRunes))
	}
	return reason, nil
}

// Note reads an optional note.
func Note(raw string) (string, error) {
	note := strings.TrimSpace(raw)
	if utf8.RuneCountInString(note) > MaxNoteRunes {
		return "", errs.Validation(CodeNoteTooLong, "the note is too long").
			WithField(FieldNote, CodeNoteTooLong, "too long").WithParam("max", strconv.Itoa(MaxNoteRunes))
	}
	return note, nil
}

// ErrNotFound is a supplier that does not exist.
func ErrNotFound() error { return errs.NotFound(CodeNotFound, "no such supplier") }

// ErrEntryNotFound reports an entry that does not exist.
func ErrEntryNotFound() error { return errs.NotFound(CodeEntryNotFound, "no such entry") }

// ErrStale is an edit of a supplier someone else changed first.
func ErrStale() error {
	return errs.Conflict(codeStale, "the supplier changed since it was read")
}
