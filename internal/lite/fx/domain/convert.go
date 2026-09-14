package domain

import (
	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/quantity"
	"github.com/mizan-erp/mizan/internal/kernel/round"
)

// Currency is a currency a conversion lands in.
type Currency struct {
	Code     string
	Decimals int
}

// conversionRounding is the one rounding a displayed conversion uses.
const conversionRounding = round.HalfUp

// oneUnitOf carries a quantity through the kernel, which reads only its micro value.
var oneUnitOf, _ = quantity.NewUnit("any", "any", quantity.FactorScale, true)

// Converted is a figure in another currency, and the rate it was converted at.
type Converted struct {
	Minor    int64
	Currency Currency
}

// PriceInOther is one unit of a product at priceMicro in priceCurrency, in the other of the rate's two currencies, rounded
// once (L3 §5, DESIGN §4.4): a dollar price in the local currency, or a local price in dollars. ok is false for a price in
// a currency the rate does not price.
func (r Rate) PriceInOther(priceCurrency string, priceMicro int64, local, usd Currency) (Converted, bool, error) {
	one, err := quantity.FromMicro(oneUnitOf, 1_000_000)
	if err != nil {
		return Converted{}, false, err
	}
	return r.line(priceCurrency, priceMicro, one, local, usd)
}

// LineInLocal is a USD unit cost × quantity valued in the local currency, rounded once — a stock line in pounds (L3 §5.4).
func (r Rate) LineInLocal(usdUnitMicro, quantityMicro int64, local, usd Currency) (Converted, error) {
	qty, err := quantity.FromMicro(oneUnitOf, quantityMicro)
	if err != nil {
		return Converted{}, err
	}
	c, _, err := r.line(usd.Code, usdUnitMicro, qty, local, usd)
	return c, err
}

func (r Rate) line(priceCurrency string, priceMicro int64, qty quantity.Quantity, local, usd Currency) (Converted, bool, error) {
	rate := money.RateFromNano(r.Nano)
	localCur, err := money.NewCurrency(local.Code, uint8(local.Decimals), conversionRounding) //nolint:gosec // 0–4 by the schema's CHECK
	if err != nil {
		return Converted{}, false, err
	}
	usdCur, err := money.NewCurrency(usd.Code, uint8(usd.Decimals), conversionRounding) //nolint:gosec // 0–4 by the schema's CHECK
	if err != nil {
		return Converted{}, false, err
	}
	switch priceCurrency {
	case usd.Code:
		v, err := money.LineExtensionMulRate(money.UnitFromMicro(usdCur, priceMicro), qty, rate, localCur, conversionRounding)
		return Converted{Minor: v.Minor(), Currency: local}, err == nil, err
	case local.Code:
		v, err := money.LineExtensionDivRate(money.UnitFromMicro(localCur, priceMicro), qty, rate, usdCur, conversionRounding)
		return Converted{Minor: v.Minor(), Currency: usd}, err == nil, err
	default:
		return Converted{}, false, nil
	}
}

// Text is the converted figure formatted in its currency's decimals.
func (c Converted) Text() string { return FormatMinor(c.Minor, c.Currency.Decimals) }
