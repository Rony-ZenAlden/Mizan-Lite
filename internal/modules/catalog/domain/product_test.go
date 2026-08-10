package domain_test

import (
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/catalog/domain"
)

func ids(t *testing.T, n int) []id.ID {
	t.Helper()
	out := make([]id.ID, n)
	for i := range out {
		identifier, err := id.New()
		if err != nil {
			t.Fatalf("id.New: %v", err)
		}
		out[i] = identifier
	}
	return out
}

// ── §A.1: every product has at least one variant ────────────────────────────────

// The decision this whole phase turns on, and the reason it is a CONSTRUCTOR guarantee rather
// than a validator: there is no code path that produces a product without one.
func TestAProductIsBornWithItsDefaultVariant(t *testing.T) {
	identifiers := ids(t, 2)
	kilogram := unit(weight, "KG", 1_000_000_000_000)

	product, variant, err := domain.NewProduct(
		identifiers[0], identifiers[1], "cement", "Bag of cement", kilogram, kilogram, kilogram)
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}

	if variant.ProductID != product.ID {
		t.Error("the default variant does not belong to the product it was born with")
	}
	if !variant.IsDefault {
		t.Error("the variant a product is born with is not its default")
	}
	if !variant.IsActive {
		t.Error("the default variant is inactive")
	}
	// A simple product's variant inherits the product's name, which is what keeps a bag of
	// cement from ever showing the word "variant" anywhere in the interface.
	if variant.Name != "" {
		t.Errorf("the default variant carries its own name %q instead of inheriting", variant.Name)
	}
	if variant.SKU != "CEMENT" {
		t.Errorf("SKU = %q, want the product code", variant.SKU)
	}
	if variant.Combination != "" {
		t.Errorf("combination = %q, want empty for a product with no dimensions", variant.Combination)
	}
}

// The type has no zero-variant state to construct, and the compiler is what enforces it: a
// caller cannot take the product and discard the variant without writing `_`, which is visible
// in review. This test pins the *return arity* so that a later refactor cannot quietly reduce
// it to one value and reintroduce the state §A.1 exists to make unreachable.
func TestNewProductCannotReturnAProductAlone(t *testing.T) {
	identifiers := ids(t, 2)
	kilogram := unit(weight, "KG", 1_000_000_000_000)

	product, variant, err := domain.NewProduct(
		identifiers[0], identifiers[1], "X", "X", kilogram, kilogram, kilogram)
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	if product.ID.IsZero() || variant.ID.IsZero() {
		t.Fatal("construction produced an incomplete pair")
	}
}

func TestAProductNeedsAnIdentityForItsVariantToo(t *testing.T) {
	identifiers := ids(t, 1)
	kilogram := unit(weight, "KG", 1_000_000_000_000)

	// Without a variant id there is nothing to create the variant with, so the product is
	// refused rather than created alone and patched up later.
	if _, _, err := domain.NewProduct(
		identifiers[0], id.ID(""), "X", "X", kilogram, kilogram, kilogram); err == nil {
		t.Fatal("a product was constructed with no identity for its default variant")
	}
}

// ── §B.3: three units, one category ─────────────────────────────────────────────

func TestAProductsThreeUnitsMayDiffer(t *testing.T) {
	identifiers := ids(t, 2)
	gram := unit(weight, "G", 1_000_000_000)
	kilogram := unit(weight, "KG", 1_000_000_000_000)
	tonne := unit(weight, "T", 1_000_000_000_000_000)

	// Buy by the tonne, stock in kilograms, sell by the gram — the thing that makes real trade
	// work, and all three measure weight.
	product, _, err := domain.NewProduct(
		identifiers[0], identifiers[1], "SAND", "Sand", kilogram, gram, tonne)
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	if product.StockUnitID != kilogram.ID ||
		product.SalesUnitID != gram.ID || product.PurchaseUnitID != tonne.ID {
		t.Error("the three units were not kept distinct")
	}
}

// Buying in rolls and stocking in kilograms is not a conversion, it is a mistake with no
// arithmetic that can rescue it — so it is refused at construction, not at the first purchase.
func TestAProductsUnitsMustMeasureTheSameThing(t *testing.T) {
	identifiers := ids(t, 2)
	kilogram := unit(weight, "KG", 1_000_000_000_000)
	metre := unit(length, "M", 1_000_000_000)

	_, _, err := domain.NewProduct(
		identifiers[0], identifiers[1], "CABLE", "Cable", kilogram, metre, kilogram)
	if err == nil {
		t.Fatal("a product was sold in metres and stocked in kilograms")
	}
	if code := errs.CodeOf(err); code != domain.CodeUnitsNotComparable {
		t.Errorf("code = %q, want %q", code, domain.CodeUnitsNotComparable)
	}
	// Both units are named, because "units not comparable" is not actionable and
	// "KG and M measure different things" is.
	typed, _ := errs.AsError(err)
	if typed.Params["stock"] != "KG" || typed.Params["other"] != "M" {
		t.Errorf("params = %v, want both unit codes", typed.Params)
	}
}

// ── §2.2: stock_uom is immutable once anything has moved ────────────────────────

// Changing the unit stock is held in does not convert anything. Every historical quantity was
// recorded as a number of the OLD unit, so the change restates the product's whole history by
// the factor between them — silently, because each number stays individually consistent.
func TestTheStockUnitCannotChangeOnceStockHasMoved(t *testing.T) {
	identifiers := ids(t, 2)
	gram := unit(weight, "G", 1_000_000_000)
	kilogram := unit(weight, "KG", 1_000_000_000_000)

	product, _, err := domain.NewProduct(
		identifiers[0], identifiers[1], "SAND", "Sand", kilogram, kilogram, kilogram)
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}

	// Before anything moves it is an ordinary correction.
	changed, err := product.ChangeStockUnit(gram, kilogram)
	if err != nil {
		t.Fatalf("a stock unit could not be corrected before any movement: %v", err)
	}
	if changed.StockUnitID != gram.ID {
		t.Error("the correction did not take effect")
	}

	// Once stock has moved it is refused, and the refusal is a conflict rather than a
	// validation error: nothing about the request is malformed, the world simply moved on.
	product.HasHistory = true
	if _, err = product.ChangeStockUnit(gram, kilogram); err == nil {
		t.Fatal("the stock unit changed after stock had moved")
	} else if code := errs.CodeOf(err); code != domain.CodeStockUnitLocked {
		t.Errorf("code = %q, want %q", code, domain.CodeStockUnitLocked)
	}
}

func TestTheNewStockUnitMustStillMeasureTheSameThing(t *testing.T) {
	identifiers := ids(t, 2)
	kilogram := unit(weight, "KG", 1_000_000_000_000)
	metre := unit(length, "M", 1_000_000_000)

	product, _, err := domain.NewProduct(
		identifiers[0], identifiers[1], "SAND", "Sand", kilogram, kilogram, kilogram)
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	if _, err = product.ChangeStockUnit(metre, kilogram); err == nil {
		t.Fatal("a product stocked in kilograms was moved to metres")
	}
}

// ── §A.4: a variant with history is deactivated, never deleted ──────────────────

func TestAVariantWithHistoryCannotBeDeleted(t *testing.T) {
	identifiers := ids(t, 2)
	kilogram := unit(weight, "KG", 1_000_000_000_000)
	_, variant, err := domain.NewProduct(
		identifiers[0], identifiers[1], "SHIRT", "Shirt", kilogram, kilogram, kilogram)
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}

	variant.IsDefault = false // a generated variant, not the default
	variant.HasHistory = true

	// Deleting it would break reprinting a two-year-old invoice and every report that groups
	// by it.
	if err = variant.RequireDeletable(); err == nil {
		t.Fatal("a variant that appears on documents was deletable")
	}
	if code := errs.CodeOf(err); code != domain.CodeVariantHasHistory {
		t.Errorf("code = %q, want %q", code, domain.CodeVariantHasHistory)
	}

	// Deactivation is always available, which is what a business actually does to a
	// discontinued line — and it leaves every past document able to reprint.
	retired := variant.Deactivate()
	if retired.IsActive {
		t.Error("a variant could not be retired")
	}
	if !retired.HasHistory {
		t.Error("retiring a variant erased the fact that it has history")
	}
}

// Deleting the default would leave the product with none, which is the state §A.1 exists to make
// unreachable — so it is refused even when the variant has never been used.
func TestTheDefaultVariantCannotBeDeleted(t *testing.T) {
	identifiers := ids(t, 2)
	kilogram := unit(weight, "KG", 1_000_000_000_000)
	_, variant, err := domain.NewProduct(
		identifiers[0], identifiers[1], "CEMENT", "Cement", kilogram, kilogram, kilogram)
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}

	if variant.HasHistory {
		t.Fatal("a brand-new variant claims to have history")
	}
	if err = variant.RequireDeletable(); err == nil {
		t.Fatal("a product's only variant was deletable, leaving it with none")
	}
	if code := errs.CodeOf(err); code != domain.CodeDefaultVariant {
		t.Errorf("code = %q, want %q", code, domain.CodeDefaultVariant)
	}
}

func TestAnUnusedGeneratedVariantCanBeDeleted(t *testing.T) {
	variant := domain.Variant{SKU: "SHIRT-RED-L", IsDefault: false, HasHistory: false}
	if err := variant.RequireDeletable(); err != nil {
		t.Errorf("an unused generated variant was not deletable: %v", err)
	}
}

// ── §A.2: variant-defining versus descriptive ───────────────────────────────────

// Generation is a Cartesian product, which needs a finite set to multiply. "Warranty in months"
// has none, so the refusal names the attribute rather than silently producing zero variants.
func TestOnlyAnEnumerableAttributeCanDefineVariants(t *testing.T) {
	colour := domain.Attribute{Code: "COLOUR", ValueType: domain.ValueTypeList}
	if err := colour.RequireVariantDefinable(); err != nil {
		t.Errorf("a list attribute was refused as a variant dimension: %v", err)
	}

	for _, valueType := range []string{
		domain.ValueTypeText, domain.ValueTypeNumber,
		domain.ValueTypeBoolean, domain.ValueTypeDate,
	} {
		warranty := domain.Attribute{Code: "WARRANTY", ValueType: valueType}
		err := warranty.RequireVariantDefinable()
		if err == nil {
			t.Errorf("a %s attribute was accepted as a variant dimension", valueType)
			continue
		}
		if code := errs.CodeOf(err); code != domain.CodeNotEnumerable {
			t.Errorf("code = %q, want %q", code, domain.CodeNotEnumerable)
		}
	}
}

// ── combinations and SKUs ───────────────────────────────────────────────────────

// The canonical form is SORTED. Without the sort, generation would create a duplicate every time
// the attributes came back from the database in a different order — and nothing guarantees they
// will not.
func TestACombinationIsIndependentOfAttributeOrder(t *testing.T) {
	first := []domain.Assignment{
		{AttributeCode: "COLOUR", ValueCode: "RED"},
		{AttributeCode: "SIZE", ValueCode: "L"},
	}
	second := []domain.Assignment{
		{AttributeCode: "SIZE", ValueCode: "L"},
		{AttributeCode: "COLOUR", ValueCode: "RED"},
	}

	if domain.Combination(first) != domain.Combination(second) {
		t.Errorf("%q and %q are the same variant described in two orders",
			domain.Combination(first), domain.Combination(second))
	}
	if got := domain.Combination(first); got != "COLOUR:RED|SIZE:L" {
		t.Errorf("combination = %q, want COLOUR:RED|SIZE:L", got)
	}
	if got := domain.Combination(nil); got != "" {
		t.Errorf("a product with no dimensions has combination %q, want empty", got)
	}
}

// A SKU that changed between two runs of generation would orphan every label already printed, so
// it is derived deterministically from the same sorted order.
func TestASKUIsDeterministic(t *testing.T) {
	assignments := []domain.Assignment{
		{AttributeCode: "SIZE", ValueCode: "L"},
		{AttributeCode: "COLOUR", ValueCode: "RED"},
	}
	got := domain.SKUFor("shirt", assignments)
	if got != "SHIRT-RED-L" {
		t.Errorf("SKU = %q, want SHIRT-RED-L", got)
	}
	if again := domain.SKUFor("shirt", assignments); again != got {
		t.Errorf("the same input produced %q then %q", got, again)
	}
	// A simple product's SKU is just its code.
	if bare := domain.SKUFor("CEMENT", nil); bare != "CEMENT" {
		t.Errorf("SKU = %q, want CEMENT", bare)
	}
}

// ── the category hierarchy ──────────────────────────────────────────────────────

func TestACategoryPathIsRootFirst(t *testing.T) {
	identifiers := ids(t, 3)

	food, err := domain.NewCategory(identifiers[0], "food", "Food", nil)
	if err != nil {
		t.Fatalf("NewCategory: %v", err)
	}
	if food.Path != "/FOOD/" || food.Depth != 0 {
		t.Errorf("root path = %q depth %d, want /FOOD/ and 0", food.Path, food.Depth)
	}

	dairy, err := domain.NewCategory(identifiers[1], "dairy", "Dairy", &food)
	if err != nil {
		t.Fatalf("NewCategory: %v", err)
	}
	if dairy.Path != "/FOOD/DAIRY/" || dairy.Depth != 1 {
		t.Errorf("child path = %q depth %d, want /FOOD/DAIRY/ and 1", dairy.Path, dairy.Depth)
	}
	if dairy.ParentID != food.ID {
		t.Error("the child was not attached to its parent")
	}

	// The whole point: "everything under Food" is a prefix test.
	if !strings.HasPrefix(dairy.Path, food.Path) {
		t.Errorf("%q is not beneath %q", dairy.Path, food.Path)
	}
}

// A cycle would make the path infinite and every subtree query non-terminating.
func TestACategoryCannotBeItsOwnAncestor(t *testing.T) {
	identifiers := ids(t, 2)

	food, err := domain.NewCategory(identifiers[0], "FOOD", "Food", nil)
	if err != nil {
		t.Fatalf("NewCategory: %v", err)
	}
	dairy, err := domain.NewCategory(identifiers[1], "DAIRY", "Dairy", &food)
	if err != nil {
		t.Fatalf("NewCategory: %v", err)
	}

	// Re-creating FOOD beneath DAIRY closes the loop.
	if _, err = domain.NewCategory(identifiers[0], "FOOD", "Food", &dairy); err == nil {
		t.Fatal("a category was placed beneath its own descendant")
	}
	// And the direct case.
	if _, err = domain.NewCategory(identifiers[0], "FOOD", "Food", &food); err == nil {
		t.Fatal("a category was made its own parent")
	}
}
