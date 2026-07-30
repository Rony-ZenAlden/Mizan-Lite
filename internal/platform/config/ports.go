package config

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// The three ports below exist so this package — which is platform, and sits below the API
// layer — never imports internal/api, and does not pre-empt subsystems that arrive later
// (the event bus in Step 0.6, RBAC in Phase 1). Each has a trivial Phase-0
// implementation; replacing it is a constructor argument, not an edit to any call site.

// ScopeProvider reports the acting company, branch, and user for a request, so settings
// can be resolved by scope. Implemented by the app context layer in Step 0.10.
type ScopeProvider interface {
	CompanyID(ctx context.Context) (id.ID, bool)
	BranchID(ctx context.Context) (id.ID, bool)
	UserID(ctx context.Context) (id.ID, bool)
}

// ChangeNotifier is told when a setting changes, so subscribers can react without a
// restart — the mechanism behind "changing language takes effect immediately"
// (ARCHITECTURE_v1 §16.3). Step 0.6 supplies the event-bus implementation.
type ChangeNotifier interface {
	SettingChanged(ctx context.Context, key string, scope Scope, scopeID id.ID)
}

// Authorizer answers whether the caller holds a permission. Phase 1 supplies the RBAC
// implementation.
type Authorizer interface {
	Can(ctx context.Context, permission string) bool
}

// NoScopes is a ScopeProvider that reports no active company, branch, or user, so every
// setting resolves at system scope or falls through to its declared default. It is the
// correct behaviour before the setup wizard has created a company (Phase 1).
type NoScopes struct{}

func (NoScopes) CompanyID(context.Context) (id.ID, bool) { return "", false }
func (NoScopes) BranchID(context.Context) (id.ID, bool)  { return "", false }
func (NoScopes) UserID(context.Context) (id.ID, bool)    { return "", false }

// NoNotifier discards change notifications. Used until the event bus exists.
type NoNotifier struct{}

func (NoNotifier) SettingChanged(context.Context, string, Scope, id.ID) {}

// AllowAll permits every permission check.
//
// This is the honest Phase-0 behaviour: there are no users, roles, or sessions yet, so
// there is nothing to deny. It is a port implementation rather than a skipped check, so
// Phase 1 turns authorization on by construction instead of by remembering to add it.
type AllowAll struct{}

func (AllowAll) Can(context.Context, string) bool { return true }

// fixedScopes is a ScopeProvider with constant values, used by tests and by the bootstrap
// once the active company and branch are known.
type fixedScopes struct {
	company, branch, user id.ID
}

// FixedScopes returns a ScopeProvider reporting the given identifiers. An empty value
// means "not set at this scope", and resolution skips that tier.
func FixedScopes(company, branch, user id.ID) ScopeProvider {
	return fixedScopes{company: company, branch: branch, user: user}
}

func (f fixedScopes) CompanyID(context.Context) (id.ID, bool) {
	return f.company, !f.company.IsZero()
}
func (f fixedScopes) BranchID(context.Context) (id.ID, bool) { return f.branch, !f.branch.IsZero() }
func (f fixedScopes) UserID(context.Context) (id.ID, bool)   { return f.user, !f.user.IsZero() }
