package tabular

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"strconv"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// XLSX renders a sheet as a real Excel workbook.
//
// # Why this is written by hand rather than by a library
//
// The package comment above argued for CSV over XLSX on the grounds that "a library that writes a
// binary format is a dependency for a decade". That argument still holds — and it was an argument
// against the DEPENDENCY, not against the format.
//
// An .xlsx is a ZIP of four small XML documents. `archive/zip` and `encoding/xml` are in the
// standard library, the subset of SpreadsheetML needed for "headers and rows" is a page of markup,
// and every part of it is asserted by tests in this package. There is no third-party code to
// audit, no CVE feed to watch, and nothing to upgrade in five years.
//
// CSV stays, and stays the default: an accountant's own system reads it, and a shop that only
// wants numbers should not be handed a zip archive. This exists because "export to Excel" is what
// people ask for, and because CSV cannot carry a right-to-left sheet, a frozen header row, or
// text that stays text.
//
// # What it deliberately does not do
//
// No formulas, no formatting beyond a bold header, no multiple sheets, no number typing — every
// cell is an inline STRING. That last one matters: the money and quantity values in this
// application are decimal strings of exact integers, and letting Excel parse them as numbers is
// how "1,234.56" becomes a float and a total stops adding up. A spreadsheet that shows the shop
// the same digits the screen showed is worth more than one that can sum a column.
func XLSX(sheet Sheet, opts XLSXOptions) ([]byte, error) {
	if err := validate(sheet); err != nil {
		return nil, err
	}

	name := strings.TrimSpace(opts.SheetName)
	if name == "" {
		name = "Sheet1"
	}
	// Excel refuses these in a sheet name, and refuses the whole file rather than the name.
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`:\/?*[]`, r) {
			return '-'
		}
		return r
	}, name)
	if len([]rune(name)) > 31 {
		name = string([]rune(name)[:31])
	}

	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)

	write := func(path, body string) error {
		w, err := archive.Create(path)
		if err != nil {
			return errs.Wrap(err, errs.CategoryInternal, CodeWriteFailed, "creating "+path)
		}
		if _, err = w.Write([]byte(body)); err != nil {
			return errs.Wrap(err, errs.CategoryInternal, CodeWriteFailed, "writing "+path)
		}
		return nil
	}

	for _, part := range []struct{ path, body string }{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", packageRels},
		{"xl/_rels/workbook.xml.rels", workbookRels},
		{"xl/styles.xml", styles},
		{"xl/workbook.xml", workbook(name)},
		{"xl/worksheets/sheet1.xml", worksheet(sheet, opts.RightToLeft)},
	} {
		if err := write(part.path, part.body); err != nil {
			return nil, err
		}
	}

	if err := archive.Close(); err != nil {
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeWriteFailed, "closing the workbook")
	}
	return buffer.Bytes(), nil
}

// XLSXOptions configures the workbook.
type XLSXOptions struct {
	// SheetName is the tab's label. Blank means "Sheet1".
	SheetName string
	// RightToLeft lays the sheet out with column A on the right.
	//
	// A property of the SHEET, not of the text: Excel renders Arabic correctly either way, but a
	// right-to-left reader scanning a left-to-right sheet reads the columns backwards. This is
	// the one piece of presentation CSV cannot carry at all.
	RightToLeft bool
}

// ── the parts ───────────────────────────────────────────────────────────────────

const contentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>
</Types>`

const packageRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`

const workbookRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
</Relationships>`

// styles carries exactly two: the default, and a bold one for the header row.
//
// Style 0 must exist and must be the default — Excel reads cell style indexes against this list,
// and a file whose styles are missing opens as "unreadable content" rather than as unstyled.
const styles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>
<fills count="1"><fill><patternFill patternType="none"/></fill></fills>
<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>
<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>
<cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/></cellXfs>
</styleSheet>`

func workbook(sheetName string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<sheets><sheet name="` + escapeXML(sheetName) + `" sheetId="1" r:id="rId1"/></sheets>
</workbook>`
}

func worksheet(sheet Sheet, rtl bool) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)

	// The header stays on screen while a shop scrolls a thousand stock lines, and the sheet reads
	// right to left when the user does.
	b.WriteString(`<sheetViews><sheetView workbookViewId="0"`)
	if rtl {
		b.WriteString(` rightToLeft="1"`)
	}
	b.WriteString(`><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>`)

	b.WriteString(`<sheetData>`)
	writeRow(&b, 1, sheet.Columns, true)
	for i, row := range sheet.Rows {
		writeRow(&b, i+2, row, false)
	}
	b.WriteString(`</sheetData></worksheet>`)
	return b.String()
}

func writeRow(b *strings.Builder, number int, values []string, header bool) {
	b.WriteString(`<row r="` + strconv.Itoa(number) + `">`)
	for i, value := range values {
		ref := columnName(i) + strconv.Itoa(number)
		b.WriteString(`<c r="` + ref + `" t="inlineStr"`)
		if header {
			b.WriteString(` s="1"`)
		}
		b.WriteString(`><is><t xml:space="preserve">`)
		b.WriteString(escapeXML(value))
		b.WriteString(`</t></is></c>`)
	}
	b.WriteString(`</row>`)
}

// columnName turns a zero-based index into A, B … Z, AA, AB …
func columnName(index int) string {
	name := ""
	for {
		name = string(rune('A'+index%26)) + name
		index = index/26 - 1
		if index < 0 {
			return name
		}
	}
}

// escapeXML escapes text for XML, and strips the control characters XML cannot carry.
//
// A stray control byte in a product name makes the whole workbook unopenable — Excel reports
// "unreadable content" and offers to repair it, which loses the sheet. Dropping the byte loses
// one invisible character instead.
func escapeXML(value string) string {
	var b bytes.Buffer
	for _, r := range value {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			continue
		}
		_ = xml.EscapeText(&b, []byte(string(r)))
	}
	return b.String()
}
