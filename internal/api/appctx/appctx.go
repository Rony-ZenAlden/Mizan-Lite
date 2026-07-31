// Package appctx resolves the per-request context every use case reads: the active locale,
// the correlation id that links an event chain, and the scopes settings resolve against.
//
// It sits in the API layer because a "request" is an API concept — a Wails binding call
// today, and whatever replaces it later. The platform packages below define ports; this is
// where they are satisfied.
package appctx

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/event"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/locale"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/i18n"
)

// Scopes reports the acting company, branch, and user for settings resolution.
//
// In Phase 0 it reports NONE of them, because identity does not exist until Phase 1 — there
// are no companies, branches, or users to be acting as. Settings therefore resolve at system
// scope or fall through to their declared defaults, which is the correct behaviour before the
// setup wizard has run, not a placeholder.
//
// Phase 1 replaces this with a session-reading implementation. Because it satisfies
// config.ScopeProvider, that is a constructor argument in the composition root and no call
// site changes.
type Scopes struct{}

var _ config.ScopeProvider = Scopes{}

func (Scopes) CompanyID(context.Context) (id.ID, bool) { return "", false }
func (Scopes) BranchID(context.Context) (id.ID, bool)  { return "", false }
func (Scopes) UserID(context.Context) (id.ID, bool)    { return "", false }

// Enrich stamps a context for one unit of work.
//
// It closes the item Step 0.8 carried forward: the locale was resolvable from the `ui.locale`
// setting, but nothing put it on the context, so every downstream reader saw the default.
//
// A fresh correlation id is stamped per call, so every event raised while handling it — and
// everything those events cause (Step 0.6) — shares one identifier. That is what makes a
// support question like "what did this click actually do?" answerable from the outbox alone.
//
// Requires settings to be bound (config.Bind). Without them it degrades to the default locale
// rather than failing: a missing locale must never stop work.
func Enrich(ctx context.Context) context.Context {
	ctx = locale.WithLocale(ctx, i18n.Active(ctx))

	if _, already := event.CorrelationID(ctx); !already {
		if correlation, err := id.New(); err == nil {
			ctx = event.WithCorrelationID(ctx, correlation)
		}
	}
	return ctx
}

// WithLocale forces a specific locale for one call, overriding the setting.
//
// The preview case: printing a document in a customer's language, or showing an invoice as it
// will appear to its recipient, without changing anyone's stored preference.
func WithLocale(ctx context.Context, l locale.Locale) context.Context {
	return locale.WithLocale(ctx, l)
}
