package domain_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	domain "github.com/mizan-erp/mizan/internal/lite/catalog/domain"
)

// costRef is a catalogue reference with the two currencies a shop prices in: dollars to two decimals, pounds to none.
func costRef() domain.Reference {
	return domain.Reference{
		Units:      map[string]domain.Unit{"l": {Code: "l", InputDecimals: 3}, "kg": {Code: "kg", InputDecimals: 3}},
		Currencies: map[string]domain.Currency{"USD": {Code: "USD", Decimals: 2}, "SYP": {Code: "SYP", Decimals: 0}},
	}
}

func priced(t *testing.T, currency, price string) domain.Product {
	t.Helper()
	pid, _ := id.New()
	p, err := domain.NewProduct(pid, domain.Draft{NameAR: "زيت", UnitCode: "l", PriceCurrency: currency, Price: price}, costRef())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestACostPriceIsTypedInTheProductsOwnCurrency(t *testing.T) {
	p := priced(t, "USD", "2.50")
	if _, _, known := p.Margin(); known {
		t.Fatal("a product with no cost claims to know its margin")
	}

	withCost, err := p.SetCost("2.00", costRef())
	if err != nil {
		t.Fatal(err)
	}
	if !withCost.HasCost || withCost.CostMicro != 2_000_000 {
		t.Fatalf("SetCost = %+v", withCost)
	}
	// 2.00 → 2.50 is half a dollar, which is 25% of what the shop paid.
	amount, percent, known := withCost.Margin()
	if !known || amount != 500_000 || percent != 25_000_000 {
		t.Fatalf("margin = %d, %d, %v", amount, percent, known)
	}

	// Cleared again: the shop takes back a cost it typed by mistake, and stops claiming a margin.
	cleared, err := withCost.SetCost("", costRef())
	if err != nil {
		t.Fatal(err)
	}
	if cleared.HasCost || cleared.CostMicro != 0 {
		t.Fatalf("a cleared cost = %+v", cleared)
	}
	if _, _, known := cleared.Margin(); known {
		t.Fatal("a cleared cost left a margin behind")
	}
}

func TestACostPriceIsRefusedWhenItIsNotAPrice(t *testing.T) {
	p := priced(t, "USD", "2.50")
	for name, raw := range map[string]string{
		"nothing":      "0",
		"below zero":   "-1",
		"too precise":  "2.005",
		"not a number": "abc",
	} {
		if _, err := p.SetCost(raw, costRef()); err == nil {
			t.Errorf("%s was accepted as a cost price", name)
		}
	}
	// A pound has no fractions to pay.
	pounds := priced(t, "SYP", "30000")
	if _, err := pounds.SetCost("24000.5", costRef()); errs.CodeOf(err) != domain.CodeCostDecimals {
		t.Fatalf("a fraction of a pound as a cost = %v", err)
	}
}

func TestTheSellPriceIsWorkedOutFromTheMarginBothWays(t *testing.T) {
	base, err := priced(t, "USD", "0.01").SetCost("2.00", costRef())
	if err != nil {
		t.Fatal(err)
	}

	// As a percentage of the cost: 2.00 + 25% = 2.50.
	byPercent, err := base.PriceFromMarginPercent("25", costRef())
	if err != nil || byPercent.PriceMicro != 2_500_000 {
		t.Fatalf("2.00 + 25%% = %d, %v", byPercent.PriceMicro, err)
	}
	// As money: 2.00 + 0.50 = 2.50. The two directions agree.
	byAmount, err := base.PriceFromMarginAmount("0.50", costRef())
	if err != nil || byAmount.PriceMicro != byPercent.PriceMicro {
		t.Fatalf("2.00 + 0.50 = %d, %v", byAmount.PriceMicro, err)
	}
	// And the margin read back off the result is the one that was asked for.
	if amount, percent, _ := byPercent.Margin(); amount != 500_000 || percent != 25_000_000 {
		t.Fatalf("the margin did not survive the round trip: %d, %d", amount, percent)
	}

	// A third of a dollar cannot be charged: the computed price is held to the currency's decimals.
	third, err := base.PriceFromMarginPercent("33.333", costRef())
	if err != nil || third.PriceMicro != 2_670_000 { // 2.66666 → 2.67
		t.Fatalf("2.00 + 33.333%% = %d, %v", third.PriceMicro, err)
	}

	// In pounds, which have no decimals at all: 15,000 + 12% = 16,800 exactly.
	pounds, err := priced(t, "SYP", "1").SetCost("15000", costRef())
	if err != nil {
		t.Fatal(err)
	}
	up, err := pounds.PriceFromMarginPercent("12", costRef())
	if err != nil || up.PriceMicro != 16_800_000_000 {
		t.Fatalf("15,000 + 12%% = %d, %v", up.PriceMicro, err)
	}
}

func TestAMarginIsRefusedWithoutACostAndBelowNothing(t *testing.T) {
	noCost := priced(t, "USD", "2.50")
	if _, err := noCost.PriceFromMarginPercent("25", costRef()); errs.CodeOf(err) != domain.CodeMarginInvalid {
		t.Fatalf("a margin without a cost = %v", err)
	}
	if _, err := noCost.PriceFromMarginAmount("0.50", costRef()); errs.CodeOf(err) != domain.CodeMarginInvalid {
		t.Fatalf("a money margin without a cost = %v", err)
	}

	withCost, err := priced(t, "USD", "2.50").SetCost("2.00", costRef())
	if err != nil {
		t.Fatal(err)
	}
	// −100% is the whole cost gone; below that the price would be less than nothing.
	if _, tooFar := withCost.PriceFromMarginPercent("-150", costRef()); errs.CodeOf(tooFar) != domain.CodeMarginBelowCost {
		t.Fatalf("a margin past the whole cost = %v", tooFar)
	}
	if _, tooFar := withCost.PriceFromMarginAmount("-3.00", costRef()); errs.CodeOf(tooFar) != domain.CodeMarginBelowCost {
		t.Fatalf("a money margin past the whole cost = %v", tooFar)
	}
	// A loss the shop chooses is not an error: selling at 1.50 what cost 2.00 is a margin of −25%.
	loss, err := withCost.PriceFromMarginPercent("-25", costRef())
	if err != nil || loss.PriceMicro != 1_500_000 {
		t.Fatalf("2.00 − 25%% = %d, %v", loss.PriceMicro, err)
	}
	if amount, percent, _ := loss.Margin(); amount != -500_000 || percent != -25_000_000 {
		t.Fatalf("a loss reads back as %d, %d", amount, percent)
	}
}

// TestAMarginOnALargePoundPriceDoesNotOverflow: a shop pricing in old pounds carries figures in the millions, and the margin
// arithmetic multiplies by 100,000,000 before it divides. In int64 that overflows and comes back a plausible wrong number.
func TestAMarginOnALargePoundPriceDoesNotOverflow(t *testing.T) {
	big, err := priced(t, "SYP", "1").SetCost("9000000", costRef()) // nine million pounds
	if err != nil {
		t.Fatal(err)
	}
	up, err := big.PriceFromMarginPercent("10", costRef())
	if err != nil || up.PriceMicro != 9_900_000_000_000 {
		t.Fatalf("9,000,000 + 10%% = %d, %v", up.PriceMicro, err)
	}
	if _, percent, _ := up.Margin(); percent != 10_000_000 {
		t.Fatalf("the margin on a large price reads back as %d", percent)
	}
}
