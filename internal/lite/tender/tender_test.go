package tender_test

import (
	"math/big"
	"testing"

	"pgregory.net/rapid"

	"github.com/mizan-erp/mizan/internal/lite/tender"
)

var (
	syp = tender.Currency{Code: "SYP", Decimals: 0}
	usd = tender.Currency{Code: "USD", Decimals: 2}
)

const r15000 = 15_000_000_000_000

func TestConvertIsExactBothWays(t *testing.T) {
	// $6.67 is 100,050 pounds; 100,000 pounds is 666.666… cents.
	if got := tender.Convert(667, usd, syp, r15000); got.Cmp(big.NewRat(100_050, 1)) != 0 {
		t.Fatalf("$6.67 = %s pounds", got.RatString())
	}
	if got := tender.Convert(100_000, syp, usd, r15000); got.Cmp(big.NewRat(2000, 3)) != 0 {
		t.Fatalf("100,000 pounds = %s cents", got.RatString())
	}
	if got := tender.Convert(123, usd, usd, r15000); got.Cmp(big.NewRat(123, 1)) != 0 {
		t.Fatalf("same currency = %s", got.RatString())
	}
}

func TestRoundIsHalfUpToTheIncrement(t *testing.T) {
	for _, c := range []struct {
		v         *big.Rat
		increment int64
		want      int64
	}{
		{big.NewRat(143_700, 1), 500, 143_500},
		{big.NewRat(143_750, 1), 500, 144_000}, // exactly half: up
		{big.NewRat(2000, 3), 1, 667},
		{big.NewRat(-250, 1), 500, -500}, // half away from zero
		{big.NewRat(7, 1), 0, 7},
	} {
		if got := tender.Round(c.v, c.increment); got != c.want {
			t.Errorf("Round(%s, %d) = %d, want %d", c.v.RatString(), c.increment, got, c.want)
		}
	}
}

// TestConversionRoundsOnceEitherWay: whatever the rate and amount, a rounded conversion is within half an increment of
// the exact one — the bound the verifiers hold.
func TestConversionRoundsOnceEitherWay(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		minor := rapid.Int64Range(0, 10_000_000_000).Draw(t, "minor")
		rate := rapid.Int64Range(1_000_000_000, 100_000_000_000_000).Draw(t, "rate")
		note := rapid.SampledFrom([]int64{1, 50, 100, 500, 1000}).Draw(t, "note")
		for _, pair := range [][2]tender.Currency{{usd, syp}, {syp, usd}} {
			exact := tender.Convert(minor, pair[0], pair[1], rate)
			increment := tender.Increment(pair[1], note)
			if !tender.WithinHalf(tender.Round(exact, increment), exact, increment) {
				t.Fatalf("%d %s → %s at %d: rounded beyond half an increment", minor, pair[0].Code, pair[1].Code, rate)
			}
		}
	})
}

func TestIncrementIsTheNoteOnlyInTheLocalCurrency(t *testing.T) {
	if tender.Increment(usd, 500) != 1 || tender.Increment(syp, 500) != 500 {
		t.Fatal("increments")
	}
	if tender.WithinHalf(251, big.NewRat(0, 1), 500) || !tender.WithinHalf(250, big.NewRat(0, 1), 500) {
		t.Fatal("WithinHalf's bound")
	}
}
