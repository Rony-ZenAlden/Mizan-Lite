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

// Session and lockout keys.
const (
	SettingIdleMinutes      = "identity.session.idle_minutes"
	SettingAbsoluteHours    = "identity.session.absolute_hours"
	SettingRememberDays     = "identity.session.remember_days"
	SettingLockoutThreshold = "identity.lockout.threshold"
	SettingLockoutBase      = "identity.lockout.base_seconds"
	SettingLockoutMax       = "identity.lockout.max_seconds"
)

// Session lifetime and lockout behaviour.
//
// # Why 60 minutes idle and not 30 (Step 1.3, D1)
//
// A shop is quiet for stretches. A 30-minute idle timeout means re-typing a twelve-character
// passphrase after every lull, and the predictable outcome is a shorter password, a password on
// a sticky note, or the timeout switched off. That is the same failure the password policy was
// designed to avoid: friction that drives users toward weaker security is not security.
//
// 60 minutes with a 12-hour absolute cap keeps what matters — an unattended machine overnight,
// a stolen laptop — without the daily friction. The right answer for a till is not a shorter
// timeout but a screen lock resumed with a PIN, which is what §13.1's PIN credential is for and
// what Phase 5 will build.
var (
	IdleMinutes = config.DeclareInt(config.Def{
		Key:         SettingIdleMinutes,
		Default:     int64(60),
		Scopes:      []config.Scope{config.ScopeSystem, config.ScopeCompany},
		Description: "settings.identity.session.idle_minutes",
	})

	AbsoluteHours = config.DeclareInt(config.Def{
		Key:         SettingAbsoluteHours,
		Default:     int64(12),
		Scopes:      []config.Scope{config.ScopeSystem, config.ScopeCompany},
		Description: "settings.identity.session.absolute_hours",
	})

	RememberDays = config.DeclareInt(config.Def{
		Key:         SettingRememberDays,
		Default:     int64(30),
		Scopes:      []config.Scope{config.ScopeSystem, config.ScopeCompany},
		Description: "settings.identity.session.remember_days",
	})

	// LockoutThreshold is how many consecutive failures precede any delay.
	LockoutThreshold = config.DeclareInt(config.Def{
		Key:         SettingLockoutThreshold,
		Default:     int64(5),
		Scopes:      []config.Scope{config.ScopeSystem, config.ScopeCompany},
		Description: "settings.identity.lockout.threshold",
	})

	LockoutBaseSeconds = config.DeclareInt(config.Def{
		Key:         SettingLockoutBase,
		Default:     int64(30),
		Scopes:      []config.Scope{config.ScopeSystem, config.ScopeCompany},
		Description: "settings.identity.lockout.base_seconds",
	})

	// LockoutMaxSeconds CAPS the delay, and the cap is the point (Step 1.3, D2): this product
	// has no password-reset email, so an unbounded lockout on the sole administrator would be
	// an attacker-triggered outage recoverable only by editing the database by hand.
	LockoutMaxSeconds = config.DeclareInt(config.Def{
		Key:         SettingLockoutMax,
		Default:     int64(900),
		Scopes:      []config.Scope{config.ScopeSystem, config.ScopeCompany},
		Description: "settings.identity.lockout.max_seconds",
	})
)

// settingDefinitions is what the module reports to the composition root.
//
// Looked up from the registry by key rather than built from the handles: a handle is a typed
// accessor, and the Definition (with its encoded default and value type) is what startup
// validation and the future settings UI need. Same shape as the currency module.
func settingDefinitions() []config.Definition {
	keys := []string{
		SettingMinLength, SettingHistoryCount, SettingExpiryDays,
		SettingIdleMinutes, SettingAbsoluteHours, SettingRememberDays,
		SettingLockoutThreshold, SettingLockoutBase, SettingLockoutMax,
	}
	out := make([]config.Definition, 0, len(keys))
	for _, k := range keys {
		if def, ok := config.Default().Lookup(k); ok {
			out = append(out, def)
		}
	}
	return out
}
