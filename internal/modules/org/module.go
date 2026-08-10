package org

import (
	"embed"
	"io/fs"

	"github.com/mizan-erp/mizan/internal/platform/auth"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/jobs"
	"github.com/mizan-erp/mizan/internal/platform/metadata"
	"github.com/mizan-erp/mizan/internal/platform/modules"
	"github.com/mizan-erp/mizan/internal/platform/outbox"
)

// The module owns its schema (§10.3). Currency owns 0003, so org owns 0004.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// Module is the org module's registration surface.
type Module struct {
	svc *Service
}

var _ modules.Module = (*Module)(nil)

// NewModule builds the module around an already-constructed service.
func NewModule(svc *Service) *Module { return &Module{svc: svc} }

// Service exposes the module's application layer to the composition root.
func (m *Module) Service() *Service { return m.svc }

// Name identifies the module.
func (m *Module) Name() string { return "org" }

// DependsOn declares currency.
//
// A real dependency, not bookkeeping: companies.functional_currency carries a foreign key to
// currencies(code), so currency's schema must exist first. This is also the first time the
// topological sort built in 0.10 orders anything — with one module it was trivially correct
// and entirely untested against a real edge.
func (m *Module) DependsOn() []string { return []string{"currency"} }

// Migrations returns the module's schema, rooted so filenames are bare.
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		// Unreachable: the path is a compile-time constant matched by the go:embed directive.
		return migrationFS
	}
	return sub
}

// Settings: none. The company's country and currencies are COLUMNS, not settings — they are
// attributes of a row that exists, and modelling them as scoped key/values would give two
// answers to "what currency is the ledger in?" (§CFG.1 keeps the mechanisms separate).
func (m *Module) Settings() []config.Definition { return nil }

// FeatureFlags: none. There is no progressively-revealed org surface; a company either exists
// or the setup wizard is showing.
func (m *Module) FeatureFlags() []config.FlagDef { return nil }

// Metadata: none, and this is the load-bearing omission of Step 1.1 (D1).
//
// PHASE_1_CORE_DATA §SEQ said this step would seed a default company/branch/warehouse. It must
// not: the setup wizard creates them in one transaction (§WIZ.1), and a company seeded at boot
// would make the wizard's "no company exists yet" invariant false before it ever ran, leaving
// a placeholder whose name, country, and currency are all wrong. §26.1's "seeded with one
// default row each" describes the state AFTER setup, not a seeder.
func (m *Module) Metadata() []metadata.SeedSpec { return nil }

// Subscribe registers nothing yet. Org will publish Auditable events once the audit module
// exists (1.7); publishing to no subscriber now would be the guess 0.6 and 0.9 both declined.
func (m *Module) Subscribe(_ *eventbus.Bus, _ *outbox.Subscribers) error { return nil }

// Permissions guard the organisational settings screens (Step 1.11).
func (m *Module) Permissions() []auth.PermissionDef {
	return []auth.PermissionDef{
		{Code: "org.company.edit", Description: "permissions.org.company.edit"},
		{Code: "org.branch.manage", Description: "permissions.org.branch.manage"},
		{Code: "org.warehouse.manage", Description: "permissions.org.warehouse.manage"},
		{Code: "org.fiscal.manage", Description: "permissions.org.fiscal.manage"},
	}
}

// Jobs: none.
func (m *Module) Jobs() []jobs.Registration { return nil }
