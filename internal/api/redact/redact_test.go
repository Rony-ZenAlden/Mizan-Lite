package redact_test

import (
	"context"
	"testing"

	"github.com/mizan-erp/mizan/internal/api/redact"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/auth"
)

// stubAuthorizer answers a fixed set of permissions.
type stubAuthorizer struct{ allowed map[string]bool }

func (s stubAuthorizer) Can(_ context.Context, permission string, _ auth.Scope) bool {
	return s.allowed[permission]
}

func (s stubAuthorizer) Effective(context.Context) ([]string, error) { return nil, nil }

func TestVisibleReturnsTheValueWithThePermission(t *testing.T) {
	authz := stubAuthorizer{allowed: map[string]bool{"a.b.c": true}}

	got := redact.Visible(context.Background(), authz, "a.b.c", "secret")
	if got == nil || *got != "secret" {
		t.Errorf("Visible = %v, want a pointer to the value", got)
	}
}

func TestVisibleReturnsNilWithoutThePermission(t *testing.T) {
	authz := stubAuthorizer{allowed: map[string]bool{"something.else": true}}

	if got := redact.Visible(context.Background(), authz, "a.b.c", "secret"); got != nil {
		t.Errorf("Visible = %v, want nil", got)
	}
}

// TestVisibleFailsClosed: every reason a check cannot be made must deny.
//
// A redaction helper that failed open would defeat its own purpose — the whole point is that
// the absence of an affirmative answer hides the field.
func TestVisibleFailsClosed(t *testing.T) {
	cases := map[string]struct {
		authz      auth.Authorizer
		permission string
	}{
		"no authorizer wired": {nil, "a.b.c"},
		"no permission named": {stubAuthorizer{allowed: map[string]bool{"": true}}, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := redact.Visible(context.Background(), tc.authz, tc.permission, "secret"); got != nil {
				t.Errorf("Visible = %v, want nil — redaction must fail closed", got)
			}
		})
	}
}

// TestVisibleInPassesTheScope: a field may be visible in one branch and not another.
func TestVisibleInPassesTheScope(t *testing.T) {
	var seen auth.Scope
	authz := scopeRecorder{seen: &seen}

	branchID := "019fd000-0000-7000-8000-000000000001"
	redact.VisibleIn(context.Background(), authz, "a.b.c", auth.InBranch(id.ID(branchID)), "x")

	if seen.Kind != auth.ScopeBranch || string(seen.ID) != branchID {
		t.Errorf("scope passed through = %+v, want the branch scope", seen)
	}
}

type scopeRecorder struct{ seen *auth.Scope }

func (r scopeRecorder) Can(_ context.Context, _ string, scope auth.Scope) bool {
	*r.seen = scope
	return false
}

func (r scopeRecorder) Effective(context.Context) ([]string, error) { return nil, nil }
