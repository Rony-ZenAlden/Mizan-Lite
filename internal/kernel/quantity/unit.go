package quantity

import "fmt"

// FactorScale is the fixed-point scale of a Unit's conversion factor (10^9). A
// factor of 1.0 (the category reference unit) is expressed as FactorScale; a unit
// worth 1000 reference units uses 1000 × FactorScale.
const FactorScale = 1_000_000_000

// Unit is a unit of measure within a category (weight, volume, length, count, …).
//
// # Why it exists
//
// A quantity is meaningless without its unit, and conversions must be exact and
// confined to compatible units. Unit carries a category and a conversion factor to
// that category's reference unit, so kg↔g is well defined and kg↔litre is rejected.
//
// # When to use
//
// Build one from the authoritative units_of_measure row. The same mechanism serves
// metric and imperial: pound and kilogram are two weight units with different
// factors — "multiple unit systems" needs no new type.
//
// # What it prevents
//
//   - Nonsensical cross-category conversion.
//   - Fractional amounts of indivisible units (e.g. 0.5 pieces) when AllowsFractional
//     is false.
type Unit struct {
	code       string
	category   string
	factorNano int64 // how many reference units equal one of this unit, ×10^9
	fractional bool
}

// NewUnit constructs a Unit. code and category must be non-empty; factorNano must be
// positive. The category's reference unit has factorNano == factorScale (1.0).
func NewUnit(code, category string, factorNano int64, allowsFractional bool) (Unit, error) {
	if code == "" {
		return Unit{}, fmt.Errorf("%w: empty code", ErrInvalidUnit)
	}
	if category == "" {
		return Unit{}, fmt.Errorf("%w: %s has empty category", ErrInvalidUnit, code)
	}
	if factorNano <= 0 {
		return Unit{}, fmt.Errorf("%w: %s has non-positive factor", ErrInvalidUnit, code)
	}
	return Unit{code: code, category: category, factorNano: factorNano, fractional: allowsFractional}, nil
}

// Code returns the unit code (e.g. "KG", "M", "PCS").
func (u Unit) Code() string { return u.code }

// Category returns the measurement category (e.g. "weight").
func (u Unit) Category() string { return u.category }

// AllowsFractional reports whether fractional amounts are permitted.
func (u Unit) AllowsFractional() bool { return u.fractional }

// IsZero reports whether u is the zero value.
func (u Unit) IsZero() bool { return u.code == "" }

// Equal reports whether two units have the same code.
func (u Unit) Equal(o Unit) bool { return u.code == o.code }

// String returns the unit code.
func (u Unit) String() string {
	if u.code == "" {
		return "<nil>"
	}
	return u.code
}
