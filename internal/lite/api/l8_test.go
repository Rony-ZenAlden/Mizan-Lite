package api_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/api"
	"github.com/mizan-erp/mizan/internal/lite/guide"
	ownerdomain "github.com/mizan-erp/mizan/internal/lite/owner/domain"
	"github.com/mizan-erp/mizan/internal/lite/sheets"
)

func workbook(t *testing.T, rows ...[]string) string {
	t.Helper()
	sheet := sheets.Sheet{Name: "products", Rows: [][]sheets.Cell{{sheets.Text("head")}}}
	for _, r := range rows {
		var cells []sheets.Cell
		for _, v := range r {
			cells = append(cells, sheets.Text(v))
		}
		sheet.Rows = append(sheet.Rows, cells)
	}
	body, err := sheets.XLSX(sheets.Workbook{Sheets: []sheets.Sheet{sheet}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "products.xlsx")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestProductsImportFromExcelAllOrNothing(t *testing.T) {
	set, oil := tillShop(t)
	files := &fakeFiles{}
	set.SetFiles(files)
	before := len(set.Catalog.Products(api.ProductQueryDTO{}).Data)

	files.open = workbook(t,
		[]string{"رز مصري", "Egyptian rice", "6290000000011", "kg", "USD", "1.10", "25.5", "0.80"},
		[]string{"سكر", "", "", "كيلوغرام", "ليرة سورية", "12000", "", ""},
		[]string{"", "", "", "", "", "", "", ""},
		[]string{"شاي", "", "", "box", "دولار", "2.50", "10", ""},
		[]string{"ملح", "", oil.Barcode, "kg", "USD", "0.30", "", ""},
		[]string{"", "No Arabic", "", "kg", "USD", "1", "", ""},
		[]string{"عدس", "", "", "sack", "USD", "1", "", ""},
		[]string{"بن", "", "", "kg", "EUR", "1", "", ""},
		[]string{"هيل", "", "", "kg", "USD", "abc", "", ""},
	)
	preview := set.Catalog.ImportPreview()
	if !preview.OK {
		t.Fatal(preview.Error)
	}
	if len(preview.Data.Rows) != 2 || preview.Data.Rows[0].Row != 2 || preview.Data.Rows[1].UnitCode != "kg" || preview.Data.Rows[1].Currency != "SYP" {
		t.Fatalf("rows %+v", preview.Data.Rows)
	}
	got := map[int]string{}
	for _, p := range preview.Data.Problems {
		got[p.Row] = p.Column + " " + p.Code
	}
	want := map[int]string{5: "cost " + api.CodeImportCostNeeds, 6: "barcode", 7: "name_ar", 8: "unit " + api.CodeImportUnit, 9: "currency " + api.CodeImportCurrency, 10: "price"}
	for row, prefix := range want {
		if !strings.HasPrefix(got[row], prefix) {
			t.Errorf("row %d: %q, want %q…", row, got[row], prefix)
		}
	}
	if len(got) != len(want) {
		t.Errorf("problems %v", got)
	}
	if n := len(set.Catalog.Products(api.ProductQueryDTO{}).Data); n != before {
		t.Fatalf("a preview wrote %d products", n-before)
	}

	apply := api.ImportApplyInput{Path: files.open, Digest: preview.Data.Digest}
	if r := set.Catalog.ImportApply(apply); codeOf(t, r) != api.CodeImportHasErrors {
		t.Fatal("a workbook with problems applied")
	}

	files.open = workbook(t,
		[]string{"رز مصري", "Egyptian rice", "6290000000011", "kg", "USD", "1.10", "25.5", "0.80"},
		[]string{"سكر", "", "", "كيلوغرام", "ليرة سورية", "12000", "", ""},
	)
	clean := set.Catalog.ImportPreview()
	if !clean.OK || len(clean.Data.Problems) != 0 {
		t.Fatalf("clean %+v", clean)
	}
	if raw, _ := json.Marshal(clean.Data); !strings.Contains(string(raw), `"problems":[]`) {
		t.Fatalf("no problems must reach the screen as an empty list, not null: %s", raw)
	}
	apply = api.ImportApplyInput{Path: files.open, Digest: clean.Data.Digest}
	if r := set.Catalog.ImportApply(apply); codeOf(t, r) != ownerdomain.CodeRequired {
		t.Fatal("an import at the counter")
	}
	elevate(t, set)
	if r := set.Catalog.ImportApply(api.ImportApplyInput{Path: files.open, Digest: "stale"}); codeOf(t, r) != api.CodeImportChanged {
		t.Fatal("a changed workbook applied")
	}
	done := set.Catalog.ImportApply(apply)
	if !done.OK || done.Data.Created != 2 || done.Data.WithStock != 1 {
		t.Fatalf("apply %+v", done)
	}
	products := set.Catalog.Products(api.ProductQueryDTO{Text: "رز"}).Data
	if len(products) != 1 || products[0].Price != "1.10" {
		t.Fatalf("rice %+v", products)
	}
	var riceStock string
	for _, l := range set.Stock.Levels().Data {
		if l.ProductID == products[0].ID {
			riceStock = l.OnHand
		}
	}
	if riceStock != "25.500" {
		t.Fatalf("rice on hand %q", riceStock)
	}
	if again := set.Catalog.ImportApply(apply); again.OK {
		t.Fatal("the same workbook loaded twice")
	}
}

func TestTheImportTemplateImportsCleanly(t *testing.T) {
	set, _ := tillShop(t)
	files := &fakeFiles{save: filepath.Join(t.TempDir(), "template")}
	set.SetFiles(files)
	saved := set.Catalog.ImportTemplate()
	if !saved.OK || !strings.HasSuffix(saved.Data.Path, ".xlsx") {
		t.Fatalf("template %+v", saved)
	}
	files.open = saved.Data.Path
	preview := set.Catalog.ImportPreview()
	if !preview.OK || len(preview.Data.Problems) != 0 || len(preview.Data.Rows) != 2 {
		t.Fatalf("the template's examples: %+v", preview.Data)
	}
	if cancelled := func() api.ImportPreviewDTO { files.open = ""; return set.Catalog.ImportPreview().Data }(); !cancelled.Cancelled {
		t.Fatal("a closed dialog")
	}
}

func TestAboutGuidesAndTheSupportFile(t *testing.T) {
	set, _ := tillShop(t)
	app := api.Graph(set)
	files := &fakeFiles{}
	set.SetFiles(files)
	about := set.App.About()
	if !about.OK || about.Data.Version != "1.2.3-test" || about.Data.DataDir == "" || !strings.Contains(about.Data.Notices, "SIL Open Font License") ||
		len(about.Data.Guides) != len(guide.Sources) {
		t.Fatalf("about %+v", about.Data.Guides)
	}
	files.save = filepath.Join(t.TempDir(), "guide")
	g := set.App.SaveGuide("shop-guide-ar")
	if raw, _ := os.ReadFile(g.Data.Path); !g.OK || !bytes.HasPrefix(raw, []byte("%PDF")) || !strings.HasSuffix(g.Data.Path, ".pdf") {
		t.Fatalf("guide %+v", g)
	}
	if r := set.App.SaveGuide("../../etc/passwd"); r.OK {
		t.Fatal("an unknown guide saved")
	}

	if err := os.WriteFile(filepath.Join(app.Paths.Logs, "mizan-lite.log"), []byte("started\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	files.save = filepath.Join(t.TempDir(), "support.zip")
	plain := set.App.SaveSupportFile(false)
	if !plain.OK {
		t.Fatal(plain.Error)
	}
	names, content := unzipAll(t, plain.Data.Path)
	if !names["diagnostics.json"] || !names["logs/mizan-lite.log"] {
		t.Fatalf("parts %v", names)
	}
	for name := range names {
		if strings.HasPrefix(name, "database/") {
			t.Fatal("a database in a support file nobody asked for")
		}
	}
	var hash string
	if err := app.DB.Reader(t.Context()).QueryRowContext(t.Context(), `SELECT pin_hash FROM owner_credentials LIMIT 1`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{testPIN, hash} {
		if secret != "" && strings.Contains(content, secret) {
			t.Fatalf("the support file holds a secret: %q", secret)
		}
	}
	if !strings.Contains(content, `"schemaVersion"`) || !strings.Contains(content, `"verifiers"`) {
		t.Fatalf("diagnostics %s", content[:min(400, len(content))])
	}

	if r := set.App.SaveSupportFile(true); codeOf(t, r) != ownerdomain.CodeRequired {
		t.Fatal("the database in a support file at the counter")
	}
	elevate(t, set)
	withDB := set.App.SaveSupportFile(true)
	names, _ = unzipAll(t, withDB.Data.Path)
	found := false
	for name := range names {
		found = found || strings.HasPrefix(name, "database/") && strings.HasSuffix(name, ".db")
	}
	if !withDB.OK || !found {
		t.Fatalf("with database %+v %v", withDB, names)
	}
}

func unzipAll(t *testing.T, path string) (map[string]bool, string) {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	names := map[string]bool{}
	var all strings.Builder
	for _, f := range zr.File {
		names[f.Name] = true
		if strings.HasPrefix(f.Name, "database/") {
			continue
		}
		rc, _ := f.Open()
		body, _ := io.ReadAll(rc)
		rc.Close()
		all.Write(body)
	}
	return names, all.String()
}
