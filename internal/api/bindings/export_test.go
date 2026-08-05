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
