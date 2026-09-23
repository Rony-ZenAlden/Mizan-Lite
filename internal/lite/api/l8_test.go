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
	settingsdomain "github.com/mizan-erp/mizan/internal/lite/settings/domain"
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
	// (An import no longer asks for the PIN — owner.ReservedActs, 2026-09-16. It is applied once, below, because applying
	// it twice would refuse on duplicate names.)
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

	// The database may be put in a support file at the counter now (2026-09-16): the owner asked for the PIN to guard a
	// restore and nothing else. It is still only included when the ticked box asks for it.
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

// TestEveryOwnersActGoesThroughAtTheCounterWithoutAPIN is the owner's decision of 2026-09-16 held at the boundary the
// frontend actually calls: a single-computer shop with no login should not be stopped for a PIN to void a receipt, give a
// discount, take an expense out of the drawer, read a report or export it. What stays reserved is the pair that replaces
// the shop's books wholesale — a restore, and bringing in a backup file to restore from.
//
// The acts are still written to the owner's history: the record is what survives the gate.
func TestEveryOwnersActGoesThroughAtTheCounterWithoutAPIN(t *testing.T) {
	set, oil := tillShop(t)
	elevate(t, set)
	if r := set.Stock.Opening(api.ReceiveInput{ProductID: oil.ID, Quantity: "20", CostMode: "total", Cost: "40", Currency: "USD"}); !r.OK {
		t.Fatal(r.Error)
	}
	abu := set.Customers.Create(api.CustomerInput{Name: "أبو محمد"}).Data
	set.Owner.EndElevation()
	if s := set.Owner.Status(); s.Data.ElevatedSeconds != 0 {
		t.Fatal("this test must run at the counter, not in owner mode")
	}

	// A sale, then the acts a shop does all day — none of them elevated.
	cart := api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "2"}}}
	sale := set.Till.Checkout(api.CheckoutInput{Cart: cart, Token: quoted(t, set, cart).Token})
	if !sale.OK {
		t.Fatal(sale.Error)
	}
	discounted := api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "1", DiscountPercent: "10"}}}
	if r := set.Till.Checkout(api.CheckoutInput{Cart: discounted, Token: quoted(t, set, discounted).Token}); !r.OK {
		t.Fatalf("a discount at the counter = %+v", r.Error)
	}
	if r := set.Sales.Void(api.VoidInput{SaleID: sale.Data.ID, Reason: "أعاد الزيت"}); !r.OK {
		t.Fatalf("a void at the counter = %+v", r.Error)
	}
	if r := set.Cash.Record(api.CashRecordInput{Kind: "expense", Currency: "SYP", Amount: "25000", Category: "electricity", FromDrawer: true}); !r.OK {
		t.Fatalf("an expense at the counter = %+v", r.Error)
	}
	if r := set.Cash.Record(api.CashRecordInput{Kind: "withdrawal", Currency: "SYP", Amount: "10000"}); !r.OK {
		t.Fatalf("a withdrawal at the counter = %+v", r.Error)
	}
	if r := set.Customers.Opening(api.DebtAmountInput{CustomerID: abu.ID, Currency: "SYP", Amount: "50000", Note: "صفحة 3"}); !r.OK {
		t.Fatalf("an opening balance at the counter = %+v", r.Error)
	}
	if r := set.Customers.WriteOff(api.DebtAmountInput{CustomerID: abu.ID, Currency: "SYP", Amount: "10000", Note: "خصم"}); !r.OK {
		t.Fatalf("a write-off at the counter = %+v", r.Error)
	}
	if r := set.Stock.Adjust(api.AdjustInput{ProductID: oil.ID, Direction: "out", Quantity: "1", Reason: "expired"}); !r.OK {
		t.Fatalf("stock written off at the counter = %+v", r.Error)
	}
	if r := set.Stock.CorrectCost(api.CorrectCostInput{ProductID: oil.ID, AverageCost: "2.10", Note: "فاتورة"}); !r.OK {
		t.Fatalf("the average cost corrected at the counter = %+v", r.Error)
	}
	if r := set.FX.SetRate(api.SetRateInput{Rate: "15500", Note: "السوق"}); !r.OK {
		t.Fatalf("the rate at the counter = %+v", r.Error)
	}

	// The figures, which the owner asked to be readable without a PIN.
	for name, ok := range map[string]bool{
		"day":      set.Reports.Day("").OK,
		"month":    set.Reports.Month("").OK,
		"products": set.Reports.Products(api.RangeInput{}).OK,
		"stock":    set.Reports.Stock(api.RangeInput{}).OK,
		"drawer":   set.Cash.Drawer("").OK,
		"value":    set.Stock.Valuation().OK,
	} {
		if !ok {
			t.Errorf("%s was refused at the counter", name)
		}
	}
	if d := set.Cash.Drawer(""); !d.Data.OwnerView {
		t.Error("the drawer is still the counter's cut-down view")
	}

	// Every one of those acts is in the owner's history.
	events := set.Owner.Events(200)
	acts := 0
	for _, e := range events.Data {
		if e.Kind == "guarded_act" {
			acts++
		}
	}
	if acts < 9 {
		t.Errorf("acts recorded = %d, want at least 9 — the history is what survives the gate", acts)
	}

	// And the reserved pair still asks, exactly as before: a real backup, refused at the counter.
	taken := set.Backups.TakeNow()
	if !taken.OK {
		t.Fatal(taken.Error)
	}
	if code := codeOf(t, set.Backups.Restore(taken.Data.Name)); code != ownerdomain.CodeRequired {
		t.Fatalf("a restore at the counter = %s", code)
	}
}

// TestTheBackupFrequencyIsTheShopsToChoose is the owner's request of 2026-09-16: a shop that does not want a backup every
// day may ask for weekly, monthly, or none at all — and the backups that save it from an upgrade, a restore or a close
// still happen whatever it chose.
func TestTheBackupFrequencyIsTheShopsToChoose(t *testing.T) {
	set, _ := tillShop(t)

	if st := set.Backups.Status(); !st.OK || st.Data.Every != "daily" {
		t.Fatalf("a fresh shop is backed up daily, as it always was: %+v", st.Data)
	}
	for _, every := range []string{"weekly", "monthly", "manual", "daily"} {
		st := set.Backups.SetBackupEvery(every)
		if !st.OK || st.Data.Every != every {
			t.Fatalf("SetBackupEvery(%q) = %+v", every, st)
		}
	}
	if code := codeOf(t, set.Backups.SetBackupEvery("hourly")); code != settingsdomain.CodeInvalidBackupEvery {
		t.Fatalf("an unknown frequency = %s", code)
	}
	// Each change is in the owner's history, like every other setting.
	events := set.Owner.Events(50)
	changes := 0
	for _, e := range events.Data {
		if e.Action == api.ActBackupEvery {
			changes++
		}
	}
	if changes != 4 {
		t.Fatalf("frequency changes recorded = %d, want 4", changes)
	}
	// Taking one by hand is never governed by the setting.
	if st := set.Backups.SetBackupEvery("manual"); !st.OK {
		t.Fatal(st.Error)
	}
	if taken := set.Backups.TakeNow(); !taken.OK {
		t.Fatalf("a backup by hand under \"manual only\" = %+v", taken.Error)
	}
}

// TestTheShopsHeaderIsOnEveryPrintedThing is the owner's request of 2026-09-16: the shop's name, address and telephone are
// saved once and appear the same on a receipt and on an A4 report — on paper and in a workbook. Since 0.10.0 they are
// saved under Settings > Store information, with the city and the logo.
func TestTheShopsHeaderIsOnEveryPrintedThing(t *testing.T) {
	set, oil := tillShop(t)
	files := &fakeFiles{}
	set.SetFiles(files)
	if saved := set.Printers.Save(api.PrinterSettingsInput{Printer: "XP-80", PaperMM: "80", Path: "driver", AutoPrint: "none",
		Footer: "أهلاً بكم"}); !saved.OK {
		t.Fatal(saved.Error)
	}
	shop := set.Settings.SaveShop(api.ShopInput{Name: "بقالية المونة", Phone: "0933 123 456", City: "عفرين", Address: "شارع القوتلي، عفرين"})
	if !shop.OK {
		t.Fatal(shop.Error)
	}
	// Saved once and kept: read back through a second call, as a restart would.
	if again := set.Settings.Shop(); again.Data.Phone != "0933 123 456" || again.Data.Address != "شارع القوتلي، عفرين" || again.Data.City != "عفرين" {
		t.Fatalf("the shop's header was not kept: %+v", again.Data)
	}
	if again := set.Printers.Settings(); again.Data.Footer != "أهلاً بكم" {
		t.Fatalf("the receipt's footer was not kept: %+v", again.Data)
	}

	cart := api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "1"}}}
	sale := set.Till.Checkout(api.CheckoutInput{Cart: cart, Token: quoted(t, set, cart).Token})
	if !sale.OK {
		t.Fatal(sale.Error)
	}

	// On the receipt.
	receipt, err := api.PrintDocument(set, "sale", sale.Data.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"بقالية المونة", "شارع القوتلي، عفرين", "0933 123 456", "أهلاً بكم"} {
		if !strings.Contains(textOf(receipt), want) {
			t.Errorf("the receipt does not carry %q", want)
		}
	}

	// And on an A4 report, which used to carry the name alone.
	dir := t.TempDir()
	files.save = filepath.Join(dir, "day.pdf")
	if r := set.Export.Report(api.ExportReportInput{Kind: "day", Format: "pdf"}); !r.OK {
		t.Fatal(r.Error)
	}
	pdf, err := os.ReadFile(files.save)
	if err != nil || len(pdf) == 0 {
		t.Fatalf("the report was not written: %v", err)
	}
	day, err := api.ExportDocument(set, "report", api.ExportReportInput{Kind: "day", Format: "pdf"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"بقالية المونة", "شارع القوتلي، عفرين", "0933 123 456"} {
		if !strings.Contains(textOf(day), want) {
			t.Errorf("the A4 report does not carry %q", want)
		}
	}
}
