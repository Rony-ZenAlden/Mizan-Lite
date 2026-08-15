// Package tax owns the tax engine (§19).
//
// # It ships with no rates, and that is the design
//
// §19.1: "no tax rate, threshold, or rule is ever written in Go code. The code contains a
// resolution algorithm; the rates and rules are data." §C.3 goes further — jurisdictional facts
// are not ours to assert from memory.
//
// So v1 ships the complete engine and not one rate. The eight-level resolution order, the
// versioned-rate lookup, the inclusive decomposition, the compound bases, and the disabled
// state are all built and tested. What is absent is the customer's data.
//
// §19.5 makes the empty state first-class: with `tax.enabled` off, every document resolves to
// zero tax with the reason recorded, and no tax posting is made — while the tables and code
// paths remain, so enabling it later needs no migration.
package tax

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"io/fs"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/round"
	"github.com/mizan-erp/mizan/internal/modules/tax/domain"
	"github.com/mizan-erp/mizan/internal/modules/tax/infra/sqlite"
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

// The module owns its schema (§10.3). Accounting owns 0010–0013, so tax owns 0014.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// Stable error codes.
const (
	CodeUnknownGroup = "tax.unknown_group"
)

// The permissions this module protects.
const (
	PermTaxView   = "tax.view"
	PermTaxManage = "tax.manage"
)

// Settings and flags (§19.5).
var (
	// Enabled is the switch §19.5 designs. OFF by default, deliberately: a fresh install has no
	// rates — because none are ours to invent — and a tax engine turned on with nothing
	// configured would charge zero while implying it had calculated something.
	//
	// A SETTING rather than a feature flag: a feature flag is about progressive delivery of
	// something the product does (§17), and whether a business charges tax is a fact about the
	// business, not a rollout stage.
	Enabled = config.DeclareBool(config.Def{
		Key:         "tax.enabled",
		Default:     false,
		Scopes:      []config.Scope{config.ScopeSystem, config.ScopeCompany, config.ScopeBranch},
		Description: "settings.tax.enabled",
	})

	// RoundingStage decides whether tax rounds per line or once per document.
	//
	// Configurable because the two produce LEGALLY DIFFERENT totals in different jurisdictions
	// (§19.4) — this is exactly the sort of fact a generic ERP must not hardcode.
	RoundingStage = config.DeclareEnum(config.Def{
		Key:         "tax.rounding_stage",
		Default:     StageLine,
		Enum:        []string{StageLine, StageDocument},
		Scopes:      []config.Scope{config.ScopeSystem, config.ScopeCompany},
		Description: "settings.tax.rounding_stage",
	})
)

// The rounding stages.
const (
	StageLine     = "line"
	StageDocument = "document"
)

// Database is the narrow surface this module needs.
type Database interface {
	database.DB
	database.UnitOfWork
}

// Service is the tax module's application layer.
type Service struct {
	db    Database
	repos *sqlite.Repos
	clk   clock.Clock
}

// NewService builds the service.
func NewService(db Database, clk clock.Clock) *Service {
	if clk == nil {
		clk = clock.System()
	}
	return &Service{db: db, repos: sqlite.New(db, clk), clk: clk}
}

// Request is what a caller needs taxed.
//
// Every candidate level of §19.3 is a field the caller fills in with what it knows. A document
// that knows nothing but its company still resolves correctly — to the company default, or to
// nothing.
type Request struct {
	CompanyID id.ID
	BranchID  id.ID
	PartnerID id.ID
	Date      time.Time

	// Amount is the line's price: the NET for an exclusive group, the GROSS for an inclusive
	// one. Which it is comes from the group, not from the caller — a caller that had to know
	// would have to know the tax configuration to quote a price.
	Amount money.Money

	LineGroupID     id.ID
	DocumentGroupID id.ID
	PartnerGroupID  id.ID
	ProductGroupID  id.ID
	CategoryGroupID id.ID
	BranchGroupID   id.ID
}

// Quote is a taxed amount with its reasoning.
type Quote struct {
	domain.Result

	// Reason and ExemptionCode are stored on the document at posting (§19.3): "why was this
	// customer charged 15%?" is asked years later by somebody who was not in the room.
	Reason        domain.Reason
	GroupID       id.ID
	GroupCode     string
	ExemptionCode string
}

// Calculate resolves which taxes apply and works out the amounts (§19.3, §19.4).
//
// # The disabled path is the shipped path
//
// With `tax.enabled` off — the default, and what v1 ships — this returns the amount untouched
// with `Reason: tax_disabled`. No group lookup, no rate lookup, no arithmetic. A business in a
// tax-free jurisdiction gets a genuinely simpler system rather than one pretending to compute
// zero.
func (s *Service) Calculate(ctx context.Context, in Request) (Quote, error) {
	enabled := Enabled.Get(ctx)

	exemptionCode := ""
	exempt := false
	if enabled {
		var err error
		exemptionCode, exempt, err = s.repos.IsExempt(ctx, in.CompanyID, in.PartnerID, in.Date)
		if err != nil {
			return Quote{}, err
		}
	}

	companyDefault, _, err := s.repos.DefaultGroup(ctx, in.CompanyID)
	if err != nil {
		return Quote{}, err
	}

	resolution := domain.Resolve(enabled, domain.Candidates{
		LineOverride:     in.LineGroupID,
		DocumentOverride: in.DocumentGroupID,
		PartnerExempt:    exempt,
		PartnerGroup:     in.PartnerGroupID,
		ProductGroup:     in.ProductGroupID,
		CategoryGroup:    in.CategoryGroupID,
		BranchDefault:    in.BranchGroupID,
		CompanyDefault:   companyDefault,
	})

	quote := Quote{Reason: resolution.Reason, ExemptionCode: exemptionCode}

	if resolution.GroupID.IsZero() {
		// Disabled, exempt, or nothing configured. All three mean no tax — and all three record
		// WHICH, because "zero tax" with no reason is the answer nobody can audit.
		zero := money.Zero(in.Amount.Currency())
		quote.Result = domain.Result{Net: in.Amount, Tax: zero, Gross: in.Amount}
		return quote, nil
	}

	group, err := s.repos.GroupByID(ctx, resolution.GroupID)
	if errors.Is(err, sql.ErrNoRows) {
		return Quote{}, errs.NotFound(CodeUnknownGroup, "no such tax group").
			WithParam("group", string(resolution.GroupID))
	}
	if err != nil {
		return Quote{}, err
	}

	rates, err := s.repos.RatesOn(ctx, group.ID, in.Date)
	if err != nil {
		return Quote{}, err
	}

	result, err := domain.Calculate(in.Amount, group, rates, s.rounding(ctx, in.Amount))
	if err != nil {
		return Quote{}, err
	}

	quote.Result = result
	quote.GroupID = group.ID
	quote.GroupCode = group.Code
	return quote, nil
}

// rounding picks the mode for a computation.
//
// The CURRENCY's own mode, not a tax setting: how a currency rounds is a property of the
// currency (0.2), and a tax that rounded differently from every other amount in the same
// document would produce totals that do not reconcile.
func (s *Service) rounding(_ context.Context, amount money.Money) round.RoundingMode {
	return amount.Currency().Rounding()
}

// ── administration ──────────────────────────────────────────────────────────────

// ChangeRate supersedes a tax's rate from a date (§19.2).
//
// # Why this is not an update
//
// The old version's window is CLOSED and a new one opens. The old rate itself is never touched,
// so every document already issued keeps resolving what it was charged at. An update would
// silently restate history — which is the failure the whole phase is shaped around.
func (s *Service) ChangeRate(
	ctx context.Context, taxID id.ID, newRateMicro int64, from time.Time,
) error {
	return s.db.Do(ctx, func(ctx context.Context) error {
		// The day before the new rate takes effect is the last day of the old one. Closing the
		// window is the only thing that touches an existing row, and it changes WHEN a rate
		// applied — never WHAT it was.
		if err := s.repos.CloseOpenVersions(ctx, taxID, from.AddDate(0, 0, -1)); err != nil {
			return err
		}

		identifier, err := id.New()
		if err != nil {
			return err
		}
		return s.repos.InsertVersion(ctx, identifier, domain.Version{
			TaxID: taxID, RateMicro: newRateMicro, From: from,
		})
	})
}

// ── module ──────────────────────────────────────────────────────────────────────

// Module is the tax module's registration surface.
type Module struct{ svc *Service }

var _ modules.Module = (*Module)(nil)

// NewModule builds the module around an already-constructed service.
func NewModule(svc *Service) *Module { return &Module{svc: svc} }

// Service exposes the module's application layer to the composition root.
func (m *Module) Service() *Service { return m.svc }

// Name identifies the module.
func (m *Module) Name() string { return "tax" }

// DependsOn declares org and accounting: taxes carry a foreign key to companies(id), and a tax
// may name the accounts it posts to.
func (m *Module) DependsOn() []string { return []string{"org", "accounting"} }

// Migrations returns the module's schema.
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return migrationFS
	}
	return sub
}

// Permissions gate reading and configuring tax.
func (m *Module) Permissions() []auth.PermissionDef {
	return []auth.PermissionDef{
		{Code: PermTaxView, Description: "permissions.tax.view"},
		{Code: PermTaxManage, Description: "permissions.tax.manage"},
	}
}

// Settings are the enable switch and the rounding stage (§19.4, §19.5).
func (m *Module) Settings() []config.Definition {
	return []config.Definition{
		mustDefinition(Enabled.Key()),
		mustDefinition(RoundingStage.Key()),
	}
}

func mustDefinition(key string) config.Definition {
	def, _ := config.Default().Lookup(key)
	return def
}

// FeatureFlags: none. Whether a business charges tax is a fact about the business, not a
// rollout stage — so it is a setting (see Enabled).
func (m *Module) FeatureFlags() []config.FlagDef { return nil }

// Metadata: NONE, and this is the §C.3 decision made visible.
//
// A seeded tax rate would be an assertion about a jurisdiction, and jurisdictional facts change
// and are not ours to make from memory. The engine ships complete; the rates arrive from the
// customer or their accountant.
func (m *Module) Metadata() []metadata.SeedSpec { return nil }

// Subscribe registers nothing. Tax is consulted by documents; it reacts to nothing.
func (m *Module) Subscribe(_ *eventbus.Bus, _ *outbox.Subscribers) error { return nil }

// Jobs: none.
func (m *Module) Jobs() []jobs.Registration { return nil }

// Series is empty: this module numbers no documents of its own.
func (m *Module) Series() []numbering.SeriesSpec { return nil }
