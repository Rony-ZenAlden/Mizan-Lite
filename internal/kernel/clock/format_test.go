package clock_test

import (
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
)

func TestFormatIsFixedWidthUTC(t *testing.T) {
	// Fixed width in UTC is what makes a lexical comparison a chronological one, which is
	// how a plain text column stays orderable on every engine.
	got := clock.Format(time.Date(2026, 7, 30, 9, 5, 3, 456_000_000, time.UTC))
	if want := "2026-07-30T09:05:03.456Z"; got != want {
		t.Fatalf("Format = %q, want %q", got, want)
	}
	if len(got) != 24 {
		t.Errorf("length = %d, want 24 (CHAR(24) contract)", len(got))
	}
}

func TestFormatConvertsToUTC(t *testing.T) {
	zone := time.FixedZone("UTC+3", 3*60*60)
	local := time.Date(2026, 7, 30, 12, 0, 0, 0, zone)
	if got, want := clock.Format(local), "2026-07-30T09:00:00.000Z"; got != want {
		t.Errorf("Format = %q, want %q — a non-UTC timestamp must be converted, not relabelled", got, want)
	}
}

func TestLexicalOrderMatchesChronologicalOrder(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	prev := ""
	for i := 0; i < 500; i++ {
		got := clock.Format(base.Add(time.Duration(i) * 137 * time.Millisecond))
		if prev != "" && got <= prev {
			t.Fatalf("ordering broke at %d: %q then %q", i, prev, got)
		}
		prev = got
	}
}

func TestParseTimestampRoundTrip(t *testing.T) {
	original := time.Date(2026, 7, 30, 9, 5, 3, 456_000_000, time.UTC)
	got, ok := clock.ParseTimestamp(clock.Format(original))
	if !ok {
		t.Fatal("ParseTimestamp rejected its own output")
	}
	if !got.Equal(original) {
		t.Errorf("round trip = %v, want %v", got, original)
	}
}

func TestParseTimestampRejectsOtherLayouts(t *testing.T) {
	for _, bad := range []string{"", "2026-07-30", "2026-07-30T09:05:03Z", "not a time"} {
		if _, ok := clock.ParseTimestamp(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestFixedClockAdvances(t *testing.T) {
	start := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	c := clock.NewFixed(start)
	if !c.Now().Equal(start) {
		t.Fatalf("Now = %v, want %v", c.Now(), start)
	}
	c.Advance(90 * time.Minute)
	if want := start.Add(90 * time.Minute); !c.Now().Equal(want) {
		t.Errorf("after Advance, Now = %v, want %v", c.Now(), want)
	}
}
