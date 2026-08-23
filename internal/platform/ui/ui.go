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

// LandingSettingKey is the stable key of the screen a user lands on.
const LandingSettingKey = "ui.landing"

// The screens a workspace can open on.
//
// PATHS, matching the router's own — not module names. The alternative was an abstract token the
// frontend maps to a route, which is a second list to keep in step and a mapping that goes stale
// the first time a route moves.
const (
	LandingOverview = "/"
	LandingTill     = "/pos"
	LandingInvoices = "/sales/invoices"
)

// Landing is the screen this installation opens on.
//
// # Why a shop's trade decides its home screen
//
// A retail counter opens Mizan to sell something; the till is the screen it lives on all day, and
// starting anywhere else is a click before every shift. A wholesaler opens it to raise and chase
// invoices, and a dashboard of today's takings is the wrong first thing when most of the money is
// owed rather than taken. A services business is closer to the second than the first.
//
// So the business profile sets it — through the ordinary settings mechanism, which means it is a
// DEFAULT and not a rule: §C.1's constraint on the whole profile system is that a profile supplies
// defaults and never constrains later configuration. A shop that disagrees changes one setting.
//
// An enum rather than free text: an arbitrary string here would be a route this application may
// not serve, and the landing screen is the one place a bad value strands somebody with nowhere to
// go.
var Landing = config.DeclareEnum(config.Def{
	Key:         LandingSettingKey,
	Default:     LandingOverview,
	Enum:        []string{LandingOverview, LandingTill, LandingInvoices},
	Scopes:      []config.Scope{config.ScopeSystem, config.ScopeCompany, config.ScopeUser},
	Description: "settings.ui.landing",
})
