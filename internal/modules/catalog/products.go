package catalog

import (
	"context"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/catalog/domain"
	"github.com/mizan-erp/mizan/internal/modules/catalog/infra/sqlite"
)

// The audited actions this step adds (§15.3).
const (
	ActionCategoryCreated  = "catalog.category.created"
	ActionProductCreated   = "catalog.product.created"
	ActionAttributeCreated = "catalog.attribute.created"
	ActionAttributeLinked  = "catalog.attribute.linked"

	EntityCategory  = "catalog.category"
	EntityProduct   = "catalog.product"
	EntityAttribute = "catalog.attribute"
)

// Stable codes for the failures this step can report.
const (
	CodeUnknownCategory = "catalog.unknown_category"
	CodeDuplicateCode   = "catalog.duplicate_code"
	CodeUnknownProduct  = "catalog.unknown_product"
)

// NewCategoryInput is what creating a category needs.
type NewCategoryInput struct {
	CompanyID  id.ID
	Code       string
	Name       string
	NameKey    string
	ParentCode string
}

// CreateCategory adds a node to the product hierarchy.
func (s *Service) CreateCategory(
	ctx context.Context, in NewCategoryInput,
) (domain.Category, error) {
	var created domain.Category

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		existing, found, err := s.repos.CategoryByCode(txCtx, in.CompanyID, upper(in.Code))
		if err != nil {
			return err
		}
		if found {
			return errs.Conflict(CodeDuplicateCode,
				"a category with this code already exists").WithParam("code", existing.Code)
		}

		var parent *domain.Category
		if in.ParentCode != "" {
			above, ok, parentErr := s.repos.CategoryByCode(
				txCtx, in.CompanyID, upper(in.ParentCode))
			if parentErr != nil {
				return parentErr
			}
			if !ok {
				return errs.NotFound(CodeUnknownCategory,
					"there is no category with that code").
					WithParam("code", upper(in.ParentCode))
			}
			parent = &above
		}

		identifier, err := id.New()
		if err != nil {
			return err
		}
		category, err := domain.NewCategory(identifier, in.Code, in.Name, parent)
		if err != nil {
			return err
		}
		category.NameKey = in.NameKey

		if err = s.repos.InsertCategory(txCtx, in.CompanyID, category); err != nil {
			return err
		}
		created = category

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionCategoryCreated, EntityType: EntityCategory,
			EntityID: category.ID,
			After: map[string]any{
				"code": category.Code, "name": category.Name, "path": category.Path,
			},
		})
	})
	if err != nil {
		return domain.Category{}, err
	}
	return created, nil
}

// Categories lists a company's product hierarchy in tree order.
func (s *Service) Categories(ctx context.Context, companyID id.ID) ([]domain.Category, error) {
	return s.repos.Categories(ctx, companyID)
}

// NewProductInput is what creating a product needs.
//
// The three units arrive as CODES, not ids. A unit code is what an operator types, what an
// import file carries, and what a seed names; making the caller resolve an id first would push
// that lookup — and its error handling — into every caller.
type NewProductInput struct {
	CompanyID    id.ID
	Code         string
	Name         string
	NameKey      string
	CategoryCode string
	Type         domain.ProductType
	StockUnit    string
	SalesUnit    string
	PurchaseUnit string
	Tracking     domain.Tracking
	TaxGroupID   id.ID
}

// CreateProduct adds a product and its default variant.
//
// # One transaction, both rows
//
// §A.1 says every product has at least one variant. The domain guarantees it by construction —
// NewProduct cannot return a product alone — and this guarantees it in storage by writing both
// inside one transaction. A crash between the two writes would otherwise produce exactly the
// state the decision exists to make unreachable.
//
// # A service defaults to being untracked
//
// A haircut has no stock. Passing `goods` tracking for a service would create stock rows Phase 4
// then has to explain, so the type decides the default and an explicit Tracking overrides it.
func (s *Service) CreateProduct(
	ctx context.Context, in NewProductInput,
) (domain.Product, domain.Variant, error) {
	var (
		product domain.Product
		variant domain.Variant
	)

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		if _, found, err := s.repos.ProductByCode(txCtx, in.CompanyID, upper(in.Code)); err != nil {
			return err
		} else if found {
			return errs.Conflict(CodeDuplicateCode,
				"a product with this code already exists").WithParam("code", upper(in.Code))
		}

		stock, err := s.requireUnit(txCtx, in.StockUnit)
		if err != nil {
			return err
		}
		// Sales and purchase default to the stock unit, which is what a business that measures
		// one way throughout wants and never has to say.
		sales := stock
		if in.SalesUnit != "" {
			if sales, err = s.requireUnit(txCtx, in.SalesUnit); err != nil {
				return err
			}
		}
		purchase := stock
		if in.PurchaseUnit != "" {
			if purchase, err = s.requireUnit(txCtx, in.PurchaseUnit); err != nil {
				return err
			}
		}

		productID, err := id.New()
		if err != nil {
			return err
		}
		variantID, err := id.New()
		if err != nil {
			return err
		}
		built, defaultVariant, err := domain.NewProduct(
			productID, variantID, in.Code, in.Name, stock, sales, purchase)
		if err != nil {
			return err
		}
		built.NameKey = in.NameKey
		built.TaxGroupID = in.TaxGroupID

		if in.Type != "" {
			built.Type = in.Type
		}
		switch {
		case in.Tracking != "":
			built.Tracking = in.Tracking
		case built.Type == domain.TypeService:
			// A service is not stocked. Not a validation rule — a business may well want to
			// track a serialised service contract — just the sensible default.
			built.Tracking = domain.TrackNone
		}

		if in.CategoryCode != "" {
			category, found, catErr := s.repos.CategoryByCode(
				txCtx, in.CompanyID, upper(in.CategoryCode))
			if catErr != nil {
				return catErr
			}
			if !found {
				return errs.NotFound(CodeUnknownCategory,
					"there is no category with that code").
					WithParam("code", upper(in.CategoryCode))
			}
			built.CategoryID = category.ID
		}

		if err = s.repos.InsertProduct(txCtx, in.CompanyID, built, defaultVariant); err != nil {
			return err
		}
		product, variant = built, defaultVariant

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionProductCreated, EntityType: EntityProduct,
			EntityID: built.ID,
			After: map[string]any{
				"code": built.Code, "name": built.Name,
				"stock_uom": stock.Code, "sku": defaultVariant.SKU,
			},
		})
	})
	if err != nil {
		return domain.Product{}, domain.Variant{}, err
	}
	return product, variant, nil
}

// Products lists a company's products.
func (s *Service) Products(ctx context.Context, companyID id.ID) ([]domain.Product, error) {
	return s.repos.Products(ctx, companyID)
}

// ProductByCode finds one product.
func (s *Service) ProductByCode(
	ctx context.Context, companyID id.ID, code string,
) (domain.Product, error) {
	product, found, err := s.repos.ProductByCode(ctx, companyID, upper(code))
	if err != nil {
		return domain.Product{}, err
	}
	if !found {
		return domain.Product{}, errs.NotFound(CodeUnknownProduct,
			"there is no product with that code").WithParam("code", upper(code))
	}
	return product, nil
}

// Variants lists a product's variants, the default first.
func (s *Service) Variants(ctx context.Context, productID id.ID) ([]domain.Variant, error) {
	return s.repos.Variants(ctx, productID)
}

// ChangeStockUnit moves a product to a different stock unit, or refuses.
//
// Refused once anything has moved, because a change does not convert history: every past
// quantity was recorded as a number of the OLD unit and every cost derived from one, so the
// change silently restates the product's entire history by the factor between them.
func (s *Service) ChangeStockUnit(
	ctx context.Context, companyID id.ID, productCode, unitCode string,
) error {
	return s.db.Do(ctx, func(txCtx context.Context) error {
		product, found, err := s.repos.ProductByCode(txCtx, companyID, upper(productCode))
		if err != nil {
			return err
		}
		if !found {
			return errs.NotFound(CodeUnknownProduct,
				"there is no product with that code").WithParam("code", upper(productCode))
		}

		current, err := s.repos.UnitByID(txCtx, product.StockUnitID)
		if err != nil {
			return err
		}
		next, err := s.requireUnit(txCtx, unitCode)
		if err != nil {
			return err
		}

		updated, err := product.ChangeStockUnit(next, current)
		if err != nil {
			return err
		}
		return s.repos.UpdateStockUnit(txCtx, updated.ID, updated.StockUnitID)
	})
}

// ── attributes ──────────────────────────────────────────────────────────────────

// NewAttributeInput defines a dictionary attribute and its values.
type NewAttributeInput struct {
	CompanyID id.ID
	Code      string
	Name      string
	NameKey   string
	ValueType string
	Values    []AttributeValueInput
}

// AttributeValueInput is one option of a list attribute.
type AttributeValueInput struct {
	Code        string
	Name        string
	NameKey     string
	DisplayHint string
}

// CreateAttribute adds an attribute to the global dictionary.
func (s *Service) CreateAttribute(
	ctx context.Context, in NewAttributeInput,
) (domain.Attribute, error) {
	var created domain.Attribute

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		if _, found, err := s.repos.AttributeByCode(
			txCtx, in.CompanyID, upper(in.Code)); err != nil {
			return err
		} else if found {
			return errs.Conflict(CodeDuplicateCode,
				"an attribute with this code already exists").WithParam("code", upper(in.Code))
		}

		identifier, err := id.New()
		if err != nil {
			return err
		}
		valueType := in.ValueType
		if valueType == "" {
			valueType = domain.ValueTypeList
		}
		attribute := domain.Attribute{
			ID: identifier, Code: upper(in.Code), Name: in.Name, NameKey: in.NameKey,
			ValueType: valueType, IsActive: true,
		}
		if err = s.repos.InsertAttribute(txCtx, in.CompanyID, attribute); err != nil {
			return err
		}

		for i, v := range in.Values {
			valueID, valErr := id.New()
			if valErr != nil {
				return valErr
			}
			if err = s.repos.InsertAttributeValue(txCtx, domain.AttributeValue{
				ID: valueID, AttributeID: identifier, Code: upper(v.Code), Name: v.Name,
				NameKey: v.NameKey, DisplayHint: v.DisplayHint, SortOrder: i, IsActive: true,
			}); err != nil {
				return err
			}
		}
		created = attribute

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionAttributeCreated, EntityType: EntityAttribute,
			EntityID: identifier,
			After: map[string]any{
				"code": attribute.Code, "value_type": valueType, "values": len(in.Values),
			},
		})
	})
	if err != nil {
		return domain.Attribute{}, err
	}
	return created, nil
}

// LinkAttributeInput attaches a dictionary attribute to a product.
type LinkAttributeInput struct {
	CompanyID     id.ID
	ProductCode   string
	AttributeCode string
	// VariantDefining is §A.2's flag, and it lives on the LINK rather than the attribute:
	// material defines variants for a sofa and describes a screwdriver.
	VariantDefining bool
	// ValueCodes limits which of the attribute's values this product offers. Empty means all of
	// them — a shirt in three of the dictionary's twenty colours names those three.
	ValueCodes []string
}

// LinkAttribute attaches an attribute to a product.
//
// A variant-defining attribute must be enumerable: generation is a Cartesian product, and
// "warranty in months" has no finite set to multiply. Refused here, naming the attribute, rather
// than silently producing no variants and leaving the user to work out why.
func (s *Service) LinkAttribute(ctx context.Context, in LinkAttributeInput) error {
	return s.db.Do(ctx, func(txCtx context.Context) error {
		product, found, err := s.repos.ProductByCode(txCtx, in.CompanyID, upper(in.ProductCode))
		if err != nil {
			return err
		}
		if !found {
			return errs.NotFound(CodeUnknownProduct,
				"there is no product with that code").WithParam("code", upper(in.ProductCode))
		}

		attribute, found, err := s.repos.AttributeByCode(
			txCtx, in.CompanyID, upper(in.AttributeCode))
		if err != nil {
			return err
		}
		if !found {
			return errs.NotFound(CodeUnknownCategory,
				"there is no attribute with that code").
				WithParam("code", upper(in.AttributeCode))
		}
		if in.VariantDefining {
			if err = attribute.RequireVariantDefinable(); err != nil {
				return err
			}
		}

		linkID, err := id.New()
		if err != nil {
			return err
		}
		if err = s.repos.LinkAttribute(
			txCtx, linkID, product.ID, attribute.ID, in.VariantDefining, 0); err != nil {
			return err
		}

		values, err := s.repos.AttributeValues(txCtx, attribute.ID)
		if err != nil {
			return err
		}
		wanted := make(map[string]bool, len(in.ValueCodes))
		for _, code := range in.ValueCodes {
			wanted[upper(code)] = true
		}

		order := 0
		for _, value := range values {
			if len(wanted) > 0 && !wanted[value.Code] {
				continue
			}
			rowID, rowErr := id.New()
			if rowErr != nil {
				return rowErr
			}
			if err = s.repos.OfferValue(txCtx, rowID, linkID, value.ID, order); err != nil {
				return err
			}
			order++
		}

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionAttributeLinked, EntityType: EntityProduct,
			EntityID: product.ID,
			After: map[string]any{
				"product": product.Code, "attribute": attribute.Code,
				"variant_defining": in.VariantDefining, "values": order,
			},
		})
	})
}

// ProductAttributes lists a product's attributes with the values it offers.
func (s *Service) ProductAttributes(
	ctx context.Context, productID id.ID,
) ([]sqlite.ProductAttribute, error) {
	return s.repos.ProductAttributes(ctx, productID)
}

// requireUnit resolves a unit code.
//
// Delegated rather than re-checked: UnitByCode already reports a typed not-found naming the
// code, and a second code for the same condition would mean two different messages for one
// situation depending on which path a caller took.
func (s *Service) requireUnit(ctx context.Context, code string) (domain.Unit, error) {
	return s.repos.UnitByCode(ctx, upper(code))
}

// upper normalises a code the way every code in this module is stored: trimmed and upper-cased.
// Codes are matched exactly by seeds, imports, and barcodes, so "shirt" and "SHIRT" being two
// products is a duplicate nobody spots until a stock count.
func upper(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// MarkVariantHistory records that a variant now appears on a document or a movement.
//
// Phase 4 calls this on the first stock movement and Phase 5 on the first document line. It
// exists now rather than then because it is what the two rules above READ — a variant that
// cannot be deleted, a stock unit that cannot change — and a rule whose trigger does not exist
// yet is a rule no test can exercise. Declared at the point of use, as identity's Organisation
// port was (1.2).
func (s *Service) MarkVariantHistory(ctx context.Context, variantID id.ID) error {
	return s.db.Do(ctx, func(txCtx context.Context) error {
		return s.repos.MarkVariantHistory(txCtx, variantID)
	})
}
