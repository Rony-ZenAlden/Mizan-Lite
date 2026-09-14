package domain

import (
	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/quantity"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
)

// Value is what the level is worth at its average cost, in USD minor units (cents).
//
// Through money.LineExtension, the kernel's one scale-aware price × quantity (L2 §3.4): a quantity at 10⁻⁶ times a
// cost at 10⁻⁶ of a dollar is dollars at 10⁻¹², and cents are what is stored. Mizan's costing once skipped that
// conversion and was wrong by a factor of a hundred (H1).
func (l Level) Value() (int64, error) {
	return value(usdCurrency, l.AvgCostMicro, l.OnHandMicro)
}

func value(c money.Currency, unitCostMicro, quantityMicro int64) (int64, error) {
	qty, err := quantity.FromMicro(anyUnit, quantityMicro)
	if err != nil {
		return 0, err
	}
	v, err := money.LineExtension(money.UnitFromMicro(c, unitCostMicro), qty, costRounding)
	if err != nil {
		return 0, err
	}
	return v.Minor(), nil
}

// FormatScaled formats a signed 10⁻⁶-scaled amount with at least `decimals` decimals, and every significant digit
// beyond them — a value is never rounded for display. "12.500" kg, "3" jars, "5.9375" dollars a litre.
func FormatScaled(v int64, decimals int) string { return formatFixed(v, 6, decimals) }

// FormatMinor formats a signed amount of USD minor units: "1140.00".
func FormatMinor(minor int64) string {
	d := int(usdCurrency.Decimals())
	return formatFixed(minor, d, d)
}

func formatFixed(v int64, scale, decimals int) string {
	return numinput.FormatFixed(v, scale, decimals)
}

// FormatRate formats a rate held at 10⁻⁹: "15000", "14250.5".
func FormatRate(nano int64) string { return formatFixed(nano, 9, 0) }
