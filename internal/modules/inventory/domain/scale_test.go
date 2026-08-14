package domain_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/modules/inventory/domain"
)

// TestCostingIsInMinorUnitsWhateverTheCurrencyScale
//
// # The test that was missing for two phases
//
// A quantity is 10⁻⁶ and a unit cost is 10⁻⁶ of the MAJOR unit (§E), so their product is an
// amount in major units — and money is stored in MINOR units. The conversion needs the currency's
// scale, and it was omitted entirely: every `…Minor` field carried major units.
//
// It survived Phases 4 and 5 because every test that consumed a costed value used SYP, which has
// NO minor unit. At scale 0 the two numbers are identical, so the conversion was only ever
// exercised where it could not be wrong. **A scale conversion tested only at scale 1 is a
// conversion nobody has tested.**
//
// A two-decimal currency found it on the first attempt: two items at 10.00 costed at 20 instead
// of 2,000, which would have made every cost of goods sold a hundredth of the truth.
func TestCostingIsInMinorUnitsWhateverTheCurrencyScale(t *testing.T) {
	// Two units at 10.00 each is 20.00.
	const (
		twoUnits = 2_000_000  // 2
		tenEach  = 10_000_000 // 10.00 per unit, at the UnitAmount scale
	)

	cases := []struct {
		name     string
		decimals int
		want     int64
	}{
		{"no minor unit, as SYP and JPY", 0, 20},
		{"two decimals, as SAR and USD", 2, 2_000},
		{"three decimals, as KWD and BHD", 3, 20_000},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			movement := domain.Movement{
				Type: domain.Receipt, QuantityMicro: twoUnits,
				UnitCostMicro: tenEach, Decimals: test.decimals,
			}
			result, err := domain.WAC{}.Cost(domain.State{}, movement, domain.Movement{})
			if err != nil {
				t.Fatalf("Cost: %v", err)
			}
			if result.ValueDeltaMinor != test.want {
				t.Fatalf("value = %d, want %d — the currency's scale was not applied",
					result.ValueDeltaMinor, test.want)
			}
		})
	}
}

// TestAnIssueIsCostedInMinorUnitsToo
//
// The outward path reads the AVERAGE rather than the movement's cost, and had its own call to the
// same conversion. A fix applied to one direction and not the other would leave sales right and
// purchases wrong, or the reverse.
func TestAnIssueIsCostedInMinorUnitsToo(t *testing.T) {
	state := domain.State{OnHandMicro: 10_000_000, AverageMicro: 10_000_000}
	movement := domain.Movement{
		Type: domain.Issue, QuantityMicro: 2_000_000, Decimals: 2,
	}

	result, err := domain.WAC{}.Cost(state, movement, domain.Movement{})
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}
	// Two leaving at 10.00 is 20.00 of value gone: −2,000 minor units.
	if result.ValueDeltaMinor != -2_000 {
		t.Fatalf("value = %d, want -2000", result.ValueDeltaMinor)
	}
}

// TestASupplierReturnIsCostedInMinorUnitsToo
//
// The third path, added in 6.6. Included because the first two were fixed together and a third
// written afterwards is exactly where the same omission comes back.
func TestASupplierReturnIsCostedInMinorUnitsToo(t *testing.T) {
	state := domain.State{OnHandMicro: 10_000_000, AverageMicro: 10_000_000}
	original := domain.Movement{UnitCostMicro: 10_000_000}
	movement := domain.Movement{
		Type: domain.ReturnOut, QuantityMicro: 2_000_000,
		SourceMovementID: "m1", Decimals: 2,
	}

	result, err := domain.WAC{}.Cost(state, movement, original)
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}
	if result.ValueDeltaMinor != -2_000 {
		t.Fatalf("value = %d, want -2000", result.ValueDeltaMinor)
	}
	// And it left at the ORIGINAL cost, not the average — §D.3.
	if result.UnitCostMicro != 10_000_000 {
		t.Errorf("unit cost = %d, want the original 10000000", result.UnitCostMicro)
	}
}
