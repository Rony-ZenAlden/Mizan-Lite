package money

import (
	"math/big"

	"github.com/mizan-erp/mizan/internal/kernel/round"
)

// percentScale is the fixed-point scale for Percent: 10^-6 (of the fraction).
// 15% is the fraction 0.15, stored as 150_000.
const percentScale = 1_000_000

var bigPercentScale = big.NewInt(percentScale)

// Percent is an exact rate applied to a Money amount (tax, discount, markup).
//
// # Why it exists
//
// Tax and discount rates must be exact and free of floating point. Percent stores
// the underlying fraction at 10^-6 precision: 15% → 0.15 → 150_000. Applying it is
// Money.MulPercent, which rounds exactly once.
//
// # When to use
//
// Use Percent for any rate applied to money. Do not use it as a general ratio for
// conversion — that is Rate. Keeping the two distinct prevents mixing "a rate you
// multiply money by" with "a rate that converts between currencies".
//
// # What it prevents
//
//   - Float rounding error in tax/discount math.
//   - Ambiguity between a percentage (15) and its fraction (0.15): the constructors
//     name their input explicitly.
type Percent struct {
	micro int64 // the fraction × 10^6
}

// PercentFromMicro builds a Percent from the raw fraction×10^6 (the stored form):
// 150_000 → 15%.
func PercentFromMicro(micro int64) Percent { return Percent{micro: micro} }

// PercentFromBasisPoints builds a Percent from basis points: 1500 bps → 15%.
// (1 bp = 0.01% = 100 micro.)
func PercentFromBasisPoints(bps int64) Percent { return Percent{micro: bps * 100} }

// PercentFromRatio builds a Percent from a ratio num/den (e.g. 15/100 → 15%),
// rounding the fraction to 10^-6 once. den must be non-zero.
func PercentFromRatio(num, den int64, mode round.RoundingMode) (Percent, error) {
	if den == 0 {
		return Percent{}, ErrParse
	}
	// fraction = num/den, stored ×10^6.
	n := new(big.Int).Mul(big.NewInt(num), bigPercentScale)
	d := big.NewInt(den)
	if d.Sign() < 0 { // keep denominator positive for the rounding engine
		n.Neg(n)
		d.Neg(d)
	}
	q := round.Div(n, d, mode)
	iv, ok := int64FromBig(q)
	if !ok {
		return Percent{}, ErrOverflow
	}
	return Percent{micro: iv}, nil
}

// Micro returns the fraction×10^6 — the value persisted to the BIGINT column.
func (p Percent) Micro() int64 { return p.micro }

// IsZero reports whether the percent is zero.
func (p Percent) IsZero() bool { return p.micro == 0 }

// Sign returns -1, 0, or +1.
func (p Percent) Sign() int {
	switch {
	case p.micro > 0:
		return 1
	case p.micro < 0:
		return -1
	default:
		return 0
	}
}

// String returns a representation such as "15%" or "7.5%".
func (p Percent) String() string {
	// micro is the fraction ×10^6; the percentage value is fraction ×100,
	// i.e. micro ÷ 10^4, so it has up to 4 fractional digits as a percentage.
	return trimTrailingZeros(formatScaled(p.micro, 4)) + "%"
}

// trimTrailingZeros removes trailing fractional zeros (and a bare trailing dot),
// so "15.0000" → "15" and "7.5000" → "7.5".
func trimTrailingZeros(s string) string {
	if !hasDot(s) {
		return s
	}
	i := len(s)
	for i > 0 && s[i-1] == '0' {
		i--
	}
	if i > 0 && s[i-1] == '.' {
		i--
	}
	return s[:i]
}

func hasDot(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return true
		}
	}
	return false
}
