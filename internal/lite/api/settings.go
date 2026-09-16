package api

import (
	"context"
	"strconv"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/fx/infra/httpsource"
	"github.com/mizan-erp/mizan/internal/lite/settings/domain"
)

// Settings reads and changes the shop's settings.
type Settings struct{ core *core }

// SettingsDTO is every setting, resolved.
type SettingsDTO struct {
	Locale   string `json:"locale"`
	ShopName string `json:"shopName"`
	// Direction is derived, not stored, and sent so the frontend never re-derives it.
	Direction string `json:"direction"`
	// DebtCurrency is the currency the till charges a credit sale in by default (Q-L5.1).
	DebtCurrency string `json:"debtCurrency"`
	// LocalCurrency is the currency the exchange rate prices, and CashNote the smallest note local totals round to.
	LocalCurrency string `json:"localCurrency"`
	CashNote      string `json:"cashNote"`
	// RateSource is where a fetched rate comes from — "standard" or "local" — with the shop's own endpoint and the
	// dotted path to the number inside its JSON (2026-09-17). The URL is empty until a shop sets one up.
	RateSource     string `json:"rateSource"`
	LocalRateURL   string `json:"localRateUrl"`
	LocalRateField string `json:"localRateField"`
}

// SettingsInput is a partial change. An absent field is left as it is.
type SettingsInput struct {
	Locale   *string `json:"locale,omitempty"`
	ShopName *string `json:"shopName,omitempty"`
	// The exchange rate's source and the shop's own endpoint (2026-09-17).
	RateSource     *string `json:"rateSource,omitempty"`
	LocalRateURL   *string `json:"localRateUrl,omitempty"`
	LocalRateField *string `json:"localRateField,omitempty"`
}

func toSettingsDTO(s domain.Settings) SettingsDTO {
	return SettingsDTO{Locale: string(s.Locale), ShopName: s.ShopName, Direction: string(s.Locale.Direction()),
		DebtCurrency: s.DebtCurrency, LocalCurrency: s.LocalCurrency, CashNote: strconv.FormatInt(s.CashNote, 10),
		RateSource: s.RateSource, LocalRateURL: s.LocalRateURL, LocalRateField: s.LocalRateField}
}

// Get returns the current settings.
func (s *Settings) Get() envelope.Result[SettingsDTO] {
	return call(s.core, "Settings.Get", func(ctx context.Context, app *bootstrap.App) (SettingsDTO, error) {
		resolved, err := app.Settings.Get(ctx)
		return toSettingsDTO(resolved), err
	})
}

// ActRateSource names a change of rate source in the owner's history.
const ActRateSource = "fx.source.set"

// Update applies a partial change and returns the settings as they now are.
func (s *Settings) Update(in SettingsInput) envelope.Result[SettingsDTO] {
	return call(s.core, "Settings.Update", func(ctx context.Context, app *bootstrap.App) (SettingsDTO, error) {
		// A local-market endpoint is checked here, where the shop can be told about it, rather than at fetch time where
		// the only sign would be a rate that never arrives (2026-09-17).
		var url *string
		if in.LocalRateURL != nil {
			checked, err := httpsource.ParseLocalURL(*in.LocalRateURL)
			if err != nil {
				return SettingsDTO{}, err
			}
			url = &checked
		}
		var updated domain.Settings
		err := app.DB.Do(ctx, func(ctx context.Context) error {
			var updateErr error
			updated, updateErr = app.Settings.Update(ctx, domain.Update{Locale: in.Locale, ShopName: in.ShopName,
				RateSource: in.RateSource, LocalRateURL: url, LocalRateField: in.LocalRateField})
			if updateErr != nil {
				return updateErr
			}
			// The rate's source is the shop's money: recorded like every other act (D-091.5).
			if in.RateSource != nil || in.LocalRateURL != nil || in.LocalRateField != nil {
				return requireOwner(ctx, app, ActRateSource, updated.RateSource)
			}
			return nil
		})
		return toSettingsDTO(updated), err
	})
}
