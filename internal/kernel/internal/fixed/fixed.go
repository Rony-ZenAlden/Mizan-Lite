// Package fixed holds the low-level, overflow-checked fixed-point integer helpers
// shared by the money and quantity kernels.
//
// It exists so there is exactly ONE tested implementation of checked int64
// arithmetic and decimal string handling — two copies could silently diverge, and
// this is the most correctness-critical code in the system. It is kernel-internal
// (import path .../kernel/internal/fixed): only kernel packages may use it.
package fixed

import (
	"math"
	"math/big"
	"strings"
)

// AddI64 returns a+b and whether it fit in int64 (no wraparound).
func AddI64(a, b int64) (int64, bool) {
	if b > 0 && a > math.MaxInt64-b {
		return 0, false
	}
	if b < 0 && a < math.MinInt64-b {
		return 0, false
	}
	return a + b, true
}

// SubI64 returns a-b and whether it fit in int64.
func SubI64(a, b int64) (int64, bool) {
	if b > 0 && a < math.MinInt64+b {
		return 0, false
	}
	if b < 0 && a > math.MaxInt64+b {
		return 0, false
	}
	return a - b, true
}

// MulI64 returns a*b and whether it fit in int64.
func MulI64(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	if (a == math.MinInt64 && b == -1) || (b == math.MinInt64 && a == -1) {
		return 0, false
	}
	p := a * b
	if p/b != a {
		return 0, false
	}
	return p, true
}

// Int64FromBig returns v as int64, or false if it does not fit.
func Int64FromBig(v *big.Int) (int64, bool) {
	if !v.IsInt64() {
		return 0, false
	}
	return v.Int64(), true
}

// Pow10 returns 10^n as a big.Int (n >= 0).
func Pow10(n int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
}

// ParseScaled parses a signed decimal string into an integer at the given scale
// (number of fractional digits): "19.99" at scale 2 → 1999. It returns ok=false for
// malformed input or more fractional digits than the scale allows. The result is a
// big.Int so callers can range-check for their own type.
func ParseScaled(s string, scale int) (*big.Int, bool) {
	str := strings.TrimSpace(s)
	if str == "" {
		return nil, false
	}
	neg := false
	switch str[0] {
	case '+':
		str = str[1:]
	case '-':
		neg = true
		str = str[1:]
	}
	intPart, fracPart := str, ""
	if dot := strings.IndexByte(str, '.'); dot >= 0 {
		intPart, fracPart = str[:dot], str[dot+1:]
	}
	if intPart == "" && fracPart == "" {
		return nil, false
	}
	if !allDigits(intPart) || !allDigits(fracPart) {
		return nil, false
	}
	if len(fracPart) > scale {
		return nil, false
	}
	digits := intPart + fracPart + strings.Repeat("0", scale-len(fracPart))
	if digits == "" {
		digits = "0"
	}
	v, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return nil, false
	}
	if neg {
		v.Neg(v)
	}
	return v, true
}

// FormatScaled renders a scaled integer as a decimal string with `scale` fractional
// digits: FormatScaled(1999, 2) → "19.99"; FormatScaled(-5, 0) → "-5".
func FormatScaled(value int64, scale int) string {
	neg := value < 0
	var mag uint64
	if neg {
		mag = uint64(-(value + 1)) + 1 // safe magnitude even for math.MinInt64
	} else {
		mag = uint64(value)
	}
	ds := uintToString(mag)

	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	if scale == 0 {
		b.WriteString(ds)
		return b.String()
	}
	if len(ds) <= scale {
		ds = strings.Repeat("0", scale-len(ds)+1) + ds
	}
	split := len(ds) - scale
	b.WriteString(ds[:split])
	b.WriteByte('.')
	b.WriteString(ds[split:])
	return b.String()
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func uintToString(u uint64) string {
	if u == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for u > 0 {
		i--
		buf[i] = byte('0' + u%10)
		u /= 10
	}
	return string(buf[i:])
}
