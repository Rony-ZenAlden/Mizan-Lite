package currency

import (
	"github.com/mizan-erp/mizan/internal/modules/currency/domain"
	"github.com/mizan-erp/mizan/internal/platform/config"
)

// The three currency ROLES of ARCHITECTURE_v1 §18.1, as settings.
//
// "Store prices in USD and calculate local currency" is right for pricing and insufficient
// for accounting. Keeping the roles separate is what makes the ledger legally usable, and
// making them settings is what lets a single-currency company collapse every conversion path
// to identity by setting them all the same.
var (
	// Functional is the ledger currency — what the general ledger and financial statements
	// are kept in, usually mandated by law. Chosen in the setup wizard (Phase 1).
	Functional = config.DeclareString(config.Def{
		Key:         "currency.functional",
		Default:     "SYP",
		Scopes:      []config.Scope{config.ScopeSystem, config.ScopeCompany},
		Description: "settings.currency.functional",
	})

	// Pricing is what catalogue prices are authored in. USD by default per the original
	// brief, but demoted to exactly this one role.
	Pricing = config.DeclareString(config.Def{
		Key:         "currency.pricing",
		Default:     "USD",
		Scopes:      []config.Scope{config.ScopeSystem, config.ScopeCompany},
		Description: "settings.currency.pricing",
	})

	// Pivot is the intermediate currency used when no direct or inverse rate exists between
	// a pair (§18.1's "exchange-rate pivot").
	Pivot = config.DeclareString(config.Def{
		Key:         "currency.pivot",
		Default:     "USD",
		Scopes:      []config.Scope{config.ScopeSystem},
		Description: "settings.currency.pivot",
	})
)

// ContextKind names a business context that resolves its own rate type (§CUR.2).
//
// Six contexts resolved independently from day one. A business under currency controls uses
// the official rate for tax and statutory accounts and the market rate for pricing and
// purchasing — without this they would pick one and manually adjust everywhere else,
// destroying the accuracy the whole subsystem exists to provide.
//
// A company with a single rate sets all six the same and never encounters the concept.
type ContextKind string

const (
	ContextPricing    ContextKind = "pricing"
	ContextPurchasing ContextKind = "purchasing"
	ContextSales      ContextKind = "sales"
	ContextAccounting ContextKind = "accounting"
	ContextReporting  ContextKind = "reporting"
	ContextTax        ContextKind = "tax"
)

// rateTypeSetting declares one context binding.
func rateTypeSetting(kind ContextKind, def string) *config.Setting[string] {
	return config.DeclareEnum(config.Def{
		Key:     "currency.rate_type." + string(kind),
		Default: def,
		Enum: []string{
			domain.RateTypeOfficial, domain.RateTypeMarket,
			domain.RateTypeCustom, domain.RateTypeManual,
		},
		Scopes:      []config.Scope{config.ScopeSystem, config.ScopeCompany},
		Description: "settings.currency.rate_type." + string(kind),
	})
}

// Rate-type bindings. Market for anything commercial, official for anything the state reads —
// which is the distinction §G.1 exists to serve.
var rateTypeBindings = map[ContextKind]*config.Setting[string]{
	ContextPricing:    rateTypeSetting(ContextPricing, domain.RateTypeMarket),
	ContextPurchasing: rateTypeSetting(ContextPurchasing, domain.RateTypeMarket),
	ContextSales:      rateTypeSetting(ContextSales, domain.RateTypeMarket),
	ContextAccounting: rateTypeSetting(ContextAccounting, domain.RateTypeOfficial),
	ContextReporting:  rateTypeSetting(ContextReporting, domain.RateTypeOfficial),
	ContextTax:        rateTypeSetting(ContextTax, domain.RateTypeOfficial),
}

// Contexts lists the declared business contexts, in a stable order.
func Contexts() []ContextKind {
	return []ContextKind{
		ContextPricing, ContextPurchasing, ContextSales,
		ContextAccounting, ContextReporting, ContextTax,
	}
}

// RateTypeSetting returns the typed handle bound to a context.
func RateTypeSetting(kind ContextKind) (*config.Setting[string], bool) {
	h, ok := rateTypeBindings[kind]
	return h, ok
}

// settingDefinitions is what the module reports to the composition root.
func settingDefinitions() []config.Definition {
	keys := []string{Functional.Key(), Pricing.Key(), Pivot.Key()}
	for _, kind := range Contexts() {
		keys = append(keys, "currency.rate_type."+string(kind))
	}
	out := make([]config.Definition, 0, len(keys))
	for _, k := range keys {
		if def, ok := config.Default().Lookup(k); ok {
			out = append(out, def)
		}
	}
	return out
}
