package api

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/documents"
	"github.com/mizan-erp/mizan/internal/lite/sheets"
	"github.com/mizan-erp/mizan/internal/lite/typeset"
)

// Export writes reports, statements, the debt ledger and the sales history to Excel or A4 PDF, saved where the owner chooses
// in the system's Save dialog (L7 §4). Every figure is the screen's: each export is built from the DTO its screen receives
// (D-L7.6).
type Export struct{ core *core }

// Codes for exports.
const (
	CodeUnknownFormat = "lite.export.unknown_format"
	CodeUnknownReport = "lite.export.unknown_report"
)

// The formats.
const (
	FormatXLSX = "xlsx"
	FormatPDF  = "pdf"
)

// ExportResultDTO is where a file was saved; Cancelled when the dialog was closed and nothing written.
type ExportResultDTO struct {
	Path      string `json:"path"`
	Bytes     int    `json:"bytes"`
	Cancelled bool   `json:"cancelled"`
}

// ExportReportInput asks for a report: kind day (Date), month (Month), products, stock (From, To) or drawer (Date).
type ExportReportInput struct {
	Kind   string `json:"kind"`
	Date   string `json:"date"`
	Month  string `json:"month"`
	From   string `json:"from"`
	To     string `json:"to"`
	Format string `json:"format"`
}

// ExportStatementInput asks for a customer's statement in every currency they have entries in.
type ExportStatementInput struct {
	CustomerID string `json:"customerId"`
	Format     string `json:"format"`
}

// ExportRangeInput asks for the debt ledger or the sales history over a range ("" for the month to date).
type ExportRangeInput struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Format string `json:"format"`
}

// exportable is one export's content, laid out once for both formats.
type exportable struct {
	title    string // the report's name
	subtitle string // its date or range
	blocks   []documents.Block
	sheets   []sheets.Sheet
}

// render is the file's bytes in the format asked for.
func (w words) render(x exportable, format string) ([]byte, error) {
	shop := w.settings.ShopName
	switch format {
	case FormatPDF:
		doc := documents.Document{
			Direction: w.dir,
			Running:   w.user(shop) + " — " + x.title,
			Footer:    w.t("doc.page", "page", "{page}", "pages", "{pages}", "printed", w.now()),
			Meta:      documents.Meta{Title: x.title, Author: shop, Created: "D:" + w.app.Now().UTC().Format("20060102150405") + "Z"},
			Blocks:    append(w.paperHeader(x), x.blocks...),
		}
		return documents.PDF(w.ts, doc), nil
	case FormatXLSX:
		for i := range x.sheets {
			plainCells(x.sheets[i].Rows)
			x.sheets[i].RightToLeft = !w.english()
			header := [][]sheets.Cell{{sheets.Text(x.title).Bolded()}, {sheets.Text(shop + " · " + x.subtitle)}}
			if line := w.shopDetails(); line != "" {
				header = append(header, []sheets.Cell{sheets.Text(line)})
			}
			header = append(header, []sheets.Cell{sheets.Text(w.t("doc.printed_at", "printed", w.now()))}, []sheets.Cell{})
			if x.sheets[i].Frozen > 0 {
				x.sheets[i].Frozen += len(header)
			}
			x.sheets[i].Rows = append(header, x.sheets[i].Rows...)
		}
		return sheets.XLSX(sheets.Workbook{Sheets: x.sheets, Title: x.title, Creator: shop, Created: w.app.Now().UTC().Format("2006-01-02T15:04:05Z")})
	}
	return nil, errs.Validation(CodeUnknownFormat, "xlsx or pdf").WithParam("value", format)
}

// paperHeader is the shop's own heading on an A4 export: its name, then the address and telephone it set for its receipts
// (the owner's request, 2026-09-16 — the same header on a receipt and on a report, not one of each).
func (w words) paperHeader(x exportable) []documents.Block {
	blocks := []documents.Block{documents.Title{Text: x.title, Subtitle: w.user(w.settings.ShopName) + " · " + x.subtitle}}
	r := w.settings.Receipt
	if r.Address != "" {
		blocks = append(blocks, documents.Paragraph{Text: w.user(r.Address), Small: true})
	}
	if r.Phone != "" {
		blocks = append(blocks, documents.Paragraph{Text: w.fig(r.Phone), Small: true})
	}
	return blocks
}

// shopDetails is the address and telephone on one line, for a workbook's heading; empty when neither is set.
func (w words) shopDetails() string {
	r := w.settings.Receipt
	switch {
	case r.Address != "" && r.Phone != "":
		return r.Address + " · " + r.Phone
	case r.Address != "":
		return r.Address
	default:
		return r.Phone
	}
}

var unsafeName = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f\x{2066}-\x{2069}]+`)

// filename is the proposed name: shop - report - range.ext, with what a file system refuses removed.
func (w words) filename(x exportable, format string) string {
	// A date reads 15/09/2026 in the document; a file name cannot hold "/", so it becomes 15-09-2026.
	base := unsafeName.ReplaceAllString(strings.ReplaceAll(w.settings.ShopName+" - "+x.title+" - "+x.subtitle, "/", "-"), " ")
	return strings.Join(strings.Fields(base), " ") + "." + format
}

// save renders, asks where, and writes atomically.
func (e *Export) save(ctx context.Context, app *bootstrap.App, format string, content func(w words) (exportable, error)) (ExportResultDTO, error) {
	if format != FormatPDF && format != FormatXLSX {
		return ExportResultDTO{}, errs.Validation(CodeUnknownFormat, "xlsx or pdf").WithParam("value", format)
	}
	w, err := newWords(ctx, app)
	if err != nil {
		return ExportResultDTO{}, err
	}
	x, err := content(w)
	if err != nil {
		return ExportResultDTO{}, err
	}
	body, err := w.render(x, format)
	if err != nil {
		return ExportResultDTO{}, err
	}
	files, err := e.core.dialogs()
	if err != nil {
		return ExportResultDTO{}, err
	}
	filter := Filter{Name: w.t("doc.filter_pdf"), Pattern: "*.pdf"}
	if format == FormatXLSX {
		filter = Filter{Name: w.t("doc.filter_xlsx"), Pattern: "*.xlsx"}
	}
	path, err := files.SaveFile(ctx, w.t("doc.save_title"), w.filename(x, format), []Filter{filter})
	if err != nil {
		return ExportResultDTO{}, errs.Wrap(err, errs.CategoryConflict, CodeSaveFailed, "the save dialog failed")
	}
	if path == "" {
		return ExportResultDTO{Cancelled: true}, nil
	}
	if !strings.HasSuffix(strings.ToLower(path), "."+format) {
		path += "." + format
	}
	if err = writeAtomically(path, body); err != nil {
		return ExportResultDTO{}, err
	}
	return ExportResultDTO{Path: path, Bytes: len(body)}, nil
}

// Report exports the Day, Month, Products, Stock or Cash drawer report. The reports are the owner's; so is the drawer's export,
// which lists expenses.
func (e *Export) Report(in ExportReportInput) envelope.Result[ExportResultDTO] {
	return call(e.core, "Export.Report", func(ctx context.Context, app *bootstrap.App) (ExportResultDTO, error) {
		return e.save(ctx, app, in.Format, func(w words) (exportable, error) { return reportContent(ctx, app, w, in) })
	})
}

// reportContent is the export's content, for both formats.
func reportContent(ctx context.Context, app *bootstrap.App, w words, in ExportReportInput) (exportable, error) {
	switch in.Kind {
	case "day":
		d, err := dayReportDTO(ctx, app, in.Date)
		return w.dayExport(d), err
	case "month":
		m, err := monthReportDTO(ctx, app, in.Month)
		return w.monthExport(m), err
	case "products":
		p, err := productsReportDTO(ctx, app, RangeInput{From: in.From, To: in.To})
		return w.productsExport(p), err
	case "stock":
		s, err := stockReportDTO(ctx, app, RangeInput{From: in.From, To: in.To})
		return w.stockExport(s), err
	case "drawer":
		d, err := drawerDTO(ctx, app, in.Date)
		return w.drawerExport(d), err
	}
	return exportable{}, errs.Validation(CodeUnknownReport, "unknown report").WithParam("value", in.Kind)
}

// Statement exports a customer's statement in every currency. Anyone at the counter may: it goes to the customer (Q-L7.5).
func (e *Export) Statement(in ExportStatementInput) envelope.Result[ExportResultDTO] {
	return call(e.core, "Export.Statement", func(ctx context.Context, app *bootstrap.App) (ExportResultDTO, error) {
		return e.save(ctx, app, in.Format, func(w words) (exportable, error) { return statementContent(ctx, app, w, in) })
	})
}

// statementContent is the export's content, for both formats.
func statementContent(ctx context.Context, app *bootstrap.App, w words, in ExportStatementInput) (exportable, error) {
	customerID, err := parseCustomerID(in.CustomerID)
	if err != nil {
		return exportable{}, err
	}
	withBalances, err := app.Customers.WithBalancesOf(ctx, customerID)
	if err != nil {
		return exportable{}, err
	}
	var statements []StatementDTO
	for _, sum := range withBalances.Summaries {
		st, err := statementDTO(ctx, app, StatementQueryDTO{CustomerID: in.CustomerID, Currency: sum.Currency})
		if err != nil {
			return exportable{}, err
		}
		statements = append(statements, st)
	}
	return w.statementExport(withBalances.Customer.Name, statements), nil
}

// DebtLedger exports who owes what and the debt book of a range. Owner only: it is the whole book (Q-L7.5).
func (e *Export) DebtLedger(in ExportRangeInput) envelope.Result[ExportResultDTO] {
	return call(e.core, "Export.DebtLedger", func(ctx context.Context, app *bootstrap.App) (ExportResultDTO, error) {
		return e.save(ctx, app, in.Format, func(w words) (exportable, error) { return ledgerContent(ctx, app, w, in) })
	})
}

// ledgerContent is the export's content, for both formats.
func ledgerContent(ctx context.Context, app *bootstrap.App, w words, in ExportRangeInput) (exportable, error) {
	from, to, err := app.Reports.Range(in.From, in.To)
	if err != nil {
		return exportable{}, err
	}
	out, err := outstandingDTO(ctx, app)
	if err != nil {
		return exportable{}, err
	}
	facts, err := app.Customers.EntriesBetween(ctx, from, to)
	if err != nil {
		return exportable{}, err
	}
	v, err := newDebtView(ctx, app)
	if err != nil {
		return exportable{}, err
	}
	entries := make([]EntryDTO, 0, len(facts))
	for _, f := range facts {
		entries = append(entries, v.entry(f.Entry, false))
	}
	return w.ledgerExport(out, entries, from, to), nil
}

// SalesHistory exports the sales of a range and their lines, with cost. Owner only (Q-L7.5).
func (e *Export) SalesHistory(in ExportRangeInput) envelope.Result[ExportResultDTO] {
	return call(e.core, "Export.SalesHistory", func(ctx context.Context, app *bootstrap.App) (ExportResultDTO, error) {
		return e.save(ctx, app, in.Format, func(w words) (exportable, error) { return salesContent(ctx, app, w, in) })
	})
}

// salesContent is the export's content, for both formats.
func salesContent(ctx context.Context, app *bootstrap.App, w words, in ExportRangeInput) (exportable, error) {
	from, to, err := app.Reports.Range(in.From, in.To)
	if err != nil {
		return exportable{}, err
	}
	facts, err := app.Sales.Facts(ctx, from, to)
	if err != nil {
		return exportable{}, err
	}
	v, err := newTillView(ctx, app)
	if err != nil {
		return exportable{}, err
	}
	var rows []historySale
	for _, s := range facts {
		if s.BusinessDate < from || s.BusinessDate > to {
			continue // a void in the range of a sale before it: the sale belongs to its own range
		}
		h := historySale{sale: v.sale(s), costUSD: v.money(s.CostUSDMinor, "USD")}
		for _, l := range s.Lines {
			h.lineCostUSD = append(h.lineCostUSD, v.money(l.CostUSDMinor, "USD"))
			h.lineCostLocal = append(h.lineCostLocal, v.money(l.CostLocalMinor, s.LocalCurrency))
			h.lineCostKnown = append(h.lineCostKnown, l.CostKnown)
		}
		rows = append(rows, h)
	}
	return w.salesExport(rows, from, to), nil
}

// ShowInFolder opens the folder a file was saved in, with the file selected.
func (e *Export) ShowInFolder(path string) envelope.Result[bool] {
	return call(e.core, "Export.ShowInFolder", func(context.Context, *bootstrap.App) (bool, error) {
		files, err := e.core.dialogs()
		if err != nil {
			return false, err
		}
		return true, files.ShowInFolder(path)
	})
}

type historySale struct {
	sale          SaleDTO
	costUSD       string
	lineCostUSD   []string
	lineCostLocal []string
	lineCostKnown []bool
}

// ─── cells ────────────────────────────────────────────────────────────────────

var plainDecimal = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)

// num is a figure Go formatted as a numeric cell in its own decimals (Q-L7.6), or text when it is not a plain number.
func num(s string) sheets.Cell {
	if !plainDecimal.MatchString(s) {
		return sheets.Text(s)
	}
	decimals := 0
	if _, frac, ok := strings.Cut(s, "."); ok {
		decimals = len(frac)
	}
	return sheets.Number(s, decimals)
}

func count(n int) sheets.Cell { return sheets.Number(strconv.Itoa(n), 0) }

func headings(labels ...string) []sheets.Cell {
	out := make([]sheets.Cell, len(labels))
	for i, l := range labels {
		out[i] = sheets.Text(l).Bolded()
	}
	return out
}

func (w words) cur(code string) string { return w.t("currency." + code) }

func (w words) rangeText(from, to string) string {
	return w.t("doc.range", "from", w.date(from), "to", w.date(to))
}

// plainCells strips direction marks from a workbook's text: a spreadsheet lays its sheet out itself, and an invisible mark in
// a cell would break sorting, searching and formulas.
func plainCells(rows [][]sheets.Cell) {
	for _, row := range rows {
		for i := range row {
			row[i].Text = strings.Map(func(r rune) rune {
				if typeset.IsBidiControl(r) {
					return -1
				}
				return r
			}, row[i].Text)
		}
	}
}
