package i18n

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/locale"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
)

// LocaleSettingKey is the stable key of the active-language setting.
const LocaleSettingKey = "ui.locale"

// Locale is the typed handle for the active language.
//
// It is an ordinary setting, not a subsystem of its own (design D2). Scoped to system,
// company, AND user: a cashier may prefer Arabic on a shop configured in English, and the
// scope chain from Step 0.5 already resolves that correctly.
//
// Declared on the process registry so a module can read the active language with
// `i18n.Locale.Get(ctx)` and no key string.
var Locale = config.DeclareString(config.Def{
	Key:         LocaleSettingKey,
	Default:     locale.Default.String(),
	Scopes:      []config.Scope{config.ScopeSystem, config.ScopeCompany, config.ScopeUser},
	Description: "settings.ui.locale",
	Validate: func(v any) error {
		s, _ := v.(string)
		if _, ok := locale.Parse(s); !ok {
			return errs.Validation(CodeInvalidLocale, "that is not a valid language tag").
				WithParam("locale", s)
		}
		return nil
	},
})

// Active returns the locale to use for ctx.
//
// Preference order: an explicit locale attached to the context (set per request by the API
// layer) wins; otherwise the resolved `ui.locale` setting; otherwise the default. A tag that
// no longer parses falls back rather than failing — a bad stored value must not stop a shop
// from opening.
func Active(ctx context.Context) locale.Locale {
	if l := locale.FromContext(ctx); !l.IsZero() && l != locale.Default {
		return l
	}
	if parsed, ok := locale.Parse(Locale.Get(ctx)); ok {
		return parsed
	}
	return locale.Default
}

// OnLocaleChanged subscribes to language changes.
//
// There is deliberately no LocaleChanged event type. Writing the setting already publishes
// config.SettingChangedEvent on the domain bus (Step 0.6, D7), which IS "subscribers react
// without a restart" (§22.3). A second event would be a second way to observe one fact, and
// the two would eventually disagree about ordering.
//
// This helper gives subscribers the typed ergonomics of a dedicated event over the one
// mechanism: it filters for the locale key and hands the handler a parsed Locale.
func OnLocaleChanged(bus *eventbus.Bus, name string, fn func(context.Context, locale.Locale) error) error {
	return eventbus.Subscribe(bus, name, func(ctx context.Context, e config.SettingChangedEvent) error {
		if e.Key != LocaleSettingKey {
			return nil
		}
		return fn(ctx, Active(ctx))
	})
}
