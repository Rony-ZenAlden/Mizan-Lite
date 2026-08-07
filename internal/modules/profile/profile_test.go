package profile_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/audit"
	"github.com/mizan-erp/mizan/internal/modules/currency"
	"github.com/mizan-erp/mizan/internal/modules/profile"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/metadata"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
	"github.com/mizan-erp/mizan/internal/platform/seeds"
	"github.com/mizan-erp/mizan/migrations"

	// The settings registry is populated by PACKAGE INIT, so a key is only declared if
	// something imported the package that declares it. Bootstrap imports every module, so
	// production is complete; a focused test is not, and the first run of this file proved it
	// — a bundle setting ui.theme was rejected as "undeclared" and the wrong-type test passed
	// for the wrong reason. Imported blank so the tests below check what they claim to.
	_ "github.com/mizan-erp/mizan/internal/platform/ui"
)

// ── harness ─────────────────────────────────────────────────────────────────────

type fixture struct {
	svc      *profile.Service
	store    *database.Store
	settings *config.Settings
	audit    *audit.Service
	ctx      context.Context
}

// newFixture builds the module over the real merged schema, with the audit subscriber wired —
// Apply writes an audit entry inside its transaction, so every test here exercises that path.
func newFixture(t *testing.T, opts profile.Options) fixture {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")

	store, err := database.Open(database.Config{Path: path})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	merged := migrate.Merge(
		migrations.SQLite(),
		currency.NewModule(nil).Migrations(),
		audit.NewModule(nil).Migrations(),
		profile.NewModule(nil).Migrations(),
	)
	runner, err := migrate.New(store, migrate.Options{FS: merged, DBPath: path, SkipBackup: true})
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	if _, err = runner.Up(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	bus := eventbus.New(eventbus.Options{})
	auditSvc := audit.NewService(store, clock.System(), nil)
	if err = audit.NewModule(auditSvc).Subscribe(bus, nil); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	settings, err := config.Open(ctx, store, config.Options{})
	if err != nil {
		t.Fatalf("config.Open: %v", err)
	}

	opts.Bus = bus
	opts.Settings = settings
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}
	svc, err := profile.NewService(store, opts)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	return fixture{
		svc: svc, store: store, settings: settings, audit: auditSvc,
		ctx: config.Bind(ctx, settings),
	}
}

func overlay(files map[string]string) fstest.MapFS {
	out := fstest.MapFS{}
	for name, body := range files {
		out[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return out
}

// ── the shipped files ───────────────────────────────────────────────────────────

// The test that makes "a broken shipped file is fatal" a real guarantee rather than a claim:
// if any file in this build fails to parse or validate, NewService returns an error and this
// fails here, on a developer's machine.
func TestEveryShippedProfileLoads(t *testing.T) {
	f := newFixture(t, profile.Options{})

	countries := f.svc.Countries()
	if len(countries) == 0 {
		t.Fatal("no country profiles shipped")
	}
	if _, ok := f.svc.Country("SY"); !ok {
		t.Error("sy.json is missing — it is the development seed (§C.3)")
	}
	if len(f.svc.BusinessProfiles()) == 0 {
		t.Fatal("no business profiles shipped")
	}
	if len(f.svc.Problems()) != 0 {
		t.Errorf("shipped files reported problems: %+v", f.svc.Problems())
	}
}

// §C.3, asserted rather than trusted.
//
// No shipped profile encodes a tax rate or a chart of accounts. Those are jurisdictional facts
// that change, that must come from the customer's accountant, and that the architecture exists
// to accept as data — asserting one from memory is the failure this whole design avoids.
func TestNoShippedCountryEncodesTaxOrAccounting(t *testing.T) {
	f := newFixture(t, profile.Options{})

	for _, c := range f.svc.Countries() {
		if c.TaxProfile != nil {
			t.Errorf("%s ships a tax profile (%q) — §C.3 forbids asserting one", c.Code, *c.TaxProfile)
		}
		if c.ChartOfAccounts != nil {
			t.Errorf("%s ships a chart of accounts (%q) — §C.3 forbids asserting one",
				c.Code, *c.ChartOfAccounts)
		}
	}
}

// Every shipped country's name is a translation KEY that the catalogues actually carry.
// A key with no translation renders as itself, which is how a country list ends up showing
// "country.sy" to a customer.
func TestShippedCountryNameKeysAreTranslatable(t *testing.T) {
	f := newFixture(t, profile.Options{})

	for _, c := range f.svc.Countries() {
		if !strings.HasPrefix(c.NameKey, "country.") {
			t.Errorf("%s: name_key = %q, want a country.* translation key", c.Code, c.NameKey)
		}
	}
}

// ── the overlay ─────────────────────────────────────────────────────────────────

// Addendum §C's promise, end to end: a country arrives by dropping in a file.
func TestAUserFileAddsACountry(t *testing.T) {
	f := newFixture(t, profile.Options{UserFS: overlay(map[string]string{
		"seeds/country_profiles/jo.json": countryJSON("JO", "JOD", "+962"),
	})})

	jo, ok := f.svc.Country("JO")
	if !ok {
		t.Fatal("the added country did not load")
	}
	if jo.FunctionalCurrency != "JOD" {
		t.Errorf("currency = %q, want JOD", jo.FunctionalCurrency)
	}
	if len(f.svc.Problems()) != 0 {
		t.Errorf("a valid added file reported problems: %+v", f.svc.Problems())
	}
}

// The asymmetry, at the module level: a customer's broken file is reported and skipped, and
// every shipped profile still loads. A seed file must not be able to stop a shop from opening.
func TestABrokenUserFileDoesNotStopTheModule(t *testing.T) {
	f := newFixture(t, profile.Options{UserFS: overlay(map[string]string{
		"seeds/country_profiles/jo.json": `{ not json at all`,
	})})

	if _, ok := f.svc.Country("SY"); !ok {
		t.Error("a customer's broken file cost them the shipped profiles")
	}
	problems := f.svc.Problems()
	if len(problems) != 1 {
		t.Fatalf("%d problems, want 1 — a skipped file must be visible", len(problems))
	}
	if problems[0].Origin != seeds.OriginUser {
		t.Errorf("origin = %q, want %q", problems[0].Origin, seeds.OriginUser)
	}
}

// A user file that parses but is nonsense is skipped the same way — validation failures are
// not a separate, harsher class.
func TestAnInvalidUserProfileIsSkipped(t *testing.T) {
	f := newFixture(t, profile.Options{UserFS: overlay(map[string]string{
		// Valid JSON, month 13.
		"seeds/country_profiles/jo.json": strings.Replace(
			countryJSON("JO", "JOD", "+962"), `"fiscal_year_start_month": 1`,
			`"fiscal_year_start_month": 13`, 1),
	})})

	if _, ok := f.svc.Country("JO"); ok {
		t.Error("a profile with a 13th month was accepted")
	}
	if len(f.svc.Problems()) != 1 {
		t.Fatalf("problems = %+v, want the invalid file reported", f.svc.Problems())
	}
}

// The filename IS the identity. A file named jo.json declaring SY would load two profiles for
// Syria and none for Jordan, and every symptom would appear somewhere other than its cause.
func TestAMismatchedFilenameIsRejected(t *testing.T) {
	f := newFixture(t, profile.Options{UserFS: overlay(map[string]string{
		"seeds/country_profiles/jo.json": countryJSON("SY", "SYP", "+963"),
	})})

	if len(f.svc.Problems()) != 1 {
		t.Fatalf("problems = %+v, want the mismatch reported", f.svc.Problems())
	}
	// And the shipped SY is untouched — the bad file did not quietly become an override.
	sy, _ := f.svc.Country("SY")
	if sy.PhoneCountryCode != "+963" || sy.NameKey != "country.sy" {
		t.Errorf("the shipped SY profile was disturbed: %+v", sy)
	}
}

// ── the catalogue ───────────────────────────────────────────────────────────────

// The business-profile catalogue is seeded from the FILES — the first file-sourced seed in the
// system, closing the item 0.5 D7 deferred and 0.9 and 0.12 carried.
func TestTheCatalogueSeedsFromTheFiles(t *testing.T) {
	f := newFixture(t, profile.Options{})
	seeder := metadata.NewSeeder(f.store, clock.System())
	mod := profile.NewModule(f.svc)

	specs := mod.Metadata()
	if len(specs) != 1 || specs[0].Table != "business_profiles" {
		t.Fatalf("specs = %+v", specs)
	}
	report, err := seeder.Seed(f.ctx, specs[0])
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if report.Inserted != len(f.svc.BusinessProfiles()) {
		t.Errorf("inserted %d, want one row per shipped bundle (%d)",
			report.Inserted, len(f.svc.BusinessProfiles()))
	}

	// Idempotency: a second run must change nothing. A seeder that rewrites rows every launch
	// generates audit noise and looks like corruption.
	second, err := seeder.Seed(f.ctx, specs[0])
	if err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	if second.Inserted != 0 || second.Updated != 0 {
		t.Errorf("re-seeding changed rows: %+v", second)
	}
}

// ── bundle validation (D5) ──────────────────────────────────────────────────────

// A bundle naming a setting no module declares is rejected at LOAD.
//
// Settings.Set would reject it too — but at apply time, halfway through the wizard, on the
// customer's machine with three settings already written. Catching it here turns the same
// defect into something fixable.
func TestABundleNamingAnUndeclaredSettingIsRejected(t *testing.T) {
	f := newFixture(t, profile.Options{UserFS: overlay(map[string]string{
		"seeds/business_profiles/bakery.json": `{
			"code": "bakery", "name_key": "business_profile.bakery", "name": "Bakery",
			"description": "", "settings": {"bakery.proving_time": "2h"}, "flags": {}
		}`,
	})})

	if _, ok := f.svc.Business("bakery"); ok {
		t.Error("a bundle setting an undeclared key was accepted")
	}
	if len(f.svc.Problems()) != 1 {
		t.Fatalf("problems = %+v, want the bad bundle reported", f.svc.Problems())
	}
}

// A value of the wrong kind — "true" where a number belongs — is the mistake a human editing
// JSON actually makes.
func TestABundleWithAWrongTypedValueIsRejected(t *testing.T) {
	f := newFixture(t, profile.Options{UserFS: overlay(map[string]string{
		"seeds/business_profiles/bakery.json": `{
			"code": "bakery", "name_key": "business_profile.bakery", "name": "Bakery",
			"description": "", "settings": {"ui.theme": true}, "flags": {}
		}`,
	})})

	if _, ok := f.svc.Business("bakery"); ok {
		t.Error("a bundle setting an enum to a boolean was accepted")
	}
}

// ── applying ────────────────────────────────────────────────────────────────────

func TestApplyWritesAtCompanyScopeAndIsAudited(t *testing.T) {
	f := newFixture(t, profile.Options{UserFS: overlay(map[string]string{
		"seeds/business_profiles/bakery.json": `{
			"code": "bakery", "name_key": "business_profile.bakery", "name": "Bakery",
			"description": "", "settings": {"ui.theme": "dark"}, "flags": {}
		}`,
	})})
	if len(f.svc.Problems()) != 0 {
		t.Fatalf("the fixture bundle did not load: %+v", f.svc.Problems())
	}

	companyID, err := id.New()
	if err != nil {
		t.Fatalf("id: %v", err)
	}
	if err = f.svc.Apply(f.ctx, companyID, "bakery"); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The setting is a row at company scope, editable afterwards — a seeding operation, not a
	// resolution tier (0.5 §3.3). That distinction is what keeps every value individually
	// changeable once the profile has done its job.
	var count int
	if err = f.store.Reader(f.ctx).QueryRowContext(f.ctx,
		`SELECT COUNT(*) FROM settings WHERE scope = 'company' AND scope_id = ? AND setting_key = 'ui.theme'`,
		string(companyID)).Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Errorf("company-scope rows for ui.theme = %d, want 1", count)
	}

	entries, err := f.audit.Entries(f.ctx, audit.Filter{})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 1 || entries[0].Action != profile.ActionApplied {
		t.Fatalf("audit entries = %+v, want one %q", entries, profile.ActionApplied)
	}
	// The bundle itself is recorded, so "what did selecting Bakery actually change?" stays
	// answerable after the values have been edited.
	if !strings.Contains(entries[0].AfterJSON, "ui.theme") {
		t.Errorf("the applied bundle was not recorded: %q", entries[0].AfterJSON)
	}
}

// A profile that does not exist must not half-apply.
func TestApplyingAnUnknownProfileFails(t *testing.T) {
	f := newFixture(t, profile.Options{})
	companyID, _ := id.New()
	if err := f.svc.Apply(f.ctx, companyID, "no_such_trade"); err == nil {
		t.Fatal("applying an unknown profile succeeded")
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────────

func countryJSON(code, currency, phone string) string {
	return `{
  "country_code": "` + code + `",
  "name_key": "country.` + strings.ToLower(code) + `",
  "default_locale": "ar",
  "supported_locales": ["ar", "en"],
  "functional_currency_default": "` + currency + `",
  "pricing_currency_default": "USD",
  "chart_of_accounts_template": null,
  "tax_profile": null,
  "fiscal_year_start_month": 1,
  "date_format": "dd/MM/yyyy",
  "time_format": "HH:mm",
  "first_day_of_week": 6,
  "number_format": {
    "decimal_separator": ".", "thousand_separator": ",",
    "digit_grouping": [3], "numeral_system": "western"
  },
  "calendar": { "primary": "gregorian", "secondary": "hijri" },
  "address_format": ["street", "city", "country"],
  "phone_country_code": "` + phone + `",
  "rounding_rule": { "mode": "none", "increment_minor": 0, "stage": "grand_total" }
}`
}
