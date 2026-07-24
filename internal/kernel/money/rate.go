package money

import (
	"math/big"

	"github.com/mizan-erp/mizan/internal/kernel/round"
)

// rateScale is the fixed-point scale for Rate: 10^-9.
const rateScale = 1_000_000_000

var bigRateScale = big.NewInt(rateScale)

// Rate is an exact ratio stored at 10^-9 precision.
//
// # Why it exists
//
// Exchange rates and other pure multipliers must be exact and free of floating
// point. 10^-9 precision covers fiat FX across the full range Mizan targets — from
// near-parity to hyperinflation (e.g. 1 USD = 15,000+ SYP) — with wide headroom.
//
// # When to use
//
// Use Rate for currency conversion (Money.MulRate) and any pure multiplier. It is
// intentionally currency-agnostic: the same type expresses a unit-of-measure factor.
// The caller pairs a Rate with the correct currencies; Rate itself carries no units.
//
// # What it prevents
//
//   - Floating-point error in conversion, which would desync stored functional
//     amounts from recomputed ones.
//   - Losing the exact rate used on a document (store Nano() alongside the result).
type Rate struct {
	nano int64
}

// RateFromNano builds a Rate from a raw 10^-9-scaled value (the stored form).
func RateFromNano(nano int64) Rate { return Rate{nano: nano} }

// ParseRate parses a decimal string ("15000", "1.234567890") into a Rate.
func ParseRate(s string) (Rate, error) {
	v, err := parseScaled(s, 9)
	if err != nil {
		return Rate{}, err
	}
	iv, ok := int64FromBig(v)
	if !ok {
		return Rate{}, ErrOverflow
	}
	return Rate{nano: iv}, nil
}

// Nano returns the rate in 10^-9 units — the value persisted to the BIGINT column.
func (r Rate) Nano() int64 { return r.nano }

// IsZero reports whether the rate is zero.
func (r Rate) IsZero() bool { return r.nano == 0 }

// Sign returns -1, 0, or +1.
func (r Rate) Sign() int {
	switch {
	case r.nano > 0:
		return 1
	case r.nano < 0:
		return -1
	default:
		return 0
	}
}

// Mul composes two rates (e.g. chaining A→B and B→C into A→C), rounding once.
func (r Rate) Mul(o Rate, mode round.RoundingMode) (Rate, error) {
	num := new(big.Int).Mul(big.NewInt(r.nano), big.NewInt(o.nano))
	q := round.Div(num, bigRateScale, mode)
	iv, ok := int64FromBig(q)
	if !ok {
		return Rate{}, ErrOverflow
	}
	return Rate{nano: iv}, nil
}

// Invert returns 1/r (e.g. to convert B→A given an A→B rate), rounding once.
func (r Rate) Invert(mode round.RoundingMode) (Rate, error) {
	if r.nano <= 0 {
		return Rate{}, ErrInvalidRate
	}
	// 1/r at 10^-9 = round( 10^9 × 10^9 / r.nano ).
	num := new(big.Int).Mul(bigRateScale, bigRateScale)
	q := round.Div(num, big.NewInt(r.nano), mode)
	iv, ok := int64FromBig(q)
	if !ok {
		return Rate{}, ErrOverflow
	}
	return Rate{nano: iv}, nil
}

// String returns a decimal representation such as "15000" or "1.234567890".
func (r Rate) String() string { return formatScaled(r.nano, 9) }
