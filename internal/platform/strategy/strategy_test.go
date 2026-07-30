package strategy_test

import (
	"sync"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/platform/strategy"
)

// A stand-in for a real extension-point contract (a costing strategy, a rate provider).
type coster interface{ Name() string }

type namedCoster struct{ name string }

func (c namedCoster) Name() string { return c.name }

func TestRegisterAndResolve(t *testing.T) {
	r := strategy.New[coster](strategy.PointCosting)
	if err := r.Register("wac", namedCoster{"weighted average"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, err := r.Resolve("wac")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Name() != "weighted average" {
		t.Errorf("resolved %q", got.Name())
	}
}

func TestDuplicateRegistrationIsAnError(t *testing.T) {
	// Two modules claiming "fifo" would otherwise resolve by package init order, so the
	// winner could differ between builds.
	r := strategy.New[coster](strategy.PointCosting)
	if err := r.Register("fifo", namedCoster{"first"}); err != nil {
		t.Fatal(err)
	}
	err := r.Register("fifo", namedCoster{"second"})
	if errs.CodeOf(err) != strategy.CodeDuplicate {
		t.Fatalf("code = %q, want %q", errs.CodeOf(err), strategy.CodeDuplicate)
	}
	// The first registration must survive; a failed Register must not corrupt the table.
	got, resolveErr := r.Resolve("fifo")
	if resolveErr != nil {
		t.Fatal(resolveErr)
	}
	if got.Name() != "first" {
		t.Errorf("duplicate registration overwrote the original: %q", got.Name())
	}
}

func TestEmptyKeyIsRejected(t *testing.T) {
	r := strategy.New[coster](strategy.PointCosting)
	if errs.CodeOf(r.Register("", namedCoster{"x"})) != strategy.CodeInvalid {
		t.Error("an empty strategy key was accepted")
	}
}

func TestResolveUnknownReturnsTypedNotFound(t *testing.T) {
	// Data selects behaviour and data can be wrong: a payment_methods row naming an
	// uninstalled provider must produce a clear error, not a nil interface that panics
	// three frames later in front of a customer.
	r := strategy.New[coster](strategy.PointPaymentProvider)
	_, err := r.Resolve("stripe")
	if err == nil {
		t.Fatal("resolving an unregistered key returned no error")
	}
	if errs.CodeOf(err) != strategy.CodeUnknown {
		t.Errorf("code = %q, want %q", errs.CodeOf(err), strategy.CodeUnknown)
	}
	if !errs.IsCategory(err, errs.CategoryNotFound) {
		t.Errorf("category = %v, want NotFound", errs.CategoryOf(err))
	}
	e, _ := errs.AsError(err)
	if e.Params["key"] != "stripe" || e.Params["point"] != strategy.PointPaymentProvider {
		t.Errorf("error params = %v; must name both the point and the key", e.Params)
	}
}

func TestKeysAreSorted(t *testing.T) {
	// These drive admin dropdowns; map order would reshuffle the list every launch.
	r := strategy.New[coster](strategy.PointReport)
	for _, k := range []string{"zulu", "alpha", "mike"} {
		if err := r.Register(k, namedCoster{k}); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"alpha", "mike", "zulu"}
	got := r.Keys()
	if len(got) != len(want) {
		t.Fatalf("Keys = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Keys = %v, want %v", got, want)
		}
	}
}

func TestHasDoesNotConstruct(t *testing.T) {
	r := strategy.New[coster](strategy.PointCosting)
	if r.Has("wac") {
		t.Error("Has reported an unregistered key")
	}
	if err := r.Register("wac", namedCoster{"x"}); err != nil {
		t.Fatal(err)
	}
	if !r.Has("wac") {
		t.Error("Has missed a registered key")
	}
	if r.Len() != 1 {
		t.Errorf("Len = %d, want 1", r.Len())
	}
}

func TestConcurrentRegisterAndResolve(t *testing.T) {
	r := strategy.New[coster](strategy.PointExportFormat)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := string(rune('a' + n))
			_ = r.Register(key, namedCoster{key})
			_, _ = r.Resolve(key)
			_ = r.Keys()
			_ = r.Has(key)
		}(i)
	}
	wg.Wait()
	if r.Len() != 16 {
		t.Errorf("Len = %d, want 16", r.Len())
	}
}

func TestPointsAreDistinctAndSorted(t *testing.T) {
	points := strategy.Points()
	if len(points) != 9 {
		t.Fatalf("got %d extension points, want the 9 named in §CFG.4: %v", len(points), points)
	}
	seen := map[string]bool{}
	for i, p := range points {
		if seen[p] {
			t.Errorf("duplicate extension point %q", p)
		}
		seen[p] = true
		if i > 0 && points[i-1] > p {
			t.Errorf("Points() is not sorted: %v", points)
		}
	}
}
