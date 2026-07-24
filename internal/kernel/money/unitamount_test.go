package money_test

import (
	"errors"
	"math"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/quantity"
	"github.com/mizan-erp/mizan/internal/kernel/round"
)

func pcs(t *testing.T) quantity.Unit {
	t.Helper()
	u, err := quantity.NewUnit("PCS", "count", quantity.FactorScale, false)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func kg(t *testing.T) quantity.Unit {
	t.Helper()
	u, err := quantity.NewUnit("KG", "weight", 1000*quantity.FactorScale, true) // reference = gram
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestLineExtensionBusinessCases(t *testing.T) {
	usd := mustCur(t, "USD", 2)
	cases := []struct {
		name      string
		priceStr  string
		qtyStr    string
		unit      func(*testing.T) quantity.Unit
		mode      round.RoundingMode
		wantMinor int64
	}{
		{"3 pieces at 1.50", "1.50", "3", pcs, round.HalfAwayFromZero, 450},
		{"0.375 kg at 2.00", "2.00", "0.375", kg, round.HalfAwayFromZero, 75},
		{"tie rounds away", "0.325", "1", pcs, round.HalfAwayFromZero, 33}, // 32.5 → 33
		{"tie rounds to even", "0.325", "1", pcs, round.HalfToEven, 32},    // 32.5 → 32
	}
	for _, c := range cases {
		price, err := money.ParseUnitAmount(usd, c.priceStr)
		if err != nil {
			t.Fatalf("%s: price: %v", c.name, err)
		}
		qty, err := quantity.Parse(c.unit(t), c.qtyStr)
		if err != nil {
			t.Fatalf("%s: qty: %v", c.name, err)
		}
		got, err := money.LineExtension(price, qty, c.mode)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got.Minor() != c.wantMinor {
			t.Errorf("%s: got %d, want %d", c.name, got.Minor(), c.wantMinor)
		}
	}
}

// TestLineExtensionSubCentPrecision is the reason UnitAmount exists: a unit price
// below one minor unit must survive until the single rounding. If the price were
// rounded to cents first (0.001234 → 0.00), the extension would be 0 — catastrophic
// for inventory valuation. LineExtension keeps the precision and yields 1.23.
func TestLineExtensionSubCentPrecision(t *testing.T) {
	usd := mustCur(t, "USD", 2)
	price, err := money.ParseUnitAmount(usd, "0.001234") // 0.1234 cents per unit
	if err != nil {
		t.Fatal(err)
	}
	qty, err := quantity.Parse(pcs(t), "1000")
	if err != nil {
		t.Fatal(err)
	}
	got, err := money.LineExtension(price, qty, round.HalfAwayFromZero)
	if err != nil {
		t.Fatal(err)
	}
	if got.Minor() != 123 { // 0.001234 × 1000 = 1.234 → 1.23
		t.Errorf("sub-cent extension = %d, want 123", got.Minor())
	}
}

func TestLineExtensionOverflowAndErrors(t *testing.T) {
	usd := mustCur(t, "USD", 2)
	// Huge price × huge qty: the big.Int intermediate does not overflow, but the
	// final result exceeds int64 → ErrOverflow (not a wrong number). Use a fractional
	// unit so the extreme micro value is a valid quantity.
	price := money.UnitFromMicro(usd, math.MaxInt64)
	qty, err := quantity.FromMicro(kg(t), math.MaxInt64)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := money.LineExtension(price, qty, round.HalfAwayFromZero); !errors.Is(err, money.ErrOverflow) {
		t.Errorf("want ErrOverflow, got %v", err)
	}
	// zero-value currency
	if _, err := money.LineExtension(money.UnitAmount{}, qty, round.HalfAwayFromZero); !errors.Is(err, money.ErrNoCurrency) {
		t.Errorf("want ErrNoCurrency, got %v", err)
	}
}

func TestParseUnitAmount(t *testing.T) {
	usd := mustCur(t, "USD", 2)
	u, err := money.ParseUnitAmount(usd, "1.234567") // 6 decimals of major unit
	if err != nil || u.Micro() != 1234567 {
		t.Errorf("ParseUnitAmount = %d (%v), want 1234567", u.Micro(), err)
	}
	if _, err := money.ParseUnitAmount(usd, "1.2345678"); !errors.Is(err, money.ErrParse) {
		t.Errorf("7 decimals: want ErrParse, got %v", err)
	}
}
