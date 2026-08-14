package bootstrap_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/bootstrap"
)

// TestEveryModuleTheApplicationRunsAlsoDeclaresItself
//
// # Why this test exists
//
// There are TWO module lists. `bootstrap.Start` builds the real graph with real services;
// `DeclarationModules` builds the same modules with nil services, so permissions, settings and
// migrations can be enumerated without a database.
//
// Nothing connects them. A module added to one and not the other is silent in a specific way:
// every binding that requires one of its permissions becomes permanently unreachable, because
// the permission cannot be granted to anybody. Not a security hole — an outage that looks like a
// configuration problem.
//
// Purchasing was in exactly that state: wired into `Start`, absent from `DeclarationModules`, and
// the failure surfaced as "requires permission purchasing.order.view, which no module declares".
//
// This is drill 44's lesson in a second place. A hand-written list is a place to forget, and the
// answer is to compare it with the thing that cannot be forgotten — the running application.
func TestEveryModuleTheApplicationRunsAlsoDeclaresItself(t *testing.T) {
	app := boot(t)

	declared := make(map[string]bool)
	for _, module := range bootstrap.DeclarationModules() {
		declared[module.Name()] = true
	}

	if len(app.Modules) == 0 {
		t.Fatal("the application booted with no modules, so this test proves nothing")
	}
	for _, module := range app.Modules {
		if !declared[module.Name()] {
			t.Errorf("module %q runs in the application but is missing from "+
				"DeclarationModules — every binding requiring one of its permissions is "+
				"permanently unreachable", module.Name())
		}
	}

	// And the reverse. A module declared but never started would let a permission be granted
	// that nothing honours, which reads to an administrator as a permission that does not work.
	running := make(map[string]bool, len(app.Modules))
	for _, module := range app.Modules {
		running[module.Name()] = true
	}
	for name := range declared {
		if !running[name] {
			t.Errorf("module %q declares permissions but never runs", name)
		}
	}
}
