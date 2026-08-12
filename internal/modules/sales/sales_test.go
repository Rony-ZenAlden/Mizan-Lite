package sales_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/accounting"
	"github.com/mizan-erp/mizan/internal/modules/audit"
	"github.com/mizan-erp/mizan/internal/modules/catalog"
	catalogdomain "github.com/mizan-erp/mizan/internal/modules/catalog/domain"
	"github.com/mizan-erp/mizan/internal/modules/currency"
	"github.com/mizan-erp/mizan/internal/modules/identity"
	"github.com/mizan-erp/mizan/internal/modules/inventory"
	"github.com/mizan-erp/mizan/internal/modules/org"
	"github.com/mizan-erp/mizan/internal/modules/partner"
	"github.com/mizan-erp/mizan/internal/modules/profile"
	"github.com/mizan-erp/mizan/internal/modules/sales"
	"github.com/mizan-erp/mizan/internal/modules/tax"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
	"github.com/mizan-erp/mizan/migrations"
)

type fixture struct {
	svc         *sales.Service
	audit       *audit.Service
	bus         *eventbus.Bus
	store       *database.Store
	companyID   id.ID
	branchID    id.ID
	warehouseID id.ID
	ctx         context.Context

	// Populated by newSellingFixture, which adds a catalog.
	catalog      *catalog.Service
	variant      catalogdomain.Variant
	heavyVariant catalogdomain.Variant
	kilogramID   id.ID
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

	// Sales lines key products, variants, units, lots, serials, and stock movements, so the
	// fixture runs the same merged schema the application does.
	merged := migrate.Merge(
		migrations.SQLite(),
		currency.NewModule(nil).Migrations(),
		org.NewModule(nil).Migrations(),
		audit.NewModule(nil).Migrations(),
		identity.NewModule(nil).Migrations(),
		profile.NewModule(nil).Migrations(),
		accounting.NewModule(nil).Migrations(),
		tax.NewModule(nil).Migrations(),
		catalog.NewModule(nil).Migrations(),
		partner.NewModule(nil).Migrations(),
		inventory.NewModule(nil).Migrations(),
		sales.NewModule(nil).Migrations(),
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
	provisioned, err := orgSvc.Provision(ctx, org.ProvisionInput{
		Company: org.CompanyInput{
			Code: "MAIN", Name: "Demo", CountryCode: "SY", FunctionalCurrency: "SYP",
		},
		Branch:               org.LocationInput{Code: "HQ", Name: "Head Office"},
		Warehouse:            org.LocationInput{Code: "WH1", Name: "Main"},
		FiscalYearStartMonth: time.January,
		FiscalYearStartYear:  2026,
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	return fixture{
		svc:   sales.NewService(store, sales.Options{Clock: clock.System(), Bus: bus}),
		audit: auditSvc, store: store, bus: bus,
		companyID: provisioned.CompanyID, branchID: provisioned.BranchID,
		warehouseID: provisioned.WarehouseID, ctx: ctx,
	}
}

// newSellingFixture adds a catalog with something to sell, and wires the Catalog port to the real
// service — so a line's snapshot and its unit conversion travel the path production uses.
func newSellingFixture(t *testing.T) fixture {
	t.Helper()
	f := newFixture(t)

	bus := eventbus.New(eventbus.Options{})
	catalogSvc, err := catalog.NewService(f.store, catalog.Options{
		Clock: clock.System(), Bus: bus,
	})
	if err != nil {
		t.Fatalf("catalog.NewService: %v", err)
	}
	if err = catalogSvc.ApplyUnits(f.ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}

	// A widget, counted in pieces — which cannot be divided (3.1's rule).
	_, variant, err := catalogSvc.CreateProduct(f.ctx, catalog.NewProductInput{
		CompanyID: f.companyID, Code: "WIDGET", Name: "Widget", StockUnit: "PCS",
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}

	// Something stocked in grams and sold in kilograms, so the dual quantity has work to do.
	_, heavy, err := catalogSvc.CreateProduct(f.ctx, catalog.NewProductInput{
		CompanyID: f.companyID, Code: "SAND", Name: "Sand",
		StockUnit: "G", SalesUnit: "KG",
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	kilogram, err := catalogSvc.UnitByCode(f.ctx, "KG")
	if err != nil {
		t.Fatalf("UnitByCode: %v", err)
	}

	f.catalog = catalogSvc
	f.variant = variant
	f.heavyVariant = heavy
	f.kilogramID = kilogram.ID

	// The service is rebuilt with the catalog port wired.
	f.svc = sales.NewService(f.store, sales.Options{
		Clock: clock.System(), Bus: f.bus, Catalog: realCatalog{svc: catalogSvc},
	})
	return f
}

func (f fixture) series(t *testing.T, code, prefix string, padding int) {
	t.Helper()
	if _, err := f.svc.CreateSeries(f.ctx, sales.NewSeriesInput{
		CompanyID: f.companyID, Code: code, Prefix: prefix, Padding: padding,
	}); err != nil {
		t.Fatalf("CreateSeries(%s): %v", code, err)
	}
}

// take allocates one number through the service's own transaction, the way posting will.
func (f fixture) take(t *testing.T, code string) string {
	t.Helper()
	number, err := f.svc.AllocateNumberForTest(f.ctx, f.branchID, code)
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	return number
}

// ── allocation ──────────────────────────────────────────────────────────────────

func TestNumbersAreIssuedConsecutively(t *testing.T) {
	f := newFixture(t)
	f.series(t, sales.SeriesInvoice, "INV-", 6)

	want := []string{"INV-000001", "INV-000002", "INV-000003"}
	for i, expected := range want {
		if got := f.take(t, sales.SeriesInvoice); got != expected {
			t.Errorf("number %d = %q, want %q", i, got, expected)
		}
	}
}

// The counter is written back in the SAME transaction that took the number. A caller that
// discarded the advanced series would issue "INV-000001" forever, and every invoice after the
// first would collide.
func TestTakingANumberAdvancesTheStoredCounter(t *testing.T) {
	f := newFixture(t)
	f.series(t, sales.SeriesInvoice, "INV-", 6)

	f.take(t, sales.SeriesInvoice)

	var next int64
	if err := f.store.Reader(f.ctx).QueryRowContext(f.ctx,
		`SELECT next_value FROM number_series WHERE code = ?`,
		sales.SeriesInvoice).Scan(&next); err != nil {
		t.Fatalf("reading the counter: %v", err)
	}
	if next != 2 {
		t.Errorf("next_value = %d after issuing one number, want 2", next)
	}
}

// # The guarantee that matters
//
// Two terminals posting at the same moment must never produce the same number. SQLite serialises
// writers, and the counter is read through the WRITER connection inside the transaction — so the
// second allocation sees the first one's advance rather than a stale value.
//
// Run concurrently rather than in sequence, because a sequential test passes on an implementation
// that reads from the reader connection and would still collide in production.
func TestConcurrentAllocationsNeverCollide(t *testing.T) {
	f := newFixture(t)
	f.series(t, sales.SeriesInvoice, "INV-", 6)

	const terminals = 8
	results := make(chan string, terminals)
	errsCh := make(chan error, terminals)

	for i := 0; i < terminals; i++ {
		go func() {
			number, err := f.svc.AllocateNumberForTest(f.ctx, f.branchID, sales.SeriesInvoice)
			if err != nil {
				errsCh <- err
				return
			}
			results <- number
		}()
	}

	seen := map[string]bool{}
	for i := 0; i < terminals; i++ {
		select {
		case err := <-errsCh:
			t.Fatalf("allocation failed under contention: %v", err)
		case number := <-results:
			if seen[number] {
				t.Fatalf("two terminals were issued %q", number)
			}
			seen[number] = true
		}
	}
	if len(seen) != terminals {
		t.Errorf("%d distinct numbers for %d terminals", len(seen), terminals)
	}
}

// ── resolution ──────────────────────────────────────────────────────────────────

// The most specific series wins, exactly as a branch's account mapping beats the company's
// (Phase 2) and a partner's price list beats the default (Phase 3). One resolution shape, reused.
func TestABranchSeriesBeatsTheCompanyWideOne(t *testing.T) {
	f := newFixture(t)
	f.series(t, sales.SeriesInvoice, "INV-", 6)

	if _, err := f.svc.CreateSeries(f.ctx, sales.NewSeriesInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		Code: sales.SeriesInvoice, Prefix: "HQ-", Padding: 4,
	}); err != nil {
		t.Fatalf("CreateSeries: %v", err)
	}

	if got := f.take(t, sales.SeriesInvoice); got != "HQ-0001" {
		t.Errorf("number = %q, want the branch's HQ-0001", got)
	}
}

// Two kinds of document keep separate sequences: an invoice and a credit note both starting at 1
// is correct, and sharing a counter would make the invoice numbers jump every time a return was
// issued.
func TestEachDocumentKindHasItsOwnSequence(t *testing.T) {
	f := newFixture(t)
	f.series(t, sales.SeriesInvoice, "INV-", 6)
	f.series(t, sales.SeriesCreditNote, "CN-", 6)

	f.take(t, sales.SeriesInvoice)
	f.take(t, sales.SeriesInvoice)

	if got := f.take(t, sales.SeriesCreditNote); got != "CN-000001" {
		t.Errorf("first credit note = %q, want CN-000001", got)
	}
	if got := f.take(t, sales.SeriesInvoice); got != "INV-000003" {
		t.Errorf("next invoice = %q, want INV-000003", got)
	}
}

func TestAllocatingFromAMissingSeriesIsRefusedByName(t *testing.T) {
	f := newFixture(t)

	_, err := f.svc.AllocateNumberForTest(f.ctx, f.branchID, sales.SeriesInvoice)
	if err == nil {
		t.Fatal("a number was allocated from a series that does not exist")
	}
	if code := errs.CodeOf(err); code != sales.CodeUnknownSeries {
		t.Errorf("code = %q, want %q", code, sales.CodeUnknownSeries)
	}
	typed, _ := errs.AsError(err)
	if typed.Params["code"] != sales.SeriesInvoice {
		t.Errorf("params = %v, want the series code that was missing", typed.Params)
	}
}

// ── preview ─────────────────────────────────────────────────────────────────────

// A screen that says "this will be INV-000124" must not consume it. The separation of Format from
// Next in the domain is what makes this possible without a second implementation of the padding.
func TestPreviewingANumberDoesNotConsumeIt(t *testing.T) {
	f := newFixture(t)
	f.series(t, sales.SeriesInvoice, "INV-", 6)

	preview, err := f.svc.PreviewNumber(f.ctx, f.branchID, sales.SeriesInvoice)
	if err != nil {
		t.Fatalf("PreviewNumber: %v", err)
	}
	if preview != "INV-000001" {
		t.Errorf("preview = %q, want INV-000001", preview)
	}

	// Twice, to prove it takes nothing.
	if again, _ := f.svc.PreviewNumber(f.ctx, f.branchID, sales.SeriesInvoice); again != preview {
		t.Errorf("a second preview said %q — the first consumed a number", again)
	}
	// And the real allocation still gets the first number.
	if got := f.take(t, sales.SeriesInvoice); got != "INV-000001" {
		t.Errorf("allocation = %q after two previews, want INV-000001", got)
	}
}

// ── audit ───────────────────────────────────────────────────────────────────────

func TestCreatingASeriesIsAudited(t *testing.T) {
	f := newFixture(t)
	f.series(t, sales.SeriesInvoice, "INV-", 6)

	entries, err := f.audit.Entries(f.ctx, audit.Filter{EntityType: sales.EntitySeries})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 1 || entries[0].Action != sales.ActionSeriesCreated {
		t.Errorf("entries = %+v, want one series-created", entries)
	}
}
