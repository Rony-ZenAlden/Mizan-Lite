package bindings

import (
	"strconv"
	"strings"
	"time"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/api/policy"
	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/modules/accounting"
	currencydomain "github.com/mizan-erp/mizan/internal/modules/currency/domain"
	"github.com/mizan-erp/mizan/internal/modules/identity"
)

// CurrencyDTO is a currency as the frontend sees it.
//
// Domain types are never exposed to JavaScript (§5.4): this flat shape decouples the UI from
// domain refactoring and keeps internal fields — the redenomination chain, the rounding mode —
// off the wire, where they would become an accidental contract.
type CurrencyDTO struct {
	Code           string `json:"code"`
	Name           string `json:"name"`
	Symbol         string `json:"symbol"`
	DecimalPlaces  int    `json:"decimalPlaces"`
	SymbolPosition string `json:"symbolPosition"`
}

// moneyPolicies declares what Money's methods require.
func moneyPolicies() map[string]policy.Policy {
	return map[string]policy.Policy{
		"Currencies": policy.Requires(identity.PermUserView),
		// Reading a rate is reading the books' configuration; setting one CHANGES what every
		// future document converts at, which is why the two are not the same permission.
		"Rates":     policy.Requires(accounting.PermAccountView),
		"RateTypes": policy.Requires(accounting.PermAccountView),
		"SetRate":   policy.Requires(accounting.PermAccountManage),
	}
}

// Money exposes the currency catalogue.
type Money struct{ graph }

// Currencies lists the configured currencies, named in the caller's language.
//
// The end-to-end proof that the graph is genuinely wired: one call through the per-request
// context, the settings-bound locale, the currency module, and the i18n translation resolver.
func (m *Money) Currencies() envelope.Result[[]CurrencyDTO] {
	ctx, app, err := m.guard("Currencies")
	if err != nil {
		return envelope.Fail[[]CurrencyDTO](err)
	}

	infos, err := app.Currency.List(ctx, false)
	if err != nil {
		return envelope.Fail[[]CurrencyDTO](err)
	}

	// Built with make, never left nil: a nil slice and an empty one differ on the wire, and
	// the frontend must always receive an array it can map over.
	out := make([]CurrencyDTO, 0, len(infos))
	for _, info := range infos {
		out = append(out, CurrencyDTO{
			Code:           info.Code,
			Name:           info.Name,
			Symbol:         info.Symbol,
			DecimalPlaces:  int(info.DecimalPlaces),
			SymbolPosition: info.SymbolPosition,
		})
	}
	return envelope.Ok(out)
}

// ── exchange rates ──────────────────────────────────────────────────────────────

// RateDTO is one recorded exchange rate.
//
// # Why the rate crosses as a STRING
//
// A rate is scaled by 10⁹, and a Syrian pound to dollar rate is in the thousands — so the product
// of a rate and an amount leaves the range JavaScript numbers represent exactly long before
// anything looks wrong. Everything numeric in Mizan crosses this boundary as a string for the
// same reason (§5.4), and a rate is the field where a float would do the most damage: it
// multiplies every figure on the document.
type RateDTO struct {
	From string `json:"from"`
	To   string `json:"to"`
	// RateType is which kind of rate this is — market for trade, official for the state.
	RateType string `json:"rateType"`
	// RateNano is the rate ×10⁹, as a decimal string.
	RateNano string `json:"rateNano"`
	// Rate is the same number formatted for display, e.g. "15000.000000000".
	Rate string `json:"rate"`
	// ValidFrom is the ISO date the rate takes effect.
	ValidFrom string `json:"validFrom"`
	// Source says where it came from — a person, an import, a seed.
	Source string `json:"source"`
	// RecordedAt is when the row was written, which is what distinguishes a correction from
	// the rate it corrects: both carry the same validFrom.
	RecordedAt string `json:"recordedAt"`
	// InForce marks the row the engine would actually use today. Exactly one row per date is
	// in force, and it is not always the one a reader would guess.
	InForce bool `json:"inForce"`
}

// RateTypeDTO is a kind of rate — daily, customs, budget.
type RateTypeDTO struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// NewRateInput records one rate.
type NewRateInput struct {
	From string `json:"from"`
	To   string `json:"to"`
	// Rate is a decimal string: "15000", "15000.5". Parsed at 10⁹ scale.
	Rate string `json:"rate"`
	// RateTypeCode is which kind of rate. Blank means the default type.
	RateTypeCode string `json:"rateTypeCode"`
	// ValidFrom is the ISO date it takes effect. Blank means today.
	ValidFrom string `json:"validFrom"`
	Source    string `json:"source"`
}

// RateTypes lists the kinds of rate this installation keeps.
func (m *Money) RateTypes() envelope.Result[[]RateTypeDTO] {
	ctx, app, err := m.guard("RateTypes")
	if err != nil {
		return envelope.Fail[[]RateTypeDTO](err)
	}
	types, err := app.Currency.RateTypes(ctx)
	if err != nil {
		return envelope.Fail[[]RateTypeDTO](err)
	}
	out := make([]RateTypeDTO, 0, len(types))
	for _, rt := range types {
		if !rt.IsActive {
			continue
		}
		out = append(out, RateTypeDTO{Code: rt.Code, Name: rt.Name})
	}
	return envelope.Ok(out)
}

// Rates lists a pair's recorded history, newest first.
func (m *Money) Rates(from, to, rateTypeCode string, limit int) envelope.Result[[]RateDTO] {
	ctx, app, err := m.guard("Rates")
	if err != nil {
		return envelope.Fail[[]RateDTO](err)
	}
	if strings.TrimSpace(rateTypeCode) == "" {
		rateTypeCode = defaultRateType
	}

	history, err := app.Currency.Rates(ctx, strings.ToUpper(from), strings.ToUpper(to),
		rateTypeCode, limit)
	if err != nil {
		return envelope.Fail[[]RateDTO](err)
	}

	// Which row the engine would use TODAY, asked of the engine rather than derived here. A
	// screen that re-implemented the resolution order would eventually disagree with the till,
	// and the disagreement would be invisible until somebody compared a receipt with a report.
	today, _ := clock.ParseDate(app.Today())
	inForce, found, err := app.Currency.RateOn(ctx, strings.ToUpper(from), strings.ToUpper(to),
		rateTypeCode, today)
	if err != nil {
		// Not fatal: a pair with no rate yet is the ordinary state on a fresh install, and the
		// history is still worth showing.
		found = false
	}

	out := make([]RateDTO, 0, len(history))
	for _, rate := range history {
		out = append(out, RateDTO{
			From: rate.From, To: rate.To, RateType: rateTypeCode,
			RateNano:   strconv.FormatInt(rate.Nano, 10),
			Rate:       money.RateFromNano(rate.Nano).String(),
			ValidFrom:  rate.ValidFrom.Format(time.DateOnly),
			Source:     rate.Source,
			RecordedAt: rate.CreatedAt.UTC().Format(time.RFC3339),
			InForce:    found && rate.ID == inForce.ID,
		})
	}
	return envelope.Ok(out)
}

// SetRate records a rate.
//
// Append-only: this never updates a row. Correcting yesterday's rate means recording it again
// with the same date, and the newer row wins — so the mistake stays visible, which is what makes
// "why did this invoice convert at that rate" answerable.
func (m *Money) SetRate(in NewRateInput) envelope.Result[[]RateDTO] {
	ctx, app, err := m.guard("SetRate")
	if err != nil {
		return envelope.Fail[[]RateDTO](err)
	}

	rate, err := money.ParseRate(strings.TrimSpace(in.Rate))
	if err != nil {
		return envelope.Fail[[]RateDTO](errs.Validation(currencydomain.CodeInvalidRate,
			"that is not a rate").WithField("rate", currencydomain.CodeInvalidRate, "invalid"))
	}
	if rate.Nano() <= 0 {
		// A zero rate converts every amount to nothing, and a negative one flips the sign of
		// every figure on a document. Neither is a rate; both are refused here rather than
		// producing a document nobody can explain.
		return envelope.Fail[[]RateDTO](errs.Validation(currencydomain.CodeInvalidRate,
			"a rate must be greater than zero").
			WithField("rate", currencydomain.CodeInvalidRate, "invalid"))
	}

	// Today, from the INJECTED clock rather than the wall clock: a rate dated by the machine's
	// idea of now is a rate that lands in yesterday's period when the two disagree.
	validFrom, _ := clock.ParseDate(app.Today())
	if trimmed := strings.TrimSpace(in.ValidFrom); trimmed != "" {
		parsed, ok := clock.ParseDate(trimmed)
		if !ok {
			return envelope.Fail[[]RateDTO](errs.Validation(currencydomain.CodeInvalidRate,
				"that is not a date").
				WithField("validFrom", currencydomain.CodeInvalidRate, "invalid"))
		}
		validFrom = parsed
	}

	rateType := strings.TrimSpace(in.RateTypeCode)
	if rateType == "" {
		rateType = defaultRateType
	}
	source := strings.TrimSpace(in.Source)
	if source == "" {
		// Who recorded it, not what they typed. "manual" is the honest answer when a person
		// keyed it in, and it is what distinguishes it from a seeded or imported rate later.
		source = "manual"
	}

	if err = app.Currency.SetRate(ctx, strings.ToUpper(in.From), strings.ToUpper(in.To),
		rateType, rate, validFrom, source); err != nil {
		return envelope.Fail[[]RateDTO](err)
	}

	// The whole history comes back, so a screen redraws from one answer rather than stitching a
	// row onto state it is holding — the same choice the till makes after adding a line.
	return m.Rates(in.From, in.To, rateType, 0)
}

// defaultRateType is the rate this screen works in when nobody says otherwise.
//
// MARKET, not official. §G.1 binds pricing, sales and purchasing to the market rate and only the
// state-facing contexts — accounting, reporting, tax — to the official one. A shop recording
// "today's rate" means the rate it trades at, and in a market with a parallel rate those are
// different numbers by a wide margin. Defaulting to `official` would quietly reprice a shop's
// whole day at a rate nobody can actually buy dollars at.
const defaultRateType = currencydomain.RateTypeMarket
