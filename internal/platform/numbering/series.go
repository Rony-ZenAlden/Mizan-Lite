// Package numbering allocates document numbers from shared series (§9.4).
//
// # Why this is platform and not a module
//
// `migrations/sqlite/0001_platform.sql` puts `number_series` at platform level, commented
// "used by every transactional module". Sales 0023 wrote the reason down when it declined to
// create a second one:
//
//	"purchasing (Phase 6) and payments will number documents too, and a series table owned by
//	 sales would make them either import sales or build a second one."
//
// The ALLOCATOR has exactly the same property, and Phase 6 is where that became concrete: a
// purchase order needs a number, purchasing cannot import sales (`module-isolation`), and two
// allocators against one table would each be correct alone and race together. So the allocator
// followed its table here.
//
// # The guarantee
//
// §9.4: unique and SEQUENTIAL, not gapless. A number is taken inside the caller's transaction,
// so a failure anywhere in that transaction gives the number back. What this does not promise is
// that no number is ever skipped — a gapless sequence requires serialising every document behind
// one lock, which is a correctness claim traded for a throughput disaster.
package numbering

import (
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Stable codes for numbering.
const (
	CodeInvalidSeries = "numbering.invalid_series"
	CodeNoSeries      = "numbering.no_series"
	CodeNumberTooLong = "numbering.number_too_long"
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

// defaultPadding matches the column default in 0001_platform.sql. Named here so a declaration
// that omits a width and the schema that stores it cannot drift apart.
const defaultPadding = 6

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

// SeriesSpec is a series a module DECLARES, as opposed to one an administrator created.
//
// # Why this type exists at all
//
// The Phase 7 Definition-of-Done review went looking for the caller that creates the series a
// document is numbered from, and found none. Every module named its series in a constant, every
// module's TEST fixture created it, and nothing in the composition root ever did — so a real
// company, freshly provisioned, could not post an invoice, a purchase order, or an expense. The
// allocator was correct; nobody had ever asked it for a series it had not been handed.
//
// A fixture that creates production data is a fixture standing in for a mechanism, which is
// 5.4's rule: when a test needs a helper that imitates a production mechanism, that mechanism is
// untested. Here it was worse than untested — it did not exist.
//
// The shape follows the one the same problem already has. Permissions are code-defined, declared
// per module, and reconciled into the table at every startup (§14.1), which is what stops the
// permission list drifting from what the code actually checks. A series has exactly that
// property, so it gets exactly that treatment rather than a second mechanism to learn.
type SeriesSpec struct {
	// Code is the series a module allocates from, e.g. "EXPENSE".
	Code string
	// Prefix is what the number is rendered with, e.g. "EXP-".
	Prefix string
	// Padding zero-fills the counter. Zero means "declare the default", which Ensure reads as 6:
	// a spec that says nothing about width should get the width the table already defaults to,
	// not numbers one digit wide.
	Padding int
}
