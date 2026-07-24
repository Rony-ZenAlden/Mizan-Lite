package money

import (
	"fmt"

	"github.com/mizan-erp/mizan/internal/kernel/round"
)

// MaxDecimals is the largest minor-unit scale a currency may declare. 18 covers
// crypto base units (e.g. wei); mainstream fiat uses 0..3.
const MaxDecimals = 18

// Currency identifies a monetary unit and its settlement conventions.
//
// # Why it exists
//
// Money arithmetic needs to know a currency's minor-unit scale (2 for USD, 0 for
// SYP/JPY, 3 for KWD) and its default rounding. Currency carries just enough of
// that to be self-contained, as an immutable value object.
//
// # When to use
//
// Build one from the authoritative `currencies` table row (done by the currency
// module) and pass it into money constructors. The kernel never reads the database,
// so Currency — not a table lookup — is how scale travels into calculations.
//
// # What it prevents
//
//   - Hardcoding "2 decimal places" (wrong for SYP, KWD, and crypto).
//   - Treating currency codes as a closed enum (they are open data: non-ISO,
//     crypto, and redenominated successors are all representable).
//
// Two currencies are equal iff their codes match; decimals and rounding travel
// along for convenience but are not part of identity.
type Currency struct {
	code     string
	decimals uint8
	rounding round.RoundingMode
}

// NewCurrency constructs a Currency. code must be non-empty; decimals must be
// 0..MaxDecimals.
func NewCurrency(code string, decimals uint8, rounding round.RoundingMode) (Currency, error) {
	if code == "" {
		return Currency{}, fmt.Errorf("%w: empty code", ErrInvalidCurrency)
	}
	if decimals > MaxDecimals {
		return Currency{}, fmt.Errorf("%w: %s has %d decimals (max %d)",
			ErrInvalidCurrency, code, decimals, MaxDecimals)
	}
	if !rounding.Valid() {
		return Currency{}, fmt.Errorf("%w: %s has invalid rounding mode", ErrInvalidCurrency, code)
	}
	return Currency{code: code, decimals: decimals, rounding: rounding}, nil
}

// Code returns the currency code (e.g. "USD", "SYP", "BTC").
func (c Currency) Code() string { return c.code }

// Decimals returns the minor-unit scale.
func (c Currency) Decimals() uint8 { return c.decimals }

// Rounding returns the currency's default rounding mode, used at settlement when a
// caller does not specify one.
func (c Currency) Rounding() round.RoundingMode { return c.rounding }

// IsZero reports whether c is the zero value (no currency).
func (c Currency) IsZero() bool { return c.code == "" }

// Equal reports whether two currencies have the same code.
func (c Currency) Equal(o Currency) bool { return c.code == o.code }

// String returns the currency code, or "<nil>" for the zero value.
func (c Currency) String() string {
	if c.code == "" {
		return "<nil>"
	}
	return c.code
}
