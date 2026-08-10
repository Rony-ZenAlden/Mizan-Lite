package domain

import (
	"sort"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Stable error codes for products, categories, and variants.
const (
	CodeInvalidProduct     = "catalog.invalid_product"
	CodeInvalidCategory    = "catalog.invalid_category"
	CodeInvalidVariant     = "catalog.invalid_variant"
	CodeUnitsNotComparable = "catalog.units_not_comparable"
	CodeStockUnitLocked    = "catalog.stock_unit_locked"
	CodeVariantHasHistory  = "catalog.variant_has_history"
	CodeDefaultVariant     = "catalog.default_variant_required"
	CodeNotEnumerable      = "catalog.attribute_not_enumerable"
)

// ProductType separates what is stocked from what is not.
type ProductType string

// The product types.
const (
	TypeGoods   ProductType = "goods"
	TypeService ProductType = "service"
)

// Tracking is how finely stock is followed.
type Tracking string

// The tracking modes. Lot and serial are declared now and honoured in Phase 4, so that turning
// one on is a data change rather than a migration.
const (
	TrackNone     Tracking = "none"
	TrackQuantity Tracking = "quantity"
	TrackLot      Tracking = "lot"
	TrackSerial   Tracking = "serial"
)

// Category is a node in the product hierarchy.
//
// The same materialised-path shape the chart of accounts uses, and maintained the same way: the
// domain computes `Path` on insert and on re-parent, so "everything under Beverages" is an
// indexed prefix scan rather than a recursive query.
type Category struct {
	ID       id.ID
	Code     string
	Name     string
	NameKey  string
	ParentID id.ID
	Path     string
	Depth    int

	IncomeAccountID    id.ID
	ExpenseAccountID   id.ID
	InventoryAccountID id.ID

	SortOrder int
	IsActive  bool
}

// NewCategory builds a category beneath an optional parent.
func NewCategory(identifier id.ID, code, name string, parent *Category) (Category, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	name = strings.TrimSpace(name)

	if identifier.IsZero() {
		return Category{}, errs.Validation(CodeInvalidCategory, "a category needs an identity")
	}
	if code == "" {
		return Category{}, errs.Validation(CodeInvalidCategory, "a category needs a code").
			WithField("code", CodeInvalidCategory, "required")
	}
	if name == "" {
		return Category{}, errs.Validation(CodeInvalidCategory, "a category needs a name").
			WithField("name", CodeInvalidCategory, "required")
	}

	built := Category{
		ID: identifier, Code: code, Name: name,
		Path: "/" + code + "/", IsActive: true,
	}
	if parent != nil {
		if parent.ID == identifier {
			return Category{}, errs.Validation(CodeInvalidCategory,
				"a category cannot be its own parent").WithParam("code", code)
		}
		// A cycle would make the path infinite and every subtree query non-terminating. The
		// prefix test catches it here rather than after the write.
		if strings.Contains(parent.Path, "/"+code+"/") {
			return Category{}, errs.Validation(CodeInvalidCategory,
				"that parent is already beneath this category").WithParam("code", code)
		}
		built.ParentID = parent.ID
		built.Path = parent.Path + code + "/"
		built.Depth = parent.Depth + 1
	}
	return built, nil
}

// AdoptCategory rebuilds a category from storage without re-running construction rules.
func AdoptCategory(c Category) Category { return c }

// Product is something a business trades.
//
// A product is never constructed alone: NewProduct returns it together with its default variant,
// because §A.1 says every product has at least one and a constructor that can return a product
// without one makes that a rule someone must remember rather than a fact of the type.
type Product struct {
	ID         id.ID
	Code       string
	Name       string
	NameKey    string
	CategoryID id.ID
	Type       ProductType

	// The three units (§B.3). All in one category — checked at construction and again whenever
	// one changes.
	StockUnitID    id.ID
	SalesUnitID    id.ID
	PurchaseUnitID id.ID

	Tracking   Tracking
	TaxGroupID id.ID

	IncomeAccountID    id.ID
	ExpenseAccountID   id.ID
	InventoryAccountID id.ID

	// HasHistory is set by Phase 4 the moment anything moves. The domain only reads it.
	HasHistory  bool
	IsActive    bool
	IsSold      bool
	IsPurchased bool
}

// Variant is the row every downstream table points at.
type Variant struct {
	ID          id.ID
	ProductID   id.ID
	SKU         string
	Name        string
	IsDefault   bool
	Combination string
	HasHistory  bool
	IsActive    bool
}

// NewProduct builds a product AND its default variant.
//
// # Why they are returned together
//
// §A.1 is the decision this phase turns on: every product has at least one variant. A
// constructor able to hand back a product on its own would make that a rule enforced by whoever
// remembers to call a second function — and the failure mode, a product with no variant, is
// invisible until a sale, a stock count, or a price lookup finds nothing to point at.
//
// So the invariant is a property of construction, exactly as a balanced journal entry is
// (Phase 2): there is no code path that produces one without the other.
//
// # The three units must be comparable
//
// Buying in rolls, stocking in metres and selling in metres is the point of three units. Buying
// in rolls and stocking in kilograms is not a conversion; it is a mistake with no arithmetic
// that can rescue it, so it is refused here rather than at the first purchase.
func NewProduct(
	identifier, variantID id.ID, code, name string, stock, sales, purchase Unit,
) (Product, Variant, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	name = strings.TrimSpace(name)

	if identifier.IsZero() || variantID.IsZero() {
		return Product{}, Variant{}, errs.Validation(CodeInvalidProduct,
			"a product needs an identity and one for its default variant")
	}
	if code == "" {
		return Product{}, Variant{}, errs.Validation(CodeInvalidProduct,
			"a product needs a code").WithField("code", CodeInvalidProduct, "required")
	}
	if name == "" {
		return Product{}, Variant{}, errs.Validation(CodeInvalidProduct,
			"a product needs a name").WithField("name", CodeInvalidProduct, "required")
	}
	if err := ValidateProductUnits(stock, sales, purchase); err != nil {
		return Product{}, Variant{}, err
	}

	product := Product{
		ID: identifier, Code: code, Name: name, Type: TypeGoods,
		StockUnitID: stock.ID, SalesUnitID: sales.ID, PurchaseUnitID: purchase.ID,
		Tracking: TrackQuantity,
		IsActive: true, IsSold: true, IsPurchased: true,
	}
	// The default variant. Its SKU is the product code until real variants arrive and each
	// gains its own suffix; its name is empty, so it inherits and a simple product never shows
	// a variant anywhere in the interface.
	variant := Variant{
		ID: variantID, ProductID: identifier, SKU: code,
		IsDefault: true, Combination: "", IsActive: true,
	}
	return product, variant, nil
}

// AdoptProduct rebuilds a product from storage without re-running construction rules.
//
// Rows already in the database were valid when written; re-validating them on every read would
// make a rule added in a later version retroactively unable to load old data.
func AdoptProduct(p Product) Product { return p }

// AdoptVariant rebuilds a variant from storage.
func AdoptVariant(v Variant) Variant { return v }

// ValidateProductUnits checks that a product's three units are comparable.
func ValidateProductUnits(stock, sales, purchase Unit) error {
	if stock.ID.IsZero() {
		return errs.Validation(CodeInvalidProduct, "a product needs a stock unit").
			WithField("stock_uom", CodeInvalidProduct, "required")
	}
	for _, other := range []Unit{sales, purchase} {
		if other.ID.IsZero() {
			return errs.Validation(CodeInvalidProduct,
				"a product needs a sales unit and a purchase unit")
		}
		if other.CategoryID != stock.CategoryID {
			return errs.Validation(CodeUnitsNotComparable,
				"a product's units must all measure the same thing").
				WithParam("stock", stock.Code).WithParam("other", other.Code)
		}
	}
	return nil
}

// ChangeStockUnit changes the unit stock is held in, or refuses.
//
// # Immutable once anything has moved (§2.2)
//
// Changing the unit stock is held in does not convert anything. Every historical quantity was
// recorded as a number of the OLD unit, and every cost was derived from one, so a change after
// the fact silently restates the entire history of the product by whatever factor separates the
// two units — a 1000× error in a stock valuation that no report flags because every number is
// individually consistent.
//
// Phase 4 sets HasHistory; this reads it. The rule is written now, against the flag, rather than
// left for the phase that makes it reachable — which is the same reason identity declared its
// Organisation port before org existed (1.2).
func (p Product) ChangeStockUnit(unit Unit, current Unit) (Product, error) {
	if p.HasHistory {
		return p, errs.Conflict(CodeStockUnitLocked,
			"this product's stock unit cannot change because stock has already moved").
			WithParam("code", p.Code)
	}
	if unit.ID.IsZero() {
		return p, errs.Validation(CodeInvalidProduct, "a product needs a stock unit")
	}
	if unit.CategoryID != current.CategoryID {
		return p, errs.Validation(CodeUnitsNotComparable,
			"the new stock unit measures something else").
			WithParam("stock", current.Code).WithParam("other", unit.Code)
	}
	p.StockUnitID = unit.ID
	return p, nil
}

// Deactivate retires a variant.
//
// Always available, including for a variant with history — which is the point. Retiring is what
// a business does to a discontinued line, and it leaves every past document able to reprint.
func (v Variant) Deactivate() Variant {
	v.IsActive = false
	return v
}

// RequireDeletable refuses to delete a variant that anything refers to (§A.4).
//
// Deleting a variant that appears on a two-year-old invoice breaks document reprinting and every
// historical report that groups by it. Deactivation is the answer, and the error says so.
func (v Variant) RequireDeletable() error {
	if v.HasHistory {
		return errs.Conflict(CodeVariantHasHistory,
			"this variant appears on documents and can be deactivated but not deleted").
			WithParam("sku", v.SKU)
	}
	if v.IsDefault {
		// Deleting the default would leave the product with none, which is the state §A.1
		// exists to make unreachable. Generation replaces a default before removing it.
		return errs.Conflict(CodeDefaultVariant,
			"a product's default variant cannot be deleted").WithParam("sku", v.SKU)
	}
	return nil
}

// ── combinations ────────────────────────────────────────────────────────────────

// Assignment is one attribute value a variant carries.
type Assignment struct {
	AttributeID   id.ID
	AttributeCode string
	ValueID       id.ID
	ValueCode     string
}

// Combination renders a set of assignments as the canonical string stored on a variant.
//
// Sorted by attribute code, so 'COLOUR:RED|SIZE:L' and 'SIZE:L|COLOUR:RED' are the same variant.
// Without the sort, generation would create a duplicate every time the attributes came back
// from the database in a different order — and they will, because nothing guarantees otherwise.
func Combination(assignments []Assignment) string {
	if len(assignments) == 0 {
		return ""
	}
	parts := make([]string, 0, len(assignments))
	for _, a := range assignments {
		parts = append(parts, strings.ToUpper(a.AttributeCode)+":"+strings.ToUpper(a.ValueCode))
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

// SKUFor builds a variant's SKU from the product code and its combination.
//
// "SHIRT" + colour RED + size L becomes "SHIRT-RED-L". Deterministic, because a SKU that changed
// between two runs of generation would orphan every label already printed.
func SKUFor(productCode string, assignments []Assignment) string {
	productCode = strings.ToUpper(strings.TrimSpace(productCode))
	if len(assignments) == 0 {
		return productCode
	}
	ordered := make([]Assignment, len(assignments))
	copy(ordered, assignments)
	sort.Slice(ordered, func(i, j int) bool {
		return strings.ToUpper(ordered[i].AttributeCode) < strings.ToUpper(ordered[j].AttributeCode)
	})

	var sb strings.Builder
	sb.WriteString(productCode)
	for _, a := range ordered {
		sb.WriteString("-")
		sb.WriteString(strings.ToUpper(strings.TrimSpace(a.ValueCode)))
	}
	return sb.String()
}

// Attribute is one entry in the global dictionary (§A.3).
type Attribute struct {
	ID        id.ID
	Code      string
	Name      string
	NameKey   string
	ValueType string
	SortOrder int
	IsActive  bool
}

// The attribute value types.
const (
	ValueTypeList    = "list"
	ValueTypeText    = "text"
	ValueTypeNumber  = "number"
	ValueTypeBoolean = "boolean"
	ValueTypeDate    = "date"
)

// AttributeValue is one option of a `list` attribute.
type AttributeValue struct {
	ID          id.ID
	AttributeID id.ID
	Code        string
	Name        string
	NameKey     string
	DisplayHint string
	SortOrder   int
	IsActive    bool
}

// RequireVariantDefinable refuses to make a non-enumerable attribute define variants.
//
// Generation is a Cartesian product, which needs a finite set of values to multiply. "Warranty
// in months" as a variant dimension has no such set — the request is meaningful to a user only
// as a specification, so the refusal names the attribute rather than producing zero variants and
// leaving them to wonder.
func (a Attribute) RequireVariantDefinable() error {
	if a.ValueType != ValueTypeList {
		return errs.Validation(CodeNotEnumerable,
			"only an attribute with a fixed list of values can define variants").
			WithParam("attribute", a.Code)
	}
	return nil
}
