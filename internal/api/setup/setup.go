// Package setup is the first-run wizard's application service (§WIZ).
//
// # Why it is here and not in internal/modules
//
// Setting up an installation composes FOUR modules in one transaction — org, identity, profile,
// and currency. A package under internal/modules may reach another module only through its
// `contract` (module-isolation, 1.1), so a `modules/setup` is structurally impossible.
//
// That constraint is right rather than inconvenient: setup is not a domain. It owns no
// entities, enforces no invariant of its own, and exists for one moment in an installation's
// life. It is a composition of four domains, and the API layer is the layer already permitted
// to see several at once.
//
// It is deliberately NOT in the composition root either. `bootstrap` starts the system; it
// should not also own a business use case.
package setup

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/event"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/locale"
	"github.com/mizan-erp/mizan/internal/modules/accounting"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/catalog"
	"github.com/mizan-erp/mizan/internal/modules/currency"
	currencydomain "github.com/mizan-erp/mizan/internal/modules/currency/domain"
	"github.com/mizan-erp/mizan/internal/modules/identity"
	"github.com/mizan-erp/mizan/internal/modules/org"
	"github.com/mizan-erp/mizan/internal/modules/profile"
	"github.com/mizan-erp/mizan/internal/modules/sales"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/i18n"
)

// Stable error codes. They double as i18n keys and are part of the public contract.
const (
	CodeAlreadyDone       = "setup.already_complete"
	CodeUnknownCountry    = "setup.unknown_country"
	CodeUnknownProfile    = "setup.unknown_business_profile"
	CodeUnknownCurrency   = "setup.unknown_currency"
	CodeMissingField      = "setup.missing_field"
	CodeNotConfigured     = "setup.not_configured"
	CodePublisherMissing  = "setup.publisher_missing"
	CodeInvalidFiscalYear = "setup.invalid_fiscal_year"
)

// defaultChart is the chart of accounts a fresh installation gets.
//
// Named here rather than in the wizard's input because there is exactly one shipped template
// and no user-facing choice yet. When a second ships, this becomes an Input field and the
// constant goes — which is a smaller change than un-picking a question asked too early.
// The rule set shares the chart's code deliberately: the rules name account ROLES, and the
// chart is what says which account plays each one. A pairing that had two names would be two
// things to keep in step.
const defaultChart = "generic_trading"

// defaultUnits is the shipped set of measures.
const defaultUnits = "standard"

// ActionCompleted is the audited action that ties one setup run together.
const ActionCompleted = "setup.completed"

// EntitySetup is what the completion entry is filed under.
const EntitySetup = "setup.installation"

// Database is the narrow surface this service needs.
type Database interface {
	database.DB
	database.UnitOfWork
}

// Service performs first-run setup.
//
// Dependencies are the individual services rather than *bootstrap.App: taking the App would
// make every future field of the object graph an invisible dependency of setup, and "what does
// the wizard actually touch?" would stop being answerable from the constructor.
type Service struct {
	db         Database
	org        *org.Service
	identity   *identity.Service
	profile    *profile.Service
	currency   *currency.Service
	accounting *accounting.Service
	catalog    *catalog.Service
	settings   *config.Settings
	// messages is the i18n catalogue. Renamed from `catalog` when the catalog MODULE arrived
	// in 3.1 — two things called "catalog" in one struct is a collision waiting for whoever
	// reads it next, and "messages" says what it holds.
	messages *i18n.Catalog
	bus      event.Publisher
}

// NewService builds the wizard's service.
func NewService(
	db Database,
	orgSvc *org.Service, identitySvc *identity.Service,
	profileSvc *profile.Service, currencySvc *currency.Service,
	accountingSvc *accounting.Service, catalogSvc *catalog.Service,
	settings *config.Settings, messages *i18n.Catalog, bus event.Publisher,
) *Service {
	return &Service{
		db: db, org: orgSvc, identity: identitySvc, profile: profileSvc,
		currency: currencySvc, accounting: accountingSvc, catalog: catalogSvc,
		settings: settings, messages: messages, bus: bus,
	}
}

// Required reports whether first-run setup still has to happen.
//
// One row count, which is the whole of §WIZ.1's answer to the bootstrap paradox. The
// alternative — a magic bootstrap principal — would exist for the life of the system as an
// identity with no password, no audit trail, and no way to revoke.
func (s *Service) Required(ctx context.Context) (bool, error) {
	provisioned, err := s.org.IsProvisioned(ctx)
	if err != nil {
		return false, err
	}
	return !provisioned, nil
}

// ── what the wizard offers ──────────────────────────────────────────────────────

// Options is everything the wizard needs to render its choices.
//
// Returned in one call rather than four, because the wizard shows them across consecutive
// steps and a fresh install has no reason to make four round trips for data that cannot
// change between them.
type Options struct {
	Countries        []CountryOption
	BusinessProfiles []BusinessOption
	Currencies       []CurrencyOption
	Locales          []string
}

// CountryOption is one country's defaults, flattened for the wizard.
//
// Not the whole profile: the wizard needs what it displays and what it pre-fills. Sending the
// entire document would make the frontend a second interpreter of a shape only the backend
// should understand.
type CountryOption struct {
	Code               string
	NameKey            string
	DefaultLocale      string
	SupportedLocales   []string
	FunctionalCurrency string
	PricingCurrency    string
	FiscalYearStart    int
	DateFormat         string
	FirstDayOfWeek     int
	PhoneCode          string
}

// BusinessOption is one selectable trade.
type BusinessOption struct {
	Code        string
	NameKey     string
	Name        string
	Description string
}

// CurrencyOption is one selectable currency.
type CurrencyOption struct {
	Code   string
	Name   string
	Symbol string
}

// locales lists the languages this build ships.
//
// Discovered from the embedded catalogues, not hardcoded — adding locales/ku/ ships a new
// language with no code change (0.8 §3.2), and the wizard's first screen is a language picker,
// so a hardcoded list here would be the one place that never learned about it.
func (s *Service) locales() []string {
	if s.messages == nil {
		return nil
	}
	out := make([]string, 0, 2)
	for _, l := range s.messages.Locales() {
		out = append(out, l.String())
	}
	return out
}

// Options lists the countries, trades, currencies, and languages on offer.
func (s *Service) Options(ctx context.Context) (Options, error) {
	out := Options{Locales: s.locales()}

	for _, c := range s.profile.Countries() {
		pricing := c.PricingCurrency
		if pricing == "" {
			// Empty means "same as functional" (§18.1) — a legitimate configuration for a
			// business that does not author prices in a second currency. Resolved here so the
			// wizard shows a currency rather than a blank.
			pricing = c.FunctionalCurrency
		}
		out.Countries = append(out.Countries, CountryOption{
			Code: c.Code, NameKey: c.NameKey,
			DefaultLocale: c.DefaultLocale, SupportedLocales: c.SupportedLocales,
			FunctionalCurrency: c.FunctionalCurrency, PricingCurrency: pricing,
			FiscalYearStart: c.FiscalYearStartMonth, DateFormat: c.DateFormat,
			FirstDayOfWeek: c.FirstDayOfWeek, PhoneCode: c.PhoneCountryCode,
		})
	}

	for _, b := range s.profile.BusinessProfiles() {
		out.BusinessProfiles = append(out.BusinessProfiles, BusinessOption{
			Code: b.Code, NameKey: b.NameKey, Name: b.Name, Description: b.Description,
		})
	}

	infos, err := s.currency.List(ctx, false)
	if err != nil {
		return Options{}, err
	}
	for _, info := range infos {
		out.Currencies = append(out.Currencies, CurrencyOption{
			Code: info.Code, Name: info.Name, Symbol: info.Symbol,
		})
	}
	return out, nil
}

// ── applying ────────────────────────────────────────────────────────────────────

// Input is everything the wizard collects (§C.2).
type Input struct {
	Locale      string
	CountryCode string

	CompanyCode string
	CompanyName string
	LegalName   string
	TaxNumber   string

	FunctionalCurrency string
	PricingCurrency    string

	BusinessProfile string

	FiscalYearStartMonth int
	FiscalYearStartYear  int

	BranchCode    string
	BranchName    string
	WarehouseCode string
	WarehouseName string

	AdminUsername    string
	AdminDisplayName string
	AdminPassword    string

	// ReceiptHeader is what prints at the top of a receipt. Blank means the company's name.
	//
	// Optional, and it must stay optional: a shop whose trading name IS its registered name has
	// nothing to say here, and making them retype it would be a step that exists to be skipped.
	ReceiptHeader string
}

// Result reports what was created.
type Result struct {
	CompanyID   id.ID
	BranchID    id.ID
	WarehouseID id.ID
	AdminUserID id.ID
}

// Apply performs the whole of first-run setup, in ONE transaction.
//
// # Atomicity (D3)
//
// Every inner call opens its own Unit of Work, which JOINS this one (0.3 join semantics), so
// there is one transaction and one commit. A failure anywhere leaves NO COMPANY AT ALL.
//
// That asymmetry is the point. An unconfigured install is just the wizard again — annoying, and
// completely recoverable. A company with no administrator, or with no branch, is a state no
// screen in the system can repair and no support engineer can talk a shopkeeper through.
//
// # The actor is nobody (D5)
//
// There is no authenticated user, and the administrator does not exist until part-way through.
// Every audit entry this produces records `system` with no actor. Stamping the just-created
// administrator on the later half would produce a trail implying a sequence of decisions that
// never happened — an unattributed entry is honest, a fabricated one is not (1.7).
func (s *Service) Apply(ctx context.Context, in Input) (Result, error) {
	if s.settings == nil || s.org == nil || s.identity == nil || s.profile == nil {
		return Result{}, errs.Internal(CodeNotConfigured, "the setup service is not wired")
	}
	if s.bus == nil {
		return Result{}, errs.Internal(CodePublisherMissing,
			"the setup service was built without an event publisher")
	}

	// Validated BEFORE the transaction opens. Apply is a public, unauthenticated method, so its
	// input is untrusted; and a rejection that has already written three rows is worse than one
	// that has written none, even though the transaction would undo them.
	in, err := s.normalise(ctx, in)
	if err != nil {
		return Result{}, err
	}

	var out Result
	err = s.db.Do(ctx, func(ctx context.Context) error {
		// There is deliberately NO "is it already provisioned?" check here.
		//
		// It was written, and a mutation drill proved it harmful: removing the binding's
		// setupGuard changed nothing, because this check caught the second call instead. A
		// redundant guard that hides the failure of the real one is worse than no guard —
		// it makes the protection untestable, which is how a protection quietly stops working.
		//
		// Two enforcement points remain, answering different questions:
		//   - the binding's setupGuard: REACHABILITY, keeping the wizard off a configured
		//     install, and it is now the thing the "unreachable after setup" test pins;
		//   - org.Provision's own check: TRUTH, refusing a second company however it is
		//     reached, including by a future importer calling the service directly.
		provision, provisionErr := s.org.Provision(ctx, org.ProvisionInput{
			Company: org.CompanyInput{
				Code: in.CompanyCode, Name: in.CompanyName,
				LegalName: in.LegalName, TaxNumber: in.TaxNumber,
				CountryCode:        in.CountryCode,
				FunctionalCurrency: in.FunctionalCurrency,
				PricingCurrency:    in.PricingCurrency,
			},
			Branch:               org.LocationInput{Code: in.BranchCode, Name: in.BranchName},
			Warehouse:            org.LocationInput{Code: in.WarehouseCode, Name: in.WarehouseName},
			FiscalYearStartMonth: time.Month(in.FiscalYearStartMonth),
			FiscalYearStartYear:  in.FiscalYearStartYear,
		})
		if provisionErr != nil {
			return provisionErr
		}

		if roleErr := s.identity.SeedRoles(ctx, provision.CompanyID); roleErr != nil {
			return roleErr
		}

		admin, userErr := s.identity.CreateUser(ctx, identity.CreateUserInput{
			CompanyID:   provision.CompanyID,
			Username:    in.AdminUsername,
			DisplayName: in.AdminDisplayName,
			Password:    in.AdminPassword,
			// The setup administrator is protected from DELETION, not from deactivation: an
			// install must always have someone who can get in, but a person who leaves the
			// company must be switchable off.
			IsSystem: true,
			// False, deliberately. They chose this password thirty seconds ago; demanding they
			// change it immediately is the kind of ceremony that teaches people to pick
			// throwaway passwords. NO DEFAULT PASSWORD SHIPS (§13.1) — there is nothing here
			// for a rotation to protect against.
			MustChange: false,
		})
		if userErr != nil {
			return userErr
		}
		if assignErr := s.identity.AssignRoleByCode(
			ctx, admin.ID, provision.CompanyID, identity.RoleAdministrator); assignErr != nil {
			return assignErr
		}

		// The chart of accounts, inside the same transaction as everything else.
		//
		// A company without one cannot post anything, and §20's whole premise is that the books
		// keep themselves from the first release — so a setup that produced a company with no
		// ledger would produce one that silently records nothing.
		//
		// No choice is offered yet: one template ships, and the wizard would be asking a
		// question with a single answer. The step that offers a chart picker is the one that
		// ships a second chart.
		if chartErr := s.accounting.ApplyChart(
			ctx, provision.CompanyID, defaultChart); chartErr != nil {
			return chartErr
		}
		// And the posting rules that use it. A chart with no rules is a ledger nothing can
		// post to, which would make the books silently empty rather than visibly broken.
		if rulesErr := s.accounting.ApplyRules(
			ctx, provision.CompanyID, defaultChart); rulesErr != nil {
			return rulesErr
		}
		// Units of measure. Not company-scoped — a kilogram is a kilogram — but applied here so
		// a fresh install has the measures a product needs before the first product exists.
		if unitErr := s.catalog.ApplyUnits(ctx, defaultUnits); unitErr != nil {
			return unitErr
		}

		// The business profile FIRST, the wizard's explicit choices after (§3.2). The bundle
		// suggests defaults for a trade; the person chose their language on screen. Whoever
		// writes last wins, and it must be the person.
		if in.BusinessProfile != "" {
			if profileErr := s.profile.Apply(ctx, provision.CompanyID, in.BusinessProfile); profileErr != nil {
				return profileErr
			}
		}

		if setErr := s.applySettings(ctx, provision.CompanyID, in); setErr != nil {
			return setErr
		}

		out = Result{
			CompanyID:   provision.CompanyID,
			BranchID:    provision.BranchID,
			WarehouseID: provision.WarehouseID,
			AdminUserID: admin.ID,
		}

		// One entry that ties the whole run together. The individual operations audit
		// themselves (1.7); this is the row an auditor looks for, sharing a correlation id
		// with all of them.
		return s.bus.Publish(ctx, auditc.Auditable{
			Action:      ActionCompleted,
			EntityType:  EntitySetup,
			EntityID:    provision.CompanyID,
			EntityLabel: in.CompanyName,
			After: completedSnapshot{
				CountryCode: in.CountryCode, CompanyCode: in.CompanyCode,
				Locale: in.Locale, FunctionalCurrency: in.FunctionalCurrency,
				PricingCurrency: in.PricingCurrency, BusinessProfile: in.BusinessProfile,
				AdminUsername: in.AdminUsername,
			},
		})
	})
	if err != nil {
		return Result{}, err
	}

	// Settings were written inside the transaction, and the in-memory cache was loaded before
	// it. Without this, the very next call would resolve the OLD values — the same reload the
	// 0.5 design flagged for a Set nested in a business transaction.
	if reloadErr := s.settings.Reload(ctx); reloadErr != nil {
		return Result{}, reloadErr
	}
	return out, nil
}

type completedSnapshot struct {
	CountryCode        string `json:"countryCode"`
	CompanyCode        string `json:"companyCode"`
	Locale             string `json:"locale"`
	FunctionalCurrency string `json:"functionalCurrency"`
	PricingCurrency    string `json:"pricingCurrency"`
	BusinessProfile    string `json:"businessProfile,omitempty"`
	AdminUsername      string `json:"adminUsername"`
}

// applySettings writes the wizard's explicit choices at company scope.
//
// Company scope, not system: these are facts about THIS company, and writing them at system
// scope would make them the default for a second company that §ORG will never allow anyway —
// but more importantly it would put them at a tier the Settings screen does not edit.
//
// Everything written here is rewritable from a Settings screen. That is §C.2's constraint on
// the wizard, and it is what stops it becoming a hidden source of truth.
func (s *Service) applySettings(ctx context.Context, companyID id.ID, in Input) error {
	values := []struct {
		key   string
		value any
	}{
		{i18n.LocaleSettingKey, in.Locale},
		{currency.Functional.Key(), in.FunctionalCurrency},
		{currency.Pricing.Key(), in.PricingCurrency},
		// Written even when blank, which is deliberate: blank is the value that MEANS "use the
		// company name", and writing it makes the row exist for the Settings screen to edit
		// rather than leaving a setting that only appears once somebody has already set it.
		{sales.ReceiptHeader.Key(), strings.TrimSpace(in.ReceiptHeader)},
	}
	for _, v := range values {
		if err := s.settings.Set(ctx, config.ScopeCompany, companyID, v.key, v.value); err != nil {
			return err
		}
	}
	return nil
}

// ── validation ──────────────────────────────────────────────────────────────────

// normalise checks and fills the input.
//
// It returns a corrected copy rather than mutating: defaults applied here (the country's
// currency, the country's fiscal-year start) end up in the audit payload, so the entry records
// what was actually used and not what was typed.
//
// The password is NOT validated here. identity owns the policy (§13.1), and a second copy of
// the rule would eventually disagree with the first.
func (s *Service) normalise(ctx context.Context, in Input) (Input, error) {
	required := func(field, value string) error {
		if value == "" {
			return errs.Validation(CodeMissingField, "a required value is missing").
				WithParam("field", field)
		}
		return nil
	}
	for _, f := range []struct{ name, value string }{
		{"companyCode", in.CompanyCode}, {"companyName", in.CompanyName},
		{"branchCode", in.BranchCode}, {"branchName", in.BranchName},
		{"warehouseCode", in.WarehouseCode}, {"warehouseName", in.WarehouseName},
		{"adminUsername", in.AdminUsername}, {"adminPassword", in.AdminPassword},
	} {
		if err := required(f.name, f.value); err != nil {
			return Input{}, err
		}
	}
	if in.AdminDisplayName == "" {
		in.AdminDisplayName = in.AdminUsername
	}

	country, ok := s.profile.Country(in.CountryCode)
	if !ok {
		return Input{}, errs.Validation(CodeUnknownCountry, "no profile ships for that country").
			WithParam("country", in.CountryCode)
	}

	if in.Locale == "" {
		in.Locale = country.DefaultLocale
	}
	if _, valid := locale.Parse(in.Locale); !valid {
		return Input{}, errs.Validation(CodeMissingField, "that is not a valid language").
			WithParam("field", "locale").WithParam("value", in.Locale)
	}

	if in.FunctionalCurrency == "" {
		in.FunctionalCurrency = country.FunctionalCurrency
	}
	if in.PricingCurrency == "" {
		// Empty means "same as functional" (§18.1), resolved from the country's suggestion.
		in.PricingCurrency = country.PricingCurrency
		if in.PricingCurrency == "" {
			in.PricingCurrency = in.FunctionalCurrency
		}
	}
	for _, code := range []string{in.FunctionalCurrency, in.PricingCurrency} {
		if err := s.requireCurrency(ctx, code); err != nil {
			return Input{}, err
		}
	}

	if in.BusinessProfile != "" {
		if _, found := s.profile.Business(in.BusinessProfile); !found {
			return Input{}, errs.Validation(CodeUnknownProfile, "no such business profile").
				WithParam("profile", in.BusinessProfile)
		}
	}

	if in.FiscalYearStartMonth == 0 {
		in.FiscalYearStartMonth = country.FiscalYearStartMonth
	}
	if in.FiscalYearStartMonth < 1 || in.FiscalYearStartMonth > 12 {
		return Input{}, errs.Validation(CodeInvalidFiscalYear,
			"the fiscal year must start in a real month").
			WithParam("month", strconv.Itoa(in.FiscalYearStartMonth))
	}
	if in.FiscalYearStartYear == 0 {
		return Input{}, errs.Validation(CodeInvalidFiscalYear,
			"the first fiscal year needs a year")
	}
	return in, nil
}

// requireCurrency refuses a currency the system does not have.
//
// Checked here rather than left to the foreign key, because the constraint error would arrive
// mid-transaction on a fresh install and read as a database failure rather than as "that
// currency is not available". This is exactly the gap Step 1.8 left and §5 closes.
func (s *Service) requireCurrency(ctx context.Context, code string) error {
	if _, err := s.currency.Info(ctx, code); err != nil {
		if errs.IsCategory(err, errs.CategoryNotFound) ||
			errs.CodeOf(err) == currencydomain.CodeUnknownCurrency {
			return errs.Validation(CodeUnknownCurrency,
				"that currency is not available on this installation").WithParam("currency", code)
		}
		return err
	}
	return nil
}
