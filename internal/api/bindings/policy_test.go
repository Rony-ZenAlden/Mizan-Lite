package bindings_test

import (
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/api/bindings"
	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// allPermissions is what the built modules declare, as the shell collects it at boot.
// allPermissions is what THIS BUILD declares, from the one list every module appears in.
//
// It used to enumerate three modules by hand, which meant a fourth module's permissions were
// missing from the very check that exists to catch an unreachable method — and the omission
// surfaced only when someone added a binding that used one. Step 2.8 replaced it with
// bootstrap.DeclaredPermissions, which is the same source production validates against.
func allPermissions() map[string]bool {
	return bootstrap.DeclaredPermissions()
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

		// The bootstrap paradox (§WIZ.1): no user exists, so nothing about first-run setup can
		// be permissioned. Both are Public, and Apply carries a SECOND gate on top —
		// setupGuard refuses once a company exists, which TestSetupIsUnreachableAfterSetup
		// pins. Public here means "no session", not "no rule".
		"Setup.Status": true,
		"Setup.Apply":  true,

		// Public means "no PERMISSION required", not "no session required" — the method reads
		// its subject from the context and takes no user id, so a caller with no session has no
		// actor and is refused. It is public because the user who most needs it may hold no
		// permission at all: must_change is typically set on an account created seconds
		// earlier with no role assigned yet (1.11 D3).
		"Identity.ChangeMyPassword": true,
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

// TestAWritingMethodIsNotGatedOnMerelyViewing.
//
// # Why this exists
//
// TestEveryBindingMethodHasAPolicy proves each method has a policy. It does NOT prove the policy
// is the right one — and a mutation drill found the gap: changing `Inventory.Adjust` to require
// `inventory.stock.view` instead of `inventory.stock.adjust` broke nothing. Every read-only user
// would silently have gained the ability to write stock off.
//
// That failure is invisible in exactly the way this codebase keeps guarding against: nothing
// errors, no screen changes, and the only symptom is that a permission which was supposed to
// separate two jobs no longer does.
//
// The rule asserted here is general rather than a list: a method whose name says it CHANGES
// something must not be gated on a permission whose name says `.view`. It costs nothing and
// catches the whole class.
func TestAWritingMethodIsNotGatedOnMerelyViewing(t *testing.T) {
	// Verbs that mean "this changes something". Names rather than a hand-maintained list of
	// methods, so a new writing method is covered the day it is written rather than the day
	// somebody remembers to add it here.
	//
	// The first version used prefixes and caught `Inventory.Movements` — a READ whose name
	// happens to start with "Move". A heuristic that reports a false positive is a heuristic
	// people learn to silence, so the match is now on a whole leading word: a method is a write
	// when its name IS the verb or continues with an upper-case letter after it.
	writingVerbs := []string{
		"Adjust", "Create", "Update", "Delete", "Set", "Apply", "Assign", "Revoke",
		"Grant", "Move", "Transfer", "Post", "Close", "Reopen", "Cancel", "Submit",
		"Begin", "Record", "Open", "Change", "Rebuild",
	}

	// Methods that change only the CALLER's own presentation.
	//
	// Choosing your own language or theme is not an administrative act, and the person who most
	// needs it may hold almost nothing — the same reasoning 1.11 D3 applied to changing your own
	// password. Listing them explicitly, with this reason attached, is better than widening the
	// rule until it stops catching anything.
	personal := map[string]bool{
		"Config.SetLocale": true,
		"Config.SetTheme":  true,
	}

	for name, permission := range bindings.DeclaredPoliciesForTest(bindings.New()) {
		if permission == "" || personal[name] {
			continue
		}
		method := name[strings.Index(name, ".")+1:]

		writes := false
		for _, verb := range writingVerbs {
			if !strings.HasPrefix(method, verb) {
				continue
			}
			rest := method[len(verb):]
			// "Move" and "MoveSerial" are writes; "Movements" is not.
			if rest == "" || (rest[0] >= 'A' && rest[0] <= 'Z') {
				writes = true
				break
			}
		}
		if !writes {
			continue
		}

		if strings.HasSuffix(permission, ".view") {
			t.Errorf("%s changes something but is gated on %q — a read-only user could do it",
				name, permission)
		}
	}
}
