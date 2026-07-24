package quantity

import "errors"

// Sentinel errors returned by this package. Plain stdlib errors keep quantity a
// standalone, dependency-light package; higher layers map them to domain errors.
var (
	// ErrNoUnit is returned for operations on a zero-value (unit-less) Quantity.
	ErrNoUnit = errors.New("quantity: operation on a zero-value quantity (no unit)")

	// ErrUnitMismatch is returned when combining quantities of different units.
	ErrUnitMismatch = errors.New("quantity: unit mismatch")

	// ErrUnitCategory is returned when converting across measurement categories
	// (e.g. weight → length).
	ErrUnitCategory = errors.New("quantity: cannot convert across unit categories")

	// ErrFractionNotAllowed is returned when a fractional amount is used with a unit
	// that only permits whole quantities (e.g. pieces).
	ErrFractionNotAllowed = errors.New("quantity: unit does not allow fractional amounts")

	// ErrInvalidUnit is returned when constructing an invalid Unit.
	ErrInvalidUnit = errors.New("quantity: invalid unit")

	// ErrOverflow is returned when a result exceeds the int64 range.
	ErrOverflow = errors.New("quantity: int64 overflow")

	// ErrParse is returned when a decimal string cannot be parsed.
	ErrParse = errors.New("quantity: invalid quantity")
)
