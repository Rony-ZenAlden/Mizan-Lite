package bindings_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/api/bindings"
	"github.com/mizan-erp/mizan/internal/api/setup"
	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/audit"
	identitydomain "github.com/mizan-erp/mizan/internal/modules/identity/domain"
	"github.com/mizan-erp/mizan/internal/modules/inventory"
	"github.com/mizan-erp/mizan/internal/modules/pricing"
)

const wizardPassword = "a sufficiently long passphrase"

// validInput is a complete, valid wizard submission.
func validInput() bindings.SetupInputDTO {
	return bindings.SetupInputDTO{
		Locale:               "ar",
		CountryCode:          "SY",
		CompanyCode:          "MAIN",
		CompanyName:          "Demo Trading",
		FunctionalCurrency:   "SYP",
		PricingCurrency:      "USD",
		BusinessProfile:      "general_retail",
		FiscalYearStartMonth: 1,
		FiscalYearStartYear:  2026,
		BranchCode:           "HQ",
		BranchName:           "Head Office",
		WarehouseCode:        "WH1",
		WarehouseName:        "Main Store",
		AdminUsername:        "nadia",
		AdminDisplayName:     "Nadia Haddad",
		AdminPassword:        wizardPassword,
	}
}

// ── status ──────────────────────────────────────────────────────────────────────

// A fresh install needs setting up, and the wizard gets everything it renders in one call.
func TestSetupIsRequiredOnAFreshInstall(t *testing.T) {
	set, _ := freshApp(t)

	result := set.Setup.Status()
	if !result.OK {
		t.Fatalf("Status failed: %+v", result.Error)
	}
	if !result.Data.Required {
		t.Fatal("a fresh install does not report setup as required")
	}
	if result.Data.Options == nil {
		t.Fatal("no options were offered")
	}

	options := result.Data.Options
	if len(options.Countries) == 0 {
		t.Error("no countries offered")
	}
	if len(options.BusinessProfiles) == 0 {
		t.Error("no business profiles offered")
	}
	if len(options.Currencies) == 0 {
		t.Error("no currencies offered")
	}
	if len(options.Locales) == 0 {
		t.Error("no languages offered — the wizard's first screen is a language picker")
	}
}

// Status stays callable forever, unlike Apply: the shell asks it on every launch to decide
// whether to route to the wizard, including on an install configured two years ago.
//
// It answers LESS afterwards. A public method should stop volunteering data once its purpose
// is over.
func TestStatusStaysCallableButSaysLessAfterSetup(t *testing.T) {
	set, _ := freshApp(t)
	if result := set.Setup.Apply(validInput()); !result.OK {
		t.Fatalf("Apply failed: %+v", result.Error)
	}

	result := set.Setup.Status()
	if !result.OK {
		t.Fatalf("Status stopped answering after setup: %+v", result.Error)
	}
	if result.Data.Required {
		t.Error("setup still reports as required after it ran")
	}
	if result.Data.Options != nil {
		t.Error("the country list is still offered at an unauthenticated endpoint after setup")
	}
}

// ── the gate (D2) ───────────────────────────────────────────────────────────────

// Apply is a public, unauthenticated method that creates an administrator. Once a company
// exists it must refuse, forever.
func TestSetupIsUnreachableAfterSetup(t *testing.T) {
	set, _ := freshApp(t)
	if result := set.Setup.Apply(validInput()); !result.OK {
		t.Fatalf("the first Apply failed: %+v", result.Error)
	}

	second := validInput()
	second.CompanyCode = "OTHER"
	second.AdminUsername = "intruder"

	result := set.Setup.Apply(second)
	if result.OK {
		t.Fatal("a second, unauthenticated Apply created another administrator")
	}
	if result.Error.Code != setup.CodeAlreadyDone {
		t.Errorf("code = %q, want %q", result.Error.Code, setup.CodeAlreadyDone)
	}

	// And it refuses on every subsequent attempt: the check is a row count, so there is no
	// flag to reset and no window that re-opens.
	if again := set.Setup.Apply(second); again.OK {
		t.Fatal("Apply succeeded on a third attempt")
	}
}

// The intruder's user must not exist. A refused call that had already written half its rows
// would be worse than one that ran.
func TestARefusedSetupWritesNothing(t *testing.T) {
	set, app := freshApp(t)
	if result := set.Setup.Apply(validInput()); !result.OK {
		t.Fatalf("Apply: %+v", result.Error)
	}

	second := validInput()
	second.AdminUsername = "intruder"
	_ = set.Setup.Apply(second)

	ctx := app.Context()
	company, err := app.Org.Company(ctx)
	if err != nil {
		t.Fatalf("Company: %v", err)
	}
	users, err := app.Identity.Users(ctx, company.ID)
	if err != nil {
		t.Fatalf("Users: %v", err)
	}
	for _, u := range users {
		if u.Username == "intruder" {
			t.Fatal("a refused setup created a user")
		}
	}
}

// ── what Apply builds ───────────────────────────────────────────────────────────

// The end-to-end guarantee: after the wizard, the administrator can sign in and holds
// administrator rights.
//
// A wizard that completes and leaves an unusable login is the worst outcome on a fresh install
// — the customer has a configured system they cannot enter — so it is asserted rather than
// assumed from "Apply returned Ok".
func TestTheAdministratorCanSignInAfterSetup(t *testing.T) {
	set, app := freshApp(t)

	result := set.Setup.Apply(validInput())
	if !result.OK {
		t.Fatalf("Apply failed: %+v", result.Error)
	}
	if result.Data.CompanyID == "" || result.Data.AdminUserID == "" {
		t.Fatalf("result is incomplete: %+v", result.Data)
	}

	login := set.Auth.Login("nadia", wizardPassword, false)
	if !login.OK {
		t.Fatalf("the administrator the wizard created cannot sign in: %+v", login.Error)
	}
	if login.Data.MustChange {
		t.Error("the administrator must change a password they chose 30 seconds ago")
	}
	if len(login.Data.Permissions) == 0 {
		t.Error("the administrator holds no permissions")
	}

	// Everything org owns exists, and the branch is the default. A company with no branch is a
	// state no screen in the system can repair.
	ctx := app.Context()
	branches, err := app.Org.Branches(ctx)
	if err != nil {
		t.Fatalf("Branches: %v", err)
	}
	if len(branches) != 1 || branches[0].Code != "HQ" {
		t.Fatalf("branches = %+v, want the one the wizard created", branches)
	}
}

// No default password ships (§13.1). The account works with what the user typed, and nothing
// else — the obvious failure would be a fallback credential nobody remembered to remove.
func TestNoDefaultPasswordWorks(t *testing.T) {
	set, _ := freshApp(t)
	if result := set.Setup.Apply(validInput()); !result.OK {
		t.Fatalf("Apply: %+v", result.Error)
	}

	for _, guess := range []string{"admin", "password", "mizan", "changeme", ""} {
		if login := set.Auth.Login("nadia", guess, false); login.OK {
			t.Fatalf("the administrator account accepts %q", guess)
		}
	}
}

// The password must never reach the audit trail, and the completion entry must be there.
func TestSetupIsAuditedWithoutTheCredential(t *testing.T) {
	set, app := freshApp(t)
	if result := set.Setup.Apply(validInput()); !result.OK {
		t.Fatalf("Apply: %+v", result.Error)
	}

	entries, err := app.Audit.Entries(app.Context(), audit.Filter{})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}

	var completed, correlations = 0, map[string]int{}
	for _, e := range entries {
		if strings.Contains(e.BeforeJSON+e.AfterJSON, wizardPassword) {
			t.Fatalf("action %q leaked the administrator's password", e.Action)
		}
		if e.Action == setup.ActionCompleted {
			completed++
		}
		// Setup is performed by nobody: there is no authenticated user, and the administrator
		// does not exist until part-way through (1.9 D5). Fabricating an actor would imply a
		// sequence of decisions nobody made.
		if !e.ActorUserID.IsZero() {
			t.Errorf("action %q records an actor during setup: %q", e.Action, e.ActorUserID)
		}
		correlations[string(e.Correlation)]++
	}
	if completed != 1 {
		t.Errorf("%d setup.completed entries, want exactly 1", completed)
	}
	// One user action, one correlation id — which is what makes "what did the wizard actually
	// do?" answerable from the trail alone.
	if len(correlations) != 1 {
		t.Errorf("%d distinct correlation ids across one setup run, want 1", len(correlations))
	}
}

// ── ordering (§3.2) ─────────────────────────────────────────────────────────────

// The person's explicit choice beats the bundle's suggestion.
//
// The business profile is applied first and the wizard's settings after; reversing that would
// let a trade's default silently override a language the user picked on screen two steps
// earlier — a bug that would look like the app ignoring them.
func TestTheWizardsChoiceBeatsTheProfilesDefault(t *testing.T) {
	// A bundle that actually sets the language, dropped in through the OVERLAY layer (1.8) —
	// the same mechanism a customer uses to add a profile.
	//
	// Necessary rather than decorative: the shipped bundles are empty, so with either of them
	// this test would pass no matter which order the writes happened in. It was written that
	// way first, and the mutation drill exposed it.
	set, app := freshAppWith(t, map[string]string{
		"seeds/business_profiles/loud.json": `{
			"code": "loud", "name_key": "business_profile.loud", "name": "Loud",
			"description": "sets a language, so ordering is observable",
			"settings": {"ui.locale": "ar"}, "flags": {}
		}`,
	})

	in := validInput()
	in.Locale = "en"
	in.BusinessProfile = "loud"
	if result := set.Setup.Apply(in); !result.OK {
		t.Fatalf("Apply: %+v", result.Error)
	}

	login := set.Auth.Login("nadia", wizardPassword, false)
	if !login.OK {
		t.Fatalf("Login: %+v", login.Error)
	}
	prefs := set.Config.Preferences()
	if !prefs.OK {
		t.Fatalf("Preferences: %+v", prefs.Error)
	}
	if prefs.Data.Locale != "en" {
		t.Errorf("locale = %q, want the language the user chose in the wizard", prefs.Data.Locale)
	}

	// And the bundle DID apply — otherwise this test would prove nothing about ordering, only
	// that an empty bundle overwrites nothing.
	if _, applied := app.Profile.Business("loud"); !applied {
		t.Fatal("the fixture bundle never loaded; the ordering above was not exercised")
	}
}

// ── validation ──────────────────────────────────────────────────────────────────

func TestInvalidSubmissionsAreRefusedBeforeAnythingIsWritten(t *testing.T) {
	cases := []struct {
		name string
		code string
		edit func(*bindings.SetupInputDTO)
	}{
		{"an unknown country", setup.CodeUnknownCountry,
			func(in *bindings.SetupInputDTO) { in.CountryCode = "ZZ" }},
		{"an unknown business profile", setup.CodeUnknownProfile,
			func(in *bindings.SetupInputDTO) { in.BusinessProfile = "spaceport" }},
		{"a currency nobody seeded", setup.CodeUnknownCurrency,
			func(in *bindings.SetupInputDTO) { in.FunctionalCurrency = "XAU" }},
		{"no company name", setup.CodeMissingField,
			func(in *bindings.SetupInputDTO) { in.CompanyName = "" }},
		{"no administrator", setup.CodeMissingField,
			func(in *bindings.SetupInputDTO) { in.AdminUsername = "" }},
		{"a 13th month", setup.CodeInvalidFiscalYear,
			func(in *bindings.SetupInputDTO) { in.FiscalYearStartMonth = 13 }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			set, app := freshApp(t)
			in := validInput()
			tc.edit(&in)

			result := set.Setup.Apply(in)
			if result.OK {
				t.Fatalf("%s was accepted", tc.name)
			}
			if result.Error.Code != tc.code {
				t.Errorf("code = %q, want %q", result.Error.Code, tc.code)
			}

			// Nothing was written, so the wizard is still available. A refusal that left a
			// company behind would lock the customer out of their own first-run screen.
			if _, err := app.Org.Company(app.Context()); err == nil {
				t.Fatal("a refused submission created a company")
			} else if !errs.IsCategory(err, errs.CategoryNotFound) {
				t.Fatalf("Company: %v", err)
			}
			if status := set.Setup.Status(); !status.Data.Required {
				t.Error("setup is no longer required after a refused submission")
			}
		})
	}
}

// A weak password is refused by identity's policy, not by a second copy of the rule here.
func TestAWeakAdministratorPasswordIsRefused(t *testing.T) {
	set, _ := freshApp(t)
	in := validInput()
	in.AdminPassword = "short"

	result := set.Setup.Apply(in)
	if result.OK {
		t.Fatal("a five-character administrator password was accepted")
	}
	if result.Error.Code != identitydomain.CodePasswordTooShort {
		t.Errorf("code = %q, want identity's policy to have refused it", result.Error.Code)
	}
}

// ── the invariant Step 1.8 broke (§5) ───────────────────────────────────────────

// Every currency a shipped country profile names must exist in the seeded currency table.
//
// Step 1.8 shipped sa/ae/eg naming SAR/AED/EGP, which currency did not seed — and
// companies.functional_currency carries a foreign key to currencies(code). Choosing Saudi
// Arabia would have failed mid-transaction on a fresh install, with a constraint error.
//
// Asserted against the REAL seeded database, so the next profile cannot reintroduce the gap.
func TestEveryShippedCountrysCurrenciesAreSeeded(t *testing.T) {
	_, app := freshApp(t)
	ctx := app.Context()

	seeded := map[string]bool{}
	infos, err := app.Currency.List(ctx, true)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, info := range infos {
		seeded[info.Code] = true
	}

	for _, c := range app.Profile.Countries() {
		for label, code := range map[string]string{
			"functional": c.FunctionalCurrency, "pricing": c.PricingCurrency,
		} {
			if code == "" {
				continue // pricing may be absent, meaning "same as functional" (§18.1)
			}
			if !seeded[code] {
				t.Errorf("country %s names %s currency %q, which is not seeded — "+
					"choosing it in the wizard would fail on a foreign key", c.Code, label, code)
			}
		}
	}
}

// Every shipped country can actually be set up. The strongest form of the check above: it runs
// the wizard once per country rather than reasoning about what it would do.
func TestEveryShippedCountryCanCompleteSetup(t *testing.T) {
	_, probe := freshApp(t)
	countries := probe.Profile.Countries()

	for _, country := range countries {
		t.Run(country.Code, func(t *testing.T) {
			set, _ := freshApp(t)
			in := validInput()
			in.CountryCode = country.Code
			in.Locale = country.DefaultLocale
			// Left empty on purpose, so the country profile's own defaults are what get used.
			in.FunctionalCurrency = ""
			in.PricingCurrency = ""
			in.FiscalYearStartMonth = 0

			if result := set.Setup.Apply(in); !result.OK {
				t.Fatalf("setting up in %s failed: %+v", country.Code, result.Error)
			}
			if login := set.Auth.Login("nadia", wizardPassword, false); !login.OK {
				t.Fatalf("the administrator cannot sign in after setup in %s: %+v",
					country.Code, login.Error)
			}
		})
	}
}

// freshApp boots an application with NOTHING provisioned — the state the wizard exists for.
func freshApp(t *testing.T) (*bindings.Set, *bootstrap.App) {
	t.Helper()
	return freshAppWith(t, nil)
}

// freshAppWith is freshApp with extra seed files written into the data directory first.
//
// Through the real overlay path (1.8), not a stub: the files land where a customer would put
// them, and the boot that follows discovers them exactly as it would in the field.
func freshAppWith(t *testing.T, files map[string]string) (*bindings.Set, *bootstrap.App) {
	t.Helper()
	app := bootWithSeeds(t, files)
	set := bindings.New()
	set.Attach(app)
	return set, app
}

// TestAFinishedSetupLeavesTheApplicationReadyToTrade
//
// # The out-of-the-box claim, asserted rather than assumed
//
// A first run migrates the schema, syncs permissions and creates the number series — that much
// happens with no company at all, and 10.8 verified it on a shipped artefact.
//
// Everything else is PER COMPANY and happens when the wizard finishes: the chart of accounts, the
// posting rules that decide where money lands, the units a product can be stocked in, and the
// roles a user can hold. A first run that migrated cleanly and left a company unable to sell
// anything would still look like a success in the log.
//
// This asserts the state a shopkeeper is actually in when the wizard closes.
func TestAFinishedSetupLeavesTheApplicationReadyToTrade(t *testing.T) {
	set, app := freshApp(t)
	ctx := app.Context()

	if result := set.Setup.Apply(validInput()); !result.OK {
		t.Fatalf("Apply: %+v", result.Error)
	}

	count := func(query string) int {
		t.Helper()
		var n int
		if err := app.DB.Reader(ctx).QueryRowContext(ctx, query).Scan(&n); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		return n
	}

	for _, ready := range []struct {
		what  string
		query string
		why   string
	}{
		{
			"units of measure", "SELECT COUNT(*) FROM units_of_measure",
			"a product cannot be created without one, so a shop could not add its first item",
		},
		{
			"chart of accounts", "SELECT COUNT(*) FROM accounts",
			"there would be nowhere for a sale to post",
		},
		{
			"posting rules", "SELECT COUNT(*) FROM posting_rules",
			"a sale would announce itself and nothing would decide which accounts move",
		},
		{
			"roles", "SELECT COUNT(*) FROM roles",
			"no user could be given any permission at all",
		},
		{
			"number series", "SELECT COUNT(*) FROM number_series",
			"no document could be numbered — the defect 7.6 found",
		},
		{
			"background jobs", "SELECT COUNT(*) FROM jobs",
			"the daily backup would never run",
		},
	} {
		if n := count(ready.query); n == 0 {
			t.Errorf("after setup there are no %s — %s", ready.what, ready.why)
		}
	}

	// And the company is provisioned with a branch and a warehouse, which every document needs.
	if n := count("SELECT COUNT(*) FROM companies"); n != 1 {
		t.Errorf("%d companies after setup, want 1", n)
	}
	if n := count("SELECT COUNT(*) FROM warehouses"); n == 0 {
		t.Error("no warehouse after setup, so stock has nowhere to be")
	}
}

// ── the receipt heading ─────────────────────────────────────────────────────────

// TestTheReceiptHeadingIsWhatTheWizardWasToldToPrint
//
// # Why this is asserted on a PRINTED receipt and not on the setting
//
// Reading the setting back would prove the wizard wrote a row. It would not prove anything a
// shopkeeper cares about, and this repository has found the same defect six times: built,
// tested, never connected. A setting nobody reads is that defect wearing a different hat.
//
// So the drill runs the whole path — wizard, setting, letterhead, template — and looks for the
// words on the receipt.
func TestTheReceiptHeadingIsWhatTheWizardWasToldToPrint(t *testing.T) {
	set, app := freshApp(t)

	in := validInput()
	in.CompanyName = "Al-Noor General Trading Est."
	in.ReceiptHeader = "Al-Noor Market"
	result := set.Setup.Apply(in)
	if !result.OK {
		t.Fatalf("Apply: %+v", result.Error)
	}

	html := printedReceipt(t, set, app, result.Data.WarehouseID)
	if !strings.Contains(html, "Al-Noor Market") {
		t.Errorf("the receipt does not carry the heading the wizard was given:\n%s", html)
	}
	// The registered name is what belongs on a tax return, not over the counter. A shop sets
	// this precisely so the legal name STOPS appearing on every customer's receipt.
	if strings.Contains(html, "Al-Noor General Trading Est.") {
		t.Error("the receipt still prints the registered name the heading was set to replace")
	}
}

// TestAReceiptWithNoHeadingSetStillNamesTheShop
//
// Blank means "use the company name", and that is what makes the wizard step optional and the
// whole feature safe to add with no migration: an installation that never sets it prints exactly
// what it printed before.
//
// The boundary that matters is WHITESPACE. A heading of three spaces is not a heading, and
// treating it as one would print an empty line where the shop's name belongs — with nothing on
// screen to explain why.
func TestAReceiptWithNoHeadingSetStillNamesTheShop(t *testing.T) {
	for _, header := range []string{"", "   "} {
		t.Run("header="+strconv.Quote(header), func(t *testing.T) {
			set, app := freshApp(t)

			in := validInput()
			in.CompanyName = "Corner Shop"
			in.ReceiptHeader = header
			result := set.Setup.Apply(in)
			if !result.OK {
				t.Fatalf("Apply: %+v", result.Error)
			}

			html := printedReceipt(t, set, app, result.Data.WarehouseID)
			if !strings.Contains(html, "Corner Shop") {
				t.Errorf("a receipt with no heading set does not name the shop:\n%s", html)
			}
		})
	}
}

// printedReceipt signs in, sells one thing, and returns the rendered receipt.
func printedReceipt(
	t *testing.T, set *bindings.Set, app *bootstrap.App, warehouseID string,
) string {
	t.Helper()

	if login := set.Auth.Login("nadia", wizardPassword, false); !login.OK {
		t.Fatalf("Login: %+v", login.Error)
	}
	ctx := app.Context()
	if err := app.Catalog.ApplyUnits(ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}

	if created := set.Catalog.CreateProduct(bindings.NewProductInput{
		Code: "TEA", Name: "Tea",
	}); !created.OK {
		t.Fatalf("CreateProduct: %+v", created.Error)
	}

	// The default variant, read back rather than assumed: a sale line is written against
	// identities, not codes.
	detail := set.Catalog.Product("TEA")
	if !detail.OK {
		t.Fatalf("Product: %+v", detail.Error)
	}
	if len(detail.Data.Variants) == 0 {
		t.Fatal("a product was created with no variant, which §A.1 forbids")
	}

	// A default sale price list, because posting resolves prices rather than trusting the
	// caller — a till operator who can type a price is a discount nobody approved.
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		t.Fatalf("CurrentCompanyID: %v", err)
	}
	if _, err = app.Pricing.CreateList(ctx, pricing.NewListInput{
		CompanyID: companyID, Code: "RETAIL", Name: "Retail",
		CurrencyCode: "SYP", Direction: "sale", IsDefault: true,
	}); err != nil {
		t.Fatalf("CreateList: %v", err)
	}
	if _, err = app.Pricing.SetPrice(ctx, pricing.SetPriceInput{
		CompanyID: companyID, ListCode: "RETAIL",
		VariantID: id.ID(detail.Data.Variants[0].ID), PriceMinor: 1500,
	}); err != nil {
		t.Fatalf("SetPrice: %v", err)
	}

	// And something on the shelf. Posting a sale issues stock, and there is none on a fresh
	// install — which is correct, and which this fixture has to satisfy rather than bypass.
	if _, err = app.Inventory.Move(ctx, inventory.MoveInput{
		CompanyID: companyID, WarehouseID: id.ID(warehouseID),
		ProductID: id.ID(detail.Data.Product.ID), VariantID: id.ID(detail.Data.Variants[0].ID),
		Type: "receipt", QuantityMicro: 10_000_000, UnitCostMicro: 1_000_000,
	}); err != nil {
		t.Fatalf("Move: %v", err)
	}
	// The warehouse and the date are both named: stock comes out of somewhere, and the date must
	// fall inside a fiscal period the wizard opened.
	draft := set.Sales.Draft(bindings.DraftInput{
		WarehouseID: warehouseID, Date: "2026-06-01", Currency: "SYP",
	})
	if !draft.OK {
		t.Fatalf("Draft: %+v", draft.Error)
	}
	line := set.Sales.AddLine(bindings.AddLineToSaleInput{
		DocumentID: draft.Data.ID, VariantID: detail.Data.Variants[0].ID,
		// No unit named: the service falls back to the product's own sales unit, which is
		// exactly what the till does when nobody picks one.
		QuantityMicro: "1000000",
	})
	if !line.OK {
		t.Fatalf("AddLine: %+v", line.Error)
	}
	if posted := set.Sales.Post(draft.Data.ID); !posted.OK {
		t.Fatalf("Post: %+v", posted.Error)
	}

	printed := set.Sales.Print(draft.Data.ID, "receipt", "thermal80")
	if !printed.OK {
		t.Fatalf("Print: %+v", printed.Error)
	}
	return printed.Data.HTML
}
