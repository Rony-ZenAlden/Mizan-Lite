// Package stock is Mizan Lite's stock: what is on hand, what it cost, and every movement that changed either (L2).
package stock

import (
	"context"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/bizdate"
	"github.com/mizan-erp/mizan/internal/lite/stock/domain"
)

// The acts only the owner may do (Q-L2.3, Q-L2.5). Their codes are what the owner's history records.
const (
	ActCountLower     = "stock.count.lower"
	ActAdjustLower    = "stock.adjust.lower"
	ActReverseReceipt = "stock.receipt.reverse"
	ActCorrectCost    = "stock.cost.correct"
)

// CodeOwnerRequired is the owner module's refusal, returned by a guarded READ outside owner mode so the frontend
// answers it with the same PIN dialog. Declared here because stock may not import the owner module; a bootstrap test
// holds the two equal.
const CodeOwnerRequired = "lite.owner.required"

// MaxHistory bounds a product's movement history in one read.
const MaxHistory = 500

// Store persists levels and the ledger. infra/sqlite implements it; stocktest.Fake implements it in memory, and one
// contract suite runs against both.
type Store interface {
	// Level returns the product's level, or a zero Level carrying only the product id when it never moved.
	Level(ctx context.Context, productID id.ID) (domain.Level, error)
	Levels(ctx context.Context) ([]domain.Level, error)
	// Movement returns one movement, or domain.ErrMovementNotFound.
	Movement(ctx context.Context, movementID id.ID) (domain.Movement, error)
	// Movements returns a product's newest movements first.
	Movements(ctx context.Context, productID id.ID, limit int) ([]domain.Movement, error)
	// EachMovement calls fn with every movement ordered by product, then place — the verifier's walk.
	EachMovement(ctx context.Context, fn func(domain.Movement) error) error
	// Append inserts a movement. The ledger is insert-only.
	Append(ctx context.Context, m domain.Movement) error
	// SaveLevel inserts a level whose RowVersion is 0, or updates one still at its RowVersion, and returns it at
	// its new version. A level that moved on is refused with domain.ErrStale.
	SaveLevel(ctx context.Context, l domain.Level) (domain.Level, error)
}

// Catalogue is what stock needs from the product catalogue, declared here and satisfied in the composition root —
// so stock never imports the catalogue module.
type Catalogue interface {
	// Product returns what stock needs to know about one product, or the catalogue's NotFound.
	Product(ctx context.Context, productID id.ID) (domain.Product, error)
	// Products returns every product, active or not.
	Products(ctx context.Context) ([]domain.Product, error)
	// Package returns what a package product opens into, or found=false.
	Package(ctx context.Context, packageProductID id.ID) (domain.Package, bool, error)
	Currencies(ctx context.Context) ([]domain.Currency, error)
}

// OwnerGate is what stock needs from the owner.
type OwnerGate interface {
	// Require permits an owner-only act, recording it in the caller's transaction, or refuses with CodeOwnerRequired.
	Require(ctx context.Context, act GuardedAct) error
	// Allowed reports owner mode without recording anything — for guarded reads (D-L2.13).
	Allowed(ctx context.Context) bool
}

// GuardedAct describes an owner-only act, for the owner's history.
type GuardedAct struct {
	Action    string
	SubjectID id.ID
	Before    string
	After     string
}

// Transactor runs fn atomically. platform/database.Store satisfies it.
type Transactor interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// Service is the stock use cases.
type Service struct {
	tx        Transactor
	store     Store
	catalogue Catalogue
	gate      OwnerGate
	clk       clock.Clock
	loc       *time.Location
	newID     func() (id.ID, error)
}

// NewService builds the service. loc is the shop's time zone for business dates — time.Local in the application,
// a fixed zone in tests; never time.LoadLocation (bizdate).
func NewService(tx Transactor, store Store, catalogue Catalogue, gate OwnerGate, clk clock.Clock, loc *time.Location) *Service {
	return &Service{tx: tx, store: store, catalogue: catalogue, gate: gate, clk: clk, loc: loc, newID: id.New}
}

// ReceiveInput is a delivery, or opening stock, as typed.
type ReceiveInput struct {
	ProductID id.ID
	Quantity  string
	Cost      domain.CostInput
	Note      string
}

// Receive books a delivery in (L2 §5.1).
func (s *Service) Receive(ctx context.Context, in ReceiveInput) (domain.Movement, error) {
	return s.receive(ctx, in, domain.Receive)
}

// Opening books the stock a shop already had (L2 §5.2).
func (s *Service) Opening(ctx context.Context, in ReceiveInput) (domain.Movement, error) {
	return s.receive(ctx, in, domain.Opening)
}

type receiveAct func(domain.Level, domain.Stamp, int64, domain.Cost, string) (domain.Movement, domain.Level, error)

func (s *Service) receive(ctx context.Context, in ReceiveInput, act receiveAct) (domain.Movement, error) {
	var out domain.Movement
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		product, err := s.catalogue.Product(ctx, in.ProductID)
		if err != nil {
			return err
		}
		if !product.Active {
			return errs.Conflict(domain.CodeInactiveProduct, "reactivate the product before receiving it")
		}
		quantity, err := domain.ParseQuantity(in.Quantity, product.UnitDecimals, domain.FieldQuantity)
		if err != nil {
			return err
		}
		currencies, err := s.catalogue.Currencies(ctx)
		if err != nil {
			return err
		}
		cost, err := domain.ResolveCost(in.Cost, quantity, currencies)
		if err != nil {
			return err
		}
		level, st, err := s.begin(ctx, in.ProductID)
		if err != nil {
			return err
		}
		m, after, err := act(level, st, quantity, cost, in.Note)
		if err != nil {
			return err
		}
		out = m
		return s.write(ctx, m, after)
	})
	return out, err
}

// CountInput is what is on the shelf (D-L2.10).
type CountInput struct {
	ProductID id.ID
	Counted   string
	Note      string
}

// Count records a count. One that lowers stock needs the owner (Q-L2.3).
func (s *Service) Count(ctx context.Context, in CountInput) (domain.Movement, error) {
	var out domain.Movement
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		product, err := s.catalogue.Product(ctx, in.ProductID)
		if err != nil {
			return err
		}
		counted, err := domain.ParseQuantity(in.Counted, product.UnitDecimals, domain.FieldQuantity)
		if err != nil {
			return err
		}
		level, st, err := s.begin(ctx, in.ProductID)
		if err != nil {
			return err
		}
		m, after, err := domain.Count(level, st, counted, in.Note)
		if err != nil {
			return err
		}
		if err = s.guardLowering(ctx, ActCountLower, product, m); err != nil {
			return err
		}
		out = m
		return s.write(ctx, m, after)
	})
	return out, err
}

// Direction says whether an adjustment adds stock or takes it out.
type Direction string

// The directions.
const (
	DirectionIn  Direction = "in"
	DirectionOut Direction = "out"
)

// AdjustInput is an adjustment as typed: an unsigned quantity and its direction.
type AdjustInput struct {
	ProductID id.ID
	Direction Direction
	Quantity  string
	Reason    domain.Reason
	Note      string
}

// Adjust adds or writes off stock for a reason (L2 §5.4). Taking stock out needs the owner (Q-L2.3).
func (s *Service) Adjust(ctx context.Context, in AdjustInput) (domain.Movement, error) {
	var out domain.Movement
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		product, err := s.catalogue.Product(ctx, in.ProductID)
		if err != nil {
			return err
		}
		quantity, err := domain.ParseQuantity(in.Quantity, product.UnitDecimals, domain.FieldQuantity)
		if err != nil {
			return err
		}
		switch in.Direction {
		case DirectionIn:
		case DirectionOut:
			quantity = -quantity
		default:
			return errs.Validation(domain.CodeQuantityRequired, "say whether stock comes in or goes out").
				WithField("direction", domain.CodeQuantityRequired, "required")
		}
		level, st, err := s.begin(ctx, in.ProductID)
		if err != nil {
			return err
		}
		m, after, err := domain.Adjust(level, st, quantity, in.Reason, in.Note)
		if err != nil {
			return err
		}
		if err = s.guardLowering(ctx, ActAdjustLower, product, m); err != nil {
			return err
		}
		out = m
		return s.write(ctx, m, after)
	})
	return out, err
}

// OpenPackageInput is how many packages are opened.
type OpenPackageInput struct {
	PackageProductID id.ID
	Packages         string
}

// Opened is the two movements an opening wrote.
type Opened struct {
	Out, In domain.Movement
}

// OpenPackage opens whole packages into their content (L2 §5.7): two movements, one transaction.
func (s *Service) OpenPackage(ctx context.Context, in OpenPackageInput) (Opened, error) {
	var out Opened
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		link, found, err := s.catalogue.Package(ctx, in.PackageProductID)
		if err != nil {
			return err
		}
		if !found {
			return errs.Conflict(domain.CodeNotAPackage, "the product is not linked to a content product")
		}
		content, err := s.catalogue.Product(ctx, link.ContentProductID)
		if err != nil {
			return err
		}
		if !content.Active {
			return errs.Conflict(domain.CodeInactiveContent, "reactivate the content product first")
		}
		packages, err := domain.ParseQuantity(in.Packages, 0, domain.FieldPackages)
		if err != nil {
			return err
		}
		pkgLevel, outStamp, err := s.begin(ctx, in.PackageProductID)
		if err != nil {
			return err
		}
		contentLevel, inStamp, err := s.begin(ctx, link.ContentProductID)
		if err != nil {
			return err
		}
		pairID, err := s.newID()
		if err != nil {
			return err
		}
		opened, err := domain.OpenPackage(pkgLevel, contentLevel, link, packages, outStamp, inStamp, pairID)
		if err != nil {
			return err
		}
		if err = s.write(ctx, opened.Out, opened.PackageLevel); err != nil {
			return err
		}
		out = Opened{Out: opened.Out, In: opened.In}
		return s.write(ctx, opened.In, opened.ContentLv)
	})
	return out, err
}

// ReverseReceipt takes back a receipt while it is its product's newest movement (L2 §5.5). Owner only.
func (s *Service) ReverseReceipt(ctx context.Context, receiptID id.ID, note string) (domain.Movement, error) {
	var out domain.Movement
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		receipt, err := s.store.Movement(ctx, receiptID)
		if err != nil {
			return err
		}
		product, err := s.catalogue.Product(ctx, receipt.ProductID)
		if err != nil {
			return err
		}
		level, st, err := s.begin(ctx, receipt.ProductID)
		if err != nil {
			return err
		}
		m, after, err := domain.ReverseReceipt(level, receipt, st, note)
		if err != nil {
			return err
		}
		err = s.gate.Require(ctx, GuardedAct{
			Action: ActReverseReceipt, SubjectID: receipt.ProductID,
			Before: domain.FormatScaled(m.OnHandBeforeMicro, product.UnitDecimals),
			After:  domain.FormatScaled(m.OnHandAfterMicro, product.UnitDecimals),
		})
		if err != nil {
			return err
		}
		out = m
		return s.write(ctx, m, after)
	})
	return out, err
}

// CorrectCostInput is the average cost a product should have, in USD, and why.
type CorrectCostInput struct {
	ProductID   id.ID
	AverageCost string
	Note        string
}

// CorrectCost sets a product's average cost (L2 §5.6). Owner only.
func (s *Service) CorrectCost(ctx context.Context, in CorrectCostInput) (domain.Movement, error) {
	var out domain.Movement
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		if _, err := s.catalogue.Product(ctx, in.ProductID); err != nil {
			return err
		}
		avg, err := domain.ParseUSDCost(in.AverageCost)
		if err != nil {
			return err
		}
		level, st, err := s.begin(ctx, in.ProductID)
		if err != nil {
			return err
		}
		m, after, err := domain.CorrectCost(level, st, avg, in.Note)
		if err != nil {
			return err
		}
		err = s.gate.Require(ctx, GuardedAct{
			Action: ActCorrectCost, SubjectID: in.ProductID,
			Before: domain.FormatScaled(m.AvgCostBeforeMicro, 2), After: domain.FormatScaled(m.AvgCostAfterMicro, 2),
		})
		if err != nil {
			return err
		}
		out = m
		return s.write(ctx, m, after)
	})
	return out, err
}

// OnHand is a product's quantity, with no cost (Q-L2.4).
type OnHand struct {
	ProductID    id.ID
	OnHandMicro  int64
	UnitDecimals int
}

// Levels lists the quantity of every product that has moved. Everyone may see quantities.
func (s *Service) Levels(ctx context.Context) ([]OnHand, error) {
	levels, err := s.store.Levels(ctx)
	if err != nil {
		return nil, err
	}
	decimals, err := s.unitDecimals(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]OnHand, 0, len(levels))
	for _, l := range levels {
		out = append(out, OnHand{ProductID: l.ProductID, OnHandMicro: l.OnHandMicro, UnitDecimals: decimals[l.ProductID]})
	}
	return out, nil
}

// Valued is a product's stock with its cost and value.
type Valued struct {
	OnHand
	AvgCostMicro int64
	ValueMinor   int64
}

// Valuation is every product's cost and value, and their total, in USD.
type Valuation struct {
	Lines      []Valued
	TotalMinor int64
}

// Valuation is what the stock cost. Owner mode only (Q-L2.4); viewing records nothing (D-L2.13).
func (s *Service) Valuation(ctx context.Context) (Valuation, error) {
	if !s.gate.Allowed(ctx) {
		return Valuation{}, ownerRequired()
	}
	levels, err := s.store.Levels(ctx)
	if err != nil {
		return Valuation{}, err
	}
	decimals, err := s.unitDecimals(ctx)
	if err != nil {
		return Valuation{}, err
	}
	out := Valuation{Lines: make([]Valued, 0, len(levels))}
	for _, l := range levels {
		v, err := l.Value()
		if err != nil {
			return Valuation{}, err
		}
		out.Lines = append(out.Lines, Valued{
			OnHand:       OnHand{ProductID: l.ProductID, OnHandMicro: l.OnHandMicro, UnitDecimals: decimals[l.ProductID]},
			AvgCostMicro: l.AvgCostMicro, ValueMinor: v,
		})
		out.TotalMinor += v
	}
	return out, nil
}

// History is a product's movements, newest first.
type History struct {
	Movements    []domain.Movement
	UnitDecimals int
	// CostsVisible is false outside owner mode, and then every cost field in Movements is zero.
	CostsVisible bool
	// ReversibleID is the receipt a reversal may be offered on — the newest movement, when it is a receipt — or zero.
	ReversibleID id.ID
}

// History returns a product's newest movements. Costs are cleared outside owner mode, here rather than on the
// screen, so no caller can forget to (Q-L2.4).
func (s *Service) History(ctx context.Context, productID id.ID, limit int) (History, error) {
	product, err := s.catalogue.Product(ctx, productID)
	if err != nil {
		return History{}, err
	}
	if limit <= 0 || limit > MaxHistory {
		limit = MaxHistory
	}
	movements, err := s.store.Movements(ctx, productID, limit)
	if err != nil {
		return History{}, err
	}
	out := History{Movements: movements, UnitDecimals: product.UnitDecimals, CostsVisible: s.gate.Allowed(ctx)}
	if len(movements) > 0 && movements[0].Kind == domain.KindReceipt {
		out.ReversibleID = movements[0].ID
	}
	if !out.CostsVisible {
		for i := range out.Movements {
			m := &out.Movements[i]
			m.UnitCostMicro, m.AvgCostBeforeMicro, m.AvgCostAfterMicro, m.Entered = 0, 0, 0, domain.Entered{}
		}
	}
	return out, nil
}

// Verify checks the ledger against itself and the levels (L2 §7). Owner mode only: its findings name costs. It
// reports; it never repairs (D-L2.14).
func (s *Service) Verify(ctx context.Context) ([]domain.Finding, error) {
	if !s.gate.Allowed(ctx) {
		return nil, ownerRequired()
	}
	return s.VerifyUnguarded(ctx)
}

// VerifyUnguarded is Verify for the demo seeder and tests, which have no owner at the keyboard.
func (s *Service) VerifyUnguarded(ctx context.Context) ([]domain.Finding, error) {
	var findings []domain.Finding
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		levels, err := s.store.Levels(ctx)
		if err != nil {
			return err
		}
		v := domain.NewVerifier(levels)
		if err = s.store.EachMovement(ctx, func(m domain.Movement) error { v.Add(m); return nil }); err != nil {
			return err
		}
		findings = v.Findings()
		return nil
	})
	return findings, err
}

// begin reads a product's level and stamps its next movement.
func (s *Service) begin(ctx context.Context, productID id.ID) (domain.Level, domain.Stamp, error) {
	level, err := s.store.Level(ctx, productID)
	if err != nil {
		return domain.Level{}, domain.Stamp{}, err
	}
	movementID, err := s.newID()
	if err != nil {
		return domain.Level{}, domain.Stamp{}, err
	}
	// At the ledger's precision, so a movement an act returns is the movement a read returns.
	now := s.clk.Now().UTC().Truncate(time.Millisecond)
	return level, domain.Stamp{ID: movementID, BusinessDate: bizdate.Date(now, s.loc), OccurredAt: now}, nil
}

// write appends the movement, then the level that points at it — in the caller's transaction, so neither is ever
// written without the other.
func (s *Service) write(ctx context.Context, m domain.Movement, after domain.Level) error {
	if err := s.store.Append(ctx, m); err != nil {
		return err
	}
	_, err := s.store.SaveLevel(ctx, after)
	return err
}

// guardLowering asks for the owner when a count or adjustment takes stock out (Q-L2.3). Raising stock asks nothing.
func (s *Service) guardLowering(ctx context.Context, action string, product domain.Product, m domain.Movement) error {
	if !m.Lowers() {
		return nil
	}
	return s.gate.Require(ctx, GuardedAct{
		Action: action, SubjectID: m.ProductID,
		Before: domain.FormatScaled(m.OnHandBeforeMicro, product.UnitDecimals),
		After:  domain.FormatScaled(m.OnHandAfterMicro, product.UnitDecimals),
	})
}

func (s *Service) unitDecimals(ctx context.Context) (map[id.ID]int, error) {
	products, err := s.catalogue.Products(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[id.ID]int, len(products))
	for _, p := range products {
		out[p.ID] = p.UnitDecimals
	}
	return out, nil
}

func ownerRequired() error {
	return errs.Permission(CodeOwnerRequired, "the owner's PIN is required")
}
