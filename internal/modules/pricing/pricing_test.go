package pricing_test

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
	"github.com/mizan-erp/mizan/internal/modules/org"
	"github.com/mizan-erp/mizan/internal/modules/partner"
	"github.com/mizan-erp/mizan/internal/modules/pricing"
	"github.com/mizan-erp/mizan/internal/modules/pricing/domain"
	"github.com/mizan-erp/mizan/internal/modules/profile"
	"github.com/mizan-erp/mizan/internal/modules/tax"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
	"github.com/mizan-erp/mizan/migrations"
)

type fixture struct {
	svc       *pricing.Service
	catalog   *catalog.Service
	partner   *partner.Service
	audit     *audit.Service
	store     *database.Store
	companyID id.ID
	branchID  id.ID
	ctx       context.Context

	product catalogdomain.Product
	variant catalogdomain.Variant
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
		catalog.NewModule(nil).Migrations(),
		partner.NewModule(nil).Migrations(),
		pricing.NewModule(nil).Migrations(),
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

	catalogSvc, err := catalog.NewService(store, catalog.Options{Clock: clock.System(), Bus: bus})
	if err != nil {
		t.Fatalf("catalog.NewService: %v", err)
	}
	if err = catalogSvc.ApplyUnits(ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}
	product, variant, err := catalogSvc.CreateProduct(ctx, catalog.NewProductInput{
		CompanyID: provisioned.CompanyID, Code: "SHIRT", Name: "Shirt", StockUnit: "PCS",
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}

	return fixture{
		svc:     pricing.NewService(store, pricing.Options{Clock: clock.System(), Bus: bus}),
		catalog: catalogSvc,
		partner: partner.NewService(store, partner.Options{Clock: clock.System(), Bus: bus}),
		audit:   auditSvc, store: store,
		companyID: provisioned.CompanyID, branchID: provisioned.BranchID, ctx: ctx,
		product: product, variant: variant,
	}
}

func (f fixture) list(t *testing.T, code string, isDefault bool) domain.List {
	t.Helper()
	created, err := f.svc.CreateList(f.ctx, pricing.NewListInput{
		CompanyID: f.companyID, Code: code, Name: code, CurrencyCode: "SYP",
		Direction: pricing.Sale, IsDefault: isDefault,
	})
	if err != nil {
		t.Fatalf("CreateList(%s): %v", code, err)
	}
	return created
}

func (f fixture) priceVariant(t *testing.T, listCode string, minor, minQuantity int64) {
	t.Helper()
	if _, err := f.svc.SetPrice(f.ctx, pricing.SetPriceInput{
		CompanyID: f.companyID, ListCode: listCode, VariantID: f.variant.ID,
		PriceMinor: minor, MinQuantity: minQuantity,
	}); err != nil {
		t.Fatalf("SetPrice(variant, %s): %v", listCode, err)
	}
}

func (f fixture) priceProduct(t *testing.T, listCode string, minor, minQuantity int64) {
	t.Helper()
	if _, err := f.svc.SetPrice(f.ctx, pricing.SetPriceInput{
		CompanyID: f.companyID, ListCode: listCode, ProductID: f.product.ID,
		PriceMinor: minor, MinQuantity: minQuantity,
	}); err != nil {
		t.Fatalf("SetPrice(product, %s): %v", listCode, err)
	}
}

func (f fixture) query() pricing.PriceQuery {
	return pricing.PriceQuery{
		CompanyID: f.companyID, ProductID: f.product.ID, VariantID: f.variant.ID,
		QuantityMicro: 1_000_000, Direction: pricing.Sale,
	}
}

// ── the single-price shop ───────────────────────────────────────────────────────

// The case that must stay simple. One default list, no partner, no branch — the shop that has
// one price and never wants to hear the words "price list".
func TestAShopWithOneListJustGetsItsPrice(t *testing.T) {
	f := newFixture(t)
	f.list(t, "RETAIL", true)
	f.priceProduct(t, "RETAIL", 1000, 0)

	resolved, err := f.svc.Price(f.ctx, f.query())
	if err != nil {
		t.Fatalf("Price: %v", err)
	}
	if resolved.PriceMinor != 1000 {
		t.Errorf("price = %d, want 1000", resolved.PriceMinor)
	}
	if resolved.CurrencyCode != "SYP" {
		t.Errorf("currency = %q, want SYP", resolved.CurrencyCode)
	}
	if resolved.ListCode != "RETAIL" {
		t.Errorf("list = %q, want RETAIL", resolved.ListCode)
	}
}

// ── the resolution chain, through real tables ───────────────────────────────────

func TestAPartnersListBeatsTheBranchAndTheDefault(t *testing.T) {
	f := newFixture(t)
	f.list(t, "RETAIL", true)
	f.list(t, "SHOP", false)
	f.list(t, "WHOLESALE", false)
	f.priceVariant(t, "RETAIL", 1000, 0)
	f.priceVariant(t, "SHOP", 900, 0)
	f.priceVariant(t, "WHOLESALE", 800, 0)

	created, err := f.partner.CreatePartner(f.ctx, partner.NewPartnerInput{
		CompanyID: f.companyID, Code: "TRADER", Name: "Trader", IsCustomer: true,
	})
	if err != nil {
		t.Fatalf("CreatePartner: %v", err)
	}
	if err = f.svc.AssignToPartner(f.ctx, f.companyID, created.ID, "WHOLESALE"); err != nil {
		t.Fatalf("AssignToPartner: %v", err)
	}
	if err = f.svc.AssignToBranch(f.ctx, f.companyID, f.branchID, "SHOP"); err != nil {
		t.Fatalf("AssignToBranch: %v", err)
	}

	query := f.query()
	query.PartnerID = created.ID
	query.BranchID = f.branchID

	resolved, err := f.svc.Price(f.ctx, query)
	if err != nil {
		t.Fatalf("Price: %v", err)
	}
	if resolved.PriceMinor != 800 || resolved.ListCode != "WHOLESALE" {
		t.Errorf("resolved %d from %q, want 800 from WHOLESALE",
			resolved.PriceMinor, resolved.ListCode)
	}
	// §2.6: "records which list answered".
	if resolved.Source != domain.SourcePartnerVariant {
		t.Errorf("source = %q, want %q", resolved.Source, domain.SourcePartnerVariant)
	}
}

func TestTheBranchListAnswersForAWalkInCustomer(t *testing.T) {
	f := newFixture(t)
	f.list(t, "RETAIL", true)
	f.list(t, "SHOP", false)
	f.priceVariant(t, "RETAIL", 1000, 0)
	f.priceVariant(t, "SHOP", 900, 0)

	if err := f.svc.AssignToBranch(f.ctx, f.companyID, f.branchID, "SHOP"); err != nil {
		t.Fatalf("AssignToBranch: %v", err)
	}

	// No partner: the walk-in customer at the till.
	query := f.query()
	query.BranchID = f.branchID

	resolved, err := f.svc.Price(f.ctx, query)
	if err != nil {
		t.Fatalf("Price: %v", err)
	}
	if resolved.PriceMinor != 900 || resolved.Source != domain.SourceBranchVariant {
		t.Errorf("resolved %d from %q, want 900 from the branch list",
			resolved.PriceMinor, resolved.Source)
	}
}

// The mechanism that replaced §2.6's levels 4 and 5: granularity inside a list rather than
// price columns on tables the catalog module owns.
func TestAVariantPriceOverridesTheProductPriceInTheSameList(t *testing.T) {
	f := newFixture(t)
	f.list(t, "RETAIL", true)
	f.priceProduct(t, "RETAIL", 1000, 0)

	resolved, err := f.svc.Price(f.ctx, f.query())
	if err != nil {
		t.Fatalf("Price: %v", err)
	}
	if resolved.Source != domain.SourceDefaultProduct {
		t.Fatalf("source = %q, want the product-level price", resolved.Source)
	}

	// Now price this one variant specifically — the XXL that costs more.
	f.priceVariant(t, "RETAIL", 1200, 0)

	resolved, err = f.svc.Price(f.ctx, f.query())
	if err != nil {
		t.Fatalf("Price: %v", err)
	}
	if resolved.PriceMinor != 1200 || resolved.Source != domain.SourceDefaultVariant {
		t.Errorf("resolved %d from %q, want 1200 from the variant-level price",
			resolved.PriceMinor, resolved.Source)
	}
}

// List priority outranks granularity: a customer's negotiated blanket price applies even where
// the retail list prices that exact variant, which is why they negotiated it.
func TestAPartnersProductPriceBeatsTheDefaultListsVariantPrice(t *testing.T) {
	f := newFixture(t)
	f.list(t, "RETAIL", true)
	f.list(t, "WHOLESALE", false)
	f.priceVariant(t, "RETAIL", 1000, 0)
	f.priceProduct(t, "WHOLESALE", 700, 0)

	created, err := f.partner.CreatePartner(f.ctx, partner.NewPartnerInput{
		CompanyID: f.companyID, Code: "TRADER", Name: "Trader", IsCustomer: true,
	})
	if err != nil {
		t.Fatalf("CreatePartner: %v", err)
	}
	if err = f.svc.AssignToPartner(f.ctx, f.companyID, created.ID, "WHOLESALE"); err != nil {
		t.Fatalf("AssignToPartner: %v", err)
	}

	query := f.query()
	query.PartnerID = created.ID

	resolved, err := f.svc.Price(f.ctx, query)
	if err != nil {
		t.Fatalf("Price: %v", err)
	}
	if resolved.PriceMinor != 700 || resolved.Source != domain.SourcePartnerProduct {
		t.Errorf("resolved %d from %q, want the partner's blanket 700",
			resolved.PriceMinor, resolved.Source)
	}
}

// ── quantity breaks ─────────────────────────────────────────────────────────────

func TestQuantityBreaksResolveThroughTheRealTables(t *testing.T) {
	f := newFixture(t)
	f.list(t, "RETAIL", true)
	f.priceVariant(t, "RETAIL", 1000, 0)
	f.priceVariant(t, "RETAIL", 900, 10_000_000)
	f.priceVariant(t, "RETAIL", 800, 100_000_000)

	for _, tc := range []struct{ quantity, want int64 }{
		{1_000_000, 1000},
		{10_000_000, 900},
		{150_000_000, 800},
	} {
		query := f.query()
		query.QuantityMicro = tc.quantity

		resolved, err := f.svc.Price(f.ctx, query)
		if err != nil {
			t.Fatalf("Price(%d): %v", tc.quantity, err)
		}
		if resolved.PriceMinor != tc.want {
			t.Errorf("%d units priced at %d, want %d",
				tc.quantity, resolved.PriceMinor, tc.want)
		}
	}
}

// Two prices for the same target at the same break would make the answer depend on row order.
func TestTheSameTargetCannotBePricedTwiceAtOneBreak(t *testing.T) {
	f := newFixture(t)
	f.list(t, "RETAIL", true)
	f.priceVariant(t, "RETAIL", 1000, 0)

	_, err := f.svc.SetPrice(f.ctx, pricing.SetPriceInput{
		CompanyID: f.companyID, ListCode: "RETAIL", VariantID: f.variant.ID,
		PriceMinor: 900, MinQuantity: 0,
	})
	if err == nil {
		t.Fatal("one variant was priced twice at the same quantity break")
	}
}

// ── the refusals ────────────────────────────────────────────────────────────────

// An unpriced variant is a configuration gap, not a free item. Returning zero would sell it for
// nothing on a receipt that looks perfectly ordinary.
func TestAnUnpricedVariantIsRefused(t *testing.T) {
	f := newFixture(t)
	f.list(t, "RETAIL", true)

	_, err := f.svc.Price(f.ctx, f.query())
	if err == nil {
		t.Fatal("an unpriced variant resolved to something")
	}
	if code := errs.CodeOf(err); code != domain.CodeNoPrice {
		t.Errorf("code = %q, want %q", code, domain.CodeNoPrice)
	}
}

// A missing default list is a setup step nobody completed; an unpriced item is a product nobody
// priced. Two different problems needing two different answers.
func TestAMissingDefaultListIsADistinctError(t *testing.T) {
	f := newFixture(t)

	_, err := f.svc.Price(f.ctx, f.query())
	if err == nil {
		t.Fatal("a price resolved with no default list at all")
	}
	if code := errs.CodeOf(err); code != domain.CodeNoDefaultList {
		t.Errorf("code = %q, want %q", code, domain.CodeNoDefaultList)
	}
}

// Resolution's last stop must be unambiguous: two defaults would make the price depend on which
// row the database happened to return.
func TestOnlyOneDefaultListPerDirection(t *testing.T) {
	f := newFixture(t)
	f.list(t, "RETAIL", true)

	_, err := f.svc.CreateList(f.ctx, pricing.NewListInput{
		CompanyID: f.companyID, Code: "OTHER", Name: "Other", CurrencyCode: "SYP",
		Direction: pricing.Sale, IsDefault: true,
	})
	if err == nil {
		t.Fatal("a second default sale list was accepted")
	}

	// A default PURCHASE list is a different direction and perfectly fine.
	if _, err = f.svc.CreateList(f.ctx, pricing.NewListInput{
		CompanyID: f.companyID, Code: "COST", Name: "Supplier costs", CurrencyCode: "SYP",
		Direction: pricing.Purchase, IsDefault: true,
	}); err != nil {
		t.Errorf("a default purchase list was refused: %v", err)
	}
}

// Sale and purchase lists never see each other. Resolving a sale against a purchase list would
// silently sell at cost, and nobody notices until someone reconciles a margin.
func TestSaleAndPurchaseListsAreSeparate(t *testing.T) {
	f := newFixture(t)
	f.list(t, "RETAIL", true)
	f.priceVariant(t, "RETAIL", 1000, 0)

	if _, err := f.svc.CreateList(f.ctx, pricing.NewListInput{
		CompanyID: f.companyID, Code: "COST", Name: "Supplier costs", CurrencyCode: "SYP",
		Direction: pricing.Purchase, IsDefault: true,
	}); err != nil {
		t.Fatalf("CreateList: %v", err)
	}
	if _, err := f.svc.SetPrice(f.ctx, pricing.SetPriceInput{
		CompanyID: f.companyID, ListCode: "COST", VariantID: f.variant.ID, PriceMinor: 600,
	}); err != nil {
		t.Fatalf("SetPrice: %v", err)
	}

	sale := f.query()
	resolvedSale, err := f.svc.Price(f.ctx, sale)
	if err != nil {
		t.Fatalf("Price(sale): %v", err)
	}
	if resolvedSale.PriceMinor != 1000 {
		t.Errorf("sale price = %d, want 1000 — the purchase list answered",
			resolvedSale.PriceMinor)
	}

	purchase := f.query()
	purchase.Direction = pricing.Purchase
	resolvedPurchase, err := f.svc.Price(f.ctx, purchase)
	if err != nil {
		t.Fatalf("Price(purchase): %v", err)
	}
	if resolvedPurchase.PriceMinor != 600 {
		t.Errorf("purchase price = %d, want 600", resolvedPurchase.PriceMinor)
	}
}

// Assigning a retired list would give a partner prices that silently fall through to the
// default, which reads as a pricing bug rather than a stale setting.
func TestARetiredListCannotBeAssigned(t *testing.T) {
	f := newFixture(t)
	f.list(t, "RETAIL", true)
	f.list(t, "OLD", false)

	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
		`UPDATE price_lists SET is_active = 0 WHERE code = 'OLD'`); err != nil {
		t.Fatalf("retiring the list: %v", err)
	}

	created, err := f.partner.CreatePartner(f.ctx, partner.NewPartnerInput{
		CompanyID: f.companyID, Code: "TRADER", Name: "Trader", IsCustomer: true,
	})
	if err != nil {
		t.Fatalf("CreatePartner: %v", err)
	}
	if err = f.svc.AssignToPartner(f.ctx, f.companyID, created.ID, "OLD"); err == nil {
		t.Fatal("a retired price list was assigned")
	}
}

func TestAnUnknownListIsRefusedByName(t *testing.T) {
	f := newFixture(t)

	_, err := f.svc.SetPrice(f.ctx, pricing.SetPriceInput{
		CompanyID: f.companyID, ListCode: "NOWHERE", VariantID: f.variant.ID, PriceMinor: 100,
	})
	if err == nil {
		t.Fatal("a price was set in a list that does not exist")
	}
	typed, _ := errs.AsError(err)
	if typed.Params["code"] != "NOWHERE" {
		t.Errorf("params = %v, want the list code that was not found", typed.Params)
	}
}

// The CHECK is the SCHEMA's, not the service's.
//
// This test exists because the mutation drill for the constraint PASSED: NewItem refuses a price
// targeting both or neither before the CHECK is ever reached, so dropping it changed nothing any
// test could observe. But the domain guard only protects writes that go through the domain, and
// the migration comment claims more — that such a row is unrepresentable. So this writes straight
// to the table, as 3.2's default-variant and 3.3's barcode tests do.
//
// Third time this shape has appeared in Phase 3. The lesson has a name now: when two layers keep
// one rule, each needs a test that can only fail if THAT layer is the one enforcing it.
func TestTheDatabaseRefusesAPriceTargetingBothOrNeither(t *testing.T) {
	f := newFixture(t)
	list := f.list(t, "RETAIL", true)

	const insert = `
		INSERT INTO price_list_items (
			id, price_list_id, product_id, variant_id, price_minor, min_quantity_micro,
			is_active, row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, 100, 0, 1, 1, '2026-01-01', '2026-01-01')`

	// Both: two answers with no rule to choose between them.
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx, insert,
		"both", string(list.ID), string(f.product.ID), string(f.variant.ID)); err == nil {
		t.Error("the database accepted a price targeting a product AND a variant")
	}
	// Neither: a price for nothing.
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx, insert,
		"neither", string(list.ID), nil, nil); err == nil {
		t.Error("the database accepted a price targeting nothing")
	}
	// One is fine.
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx, insert,
		"one", string(list.ID), nil, string(f.variant.ID)); err != nil {
		t.Errorf("a valid variant-level price was refused: %v", err)
	}
}

// ── audit ───────────────────────────────────────────────────────────────────────

func TestPriceChangesAreAudited(t *testing.T) {
	f := newFixture(t)
	f.list(t, "RETAIL", true)
	f.priceVariant(t, "RETAIL", 1000, 0)

	entries, err := f.audit.Entries(f.ctx, audit.Filter{EntityType: pricing.EntityPriceList})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("%d entries, want a list creation and a price: %+v", len(entries), entries)
	}
}

// A refused price records nothing: the trail says what happened, and a rejected request did not.
func TestARefusedPriceIsNotAudited(t *testing.T) {
	f := newFixture(t)
	f.list(t, "RETAIL", true)

	before, err := f.audit.Entries(f.ctx, audit.Filter{EntityType: pricing.EntityPriceList})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}

	if _, err = f.svc.SetPrice(f.ctx, pricing.SetPriceInput{
		CompanyID: f.companyID, ListCode: "RETAIL",
		ProductID: f.product.ID, VariantID: f.variant.ID, PriceMinor: 100,
	}); err == nil {
		t.Fatal("a price targeting both a product and a variant was accepted")
	}

	after, err := f.audit.Entries(f.ctx, audit.Filter{EntityType: pricing.EntityPriceList})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("%d entries after a refused price, want %d", len(after), len(before))
	}
}
