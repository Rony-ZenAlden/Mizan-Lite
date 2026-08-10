// Package domain holds the catalog module's business rules.
//
// Units of measure first (§B): conversion, fractional control, and the rounding that turns a
// scale reading into a quantity. All pure — no database, no clock — because these are the rules
// a warehouse argues about, and they must be testable against a table rather than a fixture.
package domain

import (
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/quantity"
	"github.com/mizan-erp/mizan/internal/kernel/round"
)

// Stable error codes. They double as i18n keys and are part of the public contract.
const (
	CodeInvalidUnit      = "catalog.invalid_unit"
	CodeCrossCategory    = "catalog.cross_category_conversion"
	CodeFractionalUnit   = "catalog.fractional_not_allowed"
	CodeNoReferenceUnit  = "catalog.no_reference_unit"
	CodeNegativeQuantity = "catalog.negative_quantity"
)

// referenceFactor is what a category's reference unit's factor must be: 1, in nano.
const referenceFactor = 1_000_000_000

// UnitCategory groups units that can convert to one another.
//
// Conversion is legal ONLY within a category (§B.2). `kg → g` is arithmetic; `kg → m` is a
// category error, not a silent zero — and a silent zero in a stock system is a quantity that
// vanishes without anybody noticing until a count.
type UnitCategory struct {
	ID       id.ID
	Code     string
	Name     string
	NameKey  string
	IsSystem bool
	IsActive bool
}

// Unit is one unit of measure.
type Unit struct {
	ID         id.ID
	CategoryID id.ID
	Code       string
	Name       string
	NameKey    string
	Symbol     string

	// FactorNano is the unit's size relative to its category's reference, ×10⁹.
	FactorNano  int64
	IsReference bool

	AllowsFractional bool
	// RoundingPrecision is the smallest meaningful step, ×10⁶. 1000 means 0.001.
	RoundingPrecision int64
	DisplayDecimals   int

	IsSystem bool
	IsActive bool
}

// NewUnit builds a unit, or refuses.
func NewUnit(
	identifier, categoryID id.ID, code, name, symbol string, factorNano int64,
) (Unit, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	name = strings.TrimSpace(name)

	if identifier.IsZero() || categoryID.IsZero() {
		return Unit{}, errs.Validation(CodeInvalidUnit, "a unit needs an identity and a category")
	}
	if code == "" {
		return Unit{}, errs.Validation(CodeInvalidUnit, "a unit needs a code").
			WithField("code", CodeInvalidUnit, "required")
	}
	if name == "" || symbol == "" {
		return Unit{}, errs.Validation(CodeInvalidUnit, "a unit needs a name and a symbol")
	}
	if factorNano <= 0 {
		// A zero factor makes every conversion produce zero; a negative one makes quantities
		// change sign. Both are silent, which is why neither is representable.
		return Unit{}, errs.Validation(CodeInvalidUnit,
			"a unit's conversion factor must be positive").WithParam("code", code)
	}

	return Unit{
		ID: identifier, CategoryID: categoryID, Code: code, Name: name, Symbol: symbol,
		FactorNano: factorNano, IsReference: factorNano == referenceFactor,
		AllowsFractional: true, RoundingPrecision: 1000, DisplayDecimals: 3,
		IsActive: true,
	}, nil
}

// Kernel converts the unit into the kernel's own type, which owns the arithmetic.
//
// The persistence layer and the arithmetic layer are deliberately separate types: this one
// carries what a screen shows — a name, a symbol, an active flag — and the kernel's carries
// only what the maths needs. Merging them would put display concerns inside `internal/kernel`,
// which `kernel-purity` forbids and which would make the numeric core depend on a product
// decision.
func (u Unit) Kernel() (quantity.Unit, error) {
	unit, err := quantity.NewUnit(u.Code, string(u.CategoryID), u.FactorNano, u.AllowsFractional)
	if err != nil {
		return quantity.Unit{}, errs.Wrap(err, errs.CategoryValidation, CodeInvalidUnit,
			"the unit is not usable for arithmetic").WithParam("code", u.Code)
	}
	return unit, nil
}

// Convert changes a quantity from one unit to another (§B.2).
//
// # Only within a category
//
// Checked HERE as well as by the kernel, and the duplication is deliberate: the kernel reports
// a mismatch of its own opaque category strings, and this reports the two unit CODES a user
// recognises. "Cannot convert KG to M" is actionable; "category mismatch" is not.
//
// # Rounded once, to the TARGET's precision
//
// Not the source's. Converting 1 kg to grams and back must not lose a milligram to a precision
// that belonged to the other end of the conversion.
func Convert(micro int64, from, to Unit, mode round.RoundingMode) (int64, error) {
	if from.CategoryID != to.CategoryID {
		return 0, errs.Validation(CodeCrossCategory,
			"these units measure different things and cannot be converted").
			WithParam("from", from.Code).WithParam("to", to.Code)
	}
	if from.Code == to.Code {
		return micro, nil
	}

	sourceUnit, err := from.Kernel()
	if err != nil {
		return 0, err
	}
	targetUnit, err := to.Kernel()
	if err != nil {
		return 0, err
	}

	value, err := quantity.FromMicro(sourceUnit, micro)
	if err != nil {
		return 0, errs.Wrap(err, errs.CategoryValidation, CodeInvalidUnit,
			"that quantity cannot be represented").WithParam("unit", from.Code)
	}
	converted, err := value.ConvertTo(targetUnit, mode)
	if err != nil {
		return 0, errs.Wrap(err, errs.CategoryValidation, CodeCrossCategory,
			"the quantity could not be converted").
			WithParam("from", from.Code).WithParam("to", to.Code)
	}
	return converted.Micro(), nil
}

// Normalise applies a unit's rules to an entered quantity (§B.4).
//
// # Rounded at the boundary, once
//
// A scale reading of 1.4372381 kg becomes 1.437 kg HERE, on entry — not repeatedly and
// inconsistently in later calculations, which is how two reports of the same delivery come to
// disagree in the third decimal.
//
// # A fractional chair is refused, not rounded
//
// Rounding 0.5 of a chair to 1 would invent stock; rounding to 0 would lose a sale. Neither is
// a decision code should make on a user's behalf, so it is an error naming the unit.
func Normalise(micro int64, unit Unit) (int64, error) {
	if micro < 0 {
		return 0, errs.Validation(CodeNegativeQuantity,
			"a quantity cannot be negative").WithParam("unit", unit.Code)
	}

	precision := unit.RoundingPrecision
	if precision <= 0 {
		precision = 1
	}

	if !unit.AllowsFractional {
		// "Fractional" for a count unit means "not a whole one of these", which is a whole
		// micro-unit multiple of 10⁶ — not of the rounding precision.
		if micro%1_000_000 != 0 {
			return 0, errs.Validation(CodeFractionalUnit,
				"this unit cannot be divided").WithParam("unit", unit.Code)
		}
		return micro, nil
	}

	// Round to the nearest step. Half away from zero, matching the kernel's default and every
	// other rounding a user sees, so a quantity rounds the way a price does.
	remainder := micro % precision
	if remainder == 0 {
		return micro, nil
	}
	if remainder*2 >= precision {
		return micro - remainder + precision, nil
	}
	return micro - remainder, nil
}

// ValidateCategoryUnits checks the invariant a category must satisfy.
//
// Exactly one reference, whose factor is 1. The database enforces both — a partial unique index
// and a CHECK — and this exists for the moment BEFORE the write, so a seed or an import is
// refused with a message naming the category rather than a constraint violation naming an
// index.
func ValidateCategoryUnits(units []Unit) error {
	references := 0
	for _, unit := range units {
		if !unit.IsReference {
			continue
		}
		references++
		if unit.FactorNano != referenceFactor {
			return errs.Validation(CodeInvalidUnit,
				"the reference unit's factor must be exactly one").WithParam("unit", unit.Code)
		}
	}
	if references != 1 {
		// Zero references makes every conversion in the category impossible; two makes every
		// one ambiguous. Both are configuration errors worth naming precisely.
		return errs.Validation(CodeNoReferenceUnit,
			"a unit category needs exactly one reference unit").
			WithParam("references", itoa(references))
	}
	return nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}
