// Package purchasing owns the money going out for goods: orders, deliveries, bills, supplier
// returns, and landed cost.
//
// # What this module is really testing
//
// §32 calls Phase 6 "completes the stock cycle", and almost everything it needs was built
// earlier and has been waiting for a caller: Phase 2's `purchasing.bill.posted` rule, Phase 3's
// `purchase_uom_id` and supplier role, Phase 4's `Revaluation` movement, its `Allocate`, and its
// `inventory_layers` — written on every receipt since 4.2 and read by nothing.
//
// Phase 5 asked that question of Phase 2 and the answer was yes. This module asks it of Phase 4.
//
// # Three documents, not one
//
// Sales models quotation → order → invoice as one table maturing. Purchasing does not have that
// shape: an order, a delivery, and an invoice describe three different events that routinely
// disagree about quantity, timing, and count. Collapsing them would assert they always coincide,
// and the three-way match — the control that stops a business paying for goods it never got —
// would have nothing to compare.
package purchasing

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/event"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/purchasing/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/platform/auth"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/jobs"
	"github.com/mizan-erp/mizan/internal/platform/metadata"
	"github.com/mizan-erp/mizan/internal/platform/modules"
	"github.com/mizan-erp/mizan/internal/platform/outbox"
)

// The module owns its schema (§10.3). Sales owns 0023–0025 and identity took 0026.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// The permissions this module protects.
//
// Ordering and RECEIVING are separate grants, and so is billing. That is not ceremony: it is the
// segregation of duties that makes the three-way match worth having. One person ordering, taking
// delivery, and approving the invoice can pay a supplier for nothing, and no amount of matching
// detects it because they control all three sides.
const (
	PermOrderView             = "purchasing.order.view"
	PermOrderDraft            = "purchasing.order.draft"
	PermOrderPlace            = "purchasing.order.place"
	PermOrderCancel           = "purchasing.order.cancel"
	PermSupplierProductManage = "purchasing.supplier_product.manage"
)

// The audited actions (§15.3).
//
// AUDIT actions only. Posting-rule keys — what Phase 2's seeded rules match on — are declared
// separately where they are used, because merging the two vocabularies is the mistake the Phase 5
// DoD review found: one selects a journal entry, the other records that a person did something.
const (
	ActionOrderDrafted       = "purchasing.order.drafted"
	ActionOrderLineAdded     = "purchasing.order.line_added"
	ActionOrderLineRemoved   = "purchasing.order.line_removed"
	ActionOrderPlaced        = "purchasing.order.placed"
	ActionOrderCancelled     = "purchasing.order.cancelled"
	ActionOrderClosed        = "purchasing.order.closed"
	ActionSupplierProductSet = "purchasing.supplier_product.set"

	EntityOrder           = "purchasing.order"
	EntitySupplierProduct = "purchasing.supplier_product"
)

// Stable codes for this module's failures.
const (
	CodePublisherMissing  = "purchasing.publisher_missing"
	CodeUnknownOrder      = "purchasing.unknown_order"
	CodeUnknownLine       = "purchasing.unknown_line"
	CodeUnknownReceipt    = "purchasing.unknown_receipt"
	CodeUnknownBill       = "purchasing.unknown_bill"
	CodeUnknownLandedCost = "purchasing.unknown_landed_cost"
	CodeUnknownReturn     = "purchasing.unknown_return"
	CodeUnknownPayment    = "purchasing.unknown_payment"
	CodePortMissing       = "purchasing.port_missing"
)

// The series codes this module allocates from.
//
// Four, from the PLATFORM's number_series table — the one sales 0023 declined to duplicate,
// noting that "purchasing (Phase 6) will number documents too". It does, and it needed no new
// table.
const (
	SeriesOrder   = "PURCHASE_ORDER"
	SeriesReceipt = "GOODS_RECEIPT"
	SeriesBill    = "PURCHASE_BILL"
	SeriesPayment = "SUPPLIER_PAYMENT"
)

// Database is the narrow surface this module needs.
type Database interface {
	database.DB
	database.UnitOfWork
}

// ActorResolver reports who is acting, if anyone is.
type ActorResolver interface {
	Actor(ctx context.Context) (Actor, bool)
}

// Actor is who made a document.
type Actor struct {
	UserID   id.ID
	BranchID id.ID
}

// Options configures the service.
type Options struct {
	Clock   clock.Clock
	Bus     event.Publisher
	Actors  ActorResolver
	Catalog Catalog
	Pricing Pricing
	Tax     Tax
	Stock   Stock
	Numbers Numbering
	Logger  *slog.Logger
}

// Service is the purchasing module's application layer.
type Service struct {
	db      Database
	repos   *sqlite.Repos
	clk     clock.Clock
	bus     event.Publisher
	actors  ActorResolver
	catalog Catalog
	pricing Pricing
	tax     Tax
	stock   Stock
	numbers Numbering
	logger  *slog.Logger
}

// NewService builds the service.
func NewService(db Database, opts Options) *Service {
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}
	return &Service{
		db: db, repos: sqlite.New(db, opts.Clock), clk: opts.Clock,
		bus: opts.Bus, actors: opts.Actors, catalog: opts.Catalog,
		pricing: opts.Pricing, tax: opts.Tax, stock: opts.Stock,
		numbers: opts.Numbers, logger: opts.Logger,
	}
}

func (s *Service) audit(ctx context.Context, a auditc.Auditable) error {
	if s.bus == nil {
		return errs.Internal(CodePublisherMissing,
			"the purchasing service was built without an event publisher")
	}
	return s.bus.Publish(ctx, a)
}

// actorOf reports the acting user, or the zero id when nobody is acting.
func (s *Service) actorOf(ctx context.Context) id.ID {
	if s.actors == nil {
		return ""
	}
	actor, ok := s.actors.Actor(ctx)
	if !ok {
		return ""
	}
	return actor.UserID
}

// ── module ──────────────────────────────────────────────────────────────────────

// Module is the purchasing module's registration surface.
type Module struct{ svc *Service }

var _ modules.Module = (*Module)(nil)

// NewModule builds the module around an already-constructed service.
func NewModule(svc *Service) *Module { return &Module{svc: svc} }

// Service exposes the module's application layer to the composition root.
func (m *Module) Service() *Service { return m.svc }

// Name identifies the module.
func (m *Module) Name() string { return "purchasing" }

// DependsOn names the modules whose tables this one references.
func (m *Module) DependsOn() []string {
	return []string{"org", "accounting", "catalog", "partner", "inventory"}
}

// Migrations returns the module's schema.
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return migrationFS
	}
	return sub
}

// Permissions gate ordering, receiving, and billing separately (segregation of duties).
func (m *Module) Permissions() []auth.PermissionDef {
	return []auth.PermissionDef{
		{Code: PermOrderView, Description: "permissions.purchasing.order.view"},
		{Code: PermOrderDraft, Description: "permissions.purchasing.order.draft"},
		{Code: PermOrderPlace, Description: "permissions.purchasing.order.place"},
		{Code: PermOrderCancel, Description: "permissions.purchasing.order.cancel"},
		{Code: PermSupplierProductManage,
			Description: "permissions.purchasing.supplier_product.manage"},
		{Code: PermBillView, Description: "permissions.purchasing.bill.view"},
		{Code: PermBillDraft, Description: "permissions.purchasing.bill.draft"},
		{Code: PermBillPost, Description: "permissions.purchasing.bill.post"},
		{Code: PermLandedCostManage,
			Description: "permissions.purchasing.landed_cost.manage"},
		{Code: PermReturnView, Description: "permissions.purchasing.return.view"},
		{Code: PermReturnPost, Description: "permissions.purchasing.return.post"},
		{Code: PermPaymentView, Description: "permissions.purchasing.payment.view"},
		{Code: PermPaymentPost, Description: "permissions.purchasing.payment.post"},
	}
}

// Settings: the over-receipt tolerance (6.2).
func (m *Module) Settings() []config.Definition {
	return []config.Definition{mustDefinition(OverReceiptTolerance.Key())}
}

func mustDefinition(key string) config.Definition {
	def, _ := config.Default().Lookup(key)
	return def
}

// FeatureFlags: none.
func (m *Module) FeatureFlags() []config.FlagDef { return nil }

// Metadata: none. A number series is a customer's own configuration.
func (m *Module) Metadata() []metadata.SeedSpec { return nil }

// Subscribe registers nothing. Purchasing publishes; it does not react.
func (m *Module) Subscribe(_ *eventbus.Bus, _ *outbox.Subscribers) error { return nil }

// Jobs: none yet.
func (m *Module) Jobs() []jobs.Registration { return nil }
