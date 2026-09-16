package domain

import (
	"math/big"
	"strconv"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/round"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
)

// The cost price a shop types, and the margin between it and the selling price (L9, the owner's request of 2026-09-16).
//
// # Why the margin is not stored
//
// A margin is the distance between two prices the product already carries. Storing it would be a third number free to
// disagree with the two it came from — the same reasoning that keeps every figure a string at the boundary (DESIGN D9).
// Margin() computes it; MarginPercent and MarginAmount are how a form asks for a sell price to be worked out.
const (
	CodeCostDecimals    = "lite.catalog.cost_decimals"
	CodeCostNotPositive = "lite.catalog.cost_not_positive"
	CodeMarginInvalid   = "lite.catalog.margin_invalid"
	CodeMarginBelowCost = "lite.catalog.margin_below_cost"
	CodeCostCurrency    = "lite.catalog.cost_currency"
)

// Field names a form marks.
const (
	FieldCost   = "costPrice"
	FieldMargin = "margin"
)

// SetCost records what the shop believes one unit costs it, in the product's own selling currency.
//
// The currency is not free: a product priced in pounds whose cost is in dollars would need a rate to compare the two, and a
// rate that moves would make the margin move with it. One currency for both, and the margin is arithmetic.
//
// An empty amount clears the cost — a shop that typed one by mistake must be able to take it back.
func (p Product) SetCost(raw string, ref Reference) (Product, error) {
	if raw == "" {
		p.CostMicro = 0
		p.HasCost = false
		return p, nil
	}
	currency, ok := ref.Currencies[p.PriceCurrency]
	if !ok {
		return p, errs.Validation(CodeUnknownCurrency, "unknown currency").
			WithField(FieldCost, CodeUnknownCurrency, "unknown currency").WithParam("value", p.PriceCurrency)
	}
	normalised, err := numinput.Normalise(raw)
	if err != nil {
		return p, withField(err, FieldCost)
	}
	if numinput.Decimals(normalised) > currency.Decimals {
		return p, errs.Validation(CodeCostDecimals, "too many decimals for the currency").
			WithField(FieldCost, CodeCostDecimals, "too many decimals").
			WithParam("currency", currency.Code).WithParam("decimals", strconv.Itoa(currency.Decimals))
	}
	kernelCurrency, err := money.NewCurrency(currency.Code, uint8(currency.Decimals), round.HalfUp) //nolint:gosec // 0–4 by the schema's CHECK
	if err != nil {
		return p, err
	}
	amount, err := money.ParseUnitAmount(kernelCurrency, normalised)
	if err != nil {
		return p, errs.Validation(numinput.CodeInvalid, "cost out of range").
			WithField(FieldCost, numinput.CodeInvalid, "out of range")
	}
	if amount.Micro() <= 0 {
		return p, errs.Validation(CodeCostNotPositive, "a cost price is more than nothing").
			WithField(FieldCost, CodeCostNotPositive, "more than nothing")
	}
	p.CostMicro = amount.Micro()
	p.HasCost = true
	return p, nil
}

// Margin is what the shop makes on one unit: the amount, and the percentage of the cost it represents.
//
// The percentage is of the COST, not of the price — "I buy at 2 and sell at 2.50" is a margin of 25%, which is how a
// shopkeeper says it. Both are zero when no cost is known, and the amount may be negative: a product sold below cost is a
// fact a shop needs shown, not an error to refuse.
func (p Product) Margin() (amountMicro int64, percentMicro int64, known bool) {
	if !p.HasCost || p.CostMicro <= 0 {
		return 0, 0, false
	}
	amountMicro = p.PriceMicro - p.CostMicro
	// percent = amount / cost × 100, at 10⁻⁶ of a percentage point, rounded half away from zero.
	percentMicro = mulDivRound(amountMicro, 100_000_000, p.CostMicro)
	return amountMicro, percentMicro, true
}

// PriceFromMarginPercent works the selling price out of the cost and a margin percentage — the form's "I buy at 2 and want
// 25%" direction. The percentage arrives as a plain number of percentage points ("25", "12٫5").
func (p Product) PriceFromMarginPercent(raw string, ref Reference) (Product, error) {
	if !p.HasCost {
		return p, errs.Validation(CodeMarginInvalid, "a margin needs a cost price first").
			WithField(FieldMargin, CodeMarginInvalid, "needs a cost price")
	}
	normalised, err := normaliseSigned(raw)
	if err != nil {
		return p, withField(err, FieldMargin)
	}
	if numinput.Decimals(strings.TrimPrefix(normalised, "-")) > 4 {
		return p, errs.Validation(CodeMarginInvalid, "a margin takes at most four decimals").
			WithField(FieldMargin, CodeMarginInvalid, "too many decimals")
	}
	percentMicro, err := micros(normalised)
	if err != nil {
		return p, errs.Validation(CodeMarginInvalid, "margin out of range").
			WithField(FieldMargin, CodeMarginInvalid, "out of range")
	}
	if percentMicro <= -100_000_000 {
		return p, errs.Validation(CodeMarginBelowCost, "a margin cannot take the price below nothing").
			WithField(FieldMargin, CodeMarginBelowCost, "below nothing")
	}
	// price = cost × (1 + percent/100)
	price := p.CostMicro + mulDivRound(p.CostMicro, percentMicro, 100_000_000)
	return p.withPrice(price, ref)
}

// PriceFromMarginAmount works the selling price out of the cost and a margin in money — "I buy at 2 and want half a dollar".
func (p Product) PriceFromMarginAmount(raw string, ref Reference) (Product, error) {
	if !p.HasCost {
		return p, errs.Validation(CodeMarginInvalid, "a margin needs a cost price first").
			WithField(FieldMargin, CodeMarginInvalid, "needs a cost price")
	}
	currency, ok := ref.Currencies[p.PriceCurrency]
	if !ok {
		return p, errs.Validation(CodeUnknownCurrency, "unknown currency").
			WithField(FieldMargin, CodeUnknownCurrency, "unknown currency")
	}
	normalised, err := normaliseSigned(raw)
	if err != nil {
		return p, withField(err, FieldMargin)
	}
	if numinput.Decimals(strings.TrimPrefix(normalised, "-")) > currency.Decimals {
		return p, errs.Validation(CodeMarginInvalid, "too many decimals for the currency").
			WithField(FieldMargin, CodeMarginInvalid, "too many decimals").
			WithParam("currency", currency.Code).WithParam("decimals", strconv.Itoa(currency.Decimals))
	}
	marginMicro, err := micros(normalised)
	if err != nil {
		return p, errs.Validation(CodeMarginInvalid, "margin out of range").
			WithField(FieldMargin, CodeMarginInvalid, "out of range")
	}
	if p.CostMicro+marginMicro < 0 {
		return p, errs.Validation(CodeMarginBelowCost, "a margin cannot take the price below nothing").
			WithField(FieldMargin, CodeMarginBelowCost, "below nothing")
	}
	return p.withPrice(p.CostMicro+marginMicro, ref)
}

// withPrice puts a computed price on the product, held to the currency's decimals like any typed one.
func (p Product) withPrice(priceMicro int64, ref Reference) (Product, error) {
	currency, ok := ref.Currencies[p.PriceCurrency]
	if !ok {
		return p, errs.Validation(CodeUnknownCurrency, "unknown currency").WithParam("value", p.PriceCurrency)
	}
	// Round the computed price to what the currency can actually hold: a pound has no fractions to charge.
	step := int64(1)
	for range currency.Decimals {
		step *= 10
	}
	unit := 1_000_000 / step
	if unit > 1 {
		priceMicro = ((priceMicro + unit/2) / unit) * unit
	}
	if priceMicro < 0 {
		return p, errs.Validation(CodeMarginBelowCost, "a price cannot be below nothing").
			WithField(FieldMargin, CodeMarginBelowCost, "below nothing")
	}
	p.PriceMicro = priceMicro
	return p, nil
}

// normaliseSigned reads a figure that may be negative. numinput.Normalise is built for quantities and prices, which are never
// below nothing; a margin may be — a shop that sells below cost is doing something real, and refusing to express it would
// leave the form unable to say what the shop is already doing.
func normaliseSigned(raw string) (string, error) {
	trimmed := strings.TrimSpace(numinput.LatinDigits(raw))
	negative := strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "\u2212") // ASCII hyphen or the real minus sign
	unsigned := strings.TrimLeft(trimmed, "+-\u2212")
	normalised, err := numinput.Normalise(unsigned)
	if err != nil {
		return "", err
	}
	if negative {
		return "-" + normalised, nil
	}
	return normalised, nil
}

// micros reads a normalised decimal into 10⁻⁶ units. A margin percentage is not money, so it is parsed against a six-decimal
// scale of its own rather than a currency's.
func micros(normalised string) (int64, error) {
	scaled, ok := fixedParse(normalised)
	if !ok {
		return 0, errs.Validation(numinput.CodeInvalid, "out of range")
	}
	return scaled, nil
}

// fixedParse reads a signed decimal of at most six places as 10⁻⁶ units, refusing what will not fit in an int64.
func fixedParse(normalised string) (int64, bool) {
	negative := strings.HasPrefix(normalised, "-")
	digits := strings.TrimLeft(normalised, "+-")
	whole, fraction, _ := strings.Cut(digits, ".")
	if whole == "" {
		whole = "0"
	}
	if len(fraction) > 6 {
		return 0, false
	}
	fraction += strings.Repeat("0", 6-len(fraction))
	n, err := strconv.ParseInt(whole+fraction, 10, 64)
	if err != nil {
		return 0, false
	}
	if negative {
		n = -n
	}
	return n, true
}

// mulDivRound is a×b÷c rounded half away from zero, in big.Int so that a margin on a large pound price cannot overflow
// halfway through the multiplication and come back as a plausible wrong number.
func mulDivRound(a, b, c int64) int64 {
	if c == 0 {
		return 0
	}
	product := new(big.Int).Mul(big.NewInt(a), big.NewInt(b))
	divisor := big.NewInt(c)
	quotient, remainder := new(big.Int).QuoRem(product, divisor, new(big.Int))
	// Half away from zero: twice the remainder reaching the divisor rounds the quotient one further out.
	twice := new(big.Int).Abs(new(big.Int).Lsh(remainder, 1))
	if twice.Cmp(new(big.Int).Abs(divisor)) >= 0 {
		if product.Sign()*divisor.Sign() < 0 {
			quotient.Sub(quotient, big.NewInt(1))
		} else {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	if !quotient.IsInt64() {
		return 0
	}
	return quotient.Int64()
}
