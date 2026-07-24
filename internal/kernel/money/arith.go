package money

import (
	"math/big"

	"github.com/mizan-erp/mizan/internal/kernel/internal/fixed"
)

// This file adapts the shared fixed-point helpers (kernel/internal/fixed) to the
// money package's local names, so the value-type files read cleanly. There is no
// arithmetic logic here — it all lives in the single tested `fixed` package.

func addI64(a, b int64) (int64, bool)       { return fixed.AddI64(a, b) }
func subI64(a, b int64) (int64, bool)       { return fixed.SubI64(a, b) }
func mulI64(a, b int64) (int64, bool)       { return fixed.MulI64(a, b) }
func pow10(n int) *big.Int                  { return fixed.Pow10(n) }
func int64FromBig(v *big.Int) (int64, bool) { return fixed.Int64FromBig(v) }

// parseScaled wraps fixed.ParseScaled, mapping a parse failure to ErrParse.
func parseScaled(s string, scale int) (*big.Int, error) {
	v, ok := fixed.ParseScaled(s, scale)
	if !ok {
		return nil, ErrParse
	}
	return v, nil
}

func formatScaled(value int64, scale int) string { return fixed.FormatScaled(value, scale) }
