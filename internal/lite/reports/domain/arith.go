package domain

import (
	"math/big"
	"strconv"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

var bigOne = big.NewInt(1)

func pow10(n int) *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil) }

// roundDiv is num ÷ den rounded once, half away from zero — the one rounding every converted figure in a report uses.
func roundDiv(num, den *big.Int) int64 {
	q, r := new(big.Int).QuoRem(num, den, new(big.Int))
	twice := new(big.Int).Mul(new(big.Int).Abs(r), big.NewInt(2))
	if twice.Cmp(new(big.Int).Abs(den)) >= 0 {
		if (num.Sign() < 0) != (den.Sign() < 0) {
			q.Sub(q, bigOne)
		} else {
			q.Add(q, bigOne)
		}
	}
	return q.Int64()
}

func mul(vs ...int64) *big.Int {
	out := big.NewInt(1)
	for _, v := range vs {
		out.Mul(out, big.NewInt(v))
	}
	return out
}

// Convert is an amount in minor units of one of the pair's currencies in minor units of the other, at a rate, rounded once.
func (p Pair) Convert(minor int64, from string, nano int64) int64 {
	if from == p.USD.Code { // dollars → local: × rate
		return roundDiv(new(big.Int).Mul(mul(minor, nano), pow10(p.Local.Decimals)), new(big.Int).Mul(pow10(9), pow10(p.USD.Decimals)))
	}
	return roundDiv(new(big.Int).Mul(mul(minor, 1_000_000_000), pow10(p.USD.Decimals)), new(big.Int).Mul(big.NewInt(nano), pow10(p.Local.Decimals)))
}

// Margin is profit ÷ revenue in percent, exact, shown to one decimal rounded half away from zero; ok is false when
// there is no revenue to divide by (L6 §3.4).
func Margin(profit, revenue int64) (string, bool) {
	if revenue == 0 {
		return "", false
	}
	tenths := roundDiv(mul(profit, 1000), big.NewInt(revenue))
	sign := ""
	if tenths < 0 {
		sign, tenths = "-", -tenths
	}
	return sign + strconv.FormatInt(tenths/10, 10) + "." + strconv.FormatInt(tenths%10, 10), true
}

const dateLayout = "2006-01-02"

// ParseDate checks a business date, YYYY-MM-DD.
func ParseDate(raw string) (time.Time, error) {
	t, err := time.Parse(dateLayout, raw)
	if err != nil || t.Format(dateLayout) != raw {
		return time.Time{}, errs.Validation(CodeBadDate, "a date is YYYY-MM-DD").WithParam("value", raw)
	}
	return t, nil
}

// ParseRange checks from ≤ to.
func ParseRange(from, to string) error {
	f, err := ParseDate(from)
	if err != nil {
		return err
	}
	t, err := ParseDate(to)
	if err != nil {
		return err
	}
	if t.Before(f) {
		return errs.Validation(CodeBadRange, "the range ends before it starts").WithParam("from", from).WithParam("to", to)
	}
	return nil
}

// MonthRange is a calendar month's first and last business dates (Q-L6.8).
func MonthRange(month string) (string, string, error) {
	t, err := time.Parse("2006-01", month)
	if err != nil || t.Format("2006-01") != month {
		return "", "", errs.Validation(CodeBadMonth, "a month is YYYY-MM").WithParam("value", month)
	}
	return t.Format(dateLayout), t.AddDate(0, 1, -1).Format(dateLayout), nil
}

// AddDays moves a valid business date by n days.
func AddDays(date string, n int) string {
	t, err := time.Parse(dateLayout, date)
	if err != nil {
		return date
	}
	return t.AddDate(0, 0, n).Format(dateLayout)
}

// Dates lists the business dates from..to inclusive.
func Dates(from, to string) []string {
	var out []string
	for d := from; d <= to; d = AddDays(d, 1) {
		out = append(out, d)
	}
	return out
}

// RateOfDay is the rate of a business day: the highest place among the rates recorded on or before it — fixed once the
// day is past, and never read from a clock (L6 §4.3).
func RateOfDay(rates []Rate, date string) (Rate, bool) {
	var best Rate
	found := false
	for _, r := range rates {
		if r.BusinessDate <= date && (!found || r.Seq > best.Seq) {
			best, found = r, true
		}
	}
	return best, found
}

// InForce is the newest rate.
func InForce(rates []Rate) (Rate, bool) {
	var best Rate
	found := false
	for _, r := range rates {
		if !found || r.Seq > best.Seq {
			best, found = r, true
		}
	}
	return best, found
}
