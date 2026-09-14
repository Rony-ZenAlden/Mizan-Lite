// Package tender is the arithmetic of cash changing hands in two currencies: an amount in one currency, exactly, in
// another at a rate, and a rounding to the smallest note the shop hands over (L4 §3.3, L5 §10.2).
//
// # Why a package of its own
//
// The till gives change on a sale and the debt book gives change on a repayment. Two copies of this arithmetic would
// round a pound differently one day, and a customer would be told two amounts for the same money. Pure: the standard
// library only.
package tender

import "math/big"

// USD is the code of the currency every rate is quoted against: local per ONE US dollar (L3 §4).
const USD = "USD"

// Currency is a currency's code and decimals.
type Currency struct {
	Code     string
	Decimals int
}

// nanoScale is a rate's fixed point: local currency per US dollar at 10⁻⁹.
const nanoScale = 1_000_000_000

// Convert is minor units of one currency, exactly, in minor units of another, at localPerUSDNano.
func Convert(minor int64, from, to Currency, localPerUSDNano int64) *big.Rat {
	v := new(big.Rat).SetFrac(big.NewInt(minor), pow10(from.Decimals)) // major units of `from`
	switch from.Code {
	case to.Code:
	case USD: // dollars → local: × rate
		v.Mul(v, new(big.Rat).SetFrac(big.NewInt(localPerUSDNano), big.NewInt(nanoScale)))
	default: // local → dollars: ÷ rate
		v.Mul(v, new(big.Rat).SetFrac(big.NewInt(nanoScale), big.NewInt(localPerUSDNano)))
	}
	return v.Mul(v, new(big.Rat).SetInt(pow10(to.Decimals)))
}

// Round rounds an amount in minor units to the nearest multiple of increment, half away from zero — to the cash note in
// the local currency, to 1 (the cent) in dollars (Q-L4.1). An increment below 1 is 1.
func Round(v *big.Rat, increment int64) int64 {
	if increment < 1 {
		increment = 1
	}
	units := new(big.Rat).Quo(v, big.NewRat(increment, 1))
	num, den := units.Num(), units.Denom()
	neg := num.Sign() < 0
	abs := new(big.Int).Abs(num)
	half := new(big.Int).Quo(new(big.Int).Add(new(big.Int).Mul(abs, big.NewInt(2)), den), new(big.Int).Mul(den, big.NewInt(2)))
	if neg {
		half.Neg(half)
	}
	return half.Int64() * increment
}

// Increment is what an amount in currency c rounds to when cash changes hands: the note in the local currency, the
// minor unit in dollars.
func Increment(c Currency, cashNote int64) int64 {
	if c.Code == USD {
		return 1
	}
	return cashNote
}

// WithinHalf reports whether a recorded figure is within half an increment of the exact one — the bound every rounding
// here keeps, which the verifiers hold.
func WithinHalf(recorded int64, exact *big.Rat, increment int64) bool {
	diff := new(big.Rat).Sub(new(big.Rat).SetInt64(recorded), exact)
	return new(big.Rat).Abs(diff).Cmp(big.NewRat(max(increment, 1), 2)) <= 0
}

func pow10(n int) *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil) }
