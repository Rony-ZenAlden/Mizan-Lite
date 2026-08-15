// Package sales owns the money coming in: quotations, orders, invoices, returns, payments, and
// the point of sale.
//
// # This module is mostly composition
//
// Almost everything a sale needs was built earlier and has been waiting for a caller: price
// resolution (3.5), the tax engine (2.6), the stock ledger and costing port (4.1–4.2), the
// posting rules that Phase 2 seeded under `sales.invoice.posted` and that have never fired.
// Phase 5 is where those seams find out whether they were designed for a real caller.
//
// The two genuinely new mechanisms are number series and the document SNAPSHOT — the rule that a
// posted line stores what it was computed from rather than a reference to something that can
// change (§9.3).
package sales

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
	"github.com/mizan-erp/mizan/internal/modules/sales/domain"
	"github.com/mizan-erp/mizan/internal/modules/sales/infra/sqlite"
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

// The module owns its schema (§10.3). Inventory owns 0020–0022, so sales owns 0023.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// The permissions this module protects.
const (
	PermSaleView     = "sales.document.view"
	PermSaleDraft    = "sales.document.draft"
	PermSalePost     = "sales.document.post"
	PermSaleCancel   = "sales.document.cancel"
	PermSeriesManage = "sales.series.manage"
)

// The audited actions (§15.3).
const (
	ActionSeriesCreated = "sales.series.created"

	EntitySeries = "sales.series"
)

// Stable codes for this module's failures.
const (
	CodePublisherMissing = "sales.publisher_missing"
	// Series errors now come from the platform allocator. Re-exported so this module's
	// callers and tests need not learn where the mechanism lives.
	CodeDuplicateSeries = numbering.CodeDuplicateSeries
	CodeUnknownSeries   = numbering.CodeUnknownSeries
)

// The series codes this build allocates from.
const (
	SeriesInvoice    = "SALES_INVOICE"
	SeriesCreditNote = "SALES_CREDIT_NOTE"
	SeriesOrder      = "SALES_ORDER"
	SeriesQuotation  = "SALES_QUOTATION"
)

// Database is the narrow surface this module needs.
type Database interface {
	database.DB
	database.UnitOfWork
}

// Options configures the service.
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
	Credit  Credit
	// Messages translates printed labels. Optional.
	Messages Translator
	Logger   *slog.Logger
}

// Service is the sales module's application layer.
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
	credit  Credit
	// messages translates the labels on a printed document. Optional: a service built without
	// one prints keys, which is visible and harmless, rather than refusing to construct.
	messages Translator
	// numbers is the platform's allocator, shared with every other transactional module.
	numbers *numbering.Allocator
	logger  *slog.Logger
}

// NewService builds the service.
func NewService(db Database, opts Options) *Service {
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}
	return &Service{
		db: db, repos: sqlite.New(db, opts.Clock), clk: opts.Clock,
		numbers: numbering.New(db, opts.Clock),
		bus:     opts.Bus, actors: opts.Actors, catalog: opts.Catalog,
		pricing: opts.Pricing, tax: opts.Tax, stock: opts.Stock, credit: opts.Credit,
		messages: opts.Messages,
		logger:   opts.Logger,
	}
}

func (s *Service) audit(ctx context.Context, a auditc.Auditable) error {
	if s.bus == nil {
		return errs.Internal(CodePublisherMissing,
			"the sales service was built without an event publisher")
	}
	return s.bus.Publish(ctx, a)
}

// ── numbering ───────────────────────────────────────────────────────────────────

// NewSeriesInput describes a document numbering sequence.
type NewSeriesInput struct {
	CompanyID id.ID
	BranchID  id.ID
	Code      string
	Prefix    string
	Suffix    string
	Padding   int
}

// CreateSeries adds a numbering sequence.
//
// The series LIVES in the platform's table and is allocated by the platform's allocator; what
// sales adds here is the audit entry, because creating a series is an administrative act by a
// person and the platform has no opinion about people.
func (s *Service) CreateSeries(ctx context.Context, in NewSeriesInput) (numbering.Series, error) {
	var created numbering.Series

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		identifier, err := id.New()
		if err != nil {
			return err
		}
		built, err := numbering.NewSeries(identifier, in.Code, in.Prefix, in.Padding)
		if err != nil {
			return err
		}
		built.Suffix = in.Suffix
		built.BranchID = in.BranchID

		if err = s.numbers.Create(txCtx, built); err != nil {
			return err
		}
		created = built

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionSeriesCreated, EntityType: EntitySeries, EntityID: built.ID,
			After: map[string]any{
				"code": built.Code, "prefix": built.Prefix, "padding": built.Padding,
			},
		})
	})
	if err != nil {
		return numbering.Series{}, err
	}
	return created, nil
}

// allocateNumber takes the next number from a series, INSIDE the caller's transaction.
//
// # Why this stays unexported even though the allocator moved
//
// The allocator is now platform (`internal/platform/numbering`), because purchasing needs one
// too and could neither import sales nor safely build a second — which is what sales 0023
// predicted when it declined to create a second series TABLE.
//
// This wrapper remains unexported for the reason the original comment gave: no caller outside
// this module should take a number without a document to attach it to. The structural guard is
// worth keeping even though the mechanism it guards now lives elsewhere.
func (s *Service) allocateNumber(
	ctx context.Context, branchID id.ID, code string,
) (string, error) {
	return s.numbers.Allocate(ctx, branchID, code)
}

// PreviewNumber reports what the next number WOULD be, without consuming it.
func (s *Service) PreviewNumber(
	ctx context.Context, branchID id.ID, code string,
) (string, error) {
	return s.numbers.Preview(ctx, branchID, code)
}

// ── module ──────────────────────────────────────────────────────────────────────

// Module is the sales module's registration surface.
type Module struct{ svc *Service }

var _ modules.Module = (*Module)(nil)

// NewModule builds the module around an already-constructed service.
func NewModule(svc *Service) *Module { return &Module{svc: svc} }

// Service exposes the module's application layer to the composition root.
func (m *Module) Service() *Service { return m.svc }

// Name identifies the module.
func (m *Module) Name() string { return "sales" }

// DependsOn names the modules whose tables this one references.
func (m *Module) DependsOn() []string {
	return []string{"org", "accounting"}
}

// Migrations returns the module's schema.
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return migrationFS
	}
	return sub
}

// Permissions gate seeing, drafting, posting, and cancelling.
//
// Drafting and POSTING are separate, deliberately: in many shops an assistant prepares an
// invoice and somebody else commits it. Posting is the irreversible act — it issues stock, moves
// the books, and consumes a number — so it is the one that deserves its own grant.
func (m *Module) Permissions() []auth.PermissionDef {
	return []auth.PermissionDef{
		{Code: PermSaleView, Description: "permissions.sales.document.view"},
		{Code: PermSaleDraft, Description: "permissions.sales.document.draft"},
		{Code: PermSalePost, Description: "permissions.sales.document.post"},
		{Code: PermSaleCancel, Description: "permissions.sales.document.cancel"},
		{Code: PermSalePrint, Description: "permissions.sales.document.print"},
		{Code: PermSeriesManage, Description: "permissions.sales.series.manage"},
		{Code: PermShiftOpen, Description: "permissions.pos.shift.open"},
		{Code: PermShiftClose, Description: "permissions.pos.shift.close"},
		{Code: PermAnalysisView, Description: "permissions.sales.analysis.view"},
	}
}

// Settings: none yet.
func (m *Module) Settings() []config.Definition { return nil }

// FeatureFlags: none yet.
func (m *Module) FeatureFlags() []config.FlagDef { return nil }

// Metadata: none. A number series is a customer's own configuration, created by the setup wizard
// rather than shipped — a shop's invoice prefix is not ours to choose.
func (m *Module) Metadata() []metadata.SeedSpec { return nil }

// Subscribe registers nothing. Sales publishes; it does not react.
func (m *Module) Subscribe(_ *eventbus.Bus, _ *outbox.Subscribers) error { return nil }

// Jobs: none yet.
func (m *Module) Jobs() []jobs.Registration { return nil }

// Re-exported so callers need not import the domain package.
type (
	// Series is a document numbering sequence, owned by the platform allocator since Phase 6.
	Series = numbering.Series
	// Document is a sale in one of its forms.
	Document = domain.Document
	// Line is one item on a document.
	Line = domain.Line
	// Type is what kind of document this is.
	Type = domain.Type
	// Status is where a document has got to.
	Status = domain.Status
)

// The document types, re-exported.
const (
	Quotation  = domain.Quotation
	Order      = domain.Order
	Invoice    = domain.Invoice
	CreditNote = domain.CreditNote
)

// The statuses, re-exported.
const (
	Draft     = domain.Draft
	Posted    = domain.Posted
	Cancelled = domain.Cancelled
)

// Series are the five sequences sales allocates from.
//
// The prefixes are what a fresh install starts with, not what it is stuck with: 0001's series
// table carries prefix and padding as data, this module exposes CreateSeries for a branch that
// wants its own, and Ensure never overwrites one that exists. What the declaration guarantees is
// that a company which changed nothing can still post an invoice — which is what it could not do
// before the Phase 7 review went looking for the caller.
func (m *Module) Series() []numbering.SeriesSpec {
	return []numbering.SeriesSpec{
		{Code: SeriesInvoice, Prefix: "INV-", Padding: 6},
		{Code: SeriesCreditNote, Prefix: "CN-", Padding: 6},
		{Code: SeriesOrder, Prefix: "SO-", Padding: 6},
		{Code: SeriesQuotation, Prefix: "QT-", Padding: 6},
		{Code: SeriesPayment, Prefix: "RCT-", Padding: 6},
	}
}
