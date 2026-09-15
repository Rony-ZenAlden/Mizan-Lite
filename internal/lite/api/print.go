package api

import (
	"context"
	"encoding/base64"
	"strconv"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	customersdomain "github.com/mizan-erp/mizan/internal/lite/customers/domain"
	"github.com/mizan-erp/mizan/internal/lite/documents"
	"github.com/mizan-erp/mizan/internal/lite/owner"
	ownerdomain "github.com/mizan-erp/mizan/internal/lite/owner/domain"
	"github.com/mizan-erp/mizan/internal/lite/printers"
	"github.com/mizan-erp/mizan/internal/lite/printing"
	salesdomain "github.com/mizan-erp/mizan/internal/lite/sales/domain"
	settingsdomain "github.com/mizan-erp/mizan/internal/lite/settings/domain"
)

// Print sends receipts and vouchers to the receipt printer and previews them (L7 §5). A print never undoes what it prints
// (D-L7.10): the sale or payment is recorded before; a failure is returned with its code and the screen offers Print again.
type Print struct{ core *core }

// Printers is the receipt printer: the system's printers, the settings, a test page.
type Printers struct{ core *core }

// ActPrinterSettings is changing the printer or the receipt's header and footer — the owner's.
const ActPrinterSettings = "printers.settings"

// Codes for printing.
const (
	CodeNotPrintable = "lite.print.not_printable"
)

// PrintResultDTO is a job the printer's queue took.
type PrintResultDTO struct {
	Printer string `json:"printer"`
	CopyNo  int    `json:"copyNo"`
	Path    string `json:"path"`
}

// PreviewInput names what to preview: kind sale, entry or test, and its id.
type PreviewInput struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// PreviewDTO is the printed bitmap, as a PNG (D-L7.11).
type PreviewDTO struct {
	PNG    string `json:"png"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	CopyNo int    `json:"copyNo"`
}

// printable is a document ready to render, and what to record of it.
type printable struct {
	doc     documents.Document
	kind    printing.Kind
	subject id.ID
	copyNo  int
	cash    bool // a cash sale or payment: the drawer may open (Q-L7.11)
	title   string
}

func media(paper int) documents.Media {
	if paper == 58 {
		return documents.Receipt58
	}
	return documents.Receipt80
}

// build is the document for a sale, a debt entry or a test page, with the copy number the next print will be.
func build(ctx context.Context, app *bootstrap.App, w words, kind, rawID string) (printable, error) {
	switch kind {
	case "sale":
		sale, err := receiptDTO(ctx, app, rawID)
		if err != nil {
			return printable{}, err
		}
		subject := id.ID(sale.ID)
		copyNo, err := app.Printing.NextCopy(ctx, subject)
		if err != nil {
			return printable{}, err
		}
		k := printing.KindSale
		if sale.Payment == string(salesdomain.PaymentCredit) {
			k = printing.KindCreditSale
		}
		return printable{doc: w.receiptDocument(sale, copyNo), kind: k, subject: subject, copyNo: copyNo,
			cash: k == printing.KindSale && sale.Status == string(salesdomain.StatusPosted), title: w.t("receipt.number", "number", strconv.FormatInt(sale.ReceiptNo, 10))}, nil
	case "entry":
		entryID, err := id.Parse(rawID)
		if err != nil {
			return printable{}, errs.NotFound(customersdomain.CodeEntryNotFound, "no such entry")
		}
		e, err := app.Customers.Entry(ctx, entryID)
		if err != nil {
			return printable{}, err
		}
		k := printing.KindPayment
		switch e.Kind {
		case customersdomain.KindPayment:
		case customersdomain.KindRefund:
			k = printing.KindRefund
			if !app.Owner.Allowed(ctx) {
				return printable{}, errs.Permission(ownerdomain.CodeRequired, "a refund voucher is the owner's")
			}
		default:
			return printable{}, errs.Conflict(CodeNotPrintable, "only payments and refunds have vouchers").WithParam("kind", string(e.Kind))
		}
		v, err := newDebtView(ctx, app)
		if err != nil {
			return printable{}, err
		}
		number, numbered, err := app.Printing.Voucher(ctx, e.ID)
		if err != nil {
			return printable{}, err
		}
		copyNo, err := app.Printing.NextCopy(ctx, e.ID)
		if err != nil {
			return printable{}, err
		}
		return printable{doc: w.voucherDocument(v, e, number, numbered, copyNo), kind: k, subject: e.ID, copyNo: copyNo,
			cash: k == printing.KindPayment, title: w.t("doc.voucher_payment")}, nil
	case "test":
		return printable{doc: w.testDocument(w.settings.Receipt.Printer), kind: printing.KindTest, copyNo: 1, title: w.t("doc.test_title")}, nil
	}
	return printable{}, errs.Validation(CodeNotPrintable, "unknown document").WithParam("kind", kind)
}

// send renders and prints a document, and records the job whatever happens.
func (c *core) send(ctx context.Context, app *bootstrap.App, w words, p printable) (PrintResultDTO, error) {
	r := w.settings.Receipt
	m := media(r.PaperMM)
	img := documents.Raster(w.ts, p.doc, m)
	job := printers.Job{Printer: r.Printer, Path: printers.Path(r.Path), Title: p.title, Raster: img, PaperMillimetres: r.PaperMM}
	if job.Path == printers.PathRaw {
		job.Escpos = documents.ESCPOS(img, r.Drawer && p.cash)
	} else {
		job.DriverPDF = documents.RasterPDF(img, m.PaperPoints, m.PrintPoints)
	}
	c.mu.RLock()
	system := c.printers
	c.mu.RUnlock()
	sendErr := system.Send(ctx, job)
	if r.Printer != "" {
		record := printing.Job{Kind: p.kind, SubjectID: p.subject, CopyNo: p.copyNo, Printer: r.Printer, Path: r.Path, Sent: sendErr == nil}
		if sendErr != nil {
			record.ErrorCode = errs.CodeOf(sendErr)
			if record.ErrorCode == "" {
				record.ErrorCode = printers.CodeSendFailed
			}
		}
		if _, err := app.Printing.Record(context.WithoutCancel(ctx), record); err != nil && sendErr == nil {
			return PrintResultDTO{}, err
		}
	}
	if sendErr != nil {
		return PrintResultDTO{}, sendErr
	}
	return PrintResultDTO{Printer: r.Printer, CopyNo: p.copyNo, Path: r.Path}, nil
}

func (p *Print) print(method, kind, rawID string) envelope.Result[PrintResultDTO] {
	return call(p.core, method, func(ctx context.Context, app *bootstrap.App) (PrintResultDTO, error) {
		w, err := newWords(ctx, app)
		if err != nil {
			return PrintResultDTO{}, err
		}
		doc, err := build(ctx, app, w, kind, rawID)
		if err != nil {
			return PrintResultDTO{}, err
		}
		return p.core.send(ctx, app, w, doc)
	})
}

// Sale prints a sale's receipt; a reprint is stamped as a copy. Anyone at the counter may.
func (p *Print) Sale(saleID string) envelope.Result[PrintResultDTO] {
	return p.print("Print.Sale", "sale", saleID)
}

// Entry prints a debt payment's voucher (anyone) or a refund's (owner).
func (p *Print) Entry(entryID string) envelope.Result[PrintResultDTO] {
	return p.print("Print.Entry", "entry", entryID)
}

// Preview is the bitmap a print would send, at the chosen paper width — what the receipt dialog shows.
func (p *Print) Preview(in PreviewInput) envelope.Result[PreviewDTO] {
	return call(p.core, "Print.Preview", func(ctx context.Context, app *bootstrap.App) (PreviewDTO, error) {
		w, err := newWords(ctx, app)
		if err != nil {
			return PreviewDTO{}, err
		}
		doc, err := build(ctx, app, w, in.Kind, in.ID)
		if err != nil {
			return PreviewDTO{}, err
		}
		m := media(w.settings.Receipt.PaperMM)
		img := documents.Raster(w.ts, doc.doc, m)
		return PreviewDTO{PNG: base64.StdEncoding.EncodeToString(documents.PNG(img)), Width: img.Bounds().Dx(), Height: img.Bounds().Dy(), CopyNo: doc.copyNo}, nil
	})
}

// PrinterDTO is a printer the system knows.
type PrinterDTO struct {
	Name    string `json:"name"`
	Default bool   `json:"default"`
}

// PrinterSettingsDTO is the receipt printer and the receipt's header and footer.
type PrinterSettingsDTO struct {
	Printer   string `json:"printer"`
	PaperMM   int    `json:"paperMm"`
	Path      string `json:"path"`
	AutoPrint string `json:"autoPrint"`
	Drawer    bool   `json:"drawer"`
	Phone     string `json:"phone"`
	Address   string `json:"address"`
	Footer    string `json:"footer"`
}

// PrinterSettingsInput changes them; PaperMM is "80" or "58".
type PrinterSettingsInput struct {
	Printer   string `json:"printer"`
	PaperMM   string `json:"paperMm"`
	Path      string `json:"path"`
	AutoPrint string `json:"autoPrint"`
	Drawer    bool   `json:"drawer"`
	Phone     string `json:"phone"`
	Address   string `json:"address"`
	Footer    string `json:"footer"`
}

func printerSettings(r settingsdomain.Receipt) PrinterSettingsDTO {
	return PrinterSettingsDTO{Printer: r.Printer, PaperMM: r.PaperMM, Path: r.Path, AutoPrint: r.AutoPrint, Drawer: r.Drawer, Phone: r.Phone, Address: r.Address, Footer: r.Footer}
}

// List is the operating system's printers.
func (p *Printers) List() envelope.Result[[]PrinterDTO] {
	return call(p.core, "Printers.List", func(ctx context.Context, _ *bootstrap.App) ([]PrinterDTO, error) {
		p.core.mu.RLock()
		system := p.core.printers
		p.core.mu.RUnlock()
		found, err := system.List(ctx)
		out := make([]PrinterDTO, 0, len(found))
		for _, f := range found {
			out = append(out, PrinterDTO{Name: f.Name, Default: f.Default})
		}
		return out, err
	})
}

// Settings reads the printer settings; the counter needs them to print automatically (Q-L7.2).
func (p *Printers) Settings() envelope.Result[PrinterSettingsDTO] {
	return call(p.core, "Printers.Settings", func(ctx context.Context, app *bootstrap.App) (PrinterSettingsDTO, error) {
		current, err := app.Settings.Get(ctx)
		return printerSettings(current.Receipt), err
	})
}

// Save changes the printer settings. Owner only.
func (p *Printers) Save(in PrinterSettingsInput) envelope.Result[PrinterSettingsDTO] {
	return call(p.core, "Printers.Save", func(ctx context.Context, app *bootstrap.App) (PrinterSettingsDTO, error) {
		var out PrinterSettingsDTO
		err := app.DB.Do(ctx, func(ctx context.Context) error {
			drawer := strconv.FormatBool(in.Drawer)
			next, err := app.Settings.Update(ctx, settingsdomain.Update{Printing: settingsdomain.PrintingUpdate{Printer: &in.Printer, PaperMM: &in.PaperMM,
				Path: &in.Path, AutoPrint: &in.AutoPrint, Drawer: &drawer, Phone: &in.Phone, Address: &in.Address, Footer: &in.Footer}})
			if err != nil {
				return err
			}
			if err = requireOwner(ctx, app, ActPrinterSettings, next.Receipt.Printer); err != nil {
				return err
			}
			out = printerSettings(next.Receipt)
			return nil
		})
		return out, err
	})
}

// Test prints the test page. Anyone may.
func (p *Printers) Test() envelope.Result[PrintResultDTO] {
	pr := &Print{core: p.core}
	return pr.print("Printers.Test", "test", "")
}

// requireOwner asks for owner mode and records the act in the owner's history, inside the caller's transaction.
func requireOwner(ctx context.Context, app *bootstrap.App, action, after string) error {
	return app.Owner.Require(ctx, owner.Act{Action: action, After: after})
}
