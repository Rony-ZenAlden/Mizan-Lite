package api_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/api"
	"github.com/mizan-erp/mizan/internal/lite/documents"
	"github.com/mizan-erp/mizan/internal/lite/printers"
	"github.com/mizan-erp/mizan/internal/lite/printing"
)

type fakeFiles struct {
	mu      sync.Mutex
	save    string // the path the Save dialog answers; "" cancels
	open    string
	folder  string
	asked   []string
	showed  []string
	failing bool
}

func (f *fakeFiles) SaveFile(_ context.Context, title, name string, filters []api.Filter) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, name)
	if f.failing {
		return "", errors.New("dialog crashed")
	}
	return f.save, nil
}

func (f *fakeFiles) OpenFile(context.Context, string, []api.Filter) (string, error) {
	return f.open, nil
}
func (f *fakeFiles) PickFolder(context.Context, string) (string, error) { return f.folder, nil }
func (f *fakeFiles) ShowInFolder(path string) error                     { f.showed = append(f.showed, path); return nil }

type fakePrinter struct {
	mu    sync.Mutex
	jobs  []printers.Job
	fail  error
	names []printers.Printer
}

func (p *fakePrinter) List(context.Context) ([]printers.Printer, error) { return p.names, nil }

func (p *fakePrinter) Send(_ context.Context, job printers.Job) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if job.Printer == "" {
		return errs.Conflict(printers.CodeNoPrinter, "none")
	}
	if p.fail != nil {
		return p.fail
	}
	p.jobs = append(p.jobs, job)
	return nil
}

func saleOnTheTill(t *testing.T, set *api.Set, oil api.ProductDTO, cart api.CartInput) api.SaleDTO {
	t.Helper()
	sold := set.Till.Checkout(api.CheckoutInput{Cart: cart, Token: quoted(t, set, cart).Token})
	if !sold.OK {
		t.Fatal(sold.Error)
	}
	return sold.Data
}

func TestReceiptsPrintThroughTheChosenPathAndCopiesAreStamped(t *testing.T) {
	set, oil := tillShop(t)
	printer := &fakePrinter{names: []printers.Printer{{Name: "Xprinter XP-80", Default: true}}}
	set.SetPrinters(printer)
	elevate(t, set)
	if r := set.Stock.Opening(api.ReceiveInput{ProductID: oil.ID, Quantity: "10", CostMode: "total", Cost: "20", Currency: "USD"}); !r.OK {
		t.Fatal(r.Error)
	}
	sale := saleOnTheTill(t, set, oil, api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "2"}}})

	if r := set.Print.Sale(sale.ID); codeOf(t, r) != printers.CodeNoPrinter {
		t.Fatal("printing with no printer chosen")
	}
	if list := set.Printers.List(); !list.OK || len(list.Data) != 1 || !list.Data[0].Default {
		t.Fatalf("printers %+v", list)
	}
	set.Owner.EndElevation()
	settings := api.PrinterSettingsInput{Printer: "Xprinter XP-80", PaperMM: "80", Path: "raw", AutoPrint: "credit", Drawer: true, Phone: "0933 123 456", Footer: "أهلاً بكم"}
	// Printer settings are saved at the counter since 2026-09-16 (owner.ReservedActs), and recorded in the owner's history.
	if r := set.Printers.Save(settings); !r.OK || r.Data.Printer != "Xprinter XP-80" || r.Data.PaperMM != 80 || !r.Data.Drawer {
		t.Fatalf("save without owner mode = %+v", r)
	}
	if s := set.Printers.Settings(); s.Data.Printer != "Xprinter XP-80" {
		t.Fatalf("the saved settings were not kept: %+v", s.Data)
	}

	first := set.Print.Sale(sale.ID)
	if !first.OK || first.Data.CopyNo != 1 || first.Data.Path != "raw" || len(printer.jobs) != 1 {
		t.Fatalf("first print %+v", first)
	}
	job := printer.jobs[0]
	if !bytes.HasPrefix(job.Escpos, []byte{0x1B, 0x40, 0x1D, 0x76, 0x30}) || !bytes.HasSuffix(job.Escpos, []byte{0x1B, 0x70, 0x00, 0x19, 0xFA}) ||
		job.DriverPDF != nil || job.Raster.Bounds().Dx() != 576 {
		t.Fatalf("a cash sale's raw job opens the drawer: % X", job.Escpos[len(job.Escpos)-12:])
	}
	preview := set.Print.Preview(api.PreviewInput{Kind: "sale", ID: sale.ID})
	raw, _ := base64.StdEncoding.DecodeString(preview.Data.PNG)
	img, err := png.Decode(bytes.NewReader(raw))
	if !preview.OK || err != nil || img.Bounds().Dx() != 576 || preview.Data.CopyNo != 2 || preview.Data.Height != img.Bounds().Dy() {
		t.Fatalf("preview %+v %v", preview.Data.CopyNo, err)
	}
	doc, _ := api.PrintDocument(set, "sale", sale.ID)
	if !hasStamp(doc) {
		t.Fatal("the reprint's document carries no copy stamp")
	}
	if second := set.Print.Sale(sale.ID); !second.OK || second.Data.CopyNo != 2 {
		t.Fatalf("a reprint is copy 2: %+v", second)
	}

	// The driver path sends the same bitmap as a page; a failure is recorded and returned.
	elevate(t, set)
	settings.Path, settings.PaperMM = "driver", "58"
	set.Printers.Save(settings)
	set.Owner.EndElevation()
	if r := set.Print.Sale(sale.ID); !r.OK || !bytes.HasPrefix(printer.jobs[2].DriverPDF, []byte("%PDF-1.7")) || printer.jobs[2].Raster.Bounds().Dx() != 384 {
		t.Fatalf("driver path %+v", r)
	}
	printer.fail = errs.Conflict(printers.CodeSendFailed, "offline")
	if r := set.Print.Sale(sale.ID); codeOf(t, r) != printers.CodeSendFailed {
		t.Fatal("a failed print")
	}
	jobs, _ := api.Graph(set).Printing.Jobs(context.Background(), 10)
	if len(jobs) != 4 || jobs[0].Sent || jobs[0].ErrorCode != printers.CodeSendFailed || jobs[0].CopyNo != 4 || jobs[1].Kind != printing.KindSale {
		t.Fatalf("jobs %+v", jobs)
	}
	printer.fail = nil
	if test := set.Printers.Test(); !test.OK || test.Data.CopyNo != 1 {
		t.Fatalf("test page %+v", test)
	}
	if r := set.Print.Preview(api.PreviewInput{Kind: "invoice"}); codeOf(t, r) != api.CodeNotPrintable {
		t.Fatal("an unknown document")
	}
}

func hasStamp(doc documents.Document) bool {
	for _, b := range doc.Blocks {
		if _, ok := b.(documents.Stamp); ok {
			return true
		}
	}
	return false
}

func TestVouchersAreNumberedAndARefundPrintsOneToo(t *testing.T) {
	set, oil := tillShop(t)
	set.SetPrinters(&fakePrinter{names: []printers.Printer{{Name: "P"}}})
	abu := set.Customers.Create(api.CustomerInput{Name: "أبو محمد"}).Data
	cart := api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "4"}}, Settlement: "USD", Payment: "credit", CustomerID: abu.ID}
	credit := saleOnTheTill(t, set, oil, cart)
	doc, err := api.PrintDocument(set, "sale", credit.ID)
	if err != nil || !hasSignature(doc) {
		t.Fatalf("a credit receipt has a signature line: %v", err)
	}
	pay := api.PaymentInput{CustomerID: abu.ID, Currency: "USD", TenderCurrency: "USD", Amount: "5"}
	q := set.Customers.QuotePayment(pay)
	pay.Token = q.Data.Token
	entry := set.Customers.RecordPayment(pay)
	if !entry.OK {
		t.Fatal(entry.Error)
	}
	voucher, _ := api.PrintDocument(set, "entry", entry.Data.ID)
	if !strings.Contains(textOf(voucher), "\u20661\u2069") {
		t.Fatalf("the voucher carries no number 1: %s", textOf(voucher))
	}
	if r := set.Print.Entry(credit.ID); codeOf(t, r) != "lite.customers.entry_not_found" {
		t.Fatal("a sale's id is not an entry")
	}
	elevate(t, set)
	set.Sales.Void(api.VoidInput{SaleID: credit.ID, Reason: "خطأ"})
	refund := set.Customers.Refund(api.RefundInput{CustomerID: abu.ID, Currency: "USD", All: true, Reason: "رد"})
	if !refund.OK {
		t.Fatal(refund.Error)
	}
	set.Owner.EndElevation()
	// A refund's voucher is no longer the owner's (2026-09-16): at the counter it reaches the printer, and stops only
	// because this shop has chosen none.
	if r := set.Print.Entry(refund.Data.ID); codeOf(t, r) != printers.CodeNoPrinter {
		t.Fatalf("a refund voucher at the counter = %+v", r)
	}
	refundDoc, err := api.PrintDocument(set, "entry", refund.Data.ID)
	if err != nil || !strings.Contains(textOf(refundDoc), "\u20662\u2069") {
		t.Fatalf("the refund voucher is number 2: %v %s", err, textOf(refundDoc))
	}
	for _, d := range []documents.Document{doc, voucher, refundDoc} {
		if bad := documents.Unisolated(d); len(bad) > 0 {
			t.Fatalf("figures not isolated: %q", bad)
		}
	}
}

func hasSignature(doc documents.Document) bool {
	for _, b := range doc.Blocks {
		if _, ok := b.(documents.Signature); ok {
			return true
		}
	}
	return false
}

func textOf(doc documents.Document) string {
	var b strings.Builder
	for _, block := range doc.Blocks {
		switch x := block.(type) {
		case documents.Title:
			b.WriteString(x.Text + " " + x.Subtitle + "\n")
		case documents.Paragraph:
			b.WriteString(x.Text + "\n")
		case documents.Heading:
			b.WriteString(x.Text + "\n")
		case documents.Pairs:
			for _, p := range x.Rows {
				b.WriteString(p.Label + " = " + p.Value.Text + "\n")
			}
		case documents.Table:
			b.WriteString(strings.Join(x.Headings, " | ") + "\n")
			for _, r := range append(x.Rows, x.Total) {
				for _, c := range r {
					b.WriteString(c.Text + " | ")
				}
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

func unzip(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, f := range z.File {
		r, _ := f.Open()
		body, _ := io.ReadAll(r)
		all.Write(body)
	}
	return all.String()
}

func TestEveryExportCarriesTheScreensFiguresAndSavesWhereChosen(t *testing.T) {
	set, oil := tillShop(t)
	files := &fakeFiles{}
	// Exporting reaches the Save dialog without a PIN since 2026-09-16; with no dialogs wired it says so.
	if r := set.Export.Report(api.ExportReportInput{Kind: "day", Format: "xlsx"}); codeOf(t, r) != api.CodeNoDialogs {
		t.Fatalf("a report exported without owner mode = %+v", r)
	}
	elevate(t, set)
	if r := set.Export.Report(api.ExportReportInput{Kind: "day", Format: "xlsx"}); codeOf(t, r) != api.CodeNoDialogs {
		t.Fatal("no dialogs")
	}
	set.SetFiles(files)
	set.Stock.Opening(api.ReceiveInput{ProductID: oil.ID, Quantity: "10", CostMode: "total", Cost: "20", Currency: "USD"})
	saleOnTheTill(t, set, oil, api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "2"}}})
	abu := set.Customers.Create(api.CustomerInput{Name: "=SUM(A1) أبو محمد", Phone: "0933"}).Data
	saleOnTheTill(t, set, oil, api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "1"}}, Settlement: "USD", Payment: "credit", CustomerID: abu.ID})

	// Cancelled: nothing written.
	if r := set.Export.Report(api.ExportReportInput{Kind: "day", Format: "pdf"}); !r.OK || !r.Data.Cancelled {
		t.Fatalf("cancel %+v", r)
	}
	if !strings.HasSuffix(files.asked[0], ".pdf") || !strings.Contains(files.asked[0], "بقالية المونة") || strings.ContainsAny(files.asked[0], "/\\:\u2066") {
		t.Fatalf("proposed name %q", files.asked[0])
	}
	dir := t.TempDir()
	day := set.Reports.Day("").Data
	files.save = filepath.Join(dir, "day")
	saved := set.Export.Report(api.ExportReportInput{Kind: "day", Format: "xlsx"})
	if !saved.OK || saved.Data.Path != files.save+".xlsx" || saved.Data.Bytes == 0 {
		t.Fatalf("saved %+v", saved)
	}
	book := unzip(t, saved.Data.Path)
	for _, figure := range []string{day.Profit.RevenueUSD, day.Profit.CostLocal, day.Profit.ProfitLocal, day.NetUSD} {
		if !strings.Contains(book, "<v>"+figure+"</v>") {
			t.Fatalf("the workbook lacks the screen's %s", figure)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".mizan-lite-*")); len(left) != 0 {
		t.Fatal("a temporary file was left")
	}
	files.save = filepath.Join(dir, "day.pdf")
	pdf := set.Export.Report(api.ExportReportInput{Kind: "day", Format: "pdf"})
	if raw, _ := os.ReadFile(pdf.Data.Path); !pdf.OK || !bytes.HasPrefix(raw, []byte("%PDF-1.7")) {
		t.Fatalf("pdf %+v", pdf)
	}
	if r := set.Export.ShowInFolder(pdf.Data.Path); !r.OK || files.showed[0] != pdf.Data.Path {
		t.Fatal("show in folder")
	}

	// Every export: its document's figures are the screen's and every figure is isolated.
	products := set.Reports.Products(api.RangeInput{}).Data
	stockReport := set.Reports.Stock(api.RangeInput{}).Data
	for _, c := range []struct {
		kind string
		in   any
		want []string
	}{
		{"day", api.ExportReportInput{Kind: "day"}, []string{documents.Group(day.Profit.RevenueLocal), day.Profit.ProfitUSD}},
		{"month", api.ExportReportInput{Kind: "month"}, []string{day.NetUSD}},
		{"products", api.ExportReportInput{Kind: "products"}, []string{products.Rows[0].ProfitUSD, products.Total.RevenueUSD, documents.Group(products.Total.ProfitLocal)}},
		{"stock", api.ExportReportInput{Kind: "stock"}, []string{stockReport.TotalUSD, stockReport.Reconciliation.Closing}},
		{"drawer", api.ExportReportInput{Kind: "drawer"}, []string{}},
		{"statement", api.ExportStatementInput{CustomerID: abu.ID}, []string{"3.25"}},
		{"ledger", api.ExportRangeInput{}, []string{"3.25"}},
		{"sales", api.ExportRangeInput{}, []string{"3.25", documents.Group("97500")}},
	} {
		doc, err := api.ExportDocument(set, c.kind, c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.kind, err)
		}
		if bad := documents.Unisolated(doc); len(bad) > 0 {
			t.Fatalf("%s: figures not isolated: %q", c.kind, bad)
		}
		text := textOf(doc)
		for _, want := range c.want {
			if !strings.Contains(text, "\u2066"+want+"\u2069") {
				t.Fatalf("%s lacks the screen's %s:\n%s", c.kind, want, text)
			}
		}
		if c.kind == "products" {
			// A row's figures in their columns' order, each the screen's: a column showing another's figure fails here.
			r := products.Rows[0]
			line := ""
			for _, l := range strings.Split(text, "\n") {
				if strings.Contains(l, "\u2066"+r.Quantity+"\u2069") {
					line = l
					break
				}
			}
			at := 0
			for _, want := range []string{r.Quantity, r.RevenueUSD, r.CostUSD, r.ProfitUSD, r.MarginUSD, documents.Group(r.ProfitLocal)} {
				i := strings.Index(line[at:], "\u2066"+want+"\u2069")
				if i < 0 {
					t.Fatalf("products: the row's %s is not in its column's place: %q", want, line)
				}
				at += i + len(want)
			}
		}
	}
	for _, in := range []struct {
		name string
		call func() string
	}{
		{"statement", func() string {
			files.save = filepath.Join(dir, "statement.xlsx")
			r := set.Export.Statement(api.ExportStatementInput{CustomerID: abu.ID, Format: "xlsx"})
			if !r.OK {
				return r.Error.Code
			}
			return unzip(t, r.Data.Path)
		}},
		{"sales", func() string {
			files.save = filepath.Join(dir, "sales.xlsx")
			r := set.Export.SalesHistory(api.ExportRangeInput{Format: "xlsx"})
			if !r.OK {
				return r.Error.Code
			}
			return unzip(t, r.Data.Path)
		}},
		{"ledger", func() string {
			files.save = filepath.Join(dir, "ledger.pdf")
			r := set.Export.DebtLedger(api.ExportRangeInput{Format: "pdf"})
			if !r.OK {
				return r.Error.Code
			}
			return "pdf"
		}},
	} {
		out := in.call()
		if strings.HasPrefix(out, "lite.") {
			t.Fatalf("%s: %s", in.name, out)
		}
		if in.name != "ledger" && (strings.Contains(out, "<f>") || !strings.Contains(out, "=SUM(A1) أبو محمد")) {
			t.Fatalf("%s: a formula-like name is not plain text", in.name)
		}
	}
	if r := set.Export.Report(api.ExportReportInput{Kind: "day", Format: "docx"}); codeOf(t, r) != api.CodeUnknownFormat {
		t.Fatal("unknown format")
	}
	if r := set.Export.Report(api.ExportReportInput{Kind: "profit", Format: "pdf"}); codeOf(t, r) != api.CodeUnknownReport {
		t.Fatal("unknown report")
	}
	files.failing = true
	if r := set.Export.Report(api.ExportReportInput{Kind: "day", Format: "pdf"}); codeOf(t, r) != api.CodeSaveFailed {
		t.Fatal("a dialog failure")
	}
	files.failing = false

	// The counter, since 2026-09-16: every export goes, the ledger and the sales history and the drawer with it. The owner
	// asked for the shop's own papers to be printable without a PIN (owner.ReservedActs); only a restore is still reserved.
	set.Owner.EndElevation()
	files.save = filepath.Join(dir, "counter.pdf")
	if r := set.Export.Statement(api.ExportStatementInput{CustomerID: abu.ID, Format: "pdf"}); !r.OK {
		t.Fatalf("a statement at the counter: %+v", r.Error)
	}
	for name, r := range map[string]envelope.Result[api.ExportResultDTO]{
		"ledger": set.Export.DebtLedger(api.ExportRangeInput{Format: "pdf"}),
		"sales":  set.Export.SalesHistory(api.ExportRangeInput{Format: "pdf"}),
		"drawer": set.Export.Report(api.ExportReportInput{Kind: "drawer", Format: "pdf"}),
	} {
		if !r.OK {
			t.Errorf("%s at the counter: %+v", name, r.Error)
		}
	}
}

func TestBackupsThroughTheBindings(t *testing.T) {
	set, _ := tillShop(t)
	files := &fakeFiles{folder: t.TempDir()}
	set.SetFiles(files)
	restarted := make(chan struct{}, 1)
	set.SetRestarter(func() { restarted <- struct{}{} })
	taken := set.Backups.TakeNow()
	if !taken.OK || taken.Data.Reason != "on_demand" || taken.Data.SizeBytes == "0" || taken.Data.AgeSeconds < 0 || taken.Data.AgeSeconds > 60 {
		t.Fatalf("take %+v", taken)
	}
	status := set.Backups.Status()
	if !status.OK || status.Data.Last == nil || status.Data.Folder != "" || status.Data.Restored != nil {
		t.Fatalf("status %+v", status)
	}
	// Choosing the outside folder and reading what a restore would cost are open at the counter (2026-09-16); the restore
	// itself is not — TestARestoreStillAsksForThePIN holds that.
	if r := set.Backups.LossPreview(taken.Data.Name); !r.OK {
		t.Fatalf("a loss preview at the counter = %+v", r.Error)
	}
	elevate(t, set)
	folder := set.Backups.SetOutsideFolder(false)
	if !folder.OK || folder.Data.Folder != files.folder || folder.Data.LastOutside == nil || folder.Data.OutsideStale {
		t.Fatalf("folder %+v", folder)
	}
	list := set.Backups.List()
	outside := 0
	for _, b := range list.Data {
		if b.Outside {
			outside++
		}
	}
	if !list.OK || len(list.Data) < 2 || outside < 1 {
		t.Fatalf("list %+v", list)
	}
	preview := set.Backups.LossPreview(taken.Data.Name)
	if !preview.OK || preview.Data.Backup.Name != taken.Data.Name {
		t.Fatalf("preview %+v", preview)
	}
	copyPath := filepath.Join(t.TempDir(), "copy.db")
	files.save = copyPath
	if r := set.Backups.SaveCopy(taken.Data.Name); !r.OK || r.Data.Path != copyPath {
		t.Fatalf("save copy %+v", r)
	}
	files.open = copyPath
	imported := set.Backups.RestoreFromFile()
	if !imported.OK || imported.Data.Reason != "imported" {
		t.Fatalf("import %+v", imported)
	}
	restore := set.Backups.Restore(imported.Data.Name)
	if !restore.OK || !restore.Data.Staged || !restore.Data.Restarting {
		t.Fatalf("restore %+v", restore)
	}
	<-restarted
	if cleared := set.Backups.SetOutsideFolder(true); !cleared.OK || cleared.Data.Folder != "" {
		t.Fatalf("clear %+v", cleared)
	}
}
