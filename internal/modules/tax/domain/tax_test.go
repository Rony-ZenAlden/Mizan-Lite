package domain_test

import (
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/round"
	"github.com/mizan-erp/mizan/internal/modules/tax/domain"
)

// A two-decimal currency, so the rounding cases below are the ones a real till hits.
func currency(t *testing.T) money.Currency {
	t.Helper()
	c, err := money.NewCurrency("USD", 2, round.HalfAwayFromZero)
	if err != nil {
		t.Fatalf("currency: %v", err)
	}
	return c
}

func taxID(t *testing.T, seed string) id.ID {
	t.Helper()
	return id.ID(seed)
}

// vat builds a percentage tax at a rate given in micro (15% = 150000).
func vat(t *testing.T, code string, rateMicro int64) (domain.Tax, domain.Version) {
	t.Helper()
	levy := domain.Tax{
		ID: taxID(t, code), Code: code, Name: code,
		Kind: domain.VAT, Calculation: domain.Percentage,
		IsRecoverable: true, IsActive: true,
	}
	version := domain.Version{
		TaxID: levy.ID, RateMicro: rateMicro,
		From: time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC),
	}
	return levy, version
}

func group(inclusive bool, items ...domain.GroupItem) domain.Group {
	return domain.Group{ID: id.ID("g"), Code: "g", PriceInclusive: inclusive, Items: items}
}

// ── versioned rates (§19.2) ─────────────────────────────────────────────────────

// The critical property: a rate change adds a VERSION, and historical documents keep resolving
// the old rate. Editing in place would silently falsify every invoice already issued.
func TestAHistoricalDateResolvesTheHistoricalRate(t *testing.T) {
	levy := id.ID("vat")
	versions := []domain.Version{
		{
			TaxID: levy, RateMicro: 150_000,
			From: time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC),
			To:   time.Date(2025, time.December, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			TaxID: levy, RateMicro: 160_000,
			From: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	cases := []struct {
		when time.Time
		want int64
	}{
		{time.Date(2024, time.June, 1, 0, 0, 0, 0, time.UTC), 150_000},
		{time.Date(2025, time.December, 31, 0, 0, 0, 0, time.UTC), 150_000}, // the last day
		{time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), 160_000},   // the first day
		{time.Date(2030, time.March, 9, 0, 0, 0, 0, time.UTC), 160_000},     // still in force
	}
	for _, tc := range cases {
		version, err := domain.RateOn(versions, tc.when)
		if err != nil {
			t.Fatalf("%s: %v", tc.when.Format("2006-01-02"), err)
		}
		if version.RateMicro != tc.want {
			t.Errorf("%s resolved %d, want %d",
				tc.when.Format("2006-01-02"), version.RateMicro, tc.want)
		}
	}
}

// A date with no rate is a configuration gap. Silently charging nothing would hide it until an
// authority noticed.
func TestADateWithNoRateIsATypedFailure(t *testing.T) {
	versions := []domain.Version{{
		TaxID: id.ID("vat"), RateMicro: 150_000,
		From: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
	}}

	_, err := domain.RateOn(versions, time.Date(2025, time.June, 1, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("a date before every version resolved a rate")
	}
	if code := errs.CodeOf(err); code != domain.CodeNoRate {
		t.Errorf("code = %q, want %q", code, domain.CodeNoRate)
	}
}

// ── resolution (§19.3) ──────────────────────────────────────────────────────────

// The eight-level priority, as a table. The order has legal consequences — an export
// customer's exemption must beat a product's default — so it is asserted as a whole.
func TestResolutionPriority(t *testing.T) {
	line, doc := id.ID("line"), id.ID("doc")
	partner, product := id.ID("partner"), id.ID("product")
	category, branch, company := id.ID("category"), id.ID("branch"), id.ID("company")

	full := domain.Candidates{
		LineOverride: line, DocumentOverride: doc, PartnerGroup: partner,
		ProductGroup: product, CategoryGroup: category,
		BranchDefault: branch, CompanyDefault: company,
	}

	cases := []struct {
		name       string
		enabled    bool
		candidates domain.Candidates
		wantGroup  id.ID
		wantReason domain.Reason
	}{
		{"a line override beats everything", true, full, line, domain.ReasonLineOverride},
		{"then the document", true, without(full, "line"), doc, domain.ReasonDocumentOverride},
		{"an exemption beats every default", true,
			domain.Candidates{PartnerExempt: true, PartnerGroup: partner, CompanyDefault: company},
			id.ID(""), domain.ReasonPartnerExempt},
		{"then the partner's group", true, without(full, "line", "doc"), partner, domain.ReasonPartnerGroup},
		{"then the product's", true, without(full, "line", "doc", "partner"), product, domain.ReasonProductGroup},
		{"then the category's", true,
			without(full, "line", "doc", "partner", "product"), category, domain.ReasonCategoryGroup},
		{"then the branch default", true,
			without(full, "line", "doc", "partner", "product", "category"), branch, domain.ReasonBranchDefault},
		{"finally the company default", true,
			domain.Candidates{CompanyDefault: company}, company, domain.ReasonCompanyDefault},
		{"nothing configured resolves to no group", true,
			domain.Candidates{}, id.ID(""), domain.ReasonNoGroup},
		// §19.5: disabling tax short-circuits the whole ladder, and records WHY.
		{"disabled beats every override", false, full, id.ID(""), domain.ReasonTaxDisabled},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := domain.Resolve(tc.enabled, tc.candidates)
			if got.GroupID != tc.wantGroup {
				t.Errorf("group = %q, want %q", got.GroupID, tc.wantGroup)
			}
			if got.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", got.Reason, tc.wantReason)
			}
		})
	}
}

// without clears the named levels, so each case states only what it removes.
func without(c domain.Candidates, levels ...string) domain.Candidates {
	for _, level := range levels {
		switch level {
		case "line":
			c.LineOverride = id.ID("")
		case "doc":
			c.DocumentOverride = id.ID("")
		case "partner":
			c.PartnerGroup = id.ID("")
		case "product":
			c.ProductGroup = id.ID("")
		case "category":
			c.CategoryGroup = id.ID("")
		}
	}
	return c
}

// ── exclusive calculation (§19.4) ───────────────────────────────────────────────

func TestExclusiveTaxIsAddedToTheNet(t *testing.T) {
	usd := currency(t)
	levy, version := vat(t, "VAT", 150_000) // 15%

	result, err := domain.Calculate(
		money.FromMinor(usd, 10_000), // 100.00 net
		group(false, domain.GroupItem{Tax: levy, Sequence: 1}),
		map[id.ID]domain.Version{levy.ID: version},
		round.HalfAwayFromZero,
	)
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}

	if result.Net.Minor() != 10_000 {
		t.Errorf("net = %d, want 10000", result.Net.Minor())
	}
	if result.Tax.Minor() != 1_500 {
		t.Errorf("tax = %d, want 1500", result.Tax.Minor())
	}
	if result.Gross.Minor() != 11_500 {
		t.Errorf("gross = %d, want 11500", result.Gross.Minor())
	}
	if len(result.Components) != 1 || result.Components[0].Base.Minor() != 10_000 {
		t.Errorf("components = %+v, want one on a base of 10000", result.Components)
	}
}

// A compound tax computes on a base that already includes the tax before it — which is why the
// group's order is data rather than an implementation detail.
func TestACompoundTaxComputesOnTheTaxedBase(t *testing.T) {
	usd := currency(t)
	first, firstVersion := vat(t, "VAT", 100_000)   // 10%
	second, secondVersion := vat(t, "MUNI", 50_000) // 5%, compound

	result, err := domain.Calculate(
		money.FromMinor(usd, 10_000), // 100.00
		group(false,
			domain.GroupItem{Tax: first, Sequence: 1},
			domain.GroupItem{Tax: second, Sequence: 2, CompoundOn: true},
		),
		map[id.ID]domain.Version{first.ID: firstVersion, second.ID: secondVersion},
		round.HalfAwayFromZero,
	)
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}

	// 10% of 100 is 10; 5% of 110 is 5.50 — NOT 5% of 100.
	if result.Components[0].Amount.Minor() != 1_000 {
		t.Errorf("first tax = %d, want 1000", result.Components[0].Amount.Minor())
	}
	if result.Components[1].Amount.Minor() != 550 {
		t.Errorf("compound tax = %d, want 550 (5%% of 110, not of 100)",
			result.Components[1].Amount.Minor())
	}
	if result.Components[1].Base.Minor() != 11_000 {
		t.Errorf("compound base = %d, want 11000 — the base is recorded so the working is shown",
			result.Components[1].Base.Minor())
	}
	if result.Tax.Minor() != 1_550 || result.Gross.Minor() != 11_550 {
		t.Errorf("tax = %d, gross = %d; want 1550 and 11550", result.Tax.Minor(), result.Gross.Minor())
	}
}

// ── inclusive calculation — the one that goes wrong (§19.4) ─────────────────────

// A retail price of 115.00 at 15% is 100.00 net and 15.00 tax.
//
// Multiplying the gross by the rate would give 17.25 — the tax on the wrong base, and how a
// shop's returns stop matching its till.
func TestInclusivePricesAreDecomposedNotMultiplied(t *testing.T) {
	usd := currency(t)
	levy, version := vat(t, "VAT", 150_000)

	result, err := domain.Calculate(
		money.FromMinor(usd, 11_500), // 115.00 on the shelf
		group(true, domain.GroupItem{Tax: levy, Sequence: 1}),
		map[id.ID]domain.Version{levy.ID: version},
		round.HalfAwayFromZero,
	)
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}

	if result.Net.Minor() != 10_000 {
		t.Errorf("net = %d, want 10000", result.Net.Minor())
	}
	if result.Tax.Minor() != 1_500 {
		t.Errorf("tax = %d, want 1500 — not 1725, which is the rate on the wrong base",
			result.Tax.Minor())
	}
	if result.Gross.Minor() != 11_500 {
		t.Errorf("gross = %d, want the shelf price back", result.Gross.Minor())
	}
}

// The property a till receipt is judged by: net + tax is EXACTLY the price quoted, at every
// amount, with no stray minor unit.
func TestAnInclusivePriceAlwaysReAddsToItself(t *testing.T) {
	usd := currency(t)
	levy, version := vat(t, "VAT", 150_000)
	g := group(true, domain.GroupItem{Tax: levy, Sequence: 1})
	rates := map[id.ID]domain.Version{levy.ID: version}

	// Every price from 1 cent to 10.00, which covers the rounding boundaries densely.
	for gross := int64(1); gross <= 1_000; gross++ {
		result, err := domain.Calculate(money.FromMinor(usd, gross), g, rates, round.HalfAwayFromZero)
		if err != nil {
			t.Fatalf("%d: %v", gross, err)
		}
		if sum := result.Net.Minor() + result.Tax.Minor(); sum != gross {
			t.Fatalf("gross %d decomposed to net %d + tax %d = %d",
				gross, result.Net.Minor(), result.Tax.Minor(), sum)
		}
	}
}

// With several taxes in an inclusive group, the components must sum EXACTLY to the extracted
// tax — largest-remainder allocation (0.2), not independent rounding that leaves a stray unit
// for a tax return to explain.
func TestInclusiveComponentsSumExactlyToTheTax(t *testing.T) {
	usd := currency(t)
	first, firstVersion := vat(t, "VAT", 150_000)
	second, secondVersion := vat(t, "MUNI", 20_000)
	g := group(true,
		domain.GroupItem{Tax: first, Sequence: 1},
		domain.GroupItem{Tax: second, Sequence: 2},
	)
	rates := map[id.ID]domain.Version{first.ID: firstVersion, second.ID: secondVersion}

	for gross := int64(1); gross <= 2_000; gross++ {
		result, err := domain.Calculate(money.FromMinor(usd, gross), g, rates, round.HalfAwayFromZero)
		if err != nil {
			t.Fatalf("%d: %v", gross, err)
		}
		var parts int64
		for _, component := range result.Components {
			parts += component.Amount.Minor()
		}
		if parts != result.Tax.Minor() {
			t.Fatalf("gross %d: components sum to %d, tax is %d", gross, parts, result.Tax.Minor())
		}
		if result.Net.Minor()+result.Tax.Minor() != gross {
			t.Fatalf("gross %d: net + tax = %d", gross, result.Net.Minor()+result.Tax.Minor())
		}
	}
}

// A fixed levy cannot be extracted from a price by ratio: it is added or it is not. Guessing
// would produce a net nobody could reproduce.
func TestAFixedTaxInAnInclusiveGroupIsRefused(t *testing.T) {
	usd := currency(t)
	levy := domain.Tax{
		ID: id.ID("duty"), Code: "DUTY", Kind: domain.Excise,
		Calculation: domain.FixedPerUnit, IsActive: true,
	}
	version := domain.Version{TaxID: levy.ID, FixedMinor: 500}

	_, err := domain.Calculate(
		money.FromMinor(usd, 10_000),
		group(true, domain.GroupItem{Tax: levy, Sequence: 1}),
		map[id.ID]domain.Version{levy.ID: version},
		round.HalfAwayFromZero,
	)
	if err == nil {
		t.Fatal("a fixed duty was extracted from an inclusive price")
	}
	if code := errs.CodeOf(err); code != domain.CodeInvalidGroup {
		t.Errorf("code = %q, want %q", code, domain.CodeInvalidGroup)
	}
}

func TestAFixedTaxIsAddedRegardlessOfTheBase(t *testing.T) {
	usd := currency(t)
	levy := domain.Tax{
		ID: id.ID("duty"), Code: "DUTY", Kind: domain.Excise,
		Calculation: domain.FixedPerDocument, IsActive: true,
	}
	version := domain.Version{TaxID: levy.ID, FixedMinor: 250}

	for _, base := range []int64{1_000, 50_000, 1_000_000} {
		result, err := domain.Calculate(
			money.FromMinor(usd, base),
			group(false, domain.GroupItem{Tax: levy, Sequence: 1}),
			map[id.ID]domain.Version{levy.ID: version},
			round.HalfAwayFromZero,
		)
		if err != nil {
			t.Fatalf("Calculate: %v", err)
		}
		// An excise duty per litre does not care what the litre sold for.
		if result.Tax.Minor() != 250 {
			t.Errorf("base %d produced tax %d, want 250", base, result.Tax.Minor())
		}
	}
}

// ── the empty state (§19.5) ─────────────────────────────────────────────────────

// A group with no taxes — which is what v1 ships, because no rate is ours to invent — leaves
// the amount untouched and produces no components.
func TestAnEmptyGroupChargesNothing(t *testing.T) {
	usd := currency(t)

	result, err := domain.Calculate(
		money.FromMinor(usd, 12_345), domain.Group{ID: id.ID("g")},
		map[id.ID]domain.Version{}, round.HalfAwayFromZero,
	)
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if result.Tax.Minor() != 0 {
		t.Errorf("tax = %d, want 0", result.Tax.Minor())
	}
	if result.Net.Minor() != 12_345 || result.Gross.Minor() != 12_345 {
		t.Errorf("net = %d, gross = %d; want the amount unchanged",
			result.Net.Minor(), result.Gross.Minor())
	}
	if len(result.Components) != 0 {
		t.Errorf("%d components on an untaxed line", len(result.Components))
	}
}

// A tax in a group with no rate in force is a configuration gap, reported rather than treated
// as zero.
func TestATaxWithNoRateIsRefused(t *testing.T) {
	usd := currency(t)
	levy, _ := vat(t, "VAT", 150_000)

	_, err := domain.Calculate(
		money.FromMinor(usd, 10_000),
		group(false, domain.GroupItem{Tax: levy, Sequence: 1}),
		map[id.ID]domain.Version{}, // no rate supplied
		round.HalfAwayFromZero,
	)
	if err == nil {
		t.Fatal("a tax with no rate silently charged nothing")
	}
	if code := errs.CodeOf(err); code != domain.CodeNoRate {
		t.Errorf("code = %q, want %q", code, domain.CodeNoRate)
	}
}
