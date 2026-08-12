package sales

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/locale"
	"github.com/mizan-erp/mizan/internal/modules/sales/domain"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/printing"
)

//go:embed seeds/print_templates/*.json
var templateFS embed.FS

// Stable codes for printing a sales document.
const (
	CodeUnknownTemplate = "sales.unknown_template"
	CodeNotPrintable    = "sales.not_printable"
)

// PermSalePrint is the permission to print a document.
//
// Separate from viewing, and the separation is not pedantry: a printed invoice leaves the
// building. Somebody who may look up what a customer owes is not automatically somebody who may
// produce a document on company letterhead and hand it over.
const PermSalePrint = "sales.document.print"

// The numeral systems a printed document may use.
const (
	DigitsWestern = "western"
	DigitsArabic  = "arabic"
)

// PrintDigits selects the numeral system on printed documents (§22.5).
//
// A SETTING rather than a consequence of the language, because it is not one: §22.5 records that
// this varies by country and by customer, and an Arabic invoice from a Gulf exporter usually
// carries Western digits. Inferring it from the locale would be right in Cairo and wrong in
// Dubai, with no way for either to correct it.
var PrintDigits = config.DeclareEnum(config.Def{
	Key:         "print.digits",
	Default:     DigitsWestern,
	Enum:        []string{DigitsWestern, DigitsArabic},
	Scopes:      []config.Scope{config.ScopeSystem, config.ScopeCompany},
	Description: "settings.print.digits",
})

// Translator is the narrow slice of i18n this module needs.
//
// A port rather than the catalog itself, for the reason every port here exists: the module
// depends on "turn a key into words", not on the platform's catalog type.
type Translator interface {
	T(l locale.Locale, key string, params map[string]string) string
}

// Letterhead is who is printing. Supplied by the caller, because a company's identity belongs to
// the org module and this one must not learn it.
type Letterhead struct {
	Company   string
	Branch    string
	Address   string
	Phone     string
	TaxNumber string
	Footer    string
}

// PrintRequest asks for one document, rendered.
type PrintRequest struct {
	DocumentID id.ID
	// Template is "receipt" or "invoice".
	Template   string
	Locale     locale.Locale
	Direction  string
	Digits     string
	Letterhead Letterhead
	// Decimals is the currency's minor-unit count, for formatting.
	Decimals int
}

// Templates loads the shipped print templates.
//
// # Data, not code
//
// Phase 0's table lists print templates as REFERENCE DATA — the kind of thing an administrator
// changes without a release. They are JSON for that reason. What ships is a sensible default;
// what a shop actually prints can differ without recompiling anything.
func Templates() ([]printing.Template, error) {
	entries, err := fs.Glob(templateFS, "seeds/print_templates/*.json")
	if err != nil {
		return nil, errs.Internal(CodeUnknownTemplate, "the print templates could not be listed")
	}

	templates := make([]printing.Template, 0, len(entries))
	for _, name := range entries {
		raw, readErr := templateFS.ReadFile(name)
		if readErr != nil {
			return nil, errs.Internal(CodeUnknownTemplate,
				"a shipped print template could not be read").WithParam("file", name)
		}
		var template printing.Template
		if jsonErr := json.Unmarshal(raw, &template); jsonErr != nil {
			// A SHIPPED template that does not parse is a build fault, not a user's problem.
			// It fails loudly rather than leaving the till with nothing to print.
			return nil, errs.Internal(printing.CodeBadTemplate,
				"a shipped print template is not valid JSON").WithParam("file", name)
		}
		templates = append(templates, template)
	}
	return templates, nil
}

// Print renders one sales document.
//
// # Everything printed comes from the DOCUMENT, never from master data
//
// This is §9.3, and it is the reason the line table stores `product_name`, `variant_sku`, and
// `uom_code` next to `variant_id` — columns that look redundant until you ask what a reprint
// should say. A product renamed last month, a price list revised, a tax rate changed: none of
// them may alter a document that has already been issued, because the customer is holding a copy
// and the two must be the same piece of paper.
//
// The test that proves it does not check the totals. It renames the product, revises the price,
// and re-prints — and requires the bytes to be identical.
func (s *Service) Print(ctx context.Context, in PrintRequest) (printing.Document, error) {
	document, lines, err := s.Document(ctx, in.DocumentID)
	if err != nil {
		return printing.Document{}, err
	}

	// A draft has no number, no tax point, and no agreement behind it. Printing one produces a
	// document that looks like an invoice and is not — which is the sort of thing a customer
	// keeps and an auditor later finds.
	if document.Status == domain.Draft {
		return printing.Document{}, errs.Conflict(CodeNotPrintable,
			"a draft cannot be printed; post it first")
	}

	template, err := templateNamed(in.Template)
	if err != nil {
		return printing.Document{}, err
	}

	outstanding, err := s.Outstanding(ctx, in.DocumentID)
	if err != nil {
		return printing.Document{}, err
	}

	data := printing.Data{
		Fields:    s.printFields(in, document, outstanding),
		Rows:      printRows(lines, in.Decimals),
		Direction: in.Direction,
		Digits:    in.Digits,
	}
	return printing.Render(template, data)
}

func (s *Service) printFields(
	in PrintRequest, document domain.Document, outstandingMinor int64,
) map[string]string {
	t := func(key string) string {
		if s.messages == nil {
			return key
		}
		return s.messages.T(in.Locale, key, nil)
	}

	paid := document.TotalMinor - outstandingMinor

	fields := map[string]string{
		// The letterhead.
		"company": in.Letterhead.Company, "branch": in.Letterhead.Branch,
		"address": in.Letterhead.Address, "phone": in.Letterhead.Phone,
		"taxNumber": in.Letterhead.TaxNumber, "footer": in.Letterhead.Footer,

		// The document, from its own record.
		"number": document.Number, "date": document.Date,
		"customer": document.PartnerName, "servedBy": "",
		"customerTaxNumber": "",
		"documentTitle":     t("print.title." + string(document.Type)),

		// The money, formatted here because a template must not be a place where rounding is
		// decided.
		"net":      formatMinor(document.NetMinor, in.Decimals),
		"discount": zeroAsBlank(document.DiscountMinor, in.Decimals),
		"tax":      formatMinor(document.TaxMinor, in.Decimals),
		"total":    formatMinor(document.TotalMinor, in.Decimals),

		// Settlement. Blank when there is nothing to say: a receipt for a sale paid in full
		// should not carry a line reading "Outstanding 0.00", which reads as a debt.
		"paid":        zeroAsBlank(paid, in.Decimals),
		"change":      "",
		"outstanding": zeroAsBlank(outstandingMinor, in.Decimals),

		// The labels, so one template serves every language.
		"numberLabel": t("print.label.number"), "dateLabel": t("print.label.date"),
		"customerLabel": t("print.label.customer"), "servedByLabel": t("print.label.servedBy"),
		"taxNumberLabel":   t("print.label.taxNumber"),
		"customerTaxLabel": t("print.label.customerTax"),
		"netLabel":         t("print.label.net"), "discountLabel": t("print.label.discount"),
		"taxLabel": t("print.label.tax"), "grandTotalLabel": t("print.label.total"),
		"paidLabel": t("print.label.paid"), "changeLabel": t("print.label.change"),
		"outstandingLabel": t("print.label.outstanding"),
		"itemHeader":       t("print.header.item"), "qtyHeader": t("print.header.quantity"),
		"priceHeader": t("print.header.price"), "totalHeader": t("print.header.total"),
		"skuHeader": t("print.header.sku"), "taxHeader": t("print.header.tax"),
	}
	return fields
}

// printRows turns lines into rows, from the SNAPSHOT columns only.
//
// `line.ProductName`, not a lookup of `line.VariantID`. That single choice is the difference
// between a reprint and a re-derivation.
func printRows(lines []domain.Line, decimals int) []map[string]string {
	rows := make([]map[string]string, 0, len(lines))
	for _, line := range lines {
		rows = append(rows, map[string]string{
			"line":     strconv.Itoa(line.LineNumber),
			"sku":      line.VariantSKU,
			"item":     line.ProductName,
			"quantity": formatQuantity(line.QuantityMicro) + " " + line.UomCode,
			"price":    formatMinor(line.UnitPriceMinor, decimals),
			"tax":      formatMinor(line.TaxAmountMinor, decimals),
			"total":    formatMinor(line.TotalMinor, decimals),
		})
	}
	return rows
}

func templateNamed(code string) (printing.Template, error) {
	templates, err := Templates()
	if err != nil {
		return printing.Template{}, err
	}
	for _, template := range templates {
		if template.Code == code {
			return template, nil
		}
	}
	return printing.Template{}, errs.NotFound(CodeUnknownTemplate,
		"there is no print template with that code").WithParam("template", code)
}

// formatMinor turns minor units into a grouped decimal string.
//
// Digit manipulation on the integer, never a float: this is the same rule the frontend's
// money.ts follows, for the same reason (§17, §18).
func formatMinor(minor int64, decimals int) string {
	if decimals < 0 {
		decimals = 0
	}
	negative := minor < 0
	digits := strconv.FormatInt(minor, 10)
	if negative {
		digits = digits[1:]
	}
	for len(digits) <= decimals {
		digits = "0" + digits
	}

	whole, fraction := digits[:len(digits)-decimals], digits[len(digits)-decimals:]

	var grouped strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(r)
	}

	out := grouped.String()
	if decimals > 0 {
		out += "." + fraction
	}
	if negative {
		return "-" + out
	}
	return out
}

// formatQuantity turns micro units into a readable quantity, trimming meaningless zeros.
//
// "2" rather than "2.000000": six decimals on a receipt is noise, and a customer reading
// "2.000000 PCS" is reading a bug report.
func formatQuantity(micro int64) string {
	const scale = 1_000_000
	whole, fraction := micro/scale, micro%scale
	if fraction == 0 {
		return strconv.FormatInt(whole, 10)
	}
	text := fmt.Sprintf("%d.%06d", whole, abs64(fraction))
	return strings.TrimRight(text, "0")
}

// zeroAsBlank formats an amount, or returns nothing at all when it is zero.
//
// The blank is what drives `omitWhenEmpty`, and it is the mechanism that lets ONE template serve
// a cash sale and a credit sale. "Outstanding: 0.00" on a receipt for a sale paid in full reads
// as a debt to anybody scanning it.
func zeroAsBlank(minor int64, decimals int) string {
	if minor == 0 {
		return ""
	}
	return formatMinor(minor, decimals)
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
