package round_test

import (
	"errors"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/round"
)

func sum(values []int64) int64 {
	var total int64
	for _, v := range values {
		total += v
	}
	return total
}

func TestAnAllocationAlwaysTiesToTheWhole(t *testing.T) {
	// The one property everything else depends on. Naive proportional division leaves the parts
	// summing to a little less than the amount, and those units land in nobody's account — over a
	// year of shipments, sales and tax lines that is a reconciliation nobody can close.
	cases := []struct {
		amount  int64
		weights []int64
	}{
		{100, []int64{1, 1, 1}},
		{10_000, []int64{1, 2, 3, 4}},
		{1, []int64{1, 1, 1, 1, 1}},
		{-100, []int64{1, 1, 1}},
		{7, []int64{3, 3, 3, 3, 3, 3, 3}},
		{999_999_999, []int64{7, 11, 13}},
		{0, []int64{5, 5}},
		{100, []int64{0, 0, 0}},
		{5, []int64{1_000_000, 1}},
	}
	for _, test := range cases {
		parts, err := round.Allocate(test.amount, test.weights)
		if err != nil {
			t.Fatalf("Allocate(%d, %v): %v", test.amount, test.weights, err)
		}
		if got := sum(parts); got != test.amount {
			t.Errorf("Allocate(%d, %v) = %v summing to %d",
				test.amount, test.weights, parts, got)
		}
	}
}

func TestTheClassicExample(t *testing.T) {
	// 100.00 split three ways is 33.34, 33.33, 33.33 — not three of 33.33 and a penny nobody
	// owns.
	parts, err := round.Allocate(10_000, []int64{1, 1, 1})
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if parts[0] != 3_334 || parts[1] != 3_333 || parts[2] != 3_333 {
		t.Fatalf("parts = %v, want [3334 3333 3333]", parts)
	}
}

func TestAllZeroWeightsSpreadEvenly(t *testing.T) {
	// Nothing to weigh by. Spreading evenly is the only defensible answer, and it must still tie.
	parts, err := round.Allocate(10, []int64{0, 0, 0, 0})
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if sum(parts) != 10 {
		t.Fatalf("parts = %v, summing to %d, want 10", parts, sum(parts))
	}
	for _, part := range parts {
		if part < 2 || part > 3 {
			t.Errorf("parts = %v, which is not an even spread", parts)
		}
	}
}

func TestTheCallersWeightsAreNotRewritten(t *testing.T) {
	// # A real defect the extraction fixed
	//
	// The version this was lifted from implemented the even-spread case by writing 1 into the
	// caller's slice. A caller that still needed those weights — to allocate a second charge
	// across the same lines, which is exactly what landed costs do — got them back full of ones,
	// and every charge after the first was spread evenly regardless of value.
	weights := []int64{0, 0, 0}
	if _, err := round.Allocate(9, weights); err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	for i, weight := range weights {
		if weight != 0 {
			t.Fatalf("the caller's weights were rewritten: index %d became %d", i, weight)
		}
	}
}

func TestAllocationRefusesWhatItCannotDivide(t *testing.T) {
	if _, err := round.Allocate(100, nil); !errors.Is(err, round.ErrNoWeights) {
		t.Errorf("err = %v, want ErrNoWeights", err)
	}
	if _, err := round.Allocate(100, []int64{1, -1}); !errors.Is(err, round.ErrNegativeWeight) {
		t.Errorf("err = %v, want ErrNegativeWeight", err)
	}
}

func TestNoSingleLineCollectsEveryLeftover(t *testing.T) {
	// Each recipient drops out after taking a unit, so the remainder is spread rather than piled
	// onto whichever line the loop happened to reach first.
	parts, err := round.Allocate(10, []int64{1, 1, 1, 1})
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if sum(parts) != 10 {
		t.Fatalf("parts = %v summing to %d", parts, sum(parts))
	}
	for _, part := range parts {
		if part > 3 {
			t.Errorf("parts = %v — one line collected the leftovers", parts)
		}
	}
}
