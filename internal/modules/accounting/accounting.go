// Package accounting owns the general ledger (§20).
//
// Built completely from day one and exposed progressively in the UI (§20.6): every transaction
// the product records will keep perfect double-entry books from the first release, whether or
// not any screen shows them. That is only possible if the ledger exists before the first
// invoice does, which is what this module is.
//
// Step 2.1 builds the chart of accounts and the mapping layer posting rules resolve through.
// Journal entries and the balance invariant arrive in 2.2.
package accounting

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"sort"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/event"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/accounting/domain"
	"github.com/mizan-erp/mizan/internal/modules/accounting/infra/sqlite"
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

// The module owns its schema (§10.3). Profile owns 0009, so accounting owns 0010.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// The chart templates this build ships. Adding one is adding a file.
//
//go:embed seeds/chart_of_accounts/*.json seeds/posting_rules/*.json
var seedFS embed.FS

// Stable error codes. They double as i18n keys and are part of the public contract.
const (
	CodeInvalidChart        = "accounting.invalid_chart"
	CodeShippedChartInvalid = "accounting.shipped_chart_invalid"
	CodeUnknownChart        = "accounting.unknown_chart"
	CodeChartExists         = "accounting.chart_exists"
	CodePublisherMissing    = "accounting.publisher_missing"
)

// The permissions this module protects.
//
// Read and manage, split: §20.6 exposes the chart read-only two releases before anyone may edit
// it, and a single permission would make that impossible to express.
const (
	PermAccountView   = "accounting.account.view"
	PermAccountManage = "accounting.account.manage"
)

// The audited actions (§15.3). Stable: renaming one orphans its history.
const (
	ActionChartApplied   = "accounting.chart.applied"
	ActionAccountCreated = "accounting.account.created"

	EntityChart   = "accounting.chart"
	EntityAccount = "accounting.account"
)

// Database is the narrow surface this module needs.
type Database interface {
	database.DB
	database.UnitOfWork
}

// Options configures the service.
type Options struct {
	// UserFS is the customer's overlay, usually os.DirFS(<dataDir>) — where an accountant
	// drops their own chart.
	UserFS fs.FS
	Clock  clock.Clock
	Bus    event.Publisher
	Logger *slog.Logger
}

// Service is the accounting module's application layer.
type Service struct {
	db       Database
	repos    *sqlite.Repos
	clk      clock.Clock
	bus      event.Publisher
	charts   []Chart
	ruleSets []RuleSet
	problems []seeds.Problem
}

// NewService loads the chart templates and builds the service.
//
// Returns an error only for a template this build ships (1.8 D4). An accountant's broken file
// is reported and skipped — a seed file must not stop a shop from opening.
func NewService(db Database, opts Options) (*Service, error) {
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}

	layers := []seeds.Layer{{FS: seedFS, Origin: seeds.OriginShipped}}
	if opts.UserFS != nil {
		layers = append(layers, seeds.Layer{FS: opts.UserFS, Origin: seeds.OriginUser})
	}
	charts, problems, err := loadCharts(layers)
	if err != nil {
		return nil, err
	}
	ruleSets, ruleProblems, err := loadRuleSets(layers)
	if err != nil {
		return nil, err
	}
	problems = append(problems, ruleProblems...)

	svc := &Service{
		db: db, repos: sqlite.New(db, opts.Clock), clk: opts.Clock, bus: opts.Bus,
		charts: charts, ruleSets: ruleSets, problems: problems,
	}
	if opts.Logger != nil {
		for _, p := range problems {
			opts.Logger.Warn("chart of accounts skipped",
				slog.String("path", p.Path), slog.String("origin", string(p.Origin)),
				slog.Any("error", p.Err))
		}
	}
	return svc, nil
}

// Charts lists the templates on offer, ordered by code.
func (s *Service) Charts() []Chart {
	out := make([]Chart, len(s.charts))
	copy(out, s.charts)
	return out
}

// Chart returns one template by code.
func (s *Service) Chart(code string) (Chart, bool) {
	for _, chart := range s.charts {
		if chart.Code == code {
			return chart, true
		}
	}
	return Chart{}, false
}

// Problems reports templates that were found but could not be used.
func (s *Service) Problems() []seeds.Problem {
	out := make([]seeds.Problem, len(s.problems))
	copy(out, s.problems)
	return out
}

// ApplyChart writes a template's accounts and mappings for a company, in one transaction.
//
// # Once, and only once
//
// Refused if the company already has accounts. A chart is the skeleton every future posting
// hangs on: applying a second one over the first would leave two overlapping hierarchies and a
// mapping layer pointing at whichever won, which is not a state anything could untangle.
// Extending a chart afterwards is what CreateAccount is for.
//
// # Atomicity
//
// One Unit of Work covering every account, every mapping, and the audit entry. A half-applied
// chart is worse than none: the missing accounts are the ones nobody notices until a posting
// needs them.
func (s *Service) ApplyChart(ctx context.Context, companyID id.ID, code string) error {
	chart, ok := s.Chart(code)
	if !ok {
		return errs.NotFound(CodeUnknownChart, "no such chart of accounts").WithParam("chart", code)
	}
	if companyID.IsZero() {
		return errs.Validation(CodeInvalidChart, "a chart of accounts belongs to a company")
	}

	return s.db.Do(ctx, func(ctx context.Context) error {
		existing, err := s.repos.CountAccounts(ctx, companyID)
		if err != nil {
			return err
		}
		if existing > 0 {
			return errs.Conflict(CodeChartExists,
				"this company already has a chart of accounts")
		}

		// Parents first, so each child can read the path its parent was given (§20.1). The
		// template's own line order is deliberately not trusted — see Chart.ordered.
		built := make(map[string]domain.Account, len(chart.Accounts))
		for _, line := range chart.ordered() {
			var parent *domain.Account
			if line.Parent != "" {
				found, ok := built[line.Parent]
				if !ok {
					return errs.Validation(domain.CodeParentNotFound,
						"an account's parent was not created first").
						WithParam("code", line.Code).WithParam("parent", line.Parent)
				}
				parent = &found
			}

			identifier, idErr := id.New()
			if idErr != nil {
				return idErr
			}
			account, accErr := domain.NewAccount(
				identifier, companyID, line.Code, line.Name,
				domain.AccountType(line.Type), parent)
			if accErr != nil {
				return accErr
			}
			account.NameKey = line.NameKey
			account.Subtype = line.Subtype
			account.IsSystem = line.System

			if parent != nil {
				// The parent stops accepting postings the moment it has a child. Persisted
				// immediately rather than collected and written at the end, so the invariant
				// holds at every point inside the transaction and not merely at its edge.
				adopted := parent.Adopt()
				if err = s.repos.SetPostable(ctx, adopted.ID, false); err != nil {
					return err
				}
				built[adopted.Code] = adopted
			}

			if err = s.repos.InsertAccount(ctx, account); err != nil {
				return err
			}
			built[account.Code] = account
		}

		for _, key := range sortedKeys(chart.Mappings) {
			account, ok := built[chart.Mappings[key]]
			if !ok {
				return errs.Validation(domain.CodeUnknownMapping,
					"a mapping names an account the chart did not create").
					WithParam("mapping", key)
			}
			mappingID, idErr := id.New()
			if idErr != nil {
				return idErr
			}
			if err = s.repos.InsertMapping(ctx, sqlite.Mapping{
				ID: mappingID, CompanyID: companyID, Key: key, AccountID: account.ID,
			}); err != nil {
				return err
			}
		}

		return s.audit(ctx, auditc.Auditable{
			Action:      ActionChartApplied,
			EntityType:  EntityChart,
			EntityID:    companyID,
			EntityLabel: chart.Name,
			After: chartSnapshot{
				Code: chart.Code, Accounts: len(chart.Accounts), Mappings: len(chart.Mappings),
			},
		})
	})
}

type chartSnapshot struct {
	Code     string `json:"code"`
	Accounts int    `json:"accounts"`
	Mappings int    `json:"mappings"`
}

// Accounts lists a company's chart, ordered by code.
func (s *Service) Accounts(ctx context.Context, companyID id.ID) ([]domain.Account, error) {
	return s.repos.Accounts(ctx, companyID)
}

// AccountByCode finds one account.
func (s *Service) AccountByCode(ctx context.Context, companyID id.ID, code string) (domain.Account, error) {
	return s.repos.AccountByCode(ctx, companyID, code)
}

// ResolveMapping answers "which account plays this role?" (§20.3).
//
// Branch first, then the company-wide row — the most specific match wins, which is the same
// precedence the settings scope chain uses (0.5). A second precedence model would be a second
// thing to learn for no gain.
func (s *Service) ResolveMapping(
	ctx context.Context, companyID, branchID id.ID, key string,
) (domain.Account, error) {
	return s.repos.ResolveMapping(ctx, companyID, branchID, key)
}

// audit publishes inside the caller's transaction (1.7, phase D7).
func (s *Service) audit(ctx context.Context, a auditc.Auditable) error {
	if s.bus == nil {
		return errs.Internal(CodePublisherMissing,
			"the accounting service was built without an event publisher")
	}
	return s.bus.Publish(ctx, a)
}

// sortedKeys orders a mapping table, so applying the same chart twice writes its rows in the
// same order — which makes a diff between two installations mean something.
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ── module ──────────────────────────────────────────────────────────────────────

// Module is the accounting module's registration surface.
type Module struct {
	svc *Service
}

var _ modules.Module = (*Module)(nil)

// NewModule builds the module around an already-constructed service.
func NewModule(svc *Service) *Module { return &Module{svc: svc} }

// Service exposes the module's application layer to the composition root.
func (m *Module) Service() *Service { return m.svc }

// Name identifies the module.
func (m *Module) Name() string { return "accounting" }

// DependsOn declares org and currency: accounts carry a foreign key to companies(id) and an
// optional one to currencies(code), so both schemas must exist first.
func (m *Module) DependsOn() []string { return []string{"org", "currency"} }

// Migrations returns the module's schema.
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return migrationFS
	}
	return sub
}

// Permissions gate reading and editing the chart.
func (m *Module) Permissions() []auth.PermissionDef {
	return []auth.PermissionDef{
		{Code: PermAccountView, Description: "permissions.accounting.account.view"},
		{Code: PermAccountManage, Description: "permissions.accounting.account.manage"},
	}
}

// Settings: none yet. The rounding stage and period-close policy arrive with the steps that
// read them (2.3, 2.7); declaring them now would declare knobs nothing consults.
func (m *Module) Settings() []config.Definition { return nil }

// FeatureFlags: none. §20.6 exposes accounting progressively through RELEASES, not through a
// flag a customer can toggle — the books are always kept.
func (m *Module) FeatureFlags() []config.FlagDef { return nil }

// Metadata: none. A chart of accounts is not `code`-keyed reference data — it is a hierarchy
// applied once per company, which ApplyChart owns.
func (m *Module) Metadata() []metadata.SeedSpec { return nil }

// Subscribe registers the one handler that turns domain events into journal entries.
//
// On the SYNCHRONOUS domain bus, for the reason 1.7 established for the audit trail: a document
// and its accounting entry must commit together or neither does. An entry produced after commit
// leaves a window in which a sale exists and its bookkeeping does not — and unlike a missing
// audit row, a missing journal entry makes the books wrong rather than merely incomplete.
//
// Accounting subscribing to other modules' events is the §20.3 design, not a violation of
// §23.1's "domain events are within a module": accounting is downstream infrastructure. It
// participates in no business decision and never calls back into a module. Sales publishes
// Postable; accounting is its only subscriber; sales does not know accounting exists.
func (m *Module) Subscribe(bus *eventbus.Bus, _ *outbox.Subscribers) error {
	if m.svc == nil {
		return nil
	}
	return eventbus.Subscribe(bus, "accounting.record", m.svc.onPostable)
}

// Jobs: the nightly ledger integrity check (§20.5, §20.2).
//
// # Why a job and not a startup check
//
// Both guarantees it verifies — that every entry balances, and that the maintained totals match
// a fresh recomputation — protect only the paths that go through the domain. A restore, a
// repair script, or a future importer meets neither. Those things happen between launches, not
// during one, so the check has to run on a schedule rather than at boot.
//
// It REPORTS rather than repairs. A rebuild that silently corrects would mean a bug in the
// incremental path is fixed nightly and never reported, and the books are wrong for exactly one
// day at a time, forever.
func (m *Module) Jobs() []jobs.Registration {
	return []jobs.Registration{{
		Def: jobs.Def{
			Key: "accounting.ledger_integrity",
			// Nightly, not hourly: it reads every journal line, and the failures it looks for
			// are introduced by restores and repairs rather than by ordinary trading.
			Schedule:    jobs.Every(24 * time.Hour),
			CatchUp:     jobs.Skip,
			Timeout:     10 * time.Minute,
			Description: "jobs.accounting.ledger_integrity",
		},
		Handler: func(ctx context.Context, _ jobs.RunContext) error {
			return m.svc.VerifyLedger(ctx)
		},
	}}
}
