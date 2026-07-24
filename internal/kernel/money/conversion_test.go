package money_test

import (
	"errors"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/round"
)

// TestMulRateAcrossScales verifies the exact conversion formula across currencies
// with different decimal places — the case most prone to scale bugs.
func TestMulRateAcrossScales(t *testing.T) {
	usd := mustCur(t, "USD", 2)
	syp := mustCur(t, "SYP", 0)
	kwd := mustCur(t, "KWD", 3)

	cases := []struct {
		name      string
		from      money.Currency
		amount    int64 // minor
		rateStr   string
		to        money.Currency
		wantMinor int64
	}{
		// 100.00 USD × 15000 = 1,500,000 SYP (2 decimals → 0 decimals)
		{"USD→SYP hyperinflation", usd, 10000, "15000", syp, 1500000},
		// 1,500,000 SYP × (1/15000) → 100.00 USD (0 → 2 decimals)
		{"SYP→USD reverse", syp, 1500000, "0.000066667", usd, 10000},
		// 100.00 USD × 0.305 = 30.500 KWD (2 → 3 decimals)
		{"USD→KWD more decimals", usd, 10000, "0.305", kwd, 30500},
		// 10.000 KWD × 3.28 = 32.80 USD (3 → 2 decimals)
		{"KWD→USD fewer decimals", kwd, 10000, "3.28", usd, 3280},
	}
	for _, c := range cases {
		rate, err := money.ParseRate(c.rateStr)
		if err != nil {
			t.Fatalf("%s: ParseRate(%q): %v", c.name, c.rateStr, err)
		}
		got, err := money.FromMinor(c.from, c.amount).MulRate(rate, c.to, round.HalfAwayFromZero)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got.Minor() != c.wantMinor {
			t.Errorf("%s: got %d, want %d", c.name, got.Minor(), c.wantMinor)
		}
		if !got.Currency().Equal(c.to) {
			t.Errorf("%s: result currency = %s, want %s", c.name, got.Currency(), c.to)
		}
	}
}

func TestMulRateErrors(t *testing.T) {
	usd := mustCur(t, "USD", 2)
	syp := mustCur(t, "SYP", 0)
	m := money.FromMinor(usd, 100)
	if _, err := m.MulRate(money.RateFromNano(0), syp, round.HalfAwayFromZero); !errors.Is(err, money.ErrInvalidRate) {
		t.Errorf("zero rate: want ErrInvalidRate, got %v", err)
	}
	if _, err := m.MulRate(money.RateFromNano(-5), syp, round.HalfAwayFromZero); !errors.Is(err, money.ErrInvalidRate) {
		t.Errorf("negative rate: want ErrInvalidRate, got %v", err)
	}
	if _, err := m.MulRate(money.RateFromNano(1e9), money.Currency{}, round.HalfAwayFromZero); !errors.Is(err, money.ErrNoCurrency) {
		t.Errorf("zero target currency: want ErrNoCurrency, got %v", err)
	}
}

func TestRateInvertAndMul(t *testing.T) {
	// 1/2.0 = 0.5
	r, _ := money.ParseRate("2")
	inv, err := r.Invert(round.HalfAwayFromZero)
	if err != nil || inv.Nano() != 500_000_000 {
		t.Errorf("Invert(2) = %d, want 500000000 (%v)", inv.Nano(), err)
	}
	// compose 2.0 × 3.0 = 6.0
	a, _ := money.ParseRate("2")
	b, _ := money.ParseRate("3")
	prod, _ := a.Mul(b, round.HalfAwayFromZero)
	if prod.Nano() != 6_000_000_000 {
		t.Errorf("2×3 = %d, want 6000000000", prod.Nano())
	}
	// invert of zero errors
	if _, err := money.RateFromNano(0).Invert(round.HalfAwayFromZero); !errors.Is(err, money.ErrInvalidRate) {
		t.Errorf("Invert(0): want ErrInvalidRate, got %v", err)
	}
}

func TestPercentConstructorsAndString(t *testing.T) {
	if money.PercentFromMicro(150000).Micro() != 150000 {
		t.Error("PercentFromMicro")
	}
	if money.PercentFromBasisPoints(1500).Micro() != 150000 {
		t.Error("PercentFromBasisPoints 1500bps should be 15%")
	}
	p, _ := money.PercentFromRatio(15, 100, round.HalfAwayFromZero)
	if p.Micro() != 150000 {
		t.Errorf("PercentFromRatio 15/100 = %d micro, want 150000", p.Micro())
	}
	if money.PercentFromBasisPoints(1500).String() != "15%" {
		t.Errorf("String = %q, want 15%%", money.PercentFromBasisPoints(1500).String())
	}
	if money.PercentFromMicro(75000).String() != "7.5%" {
		t.Errorf("String = %q, want 7.5%%", money.PercentFromMicro(75000).String())
	}
	if _, err := money.PercentFromRatio(1, 0, round.HalfAwayFromZero); err == nil {
		t.Error("PercentFromRatio with zero denominator should error")
	}
}
