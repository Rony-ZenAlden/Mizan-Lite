// Package moneyfmt is the one place a money figure changes shape between what the books hold and what a person reads
// (L10, the owner's request of 2026-09-17).
//
// # Why this exists
//
// Syria dropped two noughts from the pound. A shop may want to read the new figure, the old one, or both — and whichever
// it picks must be true on the till, on the shelf label, on the receipt, in the reports and in the workbooks, all at once.
// Before this package the application formatted money in eight different places and parsed it in twenty-one, and the way
// to add redenomination was to edit ninety of them and hope. One missed site shows a shop 13,675 where it means 136.75,
// which is not a cosmetic fault: it is a wrong price.
//
// So every figure passes through here. Display() on the way out, Base() on the way in, and a gate test that fails when a
// figure carrying local money reaches the boundary without doing so.
//
// # Why decimal text, not minor units
//
// Every formatter in this edition already produces exact decimal text — "15000", "4.88" — because DESIGN D9 keeps money a
// string across the boundary. Dividing that by a hundred is moving the point two places, which is exact, needs no
// rounding rule, and cannot lose a unit. Converting back to integers first and dividing would introduce a rounding
// question that the shop's books never asked.
//
// # What is never redenominated
//
// Dollars. The redenomination is the pound's; a price of $3.25 is $3.25 whichever way the shop reads its pounds. Only the
// local currency is shifted, which is why every entry point takes the local currency's code.
package moneyfmt

import (
	"strings"

	"github.com/mizan-erp/mizan/internal/lite/numinput"
)

// Display is how a shop reads its own currency.
type Display string

const (
	// Legacy is the old pound, undivided: 15,000.
	Legacy Display = "legacy"
	// New is the redenominated pound: 150. What a person types is read the same way.
	New Display = "new"
	// Dual shows the new figure with the old one beside it: 150 (15,000).
	Dual Display = "dual"
)

// Default is what a shop that has never chosen reads: the pounds it has always used.
const Default = Legacy

// Noughts is how many the redenomination drops.
const Noughts = 2

// Parse reads a stored display setting, falling back to the old pound rather than guessing.
func Parse(raw string) Display {
	switch Display(strings.ToLower(strings.TrimSpace(raw))) {
	case New:
		return New
	case Dual:
		return Dual
	default:
		return Default
	}
}

// Valid reports whether a display was one this application knows — for the setting's validation, which must refuse an
// unknown value rather than silently showing the shop the old pound.
func Valid(raw string) bool {
	switch Display(strings.ToLower(strings.TrimSpace(raw))) {
	case Legacy, New, Dual:
		return true
	default:
		return false
	}
}

// Shop is the two facts every conversion needs: how the shop reads its money, and which currency is the local one.
type Shop struct {
	Mode Display
	// Local is the code the redenomination applies to. Everything else passes through untouched.
	Local string
}

// Display converts a base figure — the pounds the books hold — into what a person reads.
//
// The result is decimal text, never localised: grouping and digit shaping belong to whoever draws it, here or in the
// webview. Dual returns the new figure with the old one after it in brackets — "150 (15000)" — which is both the
// rendering a person should read and a shape a caller can take apart again with SplitDual.
func (s Shop) Display(text, currency string) string {
	if !s.applies(currency) || text == "" {
		return text
	}
	switch s.Mode {
	case New:
		return shift(text, -Noughts)
	case Dual:
		return shift(text, -Noughts) + DualOpen + text + DualClose
	default:
		return text
	}
}

// DualOpen and DualClose bracket the old figure that follows the new one in a dual reading.
//
// # Why brackets and not a control character
//
// The first cut of this package divided the two figures with an ASCII unit separator, on the reasoning that it could
// never be part of a figure and so could never be mistaken for one. That was true and useless: every place that printed
// the string without knowing to split it drew a control character, which a webview and a printer both render as a
// broken box — "1 USD = 137 ☒ 13700 SYP" (the owner's report, 2026-09-17). A separator that must be understood to be
// readable will be misunderstood somewhere, because there are more places that draw money than places that know about
// this package.
//
// Brackets need no understanding. A site that knows nothing draws "137 (13700)", which is what a person should read
// anyway; a site that wants the two apart still has SplitDual. The shape is unambiguous because a figure produced by
// this application is digits, at most one point and at most one leading minus — never a bracket.
const (
	DualOpen  = " ("
	DualClose = ")"
)

// SplitDual takes a dual reading apart into the new figure and the old one. ok is false for anything else, including a
// single figure, which the caller should draw as it stands.
func SplitDual(text string) (fresh, legacy string, ok bool) {
	if !strings.HasSuffix(text, DualClose) {
		return text, "", false
	}
	cut := strings.LastIndex(text, DualOpen)
	if cut <= 0 {
		return text, "", false
	}
	return text[:cut], text[cut+len(DualOpen) : len(text)-len(DualClose)], true
}

// Base converts what a person typed into the figure the books hold.
//
// In dual mode a person types the NEW figure: the old one is shown for recognition, not for typing, and a shop asked to
// type whichever it liked would have no way to say which it meant.
//
// The figure is read the way every typed number is read (numinput.Normalise) before its point is moved: Arabic-Indic
// digits and the Arabic decimal separator are a figure like any other. Until 0.9.9 the point was moved in the text AS
// TYPED, which knows only '.', so "١٫٥" new pounds became 1.5 old pounds instead of 150. Text Normalise refuses is
// handed on untouched, for the parser it is going to to refuse with its reason — "no thousands separators", say.
func (s Shop) Base(typed, currency string) string {
	if !s.applies(currency) || typed == "" {
		return typed
	}
	switch s.Mode {
	case New, Dual:
		sign, body := "", strings.TrimSpace(typed)
		if strings.HasPrefix(body, "-") {
			sign, body = "-", body[1:]
		}
		figure, err := numinput.Normalise(body)
		if err != nil {
			return typed
		}
		return shift(sign+figure, Noughts)
	default:
		return typed
	}
}

// applies reports whether the redenomination touches this currency at all.
func (s Shop) applies(currency string) bool {
	return s.Mode != Legacy && s.Local != "" && strings.EqualFold(currency, s.Local)
}

// shift moves a decimal point by places — negative left (dividing), positive right (multiplying). Exact: it is text
// arithmetic, so no figure is rounded and none can overflow.
func shift(text string, places int) string {
	sign := ""
	body := strings.TrimSpace(text)
	if strings.HasPrefix(body, "-") {
		sign, body = "-", body[1:]
	}
	whole, fraction, _ := strings.Cut(body, ".")
	digits := whole + fraction
	point := len(whole) + places // where the point lands, counted from the left of digits

	switch {
	case point <= 0:
		digits = strings.Repeat("0", 1-point) + digits
		point = 1
	case point > len(digits):
		digits += strings.Repeat("0", point-len(digits))
	}
	whole, fraction = digits[:point], digits[point:]

	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	fraction = strings.TrimRight(fraction, "0")
	if fraction == "" {
		return sign + whole
	}
	return sign + whole + "." + fraction
}
