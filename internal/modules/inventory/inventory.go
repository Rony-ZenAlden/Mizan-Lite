// Package inventory owns how much of a thing there is and what it cost.
//
// # The shape, and why it is Phase 2's shape
//
// `stock_movements` is an append-only ledger and the source of truth; `stock_levels` is a
// maintained projection that can always be rebuilt from it and is verified against it. That is
// exactly what Phase 2 did with journal lines and account balances, and the reasoning transfers
// unchanged — a projection that cannot be rebuilt is a number nobody can defend.
//
// # No accounting logic lives here
//
// A stock movement produces a value delta. Which accounts that delta lands in is Phase 2's
// table-driven decision (§20.3), reached by publishing a Postable event. This module never
// learns a debit from a credit, which is what lets a country with different inventory accounting
// be a seed file rather than a code change.
package inventory

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/event"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/inventory/domain"
	"github.com/mizan-erp/mizan/internal/modules/inventory/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/platform/auth"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/metadata"
	"github.com/mizan-erp/mizan/internal/platform/modules"
	"github.com/mizan-erp/mizan/internal/platform/outbox"
)

// The module owns its schema (§10.3). Pricing owns 0019, so inventory owns 0020.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// The permissions this module protects.
//
// Seeing what is on the shelf and changing what the system believes is there are very different
// acts: a stock adjustment is how theft is concealed, so it is its own permission rather than
// part of a general "manage".
const (
	PermStockView     = "inventory.stock.view"
	PermStockAdjust   = "inventory.stock.adjust"
	PermStockTransfer = "inventory.stock.transfer"
	PermStockCount    = "inventory.stock.count"
	PermCostView      = "inventory.cost.view"
)

// The settings this module declares.
//
// Typed handles rather than key strings, so no call site can mismatch a type (0.5's rule).
var (
	// AllowNegativeStock permits issuing more than is on hand.
	//
	// FALSE by default. Some businesses genuinely need it — a workshop that issues components
	// before the delivery note is entered — and most should not have it, because what it permits
	// is selling what does not exist.
	//
	// A SETTING rather than a feature flag, for tax's reason: whether a business tolerates
	// negative stock is a fact about the business, not a rollout stage.
	AllowNegativeStock = config.DeclareBool(config.Def{
		Key:     "inventory.allow_negative_stock",
		Default: false,
		// System and company only. Per-WAREHOUSE would arguably be righter — one bonded store
		// might tolerate what the shop floor must not — but config has no warehouse scope, and
		// inventing one for a single setting would be a platform change made from the wrong end.
		Scopes:      []config.Scope{config.ScopeSystem, config.ScopeCompany},
		Description: "settings.inventory.allow_negative_stock",
	})

	// CostingMethod is which strategy prices movements.
	//
	// Only "wac" is implemented in v1. The setting exists NOW, with the enum already naming
	// fifo and standard, so that adding one is a value change rather than a schema change
	// (§D.4) — and so that a company's stored choice does not have to be invented later.
	CostingMethod = config.DeclareEnum(config.Def{
		Key:         "inventory.costing_method",
		Default:     MethodWAC,
		Enum:        []string{MethodWAC, MethodFIFO, MethodStandard},
		Scopes:      []config.Scope{config.ScopeSystem, config.ScopeCompany},
		Description: "settings.inventory.costing_method",
	})
)

// The feature flags that gate lot and serial tracking (§21.2).
//
// A pharmacy needs lot expiry; a furniture store does not and must never encounter the concept.
// Off by default, so the ordinary install is the simple one.
var (
	LotTracking = config.DeclareFlag(config.FlagDef{
		Key:         "inventory.lot_tracking",
		Default:     false,
		Stability:   config.Stable,
		Scopes:      []config.Scope{config.ScopeSystem, config.ScopeCompany},
		Description: "flags.inventory.lot_tracking",
	})

	SerialTracking = config.DeclareFlag(config.FlagDef{
		Key:         "inventory.serial_tracking",
		Default:     false,
		Stability:   config.Stable,
		Scopes:      []config.Scope{config.ScopeSystem, config.ScopeCompany},
		Description: "flags.inventory.serial_tracking",
	})
)

// The costing methods §21.1 names. Only the first is implemented in v1.
const (
	MethodWAC      = "wac"
	MethodFIFO     = "fifo"
	MethodStandard = "standard"
)

// The audited actions (§15.3).
const (
	ActionStockReceived = "inventory.stock.received"
	ActionStockIssued   = "inventory.stock.issued"
	ActionStockAdjusted = "inventory.stock.adjusted"
	ActionStockCounted  = "inventory.stock.counted"
	ActionStockRebuilt  = "inventory.stock.rebuilt"

	EntityStock = "inventory.stock"
)

// Stable codes for this module's failures.
const (
	CodePublisherMissing = "inventory.publisher_missing"
	CodeUnknownVariant   = "inventory.unknown_variant"
	CodeUnknownWarehouse = "inventory.unknown_warehouse"
)

// Database is the narrow surface this module needs.
type Database interface {
	database.DB
	database.UnitOfWork
}

// Products reports what inventory needs to know about a product.
//
// A PORT, not an import of catalog. Inventory needs one fact — how finely this product is
// tracked — and the module-isolation rule (§10.3) says a module may reach another only through
// its contract package. Declaring the narrow shape here and satisfying it in the composition
// root keeps that true, and keeps this module testable with a two-line fake.
//
// The tracking mode lives on catalog's `products` table (Phase 3, 0016), where it was declared
// with lot and serial values from the start so that turning one on is a data change.
type Products interface {
	TrackingOf(ctx context.Context, productID id.ID) (domain.Tracking, error)
}

// ActorResolver reports who is acting, if anyone is.
//
// Declared here rather than imported from audit, so that inventory does not depend on audit's
// package for a two-method shape it happens to share — the module-isolation rule (§10.3). The
// composition root satisfies both from one implementation.
type ActorResolver interface {
	Actor(ctx context.Context) (Actor, bool)
}

// Actor is who made a movement.
type Actor struct {
	UserID   id.ID
	BranchID id.ID
}

// Options configures the service.
type Options struct {
	Clock    clock.Clock
	Bus      event.Publisher
	Settings *config.Settings
	Actors   ActorResolver
	Products Products
	Ledger   Ledger
	Logger   *slog.Logger
}

// Service is the inventory module's application layer.
type Service struct {
	db       Database
	repos    *sqlite.Repos
	clk      clock.Clock
	bus      event.Publisher
	settings *config.Settings
	actors   ActorResolver
	products Products
	ledger   Ledger
	control  ControlLedger
	logger   *slog.Logger
}

// NewService builds the service.
func NewService(db Database, opts Options) *Service {
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}
	return &Service{
		db: db, repos: sqlite.New(db, opts.Clock), clk: opts.Clock, bus: opts.Bus,
		settings: opts.Settings, actors: opts.Actors, products: opts.Products,
		ledger: opts.Ledger, logger: opts.Logger,
	}
}

// strategy builds the costing strategy for one warehouse.
//
// # Read fresh, not cached
//
// The costing method can change (§D.4 permits it at a period boundary), and a strategy captured
// at startup would keep pricing movements by the old method until a restart — silently, with the
// audit trail claiming the new one.
//
// # Per warehouse, not per company
//
// Whether negative stock is tolerated is a property of the PLACE: a bonded store may permit what
// the shop floor must not. Phase 1 put the column on `warehouses` for exactly this, and reading
// it here is what makes that seam real.
func (s *Service) strategy(ctx context.Context, warehouseID id.ID) (domain.Strategy, error) {
	allows, err := s.repos.WarehouseAllowsNegative(ctx, warehouseID)
	if err != nil {
		return nil, err
	}
	options := domain.Options{AllowNegativeStock: allows}

	// Only WAC exists in v1. When FIFO arrives it is selected HERE, by the setting, and nothing
	// else in this module changes — which is the whole point of the port.
	switch method := CostingMethod.Get(ctx); method {
	case MethodWAC:
		return domain.WAC{Options: options}, nil
	default:
		// A company whose setting names a method this build cannot price must be refused, not
		// silently costed by the default: valuing stock by a method nobody chose is exactly the
		// kind of wrong number that survives for a year.
		return nil, errs.Validation(domain.CodeUnknownStrategy,
			"this version cannot cost stock by that method").WithParam("method", method)
	}
}

// ── module ──────────────────────────────────────────────────────────────────────

// Module is the inventory module's registration surface.
type Module struct{ svc *Service }

var _ modules.Module = (*Module)(nil)

// NewModule builds the module around an already-constructed service.
func NewModule(svc *Service) *Module { return &Module{svc: svc} }

// Service exposes the module's application layer to the composition root.
func (m *Module) Service() *Service { return m.svc }

// Name identifies the module.
func (m *Module) Name() string { return "inventory" }

// DependsOn names the modules whose tables this one references.
func (m *Module) DependsOn() []string {
	// Migration ORDER: stock_movements keys warehouses, products, variants, and users; a lot
	// names the supplier it came from, for a recall.
	return []string{"org", "catalog", "identity", "partner"}
}

// Migrations returns the module's schema.
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return migrationFS
	}
	return sub
}

// Permissions gate seeing stock and changing it.
func (m *Module) Permissions() []auth.PermissionDef {
	return []auth.PermissionDef{
		{Code: PermStockView, Description: "permissions.inventory.stock.view"},
		{Code: PermStockAdjust, Description: "permissions.inventory.stock.adjust"},
		{Code: PermStockTransfer, Description: "permissions.inventory.stock.transfer"},
		{Code: PermStockCount, Description: "permissions.inventory.stock.count"},
		{Code: PermCostView, Description: "permissions.inventory.cost.view"},
		{Code: PermValuationView, Description: "permissions.inventory.valuation.view"},
	}
}

// Settings declares the two decisions a company makes about its stock.
func (m *Module) Settings() []config.Definition {
	return []config.Definition{
		mustDefinition(CostingMethod.Key()),
	}
}

func mustDefinition(key string) config.Definition {
	def, _ := config.Default().Lookup(key)
	return def
}

// FeatureFlags gate the CONCEPTS of lot and serial tracking (§21.2).
//
// The tables and columns exist for every install; what a furniture shop never sees is the idea.
// A flag rather than a setting because these are progressive delivery of something the product
// does (§17) — unlike "does this business tolerate negative stock", which is a fact about the
// business and therefore lives on the warehouse row.
func (m *Module) FeatureFlags() []config.FlagDef {
	return []config.FlagDef{
		mustFlag(LotTracking.Key()),
		mustFlag(SerialTracking.Key()),
	}
}

func mustFlag(key string) config.FlagDef {
	def, _ := config.Default().LookupFlag(key)
	return def
}

// Metadata: none. Stock is a customer's own data, never something the product ships.
func (m *Module) Metadata() []metadata.SeedSpec { return nil }

// Subscribe registers nothing yet. Phase 5's documents will drive movements through the service
// rather than through events, because a sale must fail if its stock movement fails — and an
// event that can be retried later is precisely the wrong shape for that.
func (m *Module) Subscribe(_ *eventbus.Bus, _ *outbox.Subscribers) error { return nil }

// Re-exported so callers need not import the domain package.
type (
	// Movement is one entry in the append-only ledger.
	Movement = domain.Movement
	// State is a variant's stock in one warehouse.
	State = domain.State
	// Type is what a movement is, and which way it points.
	Type = domain.Type
	// Discrepancy is one place a projection disagrees with the ledger.
	Discrepancy = domain.Discrepancy
	// Lot is a batch, with the dates that make it matter.
	Lot = domain.Lot
	// Serial is one physical unit.
	Serial = domain.Serial
	// Tracking is how finely a product is followed.
	Tracking = domain.Tracking
	// Level is a stored stock level.
	Level = sqlite.Level
)

// The movement types, re-exported.
const (
	Receipt       = domain.Receipt
	Issue         = domain.Issue
	AdjustmentIn  = domain.AdjustmentIn
	AdjustmentOut = domain.AdjustmentOut
	TransferOut   = domain.TransferOut
	TransferIn    = domain.TransferIn
	Count         = domain.Count
	Revaluation   = domain.Revaluation
	ReturnIn      = domain.ReturnIn
)

// Clock exposes the service's clock, so a test can rebuild the service with the same one.
func (s *Service) Clock() clock.Clock { return s.clk }

func (s *Service) requirePublisher() error {
	if s.bus == nil {
		return errs.Internal(CodePublisherMissing,
			"the inventory service was built without an event publisher")
	}
	return nil
}
