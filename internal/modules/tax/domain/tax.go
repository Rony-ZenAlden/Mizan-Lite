// Package domain holds the tax module's business rules (§19).
//
// Two things live here and nowhere else: the RESOLUTION order that decides which taxes apply,
// and the ARITHMETIC that turns a rate into an amount. Both are pure — no database, no clock,
// no context — because both are the kind of thing that must be testable against a table of
// cases rather than against a fixture.
//
// No rate, threshold, or jurisdiction rule appears in this package (§19.1). The code is the
// algorithm; the rates are data.
package domain

import (
	"math/big"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/round"
)

// Stable error codes. They double as i18n keys and are part of the public contract.
const (
	CodeInvalidTax   = "tax.invalid"
	CodeNoRate       = "tax.no_rate_in_force"
	CodeInvalidGroup = "tax.invalid_group"
	CodeRateOverlap  = "tax.rate_overlap"
	CodeCalculation  = "tax.calculation_failed"
)

// Kind is what a tax is, for reporting and for the accounts it posts to.
type Kind string

const (
	VAT         Kind = "vat"
	Sales       Kind = "sales"
	Excise      Kind = "excise"
	Withholding Kind = "withholding"
)

// Calculation is how a tax turns a base into an amount.
type Calculation string

const (
	Percentage       Calculation = "percentage"
	FixedPerUnit     Calculation = "fixed_per_unit"
	FixedPerDocument Calculation = "fixed_per_document"
)

// Tax is one levy.
type Tax struct {
	ID          id.ID
	Code        string
	Name        string
	Kind        Kind
	Calculation Calculation

	IsCompound    bool
	IsRecoverable bool
	IsActive      bool
}

// Version is a rate in force over a date range (§19.2).
//
// Rates are VERSIONED, never edited. When VAT moves 15% → 16% a version is added; historical
// invoices keep resolving the old rate. Editing in place would silently falsify every document
// already issued, which is the failure this whole phase is shaped around.
type Version struct {
	TaxID      id.ID
	RateMicro  int64
	FixedMinor int64
	From       time.Time
	To         time.Time // zero means still in force
}

// AppliesOn reports whether the version is the one in force on a business date.
func (v Version) AppliesOn(date time.Time) bool {
	day := truncate(date)
	if day.Before(truncate(v.From)) {
		return false
	}
	if v.To.IsZero() {
		return true
	}
	return !day.After(truncate(v.To))
}

// Rate is the version's rate as a kernel Percent.
func (v Version) Rate() money.Percent { return money.PercentFromMicro(v.RateMicro) }

func truncate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// RateOn finds the version in force on a date.
//
// Returns a typed error rather than zero when none is: a tax with no rate for the date is a
// configuration gap, and silently charging nothing would hide it until an authority noticed.
func RateOn(versions []Version, date time.Time) (Version, error) {
	for _, version := range versions {
		if version.AppliesOn(date) {
			return version, nil
		}
	}
	return Version{}, errs.Validation(CodeNoRate,
		"no rate is in force for that date").WithParam("date", date.Format("2006-01-02"))
}

// GroupItem is one tax within a group, in application order.
type GroupItem struct {
	Tax        Tax
	Sequence   int
	CompoundOn bool
}

// Group is a set of taxes applied together (§19.2).
type Group struct {
	ID             id.ID
	Code           string
	Name           string
	PriceInclusive bool
	Items          []GroupItem
}

// ── resolution (§19.3) ──────────────────────────────────────────────────────────

// Reason records WHY a tax group was chosen — or why none was.
//
// Stored on the document at posting time (§19.3), because "why was this customer charged 15%?"
// is a question asked years later by somebody who was not in the room. A resolution that
// records only its answer makes that unanswerable.
type Reason string

const (
	ReasonLineOverride     Reason = "line_override"
	ReasonDocumentOverride Reason = "document_override"
	ReasonPartnerExempt    Reason = "partner_exempt"
	ReasonPartnerGroup     Reason = "partner_group"
	ReasonProductGroup     Reason = "product_group"
	ReasonCategoryGroup    Reason = "category_group"
	ReasonBranchDefault    Reason = "branch_default"
	ReasonCompanyDefault   Reason = "company_default"
	ReasonTaxDisabled      Reason = "tax_disabled"
	ReasonNoGroup          Reason = "no_group"
)

// Candidates are the group identifiers each resolution level offers, highest priority first in
// the order §19.3 lists them.
//
// A struct of optional values rather than a list of functions: the caller supplies what it
// knows, and the algorithm is then a single readable sequence rather than a chain of callbacks
// whose order is implied by registration.
type Candidates struct {
	LineOverride     id.ID
	DocumentOverride id.ID
	PartnerExempt    bool
	PartnerGroup     id.ID
	ProductGroup     id.ID
	CategoryGroup    id.ID
	BranchDefault    id.ID
	CompanyDefault   id.ID
}

// Resolution is the outcome: which group applies, and why.
type Resolution struct {
	GroupID id.ID
	Reason  Reason
}

// Resolve applies §19.3's priority order, highest first.
//
// # Why this is one function
//
// §19.3 calls for "a single, testable function". The order is a business rule with legal
// consequences — an export customer's exemption must beat a product's default, not the other
// way round — and a rule spread across the call sites that each know one level is a rule nobody
// can read or test as a whole.
//
// `enabled` short-circuits everything (§19.5): a business in a tax-free jurisdiction gets zero
// tax and a recorded reason, with no tables emptied and no migration needed to turn it back on.
func Resolve(enabled bool, c Candidates) Resolution {
	if !enabled {
		return Resolution{Reason: ReasonTaxDisabled}
	}

	// 1–2: an explicit override. Somebody decided this deliberately, and nothing below outranks
	// a decision made about this specific document.
	if !c.LineOverride.IsZero() {
		return Resolution{GroupID: c.LineOverride, Reason: ReasonLineOverride}
	}
	if !c.DocumentOverride.IsZero() {
		return Resolution{GroupID: c.DocumentOverride, Reason: ReasonDocumentOverride}
	}

	// 3: an exemption. It beats every default below because it is evidence about this customer,
	// not a convention — and it resolves to NO group with the reason recorded, so the document
	// shows zero tax and says why.
	if c.PartnerExempt {
		return Resolution{Reason: ReasonPartnerExempt}
	}

	// 4–8: defaults, narrowing outwards from the customer to the company.
	for _, candidate := range []struct {
		id     id.ID
		reason Reason
	}{
		{c.PartnerGroup, ReasonPartnerGroup},
		{c.ProductGroup, ReasonProductGroup},
		{c.CategoryGroup, ReasonCategoryGroup},
		{c.BranchDefault, ReasonBranchDefault},
		{c.CompanyDefault, ReasonCompanyDefault},
	} {
		if !candidate.id.IsZero() {
			return Resolution{GroupID: candidate.id, Reason: candidate.reason}
		}
	}

	return Resolution{Reason: ReasonNoGroup}
}

// ── calculation (§19.4) ─────────────────────────────────────────────────────────

// Component is one tax's contribution to a document.
type Component struct {
	TaxID     id.ID
	TaxCode   string
	RateMicro int64
	// Base is what the rate was applied to — recorded because a compound tax's base is not the
	// document's net, and "show me your working" is the whole point of storing this.
	Base   money.Money
	Amount money.Money
}

// Result is a line's tax, decomposed.
type Result struct {
	// Net is the amount excluding tax, Gross includes it. For an inclusive group the caller's
	// price IS the gross and the net is derived; for an exclusive group the reverse.
	Net        money.Money
	Tax        money.Money
	Gross      money.Money
	Components []Component
}

// Calculate works out a line's tax (§19.4).
//
// # Inclusive prices are decomposed, not approximated
//
// A retail price of 115 with 15% VAT is 100 net and 15 tax — worked out by dividing, with the
// rounding applied once. Multiplying 115 by 15% would give 17.25, which is the tax on the wrong
// base and is how a shop's returns stop matching its till.
//
// # Compound taxes
//
// A tax marked compound applies to a base that already includes the taxes before it in the
// group's sequence — a municipal tax on a VAT-inclusive amount. The running base is carried
// forward, which is why the group's order is data rather than an implementation detail.
func Calculate(
	amount money.Money, group Group, rates map[id.ID]Version, mode round.RoundingMode,
) (Result, error) {
	if !amount.Valid() {
		return Result{}, errs.Validation(CodeCalculation, "an amount needs a currency")
	}
	zero := money.Zero(amount.Currency())

	if len(group.Items) == 0 {
		// No taxes in the group: net and gross are the same, and the caller's amount is both.
		return Result{Net: amount, Tax: zero, Gross: amount}, nil
	}

	if group.PriceInclusive {
		return calculateInclusive(amount, group, rates, mode)
	}
	return calculateExclusive(amount, group, rates, mode)
}

// calculateExclusive treats the amount as the net and adds tax to it.
func calculateExclusive(
	net money.Money, group Group, rates map[id.ID]Version, mode round.RoundingMode,
) (Result, error) {
	zero := money.Zero(net.Currency())
	result := Result{Net: net, Tax: zero, Gross: net}

	// The base a compound tax computes on: the net plus everything charged before it.
	running := net

	for _, item := range group.Items {
		version, ok := rates[item.Tax.ID]
		if !ok {
			return Result{}, errs.Validation(CodeNoRate,
				"no rate is in force for that tax").WithParam("tax", item.Tax.Code)
		}

		base := net
		if item.CompoundOn {
			base = running
		}

		amount, err := taxOn(base, item.Tax, version, mode)
		if err != nil {
			return Result{}, err
		}

		result.Tax, err = result.Tax.Add(amount)
		if err != nil {
			return Result{}, err
		}
		running, err = running.Add(amount)
		if err != nil {
			return Result{}, err
		}
		result.Components = append(result.Components, Component{
			TaxID: item.Tax.ID, TaxCode: item.Tax.Code,
			RateMicro: version.RateMicro, Base: base, Amount: amount,
		})
	}

	gross, err := net.Add(result.Tax)
	if err != nil {
		return Result{}, err
	}
	result.Gross = gross
	return result, nil
}

// calculateInclusive treats the amount as the gross and extracts the tax from within it.
//
// # The arithmetic, and why it is done this way
//
// With a single rate r, net = gross × 10⁶ / (10⁶ + r). Done with the kernel's exact integer
// division and rounded once, so the pair (net, tax) always re-adds to exactly the gross the
// customer was quoted — which is the property a till receipt is judged by.
//
// The tax is then the REMAINDER, not a second computation: gross − net. Computing it
// independently would let rounding produce a pair that misses the gross by one minor unit, and
// that unit shows up in a day's takings.
func calculateInclusive(
	gross money.Money, group Group, rates map[id.ID]Version, mode round.RoundingMode,
) (Result, error) {
	// The combined divisor. Compound taxes multiply rather than add, which is what "a tax on a
	// tax-inclusive amount" means arithmetically.
	const scale = 1_000_000

	divisorNum := int64(scale)
	divisorDen := int64(scale)
	for _, item := range group.Items {
		version, ok := rates[item.Tax.ID]
		if !ok {
			return Result{}, errs.Validation(CodeNoRate,
				"no rate is in force for that tax").WithParam("tax", item.Tax.Code)
		}
		if item.Tax.Calculation != Percentage {
			// A fixed amount cannot be extracted from a price by ratio: it is added or it is
			// not. Mixing one into an inclusive group is a configuration error, and guessing
			// would produce a net nobody could reproduce.
			return Result{}, errs.Validation(CodeInvalidGroup,
				"a fixed tax cannot be part of a tax-inclusive price").
				WithParam("tax", item.Tax.Code)
		}
		divisorNum *= scale
		divisorDen *= scale + version.RateMicro
	}

	netMinor := mulDiv(gross.Minor(), divisorNum, divisorDen, mode)
	net := money.FromMinor(gross.Currency(), netMinor)

	tax, err := gross.Sub(net)
	if err != nil {
		return Result{}, err
	}

	result := Result{Net: net, Tax: tax, Gross: gross}

	// Split the extracted tax across the group's taxes in proportion to their rates, using
	// largest-remainder so the components sum EXACTLY to the tax (0.2's Allocate). Anything
	// else leaves a stray unit that a tax return has to explain.
	if len(group.Items) == 1 {
		item := group.Items[0]
		result.Components = []Component{{
			TaxID: item.Tax.ID, TaxCode: item.Tax.Code,
			RateMicro: rates[item.Tax.ID].RateMicro, Base: net, Amount: tax,
		}}
		return result, nil
	}

	weights := make([]int64, 0, len(group.Items))
	for _, item := range group.Items {
		weights = append(weights, rates[item.Tax.ID].RateMicro)
	}
	parts, err := tax.Allocate(weights)
	if err != nil {
		return Result{}, err
	}
	for i, item := range group.Items {
		result.Components = append(result.Components, Component{
			TaxID: item.Tax.ID, TaxCode: item.Tax.Code,
			RateMicro: rates[item.Tax.ID].RateMicro, Base: net, Amount: parts[i],
		})
	}
	return result, nil
}

// taxOn computes one tax on one base.
func taxOn(
	base money.Money, tax Tax, version Version, mode round.RoundingMode,
) (money.Money, error) {
	switch tax.Calculation {
	case Percentage:
		return base.MulPercent(version.Rate(), mode)
	case FixedPerUnit, FixedPerDocument:
		// A fixed levy ignores the base entirely — an excise duty per litre does not care what
		// the litre sold for.
		return money.FromMinor(base.Currency(), version.FixedMinor), nil
	}
	return money.Money{}, errs.Validation(CodeInvalidTax,
		"that is not a way of calculating a tax").WithParam("calculation", string(tax.Calculation))
}

// mulDiv computes n × num / den with ONE rounding, through big.Int.
//
// Big integers rather than int64 because the divisor for two compound taxes is already 10¹²,
// and a gross of a few million minor units overflows an int64 multiplication long before the
// division brings it back. The money kernel makes the same choice for the same reason (0.2).
//
// `round.Div` is the kernel's rounding, so a tax extracted from an inclusive price rounds the
// same way as every other amount in the system.
func mulDiv(n, num, den int64, mode round.RoundingMode) int64 {
	if den == 0 {
		return 0
	}
	product := new(big.Int).Mul(big.NewInt(n), big.NewInt(num))
	quotient := round.Div(product, big.NewInt(den), mode)
	if !quotient.IsInt64() {
		return 0
	}
	return quotient.Int64()
}
