package bindings_test

import (
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/api/bindings"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/modules/identity"
)

// allPermissions is what the built modules declare, as the shell collects it at boot.
func allPermissions() map[string]bool {
	out := map[string]bool{}
	for _, def := range identity.NewModule(nil).Permissions() {
		out[def.Code] = true
	}
	return out
}

// TestEveryBindingMethodHasAPolicy is §14.3's guarantee, and the reason this step exists.
//
// A method reachable from JavaScript with no declared policy is exactly what "nothing ships
// unprotected by accident" forbids. Adding a binding method without a policy fails HERE, on the
// developer's machine at first boot, rather than on a customer's.
//
// Mutation check: delete any entry from a *Policies() map and this fails naming it.
func TestEveryBindingMethodHasAPolicy(t *testing.T) {
	set := bindings.New()

	if err := set.ValidatePolicies(allPermissions()); err != nil {
		t.Fatalf("the binding surface is not fully protected:\n%v", err)
	}
}

// TestPolicyCoverageRejectsAnUndeclaredPermission: a policy naming a permission no module
// declares could never be granted, so the method would be permanently unreachable. That is a
// silent outage rather than a security hole, and just as worth catching.
func TestPolicyCoverageRejectsAnUndeclaredPermission(t *testing.T) {
	set := bindings.New()

	// An empty permission universe: every non-public policy now names something undeclared.
	err := set.ValidatePolicies(map[string]bool{"nothing.at.all": true})
	if err == nil {
		t.Fatal("a policy naming an undeclared permission was accepted")
	}
	if code := errs.CodeOf(err); code != bindings.CodePolicyCoverage {
		t.Errorf("code = %q, want %q", code, bindings.CodePolicyCoverage)
	}
	// The detail must be in the MESSAGE — this is a startup error read in a log, and
	// errs.Error.Error() does not render params (0.10 §9.2).
	if !strings.Contains(err.Error(), "which no module declares") {
		t.Errorf("the error does not explain the problem: %v", err)
	}
}

// TestPublicMethodsArePinned makes widening the unauthenticated surface a visible diff.
//
// Public is how a method becomes reachable without a session. That is occasionally correct —
// login must be — but it is exactly the change that should never happen quietly, so the set is
// asserted rather than trusted.
func TestPublicMethodsArePinned(t *testing.T) {
	want := map[string]bool{
		"Auth.Login":  true, // must be reachable by an unauthenticated caller
		"Auth.Logout": true, // a JUST-expired session must still be able to clear itself
		"Auth.Me":     true, // "am I signed in?" is asked before the answer is known
	}

	got := bindings.PublicMethodsForTest(bindings.New())
	for name := range got {
		if !want[name] {
			t.Errorf("%s is PUBLIC and was not expected to be — "+
				"widening the unauthenticated surface must be deliberate", name)
		}
	}
	for name := range want {
		if !got[name] {
			t.Errorf("%s is no longer public; the frontend flow that relies on it will break", name)
		}
	}
}

// TestBootNeedsNoPolicy: Boot is the binding that reports whether the graph exists, so it
// cannot require one. It is the single deliberate exemption, and it holds no graph at all.
func TestBootNeedsNoPolicy(t *testing.T) {
	set := bindings.New()

	result := set.Boot.Status()
	if !result.OK {
		t.Errorf("Boot.Status is not callable before the graph exists: %+v", result.Error)
	}
}
