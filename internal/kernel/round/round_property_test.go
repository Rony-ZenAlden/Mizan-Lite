package round

import (
	"math/big"
	"testing"

	"pgregory.net/rapid"
)

// The rounding engine must, for every input and mode, return an integer that is
// either the floor or the ceiling of the exact quotient — never further away, and
// never a panic. This single property backstops the whole engine against surprises
// the truth table might not enumerate.
func TestDivIsAlwaysFloorOrCeil(t *testing.T) {
	modes := []RoundingMode{
		HalfAwayFromZero, HalfToEven, HalfUp, HalfDown, Ceiling, Floor, TowardZero,
	}
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.Int64().Draw(rt, "num")
		d := rapid.Int64Range(1, 1<<62).Draw(rt, "den") // den > 0 by contract
		num := big.NewInt(n)
		den := big.NewInt(d)

		// Exact floor and ceil of num/den via big.Int (Euclidean-ish for our positive den).
		floor := new(big.Int)
		mod := new(big.Int)
		floor.DivMod(num, den, mod) // Go's DivMod gives Euclidean: 0 <= mod < den
		ceil := new(big.Int).Set(floor)
		if mod.Sign() != 0 {
			ceil.Add(ceil, big.NewInt(1))
		}

		for _, m := range modes {
			got := Div(new(big.Int).Set(num), new(big.Int).Set(den), m)
			if got.Cmp(floor) != 0 && got.Cmp(ceil) != 0 {
				rt.Fatalf("Div(%d/%d, %s) = %s, not in {floor %s, ceil %s}",
					n, d, m, got, floor, ceil)
			}
		}
	})
}

// Rounding an exact multiple of den returns the exact quotient for every mode.
func TestDivExactHasNoRounding(t *testing.T) {
	modes := []RoundingMode{
		HalfAwayFromZero, HalfToEven, HalfUp, HalfDown, Ceiling, Floor, TowardZero,
	}
	rapid.Check(t, func(rt *rapid.T) {
		q := rapid.Int64Range(-1<<40, 1<<40).Draw(rt, "q")
		d := rapid.Int64Range(1, 1<<20).Draw(rt, "den")
		num := new(big.Int).Mul(big.NewInt(q), big.NewInt(d))
		for _, m := range modes {
			got := Div(new(big.Int).Set(num), big.NewInt(d), m).Int64()
			if got != q {
				rt.Fatalf("Div(exact %d×%d, %s) = %d, want %d", q, d, m, got, q)
			}
		}
	})
}
