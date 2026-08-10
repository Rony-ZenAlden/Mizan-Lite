// Package profile owns country and business profiles — the data that makes Mizan generic
// across countries and trades without a per-customer build (§16.4, Addendum §C).
//
// Two kinds of profile, deliberately shaped differently:
//
//   - A COUNTRY profile supplies jurisdictional defaults to the setup wizard and is never
//     stored (D1). Read once, copied into settings, then irrelevant.
//   - A BUSINESS profile is a bundle of settings and flags. Its catalogue row is stored so the
//     UI can list it; its bundle stays in the file and is applied once.
//
// Both come from JSON files in two layers: what this binary ships, and what an administrator
// dropped into the data directory. That second layer is what makes Addendum §C's promise true
// — "adding a country is dropping in a JSON file, no code, no release".
package profile

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/event"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/platform/auth"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/jobs"
	"github.com/mizan-erp/mizan/internal/platform/metadata"
	"github.com/mizan-erp/mizan/internal/platform/modules"
	"github.com/mizan-erp/mizan/internal/platform/outbox"
	"github.com/mizan-erp/mizan/internal/platform/seeds"
)

// The module owns its schema (§10.3). Audit owns 0008, so profile owns 0009.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// The profiles this build ships. Adding one is adding a file — there is no registration list,
// on purpose: a list is a second place to forget.
//
//go:embed seeds/country_profiles/*.json seeds/business_profiles/*.json
var seedFS embed.FS

// Stable error codes. They double as i18n keys and are part of the public contract.
const (
	CodeInvalidProfile = "profile.invalid"
	CodeShippedInvalid = "profile.shipped_invalid"
	CodeUndeclaredKey  = "profile.undeclared_key"
	CodeUnknownProfile = "profile.unknown"
	CodeNotConfigured  = "profile.not_configured"
	CodePublisherError = "profile.publisher_missing"
)

// UserSeedDir is the folder an administrator drops profiles into, under the data directory.
const UserSeedDir = "seeds"

// Database is the narrow surface this module needs.
type Database interface {
	database.DB
	database.UnitOfWork
}

// Options configures the service.
type Options struct {
	// UserFS is the customer's overlay, usually os.DirFS(<dataDir>). Nil means "no overlay",
	// which is the normal state until someone adds a file.
	UserFS fs.FS
	// Registry is where bundle keys are checked. Nil uses the process-wide default.
	Registry Registry
	// Settings is what Apply writes to. Nil makes Apply fail rather than silently do nothing.
	Settings *config.Settings
	Clock    clock.Clock
	Bus      event.Publisher
	Logger   *slog.Logger
}

// Service is the profile module's application layer.
//
// The profiles are loaded ONCE, at construction, and held. They are small, immutable for the
// life of the process, and read during a wizard step where a disk read would be a strange
// place to fail. Re-reading them would also mean a file edited mid-session takes effect
// halfway through setup, which is worse than needing a restart.
type Service struct {
	db        Database
	settings  *config.Settings
	bus       event.Publisher
	clk       clock.Clock
	countries []Country
	business  []Business
	problems  []seeds.Problem
}

// NewService loads the profiles and builds the service.
//
// It returns an error ONLY for a file this build ships (D4). A customer's broken file becomes a
// Problem: reported, logged, skipped, and the shipped file it was shadowing stays in force. A
// seed file must not be able to stop a shop from opening.
func NewService(db Database, opts Options) (*Service, error) {
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}
	registry := opts.Registry
	if registry == nil {
		registry = config.Default()
	}

	layers := []seeds.Layer{{FS: seedFS, Origin: seeds.OriginShipped}}
	if opts.UserFS != nil {
		layers = append(layers, seeds.Layer{FS: opts.UserFS, Origin: seeds.OriginUser})
	}

	countries, countryProblems, err := loadCountries(layers)
	if err != nil {
		return nil, err
	}
	business, businessProblems, err := loadBusiness(layers, registry)
	if err != nil {
		return nil, err
	}

	svc := &Service{
		db: db, settings: opts.Settings, bus: opts.Bus, clk: opts.Clock,
		countries: countries, business: business,
		problems: append(countryProblems, businessProblems...),
	}
	svc.report(opts.Logger)
	return svc, nil
}

// report logs what could not be used.
//
// Logged rather than swallowed: the administrator who wrote the file gets no other signal, and
// "I added the file and nothing happened" is an unanswerable support call otherwise. The
// diagnostics screen (Phase 9) is where these eventually surface to the user.
func (s *Service) report(logger *slog.Logger) {
	if logger == nil || len(s.problems) == 0 {
		return
	}
	for _, p := range s.problems {
		logger.Warn("seed file skipped",
			slog.String("path", p.Path), slog.String("origin", string(p.Origin)),
			slog.Any("error", p.Err))
	}
}

// UserFS opens the overlay directory beneath a data directory.
//
// Returns nil when it does not exist, which is the normal case: a customer who has never added
// a profile has no such folder, and creating one on their behalf would suggest we expected
// something to be in it.
func UserFS(dataDir string) fs.FS {
	if dataDir == "" {
		return nil
	}
	root := filepath.Join(dataDir, UserSeedDir)
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return nil
	}
	// Rooted at the data directory, not at <dataDir>/seeds, because Discover searches for
	// "seeds/country_profiles" — the same relative path it searches in the embedded FS. One
	// path, two layers, no special case.
	return os.DirFS(dataDir)
}

// Countries lists the loaded country profiles, ordered by code.
func (s *Service) Countries() []Country {
	out := make([]Country, len(s.countries))
	copy(out, s.countries)
	return out
}

// Country returns one profile by ISO code.
func (s *Service) Country(code string) (Country, bool) {
	for _, c := range s.countries {
		if c.Code == code {
			return c, true
		}
	}
	return Country{}, false
}

// BusinessProfiles lists the loaded bundles, ordered by code.
func (s *Service) BusinessProfiles() []Business {
	out := make([]Business, len(s.business))
	copy(out, s.business)
	return out
}

// Business returns one bundle by code.
func (s *Service) Business(code string) (Business, bool) {
	for _, b := range s.business {
		if b.Code == code {
			return b, true
		}
	}
	return Business{}, false
}

// Problems reports the files that were found but could not be used.
func (s *Service) Problems() []seeds.Problem {
	out := make([]seeds.Problem, len(s.problems))
	copy(out, s.problems)
	return out
}

// audit publishes inside the caller's transaction (1.7, phase D7).
func (s *Service) audit(ctx context.Context, a auditc.Auditable) error {
	if s.bus == nil {
		return errs.Internal(CodePublisherError,
			"the profile service was built without an event publisher")
	}
	return s.bus.Publish(ctx, a)
}

// ── module ──────────────────────────────────────────────────────────────────────

// Module is the profile module's registration surface.
type Module struct {
	svc *Service
}

var _ modules.Module = (*Module)(nil)

// NewModule builds the module around an already-constructed service.
func NewModule(svc *Service) *Module { return &Module{svc: svc} }

// Service exposes the module's application layer to the composition root.
func (m *Module) Service() *Service { return m.svc }

// Name identifies the module.
func (m *Module) Name() string { return "profile" }

// DependsOn is empty: business_profiles is a standalone catalogue with no foreign key.
//
// Country profiles are not stored at all (D1), so they impose no ordering either. The module
// READS the settings registry, but that is a process-wide declaration resolved before any
// module is constructed, not a schema dependency.
func (m *Module) DependsOn() []string { return nil }

// Migrations returns the module's schema.
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return migrationFS
	}
	return sub
}

// Metadata seeds the business-profile CATALOGUE from the loaded bundles.
//
// The rows come from files rather than Go literals, which is the item 0.5 D7 deferred and 0.9
// and 0.12 carried. It needed no change to modules.Module: Metadata() always returned
// []SeedSpec, and where the specs came from was never part of its contract.
func (m *Module) Metadata() []metadata.SeedSpec {
	if m.svc == nil {
		return nil
	}
	return []metadata.SeedSpec{catalogueSeed(m.svc.business)}
}

// Settings: none. A profile WRITES settings; it declares none of its own.
func (m *Module) Settings() []config.Definition { return nil }

// FeatureFlags: none, for the same reason.
func (m *Module) FeatureFlags() []config.FlagDef { return nil }

// Permissions: none yet.
//
// Applying a profile happens during setup, which is gated by "no company exists yet" rather
// than by a permission (§WIZ.1) — there is no user to hold one. Re-applying a profile to a
// configured company is a Settings-screen operation that does not exist yet, and declaring its
// permission now would declare a code nothing checks.
func (m *Module) Permissions() []auth.PermissionDef { return nil }

// Subscribe registers nothing. This module publishes; it reacts to nothing.
func (m *Module) Subscribe(_ *eventbus.Bus, _ *outbox.Subscribers) error { return nil }

// Jobs: none.
func (m *Module) Jobs() []jobs.Registration { return nil }

// SortedCountryCodes is a small convenience for diagnostics and tests.
func SortedCountryCodes(countries []Country) []string {
	out := make([]string, 0, len(countries))
	for _, c := range countries {
		out = append(out, c.Code)
	}
	sort.Strings(out)
	return out
}
