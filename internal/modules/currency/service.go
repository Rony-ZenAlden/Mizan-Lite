// Package currency is the currency module: currencies, rate types, temporal exchange rates,
// conversion, and redenomination.
//
// It is the first business module, and it lives in Phase 0 because Money, rate types, and
// redenomination are kernel-adjacent — every later module depends on them
// (PHASE_0_FOUNDATION §CUR).
package currency

import (
	"context"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/modules/currency/domain"
	"github.com/mizan-erp/mizan/internal/modules/currency/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/i18n"
)

// EntityType is the `translations` entity type for currency names, so a currency's label can
// be translated as data (§9.5) rather than by adding name_ar/name_en columns.
const EntityType = "currency"

// Service is the currency module's application layer.
//
// It resolves WHICH rate type applies to a business context from settings and hands the pure
// domain converter an explicit answer. The domain may not import platform/config — that is
// the domain-purity rule — so this is where configuration meets business logic.
type Service struct {
	repos     *sqlite.Repos
	converter *domain.Converter
	trans     *i18n.Translations
}

// NewService builds the module's service.
func NewService(db database.DB, clk clock.Clock, trans *i18n.Translations) *Service {
	repos := sqlite.New(db, clk)
	return &Service{
		repos:     repos,
		converter: domain.NewConverter(repos.Currencies, repos.RateTypes, repos.Rates),
		trans:     trans,
	}
}

// ── catalog ─────────────────────────────────────────────────────────────────────

// Get returns the kernel value type for a currency code.
//
// Other modules receive money.Currency and never a currency row, so adding a column to
// `currencies` cannot ripple outward.
func (s *Service) Get(ctx context.Context, code string) (money.Currency, error) {
	info, err := s.repos.Currencies.ByCode(ctx, code)
	if err != nil {
		return money.Currency{}, err
	}
	return info.Currency()
}

// Info returns the full stored record, including presentation fields.
func (s *Service) Info(ctx context.Context, code string) (domain.Info, error) {
	return s.repos.Currencies.ByCode(ctx, code)
}

// List returns active currencies with their names resolved for the context's locale.
//
// The name comes from the `translations` table when one exists, falling back to the base
// column — which is the 0.8 resolver doing exactly what §9.5 designed it for.
func (s *Service) List(ctx context.Context, includeInactive bool) ([]domain.Info, error) {
	infos, err := s.repos.Currencies.List(ctx, includeInactive)
	if err != nil {
		return nil, err
	}
	if s.trans == nil {
		return infos, nil
	}
	for i := range infos {
		infos[i].Name = s.trans.Resolve(ctx, EntityType, infos[i].ID, "name", infos[i].Name)
	}
	return infos, nil
}

// Functional returns the ledger currency (§18.1).
func (s *Service) Functional(ctx context.Context) (money.Currency, error) {
	return s.Get(ctx, Functional.Get(ctx))
}

// PricingCurrency returns the currency catalogue prices are authored in.
func (s *Service) PricingCurrency(ctx context.Context) (money.Currency, error) {
	return s.Get(ctx, Pricing.Get(ctx))
}

// ── conversion ──────────────────────────────────────────────────────────────────

// Convert converts using an explicit rate type.
func (s *Service) Convert(
	ctx context.Context, amt money.Money, to money.Currency, at time.Time, rateType string,
) (domain.Result, error) {
	return s.converter.Convert(ctx, amt, to, at, rateType, Pivot.Get(ctx))
}

// ConvertForContext converts using the rate type bound to a business context.
//
// This is the call sites use: a sales screen asks for "the sales rate" rather than knowing
// whether this company uses the official or the market one.
func (s *Service) ConvertForContext(
	ctx context.Context, amt money.Money, to money.Currency, at time.Time, kind ContextKind,
) (domain.Result, error) {
	handle, ok := RateTypeSetting(kind)
	if !ok {
		return domain.Result{}, errs.Validation(domain.CodeUnknownRateType,
			"unknown conversion context").WithParam("context", string(kind))
	}
	return s.converter.Convert(ctx, amt, to, at, handle.Get(ctx), Pivot.Get(ctx))
}

// ToFunctional converts an amount into the ledger currency, which is what every monetary row
// must store alongside the transaction amount (§18.1).
//
// The returned Result carries the rate used: store it on the document. A report run next year
// must not silently change because a rate was later corrected.
func (s *Service) ToFunctional(
	ctx context.Context, amt money.Money, at time.Time, kind ContextKind,
) (domain.Result, error) {
	functional, err := s.Functional(ctx)
	if err != nil {
		return domain.Result{}, err
	}
	return s.ConvertForContext(ctx, amt, functional, at, kind)
}

// ResolveRedenomination restates an amount in the currency that succeeded its own (§G.2).
func (s *Service) ResolveRedenomination(ctx context.Context, amt money.Money) (money.Money, error) {
	return s.converter.ResolveRedenomination(ctx, amt)
}

// ── rates ───────────────────────────────────────────────────────────────────────

// SetRate records a rate. Rates are append-only: a correction is a new row, and resolution
// returns the most recently recorded one for a given date (Step 0.9, D3).
func (s *Service) SetRate(
	ctx context.Context, from, to string, rateTypeCode string, rate money.Rate,
	validFrom time.Time, source string,
) error {
	rt, err := s.repos.RateTypes.ByCode(ctx, rateTypeCode)
	if err != nil {
		return err
	}
	// Both currencies must exist: a rate against an unknown code would resolve forever
	// without ever being usable.
	if _, err := s.repos.Currencies.ByCode(ctx, from); err != nil {
		return err
	}
	if _, err := s.repos.Currencies.ByCode(ctx, to); err != nil {
		return err
	}
	return s.repos.Rates.Append(ctx, domain.Rate{
		From: from, To: to, RateTypeID: rt.ID, Nano: rate.Nano(),
		ValidFrom: validFrom, Source: source,
	})
}

// RateTypes lists the configured rate types.
func (s *Service) RateTypes(ctx context.Context) ([]domain.RateType, error) {
	return s.repos.RateTypes.List(ctx)
}

// Rates lists the recorded history for a currency pair, newest first.
//
// The history is the point, not a single current figure. A shop in a country whose currency moves
// weekly needs to see WHEN each rate started and what it replaced — and because rates are
// append-only, the list is also the audit trail of every correction.
func (s *Service) Rates(
	ctx context.Context, from, to, rateTypeCode string, limit int,
) ([]domain.Rate, error) {
	rt, err := s.repos.RateTypes.ByCode(ctx, rateTypeCode)
	if err != nil {
		return nil, err
	}
	return s.repos.Rates.History(ctx, from, to, rt.ID, limit)
}

// RateOn resolves the rate the engine would actually use on a date.
//
// Exposed so a screen can show what a document WILL be converted at, rather than showing the
// newest row and leaving the reader to work out whether it applies yet. The two differ whenever
// somebody records tomorrow's rate today, which is the ordinary case for a shop that gets its
// rate in the evening.
func (s *Service) RateOn(
	ctx context.Context, from, to, rateTypeCode string, at time.Time,
) (domain.Rate, bool, error) {
	rt, err := s.repos.RateTypes.ByCode(ctx, rateTypeCode)
	if err != nil {
		return domain.Rate{}, false, err
	}
	return s.repos.Rates.Newest(ctx, from, to, rt.ID, at)
}
