package bindings

import (
	"context"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/locale"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/i18n"
	"github.com/mizan-erp/mizan/internal/platform/ui"
)

// PreferencesDTO is the shell's view of the user's presentation preferences.
type PreferencesDTO struct {
	Locale string `json:"locale"`
	Theme  string `json:"theme"`
	// AvailableLocales is discovered from the embedded catalogs, not hardcoded — adding
	// locales/ku/ ships a new language with no code change (Step 0.8 §3.2).
	AvailableLocales []string `json:"availableLocales"`
}

// Config exposes presentation preferences as settings.
//
// Step 0.11 §1.4: the shell used to hold locale in component state, which meant the frontend's
// idea of the active language and the backend's `ui.locale` setting were two unrelated facts.
// They had not disagreed only because no backend call rendered text yet; the first printed
// document would have exposed it as "the invoice printed in the wrong language", which is a
// hard symptom to trace back to a useState. Both are now server state.
type Config struct{ graph }

// Preferences returns the resolved locale and theme.
func (c *Config) Preferences() envelope.Result[PreferencesDTO] {
	app, ok := c.resolve()
	if !ok {
		return envelope.Fail[PreferencesDTO](notReady())
	}
	return envelope.Ok(c.read(app.Context()))
}

// SetLocale changes the active language and returns the new preferences.
//
// Writing publishes config.SettingChangedEvent on the domain bus (0.6 D7), so "language change
// without restart" is the architecture doing what §16.3 designed it for, rather than a
// frontend trick.
func (c *Config) SetLocale(value string) envelope.Result[PreferencesDTO] {
	return c.set(i18n.LocaleSettingKey, value)
}

// SetTheme changes the colour scheme and returns the new preferences.
func (c *Config) SetTheme(value string) envelope.Result[PreferencesDTO] {
	return c.set(ui.ThemeSettingKey, value)
}

// set writes one preference inside a Unit of Work and returns the resolved result.
//
// Phase 0 writes at SYSTEM scope because identity does not exist: appctx.Scopes reports no
// user (0.10 §6.2). Both settings already permit user scope, so Phase 1 changes the scope
// argument and nothing else — the shell does not change at all.
func (c *Config) set(key, value string) envelope.Result[PreferencesDTO] {
	app, ok := c.resolve()
	if !ok {
		return envelope.Fail[PreferencesDTO](notReady())
	}
	ctx := app.Context()

	err := app.DB.Do(ctx, func(ctx context.Context) error {
		return app.Settings.Set(ctx, config.ScopeSystem, "", key, value)
	})
	if err != nil {
		// A rejected value (an unparseable locale, a theme outside the enum) arrives here as a
		// typed validation error and crosses as a code the shell renders. It must NOT be
		// swallowed: the UI has to revert rather than display a language the backend refused.
		return envelope.Fail[PreferencesDTO](err)
	}

	// Read through a FRESH context, not the one the write used.
	//
	// app.Context() stamps the resolved locale onto the context, and i18n.Active prefers a
	// stamped non-default locale over the setting. Reusing ctx would therefore echo the
	// PREVIOUS language back: switching ar→en would return "ar", because "ar" was stamped
	// before the write and takes precedence over the freshly-stored "en".
	return envelope.Ok(c.read(app.Context()))
}

// read resolves the current preferences for ctx.
func (c *Config) read(ctx context.Context) PreferencesDTO {
	app, ok := c.resolve()
	if !ok {
		return PreferencesDTO{}
	}

	available := make([]string, 0, 2)
	for _, l := range app.Catalog.Locales() {
		available = append(available, l.String())
	}

	return PreferencesDTO{
		Locale:           resolvedLocale(ctx).String(),
		Theme:            ui.Theme.Get(ctx),
		AvailableLocales: available,
	}
}

// resolvedLocale reports the language the backend will actually use.
func resolvedLocale(ctx context.Context) locale.Locale {
	return i18n.Active(ctx)
}
