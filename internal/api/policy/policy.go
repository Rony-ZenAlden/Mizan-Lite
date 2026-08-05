// Package policy declares what each binding method requires of its caller.
//
// §14.3 asks for a compile-time-adjacent guarantee that nothing ships unprotected by accident.
// The mechanism is two halves:
//
//   - Every exported binding method declares a Policy, and startup REJECTS any that does not
//     (internal/api/bindings, Set.ValidatePolicies).
//   - The guard that consults the policy is also the only way a method reaches the object
//     graph, so a method that skips it is non-functional rather than unprotected
//     (Step 1.5, D1).
//
// Together those make the guarantee structural rather than declarative: forgetting to check a
// permission is not a silent hole, it is a method that cannot do anything.
package policy

import "github.com/mizan-erp/mizan/internal/platform/auth"

// Policy is what a binding method requires.
type Policy struct {
	// Permission is the code the caller must hold. Empty only when Public.
	Permission string
	// Scope is the level the permission is checked at. Global unless stated.
	Scope auth.ScopeKind
	// Public marks a method reachable without a session.
	Public bool
}

// Requires builds a policy demanding a permission at global scope.
func Requires(permission string) Policy {
	return Policy{Permission: permission, Scope: auth.ScopeGlobal}
}

// InScope narrows a policy to a branch or warehouse.
//
// The scope's IDENTIFIER comes from the session at call time — a policy says "in the branch
// you are acting in", not "in branch X", because which branch that is depends on who is asking.
func (p Policy) InScope(kind auth.ScopeKind) Policy {
	p.Scope = kind
	return p
}

// Public marks a method reachable without a session.
//
// A separate constructor rather than the zero value, deliberately: an accidentally-empty map
// entry must FAIL the startup check, not silently open the surface. `Policy{}` is invalid, and
// ValidatePolicies rejects it.
func Public() Policy {
	return Policy{Public: true}
}

// IsValid reports whether a policy says anything at all.
//
// A policy that is neither public nor permissioned is the empty-value hole: it would let a
// method pass the coverage check while requiring nothing.
func (p Policy) IsValid() bool {
	return p.Public || p.Permission != ""
}
