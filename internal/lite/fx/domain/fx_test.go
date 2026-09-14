package domain_test

import (
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/fx/domain"
)

const n = int64(1_000_000_000) // one pound per dollar

var (
	syp = domain.Currency{Code: "SYP", Decimals: 0}
	usd = domain.Currency{Code: "USD", Decimals: 2}
)

func TestARateTakesAtMostFourDecimals(t *testing.T) {
	for raw, want := range map[string]int64{
		"15000": 15_000 * n, "١٥٠٠٠": 15_000 * n, "13007.5355": 13_007_535_500_000, "0.5": n / 2, "121.9579": 121_957_900_000,
	} {
		if got, err := domain.ParseRate(raw); err != nil || got != want {
			t.Errorf("ParseRate(%q) = %d, %v; want %d", raw, got, err, want)
		}
	}
	for raw, code := range map[string]string{
		"0.0000667":         domain.CodeRateDecimals, // the inverse, typed in the wrong direction
		"13007.53553":       domain.CodeRateDecimals,
		"0":                 domain.CodeRateRequired,
		"0.0000":            domain.CodeRateRequired,
		"15,000":            "lite.number.grouping",
		"":                  "lite.number.invalid",
		"-15000":            "lite.number.invalid",
		"99999999999999999": domain.CodeRateTooLarge,
	} {
		_, err := domain.ParseRate(raw)
		if errs.CodeOf(err) != code {
			t.Errorf("ParseRate(%q) = %v; want %s", raw, err, code)
			continue
		}
		if typed, _ := errs.AsError(err); len(typed.Fields) == 0 || typed.Fields[len(typed.Fields)-1].Field != domain.FieldRate {
			t.Errorf("ParseRate(%q): the refusal names no field", raw)
		}
	}
}

func TestAQuoteIsExactScaledAndRoundedOnce(t *testing.T) {
	for _, c := range []struct {
		text  string
		scale int64
		want  int64
	}{
		{"13007.53553648", 1, 13_007_535_500_000}, // .53553648 → .5355
		{"13007.53555", 1, 13_007_535_600_000},    // half up at the fifth decimal
		{"121.957862", 100, 12_195_786_200_000},   // the new pound, scaled to the old
		{"121.9578625", 100, 12_195_786_300_000},  // ×100 first, then one rounding
		{"1.3007535536e4", 1, 13_007_535_500_000}, // exponent notation is still exact
		{"0.00004", 1, 0},                         // rounds to nothing: refused below
	} {
		got, ok := domain.QuoteFromDecimal(c.text, c.scale)
		if c.want == 0 {
			if ok {
				t.Errorf("%s accepted as %d", c.text, got)
			}
			continue
		}
		if !ok || got != c.want {
			t.Errorf("QuoteFromDecimal(%s, %d) = %d, %v; want %d", c.text, c.scale, got, ok, c.want)
		}
	}
	for _, bad := range []string{"", "abc", "-1", "0", "NaN"} {
		if _, ok := domain.QuoteFromDecimal(bad, 1); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestALargeChangeNeedsConfirmationBothWays(t *testing.T) {
	for _, c := range []struct {
		from, to int64
		exceeds  bool
		percent  string
	}{
		{15_000 * n, 18_000 * n, false, "20.0"}, // exactly 20%: not beyond
		{15_000 * n, 18_001 * n, true, "20.0"},  // beyond, though it rounds to 20.0
		{15_000 * n, 12_000 * n, false, "-20.0"},
		{15_000 * n, 11_999 * n, true, "-20.0"},
		{15_000 * n, 13_007_535_500_000, false, "-13.3"},     // the internet's rate against the market's (L3 §14.3 F2)
		{15_000 * n, 15 * n, true, "-99.9"},                  // 15.000 read as fifteen (H1)
		{13_007_535_500_000, 121_957_900_000, true, "-99.1"}, // the new pound, unscaled (F1)
		{15_000 * n, 15_000 * n, false, "0.0"},
	} {
		if got := domain.ChangeExceedsLimit(c.from, c.to); got != c.exceeds {
			t.Errorf("%d → %d exceeds = %v", c.from, c.to, got)
		}
		if got := domain.ChangePercent(c.from, c.to); got != c.percent {
			t.Errorf("%d → %d percent = %s, want %s", c.from, c.to, got, c.percent)
		}
	}
	err := domain.LargeChange(15_000*n, 15*n)
	typed, _ := errs.AsError(err)
	if errs.CodeOf(err) != domain.CodeLargeChange || typed.Params["inForce"] != "15000" || typed.Params["typed"] != "15" || typed.Params["percent"] != "-99.9" {
		t.Fatalf("LargeChange = %+v", typed)
	}
}

// TestTheFetchDecision is L3 §14.5's table, row by row, in order.
func TestTheFetchDecision(t *testing.T) {
	today, yesterday := "2026-09-14", "2026-09-13"
	quote := domain.Quote{Provider: "p", Nano: 13_007_535_500_000}
	rate := func(nano int64, source domain.Source, day string) *domain.Rate {
		return &domain.Rate{Nano: nano, Source: source, BusinessDate: day}
	}
	for _, c := range []struct {
		name    string
		mode    domain.Mode
		inForce *domain.Rate
		quote   domain.Quote
		want    domain.Outcome
	}{
		{"manual mode never applies", domain.ModeManual, rate(13_000*n, domain.SourceFetched, yesterday), quote, domain.OutcomeHeldMode},
		{"manual mode, even with no rate", domain.ModeManual, nil, quote, domain.OutcomeHeldMode},
		{"no rate: the first rate is a person's", domain.ModeAutomatic, nil, quote, domain.OutcomeProposed},
		{"the owner's rate today holds", domain.ModeAutomatic, rate(15_000*n, domain.SourceManual, today), quote, domain.OutcomeHeldToday},
		{"the owner's rate from yesterday does not", domain.ModeAutomatic, rate(13_500*n, domain.SourceManual, yesterday), quote, domain.OutcomeApplied},
		{"a first-run rate today does not hold", domain.ModeAutomatic, rate(13_500*n, domain.SourceFirstRun, today), quote, domain.OutcomeApplied},
		{"beyond 20% is a proposal", domain.ModeAutomatic, rate(13_000*n, domain.SourceFetched, today), domain.Quote{Provider: "p", Nano: 130 * n}, domain.OutcomeProposed},
		{"the same figure today is unchanged", domain.ModeAutomatic, rate(quote.Nano, domain.SourceFetched, today), quote, domain.OutcomeUnchanged},
		{"the same figure on a new day is the day's confirmation", domain.ModeAutomatic, rate(quote.Nano, domain.SourceFetched, yesterday), quote, domain.OutcomeApplied},
		{"a small move applies", domain.ModeAutomatic, rate(13_100*n, domain.SourceFetched, today), quote, domain.OutcomeApplied},
	} {
		if got := domain.Decide(c.mode, c.inForce, today, c.quote); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func TestStaleMeansNotRecordedToday(t *testing.T) {
	r := domain.Rate{BusinessDate: "2026-09-14"}
	if r.Stale("2026-09-14") || !r.Stale("2026-09-15") {
		t.Fatal("stale is not 'recorded before today'")
	}
}

func TestAgeIsNeverNegative(t *testing.T) {
	at := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	r := domain.Rate{RecordedAt: at}
	if r.Age(at.Add(3*time.Hour)) != 3*time.Hour || r.Age(at.Add(-time.Hour)) != 0 {
		t.Fatal("age wrong, or negative when the clock went back")
	}
}

func TestModesAndNotes(t *testing.T) {
	if m, err := domain.ParseMode("manual"); err != nil || m != domain.ModeManual {
		t.Fatal(m, err)
	}
	if _, err := domain.ParseMode("Manual"); errs.CodeOf(err) != domain.CodeInvalidMode {
		t.Fatal(err)
	}
	long := ""
	for range domain.MaxNoteRunes + 1 {
		long += "ت"
	}
	if _, err := domain.CleanNote(long); errs.CodeOf(err) != domain.CodeNoteTooLong {
		t.Fatal(err)
	}
	if note, _ := domain.CleanNote("  تصحيح "); note != "تصحيح" {
		t.Fatal(note)
	}
}

func TestFormatting(t *testing.T) {
	for v, want := range map[int64]string{15_000 * n: "15000", 13_007_535_500_000: "13007.5355", n / 2: "0.5"} {
		if got := domain.FormatRate(v); got != want {
			t.Errorf("FormatRate(%d) = %s", v, got)
		}
	}
	if domain.FormatMinor(120, 2) != "1.20" || domain.FormatMinor(97_500, 0) != "97500" || domain.FormatMinor(-5, 3) != "-0.005" {
		t.Error("FormatMinor")
	}
}

func TestAProductShowsItsPriceInTheOtherCurrency(t *testing.T) {
	r := domain.Rate{Nano: 15_000 * n}
	// $6.50 at 15,000 is 97,500 pounds.
	c, ok, err := r.PriceInOther("USD", 6_500_000, syp, usd)
	if err != nil || !ok || c.Minor != 97_500 || c.Currency != syp || c.Text() != "97500" {
		t.Fatalf("USD price = %+v %v %v", c, ok, err)
	}
	// 18,000 pounds at 15,000 is $1.20.
	c, ok, _ = r.PriceInOther("SYP", 18_000_000_000, syp, usd)
	if !ok || c.Minor != 120 || c.Text() != "1.20" {
		t.Fatalf("SYP price = %+v", c)
	}
	// 10,000 pounds at 15,000 is $0.6666… → 0.67, one rounding.
	c, _, _ = r.PriceInOther("SYP", 10_000_000_000, syp, usd)
	if c.Minor != 67 {
		t.Fatalf("got %d", c.Minor)
	}
	if _, ok, err := r.PriceInOther("EUR", 1, syp, usd); ok || err != nil {
		t.Fatal("a currency the rate does not price was converted")
	}
}

func TestStockValueInPoundsIsPerLine(t *testing.T) {
	r := domain.Rate{Nano: 13_000 * n}
	// DESIGN §4.4: 0.5 L at $3.33 at 13,000 is 21,645 pounds, not the 21,710 of rounding twice.
	c, err := r.LineInLocal(3_330_000, 500_000, syp, usd)
	if err != nil || c.Minor != 21_645 || c.Currency != syp {
		t.Fatalf("got %+v, %v", c, err)
	}
}
