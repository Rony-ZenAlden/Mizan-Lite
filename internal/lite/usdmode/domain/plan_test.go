package domain_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/usdmode/domain"
)

var at15000 = domain.Money{Local: "SYP", LocalDecimals: 0, USDDecimals: 2, RateNano: 15_000_000_000_000}

// TestEveryFigureComesToDollarsAtOneRate: prices and costs to the cent, balances either way, the drawer's cash — and a
// price too small to be a cent becomes one, shown to the owner.
func TestEveryFigureComesToDollarsAtOneRate(t *testing.T) {
	products := []domain.Product{
		{ID: "oil", NameAR: "زيت", LocalPriceMicro: 48_750_000_000, HasCost: true, LocalCostMicro: 36_000_000_000}, // 48,750 and 36,000
		{ID: "gum", NameAR: "علكة", LocalPriceMicro: 25_000_000},                                                   // 25 pounds
		{ID: "misc", NameAR: "متفرقات", OpenPrice: true},
	}
	customers := []domain.Balance{{ID: "c1", Name: "أبو محمد", LocalMinor: 150_000}, {ID: "c2", Name: "سامي", LocalMinor: -22_500}}
	suppliers := []domain.Balance{{ID: "s1", Name: "المروى", LocalMinor: 1_000_000}}
	p, err := domain.NewPlan(at15000, products, customers, suppliers, 375_000)
	if err != nil {
		t.Fatal(err)
	}
	oil, gum := p.Prices[0], p.Prices[1] // by Arabic name: زيت, then علكة
	// 48,750 at 15,000 is $3.25; 36,000 is $2.40.
	if oil.USDPriceMicro != 3_250_000 || oil.USDCostMicro != 2_400_000 || oil.RaisedToCent {
		t.Fatalf("oil %+v", oil)
	}
	if gum.USDPriceMicro != 10_000 || !gum.RaisedToCent {
		t.Fatalf("gum %+v — 25 pounds is less than half a cent, and becomes a cent the owner is shown", gum)
	}
	if len(p.OpenItems) != 1 || p.OpenItems[0].ID != "misc" {
		t.Fatalf("open items %+v", p.OpenItems)
	}
	// 150,000 is $10.00; −22,500 is −$1.50 — the shop owes Sami, and still does.
	want := map[string]int64{"c1": 1_000, "c2": -150}
	for _, c := range p.Customers {
		if c.USDMinor != want[string(c.ID)] {
			t.Fatalf("customer %+v", c)
		}
	}
	if p.Suppliers[0].USDMinor != 6_667 {
		t.Fatalf("supplier %+v — 1,000,000 at 15,000 is $66.67", p.Suppliers[0])
	}
	if p.Drawer != (domain.DrawerChange{LocalMinor: 375_000, USDMinor: 2_500}) {
		t.Fatalf("drawer %+v", p.Drawer)
	}
	if at15000.NoteText() != "USD @ 15000" {
		t.Fatalf("note %q", at15000.NoteText())
	}
}

// TestThePlanIsNamedByItsFigures: the same figures give the same token, and any figure moving changes it.
func TestThePlanIsNamedByItsFigures(t *testing.T) {
	a, _ := domain.NewPlan(at15000, nil, []domain.Balance{{ID: "c1", LocalMinor: 150_000}}, nil, 0)
	b, _ := domain.NewPlan(at15000, nil, []domain.Balance{{ID: "c1", LocalMinor: 150_000}}, nil, 0)
	c, _ := domain.NewPlan(at15000, nil, []domain.Balance{{ID: "c1", LocalMinor: 150_001}}, nil, 0)
	moved := at15000
	moved.RateNano = 15_100_000_000_000
	d, _ := domain.NewPlan(moved, nil, []domain.Balance{{ID: "c1", LocalMinor: 150_000}}, nil, 0)
	if a.Token == "" || a.Token != b.Token || a.Token == c.Token || a.Token == d.Token {
		t.Fatalf("tokens %s %s %s %s", a.Token, b.Token, c.Token, d.Token)
	}
	// A drawer short of nothing converts nothing.
	if e, _ := domain.NewPlan(at15000, nil, nil, nil, -5_000); e.Drawer != (domain.DrawerChange{}) {
		t.Fatalf("a negative drawer converted: %+v", e.Drawer)
	}
	if _, err := domain.NewPlan(domain.Money{Local: "SYP", USDDecimals: 2}, nil, nil, nil, 0); errs.CodeOf(err) != domain.CodeNoRate {
		t.Fatalf("no rate: %v", err)
	}
}
