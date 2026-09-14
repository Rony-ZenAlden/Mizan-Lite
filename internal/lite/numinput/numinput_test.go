package numinput_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
)

type fixture struct {
	Normalise []struct {
		Input  string `json:"input"`
		Output string `json:"output"`
		Error  string `json:"error"`
	} `json:"normalise"`
	LatinDigits []struct {
		Input  string `json:"input"`
		Output string `json:"output"`
	} `json:"latinDigits"`
}

func loadFixture(t *testing.T) fixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Normalise) < 20 || len(f.LatinDigits) < 3 {
		t.Fatalf("the fixture has %d/%d cases; it did not load", len(f.Normalise), len(f.LatinDigits))
	}
	return f
}

// TestSharedFixture is the Go half of the contract. The TypeScript half reads the same file.
func TestSharedFixture(t *testing.T) {
	f := loadFixture(t)
	for _, tc := range f.Normalise {
		t.Run("normalise "+tc.Input, func(t *testing.T) {
			got, err := numinput.Normalise(tc.Input)
			if tc.Error != "" {
				if errs.CodeOf(err) != tc.Error {
					t.Fatalf("Normalise(%q) = %q, %v; want error %s", tc.Input, got, err, tc.Error)
				}
				return
			}
			if err != nil || got != tc.Output {
				t.Fatalf("Normalise(%q) = %q, %v; want %q", tc.Input, got, err, tc.Output)
			}
		})
	}
	for _, tc := range f.LatinDigits {
		if got := numinput.LatinDigits(tc.Input); got != tc.Output {
			t.Errorf("LatinDigits(%q) = %q, want %q", tc.Input, got, tc.Output)
		}
	}
}

func TestAPlainLatinDecimalIsUnchanged(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		whole := rapid.StringMatching(`[1-9][0-9]{0,8}`).Draw(rt, "whole")
		s := whole
		if rapid.Bool().Draw(rt, "hasFraction") {
			s += "." + rapid.StringMatching(`[0-9]{1,6}`).Draw(rt, "fraction")
		}
		got, err := numinput.Normalise(s)
		if err != nil || got != s {
			rt.Fatalf("Normalise(%q) = %q, %v", s, got, err)
		}
	})
}

func TestArabicDigitsNormaliseToTheirLatinTwin(t *testing.T) {
	arabic := []rune("٠١٢٣٤٥٦٧٨٩")
	rapid.Check(t, func(rt *rapid.T) {
		latin := rapid.StringMatching(`[0-9]{1,9}(\.[0-9]{1,6})?`).Draw(rt, "latin")
		var b strings.Builder
		for _, r := range latin {
			if r == '.' {
				b.WriteRune('٫')
			} else {
				b.WriteRune(arabic[r-'0'])
			}
		}
		got, err := numinput.Normalise(b.String())
		if err != nil || got != latin {
			rt.Fatalf("Normalise(%q) = %q, %v; want %q", b.String(), got, err, latin)
		}
	})
}

func TestDecimals(t *testing.T) {
	for in, want := range map[string]int{"3": 0, "3.2": 1, "3.25": 2, "1.750": 3, "0.000001": 6} {
		if got := numinput.Decimals(in); got != want {
			t.Errorf("Decimals(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestFormatFixed(t *testing.T) {
	for _, c := range []struct {
		v                  int64
		scale, minDecimals int
		want               string
	}{
		{12_500_000, 6, 3, "12.500"}, {3_000_000, 6, 0, "3"}, {5_937_500, 6, 2, "5.9375"}, {-2_500_000, 6, 3, "-2.500"},
		{97_500, 0, 0, "97500"}, {120, 2, 2, "1.20"}, {-5, 3, 3, "-0.005"}, {13_007_535_500_000, 9, 0, "13007.5355"},
		{0, 2, 2, "0.00"}, {-9_223_372_036_854_775_808, 0, 0, "-9223372036854775808"},
	} {
		if got := numinput.FormatFixed(c.v, c.scale, c.minDecimals); got != c.want {
			t.Errorf("FormatFixed(%d, %d, %d) = %s, want %s", c.v, c.scale, c.minDecimals, got, c.want)
		}
	}
}
