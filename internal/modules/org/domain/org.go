// Package domain holds the organisational entities and their invariants.
//
// Pure Go: stdlib and kernel only (enforced by domain-purity). No SQL, no driver, no clock
// call — construction takes the timestamp it needs, so the domain never asks what time it is.
package domain

import (
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Stable error codes. They double as i18n keys and are part of the public contract.
const (
	CodeInvalidCompany      = "org.invalid_company"
	CodeInvalidBranch       = "org.invalid_branch"
	CodeInvalidWarehouse    = "org.invalid_warehouse"
	CodeAlreadyProvisioned  = "org.already_provisioned"
	CodeNotProvisioned      = "org.not_provisioned"
	CodeLastActiveBranch    = "org.last_active_branch"
	CodeLastActiveWarehouse = "org.last_active_warehouse"
	CodeCompanyNotFound     = "org.company_not_found"
	CodeBranchNotFound      = "org.branch_not_found"
	CodeInvalidFiscalYear   = "org.invalid_fiscal_year"
)

// Company is the organisation the installation belongs to.
//
// Fields are exported because this is a record, not a behaviour-bearing aggregate: every
// invariant it has is checked at construction, and there is no state transition an outside
// caller could corrupt by assignment. Sales invoices will look very different.
type Company struct {
	ID                 id.ID
	Code               string
	Name               string
	LegalName          string
	TaxNumber          string
	CountryCode        string
	FunctionalCurrency string
	// PricingCurrency empty means "same as functional" — §18.1's collapse to identity, which
	// is what lets a single-currency business never encounter the concept.
	PricingCurrency string
	LogoRef         string
	IsActive        bool
}

// NewCompany builds a validated company.
func NewCompany(identifier id.ID, code, name, countryCode, functional string) (Company, error) {
	c := Company{
		ID:                 identifier,
		Code:               strings.TrimSpace(code),
		Name:               strings.TrimSpace(name),
		CountryCode:        strings.ToUpper(strings.TrimSpace(countryCode)),
		FunctionalCurrency: strings.ToUpper(strings.TrimSpace(functional)),
		IsActive:           true,
	}
	return c, c.validate()
}

func (c Company) validate() error {
	switch {
	case c.ID.IsZero():
		return errs.Validation(CodeInvalidCompany, "a company needs an identifier")
	case c.Code == "":
		return errs.Validation(CodeInvalidCompany, "a company needs a code").
			WithField("code", CodeInvalidCompany, "required")
	case c.Name == "":
		return errs.Validation(CodeInvalidCompany, "a company needs a name").
			WithField("name", CodeInvalidCompany, "required")
	case len(c.CountryCode) != 2:
		// Two letters is a WIDTH check, not a claim of ISO-3166 membership — the same
		// reasoning that keeps currency codes data rather than an enum (0.9 §3.1).
		return errs.Validation(CodeInvalidCompany, "country code must be two letters").
			WithField("countryCode", CodeInvalidCompany, "invalid")
	case c.FunctionalCurrency == "":
		return errs.Validation(CodeInvalidCompany, "a company needs a functional currency").
			WithField("functionalCurrency", CodeInvalidCompany, "required")
	}
	return nil
}

// EffectivePricingCurrency resolves the §18.1 default rather than making every caller do it.
func (c Company) EffectivePricingCurrency() string {
	if c.PricingCurrency == "" {
		return c.FunctionalCurrency
	}
	return c.PricingCurrency
}

// Branch is a trading location. Every transactional table will carry its id (§26.1).
type Branch struct {
	ID        id.ID
	CompanyID id.ID
	Code      string
	Name      string
	IsDefault bool
	Address   string // JSON, shaped by the country profile
	IsActive  bool
}

// NewBranch builds a validated branch.
func NewBranch(identifier, companyID id.ID, code, name string, isDefault bool) (Branch, error) {
	b := Branch{
		ID:        identifier,
		CompanyID: companyID,
		Code:      strings.TrimSpace(code),
		Name:      strings.TrimSpace(name),
		IsDefault: isDefault,
		IsActive:  true,
	}
	switch {
	case b.ID.IsZero() || b.CompanyID.IsZero():
		return Branch{}, errs.Validation(CodeInvalidBranch, "a branch needs identifiers")
	case b.Code == "":
		return Branch{}, errs.Validation(CodeInvalidBranch, "a branch needs a code").
			WithField("code", CodeInvalidBranch, "required")
	case b.Name == "":
		return Branch{}, errs.Validation(CodeInvalidBranch, "a branch needs a name").
			WithField("name", CodeInvalidBranch, "required")
	}
	return b, nil
}

// Warehouse is a stock-bearing location within a branch.
type Warehouse struct {
	ID                  id.ID
	BranchID            id.ID
	Code                string
	Name                string
	IsDefault           bool
	AllowsNegativeStock bool
	IsActive            bool
}

// NewWarehouse builds a validated warehouse.
func NewWarehouse(identifier, branchID id.ID, code, name string, isDefault bool) (Warehouse, error) {
	w := Warehouse{
		ID:        identifier,
		BranchID:  branchID,
		Code:      strings.TrimSpace(code),
		Name:      strings.TrimSpace(name),
		IsDefault: isDefault,
		IsActive:  true,
	}
	switch {
	case w.ID.IsZero() || w.BranchID.IsZero():
		return Warehouse{}, errs.Validation(CodeInvalidWarehouse, "a warehouse needs identifiers")
	case w.Code == "":
		return Warehouse{}, errs.Validation(CodeInvalidWarehouse, "a warehouse needs a code").
			WithField("code", CodeInvalidWarehouse, "required")
	case w.Name == "":
		return Warehouse{}, errs.Validation(CodeInvalidWarehouse, "a warehouse needs a name").
			WithField("name", CodeInvalidWarehouse, "required")
	}
	return w, nil
}

// CanDeactivate reports whether a location may be switched off, given how many siblings are
// still active.
//
// A business with nowhere to trade is a state no screen can recover from, so the last active
// branch of a company — and the last active warehouse of a branch — cannot be deactivated.
// Expressed as a domain function rather than a repository check so the rule is testable
// without a database and cannot be bypassed by a second call site.
func CanDeactivate(activeSiblings int) bool { return activeSiblings > 1 }

// ErrLastActiveBranch and ErrLastActiveWarehouse are the typed refusals.
func ErrLastActiveBranch() error {
	return errs.Conflict(CodeLastActiveBranch,
		"the last active branch cannot be deactivated")
}

func ErrLastActiveWarehouse() error {
	return errs.Conflict(CodeLastActiveWarehouse,
		"the last active warehouse of a branch cannot be deactivated")
}
