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

// Actor is who a call is being made by, and where.
//
// Held in the context rather than passed as an argument because every layer beneath the
// binding needs it and none of them should have to thread it — the same reasoning that puts
// the transaction in the context (0.3 §3.2).
type Actor struct {
	UserID    id.ID
	CompanyID id.ID
	BranchID  id.ID
	Username  string
	// Locale is the acting user's stored preference, empty if they have none.
	Locale string
}

type actorKey struct{}

// WithActor stamps the acting principal onto a context.
//
// Called by the binding decorator (1.5) after a session validates. The setup wizard and the
// login screen run WITHOUT one, which is a legitimate state, not an error.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

// ActorFrom returns the acting principal, if there is one.
func ActorFrom(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(actorKey{}).(Actor)
	return a, ok
}

// Scopes reports the acting company, branch, and user for settings resolution.
//
// Until Step 1.3 this reported NONE of them, because identity did not exist — settings
// resolved at system scope, which is why Step 0.11 (D4) had to write `ui.locale` and
// `ui.theme` at system scope even though both are declared user-scopable.
//
// It now reads the session's actor, which closes the item 0.10 §6.2 and 0.11 D4 both carried.
// Two things arrive for free, because the machinery was built expecting them: settings resolve
// PER USER through the 0.5 scope chain with no frontend change, and two people sharing a
// machine each get their own language and theme.
//
// With no actor — the login screen, the setup wizard — every scope is absent and resolution
// falls through to system scope exactly as before. That is a legitimate state, not a
// degradation.
type Scopes struct{}

var _ config.ScopeProvider = Scopes{}

func (Scopes) CompanyID(ctx context.Context) (id.ID, bool) {
	a, ok := ActorFrom(ctx)
	return a.CompanyID, ok && !a.CompanyID.IsZero()
}

func (Scopes) BranchID(ctx context.Context) (id.ID, bool) {
	a, ok := ActorFrom(ctx)
	return a.BranchID, ok && !a.BranchID.IsZero()
}

func (Scopes) UserID(ctx context.Context) (id.ID, bool) {
	a, ok := ActorFrom(ctx)
	return a.UserID, ok && !a.UserID.IsZero()
}

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
