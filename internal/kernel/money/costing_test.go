package money_test

import (
	"errors"
	"math/big"
	"testing"

	"pgregory.net/rapid"

	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/quantity"
	"github.com/mizan-erp/mizan/internal/kernel/round"
)

const m = 1_000_000 // one whole unit, or one whole currency unit, at the 10⁻⁶ scale

func TestWeightedAverageUnit(t *testing.T) {
	cases := []struct {
		name                         string
		onHand, avg, qty, cost, want int64
	}{
		{"first receipt takes its cost", 0, 0, 10 * m, 6 * m, 6 * m},
		{"equal quantities average evenly", 10 * m, 6 * m, 10 * m, 8 * m, 7 * m},
		{"weighted by quantity", 30 * m, 6 * m, 10 * m, 10 * m, 7 * m},
		{"fractional quantities", 1_500_000, 4 * m, 500_000, 8 * m, 5 * m},
		// Mizan's own trap: 10 received at 120 into −5 carrying a phantom average of 999. The general
		// formula gives −759; the receipt's cost is the only meaningful answer.
		{"into negative stock takes the receipt cost", -5 * m, 999 * m, 10 * m, 120 * m, 120 * m},
		{"rounds once, half up", 1 * m, 1, 2 * m, 2, 2}, // (1 + 4) / 3 = 1.666… → 2
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := money.WeightedAverageUnit(tc.onHand, tc.avg, tc.qty, tc.cost, round.HalfUp)
			if err != nil || got != tc.want {
				t.Fatalf("got %d, %v; want %d", got, err, tc.want)
			}
		})
	}
}

func TestWeightedAverageUnitRefusals(t *testing.T) {
	if _, err := money.WeightedAverageUnit(m, m, 0, m, round.HalfUp); !errors.Is(err, money.ErrNonPositiveQuantity) {
		t.Errorf("zero receipt: %v", err)
	}
	if _, err := money.WeightedAverageUnit(m, m, m, -1, round.HalfUp); !errors.Is(err, money.ErrNegativeCost) {
		t.Errorf("negative cost: %v", err)
	}
}

func TestWeightedAverageLiesBetweenOldAndIncoming(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		onHand := rapid.Int64Range(1, 1_000_000*m).Draw(rt, "onHand")
		avg := rapid.Int64Range(0, 100_000*m).Draw(rt, "avg")
		qty := rapid.Int64Range(1, 1_000_000*m).Draw(rt, "qty")
		cost := rapid.Int64Range(0, 100_000*m).Draw(rt, "cost")
		got, err := money.WeightedAverageUnit(onHand, avg, qty, cost, round.HalfUp)
		if err != nil {
			rt.Fatal(err)
		}
		lo, hi := min(avg, cost), max(avg, cost)
		if got < lo || got > hi {
			rt.Fatalf("average %d outside [%d, %d]", got, lo, hi)
		}
	})
}

func TestSplitUnitCost(t *testing.T) {
	// One tin at 95.000000 opened into 16 litres: 5.937500 a litre, exactly.
	if got, err := money.SplitUnitCost(95*m, 1*m, 16*m, round.HalfUp); err != nil || got != 5_937_500 {
		t.Fatalf("got %d, %v", got, err)
	}
	// Into 15 litres: 6.333333… rounds to 6.333333.
	if got, _ := money.SplitUnitCost(95*m, 1*m, 15*m, round.HalfUp); got != 6_333_333 {
		t.Fatalf("got %d", got)
	}
	if _, err := money.SplitUnitCost(95*m, 0, 15*m, round.HalfUp); !errors.Is(err, money.ErrNonPositiveQuantity) {
		t.Fatal(err)
	}
}

// TestSplitUnitCostConservesValueWithinOneMinorUnit holds the bound at every currency scale: what a package was
// worth and what its content is worth, each rounded to the currency's minor unit once, differ by at most one minor
// unit — while the content is under 2 × 10^(6−decimals) units.
//
// # Why the limit depends on the scale
//
// The split unit cost is exact to 0.5 × 10⁻⁶ of the major unit, so the exact values differ by less than
// content × 0.5 × 10⁻⁶. That stays under one minor unit (10^−decimals) only while content < 2 × 10^(6−decimals):
// 20,000 units in a two-decimal currency such as USD, but 2,000 in a three-decimal one. The first version of this
// test drew 20,000 units at three decimals and rapid found a two-fils difference on its first case — the bound as
// first written in the L2 design note held for USD and was stated as if it held everywhere.
func TestSplitUnitCostConservesValueWithinTheBound(t *testing.T) {
	for _, decimals := range []uint8{0, 2, 3} {
		t.Run("decimals "+string(rune('0'+decimals)), func(t *testing.T) {
			cur, err := money.NewCurrency("XXX", decimals, round.HalfUp)
			if err != nil {
				t.Fatal(err)
			}
			pcs := mustUnit(t, "piece", false)
			litre := mustUnit(t, "l", true)
			rapid.Check(t, func(rt *rapid.T) {
				limit := int64(2) // units of content, as 2 × 10^(6−decimals)
				for i := 0; i < 6-int(decimals); i++ {
					limit *= 10
				}
				packageCost := rapid.Int64Range(0, 10_000*m).Draw(rt, "packageCost")
				packages := rapid.Int64Range(1, 50).Draw(rt, "packages")
				contentEach := rapid.Int64Range(1, max(1, (limit-1)/packages)).Draw(rt, "contentEach")
				content := packages * contentEach * m
				packages *= m

				split, err := money.SplitUnitCost(packageCost, packages, content, round.HalfUp)
				if err != nil {
					rt.Fatal(err)
				}
				out, _ := money.LineExtension(money.UnitFromMicro(cur, packageCost), mustQty(rt, pcs, packages), round.HalfUp)
				in, _ := money.LineExtension(money.UnitFromMicro(cur, split), mustQty(rt, litre, content), round.HalfUp)
				if diff := out.Minor() - in.Minor(); diff < -1 || diff > 1 {
					rt.Fatalf("value out %d, value in %d: differ by %d minor units", out.Minor(), in.Minor(), diff)
				}
			})
		})
	}
}

func TestUnitCostFromTotalAtEveryScale(t *testing.T) {
	kgUnit := mustUnit(t, "kg", true)
	cases := []struct {
		name     string
		decimals uint8
		minor    int64
		qty      int64
		want     int64
	}{
		// 25 kg for 450,000 pounds: 18,000 a kilo.
		{"SYP, no minor unit", 0, 450_000, 25 * m, 18_000 * m},
		// 50 kg for $63.10 (6,310 cents): 1.262 a kilo — more decimals than the currency has, kept.
		{"USD, two decimals", 2, 6_310, 50 * m, 1_262_000},
		// 4 kg for 10.500 dinars (10,500 fils): 2.625 a kilo.
		{"three decimals", 3, 10_500, 4 * m, 2_625_000},
		// 3 kg for $10.00: 3.333333… rounds once.
		{"rounds once", 2, 1_000, 3 * m, 3_333_333},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cur, err := money.NewCurrency("XXX", tc.decimals, round.HalfUp)
			if err != nil {
				t.Fatal(err)
			}
			got, err := money.UnitCostFromTotal(money.FromMinor(cur, tc.minor), mustQtyT(t, kgUnit, tc.qty), round.HalfUp)
			if err != nil || got.Micro() != tc.want {
				t.Fatalf("got %d, %v; want %d", got.Micro(), err, tc.want)
			}
		})
	}
}

// TestUnitCostFromTotalRoundTripsWithinOneMinorUnit: a total turned into a unit cost and multiplied back by the
// same quantity returns the total, to within one minor unit, at 0, 2 and 3 decimals — the scale test Mizan's
// costing lacked for two phases.
func TestUnitCostFromTotalRoundTripsWithinOneMinorUnit(t *testing.T) {
	kgUnit := mustUnit(t, "kg", true)
	for _, decimals := range []uint8{0, 2, 3} {
		t.Run("decimals "+string(rune('0'+decimals)), func(t *testing.T) {
			cur, _ := money.NewCurrency("XXX", decimals, round.HalfUp)
			rapid.Check(t, func(rt *rapid.T) {
				minor := rapid.Int64Range(0, 1_000_000_000).Draw(rt, "minor")
				qty := rapid.Int64Range(1_000, 10_000*m).Draw(rt, "qty")
				unit, err := money.UnitCostFromTotal(money.FromMinor(cur, minor), mustQty(rt, kgUnit, qty), round.HalfUp)
				if err != nil {
					rt.Fatal(err)
				}
				back, _ := money.LineExtension(unit, mustQty(rt, kgUnit, qty), round.HalfUp)
				// The bound: the unit cost is exact to 10⁻⁶ of the major unit, so the rebuilt total is off by at
				// most qty × 0.5 × 10⁻⁶ major units, plus one minor unit of rounding.
				slack := new(big.Int).Mul(big.NewInt(qty), pow10(int(decimals)))
				slack.Div(slack, big.NewInt(2*m*m))
				if diff := abs(back.Minor() - minor); diff > slack.Int64()+1 {
					rt.Fatalf("total %d → unit %d → %d: off by %d (allowed %d)", minor, unit.Micro(), back.Minor(), diff, slack.Int64()+1)
				}
			})
		})
	}
}

func TestDivideByRateRoundsOnce(t *testing.T) {
	syp, _ := money.NewCurrency("SYP", 0, round.HalfUp)
	usd, _ := money.NewCurrency("USD", 2, round.HalfUp)
	rate, err := money.ParseRate("15000")
	if err != nil {
		t.Fatal(err)
	}
	// 18,000 pounds a kilo at 15,000 per dollar: $1.200000.
	got, err := money.UnitFromMicro(syp, 18_000*m).DivideByRate(rate, usd, round.HalfUp)
	if err != nil || got.Micro() != 1_200_000 || got.Currency().Code() != "USD" {
		t.Fatalf("got %d %s, %v", got.Micro(), got.Currency().Code(), err)
	}
	// 10,000 pounds at 15,000: 0.666666… rounds ONCE to 0.666667. Inverting the rate first (0.000067 at 10⁻⁹
	// is 66,667 nano) and multiplying would give 0.666670 — the double rounding this function exists to avoid.
	got, _ = money.UnitFromMicro(syp, 10_000*m).DivideByRate(rate, usd, round.HalfUp)
	if got.Micro() != 666_667 {
		t.Fatalf("got %d, want 666667", got.Micro())
	}
	if _, err := money.UnitFromMicro(syp, m).DivideByRate(money.RateFromNano(0), usd, round.HalfUp); !errors.Is(err, money.ErrInvalidRate) {
		t.Fatalf("zero rate: %v", err)
	}
}

func mustUnit(t testing.TB, code string, fractional bool) quantity.Unit {
	t.Helper()
	u, err := quantity.NewUnit(code, "test", 1_000_000_000, fractional)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func mustQty(rt *rapid.T, u quantity.Unit, micro int64) quantity.Quantity {
	q, err := quantity.FromMicro(u, micro)
	if err != nil {
		rt.Fatal(err)
	}
	return q
}

func mustQtyT(t *testing.T, u quantity.Unit, micro int64) quantity.Quantity {
	t.Helper()
	q, err := quantity.FromMicro(u, micro)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func pow10(n int) *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil) }

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
