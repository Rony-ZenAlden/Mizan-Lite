package identity

import (
	"embed"
	"io/fs"

	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/jobs"
	"github.com/mizan-erp/mizan/internal/platform/metadata"
	"github.com/mizan-erp/mizan/internal/platform/modules"
	"github.com/mizan-erp/mizan/internal/platform/outbox"
)

// The module owns its schema (§10.3). Org owns 0004, so identity owns 0005.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// Module is the identity module's registration surface.
type Module struct {
	svc *Service
}

var _ modules.Module = (*Module)(nil)

// NewModule builds the module around an already-constructed service.
func NewModule(svc *Service) *Module { return &Module{svc: svc} }

// Service exposes the module's application layer to the composition root.
func (m *Module) Service() *Service { return m.svc }

// Name identifies the module.
func (m *Module) Name() string { return "identity" }

// DependsOn declares org: users.company_id carries a foreign key to companies(id), so org's
// schema must exist first.
func (m *Module) DependsOn() []string { return []string{"org"} }

// Migrations returns the module's schema, rooted so filenames are bare.
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		// Unreachable: the path is a compile-time constant matched by the go:embed directive.
		return migrationFS
	}
	return sub
}

// Settings are the password policy (§13.1).
func (m *Module) Settings() []config.Definition { return settingDefinitions() }

// FeatureFlags: none. Authentication is not a progressively-revealed surface.
func (m *Module) FeatureFlags() []config.FlagDef { return nil }

// Metadata: none. Users are created by the setup wizard and by administrators, never seeded —
// a seeded account is a default credential, and §13.1 is explicit that none ships.
func (m *Module) Metadata() []metadata.SeedSpec { return nil }

// Subscribe registers nothing yet. Identity will publish Auditable events — created, password
// changed, deactivated — once the audit module exists (1.7).
func (m *Module) Subscribe(_ *eventbus.Bus, _ *outbox.Subscribers) error { return nil }

// Jobs: none. Session expiry sweeping arrives with sessions in 1.3.
func (m *Module) Jobs() []jobs.Def { return nil }

// Bindings: none in this step. Login lands in 1.3 with sessions, and the user admin screens in
// 1.11 — a binding with no screen would be shape without a consumer.
func (m *Module) Bindings() any { return nil }
