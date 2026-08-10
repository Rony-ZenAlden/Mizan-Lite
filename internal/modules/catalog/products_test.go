package catalog_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/modules/audit"
	"github.com/mizan-erp/mizan/internal/modules/catalog"
	"github.com/mizan-erp/mizan/internal/modules/catalog/domain"
)

// seeded builds a fixture with the standard units already applied, which is what every product
// test needs before it can name a unit.
func seeded(t *testing.T) fixture {
	t.Helper()
	f := newFixture(t, catalog.Options{})
	if err := f.svc.ApplyUnits(f.ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}
	return f
}

func (f fixture) product(t *testing.T, code, name, unit string) domain.Product {
	t.Helper()
	product, _, err := f.svc.CreateProduct(f.ctx, catalog.NewProductInput{
		CompanyID: f.companyID, Code: code, Name: name, StockUnit: unit,
	})
	if err != nil {
		t.Fatalf("CreateProduct(%s): %v", code, err)
	}
	return product
}

// ── §A.1: the default variant reaches storage ───────────────────────────────────

// The domain guarantees the pair by construction; this guarantees it survives the write. A
// product row with no variant row is exactly the state §A.1 exists to make unreachable, and it
// would only ever appear here — between two INSERTs.
func TestCreatingAProductCreatesItsDefaultVariant(t *testing.T) {
	f := seeded(t)

	product, variant, err := f.svc.CreateProduct(f.ctx, catalog.NewProductInput{
		CompanyID: f.companyID, Code: "cement", Name: "Bag of cement", StockUnit: "PCS",
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}

	stored, err := f.svc.Variants(f.ctx, product.ID)
	if err != nil {
		t.Fatalf("Variants: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("%d variants stored, want exactly 1", len(stored))
	}
	if !stored[0].IsDefault {
		t.Error("the stored variant is not marked as the default")
	}
	if stored[0].ID != variant.ID {
		t.Error("the stored variant is not the one construction returned")
	}
	if stored[0].SKU != "CEMENT" {
		t.Errorf("SKU = %q, want CEMENT", stored[0].SKU)
	}
}

// A product that fails on its way to storage must leave NOTHING behind — not a product without
// its variant, and not a variant without its product. One transaction, both rows.
func TestAFailedProductLeavesNothingBehind(t *testing.T) {
	f := seeded(t)
	f.product(t, "SHIRT", "Shirt", "PCS")

	// The same code again. The duplicate is refused, and the point of the test is what the
	// database looks like afterwards.
	if _, _, err := f.svc.CreateProduct(f.ctx, catalog.NewProductInput{
		CompanyID: f.companyID, Code: "SHIRT", Name: "Another shirt", StockUnit: "KG",
	}); err == nil {
		t.Fatal("a duplicate product code was accepted")
	}

	products, err := f.svc.Products(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Products: %v", err)
	}
	if len(products) != 1 {
		t.Fatalf("%d products, want the original only", len(products))
	}

	// And no orphan variant from the rejected attempt.
	var orphans int
	if err = f.store.Reader(f.ctx).QueryRowContext(f.ctx,
		`SELECT COUNT(*) FROM product_variants v
		 WHERE NOT EXISTS (SELECT 1 FROM products p WHERE p.id = v.product_id)`).
		Scan(&orphans); err != nil {
		t.Fatalf("counting orphans: %v", err)
	}
	if orphans != 0 {
		t.Errorf("%d variants have no product", orphans)
	}
}

// Every product in storage has at least one variant. Asserted as a property over the whole
// table rather than per-product, because the failure this guards is a path nobody thought of.
func TestNoProductIsEverWithoutAVariant(t *testing.T) {
	f := seeded(t)
	f.product(t, "CEMENT", "Cement", "PCS")
	f.product(t, "SAND", "Sand", "KG")
	f.product(t, "CABLE", "Cable", "M")

	var without int
	if err := f.store.Reader(f.ctx).QueryRowContext(f.ctx,
		`SELECT COUNT(*) FROM products p
		 WHERE NOT EXISTS (SELECT 1 FROM product_variants v WHERE v.product_id = p.id)`).
		Scan(&without); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if without != 0 {
		t.Errorf("%d products have no variant", without)
	}
}

// Exactly one default per product, kept by the database rather than only by the code that
// writes it — a partial unique index survives an import the application never saw.
func TestAProductCannotHaveTwoDefaultVariants(t *testing.T) {
	f := seeded(t)
	product := f.product(t, "SHIRT", "Shirt", "PCS")

	_, err := f.store.Writer(f.ctx).ExecContext(f.ctx, `
		INSERT INTO product_variants (
			id, product_id, sku, name, is_default, combination, has_history, is_active,
			row_version, created_at, updated_at
		) VALUES ('second', ?, 'SHIRT-2', NULL, 1, 'X', 0, 1, 1, '2026-01-01', '2026-01-01')`,
		string(product.ID))
	if err == nil {
		t.Fatal("a second default variant was accepted")
	}
}

// ── the three units ─────────────────────────────────────────────────────────────

func TestAProductCanBeBoughtStockedAndSoldInDifferentUnits(t *testing.T) {
	f := seeded(t)

	product, _, err := f.svc.CreateProduct(f.ctx, catalog.NewProductInput{
		CompanyID: f.companyID, Code: "SAND", Name: "Sand",
		StockUnit: "KG", SalesUnit: "G", PurchaseUnit: "T",
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}

	stored, err := f.svc.ProductByCode(f.ctx, f.companyID, "SAND")
	if err != nil {
		t.Fatalf("ProductByCode: %v", err)
	}
	if stored.StockUnitID == stored.SalesUnitID || stored.StockUnitID == stored.PurchaseUnitID {
		t.Error("the three units collapsed into one on the way to storage")
	}
	if stored.StockUnitID != product.StockUnitID {
		t.Error("the stored stock unit is not the one that was created")
	}
}

// Omitting sales and purchase units is the ordinary case — a business that measures one way
// throughout should never have to say so three times.
func TestTheSalesAndPurchaseUnitsDefaultToTheStockUnit(t *testing.T) {
	f := seeded(t)
	product := f.product(t, "CEMENT", "Cement", "PCS")

	if product.SalesUnitID != product.StockUnitID ||
		product.PurchaseUnitID != product.StockUnitID {
		t.Error("an unspecified sales or purchase unit did not default to the stock unit")
	}
}

func TestAProductCannotBeStockedAndSoldInDifferentCategories(t *testing.T) {
	f := seeded(t)

	_, _, err := f.svc.CreateProduct(f.ctx, catalog.NewProductInput{
		CompanyID: f.companyID, Code: "CABLE", Name: "Cable",
		StockUnit: "KG", SalesUnit: "M",
	})
	if err == nil {
		t.Fatal("a product stocked in kilograms was sold in metres")
	}
	if code := errs.CodeOf(err); code != domain.CodeUnitsNotComparable {
		t.Errorf("code = %q, want %q", code, domain.CodeUnitsNotComparable)
	}
}

func TestAnUnknownUnitIsRefusedByName(t *testing.T) {
	f := seeded(t)

	_, _, err := f.svc.CreateProduct(f.ctx, catalog.NewProductInput{
		CompanyID: f.companyID, Code: "X", Name: "X", StockUnit: "FURLONG",
	})
	if err == nil {
		t.Fatal("a product was created with a unit that does not exist")
	}
	typed, _ := errs.AsError(err)
	if typed.Params["code"] != "FURLONG" {
		t.Errorf("params = %v, want the unit code that was not found", typed.Params)
	}
}

// ── the stock unit after movement ───────────────────────────────────────────────

func TestTheStockUnitIsLockedOnceStockHasMoved(t *testing.T) {
	f := seeded(t)
	product := f.product(t, "SAND", "Sand", "KG")

	// Before anything moves, correcting it is ordinary.
	if err := f.svc.ChangeStockUnit(f.ctx, f.companyID, "SAND", "G"); err != nil {
		t.Fatalf("correcting a stock unit before any movement: %v", err)
	}
	corrected, err := f.svc.ProductByCode(f.ctx, f.companyID, "SAND")
	if err != nil {
		t.Fatalf("ProductByCode: %v", err)
	}
	if corrected.StockUnitID == product.StockUnitID {
		t.Fatal("the correction did not reach storage")
	}

	// Phase 4 will call this the first time anything moves.
	variants, err := f.svc.Variants(f.ctx, product.ID)
	if err != nil {
		t.Fatalf("Variants: %v", err)
	}
	if err = f.svc.MarkVariantHistory(f.ctx, variants[0].ID); err != nil {
		t.Fatalf("MarkVariantHistory: %v", err)
	}

	// Now it is refused, because the change would restate every historical quantity by the
	// factor between the two units without touching a single number.
	err = f.svc.ChangeStockUnit(f.ctx, f.companyID, "SAND", "KG")
	if err == nil {
		t.Fatal("the stock unit changed after stock had moved")
	}
	if code := errs.CodeOf(err); code != domain.CodeStockUnitLocked {
		t.Errorf("code = %q, want %q", code, domain.CodeStockUnitLocked)
	}

	// And the refusal left storage alone.
	after, err := f.svc.ProductByCode(f.ctx, f.companyID, "SAND")
	if err != nil {
		t.Fatalf("ProductByCode: %v", err)
	}
	if after.StockUnitID != corrected.StockUnitID {
		t.Error("a refused change still altered the stored stock unit")
	}
}

// ── categories ──────────────────────────────────────────────────────────────────

func TestCategoriesFormATreeOrderedByPath(t *testing.T) {
	f := seeded(t)

	for _, in := range []catalog.NewCategoryInput{
		{CompanyID: f.companyID, Code: "FOOD", Name: "Food"},
		{CompanyID: f.companyID, Code: "DAIRY", Name: "Dairy", ParentCode: "FOOD"},
		{CompanyID: f.companyID, Code: "CHEESE", Name: "Cheese", ParentCode: "DAIRY"},
		{CompanyID: f.companyID, Code: "TOOLS", Name: "Tools"},
	} {
		if _, err := f.svc.CreateCategory(f.ctx, in); err != nil {
			t.Fatalf("CreateCategory(%s): %v", in.Code, err)
		}
	}

	categories, err := f.svc.Categories(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}

	byCode := map[string]domain.Category{}
	for _, c := range categories {
		byCode[c.Code] = c
	}
	if byCode["CHEESE"].Path != "/FOOD/DAIRY/CHEESE/" {
		t.Errorf("path = %q, want /FOOD/DAIRY/CHEESE/", byCode["CHEESE"].Path)
	}
	if byCode["CHEESE"].Depth != 2 {
		t.Errorf("depth = %d, want 2", byCode["CHEESE"].Depth)
	}

	// The listing arrives in tree order from a flat ORDER BY, which is the whole reason the
	// path is materialised.
	want := []string{"FOOD", "DAIRY", "CHEESE", "TOOLS"}
	for i, code := range want {
		if categories[i].Code != code {
			t.Errorf("position %d is %s, want %s", i, categories[i].Code, code)
		}
	}
}

func TestAProductJoinsItsCategoryByCode(t *testing.T) {
	f := seeded(t)
	category, err := f.svc.CreateCategory(f.ctx, catalog.NewCategoryInput{
		CompanyID: f.companyID, Code: "BUILD", Name: "Building materials",
	})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	_, _, err = f.svc.CreateProduct(f.ctx, catalog.NewProductInput{
		CompanyID: f.companyID, Code: "CEMENT", Name: "Cement",
		StockUnit: "PCS", CategoryCode: "build",
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}

	stored, err := f.svc.ProductByCode(f.ctx, f.companyID, "CEMENT")
	if err != nil {
		t.Fatalf("ProductByCode: %v", err)
	}
	if stored.CategoryID != category.ID {
		t.Error("the product was not filed under its category")
	}
}

func TestAnUnknownCategoryIsRefused(t *testing.T) {
	f := seeded(t)

	_, _, err := f.svc.CreateProduct(f.ctx, catalog.NewProductInput{
		CompanyID: f.companyID, Code: "X", Name: "X",
		StockUnit: "PCS", CategoryCode: "NOWHERE",
	})
	if err == nil {
		t.Fatal("a product was filed under a category that does not exist")
	}
	if code := errs.CodeOf(err); code != catalog.CodeUnknownCategory {
		t.Errorf("code = %q, want %q", code, catalog.CodeUnknownCategory)
	}
}

// ── §A.2: the same attribute means different things to different products ───────

// The flag lives on the LINK, and this is the test that proves it: material defines variants for
// a sofa and describes a screwdriver, from one dictionary entry.
func TestAnAttributeIsVariantDefiningForOneProductAndDescriptiveForAnother(t *testing.T) {
	f := seeded(t)

	if _, err := f.svc.CreateAttribute(f.ctx, catalog.NewAttributeInput{
		CompanyID: f.companyID, Code: "MATERIAL", Name: "Material",
		Values: []catalog.AttributeValueInput{
			{Code: "OAK", Name: "Oak"}, {Code: "STEEL", Name: "Steel"},
		},
	}); err != nil {
		t.Fatalf("CreateAttribute: %v", err)
	}

	sofa := f.product(t, "SOFA", "Sofa", "PCS")
	screwdriver := f.product(t, "SCREWDRIVER", "Screwdriver", "PCS")

	if err := f.svc.LinkAttribute(f.ctx, catalog.LinkAttributeInput{
		CompanyID: f.companyID, ProductCode: "SOFA", AttributeCode: "MATERIAL",
		VariantDefining: true,
	}); err != nil {
		t.Fatalf("linking to the sofa: %v", err)
	}
	if err := f.svc.LinkAttribute(f.ctx, catalog.LinkAttributeInput{
		CompanyID: f.companyID, ProductCode: "SCREWDRIVER", AttributeCode: "MATERIAL",
		VariantDefining: false,
	}); err != nil {
		t.Fatalf("linking to the screwdriver: %v", err)
	}

	sofaAttrs, err := f.svc.ProductAttributes(f.ctx, sofa.ID)
	if err != nil {
		t.Fatalf("ProductAttributes: %v", err)
	}
	if len(sofaAttrs) != 1 || !sofaAttrs[0].VariantDefining {
		t.Fatalf("the sofa's material is not variant-defining: %+v", sofaAttrs)
	}

	toolAttrs, err := f.svc.ProductAttributes(f.ctx, screwdriver.ID)
	if err != nil {
		t.Fatalf("ProductAttributes: %v", err)
	}
	if len(toolAttrs) != 1 || toolAttrs[0].VariantDefining {
		t.Fatalf("the screwdriver's material is variant-defining: %+v", toolAttrs)
	}

	// One dictionary entry, two meanings.
	if sofaAttrs[0].Attribute.ID != toolAttrs[0].Attribute.ID {
		t.Error("the two products are using different attributes, not one shared definition")
	}
}

// A product offers three of the dictionary's twenty colours. Without this the Cartesian product
// in 3.3 would use all twenty.
func TestAProductOffersOnlyTheValuesItNames(t *testing.T) {
	f := seeded(t)

	if _, err := f.svc.CreateAttribute(f.ctx, catalog.NewAttributeInput{
		CompanyID: f.companyID, Code: "COLOUR", Name: "Colour",
		Values: []catalog.AttributeValueInput{
			{Code: "RED", Name: "Red"}, {Code: "BLUE", Name: "Blue"},
			{Code: "GREEN", Name: "Green"}, {Code: "BLACK", Name: "Black"},
		},
	}); err != nil {
		t.Fatalf("CreateAttribute: %v", err)
	}

	shirt := f.product(t, "SHIRT", "Shirt", "PCS")
	if err := f.svc.LinkAttribute(f.ctx, catalog.LinkAttributeInput{
		CompanyID: f.companyID, ProductCode: "SHIRT", AttributeCode: "COLOUR",
		VariantDefining: true, ValueCodes: []string{"red", "blue"},
	}); err != nil {
		t.Fatalf("LinkAttribute: %v", err)
	}

	attrs, err := f.svc.ProductAttributes(f.ctx, shirt.ID)
	if err != nil {
		t.Fatalf("ProductAttributes: %v", err)
	}
	if len(attrs) != 1 {
		t.Fatalf("%d attributes, want 1", len(attrs))
	}
	offered := map[string]bool{}
	for _, v := range attrs[0].Values {
		offered[v.Code] = true
	}
	if len(offered) != 2 || !offered["RED"] || !offered["BLUE"] {
		t.Errorf("offered = %v, want RED and BLUE only", offered)
	}
}

// Naming no values means all of them, which is what a product using the whole dictionary wants.
func TestAProductNamingNoValuesOffersThemAll(t *testing.T) {
	f := seeded(t)

	if _, err := f.svc.CreateAttribute(f.ctx, catalog.NewAttributeInput{
		CompanyID: f.companyID, Code: "SIZE", Name: "Size",
		Values: []catalog.AttributeValueInput{
			{Code: "S", Name: "Small"}, {Code: "M", Name: "Medium"}, {Code: "L", Name: "Large"},
		},
	}); err != nil {
		t.Fatalf("CreateAttribute: %v", err)
	}

	shirt := f.product(t, "SHIRT", "Shirt", "PCS")
	if err := f.svc.LinkAttribute(f.ctx, catalog.LinkAttributeInput{
		CompanyID: f.companyID, ProductCode: "SHIRT", AttributeCode: "SIZE",
		VariantDefining: true,
	}); err != nil {
		t.Fatalf("LinkAttribute: %v", err)
	}

	attrs, err := f.svc.ProductAttributes(f.ctx, shirt.ID)
	if err != nil {
		t.Fatalf("ProductAttributes: %v", err)
	}
	if len(attrs[0].Values) != 3 {
		t.Errorf("%d values offered, want all 3", len(attrs[0].Values))
	}
}

// Generation is a Cartesian product and needs a finite set to multiply. The refusal names the
// attribute rather than silently producing no variants.
func TestANonEnumerableAttributeCannotDefineVariants(t *testing.T) {
	f := seeded(t)

	if _, err := f.svc.CreateAttribute(f.ctx, catalog.NewAttributeInput{
		CompanyID: f.companyID, Code: "WARRANTY", Name: "Warranty months",
		ValueType: domain.ValueTypeNumber,
	}); err != nil {
		t.Fatalf("CreateAttribute: %v", err)
	}
	f.product(t, "DRILL", "Drill", "PCS")

	err := f.svc.LinkAttribute(f.ctx, catalog.LinkAttributeInput{
		CompanyID: f.companyID, ProductCode: "DRILL", AttributeCode: "WARRANTY",
		VariantDefining: true,
	})
	if err == nil {
		t.Fatal("a numeric attribute was made a variant dimension")
	}
	if code := errs.CodeOf(err); code != domain.CodeNotEnumerable {
		t.Errorf("code = %q, want %q", code, domain.CodeNotEnumerable)
	}

	// As a specification it is perfectly fine, which is the other half of §A.2.
	if err = f.svc.LinkAttribute(f.ctx, catalog.LinkAttributeInput{
		CompanyID: f.companyID, ProductCode: "DRILL", AttributeCode: "WARRANTY",
		VariantDefining: false,
	}); err != nil {
		t.Errorf("a numeric attribute was refused as a specification: %v", err)
	}
}

// ── audit ───────────────────────────────────────────────────────────────────────

func TestEveryCatalogChangeIsAudited(t *testing.T) {
	f := seeded(t)

	if _, err := f.svc.CreateCategory(f.ctx, catalog.NewCategoryInput{
		CompanyID: f.companyID, Code: "FOOD", Name: "Food",
	}); err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	f.product(t, "CEMENT", "Cement", "PCS")

	for _, want := range []struct {
		entity string
		action string
	}{
		{catalog.EntityCategory, catalog.ActionCategoryCreated},
		{catalog.EntityProduct, catalog.ActionProductCreated},
	} {
		entries, err := f.audit.Entries(f.ctx, audit.Filter{EntityType: want.entity})
		if err != nil {
			t.Fatalf("Entries: %v", err)
		}
		if len(entries) != 1 || entries[0].Action != want.action {
			t.Errorf("%s: entries = %+v, want one %s", want.entity, entries, want.action)
		}
	}
}

// A refused create records nothing. The audit trail says what happened, and a rejected request
// did not happen — the subscriber runs inside the same transaction, so the rollback takes the
// entry with it.
func TestARefusedCreateIsNotAudited(t *testing.T) {
	f := seeded(t)

	if _, _, err := f.svc.CreateProduct(f.ctx, catalog.NewProductInput{
		CompanyID: f.companyID, Code: "CABLE", Name: "Cable",
		StockUnit: "KG", SalesUnit: "M",
	}); err == nil {
		t.Fatal("the invalid product was accepted")
	}

	entries, err := f.audit.Entries(f.ctx, audit.Filter{EntityType: catalog.EntityProduct})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("%d entries for a product that was never created", len(entries))
	}
}
