package domain

import (
	"math/big"
	"strconv"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/quantity"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
)

// ParseQuantity reads a quantity as typed, in a unit with the given input decimals: "12.5" kg, "3" jars.
func ParseQuantity(raw string, unitDecimals int, field string) (int64, error) {
	normalised, err := numinput.Normalise(raw)
	if err != nil {
		return 0, withField(err, field)
	}
	if numinput.Decimals(normalised) > unitDecimals {
		return 0, errs.Validation(CodeQuantityDecimals, "too many decimals for the unit").
			WithField(field, CodeQuantityDecimals, "too many decimals").
			WithParam("decimals", strconv.Itoa(unitDecimals))
	}
	v, err := decimalMicro(normalised)
	if err != nil {
		return 0, errs.Validation(CodeQuantityTooLarge, "quantity too large").
			WithField(field, CodeQuantityTooLarge, "too large")
	}
	return v, nil
}

// CostMode says whether a typed cost is the invoice total or the cost of one unit (Q-L2.7).
type CostMode string

// The cost modes. Total is the default, because invoices state totals.
const (
	CostTotal CostMode = "total"
	CostUnit  CostMode = "unit"
)

// CostInput is a receipt's cost as typed.
type CostInput struct {
	Mode     CostMode
	Amount   string
	Currency string
	// Rate is pounds per dollar, typed on the receipt when the currency is not USD (Q-L2.1). Ignored for USD.
	Rate string
	// DiscountPercent is a supplier's discount off the cost typed, a percentage above nothing and below a hundred, or ""
	// for none (0.10.0). The delivery is costed at what the shop really paid, so the average cost is.
	DiscountPercent string
}

// ResolveCost turns a typed cost for quantityMicro into a USD unit cost, keeping what was typed (L2 §5.1).
//
// A total becomes a unit cost with money.UnitCostFromTotal at 10⁻⁶ of the major unit — never rounded to the
// currency's decimals first (H5). A cost in another currency becomes USD with DivideByRate, one rounding.
func ResolveCost(in CostInput, quantityMicro int64, currencies []Currency) (Cost, error) {
	currency, ok := findCurrency(currencies, in.Currency)
	if !ok {
		return Cost{}, errs.Validation(CodeUnknownCurrency, "unknown currency").
			WithField(FieldCurrency, CodeUnknownCurrency, "unknown").WithParam("value", in.Currency)
	}
	kernelCurrency, err := money.NewCurrency(currency.Code, uint8(currency.Decimals), costRounding) //nolint:gosec // 0–4 by the schema's CHECK
	if err != nil {
		return Cost{}, err
	}
	if quantityMicro <= 0 {
		return Cost{}, quantityRequired()
	}

	normalised, err := numinput.Normalise(in.Amount)
	if err != nil {
		return Cost{}, withField(err, FieldCost)
	}
	var unit money.UnitAmount
	switch in.Mode {
	case CostTotal, "":
		if numinput.Decimals(normalised) > currency.Decimals {
			return Cost{}, costDecimals(currency.Code, currency.Decimals)
		}
		if unit, err = unitCostFromTotal(kernelCurrency, normalised, quantityMicro); err != nil {
			return Cost{}, err
		}
	case CostUnit:
		if numinput.Decimals(normalised) > 6 {
			return Cost{}, costDecimals(currency.Code, 6)
		}
		if unit, err = money.ParseUnitAmount(kernelCurrency, normalised); err != nil {
			return Cost{}, costTooLarge(err)
		}
	default:
		return Cost{}, errs.Validation(numinput.CodeInvalid, "unknown cost mode").WithParam("value", string(in.Mode))
	}

	if unit, err = discounted(unit, kernelCurrency, in.DiscountPercent); err != nil {
		return Cost{}, err
	}

	if currency.Code == CostCurrency {
		return Cost{UnitCostMicro: unit.Micro(), Entered: Entered{Currency: currency.Code, UnitCostMicro: unit.Micro()}}, nil
	}
	rate, err := parseRate(in.Rate)
	if err != nil {
		return Cost{}, err
	}
	converted, err := unit.DivideByRate(rate, usdCurrency, costRounding)
	if err != nil {
		return Cost{}, costTooLarge(err)
	}
	return Cost{
		UnitCostMicro: converted.Micro(),
		Entered:       Entered{Currency: currency.Code, UnitCostMicro: unit.Micro(), LocalPerUSDNano: rate.Nano()},
	}, nil
}

// ParseUSDCost reads a USD unit cost as typed, to six decimals — a cost correction's figure.
func ParseUSDCost(raw string) (int64, error) {
	normalised, err := numinput.Normalise(raw)
	if err != nil {
		return 0, withField(err, FieldCost)
	}
	if numinput.Decimals(normalised) > 6 {
		return 0, costDecimals(CostCurrency, 6)
	}
	v, err := decimalMicro(normalised)
	if err != nil {
		return 0, costTooLarge(err)
	}
	return v, nil
}

// unitCostFromTotal divides a typed invoice total by the quantity, at 10⁻⁶ of the major unit, one rounding.
func unitCostFromTotal(c money.Currency, normalised string, quantityMicro int64) (money.UnitAmount, error) {
	total, err := money.Parse(c, normalised)
	if err != nil {
		return money.UnitAmount{}, costTooLarge(err)
	}
	// The quantity's unit plays no part in the division; a placeholder unit that allows fractions carries it.
	qty, err := quantity.FromMicro(anyUnit, quantityMicro)
	if err != nil {
		return money.UnitAmount{}, err
	}
	unit, err := money.UnitCostFromTotal(total, qty, costRounding)
	if err != nil {
		return money.UnitAmount{}, costTooLarge(err)
	}
	return unit, nil
}

func parseRate(raw string) (money.Rate, error) {
	if raw == "" {
		return money.Rate{}, rateRequired()
	}
	normalised, err := numinput.Normalise(raw)
	if err != nil {
		return money.Rate{}, withField(err, FieldRate)
	}
	rate, err := money.ParseRate(normalised)
	if err != nil || rate.Sign() <= 0 {
		return money.Rate{}, rateRequired()
	}
	return rate, nil
}

// anyUnit carries a quantity through UnitCostFromTotal, which reads only its micro value.
var anyUnit, _ = quantity.NewUnit("any", "any", quantity.FactorScale, true)

// decimalMicro reads a normalised non-negative decimal of at most six decimals at 10⁻⁶.
func decimalMicro(normalised string) (int64, error) {
	unit, err := money.ParseUnitAmount(usdCurrency, normalised)
	if err != nil {
		return 0, err
	}
	return unit.Micro(), nil
}

// usdCurrency is the cost currency as the kernel knows it. Its two decimals are the schema's seed.
var usdCurrency, _ = money.NewCurrency(CostCurrency, 2, costRounding)

func findCurrency(currencies []Currency, code string) (Currency, bool) {
	for _, c := range currencies {
		if c.Code == code {
			return c, true
		}
	}
	return Currency{}, false
}

func costDecimals(currency string, decimals int) error {
	return errs.Validation(CodeCostDecimals, "too many decimals for the cost").
		WithField(FieldCost, CodeCostDecimals, "too many decimals").
		WithParam("currency", currency).WithParam("decimals", strconv.Itoa(decimals))
}

func rateRequired() error {
	return errs.Validation(CodeRateRequired, "the rate the shop paid at is required").
		WithField(FieldRate, CodeRateRequired, "required")
}

// withField attaches a form field to a typed validation error from another package.
func withField(err error, field string) error {
	if typed, ok := errs.AsError(err); ok {
		return typed.WithField(field, typed.Code, "invalid")
	}
	return err
}

// discounted takes a supplier's discount off a unit cost, half up at 10⁻⁶ of the major unit (0.10.0).
func discounted(unit money.UnitAmount, c money.Currency, raw string) (money.UnitAmount, error) {
	if strings.TrimSpace(raw) == "" {
		return unit, nil
	}
	invalid := errs.Validation(CodeDiscountInvalid, "a discount is a percentage above nothing and below a hundred").
		WithField(FieldDiscount, CodeDiscountInvalid, "invalid")
	normalised, err := numinput.Normalise(raw)
	if err != nil || numinput.Decimals(normalised) > 4 {
		return unit, invalid
	}
	percentMicro, err := decimalMicro(normalised)
	if err != nil || percentMicro <= 0 || percentMicro >= 100_000_000 {
		return unit, invalid
	}
	off, rest := new(big.Int).QuoRem(new(big.Int).Mul(big.NewInt(unit.Micro()), big.NewInt(percentMicro)), big.NewInt(100_000_000), new(big.Int))
	if new(big.Int).Lsh(rest, 1).Cmp(big.NewInt(100_000_000)) >= 0 {
		off.Add(off, big.NewInt(1))
	}
	return money.UnitFromMicro(c, unit.Micro()-off.Int64()), nil
}
