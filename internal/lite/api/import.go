package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/locale"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	catalogdomain "github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	"github.com/mizan-erp/mizan/internal/lite/sheets"
	"github.com/mizan-erp/mizan/internal/lite/stock"
	stockdomain "github.com/mizan-erp/mizan/internal/lite/stock/domain"
)

// Importing products from Excel (L8 Q-L8.9): a shop's product list and opening stock, typed into the template once, loaded in one
// transaction. The preview runs the very import it previews and rolls it back, so what it reports is what applying does.

// ActImport is loading products from a workbook — the owner's.
const ActImport = "catalog.import"

// Codes for the import.
const (
	CodeImportEmpty     = "lite.import.empty"
	CodeImportHasErrors = "lite.import.has_errors"
	CodeImportChanged   = "lite.import.changed"
	CodeImportUnit      = "lite.import.unknown_unit"
	CodeImportCurrency  = "lite.import.unknown_currency"
	CodeImportCostNeeds = "lite.import.cost_required"
	CodeImportRead      = "lite.import.unreadable"
)

// The template's columns, in order.
var importColumns = []string{"name_ar", "name_en", "barcode", "unit", "currency", "price", "quantity", "cost"}

// ImportRowDTO is a product the workbook would create.
type ImportRowDTO struct {
	Row      int    `json:"row"`
	NameAR   string `json:"nameAr"`
	NameEN   string `json:"nameEn"`
	Barcode  string `json:"barcode"`
	UnitCode string `json:"unitCode"`
	Currency string `json:"currency"`
	Price    string `json:"price"`
	Quantity string `json:"quantity"`
	Cost     string `json:"cost"`
}

// ImportProblemDTO is why a row cannot be loaded: its row number as the spreadsheet shows it, the column, and a code the screen
// translates with its params.
type ImportProblemDTO struct {
	Row    int               `json:"row"`
	Column string            `json:"column"`
	Code   string            `json:"code"`
	Params map[string]string `json:"params"`
}

// ImportPreviewDTO is what a chosen workbook would load. Digest pins ImportApply to the file previewed.
type ImportPreviewDTO struct {
	Path      string             `json:"path"`
	Digest    string             `json:"digest"`
	Rows      []ImportRowDTO     `json:"rows"`
	Problems  []ImportProblemDTO `json:"problems"`
	Cancelled bool               `json:"cancelled"`
}

// ImportApplyInput applies a previewed workbook.
type ImportApplyInput struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

// ImportResultDTO is what was loaded.
type ImportResultDTO struct {
	Created   int `json:"created"`
	WithStock int `json:"withStock"`
}

// ImportTemplate saves the workbook to fill in: the columns in the shop's language, two example rows, and the units and currencies
// the columns accept. Anyone may save it.
func (c *Catalog) ImportTemplate() envelope.Result[ExportResultDTO] {
	return call(c.core, "Catalog.ImportTemplate", func(ctx context.Context, app *bootstrap.App) (ExportResultDTO, error) {
		w, err := newWords(ctx, app)
		if err != nil {
			return ExportResultDTO{}, err
		}
		units, err := app.Catalog.Units(ctx)
		if err != nil {
			return ExportResultDTO{}, err
		}
		heads := make([]sheets.Cell, len(importColumns))
		for i, col := range importColumns {
			heads[i] = sheets.Text(w.t("import.col." + col)).Bolded()
		}
		local := w.settings.LocalCurrency
		products := sheets.Sheet{Name: w.t("import.sheet_products"), RightToLeft: !w.english(), Frozen: 1, Rows: [][]sheets.Cell{heads,
			{sheets.Text("زيت زيتون (مثال)"), sheets.Text("Olive oil (example)"), sheets.Text("6211000000017"), sheets.Text("l"), sheets.Text("USD"),
				sheets.Number("3.25", 2), sheets.Number("20", 0), sheets.Number("2.40", 2)},
			{sheets.Text("لبنة (مثال)"), sheets.Text("Labneh (example)"), sheets.Text(""), sheets.Text("kg"), sheets.Text(local),
				sheets.Number("45000", 0), sheets.Text(""), sheets.Text("")},
		}}
		help := sheets.Sheet{Name: w.t("import.sheet_help"), RightToLeft: products.RightToLeft, Rows: [][]sheets.Cell{
			{sheets.Text(w.t("import.help_unit")).Bolded(), sheets.Text(w.t("import.help_unit_name")).Bolded()},
		}}
		for _, u := range units {
			help.Rows = append(help.Rows, []sheets.Cell{sheets.Text(u.Code), sheets.Text(w.t("uom." + u.Code))})
		}
		help.Rows = append(help.Rows, []sheets.Cell{}, []sheets.Cell{sheets.Text(w.t("import.help_currency")).Bolded()},
			[]sheets.Cell{sheets.Text("USD"), sheets.Text(w.t("currency.USD"))}, []sheets.Cell{sheets.Text(local), sheets.Text(w.t("currency." + local))},
			[]sheets.Cell{}, []sheets.Cell{sheets.Text(w.t("import.help_rules"))})
		body, err := sheets.XLSX(sheets.Workbook{Sheets: []sheets.Sheet{products, help}, Title: w.t("import.template_title"), Creator: w.settings.ShopName})
		if err != nil {
			return ExportResultDTO{}, err
		}
		return c.core.saveBytes(ctx, w.t("import.template_save_title"), w.t("import.template_file")+".xlsx",
			Filter{Name: w.t("doc.filter_xlsx"), Pattern: "*.xlsx"}, body)
	})
}

// ImportPreview asks for a workbook and reports what it would load and every row that cannot be — nothing is written.
func (c *Catalog) ImportPreview() envelope.Result[ImportPreviewDTO] {
	return call(c.core, "Catalog.ImportPreview", func(ctx context.Context, app *bootstrap.App) (ImportPreviewDTO, error) {
		w, err := newWords(ctx, app)
		if err != nil {
			return ImportPreviewDTO{}, err
		}
		files, err := c.core.dialogs()
		if err != nil {
			return ImportPreviewDTO{}, err
		}
		path, err := files.OpenFile(ctx, w.t("import.open_title"), []Filter{{Name: w.t("doc.filter_xlsx"), Pattern: "*.xlsx"}})
		if err != nil || path == "" {
			return ImportPreviewDTO{Cancelled: path == ""}, err
		}
		body, digest, err := readImport(path)
		if err != nil {
			return ImportPreviewDTO{}, err
		}
		rows, problems, err := runImport(ctx, app, body, false)
		return ImportPreviewDTO{Path: path, Digest: digest, Rows: rows, Problems: problems}, err
	})
}

// ImportApply loads a previewed workbook, all or nothing, in the owner's name. A file changed since its preview is refused.
func (c *Catalog) ImportApply(in ImportApplyInput) envelope.Result[ImportResultDTO] {
	return call(c.core, "Catalog.ImportApply", func(ctx context.Context, app *bootstrap.App) (ImportResultDTO, error) {
		body, digest, err := readImport(in.Path)
		if err != nil {
			return ImportResultDTO{}, err
		}
		if digest != in.Digest {
			return ImportResultDTO{}, errs.Conflict(CodeImportChanged, "the workbook changed since it was previewed")
		}
		rows, problems, err := runImport(ctx, app, body, true)
		if err != nil {
			return ImportResultDTO{}, err
		}
		if len(problems) > 0 {
			return ImportResultDTO{}, errs.Validation(CodeImportHasErrors, "rows cannot be loaded").WithParam("count", strconv.Itoa(len(problems)))
		}
		out := ImportResultDTO{Created: len(rows)}
		for _, r := range rows {
			if r.Quantity != "" {
				out.WithStock++
			}
		}
		return out, nil
	})
}

func readImport(path string) ([]byte, string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, "", errs.Wrap(err, errs.CategoryValidation, CodeImportRead, "reading the workbook")
	}
	sum := sha256.Sum256(body)
	return body, hex.EncodeToString(sum[:]), nil
}

// errRollback ends a preview's transaction without writing.
var errRollback = errors.New("preview: roll back")

// runImport parses the workbook's first sheet and loads every row inside one transaction: products created, opening stock booked.
// It collects every row's problem rather than stopping at the first; the transaction commits only when apply is set and nothing
// was wrong.
func runImport(ctx context.Context, app *bootstrap.App, body []byte, apply bool) ([]ImportRowDTO, []ImportProblemDTO, error) {
	book, err := sheets.ReadXLSX(body)
	if err != nil {
		return nil, nil, err
	}
	units, err := app.Catalog.Units(ctx)
	if err != nil {
		return nil, nil, err
	}
	currencies, err := app.Catalog.Currencies(ctx)
	if err != nil {
		return nil, nil, err
	}
	unitOf := aliases(app, func(add func(alias, code string)) {
		for _, u := range units {
			add(u.Code, u.Code)
			add("uom."+u.Code, u.Code)
		}
	})
	currencyOf := aliases(app, func(add func(alias, code string)) {
		for _, cur := range currencies {
			add(cur.Code, cur.Code)
			add("currency."+cur.Code, cur.Code)
			add("currency.short."+cur.Code, cur.Code)
		}
	})

	// Empty, never nil: the screen reads both as lists, and a nil slice reaches it as null (found by the end-to-end journey J2).
	rows := []ImportRowDTO{}
	problems := []ImportProblemDTO{}
	problem := func(row int, column string, err error) {
		p := ImportProblemDTO{Row: row, Column: column, Code: errs.CodeOf(err), Params: map[string]string{}}
		var e *errs.Error
		if errors.As(err, &e) {
			for k, v := range e.Params {
				p.Params[k] = v
			}
			if column == "" && len(e.Fields) > 0 {
				p.Column = e.Fields[0].Field
			}
		}
		if p.Code == "" {
			p.Code = CodeImportRead
		}
		problems = append(problems, p)
	}

	err = app.DB.Do(ctx, func(ctx context.Context) error {
		sheet := book[0].Rows
		for i := 1; i < len(sheet); i++ {
			cells := sheet[i]
			get := func(col int) string {
				if col < len(cells) {
					return strings.TrimSpace(cells[col])
				}
				return ""
			}
			if strings.TrimSpace(strings.Join(cells, "")) == "" {
				continue
			}
			rowNo := i + 1
			r := ImportRowDTO{Row: rowNo, NameAR: get(0), NameEN: get(1), Barcode: get(2), Price: get(5), Quantity: get(6), Cost: get(7)}
			code, ok := unitOf(get(3))
			if !ok {
				problem(rowNo, "unit", errs.Validation(CodeImportUnit, "unknown unit").WithParam("value", get(3)))
				continue
			}
			r.UnitCode = code
			if r.Currency, ok = currencyOf(get(4)); !ok {
				problem(rowNo, "currency", errs.Validation(CodeImportCurrency, "unknown currency").WithParam("value", get(4)))
				continue
			}
			if r.Quantity != "" && r.Cost == "" {
				problem(rowNo, "cost", errs.Validation(CodeImportCostNeeds, "opening stock needs its cost"))
				continue
			}
			product, createErr := app.Catalog.Create(ctx, catalogdomain.Draft{NameAR: r.NameAR, NameEN: r.NameEN, Barcode: r.Barcode,
				UnitCode: r.UnitCode, PriceCurrency: r.Currency, Price: r.Price})
			if createErr != nil {
				problem(rowNo, importColumn(createErr), createErr)
				continue
			}
			if r.Quantity != "" {
				if _, openErr := app.Stock.Opening(ctx, stock.ReceiveInput{ProductID: product.ID, Quantity: r.Quantity,
					Cost: stockdomain.CostInput{Mode: stockdomain.CostUnit, Amount: r.Cost, Currency: r.Currency}}); openErr != nil {
					problem(rowNo, importColumn(openErr), openErr)
					continue
				}
			}
			rows = append(rows, r)
		}
		if len(rows) == 0 && len(problems) == 0 {
			return errs.Validation(CodeImportEmpty, "the workbook has no products")
		}
		if !apply || len(problems) > 0 {
			return errRollback
		}
		// Asked last, so a counter previewing or applying a workbook with problems is told about the problems, not the PIN.
		return requireOwner(ctx, app, ActImport, strconv.Itoa(len(rows)))
	})
	if errors.Is(err, errRollback) {
		err = nil
	}
	return rows, problems, err
}

// importColumn names the template column a refusal is about, from the field the domain names.
func importColumn(err error) string {
	var e *errs.Error
	if !errors.As(err, &e) || len(e.Fields) == 0 {
		return ""
	}
	switch f := e.Fields[0].Field; {
	case strings.Contains(strings.ToLower(f), "namear"), f == "name_ar":
		return "name_ar"
	case strings.Contains(strings.ToLower(f), "nameen"), f == "name_en":
		return "name_en"
	case strings.Contains(strings.ToLower(f), "barcode"):
		return "barcode"
	case strings.Contains(strings.ToLower(f), "price"), strings.Contains(strings.ToLower(f), "currency"):
		return "price"
	case strings.Contains(strings.ToLower(f), "quantity"):
		return "quantity"
	case strings.Contains(strings.ToLower(f), "cost"), strings.Contains(strings.ToLower(f), "amount"):
		return "cost"
	default:
		return f
	}
}

// aliases resolves what a person typed in a column — a code, or its name in either language — to a code, without regard to case
// or surrounding space.
func aliases(app *bootstrap.App, fill func(add func(alias, code string))) func(string) (string, bool) {
	known := map[string]string{}
	fill(func(alias, code string) {
		if strings.Contains(alias, ".") {
			for _, loc := range []locale.Locale{"ar", "en"} {
				if text := app.Messages.T(loc, alias, nil); text != alias {
					known[strings.ToLower(strings.TrimSpace(text))] = code
				}
			}
			return
		}
		known[strings.ToLower(alias)] = code
	})
	return func(typed string) (string, bool) {
		code, ok := known[strings.ToLower(strings.TrimSpace(typed))]
		return code, ok
	}
}
