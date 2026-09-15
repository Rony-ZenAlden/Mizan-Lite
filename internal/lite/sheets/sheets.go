// Package sheets writes Excel workbooks (.xlsx) with the standard library: a sheet per section, right to left, frozen
// headings, and typed cells — money as numbers in its currency's decimals, text as text (L7 §4.2, D-L7.5, Q-L7.6).
//
// # Why not Mizan's writer
//
// platform/tabular.XLSX writes one sheet of strings, by design: its figures stay exactly the screen's digits and cannot be
// summed. The owner asked for money as numbers so sums and formulas work (Q-L7.6), and for reports of several sections. A
// number here is written from Go's exact decimal text — never through a float — and shown by Excel in the reader's number
// format (L7 H5).
//
// # Text is never a formula
//
// Every text cell is an inline string (t="inlineStr"). Excel evaluates only <f> elements, so a product named =SUM(A1) or a note
// starting with +963 stays what it says, without the apostrophe CSV needs.
package sheets

import (
	"archive/zip"
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// Stable error codes.
const (
	CodeNotANumber  = "lite.sheets.not_a_number"
	CodeWriteFailed = "lite.sheets.write_failed"
)

// Cell is a text or a number.
type Cell struct {
	Text string
	// Number is an exact decimal ("-1234.50"); when set the cell is numeric, shown with Decimals places.
	Number   string
	Decimals int
	Bold     bool
	IsNumber bool
}

// Text is a text cell.
func Text(s string) Cell { return Cell{Text: s} }

// Number is a numeric cell from Go's exact decimal text.
func Number(decimal string, decimals int) Cell {
	return Cell{Number: decimal, Decimals: decimals, IsNumber: true}
}

// Bold returns the cell set in bold.
func (c Cell) Bolded() Cell { c.Bold = true; return c }

// Sheet is one sheet.
type Sheet struct {
	Name        string
	Rows        [][]Cell
	RightToLeft bool
	// Frozen is how many top rows stay in view (the headings).
	Frozen int
}

// Workbook is sheets and the file's properties.
type Workbook struct {
	Sheets  []Sheet
	Title   string
	Creator string
	// Created is RFC 3339 UTC, given by the caller: this package has no clock.
	Created string
}

var decimalRe = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)

// XLSX writes the workbook.
func XLSX(wb Workbook) ([]byte, error) {
	styles := newStyles()
	sheetXML := make([]string, len(wb.Sheets))
	names := uniqueNames(wb.Sheets)
	for i, sh := range wb.Sheets {
		x, err := sheet(sh, styles)
		if err != nil {
			return nil, err
		}
		sheetXML[i] = x
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	write := func(name, body string) error {
		w, err := z.Create(name)
		if err == nil {
			_, err = w.Write([]byte(body))
		}
		if err != nil {
			return errs.Wrap(err, errs.CategoryInternal, CodeWriteFailed, "writing "+name)
		}
		return nil
	}
	var overrides, rels, entries strings.Builder
	for i := range wb.Sheets {
		fmt.Fprintf(&overrides, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, i+1)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, i+1, i+1)
		fmt.Fprintf(&entries, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, esc(names[i]), i+1, i+1)
	}
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", xmlHead + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/><Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>` + overrides.String() + `</Types>`},
		{"_rels/.rels", xmlHead + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/></Relationships>`},
		{"docProps/core.xml", xmlHead + `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"><dc:title>` + esc(wb.Title) + `</dc:title><dc:creator>` + esc(wb.Creator) + `</dc:creator><dcterms:created xsi:type="dcterms:W3CDTF">` + esc(wb.Created) + `</dcterms:created></cp:coreProperties>`},
		{"xl/workbook.xml", xmlHead + `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>` + entries.String() + `</sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", xmlHead + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + rels.String() + fmt.Sprintf(`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`, len(wb.Sheets)+1) + `</Relationships>`},
		{"xl/styles.xml", styles.xml()},
	}
	for i, x := range sheetXML {
		parts = append(parts, struct{ name, body string }{fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), x})
	}
	for _, p := range parts {
		if err := write(p.name, p.body); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeWriteFailed, "closing the workbook")
	}
	return buf.Bytes(), nil
}

const xmlHead = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`

func sheet(sh Sheet, st *styles) (string, error) {
	var b strings.Builder
	b.WriteString(xmlHead + `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0"`)
	if sh.RightToLeft {
		b.WriteString(` rightToLeft="1"`)
	}
	b.WriteString(`>`)
	if sh.Frozen > 0 {
		fmt.Fprintf(&b, `<pane ySplit="%d" topLeftCell="A%d" activePane="bottomLeft" state="frozen"/>`, sh.Frozen, sh.Frozen+1)
	}
	b.WriteString(`</sheetView></sheetViews>`)
	widths := map[int]int{}
	for _, row := range sh.Rows {
		for c, cell := range row {
			n := utf8.RuneCountInString(cell.Text)
			if cell.IsNumber {
				n = len(cell.Number) + len(cell.Number)/3
			}
			widths[c] = max(widths[c], n)
		}
	}
	if len(widths) > 0 {
		b.WriteString(`<cols>`)
		for c := 0; c < len(widths); c++ {
			fmt.Fprintf(&b, `<col min="%d" max="%d" width="%d" customWidth="1"/>`, c+1, c+1, min(max(widths[c]+3, 8), 60))
		}
		b.WriteString(`</cols>`)
	}
	b.WriteString(`<sheetData>`)
	for r, row := range sh.Rows {
		fmt.Fprintf(&b, `<row r="%d">`, r+1)
		for c, cell := range row {
			ref := column(c) + strconv.Itoa(r+1)
			if cell.IsNumber {
				if !decimalRe.MatchString(cell.Number) {
					return "", errs.Validation(CodeNotANumber, "a numeric cell holds text").WithParam("value", cell.Number).WithParam("cell", ref)
				}
				fmt.Fprintf(&b, `<c r="%s" s="%d"><v>%s</v></c>`, ref, st.number(cell.Decimals, cell.Bold), cell.Number)
				continue
			}
			if cell.Text == "" {
				continue
			}
			fmt.Fprintf(&b, `<c r="%s" s="%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, st.text(cell.Bold), esc(cell.Text))
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData></worksheet>`)
	return b.String(), nil
}

// styles collects the cell formats a workbook uses: text or number of so many decimals, plain or bold.
type styles struct {
	xfs   []string
	index map[string]int
}

func newStyles() *styles {
	s := &styles{index: map[string]int{}}
	s.add("text", `<xf numFmtId="0" fontId="0" applyFont="1"/>`)
	return s
}

func (s *styles) add(key, xf string) int {
	if i, ok := s.index[key]; ok {
		return i
	}
	s.xfs = append(s.xfs, xf)
	s.index[key] = len(s.xfs) - 1
	return len(s.xfs) - 1
}

func boldFont(bold bool) int {
	if bold {
		return 1
	}
	return 0
}

func (s *styles) text(bold bool) int {
	return s.add(fmt.Sprintf("text-%v", bold), fmt.Sprintf(`<xf numFmtId="0" fontId="%d" applyFont="1"/>`, boldFont(bold)))
}

func (s *styles) number(decimals int, bold bool) int {
	decimals = min(max(decimals, 0), 6)
	return s.add(fmt.Sprintf("num-%d-%v", decimals, bold),
		fmt.Sprintf(`<xf numFmtId="%d" fontId="%d" applyNumberFormat="1" applyFont="1"/>`, 164+decimals, boldFont(bold)))
}

func (s *styles) xml() string {
	var fmts strings.Builder
	for d := 0; d <= 6; d++ {
		code := "#,##0"
		if d > 0 {
			code += "." + strings.Repeat("0", d)
		}
		fmt.Fprintf(&fmts, `<numFmt numFmtId="%d" formatCode="%s"/>`, 164+d, code)
	}
	return xmlHead + `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><numFmts count="7">` + fmts.String() +
		`</numFmts><fonts count="2"><font><sz val="11"/><name val="Arial"/></font><font><b/><sz val="11"/><name val="Arial"/></font></fonts>` +
		`<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>` +
		`<borders count="1"><border/></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0"/></cellStyleXfs>` +
		fmt.Sprintf(`<cellXfs count="%d">%s</cellXfs></styleSheet>`, len(s.xfs), strings.Join(s.xfs, ""))
}

func column(i int) string {
	name := ""
	for i++; i > 0; i = (i - 1) / 26 {
		name = string(rune('A'+(i-1)%26)) + name
	}
	return name
}

// uniqueNames makes names Excel accepts: at most 31 characters, none of :\/?*[], not empty, no two alike.
func uniqueNames(sheets []Sheet) []string {
	seen := map[string]bool{}
	out := make([]string, len(sheets))
	for i, sh := range sheets {
		name := strings.Map(func(r rune) rune {
			if strings.ContainsRune(`:\/?*[]`, r) {
				return '-'
			}
			return r
		}, strings.TrimSpace(sh.Name))
		if name == "" {
			name = "Sheet" + strconv.Itoa(i+1)
		}
		base := []rune(name)
		if len(base) > 31 {
			base = base[:31]
		}
		name = string(base)
		for n := 2; seen[strings.ToLower(name)]; n++ {
			suffix := " (" + strconv.Itoa(n) + ")"
			name = string(base[:min(len(base), 31-len(suffix))]) + suffix
		}
		seen[strings.ToLower(name)] = true
		out[i] = name
	}
	return out
}

func esc(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '"':
			b.WriteString("&quot;")
		case r < 0x20 && r != '\t' && r != '\n' && r != '\r', r == 0xFFFE, r == 0xFFFF:
			// not allowed in XML 1.0: dropped rather than producing a file Excel refuses
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
