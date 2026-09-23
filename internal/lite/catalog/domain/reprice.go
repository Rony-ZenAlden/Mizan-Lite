package domain

import (
	"math/big"
	"strconv"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// Re-pricing when the exchange rate moves (the owner's request, 2026-09-23).
//
// # A proposal, never an action
//
// Everything in this file computes. Nothing in it changes a price: the owner sees each proposed price beside the old
// one and confirms, or does nothing and the prices stay exactly as they were. The owner was explicit — optional, never
// mandatory, never applied automatically — and a price the shop did not choose is a price the shop cannot defend at
// the counter.
//
// # Which products
//
// Only products priced in the LOCAL currency. A product priced in dollars already floats with the rate: its pound price
// is worked out at the rate in force at every sale, so it never goes stale. A product priced in pounds keeps the
// number it was given while the pound loses or gains value around it, and that is the product this finds.
//
// # What the proposal is
//
// The price that keeps the product's DOLLAR value where it was when it was priced: old × now ÷ then, rounded to the
// shop's smallest note. A product priced at 15,000 when the rate was 15,000 was priced at one dollar; at 16,500 the
// proposal is 16,500 — still one dollar.

// StaleThresholdPercent is how far the rate must have moved since a price was set before the price is called stale.
// Five percent: less and every ordinary day's wobble would flag the whole shop; more and a real move would go unseen for
// weeks. Both directions count — a dollar that fell leaves pound prices too HIGH, which loses customers instead of
// margin.
const StaleThresholdPercent = 5

// CodeRepriceInvalid refuses a uniform percentage that is not one.
const CodeRepriceInvalid = "lite.catalog.reprice_invalid"

// RateShift is how far the rate has moved since this product's price was set, at 10⁻⁶ of a percentage point, signed.
// known is false where there is nothing to measure: an open-priced product, one priced in dollars, or one priced
// before any rate existed.
func (p Product) RateShift(localCode string, nowNano int64) (percentMicro int64, known bool) {
	if p.OpenPrice || p.PriceCurrency != localCode || p.PricedRateNano <= 0 || nowNano <= 0 {
		return 0, false
	}
	// (now − then) ÷ then × 100, at 10⁻⁶ of a point.
	return mulDivRound(nowNano-p.PricedRateNano, 100_000_000, p.PricedRateNano), true
}

// Stale reports whether the rate has moved by StaleThresholdPercent or more, either way, since the price was set.
func (p Product) Stale(localCode string, nowNano int64) bool {
	shift, known := p.RateShift(localCode, nowNano)
	if !known {
		return false
	}
	if shift < 0 {
		shift = -shift
	}
	return shift >= StaleThresholdPercent*1_000_000
}

// FollowRate is the price that keeps this product's dollar value where it was when it was priced, rounded to the
// nearest step. ok is false where the product has no pricing rate to follow.
func (p Product) FollowRate(localCode string, nowNano, stepMicro int64) (priceMicro int64, ok bool) {
	if _, known := p.RateShift(localCode, nowNano); !known {
		return 0, false
	}
	return roundToStep(mulDivRound(p.PriceMicro, nowNano, p.PricedRateNano), stepMicro), true
}

// ByPercent is the price moved by a percentage the owner typed instead of following the rate, rounded to the nearest
// step. The percentage is signed: a shop can lower prices as well as raise them.
func (p Product) ByPercent(percentMicro, stepMicro int64) int64 {
	return roundToStep(p.PriceMicro+mulDivRound(p.PriceMicro, percentMicro, 100_000_000), stepMicro)
}

// ParseRepricePercent reads a uniform percentage the owner typed: "10", "-5", "12.5". Bounded to ±90%: a typo of 1000
// for 10 would otherwise propose a price eleven times the shelf, and a proposal that absurd is a mistake, not a choice.
func ParseRepricePercent(raw string) (int64, error) {
	normalised, err := normaliseSigned(raw)
	if err != nil {
		return 0, withField(err, FieldRepricePercent)
	}
	micro, ok := fixedParse(normalised)
	if !ok || micro == 0 || micro > 90_000_000 || micro < -90_000_000 {
		return 0, errs.Validation(CodeRepriceInvalid, "a percentage between -90 and 90").
			WithField(FieldRepricePercent, CodeRepriceInvalid, "out of range").WithParam("max", strconv.Itoa(90))
	}
	return micro, nil
}

// FieldRepricePercent is the field a form marks.
const FieldRepricePercent = "percent"

// roundToStep rounds to the nearest multiple of step, half up, and never below one step: a proposal of nothing would
// put a product on the shelf for free.
func roundToStep(micro, step int64) int64 {
	if step <= 1 {
		if micro < 1 {
			return 1
		}
		return micro
	}
	rounded := new(big.Int).Add(big.NewInt(micro), big.NewInt(step/2))
	rounded.Quo(rounded, big.NewInt(step))
	rounded.Mul(rounded, big.NewInt(step))
	if !rounded.IsInt64() {
		return micro
	}
	if out := rounded.Int64(); out >= step {
		return out
	}
	return step
}

// StepMicro is the rounding step for a price in a currency, at 10⁻⁶ of the major unit: the shop's smallest note for the
// local currency, and the currency's smallest unit otherwise.
func StepMicro(currency Currency, localCode string, cashNoteMinor int64) int64 {
	unit := int64(1_000_000)
	for range currency.Decimals {
		unit /= 10
	}
	if currency.Code == localCode && cashNoteMinor > 0 {
		return cashNoteMinor * unit
	}
	return unit
}
