// Package usdmode takes a shop over to dollars only (0.10.0, the owner's request of 2026-09-24, "convert everything"):
// every price in the local currency becomes a dollar price, every customer's and supplier's local balance becomes a
// dollar balance, the drawer's local cash becomes dollars — all at the rate in force, in one act the owner confirms —
// and then the shop reads, sells, lends, owes and counts in dollars only.
//
// It imports no other module. Each part is reached through a port satisfied in the composition root by the module that
// owns it, so the prices are set by the catalogue's own rules, the balances by each book's, and the drawer by the cash
// book's — and all of it in one transaction, or none.
package usdmode

import (
	"context"
	"strconv"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/usdmode/domain"
)

// ActSwitch is the owner's act of going over to dollars only, as the owner's history records it.
const ActSwitch = "settings.usd_only.set"

// CodeOwnerRequired is the owner module's refusal, returned by a read outside owner mode. Declared here because usdmode
// may not import the owner module; a bootstrap test holds the two equal.
const CodeOwnerRequired = "lite.owner.required"

// Money is the shop's two currencies, whether it is dollars-only already, and the rate in force.
type Money interface {
	// Money is the local currency, the two currencies' decimals, and the rate in force — RateNano nought for none.
	Money(ctx context.Context) (domain.Money, error)
	USDOnly(ctx context.Context) (bool, error)
	// SetUSDOnly writes the setting, in the caller's transaction.
	SetUSDOnly(ctx context.Context) error
}

// Catalogue is the products priced in the local currency, and setting them in dollars.
type Catalogue interface {
	LocalPriced(ctx context.Context, local string) ([]domain.Product, error)
	// SetUSDPrice sets a product's price, and its cost, in dollars, at the version the plan read.
	SetUSDPrice(ctx context.Context, change domain.PriceChange) error
	// SetUSDOpen makes an open-priced item one whose price is typed in dollars.
	SetUSDOpen(ctx context.Context, product domain.Product) error
}

// Book is a book of balances — the customers' or the suppliers' — and moving one to dollars.
type Book interface {
	BalancesIn(ctx context.Context, local string) ([]domain.Balance, error)
	// Convert moves a whole local balance to dollars, refusing one that moved since the plan read it.
	Convert(ctx context.Context, change domain.BalanceChange, local, note string) error
}

// Drawer is the drawer's local cash, and exchanging it for dollars.
type Drawer interface {
	ExpectedLocal(ctx context.Context, local string) (int64, error)
	// Convert takes the local cash out of the drawer and puts the dollars in, as the cash book's own entries.
	Convert(ctx context.Context, change domain.DrawerChange, local, note string) error
}

// OwnerGate is what the conversion needs of the owner.
type OwnerGate interface {
	Require(ctx context.Context, act GuardedAct) error
	Allowed(ctx context.Context) bool
}

// GuardedAct describes an owner-only act, for the owner's history.
type GuardedAct struct {
	Action    string
	SubjectID id.ID
	Before    string
	After     string
}

// Transactor runs fn atomically; a call inside another joins it.
type Transactor interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// Ports are the service's dependencies.
type Ports struct {
	Money     Money
	Catalogue Catalogue
	Customers Book
	Suppliers Book
	Drawer    Drawer
	Gate      OwnerGate
}

// Service is the going-over.
type Service struct {
	tx    Transactor
	ports Ports
}

// NewService builds the service.
func NewService(tx Transactor, ports Ports) *Service { return &Service{tx: tx, ports: ports} }

// Plan is what going over to dollars only would change, nothing written. Owner only: it shows costs.
func (s *Service) Plan(ctx context.Context) (domain.Plan, error) {
	if !s.ports.Gate.Allowed(ctx) {
		return domain.Plan{}, errs.Permission(CodeOwnerRequired, "the owner's PIN is required")
	}
	return s.plan(ctx)
}

func (s *Service) plan(ctx context.Context) (domain.Plan, error) {
	p := s.ports
	already, err := p.Money.USDOnly(ctx)
	if err != nil {
		return domain.Plan{}, err
	}
	if already {
		return domain.Plan{}, errs.Conflict(domain.CodeAlreadyUSDOnly, "the shop counts in dollars only already")
	}
	m, err := p.Money.Money(ctx)
	if err != nil {
		return domain.Plan{}, err
	}
	products, err := p.Catalogue.LocalPriced(ctx, m.Local)
	if err != nil {
		return domain.Plan{}, err
	}
	customers, err := p.Customers.BalancesIn(ctx, m.Local)
	if err != nil {
		return domain.Plan{}, err
	}
	suppliers, err := p.Suppliers.BalancesIn(ctx, m.Local)
	if err != nil {
		return domain.Plan{}, err
	}
	drawer, err := p.Drawer.ExpectedLocal(ctx, m.Local)
	if err != nil {
		return domain.Plan{}, err
	}
	return domain.NewPlan(m, products, customers, suppliers, drawer)
}

// Apply goes over to dollars only, all of it or none: the plan worked out again and held to the token the owner
// confirmed — a figure that moved since is refused, never quietly converted — then every price, balance and the drawer,
// and last the setting. The owner's act, recorded once.
func (s *Service) Apply(ctx context.Context, token string) (domain.Plan, error) {
	var out domain.Plan
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		plan, err := s.plan(ctx)
		if err != nil {
			return err
		}
		if plan.Token != token {
			return errs.Conflict(domain.CodePlanChanged, "the figures changed since they were shown")
		}
		if err = s.ports.Gate.Require(ctx, GuardedAct{Action: ActSwitch,
			Before: plan.Money.Local + " @ " + strconv.FormatInt(plan.Money.RateNano, 10),
			After: strconv.Itoa(len(plan.Prices)) + " prices, " + strconv.Itoa(len(plan.Customers)) + " customers, " +
				strconv.Itoa(len(plan.Suppliers)) + " suppliers"}); err != nil {
			return err
		}
		note := plan.Money.NoteText()
		for _, c := range plan.Prices {
			if err = s.ports.Catalogue.SetUSDPrice(ctx, c); err != nil {
				return err
			}
		}
		for _, o := range plan.OpenItems {
			if err = s.ports.Catalogue.SetUSDOpen(ctx, o); err != nil {
				return err
			}
		}
		for _, c := range plan.Customers {
			if err = s.ports.Customers.Convert(ctx, c, plan.Money.Local, note); err != nil {
				return err
			}
		}
		for _, c := range plan.Suppliers {
			if err = s.ports.Suppliers.Convert(ctx, c, plan.Money.Local, note); err != nil {
				return err
			}
		}
		if plan.Drawer.LocalMinor > 0 {
			if err = s.ports.Drawer.Convert(ctx, plan.Drawer, plan.Money.Local, note); err != nil {
				return err
			}
		}
		if err = s.ports.Money.SetUSDOnly(ctx); err != nil {
			return err
		}
		out = plan
		return nil
	})
	return out, err
}
