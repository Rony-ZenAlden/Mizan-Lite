package round

import "math/big"

var bigOne = big.NewInt(1)

// Div divides num by den and rounds the exact quotient num/den to an integer
// according to mode.
//
// Preconditions: den > 0. num may be any sign. The inputs are not modified.
//
// The computation is exact: it uses the integer quotient and remainder, then
// consults only the remainder's relationship to half of den to resolve the
// rounding. There is no floating-point anywhere, and it rounds exactly once.
func Div(num, den *big.Int, mode RoundingMode) *big.Int {
	q := new(big.Int)
	r := new(big.Int)
	// Truncated division: q = trunc(num/den) toward zero, sign(r) == sign(num).
	q.QuoRem(num, den, r)
	if r.Sign() == 0 {
		return q // exact, no rounding needed
	}

	neg := num.Sign() < 0

	// Compare 2*|r| against den: <0 below half, ==0 exactly half, >0 above half.
	twice := new(big.Int).Abs(r)
	twice.Lsh(twice, 1)
	cmp := twice.Cmp(den)

	// away moves q one step further from zero (the sign-aware "round up in magnitude").
	away := func() *big.Int {
		if neg {
			return q.Sub(q, bigOne)
		}
		return q.Add(q, bigOne)
	}

	switch mode {
	case TowardZero:
		return q
	case Floor:
		if neg {
			return q.Sub(q, bigOne)
		}
		return q
	case Ceiling:
		if neg {
			return q
		}
		return q.Add(q, bigOne)
	case HalfUp: // ties toward +infinity
		if cmp > 0 || (cmp == 0 && !neg) {
			return away()
		}
		return q
	case HalfDown: // ties toward -infinity
		if cmp > 0 || (cmp == 0 && neg) {
			return away()
		}
		return q
	case HalfToEven:
		if cmp > 0 {
			return away()
		}
		if cmp < 0 {
			return q
		}
		if q.Bit(0) == 0 { // q is even
			return q
		}
		return away()
	case HalfAwayFromZero:
		fallthrough
	default:
		if cmp >= 0 {
			return away()
		}
		return q
	}
}
