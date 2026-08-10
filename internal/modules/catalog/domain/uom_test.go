package domain_test

import (
	"errors"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/round"
	"github.com/mizan-erp/mizan/internal/modules/catalog/domain"
)

// The weight category: grams is the reference, so a kilogram is 1000 of them.
var (
	weight = id.ID("weight")
	length = id.ID("length")
	count  = id.ID("count")
)

func unit(categoryID id.ID, code string, factorNano int64) domain.Unit {
	return domain.Unit{
		ID: id.ID(code), CategoryID: categoryID, Code: code, Name: code, Symbol: code,
		FactorNano: factorNano, IsReference: factorNano == 1_000_000_000,
		AllowsFractional: true, RoundingPrecision: 1000, DisplayDecimals: 3, IsActive: true,
	}
}

func countUnit(code string) domain.Unit {
	u := unit(count, code, 1_000_000_000)
	u.AllowsFractional = false
	u.RoundingPrecision = 1_000_000
	u.DisplayDecimals = 0
	return u
}

// ── conversion within a category (§B.2) ─────────────────────────────────────────

func TestConversionWithinACategory(t *testing.T) {
	gram := unit(weight, "G", 1_000_000_000)          // reference
	kilogram := unit(weight, "KG", 1_000_000_000_000) // 1000 g

	cases := []struct {
		name     string
		micro    int64
		from, to domain.Unit
		want     int64
	}{
		{"a kilogram is a thousand grams", 1_000_000, kilogram, gram, 1_000_000_000},
		{"and back again", 1_000_000_000, gram, kilogram, 1_000_000},
		{"half a kilo", 500_000, kilogram, gram, 500_000_000},
		{"the same unit is untouched", 1_234_567, kilogram, kilogram, 1_234_567},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := domain.Convert(tc.micro, tc.from, tc.to, round.HalfAwayFromZero)
			if err != nil {
				t.Fatalf("Convert: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// A quantity that converts out and back must come home unchanged. Anything else loses stock
// every time a document is entered in a unit other than the one it is held in.
func TestConversionRoundTrips(t *testing.T) {
	gram := unit(weight, "G", 1_000_000_000)
	kilogram := unit(weight, "KG", 1_000_000_000_000)

	for _, micro := range []int64{1_000, 1_000_000, 2_500_000, 999_000_000} {
		grams, err := domain.Convert(micro, kilogram, gram, round.HalfAwayFromZero)
		if err != nil {
			t.Fatalf("to grams: %v", err)
		}
		back, err := domain.Convert(grams, gram, kilogram, round.HalfAwayFromZero)
		if err != nil {
			t.Fatalf("back: %v", err)
		}
		if back != micro {
			t.Errorf("%d kg → %d g → %d kg", micro, grams, back)
		}
	}
}

// `kg → m` is a domain error, NOT a silent zero. A silent zero in a stock system is a quantity
// that vanishes without anyone noticing until a count.
func TestCrossCategoryConversionIsRefused(t *testing.T) {
	kilogram := unit(weight, "KG", 1_000_000_000_000)
	metre := unit(length, "M", 1_000_000_000)

	_, err := domain.Convert(1_000_000, kilogram, metre, round.HalfAwayFromZero)
	if err == nil {
		t.Fatal("kilograms were converted to metres")
	}
	if code := errs.CodeOf(err); code != domain.CodeCrossCategory {
		t.Errorf("code = %q, want %q", code, domain.CodeCrossCategory)
	}
	// The message names both units, because "cannot convert KG to M" is actionable and
	// "category mismatch" is not.
	typed, _ := errs.AsError(err)
	if typed.Params["from"] != "KG" || typed.Params["to"] != "M" {
		t.Errorf("params = %v, want the two unit codes", typed.Params)
	}

	// And it was refused BEFORE any arithmetic was attempted.
	//
	// This assertion exists because the first mutation drill for this rule PASSED: the kernel
	// refuses cross-category conversion as well, and Convert wraps that refusal with the same
	// code and the same two params, so deleting the domain's own check changed nothing a test
	// could observe. A wrapped error has a cause; this one must not, which pins the check as the
	// precondition it is documented to be rather than a fallback the kernel happens to cover.
	if errors.Unwrap(err) != nil {
		t.Error("the units reached the arithmetic before being refused")
	}
}

// ── fractional control (§B.4) ───────────────────────────────────────────────────

// You can sell 1.5 kg of timber; you cannot sell 0.5 of a chair.
func TestAFractionalCountIsRefused(t *testing.T) {
	chair := countUnit("PCS")

	if _, err := domain.Normalise(500_000, chair); err == nil {
		t.Fatal("half a chair was accepted")
	} else if code := errs.CodeOf(err); code != domain.CodeFractionalUnit {
		t.Errorf("code = %q, want %q", code, domain.CodeFractionalUnit)
	}

	// Whole ones are fine.
	for _, whole := range []int64{1_000_000, 12_000_000} {
		if got, err := domain.Normalise(whole, chair); err != nil || got != whole {
			t.Errorf("Normalise(%d) = %d, %v; want it unchanged", whole, got, err)
		}
	}
}

// Refused rather than ROUNDED, deliberately: rounding up would invent stock and rounding down
// would lose a sale, and neither is a decision code should make for a user.
func TestAFractionalCountIsNotSilentlyRounded(t *testing.T) {
	chair := countUnit("PCS")

	for _, fractional := range []int64{1, 499_999, 500_000, 1_500_000} {
		if got, err := domain.Normalise(fractional, chair); err == nil {
			t.Errorf("Normalise(%d) returned %d instead of refusing", fractional, got)
		}
	}
}

// A scale reading is rounded ONCE, at entry — not repeatedly in later calculations, which is
// how two reports of the same delivery come to disagree in the third decimal.
func TestAScaleReadingIsRoundedOnEntry(t *testing.T) {
	kilogram := unit(weight, "KG", 1_000_000_000_000) // precision 0.001

	cases := []struct{ reading, want int64 }{
		{1_437_238, 1_437_000}, // 1.437238 → 1.437
		{1_437_500, 1_438_000}, // exactly half rounds away from zero
		{1_437_501, 1_438_000},
		{1_437_499, 1_437_000},
		{1_437_000, 1_437_000}, // already exact
	}
	for _, tc := range cases {
		got, err := domain.Normalise(tc.reading, kilogram)
		if err != nil {
			t.Fatalf("Normalise(%d): %v", tc.reading, err)
		}
		if got != tc.want {
			t.Errorf("Normalise(%d) = %d, want %d", tc.reading, got, tc.want)
		}
	}
}

func TestANegativeQuantityIsRefused(t *testing.T) {
	kilogram := unit(weight, "KG", 1_000_000_000_000)

	if _, err := domain.Normalise(-1_000, kilogram); err == nil {
		t.Fatal("a negative quantity was accepted")
	} else if code := errs.CodeOf(err); code != domain.CodeNegativeQuantity {
		t.Errorf("code = %q, want %q", code, domain.CodeNegativeQuantity)
	}
}

// ── the category invariant ──────────────────────────────────────────────────────

// Conversion goes THROUGH the reference, so a category with none makes every conversion
// impossible and one with two makes every conversion ambiguous.
func TestACategoryNeedsExactlyOneReference(t *testing.T) {
	gram := unit(weight, "G", 1_000_000_000)
	kilogram := unit(weight, "KG", 1_000_000_000_000)

	if err := domain.ValidateCategoryUnits([]domain.Unit{gram, kilogram}); err != nil {
		t.Fatalf("a valid category was refused: %v", err)
	}

	if err := domain.ValidateCategoryUnits([]domain.Unit{kilogram}); err == nil {
		t.Error("a category with no reference was accepted")
	} else if code := errs.CodeOf(err); code != domain.CodeNoReferenceUnit {
		t.Errorf("code = %q, want %q", code, domain.CodeNoReferenceUnit)
	}

	second := unit(weight, "G2", 1_000_000_000)
	if err := domain.ValidateCategoryUnits([]domain.Unit{gram, second}); err == nil {
		t.Error("a category with two references was accepted")
	}
}

// A reference whose factor is not 1 makes every conversion in the category wrong by that
// factor, consistently — the hardest kind of wrong to notice.
func TestAReferenceMustHaveAFactorOfOne(t *testing.T) {
	broken := unit(weight, "G", 1_000_000_000)
	broken.FactorNano = 2_000_000_000
	broken.IsReference = true

	if err := domain.ValidateCategoryUnits([]domain.Unit{broken}); err == nil {
		t.Fatal("a reference unit with a factor of two was accepted")
	}
}

// ── construction ────────────────────────────────────────────────────────────────

func TestNewUnitRefusesAnUnusableFactor(t *testing.T) {
	identifier, _ := id.New()

	for _, factor := range []int64{0, -1_000_000_000} {
		// Zero makes every conversion produce nothing; negative makes quantities change sign.
		// Both are silent, which is why neither is representable.
		if _, err := domain.NewUnit(identifier, weight, "KG", "Kilogram", "kg", factor); err == nil {
			t.Errorf("a factor of %d was accepted", factor)
		}
	}
}

func TestNewUnitNormalisesTheCode(t *testing.T) {
	identifier, _ := id.New()

	built, err := domain.NewUnit(identifier, weight, "  kg  ", "Kilogram", "kg", 1_000_000_000_000)
	if err != nil {
		t.Fatalf("NewUnit: %v", err)
	}
	// Codes are matched exactly by seeds, imports, and barcodes; "kg" and "KG" being two units
	// would be a duplicate nobody spots until a conversion fails.
	if built.Code != "KG" {
		t.Errorf("code = %q, want KG", built.Code)
	}
}
