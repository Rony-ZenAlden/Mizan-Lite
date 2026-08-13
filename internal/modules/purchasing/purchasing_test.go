package purchasing_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
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
	"github.com/mizan-erp/mizan/internal/modules/purchasing"
	"github.com/mizan-erp/mizan/internal/modules/sales"
	"github.com/mizan-erp/mizan/internal/modules/tax"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
	"github.com/mizan-erp/mizan/internal/platform/numbering"
	"github.com/mizan-erp/mizan/migrations"
)

type fixture struct {
	svc       *purchasing.Service
	inventory *inventory.Service
	settings  *config.Settings
	sales     *sales.Service
	catalog   *catalog.Service
	audit     *audit.Service
	bus       *eventbus.Bus
	store     *database.Store
	ctx       context.Context

	companyID   id.ID
	branchID    id.ID
	warehouseID id.ID
	partnerID   id.ID
	variant     catalogdomain.Variant
	sackVariant catalogdomain.Variant
}

// ── the ports, faked ────────────────────────────────────────────────────────────

// realCatalog wires the ACTUAL catalog service, so a line's snapshot and its unit conversion
// travel the path production uses — including 3.2's purchase unit, which nothing has read until
// now.
type realCatalog struct{ svc *catalog.Service }

func (c realCatalog) FactsFor(
	ctx context.Context, companyID, variantID, uomID id.ID, quantityMicro int64,
) (purchasing.ProductFacts, error) {
	product, variant, unit, inStock, err := c.svc.PurchaseFacts(
		ctx, companyID, variantID, uomID, quantityMicro)
	if err != nil {
		return purchasing.ProductFacts{}, err
	}
	return purchasing.ProductFacts{
		ProductID: product.ID, ProductName: product.Name, VariantSKU: variant.SKU,
		UomID: unit.ID, UomCode: unit.Code, QuantityStockMicro: inStock,
	}, nil
}

type fixedPricing struct {
	priceMicro int64
	err        error
}

func (p fixedPricing) PurchasePriceFor(
	context.Context, purchasing.PriceQuery,
) (purchasing.ResolvedPrice, error) {
	if p.err != nil {
		return purchasing.ResolvedPrice{}, p.err
	}
	return purchasing.ResolvedPrice{
		UnitPriceMicro: p.priceMicro, ListCode: "SUPPLIER", Source: "default_list_variant",
	}, nil
}

type fixedTax struct{ rateMicro int64 }

func (t fixedTax) TaxFor(
	_ context.Context, q purchasing.TaxQuery,
) (purchasing.ResolvedTax, error) {
	return purchasing.ResolvedTax{
		AmountMinor: q.NetMinor * t.rateMicro / 1_000_000,
		RateMicro:   t.rateMicro, Code: "VAT",
	}, nil
}

// platformNumbering satisfies the Numbering port from the PLATFORM allocator.
//
// The allocator moved out of sales in 6.1 for exactly this: purchasing cannot import sales
// (module-isolation), and a second allocator against the same table would be correct alone and
// race with the first. §9.4 is kept by a transactional read-modify-write on one row, and there
// must be exactly one implementation of it.
type platformNumbering struct{ alloc *numbering.Allocator }

func (n platformNumbering) Allocate(
	ctx context.Context, branchID id.ID, seriesCode string,
) (string, error) {
	return n.alloc.Allocate(ctx, branchID, seriesCode)
}

// fixedScopes answers the scope questions settings resolution asks.
//
// The company scope is what makes the over-receipt tolerance settable per company rather than
// only system-wide, which is the point of the setting: two companies in one install can trade in
// different ways.
type fixedScopes struct{ company, branch id.ID }

func (s fixedScopes) CompanyID(context.Context) (id.ID, bool) { return s.company, true }
func (s fixedScopes) BranchID(context.Context) (id.ID, bool)  { return s.branch, true }
func (s fixedScopes) UserID(context.Context) (id.ID, bool)    { return "", false }

// ── the fixture ─────────────────────────────────────────────────────────────────

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
		identity.NewModule(nil).Migrations(),
		profile.NewModule(nil).Migrations(),
		accounting.NewModule(nil).Migrations(),
		tax.NewModule(nil).Migrations(),
		catalog.NewModule(nil).Migrations(),
		partner.NewModule(nil).Migrations(),
		inventory.NewModule(nil).Migrations(),
		sales.NewModule(nil).Migrations(),
		purchasing.NewModule(nil).Migrations(),
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
		 VALUES (?, 'SAR', 'Saudi Riyal', 'SAR', 2, ?, ?)`,
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
			Code: "MAIN", Name: "Demo", CountryCode: "SA", FunctionalCurrency: "SAR",
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

	// A widget, counted in pieces.
	_, variant, err := catalogSvc.CreateProduct(ctx, catalog.NewProductInput{
		CompanyID: provisioned.CompanyID, Code: "WIDGET", Name: "Widget", StockUnit: "PCS",
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}

	// Flour: stocked and SOLD by the gram, BOUGHT by the kilogram. The product that makes 3.2's
	// purchase unit do work — an order for 2 that defaulted to the sales unit would be 2 grams
	// where the buyer meant 2 kilograms, wrong by a factor of a thousand and in the direction
	// nobody notices until the lorry arrives.
	_, sack, err := catalogSvc.CreateProduct(ctx, catalog.NewProductInput{
		CompanyID: provisioned.CompanyID, Code: "FLOUR", Name: "Flour",
		StockUnit: "G", SalesUnit: "G", PurchaseUnit: "KG",
	})
	if err != nil {
		t.Fatalf("CreateProduct(flour): %v", err)
	}

	salesSvc := sales.NewService(store, sales.Options{Clock: clock.System(), Bus: bus})

	partnerID, _ := id.New()
	if _, err = store.Writer(ctx).ExecContext(ctx, `
		INSERT INTO partners (
			id, company_id, code, name, is_customer, is_supplier, partner_type,
			payment_terms_days, credit_limit_minor, is_active, has_history,
			row_version, created_at, updated_at
		) VALUES (?, ?, 'ACME', 'Acme Supplies', 0, 1, 'company', 30, 0, 1, 0, 1, ?, ?)`,
		string(partnerID), string(provisioned.CompanyID), now, now); err != nil {
		t.Fatalf("creating a supplier: %v", err)
	}

	settings, err := config.Open(ctx, store, config.Options{
		Scopes: fixedScopes{company: provisioned.CompanyID, branch: provisioned.BranchID},
		Clock:  clock.System(),
	})
	if err != nil {
		t.Fatalf("config.Open: %v", err)
	}

	// The settings instance travels on the CONTEXT, which is how `Setting.Get` finds it — the
	// same path bootstrap uses. A fixture that skipped this would silently read every default
	// and prove nothing about a shop that changed one.
	ctx = config.Bind(ctx, settings)

	f := fixture{
		settings: settings,
		sales:    salesSvc, catalog: catalogSvc, audit: auditSvc, bus: bus, store: store, ctx: ctx,
		companyID: provisioned.CompanyID, branchID: provisioned.BranchID,
		warehouseID: provisioned.WarehouseID, partnerID: partnerID,
		variant: variant, sackVariant: sack,
	}
	f.svc = purchasing.NewService(store, purchasing.Options{
		Clock: clock.System(), Bus: bus,
		Catalog: realCatalog{svc: catalogSvc},
		Pricing: fixedPricing{priceMicro: 10_000_000}, // 10.00 each
		Tax:     fixedTax{rateMicro: 150_000},         // 15%
		Numbers: platformNumbering{alloc: numbering.New(store, clock.System())},
	})

	for _, series := range []struct{ code, prefix string }{
		{purchasing.SeriesOrder, "PO-"},
		{purchasing.SeriesReceipt, "GRN-"},
		{purchasing.SeriesBill, "BILL-"},
		{purchasing.SeriesPayment, "PAY-"},
	} {
		if _, err = salesSvc.CreateSeries(ctx, sales.NewSeriesInput{
			CompanyID: provisioned.CompanyID, Code: series.code,
			Prefix: series.prefix, Padding: 6,
		}); err != nil {
			t.Fatalf("CreateSeries(%s): %v", series.code, err)
		}
	}
	return f
}

// allocatorFor builds an allocator over the fixture's store, for tests that rebuild the service.
func allocatorFor(f fixture) *numbering.Allocator {
	return numbering.New(f.store, clock.System())
}

// newStockedFixture adds a real inventory service, so a confirmed delivery really moves goods.
//
// Inventory is REAL rather than faked because the whole point of 6.2 is that a receipt reaches
// the stock ledger — and because `inventory_layers`, written on every receipt since 4.2 and read
// by nothing, gets its first genuine writer here.
func newStockedFixture(t *testing.T) fixture {
	t.Helper()
	f := newFixture(t)

	inventorySvc := inventory.NewService(f.store, inventory.Options{
		Clock: clock.System(), Bus: f.bus,
	})
	f.inventory = inventorySvc

	f.svc = purchasing.NewService(f.store, purchasing.Options{
		Clock: clock.System(), Bus: f.bus,
		Catalog: realCatalog{svc: f.catalog},
		Pricing: fixedPricing{priceMicro: 10_000_000},
		Tax:     fixedTax{rateMicro: 150_000},
		Stock:   realStock{svc: inventorySvc},
		Numbers: platformNumbering{alloc: numbering.New(f.store, clock.System())},
	})
	return f
}

func (f fixture) draftReceipt(t *testing.T, orderID id.ID) id.ID {
	t.Helper()
	receipt, err := f.svc.DraftReceipt(f.ctx, purchasing.NewReceiptInput{
		CompanyID: f.companyID, BranchID: f.branchID, WarehouseID: f.warehouseID,
		OrderID: orderID, PartnerID: f.partnerID, PartnerName: "Acme Supplies",
		ReceiptDate: "2026-08-14", Currency: "SAR",
		DeliveryNoteReference: "DN-99",
	})
	if err != nil {
		t.Fatalf("DraftReceipt: %v", err)
	}
	return receipt.ID
}

func (f fixture) receiveLine(t *testing.T, receiptID, orderLineID id.ID, quantityMicro int64) {
	t.Helper()
	if _, err := f.svc.ReceiveLine(f.ctx, purchasing.ReceiveLineInput{
		CompanyID: f.companyID, ReceiptID: receiptID, OrderLineID: orderLineID,
		QuantityMicro: quantityMicro,
	}); err != nil {
		t.Fatalf("ReceiveLine: %v", err)
	}
}

// receiptOf finds the delivery a line belongs to.
func (f fixture) receiptOf(t *testing.T, receiptLineID id.ID) id.ID {
	t.Helper()
	var receiptID string
	if err := f.store.Reader(f.ctx).QueryRowContext(f.ctx,
		`SELECT receipt_id FROM goods_receipt_lines WHERE id = ?`,
		string(receiptLineID)).Scan(&receiptID); err != nil {
		t.Fatalf("reading a line's delivery: %v", err)
	}
	return id.ID(receiptID)
}

func (f fixture) draft(t *testing.T) id.ID {
	t.Helper()
	order, err := f.svc.Draft(f.ctx, purchasing.NewOrderInput{
		CompanyID: f.companyID, BranchID: f.branchID, WarehouseID: f.warehouseID,
		PartnerID: f.partnerID, PartnerName: "Acme Supplies",
		OrderDate: "2026-08-13", Currency: "SAR",
	})
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}
	return order.ID
}

func (f fixture) addLine(t *testing.T, orderID, variantID id.ID, quantityMicro int64) {
	t.Helper()
	if _, err := f.svc.AddLine(f.ctx, purchasing.AddLineInput{
		CompanyID: f.companyID, OrderID: orderID, VariantID: variantID,
		QuantityMicro: quantityMicro,
	}); err != nil {
		t.Fatalf("AddLine: %v", err)
	}
}
