// Package org owns the organisational spine: the company, its branches and warehouses, and
// the fiscal calendar.
//
// It is the first module every scope in the system resolves against — settings resolve
// `company` and `branch` (0.5), RBAC grants are scoped to a branch (§14.2), and every
// transactional table will carry `branch_id NOT NULL` (§26.1).
package org

import (
	"context"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/org/domain"
	"github.com/mizan-erp/mizan/internal/modules/org/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// Database is the narrow surface this module needs: reads and writes, plus the ability to
// open a Unit of Work.
//
// Declared here rather than taking *database.Store because provisioning is the module's one
// genuinely transactional operation, and naming the two capabilities it uses is more honest
// than depending on the whole concrete type. *database.Store satisfies it.
type Database interface {
	database.DB
	database.UnitOfWork
}

// Service is the org module's application layer.
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

// ── provisioning ────────────────────────────────────────────────────────────────

// CompanyInput describes the company to create.
type CompanyInput struct {
	Code               string
	Name               string
	LegalName          string
	TaxNumber          string
	CountryCode        string
	FunctionalCurrency string
	PricingCurrency    string // empty → same as functional (§18.1)
}

// LocationInput describes a branch or a warehouse.
type LocationInput struct {
	Code string
	Name string
}

// ProvisionInput is everything the setup wizard collects that org owns.
type ProvisionInput struct {
	Company   CompanyInput
	Branch    LocationInput
	Warehouse LocationInput
	// FiscalYearStartMonth names the first month of the accounting year; the year is the
	// calendar year that month falls in.
	FiscalYearStartMonth time.Month
	FiscalYearStartYear  int
}

// ProvisionResult reports what was created.
type ProvisionResult struct {
	CompanyID    id.ID
	BranchID     id.ID
	WarehouseID  id.ID
	FiscalYearID id.ID
}

// IsProvisioned reports whether setup has been completed.
//
// The frontend's SetupGate and the 1.9 `Setup.Status` binding both read this; it is the whole
// basis of "is this a fresh install?".
func (s *Service) IsProvisioned(ctx context.Context) (bool, error) {
	n, err := s.repos.CountCompanies(ctx)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Provision creates the company, its first branch and warehouse, and the first fiscal year.
//
// # Why the one-company check lives here
//
// §WIZ.1 also guards the Setup bindings, and that guard is about REACHABILITY — keeping the
// wizard off the menu once setup is done. This one is about TRUTH: a second company would make
// every scope in the system ambiguous, so the rule belongs where no caller can route around
// it. A future importer or repair tool calling the service directly gets the same refusal.
//
// # Atomicity
//
// Everything runs in ONE transaction, joining the caller's if there is one (0.3 join
// semantics). That is what lets the wizard commit the company and its first administrator
// together, and it means a failure part-way leaves no company at all rather than one with no
// branch — a state no screen could recover from.
func (s *Service) Provision(ctx context.Context, in ProvisionInput) (ProvisionResult, error) {
	var out ProvisionResult

	err := s.db.Do(ctx, func(ctx context.Context) error {
		provisioned, err := s.IsProvisioned(ctx)
		if err != nil {
			return err
		}
		if provisioned {
			return errs.Conflict(domain.CodeAlreadyProvisioned,
				"this installation has already been set up")
		}

		companyID, err := id.New()
		if err != nil {
			return err
		}
		company, err := domain.NewCompany(companyID, in.Company.Code, in.Company.Name,
			in.Company.CountryCode, in.Company.FunctionalCurrency)
		if err != nil {
			return err
		}
		company.LegalName = in.Company.LegalName
		company.TaxNumber = in.Company.TaxNumber
		company.PricingCurrency = in.Company.PricingCurrency
		if err = s.repos.InsertCompany(ctx, company); err != nil {
			return err
		}

		branchID, err := id.New()
		if err != nil {
			return err
		}
		// The first branch and warehouse are the defaults by construction — there is nothing
		// else for them to be, and leaving it to the caller would allow an organisation whose
		// default is unset.
		branch, err := domain.NewBranch(branchID, companyID, in.Branch.Code, in.Branch.Name, true)
		if err != nil {
			return err
		}
		if err = s.repos.InsertBranch(ctx, branch); err != nil {
			return err
		}

		warehouseID, err := id.New()
		if err != nil {
			return err
		}
		warehouse, err := domain.NewWarehouse(warehouseID, branchID,
			in.Warehouse.Code, in.Warehouse.Name, true)
		if err != nil {
			return err
		}
		if err = s.repos.InsertWarehouse(ctx, warehouse); err != nil {
			return err
		}

		fy, err := domain.GenerateFiscalYear(id.New, companyID,
			in.FiscalYearStartYear, in.FiscalYearStartMonth)
		if err != nil {
			return err
		}
		if err = s.repos.InsertFiscalYear(ctx, fy); err != nil {
			return err
		}

		out = ProvisionResult{
			CompanyID:    companyID,
			BranchID:     branchID,
			WarehouseID:  warehouseID,
			FiscalYearID: fy.ID,
		}
		return nil
	})
	if err != nil {
		return ProvisionResult{}, err
	}
	return out, nil
}

// ── reads ───────────────────────────────────────────────────────────────────────

// Company returns the single company, or a typed NotFound when setup has not run.
func (s *Service) Company(ctx context.Context) (domain.Company, error) {
	return s.repos.Company(ctx)
}

// CurrentCompanyID returns the acting company's identifier.
//
// Satisfies the one-method `Organisation` port that the identity module declares at its own
// point of use (Go convention). Identity therefore needs no import of org's package and org
// owes identity nothing — the dependency is a single method, named by the consumer.
func (s *Service) CurrentCompanyID(ctx context.Context) (id.ID, error) {
	company, err := s.repos.Company(ctx)
	if err != nil {
		return "", err
	}
	return company.ID, nil
}

// Branches lists the company's branches, the default first.
func (s *Service) Branches(ctx context.Context) ([]domain.Branch, error) {
	company, err := s.repos.Company(ctx)
	if err != nil {
		return nil, err
	}
	return s.repos.Branches(ctx, company.ID)
}

// DefaultBranch returns the branch a session acts in when it has no other opinion.
func (s *Service) DefaultBranch(ctx context.Context) (domain.Branch, error) {
	branches, err := s.Branches(ctx)
	if err != nil {
		return domain.Branch{}, err
	}
	for _, b := range branches {
		if b.IsDefault && b.IsActive {
			return b, nil
		}
	}
	return domain.Branch{}, errs.NotFound(domain.CodeBranchNotFound,
		"the company has no active default branch")
}

// DefaultBranchID returns the default branch's identifier.
//
// The second half of the one-method-per-need port identity declares: every session carries a
// branch (§26.1), and a session created without one would have to invent the concept later.
func (s *Service) DefaultBranchID(ctx context.Context) (id.ID, error) {
	branch, err := s.DefaultBranch(ctx)
	if err != nil {
		return "", err
	}
	return branch.ID, nil
}

// Warehouses lists a branch's warehouses, the default first.
func (s *Service) Warehouses(ctx context.Context, branchID id.ID) ([]domain.Warehouse, error) {
	return s.repos.Warehouses(ctx, branchID)
}

// FiscalYears lists the company's fiscal years with their periods.
func (s *Service) FiscalYears(ctx context.Context) ([]domain.FiscalYear, error) {
	company, err := s.repos.Company(ctx)
	if err != nil {
		return nil, err
	}
	return s.repos.FiscalYears(ctx, company.ID)
}

// ── deactivation ────────────────────────────────────────────────────────────────

// SetBranchActive switches a branch on or off, refusing to switch off the last active one.
func (s *Service) SetBranchActive(ctx context.Context, branchID id.ID, active bool) error {
	return s.db.Do(ctx, func(ctx context.Context) error {
		if !active {
			company, err := s.repos.Company(ctx)
			if err != nil {
				return err
			}
			n, err := s.repos.CountActiveBranches(ctx, company.ID)
			if err != nil {
				return err
			}
			if !domain.CanDeactivate(n) {
				return domain.ErrLastActiveBranch()
			}
		}
		return s.repos.SetBranchActive(ctx, branchID, active)
	})
}

// SetWarehouseActive switches a warehouse on or off, refusing to switch off a branch's last.
func (s *Service) SetWarehouseActive(ctx context.Context, warehouseID id.ID, branchID id.ID, active bool) error {
	return s.db.Do(ctx, func(ctx context.Context) error {
		if !active {
			n, err := s.repos.CountActiveWarehouses(ctx, branchID)
			if err != nil {
				return err
			}
			if !domain.CanDeactivate(n) {
				return domain.ErrLastActiveWarehouse()
			}
		}
		return s.repos.SetWarehouseActive(ctx, warehouseID, active)
	})
}
