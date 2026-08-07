package profile

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/platform/seeds"
)

// countryDir is where country profiles live, in both the embedded FS and the data directory.
const countryDir = "seeds/country_profiles"

// Country is one country's defaults (Addendum §C.1).
//
// # It is NOT stored (D1)
//
// There is no country_profiles table, deliberately, and it is the one place this module departs
// from how every other reference set in the system works. A country profile is read ONCE, at
// setup, to supply defaults; the wizard then copies every value into a setting or a company
// column, which from that moment is the only source of truth.
//
// A stored row would be a SECOND answer to "what is this company's date format?" — one that
// never changes, and that some future query would eventually read instead of the setting. §C.2
// is explicit that the wizard must not become a hidden source of truth; a stored profile is how
// that happens.
//
// # Nothing here is a legal or fiscal rule
//
// Every field is either an ISO/ITU registry value or a default the user is shown and can
// change. TaxProfile and ChartOfAccounts are present in the shape and null in every shipped
// file: those are jurisdictional facts to come from the customer's accountant (§C.3), and
// asserting them from memory is exactly the failure the architecture is built to avoid.
type Country struct {
	// Code is ISO 3166-1 alpha-2, upper case, and must match the filename.
	Code string `json:"country_code"`
	// NameKey is a translation key, not a name. A profile carrying an English string would
	// make English the source language of the country list (§22).
	NameKey string `json:"name_key"`

	DefaultLocale    string   `json:"default_locale"`
	SupportedLocales []string `json:"supported_locales"`

	FunctionalCurrency string `json:"functional_currency_default"`
	PricingCurrency    string `json:"pricing_currency_default"`

	// Null in every shipped file (§C.3). Present so a customer's accountant can supply one
	// without a schema change.
	ChartOfAccounts *string `json:"chart_of_accounts_template"`
	TaxProfile      *string `json:"tax_profile"`

	FiscalYearStartMonth int `json:"fiscal_year_start_month"`

	DateFormat string `json:"date_format"`
	TimeFormat string `json:"time_format"`
	// FirstDayOfWeek is 0=Sunday … 6=Saturday.
	FirstDayOfWeek int `json:"first_day_of_week"`

	NumberFormat NumberFormat `json:"number_format"`
	Calendar     Calendar     `json:"calendar"`

	AddressFormat    []string `json:"address_format"`
	PhoneCountryCode string   `json:"phone_country_code"`

	RoundingRule RoundingRule `json:"rounding_rule"`
}

// NumberFormat carries digit presentation.
//
// NumeralSystem exists because Arabic-Indic digits (٠١٢٣) versus Western digits vary by country
// AND by customer preference within a country (§C.1). It is a default here, a setting later.
type NumberFormat struct {
	DecimalSeparator  string `json:"decimal_separator"`
	ThousandSeparator string `json:"thousand_separator"`
	DigitGrouping     []int  `json:"digit_grouping"`
	NumeralSystem     string `json:"numeral_system"`
}

// Calendar names the primary and optional secondary calendars.
//
// Secondary is carried as DATA and nothing renders it (D7). Storage is Gregorian regardless
// (§G.4), so building a Hijri display later changes a formatter and no schema — the expensive
// half of that decision is already made correctly, and the cheap half stays open.
type Calendar struct {
	Primary   string `json:"primary"`
	Secondary string `json:"secondary"`
}

// RoundingRule is the country's default cash-rounding behaviour.
//
// IncrementMinor is in MINOR UNITS, as an integer — never a float. A rounding increment
// expressed as 0.05 is the first place a currency system loses money to binary fractions, and
// the kernel forbids float in exactly this vicinity (`no-float`).
type RoundingRule struct {
	Mode           string `json:"mode"`
	IncrementMinor int64  `json:"increment_minor"`
	Stage          string `json:"stage"`
}

// Closed sets. A value outside one is a typo, and a typo that survives into a customer's
// install is a setting they believe they configured and the system never saw.
var (
	numeralSystems = map[string]bool{"western": true, "arabic_indic": true}
	calendars      = map[string]bool{"gregorian": true, "hijri": true}
	roundingModes  = map[string]bool{"none": true, "nearest": true, "up": true, "down": true}
	roundingStages = map[string]bool{"line": true, "grand_total": true}
)

// validate checks a country profile, given the filename it came from.
//
// The filename check is the useful one: `sa.json` containing `"country_code": "SY"` would load
// two profiles for Syria and none for Saudi Arabia, and every symptom of that appears somewhere
// other than the file that caused it.
func (c Country) validate(name string) error {
	invalid := func(what string, value any) error {
		return errs.Validation(CodeInvalidProfile, "the country profile is not valid").
			WithParam("field", what).WithParam("value", param(value))
	}

	if len(c.Code) != 2 || c.Code != strings.ToUpper(c.Code) {
		return invalid("country_code", c.Code)
	}
	if name != "" && !strings.EqualFold(name, c.Code) {
		return errs.Validation(CodeInvalidProfile,
			"the country profile's code does not match its filename").
			WithParam("file", name).WithParam("value", c.Code)
	}
	if c.NameKey == "" {
		return invalid("name_key", c.NameKey)
	}
	if c.DefaultLocale == "" {
		return invalid("default_locale", c.DefaultLocale)
	}
	if len(c.SupportedLocales) == 0 {
		return invalid("supported_locales", c.SupportedLocales)
	}
	// A default outside the supported set produces a wizard whose language dropdown cannot
	// show its own default — the kind of contradiction that is obvious in a file and baffling
	// on screen.
	if !contains(c.SupportedLocales, c.DefaultLocale) {
		return errs.Validation(CodeInvalidProfile,
			"the default locale is not among the supported locales").
			WithParam("value", c.DefaultLocale)
	}
	if !isCurrencyCode(c.FunctionalCurrency) {
		return invalid("functional_currency_default", c.FunctionalCurrency)
	}
	// Pricing may be absent, meaning "same as functional" — a legitimate configuration for a
	// business that does not author prices in a second currency (§18.1).
	if c.PricingCurrency != "" && !isCurrencyCode(c.PricingCurrency) {
		return invalid("pricing_currency_default", c.PricingCurrency)
	}
	if c.FiscalYearStartMonth < 1 || c.FiscalYearStartMonth > 12 {
		return invalid("fiscal_year_start_month", c.FiscalYearStartMonth)
	}
	if c.FirstDayOfWeek < 0 || c.FirstDayOfWeek > 6 {
		return invalid("first_day_of_week", c.FirstDayOfWeek)
	}
	if c.DateFormat == "" || c.TimeFormat == "" {
		return invalid("date_format", c.DateFormat)
	}
	if !numeralSystems[c.NumberFormat.NumeralSystem] {
		return invalid("number_format.numeral_system", c.NumberFormat.NumeralSystem)
	}
	if !calendars[c.Calendar.Primary] {
		return invalid("calendar.primary", c.Calendar.Primary)
	}
	if c.Calendar.Secondary != "" && !calendars[c.Calendar.Secondary] {
		return invalid("calendar.secondary", c.Calendar.Secondary)
	}
	if len(c.AddressFormat) == 0 {
		return invalid("address_format", c.AddressFormat)
	}
	if !roundingModes[c.RoundingRule.Mode] {
		return invalid("rounding_rule.mode", c.RoundingRule.Mode)
	}
	if c.RoundingRule.Mode != "none" && !roundingStages[c.RoundingRule.Stage] {
		return invalid("rounding_rule.stage", c.RoundingRule.Stage)
	}
	if c.RoundingRule.IncrementMinor < 0 {
		return invalid("rounding_rule.increment_minor", c.RoundingRule.IncrementMinor)
	}
	return nil
}

// loadCountries discovers and decodes every country profile.
//
// Shipped failures are returned; user failures come back as problems and are skipped
// (platform/seeds, D4).
func loadCountries(layers []seeds.Layer) ([]Country, []seeds.Problem, error) {
	set, err := seeds.Discover(countryDir, layers...)
	if err != nil {
		return nil, nil, err
	}

	result, err := seeds.Decode(set, func(data []byte, c *Country) error {
		if decodeErr := seeds.StrictJSON(data, c); decodeErr != nil {
			return decodeErr
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	countries := make([]Country, 0, len(result.Docs))
	problems := result.Problems
	for _, doc := range result.Docs {
		// Validation happens HERE rather than inside the decode callback because a shipped file
		// that decodes but does not validate must still be fatal, and Decode's contract is
		// exactly that: any error on a shipped file stops the load.
		if validateErr := doc.Value.validate(doc.File.Name); validateErr != nil {
			if doc.File.Origin == seeds.OriginShipped {
				return nil, nil, errs.Wrap(validateErr, errs.CategoryInternal, CodeShippedInvalid,
					"a country profile shipped with this build is invalid").
					WithParam("file", doc.File.Path)
			}
			problems = append(problems, seeds.Problem{
				Path: doc.File.Path, Origin: doc.File.Origin, Err: validateErr,
			})
			continue
		}
		countries = append(countries, doc.Value)
	}

	sort.Slice(countries, func(i, j int) bool { return countries[i].Code < countries[j].Code })
	return countries, problems, nil
}

// param renders a value for an error parameter.
//
// Errors carry string parameters because they cross the i18n boundary (§22.2), where a message
// is a template and its arguments are text. Rendering here keeps every call site honest about
// that rather than each one reaching for fmt differently.
func param(v any) string { return fmt.Sprint(v) }

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// isCurrencyCode checks the SHAPE of an ISO 4217 code, not its existence.
//
// Existence is currency's business, and it is already enforced where it matters: companies
// carry a foreign key to currencies(code) (0004), so a profile naming a currency nobody seeded
// fails at provisioning with a clear constraint error rather than being second-guessed here.
func isCurrencyCode(code string) bool {
	if len(code) != 3 {
		return false
	}
	for _, r := range code {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}
