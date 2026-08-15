// Package expenses owns money out that buys no stock: rent, wages, fuel, utilities — most of what
// a small business actually spends.
//
// # Why this is not purchasing
//
// A purchase bill line MUST name a goods receipt line, because that is what makes the three-way
// match structural rather than validated. An electricity bill has no delivery and nothing to match
// against, so reusing the table would mean making that column nullable — and the moment it is
// nullable the match becomes a rule somebody remembered to write.
//
// Phase 6's strongest guarantee would be traded for one table's reuse. The structural difference
// is one column: a purchase line points at a VARIANT, an expense line points at a CATEGORY.
package expenses

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"log/slog"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/event"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/expenses/domain"
	"github.com/mizan-erp/mizan/internal/modules/expenses/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/platform/auth"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/jobs"
	"github.com/mizan-erp/mizan/internal/platform/metadata"
	"github.com/mizan-erp/mizan/internal/platform/modules"
	"github.com/mizan-erp/mizan/internal/platform/outbox"
)

// The module owns its schema (§10.3). Purchasing owns 0027–0033.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

//go:embed seeds/expense_categories/*.json
var seedFS embed.FS

// The permissions this module protects.
const (
	PermExpenseView    = "expenses.expense.view"
	PermExpenseDraft   = "expenses.expense.draft"
	PermExpensePost    = "expenses.expense.post"
	PermCategoryManage = "expenses.category.manage"
)

// The audited actions (§15.3). AUDIT actions only — posting-rule keys are declared separately,
// which is the Phase 6 DoD review's finding applied from the start rather than discovered.
const (
	ActionExpenseDrafted   = "expenses.expense.drafted"
	ActionExpenseLineAdded = "expenses.expense.line_added"
	ActionExpenseRecorded  = "expenses.expense.recorded"
	ActionExpenseCancelled = "expenses.expense.cancelled"
	ActionCategoriesSeeded = "expenses.categories.seeded"

	EntityExpense  = "expenses.expense"
	EntityCategory = "expenses.category"
)

// SeriesExpense numbers expenses.
const SeriesExpense = "EXPENSE"

// Stable codes for this module's failures.
const (
	CodePublisherMissing = "expenses.publisher_missing"
	CodePortMissing      = "expenses.port_missing"
	CodeUnknownExpense   = "expenses.unknown_expense"
	CodeUnknownCategory  = "expenses.unknown_category"
	CodeUnknownDebt      = "expenses.unknown_debt"
	CodeUnknownAccount   = "expenses.unknown_account"
	CodeSeedInvalid      = "expenses.seed_invalid"
)

// Database is the narrow surface this module needs.
type Database interface {
	database.DB
	database.UnitOfWork
}

// ActorResolver reports who is acting.
type ActorResolver interface {
	Actor(ctx context.Context) (Actor, bool)
}

// Actor is who recorded an expense.
type Actor struct {
	UserID   id.ID
	BranchID id.ID
}

// Options configures the service.
type Options struct {
	Clock   clock.Clock
	Bus     event.Publisher
	Actors  ActorResolver
	Tax     Tax
	Numbers Numbering
	Logger  *slog.Logger
}

// Service is the expenses module's application layer.
type Service struct {
	db      Database
	repos   *sqlite.Repos
	clk     clock.Clock
	bus     event.Publisher
	actors  ActorResolver
	tax     Tax
	numbers Numbering
	logger  *slog.Logger
}

// NewService builds the service.
func NewService(db Database, opts Options) *Service {
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}
	return &Service{
		db: db, repos: sqlite.New(db, opts.Clock), clk: opts.Clock,
		bus: opts.Bus, actors: opts.Actors, tax: opts.Tax,
		numbers: opts.Numbers, logger: opts.Logger,
	}
}

func (s *Service) audit(ctx context.Context, a auditc.Auditable) error {
	if s.bus == nil {
		return errs.Internal(CodePublisherMissing,
			"the expenses service was built without an event publisher")
	}
	return s.bus.Publish(ctx, a)
}

func (s *Service) actorOf(ctx context.Context) id.ID {
	if s.actors == nil {
		return ""
	}
	actor, ok := s.actors.Actor(ctx)
	if !ok {
		return ""
	}
	return actor.UserID
}

// ── categories ──────────────────────────────────────────────────────────────────

// seedFile is a shipped set of expense categories.
type seedFile struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	Categories []struct {
		Code           string `json:"code"`
		Name           string `json:"name"`
		NameKey        string `json:"name_key"`
		Account        string `json:"account"`
		TaxRecoverable bool   `json:"tax_recoverable"`
	} `json:"categories"`
}

// ApplyCategories seeds a company's expense categories from a shipped set.
//
// # Why these are DATA and not an enum
//
// What a business must report separately is a tax question that differs by jurisdiction, and it
// changes when the rules do. A shop that must show "entertainment" apart from "staff welfare"
// because its revenue authority says so should not need a release.
//
// Idempotent by CODE: applying twice adds nothing, so a re-run after a partial failure is safe.
func (s *Service) ApplyCategories(ctx context.Context, companyID id.ID, set string) error {
	raw, err := seedFS.ReadFile("seeds/expense_categories/" + set + ".json")
	if err != nil {
		return errs.NotFound(CodeSeedInvalid,
			"there is no shipped expense category set with that code").WithParam("set", set)
	}
	var file seedFile
	if err = json.Unmarshal(raw, &file); err != nil {
		// A SHIPPED file that does not parse is a build fault, not a user's problem.
		return errs.Internal(CodeSeedInvalid,
			"a shipped expense category set is not valid JSON").WithParam("set", set)
	}

	return s.db.Do(ctx, func(txCtx context.Context) error {
		created := 0
		for _, entry := range file.Categories {
			if _, found, findErr := s.repos.CategoryByCode(
				txCtx, companyID, entry.Code); findErr != nil {
				return findErr
			} else if found {
				continue
			}

			accountID, found, accErr := s.repos.AccountByCode(txCtx, companyID, entry.Account)
			if accErr != nil {
				return accErr
			}
			if !found {
				// The chart must already carry the account. A category pointing at nothing
				// would let an expense be entered and then fail at posting, after somebody has
				// typed it — so this fails HERE, at setup, where it is one message.
				return errs.Validation(CodeUnknownAccount,
					"this expense category needs an account the chart does not have").
					WithParam("category", entry.Code).WithParam("account", entry.Account)
			}

			identifier, idErr := id.New()
			if idErr != nil {
				return idErr
			}
			category, buildErr := domain.NewCategory(
				identifier, companyID, accountID, entry.Code, entry.Name)
			if buildErr != nil {
				return buildErr
			}
			category.NameKey = entry.NameKey
			category.TaxRecoverable = entry.TaxRecoverable
			category.SortOrder = created

			if err = s.repos.InsertCategory(txCtx, category); err != nil {
				return err
			}
			created++
		}
		if created == 0 {
			return nil
		}
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionCategoriesSeeded, EntityType: EntityCategory, EntityID: companyID,
			EntityLabel: file.Name,
			After:       map[string]any{"set": set, "created": created},
		})
	})
}

// Categories lists a company's expense categories.
func (s *Service) Categories(
	ctx context.Context, companyID id.ID,
) ([]domain.Category, error) {
	return s.repos.Categories(ctx, companyID)
}

// ── module ──────────────────────────────────────────────────────────────────────

// Module is the expenses module's registration surface.
type Module struct{ svc *Service }

var _ modules.Module = (*Module)(nil)

// NewModule builds the module around an already-constructed service.
func NewModule(svc *Service) *Module { return &Module{svc: svc} }

// Service exposes the module's application layer to the composition root.
func (m *Module) Service() *Service { return m.svc }

// Name identifies the module.
func (m *Module) Name() string { return "expenses" }

// DependsOn names the modules whose tables this one references.
func (m *Module) DependsOn() []string {
	return []string{"org", "accounting", "partner"}
}

// Migrations returns the module's schema.
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return migrationFS
	}
	return sub
}

// Permissions gate seeing, drafting, posting, and configuring.
//
// Drafting and POSTING are separate, as in sales and purchasing: in many businesses somebody
// enters expenses and somebody else approves them, and posting is the irreversible act.
func (m *Module) Permissions() []auth.PermissionDef {
	return []auth.PermissionDef{
		{Code: PermExpenseView, Description: "permissions.expenses.expense.view"},
		{Code: PermExpenseDraft, Description: "permissions.expenses.expense.draft"},
		{Code: PermExpensePost, Description: "permissions.expenses.expense.post"},
		{Code: PermCategoryManage, Description: "permissions.expenses.category.manage"},
		{Code: PermSettlePost, Description: "permissions.expenses.settlement.post"},
		{Code: PermDebtView, Description: "permissions.expenses.debt.view"},
		{Code: PermDebtRecord, Description: "permissions.expenses.debt.record"},
	}
}

// Settings: none yet.
func (m *Module) Settings() []config.Definition { return nil }

// FeatureFlags: none.
func (m *Module) FeatureFlags() []config.FlagDef { return nil }

// Metadata: none. Categories arrive from seed FILES through ApplyCategories, because a category
// carries an account reference that the flat metadata seeder cannot express.
func (m *Module) Metadata() []metadata.SeedSpec { return nil }

// Subscribe registers nothing. Expenses publishes; it does not react.
func (m *Module) Subscribe(_ *eventbus.Bus, _ *outbox.Subscribers) error { return nil }

// Jobs: none yet. Recurring templates pre-fill a form somebody confirms rather than posting
// themselves (D5), so there is nothing to schedule.
func (m *Module) Jobs() []jobs.Registration { return nil }
