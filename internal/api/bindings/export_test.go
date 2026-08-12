package bindings

import "reflect"

// PublicMethodsForTest exposes the unauthenticated surface so a test can pin it.
//
// In an _test.go file so it is not part of the shipped API: making the public surface
// introspectable at runtime would be a small gift to anyone probing it.
func PublicMethodsForTest(s *Set) map[string]bool {
	out := map[string]bool{}
	for _, binding := range s.All() {
		holder, ok := binding.(policyHolder)
		if !ok {
			continue
		}
		name := reflect.TypeOf(binding).Elem().Name()
		for method, p := range holder.declaredPolicies() {
			if p.Public {
				out[name+"."+method] = true
			}
		}
	}
	return out
}

// DeclaredPoliciesForTest exposes every method's declared policy, keyed "Binding.Method".
//
// Also test-only, for the reason above. The coverage check proves each method HAS a policy; this
// lets a test ask whether it has the RIGHT one.
func DeclaredPoliciesForTest(s *Set) map[string]string {
	out := map[string]string{}
	for _, binding := range s.All() {
		holder, ok := binding.(policyHolder)
		if !ok {
			continue
		}
		name := reflect.TypeOf(binding).Elem().Name()
		for method, p := range holder.declaredPolicies() {
			out[name+"."+method] = p.Permission
		}
	}
	return out
}
