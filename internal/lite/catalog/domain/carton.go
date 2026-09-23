package domain

import (
	"strconv"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
)

// How many of a product make one carton — its طرد (the owner's request, 2026-09-24): 6 dozen coffee cups to a carton,
// 24 thermos flasks to a box. A printed invoice's package column is each line's quantity divided by it, and its footer
// the sum: how many cartons leave the shop.
//
// It is the product's own unit, counted, because that is how a supplier states it and how a shop checks a delivery. A
// product without one has no package count — an invoice leaves the cell empty rather than guess.
const (
	CodeCartonNotPositive = "lite.catalog.carton_not_positive"
	CodeCartonDecimals    = "lite.catalog.carton_decimals"
)

// FieldCarton is the field a form marks.
const FieldCarton = "unitsPerCarton"

// WithCartonSize sets how many of the product's unit make one carton; empty clears it.
func (p Product) WithCartonSize(raw string, ref Reference) (Product, error) {
	if raw == "" {
		p.UnitsPerCartonMicro = 0
		return p, nil
	}
	unit, ok := ref.Units[p.UnitCode]
	if !ok {
		return p, errs.Validation(CodeUnknownUnit, "unknown unit").
			WithField(FieldCarton, CodeUnknownUnit, "unknown unit").WithParam("value", p.UnitCode)
	}
	normalised, err := numinput.Normalise(raw)
	if err != nil {
		return p, withField(err, FieldCarton)
	}
	if numinput.Decimals(normalised) > unit.InputDecimals {
		return p, errs.Validation(CodeCartonDecimals, "too many decimals for the unit").
			WithField(FieldCarton, CodeCartonDecimals, "too many decimals").
			WithParam("unit", unit.Code).WithParam("decimals", strconv.Itoa(unit.InputDecimals))
	}
	micro, ok := microsOf(normalised)
	if !ok {
		return p, errs.Validation(numinput.CodeInvalid, "carton size out of range").
			WithField(FieldCarton, numinput.CodeInvalid, "out of range")
	}
	if micro <= 0 {
		return p, errs.Validation(CodeCartonNotPositive, "a carton holds more than nothing").
			WithField(FieldCarton, CodeCartonNotPositive, "more than nothing")
	}
	p.UnitsPerCartonMicro = micro
	return p, nil
}
