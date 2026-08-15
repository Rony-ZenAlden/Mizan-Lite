// Package audit owns the append-only audit trail (§15).
//
// Entries are recorded INSIDE the transaction of the change that caused them (phase D7): a
// module publishes an Auditable event on the synchronous domain bus, and the subscriber here
// writes through the context's executor. Either both commit or neither does, and a failed audit
// write aborts the business operation.
//
// The trail is append-only, enforced by there being no update or delete anywhere in the module.
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
	"github.com/mizan-erp/mizan/internal/platform/numbering"
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
	repo   *repo
	actors ActorResolver
}

// NewService builds the service.
//
// actors may be nil — a graph with no API layer, as some tests build. An entry is then recorded
// unattributed rather than not at all: losing the actor is bad, losing the entry is worse.
func NewService(db database.DB, clk clock.Clock, actors ActorResolver) *Service {
	if clk == nil {
		clk = clock.System()
	}
	if actors == nil {
		actors = noActor{}
	}
	return &Service{repo: newRepo(db, clk), actors: actors}
}

// noActor reports that nobody is acting.
type noActor struct{}

func (noActor) Actor(context.Context) (Actor, bool) { return Actor{}, false }

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

// DependsOn is empty.
//
// Step 1.6 declared ["identity"], and that was wrong (1.7, D5): audit_log carries NO foreign
// key to users — deliberately, so a five-year-old entry does not depend on a live identity row
// (§15.1's snapshots, the same reasoning as 1.1 D4). With no FK there is no migration-ordering
// constraint, and DependsOn exists for exactly that.
//
// Identity IMPORTS audit's contract to publish events, which is the opposite direction and
// needs no declaration: the event type is resolved at compile time, and every subscription is
// registered before any of them can fire.
func (m *Module) DependsOn() []string { return nil }

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

// Subscribe registers the one handler that writes entries.
//
// On the SYNCHRONOUS domain bus, not the outbox. §23.1 reserves the outbox for integration
// events delivered AFTER commit, which is precisely the property phase D7 rejects: an entry
// that arrives after the change has already committed leaves a window in which the change
// exists and its record does not.
//
// Audit subscribing to other modules' events looks like it breaks §23.1's "domain events are
// within a module". It does not, and the distinction is worth stating: audit is CROSS-CUTTING
// INFRASTRUCTURE, like logging. It participates in no business decision and never calls back
// into a module. Modules publish Auditable; audit is its only subscriber; no module knows audit
// exists.
func (m *Module) Subscribe(bus *eventbus.Bus, _ *outbox.Subscribers) error {
	if m.svc == nil {
		return nil
	}
	return eventbus.Subscribe(bus, "audit.record", m.svc.record)
}

// Jobs: none. Retention pruning is Phase 9, and it is an audited operation itself.
func (m *Module) Jobs() []jobs.Registration { return nil }

// Series is empty: this module numbers no documents of its own.
func (m *Module) Series() []numbering.SeriesSpec { return nil }
