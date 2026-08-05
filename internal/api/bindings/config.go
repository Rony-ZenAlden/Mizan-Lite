package bindings

import (
	"context"

	"github.com/mizan-erp/mizan/internal/api/appctx"
	"github.com/mizan-erp/mizan/internal/api/policy"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/identity"

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

// configPolicies declares what Config's methods require.
//
// Reading and setting your OWN language and theme needs no permission beyond being signed in:
// they are personal preferences resolved at user scope (1.3 §6), and gating them behind an
// administrative right would mean a cashier could not read their own screen.
func configPolicies() map[string]policy.Policy {
	return map[string]policy.Policy{
		"Preferences": policy.Requires(identity.PermUserView),
		"SetLocale":   policy.Requires(identity.PermUserView),
		"SetTheme":    policy.Requires(identity.PermUserView),
	}
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
	ctx, _, err := c.guard("Preferences")
	if err != nil {
		return envelope.Fail[PreferencesDTO](err)
	}
	return envelope.Ok(c.read(ctx))
}

// SetLocale changes the active language and returns the new preferences.
//
// Writing publishes config.SettingChangedEvent on the domain bus (0.6 D7), so "language change
// without restart" is the architecture doing what §16.3 designed it for, rather than a
// frontend trick.
func (c *Config) SetLocale(value string) envelope.Result[PreferencesDTO] {
	return c.set("SetLocale", i18n.LocaleSettingKey, value)
}

// SetTheme changes the colour scheme and returns the new preferences.
func (c *Config) SetTheme(value string) envelope.Result[PreferencesDTO] {
	return c.set("SetTheme", ui.ThemeSettingKey, value)
}

// set writes one preference inside a Unit of Work and returns the resolved result.
//
// Step 0.11 (D4) wrote at SYSTEM scope because identity did not exist and appctx.Scopes
// reported no user. It now writes at USER scope when there is an actor, which is what makes
// two people sharing a machine get their own language and theme — and the shell did not change
// at all, exactly as 0.11 predicted.
func (c *Config) set(method, key, value string) envelope.Result[PreferencesDTO] {
	ctx, app, err := c.guard(method)
	if err != nil {
		return envelope.Fail[PreferencesDTO](err)
	}

	err = app.DB.Do(ctx, func(ctx context.Context) error {
		scope, scopeID := preferenceScope(ctx)
		return app.Settings.Set(ctx, scope, scopeID, key, value)
	})
	if err != nil {
		// A rejected value (an unparseable locale, a theme outside the enum) arrives here as a
		// typed validation error and crosses as a code the shell renders. It must NOT be
		// swallowed: the UI has to revert rather than display a language the backend refused.
		return envelope.Fail[PreferencesDTO](err)
	}

	// Read through a FRESH context, not the one the write used — and one that still carries the
	// actor.
	//
	// Two distinct reasons, and missing either gives a wrong answer:
	//
	//   - app.Context() stamps the resolved locale, and i18n.Active prefers a stamped
	//     non-default locale over the setting. Reusing ctx would echo the PREVIOUS language:
	//     switching ar→en returns "ar", because "ar" was stamped before the write.
	//   - The preference now lives at USER scope, so a context with no actor resolves it at
	//     system scope and reports whatever the machine default is rather than what this user
	//     just chose.
	//
	// Re-guarding produces both: a fresh context with the actor stamped.
	fresh, _, err := c.guard(method)
	if err != nil {
		return envelope.Fail[PreferencesDTO](err)
	}
	return envelope.Ok(c.read(fresh))
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

// preferenceScope decides where a preference is stored.
//
// User scope when someone is signed in; system scope otherwise — which is the setup wizard's
// state, where "the machine's language" is the only meaningful answer.
func preferenceScope(ctx context.Context) (config.Scope, id.ID) {
	if actor, ok := appctx.ActorFrom(ctx); ok && !actor.UserID.IsZero() {
		return config.ScopeUser, actor.UserID
	}
	return config.ScopeSystem, ""
}

// resolvedLocale reports the language the backend will actually use.
func resolvedLocale(ctx context.Context) locale.Locale {
	return i18n.Active(ctx)
}
