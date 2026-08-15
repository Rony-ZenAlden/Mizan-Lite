package identity

import (
	"context"
	"embed"
	"io/fs"
	"time"

	"github.com/mizan-erp/mizan/internal/platform/auth"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/jobs"
	"github.com/mizan-erp/mizan/internal/platform/metadata"
	"github.com/mizan-erp/mizan/internal/platform/modules"
	"github.com/mizan-erp/mizan/internal/platform/numbering"
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

// Permissions are the user, role, and session operations this module protects.
func (m *Module) Permissions() []auth.PermissionDef { return declaredPermissions() }

// Jobs declares the session sweep — the first job declared by a MODULE rather than by the
// platform itself (0.7 shipped outbox.dispatch and platform.heartbeat).
//
// CatchUp Skip because it is pure housekeeping: a session is validated against its own
// timestamps on every use, so a sweep missed while the shop was closed changes nothing about
// whether an expired session works. That is exactly what Skip means (0.7 §5.2), and using
// RunOnce here would schedule work whose only effect is to tidy rows nobody is reading.
func (m *Module) Jobs() []jobs.Registration {
	if m.svc == nil {
		return nil
	}
	return []jobs.Registration{{
		Def: jobs.Def{
			Key:         "identity.session_sweep",
			Schedule:    jobs.Every(time.Hour),
			CatchUp:     jobs.Skip,
			Timeout:     2 * time.Minute,
			Description: "jobs.identity.session_sweep",
		},
		Handler: func(ctx context.Context, _ jobs.RunContext) error {
			_, _, err := m.svc.SweepSessions(ctx)
			return err
		},
	}}
}

// Series is empty: this module numbers no documents of its own.
func (m *Module) Series() []numbering.SeriesSpec { return nil }
