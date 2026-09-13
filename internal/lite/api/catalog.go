package api

import (
	"context"
	"math"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/catalog"
	"github.com/mizan-erp/mizan/internal/lite/catalog/domain"
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
}

func toProductDTO(p domain.Product, ref domain.Reference) ProductDTO {
	return ProductDTO{
		ID: p.ID.String(), NameAR: p.NameAR, NameEN: p.NameEN, Barcode: p.Barcode, UnitCode: p.UnitCode,
		PriceCurrency: p.PriceCurrency, Price: p.PriceText(ref), QuickSlot: p.QuickSlot, Active: p.Active,
		RowVersion: p.RowVersion,
	}
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
		ref, err := app.Catalog.Reference(ctx)
		if err != nil {
			return ProductDTO{}, err
		}
		return toProductDTO(p, ref), nil
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
		ref, err := app.Catalog.Reference(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]ProductDTO, 0, len(found))
		for _, p := range found {
			out = append(out, toProductDTO(p, ref))
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
}

// CreateProduct adds a product.
func (c *Catalog) CreateProduct(in CreateProductInput) envelope.Result[ProductDTO] {
	return c.withProduct("Catalog.CreateProduct", func(ctx context.Context, app *bootstrap.App) (domain.Product, error) {
		return app.Catalog.Create(ctx, domain.Draft{
			NameAR: in.NameAR, NameEN: in.NameEN, Barcode: in.Barcode,
			UnitCode: in.UnitCode, PriceCurrency: in.PriceCurrency, Price: in.Price,
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
