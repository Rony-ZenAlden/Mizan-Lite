package domain

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// Reason reads the required reason of an owner's act: a write-off, a refund, a reversal.
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
