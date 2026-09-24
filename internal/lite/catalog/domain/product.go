// Package domain holds Mizan Lite's catalogue as values: units, currencies, products, and the rules a
// product obeys. No I/O.
package domain

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/round"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
	"github.com/mizan-erp/mizan/internal/lite/textkey"
)

// Stable error codes. They double as i18n keys.
const (
	CodeNameRequired   = "lite.catalog.name_required"
	CodeNameTooLong    = "lite.catalog.name_too_long"
	CodeBarcodeInvalid = "lite.catalog.barcode_invalid"
	CodeUnknownUnit    = "lite.catalog.unknown_unit"
	// CodeOpenPriceHasNoPrice refuses a price, cost, margin or reorder level on an open-priced product (2026-09-23).
	CodeOpenPriceHasNoPrice = "lite.catalog.open_price_has_no_price"
	CodeUnknownCurrency     = "lite.catalog.unknown_currency"
	CodePriceDecimals       = "lite.catalog.price_decimals"
	CodeSlotInvalid         = "lite.catalog.slot_invalid"
	CodeSlotNeedsActive     = "lite.catalog.slot_needs_active"
	CodeDuplicateName       = "lite.catalog.duplicate_name"
	CodeDuplicateBarcode    = "lite.catalog.duplicate_barcode"
	CodeNotFound            = "lite.catalog.not_found"
)

// codeStale is platform/database's optimistic-concurrency code, reused so a stale edit reads the same
// whether the service or the database noticed it. Declared here only because this package may not import
// the database platform; the value is the contract.
const codeStale = "database.concurrent_modification"

const (
	// MaxNameRunes bounds a product name in either language.
	MaxNameRunes = 200
	// MaxBarcodeLength bounds a barcode.
	MaxBarcodeLength = 64
	// QuickSlots is the number of till buttons (Q-L1.6).
	QuickSlots = 24
)

// Fields named in validation errors, so a form can show the message under the right input.
const (
	FieldNameAR   = "nameAr"
	FieldNameEN   = "nameEn"
	FieldBarcode  = "barcode"
	FieldUnit     = "unitCode"
	FieldCurrency = "priceCurrency"
	FieldPrice    = "price"
	FieldSlot     = "quickSlot"
)

// Unit is a seeded unit of measure.
type Unit struct {
	Code          string
	Kind          string // mass, volume, count
	InputDecimals int
}

// Currency is a seeded currency.
type Currency struct {
	Code     string
	Decimals int
}

// Reference is the units and currencies a product is validated against.
type Reference struct {
	Units      map[string]Unit
	Currencies map[string]Currency
}

// Product is one thing the shop sells.
type Product struct {
	ID            id.ID
	NameAR        string
	NameEN        string
	Barcode       string
	UnitCode      string
	PriceCurrency string
	// PriceMicro is the selling price per unit at 10⁻⁶ of the currency's major unit.
	PriceMicro int64
	// CostMicro is what the shop believes one unit costs it, in PriceCurrency, at 10⁻⁶ (L9). Meaningful only when HasCost.
	CostMicro int64
	// HasCost separates "costs nothing" from "nobody has said" — a shop that has never typed a cost is not claiming zero.
	HasCost bool
	// ReorderMicro is the quantity at or below which the shop wants to be told to buy more, in the product's own unit
	// at 10⁻⁶ (2026-09-20). Meaningful only when HasReorder.
	ReorderMicro int64
	// HasReorder separates "tell me at zero" from "nobody has said" — a product with no level is never called low.
	HasReorder bool
	// OpenPrice marks a product sold at a price typed at the till and never counted in stock — the carrier bag, the bunch
	// of parsley (2026-09-23). Fixed when the product is created: the schema refuses to change it afterwards.
	OpenPrice bool
	// PricedRateNano is the exchange rate that was in force when the price was last set, or 0 where none was (or the
	// product is open-priced). It is how a price the dollar has left behind is found (2026-09-23).
	PricedRateNano int64
	// UnitsPerCartonMicro is how many of the product's unit make one carton (طرد), at 10⁻⁶, or 0 for none (0.10.0).
	// Not a package product (package.go): a carton is how goods are counted on an invoice, never a thing in stock.
	UnitsPerCartonMicro int64
	// QuickSlot is the till button, 1–QuickSlots, or 0 for none.
	QuickSlot  int
	Active     bool
	RowVersion int64
}

// NameKey is the normalised Arabic name: what uniqueness is decided on (L1 §5).
func (p Product) NameKey() string { return textkey.Normalise(p.NameAR) }

// SearchText is what search reads: the normalised Arabic name, English name and barcode.
func (p Product) SearchText() string {
	parts := []string{textkey.Normalise(p.NameAR)}
	for _, extra := range []string{p.NameEN, p.Barcode} {
		if key := textkey.Normalise(extra); key != "" {
			parts = append(parts, key)
		}
	}
	return strings.Join(parts, " ")
}

// Draft is a new product as a person typed it.
type Draft struct {
	NameAR        string
	NameEN        string
	Barcode       string
	UnitCode      string
	PriceCurrency string
	Price         string
	// Cost is what one unit costs the shop, in PriceCurrency (L9). Empty when the shop has not said.
	Cost string
	// CostDiscount is a supplier's discount, a percentage taken off Cost before anything is worked out from it (0.10.0).
	CostDiscount string
	// MarginPercent or MarginAmount works Price out of Cost instead of taking it as typed; at most one of the two.
	MarginPercent string
	MarginAmount  string
	// OpenPrice makes a product whose price is typed at the till and which is never counted in stock. It then takes no
	// price, cost or margin of its own.
	OpenPrice bool
	// UnitsPerCarton is how many of the unit make one carton (طرد); empty for none (0.10.0).
	UnitsPerCarton string
}

// NewProduct validates a draft into a product. It is active, on no till button, at version 1.
func NewProduct(productID id.ID, d Draft, ref Reference) (Product, error) {
	p := Product{ID: productID, Active: true, RowVersion: 1}

	var err error
	if p, err = p.Rename(d.NameAR, d.NameEN); err != nil {
		return Product{}, err
	}
	if p, err = p.WithBarcode(d.Barcode); err != nil {
		return Product{}, err
	}
	if _, ok := ref.Units[d.UnitCode]; !ok {
		return Product{}, errs.Validation(CodeUnknownUnit, "unknown unit").
			WithField(FieldUnit, CodeUnknownUnit, "unknown unit").WithParam("value", d.UnitCode)
	}
	p.UnitCode = d.UnitCode
	if p, err = p.WithCartonSize(d.UnitsPerCarton, ref); err != nil {
		return Product{}, err
	}
	if d.OpenPrice {
		// Its price is typed at the till, every time. A stored price, a cost or a margin would be figures the product
		// never uses, and a shop reading them would take them for the truth.
		if d.Price != "" && d.Price != "0" || d.Cost != "" || d.CostDiscount != "" || d.MarginPercent != "" || d.MarginAmount != "" {
			return Product{}, errs.Validation(CodeOpenPriceHasNoPrice, "an open-priced product takes its price at the till").
				WithField(FieldPrice, CodeOpenPriceHasNoPrice, "no price of its own")
		}
		if _, ok := ref.Currencies[d.PriceCurrency]; !ok {
			return Product{}, errs.Validation(CodeUnknownCurrency, "unknown currency").
				WithField(FieldCurrency, CodeUnknownCurrency, "unknown currency").WithParam("value", d.PriceCurrency)
		}
		p.PriceCurrency, p.PriceMicro, p.OpenPrice = d.PriceCurrency, 0, true
		return p, nil
	}
	if p, err = p.Reprice(d.PriceCurrency, d.Price, ref); err != nil {
		return Product{}, err
	}
	if d.Cost != "" {
		if p, err = p.SetCost(d.Cost, ref); err != nil {
			return Product{}, err
		}
	}
	if p, err = p.DiscountCost(d.CostDiscount, ref); err != nil {
		return Product{}, err
	}
	switch {
	case d.MarginPercent != "" && d.MarginAmount != "":
		return Product{}, errs.Validation(CodeMarginInvalid, "a margin is a percentage or an amount, not both").
			WithField(FieldMargin, CodeMarginInvalid, "one or the other")
	case d.MarginPercent != "":
		if p, err = p.PriceFromMarginPercent(d.MarginPercent, ref); err != nil {
			return Product{}, err
		}
	case d.MarginAmount != "":
		if p, err = p.PriceFromMarginAmount(d.MarginAmount, ref); err != nil {
			return Product{}, err
		}
	}
	return p, nil
}

// Rename sets both names. The Arabic name is required; the English one is optional.
func (p Product) Rename(nameAR, nameEN string) (Product, error) {
	ar := strings.TrimSpace(nameAR)
	en := strings.TrimSpace(nameEN)
	// A name made only of marks — tatweel, harakat — has an empty key, and a product whose key is empty
	// could never be found or told apart from another. It is as missing as an empty name.
	if ar == "" || textkey.Normalise(ar) == "" {
		return p, errs.Validation(CodeNameRequired, "the Arabic name is required").
			WithField(FieldNameAR, CodeNameRequired, "required")
	}
	if utf8.RuneCountInString(ar) > MaxNameRunes {
		return p, errs.Validation(CodeNameTooLong, "name too long").
			WithField(FieldNameAR, CodeNameTooLong, "too long").WithParam("max", strconv.Itoa(MaxNameRunes))
	}
	if utf8.RuneCountInString(en) > MaxNameRunes {
		return p, errs.Validation(CodeNameTooLong, "name too long").
			WithField(FieldNameEN, CodeNameTooLong, "too long").WithParam("max", strconv.Itoa(MaxNameRunes))
	}
	p.NameAR, p.NameEN = ar, en
	return p, nil
}

// WithBarcode sets or clears the barcode. Digits typed on an Arabic or Persian layout — which is what a
// keyboard-emulating scanner delivers when one is active — become Latin (L1 H2).
func (p Product) WithBarcode(raw string) (Product, error) {
	code := numinput.LatinDigits(raw)
	if len(code) > MaxBarcodeLength {
		return p, barcodeInvalid(raw)
	}
	for _, r := range code {
		// Printable ASCII only, no spaces: every symbology a shop's scanner reads (EAN, UPC, Code 128)
		// fits, and anything else in a barcode field is a typing or paste error.
		if r > unicode.MaxASCII || !unicode.IsPrint(r) || unicode.IsSpace(r) {
			return p, barcodeInvalid(raw)
		}
	}
	p.Barcode = code
	return p, nil
}

// Reprice sets the price and its currency. The price may be zero; it may not carry more decimals than the
// currency has (D-L1.5).
func (p Product) Reprice(currencyCode, raw string, ref Reference) (Product, error) {
	if p.OpenPrice {
		return p, errs.Validation(CodeOpenPriceHasNoPrice, "an open-priced product takes its price at the till").
			WithField(FieldPrice, CodeOpenPriceHasNoPrice, "no price of its own")
	}
	currency, ok := ref.Currencies[currencyCode]
	if !ok {
		return p, errs.Validation(CodeUnknownCurrency, "unknown currency").
			WithField(FieldCurrency, CodeUnknownCurrency, "unknown currency").WithParam("value", currencyCode)
	}
	normalised, err := numinput.Normalise(raw)
	if err != nil {
		return p, withField(err, FieldPrice)
	}
	if numinput.Decimals(normalised) > currency.Decimals {
		return p, errs.Validation(CodePriceDecimals, "too many decimals for the currency").
			WithField(FieldPrice, CodePriceDecimals, "too many decimals").
			WithParam("currency", currency.Code).WithParam("decimals", strconv.Itoa(currency.Decimals))
	}
	kernelCurrency, err := money.NewCurrency(currency.Code, uint8(currency.Decimals), round.HalfUp) //nolint:gosec // 0–4 by the schema's CHECK
	if err != nil {
		return p, err
	}
	amount, err := money.ParseUnitAmount(kernelCurrency, normalised)
	if err != nil {
		return p, errs.Validation(numinput.CodeInvalid, "price out of range").
			WithField(FieldPrice, numinput.CodeInvalid, "out of range")
	}
	p.PriceCurrency = currency.Code
	p.PriceMicro = amount.Micro()
	return p, nil
}

// OpenPricedIn changes the currency an open-priced item's price is typed in at the till (0.10.0). It has no price of its
// own, so a shop going over to dollars only moves nothing else of it.
func (p Product) OpenPricedIn(currencyCode string, ref Reference) (Product, error) {
	if !p.OpenPrice {
		return p, errs.Validation(CodeOpenPriceHasNoPrice, "only an open-priced product is priced at the till")
	}
	if _, ok := ref.Currencies[currencyCode]; !ok {
		return p, errs.Validation(CodeUnknownCurrency, "unknown currency").
			WithField(FieldCurrency, CodeUnknownCurrency, "unknown currency").WithParam("value", currencyCode)
	}
	p.PriceCurrency = currencyCode
	return p, nil
}

// SamePrice reports whether the product already has this price and currency — so a form that saves an
// unchanged price does not ask for the owner's PIN.
func (p Product) SamePrice(currencyCode string, micro int64) bool {
	return p.PriceCurrency == currencyCode && p.PriceMicro == micro
}

// Deactivate takes the product out of sale. It also leaves its till button: a deactivated product on the
// quick grid would be a button that sells nothing.
func (p Product) Deactivate() Product {
	p.Active = false
	p.QuickSlot = 0
	return p
}

// Activate returns the product to sale. Its old till button is not restored; somebody may hold it now.
func (p Product) Activate() Product {
	p.Active = true
	return p
}

// WithSlot puts the product on till button slot, or takes it off with 0.
func (p Product) WithSlot(slot int) (Product, error) {
	if slot < 0 || slot > QuickSlots {
		return p, errs.Validation(CodeSlotInvalid, "no such button").
			WithField(FieldSlot, CodeSlotInvalid, "no such button").WithParam("max", strconv.Itoa(QuickSlots))
	}
	if slot > 0 && !p.Active {
		return p, errs.Validation(CodeSlotNeedsActive, "an inactive product cannot hold a button").
			WithField(FieldSlot, CodeSlotNeedsActive, "inactive")
	}
	p.QuickSlot = slot
	return p, nil
}

// PriceText is the price as a decimal string in its currency's decimals: "3.25", "45000". For the owner's
// event history and for display; never parsed back.
func (p Product) PriceText(ref Reference) string {
	decimals := 6
	if c, ok := ref.Currencies[p.PriceCurrency]; ok {
		decimals = c.Decimals
	}
	return FormatMicro(p.PriceMicro, decimals)
}

// FormatMicro formats a non-negative 10⁻⁶-scaled amount with exactly `decimals` decimals when the value
// allows it, and with every significant digit when it does not — a value is never rounded for display.
func FormatMicro(micro int64, decimals int) string {
	whole := micro / 1_000_000
	frac := micro % 1_000_000
	fracText := strings.TrimRight(strconv.FormatInt(1_000_000+frac, 10)[1:], "0")
	if len(fracText) < decimals {
		fracText += strings.Repeat("0", decimals-len(fracText))
	}
	if fracText == "" {
		return strconv.FormatInt(whole, 10)
	}
	return strconv.FormatInt(whole, 10) + "." + fracText
}

// ErrStale reports that a product changed since the caller read it.
func ErrStale() error {
	return errs.Conflict(codeStale, "the product changed since it was read")
}

// ErrNotFound reports a product that does not exist.
func ErrNotFound() error {
	return errs.NotFound(CodeNotFound, "no such product")
}

func barcodeInvalid(raw string) error {
	return errs.Validation(CodeBarcodeInvalid, "invalid barcode").
		WithField(FieldBarcode, CodeBarcodeInvalid, "invalid").WithParam("max", strconv.Itoa(MaxBarcodeLength)).
		WithParam("value", raw)
}

// withField attaches a form field to a typed validation error from another package.
func withField(err error, field string) error {
	if typed, ok := errs.AsError(err); ok {
		return typed.WithField(field, typed.Code, "invalid")
	}
	return err
}
