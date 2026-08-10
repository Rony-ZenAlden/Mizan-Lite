package tax_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/round"
	"github.com/mizan-erp/mizan/internal/modules/accounting"
	"github.com/mizan-erp/mizan/internal/modules/audit"
	"github.com/mizan-erp/mizan/internal/modules/currency"
	"github.com/mizan-erp/mizan/internal/modules/org"
	"github.com/mizan-erp/mizan/internal/modules/profile"
	"github.com/mizan-erp/mizan/internal/modules/tax"
	taxdomain "github.com/mizan-erp/mizan/internal/modules/tax/domain"
	taxsqlite "github.com/mizan-erp/mizan/internal/modules/tax/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
	"github.com/mizan-erp/mizan/migrations"
)

type fixture struct {
	svc       *tax.Service
	store     *database.Store
	settings  *config.Settings
	companyID id.ID
	ctx       context.Context
	usd       money.Currency
}

func newFixture(t *testing.T) fixture {
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
		org.NewModule(nil).Migrations(),
		audit.NewModule(nil).Migrations(),
		profile.NewModule(nil).Migrations(),
		accounting.NewModule(nil).Migrations(),
		tax.NewModule(nil).Migrations(),
	)
	runner, err := migrate.New(store, migrate.Options{FS: merged, DBPath: path, SkipBackup: true})
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	if _, err = runner.Up(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	identifier, _ := id.New()
	now := clock.Format(clock.System().Now())
	if _, err = store.Writer(ctx).ExecContext(ctx,
		`INSERT INTO currencies (id, code, name, symbol, decimal_places, created_at, updated_at)
		 VALUES (?, 'USD', 'US Dollar', '$', 2, ?, ?)`,
		string(identifier), now, now); err != nil {
		t.Fatalf("seeding currency: %v", err)
	}

	bus := eventbus.New(eventbus.Options{})
	orgSvc := org.NewService(store, clock.System(), bus)
	result, err := orgSvc.Provision(ctx, org.ProvisionInput{
		Company:              org.CompanyInput{Code: "MAIN", Name: "Demo", CountryCode: "SY", FunctionalCurrency: "USD"},
		Branch:               org.LocationInput{Code: "HQ", Name: "Head Office"},
		Warehouse:            org.LocationInput{Code: "WH1", Name: "Main"},
		FiscalYearStartMonth: time.January,
		FiscalYearStartYear:  2026,
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	settings, err := config.Open(ctx, store, config.Options{})
	if err != nil {
		t.Fatalf("config.Open: %v", err)
	}

	usd, err := money.NewCurrency("USD", 2, round.HalfAwayFromZero)
	if err != nil {
		t.Fatalf("currency: %v", err)
	}

	return fixture{
		svc: tax.NewService(store, clock.System()), store: store, settings: settings,
		companyID: result.CompanyID, ctx: config.Bind(ctx, settings), usd: usd,
	}
}

// enable turns tax on at SYSTEM scope.
//
// Not company scope, and the reason is worth recording: resolving a company-scoped setting
// needs a config.ScopeProvider that can read the acting company from the context, which in
// production is appctx.Scopes (1.3) and in this fixture is nothing. Writing at company scope
// here would store a row the resolver could never see — a test that passed only because the
// value it set was invisible would be the worst kind.
//
// System scope is also what a single-company installation means in practice.
func (f fixture) enable(t *testing.T) {
	t.Helper()
	if err := f.settings.Set(f.ctx, config.ScopeSystem, id.ID(""), "tax.enabled", true); err != nil {
		t.Fatalf("enabling tax: %v", err)
	}
	if err := f.settings.Reload(f.ctx); err != nil {
		t.Fatalf("reload: %v", err)
	}
}

// seedVAT configures one percentage tax in a default group. Called only by tests: NOTHING seeds
// a rate in the product (§C.3).
func (f fixture) seedVAT(t *testing.T, rateMicro int64, inclusive bool) id.ID {
	t.Helper()
	taxID, _ := id.New()
	groupID, _ := id.New()
	versionID, _ := id.New()

	err := f.store.Do(f.ctx, func(ctx context.Context) error {
		levy := taxdomain.Tax{
			ID: taxID, Code: "VAT", Name: "VAT", Kind: taxdomain.VAT,
			Calculation: taxdomain.Percentage, IsRecoverable: true, IsActive: true,
		}
		if err := f.repos(t).InsertTax(ctx, f.companyID, levy); err != nil {
			return err
		}
		if err := f.repos(t).InsertVersion(ctx, versionID, taxdomain.Version{
			TaxID: taxID, RateMicro: rateMicro,
			From: time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC),
		}); err != nil {
			return err
		}
		return f.repos(t).InsertGroup(ctx, f.companyID, taxdomain.Group{
			ID: groupID, Code: "STD", Name: "Standard", PriceInclusive: inclusive,
			Items: []taxdomain.GroupItem{{Tax: levy, Sequence: 1}},
		}, true)
	})
	if err != nil {
		t.Fatalf("seeding VAT: %v", err)
	}
	return groupID
}

func today() time.Time { return time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC) }

// ── the shipped state (§19.5, §C.3) ─────────────────────────────────────────────

// v1 ships with tax DISABLED and no rates. A document resolves to zero, and records WHY.
//
// "Zero tax" with no reason is the answer nobody can audit — it is indistinguishable from a
// bug that failed to compute anything.
func TestTaxIsDisabledByDefaultAndSaysSo(t *testing.T) {
	f := newFixture(t)

	quote, err := f.svc.Calculate(f.ctx, tax.Request{
		CompanyID: f.companyID, Date: today(),
		Amount: money.FromMinor(f.usd, 10_000),
	})
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}

	if quote.Tax.Minor() != 0 {
		t.Errorf("tax = %d on a fresh install, want 0", quote.Tax.Minor())
	}
	if quote.Net.Minor() != 10_000 || quote.Gross.Minor() != 10_000 {
		t.Errorf("net = %d, gross = %d; want the amount untouched",
			quote.Net.Minor(), quote.Gross.Minor())
	}
	if quote.Reason != taxdomain.ReasonTaxDisabled {
		t.Errorf("reason = %q, want %q", quote.Reason, taxdomain.ReasonTaxDisabled)
	}
}

// Turning it on needs NO MIGRATION — the tables and code paths were there all along (§19.5).
func TestEnablingTaxNeedsNoMigration(t *testing.T) {
	f := newFixture(t)
	f.seedVAT(t, 150_000, false)

	// Still disabled: zero, with the reason.
	before, err := f.svc.Calculate(f.ctx, tax.Request{
		CompanyID: f.companyID, Date: today(), Amount: money.FromMinor(f.usd, 10_000),
	})
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if before.Tax.Minor() != 0 || before.Reason != taxdomain.ReasonTaxDisabled {
		t.Fatalf("tax = %d, reason = %q before enabling", before.Tax.Minor(), before.Reason)
	}

	f.enable(t)

	after, err := f.svc.Calculate(f.ctx, tax.Request{
		CompanyID: f.companyID, Date: today(), Amount: money.FromMinor(f.usd, 10_000),
	})
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if after.Tax.Minor() != 1_500 {
		t.Errorf("tax = %d after enabling, want 1500", after.Tax.Minor())
	}
	if after.Reason != taxdomain.ReasonCompanyDefault {
		t.Errorf("reason = %q, want the company default", after.Reason)
	}
}

// ── resolution through the real tables ──────────────────────────────────────────

func TestAnOverrideBeatsTheCompanyDefault(t *testing.T) {
	f := newFixture(t)
	f.seedVAT(t, 150_000, false)
	f.enable(t)

	// A zero-rated group, as an export customer would have.
	zeroGroup, _ := id.New()
	if err := f.store.Do(f.ctx, func(ctx context.Context) error {
		return f.repos(t).InsertGroup(ctx, f.companyID, taxdomain.Group{
			ID: zeroGroup, Code: "ZERO", Name: "Zero rated",
		}, false)
	}); err != nil {
		t.Fatalf("seeding the zero group: %v", err)
	}

	quote, err := f.svc.Calculate(f.ctx, tax.Request{
		CompanyID: f.companyID, Date: today(),
		Amount:          money.FromMinor(f.usd, 10_000),
		DocumentGroupID: zeroGroup,
	})
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if quote.Tax.Minor() != 0 {
		t.Errorf("tax = %d, want 0 — the override did not win", quote.Tax.Minor())
	}
	if quote.Reason != taxdomain.ReasonDocumentOverride {
		t.Errorf("reason = %q, want the document override", quote.Reason)
	}
}

// An exemption resolves to no tax AND records the certificate reason — the evidence a tax
// authority asks for years later.
func TestAnExemptPartnerIsNotChargedAndTheReasonIsRecorded(t *testing.T) {
	f := newFixture(t)
	f.seedVAT(t, 150_000, false)
	f.enable(t)

	partner, _ := id.New()
	exemption, _ := id.New()
	if err := f.store.Do(f.ctx, func(ctx context.Context) error {
		return f.repos(t).InsertExemption(ctx, exemption, f.companyID, partner,
			"export", "CERT-2026-11", time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))
	}); err != nil {
		t.Fatalf("seeding the exemption: %v", err)
	}

	quote, err := f.svc.Calculate(f.ctx, tax.Request{
		CompanyID: f.companyID, PartnerID: partner, Date: today(),
		Amount: money.FromMinor(f.usd, 10_000),
	})
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if quote.Tax.Minor() != 0 {
		t.Errorf("an exempt customer was charged %d", quote.Tax.Minor())
	}
	if quote.Reason != taxdomain.ReasonPartnerExempt {
		t.Errorf("reason = %q, want the exemption", quote.Reason)
	}
	if quote.ExemptionCode != "export" {
		t.Errorf("exemption code = %q, want the reason recorded on the document", quote.ExemptionCode)
	}
}

// ── versioned rates through the real tables (§19.2) ─────────────────────────────

// The property that makes a rate change safe: yesterday's invoice keeps yesterday's rate.
func TestChangingARateLeavesHistoryAlone(t *testing.T) {
	f := newFixture(t)
	f.seedVAT(t, 150_000, false)
	f.enable(t)

	before := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	changeOn := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	after := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)

	taxes, err := f.taxIDs()
	if err != nil || len(taxes) != 1 {
		t.Fatalf("taxes = %v, %v", taxes, err)
	}
	if err = f.svc.ChangeRate(f.ctx, taxes[0], 160_000, changeOn); err != nil {
		t.Fatalf("ChangeRate: %v", err)
	}

	// A document dated before the change still resolves 15%.
	old, err := f.svc.Calculate(f.ctx, tax.Request{
		CompanyID: f.companyID, Date: before, Amount: money.FromMinor(f.usd, 10_000),
	})
	if err != nil {
		t.Fatalf("Calculate before: %v", err)
	}
	if old.Tax.Minor() != 1_500 {
		t.Errorf("a February document resolved %d, want 1500 — history was restated",
			old.Tax.Minor())
	}

	// One dated after resolves 16%.
	current, err := f.svc.Calculate(f.ctx, tax.Request{
		CompanyID: f.companyID, Date: after, Amount: money.FromMinor(f.usd, 10_000),
	})
	if err != nil {
		t.Fatalf("Calculate after: %v", err)
	}
	if current.Tax.Minor() != 1_600 {
		t.Errorf("a May document resolved %d, want 1600", current.Tax.Minor())
	}
}

// ── inclusive pricing through the real tables ───────────────────────────────────

func TestAnInclusiveGroupDecomposesTheShelfPrice(t *testing.T) {
	f := newFixture(t)
	f.seedVAT(t, 150_000, true)
	f.enable(t)

	quote, err := f.svc.Calculate(f.ctx, tax.Request{
		CompanyID: f.companyID, Date: today(),
		Amount: money.FromMinor(f.usd, 11_500), // the shelf price
	})
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if quote.Net.Minor() != 10_000 || quote.Tax.Minor() != 1_500 {
		t.Errorf("net = %d, tax = %d; want 10000 and 1500",
			quote.Net.Minor(), quote.Tax.Minor())
	}
	if quote.Gross.Minor() != 11_500 {
		t.Errorf("gross = %d, want the shelf price back", quote.Gross.Minor())
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────────

// repos reaches the repository directly, so a test can seed the configuration a customer would
// otherwise enter through a screen that does not exist yet.
//
// Deliberately not a service method: the product seeds NO rate (§C.3), so a "seed a tax"
// service call would be a surface that exists only for tests.
func (f fixture) repos(t *testing.T) *taxsqlite.Repos {
	t.Helper()
	return taxsqlite.New(f.store, clock.System())
}

func (f fixture) taxIDs() ([]id.ID, error) {
	rows, err := f.store.Reader(f.ctx).QueryContext(f.ctx, `SELECT id FROM taxes ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []id.ID
	for rows.Next() {
		var taxID id.ID
		if scanErr := rows.Scan(&taxID); scanErr != nil {
			return nil, scanErr
		}
		out = append(out, taxID)
	}
	return out, rows.Err()
}
