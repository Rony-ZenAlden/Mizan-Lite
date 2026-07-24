package quantity_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/quantity"
	"github.com/mizan-erp/mizan/internal/kernel/round"
	"pgregory.net/rapid"
)

const safe = int64(1) << 40

// Converting to a finer unit and back to the original returns the starting value
// exactly, because the finer unit loses no precision. (kg → g → kg.)
func TestConvertToFinerRoundTrips(t *testing.T) {
	g := mustUnit(t, "G", "weight", quantity.FactorScale, true)
	kg := mustUnit(t, "KG", "weight", 1000*quantity.FactorScale, true)
	rapid.Check(t, func(rt *rapid.T) {
		micro := rapid.Int64Range(-safe, safe).Draw(rt, "micro")
		start, err := quantity.FromMicro(kg, micro)
		if err != nil {
			rt.Fatal(err)
		}
		fine, err := start.ConvertTo(g, round.HalfAwayFromZero)
		if err != nil {
			rt.Fatal(err)
		}
		back, err := fine.ConvertTo(kg, round.HalfAwayFromZero)
		if err != nil {
			rt.Fatal(err)
		}
		if !back.Equal(start) {
			rt.Fatalf("kg→g→kg drift: %s → %s → %s", start, fine, back)
		}
	})
}

// Add is commutative, and subtracting restores the original, in the same unit.
func TestQuantityArithmeticLaws(t *testing.T) {
	kg := mustUnit(t, "KG", "weight", 1000*quantity.FactorScale, true)
	rapid.Check(t, func(rt *rapid.T) {
		a, _ := quantity.FromMicro(kg, rapid.Int64Range(-safe, safe).Draw(rt, "a"))
		b, _ := quantity.FromMicro(kg, rapid.Int64Range(-safe, safe).Draw(rt, "b"))

		ab, _ := a.Add(b)
		ba, _ := b.Add(a)
		if !ab.Equal(ba) {
			rt.Fatalf("Add not commutative: %s vs %s", ab, ba)
		}
		back, _ := ab.Sub(b)
		if !back.Equal(a) {
			rt.Fatalf("(a+b)-b = %s, want %s", back, a)
		}
	})
}
