package bindings

import (
	"reflect"
	"sort"
	"strings"

	"github.com/mizan-erp/mizan/internal/api/policy"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/platform/auth"
)

// CodePolicyCoverage is the startup failure for an unprotected or malformed binding surface.
const CodePolicyCoverage = "app.policy_coverage"

// policyHolder is implemented by every façade that declares policies.
type policyHolder interface {
	declaredPolicies() map[string]policy.Policy
}

// ValidatePolicies asserts that the binding surface is fully declared.
//
// §14.3 wants "a use case without a declared policy fails to start the application". The guard
// already makes a method that skips its policy NON-FUNCTIONAL (guard.go, D1); this catches the
// three ways the declaration side can rot:
//
//  1. An exported method with no policy — the unprotected-by-accident case.
//  2. A policy for a method that no longer exists — a rename left it behind, and the stale
//     entry makes the surface look more protected than it is.
//  3. A policy naming a permission no module declares — it could never be granted, so the
//     method would be permanently unreachable. That is a silent outage, not a security hole,
//     and it is exactly as worth catching.
//
// Reflection is used ONCE, at boot, over a set fixed at compile time — not at call time, and
// not to discover behaviour.
func (s *Set) ValidatePolicies(declaredPermissions map[string]bool) error {
	var problems []string

	for _, binding := range s.All() {
		holder, ok := binding.(policyHolder)
		if !ok {
			// Boot has no graph and no policies by design: it is the binding that reports
			// whether the graph exists, so it cannot require one.
			continue
		}
		name := reflect.TypeOf(binding).Elem().Name()
		policies := holder.declaredPolicies()

		methods := exportedMethods(binding)
		for _, method := range methods {
			p, declared := policies[method]
			switch {
			case !declared:
				problems = append(problems, name+"."+method+" has no declared policy")
			case !p.IsValid():
				problems = append(problems, name+"."+method+
					" declares an empty policy (neither Public nor a permission)")
			case !p.Public && len(declaredPermissions) > 0 && !declaredPermissions[p.Permission]:
				problems = append(problems, name+"."+method+
					" requires permission "+p.Permission+", which no module declares")
			case !p.Public && p.Scope == auth.ScopeWarehouse:
				// No warehouse-scoped permission exists until Phase 4, and Satisfies does not
				// implement branch⊃warehouse containment yet (1.4 D3). Silently treating it as
				// global would be the dangerous reading.
				problems = append(problems, name+"."+method+
					" asks for warehouse scope, which is not supported until Phase 4")
			}
		}

		declaredNames := make(map[string]bool, len(methods))
		for _, m := range methods {
			declaredNames[m] = true
		}
		for method := range policies {
			if !declaredNames[method] {
				problems = append(problems, name+" declares a policy for "+method+
					", which is not an exported method (renamed or removed?)")
			}
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		// The detail goes in the MESSAGE, not only in params: this is a startup error a
		// developer reads in a log, and errs.Error.Error() does not render params (0.10 §9.2).
		return errs.Internal(CodePolicyCoverage,
			"the binding surface is not fully protected:\n  "+strings.Join(problems, "\n  "))
	}
	return nil
}

// exportedMethods lists a binding's exported method names.
//
// Methods promoted from the embedded graph (attach, resolve, guard) are unexported and
// therefore invisible here, which is what makes the enumeration exactly "what JavaScript can
// call".
func exportedMethods(binding any) []string {
	t := reflect.TypeOf(binding)
	out := make([]string, 0, t.NumMethod())
	for i := 0; i < t.NumMethod(); i++ {
		m := t.Method(i)
		if m.IsExported() {
			out = append(out, m.Name)
		}
	}
	sort.Strings(out)
	return out
}
