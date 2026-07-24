package quantity

import (
	"fmt"
	"math/big"

	"github.com/mizan-erp/mizan/internal/kernel/internal/fixed"
	"github.com/mizan-erp/mizan/internal/kernel/round"
)

// Scale is the fixed-point scale for quantities: 10^-6 (six decimal places).
const Scale = 1_000_000

// Quantity is an exact amount of a good, in a specific unit of measure.
//
// # Why it exists
//
// Businesses sell by weight, length, and volume, not just by the piece. Quantities
// must be exact (0.375 kg, 2.5 m) and free of floating point, and they must know
// their unit so conversions stay sane. Quantity stores the amount as an integer at
// 10^-6 precision plus its Unit.
//
// # When to use
//
// Use Quantity for anything counted, weighed, or measured. It meets a price exactly
// once, in money.LineExtension, which is the only place a price is multiplied by a
// quantity.
//
// # What it prevents
//
//   - Float drift in inventory maths.
//   - Adding incompatible units, or converting across categories.
//   - Fractional amounts of indivisible units.
//
// The zero value has no unit and is invalid for arithmetic. Construct via FromMicro
// or Parse. Domain excludes math.MinInt64 (as with money).
type Quantity struct {
	micro int64
	unit  Unit
}

// FromMicro builds a Quantity from a raw 10^-6-scaled amount in unit u. It rejects a
// fractional amount for a unit that does not allow fractions.
func FromMicro(u Unit, micro int64) (Quantity, error) {
	if u.IsZero() {
		return Quantity{}, ErrNoUnit
	}
	if !u.fractional && micro%Scale != 0 {
		return Quantity{}, fmt.Errorf("%w: %s", ErrFractionNotAllowed, u.code)
	}
	return Quantity{micro: micro, unit: u}, nil
}

// Parse converts a decimal string ("2.5") into a Quantity in unit u, allowing up to
// six fractional digits, and enforcing the unit's fractional rule.
func Parse(u Unit, s string) (Quantity, error) {
	if u.IsZero() {
		return Quantity{}, ErrNoUnit
	}
	v, err := parseScaled(s, 6)
	if err != nil {
		return Quantity{}, fmt.Errorf("%w: %q", ErrParse, s)
	}
	iv, ok := int64FromBig(v)
	if !ok {
		return Quantity{}, ErrOverflow
	}
	if !u.fractional && iv%Scale != 0 {
		return Quantity{}, fmt.Errorf("%w: %s", ErrFractionNotAllowed, u.code)
	}
	return Quantity{micro: iv, unit: u}, nil
}

// Micro returns the amount at 10^-6 precision — the value persisted to BIGINT.
func (q Quantity) Micro() int64 { return q.micro }

// Unit returns the quantity's unit.
func (q Quantity) Unit() Unit { return q.unit }

// Valid reports whether q has a unit.
func (q Quantity) Valid() bool { return !q.unit.IsZero() }

// IsZero reports whether the amount is zero.
func (q Quantity) IsZero() bool { return q.micro == 0 }

// IsNegative reports whether the amount is below zero.
func (q Quantity) IsNegative() bool { return q.micro < 0 }

// Sign returns -1, 0, or +1.
func (q Quantity) Sign() int {
	switch {
	case q.micro > 0:
		return 1
	case q.micro < 0:
		return -1
	default:
		return 0
	}
}

// Neg returns the additive inverse in the same unit. (Domain excludes math.MinInt64.)
func (q Quantity) Neg() Quantity { return Quantity{micro: -q.micro, unit: q.unit} }

// Add returns q+o. Errors on unit mismatch or overflow.
func (q Quantity) Add(o Quantity) (Quantity, error) {
	if err := q.requireSame(o); err != nil {
		return Quantity{}, err
	}
	sum, ok := addI64(q.micro, o.micro)
	if !ok {
		return Quantity{}, ErrOverflow
	}
	return Quantity{micro: sum, unit: q.unit}, nil
}

// Sub returns q-o. Errors on unit mismatch or overflow.
func (q Quantity) Sub(o Quantity) (Quantity, error) {
	if err := q.requireSame(o); err != nil {
		return Quantity{}, err
	}
	diff, ok := subI64(q.micro, o.micro)
	if !ok {
		return Quantity{}, ErrOverflow
	}
	return Quantity{micro: diff, unit: q.unit}, nil
}

// Cmp returns -1, 0, or +1 comparing q to o. Errors on unit mismatch.
func (q Quantity) Cmp(o Quantity) (int, error) {
	if err := q.requireSame(o); err != nil {
		return 0, err
	}
	switch {
	case q.micro < o.micro:
		return -1, nil
	case q.micro > o.micro:
		return 1, nil
	default:
		return 0, nil
	}
}

// Equal reports whether q and o have the same unit and amount.
func (q Quantity) Equal(o Quantity) bool {
	return q.unit.Equal(o.unit) && q.micro == o.micro
}

// ConvertTo converts q into target (which must be in the same category), rounding
// once with mode. The conversion goes through the category reference:
//
//	micro_target = round( micro_source × factor_source / factor_target )
func (q Quantity) ConvertTo(target Unit, mode round.RoundingMode) (Quantity, error) {
	if q.unit.IsZero() || target.IsZero() {
		return Quantity{}, ErrNoUnit
	}
	if q.unit.category != target.category {
		return Quantity{}, fmt.Errorf("%w: %s (%s) → %s (%s)",
			ErrUnitCategory, q.unit.code, q.unit.category, target.code, target.category)
	}
	if q.unit.Equal(target) {
		return q, nil
	}
	num := new(big.Int).Mul(big.NewInt(q.micro), big.NewInt(q.unit.factorNano))
	den := big.NewInt(target.factorNano)
	res := round.Div(num, den, mode)
	iv, ok := int64FromBig(res)
	if !ok {
		return Quantity{}, ErrOverflow
	}
	if !target.fractional && iv%Scale != 0 {
		return Quantity{}, fmt.Errorf("%w: converting to %s", ErrFractionNotAllowed, target.code)
	}
	return Quantity{micro: iv, unit: target}, nil
}

// String returns a debug representation such as "2.5 KG".
func (q Quantity) String() string {
	if q.unit.IsZero() {
		return formatScaled(q.micro, 6) + " <no unit>"
	}
	return formatScaled(q.micro, 6) + " " + q.unit.code
}

func (q Quantity) requireSame(o Quantity) error {
	if q.unit.IsZero() || o.unit.IsZero() {
		return ErrNoUnit
	}
	if !q.unit.Equal(o.unit) {
		return fmt.Errorf("%w: %s vs %s", ErrUnitMismatch, q.unit.code, o.unit.code)
	}
	return nil
}

// ── thin adapters over the shared fixed-point helpers ───────────────────────────

func addI64(a, b int64) (int64, bool)       { return fixed.AddI64(a, b) }
func subI64(a, b int64) (int64, bool)       { return fixed.SubI64(a, b) }
func int64FromBig(v *big.Int) (int64, bool) { return fixed.Int64FromBig(v) }
func formatScaled(value int64, scale int) string {
	return fixed.FormatScaled(value, scale)
}

// parseScaled wraps fixed.ParseScaled, mapping a parse failure to ErrParse.
func parseScaled(s string, scale int) (*big.Int, error) {
	v, ok := fixed.ParseScaled(s, scale)
	if !ok {
		return nil, ErrParse
	}
	return v, nil
}
