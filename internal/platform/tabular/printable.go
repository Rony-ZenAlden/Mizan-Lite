package tabular

import "strings"

// PrintableHTML renders a sheet as a self-contained page for the browser to print.
//
// # Why PDF is produced by printing rather than by writing one
//
// A PDF writer that handles Arabic needs the bidirectional algorithm, contextual letter shaping,
// and font subsetting — decades of work sitting inside every browser and inside nothing a Go
// program can reach offline. Wails ships a browser. The print dialogue on both supported
// platforms offers "Save as PDF", and the file it produces has correctly shaped Arabic, selectable
// text, and real page breaks.
//
// The same reasoning the printing package already made for receipts (§ its own doc comment). This
// is that argument applied to reports, which is why the answer is a page rather than a library.
//
// # Self-contained, always
//
// One <style> element and no external anything: no CDN, no web font, no image. A shop prints
// during an outage, and a stylesheet that failed to load is a report with no layout.
//
// # What it is NOT
//
// Not a receipt. Receipts are 80mm, monospaced, and built from a template a shop can edit;
// reports are A4, tabular, and the same shape for everyone. Sharing a renderer between them would
// mean one template answering to two layouts, which is how both end up wrong.
func PrintableHTML(sheet Sheet, opts PrintOptions) (string, error) {
	if err := validate(sheet); err != nil {
		return "", err
	}

	dir, lang := "ltr", "en"
	if opts.RightToLeft {
		dir, lang = "rtl", "ar"
	}

	var body strings.Builder
	if title := strings.TrimSpace(opts.Title); title != "" {
		body.WriteString(`<h1>` + escapeHTML(title) + `</h1>`)
	}
	if subtitle := strings.TrimSpace(opts.Subtitle); subtitle != "" {
		body.WriteString(`<p class="sub">` + escapeHTML(subtitle) + `</p>`)
	}

	body.WriteString(`<table><thead><tr>`)
	for _, column := range sheet.Columns {
		body.WriteString(`<th>` + escapeHTML(column) + `</th>`)
	}
	body.WriteString(`</tr></thead><tbody>`)
	for _, row := range sheet.Rows {
		body.WriteString(`<tr>`)
		for _, value := range row {
			// Every cell is text, and every cell is `dir="auto"`. A product name in Arabic beside
			// a figure in Western digits is the ordinary case here, and `auto` is what lets the
			// browser resolve each cell on its own content rather than inheriting the page's
			// direction and reordering a number.
			body.WriteString(`<td dir="auto">` + escapeHTML(value) + `</td>`)
		}
		body.WriteString(`</tr>`)
	}
	body.WriteString(`</tbody></table>`)

	if footer := strings.TrimSpace(opts.Footer); footer != "" {
		body.WriteString(`<p class="foot">` + escapeHTML(footer) + `</p>`)
	}

	return `<!doctype html>
<html lang="` + lang + `" dir="` + dir + `">
<head>
<meta charset="utf-8">
<title>` + escapeHTML(opts.Title) + `</title>
<style>` + printCSS + `</style>
</head>
<body>` + body.String() + `</body>
</html>`, nil
}

// PrintOptions configures the printable page.
type PrintOptions struct {
	// Title is the heading. It also becomes the page title, which is what a saved PDF is named.
	Title string
	// Subtitle is one line under it — a date range, a company name.
	Subtitle string
	// Footer prints under the table, in small type.
	Footer string
	// RightToLeft sets the page direction and right-aligns the table.
	RightToLeft bool
}

// printCSS is the whole stylesheet. Deliberately small.
//
// `thead { display: table-header-group }` is the line that matters and the one that is easy to
// leave out: without it a table spanning three pages prints its headers once, and pages two and
// three are columns of unlabelled numbers.
const printCSS = `
  @page { size: A4; margin: 14mm; }
  * { box-sizing: border-box; }
  body {
    margin: 0;
    font-family: "IBM Plex Sans Arabic", Cairo, Inter, -apple-system, BlinkMacSystemFont,
                 "Segoe UI", system-ui, "Noto Sans Arabic", Tahoma, Arial, sans-serif;
    font-size: 10pt;
    line-height: 1.45;
    color: #18181b;
  }
  h1 { margin: 0 0 2mm; font-size: 15pt; font-weight: 600; }
  .sub { margin: 0 0 5mm; font-size: 9.5pt; color: #52525b; }
  .foot { margin-top: 5mm; font-size: 8.5pt; color: #71717a; }
  table { width: 100%; border-collapse: collapse; }
  thead { display: table-header-group; }
  tr { page-break-inside: avoid; }
  th, td {
    border: 0.4pt solid #d4d4d8;
    padding: 1.6mm 2.2mm;
    text-align: start;
    vertical-align: top;
  }
  th { background: #f4f4f5; font-weight: 600; }
  tbody tr:nth-child(even) td { background: #fafafa; }
`

// escapeHTML escapes text for HTML, and drops the control characters that would break the page.
func escapeHTML(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			continue
		}
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&#39;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
