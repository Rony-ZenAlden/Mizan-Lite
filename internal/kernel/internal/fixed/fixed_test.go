package fixed

import (
	"math"
	"testing"
)

func TestAddI64Boundaries(t *testing.T) {
	cases := []struct {
		a, b int64
		want int64
		ok   bool
	}{
		{1, 2, 3, true},
		{math.MaxInt64, 0, math.MaxInt64, true},
		{math.MaxInt64, 1, 0, false},  // overflow
		{math.MinInt64, -1, 0, false}, // underflow
		{math.MinInt64, math.MaxInt64, -1, true},
		{-5, -5, -10, true},
	}
	for _, c := range cases {
		got, ok := AddI64(c.a, c.b)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("AddI64(%d,%d) = (%d,%v), want (%d,%v)", c.a, c.b, got, ok, c.want, c.ok)
		}
	}
}

func TestSubI64Boundaries(t *testing.T) {
	cases := []struct {
		a, b int64
		want int64
		ok   bool
	}{
		{10, 3, 7, true},
		{math.MinInt64, 1, 0, false},  // underflow
		{math.MaxInt64, -1, 0, false}, // overflow
		{0, math.MinInt64, 0, false},  // -MinInt64 overflows
		{math.MaxInt64, math.MaxInt64, 0, true},
	}
	for _, c := range cases {
		got, ok := SubI64(c.a, c.b)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("SubI64(%d,%d) = (%d,%v), want (%d,%v)", c.a, c.b, got, ok, c.want, c.ok)
		}
	}
}

func TestMulI64Boundaries(t *testing.T) {
	cases := []struct {
		a, b int64
		want int64
		ok   bool
	}{
		{0, math.MaxInt64, 0, true},
		{math.MaxInt64, 0, 0, true},
		{3, 4, 12, true},
		{math.MaxInt64, 2, 0, false},  // overflow
		{math.MinInt64, -1, 0, false}, // -MinInt64 overflows
		{-1, math.MinInt64, 0, false},
		{math.MinInt64, 1, math.MinInt64, true},
	}
	for _, c := range cases {
		got, ok := MulI64(c.a, c.b)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("MulI64(%d,%d) = (%d,%v), want (%d,%v)", c.a, c.b, got, ok, c.want, c.ok)
		}
	}
}

func TestPow10(t *testing.T) {
	if Pow10(0).Int64() != 1 || Pow10(2).Int64() != 100 || Pow10(6).Int64() != 1_000_000 {
		t.Error("Pow10 small values wrong")
	}
	if Pow10(18).String() != "1000000000000000000" {
		t.Errorf("Pow10(18) = %s", Pow10(18))
	}
}

func TestParseScaled(t *testing.T) {
	good := []struct {
		s     string
		scale int
		want  int64
	}{
		{"19.99", 2, 1999},
		{"0", 2, 0},
		{"-5.5", 2, -550},
		{"1500", 0, 1500},
		{"1.234", 3, 1234},
		{"+3.05", 2, 305},
		{".5", 2, 50},  // leading dot
		{"5.", 2, 500}, // trailing dot
	}
	for _, c := range good {
		v, ok := ParseScaled(c.s, c.scale)
		if !ok || v.Int64() != c.want {
			t.Errorf("ParseScaled(%q,%d) = (%v,%v), want %d", c.s, c.scale, v, ok, c.want)
		}
	}
	bad := []struct {
		s     string
		scale int
	}{
		{"", 2}, {"abc", 2}, {"1.2.3", 2}, {"1.999", 2}, {"1,000", 2},
		{"-", 2}, {".", 2}, {"1e3", 2},
	}
	for _, c := range bad {
		if _, ok := ParseScaled(c.s, c.scale); ok {
			t.Errorf("ParseScaled(%q,%d): want failure", c.s, c.scale)
		}
	}
}

func TestFormatScaled(t *testing.T) {
	cases := []struct {
		value int64
		scale int
		want  string
	}{
		{1999, 2, "19.99"},
		{0, 2, "0.00"},
		{-550, 2, "-5.50"},
		{1500, 0, "1500"},
		{5, 2, "0.05"},
		{math.MinInt64, 0, "-9223372036854775808"}, // safe magnitude of MinInt64
		{math.MaxInt64, 0, "9223372036854775807"},
	}
	for _, c := range cases {
		if got := FormatScaled(c.value, c.scale); got != c.want {
			t.Errorf("FormatScaled(%d,%d) = %q, want %q", c.value, c.scale, got, c.want)
		}
	}
}
