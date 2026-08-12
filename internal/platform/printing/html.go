package printing

import (
	"fmt"
	"html"
	"strings"
)

// Media widths, in millimetres. The two thermal sizes are the industry's, not a choice.
const (
	receipt80 = "80mm"
	receipt58 = "58mm"
)

// HTMLOptions selects the paper.
type HTMLOptions struct {
	// Paper is "A4", "A5", "80mm", or "58mm".
	Paper string
	// Title appears in the print dialogue and in a saved PDF's metadata.
	Title string
}

// HTML renders a document for the browser to print.
//
// # Why the browser
//
// Arabic is the reason. Bidirectional text, contextual letter shaping, and line breaking are
// decades of work sitting inside every browser and inside nothing a Go program can reach
// offline. Wails ships one. Using it costs nothing and is the only way this product prints an
// Arabic invoice that a customer would accept.
//
// # Self-contained, always
//
// One <style> element and no external anything: no CDN, no web font, no image URL. A shop's till
// prints during an internet outage, and a stylesheet that failed to load is an invoice with no
// layout handed to a customer. The application is offline-first (§1); its printing must be too.
func HTML(document Document, options HTMLOptions) string {
	paper := options.Paper
	if paper == "" {
		paper = "A4"
	}
	thermal := paper == receipt80 || paper == receipt58

	var body strings.Builder
	for _, block := range document.Blocks {
		writeBlock(&body, block)
	}

	return fmt.Sprintf(`<!doctype html>
<html lang="%s" dir="%s">
<head>
<meta charset="utf-8">
<title>%s</title>
<style>%s</style>
</head>
<body class="%s">
<main class="sheet">
%s</main>
</body>
</html>
`,
		htmlLang(document.Direction), document.Direction, html.EscapeString(options.Title),
		stylesheet(paper, thermal), mediaClass(thermal), body.String())
}

func writeBlock(out *strings.Builder, block Resolved) {
	classes := []string{string(block.Kind), "align-" + string(block.Align)}
	if block.Bold {
		classes = append(classes, "bold")
	}
	if block.Large {
		classes = append(classes, "large")
	}
	class := strings.Join(classes, " ")

	switch block.Kind {
	case Rule:
		out.WriteString("<hr class=\"rule\">\n")
	case Space:
		for i := 0; i < max(block.Lines, 1); i++ {
			out.WriteString("<div class=\"space\"></div>\n")
		}
	case Pair:
		fmt.Fprintf(out, "<div class=\"%s\"><span>%s</span><span>%s</span></div>\n",
			class, html.EscapeString(block.Text), html.EscapeString(block.Value))
	case Barcode:
		// The VALUE is printed beneath, deliberately. A barcode this renderer cannot draw is
		// still readable as text, and a human can key it — which is what somebody does anyway
		// when a label is scuffed.
		fmt.Fprintf(out,
			"<div class=\"%s\" data-symbology=\"%s\"><code>%s</code></div>\n",
			class, html.EscapeString(block.Symbology), html.EscapeString(block.Text))
	case Rows:
		writeTable(out, block)
	default:
		fmt.Fprintf(out, "<div class=\"%s\">%s</div>\n", class, html.EscapeString(block.Text))
	}
}

func writeTable(out *strings.Builder, block Resolved) {
	// Percentages, so the browser reflows them into whatever paper it was given.
	shares := Widths(block.Headers, 100)

	out.WriteString("<table class=\"rows\">\n<thead><tr>")
	for i, cell := range block.Headers {
		fmt.Fprintf(out, "<th class=\"align-%s\" style=\"width:%d%%\">%s</th>",
			cell.Align, shares[i], html.EscapeString(cell.Text))
	}
	out.WriteString("</tr></thead>\n<tbody>\n")
	for _, row := range block.Rows {
		out.WriteString("<tr>")
		for _, cell := range row {
			fmt.Fprintf(out, "<td class=\"align-%s\">%s</td>",
				cell.Align, html.EscapeString(cell.Text))
		}
		out.WriteString("</tr>\n")
	}
	out.WriteString("</tbody>\n</table>\n")
}

func mediaClass(thermal bool) string {
	if thermal {
		return "thermal"
	}
	return "document"
}

// htmlLang reports a language for the lang attribute.
//
// Direction is what this package is given, and direction is not a language. "ar" is offered for
// RTL because it is overwhelmingly the RTL language this product serves, and a wrong-but-close
// lang attribute still gets the font stack and hyphenation nearer than none.
func htmlLang(direction string) string {
	if direction == "rtl" {
		return "ar"
	}
	return "en"
}

// stylesheet is the whole design, inline.
//
// @page is what makes one renderer serve A4 and 80mm alike: the browser is told the physical
// paper, and the same markup reflows into it.
func stylesheet(paper string, thermal bool) string {
	size := paper
	margin := "12mm"
	if thermal {
		// A thermal roll has no margins to speak of — the mechanism holds about 2mm and the
		// paper is the width it is. Asking for 12mm would throw away a quarter of an 80mm line.
		margin = "2mm"
		size = paper + " auto" // continuous roll: as long as it needs to be.
	}

	return fmt.Sprintf(`
@page { size: %s; margin: %s; }
* { box-sizing: border-box; }
html, body { margin: 0; padding: 0; }
body {
  font-family: system-ui, -apple-system, "Segoe UI", "Noto Naskh Arabic", "Noto Sans", sans-serif;
  color: #000; background: #fff;
  font-variant-numeric: tabular-nums;
}
/* Arabic needs more vertical breathing room than Latin at the same size (§22.3): its
   ascenders and descenders are taller and its diacritics sit outside the x-height. */
html[dir="rtl"] body { line-height: 1.8; }
html[dir="ltr"] body { line-height: 1.45; }

.sheet { padding: 0; }
body.thermal { font-size: 11px; }
body.thermal .sheet { width: 100%%; }
body.document { font-size: 12px; }
body.document .sheet { max-width: 186mm; margin: 0 auto; }

/* start/end rather than left/right, so one template serves both directions. */
.align-start  { text-align: start; }
.align-center { text-align: center; }
.align-end    { text-align: end; }

.text, .pair, .code { margin: 0; padding: 1px 0; }
.pair { display: flex; justify-content: space-between; gap: 8px; }
.bold  { font-weight: 700; }
.large { font-size: 1.6em; font-weight: 700; }
.rule  { border: 0; border-top: 1px dashed #000; margin: 4px 0; }
.space { height: 6px; }
.code  { text-align: center; font-family: ui-monospace, "SF Mono", Menlo, monospace; }

table.rows { width: 100%%; border-collapse: collapse; margin: 4px 0; }
table.rows th { border-bottom: 1px solid #000; padding: 2px 3px; font-weight: 600; }
table.rows td { padding: 2px 3px; vertical-align: top; }
body.document table.rows tbody tr:nth-child(even) { background: #f4f4f4; }

/* A page break between an item and its price is a document somebody has to re-read. */
table.rows tr { break-inside: avoid; }
.pair, .code { break-inside: avoid; }

@media print {
  /* Backgrounds are off by default in print. The zebra striping is decoration; the layout
     must not depend on it, and it is not asked for. */
  body.document table.rows tbody tr:nth-child(even) { background: transparent; }
}
`, size, margin)
}
