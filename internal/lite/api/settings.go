package api

import (
	"context"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/settings/domain"
)

// Settings reads and changes the shop's settings.
type Settings struct{ core *core }

// SettingsDTO is every setting, resolved.
type SettingsDTO struct {
	Locale string `json:"locale"`
	// Direction is derived, not stored, and sent so the frontend never re-derives it.
	Direction string `json:"direction"`
}

// SettingsInput is a partial change. An absent field is left as it is.
type SettingsInput struct {
	Locale *string `json:"locale,omitempty"`
}

func toSettingsDTO(s domain.Settings) SettingsDTO {
	return SettingsDTO{Locale: string(s.Locale), Direction: string(s.Locale.Direction())}
}

// Get returns the current settings.
func (s *Settings) Get() envelope.Result[SettingsDTO] {
	return call(s.core, "Settings.Get", func(ctx context.Context, app *bootstrap.App) (SettingsDTO, error) {
		resolved, err := app.Settings.Get(ctx)
		return toSettingsDTO(resolved), err
	})
}

// Update applies a partial change and returns the settings as they now are.
func (s *Settings) Update(in SettingsInput) envelope.Result[SettingsDTO] {
	return call(s.core, "Settings.Update", func(ctx context.Context, app *bootstrap.App) (SettingsDTO, error) {
		updated, err := app.Settings.Update(ctx, domain.Update{Locale: in.Locale})
		return toSettingsDTO(updated), err
	})
}
