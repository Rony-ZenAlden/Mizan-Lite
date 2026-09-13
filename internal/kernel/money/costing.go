package money

import (
	"math/big"

	"github.com/mizan-erp/mizan/internal/kernel/quantity"
	"github.com/mizan-erp/mizan/internal/kernel/round"
)

// The costing arithmetic: a moving average, a cost spread over the content of a package, a unit cost from an
// invoice total, and a unit cost converted by an exchange rate.
//
// # Why it is in the kernel
//
// Each is a division whose rounding must happen exactly once, on integers, with intermediates that overflow
// int64 at quantities and costs a real business reaches (on hand × average is 10⁻⁶ × 10⁻⁶ = 10⁻¹²). Mizan's own
// inventory module found its costing wrong by a factor of a hundred for two phases because a scale conversion
// lived in one module and was tested only in a currency with no minor unit. Arithmetic that more than one module
// needs belongs in one place, with one set of tests at every scale.
//
// Unit costs here are at the UnitAmount scale: 10⁻⁶ of the currency's MAJOR unit. Quantities are 10⁻⁶ of their
// unit. Nothing takes or returns a float.

// WeightedAverageUnit returns the moving-average unit cost after receiving qtyMicro at costMicro into
// onHandMicro carrying avgMicro, rounded once with mode.
//
//	(onHand × avg + qty × cost) ÷ (onHand + qty)
//
// When nothing is on hand — or less than nothing, because stock went out that was never booked in — the
// receipt's own cost is returned. Averaging against a negative quantity produces a figure with no meaning:
// receiving 10 at 120 into −5 carrying a phantom average of 999 would give −759.
func WeightedAverageUnit(onHandMicro, avgMicro, qtyMicro, costMicro int64, mode round.RoundingMode) (int64, error) {
	if qtyMicro <= 0 {
		return 0, ErrNonPositiveQuantity
	}
	if costMicro < 0 || avgMicro < 0 {
		return 0, ErrNegativeCost
	}
	if onHandMicro <= 0 {
		return costMicro, nil
	}
	existing := new(big.Int).Mul(big.NewInt(onHandMicro), big.NewInt(avgMicro))
	incoming := new(big.Int).Mul(big.NewInt(qtyMicro), big.NewInt(costMicro))
	total := new(big.Int).Add(existing, incoming)
	quantityAfter := new(big.Int).Add(big.NewInt(onHandMicro), big.NewInt(qtyMicro))
	v, ok := int64FromBig(round.Div(total, quantityAfter, mode))
	if !ok {
		return 0, ErrOverflow
	}
	return v, nil
}

// SplitUnitCost spreads a unit cost carried by fromQtyMicro units over toQtyMicro units, rounded once:
//
//	unitCost × from ÷ to
//
// One 16-litre tin at 95.000000 opened into 16 litres gives 5.937500 a litre. The value of what went in and
// what came out can differ only by the rounding of this one division.
func SplitUnitCost(unitCostMicro, fromQtyMicro, toQtyMicro int64, mode round.RoundingMode) (int64, error) {
	if fromQtyMicro <= 0 || toQtyMicro <= 0 {
		return 0, ErrNonPositiveQuantity
	}
	if unitCostMicro < 0 {
		return 0, ErrNegativeCost
	}
	num := new(big.Int).Mul(big.NewInt(unitCostMicro), big.NewInt(fromQtyMicro))
	v, ok := int64FromBig(round.Div(num, big.NewInt(toQtyMicro), mode))
	if !ok {
		return 0, ErrOverflow
	}
	return v, nil
}

// UnitCostFromTotal turns an invoice total into a unit cost at the UnitAmount scale, rounded once.
//
// Invoices state totals — 25 kg for 450,000 — and a unit cost worked out by hand and rounded to the currency's
// decimals before being multiplied back is how a stock value drifts. The unit cost keeps six decimals of the
// major unit, whatever the currency's own decimals are.
//
//	unit (10⁻⁶ major) = total.minor × 10⁶ × 10⁶ ÷ (10^decimals × qty.micro)
func UnitCostFromTotal(total Money, qty quantity.Quantity, mode round.RoundingMode) (UnitAmount, error) {
	if total.currency.IsZero() {
		return UnitAmount{}, ErrNoCurrency
	}
	if qty.Micro() <= 0 {
		return UnitAmount{}, ErrNonPositiveQuantity
	}
	if total.minor < 0 {
		return UnitAmount{}, ErrNegativeCost
	}
	num := new(big.Int).Mul(big.NewInt(total.minor), bigLineScale)
	den := new(big.Int).Mul(pow10(int(total.currency.decimals)), big.NewInt(qty.Micro()))
	v, ok := int64FromBig(round.Div(num, den, mode))
	if !ok {
		return UnitAmount{}, ErrOverflow
	}
	return UnitAmount{micro: v, currency: total.currency}, nil
}

// DivideByRate converts a unit amount into another currency, given how many units of this amount's currency
// one unit of the target costs — a rate quoted "local per USD", as a pantry shop quotes it. One rounding:
//
//	target (10⁻⁶ major) = u.micro × 10⁹ ÷ rate.nano
//
// The rate is never inverted first: inverting 15,000 to 0.0000666… and multiplying rounds twice.
func (u UnitAmount) DivideByRate(localPerTarget Rate, to Currency, mode round.RoundingMode) (UnitAmount, error) {
	if u.currency.IsZero() || to.IsZero() {
		return UnitAmount{}, ErrNoCurrency
	}
	if localPerTarget.nano <= 0 {
		return UnitAmount{}, ErrInvalidRate
	}
	if u.micro < 0 {
		return UnitAmount{}, ErrNegativeCost
	}
	num := new(big.Int).Mul(big.NewInt(u.micro), big.NewInt(1_000_000_000))
	v, ok := int64FromBig(round.Div(num, big.NewInt(localPerTarget.nano), mode))
	if !ok {
		return UnitAmount{}, ErrOverflow
	}
	return UnitAmount{micro: v, currency: to}, nil
}
