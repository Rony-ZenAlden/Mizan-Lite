package currency

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/locale"
	"github.com/mizan-erp/mizan/internal/kernel/round"
	"github.com/mizan-erp/mizan/internal/modules/currency/domain"
	"github.com/mizan-erp/mizan/internal/platform/i18n"
	"github.com/mizan-erp/mizan/internal/platform/metadata"
)

// Seeds go through the Step 0.5 metadata seeder: matched by `code`, `is_system` rows
// protected from deletion, admin edits to non-system rows never reverted, and re-running
// changes nothing. This is that machinery's first real use.
//
// Rates themselves are NEVER seeded. They are the customer's data, and a rate shipped in a
// binary would be wrong the day it shipped.

// currencySeeds is the starter set. Syria is the development seed (§C.3), USD is the pricing
// reference and rate pivot, EUR and TRY are regionally relevant.
//
// SYP has ZERO decimal places, which is the case that matters: a numeric kernel that assumed
// two would be wrong for the primary target market, and the money type has been integer-only
// with a configurable scale since Step 0.2 for exactly this reason.
func currencySeeds() metadata.SeedSpec {
	active := true
	return metadata.SeedSpec{
		Table:        "currencies",
		ExtraColumns: []string{"symbol", "decimal_places", "symbol_position", "rounding_mode"},
		Rows: []metadata.Row{
			{
				Code: "SYP", Name: "Syrian Pound", IsSystem: true, IsActive: &active,
				Columns: map[string]any{
					"symbol": "ل.س", "decimal_places": 0, "symbol_position": "after",
					"rounding_mode": round.HalfAwayFromZero.String(),
				},
			},
			{
				Code: "USD", Name: "US Dollar", IsSystem: true, IsActive: &active,
				Columns: map[string]any{
					"symbol": "$", "decimal_places": 2, "symbol_position": "before",
					"rounding_mode": round.HalfAwayFromZero.String(),
				},
			},
			{
				Code: "EUR", Name: "Euro", IsSystem: true, IsActive: &active,
				Columns: map[string]any{
					"symbol": "€", "decimal_places": 2, "symbol_position": "before",
					"rounding_mode": round.HalfAwayFromZero.String(),
				},
			},
			{
				Code: "TRY", Name: "Turkish Lira", IsSystem: true, IsActive: &active,
				Columns: map[string]any{
					"symbol": "₺", "decimal_places": 2, "symbol_position": "before",
					"rounding_mode": round.HalfAwayFromZero.String(),
				},
			},
			// Added in Step 1.9, because Step 1.8's country profiles name them and companies
			// carry a foreign key to currencies(code): choosing Saudi Arabia in the wizard
			// would otherwise fail at provisioning with a constraint error. A test now asserts
			// that every shipped country profile's currencies are seeded, so the next profile
			// cannot reintroduce the gap.
			{
				Code: "SAR", Name: "Saudi Riyal", IsSystem: true, IsActive: &active,
				Columns: map[string]any{
					"symbol": "ر.س", "decimal_places": 2, "symbol_position": "after",
					"rounding_mode": round.HalfAwayFromZero.String(),
				},
			},
			{
				Code: "AED", Name: "UAE Dirham", IsSystem: true, IsActive: &active,
				Columns: map[string]any{
					"symbol": "د.إ", "decimal_places": 2, "symbol_position": "after",
					"rounding_mode": round.HalfAwayFromZero.String(),
				},
			},
			{
				Code: "EGP", Name: "Egyptian Pound", IsSystem: true, IsActive: &active,
				Columns: map[string]any{
					"symbol": "ج.م", "decimal_places": 2, "symbol_position": "after",
					"rounding_mode": round.HalfAwayFromZero.String(),
				},
			},
		},
	}
}

// rateTypeSeeds are the four system rate types (§G.1).
//
// All is_system, so code can depend on RateTypeOfficial existing without hardcoding a UUID.
// A customer adding a fifth inserts a row — no migration, which is what "add rate types
// without changing the schema" was asked for.
func rateTypeSeeds() metadata.SeedSpec {
	active := true
	return metadata.SeedSpec{
		Table: "rate_types",
		Rows: []metadata.Row{
			{Code: domain.RateTypeOfficial, Name: "Official", IsSystem: true, IsActive: &active},
			{Code: domain.RateTypeMarket, Name: "Market", IsSystem: true, IsActive: &active},
			{Code: domain.RateTypeCustom, Name: "Custom", IsSystem: true, IsActive: &active},
			{Code: domain.RateTypeManual, Name: "Manual", IsSystem: true, IsActive: &active},
		},
	}
}

// arabicNames are the Arabic labels for the seeded currencies.
//
// They go into the `translations` table rather than a name_ar column — the first real use of
// the Step 0.8 resolver, and the demonstration that adding a language to reference data is a
// data operation (§9.5). Adding Kurdish is another map, not a migration.
var arabicNames = map[string]string{
	"SYP": "ليرة سورية",
	"USD": "دولار أمريكي",
	"EUR": "يورو",
	"TRY": "ليرة تركية",
}

// arabicRateTypeNames labels the rate types in Arabic.
var arabicRateTypeNames = map[string]string{
	domain.RateTypeOfficial: "الرسمي",
	domain.RateTypeMarket:   "السوق",
	domain.RateTypeCustom:   "مخصص",
	domain.RateTypeManual:   "يدوي",
}

// SeedTranslations writes the Arabic labels for seeded reference data.
//
// Separate from the metadata seeder because translations are not a column of the seeded
// table: the seeder owns rows, this owns their labels in other languages. Idempotent, because
// Translations.Set upserts on (entity_type, entity_id, field, locale).
func (m *Module) SeedTranslations(ctx context.Context, svc *Service, trans *i18n.Translations) error {
	if trans == nil {
		return nil
	}
	currencies, err := svc.repos.Currencies.List(ctx, true)
	if err != nil {
		return err
	}
	for _, info := range currencies {
		name, ok := arabicNames[info.Code]
		if !ok {
			continue // a customer-created currency; its labels are theirs to write
		}
		if setErr := trans.Set(ctx, EntityType, info.ID, "name", locale.Locale("ar"), name); setErr != nil {
			return setErr
		}
	}

	rateTypes, err := svc.repos.RateTypes.List(ctx)
	if err != nil {
		return err
	}
	for _, rt := range rateTypes {
		name, ok := arabicRateTypeNames[rt.Code]
		if !ok {
			continue
		}
		if setErr := trans.Set(ctx, RateTypeEntityType, rt.ID, "name", locale.Locale("ar"), name); setErr != nil {
			return setErr
		}
	}
	return nil
}

// RateTypeEntityType is the `translations` entity type for rate-type labels.
const RateTypeEntityType = "rate_type"
