package domain

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
)

// A purchase: goods bought from a supplier on one invoice, cash or on credit (the owner's request, 2026-09-24).
//
// # What a line costs
//
// Each line is the quantity that arrived, how many of them arrived damaged, and the supplier's price for one unit.
// Damaged units are not charged for — the owner's decision: the supplier does not charge for what cannot be sold, and
// those units never enter stock. The good units are charged at the unit price, less the line's discount (a percentage
// or an amount), less the line's share of the discount on the whole invoice. What is left is what the line costs the
// shop, and that — spread over the good units — is the cost the stock takes in, so the average cost and every margin
// after it reflect what was really paid.
//
// # Every figure is Go's, and exact
//
// Quantities are millionths of the unit, unit costs millionths of the currency, totals the currency's minor units.
// Products that could overflow an int64 are worked in math/big and rounded once, half up. The invoice's discount is
// shared out by largest remainder, so the shares add up to exactly the discount — no cent appears or disappears between
// the lines and the total.

// More codes, for purchases.
const (
	CodeNoLines           = "lite.suppliers.no_lines"
	CodeProductUnknown    = "lite.suppliers.product_unknown"
	CodeProductInactive   = "lite.suppliers.product_inactive"
	CodeProductOpenPrice  = "lite.suppliers.product_open_price"
	CodeQuantityInvalid   = "lite.suppliers.quantity_invalid"
	CodeQuantityDecimals  = "lite.suppliers.quantity_decimals"
	CodeDamagedTooMany    = "lite.suppliers.damaged_too_many"
	CodeCostInvalid       = "lite.suppliers.cost_invalid"
	CodeDiscountBoth      = "lite.suppliers.discount_both"
	CodeDiscountInvalid   = "lite.suppliers.discount_invalid"
	CodeDiscountTooLarge  = "lite.suppliers.discount_too_large"
	CodeRateRequired      = "lite.suppliers.rate_required"
	CodeRateInvalid       = "lite.suppliers.rate_invalid"
	CodeRefTooLong        = "lite.suppliers.ref_too_long"
	CodePaidFromRequired  = "lite.suppliers.paid_from_required"
	CodePurchaseNotFound  = "lite.suppliers.purchase_not_found"
	CodePurchaseVoided    = "lite.suppliers.purchase_voided"
	CodeLineNotReversible = "lite.suppliers.line_not_reversible"
)

// Fields of a purchase named in validation errors; a line's are "lines.<n>.<field>".
const (
	FieldCurrency        = "currency"
	FieldRate            = "rate"
	FieldRef             = "supplierRef"
	FieldQuantity        = "quantity"
	FieldDamaged         = "damaged"
	FieldUnitCost        = "unitCost"
	FieldDiscount        = "discount"
	FieldInvoiceDiscount = "invoiceDiscount"
	FieldPaidNow         = "paidNow"
	FieldPaidFrom        = "paidFrom"
	FieldProduct         = "productId"
)

// MaxRefRunes bounds the supplier's own invoice number.
const MaxRefRunes = 40

// Status is whether a purchase stands.
type Status string

// The statuses.
const (
	StatusPosted Status = "posted"
	StatusVoided Status = "voided"
)

// Product is what a purchase needs of the catalogue.
type Product struct {
	ID           id.ID
	NameAR       string
	NameEN       string
	UnitCode     string
	UnitDecimals int
	OpenPrice    bool
	Active       bool
}

// LineInput is one line as typed.
type LineInput struct {
	ProductID id.ID
	// Quantity is what arrived, in the product's own unit, damaged units included; Damaged is how many of them are.
	Quantity string
	Damaged  string
	// UnitCost is the supplier's price for one unit, in the purchase's currency.
	UnitCost string
	// DiscountPercent or DiscountAmount is the line's discount; at most one of the two.
	DiscountPercent string
	DiscountAmount  string
}

// Input is a purchase as typed.
type Input struct {
	SupplierID id.ID
	Currency   string
	// Rate is local currency per dollar, the rate the purchase was paid at, when the currency is the local one.
	Rate            string
	SupplierRef     string
	Lines           []LineInput
	InvoiceDiscount string
	PaidNow         string
	PaidFrom        string
	Note            string
}

// Line is one priced line.
type Line struct {
	ID                   id.ID
	LineNo               int
	ProductID            id.ID
	NameAR               string
	NameEN               string
	UnitCode             string
	QuantityMicro        int64
	DamagedMicro         int64
	UnitCostMicro        int64
	DiscountPercentMicro int64
	GrossMinor           int64
	LineDiscountMinor    int64
	InvoiceShareMinor    int64
	// StockLedgerID is the receipt that took the good units into stock; "" when every unit arrived damaged.
	StockLedgerID id.ID
}

// GoodMicro is how many units arrived fit to sell.
func (l Line) GoodMicro() int64 { return l.QuantityMicro - l.DamagedMicro }

// DueMinor is what the line costs the shop.
func (l Line) DueMinor() int64 { return l.GrossMinor - l.LineDiscountMinor - l.InvoiceShareMinor }

// DamagedValueMinor is what the damaged units would have cost at the line's unit price, in the currency's minor units,
// half up — the value a supplier did not charge (0.10.0).
func (l Line) DamagedValueMinor(decimals int) int64 {
	v, _ := roundDiv(new(big.Int).Mul(big.NewInt(l.DamagedMicro), big.NewInt(l.UnitCostMicro)), pow10(12-decimals))
	return v
}

// NetUnitMicro is what one good unit cost after every discount, at 10⁻⁶ of the currency's major unit, half up — the
// cost the stock book received it at. Nought when nothing arrived fit to sell.
func (l Line) NetUnitMicro(decimals int) int64 {
	if l.GoodMicro() <= 0 {
		return 0
	}
	// due is at 10⁻ᵈ; a unit cost at 10⁻⁶ per unit is due × 10^(6−d) ÷ (good ÷ 10⁶).
	v, _ := roundDiv(new(big.Int).Mul(big.NewInt(l.DueMinor()), pow10(12-decimals)), big.NewInt(l.GoodMicro()))
	return v
}

// Purchase is a purchase as recorded.
type Purchase struct {
	ID                   id.ID
	PurchaseNo           int64
	SupplierID           id.ID
	SupplierName         string
	BusinessDate         string
	OccurredAt           time.Time
	Currency             string
	RateNano             int64
	SupplierRef          string
	Lines                []Line
	GrossMinor           int64
	LineDiscountMinor    int64
	InvoiceDiscountMinor int64
	PaidNowMinor         int64
	PaidFrom             CashSource
	Status               Status
	VoidedAt             time.Time
	VoidReason           string
	Note                 string
	RowVersion           int64
}

// DueMinor is what the purchase costs the shop — what it adds to the supplier's book.
func (p Purchase) DueMinor() int64 {
	return p.GrossMinor - p.LineDiscountMinor - p.InvoiceDiscountMinor
}

// DamagedLines counts the lines that had damaged units, for a statement's line.
func (p Purchase) DamagedLines() int {
	n := 0
	for _, l := range p.Lines {
		if l.DamagedMicro > 0 {
			n++
		}
	}
	return n
}

// Currency is a currency's code and decimals.
type Currency struct {
	Code     string
	Decimals int
}

// Price works a purchase out from what was typed: every line's figures, the shares of the invoice's discount, and the
// totals. It writes nothing; the service records what it returns. localCode is the shop's local currency, the one a
// rate is required for.
func Price(in Input, products map[id.ID]Product, currencies []Currency, localCode string) (Purchase, error) {
	cur, ok := findCurrency(currencies, in.Currency)
	if !ok {
		return Purchase{}, errs.Validation(CodeUnknownCurrency, "unknown currency").
			WithField(FieldCurrency, CodeUnknownCurrency, "unknown").WithParam("value", in.Currency)
	}
	p := Purchase{SupplierID: in.SupplierID, Currency: cur.Code, Status: StatusPosted}
	var err error
	if cur.Code == localCode {
		if p.RateNano, err = ParseRate(in.Rate); err != nil {
			return Purchase{}, err
		}
	}
	if p.SupplierRef, err = parseText(in.SupplierRef, MaxRefRunes, FieldRef, CodeRefTooLong); err != nil {
		return Purchase{}, err
	}
	if p.Note, err = Note(in.Note); err != nil {
		return Purchase{}, err
	}
	if len(in.Lines) == 0 {
		return Purchase{}, errs.Validation(CodeNoLines, "a purchase has at least one line")
	}
	for i, li := range in.Lines {
		line, lineErr := priceLine(li, i+1, products, cur)
		if lineErr != nil {
			return Purchase{}, lineErr
		}
		p.Lines = append(p.Lines, line)
		var fits bool
		if p.GrossMinor, fits = addChecked(p.GrossMinor, line.GrossMinor); !fits {
			return Purchase{}, errs.Validation(CodeAmountTooLarge, "the purchase is out of range")
		}
		p.LineDiscountMinor += line.LineDiscountMinor // at most the gross, which fitted
	}
	if p.InvoiceDiscountMinor, err = parseMinor(in.InvoiceDiscount, cur, FieldInvoiceDiscount); err != nil {
		return Purchase{}, err
	}
	afterLines := p.GrossMinor - p.LineDiscountMinor
	if p.InvoiceDiscountMinor > afterLines {
		return Purchase{}, errs.Validation(CodeDiscountTooLarge, "the discount is more than the invoice").
			WithField(FieldInvoiceDiscount, CodeDiscountTooLarge, "too large")
	}
	share(p.Lines, p.InvoiceDiscountMinor)
	if p.PaidNowMinor, err = parseMinor(in.PaidNow, cur, FieldPaidNow); err != nil {
		return Purchase{}, err
	}
	if p.PaidNowMinor > 0 {
		if p.PaidFrom, err = ParseSource(in.PaidFrom); err != nil {
			return Purchase{}, errs.Validation(CodePaidFromRequired, "say where the money came from").
				WithField(FieldPaidFrom, CodePaidFromRequired, "required")
		}
	}
	return p, nil
}

func priceLine(li LineInput, lineNo int, products map[id.ID]Product, cur Currency) (Line, error) {
	product, ok := products[li.ProductID]
	switch {
	case !ok:
		return Line{}, lineErr(errs.Validation(CodeProductUnknown, "no such product"), lineNo, FieldProduct)
	case product.OpenPrice:
		// An open-priced item is never counted in stock (2026-09-23): there is nothing to receive.
		return Line{}, lineErr(errs.Validation(CodeProductOpenPrice, "an open-priced item is never bought into stock").
			WithParam("name", product.NameAR), lineNo, FieldProduct)
	case !product.Active:
		return Line{}, lineErr(errs.Conflict(CodeProductInactive, "reactivate the product before buying it").
			WithParam("name", product.NameAR), lineNo, FieldProduct)
	}
	l := Line{LineNo: lineNo, ProductID: product.ID, NameAR: product.NameAR, NameEN: product.NameEN, UnitCode: product.UnitCode}
	var err error
	if l.QuantityMicro, err = parseQuantity(li.Quantity, product.UnitDecimals); err != nil || l.QuantityMicro <= 0 {
		if err == nil {
			err = errs.Validation(CodeQuantityInvalid, "a quantity above nothing")
		}
		return Line{}, lineErr(err, lineNo, FieldQuantity)
	}
	if li.Damaged != "" {
		if l.DamagedMicro, err = parseQuantity(li.Damaged, product.UnitDecimals); err != nil {
			return Line{}, lineErr(err, lineNo, FieldDamaged)
		}
		if l.DamagedMicro > l.QuantityMicro {
			return Line{}, lineErr(errs.Validation(CodeDamagedTooMany, "more damaged than arrived"), lineNo, FieldDamaged)
		}
	}
	if l.UnitCostMicro, err = parseMicro(li.UnitCost); err != nil {
		return Line{}, lineErr(errs.Validation(CodeCostInvalid, "a cost of nothing or more"), lineNo, FieldUnitCost)
	}
	// The good units at the unit price, rounded once to the currency: good × cost is at 10⁻¹².
	gross, fits := roundDiv(new(big.Int).Mul(big.NewInt(l.GoodMicro()), big.NewInt(l.UnitCostMicro)), pow10(12-cur.Decimals))
	if !fits {
		return Line{}, lineErr(errs.Validation(CodeAmountTooLarge, "the line is out of range"), lineNo, FieldUnitCost)
	}
	l.GrossMinor = gross
	switch {
	case li.DiscountPercent != "" && li.DiscountAmount != "":
		return Line{}, lineErr(errs.Validation(CodeDiscountBoth, "a discount is a percentage or an amount, not both"), lineNo, FieldDiscount)
	case li.DiscountPercent != "":
		if l.DiscountPercentMicro, err = parseMicro(li.DiscountPercent); err != nil || l.DiscountPercentMicro > 100_000_000 {
			return Line{}, lineErr(errs.Validation(CodeDiscountInvalid, "a percentage from 0 to 100"), lineNo, FieldDiscount)
		}
		// A percentage of a figure that fits, at most 100%, always fits.
		l.LineDiscountMinor, _ = roundDiv(new(big.Int).Mul(big.NewInt(l.GrossMinor), big.NewInt(l.DiscountPercentMicro)), big.NewInt(100_000_000))
	case li.DiscountAmount != "":
		if l.LineDiscountMinor, err = parseMinor(li.DiscountAmount, cur, FieldDiscount); err != nil {
			return Line{}, lineErr(err, lineNo, FieldDiscount)
		}
		if l.LineDiscountMinor > l.GrossMinor {
			return Line{}, lineErr(errs.Validation(CodeDiscountTooLarge, "the discount is more than the line"), lineNo, FieldDiscount)
		}
	}
	return l, nil
}

// share splits the invoice's discount over the lines in proportion to what each costs after its own discount, by
// largest remainder: the shares add to exactly the discount, and the rounding goes to the lines it favours least.
func share(lines []Line, discount int64) {
	if discount == 0 {
		return
	}
	var base int64
	for _, l := range lines {
		base += l.GrossMinor - l.LineDiscountMinor
	}
	if base == 0 {
		return
	}
	type rest struct {
		index     int
		remainder *big.Int
	}
	var given int64
	rests := make([]rest, 0, len(lines))
	for i, l := range lines {
		q, r := new(big.Int).QuoRem(new(big.Int).Mul(big.NewInt(discount), big.NewInt(l.GrossMinor-l.LineDiscountMinor)), big.NewInt(base), new(big.Int))
		lines[i].InvoiceShareMinor = q.Int64()
		given += q.Int64()
		rests = append(rests, rest{index: i, remainder: r})
	}
	// Largest remainder first; among equals, the earlier line. Stable and repeatable, so a quote and its record agree.
	for left := discount - given; left > 0; left-- {
		best := -1
		for j, r := range rests {
			if r.remainder == nil {
				continue
			}
			if best < 0 || r.remainder.Cmp(rests[best].remainder) > 0 {
				best = j
			}
		}
		lines[rests[best].index].InvoiceShareMinor++
		rests[best].remainder = nil
	}
}

// ParseRate reads local currency per dollar, as typed, into 10⁻⁹ — the precision every rate in Lite is kept at.
func ParseRate(raw string) (int64, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, errs.Validation(CodeRateRequired, "the rate this purchase was paid at").WithField(FieldRate, CodeRateRequired, "required")
	}
	normalised, err := numinput.Normalise(raw)
	if err != nil {
		return 0, errs.Validation(CodeRateInvalid, "not a rate").WithField(FieldRate, CodeRateInvalid, "invalid")
	}
	nano, ok := scaled(normalised, 9)
	if !ok || nano <= 0 {
		return 0, errs.Validation(CodeRateInvalid, "a rate above nothing").WithField(FieldRate, CodeRateInvalid, "invalid")
	}
	return nano, nil
}

func parseQuantity(raw string, decimals int) (int64, error) {
	normalised, err := numinput.Normalise(raw)
	if err != nil {
		return 0, errs.Validation(CodeQuantityInvalid, "not a quantity")
	}
	if numinput.Decimals(normalised) > decimals {
		return 0, errs.Validation(CodeQuantityDecimals, "too many decimals for the unit").WithParam("decimals", strconv.Itoa(decimals))
	}
	micro, ok := scaled(normalised, 6)
	if !ok {
		return 0, errs.Validation(CodeQuantityInvalid, "out of range")
	}
	return micro, nil
}

// parseMicro reads a figure at 10⁻⁶ — a unit cost or a percentage: nothing or more, at most six decimals.
func parseMicro(raw string) (int64, error) {
	normalised, err := numinput.Normalise(raw)
	if err != nil || numinput.Decimals(normalised) > 6 {
		return 0, errs.Validation(CodeCostInvalid, "not a figure")
	}
	micro, ok := scaled(normalised, 6)
	if !ok {
		return 0, errs.Validation(CodeCostInvalid, "out of range")
	}
	return micro, nil
}

// parseMinor reads an amount in the currency's minor units; "" is nothing.
func parseMinor(raw string, cur Currency, field string) (int64, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	normalised, err := numinput.Normalise(raw)
	if err != nil {
		return 0, withField(err, field)
	}
	if numinput.Decimals(normalised) > cur.Decimals {
		return 0, errs.Validation(CodeAmountDecimals, "too many decimals for the currency").
			WithField(field, CodeAmountDecimals, "too many decimals").WithParam("decimals", strconv.Itoa(cur.Decimals))
	}
	minor, ok := scaled(normalised, cur.Decimals)
	if !ok {
		return 0, errs.Validation(CodeAmountTooLarge, "out of range").WithField(field, CodeAmountTooLarge, "too large")
	}
	return minor, nil
}

// ParseAmount reads a payment's or an opening's amount in a currency's minor units; above nothing.
func ParseAmount(raw string, cur Currency) (int64, error) {
	minor, err := parseMinor(raw, cur, FieldAmount)
	if err != nil {
		return 0, err
	}
	if minor <= 0 {
		return 0, errs.Validation(CodeAmountRequired, "an amount above nothing").WithField(FieldAmount, CodeAmountRequired, "required")
	}
	return minor, nil
}

func parseText(raw string, limit int, field, code string) (string, error) {
	s := strings.TrimSpace(raw)
	if utf8.RuneCountInString(s) > limit {
		return "", errs.Validation(code, "too long").WithField(field, code, "too long").WithParam("max", strconv.Itoa(limit))
	}
	return s, nil
}

// scaled turns a normalised decimal into an integer at 10⁻scale, exactly; false when it does not fit.
func scaled(normalised string, scale int) (int64, bool) {
	whole, frac, _ := strings.Cut(normalised, ".")
	if len(frac) > scale {
		return 0, false
	}
	digits := whole + frac + strings.Repeat("0", scale-len(frac))
	v, err := strconv.ParseInt(digits, 10, 64)
	return v, err == nil
}

func findCurrency(currencies []Currency, code string) (Currency, bool) {
	for _, c := range currencies {
		if c.Code == code {
			return c, true
		}
	}
	return Currency{}, false
}

func pow10(n int) *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil) }

// roundDiv is num ÷ den rounded half up, for non-negative figures, and whether it fits an int64 — a figure that does not
// is refused by the caller, never read as nought.
func roundDiv(num, den *big.Int) (int64, bool) {
	q, r := new(big.Int).QuoRem(num, den, new(big.Int))
	if new(big.Int).Lsh(r, 1).Cmp(den) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if !q.IsInt64() {
		return 0, false
	}
	return q.Int64(), true
}

func lineErr(err error, lineNo int, field string) error {
	typed, ok := errs.AsError(err)
	if !ok {
		return err
	}
	return typed.WithField(fmt.Sprintf("lines.%d.%s", lineNo, field), typed.Code, "invalid").WithParam("line", strconv.Itoa(lineNo))
}

func withField(err error, field string) error {
	typed, ok := errs.AsError(err)
	if !ok {
		return err
	}
	return typed.WithField(field, typed.Code, "invalid")
}

// ErrPurchaseNotFound is a purchase that does not exist.
func ErrPurchaseNotFound() error { return errs.NotFound(CodePurchaseNotFound, "no such purchase") }
