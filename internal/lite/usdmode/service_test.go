package usdmode_test

import (
	"context"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/usdmode"
	"github.com/mizan-erp/mizan/internal/lite/usdmode/domain"
)

// shop fakes every port and records what the conversion wrote.
type shop struct {
	usdOnly   bool
	allowed   bool
	rate      int64
	products  []domain.Product
	customers []domain.Balance
	suppliers []domain.Balance
	drawer    int64
	prices    []domain.PriceChange
	opens     []domain.Product
	converted []string
	drawerSet []domain.DrawerChange
	acts      []string
	notes     []string
}

func (s *shop) Money(context.Context) (domain.Money, error) {
	return domain.Money{Local: "SYP", LocalDecimals: 0, USDDecimals: 2, RateNano: s.rate}, nil
}
func (s *shop) USDOnly(context.Context) (bool, error) { return s.usdOnly, nil }
func (s *shop) SetUSDOnly(context.Context) error      { s.usdOnly = true; return nil }
func (s *shop) LocalPriced(context.Context, string) ([]domain.Product, error) {
	return s.products, nil
}
func (s *shop) SetUSDPrice(_ context.Context, c domain.PriceChange) error {
	s.prices = append(s.prices, c)
	return nil
}
func (s *shop) SetUSDOpen(_ context.Context, p domain.Product) error {
	s.opens = append(s.opens, p)
	return nil
}
func (s *shop) ExpectedLocal(context.Context, string) (int64, error) { return s.drawer, nil }
func (s *shop) Require(_ context.Context, act usdmode.GuardedAct) error {
	s.acts = append(s.acts, act.Action)
	return nil
}
func (s *shop) Allowed(context.Context) bool { return s.allowed }

// book is one of the two balance books.
type book struct {
	s        *shop
	name     string
	balances *[]domain.Balance
}

func (b book) BalancesIn(context.Context, string) ([]domain.Balance, error) { return *b.balances, nil }
func (b book) Convert(_ context.Context, c domain.BalanceChange, _, note string) error {
	b.s.converted = append(b.s.converted, b.name+":"+c.Name)
	b.s.notes = append(b.s.notes, note)
	return nil
}

type drawer struct{ s *shop }

func (d drawer) ExpectedLocal(ctx context.Context, local string) (int64, error) {
	return d.s.ExpectedLocal(ctx, local)
}
func (d drawer) Convert(_ context.Context, c domain.DrawerChange, _, _ string) error {
	d.s.drawerSet = append(d.s.drawerSet, c)
	return nil
}

type immediate struct{}

func (immediate) Do(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

func newShop() (*shop, *usdmode.Service) {
	s := &shop{allowed: true, rate: 15_000_000_000_000,
		products:  []domain.Product{{ID: "oil", NameAR: "زيت", LocalPriceMicro: 48_750_000_000}, {ID: "misc", NameAR: "متفرقات", OpenPrice: true}},
		customers: []domain.Balance{{ID: "c1", Name: "أبو محمد", LocalMinor: 150_000}},
		suppliers: []domain.Balance{{ID: "s1", Name: "المروى", LocalMinor: 300_000}},
		drawer:    375_000}
	svc := usdmode.NewService(immediate{}, usdmode.Ports{Money: s, Catalogue: s, Customers: book{s, "customer", &s.customers},
		Suppliers: book{s, "supplier", &s.suppliers}, Drawer: drawer{s}, Gate: s})
	return s, svc
}

func TestGoingOverToDollarsWritesWhatTheOwnerWasShown(t *testing.T) {
	s, svc := newShop()
	ctx := context.Background()
	plan, err := svc.Plan(ctx)
	if err != nil || len(plan.Prices) != 1 || len(plan.OpenItems) != 1 || len(plan.Customers) != 1 || plan.Drawer.USDMinor != 2_500 {
		t.Fatalf("Plan = %+v, %v", plan, err)
	}
	// A customer paid something in between: the token no longer names the figures, and nothing is written.
	s.customers[0].LocalMinor = 100_000
	if _, err = svc.Apply(ctx, plan.Token); errs.CodeOf(err) != domain.CodePlanChanged {
		t.Fatalf("a stale confirmation: %v", err)
	}
	if len(s.prices)+len(s.converted)+len(s.acts) != 0 || s.usdOnly {
		t.Fatal("a refused conversion wrote something")
	}
	plan, _ = svc.Plan(ctx)
	done, err := svc.Apply(ctx, plan.Token)
	if err != nil || done.Token != plan.Token {
		t.Fatalf("Apply = %+v, %v", done, err)
	}
	if !s.usdOnly || len(s.prices) != 1 || s.prices[0].USDPriceMicro != 3_250_000 || len(s.opens) != 1 || len(s.drawerSet) != 1 {
		t.Fatalf("written: %+v", s)
	}
	if len(s.converted) != 2 || s.converted[0] != "customer:أبو محمد" || s.converted[1] != "supplier:المروى" || s.notes[0] != "USD @ 15000" {
		t.Fatalf("balances converted %v with %v", s.converted, s.notes)
	}
	if len(s.acts) != 1 || s.acts[0] != usdmode.ActSwitch {
		t.Fatalf("the owner's acts %v — one act for the whole going-over", s.acts)
	}
	if _, err = svc.Plan(ctx); errs.CodeOf(err) != domain.CodeAlreadyUSDOnly {
		t.Fatalf("a second going-over: %v", err)
	}
}

func TestThePlanIsTheOwnersAndNeedsARate(t *testing.T) {
	s, svc := newShop()
	s.allowed = false
	if _, err := svc.Plan(context.Background()); errs.CodeOf(err) != usdmode.CodeOwnerRequired {
		t.Fatalf("the plan outside owner mode: %v", err)
	}
	s.allowed, s.rate = true, 0
	if _, err := svc.Plan(context.Background()); errs.CodeOf(err) != domain.CodeNoRate {
		t.Fatalf("no rate: %v", err)
	}
	// A drawer with no local cash converts nothing.
	s.rate, s.drawer = 15_000_000_000_000, 0
	plan, _ := svc.Plan(context.Background())
	if _, err := svc.Apply(context.Background(), plan.Token); err != nil || len(s.drawerSet) != 0 {
		t.Fatalf("an empty drawer: %v, %+v", err, s.drawerSet)
	}
}
