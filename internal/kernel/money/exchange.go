package money

import (
	"math/big"

	"github.com/mizan-erp/mizan/internal/kernel/quantity"
	"github.com/mizan-erp/mizan/internal/kernel/round"
)

// Conversion between two currencies at a rate quoted in ONE direction, rounded once.
//
// # Why these exist beside LineExtension and MulRate
//
// Composing them rounds twice. Half a litre at $3.33 is $1.665, which LineExtension rounds to $1.67, and $1.67 at 13,000
// is 21,710 pounds; the exact figure is 21,645. Sixty-five pounds on one line of one receipt. And converting the other
// way by inverting the rate first — 1 ÷ 15,000 = 0.0000666… — rounds the inverse before it rounds the answer.
//
// So a rate is always "units of currency B per one unit of currency A", the caller chooses whether the amount is being
// multiplied (A → B) or divided (B → A), and each function below is one big-integer expression and one rounding.

var bigNano = big.NewInt(1_000_000_000)

// LineExtensionMulRate is price × quantity × rate in currency `to`, rounded once: a line priced in A, valued in B, where
// rate is B per one A.
//
//	to.minor = round( price.micro × qty.micro × rate.nano × 10^to.decimals ÷ (10¹² × 10⁹) )
func LineExtensionMulRate(price UnitAmount, qty quantity.Quantity, rate Rate, to Currency, mode round.RoundingMode) (Money, error) {
	if price.currency.IsZero() || to.IsZero() {
		return Money{}, ErrNoCurrency
	}
	if rate.nano <= 0 {
		return Money{}, ErrInvalidRate
	}
	num := new(big.Int).Mul(big.NewInt(price.micro), big.NewInt(qty.Micro()))
	num.Mul(num, big.NewInt(rate.nano))
	num.Mul(num, pow10(int(to.decimals)))
	den := new(big.Int).Mul(bigLineScale, bigNano)
	return settle(num, den, to, mode)
}

// LineExtensionDivRate is price × quantity ÷ rate in currency `to`, rounded once: a line priced in B, valued in A, where
// rate is B per one A. The rate is never inverted.
//
//	to.minor = round( price.micro × qty.micro × 10^to.decimals × 10⁹ ÷ (10¹² × rate.nano) )
func LineExtensionDivRate(price UnitAmount, qty quantity.Quantity, rate Rate, to Currency, mode round.RoundingMode) (Money, error) {
	if price.currency.IsZero() || to.IsZero() {
		return Money{}, ErrNoCurrency
	}
	if rate.nano <= 0 {
		return Money{}, ErrInvalidRate
	}
	num := new(big.Int).Mul(big.NewInt(price.micro), big.NewInt(qty.Micro()))
	num.Mul(num, pow10(int(to.decimals)))
	num.Mul(num, bigNano)
	den := new(big.Int).Mul(bigLineScale, big.NewInt(rate.nano))
	return settle(num, den, to, mode)
}

// DivRate converts the amount to currency `to` by DIVIDING by rate, where rate means "1 major unit of `to` = rate major
// units of this currency" — MulRate's other direction, without inverting the rate. Rounded once.
//
//	to.minor = round( minor × 10^to.decimals × 10⁹ ÷ (10^from.decimals × rate.nano) )
func (m Money) DivRate(rate Rate, to Currency, mode round.RoundingMode) (Money, error) {
	if m.currency.IsZero() || to.IsZero() {
		return Money{}, ErrNoCurrency
	}
	if rate.nano <= 0 {
		return Money{}, ErrInvalidRate
	}
	num := new(big.Int).Mul(big.NewInt(m.minor), pow10(int(to.decimals)))
	num.Mul(num, bigNano)
	den := new(big.Int).Mul(pow10(int(m.currency.decimals)), big.NewInt(rate.nano))
	return settle(num, den, to, mode)
}

func settle(num, den *big.Int, to Currency, mode round.RoundingMode) (Money, error) {
	v, ok := int64FromBig(round.Div(num, den, mode))
	if !ok {
		return Money{}, ErrOverflow
	}
	return Money{minor: v, currency: to}, nil
}
