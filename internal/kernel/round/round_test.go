package round

import (
	"math/big"
	"testing"
)

// TestDivTruthTable exhaustively verifies every mode against the documented table
// (mode.go) for below-half, exactly-half, and above-half values, both signs, and
// both even- and odd-quotient ties.
func TestDivTruthTable(t *testing.T) {
	// Each case: numerator/denominator and the expected result per mode.
	type exp map[RoundingMode]int64
	cases := []struct {
		name     string
		num, den int64
		want     exp
	}{
		{"2.5 even-tie", 5, 2, exp{
			HalfAwayFromZero: 3, HalfToEven: 2, HalfUp: 3, HalfDown: 2,
			Ceiling: 3, Floor: 2, TowardZero: 2,
		}},
		{"-2.5 even-tie", -5, 2, exp{
			HalfAwayFromZero: -3, HalfToEven: -2, HalfUp: -2, HalfDown: -3,
			Ceiling: -2, Floor: -3, TowardZero: -2,
		}},
		{"3.5 odd-tie", 7, 2, exp{
			HalfAwayFromZero: 4, HalfToEven: 4, HalfUp: 4, HalfDown: 3,
			Ceiling: 4, Floor: 3, TowardZero: 3,
		}},
		{"-3.5 odd-tie", -7, 2, exp{
			HalfAwayFromZero: -4, HalfToEven: -4, HalfUp: -3, HalfDown: -4,
			Ceiling: -3, Floor: -4, TowardZero: -3,
		}},
		{"2.4 below-half", 12, 5, exp{
			HalfAwayFromZero: 2, HalfToEven: 2, HalfUp: 2, HalfDown: 2,
			Ceiling: 3, Floor: 2, TowardZero: 2,
		}},
		{"-2.4 below-half", -12, 5, exp{
			HalfAwayFromZero: -2, HalfToEven: -2, HalfUp: -2, HalfDown: -2,
			Ceiling: -2, Floor: -3, TowardZero: -2,
		}},
		{"2.6 above-half", 13, 5, exp{
			HalfAwayFromZero: 3, HalfToEven: 3, HalfUp: 3, HalfDown: 3,
			Ceiling: 3, Floor: 2, TowardZero: 2,
		}},
		{"-2.6 above-half", -13, 5, exp{
			HalfAwayFromZero: -3, HalfToEven: -3, HalfUp: -3, HalfDown: -3,
			Ceiling: -2, Floor: -3, TowardZero: -2,
		}},
		{"exact 4", 8, 2, exp{
			HalfAwayFromZero: 4, HalfToEven: 4, HalfUp: 4, HalfDown: 4,
			Ceiling: 4, Floor: 4, TowardZero: 4,
		}},
		{"exact 0", 0, 1, exp{
			HalfAwayFromZero: 0, HalfToEven: 0, HalfUp: 0, HalfDown: 0,
			Ceiling: 0, Floor: 0, TowardZero: 0,
		}},
	}

	modes := []RoundingMode{
		HalfAwayFromZero, HalfToEven, HalfUp, HalfDown, Ceiling, Floor, TowardZero,
	}

	for _, c := range cases {
		for _, m := range modes {
			num := big.NewInt(c.num)
			den := big.NewInt(c.den)
			got := Div(num, den, m).Int64()
			if got != c.want[m] {
				t.Errorf("%s / %s: Div(%d/%d) = %d, want %d",
					c.name, m, c.num, c.den, got, c.want[m])
			}
			// inputs must not be mutated
			if num.Int64() != c.num || den.Int64() != c.den {
				t.Errorf("%s / %s: Div mutated its inputs", c.name, m)
			}
		}
	}
}

func TestModeString(t *testing.T) {
	if !HalfAwayFromZero.Valid() || !TowardZero.Valid() || RoundingMode(99).Valid() {
		t.Fatal("Valid() is wrong")
	}
	want := map[RoundingMode]string{
		HalfAwayFromZero: "half_away_from_zero",
		HalfToEven:       "half_to_even",
		HalfUp:           "half_up",
		HalfDown:         "half_down",
		Ceiling:          "ceiling",
		Floor:            "floor",
		TowardZero:       "toward_zero",
	}
	for m, s := range want {
		if m.String() != s {
			t.Errorf("%d.String() = %q, want %q", m, m.String(), s)
		}
	}
	if RoundingMode(99).String() != "unknown" {
		t.Error("invalid mode should stringify as 'unknown'")
	}
}
