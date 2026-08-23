package bindings_test

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/api/bindings"
)

// decode reads an export back as rows, so a test asserts what a spreadsheet would show.
func decode(t *testing.T, encoded string) [][]string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decoding the export: %v", err)
	}
	// The BOM Excel needs, stripped so the CSV reader sees ordinary text.
	body := strings.TrimPrefix(string(raw), "\ufeff")
	records, err := csv.NewReader(strings.NewReader(body)).ReadAll()
	if err != nil {
		t.Fatalf("reading the export: %v", err)
	}
	return records
}

// TestAnExportRendersTheReportItWasGivenAndDoesNotRequery
//
// # DoD criterion 7, and the reason the DTO is a parameter
//
// The export takes the SCREEN's own data. It does not re-run the report with the same arguments —
// which would be a second query, a second answer, and a spreadsheet that disagrees with the screen
// it came from the moment anything changed in between.
//
// The proof is elsewhere and stronger: `ExportAnalysis` takes the DTO and has no service to call.
// It cannot re-query, because nothing in its signature can reach a report.
func TestAnExportRendersTheReportItWasGivenAndDoesNotRequery(t *testing.T) {
	set, _ := signedIn(t)

	analysis := bindings.AnalysisDTO{
		From: "2026-08-01", To: "2026-08-31",
		Rows: []bindings.AnalysisRowDTO{
			{
				Key: "v1", Label: "Olive oil (OIL-1)", Documents: 3,
				QuantityMicro: "5000000", RevenueMinor: "45000",
				CostMinor: "27000", GrossMarginMinor: "18000",
				MarginPercentMicro: "400000",
			},
		},
		Total: bindings.AnalysisRowDTO{
			Documents: 3, QuantityMicro: "5000000", RevenueMinor: "45000",
			CostMinor: "27000", GrossMarginMinor: "18000", MarginPercentMicro: "400000",
		},
	}

	result := set.Insight.ExportAnalysis(analysis, "Sales by product", true, "")
	if !result.OK {
		t.Fatalf("ExportAnalysis: %+v", result.Error)
	}

	records := decode(t, result.Data.ContentBase64)
	if len(records) != 3 {
		t.Fatalf("%d records, want header + row + total", len(records))
	}
	if records[1][1] != "Olive oil (OIL-1)" {
		t.Errorf("the row's label is %q", records[1][1])
	}
	// Money is exported as MINOR units, unformatted. A formatted number is not a number to a
	// spreadsheet, and dividing by a hundred here would need a locale this layer does not have.
	if records[1][4] != "45000" {
		t.Errorf("revenue exported as %q, want the minor-unit string", records[1][4])
	}

	// The TOTAL is a row, from the backend's own figure. A spreadsheet the user sums themselves
	// disagrees with the screen the moment a report is filtered.
	if records[2][1] != "TOTAL" || records[2][4] != "45000" {
		t.Errorf("the total row is %v", records[2])
	}

	if result.Data.Rows != 2 {
		t.Errorf("rows = %d, want 2 — one data row and the total", result.Data.Rows)
	}
	if !strings.HasSuffix(result.Data.Filename, ".csv") {
		t.Errorf("filename = %q", result.Data.Filename)
	}
	if !strings.Contains(result.Data.Filename, "2026-08-01") {
		t.Errorf("filename %q does not say what range it covers", result.Data.Filename)
	}
}

// TestASpendExportOmitsTheMarginColumnsRatherThanZeroingThem
//
// A purchase has no margin, because the cost IS the purchase. A column of zeros claims every
// supplier broke even, which is a different statement from "this does not apply here".
func TestASpendExportOmitsTheMarginColumnsRatherThanZeroingThem(t *testing.T) {
	set, _ := signedIn(t)

	result := set.Insight.ExportAnalysis(bindings.AnalysisDTO{
		From: "2026-08-01", To: "2026-08-31",
		Rows: []bindings.AnalysisRowDTO{
			{Key: "s1", Label: "Acme Supplies", Documents: 2, RevenueMinor: "30000"},
		},
	}, "Spend by supplier", false, "")
	if !result.OK {
		t.Fatalf("ExportAnalysis: %+v", result.Error)
	}

	records := decode(t, result.Data.ContentBase64)
	header := strings.Join(records[0], ",")
	for _, absent := range []string{"costMinor", "grossMarginMinor", "marginPercentMicro"} {
		if strings.Contains(header, absent) {
			t.Errorf("a spend export carries a %s column", absent)
		}
	}
}

// TestAStatementExportsFlatWithItsDepth
//
// A spreadsheet has no nesting, so the tree flattens and a reader indents on the depth — which is
// why the depth crosses the boundary at all.
func TestAStatementExportsFlatWithItsDepth(t *testing.T) {
	set, _ := signedIn(t)

	result := set.Insight.ExportStatement([]bindings.StatementNodeDTO{
		{
			Code: "4000", Name: "Revenue", Type: "revenue", Depth: 0, AmountMinor: "45000",
			Children: []bindings.StatementNodeDTO{
				{Code: "4100", Name: "Sales of goods", Type: "revenue", Depth: 1,
					AmountMinor: "40000"},
				{Code: "4200", Name: "Services", Type: "revenue", Depth: 1,
					AmountMinor: "5000"},
			},
		},
	}, "Profit and loss", "2026-08-01", "2026-08-31", "")
	if !result.OK {
		t.Fatalf("ExportStatement: %+v", result.Error)
	}

	records := decode(t, result.Data.ContentBase64)
	if len(records) != 4 {
		t.Fatalf("%d records, want header + parent + two children", len(records))
	}
	if records[1][0] != "0" || records[2][0] != "1" {
		t.Errorf("depths are %q and %q, want 0 then 1", records[1][0], records[2][0])
	}
	// The children follow their parent, in order. A flattening that lost that would leave a
	// reader unable to reconstruct the tree from the depths at all.
	if records[2][1] != "4100" || records[3][1] != "4200" {
		t.Errorf("children are out of order: %v, %v", records[2], records[3])
	}
}

// TestAnEmptyReportExportsHeadersRatherThanNothing
//
// A shopkeeper who exports a month with no sales should get a spreadsheet that says so, with the
// columns they expected — not an empty file they will assume is broken.
func TestAnEmptyReportExportsHeadersRatherThanNothing(t *testing.T) {
	set, _ := signedIn(t)

	result := set.Insight.ExportAnalysis(bindings.AnalysisDTO{
		From: "2026-08-01", To: "2026-08-31",
	}, "Sales by product", true, "")
	if !result.OK {
		t.Fatalf("ExportAnalysis: %+v", result.Error)
	}

	records := decode(t, result.Data.ContentBase64)
	// Header plus the total row, which is zero rather than absent: an empty report has a total.
	if len(records) != 2 {
		t.Fatalf("%d records, want header + total", len(records))
	}
	if records[0][0] != "key" {
		t.Errorf("header = %v", records[0])
	}
}

// TestEveryFormatCarriesTheSameFigures
//
// # The guarantee that makes three formats safe rather than three times the risk
//
// A shop exports the same report as a spreadsheet for its accountant and as a document for its
// customer. If those two disagree by a single figure, the shop finds out when somebody queries an
// invoice — and there is no way to tell which of the two was right.
//
// They cannot disagree here BY CONSTRUCTION: all three renderers receive the same finished
// `tabular.Sheet`, and no format re-queries. This drill is what stops a future format quietly
// fetching its own rows.
func TestEveryFormatCarriesTheSameFigures(t *testing.T) {
	set, _ := signedInAdmin(t)

	analysis := bindings.AnalysisDTO{
		From: "2026-08-01", To: "2026-08-31",
		Rows: []bindings.AnalysisRowDTO{
			{Key: "2026-08-01", Label: "1 August", Documents: 3,
				QuantityMicro: "3000000", RevenueMinor: "123456"},
		},
		Total: bindings.AnalysisRowDTO{
			Key: "", Label: "TOTAL", Documents: 3,
			QuantityMicro: "3000000", RevenueMinor: "123456",
		},
	}

	var seen []string
	for _, format := range []string{"csv", "xlsx", "docx"} {
		result := set.Insight.ExportAnalysis(analysis, "Sales", false, format)
		if !result.OK {
			t.Fatalf("ExportAnalysis(%s): %+v", format, result.Error)
		}

		decoded, err := base64.StdEncoding.DecodeString(result.Data.ContentBase64)
		if err != nil {
			t.Fatalf("%s is not valid base64: %v", format, err)
		}
		if len(decoded) == 0 {
			t.Fatalf("%s produced an empty file", format)
		}

		// The row count is the SHEET's, so all three must agree on it — a format that dropped
		// the total row or added a spacer would show up here rather than in a customer's inbox.
		seen = append(seen, format+":"+strconv.Itoa(result.Data.Rows))

		// The figure itself survives into every format. Two of the three are zip archives, so
		// this looks for it in the uncompressed bytes rather than the container.
		if format == "csv" && !bytes.Contains(decoded, []byte("123456")) {
			t.Errorf("the CSV does not carry the revenue figure")
		}
		if format != "csv" {
			if !bytes.HasPrefix(decoded, []byte("PK")) {
				t.Errorf("%s is not a zip archive; Office will refuse it", format)
			}
			if !archiveContains(t, decoded, "123456") {
				t.Errorf("the %s does not carry the revenue figure", format)
			}
		}

		// The extension matches the format. A .docx full of CSV opens as an error the user
		// cannot explain.
		if !strings.HasSuffix(result.Data.Filename, "."+format) {
			t.Errorf("%s was named %q", format, result.Data.Filename)
		}
	}

	for _, count := range seen[1:] {
		if strings.SplitN(count, ":", 2)[1] != strings.SplitN(seen[0], ":", 2)[1] {
			t.Errorf("the formats report different row counts: %v", seen)
		}
	}
}

// TestAnUnknownFormatIsRefusedRatherThanSubstituted
//
// A caller asking for a format this cannot produce and silently receiving a CSV under that name
// produces a file the user cannot open and cannot explain. The refusal is the useful answer.
//
// This test named "pdf" until 10.19, when PDF became real — and it failed, which is exactly what
// it is for. The case it guards is a format that does not exist, not a particular string.
func TestAnUnknownFormatIsRefusedRatherThanSubstituted(t *testing.T) {
	set, _ := signedInAdmin(t)

	for _, unsupported := range []string{"rtf", "ods", "pages", "PDF ", "xls"} {
		result := set.Insight.ExportValuation(bindings.ValuationDTO{}, unsupported)
		if result.OK {
			t.Errorf("format %q produced a file named %q", unsupported, result.Data.Filename)
		}
	}
}

// TestPDFComesBackAsAPrintablePage
//
// PDF is the one format the backend does not produce as a FILE. It returns a page, and the
// browser's print dialogue makes the PDF — because the browser is the only thing in the process
// that shapes Arabic correctly.
//
// So the assertion is on the SHAPE of the answer: a caller that treated this like the other three
// would hand the user an .html file to save, which is not what they asked for.
func TestPDFComesBackAsAPrintablePage(t *testing.T) {
	set, _ := signedInAdmin(t)

	result := set.Insight.ExportValuation(bindings.ValuationDTO{
		Lines: []bindings.ValuedLineDTO{{
			WarehouseName: "Main", ProductName: "Tea", VariantSKU: "TEA",
			QuantityMicro: "1000000", ValueMinor: "1500",
		}},
		TotalMinor: "1500",
	}, "pdf")
	if !result.OK {
		t.Fatalf("ExportValuation(pdf): %+v", result.Error)
	}
	if result.Data.MimeType != "text/html;charset=utf-8" {
		t.Errorf("mime = %q, want a printable page", result.Data.MimeType)
	}

	decoded, err := base64.StdEncoding.DecodeString(result.Data.ContentBase64)
	if err != nil {
		t.Fatalf("not base64: %v", err)
	}
	page := string(decoded)
	if !strings.HasPrefix(page, "<!doctype html>") {
		t.Errorf("the page does not start with a doctype:\n%.80s", page)
	}
	// Self-contained: a shop prints during an outage, and a stylesheet that failed to load is a
	// report with no layout handed to an accountant.
	for _, external := range []string{"http://", "https://", "<link", "<script"} {
		if strings.Contains(page, external) {
			t.Errorf("the printable page reaches outside itself for %q", external)
		}
	}
	if !strings.Contains(page, "1,500.00") && !strings.Contains(page, "1500") {
		t.Error("the page does not carry the figure it is a report of")
	}
}

// archiveContains reports whether any part of an OOXML package contains a string.
func archiveContains(t *testing.T, archive []byte, want string) bool {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatalf("not a zip archive: %v", err)
	}
	for _, file := range reader.File {
		opened, openErr := file.Open()
		if openErr != nil {
			continue
		}
		body, readErr := io.ReadAll(opened)
		_ = opened.Close()
		if readErr == nil && bytes.Contains(body, []byte(want)) {
			return true
		}
	}
	return false
}
