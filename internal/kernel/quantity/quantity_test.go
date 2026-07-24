package quantity_test

import (
	"errors"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/quantity"
	"github.com/mizan-erp/mizan/internal/kernel/round"
)

// Weight-category units (reference = gram).
func gram(t *testing.T) quantity.Unit { return mustUnit(t, "G", "weight", quantity.FactorScale, true) }
func kg(t *testing.T) quantity.Unit {
	return mustUnit(t, "KG", "weight", 1000*quantity.FactorScale, true)
}
func mg(t *testing.T) quantity.Unit {
	return mustUnit(t, "MG", "weight", quantity.FactorScale/1000, true)
}
func pound(t *testing.T) quantity.Unit {
	return mustUnit(t, "LB", "weight", 453592*(quantity.FactorScale/1000), true)
}                                      // 453.592 g
func pcs(t *testing.T) quantity.Unit   { return mustUnit(t, "PCS", "count", quantity.FactorScale, false) }
func litre(t *testing.T) quantity.Unit { return mustUnit(t, "L", "volume", quantity.FactorScale, true) }

func mustUnit(t *testing.T, code, cat string, factor int64, frac bool) quantity.Unit {
	t.Helper()
	u, err := quantity.NewUnit(code, cat, factor, frac)
	if err != nil {
		t.Fatalf("NewUnit(%s): %v", code, err)
	}
	return u
}

func TestNewUnitValidation(t *testing.T) {
	if _, err := quantity.NewUnit("", "weight", quantity.FactorScale, true); !errors.Is(err, quantity.ErrInvalidUnit) {
		t.Error("empty code should error")
	}
	if _, err := quantity.NewUnit("KG", "", quantity.FactorScale, true); !errors.Is(err, quantity.ErrInvalidUnit) {
		t.Error("empty category should error")
	}
	if _, err := quantity.NewUnit("KG", "weight", 0, true); !errors.Is(err, quantity.ErrInvalidUnit) {
		t.Error("non-positive factor should error")
	}
}

func TestParseAndFractionalRule(t *testing.T) {
	// fractional weight is fine
	q, err := quantity.Parse(kg(t), "2.5")
	if err != nil || q.Micro() != 2_500_000 {
		t.Errorf("Parse(2.5 kg) = %d (%v), want 2500000", q.Micro(), err)
	}
	// fractional count is rejected
	if _, err := quantity.Parse(pcs(t), "1.5"); !errors.Is(err, quantity.ErrFractionNotAllowed) {
		t.Errorf("1.5 pcs: want ErrFractionNotAllowed, got %v", err)
	}
	// whole count is fine
	if q, err := quantity.Parse(pcs(t), "3"); err != nil || q.Micro() != 3_000_000 {
		t.Errorf("3 pcs = %d (%v), want 3000000", q.Micro(), err)
	}
	// too many decimals
	if _, err := quantity.Parse(kg(t), "1.1234567"); !errors.Is(err, quantity.ErrParse) {
		t.Errorf("7 decimals: want ErrParse, got %v", err)
	}
	// FromMicro fractional rule
	if _, err := quantity.FromMicro(pcs(t), 1_500_000); !errors.Is(err, quantity.ErrFractionNotAllowed) {
		t.Errorf("FromMicro fractional pcs: want ErrFractionNotAllowed, got %v", err)
	}
}

func TestConversionExactWithinCategory(t *testing.T) {
	cases := []struct {
		name      string
		from      func(*testing.T) quantity.Unit
		amountStr string
		to        func(*testing.T) quantity.Unit
		wantMicro int64
	}{
		{"kg→g", kg, "2.5", gram, 2_500_000_000},  // 2.5 kg = 2500 g
		{"g→kg", gram, "2500", kg, 2_500_000},     // 2500 g = 2.5 kg
		{"kg→mg", kg, "1", mg, 1_000_000_000_000}, // 1 kg = 1,000,000 mg
		{"g→mg", gram, "5", mg, 5_000_000_000},    // 5 g = 5000 mg (×10^6 micro)
	}
	for _, c := range cases {
		q, err := quantity.Parse(c.from(t), c.amountStr)
		if err != nil {
			t.Fatalf("%s: parse: %v", c.name, err)
		}
		got, err := q.ConvertTo(c.to(t), round.HalfAwayFromZero)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got.Micro() != c.wantMicro {
			t.Errorf("%s: got %d, want %d", c.name, got.Micro(), c.wantMicro)
		}
	}
}

// TestConversionRoundTrip checks no drift over repeated conversions.
func TestConversionRoundTrip(t *testing.T) {
	start, _ := quantity.Parse(kg(t), "7.35")
	g, err := start.ConvertTo(gram(t), round.HalfAwayFromZero)
	if err != nil {
		t.Fatal(err)
	}
	back, err := g.ConvertTo(kg(t), round.HalfAwayFromZero)
	if err != nil {
		t.Fatal(err)
	}
	if !back.Equal(start) {
		t.Errorf("round trip: %s → %s → %s (drift)", start, g, back)
	}
}

// TestImperialMetric shows multiple unit systems are just data.
func TestImperialMetric(t *testing.T) {
	oneLb, _ := quantity.Parse(pound(t), "1")
	g, err := oneLb.ConvertTo(gram(t), round.HalfAwayFromZero)
	if err != nil {
		t.Fatal(err)
	}
	if g.Micro() != 453_592_000 { // 453.592 g
		t.Errorf("1 lb = %d micro-g, want 453592000", g.Micro())
	}
}

func TestConversionErrors(t *testing.T) {
	kgq, _ := quantity.Parse(kg(t), "1")
	// cross-category
	if _, err := kgq.ConvertTo(litre(t), round.HalfAwayFromZero); !errors.Is(err, quantity.ErrUnitCategory) {
		t.Errorf("kg→litre: want ErrUnitCategory, got %v", err)
	}
	// zero-value unit
	if _, err := kgq.ConvertTo(quantity.Unit{}, round.HalfAwayFromZero); !errors.Is(err, quantity.ErrNoUnit) {
		t.Errorf("convert to zero unit: want ErrNoUnit, got %v", err)
	}
}

func TestAddSubCmpUnitSafety(t *testing.T) {
	a, _ := quantity.Parse(kg(t), "2")
	b, _ := quantity.Parse(kg(t), "0.5")
	if got, _ := a.Add(b); got.Micro() != 2_500_000 {
		t.Errorf("Add = %d, want 2500000", got.Micro())
	}
	if got, _ := a.Sub(b); got.Micro() != 1_500_000 {
		t.Errorf("Sub = %d, want 1500000", got.Micro())
	}
	// mixing units errors
	g, _ := quantity.Parse(gram(t), "500")
	if _, err := a.Add(g); !errors.Is(err, quantity.ErrUnitMismatch) {
		t.Errorf("kg + g: want ErrUnitMismatch, got %v", err)
	}
	if c, _ := a.Cmp(b); c != 1 {
		t.Error("Cmp wrong")
	}
	// zero-value unit
	if _, err := a.Add(quantity.Quantity{}); !errors.Is(err, quantity.ErrNoUnit) {
		t.Errorf("add zero-value: want ErrNoUnit, got %v", err)
	}
}

func TestStringAndPredicates(t *testing.T) {
	q, _ := quantity.Parse(kg(t), "2.5")
	if q.String() != "2.500000 KG" {
		t.Errorf("String = %q", q.String())
	}
	if q.Sign() != 1 || q.IsZero() || q.IsNegative() {
		t.Error("predicates wrong")
	}
	if q.Neg().Sign() != -1 {
		t.Error("Neg wrong")
	}
}
