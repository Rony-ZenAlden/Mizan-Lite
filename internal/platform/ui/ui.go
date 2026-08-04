// Package ui declares presentation preferences that are settings rather than code.
//
// It exists because the theme has no owning module: `ui.locale` belongs to i18n because a
// language IS that package's subject, but a colour scheme belongs to nothing in particular.
// Declaring it here keeps every setting declaration in platform, alongside i18n's, currency's,
// and the job registry's — rather than putting the first one in the API layer and starting a
// second convention.
//
// It is deliberately tiny. Density, display date format, and first-day-of-week are the same
// kind of preference and will land here when a screen needs them.
package ui

import "github.com/mizan-erp/mizan/internal/platform/config"

// ThemeSettingKey is the stable key of the colour-scheme preference.
const ThemeSettingKey = "ui.theme"

// The permitted themes.
//
// ThemeSystem ("follow the OS") is included from the start because a desktop application that
// ignores the operating system's appearance feels foreign, and adding an enum value later
// would mean migrating stored rows. The setting records the user's CHOICE, not the resolved
// colour scheme — storing "dark" because the OS was dark on the day it was set would freeze
// the preference to that moment.
const (
	ThemeLight  = "light"
	ThemeDark   = "dark"
	ThemeSystem = "system"
)

// Theme is the typed handle for the colour-scheme preference.
//
// Scoped exactly like ui.locale: system, company, and user. Phase 0 has no identity, so writes
// land at system scope; the per-user preference becomes real in Phase 1 with no call-site
// change, because the scope chain from Step 0.5 already resolves it.
var Theme = config.DeclareEnum(config.Def{
	Key:         ThemeSettingKey,
	Default:     ThemeSystem,
	Enum:        []string{ThemeLight, ThemeDark, ThemeSystem},
	Scopes:      []config.Scope{config.ScopeSystem, config.ScopeCompany, config.ScopeUser},
	Description: "settings.ui.theme",
})
