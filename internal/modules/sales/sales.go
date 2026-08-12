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
	CodeDuplicateSeries  = "sales.duplicate_series"
	CodeUnknownSeries    = "sales.unknown_series"
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
	Logger  *slog.Logger
}

// Service is the sales module's application layer.
type Service struct {
	db      Database
	repos   *sqlite.Repos
	clk     clock.Clock
	bus     event.Publisher
	actors  ActorResolver
	catalog Catalog
	logger  *slog.Logger
}

// NewService builds the service.
func NewService(db Database, opts Options) *Service {
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}
	return &Service{
		db: db, repos: sqlite.New(db, opts.Clock), clk: opts.Clock,
		bus: opts.Bus, actors: opts.Actors, catalog: opts.Catalog, logger: opts.Logger,
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
func (s *Service) CreateSeries(ctx context.Context, in NewSeriesInput) (domain.Series, error) {
	var created domain.Series

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		identifier, err := id.New()
		if err != nil {
			return err
		}
		built, err := domain.NewSeries(identifier, in.Code, in.Prefix, in.Padding)
		if err != nil {
			return err
		}
		built.Suffix = in.Suffix
		built.BranchID = in.BranchID

		if err = s.repos.InsertSeries(txCtx, built); err != nil {
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
		return domain.Series{}, err
	}
	return created, nil
}

// allocateNumber takes the next number from a series, INSIDE the caller's transaction.
//
// # Why this is unexported and takes a transaction context
//
// A number must be allocated in the same transaction that writes the document it numbers. If the
// allocation committed separately, a posting that then failed would leave a consumed number and
// no document — a gap on every failure rather than only on an abandoned draft.
//
// Making it unexported means no caller outside this module can take a number without a document
// to attach it to. That is the structural version of the rule, rather than a comment asking
// people to remember it.
func (s *Service) allocateNumber(
	ctx context.Context, branchID id.ID, code string,
) (string, error) {
	series, found, err := s.repos.SeriesFor(ctx, branchID, code)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errs.NotFound(CodeUnknownSeries,
			"there is no number series for that kind of document").WithParam("code", code)
	}

	number, advanced, err := series.Next()
	if err != nil {
		return "", err
	}
	if err = s.repos.AdvanceSeries(ctx, series.ID, advanced.NextValue); err != nil {
		return "", err
	}
	return number, nil
}

// PreviewNumber reports what the next number WOULD be, without consuming it.
//
// For a screen that shows "this will be INV-000124". Reads the counter and formats it; takes
// nothing. The separation of Format from Next in the domain is what makes this possible without
// a second implementation of the padding rules.
func (s *Service) PreviewNumber(
	ctx context.Context, branchID id.ID, code string,
) (string, error) {
	series, found, err := s.repos.SeriesFor(ctx, branchID, code)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errs.NotFound(CodeUnknownSeries,
			"there is no number series for that kind of document").WithParam("code", code)
	}
	return series.Format(series.NextValue), nil
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
		{Code: PermSeriesManage, Description: "permissions.sales.series.manage"},
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
	// Series is a document numbering sequence.
	Series = domain.Series
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
