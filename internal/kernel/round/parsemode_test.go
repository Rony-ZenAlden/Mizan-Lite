package round_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/round"
)

func TestParseModeRoundTripsEveryMode(t *testing.T) {
	// String is documented as the storage form, so every mode must survive
	// persist-and-reload. A mode that silently changed on the way back would alter how
	// money rounds, with no visible cause.
	modes := []round.RoundingMode{
		round.HalfAwayFromZero, round.HalfToEven, round.HalfUp, round.HalfDown,
		round.Ceiling, round.Floor, round.TowardZero,
	}
	for _, want := range modes {
		t.Run(want.String(), func(t *testing.T) {
			got, ok := round.ParseMode(want.String())
			if !ok {
				t.Fatalf("ParseMode rejected %q, its own String output", want.String())
			}
			if got != want {
				t.Errorf("round trip = %v (%s), want %v (%s)", got, got, want, want)
			}
		})
	}
}

func TestParseModeRejectsUnknownNames(t *testing.T) {
	// Rejected rather than defaulted: a typo in a seed file that quietly changed the
	// rounding mode would be nearly impossible to trace back from the symptom.
	for _, bad := range []string{"", "unknown", "HALF_UP", "half up", "bankers"} {
		if got, ok := round.ParseMode(bad); ok {
			t.Errorf("ParseMode(%q) returned %v; want rejection", bad, got)
		}
	}
}

func TestEveryModeHasADistinctName(t *testing.T) {
	seen := map[string]bool{}
	for m := round.HalfAwayFromZero; m <= round.TowardZero; m++ {
		name := m.String()
		if name == "unknown" {
			t.Errorf("mode %d has no stable name", m)
		}
		if seen[name] {
			t.Errorf("two modes share the name %q, so storage is ambiguous", name)
		}
		seen[name] = true
	}
}
