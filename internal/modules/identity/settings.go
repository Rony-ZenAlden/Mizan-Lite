package identity

import (
	"github.com/mizan-erp/mizan/internal/platform/config"
)

// Setting keys. Stable, and part of the public contract once shipped.
const (
	SettingMinLength    = "identity.password.min_length"
	SettingHistoryCount = "identity.password.history"
	SettingExpiryDays   = "identity.password.expiry_days"
)

// The password policy, as settings (§13.1).
//
// # Why these defaults (Step 1.2, D3)
//
// They follow NIST SP 800-63B rather than the conventional corporate checklist, which is a
// deliberate choice and the one most likely to look "wrong" at a glance:
//
//   - LENGTH over composition. No rule demands a symbol, a digit, or mixed case. Composition
//     rules push people toward Password1! — predictable substitutions that barely raise
//     entropy while making passwords harder to remember, which drives them onto sticky notes.
//
//   - NO FORCED ROTATION (expiry 0 = never). Periodic expiry produces Summer2026! → Summer2027!
//     — measurably weaker than one stable strong password, and it trains users to write them
//     down. An installation whose auditor demands rotation sets the value and gets it; the
//     DEFAULT is what a well-run system does.
//
//   - HISTORY is kept anyway, at 5. It costs one indexed read and stops the trivial "change it
//     and change it straight back", which is the one reuse pattern rotation was ever good at
//     preventing.
//
// The minimum length is additionally floored at 8 in the domain: a setting is configuration,
// not a licence to disable security (§CFG.5).
var (
	// MinLength is the minimum password length, in runes.
	MinLength = config.DeclareInt(config.Def{
		Key:         SettingMinLength,
		Default:     int64(12),
		Scopes:      []config.Scope{config.ScopeSystem, config.ScopeCompany},
		Description: "settings.identity.password.min_length",
	})

	// HistoryCount is how many previous passwords may not be reused. 0 disables the check.
	HistoryCount = config.DeclareInt(config.Def{
		Key:         SettingHistoryCount,
		Default:     int64(5),
		Scopes:      []config.Scope{config.ScopeSystem, config.ScopeCompany},
		Description: "settings.identity.password.history",
	})

	// ExpiryDays is how long a password stays valid. 0 means never — see the note above.
	ExpiryDays = config.DeclareInt(config.Def{
		Key:         SettingExpiryDays,
		Default:     int64(0),
		Scopes:      []config.Scope{config.ScopeSystem, config.ScopeCompany},
		Description: "settings.identity.password.expiry_days",
	})
)

// settingDefinitions is what the module reports to the composition root.
//
// Looked up from the registry by key rather than built from the handles: a handle is a typed
// accessor, and the Definition (with its encoded default and value type) is what startup
// validation and the future settings UI need. Same shape as the currency module.
func settingDefinitions() []config.Definition {
	keys := []string{SettingMinLength, SettingHistoryCount, SettingExpiryDays}
	out := make([]config.Definition, 0, len(keys))
	for _, k := range keys {
		if def, ok := config.Default().Lookup(k); ok {
			out = append(out, def)
		}
	}
	return out
}
