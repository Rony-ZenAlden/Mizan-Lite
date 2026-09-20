package documents

import (
	"strings"
	"unicode"
)

// Code 128, for the shelf labels and price tags a shop prints (the owner's request, 2026-09-20).
//
// # Why Code 128 and not EAN-13
//
// EAN-13 encodes exactly thirteen digits with a check digit, and a shop's own barcodes are whatever the supplier
// printed or whatever the shop made up — letters included, any length. Code 128 takes all of it. Where a product
// already carries a real EAN, a Code 128 symbol of the same digits still scans to the same string, which is what the
// till compares. The symbology is an encoding of the code, not a fact about the product.
//
// # Subsets
//
// Set C packs two digits into one symbol, so a numeric code is about half as wide. This encoder uses C for runs of an
// even number of digits, four or longer, and B for everything else, switching between them as it goes — the shortest
// symbol a simple encoder can produce, and narrow matters on a 40 mm label.

const (
	// The special symbols, by value.
	code128StartB = 104
	code128StartC = 105
	codeSwitchToB = 100
	codeSwitchToC = 99
	code128Stop   = 106
)

// code128Patterns is the bar/space width of every symbol value, six widths each: bar, space, bar, space, bar, space.
var code128Patterns = [107][6]int{
	{2, 1, 2, 2, 2, 2}, {2, 2, 2, 1, 2, 2}, {2, 2, 2, 2, 2, 1}, {1, 2, 1, 2, 2, 3}, {1, 2, 1, 3, 2, 2},
	{1, 3, 1, 2, 2, 2}, {1, 2, 2, 2, 1, 3}, {1, 2, 2, 3, 1, 2}, {1, 3, 2, 2, 1, 2}, {2, 2, 1, 2, 1, 3},
	{2, 2, 1, 3, 1, 2}, {2, 3, 1, 2, 1, 2}, {1, 1, 2, 2, 3, 2}, {1, 2, 2, 1, 3, 2}, {1, 2, 2, 2, 3, 1},
	{1, 1, 3, 2, 2, 2}, {1, 2, 3, 1, 2, 2}, {1, 2, 3, 2, 2, 1}, {2, 2, 3, 2, 1, 1}, {2, 2, 1, 1, 3, 2},
	{2, 2, 1, 2, 3, 1}, {2, 1, 3, 2, 1, 2}, {2, 2, 3, 1, 1, 2}, {3, 1, 2, 1, 3, 1}, {3, 1, 1, 2, 2, 2},
	{3, 2, 1, 1, 2, 2}, {3, 2, 1, 2, 2, 1}, {3, 1, 2, 2, 1, 2}, {3, 2, 2, 1, 1, 2}, {3, 2, 2, 2, 1, 1},
	{2, 1, 2, 1, 2, 3}, {2, 1, 2, 3, 2, 1}, {2, 3, 2, 1, 2, 1}, {1, 1, 1, 3, 2, 3}, {1, 3, 1, 1, 2, 3},
	{1, 3, 1, 3, 2, 1}, {1, 1, 2, 3, 1, 3}, {1, 3, 2, 1, 1, 3}, {1, 3, 2, 3, 1, 1}, {2, 1, 1, 3, 1, 3},
	{2, 3, 1, 1, 1, 3}, {2, 3, 1, 3, 1, 1}, {1, 1, 2, 1, 3, 3}, {1, 1, 2, 3, 3, 1}, {1, 3, 2, 1, 3, 1},
	{1, 1, 3, 1, 2, 3}, {1, 1, 3, 3, 2, 1}, {1, 3, 3, 1, 2, 1}, {3, 1, 3, 1, 2, 1}, {2, 1, 1, 3, 3, 1},
	{2, 3, 1, 1, 3, 1}, {2, 1, 3, 1, 1, 3}, {2, 1, 3, 3, 1, 1}, {2, 1, 3, 1, 3, 1}, {3, 1, 1, 1, 2, 3},
	{3, 1, 1, 3, 2, 1}, {3, 3, 1, 1, 2, 1}, {3, 1, 2, 1, 1, 3}, {3, 1, 2, 3, 1, 1}, {3, 3, 2, 1, 1, 1},
	{3, 1, 4, 1, 1, 1}, {2, 2, 1, 4, 1, 1}, {4, 3, 1, 1, 1, 1}, {1, 1, 1, 2, 2, 4}, {1, 1, 1, 4, 2, 2},
	{1, 2, 1, 1, 2, 4}, {1, 2, 1, 4, 2, 1}, {1, 4, 1, 1, 2, 2}, {1, 4, 1, 2, 2, 1}, {1, 1, 2, 2, 1, 4},
	{1, 1, 2, 4, 1, 2}, {1, 2, 2, 1, 1, 4}, {1, 2, 2, 4, 1, 1}, {1, 4, 2, 1, 1, 2}, {1, 4, 2, 2, 1, 1},
	{2, 4, 1, 2, 1, 1}, {2, 2, 1, 1, 1, 4}, {4, 1, 3, 1, 1, 1}, {2, 4, 1, 1, 1, 2}, {1, 3, 4, 1, 1, 1},
	{1, 1, 1, 2, 4, 2}, {1, 2, 1, 1, 4, 2}, {1, 2, 1, 2, 4, 1}, {1, 1, 4, 2, 1, 2}, {1, 2, 4, 1, 1, 2},
	{1, 2, 4, 2, 1, 1}, {4, 1, 1, 2, 1, 2}, {4, 2, 1, 1, 1, 2}, {4, 2, 1, 2, 1, 1}, {2, 1, 2, 1, 4, 1},
	{2, 1, 4, 1, 2, 1}, {4, 1, 2, 1, 2, 1}, {1, 1, 1, 1, 4, 3}, {1, 1, 1, 3, 4, 1}, {1, 3, 1, 1, 4, 1},
	{1, 1, 4, 1, 1, 3}, {1, 1, 4, 3, 1, 1}, {4, 1, 1, 1, 1, 3}, {4, 1, 1, 3, 1, 1}, {1, 1, 3, 1, 4, 1},
	{1, 1, 4, 1, 3, 1}, {3, 1, 1, 1, 4, 1}, {4, 1, 1, 1, 3, 1}, {2, 1, 1, 4, 1, 2}, {2, 1, 1, 2, 1, 4},
	{2, 1, 1, 2, 3, 2}, {2, 3, 3, 1, 1, 1},
}

// code128Stop's pattern is seven widths, not six: the only symbol with a trailing bar.
var code128StopPattern = [7]int{2, 3, 3, 1, 1, 1, 2}

// Code128 encodes text as the alternating bar and space widths of a Code 128 symbol, starting with a bar.
//
// ok is false for anything Code 128 cannot carry: an empty code, or a character outside printable ASCII. A shop whose
// barcode is Arabic or empty gets a label with no symbol rather than a symbol that scans as something else.
func Code128(text string) (bars []int, ok bool) {
	if text == "" {
		return nil, false
	}
	for _, r := range text {
		if r < 32 || r > 126 {
			return nil, false
		}
	}
	values := code128Values(text)
	// The check symbol: the start value plus each value times its position, modulo 103.
	sum := values[0]
	for i, v := range values[1:] {
		sum += v * (i + 1)
	}
	values = append(values, sum%103)

	for _, v := range values {
		bars = append(bars, code128Patterns[v][:]...)
	}
	bars = append(bars, code128StopPattern[:]...)
	return bars, true
}

// code128Values is the symbol values of text, starting with a start symbol, switching between sets B and C to keep the
// symbol short.
func code128Values(text string) []int {
	var out []int
	i := 0
	inC := digitRun(text, 0) >= 4 && digitRun(text, 0)%2 == 0
	if inC {
		out = append(out, code128StartC)
	} else {
		out = append(out, code128StartB)
	}
	for i < len(text) {
		if inC {
			if run := digitRun(text, i); run >= 2 && run%2 == 0 || run >= 4 {
				out = append(out, int(text[i]-'0')*10+int(text[i+1]-'0'))
				i += 2
				continue
			}
			out = append(out, codeSwitchToB)
			inC = false
			continue
		}
		// Four or more digits left, an even number of them, is worth the switch.
		if run := digitRun(text, i); run >= 4 && run%2 == 0 {
			out = append(out, codeSwitchToC)
			inC = true
			continue
		}
		out = append(out, int(text[i])-32)
		i++
	}
	return out
}

// digitRun is how many digits start at i.
func digitRun(text string, i int) int {
	n := 0
	for ; i+n < len(text) && unicode.IsDigit(rune(text[i+n])); n++ {
	}
	return n
}

// BarcodeOf is a Barcode block for a code, or ok=false where the code cannot be drawn.
func BarcodeOf(code, text string, heightLines float32) (Barcode, bool) {
	bars, ok := Code128(strings.TrimSpace(code))
	if !ok {
		return Barcode{}, false
	}
	return Barcode{Bars: bars, Text: text, HeightLines: heightLines}, true
}
