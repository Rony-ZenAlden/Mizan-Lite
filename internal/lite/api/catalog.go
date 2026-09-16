package api

import (
	"context"
	"math"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/catalog"
	"github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	fxdomain "github.com/mizan-erp/mizan/internal/lite/fx/domain"
)

// Catalog is the product catalogue.
type Catalog struct{ core *core }

// UnitDTO is a unit of measure. Its display name is the catalog key "uom.<code>".
type UnitDTO struct {
	Code          string `json:"code"`
	Kind          string `json:"kind"`
	InputDecimals int    `json:"inputDecimals"`
}

// CurrencyDTO is a currency a price may be in.
type CurrencyDTO struct {
	Code     string `json:"code"`
	Decimals int    `json:"decimals"`
}

// ProductDTO is a product as a screen shows it. The price is a decimal string already formatted to the
// currency's decimals by Go: the frontend does no arithmetic on money (DESIGN D9).
type ProductDTO struct {
	ID            string `json:"id"`
	NameAR        string `json:"nameAr"`
	NameEN        string `json:"nameEn"`
	Barcode       string `json:"barcode"`
	UnitCode      string `json:"unitCode"`
	PriceCurrency string `json:"priceCurrency"`
	Price         string `json:"price"`
	QuickSlot     int    `json:"quickSlot"`
	Active        bool   `json:"active"`
	RowVersion    int64  `json:"rowVersion"`
	// PackageContentID is the product this one opens into, or "" — and PackageContentQuantity how much of it one
	// package holds, in the content's unit decimals (L2 §3.5). Two flat fields rather than a nullable object, so the
	// DTO stays a plain comparable value on both sides of the boundary.
	PackageContentID       string `json:"packageContentId"`
	PackageContentQuantity string `json:"packageContentQuantity"`
	// ConvertedPrice is the price in the other currency at the rate in force, rounded once in Go (L3 §3.3, Q-L3.5), and
	// ConvertedCurrency its code — both empty when there is no rate.
	ConvertedPrice    string `json:"convertedPrice"`
	ConvertedCurrency string `json:"convertedCurrency"`
	// CostPrice is what the shop pays for one unit, in PriceCurrency, or "" where it has never said (L9). MarginAmount and
	// MarginPercent are the distance between cost and price, computed in Go and never stored — both "" without a cost.
	CostPrice     string `json:"costPrice"`
	MarginAmount  string `json:"marginAmount"`
	MarginPercent string `json:"marginPercent"`
}

// catalogueView is the reference data and package links a product is formatted against.
type catalogueView struct {
	ref      domain.Reference
	packages map[id.ID]domain.Package
	units    map[id.ID]string // content product → its unit, for the content quantity's decimals
	rate     fxdomain.Rate
	hasRate  bool
	local    fxdomain.Currency
	usd      fxdomain.Currency
}

func loadCatalogueView(ctx context.Context, app *bootstrap.App) (catalogueView, error) {
	ref, err := app.Catalog.Reference(ctx)
	if err != nil {
		return catalogueView{}, err
	}
	links, err := app.Catalog.Packages(ctx)
	if err != nil {
		return catalogueView{}, err
	}
	v := catalogueView{ref: ref, packages: map[id.ID]domain.Package{}, units: map[id.ID]string{}}
	if v.rate, v.local, v.usd, v.hasRate, err = rateView(ctx, app, ref); err != nil {
		return catalogueView{}, err
	}
	for _, link := range links {
		v.packages[link.PackageProductID] = link
		content, err := app.Catalog.Get(ctx, link.ContentProductID)
		if err != nil {
			return catalogueView{}, err
		}
		v.units[content.ID] = content.UnitCode
	}
	return v, nil
}

func toProductDTO(p domain.Product, v catalogueView) ProductDTO {
	dto := ProductDTO{
		ID: p.ID.String(), NameAR: p.NameAR, NameEN: p.NameEN, Barcode: p.Barcode, UnitCode: p.UnitCode,
		PriceCurrency: p.PriceCurrency, Price: p.PriceText(v.ref), QuickSlot: p.QuickSlot, Active: p.Active,
		RowVersion: p.RowVersion,
	}
	if v.hasRate {
		if converted, ok, err := v.rate.PriceInOther(p.PriceCurrency, p.PriceMicro, v.local, v.usd); err == nil && ok {
			dto.ConvertedPrice, dto.ConvertedCurrency = converted.Text(), converted.Currency.Code
		}
	}
	if link, ok := v.packages[p.ID]; ok {
		dto.PackageContentID = link.ContentProductID.String()
		dto.PackageContentQuantity = domain.FormatMicro(link.ContentQuantityMicro, v.ref.Units[v.units[link.ContentProductID]].InputDecimals)
	}
	if p.HasCost {
		decimals := 6
		if c, ok := v.ref.Currencies[p.PriceCurrency]; ok {
			decimals = c.Decimals
		}
		dto.CostPrice = domain.FormatMicro(p.CostMicro, decimals)
		amount, percent, _ := p.Margin()
		dto.MarginAmount = signedMicro(amount, decimals)
		// A margin reads as a percentage with one decimal: "25" and "33.3" are what a shopkeeper recognises.
		dto.MarginPercent = signedMicro(percent, 1)
	}
	return dto
}

// signedMicro formats a 10⁻⁶-scaled figure that may be below zero. domain.FormatMicro takes only non-negative values,
// because a price and a quantity never are; a margin may be, and a shop that sells below cost needs to see the minus.
func signedMicro(micro int64, decimals int) string {
	if micro < 0 {
		return "-" + domain.FormatMicro(-micro, decimals)
	}
	return domain.FormatMicro(micro, decimals)
}

// parseProductID reads an id from the frontend. One that does not parse names no product.
func parseProductID(raw string) (id.ID, error) {
	parsed, err := id.Parse(raw)
	if err != nil {
		return "", domain.ErrNotFound()
	}
	return parsed, nil
}

// withProduct runs a catalogue call returning one product and formats it against the reference data.
func (c *Catalog) withProduct(method string, fn func(ctx context.Context, app *bootstrap.App) (domain.Product, error)) envelope.Result[ProductDTO] {
	return call(c.core, method, func(ctx context.Context, app *bootstrap.App) (ProductDTO, error) {
		p, err := fn(ctx, app)
		if err != nil {
			return ProductDTO{}, err
		}
		view, err := loadCatalogueView(ctx, app)
		if err != nil {
			return ProductDTO{}, err
		}
		return toProductDTO(p, view), nil
	})
}

// Units lists the units a product may be sold in.
func (c *Catalog) Units() envelope.Result[[]UnitDTO] {
	return call(c.core, "Catalog.Units", func(ctx context.Context, app *bootstrap.App) ([]UnitDTO, error) {
		units, err := app.Catalog.Units(ctx)
		out := make([]UnitDTO, 0, len(units))
		for _, u := range units {
			out = append(out, UnitDTO{Code: u.Code, Kind: u.Kind, InputDecimals: u.InputDecimals})
		}
		return out, err
	})
}

// Currencies lists the currencies a price may be in.
func (c *Catalog) Currencies() envelope.Result[[]CurrencyDTO] {
	return call(c.core, "Catalog.Currencies", func(ctx context.Context, app *bootstrap.App) ([]CurrencyDTO, error) {
		currencies, err := app.Catalog.Currencies(ctx)
		out := make([]CurrencyDTO, 0, len(currencies))
		for _, cur := range currencies {
			out = append(out, CurrencyDTO{Code: cur.Code, Decimals: cur.Decimals})
		}
		return out, err
	})
}

// ProductQueryDTO is a search.
type ProductQueryDTO struct {
	Text            string `json:"text"`
	IncludeInactive bool   `json:"includeInactive"`
}

// Products searches the catalogue.
func (c *Catalog) Products(q ProductQueryDTO) envelope.Result[[]ProductDTO] {
	return call(c.core, "Catalog.Products", func(ctx context.Context, app *bootstrap.App) ([]ProductDTO, error) {
		found, err := app.Catalog.Search(ctx, q.Text, q.IncludeInactive)
		if err != nil {
			return nil, err
		}
		view, err := loadCatalogueView(ctx, app)
		if err != nil {
			return nil, err
		}
		out := make([]ProductDTO, 0, len(found))
		for _, p := range found {
			out = append(out, toProductDTO(p, view))
		}
		return out, nil
	})
}

// Product returns one product, fresh — the form reads it before editing so it holds the current version.
func (c *Catalog) Product(productID string) envelope.Result[ProductDTO] {
	return c.withProduct("Catalog.Product", func(ctx context.Context, app *bootstrap.App) (domain.Product, error) {
		parsed, err := parseProductID(productID)
		if err != nil {
			return domain.Product{}, err
		}
		return app.Catalog.Get(ctx, parsed)
	})
}

// CreateProductInput is a new product as typed.
type CreateProductInput struct {
	NameAR        string `json:"nameAr"`
	NameEN        string `json:"nameEn"`
	Barcode       string `json:"barcode"`
	UnitCode      string `json:"unitCode"`
	PriceCurrency string `json:"priceCurrency"`
	Price         string `json:"price"`
	// CostPrice, and a margin that works the price out of it instead of taking Price as typed (L9). At most one margin.
	CostPrice     string `json:"costPrice"`
	MarginPercent string `json:"marginPercent"`
	MarginAmount  string `json:"marginAmount"`
}

// CreateProduct adds a product.
func (c *Catalog) CreateProduct(in CreateProductInput) envelope.Result[ProductDTO] {
	return c.withProduct("Catalog.CreateProduct", func(ctx context.Context, app *bootstrap.App) (domain.Product, error) {
		return app.Catalog.Create(ctx, domain.Draft{
			NameAR: in.NameAR, NameEN: in.NameEN, Barcode: in.Barcode,
			UnitCode: in.UnitCode, PriceCurrency: in.PriceCurrency, Price: in.Price,
			Cost: in.CostPrice, MarginPercent: in.MarginPercent, MarginAmount: in.MarginAmount,
		})
	})
}

// UpdateProductInput changes names and barcode.
type UpdateProductInput struct {
	ID         string `json:"id"`
	RowVersion int64  `json:"rowVersion"`
	NameAR     string `json:"nameAr"`
	NameEN     string `json:"nameEn"`
	Barcode    string `json:"barcode"`
}

// UpdateProduct changes a product's names and barcode.
func (c *Catalog) UpdateProduct(in UpdateProductInput) envelope.Result[ProductDTO] {
	return c.withProduct("Catalog.UpdateProduct", func(ctx context.Context, app *bootstrap.App) (domain.Product, error) {
		parsed, err := parseProductID(in.ID)
		if err != nil {
			return domain.Product{}, err
		}
		return app.Catalog.Update(ctx, catalog.UpdateInput{
			ID: parsed, RowVersion: in.RowVersion, NameAR: in.NameAR, NameEN: in.NameEN, Barcode: in.Barcode,
		})
	})
}

// SetPriceInput changes a price.
type SetPriceInput struct {
	ID            string `json:"id"`
	RowVersion    int64  `json:"rowVersion"`
	PriceCurrency string `json:"priceCurrency"`
	Price         string `json:"price"`
	// CostPrice is the cost to record: "" leaves it as it is, "-" takes it off (catalog.ClearCost).
	CostPrice     string `json:"costPrice"`
	MarginPercent string `json:"marginPercent"`
	MarginAmount  string `json:"marginAmount"`
}

// SetPrice changes a product's price. Outside owner mode it returns lite.owner.required, which the frontend
// answers with the PIN dialog before retrying once.
func (c *Catalog) SetPrice(in SetPriceInput) envelope.Result[ProductDTO] {
	return c.withProduct("Catalog.SetPrice", func(ctx context.Context, app *bootstrap.App) (domain.Product, error) {
		parsed, err := parseProductID(in.ID)
		if err != nil {
			return domain.Product{}, err
		}
		return app.Catalog.SetPrice(ctx, catalog.SetPriceInput{
			ID: parsed, RowVersion: in.RowVersion, Currency: in.PriceCurrency, Price: in.Price,
			Cost: in.CostPrice, MarginPercent: in.MarginPercent, MarginAmount: in.MarginAmount,
		})
	})
}

// SetActiveInput deactivates or reactivates.
type SetActiveInput struct {
	ID         string `json:"id"`
	RowVersion int64  `json:"rowVersion"`
	Active     bool   `json:"active"`
}

// SetActive deactivates (owner only) or reactivates a product.
func (c *Catalog) SetActive(in SetActiveInput) envelope.Result[ProductDTO] {
	return c.withProduct("Catalog.SetActive", func(ctx context.Context, app *bootstrap.App) (domain.Product, error) {
		parsed, err := parseProductID(in.ID)
		if err != nil {
			return domain.Product{}, err
		}
		return app.Catalog.SetActive(ctx, catalog.SetActiveInput{ID: parsed, RowVersion: in.RowVersion, Active: in.Active})
	})
}

// SetQuickSlotInput puts a product on a till button, or off with slot 0.
type SetQuickSlotInput struct {
	ID   string `json:"id"`
	Slot int    `json:"slot"`
}

// SetQuickSlot moves a product onto a till button or takes it off.
func (c *Catalog) SetQuickSlot(in SetQuickSlotInput) envelope.Result[ProductDTO] {
	return c.withProduct("Catalog.SetQuickSlot", func(ctx context.Context, app *bootstrap.App) (domain.Product, error) {
		parsed, err := parseProductID(in.ID)
		if err != nil {
			return domain.Product{}, err
		}
		slot := in.Slot
		if slot > math.MaxInt16 || slot < math.MinInt16 {
			slot = -1 // refused by the domain as no such button
		}
		return app.Catalog.SetQuickSlot(ctx, parsed, slot)
	})
}

// rateView reads the rate in force and the two currencies a conversion lands in. hasRate is false with no rate, or when
// either currency is missing from the reference data.
func rateView(ctx context.Context, app *bootstrap.App, ref domain.Reference) (fxdomain.Rate, fxdomain.Currency, fxdomain.Currency, bool, error) {
	current, err := app.FX.Current(ctx)
	if err != nil || !current.Found {
		return fxdomain.Rate{}, fxdomain.Currency{}, fxdomain.Currency{}, false, err
	}
	local, okLocal := ref.Currencies[current.Local]
	usd, okUSD := ref.Currencies[fxdomain.USD]
	if !okLocal || !okUSD {
		return fxdomain.Rate{}, fxdomain.Currency{}, fxdomain.Currency{}, false, nil
	}
	return current.Rate, fxdomain.Currency{Code: local.Code, Decimals: local.Decimals}, fxdomain.Currency{Code: usd.Code, Decimals: usd.Decimals}, true, nil
}

// SetPackageInput links a package product to what it opens into.
type SetPackageInput struct {
	PackageProductID string `json:"packageProductId"`
	ContentProductID string `json:"contentProductId"`
	ContentQuantity  string `json:"contentQuantity"`
}

// SetPackage links or relinks a package product, one level only, and returns the package product.
func (c *Catalog) SetPackage(in SetPackageInput) envelope.Result[ProductDTO] {
	return c.withProduct("Catalog.SetPackage", func(ctx context.Context, app *bootstrap.App) (domain.Product, error) {
		pkg, err := parseProductID(in.PackageProductID)
		if err != nil {
			return domain.Product{}, err
		}
		content, err := id.Parse(in.ContentProductID)
		if err != nil {
			return domain.Product{}, errs.Validation(domain.CodePackageContentRequired, "choose what the package opens into").
				WithField(domain.FieldPackageContent, domain.CodePackageContentRequired, "required")
		}
		if _, err = app.Catalog.SetPackage(ctx, catalog.SetPackageInput{PackageProductID: pkg, ContentProductID: content, ContentQuantity: in.ContentQuantity}); err != nil {
			return domain.Product{}, err
		}
		return app.Catalog.Get(ctx, pkg)
	})
}

// ClearPackage removes a package product's link and returns the product.
func (c *Catalog) ClearPackage(productID string) envelope.Result[ProductDTO] {
	return c.withProduct("Catalog.ClearPackage", func(ctx context.Context, app *bootstrap.App) (domain.Product, error) {
		parsed, err := parseProductID(productID)
		if err != nil {
			return domain.Product{}, err
		}
		if err = app.Catalog.ClearPackage(ctx, parsed); err != nil {
			return domain.Product{}, err
		}
		return app.Catalog.Get(ctx, parsed)
	})
}
