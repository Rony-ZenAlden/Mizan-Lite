package modules

import (
	"sort"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// Stable error codes. They double as i18n keys and are part of the public contract.
const (
	CodeDependencyCycle   = "modules.dependency_cycle"
	CodeUnknownDependency = "modules.unknown_dependency"
	CodeDuplicateModule   = "modules.duplicate_module"
	CodeDuplicateKey      = "modules.duplicate_key"
)

// Order returns the modules in construction order: every module after the ones it depends on.
//
// All failures here are FATAL at startup, per the Step 0.10 rule (D3): a dependency cycle or
// a duplicate key can only reach a customer through a bad build, so the developer must see it
// on their own machine. Contrast with data leftovers — an unknown stored setting key, an
// orphan job row — which are reported and ignored because refusing to start would keep a shop
// closed over something inert.
func Order(mods []Module) ([]Module, error) {
	byName := make(map[string]Module, len(mods))
	for _, m := range mods {
		name := m.Name()
		if strings.TrimSpace(name) == "" {
			return nil, errs.Validation(CodeDuplicateModule, "a module has no name")
		}
		if _, dup := byName[name]; dup {
			return nil, errs.Validation(CodeDuplicateModule,
				"two modules share a name").WithParam("module", name)
		}
		byName[name] = m
	}

	// Sorted, so construction order is deterministic across runs for modules that have no
	// dependency relationship. A graph that reorders itself between launches would make one
	// startup bug reproducible only sometimes.
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)

	const (
		unvisited = 0
		visiting  = 1
		done      = 2
	)
	state := make(map[string]int, len(names))
	var ordered []Module
	var stack []string

	var visit func(name string) error
	visit = func(name string) error {
		switch state[name] {
		case done:
			return nil
		case visiting:
			// The cycle, named in the order it was walked, so the message points at the
			// actual edges rather than just asserting one exists.
			cycle := append(append([]string{}, cycleFrom(stack, name)...), name)
			return errs.Validation(CodeDependencyCycle,
				"module dependencies form a cycle; break it with an event, never a shared "+
					"mutable singleton").
				WithParam("cycle", strings.Join(cycle, " → "))
		}

		mod, ok := byName[name]
		if !ok {
			return errs.Validation(CodeUnknownDependency,
				"a module depends on one that is not registered").WithParam("module", name)
		}

		state[name] = visiting
		stack = append(stack, name)

		deps := append([]string{}, mod.DependsOn()...)
		sort.Strings(deps)
		for _, dep := range deps {
			if dep == name {
				return errs.Validation(CodeDependencyCycle,
					"module "+name+" depends on itself; break it with an event, never a "+
						"shared mutable singleton").WithParam("module", name)
			}
			if _, known := byName[dep]; !known {
				return errs.Validation(CodeUnknownDependency,
					"a module depends on one that is not registered").
					WithParam("module", name).WithParam("depends_on", dep)
			}
			if err := visit(dep); err != nil {
				return err
			}
		}

		stack = stack[:len(stack)-1]
		state[name] = done
		ordered = append(ordered, mod)
		return nil
	}

	for _, name := range names {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}

// cycleFrom returns the portion of the walk stack starting at name.
func cycleFrom(stack []string, name string) []string {
	for i, s := range stack {
		if s == name {
			return stack[i:]
		}
	}
	return stack
}

// Validate checks the module set for collisions that no single module can detect alone.
//
// The config registry already rejects a duplicate setting key within a process, but its error
// says only "declared twice". This reports WHICH TWO MODULES collided, which is the form
// someone can act on.
func Validate(mods []Module) error {
	settingOwner := map[string]string{}
	flagOwner := map[string]string{}
	var problems []error

	for _, m := range mods {
		for _, def := range m.Settings() {
			if owner, dup := settingOwner[def.Key]; dup {
				// The detail goes in the MESSAGE, not only in Params: errs.Error() renders
				// the message, so a param-only detail would be invisible in the startup log
				// that a developer is actually reading. This error never reaches a customer
				// — it is a code defect that fails the build's first boot (D3).
				problems = append(problems, errs.Validation(CodeDuplicateKey,
					"setting key "+def.Key+" is declared by both "+owner+" and "+m.Name()).
					WithParam("key", def.Key).
					WithParam("modules", owner+" and "+m.Name()))
				continue
			}
			settingOwner[def.Key] = m.Name()
		}
		for _, flag := range m.FeatureFlags() {
			if owner, dup := flagOwner[flag.Key]; dup {
				problems = append(problems, errs.Validation(CodeDuplicateKey,
					"feature flag "+flag.Key+" is declared by both "+owner+" and "+m.Name()).
					WithParam("key", flag.Key).
					WithParam("modules", owner+" and "+m.Name()))
				continue
			}
			flagOwner[flag.Key] = m.Name()
		}
	}

	if len(problems) == 0 {
		return nil
	}
	// Reported together: fixing one collision only to be shown the next on the following run
	// is a poor way to spend a developer's afternoon.
	messages := make([]string, 0, len(problems))
	for _, p := range problems {
		messages = append(messages, p.Error())
	}
	return errs.Validation(CodeDuplicateKey,
		"the module set has key collisions: "+strings.Join(messages, "; "))
}
