package money

import (
	"math/big"

	"github.com/mizan-erp/mizan/internal/kernel/quantity"
	"github.com/mizan-erp/mizan/internal/kernel/round"
)

// unitScale is the fixed-point scale for UnitAmount: 10^-6 of the currency's MAJOR
// unit. lineScale is unitScale × quantity.Scale = 10^-12, the denominator of a
// price × quantity product before it is scaled to minor units.
const (
	unitScale = 1_000_000
	lineScale = int64(unitScale) * int64(quantity.Scale) // 10^12
)

var bigLineScale = big.NewInt(lineScale)

// UnitAmount is a per-unit price or cost, held at 10^-6 of the currency's major unit.
//
// # Why it exists
//
// A unit price or cost is routinely a fraction of a minor unit — the cost of a gram
// of a compound, a metre of cable. Rounding it to whole minor units and then
// multiplying by thousands of units produces a materially wrong total. UnitAmount
// keeps four extra decimals beyond a 2-decimal currency, so the precision survives
// until the single rounding in LineExtension.
//
// # When to use
//
// Use UnitAmount for catalogue prices and inventory unit costs. It is an INPUT to one
// operation — LineExtension — not a general calculator; it deliberately has no
// arithmetic of its own. Settled amounts are Money.
//
// # What it prevents
//
//   - Valuation error from prematurely rounding unit costs.
//   - Ad-hoc price × quantity code with inconsistent rounding (LineExtension is the
//     sole authority).
type UnitAmount struct {
	micro    int64
	currency Currency
}

// UnitFromMicro builds a UnitAmount from a raw 10^-6-major amount (the stored form).
func UnitFromMicro(c Currency, micro int64) UnitAmount {
	return UnitAmount{micro: micro, currency: c}
}

// ParseUnitAmount parses a decimal string ("1.234567") into a UnitAmount for
// currency c, allowing up to six fractional digits of the major unit.
func ParseUnitAmount(c Currency, s string) (UnitAmount, error) {
	if c.IsZero() {
		return UnitAmount{}, ErrNoCurrency
	}
	v, err := parseScaled(s, 6)
	if err != nil {
		return UnitAmount{}, err
	}
	iv, ok := int64FromBig(v)
	if !ok {
		return UnitAmount{}, ErrOverflow
	}
	return UnitAmount{micro: iv, currency: c}, nil
}

// Micro returns the amount at 10^-6-major precision — the value persisted to BIGINT.
func (u UnitAmount) Micro() int64 { return u.micro }

// Currency returns the price's currency.
func (u UnitAmount) Currency() Currency { return u.currency }

// IsZero reports whether the price is zero.
func (u UnitAmount) IsZero() bool { return u.micro == 0 }

// String returns a debug representation such as "USD 1.234567/u".
func (u UnitAmount) String() string {
	if u.currency.IsZero() {
		return "<no currency> " + formatScaled(u.micro, 6) + "/u"
	}
	return u.currency.code + " " + formatScaled(u.micro, 6) + "/u"
}

// LineExtension computes price × quantity as a settled Money amount, rounding EXACTLY
// ONCE with mode. It is the only place in the entire system where a price is
// multiplied by a quantity, which is what guarantees consistent rounding across
// sales, purchasing, and costing.
//
//	minor = round( price.micro × qty.Micro() × 10^decimals / 10^12 )
//
// The numerator is computed in arbitrary precision (math/big), so no intermediate
// overflows before the final fit into int64 minor units. Discounts and taxes then
// operate on the returned Money.
func LineExtension(price UnitAmount, qty quantity.Quantity, mode round.RoundingMode) (Money, error) {
	if price.currency.IsZero() {
		return Money{}, ErrNoCurrency
	}
	num := new(big.Int).Mul(big.NewInt(price.micro), big.NewInt(qty.Micro()))
	num.Mul(num, pow10(int(price.currency.decimals)))
	q := round.Div(num, bigLineScale, mode)
	v, ok := int64FromBig(q)
	if !ok {
		return Money{}, ErrOverflow
	}
	return Money{minor: v, currency: price.currency}, nil
}
