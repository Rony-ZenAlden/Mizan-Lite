package tabular_test

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/platform/tabular"
)

// A sheet with the things that break document writers: Arabic, a quote, an ampersand, a
// formula-looking value, and a control byte.
func awkward() tabular.Sheet {
	return tabular.Sheet{
		Columns: []string{"Code", "المنتج", "Total"},
		Rows: [][]string{
			{"CEM-1", "إسمنت \"أبيض\" & رمل", "1,234.56"},
			{"=SUM(A1)", "Widget <tag>", "-500.00"},
			{"CTRL", "bad\x07byte", "0.00"},
		},
	}
}

// unzip reads one part out of an OOXML package, failing loudly if it is not there.
func unzip(t *testing.T, archive []byte, path string) string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatalf("the file is not a zip archive: %v", err)
	}
	for _, file := range reader.File {
		if file.Name != path {
			continue
		}
		opened, openErr := file.Open()
		if openErr != nil {
			t.Fatalf("opening %s: %v", path, openErr)
		}
		defer func() { _ = opened.Close() }()
		body, readErr := io.ReadAll(opened)
		if readErr != nil {
			t.Fatalf("reading %s: %v", path, readErr)
		}
		return string(body)
	}
	t.Fatalf("%s is not in the package", path)
	return ""
}

// ── the workbook ────────────────────────────────────────────────────────────────

// TestAWorkbookIsAValidPackageAndEveryPartIsWellFormedXML
//
// # Why this parses rather than pattern-matches
//
// A hand-written OOXML file that is ALMOST right does not degrade — Excel reports "unreadable
// content" and offers to repair it, which silently drops the sheet. A test asserting that the
// bytes contain a substring would pass against a file no spreadsheet can open.
//
// So every part is parsed as XML. That is the property that decides whether the export works at
// all, and it is the one a human reviewer cannot check by eye.
func TestAWorkbookIsAValidPackageAndEveryPartIsWellFormedXML(t *testing.T) {
	book, err := tabular.XLSX(awkward(), tabular.XLSXOptions{SheetName: "Stock"})
	if err != nil {
		t.Fatalf("XLSX: %v", err)
	}

	required := []string{
		"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml",
		"xl/_rels/workbook.xml.rels", "xl/styles.xml", "xl/worksheets/sheet1.xml",
	}
	for _, part := range required {
		body := unzip(t, book, part)
		if err = xml.Unmarshal([]byte(body), new(struct {
			XMLName xml.Name
			Inner   []byte `xml:",innerxml"`
		})); err != nil {
			t.Errorf("%s is not well-formed XML: %v", part, err)
		}
	}
}

// TestEveryCellSurvivesIntoTheWorkbook
//
// Arabic, quotes and ampersands are what a shop's real data is made of, and each one breaks a
// different naive implementation: a missing escape, a missing encoding declaration, a byte
// written raw.
func TestEveryCellSurvivesIntoTheWorkbook(t *testing.T) {
	book, err := tabular.XLSX(awkward(), tabular.XLSXOptions{})
	if err != nil {
		t.Fatalf("XLSX: %v", err)
	}
	sheet := unzip(t, book, "xl/worksheets/sheet1.xml")

	for _, want := range []string{"المنتج", "إسمنت", "1,234.56", "-500.00"} {
		if !strings.Contains(sheet, want) {
			t.Errorf("the sheet does not carry %q", want)
		}
	}
	// Escaped, not raw: a bare & or < makes the part unparseable and the workbook unopenable.
	if strings.Contains(sheet, "Widget <tag>") {
		t.Error("a value with markup in it was written unescaped")
	}
	if !strings.Contains(sheet, "&lt;tag&gt;") {
		t.Error("the escaped form is not present either; the value was lost")
	}
	// The control byte is DROPPED rather than written. XML cannot carry it, and a workbook with
	// one in it opens as "unreadable content" — losing the whole sheet to save one invisible
	// character nobody typed on purpose.
	if strings.Contains(sheet, "\x07") {
		t.Error("a control character reached the workbook, which makes it unopenable")
	}
	if !strings.Contains(sheet, "bad") || !strings.Contains(sheet, "byte") {
		t.Error("the rest of the value was lost along with the control character")
	}
}

// TestTheWorkbookLaysOutRightToLeftWhenAsked
//
// The one thing CSV cannot express at all, and the reason this format exists for an Arabic shop:
// a right-to-left reader scanning a left-to-right sheet reads the columns backwards.
func TestTheWorkbookLaysOutRightToLeftWhenAsked(t *testing.T) {
	rtl, err := tabular.XLSX(awkward(), tabular.XLSXOptions{RightToLeft: true})
	if err != nil {
		t.Fatalf("XLSX: %v", err)
	}
	if !strings.Contains(unzip(t, rtl, "xl/worksheets/sheet1.xml"), `rightToLeft="1"`) {
		t.Error("a right-to-left workbook is not marked as one")
	}

	// And NOT when it is not asked for. A flag that is always on is a flag nobody has tested.
	ltr, err := tabular.XLSX(awkward(), tabular.XLSXOptions{})
	if err != nil {
		t.Fatalf("XLSX: %v", err)
	}
	if strings.Contains(unzip(t, ltr, "xl/worksheets/sheet1.xml"), `rightToLeft="1"`) {
		t.Error("an English workbook was laid out right to left")
	}
}

// TestColumnNamesRunPastZ
//
// A stock export with thirty columns needs AA, and the wrap is the one piece of arithmetic here.
// Getting it wrong puts two cells at the same reference, and Excel keeps the last — so a column
// silently disappears rather than the file failing.
func TestColumnNamesRunPastZ(t *testing.T) {
	wide := tabular.Sheet{Columns: make([]string, 30), Rows: [][]string{make([]string, 30)}}
	for i := range wide.Columns {
		wide.Columns[i] = "c"
	}
	book, err := tabular.XLSX(wide, tabular.XLSXOptions{})
	if err != nil {
		t.Fatalf("XLSX: %v", err)
	}
	sheet := unzip(t, book, "xl/worksheets/sheet1.xml")
	for _, ref := range []string{`r="A1"`, `r="Z1"`, `r="AA1"`, `r="AD1"`} {
		if !strings.Contains(sheet, ref) {
			t.Errorf("cell %s is missing; the column naming wraps wrongly", ref)
		}
	}
}

// TestASheetNameExcelWouldRefuseIsMadeSafe
//
// Excel rejects the FILE, not the name, when a sheet is called something it disallows. A report
// named "P&L: 2026/08" is an ordinary thing to want.
func TestASheetNameExcelWouldRefuseIsMadeSafe(t *testing.T) {
	book, err := tabular.XLSX(awkward(), tabular.XLSXOptions{
		SheetName: `P&L: 2026/08 [draft] ` + strings.Repeat("x", 40),
	})
	if err != nil {
		t.Fatalf("XLSX: %v", err)
	}
	name := unzip(t, book, "xl/workbook.xml")
	for _, forbidden := range []string{":", "/", "[", "]"} {
		if strings.Contains(name, `name="`) && strings.Contains(nameAttr(name), forbidden) {
			t.Errorf("the sheet name still contains %q, which Excel refuses", forbidden)
		}
	}
	if len([]rune(nameAttr(name))) > 31 {
		t.Errorf("the sheet name is %d characters; Excel's limit is 31",
			len([]rune(nameAttr(name))))
	}
}

// nameAttr reads the sheet name back out of the workbook, UNESCAPED.
//
// The raw attribute is XML, so "P&L" is stored as "P&amp;L" — five characters on the wire for
// three in the name. Measuring the escaped form makes a name that is exactly at Excel's limit
// look over it, which is what the first version of this test did.
func nameAttr(workbook string) string {
	start := strings.Index(workbook, `<sheet name="`)
	if start == -1 {
		return ""
	}
	rest := workbook[start+len(`<sheet name="`):]
	end := strings.Index(rest, `"`)
	if end == -1 {
		return ""
	}
	raw := rest[:end]

	replacer := strings.NewReplacer(
		"&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'", "&apos;", "'",
	)
	return replacer.Replace(raw)
}

// ── the document ────────────────────────────────────────────────────────────────

// TestADocumentIsAValidPackageAndWellFormed
func TestADocumentIsAValidPackageAndWellFormed(t *testing.T) {
	doc, err := tabular.DOCX(awkward(), tabular.DOCXOptions{
		Title: "Customer statement", Subtitle: "August 2026",
	})
	if err != nil {
		t.Fatalf("DOCX: %v", err)
	}

	for _, part := range []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml"} {
		body := unzip(t, doc, part)
		if err = xml.Unmarshal([]byte(body), new(struct {
			XMLName xml.Name
			Inner   []byte `xml:",innerxml"`
		})); err != nil {
			t.Errorf("%s is not well-formed XML: %v", part, err)
		}
	}

	body := unzip(t, doc, "word/document.xml")
	for _, want := range []string{"Customer statement", "August 2026", "المنتج", "1,234.56"} {
		if !strings.Contains(body, want) {
			t.Errorf("the document does not carry %q", want)
		}
	}
}

// TestAnArabicDocumentSetsAllThreeDirectionMarks
//
// # Why all three, asserted separately
//
// Word needs the SECTION right-to-left so text flows from the right, the TABLE right-to-left so
// column one is on the right, and each RUN marked rtl so the bidirectional algorithm handles
// mixed Arabic and digits correctly.
//
// Getting two of the three is worse than getting none: the document looks nearly right, and the
// one thing that is wrong — usually a column order or a trailing number — is the thing somebody
// reads a figure off. So each is asserted on its own rather than as "it contains rtl somewhere".
func TestAnArabicDocumentSetsAllThreeDirectionMarks(t *testing.T) {
	doc, err := tabular.DOCX(awkward(), tabular.DOCXOptions{
		Title: "كشف حساب", RightToLeft: true,
	})
	if err != nil {
		t.Fatalf("DOCX: %v", err)
	}
	body := unzip(t, doc, "word/document.xml")

	if !strings.Contains(body, "<w:bidi/></w:sectPr>") && !strings.Contains(body, "<w:sectPr><w:bidi/>") {
		t.Error("the SECTION is not right-to-left, so the page flows the wrong way")
	}
	if !strings.Contains(body, "<w:bidiVisual/>") {
		t.Error("the TABLE is not right-to-left, so column one is on the wrong side")
	}
	if !strings.Contains(body, "<w:rtl/>") {
		t.Error("no RUN is marked rtl, so mixed Arabic and digits will reorder wrongly")
	}
}

// TestAnEnglishDocumentIsNotSecretlyRightToLeft
//
// The other half of the flag. A direction that is always on is a direction nobody has tested, and
// the symptom — an English statement with its columns mirrored — reaches a customer.
func TestAnEnglishDocumentIsNotSecretlyRightToLeft(t *testing.T) {
	doc, err := tabular.DOCX(awkward(), tabular.DOCXOptions{Title: "Statement"})
	if err != nil {
		t.Fatalf("DOCX: %v", err)
	}
	body := unzip(t, doc, "word/document.xml")
	for _, mark := range []string{"<w:bidi/>", "<w:bidiVisual/>", "<w:rtl/>"} {
		if strings.Contains(body, mark) {
			t.Errorf("an English document carries %s", mark)
		}
	}
}

// TestAMalformedSheetIsRefusedByEveryFormat
//
// A short row silently shifts every value after it into the wrong column, and the file still
// opens — which is the worst possible outcome for a financial export. The CSV writer already
// refuses one; these must refuse the same thing, or the guarantee depends on which button the
// user pressed.
func TestAMalformedSheetIsRefusedByEveryFormat(t *testing.T) {
	short := tabular.Sheet{
		Columns: []string{"a", "b", "c"},
		Rows:    [][]string{{"1", "2"}},
	}
	if _, err := tabular.XLSX(short, tabular.XLSXOptions{}); err == nil {
		t.Error("the workbook accepted a row that does not match its columns")
	}
	if _, err := tabular.DOCX(short, tabular.DOCXOptions{}); err == nil {
		t.Error("the document accepted a row that does not match its columns")
	}
	if _, err := tabular.CSV(short); err == nil {
		t.Error("the CSV accepted a row that does not match its columns")
	}
}
