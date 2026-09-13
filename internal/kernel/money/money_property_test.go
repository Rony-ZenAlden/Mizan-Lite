package money_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/round"
	"pgregory.net/rapid"
)

// A "safe" magnitude well inside int64 so property checks exercise logic, not the
// documented overflow edge (which the boundary tests cover explicitly).
const safe = int64(1) << 50

func propCurrency(t *testing.T) money.Currency {
	c, err := money.NewCurrency("USD", 2, round.HalfAwayFromZero)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Allocation ALWAYS conserves the whole: the parts sum exactly to the original,
// for any amount (either sign), any part count, and any non-negative weights with a
// positive total. This is the single most important money property.
func TestAllocateConservation(t *testing.T) {
	usd := propCurrency(t)
	rapid.Check(t, func(rt *rapid.T) {
		minor := rapid.Int64Range(-safe, safe).Draw(rt, "minor")
		n := rapid.IntRange(1, 12).Draw(rt, "parts")
		weights := make([]int64, n)
		var total int64
		for i := range weights {
			weights[i] = rapid.Int64Range(0, 1_000_000).Draw(rt, "w")
			total += weights[i]
		}
		if total == 0 {
			weights[0] = 1 // ensure a positive total
		}

		parts, err := money.FromMinor(usd, minor).Allocate(weights)
		if err != nil {
			rt.Fatalf("Allocate returned error: %v", err)
		}
		var sum int64
		for _, p := range parts {
			sum += p.Minor()
		}
		if sum != minor {
			rt.Fatalf("allocation of %d by %v summed to %d (conservation violated)", minor, weights, sum)
		}
	})
}

// Allocation parts never differ from an equal ideal share by more than one minor
// unit when weights are equal — the fairness property of largest-remainder.
func TestAllocateFairness(t *testing.T) {
	usd := propCurrency(t)
	rapid.Check(t, func(rt *rapid.T) {
		minor := rapid.Int64Range(0, safe).Draw(rt, "minor")
		n := rapid.IntRange(1, 12).Draw(rt, "parts")
		weights := make([]int64, n)
		for i := range weights {
			weights[i] = 1
		}
		parts, err := money.FromMinor(usd, minor).Allocate(weights)
		if err != nil {
			rt.Fatal(err)
		}
		lo, hi := parts[0].Minor(), parts[0].Minor()
		for _, p := range parts {
			if p.Minor() < lo {
				lo = p.Minor()
			}
			if p.Minor() > hi {
				hi = p.Minor()
			}
		}
		if hi-lo > 1 {
			rt.Fatalf("equal-weight parts differ by %d (>1): %v", hi-lo, parts)
		}
	})
}

// Add is commutative and associative; subtracting what you added restores the
// original; double negation is identity.
func TestArithmeticLaws(t *testing.T) {
	usd := propCurrency(t)
	rapid.Check(t, func(rt *rapid.T) {
		a := money.FromMinor(usd, rapid.Int64Range(-safe, safe).Draw(rt, "a"))
		b := money.FromMinor(usd, rapid.Int64Range(-safe, safe).Draw(rt, "b"))
		c := money.FromMinor(usd, rapid.Int64Range(-safe, safe).Draw(rt, "c"))

		ab, _ := a.Add(b)
		ba, _ := b.Add(a)
		if !ab.Equal(ba) {
			rt.Fatalf("Add not commutative: %s vs %s", ab, ba)
		}

		abc1, _ := ab.Add(c)
		bc, _ := b.Add(c)
		abc2, _ := a.Add(bc)
		if !abc1.Equal(abc2) {
			rt.Fatalf("Add not associative: %s vs %s", abc1, abc2)
		}

		back, _ := ab.Sub(b)
		if !back.Equal(a) {
			rt.Fatalf("(a+b)-b = %s, want %s", back, a)
		}

		if !a.Neg().Neg().Equal(a) {
			rt.Fatalf("double negation changed %s", a)
		}
	})
}

// Parse and String round-trip: formatting an amount and parsing it back is identity.
func TestParseStringRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dec := uint8(rapid.IntRange(0, 6).Draw(rt, "decimals"))
		cur, err := money.NewCurrency("TST", dec, round.HalfAwayFromZero)
		if err != nil {
			rt.Fatal(err)
		}
		minor := rapid.Int64Range(-safe, safe).Draw(rt, "minor")
		m := money.FromMinor(cur, minor)

		// String is "CODE amount"; parse the amount part back.
		amount := m.String()[len("TST "):]
		back, err := money.Parse(cur, amount)
		if err != nil {
			rt.Fatalf("Parse(%q): %v", amount, err)
		}
		if back.Minor() != minor {
			rt.Fatalf("round trip: %d → %q → %d", minor, amount, back.Minor())
		}
	})
}
