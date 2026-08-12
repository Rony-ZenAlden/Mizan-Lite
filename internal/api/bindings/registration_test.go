package bindings_test

import (
	"reflect"
	"testing"
)

// TestEveryFacadeIsExportedAndAttached walks the Set by reflection.
//
// # Why this test exists
//
// Adding a façade takes FOUR edits in three places: a field on Set, a construction line, an entry
// in All(), and a line in Attach. Nothing connects them, so missing one of the last two is silent:
//
//   - missing from All() — Wails never generates its JavaScript, and the screen calling it gets
//     "undefined is not a function" at runtime with nothing in the Go logs.
//   - missing from Attach — every method returns app.not_ready forever, which looks exactly like
//     a slow start rather than a wiring bug, so the first instinct is to wait longer.
//
// Both were live in this package: Sales sat on Set and in neither list, the whole suite passed,
// and the façade could not have served a single call.
//
// The lesson from drill 39 applies directly. A test that listed the façades by hand would be a
// fourth place to forget, and it would pass while the application was broken. Reflection asks the
// STRUCT, which is the one place a new façade cannot be omitted from.
func TestEveryFacadeIsExportedAndAttached(t *testing.T) {
	set, _ := signedIn(t)

	exported := make(map[string]reflect.Value)
	value := reflect.ValueOf(set).Elem()
	for i := range value.NumField() {
		field := value.Type().Field(i)
		if !field.IsExported() {
			continue
		}
		// Every façade is a pointer to a struct embedding graph. The unexported members of Set
		// (session, remember) are skipped by the export check above.
		if field.Type.Kind() != reflect.Ptr || field.Type.Elem().Kind() != reflect.Struct {
			continue
		}
		exported[field.Name] = value.Field(i)
	}
	if len(exported) < 10 {
		t.Fatalf("found only %d façades, so the walk is not finding them", len(exported))
	}

	// ── every façade reaches the frontend ────────────────────────────────────────
	listed := make(map[uintptr]bool)
	for _, binding := range set.All() {
		listed[reflect.ValueOf(binding).Pointer()] = true
	}
	for name, facade := range exported {
		if !listed[facade.Pointer()] {
			t.Errorf("Set.%s is missing from All() — Wails will not generate its bindings, "+
				"and every call from the screen fails at runtime", name)
		}
	}

	// ── every façade has the graph ───────────────────────────────────────────────
	//
	// Asked through the public surface rather than by reading the private field: an attached
	// façade serves, and an unattached one answers app.not_ready. Boot is the documented
	// exception — it reports startup, so it must answer BEFORE the graph exists.
	for name, facade := range exported {
		if name == "Boot" {
			continue
		}
		graphField := facade.Elem().FieldByName("graph")
		if !graphField.IsValid() {
			t.Errorf("Set.%s does not embed graph, so it cannot be guarded", name)
			continue
		}
		if graphField.FieldByName("app").IsNil() {
			t.Errorf("Set.%s was never attached — every one of its methods returns "+
				"app.not_ready, which reads as a slow start rather than a wiring bug", name)
		}
	}
}
