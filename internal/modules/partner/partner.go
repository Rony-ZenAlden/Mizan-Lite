// Package partner owns who a business trades with: customers, suppliers, and the people and
// places behind them.
//
// # One table, two roles (decision 9)
//
// A workshop that buys steel from a merchant and sells them finished brackets is ordinary. Two
// tables would make that either a duplicate identity nobody keeps in step, or a join nobody
// remembers to write — and the statement of account for such a company would be two queries a
// developer must think to combine.
package partner

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
	"github.com/mizan-erp/mizan/internal/modules/partner/domain"
	"github.com/mizan-erp/mizan/internal/modules/partner/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/platform/auth"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/jobs"
	"github.com/mizan-erp/mizan/internal/platform/metadata"
	"github.com/mizan-erp/mizan/internal/platform/modules"
	"github.com/mizan-erp/mizan/internal/platform/numbering"
	"github.com/mizan-erp/mizan/internal/platform/outbox"
)

// The module owns its schema (§10.3). Catalog owns 0015–0017, so partner owns 0018.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// The permissions this module protects.
//
// Split by ROLE as well as by action, because the two are genuinely different jobs: a
// salesperson maintains customers and has no business editing supplier terms, and a buyer the
// reverse. One `partner.manage` permission would force a business to choose between granting
// too much and granting nothing.
const (
	PermCustomerView   = "partner.customer.view"
	PermCustomerManage = "partner.customer.manage"
	PermSupplierView   = "partner.supplier.view"
	PermSupplierManage = "partner.supplier.manage"
)

// The audited actions (§15.3).
const (
	ActionPartnerCreated = "partner.created"
	ActionPartnerUpdated = "partner.updated"
	ActionPartnerDeleted = "partner.deleted"
	ActionAddressAdded   = "partner.address.added"
	ActionContactAdded   = "partner.contact.added"

	EntityPartner = "partner"
)

// Stable codes for this module's failures.
const (
	CodePublisherMissing = "partner.publisher_missing"
	CodeDuplicateCode    = "partner.duplicate_code"
	CodeUnknownPartner   = "partner.unknown"
	CodeNotACustomer     = "partner.not_a_customer"
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

	// The ledgers a balance needs (7.4). Optional.
	Receivables Receivables
	Payables    Payables
	Control     ControlLedger
}

// Service is the partner module's application layer.
type Service struct {
	db    Database
	repos *sqlite.Repos
	clk   clock.Clock
	bus   event.Publisher

	// The ledgers a BALANCE needs. A partner's position spans sales, purchasing and expenses,
	// and this module can import none of them — so each contributes through a port satisfied in
	// the composition root (7.4).
	//
	// Optional: a service built without them serves partners and refuses balances, which is what
	// a test that only cares about addresses should get.
	receivables Receivables
	payables    Payables
	control     ControlLedger
}

// NewService builds the service.
func NewService(db Database, opts Options) *Service {
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}
	return &Service{
		db: db, repos: sqlite.New(db, opts.Clock), clk: opts.Clock, bus: opts.Bus,
		receivables: opts.Receivables, payables: opts.Payables, control: opts.Control,
	}
}

// audit publishes inside the caller's transaction (1.7, phase D7).
func (s *Service) audit(ctx context.Context, a auditc.Auditable) error {
	if s.bus == nil {
		return errs.Internal(CodePublisherMissing,
			"the partner service was built without an event publisher")
	}
	return s.bus.Publish(ctx, a)
}

// ── module ──────────────────────────────────────────────────────────────────────

// Module is the partner module's registration surface.
type Module struct{ svc *Service }

var _ modules.Module = (*Module)(nil)

// NewModule builds the module around an already-constructed service.
func NewModule(svc *Service) *Module { return &Module{svc: svc} }

// Service exposes the module's application layer to the composition root.
func (m *Module) Service() *Service { return m.svc }

// Name identifies the module.
func (m *Module) Name() string { return "partner" }

// DependsOn names the modules whose tables this one references.
//
// Migration ORDER, not a code dependency: `partners` carries foreign keys to companies, accounts,
// tax_groups, and currencies, so those tables must exist first. The list grows with the keys
// that justify it rather than ahead of them.
func (m *Module) DependsOn() []string {
	return []string{"org", "accounting", "tax", "currency"}
}

// Migrations returns the module's schema.
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return migrationFS
	}
	return sub
}

// Permissions gate reading and editing each role.
func (m *Module) Permissions() []auth.PermissionDef {
	return []auth.PermissionDef{
		{Code: PermBalanceView, Description: "permissions.partner.balance.view"},
		{Code: PermCustomerView, Description: "permissions.partner.customer.view"},
		{Code: PermCustomerManage, Description: "permissions.partner.customer.manage"},
		{Code: PermSupplierView, Description: "permissions.partner.supplier.view"},
		{Code: PermSupplierManage, Description: "permissions.partner.supplier.manage"},
	}
}

// Settings: none yet.
func (m *Module) Settings() []config.Definition { return nil }

// FeatureFlags: none.
func (m *Module) FeatureFlags() []config.FlagDef { return nil }

// Metadata: none. A partner is a customer's own data, never something the product ships.
func (m *Module) Metadata() []metadata.SeedSpec { return nil }

// Subscribe registers nothing. Partners are read by sales and purchasing; they react to nothing.
func (m *Module) Subscribe(_ *eventbus.Bus, _ *outbox.Subscribers) error { return nil }

// Jobs: none.
func (m *Module) Jobs() []jobs.Registration { return nil }

// Filter narrows a partner listing.
type Filter = sqlite.Filter

// Partner and its parts, re-exported so callers need not import the domain package.
type (
	// Partner is a customer, a supplier, or both.
	Partner = domain.Partner
	// Address is one of a partner's addresses.
	Address = domain.Address
	// Contact is a person at a partner.
	Contact = domain.Contact
	// RoleUsage reports which of a partner's roles have been used.
	RoleUsage = domain.RoleUsage
)

// MarkHistory records that a partner now appears on a document.
//
// Phase 5 calls it with sold, Phase 6 with purchased. It exists now because it is what
// RequireDeletable and SetRoles READ, and a rule whose trigger does not exist yet is a rule no
// test can exercise — the same reason catalog's MarkVariantHistory was written in 3.2.
func (s *Service) MarkHistory(
	ctx context.Context, partnerID id.ID, sold, purchased bool,
) error {
	return s.db.Do(ctx, func(txCtx context.Context) error {
		return s.repos.MarkHistory(txCtx, partnerID, sold, purchased)
	})
}

// Series is empty: this module numbers no documents of its own.
func (m *Module) Series() []numbering.SeriesSpec { return nil }
