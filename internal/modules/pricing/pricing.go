// Package pricing owns what things cost: price lists, the prices in them, and the resolution
// that decides which one answers.
//
// # D1, decided in the Phase 3 design
//
// §33.1 asked whether v1 needs multiple price lists or one price with per-line discounts.
// Multiple, with resolution — because retrofitting a resolution layer onto sales lines that
// already exist means touching every document, every report, and every historical price. The
// schema cost now is two tables and one function.
//
// A single-price shop never sees it: one default list, no picker, no concept.
package pricing

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/event"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/pricing/domain"
	"github.com/mizan-erp/mizan/internal/modules/pricing/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/platform/auth"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/jobs"
	"github.com/mizan-erp/mizan/internal/platform/metadata"
	"github.com/mizan-erp/mizan/internal/platform/modules"
	"github.com/mizan-erp/mizan/internal/platform/outbox"
)

// The module owns its schema (§10.3). Partner owns 0018, so pricing owns 0019.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// The permissions this module protects.
//
// Viewing a price is not managing one, and the split matters here more than most: everybody who
// sells needs to READ prices, and almost nobody should be able to change them.
const (
	PermPriceView   = "pricing.view"
	PermPriceManage = "pricing.manage"
)

// The audited actions (§15.3).
const (
	ActionListCreated  = "pricing.list.created"
	ActionPriceSet     = "pricing.price.set"
	ActionListAssigned = "pricing.list.assigned"

	EntityPriceList = "pricing.list"
)

// Stable codes for this module's failures.
const (
	CodePublisherMissing = "pricing.publisher_missing"
	CodeDuplicateCode    = "pricing.duplicate_code"
	CodeUnknownList      = "pricing.unknown_list"
	CodeWrongDirection   = "pricing.wrong_direction"
)

// Database is the narrow surface this module needs.
type Database interface {
	database.DB
	database.UnitOfWork
}

// Options configures the service.
type Options struct {
	Clock  clock.Clock
	Bus    event.Publisher
	Logger *slog.Logger
}

// Service is the pricing module's application layer.
type Service struct {
	db    Database
	repos *sqlite.Repos
	clk   clock.Clock
	bus   event.Publisher
}

// NewService builds the service.
func NewService(db Database, opts Options) *Service {
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}
	return &Service{
		db: db, repos: sqlite.New(db, opts.Clock), clk: opts.Clock, bus: opts.Bus,
	}
}

func (s *Service) audit(ctx context.Context, a auditc.Auditable) error {
	if s.bus == nil {
		return errs.Internal(CodePublisherMissing,
			"the pricing service was built without an event publisher")
	}
	return s.bus.Publish(ctx, a)
}

// Re-exported so callers need not import the domain package.
type (
	// List is a set of prices.
	List = domain.List
	// Item is one price in a list.
	Item = domain.Item
	// Resolved is a price and the reason for it.
	Resolved = domain.Resolved
	// Direction separates what a business charges from what it pays.
	Direction = domain.Direction
)

// The directions, re-exported.
const (
	Sale     = domain.Sale
	Purchase = domain.Purchase
)

// ── module ──────────────────────────────────────────────────────────────────────

// Module is the pricing module's registration surface.
type Module struct{ svc *Service }

var _ modules.Module = (*Module)(nil)

// NewModule builds the module around an already-constructed service.
func NewModule(svc *Service) *Module { return &Module{svc: svc} }

// Service exposes the module's application layer to the composition root.
func (m *Module) Service() *Service { return m.svc }

// Name identifies the module.
func (m *Module) Name() string { return "pricing" }

// DependsOn names the modules whose tables this one references.
//
// Migration ORDER: price_list_items key products and variants, the assignments key partners and
// branches, and a list names a currency. All must exist first.
func (m *Module) DependsOn() []string {
	return []string{"org", "currency", "catalog", "partner"}
}

// Migrations returns the module's schema.
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return migrationFS
	}
	return sub
}

// Permissions gate reading and changing prices.
func (m *Module) Permissions() []auth.PermissionDef {
	return []auth.PermissionDef{
		{Code: PermPriceView, Description: "permissions.pricing.view"},
		{Code: PermPriceManage, Description: "permissions.pricing.manage"},
	}
}

// Settings: none yet.
func (m *Module) Settings() []config.Definition { return nil }

// FeatureFlags: none.
func (m *Module) FeatureFlags() []config.FlagDef { return nil }

// Metadata: none. A price is a customer's own data, never something the product ships.
func (m *Module) Metadata() []metadata.SeedSpec { return nil }

// Subscribe registers nothing. Prices are read by sales and purchasing; they react to nothing.
func (m *Module) Subscribe(_ *eventbus.Bus, _ *outbox.Subscribers) error { return nil }

// Jobs: none.
func (m *Module) Jobs() []jobs.Registration { return nil }

func upper(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// requireList resolves a list code, or reports that there is no such list.
func (s *Service) requireList(
	ctx context.Context, companyID id.ID, code string,
) (domain.List, error) {
	list, found, err := s.repos.ListByCode(ctx, companyID, upper(code))
	if err != nil {
		return domain.List{}, err
	}
	if !found {
		return domain.List{}, errs.NotFound(CodeUnknownList,
			"there is no price list with that code").WithParam("code", upper(code))
	}
	return list, nil
}
