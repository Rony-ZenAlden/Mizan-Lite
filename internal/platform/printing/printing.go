// Package printing turns a template and a bag of values into a printable document.
//
// # What this package must never know
//
// It does not know what an invoice is. It knows blocks, fields, rows, and alignment. The sales
// module supplies the vocabulary — `number`, `customer`, `total` — and this package resolves
// placeholders against it without learning a single one (`platform-independent-of-modules`).
//
// That is what makes a print template REFERENCE DATA rather than code (Phase 0's table): adding
// a receipt layout for a new country is dropping in a JSON file, with no release.
//
// # One model, two realisations
//
// §33.1 open question 3 asked whether receipts target thermal ESC/POS, A4/A5 documents, or both.
// The answer is BOTH, and the way to have both without two products is to render each template
// to one intermediate — a Document of resolved blocks — and let the DEVICE decide how to realise
// it. A receipt and an A4 invoice of the same sale then cannot disagree, because disagreeing
// would require two templates to drift, not two renderers.
//
//	template + data ──▶ Document ──┬──▶ HTML  (browser print: A4, A5, 80mm, 58mm)
//	                               └──▶ ESC/POS bytes (straight to a till printer)
//
// # Why HTML is the primary path
//
// The browser is the only thing in this application's reach that lays out Arabic correctly:
// bidirectional text, contextual letter shaping, and line breaking are decades of work that a
// Go template cannot approximate. Wails ships a browser. Using it for A4 and for thermal-width
// receipts alike costs nothing and gets RTL right for free.
//
// ESC/POS is the fast path, not the fallback: it prints in milliseconds with no driver and no
// print dialogue, which is what a queue at a till needs. Its limits are honest and documented on
// the renderer.
package printing

import (
	"sort"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/round"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// Stable error codes, doubling as i18n keys.
const (
	CodeUnknownBlock = "printing.unknown_block"
	CodeNoTemplate   = "printing.no_template"
	CodeBadTemplate  = "printing.bad_template"
	CodeUnknownField = "printing.unknown_field"
	CodeUnsupported  = "printing.unsupported_script"
)

// Kind is what a block is.
type Kind string

// The block kinds. Deliberately few: a template language grows until it is a programming
// language, and then it needs a debugger, a test framework, and somebody to maintain it. Six
// kinds cover every receipt and invoice this product has been asked for, and a seventh is a
// decision to make deliberately rather than a habit to fall into.
const (
	// Text is a literal or a placeholder line: "Thank you", or "{number}".
	Text Kind = "text"
	// Pair is a label and a value on one line, pushed to opposite edges. The workhorse of a
	// receipt: TOTAL ............ 34.50
	Pair Kind = "pair"
	// Rows is the line-item table.
	Rows Kind = "rows"
	// Rule is a horizontal divider.
	Rule Kind = "rule"
	// Space is vertical breathing room, in lines.
	Space Kind = "space"
	// Barcode is a barcode or QR code — the invoice number a warehouse scans, or the tax QR a
	// revenue authority requires.
	//
	// Named Barcode rather than Code because every `Code*` identifier in this codebase is an
	// ERROR CODE, and the i18n coverage gate reads them as such: as `Code` this constant was
	// reported as an untranslated error. Fourth name collision in the project, resolved the way
	// the other three were — the newcomer moves.
	Barcode Kind = "barcode"
)

// Align is horizontal placement WITHIN the writing direction, not on the page.
//
// Start and End rather than left and right, and the distinction is the whole point: on an Arabic
// receipt "start" is the right-hand edge. A template that said "left" would have to be rewritten
// per language, which is exactly the per-locale forking §22 exists to prevent.
type Align string

// The alignments.
const (
	Start  Align = "start"
	Center Align = "center"
	End    Align = "end"
)

// Block is one element of a template.
type Block struct {
	Kind  Kind  `json:"kind"`
	Align Align `json:"align,omitempty"`
	Bold  bool  `json:"bold,omitempty"`
	Large bool  `json:"large,omitempty"`

	// Text is a literal with {placeholders}. On a Pair it is the label.
	Text string `json:"text,omitempty"`
	// Value is the right-hand side of a Pair.
	Value string `json:"value,omitempty"`
	// Lines is how many blank lines a Space is worth.
	Lines int `json:"lines,omitempty"`
	// Symbology is "barcode" or "qr" for a Barcode block.
	Symbology string `json:"symbology,omitempty"`

	// Columns describes a Rows block.
	Columns []Column `json:"columns,omitempty"`

	// OmitWhenEmpty drops the block when it has nothing to say.
	//
	// This is what keeps ONE template usable by a shop that collects tax numbers and one that
	// does not — the alternative is a template per configuration, which is the per-country
	// forking this product exists to avoid.
	OmitWhenEmpty bool `json:"omitWhenEmpty,omitempty"`
}

// Column is one column of a Rows block.
type Column struct {
	// Field names a key on each row.
	Field string `json:"field"`
	// Header is a literal or placeholder for the heading.
	Header string `json:"header,omitempty"`
	Align  Align  `json:"align,omitempty"`
	// Width is a share of the available width, not a character count. A receipt is 32 columns
	// and an A4 invoice is 90; a template written in characters fits exactly one of them.
	Width int `json:"width,omitempty"`
}

// Template is a document layout, loaded from a seed file.
type Template struct {
	Code string `json:"code"`
	// Media is what this layout was designed for: "receipt" or "document". It selects the
	// stylesheet, not the renderer — both media render through both renderers.
	Media  string  `json:"media"`
	Blocks []Block `json:"blocks"`
}

// Data is everything a template may refer to.
//
// Flat strings, deliberately. Every value has already been formatted by the caller — money into
// minor-unit text, quantities out of micro units, dates into the locale's calendar — because
// those are decisions with rules, and a template that could make them would be a place for the
// rules to be made differently.
type Data struct {
	Fields map[string]string
	Rows   []map[string]string
	// Direction is "rtl" or "ltr". It decides what Start means.
	Direction string
	// Digits selects the numeral system: "" or "western" for 0-9, "arabic" for ٠-٩. §22.5
	// records that this varies by COUNTRY AND BY CUSTOMER, so it cannot be inferred from the
	// language — an Arabic invoice from a Gulf exporter usually carries Western digits.
	Digits string
}

// Document is a template resolved against data: what will actually be printed.
//
// The renderers consume this and nothing else. That is what makes a golden test meaningful — a
// Document is comparable, and two renderers of the same Document cannot disagree about content.
type Document struct {
	Media     string
	Direction string
	Blocks    []Resolved
}

// Resolved is a block with every placeholder replaced.
type Resolved struct {
	Kind      Kind
	Align     Align
	Bold      bool
	Large     bool
	Text      string
	Value     string
	Lines     int
	Symbology string
	Headers   []Cell
	Rows      [][]Cell
}

// Cell is one rendered table cell.
type Cell struct {
	Text  string
	Align Align
	Width int
}

// Render resolves a template against data.
//
// # Missing fields are an ERROR, not an empty string
//
// A placeholder nobody supplied is a template referring to something that does not exist, and
// the cost of guessing is a receipt printed with a blank where the total should be — handed to a
// customer, with the till drawer already open. Refusing is recoverable; a blank is not noticed
// until somebody complains.
//
// `OmitWhenEmpty` is how a template says a field is genuinely optional. That is a DECLARATION,
// which is different from an omission.
func Render(template Template, data Data) (Document, error) {
	if len(template.Blocks) == 0 {
		return Document{}, errs.Validation(CodeNoTemplate,
			"that template has no blocks").WithParam("template", template.Code)
	}

	document := Document{
		Media:     template.Media,
		Direction: direction(data.Direction),
		Blocks:    make([]Resolved, 0, len(template.Blocks)),
	}

	for _, block := range template.Blocks {
		resolved, used, err := resolveBlock(block, data)
		if err != nil {
			return Document{}, err
		}
		// A block that declared itself optional and found nothing is dropped, along with the
		// blank line it would have left behind.
		if block.OmitWhenEmpty && !used {
			continue
		}
		document.Blocks = append(document.Blocks, resolved)
	}
	return document, nil
}

func resolveBlock(block Block, data Data) (Resolved, bool, error) {
	out := Resolved{
		Kind: block.Kind, Align: align(block.Align), Bold: block.Bold, Large: block.Large,
		Lines: block.Lines, Symbology: block.Symbology,
	}

	// Whether this block has anything to say, which is what OmitWhenEmpty acts on. Assigned in
	// every branch below rather than defaulted, so a new block kind cannot quietly inherit
	// "empty" and vanish from every template that marks it optional.
	var anyValue bool

	switch block.Kind {
	case Text, Pair, Barcode:
		text, textFound, textErr := expand(block.Text, data)
		if textErr != nil {
			return Resolved{}, false, textErr
		}
		value, valueFound, valueErr := expand(block.Value, data)
		if valueErr != nil {
			return Resolved{}, false, valueErr
		}
		out.Text, out.Value = text, value

		// # Emptiness is judged on the VALUE, never on the label
		//
		// A label is furniture: "Outstanding" is always present, because it is a word this
		// application knows in every language it speaks. Counting it as content means a block
		// with a label can never be empty — and `omitWhenEmpty` on every optional row silently
		// stops working, which is how a receipt for a fully settled sale comes to read
		// "Outstanding 0.00" and look like a debt.
		//
		// So a block that HAS a value is judged on that value alone. A block with only text —
		// a footer, an address line — is judged on its text, because there is nothing else.
		if block.Value != "" {
			anyValue = valueFound
		} else {
			anyValue = textFound
		}
	case Rule, Space:
		// Nothing to resolve. A rule is a rule.
		anyValue = true
	case Rows:
		out.Headers = make([]Cell, 0, len(block.Columns))
		for _, column := range block.Columns {
			header, _, headerErr := expand(column.Header, data)
			if headerErr != nil {
				return Resolved{}, false, headerErr
			}
			out.Headers = append(out.Headers,
				Cell{Text: header, Align: align(column.Align), Width: column.Width})
		}
		out.Rows = make([][]Cell, 0, len(data.Rows))
		for _, row := range data.Rows {
			cells := make([]Cell, 0, len(block.Columns))
			for _, column := range block.Columns {
				cells = append(cells, Cell{
					Text:  shape(row[column.Field], data.Digits),
					Align: align(column.Align), Width: column.Width,
				})
			}
			out.Rows = append(out.Rows, cells)
		}
		// Judged on the ROWS, for the same reason a Pair is judged on its value: the headers
		// are furniture and are always there. A table with headings and no rows is an empty
		// table, and printing one is printing a promise nothing kept.
		anyValue = len(out.Rows) > 0
	default:
		return Resolved{}, false, errs.Validation(CodeUnknownBlock,
			"that is not a kind of block this can print").WithParam("kind", string(block.Kind))
	}
	return out, anyValue, nil
}

// expand replaces every {placeholder} in a pattern.
//
// Returns whether any placeholder resolved to a NON-EMPTY value, which is what OmitWhenEmpty
// turns on. A literal with no placeholders counts as present: "Thank you" is not conditional.
func expand(pattern string, data Data) (string, bool, error) {
	if pattern == "" {
		return "", false, nil
	}

	var out strings.Builder
	found := !strings.Contains(pattern, "{")
	rest := pattern

	for {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			out.WriteString(rest)
			break
		}
		shut := strings.IndexByte(rest[open:], '}')
		if shut < 0 {
			// An unclosed brace is a typo in a file an administrator edited, and printing it
			// literally would put "{total" on a customer's receipt.
			return "", false, errs.Validation(CodeBadTemplate,
				"a placeholder was opened and never closed").WithParam("text", pattern)
		}
		shut += open

		out.WriteString(rest[:open])
		name := rest[open+1 : shut]
		value, ok := data.Fields[name]
		if !ok {
			return "", false, errs.Validation(CodeUnknownField,
				"this template refers to something the document does not have").
				WithParam("field", name)
		}
		if value != "" {
			found = true
		}
		out.WriteString(shape(value, data.Digits))
		rest = rest[shut+1:]
	}
	return out.String(), found, nil
}

// arabicIndic maps Western digits to their Arabic-Indic counterparts.
var arabicIndic = [...]rune{'٠', '١', '٢', '٣', '٤', '٥', '٦', '٧', '٨', '٩'}

// shape converts numerals to the configured system.
//
// Only the DIGITS are converted. Separators are left alone: a value that has already been
// formatted carries its locale's grouping and decimal marks, and re-deciding them here would
// overrule the formatter that had the currency in front of it.
func shape(value, digits string) string {
	if digits != "arabic" || value == "" {
		return value
	}
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return arabicIndic[r-'0']
		}
		return r
	}, value)
}

func align(value Align) Align {
	switch value {
	case Center, End:
		return value
	default:
		return Start
	}
}

func direction(value string) string {
	if value == "rtl" {
		return "rtl"
	}
	return "ltr"
}

// Widths distributes `total` across a Rows block's columns, summing to EXACTLY total.
//
// Templates are written by people, and people write widths that sum to 97. Normalising is
// kinder than refusing and more honest than ignoring: a column given twice the share of another
// keeps roughly twice the share whatever the total was.
//
// # Why exactly total, and not "about total"
//
// Plain integer division leaves the shares one short, and the ESC/POS renderer turns shares into
// CHARACTER COUNTS — so a 48-column receipt lays its table out across 47 and every amount sits
// one character short of the edge, on every receipt, forever. The leftover is handed to the
// largest fractional remainder, which is the same rule money allocation uses and for the same
// reason.
//
// # `total` is a parameter, and that is what keeps the two renderers agreeing
//
// HTML wants percentages and ESC/POS wants character counts. Allocating to 100 and then dividing
// again into columns rounds TWICE, and the second rounding undoes the first — which is exactly
// the defect above, reappearing one level down. One allocation against the number the caller
// actually needs has no second rounding to get wrong.
//
// # The allocator moved to the kernel in 6.5
//
// This function used to carry its own largest-remainder pass, with a comment explaining that
// neither existing implementation fitted: `money.Money.Allocate` needs a currency, and
// inventory's lives in a module platform must not import.
//
// Phase 6 needed a FOURTH copy for freight, at which point the right answer stopped being "find
// the one that fits" and became "stop making copies". `round.Allocate` is in the kernel, which
// every layer can reach, and this now delegates to it.
func Widths(headers []Cell, total int) []int {
	shares := make([]int, len(headers))
	if len(headers) == 0 || total <= 0 {
		return shares
	}

	declared := 0
	for _, cell := range headers {
		if cell.Width > 0 {
			declared += cell.Width
		}
	}
	if declared == 0 {
		// No widths declared at all: share evenly rather than collapse to zero, which would
		// swallow every column's contents.
		base, extra := total/len(headers), total%len(headers)
		for i := range shares {
			shares[i] = base
			if i < extra {
				shares[i]++
			}
		}
		return shares
	}

	weights := make([]int64, len(headers))
	for i, cell := range headers {
		weights[i] = int64(max(cell.Width, 0))
	}
	parts, err := round.Allocate(int64(total), weights)
	if err != nil {
		// Only reachable with no columns or a negative width, both handled above. Falling back
		// to an even spread is better than returning zeros, which would swallow every column.
		for i := range shares {
			shares[i] = total / len(shares)
		}
		return shares
	}
	for i, part := range parts {
		shares[i] = int(part)
	}
	return shares
}

// TemplateCodes lists a set of templates in a stable order, for tests and for a picker.
func TemplateCodes(templates []Template) []string {
	codes := make([]string, 0, len(templates))
	for _, template := range templates {
		codes = append(codes, template.Code)
	}
	sort.Strings(codes)
	return codes
}
