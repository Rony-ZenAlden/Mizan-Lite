package money_test

import (
	"errors"
	"math"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/round"
)

func TestValueAccessorsAndPredicates(t *testing.T) {
	u := usd(t)

	// Currency
	if u.Rounding() != round.HalfAwayFromZero || u.String() != "USD" {
		t.Error("currency accessors")
	}
	if (money.Currency{}).String() != "<nil>" {
		t.Error("zero currency String")
	}

	// Money
	if !money.Zero(u).Valid() || (money.Money{}).Valid() {
		t.Error("Money.Valid")
	}
	if (money.Money{}).String() == "" {
		t.Error("zero-currency Money.String should not be empty")
	}

	// Rate
	r := money.RateFromNano(1_500_000_000)
	if r.IsZero() || r.Sign() != 1 || r.String() != "1.500000000" {
		t.Errorf("rate accessors: zero=%v sign=%d str=%q", r.IsZero(), r.Sign(), r.String())
	}
	if !money.RateFromNano(0).IsZero() || money.RateFromNano(-1).Sign() != -1 {
		t.Error("rate zero/negative")
	}

	// Percent
	p := money.PercentFromMicro(150000)
	if p.IsZero() || p.Sign() != 1 {
		t.Error("percent accessors")
	}
	if !money.PercentFromMicro(0).IsZero() || money.PercentFromMicro(-1).Sign() != -1 {
		t.Error("percent zero/negative")
	}

	// UnitAmount
	ua := money.UnitFromMicro(u, 1234567)
	if ua.IsZero() || !ua.Currency().Equal(u) || ua.String() != "USD 1.234567/u" {
		t.Errorf("unit amount accessors: %q", ua.String())
	}
	if !money.UnitFromMicro(u, 0).IsZero() {
		t.Error("unit amount IsZero")
	}
	if (money.UnitAmount{}).String() == "" {
		t.Error("zero unit amount String")
	}
}

// Overflow on the rounding-based multipliers must surface as ErrOverflow, not a
// wrong number.
func TestMultiplierOverflow(t *testing.T) {
	u := usd(t)
	big := money.FromMinor(u, math.MaxInt64)

	if _, err := big.MulPercent(money.PercentFromMicro(2_000_000), round.HalfAwayFromZero); !errors.Is(err, money.ErrOverflow) {
		t.Errorf("MulPercent overflow: want ErrOverflow, got %v", err)
	}
	// Convert a huge amount at a large rate into a higher-precision currency → overflow.
	if _, err := big.MulRate(money.RateFromNano(1_000_000_000_000_000), kwd(t), round.HalfAwayFromZero); !errors.Is(err, money.ErrOverflow) {
		t.Errorf("MulRate overflow: want ErrOverflow, got %v", err)
	}
	// MulInt on a zero-value money errors.
	if _, err := (money.Money{}).MulInt(2); !errors.Is(err, money.ErrNoCurrency) {
		t.Errorf("MulInt zero-value: want ErrNoCurrency, got %v", err)
	}
	// ParseRate/ParseUnitAmount reject malformed input.
	if _, err := money.ParseRate("x"); err == nil {
		t.Error("ParseRate should reject 'x'")
	}
}
