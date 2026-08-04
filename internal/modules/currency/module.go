package currency

import (
	"context"
	"embed"
	"io/fs"

	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/jobs"
	"github.com/mizan-erp/mizan/internal/platform/metadata"
	"github.com/mizan-erp/mizan/internal/platform/modules"
	"github.com/mizan-erp/mizan/internal/platform/outbox"
	"github.com/mizan-erp/mizan/internal/platform/strategy"
)

// The module owns its schema files. This is what makes Module.Migrations() a real statement
// rather than a formality: the SQL lives beside the code that depends on it, and the runner
// merges every module's files into one globally version-ordered set (§10.3).
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// Module is the currency module's registration surface.
type Module struct {
	svc *Service
}

var _ modules.Module = (*Module)(nil)

// NewModule builds the module around an already-constructed service.
func NewModule(svc *Service) *Module { return &Module{svc: svc} }

// Service exposes the module's application layer to the composition root.
func (m *Module) Service() *Service { return m.svc }

// Name identifies the module.
func (m *Module) Name() string { return "currency" }

// DependsOn is empty: currency is the foundation every other module builds on, so it depends
// on no other module — only on the platform.
func (m *Module) DependsOn() []string { return nil }

// Migrations returns the module's schema, rooted so filenames are bare.
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		// Unreachable: the path is a compile-time constant matched by the go:embed directive.
		return migrationFS
	}
	return sub
}

// Settings are the currency roles and the six rate-type context bindings.
func (m *Module) Settings() []config.Definition { return settingDefinitions() }

// FeatureFlags: none. The module has no progressively-revealed surface of its own — currency
// is either configured or it collapses to identity, which needs no flag.
func (m *Module) FeatureFlags() []config.FlagDef { return nil }

// Metadata is the seeded currencies and rate types, applied idempotently by code.
func (m *Module) Metadata() []metadata.SeedSpec {
	return []metadata.SeedSpec{currencySeeds(), rateTypeSeeds()}
}

// Subscribe registers nothing.
//
// Deliberate (Step 0.9, D8): no subscriber exists for a currency event yet. The real consumer
// is Phase 5 price recalculation, which will know what payload it needs. Publishing an event
// nobody listens to is a guess, and Step 0.6 declined to invent a fake currency event for
// exactly this reason. The seam is here and costs nothing until it is used.
func (m *Module) Subscribe(_ *eventbus.Bus, _ *outbox.Subscribers) error { return nil }

// Jobs: none yet. Scheduled rate fetching arrives with the providers in Phase 5; there is
// nothing to fetch from while the only rate source is manual entry.
func (m *Module) Jobs() []jobs.Registration { return nil }

// Bindings returns the Wails binding surface. Nil until Step 0.11 defines the frontend
// contract — a binding struct with no screen to call it would be shape without a consumer.
func (m *Module) Bindings() any { return nil }

// ── the rate-provider extension point ───────────────────────────────────────────

// RateProvider fetches rates from an external source (§18.3).
//
// The port is defined and the registry exists; NO network provider ships. Offline is a hard
// project constraint, and a real provider needs credentials, a schedule, and the preview
// workflow — all Phase 5. Defining the seam now costs a file and means Phase 5 adds an
// implementation rather than retrofitting an abstraction.
type RateProvider interface {
	// Key is the provider's stable identifier, recorded on every rate it fetches as
	// "provider:<key>" so a number's origin is always traceable.
	Key() string
	// Fetch returns rates from base into each target.
	Fetch(ctx context.Context, base string, targets []string) ([]FetchedRate, error)
}

// FetchedRate is one rate as a provider reports it.
type FetchedRate struct {
	From string
	To   string
	Nano int64 // ×10⁹
}

// RateProviders is the registry for the `rate_provider` extension point that Step 0.5 named.
//
// Owned by this module because it defines the contract type. A cross-module registry
// aggregate arrives when a second module owns an extension point of its own.
func RateProviders() *strategy.Registry[RateProvider] {
	return strategy.New[RateProvider](strategy.PointRateProvider)
}
