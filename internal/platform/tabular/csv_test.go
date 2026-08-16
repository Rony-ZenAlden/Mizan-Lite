package tabular_test

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/platform/tabular"
)

// TestExcelCanReadArabic
//
// # The byte-order mark is not decoration
//
// Excel on Windows reads a UTF-8 file as the system's legacy code page unless it begins with a
// BOM — so an Arabic product name opens as mojibake for exactly the users this application is
// built for. Every other consumer tolerates the BOM; Excel does not tolerate its absence.
func TestExcelCanReadArabic(t *testing.T) {
	out, err := tabular.CSV(tabular.Sheet{
		Columns: []string{"الصنف", "القيمة"},
		Rows:    [][]string{{"زيت زيتون", "4500"}},
	})
	if err != nil {
		t.Fatalf("CSV: %v", err)
	}

	if !bytes.HasPrefix(out, []byte{0xEF, 0xBB, 0xBF}) {
		t.Error("no byte-order mark — Excel on Windows will render this as mojibake, for the " +
			"users this application exists for")
	}

	// And it is still valid CSV once the mark is stripped.
	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(out, []byte{0xEF, 0xBB, 0xBF})))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("the output is not readable CSV: %v", err)
	}
	if len(records) != 2 || records[1][0] != "زيت زيتون" {
		t.Errorf("round trip lost the value: %v", records)
	}
}

// TestACellThatWouldBecomeAFormulaIsDefused
//
// # Why an export has to think about this
//
// A cell beginning with `=`, `+`, `-` or `@` is a FORMULA to Excel, LibreOffice and Google
// Sheets. `=1+1` displays as 2, which is merely wrong; `=cmd|'/c calc'!A1` is a command the
// spreadsheet will offer to run.
//
// The values come from a shop's own data — a product named "-- clearance --", a note starting
// "+974…" — so this is not primarily about an attacker. It is about an export that silently
// changes what the data SAYS.
func TestACellThatWouldBecomeAFormulaIsDefused(t *testing.T) {
	out, err := tabular.CSV(tabular.Sheet{
		Columns: []string{"name", "note"},
		Rows: [][]string{
			{"-- clearance --", "+974 5555 1234"},
			{"=1+1", "@channel"},
			{"Ordinary", "nothing special"},
		},
	})
	if err != nil {
		t.Fatalf("CSV: %v", err)
	}

	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(out, []byte{0xEF, 0xBB, 0xBF})))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}

	for _, row := range records[1:3] {
		for _, cell := range row {
			if !strings.HasPrefix(cell, "'") {
				t.Errorf("cell %q would be evaluated as a formula", cell)
			}
		}
	}
	// An ordinary value is NOT touched. Prefixing everything would be safe and would also put an
	// apostrophe in front of every product name in the file.
	if records[3][0] != "Ordinary" {
		t.Errorf("an ordinary value was altered: %q", records[3][0])
	}
}

// TestARowThatDoesNotMatchItsColumnsIsRefused
//
// A short row silently shifts every value after it into the wrong column, and the file still
// opens — so the reader sees a cost under a quantity heading and has no way to know.
func TestARowThatDoesNotMatchItsColumnsIsRefused(t *testing.T) {
	_, err := tabular.CSV(tabular.Sheet{
		Columns: []string{"a", "b", "c"},
		Rows:    [][]string{{"1", "2", "3"}, {"1", "2"}},
	})
	if code := errs.CodeOf(err); code != tabular.CodeWriteFailed {
		t.Fatalf("code = %q, want %q", code, tabular.CodeWriteFailed)
	}
	typed, _ := errs.AsError(err)
	if typed.Params["row"] != "2" {
		t.Errorf("the error does not say which row is wrong: %v", typed.Params)
	}

	if _, err = tabular.CSV(tabular.Sheet{}); err == nil {
		t.Error("a sheet with no columns was accepted")
	}
}

// TestAnEmptyReportIsAFileWithHeadersAndNoRows
//
// Not an error, and not an empty file. A shopkeeper who exports a month with no sales should get
// a spreadsheet that says so, with the columns they expected.
func TestAnEmptyReportIsAFileWithHeadersAndNoRows(t *testing.T) {
	out, err := tabular.CSV(tabular.Sheet{Columns: []string{"date", "revenue"}})
	if err != nil {
		t.Fatalf("CSV: %v", err)
	}

	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(out, []byte{0xEF, 0xBB, 0xBF})))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if len(records) != 1 || records[0][1] != "revenue" {
		t.Errorf("an empty report produced %v, want one header row", records)
	}
}

// TestAFilenameCannotBecomeAPath
//
// The name reaches a filesystem. A report called "Sales / Margin" must not produce a directory.
func TestAFilenameCannotBecomeAPath(t *testing.T) {
	for _, test := range []struct{ report, from, to, want string }{
		// Spaces become hyphens, which keeps the name readable — "Sales-by-product" is what a
		// shopkeeper will look for in their downloads folder.
		{"Sales by product", "2026-08-01", "2026-08-31",
			"Sales-by-product_2026-08-01_2026-08-31.csv"},
		// Separators become hyphens and the leading run is trimmed, so a traversal collapses to
		// an ordinary name rather than an escaped one.
		{"../../etc/passwd", "", "", "etc-passwd.csv"},
		{"", "", "", "export.csv"},
		{"...", "", "", "export.csv"},
	} {
		got := tabular.Filename(test.report, test.from, test.to)
		if strings.ContainsAny(got, `/\`) {
			t.Errorf("Filename(%q) = %q, which is a path", test.report, got)
		}
		if !strings.HasSuffix(got, ".csv") {
			t.Errorf("Filename(%q) = %q, which is not a CSV", test.report, got)
		}
		if got != test.want {
			t.Errorf("Filename(%q, %q, %q) = %q, want %q",
				test.report, test.from, test.to, got, test.want)
		}
	}
}
