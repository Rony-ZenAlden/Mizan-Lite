// Package domain holds the sales module's business rules.
package domain

import (
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Stable codes for numbering.
const (
	CodeInvalidSeries = "sales.invalid_series"
	CodeNoSeries      = "sales.no_series"
	CodeNumberTooLong = "sales.number_too_long"
)

// Series is a document numbering sequence.
type Series struct {
	ID           id.ID
	Code         string
	BranchID     id.ID
	FiscalYearID id.ID
	Prefix       string
	Suffix       string
	Padding      int
	// NextValue is the next number to issue, not the last one issued. One less off-by-one to get
	// wrong, and it reads correctly on a new series: "the next invoice will be 1".
	NextValue int64
	IsGapless bool
	IsActive  bool
}

// maxPadding bounds the zero-fill.
//
// Not arbitrary: 18 digits is where an int64 stops being able to hold the value being padded, so
// a wider pad could only ever produce a number the sequence cannot reach.
const maxPadding = 18

// NewSeries builds a series, or refuses.
func NewSeries(identifier id.ID, code, prefix string, padding int) (Series, error) {
	code = strings.ToUpper(strings.TrimSpace(code))

	if identifier.IsZero() {
		return Series{}, errs.Validation(CodeInvalidSeries, "a number series needs an identity")
	}
	if code == "" {
		return Series{}, errs.Validation(CodeInvalidSeries, "a number series needs a code").
			WithField("code", CodeInvalidSeries, "required")
	}
	if padding < 0 || padding > maxPadding {
		return Series{}, errs.Validation(CodeInvalidSeries,
			"that padding cannot be represented").WithParam("padding", itoa(int64(padding)))
	}

	return Series{
		ID: identifier, Code: code, Prefix: strings.TrimSpace(prefix),
		Padding: padding, NextValue: 1,
	}, nil
}

// Format renders one number.
//
// Separated from allocation deliberately: this is a pure function of a series and a value, so the
// shape of "INV-000123" can be tested against a table without a database, and a screen can show
// what the next number WILL be without consuming it.
func (s Series) Format(value int64) string {
	digits := itoa(value)
	if pad := s.Padding - len(digits); pad > 0 {
		digits = strings.Repeat("0", pad) + digits
	}
	return s.Prefix + digits + s.Suffix
}

// Next returns the number to issue and the series advanced past it.
//
// # Why this returns a new Series rather than mutating
//
// The caller must write the advanced series back inside the same transaction that writes the
// document. Returning it makes that impossible to forget: there is no path where a number is
// taken and the counter is left alone, because the counter is part of the answer.
func (s Series) Next() (string, Series, error) {
	number := s.Format(s.NextValue)
	if len(number) > 60 {
		// The column is VARCHAR(60). A number that cannot be stored must fail HERE, before a
		// document is written against it, rather than as a truncation nobody notices until two
		// invoices share a number.
		return "", s, errs.Validation(CodeNumberTooLong,
			"that number series produces numbers too long to store").
			WithParam("series", s.Code)
	}

	s.NextValue++
	return number, s, nil
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}
