// Package audit owns the append-only audit trail (§15).
//
// This step (1.6) builds the schema and the READ path, because field-level redaction needs a
// response with a field worth gating. Step 1.7 adds the write path: the Auditable domain event
// and the subscribers that record it INSIDE the business transaction (phase D7), so a change
// and its audit record commit together or neither does.
package audit

import (
	"context"
	"embed"
	"io/fs"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/auth"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/jobs"
	"github.com/mizan-erp/mizan/internal/platform/metadata"
	"github.com/mizan-erp/mizan/internal/platform/modules"
	"github.com/mizan-erp/mizan/internal/platform/outbox"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// The permissions this module protects.
//
// The split is real, not a fixture for the redaction mechanism. Audit payloads hold exactly the
// detail a junior user should not read — an old salary, an old price, an old credit limit —
// while the FACT that a change happened is ordinary operational information a manager needs.
const (
	PermView        = "audit.entry.view"
	PermViewPayload = "audit.entry.view_payload"
)

// Entry is one recorded action.
//
// Payload fields are plain strings HERE, in the domain: redaction is an API-boundary concern,
// and a domain type that could not hold the value would make the write path (1.7) impossible.
type Entry struct {
	ID         id.ID
	OccurredAt time.Time

	ActorUserID id.ID
	ActorName   string
	BranchID    id.ID
	SessionID   id.ID
	Correlation id.ID

	Action      string
	EntityType  string
	EntityID    id.ID
	EntityLabel string

	BeforeJSON    string
	AfterJSON     string
	ChangedFields string

	Source     string
	DeviceInfo string
}

// Filter narrows an entry query.
type Filter struct {
	EntityType string
	EntityID   id.ID
	ActorID    id.ID
	Limit      int
}

// Service is the audit module's application layer.
type Service struct {
	repo *repo
}

// NewService builds the service.
func NewService(db database.DB, clk clock.Clock) *Service {
	if clk == nil {
		clk = clock.System()
	}
	return &Service{repo: newRepo(db, clk)}
}

// Entries lists audit records, newest first.
//
// There is deliberately no Update and no Delete anywhere in this module. Append-only (§15.1) is
// enforced by there being no code that could do otherwise, not by a comment asking nicely.
func (s *Service) Entries(ctx context.Context, f Filter) ([]Entry, error) {
	return s.repo.entries(ctx, f)
}

// ── module ──────────────────────────────────────────────────────────────────────

// Module is the audit module's registration surface.
type Module struct {
	svc *Service
}

var _ modules.Module = (*Module)(nil)

// NewModule builds the module around an already-constructed service.
func NewModule(svc *Service) *Module { return &Module{svc: svc} }

// Service exposes the module's application layer to the composition root.
func (m *Module) Service() *Service { return m.svc }

// Name identifies the module.
func (m *Module) Name() string { return "audit" }

// DependsOn declares identity: audit rows reference a user, and the read path is gated by
// permissions identity resolves.
func (m *Module) DependsOn() []string { return []string{"identity"} }

// Migrations returns the module's schema.
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return migrationFS
	}
	return sub
}

// Permissions gate the list and, separately, the payload within it.
func (m *Module) Permissions() []auth.PermissionDef {
	return []auth.PermissionDef{
		{Code: PermView, Description: "permissions.audit.entry.view"},
		{Code: PermViewPayload, Description: "permissions.audit.entry.view_payload"},
	}
}

// Settings: none. Retention is an administrative operation that itself writes an audit record
// (§15.1), not a knob — and it arrives with the Phase 9 diagnostics surface.
func (m *Module) Settings() []config.Definition { return nil }

// FeatureFlags: none. Auditing is never optional.
func (m *Module) FeatureFlags() []config.FlagDef { return nil }

// Metadata: none.
func (m *Module) Metadata() []metadata.SeedSpec { return nil }

// Subscribe registers nothing YET. The subscribers that write entries land in 1.7, on the
// synchronous domain bus so an entry commits in the same transaction as the change it records
// (phase D7).
func (m *Module) Subscribe(_ *eventbus.Bus, _ *outbox.Subscribers) error { return nil }

// Jobs: none. Retention pruning is Phase 9, and it is an audited operation itself.
func (m *Module) Jobs() []jobs.Registration { return nil }

// Bindings: none. The Audit binding lives in internal/api/bindings with the rest of the static
// façade set (0.11 D2), since Module.Bindings() cannot be collected before Wails runs.
func (m *Module) Bindings() any { return nil }
