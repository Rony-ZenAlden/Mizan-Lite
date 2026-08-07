package bindings

import (
	"context"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/api/policy"
	"github.com/mizan-erp/mizan/internal/api/setup"
	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// SetupStatusDTO answers "does this installation still need setting up?".
type SetupStatusDTO struct {
	Required bool `json:"required"`
	// Options is populated ONLY while setup is required.
	//
	// A public method should stop answering as soon as its purpose is over: once a company
	// exists, the country list and the profile catalogue are no longer anyone's business at an
	// unauthenticated endpoint, and the Settings screens that show them are permissioned.
	Options *SetupOptionsDTO `json:"options,omitempty"`
}

// SetupOptionsDTO is what the wizard renders its choices from.
type SetupOptionsDTO struct {
	Countries        []SetupCountryDTO  `json:"countries"`
	BusinessProfiles []SetupBusinessDTO `json:"businessProfiles"`
	Currencies       []SetupCurrencyDTO `json:"currencies"`
	Locales          []string           `json:"locales"`
}

// SetupCountryDTO is one country's defaults.
//
// NameKey rather than a name: the country list is translated like everything else (§22), and
// shipping an English string would make English the source language of the first screen a
// customer ever sees.
type SetupCountryDTO struct {
	Code               string   `json:"code"`
	NameKey            string   `json:"nameKey"`
	DefaultLocale      string   `json:"defaultLocale"`
	SupportedLocales   []string `json:"supportedLocales"`
	FunctionalCurrency string   `json:"functionalCurrency"`
	PricingCurrency    string   `json:"pricingCurrency"`
	FiscalYearStart    int      `json:"fiscalYearStart"`
	DateFormat         string   `json:"dateFormat"`
	FirstDayOfWeek     int      `json:"firstDayOfWeek"`
	PhoneCode          string   `json:"phoneCode"`
}

// SetupBusinessDTO is one selectable trade.
type SetupBusinessDTO struct {
	Code        string `json:"code"`
	NameKey     string `json:"nameKey"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// SetupCurrencyDTO is one selectable currency.
type SetupCurrencyDTO struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Symbol string `json:"symbol"`
}

// SetupInputDTO is everything the wizard collects (§C.2).
//
// The password crosses here and nowhere else. It is not stored, not echoed back, and never
// enters an audit payload — the completion entry records the username and nothing more.
type SetupInputDTO struct {
	Locale      string `json:"locale"`
	CountryCode string `json:"countryCode"`

	CompanyCode string `json:"companyCode"`
	CompanyName string `json:"companyName"`
	LegalName   string `json:"legalName"`
	TaxNumber   string `json:"taxNumber"`

	FunctionalCurrency string `json:"functionalCurrency"`
	PricingCurrency    string `json:"pricingCurrency"`

	BusinessProfile string `json:"businessProfile"`

	FiscalYearStartMonth int `json:"fiscalYearStartMonth"`
	FiscalYearStartYear  int `json:"fiscalYearStartYear"`

	BranchCode    string `json:"branchCode"`
	BranchName    string `json:"branchName"`
	WarehouseCode string `json:"warehouseCode"`
	WarehouseName string `json:"warehouseName"`

	AdminUsername    string `json:"adminUsername"`
	AdminDisplayName string `json:"adminDisplayName"`
	AdminPassword    string `json:"adminPassword"`
}

// SetupResultDTO reports what was created.
//
// It carries no token: the wizard finishes SIGNED OUT and the frontend logs in with the
// credentials just typed (1.9 D6). One extra screen, and it proves the account works before the
// wizard closes — a configured system nobody can enter is the worst outcome on a fresh install.
type SetupResultDTO struct {
	CompanyID   string `json:"companyId"`
	BranchID    string `json:"branchId"`
	WarehouseID string `json:"warehouseId"`
	AdminUserID string `json:"adminUserId"`
}

// Setup is the first-run wizard.
type Setup struct{ graph }

// setupPolicies declares what Setup's methods require.
//
// Both are Public, which is §WIZ.1's answer to the bootstrap paradox: no user exists, so
// nothing can be permissioned, and a magic bootstrap principal would then exist for the life of
// the system with no password, no audit trail, and no way to revoke.
//
// Public is not the whole story for Apply. See setupGuard.
func setupPolicies() map[string]policy.Policy {
	return map[string]policy.Policy{
		"Status": policy.Public(),
		"Apply":  policy.Public(),
	}
}

// setupGuard is guard PLUS "setup is still required".
//
// # Why this is an accessor rather than a check (1.9 D2)
//
// Apply is a public, unauthenticated method that creates an administrator. If the "no company
// yet" rule were one `if` at the top of each method, a future Setup.Reset that forgot it would
// be unauthenticated account creation — and nothing would say so.
//
// So the rule is enforced the way Step 1.5 (D1) enforced permissions: the check IS the way to
// the object graph. A mutating Setup method that skips it has no database, no services, and no
// context. Forgetting produces a method that does nothing, not one that runs unprotected.
func (s *Setup) setupGuard(method string) (context.Context, *bootstrap.App, error) {
	ctx, app, err := s.guard(method)
	if err != nil {
		return nil, nil, err
	}
	required, err := app.Setup.Required(ctx)
	if err != nil {
		return nil, nil, err
	}
	if !required {
		// Forever. There is no flag to reset and no window to re-open: the check is a row
		// count, so "has this been set up?" has exactly one answer and it cannot drift.
		return nil, nil, errs.Conflict(setup.CodeAlreadyDone,
			"this installation has already been set up")
	}
	return ctx, app, nil
}

// Status reports whether setup is required, with the wizard's options when it is.
//
// Deliberately callable FOREVER, unlike Apply: the shell asks this on every launch to decide
// whether to route to the wizard, including on an installation that was set up two years ago.
// A gate that closed here would make the answer unobtainable and the shell unable to boot.
func (s *Setup) Status() envelope.Result[SetupStatusDTO] {
	ctx, app, err := s.guard("Status")
	if err != nil {
		return envelope.Fail[SetupStatusDTO](err)
	}

	required, err := app.Setup.Required(ctx)
	if err != nil {
		return envelope.Fail[SetupStatusDTO](err)
	}
	if !required {
		return envelope.Ok(SetupStatusDTO{Required: false})
	}

	options, err := app.Setup.Options(ctx)
	if err != nil {
		return envelope.Fail[SetupStatusDTO](err)
	}
	dto := toOptionsDTO(options)
	return envelope.Ok(SetupStatusDTO{Required: true, Options: &dto})
}

// Apply performs first-run setup, in one transaction.
func (s *Setup) Apply(in SetupInputDTO) envelope.Result[SetupResultDTO] {
	ctx, app, err := s.setupGuard("Apply")
	if err != nil {
		return envelope.Fail[SetupResultDTO](err)
	}

	result, err := app.Setup.Apply(ctx, setup.Input{
		Locale:      in.Locale,
		CountryCode: in.CountryCode,

		CompanyCode: in.CompanyCode,
		CompanyName: in.CompanyName,
		LegalName:   in.LegalName,
		TaxNumber:   in.TaxNumber,

		FunctionalCurrency: in.FunctionalCurrency,
		PricingCurrency:    in.PricingCurrency,

		BusinessProfile: in.BusinessProfile,

		FiscalYearStartMonth: in.FiscalYearStartMonth,
		FiscalYearStartYear:  in.FiscalYearStartYear,

		BranchCode:    in.BranchCode,
		BranchName:    in.BranchName,
		WarehouseCode: in.WarehouseCode,
		WarehouseName: in.WarehouseName,

		AdminUsername:    in.AdminUsername,
		AdminDisplayName: in.AdminDisplayName,
		AdminPassword:    in.AdminPassword,
	})
	if err != nil {
		return envelope.Fail[SetupResultDTO](err)
	}

	return envelope.Ok(SetupResultDTO{
		CompanyID:   string(result.CompanyID),
		BranchID:    string(result.BranchID),
		WarehouseID: string(result.WarehouseID),
		AdminUserID: string(result.AdminUserID),
	})
}

func toOptionsDTO(o setup.Options) SetupOptionsDTO {
	// Slices are allocated even when empty so the JSON carries [] rather than null: a frontend
	// mapping over null is a crash, and "no countries shipped" is a legitimate state.
	dto := SetupOptionsDTO{
		Countries:        make([]SetupCountryDTO, 0, len(o.Countries)),
		BusinessProfiles: make([]SetupBusinessDTO, 0, len(o.BusinessProfiles)),
		Currencies:       make([]SetupCurrencyDTO, 0, len(o.Currencies)),
		Locales:          o.Locales,
	}
	for _, c := range o.Countries {
		dto.Countries = append(dto.Countries, SetupCountryDTO{
			Code: c.Code, NameKey: c.NameKey,
			DefaultLocale: c.DefaultLocale, SupportedLocales: c.SupportedLocales,
			FunctionalCurrency: c.FunctionalCurrency, PricingCurrency: c.PricingCurrency,
			FiscalYearStart: c.FiscalYearStart, DateFormat: c.DateFormat,
			FirstDayOfWeek: c.FirstDayOfWeek, PhoneCode: c.PhoneCode,
		})
	}
	for _, b := range o.BusinessProfiles {
		dto.BusinessProfiles = append(dto.BusinessProfiles, SetupBusinessDTO{
			Code: b.Code, NameKey: b.NameKey, Name: b.Name, Description: b.Description,
		})
	}
	for _, c := range o.Currencies {
		dto.Currencies = append(dto.Currencies, SetupCurrencyDTO{
			Code: c.Code, Name: c.Name, Symbol: c.Symbol,
		})
	}
	if dto.Locales == nil {
		dto.Locales = []string{}
	}
	return dto
}
