package domain

import (
	"strconv"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
)

// The reorder level: the quantity at or below which a shop wants to be told to buy more (the owner's request,
// 2026-09-20).
//
// # Why it is a level and not a flag
//
// "Low" is not a property of a product, it is a comparison between two quantities, and only the shop knows the second
// one. Ten tins is plenty of tomato paste and nothing at all of bread. So the shop types the level, per product, in the
// product's own unit — and a product whose level has never been typed is never called low, rather than being compared
// against a number nobody chose.
//
// # What is NOT here
//
// An expiry date. A pantry shop holds stock from several deliveries at once; one date on the product would describe
// whichever delivery was typed last and quietly call the older stock fresh. Expiry belongs to a batch, and a batch
// changes how every sale draws stock. The owner chose to ship this and design that properly later (2026-09-20).
const (
	CodeReorderNotPositive = "lite.catalog.reorder_not_positive"
	CodeReorderDecimals    = "lite.catalog.reorder_decimals"
)

// FieldReorder is the field a form marks.
const FieldReorder = "reorderLevel"

// SetReorder records the level at or below which this product is low, typed in the product's own unit.
//
// An empty level clears it — a shop that set one by mistake, or that no longer wants to be told, must be able to take
// it back, and clearing is not the same as setting it to zero.
func (p Product) SetReorder(raw string, ref Reference) (Product, error) {
	if raw != "" && p.OpenPrice {
		// Never counted, so never low: a level would be a warning that can never fire.
		return p, errs.Validation(CodeOpenPriceHasNoPrice, "an open-priced product is never counted, so it has no reorder level").
			WithField(FieldReorder, CodeOpenPriceHasNoPrice, "not counted")
	}
	if raw == "" {
		p.ReorderMicro = 0
		p.HasReorder = false
		return p, nil
	}
	unit, ok := ref.Units[p.UnitCode]
	if !ok {
		return p, errs.Validation(CodeUnknownUnit, "unknown unit").
			WithField(FieldReorder, CodeUnknownUnit, "unknown unit").WithParam("value", p.UnitCode)
	}
	normalised, err := numinput.Normalise(raw)
	if err != nil {
		return p, withField(err, FieldReorder)
	}
	if numinput.Decimals(normalised) > unit.InputDecimals {
		return p, errs.Validation(CodeReorderDecimals, "too many decimals for the unit").
			WithField(FieldReorder, CodeReorderDecimals, "too many decimals").
			WithParam("unit", unit.Code).WithParam("decimals", strconv.Itoa(unit.InputDecimals))
	}
	micro, ok := microsOf(normalised)
	if !ok {
		return p, errs.Validation(numinput.CodeInvalid, "reorder level out of range").
			WithField(FieldReorder, numinput.CodeInvalid, "out of range")
	}
	if micro <= 0 {
		return p, errs.Validation(CodeReorderNotPositive, "a reorder level is more than nothing").
			WithField(FieldReorder, CodeReorderNotPositive, "more than nothing")
	}
	p.ReorderMicro = micro
	p.HasReorder = true
	return p, nil
}

// Low reports whether this much stock is at or below the level the shop set. False when no level was set: a product
// nobody has given a level cannot be low, and guessing one would warn about every product in the shop.
//
// At the level, not below it: a shop that says "tell me at ten" means ten is when to buy, not nine.
func (p Product) Low(onHandMicro int64) bool {
	return p.HasReorder && p.ReorderMicro > 0 && onHandMicro <= p.ReorderMicro
}

// microsOf reads a normalised decimal as 10⁻⁶ of a unit, refusing what will not fit in an int64.
func microsOf(normalised string) (int64, bool) {
	whole, fraction, _ := cutPoint(normalised)
	if len(fraction) > 6 {
		return 0, false
	}
	for len(fraction) < 6 {
		fraction += "0"
	}
	n, err := strconv.ParseInt(whole+fraction, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func cutPoint(s string) (whole, fraction string, hasPoint bool) {
	for i := range s {
		if s[i] == '.' {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}
