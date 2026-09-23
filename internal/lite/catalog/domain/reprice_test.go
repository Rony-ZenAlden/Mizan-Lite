package domain_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	domain "github.com/mizan-erp/mizan/internal/lite/catalog/domain"
)

// oldPoundProduct is a jar priced at 15,000 old pounds when the dollar was 15,000 — one dollar's worth.
func oldPoundProduct() domain.Product {
	return domain.Product{PriceCurrency: "SYP", PriceMicro: 15_000_000_000, PricedRateNano: 15_000_000_000_000}
}

const (
	sypNote = 500 // the shop's smallest note, in old pounds
)

func sypStep() int64 {
	return domain.StepMicro(domain.Currency{Code: "SYP", Decimals: 0}, "SYP", sypNote)
}

// TestFollowingTheRateKeepsTheDollarValue is the whole point of the proposal: a jar priced at one dollar stays at one
// dollar. At 16,500 it is 16,500, not 15,000 — which is what the shop would otherwise go on charging.
func TestFollowingTheRateKeepsTheDollarValue(t *testing.T) {
	p := oldPoundProduct()
	got, ok := p.FollowRate("SYP", 16_500_000_000_000, sypStep())
	if !ok || got != 16_500_000_000 {
		t.Fatalf("15,000 at 15,000 → 16,500 = %d, %v; want 16,500 pounds", got, ok)
	}
	shift, _ := p.RateShift("SYP", 16_500_000_000_000)
	if shift != 10_000_000 {
		t.Fatalf("the rate moved %d micro-points, want 10%%", shift)
	}
}

// TestAProposalLandsOnANoteTheShopCanGiveChangeIn: 15,000 × 13,700 ÷ 13,000 is 15,807.69… and nobody can charge that.
func TestAProposalLandsOnANoteTheShopCanGiveChangeIn(t *testing.T) {
	p := oldPoundProduct()
	p.PricedRateNano = 13_000_000_000_000
	got, _ := p.FollowRate("SYP", 13_700_000_000_000, sypStep())
	if got != 16_000_000_000 { // 15,807.69 → the nearest 500 is 16,000
		t.Fatalf("proposal = %d micro, want 16,000 pounds", got)
	}
	if got%sypStep() != 0 {
		t.Fatalf("%d is not a whole number of 500-pound notes", got)
	}
}

// TestADollarPricedProductIsNeverStale: its pound price is worked out at the rate of every sale already.
func TestADollarPricedProductIsNeverStale(t *testing.T) {
	p := domain.Product{PriceCurrency: "USD", PriceMicro: 3_250_000, PricedRateNano: 15_000_000_000_000}
	if p.Stale("SYP", 30_000_000_000_000) {
		t.Fatal("a dollar price was called stale after the rate doubled")
	}
	if _, ok := p.FollowRate("SYP", 30_000_000_000_000, sypStep()); ok {
		t.Fatal("a dollar price was offered a re-price")
	}
}

// TestNothingIsStaleWithoutSomethingToMeasure: an open-priced product, or one priced before any rate existed.
func TestNothingIsStaleWithoutSomethingToMeasure(t *testing.T) {
	open := domain.Product{PriceCurrency: "SYP", OpenPrice: true, PricedRateNano: 15_000_000_000_000}
	if open.Stale("SYP", 30_000_000_000_000) {
		t.Fatal("an open-priced product was called stale")
	}
	unknown := oldPoundProduct()
	unknown.PricedRateNano = 0
	if unknown.Stale("SYP", 30_000_000_000_000) {
		t.Fatal("a product priced before any rate was called stale")
	}
}

// TestTheThresholdCountsBothWays: a falling dollar leaves pound prices too high, which loses customers not margin.
func TestTheThresholdCountsBothWays(t *testing.T) {
	p := oldPoundProduct()
	for name, tc := range map[string]struct {
		now   int64
		stale bool
	}{
		"up 4.9%":   {15_735_000_000_000, false},
		"up 5%":     {15_750_000_000_000, true},
		"down 5%":   {14_250_000_000_000, true},
		"down 4.9%": {14_265_000_000_000, false},
		"unchanged": {15_000_000_000_000, false},
	} {
		if got := p.Stale("SYP", tc.now); got != tc.stale {
			t.Errorf("%s: stale = %v, want %v", name, got, tc.stale)
		}
	}
}

// TestAUniformPercentageMovesEveryPriceTheSameWay, and a typo of 1000 for 10 is refused rather than proposed.
func TestAUniformPercentageMovesEveryPriceTheSameWay(t *testing.T) {
	percent, err := domain.ParseRepricePercent("10")
	if err != nil {
		t.Fatal(err)
	}
	if got := oldPoundProduct().ByPercent(percent, sypStep()); got != 16_500_000_000 {
		t.Fatalf("15,000 + 10%% = %d", got)
	}
	cut, _ := domain.ParseRepricePercent("-10")
	if got := oldPoundProduct().ByPercent(cut, sypStep()); got != 13_500_000_000 {
		t.Fatalf("15,000 − 10%% = %d", got)
	}
	for _, bad := range []string{"1000", "-95", "0", "ten", ""} {
		if _, err := domain.ParseRepricePercent(bad); errs.CodeOf(err) == "" {
			t.Errorf("%q was accepted as a re-price percentage", bad)
		}
	}
}

// TestAProposalIsNeverNothing: rounding a very cheap item to the nearest note must not put it on the shelf for free.
func TestAProposalIsNeverNothing(t *testing.T) {
	cheap := domain.Product{PriceCurrency: "SYP", PriceMicro: 100_000_000, PricedRateNano: 15_000_000_000_000} // 100 pounds
	got, _ := cheap.FollowRate("SYP", 15_100_000_000_000, sypStep())
	if got != sypStep() {
		t.Fatalf("a 100-pound item rounded to %d micro — want one note, never zero", got)
	}
}

// TestALargeOldPoundPriceDoesNotOverflow: ten million pounds in micros times a rate in nanos is ~10²⁶.
func TestALargeOldPoundPriceDoesNotOverflow(t *testing.T) {
	big := domain.Product{PriceCurrency: "SYP", PriceMicro: 10_000_000_000_000, PricedRateNano: 15_000_000_000_000}
	got, ok := big.FollowRate("SYP", 16_500_000_000_000, sypStep())
	if !ok || got != 11_000_000_000_000 {
		t.Fatalf("10,000,000 × 1.1 = %d, %v", got, ok)
	}
}

// TestOpenPricedProductsTakeNoPriceCostOrLevel.
func TestOpenPricedProductsTakeNoPriceCostOrLevel(t *testing.T) {
	ref := costRef()
	open, err := domain.NewProduct(newID(t), domain.Draft{NameAR: "متفرقات", UnitCode: "l", PriceCurrency: "SYP", OpenPrice: true}, ref)
	if err != nil || !open.OpenPrice || open.PriceMicro != 0 {
		t.Fatalf("an open-priced product = %+v, %v", open, err)
	}
	if _, err := open.Reprice("SYP", "500", ref); errs.CodeOf(err) != domain.CodeOpenPriceHasNoPrice {
		t.Fatalf("an open-priced product took a price: %v", err)
	}
	if _, err := open.SetCost("500", ref); errs.CodeOf(err) != domain.CodeOpenPriceHasNoPrice {
		t.Fatalf("an open-priced product took a cost: %v", err)
	}
	if _, err := open.SetReorder("5", ref); errs.CodeOf(err) != domain.CodeOpenPriceHasNoPrice {
		t.Fatalf("an open-priced product took a reorder level: %v", err)
	}
	if _, err := domain.NewProduct(newID(t), domain.Draft{NameAR: "كيس", UnitCode: "l", PriceCurrency: "SYP",
		OpenPrice: true, Price: "500"}, ref); errs.CodeOf(err) != domain.CodeOpenPriceHasNoPrice {
		t.Fatalf("an open-priced product was created with a price: %v", err)
	}
}
