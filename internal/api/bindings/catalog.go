package bindings

import (
	"strconv"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/api/policy"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/catalog"
	"github.com/mizan-erp/mizan/internal/modules/pricing"
)

// CodeInvalidQuantity is returned when a quantity string is not an integer.
const CodeInvalidQuantity = "catalog.invalid_quantity"

// CategoryDTO is one node of the product hierarchy.
//
// `depth` crosses so the screen can indent the tree without re-deriving it from the path — the
// same reasoning that puts `depth` on AccountDTO.
type CategoryDTO struct {
	ID      string `json:"id"`
	Code    string `json:"code"`
	Name    string `json:"name"`
	NameKey string `json:"nameKey"`
	Path    string `json:"path"`
	Depth   int    `json:"depth"`
	Active  bool   `json:"isActive"`
}

// ProductRowDTO is one product in the browse list.
//
// The UNIT CODE crosses, not the unit id: a list shows "PCS" and an id would force the screen to
// hold a second lookup table purely to render one column.
type ProductRowDTO struct {
	ID           string `json:"id"`
	Code         string `json:"code"`
	Name         string `json:"name"`
	NameKey      string `json:"nameKey"`
	CategoryCode string `json:"categoryCode"`
	Type         string `json:"type"`
	StockUnit    string `json:"stockUnit"`
	Tracking     string `json:"tracking"`
	VariantCount int    `json:"variantCount"`
	Active       bool   `json:"isActive"`
}

// VariantDTO is one variant of a product.
//
// `isDefault` crosses because the screen must NOT show a simple product's default variant: §A.1
// says every product has one, and a bag of cement showing a "Variants (1)" section would make
// the mechanism visible to somebody it was designed to hide from.
type VariantDTO struct {
	ID          string `json:"id"`
	SKU         string `json:"sku"`
	Name        string `json:"name"`
	Combination string `json:"combination"`
	Default     bool   `json:"isDefault"`
	HasHistory  bool   `json:"hasHistory"`
	Active      bool   `json:"isActive"`
}

// ProductDetailDTO is everything the detail screen shows.
type ProductDetailDTO struct {
	Product      ProductRowDTO `json:"product"`
	SalesUnit    string        `json:"salesUnit"`
	PurchaseUnit string        `json:"purchaseUnit"`
	// StockUnitLocked reports that stock has moved, so the screen can explain why the unit
	// cannot be changed instead of offering a control that always refuses.
	StockUnitLocked bool `json:"stockUnitLocked"`

	Variants   []VariantDTO          `json:"variants"`
	Attributes []ProductAttributeDTO `json:"attributes"`
	// IsSimple is the §A.1 hint: one variant, and it is the default. The screen hides the whole
	// variants section when it is true.
	IsSimple bool `json:"isSimple"`
}

// ProductAttributeDTO is one attribute of a product with the values it offers.
type ProductAttributeDTO struct {
	Code            string              `json:"code"`
	Name            string              `json:"name"`
	NameKey         string              `json:"nameKey"`
	VariantDefining bool                `json:"isVariantDefining"`
	Values          []AttributeValueDTO `json:"values"`
}

// AttributeValueDTO is one option a product offers.
type AttributeValueDTO struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	NameKey string `json:"nameKey"`
	Hint    string `json:"displayHint"`
}

// UnitDTO is one unit of measure, for a picker.
type UnitDTO struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	NameKey    string `json:"nameKey"`
	Symbol     string `json:"symbol"`
	CategoryID string `json:"categoryId"`
	Fractional bool   `json:"allowsFractional"`
	Decimals   int    `json:"displayDecimals"`
}

// ScanDTO is what a scanned barcode resolves to.
//
// The quantity crosses as a STRING of micro units, for the same reason money does: it is an
// integer that JavaScript's float64 cannot be trusted with, and nothing on the frontend does
// arithmetic with it.
type ScanDTO struct {
	VariantID     string `json:"variantId"`
	SKU           string `json:"sku"`
	ProductCode   string `json:"productCode"`
	ProductName   string `json:"productName"`
	PackagingCode string `json:"packagingCode"`
	QuantityMicro string `json:"quantityMicro"`
}

// PriceDTO is a resolved price and the reason for it.
//
// `source` and `listCode` cross because §2.6 requires resolution to record which list answered,
// and a price a salesperson cannot explain to the customer in front of them is a price they
// override by hand.
type PriceDTO struct {
	PriceMinor  string `json:"priceMinor"`
	Currency    string `json:"currency"`
	ListCode    string `json:"listCode"`
	Source      string `json:"source"`
	MinQuantity string `json:"minQuantityMicro"`
}

// Catalog is the catalog browse and product detail surface.
//
// Read-only, deliberately. Phase 3 builds the master data and the screens that READ it; the
// forms that create products belong with the release that needs them, and shipping their
// bindings now would put buttons behind a permission nobody has a screen for.
type Catalog struct{ graph }

// catalogPolicies declares what each method requires.
func catalogPolicies() map[string]policy.Policy {
	return map[string]policy.Policy{
		"Categories": policy.Requires(catalog.PermCatalogView),
		"Products":   policy.Requires(catalog.PermCatalogView),
		"Product":    policy.Requires(catalog.PermCatalogView),
		"Units":      policy.Requires(catalog.PermCatalogView),
		"Scan":       policy.Requires(catalog.PermCatalogView),
		"Price":      policy.Requires(pricing.PermPriceView),
	}
}

// Categories lists the product hierarchy, in tree order.
func (c *Catalog) Categories() envelope.Result[[]CategoryDTO] {
	ctx, app, err := c.guard("Categories")
	if err != nil {
		return envelope.Fail[[]CategoryDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[[]CategoryDTO](err)
	}
	categories, err := app.Catalog.Categories(ctx, companyID)
	if err != nil {
		return envelope.Fail[[]CategoryDTO](err)
	}

	out := make([]CategoryDTO, 0, len(categories))
	for _, category := range categories {
		out = append(out, CategoryDTO{
			ID: string(category.ID), Code: category.Code, Name: category.Name,
			NameKey: category.NameKey, Path: category.Path, Depth: category.Depth,
			Active: category.IsActive,
		})
	}
	return envelope.Ok(out)
}

// Products lists the company's products.
func (c *Catalog) Products() envelope.Result[[]ProductRowDTO] {
	ctx, app, err := c.guard("Products")
	if err != nil {
		return envelope.Fail[[]ProductRowDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[[]ProductRowDTO](err)
	}
	products, err := app.Catalog.Products(ctx, companyID)
	if err != nil {
		return envelope.Fail[[]ProductRowDTO](err)
	}

	units, err := app.Catalog.Units(ctx)
	if err != nil {
		return envelope.Fail[[]ProductRowDTO](err)
	}
	unitCode := make(map[id.ID]string, len(units))
	for _, unit := range units {
		unitCode[unit.ID] = unit.Code
	}
	categories, err := app.Catalog.Categories(ctx, companyID)
	if err != nil {
		return envelope.Fail[[]ProductRowDTO](err)
	}
	categoryCode := make(map[id.ID]string, len(categories))
	for _, category := range categories {
		categoryCode[category.ID] = category.Code
	}

	out := make([]ProductRowDTO, 0, len(products))
	for _, product := range products {
		variants, variantErr := app.Catalog.Variants(ctx, product.ID)
		if variantErr != nil {
			return envelope.Fail[[]ProductRowDTO](variantErr)
		}
		out = append(out, ProductRowDTO{
			ID: string(product.ID), Code: product.Code, Name: product.Name,
			NameKey: product.NameKey, CategoryCode: categoryCode[product.CategoryID],
			Type: string(product.Type), StockUnit: unitCode[product.StockUnitID],
			Tracking: string(product.Tracking), VariantCount: len(variants),
			Active: product.IsActive,
		})
	}
	return envelope.Ok(out)
}

// Product reads one product with its variants and attributes.
func (c *Catalog) Product(code string) envelope.Result[ProductDetailDTO] {
	ctx, app, err := c.guard("Product")
	if err != nil {
		return envelope.Fail[ProductDetailDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[ProductDetailDTO](err)
	}
	product, err := app.Catalog.ProductByCode(ctx, companyID, code)
	if err != nil {
		return envelope.Fail[ProductDetailDTO](err)
	}

	units, err := app.Catalog.Units(ctx)
	if err != nil {
		return envelope.Fail[ProductDetailDTO](err)
	}
	unitCode := make(map[id.ID]string, len(units))
	for _, unit := range units {
		unitCode[unit.ID] = unit.Code
	}
	categories, err := app.Catalog.Categories(ctx, companyID)
	if err != nil {
		return envelope.Fail[ProductDetailDTO](err)
	}
	categoryCode := make(map[id.ID]string, len(categories))
	for _, category := range categories {
		categoryCode[category.ID] = category.Code
	}

	variants, err := app.Catalog.Variants(ctx, product.ID)
	if err != nil {
		return envelope.Fail[ProductDetailDTO](err)
	}
	variantDTOs := make([]VariantDTO, 0, len(variants))
	for _, variant := range variants {
		variantDTOs = append(variantDTOs, VariantDTO{
			ID: string(variant.ID), SKU: variant.SKU, Name: variant.Name,
			Combination: variant.Combination, Default: variant.IsDefault,
			HasHistory: variant.HasHistory, Active: variant.IsActive,
		})
	}

	links, err := app.Catalog.ProductAttributes(ctx, product.ID)
	if err != nil {
		return envelope.Fail[ProductDetailDTO](err)
	}
	attributeDTOs := make([]ProductAttributeDTO, 0, len(links))
	for _, link := range links {
		values := make([]AttributeValueDTO, 0, len(link.Values))
		for _, value := range link.Values {
			values = append(values, AttributeValueDTO{
				Code: value.Code, Name: value.Name, NameKey: value.NameKey,
				Hint: value.DisplayHint,
			})
		}
		attributeDTOs = append(attributeDTOs, ProductAttributeDTO{
			Code: link.Attribute.Code, Name: link.Attribute.Name,
			NameKey: link.Attribute.NameKey, VariantDefining: link.VariantDefining,
			Values: values,
		})
	}

	return envelope.Ok(ProductDetailDTO{
		Product: ProductRowDTO{
			ID: string(product.ID), Code: product.Code, Name: product.Name,
			NameKey: product.NameKey, CategoryCode: categoryCode[product.CategoryID],
			Type: string(product.Type), StockUnit: unitCode[product.StockUnitID],
			Tracking: string(product.Tracking), VariantCount: len(variants),
			Active: product.IsActive,
		},
		SalesUnit:       unitCode[product.SalesUnitID],
		PurchaseUnit:    unitCode[product.PurchaseUnitID],
		StockUnitLocked: product.HasHistory,
		Variants:        variantDTOs, Attributes: attributeDTOs,
		// A simple product: the one variant §A.1 guarantees, and nothing else. The screen hides
		// the variants section entirely, so a bag of cement never shows the word.
		IsSimple: len(variants) == 1 && variants[0].IsDefault,
	})
}

// Units lists the units of measure, for a picker.
func (c *Catalog) Units() envelope.Result[[]UnitDTO] {
	ctx, app, err := c.guard("Units")
	if err != nil {
		return envelope.Fail[[]UnitDTO](err)
	}
	units, err := app.Catalog.Units(ctx)
	if err != nil {
		return envelope.Fail[[]UnitDTO](err)
	}

	out := make([]UnitDTO, 0, len(units))
	for _, unit := range units {
		out = append(out, UnitDTO{
			Code: unit.Code, Name: unit.Name, NameKey: unit.NameKey, Symbol: unit.Symbol,
			CategoryID: string(unit.CategoryID), Fractional: unit.AllowsFractional,
			Decimals: unit.DisplayDecimals,
		})
	}
	return envelope.Ok(out)
}

// Scan resolves a barcode to one variant and a quantity (§A.5).
func (c *Catalog) Scan(code string) envelope.Result[ScanDTO] {
	ctx, app, err := c.guard("Scan")
	if err != nil {
		return envelope.Fail[ScanDTO](err)
	}
	scan, err := app.Catalog.ScanBarcode(ctx, code)
	if err != nil {
		return envelope.Fail[ScanDTO](err)
	}

	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[ScanDTO](err)
	}
	// The scan answers with a variant id; a till needs a name to put on the line.
	products, err := app.Catalog.Products(ctx, companyID)
	if err != nil {
		return envelope.Fail[ScanDTO](err)
	}
	out := ScanDTO{
		VariantID: string(scan.VariantID), QuantityMicro: minor(scan.QuantityMicro),
		PackagingCode: string(scan.PackagingID),
	}
	for _, product := range products {
		variants, variantErr := app.Catalog.Variants(ctx, product.ID)
		if variantErr != nil {
			return envelope.Fail[ScanDTO](variantErr)
		}
		for _, variant := range variants {
			if variant.ID == scan.VariantID {
				out.SKU = variant.SKU
				out.ProductCode = product.Code
				out.ProductName = product.Name
			}
		}
	}
	return envelope.Ok(out)
}

// Price resolves what a variant costs, and reports which list answered.
func (c *Catalog) Price(
	productID, variantID, partnerID, quantityMicro string,
) envelope.Result[PriceDTO] {
	ctx, app, err := c.guard("Price")
	if err != nil {
		return envelope.Fail[PriceDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[PriceDTO](err)
	}

	// The quantity arrives as a STRING of micro units, matching how it left. Parsed here rather
	// than accepted as a number, because a float64 that has been through JSON is not the integer
	// that was sent once the value is large enough.
	quantity, err := strconv.ParseInt(quantityMicro, 10, 64)
	if err != nil {
		return envelope.Fail[PriceDTO](errs.Validation(CodeInvalidQuantity,
			"that is not a quantity").WithParam("value", quantityMicro))
	}
	resolved, err := app.Pricing.Price(ctx, pricing.PriceQuery{
		CompanyID: companyID, PartnerID: id.ID(partnerID),
		ProductID: id.ID(productID), VariantID: id.ID(variantID),
		QuantityMicro: quantity, Direction: pricing.Sale,
	})
	if err != nil {
		return envelope.Fail[PriceDTO](err)
	}

	return envelope.Ok(PriceDTO{
		PriceMinor: minor(resolved.PriceMinor), Currency: resolved.CurrencyCode,
		ListCode: resolved.ListCode, Source: string(resolved.Source),
		MinQuantity: minor(resolved.MinQuantity),
	})
}
