package money_test

import (
	"errors"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/round"
)

// test currencies used across the money tests.
func usd(t *testing.T) money.Currency { return mustCur(t, "USD", 2) }
func syp(t *testing.T) money.Currency { return mustCur(t, "SYP", 0) } // Syrian pound: 0 decimals
func kwd(t *testing.T) money.Currency { return mustCur(t, "KWD", 3) }

func mustCur(t *testing.T, code string, dec uint8) money.Currency {
	t.Helper()
	c, err := money.NewCurrency(code, dec, round.HalfAwayFromZero)
	if err != nil {
		t.Fatalf("NewCurrency(%s): %v", code, err)
	}
	return c
}

func TestNewCurrency(t *testing.T) {
	if _, err := money.NewCurrency("", 2, round.HalfAwayFromZero); !errors.Is(err, money.ErrInvalidCurrency) {
		t.Errorf("empty code: want ErrInvalidCurrency, got %v", err)
	}
	if _, err := money.NewCurrency("USD", money.MaxDecimals+1, round.HalfAwayFromZero); !errors.Is(err, money.ErrInvalidCurrency) {
		t.Errorf("too many decimals: want ErrInvalidCurrency, got %v", err)
	}
	if _, err := money.NewCurrency("USD", 2, round.RoundingMode(99)); !errors.Is(err, money.ErrInvalidCurrency) {
		t.Errorf("bad rounding: want ErrInvalidCurrency, got %v", err)
	}
	// 18 decimals (crypto) is allowed.
	if _, err := money.NewCurrency("ETH", 18, round.HalfAwayFromZero); err != nil {
		t.Errorf("18 decimals should be valid: %v", err)
	}
}

func TestCurrencyIdentity(t *testing.T) {
	a := mustCur(t, "USD", 2)
	b := mustCur(t, "USD", 2)
	c := mustCur(t, "SYP", 0)
	if !a.Equal(b) {
		t.Error("same code should be equal")
	}
	if a.Equal(c) {
		t.Error("different code should not be equal")
	}
	if (money.Currency{}).IsZero() != true {
		t.Error("zero value should be IsZero")
	}
	if a.Decimals() != 2 || a.Code() != "USD" {
		t.Error("accessors wrong")
	}
}
