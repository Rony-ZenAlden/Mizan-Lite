package modules_test

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/jobs"
	"github.com/mizan-erp/mizan/internal/platform/metadata"
	"github.com/mizan-erp/mizan/internal/platform/modules"
	"github.com/mizan-erp/mizan/internal/platform/outbox"
)

// fake is a minimal Module for exercising the registry.
type fake struct {
	name     string
	deps     []string
	settings []config.Definition
	flags    []config.FlagDef
}

func (f fake) Name() string                                       { return f.name }
func (f fake) DependsOn() []string                                { return f.deps }
func (f fake) Migrations() fs.FS                                  { return fstest.MapFS{} }
func (f fake) Settings() []config.Definition                      { return f.settings }
func (f fake) FeatureFlags() []config.FlagDef                     { return f.flags }
func (f fake) Metadata() []metadata.SeedSpec                      { return nil }
func (f fake) Jobs() []jobs.Def                                   { return nil }
func (f fake) Bindings() any                                      { return nil }
func (f fake) Subscribe(*eventbus.Bus, *outbox.Subscribers) error { return nil }

func names(mods []modules.Module) []string {
	out := make([]string, len(mods))
	for i, m := range mods {
		out[i] = m.Name()
	}
	return out
}

func assertCode(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %q, got nil", want)
	}
	if got := errs.CodeOf(err); got != want {
		t.Fatalf("code = %q, want %q (err: %v)", got, want, err)
	}
}

func TestOrderPutsDependenciesFirst(t *testing.T) {
	mods := []modules.Module{
		fake{name: "sales", deps: []string{"catalog", "currency"}},
		fake{name: "catalog", deps: []string{"currency"}},
		fake{name: "currency"},
	}
	got, err := modules.Order(mods)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	position := map[string]int{}
	for i, name := range names(got) {
		position[name] = i
	}
	if position["currency"] > position["catalog"] || position["catalog"] > position["sales"] {
		t.Errorf("order = %v; every module must follow the ones it depends on", names(got))
	}
}

func TestOrderIsDeterministic(t *testing.T) {
	// Independent modules must come out in the same order every run. A graph that reshuffles
	// itself between launches makes a startup bug reproducible only sometimes.
	mods := []modules.Module{
		fake{name: "zulu"}, fake{name: "alpha"}, fake{name: "mike"},
	}
	first, err := modules.Order(mods)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		again, orderErr := modules.Order(mods)
		if orderErr != nil {
			t.Fatal(orderErr)
		}
		if strings.Join(names(again), ",") != strings.Join(names(first), ",") {
			t.Fatalf("order changed between runs: %v then %v", names(first), names(again))
		}
	}
}

func TestOrderDetectsCycles(t *testing.T) {
	cases := map[string][]modules.Module{
		"two-module cycle": {
			fake{name: "a", deps: []string{"b"}},
			fake{name: "b", deps: []string{"a"}},
		},
		"three-module cycle": {
			fake{name: "a", deps: []string{"b"}},
			fake{name: "b", deps: []string{"c"}},
			fake{name: "c", deps: []string{"a"}},
		},
		"self dependency": {
			fake{name: "a", deps: []string{"a"}},
		},
	}
	for name, mods := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := modules.Order(mods)
			assertCode(t, err, modules.CodeDependencyCycle)
			// §6: the fix for a cycle is an event, never a shared mutable singleton — the
			// error should say so, because whoever hits this needs the direction.
			if !strings.Contains(err.Error(), "event") {
				t.Errorf("cycle error does not suggest the fix: %v", err)
			}
		})
	}
}

func TestOrderRejectsUnknownDependency(t *testing.T) {
	_, err := modules.Order([]modules.Module{
		fake{name: "sales", deps: []string{"inventory"}},
	})
	assertCode(t, err, modules.CodeUnknownDependency)
}

func TestOrderRejectsDuplicateModuleNames(t *testing.T) {
	_, err := modules.Order([]modules.Module{fake{name: "a"}, fake{name: "a"}})
	assertCode(t, err, modules.CodeDuplicateModule)
}

func TestOrderAcceptsAnEmptySet(t *testing.T) {
	// An install with no business modules is a legitimate state, not a crash.
	got, err := modules.Order(nil)
	if err != nil || len(got) != 0 {
		t.Errorf("Order(nil) = %v, %v; want an empty set and no error", names(got), err)
	}
}

func TestValidateRejectsDuplicateSettingKeysAcrossModules(t *testing.T) {
	// The config registry already rejects a duplicate within a process, but its message says
	// only "declared twice". This must name WHICH TWO MODULES collided.
	def := config.Definition{Def: config.Def{Key: "shared.key"}}
	err := modules.Validate([]modules.Module{
		fake{name: "alpha", settings: []config.Definition{def}},
		fake{name: "beta", settings: []config.Definition{def}},
	})
	assertCode(t, err, modules.CodeDuplicateKey)
	for _, want := range []string{"alpha", "beta", "shared.key"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}
}

func TestValidateRejectsDuplicateFlagKeys(t *testing.T) {
	flag := config.FlagDef{Key: "shared.flag"}
	err := modules.Validate([]modules.Module{
		fake{name: "alpha", flags: []config.FlagDef{flag}},
		fake{name: "beta", flags: []config.FlagDef{flag}},
	})
	assertCode(t, err, modules.CodeDuplicateKey)
}

func TestValidateReportsEveryCollisionAtOnce(t *testing.T) {
	// Fixing one collision only to be shown the next on the following run is a poor way to
	// spend an afternoon.
	err := modules.Validate([]modules.Module{
		fake{name: "alpha", settings: []config.Definition{
			{Def: config.Def{Key: "k1"}}, {Def: config.Def{Key: "k2"}},
		}},
		fake{name: "beta", settings: []config.Definition{
			{Def: config.Def{Key: "k1"}}, {Def: config.Def{Key: "k2"}},
		}},
	})
	if err == nil {
		t.Fatal("expected collisions")
	}
	if !strings.Contains(err.Error(), "k1") || !strings.Contains(err.Error(), "k2") {
		t.Errorf("only some collisions were reported: %v", err)
	}
}

func TestValidateAcceptsADisjointSet(t *testing.T) {
	err := modules.Validate([]modules.Module{
		fake{name: "alpha", settings: []config.Definition{{Def: config.Def{Key: "a.k"}}}},
		fake{name: "beta", settings: []config.Definition{{Def: config.Def{Key: "b.k"}}}},
	})
	if err != nil {
		t.Errorf("Validate on a disjoint set: %v", err)
	}
}
