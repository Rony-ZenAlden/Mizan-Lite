package numbering_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/numbering"
)

func series(t *testing.T, prefix string, padding int) numbering.Series {
	t.Helper()
	built, err := numbering.NewSeries(id.ID("s1"), "SALES_INVOICE", prefix, padding)
	if err != nil {
		t.Fatalf("NewSeries: %v", err)
	}
	return built
}

// ── formatting ──────────────────────────────────────────────────────────────────

// A pure function of a series and a value, so the shape can be tested against a table — and so a
// screen can show what the next number WILL be without consuming it.
func TestFormattingPadsAndAffixes(t *testing.T) {
	cases := []struct {
		name    string
		prefix  string
		suffix  string
		padding int
		value   int64
		want    string
	}{
		{"the ordinary case", "INV-", "", 6, 123, "INV-000123"},
		{"no padding at all", "INV-", "", 0, 123, "INV-123"},
		{"a suffix too", "INV-", "/2026", 4, 7, "INV-0007/2026"},
		{"no prefix", "", "", 5, 42, "00042"},
		{"a value wider than the padding is not truncated", "INV-", "", 3, 123456, "INV-123456"},
		{"the first number", "INV-", "", 6, 1, "INV-000001"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := series(t, tc.prefix, tc.padding)
			s.Suffix = tc.suffix
			if got := s.Format(tc.value); got != tc.want {
				t.Errorf("Format(%d) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

// ── allocation ──────────────────────────────────────────────────────────────────

// A new series issues 1, not 0 and not 2. `NextValue` is the next number to ISSUE, which is the
// naming that makes this obvious rather than a comment that has to explain it.
func TestANewSeriesIssuesOneFirst(t *testing.T) {
	s := series(t, "INV-", 6)

	number, _, err := s.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if number != "INV-000001" {
		t.Errorf("first number = %q, want INV-000001", number)
	}
}

// The counter is PART OF THE ANSWER, so there is no path where a number is taken and the counter
// is left alone. A caller that ignores the returned series gets no advance — which is visible in
// the code rather than silent in the data.
func TestTakingANumberReturnsTheAdvancedSeries(t *testing.T) {
	s := series(t, "INV-", 6)

	first, s, err := s.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	second, s, err := s.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	third, _, err := s.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}

	if first != "INV-000001" || second != "INV-000002" || third != "INV-000003" {
		t.Errorf("got %q, %q, %q — want consecutive", first, second, third)
	}
}

// The mutation this guards: a caller that discards the returned series would issue the same
// number forever, and every invoice after the first would collide.
func TestTheOriginalSeriesIsUnchangedByTakingANumber(t *testing.T) {
	s := series(t, "INV-", 6)

	if _, _, err := s.Next(); err != nil {
		t.Fatalf("Next: %v", err)
	}
	// `s` was passed by value: taking a number cannot have advanced it in place.
	if s.NextValue != 1 {
		t.Errorf("the original series advanced to %d — Next mutated its receiver", s.NextValue)
	}
}

// A number that cannot be stored must fail HERE, before a document is written against it, rather
// than as a truncation nobody notices until two invoices share a number.
func TestANumberTooLongToStoreIsRefused(t *testing.T) {
	s := series(t, "THIS-IS-A-VERY-LONG-PREFIX-INDEED-FOR-AN-INVOICE-NUMBER-", 18)

	_, _, err := s.Next()
	if err == nil {
		t.Fatal("a number too long for the column was issued")
	}
	if code := errs.CodeOf(err); code != numbering.CodeNumberTooLong {
		t.Errorf("code = %q, want %q", code, numbering.CodeNumberTooLong)
	}
}

// ── construction ────────────────────────────────────────────────────────────────

func TestASeriesNeedsACode(t *testing.T) {
	if _, err := numbering.NewSeries(id.ID("s1"), "   ", "INV-", 6); err == nil {
		t.Fatal("a series with no code was accepted")
	}
}

// 18 digits is where an int64 stops being able to hold the value being padded, so a wider pad
// could only ever produce a number the sequence cannot reach.
func TestPaddingIsBoundedToWhatAnInt64CanReach(t *testing.T) {
	if _, err := numbering.NewSeries(id.ID("s1"), "INV", "INV-", 19); err == nil {
		t.Error("a padding wider than an int64 can reach was accepted")
	}
	if _, err := numbering.NewSeries(id.ID("s1"), "INV", "INV-", -1); err == nil {
		t.Error("a negative padding was accepted")
	}
	if _, err := numbering.NewSeries(id.ID("s1"), "INV", "INV-", 18); err != nil {
		t.Errorf("the widest reachable padding was refused: %v", err)
	}
}

func TestASeriesCodeIsNormalised(t *testing.T) {
	built, err := numbering.NewSeries(id.ID("s1"), "  sales_invoice  ", "INV-", 6)
	if err != nil {
		t.Fatalf("NewSeries: %v", err)
	}
	// Codes are matched exactly by the allocator and by seeds; "sales_invoice" and
	// "SALES_INVOICE" being two series would be a duplicate nobody spots until the numbers
	// diverge.
	if built.Code != "SALES_INVOICE" {
		t.Errorf("code = %q, want SALES_INVOICE", built.Code)
	}
}
