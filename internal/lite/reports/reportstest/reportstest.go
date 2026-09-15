// Package reportstest is in-memory facts for the reports service: each port a slice filtered by business date.
package reportstest

import (
	"context"

	"github.com/mizan-erp/mizan/internal/lite/reports/domain"
)

// Facts satisfies every facts port from slices.
type Facts struct {
	Local       string
	RateList    []domain.Rate
	ProductList []domain.Product
	SaleList    []domain.Sale
	MoveList    []domain.Movement
	DebtList    []domain.DebtEntry
	CashList    []domain.CashEntry
	// Asked records each range asked of the sales port.
	Asked [][2]string
}

func within(date, from, to string) bool { return date >= from && date <= to }

func (f *Facts) Facts(_ context.Context, from, to string) ([]domain.Sale, error) {
	f.Asked = append(f.Asked, [2]string{from, to})
	var out []domain.Sale
	for _, s := range f.SaleList {
		if within(s.BusinessDate, from, to) || (s.Voided && within(s.VoidBusinessDate, from, to)) {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *Facts) Between(_ context.Context, from, to string) ([]domain.Movement, error) {
	var out []domain.Movement
	for _, m := range f.MoveList {
		if within(m.BusinessDate, from, to) {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *Facts) LastOnOrBefore(_ context.Context, date string) ([]domain.Movement, error) {
	last := map[string]domain.Movement{}
	var order []string
	for _, m := range f.MoveList {
		if m.BusinessDate > date {
			continue
		}
		prev, seen := last[string(m.ProductID)]
		if !seen {
			order = append(order, string(m.ProductID))
		}
		if !seen || m.Seq > prev.Seq {
			last[string(m.ProductID)] = m
		}
	}
	out := make([]domain.Movement, 0, len(order))
	for _, k := range order {
		out = append(out, last[k])
	}
	return out, nil
}

func (f *Facts) EntriesBetween(_ context.Context, from, to string) ([]domain.DebtEntry, error) {
	var out []domain.DebtEntry
	for _, e := range f.DebtList {
		if within(e.BusinessDate, from, to) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *Facts) AllRates(context.Context) ([]domain.Rate, string, error) {
	return f.RateList, f.Local, nil
}

func (f *Facts) Products(context.Context) ([]domain.Product, error) { return f.ProductList, nil }

func (f *Facts) Currencies(context.Context) ([]domain.Currency, error) {
	return []domain.Currency{{Code: "SYP", Decimals: 0}, {Code: "USD", Decimals: 2}}, nil
}

// Cash is the cash book port, apart from Facts because both declare Between.
type Cash struct{ F *Facts }

func (c Cash) Between(_ context.Context, from, to string) ([]domain.CashEntry, error) {
	var out []domain.CashEntry
	for _, e := range c.F.CashList {
		if within(e.BusinessDate, from, to) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (c Cash) LastCountBefore(_ context.Context, date, currency string) (domain.CashEntry, bool, error) {
	var best domain.CashEntry
	found := false
	for _, e := range c.F.CashList {
		if e.Kind == domain.CashCount && e.Currency == currency && e.BusinessDate < date && !e.Reversed && (!found || e.Seq > best.Seq) {
			best, found = e, true
		}
	}
	return best, found, nil
}

// Gate is an owner gate that is open or not.
type Gate struct{ Elevated bool }

func (g *Gate) Allowed(context.Context) bool { return g.Elevated }
