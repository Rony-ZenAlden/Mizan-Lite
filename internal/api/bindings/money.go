package bindings

import (
	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/api/policy"
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
