package api_test

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/api"
	"github.com/mizan-erp/mizan/internal/lite/documents"
	"github.com/mizan-erp/mizan/internal/lite/printers"
	"github.com/mizan-erp/mizan/internal/lite/typeset"
)

// plain is a document's text as a reader sees it: the invisible isolates taken out.
func plain(s string) string {
	return strings.NewReplacer("⁦", "", "⁧", "", "⁨", "", "⁩", "").Replace(s)
}

// aLogo writes a PNG the owner might upload: a dark mark on a clear ground.
func aLogo(t *testing.T) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 300, 120))
	for y := 20; y < 100; y++ {
		for x := 20; x < 280; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 20, G: 60, B: 120, A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "logo.png")
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// invoiceShop is a shop with its store information and logo set, cups sold six to a carton, and a customer in Aleppo.
func invoiceShop(t *testing.T) (*api.Set, api.ProductDTO, api.CustomerDTO) {
	t.Helper()
	set, _ := tillShop(t)
	elevate(t, set)
	if r := set.Settings.SetLogo(aLogo(t)); !r.OK || r.Data.Logo == "" || r.Data.LogoWidth != 300 {
		t.Fatalf("SetLogo = %+v", r)
	}
	if r := set.Settings.SaveShop(api.ShopInput{Name: "كردي", Phone: "0933 111 222", City: "عفرين", Address: "السوق المسقوف"}); !r.OK {
		t.Fatal(r.Error)
	}
	cups := set.Catalog.CreateProduct(api.CreateProductInput{NameAR: "فنجان قهوة مارشميلو", UnitCode: "jar", PriceCurrency: "USD",
		Price: "18", UnitsPerCarton: "6"})
	if !cups.OK || cups.Data.UnitsPerCarton != "6" {
		t.Fatalf("CreateProduct = %+v", cups)
	}
	if r := set.Stock.Opening(api.ReceiveInput{ProductID: cups.Data.ID, Quantity: "24", CostMode: "total", Cost: "240", Currency: "USD"}); !r.OK {
		t.Fatal(r.Error)
	}
	riyad := set.Customers.Create(api.CustomerInput{Name: "رياض العمر", Phone: "0988 703 785", City: "حلب"})
	if !riyad.OK || riyad.Data.City != "حلب" {
		t.Fatalf("customer = %+v", riyad)
	}
	return set, cups.Data, riyad.Data
}

// TestTheInvoiceIsTheModelInvoice is the owner's model invoice of 2026-09-24, on a credit sale of six cups — one carton —
// to a customer in Aleppo: the shop and its logo at the head, the customer, the city and the phone, the seven columns,
// the carton count, and the total in words beside the net.
func TestTheInvoiceIsTheModelInvoice(t *testing.T) {
	set, cups, riyad := invoiceShop(t)
	cart := api.CartInput{Lines: []api.CartLineInput{{ProductID: cups.ID, Quantity: "6"}}, Payment: "credit", CustomerID: riyad.ID,
		Settlement: "USD", Tendered: "0", TenderCurrency: "USD"}
	sale := set.Till.Checkout(api.CheckoutInput{Cart: cart, Token: quoted(t, set, cart).Token})
	if !sale.OK {
		t.Fatal(sale.Error)
	}
	if sale.Data.Cartons != "1.0" || sale.Data.Lines[0].Cartons != "1.0" {
		t.Fatalf("cartons: sale %q, line %q — want 1.0", sale.Data.Cartons, sale.Data.Lines[0].Cartons)
	}

	doc, err := api.PrintDocument(set, "invoice", sale.Data.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bad := documents.Unisolated(doc); len(bad) > 0 {
		t.Fatalf("figures an Arabic line would print backwards: %q", bad)
	}
	if _, ok := doc.Blocks[0].(documents.Image); !ok {
		t.Fatalf("the invoice does not open with the shop's logo: %T", doc.Blocks[0])
	}
	text := plain(textOf(doc))
	for _, want := range []string{"كردي", "فاتورة مبيعات", "عفرين", "السوق المسقوف", "0933 111 222",
		"السيد:", "رياض العمر", "الجهة:", "حلب", "هاتف:", "0988 703 785", "رقم الفاتورة:",
		"الإجمالي | الصنف | الكمية | الوحدة | طرد | ملاحظات | السعر", "عدد الطرود", "1.0",
		"مجموع إجمالي الفاتورة", "الصافي", "فقط مائة وثمانية دولارات أمريكية لا غير"} {
		if !strings.Contains(text, want) {
			t.Errorf("the invoice does not say %q", want)
		}
	}
	// A4, and it renders — as the PDF the owner saves and as the pages a Windows printer draws.
	ts, err := typeset.Default()
	if err != nil {
		t.Fatal(err)
	}
	pages := documents.RasterPages(ts, doc, documents.A4.Scaled(150.0/72))
	if len(pages) != 1 {
		t.Fatalf("%d pages for a one-line invoice", len(pages))
	}
	if out := os.Getenv("MIZAN_RECEIPT_PNG"); out != "" {
		receipt, err := api.PrintDocument(set, "sale", sale.Data.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, documents.PNG(documents.Raster(ts, receipt, documents.Receipt80)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if out := os.Getenv("MIZAN_INVOICE_PNG"); out != "" {
		// For a person to look at: MIZAN_INVOICE_PNG=/tmp/invoice.png go test -run TheModelInvoice ./internal/lite/api/
		if err := os.WriteFile(out, documents.PNG(pages[0]), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// TestAnInvoiceGoesToTheA4PrinterOrToAPDF: with no A4 printer the screen is told to choose one or save a PDF; with one, the
// job carries the vector PDF and the pages, is recorded as an invoice — and the next receipt is not "copy 2".
func TestAnInvoiceGoesToTheA4PrinterOrToAPDF(t *testing.T) {
	set, cups, _ := invoiceShop(t)
	cart := api.CartInput{Lines: []api.CartLineInput{{ProductID: cups.ID, Quantity: "15"}}}
	sale := saleOnTheTill(t, set, cups, cart)
	if sale.Cartons != "2.5" {
		t.Fatalf("15 cups of six to a carton = %q cartons, want 2.5", sale.Cartons)
	}
	printer := &fakePrinter{names: []printers.Printer{{Name: "XP-80"}, {Name: "HP LaserJet"}}}
	set.SetPrinters(printer)
	if r := set.Print.Invoice(sale.ID); codeOf(t, r) != api.CodeNoInvoicePrinter {
		t.Fatal("an invoice was printed with no A4 printer chosen")
	}
	if r := set.Printers.Save(api.PrinterSettingsInput{Printer: "XP-80", PaperMM: "80", Path: "driver", AutoPrint: "none",
		InvoicePrinter: "HP LaserJet"}); !r.OK || r.Data.InvoicePrinter != "HP LaserJet" {
		t.Fatalf("Printers.Save = %+v", r)
	}
	if r := set.Print.Invoice(sale.ID); !r.OK || r.Data.Printer != "HP LaserJet" {
		t.Fatalf("Print.Invoice = %+v %+v", r, r.Error)
	}
	job := printer.jobs[0]
	if !bytes.HasPrefix(job.DriverPDF, []byte("%PDF-")) || len(job.Pages) != 1 || job.PaperMillimetres != 0 {
		t.Fatalf("the A4 job: pdf %v, %d pages, paper %d", bytes.HasPrefix(job.DriverPDF, []byte("%PDF-")), len(job.Pages), job.PaperMillimetres)
	}
	if b := job.Pages[0].Bounds(); b.Dx() != 1653 && b.Dx() != 1654 {
		t.Fatalf("an A4 page at 200 dpi is %d dots wide", b.Dx())
	}
	if r := set.Print.Sale(sale.ID); !r.OK || r.Data.CopyNo != 1 {
		t.Fatalf("after an invoice the receipt printed as copy %d", r.Data.CopyNo)
	}

	files := &fakeFiles{save: filepath.Join(t.TempDir(), "invoice.pdf")}
	set.SetFiles(files)
	saved := set.Export.Invoice(sale.ID)
	if !saved.OK || saved.Data.Cancelled {
		t.Fatalf("Export.Invoice = %+v", saved)
	}
	body, err := os.ReadFile(saved.Data.Path)
	if err != nil || !bytes.HasPrefix(body, []byte("%PDF-")) {
		t.Fatalf("the saved invoice is not a PDF: %v", err)
	}
	if len(files.asked) != 1 || !strings.Contains(files.asked[0], "كردي") {
		t.Fatalf("the proposed name %q does not carry the shop's name", files.asked)
	}

	preview := set.Print.Preview(api.PreviewInput{Kind: "invoice", ID: sale.ID})
	raw, _ := base64.StdEncoding.DecodeString(preview.Data.PNG)
	img, err := png.Decode(bytes.NewReader(raw))
	if !preview.OK || err != nil || img.Bounds().Dx() != 909 {
		t.Fatalf("the invoice preview: %v, %v", preview.Error, err)
	}
}

// TestACashSaleIsInvoicedToACashCustomer, and a shop with no logo sets its name in type instead.
func TestACashSaleIsInvoicedToACashCustomer(t *testing.T) {
	set, oil := tillShop(t)
	if r := set.Settings.RemoveLogo(); !r.OK || r.Data.Logo != "" {
		t.Fatalf("RemoveLogo = %+v", r)
	}
	sale := saleOnTheTill(t, set, oil, api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "1"}}})
	doc, err := api.PrintDocument(set, "invoice", sale.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.Blocks[0].(documents.Title); !ok {
		t.Fatalf("with no logo the invoice opens with %T, want the shop's name in type", doc.Blocks[0])
	}
	if text := plain(textOf(doc)); !strings.Contains(text, "زبون نقدي") || strings.Contains(text, "عدد الطرود") {
		t.Fatalf("a cash sale of a product with no carton size:\n%s", text)
	}
}
