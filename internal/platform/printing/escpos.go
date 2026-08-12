package printing

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// The ESC/POS control sequences this renderer uses.
//
// Named rather than inlined, because 0x1B 0x61 0x01 is unreadable and a receipt printer gives no
// diagnostics: a wrong byte produces gibberish on paper with nothing on screen to explain it.
var (
	escInitialise = []byte{0x1B, 0x40}       // ESC @  — reset to a known state
	escAlignStart = []byte{0x1B, 0x61, 0x00} // ESC a 0
	escAlignMid   = []byte{0x1B, 0x61, 0x01} // ESC a 1
	escAlignEnd   = []byte{0x1B, 0x61, 0x02} // ESC a 2
	escBoldOn     = []byte{0x1B, 0x45, 0x01} // ESC E 1
	escBoldOff    = []byte{0x1B, 0x45, 0x00} // ESC E 0
	escLargeOn    = []byte{0x1D, 0x21, 0x11} // GS ! — double width AND height
	escLargeOff   = []byte{0x1D, 0x21, 0x00}
	escCut        = []byte{0x1D, 0x56, 0x42, 0x00} // GS V B — feed and partial cut
)

// ESCPOSOptions describes the physical printer.
type ESCPOSOptions struct {
	// Columns is the character width of the paper: 32 for 58mm, 48 for 80mm at font A.
	//
	// A count of CHARACTERS, not millimetres, because that is what the printer counts. Getting
	// it wrong wraps every line and produces a receipt twice as long as it should be.
	Columns int
	// Cut asks the printer to cut the paper at the end. Off for printers with no cutter, where
	// the command prints as garbage characters.
	Cut bool
}

// ESCPOS renders a document as bytes for a thermal printer.
//
// # What this is for, and what it is not for
//
// It is the FAST path: no driver, no print dialogue, no page-setup — bytes down a wire and paper
// out in milliseconds. That is what a queue at a till needs, and it is why this exists alongside
// the HTML renderer rather than being replaced by it.
//
// # It refuses non-Latin text, and that refusal is the design
//
// ESC/POS text mode selects a single-byte CODE PAGE. Arabic in that mode means CP864, which
// almost no printer in circulation implements, and which cannot express contextual letter
// shaping in any case — the correct output for Arabic is a RASTER IMAGE of text the browser
// laid out.
//
// So this renderer refuses rather than printing a line of question marks. A receipt of mojibake
// is worse than no receipt: it looks like a printer fault, so somebody reprints it, and the
// second one is identical. Refusing names the cause, and the caller falls back to HTML — which
// prints Arabic correctly, just more slowly.
func ESCPOS(document Document, options ESCPOSOptions) ([]byte, error) {
	columns := options.Columns
	if columns <= 0 {
		columns = 48 // 80mm at font A, the common till printer.
	}

	if err := requireASCII(document); err != nil {
		return nil, err
	}

	var out bytes.Buffer
	out.Write(escInitialise)

	for _, block := range document.Blocks {
		writeESCPOSBlock(&out, block, columns)
	}

	// Feed before cutting, or the cut lands inside the last line of text — the mechanism sits
	// some millimetres above the blade.
	out.WriteString("\n\n\n")
	if options.Cut {
		out.Write(escCut)
	}
	return out.Bytes(), nil
}

func writeESCPOSBlock(out *bytes.Buffer, block Resolved, columns int) {
	switch block.Kind {
	case Rule:
		out.Write(escAlignStart)
		out.WriteString(strings.Repeat("-", columns) + "\n")
		return
	case Space:
		out.WriteString(strings.Repeat("\n", max(block.Lines, 1)))
		return
	}

	out.Write(alignBytes(block.Align))
	if block.Bold {
		out.Write(escBoldOn)
	}
	if block.Large {
		out.Write(escLargeOn)
	}

	// Large text is double width, so half as much fits on a line. Getting this wrong wraps the
	// total — the one line on the receipt that must not wrap.
	usable := columns
	if block.Large {
		usable = columns / 2
	}

	switch block.Kind {
	case Pair:
		out.WriteString(pairLine(block.Text, block.Value, usable) + "\n")
	case Barcode:
		// Printed as text, not as a barcode. Emitting GS k would need the symbology validated
		// against what THIS printer supports, and an unsupported one prints as garbage; the
		// number in plain characters is always readable and always keyable.
		out.WriteString(block.Text + "\n")
	case Rows:
		writeESCPOSTable(out, block, usable)
	default:
		for _, line := range wrap(block.Text, usable) {
			out.WriteString(line + "\n")
		}
	}

	if block.Large {
		out.Write(escLargeOff)
	}
	if block.Bold {
		out.Write(escBoldOff)
	}
}

func writeESCPOSTable(out *bytes.Buffer, block Resolved, columns int) {
	// Allocated against the CHARACTER COUNT directly, not against 100 and then divided — the
	// second rounding is what leaves a table one column short of the paper's edge.
	widths := Widths(block.Headers, columns)
	for i := range widths {
		// At least one character per column: a column allotted zero would silently swallow its
		// contents, which on a receipt means an item with no price.
		widths[i] = max(widths[i], 1)
	}

	out.Write(escAlignStart)
	if hasText(block.Headers) {
		out.WriteString(tableLine(block.Headers, widths) + "\n")
	}
	for _, row := range block.Rows {
		out.WriteString(tableLine(row, widths) + "\n")
	}
}

func tableLine(cells []Cell, widths []int) string {
	var line strings.Builder
	for i, cell := range cells {
		if i >= len(widths) {
			break
		}
		line.WriteString(fit(cell.Text, widths[i], cell.Align))
	}
	return strings.TrimRight(line.String(), " ")
}

// pairLine pushes a label and a value to opposite edges of one line.
func pairLine(label, value string, columns int) string {
	gap := columns - utf8.RuneCountInString(label) - utf8.RuneCountInString(value)
	if gap < 1 {
		// No room for both. The VALUE survives and the label is cut, because a receipt line
		// reading "TOT 1,234.50" is understood and one reading "TOTAL PAYABLE INC" is not.
		keep := columns - utf8.RuneCountInString(value) - 1
		if keep < 1 {
			return value
		}
		return truncate(label, keep) + " " + value
	}
	return label + strings.Repeat(" ", gap) + value
}

// fit pads or truncates a cell to a width.
func fit(text string, width int, alignment Align) string {
	text = truncate(text, width)
	pad := width - utf8.RuneCountInString(text)
	if pad <= 0 {
		return text
	}
	switch alignment {
	case End:
		return strings.Repeat(" ", pad) + text
	case Center:
		left := pad / 2
		return strings.Repeat(" ", left) + text + strings.Repeat(" ", pad-left)
	default:
		return text + strings.Repeat(" ", pad)
	}
}

func truncate(text string, width int) string {
	if utf8.RuneCountInString(text) <= width {
		return text
	}
	runes := []rune(text)
	return string(runes[:max(width, 0)])
}

// wrap breaks text at word boundaries.
func wrap(text string, columns int) []string {
	if text == "" {
		return []string{""}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}

	lines := make([]string, 0, 2)
	current := words[0]
	for _, word := range words[1:] {
		if utf8.RuneCountInString(current)+1+utf8.RuneCountInString(word) <= columns {
			current += " " + word
			continue
		}
		lines = append(lines, current)
		current = word
	}
	return append(lines, current)
}

func alignBytes(alignment Align) []byte {
	switch alignment {
	case Center:
		return escAlignMid
	case End:
		return escAlignEnd
	default:
		return escAlignStart
	}
}

func hasText(cells []Cell) bool {
	for _, cell := range cells {
		if cell.Text != "" {
			return true
		}
	}
	return false
}

// requireASCII refuses a document this renderer cannot print faithfully.
//
// Checked over the WHOLE document before a single byte is emitted. A printer that has already
// consumed half a receipt cannot be un-printed, so the paper is never touched until the answer
// is known.
func requireASCII(document Document) error {
	offending := ""
	visit := func(text string) {
		if offending != "" || text == "" {
			return
		}
		for _, r := range text {
			// Beyond Latin-1 there is no single-byte code page a till printer can be relied on
			// to have. Arabic-Indic digits land here too, which is correct: they are exactly as
			// unprintable as the script they belong to.
			if r > 0xFF {
				offending = text
				return
			}
		}
	}

	for _, block := range document.Blocks {
		visit(block.Text)
		visit(block.Value)
		for _, cell := range block.Headers {
			visit(cell.Text)
		}
		for _, row := range block.Rows {
			for _, cell := range row {
				visit(cell.Text)
			}
		}
	}

	if offending != "" {
		return errs.Conflict(CodeUnsupported,
			"a till printer cannot print this script; print it through the browser instead").
			WithParam("text", offending)
	}
	return nil
}
