package domain

import (
	"context"
	"math/big"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/round"
)

// nanoScale is the fixed-point scale of a stored rate: rate_nano = rate × 10⁹.
const nanoScale = 1_000_000_000

// Converter turns money from one currency into another using stored rates.
//
// Pure: it is handed repositories and an explicit rate type and pivot. Resolving WHICH rate
// type applies to a given business context is a settings concern and lives in the app layer,
// because the domain may not import platform/config.
type Converter struct {
	currencies CurrencyRepository
	rateTypes  RateTypeRepository
	rates      RateRepository
}

// NewConverter builds a Converter.
func NewConverter(c CurrencyRepository, rt RateTypeRepository, r RateRepository) *Converter {
	return &Converter{currencies: c, rateTypes: rt, rates: r}
}

// Convert converts amt into `to`, using the rate of `rateTypeCode` valid on `at`.
//
// Resolution order (Step 0.9, D4), each step tried only if the previous found nothing:
//
//  1. identity — same currency; no lookup at all, so a single-currency company never touches
//     any other path and every conversion collapses to a no-op with no special-case code
//  2. direct   — a stored from→to rate
//  3. inverse  — a stored to→from rate, inverted (storing both directions would double the
//     rows and let them disagree)
//  4. pivot    — from→pivot→to, ONE rounding at the end
//  5. stale    — no rate valid on the date: the most recent earlier rate, flagged with its age
//  6. error    — no rate has ever existed for the pair
//
// A rate is never fabricated. Returning 1.0 for an unknown pair would silently value foreign
// money at par, which is a wrong number that looks entirely reasonable on an invoice.
func (c *Converter) Convert(
	ctx context.Context, amt money.Money, to money.Currency, at time.Time, rateTypeCode, pivotCode string,
) (Result, error) {
	if !amt.Valid() {
		return Result{}, errs.Validation(CodeConversionFailed, "amount has no currency")
	}
	if to.IsZero() {
		return Result{}, errs.Validation(CodeConversionFailed, "target currency is not set")
	}

	from := amt.Currency()
	if from.Code() == to.Code() {
		return Result{
			Amount:       amt,
			Rate:         money.RateFromNano(nanoScale),
			RateTypeCode: rateTypeCode,
			Path:         SourceIdentity,
			Source:       SourceIdentity,
			AsOf:         at,
		}, nil
	}

	rateType, err := c.rateTypes.ByCode(ctx, rateTypeCode)
	if err != nil {
		return Result{}, err
	}

	res, err := c.resolve(ctx, from.Code(), to.Code(), rateType, at, pivotCode)
	if err != nil {
		return Result{}, err
	}
	res.RateTypeCode = rateType.Code

	converted, err := amt.MulRate(res.Rate, to, to.Rounding())
	if err != nil {
		return Result{}, errs.Wrap(err, errs.CategoryInternal, CodeConversionFailed,
			"applying the exchange rate")
	}
	res.Amount = converted
	return res, nil
}

// resolve finds the effective rate, filling in everything on Result except Amount.
func (c *Converter) resolve(
	ctx context.Context, from, to string, rateType RateType, at time.Time, pivotCode string,
) (Result, error) {
	// 2. Direct.
	if r, ok, err := c.rates.Newest(ctx, from, to, rateType.ID, at); err != nil {
		return Result{}, err
	} else if ok {
		return withAge(Result{
			Rate: r.Money(), Path: SourceDirect, Source: r.Source, AsOf: r.ValidFrom,
		}, at), nil
	}

	// 3. Inverse.
	if r, ok, err := c.rates.Newest(ctx, to, from, rateType.ID, at); err != nil {
		return Result{}, err
	} else if ok {
		inverted, invErr := r.Money().Invert(round.HalfAwayFromZero)
		if invErr != nil {
			return Result{}, errs.Wrap(invErr, errs.CategoryValidation, CodeInvalidRate,
				"inverting the stored rate").WithParam("from", to).WithParam("to", from)
		}
		return withAge(Result{
			Rate: inverted, Path: SourceInverse, Source: r.Source, AsOf: r.ValidFrom,
		}, at), nil
	}

	// 4. Pivot.
	if pivotCode != "" && pivotCode != from && pivotCode != to {
		if res, ok, err := c.viaPivot(ctx, from, to, rateType, at, pivotCode); err != nil {
			return Result{}, err
		} else if ok {
			return res, nil
		}
	}

	// 5. Stale — offline is the normal case (§18.3), not an error.
	if r, ok, err := c.rates.LastKnown(ctx, from, to, rateType.ID, at); err != nil {
		return Result{}, err
	} else if ok {
		return withAge(Result{
			Rate: r.Money(), Path: SourceDirect, Source: r.Source, AsOf: r.ValidFrom,
		}, at), nil
	}
	if r, ok, err := c.rates.LastKnown(ctx, to, from, rateType.ID, at); err != nil {
		return Result{}, err
	} else if ok {
		inverted, invErr := r.Money().Invert(round.HalfAwayFromZero)
		if invErr != nil {
			return Result{}, errs.Wrap(invErr, errs.CategoryValidation, CodeInvalidRate,
				"inverting the stored rate")
		}
		return withAge(Result{
			Rate: inverted, Path: SourceInverse, Source: r.Source, AsOf: r.ValidFrom,
		}, at), nil
	}

	// 6. No rate has ever existed.
	return Result{}, errs.NotFound(CodeNoRate, "no exchange rate is available for this pair").
		WithParam("from", from).WithParam("to", to).WithParam("rate_type", rateType.Code)
}

// viaPivot converts through an intermediate currency, typically USD (§18.1's "exchange-rate
// pivot").
//
// The two legs are multiplied in exact ×10⁹ fixed point using big.Int and rounded ONCE at the
// end. Rounding each leg separately would compound the error — the single-rounding rule the
// numeric kernel was built around, applied here rather than assumed.
func (c *Converter) viaPivot(
	ctx context.Context, from, to string, rateType RateType, at time.Time, pivot string,
) (Result, bool, error) {
	first, ok, err := c.legRate(ctx, from, pivot, rateType, at)
	if err != nil || !ok {
		return Result{}, false, err
	}
	second, ok, err := c.legRate(ctx, pivot, to, rateType, at)
	if err != nil || !ok {
		return Result{}, false, err
	}

	// (a/1e9) × (b/1e9) × 1e9 = a×b/1e9, computed exactly then rounded once.
	product := new(big.Int).Mul(big.NewInt(first.nano), big.NewInt(second.nano))
	combined := round.Div(product, big.NewInt(nanoScale), round.HalfAwayFromZero)
	if !combined.IsInt64() {
		return Result{}, false, errs.Validation(CodeInvalidRate,
			"the combined pivot rate is out of range").
			WithParam("from", from).WithParam("to", to).WithParam("pivot", pivot)
	}

	asOf := first.asOf
	if second.asOf.Before(asOf) {
		asOf = second.asOf
	}
	return withAge(Result{
		Rate:   money.RateFromNano(combined.Int64()),
		Path:   SourcePivot,
		Source: SourcePivot,
		AsOf:   asOf,
	}, at), true, nil
}

// withAge stamps how old the resolved rate is.
//
// Age is reported on EVERY path, not only when a fallback was needed. A rate with no explicit
// valid_to never formally expires, so the "did we fall back?" signal would have stayed false
// while the POS silently used a three-day-old number — and §G.3 is explicit that "selling at
// a three-day-old rate during rapid movement is a real loss". What a caller needs is the age
// of the rate actually applied; how it was found is a separate question, answered by Path.
func withAge(r Result, at time.Time) Result {
	if r.AsOf.IsZero() || !r.AsOf.Before(at) {
		return r
	}
	r.Age = at.Sub(r.AsOf)
	r.Stale = true
	return r
}

// leg is one half of a pivot conversion.
type leg struct {
	nano  int64
	asOf  time.Time
	stale bool
}

// legRate resolves one leg, trying direct then inverse then last-known.
func (c *Converter) legRate(
	ctx context.Context, from, to string, rateType RateType, at time.Time,
) (leg, bool, error) {
	if from == to {
		return leg{nano: nanoScale, asOf: at}, true, nil
	}
	if r, ok, err := c.rates.Newest(ctx, from, to, rateType.ID, at); err != nil {
		return leg{}, false, err
	} else if ok {
		return leg{nano: r.Nano, asOf: r.ValidFrom}, true, nil
	}
	if r, ok, err := c.rates.Newest(ctx, to, from, rateType.ID, at); err != nil {
		return leg{}, false, err
	} else if ok {
		inverted, invErr := r.Money().Invert(round.HalfAwayFromZero)
		if invErr != nil {
			// A rate that cannot be inverted is unusable for this leg, but the pivot as a
			// whole may still resolve another way, so this reports "no leg" rather than
			// failing the whole conversion.
			return leg{}, false, nil //nolint:nilerr // an uninvertible leg is a miss, not a fault
		}
		return leg{nano: inverted.Nano(), asOf: r.ValidFrom}, true, nil
	}
	if r, ok, err := c.rates.LastKnown(ctx, from, to, rateType.ID, at); err != nil {
		return leg{}, false, err
	} else if ok {
		return leg{nano: r.Nano, asOf: r.ValidFrom, stale: true}, true, nil
	}
	return leg{}, false, nil
}

// ResolveRedenomination restates an amount in the currency that succeeded its own, so a
// report spanning a changeover is consistent (§G.2).
//
// Stored documents are never touched: an invoice for 500,000 old units keeps its original
// amount and code forever. This walks the succession chain for presentation only.
func (c *Converter) ResolveRedenomination(ctx context.Context, amt money.Money) (money.Money, error) {
	if !amt.Valid() {
		return money.Money{}, errs.Validation(CodeConversionFailed, "amount has no currency")
	}

	current := amt
	seen := map[string]bool{amt.Currency().Code(): true}

	for hop := 0; hop < maxRedenominationHops; hop++ {
		info, err := c.currencies.ByCode(ctx, current.Currency().Code())
		if err != nil {
			return money.Money{}, err
		}
		if !info.HasSuccessor() {
			return current, nil
		}
		if seen[info.SucceededBy] {
			// A data-entry mistake making A succeed B and B succeed A would otherwise hang
			// the report that discovered it.
			return money.Money{}, errs.Validation(CodeRedenominationLoop,
				"currency succession forms a loop").
				WithParam("from", info.Code).WithParam("to", info.SucceededBy)
		}
		seen[info.SucceededBy] = true

		successor, err := c.currencies.ByCode(ctx, info.SucceededBy)
		if err != nil {
			return money.Money{}, err
		}
		successorCurrency, err := successor.Currency()
		if err != nil {
			return money.Money{}, err
		}
		converted, err := current.MulRate(
			money.RateFromNano(info.RedenominationFactor), successorCurrency, successorCurrency.Rounding())
		if err != nil {
			return money.Money{}, errs.Wrap(err, errs.CategoryInternal, CodeConversionFailed,
				"applying the redenomination factor").WithParam("currency", info.Code)
		}
		current = converted
	}

	return money.Money{}, errs.Validation(CodeRedenominationLoop,
		"currency succession is longer than the supported chain").
		WithParam("currency", amt.Currency().Code())
}
