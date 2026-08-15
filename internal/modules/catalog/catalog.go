// Package catalog owns what a business trades: units of measure, products, variants, and the
// attributes that define them.
//
// Step 3.1 builds units of measure (§B). Products and variants follow in 3.2–3.3.
//
// # The decision that shapes everything after it
//
// §A.1: every product has at least one variant, including a bag of cement. Every downstream
// table — stock levels, sales lines, barcodes, price list items — references `variant_id NOT
// NULL`, so there is one column, one join, and one meaning rather than a nullable id that every
// query in five later phases would have to special-case.
package catalog

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/event"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/catalog/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/platform/auth"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/jobs"
	"github.com/mizan-erp/mizan/internal/platform/metadata"
	"github.com/mizan-erp/mizan/internal/platform/modules"
	"github.com/mizan-erp/mizan/internal/platform/numbering"
	"github.com/mizan-erp/mizan/internal/platform/outbox"
	"github.com/mizan-erp/mizan/internal/platform/seeds"
)

// The module owns its schema (§10.3). Tax owns 0014, so catalog owns 0015.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// The unit sets this build ships. Adding one is adding a file.
//
//go:embed seeds/uom/*.json
var seedFS embed.FS

// Stable error codes.
const (
	CodeInvalidUnitSet      = "catalog.invalid_unit_set"
	CodeShippedUnitsInvalid = "catalog.shipped_units_invalid"
	CodeUnknownUnitSet      = "catalog.unknown_unit_set"
	CodePublisherMissing    = "catalog.publisher_missing"
)

// The permissions this module protects.
const (
	PermCatalogView   = "catalog.view"
	PermCatalogManage = "catalog.manage"
)

// The audited actions (§15.3).
const (
	ActionUnitsApplied = "catalog.units.applied"
	EntityUnits        = "catalog.units"
)

// Database is the narrow surface this module needs.
type Database interface {
	database.DB
	database.UnitOfWork
}

// Options configures the service.
type Options struct {
	UserFS fs.FS
	Clock  clock.Clock
	Bus    event.Publisher
	Logger *slog.Logger
}

// Service is the catalog module's application layer.
type Service struct {
	db       Database
	repos    *sqlite.Repos
	clk      clock.Clock
	bus      event.Publisher
	unitSets []UnitSet
	problems []seeds.Problem
}

// NewService loads the unit sets and builds the service.
func NewService(db Database, opts Options) (*Service, error) {
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}

	layers := []seeds.Layer{{FS: seedFS, Origin: seeds.OriginShipped}}
	if opts.UserFS != nil {
		layers = append(layers, seeds.Layer{FS: opts.UserFS, Origin: seeds.OriginUser})
	}
	unitSets, problems, err := loadUnitSets(layers)
	if err != nil {
		return nil, err
	}

	svc := &Service{
		db: db, repos: sqlite.New(db, opts.Clock), clk: opts.Clock, bus: opts.Bus,
		unitSets: unitSets, problems: problems,
	}
	if opts.Logger != nil {
		for _, p := range problems {
			opts.Logger.Warn("unit set skipped",
				slog.String("path", p.Path), slog.String("origin", string(p.Origin)),
				slog.Any("error", p.Err))
		}
	}
	return svc, nil
}

// Problems reports files that were found but could not be used.
func (s *Service) Problems() []seeds.Problem {
	out := make([]seeds.Problem, len(s.problems))
	copy(out, s.problems)
	return out
}

// audit publishes inside the caller's transaction (1.7, phase D7).
func (s *Service) audit(ctx context.Context, a auditc.Auditable) error {
	if s.bus == nil {
		return errs.Internal(CodePublisherMissing,
			"the catalog service was built without an event publisher")
	}
	return s.bus.Publish(ctx, a)
}

// ── module ──────────────────────────────────────────────────────────────────────

// Module is the catalog module's registration surface.
type Module struct{ svc *Service }

var _ modules.Module = (*Module)(nil)

// NewModule builds the module around an already-constructed service.
func NewModule(svc *Service) *Module { return &Module{svc: svc} }

// Service exposes the module's application layer to the composition root.
func (m *Module) Service() *Service { return m.svc }

// Name identifies the module.
func (m *Module) Name() string { return "catalog" }

// DependsOn is empty for now.
//
// Units of measure reference nothing outside this module. Products will reference org and tax
// in 3.2, and this declaration grows with the foreign keys that justify it rather than ahead of
// them — DependsOn exists for migration ordering, and declaring a dependency with no key would
// constrain the sort for no reason.
func (m *Module) DependsOn() []string { return nil }

// Migrations returns the module's schema.
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return migrationFS
	}
	return sub
}

// Permissions gate reading and editing the catalog.
func (m *Module) Permissions() []auth.PermissionDef {
	return []auth.PermissionDef{
		{Code: PermCatalogView, Description: "permissions.catalog.view"},
		{Code: PermCatalogManage, Description: "permissions.catalog.manage"},
	}
}

// Settings: none yet.
func (m *Module) Settings() []config.Definition { return nil }

// FeatureFlags: none.
func (m *Module) FeatureFlags() []config.FlagDef { return nil }

// Metadata: none. Units arrive from seed FILES through ApplyUnits, not from the `code`-keyed
// metadata seeder — a unit set is a hierarchy of categories and units, which SeedSpec's flat
// row shape cannot express.
func (m *Module) Metadata() []metadata.SeedSpec { return nil }

// Subscribe registers nothing. The catalog is read by other modules; it reacts to nothing.
func (m *Module) Subscribe(_ *eventbus.Bus, _ *outbox.Subscribers) error { return nil }

// Jobs: none.
func (m *Module) Jobs() []jobs.Registration { return nil }

// Series is empty: this module numbers no documents of its own.
func (m *Module) Series() []numbering.SeriesSpec { return nil }
