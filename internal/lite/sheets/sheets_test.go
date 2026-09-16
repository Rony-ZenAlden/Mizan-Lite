package sheets_test

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/sheets"
)

func open(t *testing.T, raw []byte) map[string]string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range z.File {
		r, _ := f.Open()
		body, _ := io.ReadAll(r)
		_ = r.Close()
		// Every part is well-formed XML.
		d := xml.NewDecoder(bytes.NewReader(body))
		for {
			if _, err := d.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("%s is not well-formed: %v", f.Name, err)
			}
		}
		out[f.Name] = string(body)
	}
	return out
}

func workbook() sheets.Workbook {
	return sheets.Workbook{Title: "تقرير اليوم", Creator: "بقالية المونة", Created: "2026-09-14T07:32:00Z", Sheets: []sheets.Sheet{
		{Name: "الأرباح", RightToLeft: true, Frozen: 1, Rows: [][]sheets.Cell{
			{sheets.Text("البند").Bolded(), sheets.Text("بالدولار").Bolded(), sheets.Text("بالليرة").Bolded()},
			{sheets.Text("الإيراد"), sheets.Number("114.02", 2), sheets.Number("1710500", 0)},
			{sheets.Text("=SUM(A1:A9)"), sheets.Number("-8.66", 2), sheets.Number("-129905", 0).Bolded()},
		}},
		{Name: "Takings: 2026/09?", Rows: [][]sheets.Cell{{sheets.Text("+963 933 123"), sheets.Number("0.750", 3)}}},
		{Name: "الأرباح"},
	}}
}

func TestAWorkbookReadsBack(t *testing.T) {
	raw, err := sheets.XLSX(workbook())
	if err != nil {
		t.Fatal(err)
	}
	parts := open(t, raw)
	book := parts["xl/workbook.xml"]
	for _, want := range []string{`<sheet name="الأرباح" sheetId="1"`, `<sheet name="Takings- 2026-09-" sheetId="2"`, `<sheet name="الأرباح (2)" sheetId="3"`} {
		if !strings.Contains(book, want) {
			t.Fatalf("workbook lacks %s:\n%s", want, book)
		}
	}
	first := parts["xl/worksheets/sheet1.xml"]
	if !strings.Contains(first, `rightToLeft="1"`) || !strings.Contains(first, `<pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/>`) {
		t.Fatal("direction and frozen heading")
	}
	if strings.Contains(parts["xl/worksheets/sheet2.xml"], "rightToLeft") || strings.Contains(parts["xl/worksheets/sheet2.xml"], "<pane") {
		t.Fatal("the second sheet asked for neither")
	}
	if !strings.Contains(parts["docProps/core.xml"], "<dc:title>تقرير اليوم</dc:title>") || !strings.Contains(parts["[Content_Types].xml"], "sheet3.xml") {
		t.Fatal("properties and content types")
	}
}

func TestMoneyIsANumberInItsCurrencysDecimals(t *testing.T) {
	raw, _ := sheets.XLSX(workbook())
	parts := open(t, raw)
	first := parts["xl/worksheets/sheet1.xml"]
	styles := parts["xl/styles.xml"]
	for _, c := range []struct{ ref, value, format string }{
		{"B2", "114.02", "#,##0.00"}, {"C2", "1710500", "#,##0"}, {"B3", "-8.66", "#,##0.00"},
	} {
		start := strings.Index(first, `<c r="`+c.ref+`" s="`)
		if start < 0 {
			t.Fatalf("no %s", c.ref)
		}
		cell := first[start : start+strings.Index(first[start:], "</c>")]
		if strings.Contains(cell, "inlineStr") || !strings.Contains(cell, "<v>"+c.value+"</v>") {
			t.Fatalf("%s is not the number %s: %s", c.ref, c.value, cell)
		}
		idx := between(t, cell, `s="`, `"`)
		_, cellXfs, found := strings.Cut(styles, "<cellXfs")
		if !found {
			t.Fatal("no cellXfs")
		}
		xfs := strings.Split(cellXfs, "<xf ")[1:]
		var n int
		for _, ch := range idx {
			n = n*10 + int(ch-'0')
		}
		xf := xfs[n]
		fmtID := between(t, xf, `numFmtId="`, `"`)
		if !strings.Contains(styles, `<numFmt numFmtId="`+fmtID+`" formatCode="`+c.format+`"/>`) {
			t.Fatalf("%s is formatted %s, want %s", c.ref, fmtID, c.format)
		}
	}
	if !strings.Contains(parts["xl/worksheets/sheet2.xml"], "<v>0.750</v>") {
		t.Fatal("a quantity keeps its decimals")
	}
	_, err := sheets.XLSX(sheets.Workbook{Sheets: []sheets.Sheet{{Rows: [][]sheets.Cell{{sheets.Number("1,000", 0)}}}}})
	if errs.CodeOf(err) != sheets.CodeNotANumber {
		t.Fatalf("a grouped figure written as a number: %v", err)
	}
}

func TestAFormulaLikeNameStaysText(t *testing.T) {
	raw, _ := sheets.XLSX(workbook())
	parts := open(t, raw)
	for name, body := range parts {
		if strings.Contains(body, "<f>") || strings.Contains(body, "<f ") {
			t.Fatalf("%s holds a formula", name)
		}
	}
	if !strings.Contains(parts["xl/worksheets/sheet1.xml"], `t="inlineStr"><is><t xml:space="preserve">=SUM(A1:A9)</t>`) ||
		!strings.Contains(parts["xl/worksheets/sheet2.xml"], `<t xml:space="preserve">+963 933 123</t>`) {
		t.Fatal("formula-like text is typed text, unprefixed")
	}
}

func TestSheetNamesExcelAccepts(t *testing.T) {
	long := strings.Repeat("تقرير ", 10)
	raw, _ := sheets.XLSX(sheets.Workbook{Sheets: []sheets.Sheet{{Name: long}, {Name: long}, {Name: "  "}, {Name: "a<b>&\"c\""}}})
	book := open(t, raw)["xl/workbook.xml"]
	d := xml.NewDecoder(strings.NewReader(book))
	var names []string
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "sheet" {
			for _, a := range se.Attr {
				if a.Name.Local == "name" {
					names = append(names, a.Value)
				}
			}
		}
	}
	if len(names) != 4 || len([]rune(names[0])) != 31 || len([]rune(names[1])) != 31 || names[0] == names[1] || names[2] != "Sheet3" || names[3] != `a<b>&"c"` {
		t.Fatalf("names %q", names)
	}
}

// between is the text after the first open and before the next close, failing the test when either is missing.
func between(t *testing.T, s, open, closing string) string {
	t.Helper()
	_, rest, found := strings.Cut(s, open)
	if !found {
		t.Fatalf("no %q in %s", open, s)
	}
	inside, _, found := strings.Cut(rest, closing)
	if !found {
		t.Fatalf("no %q after %q in %s", closing, open, s)
	}
	return inside
}

func TestAWorkbookLiteWroteReadsBack(t *testing.T) {
	body, err := sheets.XLSX(sheets.Workbook{Sheets: []sheets.Sheet{
		{Name: "المنتجات", Rows: [][]sheets.Cell{
			{sheets.Text("الاسم"), sheets.Text("السعر")},
			{sheets.Text("زيت زيتون"), sheets.Number("3.25", 2), {}, sheets.Text("=SUM(A1)")},
			{},
			{sheets.Text("لبنة"), sheets.Number("15000", 0)},
		}},
		{Name: "units", Rows: [][]sheets.Cell{{sheets.Text("kg")}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	read, err := sheets.ReadXLSX(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(read) != 2 || read[0].Name != "المنتجات" || read[1].Name != "units" {
		t.Fatalf("sheets %+v", read)
	}
	rows := read[0].Rows
	if len(rows) != 4 || rows[1][0] != "زيت زيتون" || rows[1][1] != "3.25" || rows[1][2] != "" || rows[1][3] != "=SUM(A1)" || len(rows[2]) != 0 || rows[3][1] != "15000" {
		t.Fatalf("rows %q", rows)
	}
}

// TestAWorkbookExcelWroteReadsBack reads the parts Excel itself writes: shared strings with rich-text runs, a formula's cached
// value, a sparse row, and relationship targets written as absolute paths.
func TestAWorkbookExcelWroteReadsBack(t *testing.T) {
	parts := map[string]string{
		"xl/workbook.xml":            `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="worksheet" Target="/xl/worksheets/sheet1.xml"/></Relationships>`,
		"xl/sharedStrings.xml":       `<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><si><t>Olive oil</t></si><si><r><t>مربى </t></r><r><t>مشمش</t></r></si></sst>`,
		"xl/worksheets/sheet1.xml": `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>` +
			`<row r="2"><c r="A2" t="s"><v>0</v></c><c r="C2"><v>3.25</v></c></row>` +
			`<row r="3"><c r="B3" t="s"><v>1</v></c><c r="C3"><f>C2*2</f><v>6.5</v></c></row>` +
			`<row r="4"><c r="A4"><v>1.5E+3</v></c></row></sheetData></worksheet>`,
	}
	build := func(skip string) []byte {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		for name, body := range parts {
			if name == skip {
				continue
			}
			w, _ := zw.Create(name)
			_, _ = w.Write([]byte(body))
		}
		_ = zw.Close()
		return buf.Bytes()
	}
	_, err := sheets.ReadXLSX(build(""))
	if errs.CodeOf(err) != sheets.CodeExponent {
		t.Fatalf("an exponent: %v", err)
	}
	parts["xl/worksheets/sheet1.xml"] = strings.Replace(parts["xl/worksheets/sheet1.xml"], "1.5E+3", "1500", 1)
	read, err := sheets.ReadXLSX(build(""))
	if err != nil {
		t.Fatal(err)
	}
	rows := read[0].Rows
	if len(rows) != 4 || len(rows[0]) != 0 || rows[1][0] != "Olive oil" || rows[1][2] != "3.25" || rows[2][1] != "مربى مشمش" || rows[2][2] != "6.5" || rows[3][0] != "1500" {
		t.Fatalf("rows %q", rows)
	}
	if _, err := sheets.ReadXLSX(build("xl/workbook.xml")); errs.CodeOf(err) != sheets.CodeNotAWorkbook {
		t.Fatalf("a missing part: %v", err)
	}
	if _, err := sheets.ReadXLSX([]byte("not a zip")); errs.CodeOf(err) != sheets.CodeNotAWorkbook {
		t.Fatalf("not a zip: %v", err)
	}
}
