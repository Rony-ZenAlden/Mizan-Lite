package money_test

import (
	"errors"
	"math"
	"math/big"
	"testing"

	"pgregory.net/rapid"

	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/round"
)

// halfUp is round-half-up of a non-negative num/den, computed independently of the kernel's round package.
func halfUp(num, den *big.Int) int64 {
	twice := new(big.Int).Mul(num, big.NewInt(2))
	twice.Add(twice, den)
	return new(big.Int).Quo(twice, new(big.Int).Mul(den, big.NewInt(2))).Int64()
}

// TestHalfALitreAt333At13000Is21645NotTheTwiceRounded21710 is DESIGN §4.4 as a test.
func TestHalfALitreAt333At13000Is21645NotTheTwiceRounded21710(t *testing.T) {
	usd, syp := mustCur(t, "USD", 2), mustCur(t, "SYP", 0)
	litre := mustUnit(t, "l", true)
	price := money.UnitFromMicro(usd, 3_330_000)
	half := mustQtyT(t, litre, 500_000)
	rate := money.RateFromNano(13_000 * 1_000_000_000)

	got, err := money.LineExtensionMulRate(price, half, rate, syp, round.HalfUp)
	if err != nil || got.Minor() != 21_645 || got.Currency().Code() != "SYP" {
		t.Fatalf("got %v, %v; want 21645 SYP", got, err)
	}
	// The composition this function exists to prevent, so the test shows the difference it makes.
	dollars, _ := money.LineExtension(price, half, round.HalfUp)
	twice, _ := dollars.MulRate(rate, syp, round.HalfUp)
	if twice.Minor() != 21_710 {
		t.Fatalf("the twice-rounded composition gave %d; the example no longer shows the defect", twice.Minor())
	}
}

func TestLineExtensionDivRateNeverInvertsTheRate(t *testing.T) {
	usd, syp := mustCur(t, "USD", 2), mustCur(t, "SYP", 0)
	kg := mustUnit(t, "kg", true)
	// 2.5 kg at 18,000 pounds a kilo, at 15,000 a dollar: 45,000 pounds = $3.00 exactly.
	got, err := money.LineExtensionDivRate(money.UnitFromMicro(syp, 18_000*m), mustQtyT(t, kg, 2_500_000), money.RateFromNano(15_000*1_000_000_000), usd, round.HalfUp)
	if err != nil || got.Minor() != 300 {
		t.Fatalf("got %v, %v; want 3.00 USD", got, err)
	}
	// 7 kg at 10,000 pounds, at 15,000 a dollar: exactly $4.6666… → 4.67, from one division.
	got, _ = money.LineExtensionDivRate(money.UnitFromMicro(syp, 10_000*m), mustQtyT(t, kg, 7*m), money.RateFromNano(15_000*1_000_000_000), usd, round.HalfUp)
	if got.Minor() != 467 {
		t.Fatalf("got %d, want 467", got.Minor())
	}
}

func TestLineExtensionMulRateRoundsOnce(t *testing.T) {
	for _, decimals := range []uint8{0, 2, 3} {
		to := mustCur(t, "TGT", decimals)
		from := mustCur(t, "SRC", 2)
		unit := mustUnit(t, "u", true)
		t.Run("decimals_"+string(rune('0'+decimals)), func(t *testing.T) {
			rapid.Check(t, func(rt *rapid.T) {
				// A price up to 10,000, a quantity up to 1,000, a rate from 1 to 50,000: a shop's range with room, and the
				// division back cannot overflow at a rate of at least 1.
				price := rapid.Int64Range(0, 10_000*m).Draw(rt, "price")
				qty := rapid.Int64Range(0, 1_000*m).Draw(rt, "qty")
				rate := rapid.Int64Range(1_000_000_000, 50_000*1_000_000_000).Draw(rt, "rate")

				got, err := money.LineExtensionMulRate(money.UnitFromMicro(from, price), mustQty(rt, unit, qty), money.RateFromNano(rate), to, round.HalfUp)
				if err != nil {
					rt.Fatal(err)
				}
				num := new(big.Int).Mul(big.NewInt(price), big.NewInt(qty))
				num.Mul(num, big.NewInt(rate))
				num.Mul(num, pow10(int(decimals)))
				want := halfUp(num, pow10(21))
				if got.Minor() != want {
					rt.Fatalf("got %d, want the exact value rounded once, %d", got.Minor(), want)
				}

				back, err := money.LineExtensionDivRate(money.UnitFromMicro(to, price), mustQty(rt, unit, qty), money.RateFromNano(rate), from, round.HalfUp)
				if err != nil {
					rt.Fatal(err)
				}
				dnum := new(big.Int).Mul(big.NewInt(price), big.NewInt(qty))
				dnum.Mul(dnum, pow10(2+9))
				if want := halfUp(dnum, new(big.Int).Mul(pow10(12), big.NewInt(rate))); back.Minor() != want {
					rt.Fatalf("DivRate line got %d, want %d", back.Minor(), want)
				}
			})
		})
	}
}

// TestMoneyDivRateRoundTripsWithinTheStatedBound: dollars → pounds → dollars returns exactly when the rate is above 100
// (a pound is finer than a cent), and pounds → dollars → pounds is off by at most one cent's worth of pounds.
func TestMoneyDivRateRoundTripsWithinTheStatedBound(t *testing.T) {
	usd, syp := mustCur(t, "USD", 2), mustCur(t, "SYP", 0)
	rapid.Check(t, func(rt *rapid.T) {
		rateWhole := rapid.Int64Range(101, 100_000).Draw(rt, "rateWhole")
		rate := money.RateFromNano(rateWhole * 1_000_000_000)
		cents := rapid.Int64Range(0, 10_000_000).Draw(rt, "cents")

		pounds, err := money.FromMinor(usd, cents).MulRate(rate, syp, round.HalfUp)
		if err != nil {
			rt.Fatal(err)
		}
		back, err := pounds.DivRate(rate, usd, round.HalfUp)
		if err != nil {
			rt.Fatal(err)
		}
		if back.Minor() != cents {
			rt.Fatalf("%d cents → %d pounds → %d cents at %d", cents, pounds.Minor(), back.Minor(), rateWhole)
		}

		sypMinor := rapid.Int64Range(0, 1_000_000_000).Draw(rt, "pounds")
		dollars, _ := money.FromMinor(syp, sypMinor).DivRate(rate, usd, round.HalfUp)
		again, _ := dollars.MulRate(rate, syp, round.HalfUp)
		bound := (rateWhole + 99) / 100 // one cent's worth of pounds, rounded up
		if d := again.Minor() - sypMinor; d > bound || d < -bound {
			rt.Fatalf("%d pounds → %d cents → %d pounds at %d: off by more than %d", sypMinor, dollars.Minor(), again.Minor(), rateWhole, bound)
		}
	})
}

func TestMoneyDivRate(t *testing.T) {
	usd, syp, kwd := mustCur(t, "USD", 2), mustCur(t, "SYP", 0), mustCur(t, "KWD", 3)
	for _, c := range []struct {
		name   string
		amount money.Money
		rate   string
		to     money.Currency
		want   int64
	}{
		{"1,500,000 SYP at 15,000 is 100.00 USD", money.FromMinor(syp, 1_500_000), "15000", usd, 10_000},
		{"one pound at 15,000 is 0.01 USD, rounded half up from 0.0067", money.FromMinor(syp, 100), "15000", usd, 1},
		{"30.500 KWD at 0.305 per USD is 100.00 USD", money.FromMinor(kwd, 30_500), "0.305", usd, 10_000},
		{"100.00 USD at 0.305 KWD per USD… divided is 327.869 KWD", money.FromMinor(usd, 10_000), "0.305", kwd, 327_869},
	} {
		rate, err := money.ParseRate(c.rate)
		if err != nil {
			t.Fatal(err)
		}
		got, err := c.amount.DivRate(rate, c.to, round.HalfUp)
		if err != nil || got.Minor() != c.want || got.Currency().Code() != c.to.Code() {
			t.Errorf("%s: got %v, %v", c.name, got, err)
		}
	}
}

func TestConversionRefusals(t *testing.T) {
	usd, syp := mustCur(t, "USD", 2), mustCur(t, "SYP", 0)
	unit := mustUnit(t, "u", true)
	one := mustQtyT(t, unit, m)
	price := money.UnitFromMicro(usd, m)
	for name, err := range map[string]error{
		"mul zero rate":     second(money.LineExtensionMulRate(price, one, money.RateFromNano(0), syp, round.HalfUp)),
		"div negative rate": second(money.LineExtensionDivRate(price, one, money.RateFromNano(-1), syp, round.HalfUp)),
		"money zero rate":   second(money.FromMinor(usd, 1).DivRate(money.RateFromNano(0), syp, round.HalfUp)),
	} {
		if !errors.Is(err, money.ErrInvalidRate) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, err := range map[string]error{
		"mul no target":   second(money.LineExtensionMulRate(price, one, money.RateFromNano(1), money.Currency{}, round.HalfUp)),
		"div no price":    second(money.LineExtensionDivRate(money.UnitAmount{}, one, money.RateFromNano(1), syp, round.HalfUp)),
		"money no source": second(money.Money{}.DivRate(money.RateFromNano(1), syp, round.HalfUp)),
	} {
		if !errors.Is(err, money.ErrNoCurrency) {
			t.Errorf("%s: %v", name, err)
		}
	}
	huge := money.UnitFromMicro(usd, math.MaxInt64)
	if _, err := money.LineExtensionMulRate(huge, mustQtyT(t, unit, math.MaxInt64/m*m), money.RateFromNano(math.MaxInt64), syp, round.HalfUp); !errors.Is(err, money.ErrOverflow) {
		t.Errorf("overflow: %v", err)
	}
}

func second(_ money.Money, err error) error { return err }
