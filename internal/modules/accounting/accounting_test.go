package accounting_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/accounting"
	"github.com/mizan-erp/mizan/internal/modules/accounting/domain"
	"github.com/mizan-erp/mizan/internal/modules/audit"
	"github.com/mizan-erp/mizan/internal/modules/currency"
	"github.com/mizan-erp/mizan/internal/modules/org"
	"github.com/mizan-erp/mizan/internal/modules/profile"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
	"github.com/mizan-erp/mizan/internal/platform/seeds"
	"github.com/mizan-erp/mizan/migrations"
)

// ── harness ─────────────────────────────────────────────────────────────────────

type fixture struct {
	svc       *accounting.Service
	audit     *audit.Service
	store     *database.Store
	companyID id.ID
	ctx       context.Context
}

// newFixture builds a provisioned company over the real merged schema, with the audit
// subscriber wired — ApplyChart writes its entry inside the same transaction, so every test
// here exercises that path.
func newFixture(t *testing.T, opts accounting.Options) fixture {
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
		 VALUES (?, 'SYP', 'Syrian Pound', 'SYP', 0, ?, ?)`,
		string(identifier), now, now); err != nil {
		t.Fatalf("seeding currency: %v", err)
	}

	bus := eventbus.New(eventbus.Options{})
	auditSvc := audit.NewService(store, clock.System(), nil)
	if err = audit.NewModule(auditSvc).Subscribe(bus, nil); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	orgSvc := org.NewService(store, clock.System(), bus)
	result, err := orgSvc.Provision(ctx, org.ProvisionInput{
		Company:              org.CompanyInput{Code: "MAIN", Name: "Demo", CountryCode: "SY", FunctionalCurrency: "SYP"},
		Branch:               org.LocationInput{Code: "HQ", Name: "Head Office"},
		Warehouse:            org.LocationInput{Code: "WH1", Name: "Main"},
		FiscalYearStartMonth: time.January,
		FiscalYearStartYear:  2026,
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	opts.Bus = bus
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}
	svc, err := accounting.NewService(store, opts)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	return fixture{svc: svc, audit: auditSvc, store: store, companyID: result.CompanyID, ctx: ctx}
}

func overlay(files map[string]string) fstest.MapFS {
	out := fstest.MapFS{}
	for name, body := range files {
		out[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return out
}

// ── the shipped chart ───────────────────────────────────────────────────────────

// The test that makes "a broken shipped chart is fatal" a real guarantee: if any template in
// this build fails to parse or validate, NewService returns an error and this fails here.
func TestEveryShippedChartLoads(t *testing.T) {
	f := newFixture(t, accounting.Options{})

	charts := f.svc.Charts()
	if len(charts) == 0 {
		t.Fatal("no charts of accounts shipped")
	}
	if _, ok := f.svc.Chart("generic_trading"); !ok {
		t.Error("generic_trading is missing — the setup wizard applies it by name")
	}
	if len(f.svc.Problems()) != 0 {
		t.Errorf("shipped charts reported problems: %+v", f.svc.Problems())
	}
}

// Every mapping key the posting layer will ask for must be supplied by every shipped chart.
//
// Checked at load rather than at first posting: a chart with no receivables account fails when
// a customer tries to sell something, months later, which is both the worst moment and the
// hardest to trace back to a template.
func TestEveryShippedChartSuppliesEveryRequiredMapping(t *testing.T) {
	f := newFixture(t, accounting.Options{})

	for _, chart := range f.svc.Charts() {
		for _, key := range domain.RequiredMappings() {
			if _, mapped := chart.Mappings[key]; !mapped {
				t.Errorf("chart %q does not map %q", chart.Code, key)
			}
		}
	}
}

// ── applying a chart ────────────────────────────────────────────────────────────

func TestApplyingAChartBuildsTheHierarchy(t *testing.T) {
	f := newFixture(t, accounting.Options{})

	if err := f.svc.ApplyChart(f.ctx, f.companyID, "generic_trading"); err != nil {
		t.Fatalf("ApplyChart: %v", err)
	}

	accounts, err := f.svc.Accounts(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Accounts: %v", err)
	}
	chart, _ := f.svc.Chart("generic_trading")
	if len(accounts) != len(chart.Accounts) {
		t.Fatalf("%d accounts, want %d", len(accounts), len(chart.Accounts))
	}

	byCode := map[string]domain.Account{}
	for _, account := range accounts {
		byCode[account.Code] = account
	}

	// The path is what makes "every asset account" a prefix scan rather than a recursive walk.
	receivable := byCode["1200"]
	if receivable.Path != "/1000/1100/1200/" {
		t.Errorf("path = %q, want the ancestors root-first", receivable.Path)
	}
	if receivable.Depth != 2 {
		t.Errorf("depth = %d, want 2", receivable.Depth)
	}
	if !receivable.IsUnder(byCode["1000"].Path) {
		t.Error("receivables does not sit under assets")
	}

	// Normal balance is derived from the type and stored, so no report has to re-derive it.
	if receivable.Normal != domain.Debit {
		t.Errorf("an asset's normal balance is %q, want debit", receivable.Normal)
	}
	if byCode["4100"].Normal != domain.Credit {
		t.Errorf("revenue's normal balance is %q, want credit", byCode["4100"].Normal)
	}
}

// ONLY LEAF ACCOUNTS ACCEPT POSTINGS (§20.1).
//
// A posting to a roll-up account makes every ancestor count it twice, and the trial balance
// still balances — the silent-wrongness failure this phase is shaped around.
func TestOnlyLeafAccountsArePostable(t *testing.T) {
	f := newFixture(t, accounting.Options{})
	if err := f.svc.ApplyChart(f.ctx, f.companyID, "generic_trading"); err != nil {
		t.Fatalf("ApplyChart: %v", err)
	}

	accounts, err := f.svc.Accounts(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Accounts: %v", err)
	}

	parents := map[id.ID]bool{}
	for _, account := range accounts {
		if !account.ParentID.IsZero() {
			parents[account.ParentID] = true
		}
	}

	for _, account := range accounts {
		hasChildren := parents[account.ID]
		if hasChildren && account.IsPostable {
			t.Errorf("%s (%s) has children and still accepts postings", account.Code, account.Name)
		}
		if !hasChildren && !account.IsPostable {
			t.Errorf("%s (%s) is a leaf and cannot take postings", account.Code, account.Name)
		}
		if err = account.RequirePostable(); (err == nil) != !hasChildren {
			t.Errorf("%s: RequirePostable disagrees with the hierarchy", account.Code)
		}
	}
}

// A chart is the skeleton every future posting hangs on. Applying a second over the first would
// leave two overlapping hierarchies and a mapping layer pointing at whichever won.
func TestAChartIsAppliedOnlyOnce(t *testing.T) {
	f := newFixture(t, accounting.Options{})
	if err := f.svc.ApplyChart(f.ctx, f.companyID, "generic_trading"); err != nil {
		t.Fatalf("ApplyChart: %v", err)
	}

	err := f.svc.ApplyChart(f.ctx, f.companyID, "generic_trading")
	if err == nil {
		t.Fatal("a second chart of accounts was applied over the first")
	}
	if code := errs.CodeOf(err); code != accounting.CodeChartExists {
		t.Errorf("code = %q, want %q", code, accounting.CodeChartExists)
	}
}

// The mapping layer is the indirection that lets one posting rule serve a shop whose
// receivables account is 1200 and one whose accountant numbered it 130.
func TestMappingsResolveToPostableAccounts(t *testing.T) {
	f := newFixture(t, accounting.Options{})
	if err := f.svc.ApplyChart(f.ctx, f.companyID, "generic_trading"); err != nil {
		t.Fatalf("ApplyChart: %v", err)
	}

	for _, key := range domain.RequiredMappings() {
		account, err := f.svc.ResolveMapping(f.ctx, f.companyID, id.ID(""), key)
		if err != nil {
			t.Errorf("%s does not resolve: %v", key, err)
			continue
		}
		// A mapping pointing at a heading would fail at the moment a document posts.
		if err = account.RequirePostable(); err != nil {
			t.Errorf("%s resolves to %s, which cannot take postings: %v", key, account.Code, err)
		}
	}

	receivable, err := f.svc.ResolveMapping(f.ctx, f.companyID, id.ID(""), domain.MappingAR)
	if err != nil {
		t.Fatalf("AR: %v", err)
	}
	if receivable.Code != "1200" {
		t.Errorf("AR resolves to %q, want 1200", receivable.Code)
	}
}

func TestAnUnknownMappingIsATypedNotFound(t *testing.T) {
	f := newFixture(t, accounting.Options{})
	if err := f.svc.ApplyChart(f.ctx, f.companyID, "generic_trading"); err != nil {
		t.Fatalf("ApplyChart: %v", err)
	}

	_, err := f.svc.ResolveMapping(f.ctx, f.companyID, id.ID(""), "NOT_A_ROLE")
	if err == nil {
		t.Fatal("an unmapped role resolved to something")
	}
	if !errs.IsCategory(err, errs.CategoryNotFound) {
		t.Errorf("category = %v, want NotFound", errs.CategoryOf(err))
	}
}

// Applying a chart is an audited act, inside the same transaction (1.7 phase D7).
func TestApplyingAChartIsAudited(t *testing.T) {
	f := newFixture(t, accounting.Options{})
	if err := f.svc.ApplyChart(f.ctx, f.companyID, "generic_trading"); err != nil {
		t.Fatalf("ApplyChart: %v", err)
	}

	entries, err := f.audit.Entries(f.ctx, audit.Filter{EntityType: accounting.EntityChart})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d entries, want exactly 1", len(entries))
	}
	if entries[0].Action != accounting.ActionChartApplied {
		t.Errorf("action = %q, want %q", entries[0].Action, accounting.ActionChartApplied)
	}
}

// A failure part-way through leaves NO accounts — the missing ones are exactly those nobody
// notices until a posting needs them.
func TestAFailedApplyLeavesNoAccounts(t *testing.T) {
	f := newFixture(t, accounting.Options{})

	wanted := errBusinessRuleFailed{}
	err := f.store.Do(f.ctx, func(ctx context.Context) error {
		if applyErr := f.svc.ApplyChart(ctx, f.companyID, "generic_trading"); applyErr != nil {
			return applyErr
		}
		return wanted
	})
	if err != wanted {
		t.Fatalf("Do = %v, want the business failure to propagate", err)
	}

	accounts, err := f.svc.Accounts(context.Background(), f.companyID)
	if err != nil {
		t.Fatalf("Accounts: %v", err)
	}
	if len(accounts) != 0 {
		t.Fatalf("%d accounts survived a rolled-back transaction", len(accounts))
	}
}

type errBusinessRuleFailed struct{}

func (errBusinessRuleFailed) Error() string { return "a business rule failed" }

// ── customer-supplied charts ────────────────────────────────────────────────────

// An accountant's own chart, dropped into the data directory, is the point of the file-based
// design: adding one is adding a file, with no release.
func TestAnAccountantsOwnChartLoads(t *testing.T) {
	f := newFixture(t, accounting.Options{UserFS: overlay(map[string]string{
		"seeds/chart_of_accounts/boutique.json": customChart("boutique"),
	})})

	chart, ok := f.svc.Chart("boutique")
	if !ok {
		t.Fatalf("the added chart did not load: %+v", f.svc.Problems())
	}
	if len(chart.Accounts) == 0 {
		t.Error("the added chart has no accounts")
	}

	if err := f.svc.ApplyChart(f.ctx, f.companyID, "boutique"); err != nil {
		t.Fatalf("ApplyChart: %v", err)
	}
	accounts, err := f.svc.Accounts(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Accounts: %v", err)
	}
	if len(accounts) != len(chart.Accounts) {
		t.Errorf("%d accounts applied, want %d", len(accounts), len(chart.Accounts))
	}
}

// A chart missing a required role is refused at LOAD, not at the first posting.
func TestAChartMissingARequiredMappingIsRefused(t *testing.T) {
	broken := strings.Replace(customChart("broken"), `"AR": "1200",`, "", 1)
	f := newFixture(t, accounting.Options{UserFS: overlay(map[string]string{
		"seeds/chart_of_accounts/broken.json": broken,
	})})

	if _, ok := f.svc.Chart("broken"); ok {
		t.Error("a chart with no receivables account was accepted")
	}
	problems := f.svc.Problems()
	if len(problems) != 1 {
		t.Fatalf("%d problems, want the broken chart reported", len(problems))
	}
	if problems[0].Origin != seeds.OriginUser {
		t.Errorf("origin = %q, want %q", problems[0].Origin, seeds.OriginUser)
	}
}

// A loop in the hierarchy is invisible to the schema — SQL catches only an account that is its
// own parent — and would make path-building recurse forever.
func TestAChartWithALoopIsRefused(t *testing.T) {
	looped := strings.Replace(
		customChart("looped"),
		`{ "code": "1000", "name": "Assets", "type": "asset" }`,
		`{ "code": "1000", "name": "Assets", "type": "asset", "parent": "1200" }`, 1)

	f := newFixture(t, accounting.Options{UserFS: overlay(map[string]string{
		"seeds/chart_of_accounts/looped.json": looped,
	})})

	if _, ok := f.svc.Chart("looped"); ok {
		t.Error("a chart whose hierarchy loops was accepted")
	}
	if len(f.svc.Problems()) != 1 {
		t.Fatalf("problems = %+v, want the loop reported", f.svc.Problems())
	}
}

// A child whose type differs from its parent would roll up into the wrong statement, and every
// total would still add up.
func TestAChartWithAMismatchedTypeIsRefused(t *testing.T) {
	mismatched := strings.Replace(
		customChart("mismatched"),
		`{ "code": "1200", "name": "Receivable", "parent": "1000", "type": "asset" }`,
		`{ "code": "1200", "name": "Receivable", "parent": "1000", "type": "revenue" }`, 1)

	f := newFixture(t, accounting.Options{UserFS: overlay(map[string]string{
		"seeds/chart_of_accounts/mismatched.json": mismatched,
	})})

	if _, ok := f.svc.Chart("mismatched"); ok {
		t.Error("an asset account with a revenue parent was accepted")
	}
}

// customChart is a minimal but VALID chart: every required mapping, one level of hierarchy.
func customChart(code string) string {
	return `{
  "code": "` + code + `",
  "name_key": "chart.` + code + `",
  "name": "Custom",
  "description": "a chart written by an accountant",
  "accounts": [
    { "code": "1000", "name": "Assets", "type": "asset" },
    { "code": "1110", "name": "Cash", "parent": "1000", "type": "asset" },
    { "code": "1120", "name": "Bank", "parent": "1000", "type": "asset" },
    { "code": "1200", "name": "Receivable", "parent": "1000", "type": "asset" },
    { "code": "1300", "name": "Inventory", "parent": "1000", "type": "asset" },
    { "code": "1400", "name": "Recoverable tax", "parent": "1000", "type": "asset" },
    { "code": "2000", "name": "Liabilities", "type": "liability" },
    { "code": "2100", "name": "Payable", "parent": "2000", "type": "liability" },
    { "code": "2200", "name": "Tax payable", "parent": "2000", "type": "liability" },
    { "code": "3000", "name": "Equity", "type": "equity" },
    { "code": "3100", "name": "Retained", "parent": "3000", "type": "equity" },
    { "code": "3900", "name": "Opening", "parent": "3000", "type": "equity" },
    { "code": "4100", "name": "Sales", "type": "revenue" },
    { "code": "5000", "name": "Expenses", "type": "expense" },
    { "code": "5100", "name": "Cost of sales", "parent": "5000", "type": "expense" },
    { "code": "5800", "name": "Exchange", "parent": "5000", "type": "expense" },
    { "code": "5900", "name": "Rounding", "parent": "5000", "type": "expense" }
  ],
  "mappings": {
    "CASH": "1110",
    "BANK": "1120",
    "AR": "1200",
    "INVENTORY": "1300",
    "TAX_RECEIVABLE": "1400",
    "AP": "2100",
    "TAX_PAYABLE": "2200",
    "RETAINED_EARNINGS": "3100",
    "OPENING_BALANCE": "3900",
    "SALES": "4100",
    "COGS": "5100",
    "FX_GAIN_LOSS": "5800",
    "ROUNDING_DIFF": "5900"
  }
}`
}
