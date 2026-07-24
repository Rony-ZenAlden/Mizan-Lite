package money_test

import (
	"errors"
	"math"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/round"
)

func TestParseAndString(t *testing.T) {
	cases := []struct {
		cur       func(*testing.T) money.Currency
		in        string
		wantMinor int64
		wantStr   string
	}{
		{usd, "19.99", 1999, "USD 19.99"},
		{usd, "0", 0, "USD 0.00"},
		{usd, "-5.5", -550, "USD -5.50"},
		{usd, "1000000", 100000000, "USD 1000000.00"},
		{syp, "1500", 1500, "SYP 1500"},   // 0-decimal currency
		{kwd, "1.234", 1234, "KWD 1.234"}, // 3-decimal currency
		{usd, "+3.05", 305, "USD 3.05"},   // explicit plus
	}
	for _, c := range cases {
		cur := c.cur(t)
		m, err := money.Parse(cur, c.in)
		if err != nil {
			t.Errorf("Parse(%s, %q): %v", cur, c.in, err)
			continue
		}
		if m.Minor() != c.wantMinor {
			t.Errorf("Parse(%s, %q).Minor() = %d, want %d", cur, c.in, m.Minor(), c.wantMinor)
		}
		if m.String() != c.wantStr {
			t.Errorf("Parse(%s, %q).String() = %q, want %q", cur, c.in, m.String(), c.wantStr)
		}
	}
}

func TestParseErrors(t *testing.T) {
	u := usd(t)
	bad := []string{"", "abc", "1.2.3", "1.999", "1,000", "1..0", "-", "."}
	for _, s := range bad {
		if _, err := money.Parse(u, s); !errors.Is(err, money.ErrParse) {
			t.Errorf("Parse(USD, %q): want ErrParse, got %v", s, err)
		}
	}
	// too many decimals for the currency
	if _, err := money.Parse(syp(t), "1.5"); !errors.Is(err, money.ErrParse) {
		t.Errorf("Parse(SYP, 1.5): want ErrParse (0-decimal currency), got %v", err)
	}
	// zero-value currency
	if _, err := money.Parse(money.Currency{}, "1"); !errors.Is(err, money.ErrNoCurrency) {
		t.Errorf("Parse(zero currency): want ErrNoCurrency, got %v", err)
	}
}

func TestAddSub(t *testing.T) {
	u := usd(t)
	a := money.FromMinor(u, 1000)
	b := money.FromMinor(u, 250)

	if got, _ := a.Add(b); got.Minor() != 1250 {
		t.Errorf("Add: got %d, want 1250", got.Minor())
	}
	if got, _ := a.Sub(b); got.Minor() != 750 {
		t.Errorf("Sub: got %d, want 750", got.Minor())
	}

	// currency mismatch
	if _, err := a.Add(money.FromMinor(syp(t), 1)); !errors.Is(err, money.ErrCurrencyMismatch) {
		t.Errorf("Add mismatch: want ErrCurrencyMismatch, got %v", err)
	}
	// zero-value operand
	if _, err := a.Add(money.Money{}); !errors.Is(err, money.ErrNoCurrency) {
		t.Errorf("Add zero-value: want ErrNoCurrency, got %v", err)
	}
}

func TestAddSubOverflow(t *testing.T) {
	u := usd(t)
	atMax := money.FromMinor(u, math.MaxInt64)
	if _, err := atMax.Add(money.FromMinor(u, 1)); !errors.Is(err, money.ErrOverflow) {
		t.Errorf("Add overflow: want ErrOverflow, got %v", err)
	}
	nearMin := money.FromMinor(u, math.MinInt64+1) // MinInt64 itself is outside the domain
	if _, err := nearMin.Sub(money.FromMinor(u, 2)); !errors.Is(err, money.ErrOverflow) {
		t.Errorf("Sub overflow: want ErrOverflow, got %v", err)
	}
}

func TestMulInt(t *testing.T) {
	u := usd(t)
	price := money.FromMinor(u, 199) // 1.99
	got, err := price.MulInt(3)
	if err != nil || got.Minor() != 597 {
		t.Errorf("MulInt: got %d (%v), want 597", got.Minor(), err)
	}
	if _, err := money.FromMinor(u, math.MaxInt64).MulInt(2); !errors.Is(err, money.ErrOverflow) {
		t.Errorf("MulInt overflow: want ErrOverflow, got %v", err)
	}
}

func TestNegAbsSign(t *testing.T) {
	u := usd(t)
	m := money.FromMinor(u, -500)
	if m.Neg().Minor() != 500 {
		t.Error("Neg wrong")
	}
	if m.Abs().Minor() != 500 {
		t.Error("Abs wrong")
	}
	if m.Sign() != -1 || money.Zero(u).Sign() != 0 || m.Neg().Sign() != 1 {
		t.Error("Sign wrong")
	}
	if !m.IsNegative() || !money.Zero(u).IsZero() {
		t.Error("predicates wrong")
	}
}

func TestCmpEqual(t *testing.T) {
	u := usd(t)
	a := money.FromMinor(u, 100)
	b := money.FromMinor(u, 200)
	if c, _ := a.Cmp(b); c != -1 {
		t.Error("Cmp <")
	}
	if c, _ := b.Cmp(a); c != 1 {
		t.Error("Cmp >")
	}
	if c, _ := a.Cmp(money.FromMinor(u, 100)); c != 0 {
		t.Error("Cmp ==")
	}
	if !a.Equal(money.FromMinor(u, 100)) || a.Equal(b) {
		t.Error("Equal")
	}
	if _, err := a.Cmp(money.FromMinor(syp(t), 100)); !errors.Is(err, money.ErrCurrencyMismatch) {
		t.Error("Cmp mismatch should error")
	}
}

func TestMulPercent(t *testing.T) {
	u := usd(t)
	// 100.00 × 15% = 15.00
	got, err := money.FromMinor(u, 10000).MulPercent(money.PercentFromBasisPoints(1500), round.HalfAwayFromZero)
	if err != nil || got.Minor() != 1500 {
		t.Errorf("15%% of 100: got %d (%v), want 1500", got.Minor(), err)
	}
	// 10.00 × 8.25% = 0.825 → rounds to 0.83 (half away)
	p, err := money.PercentFromRatio(825, 10000, round.HalfAwayFromZero)
	if err != nil {
		t.Fatalf("PercentFromRatio: %v", err)
	}
	got, _ = money.FromMinor(u, 1000).MulPercent(p, round.HalfAwayFromZero)
	if got.Minor() != 83 {
		t.Errorf("8.25%% of 10: got %d, want 83", got.Minor())
	}
}

// TestAllocate covers the core exactness guarantee with worked business examples.
func TestAllocate(t *testing.T) {
	u := usd(t)
	cases := []struct {
		name    string
		minor   int64
		weights []int64
		want    []int64
	}{
		{"100 split 3 ways", 10000, []int64{1, 1, 1}, []int64{3334, 3333, 3333}},
		{"even split", 1000, []int64{1, 1}, []int64{500, 500}},
		{"weighted 70/30", 1001, []int64{70, 30}, []int64{701, 300}},
		{"one cent, three ways", 1, []int64{1, 1, 1}, []int64{1, 0, 0}},
		{"negative amount", -10000, []int64{1, 1, 1}, []int64{-3334, -3333, -3333}},
		{"zero weights among nonzero", 100, []int64{0, 1, 0}, []int64{0, 100, 0}},
	}
	for _, c := range cases {
		parts, err := money.FromMinor(u, c.minor).Allocate(c.weights)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		var sum int64
		for i, p := range parts {
			if p.Minor() != c.want[i] {
				t.Errorf("%s: part[%d] = %d, want %d", c.name, i, p.Minor(), c.want[i])
			}
			sum += p.Minor()
		}
		if sum != c.minor {
			t.Errorf("%s: parts sum to %d, want %d (conservation violated)", c.name, sum, c.minor)
		}
	}
}

func TestAllocateErrors(t *testing.T) {
	u := usd(t)
	m := money.FromMinor(u, 100)
	if _, err := m.Allocate(nil); !errors.Is(err, money.ErrInvalidWeights) {
		t.Error("nil weights should error")
	}
	if _, err := m.Allocate([]int64{0, 0}); !errors.Is(err, money.ErrInvalidWeights) {
		t.Error("all-zero weights should error")
	}
	if _, err := m.Allocate([]int64{1, -1}); !errors.Is(err, money.ErrInvalidWeights) {
		t.Error("negative weight should error")
	}
}
