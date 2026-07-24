package money

import (
	"fmt"
	"math/big"
	"sort"

	"github.com/mizan-erp/mizan/internal/kernel/round"
)

// Money is an exact monetary amount in a specific currency.
//
// # Why it exists
//
// Every settled financial figure — invoice totals, ledger postings, balances,
// payments — is a Money. It stores the amount as an integer number of the
// currency's minor units (cents, fils, or the whole unit for 0-decimal currencies),
// so arithmetic is exact. Money is NEVER a floating-point number.
//
// # When to use
//
// Use Money for any amount that will be paid, posted, or reported. For a per-unit
// price or cost that needs sub-minor precision, use UnitAmount instead; the two
// meet exactly once, in LineExtension.
//
// # What it prevents
//
//   - Floating-point drift (0.1 + 0.2 != 0.3) accumulating across the ledger.
//   - Silent currency mixing: combining different currencies returns an error.
//   - Silent overflow: results that exceed int64 return ErrOverflow, never wrap.
//
// # Representation is private and may change
//
// The int64 field is unexported on purpose. No public method exposes the raw
// storage word (Minor returns the well-defined minor-unit value, which is a stable
// contract). This lets the representation widen to 128-bit or big.Int in the future
// — for 18-decimal crypto assets — without changing this package's public API.
//
// The zero value (Money{}) has no currency and is invalid for arithmetic; every
// operation returns ErrNoCurrency for it. Construct via FromMinor, Zero, or Parse.
//
// Supported minor-unit domain: math.MinInt64 is reserved and must not be passed to
// FromMinor; it cannot arise from Parse or from checked arithmetic.
type Money struct {
	minor    int64
	currency Currency
}

// FromMinor builds a Money from a raw minor-unit amount (e.g. cents). This is the
// constructor the repository layer uses when reading the BIGINT column back.
func FromMinor(c Currency, minor int64) Money {
	return Money{minor: minor, currency: c}
}

// Zero returns the additive identity in currency c.
func Zero(c Currency) Money { return Money{minor: 0, currency: c} }

// Parse converts a decimal string ("19.99") into Money for currency c, rejecting
// more fractional digits than the currency allows.
func Parse(c Currency, s string) (Money, error) {
	if c.IsZero() {
		return Money{}, ErrNoCurrency
	}
	v, err := parseScaled(s, int(c.decimals))
	if err != nil {
		return Money{}, fmt.Errorf("%w: %q for %s", ErrParse, s, c.code)
	}
	iv, ok := int64FromBig(v)
	if !ok {
		return Money{}, ErrOverflow
	}
	return Money{minor: iv, currency: c}, nil
}

// Currency returns the amount's currency.
func (m Money) Currency() Currency { return m.currency }

// Minor returns the amount in minor units. This is the value persisted to the
// BIGINT column; it is part of the stable contract, not the internal storage width.
func (m Money) Minor() int64 { return m.minor }

// Valid reports whether m has a currency and can take part in arithmetic.
func (m Money) Valid() bool { return !m.currency.IsZero() }

// IsZero reports whether the amount is zero (regardless of currency).
func (m Money) IsZero() bool { return m.minor == 0 }

// IsNegative reports whether the amount is below zero.
func (m Money) IsNegative() bool { return m.minor < 0 }

// Sign returns -1, 0, or +1.
func (m Money) Sign() int {
	switch {
	case m.minor > 0:
		return 1
	case m.minor < 0:
		return -1
	default:
		return 0
	}
}

// Neg returns the additive inverse. (Domain excludes math.MinInt64.)
func (m Money) Neg() Money { return Money{minor: -m.minor, currency: m.currency} }

// Abs returns the absolute amount. (Domain excludes math.MinInt64.)
func (m Money) Abs() Money {
	if m.minor < 0 {
		return Money{minor: -m.minor, currency: m.currency}
	}
	return m
}

// Add returns m+n. Errors on currency mismatch or overflow.
func (m Money) Add(n Money) (Money, error) {
	if err := m.requireSame(n); err != nil {
		return Money{}, err
	}
	sum, ok := addI64(m.minor, n.minor)
	if !ok {
		return Money{}, ErrOverflow
	}
	return Money{minor: sum, currency: m.currency}, nil
}

// Sub returns m-n. Errors on currency mismatch or overflow.
func (m Money) Sub(n Money) (Money, error) {
	if err := m.requireSame(n); err != nil {
		return Money{}, err
	}
	// m - n, checked: subtracting n is adding -n, but guard n == MinInt64.
	diff, ok := subI64(m.minor, n.minor)
	if !ok {
		return Money{}, ErrOverflow
	}
	return Money{minor: diff, currency: m.currency}, nil
}

// MulInt scales the amount by a whole number (e.g. a piece count). Errors on overflow.
func (m Money) MulInt(k int64) (Money, error) {
	if err := m.requireValid(); err != nil {
		return Money{}, err
	}
	p, ok := mulI64(m.minor, k)
	if !ok {
		return Money{}, ErrOverflow
	}
	return Money{minor: p, currency: m.currency}, nil
}

// MulPercent applies a percentage, rounding the result once with mode.
// Example: USD 100.00 × 15% → USD 15.00.
func (m Money) MulPercent(p Percent, mode round.RoundingMode) (Money, error) {
	if err := m.requireValid(); err != nil {
		return Money{}, err
	}
	num := new(big.Int).Mul(big.NewInt(m.minor), big.NewInt(p.micro))
	q := round.Div(num, bigPercentScale, mode)
	v, ok := int64FromBig(q)
	if !ok {
		return Money{}, ErrOverflow
	}
	return Money{minor: v, currency: m.currency}, nil
}

// MulRate converts the amount to currency `to` using `rate`, where rate means
// "1 major unit of this currency = rate major units of `to`". The result is rounded
// once with mode. The caller is responsible for supplying the correct rate for the
// pair (Rate is a pure ratio; the currency module resolves the right one).
//
//	minorB = round( minorA × rateNano × 10^decimalsB / (10^9 × 10^decimalsA) )
func (m Money) MulRate(rate Rate, to Currency, mode round.RoundingMode) (Money, error) {
	if m.currency.IsZero() || to.IsZero() {
		return Money{}, ErrNoCurrency
	}
	if rate.nano <= 0 {
		return Money{}, ErrInvalidRate
	}
	num := new(big.Int).Mul(big.NewInt(m.minor), big.NewInt(rate.nano))
	num.Mul(num, pow10(int(to.decimals)))
	den := new(big.Int).Mul(bigRateScale, pow10(int(m.currency.decimals)))
	q := round.Div(num, den, mode)
	v, ok := int64FromBig(q)
	if !ok {
		return Money{}, ErrOverflow
	}
	return Money{minor: v, currency: to}, nil
}

// Allocate splits m into len(weights) parts proportional to the weights, whose sum
// is EXACTLY m — no minor unit is lost or invented. It uses the largest-remainder
// method: floor each ideal share, then hand the leftover units, one at a time, to
// the parts with the largest fractional remainder (ties broken by lowest index).
//
// Example: USD 100.00 split [1,1,1] → [33.34, 33.33, 33.33].
//
// Weights must be non-negative and sum to a positive total.
func (m Money) Allocate(weights []int64) ([]Money, error) {
	if m.currency.IsZero() {
		return nil, ErrNoCurrency
	}
	if len(weights) == 0 {
		return nil, ErrInvalidWeights
	}
	var total int64
	for _, w := range weights {
		if w < 0 {
			return nil, ErrInvalidWeights
		}
		t, ok := addI64(total, w)
		if !ok {
			return nil, ErrOverflow
		}
		total = t
	}
	if total <= 0 {
		return nil, ErrInvalidWeights
	}

	bigMinor := big.NewInt(m.minor)
	bigTotal := big.NewInt(total)

	parts := make([]int64, len(weights))
	remainders := make([]*big.Int, len(weights))
	var allocated int64
	for i, w := range weights {
		num := new(big.Int).Mul(bigMinor, big.NewInt(w))
		q := new(big.Int)
		r := new(big.Int)
		q.QuoRem(num, bigTotal, r) // truncated toward zero
		qi := q.Int64()            // |qi| <= |m.minor|, fits int64
		parts[i] = qi
		allocated += qi
		remainders[i] = new(big.Int).Abs(r)
	}

	// leftover has the same sign as m.minor and |leftover| < len(weights).
	leftover := m.minor - allocated
	step := int64(1)
	n := leftover
	if n < 0 {
		step = -1
		n = -n
	}

	order := make([]int, len(weights))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return remainders[order[a]].Cmp(remainders[order[b]]) > 0
	})
	for k := int64(0); k < n; k++ {
		parts[order[k]] += step
	}

	out := make([]Money, len(weights))
	for i, p := range parts {
		out[i] = Money{minor: p, currency: m.currency}
	}
	return out, nil
}

// Cmp returns -1, 0, or +1 comparing m to n. Errors on currency mismatch.
func (m Money) Cmp(n Money) (int, error) {
	if err := m.requireSame(n); err != nil {
		return 0, err
	}
	switch {
	case m.minor < n.minor:
		return -1, nil
	case m.minor > n.minor:
		return 1, nil
	default:
		return 0, nil
	}
}

// Equal reports whether m and n have the same currency and amount.
func (m Money) Equal(n Money) bool {
	return m.currency.Equal(n.currency) && m.minor == n.minor
}

// String returns a debug representation such as "USD 19.99".
func (m Money) String() string {
	if m.currency.IsZero() {
		return "<no currency> " + formatScaled(m.minor, 0)
	}
	return m.currency.code + " " + formatScaled(m.minor, int(m.currency.decimals))
}

func (m Money) requireValid() error {
	if m.currency.IsZero() {
		return ErrNoCurrency
	}
	return nil
}

func (m Money) requireSame(n Money) error {
	if m.currency.IsZero() || n.currency.IsZero() {
		return ErrNoCurrency
	}
	if !m.currency.Equal(n.currency) {
		return fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, m.currency.code, n.currency.code)
	}
	return nil
}
