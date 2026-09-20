// Package documents describes what is printed or exported — a Document of titles, pairs, tables, paragraphs, rules and
// stamps — and renders it: as A4 PDF pages, and as the 1-bit bitmap a thermal printer prints (L7 §3).
//
// A Document holds no arithmetic and knows no shop. Its figures are strings Go already formatted for the screen; the
// binding layer builds a Document from the very DTO the screen receives, so a printout and a screen cannot disagree (D-L7.6).
// Pure: no database, no clock, no operating system (lite-documents-pure).
package documents

import (
	"strings"

	"github.com/mizan-erp/mizan/internal/lite/typeset"
)

// Direction is a document's paragraph direction.
type Direction = typeset.Direction

// The directions.
const (
	RTL = typeset.RTL
	LTR = typeset.LTR
)

// Cell is a table cell: text, or a figure — a date, an amount, a quantity — isolated left to right and set against the
// cell's end edge (D-L7.3).
type Cell struct {
	Text   string
	Figure bool
	Bold   bool
	// End sets text against the cell's end edge without isolating it whole — an amount with its currency's name, whose figure
	// is isolated inside (Money).
	End bool
}

// M is an amount cell: Money's text, set where figures are.
func M(s string) Cell { return Cell{Text: s, End: true} }

// T is a text cell.
func T(s string) Cell { return Cell{Text: s} }

// F is a figure cell.
func F(s string) Cell { return Cell{Text: s, Figure: true} }

// Block is one part of a document.
type Block interface{ block() }

// Title is the document's heading and a line under it.
type Title struct {
	Text     string
	Subtitle string
	Center   bool
}

// Heading starts a section.
type Heading struct{ Text string }

// Pair is a label and its value, at opposite edges.
type Pair struct {
	Label string
	Value Cell
	Bold  bool
}

// Pairs is a run of pairs.
type Pairs struct{ Rows []Pair }

// Table is headings over rows. Widths are relative column weights; the first column starts at the paragraph's start edge.
type Table struct {
	Headings []string
	Widths   []int
	Rows     [][]Cell
	// Total is a last row set in bold, or nil.
	Total []Cell
}

// Paragraph is wrapped text.
type Paragraph struct {
	Text   string
	Small  bool
	Center bool
	Bold   bool
}

// Rule is a divider across the page.
type Rule struct{ Dashed bool }

// Space is room, in body lines.
type Space struct{ Lines float32 }

// Stamp is a word boxed across the page: نسخة, ملغاة.
type Stamp struct{ Text string }

// Signature is a labelled line to sign on.
type Signature struct{ Label string }

// Barcode is a Code 128 symbol with its digits printed under it — a shelf label's whole point (2026-09-20).
//
// The renderer draws Bars, not Text: encoding happens once, in barcode.go, so the PDF and the thermal bitmap cannot
// encode the same code two different ways. Text is what is printed underneath for a person to read.
type Barcode struct {
	// Bars are alternating bar and space widths in modules, starting with a bar.
	Bars []int
	// Text is the human-readable line under the symbol; "" prints none.
	Text string
	// HeightLines is how tall the symbol is, in body lines.
	HeightLines float32
}

func (Title) block()     {}
func (Heading) block()   {}
func (Pairs) block()     {}
func (Table) block()     {}
func (Paragraph) block() {}
func (Rule) block()      {}
func (Space) block()     {}
func (Stamp) block()     {}
func (Signature) block() {}
func (Barcode) block()   {}

// Document is what is rendered.
type Document struct {
	Direction Direction
	Blocks    []Block
	// Running is the small line at the top of every PDF page after the first (the shop and the report); Footer is set on
	// every page with {page} and {pages} replaced — both as figures.
	Running string
	Footer  string
	// Meta is the PDF's document information.
	Meta Meta
}

// Meta is a PDF's title, author and creation date (D:YYYYMMDDHHmmSS, given by the caller: this package has no clock).
type Meta struct {
	Title   string
	Author  string
	Created string
}

// Group puts thousands separators in a decimal Go formatted — "1710500" → "1,710,500", "-1234.50" → "-1,234.50" — as the
// screen's formatDecimal does. Text that is not a plain decimal is returned as it is.
//
// A shop reading both its pounds at once sends one figure with the other after it in brackets — "150 (15000)", the shape
// moneyfmt.Display gives a dual reading. Each half is grouped on its own so a receipt reads "150 (15,000)" rather than
// leaving the old figure, which is the long one, as an undivided run of digits. This package cannot import moneyfmt
// (lite-documents-pure), so it knows the shape rather than the package; a test in moneyfmt holds the two together.
func Group(s string) string {
	if fresh, legacy, ok := splitDual(s); ok {
		return Group(fresh) + " (" + Group(legacy) + ")"
	}
	neg := strings.HasPrefix(s, "-")
	digits := strings.TrimPrefix(s, "-")
	whole, frac, hasFrac := strings.Cut(digits, ".")
	if whole == "" {
		return s
	}
	for _, r := range whole + frac {
		if r < '0' || r > '9' {
			return s
		}
	}
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	out := b.String()
	if hasFrac {
		out += "." + frac
	}
	if neg {
		out = "-" + out
	}
	return out
}

// splitDual takes "150 (15000)" apart. ok is false for anything else, including a single figure.
func splitDual(s string) (fresh, legacy string, ok bool) {
	if !strings.HasSuffix(s, ")") {
		return s, "", false
	}
	cut := strings.LastIndex(s, " (")
	if cut <= 0 {
		return s, "", false
	}
	return s[:cut], s[cut+2 : len(s)-1], true
}

// Money is an amount with its currency's short name, the figure isolated and grouped: "76,000 ل.س", "13.00 USD".
func Money(value, currencyShort string) string {
	return typeset.Isolate(Group(value)) + " " + currencyShort
}

// Unisolated lists the texts in a document that carry a digit outside a figure — a figure an Arabic line would print
// backwards (L7 H3). A test holds every document the application builds to an empty list (D-L7.3).
func Unisolated(doc Document) []string {
	var out []string
	check := func(s string, figure bool) {
		if figure || s == "" {
			return
		}
		depth := 0
		for _, r := range s {
			switch {
			case r == '⁦' || r == '⁧' || r == '⁨':
				depth++
			case r == '⁩' && depth > 0:
				depth--
			case r >= '0' && r <= '9' && depth == 0:
				out = append(out, s)
				return
			}
		}
	}
	for _, b := range doc.Blocks {
		switch b := b.(type) {
		case Title:
			check(b.Text, false)
			check(b.Subtitle, false)
		case Heading:
			check(b.Text, false)
		case Paragraph:
			check(b.Text, false)
		case Stamp:
			check(b.Text, false)
		case Signature:
			check(b.Label, false)
		case Barcode:
			// The digits under a symbol ARE a figure: they are drawn left to right by the layout, whatever the
			// paragraph direction, because a barcode reads one way in every language.
			check(b.Text, true)
		case Pairs:
			for _, p := range b.Rows {
				check(p.Label, false)
				check(p.Value.Text, p.Value.Figure)
			}
		case Table:
			for _, h := range b.Headings {
				check(h, false)
			}
			for _, r := range append(b.Rows, b.Total) {
				for _, c := range r {
					check(c.Text, c.Figure)
				}
			}
		}
	}
	check(doc.Running, false)
	return out
}
