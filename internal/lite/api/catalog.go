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
	"github.com/mizan-erp/mizan/internal/lite/moneyfmt"
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
	// ReorderLevel is the quantity at or below which the shop wants to be told to buy more, in the product's own unit,
	// or "" where none was set — in which case the product is never called low (2026-09-20).
	ReorderLevel string `json:"reorderLevel"`
	// OpenPrice marks an item sold at a price typed at the till and never counted in stock (2026-09-23).
	OpenPrice bool `json:"openPrice"`
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
	// shop carries the redenomination, so a price on a shelf label reads as the till reads it (L10).
	shop moneyfmt.Shop
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
	shop, err := moneyShop(ctx, app)
	if err != nil {
		return catalogueView{}, err
	}
	v := catalogueView{ref: ref, packages: map[id.ID]domain.Package{}, units: map[id.ID]string{}, shop: shop}
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
		PriceCurrency: p.PriceCurrency, Price: v.shop.Display(p.PriceText(v.ref), p.PriceCurrency), QuickSlot: p.QuickSlot, Active: p.Active,
		RowVersion: p.RowVersion, OpenPrice: p.OpenPrice,
	}
	if v.hasRate {
		if converted, ok, err := v.rate.PriceInOther(p.PriceCurrency, p.PriceMicro, v.local, v.usd); err == nil && ok {
			dto.ConvertedPrice, dto.ConvertedCurrency = v.shop.Display(converted.Text(), converted.Currency.Code), converted.Currency.Code
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
		dto.CostPrice = v.shop.Display(domain.FormatMicro(p.CostMicro, decimals), p.PriceCurrency)
		amount, percent, _ := p.Margin()
		dto.MarginAmount = v.shop.Display(signedMicro(amount, decimals), p.PriceCurrency)
		// A margin reads as a percentage with one decimal: "25" and "33.3" are what a shopkeeper recognises. A percentage
		// is not money and is never redenominated — 25% of a price is 25% whichever way the shop reads it.
		dto.MarginPercent = signedMicro(percent, 1)
	}
	// A reorder level is a QUANTITY, not money: it is never redenominated, and it is read in the product's own unit.
	if p.HasReorder {
		dto.ReorderLevel = domain.FormatMicro(p.ReorderMicro, v.ref.Units[p.UnitCode].InputDecimals)
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
	// OpenPrice creates an item sold at a price typed at the till, never counted in stock (2026-09-23). It then takes no
	// price, cost or margin, and cannot be changed afterwards.
	OpenPrice bool `json:"openPrice"`
}

// CreateProduct adds a product.
func (c *Catalog) CreateProduct(in CreateProductInput) envelope.Result[ProductDTO] {
	return c.withProduct("Catalog.CreateProduct", func(ctx context.Context, app *bootstrap.App) (domain.Product, error) {
		shop, err := moneyShop(ctx, app)
		if err != nil {
			return domain.Product{}, err
		}
		// What a person typed is in the shop's own reading of its currency; the books hold the base figure (L10).
		return app.Catalog.Create(ctx, domain.Draft{
			NameAR: in.NameAR, NameEN: in.NameEN, Barcode: in.Barcode,
			UnitCode: in.UnitCode, PriceCurrency: in.PriceCurrency,
			Price: shop.Base(in.Price, in.PriceCurrency),
			Cost:  shop.Base(in.CostPrice, in.PriceCurrency),
			// A percentage is not money: 25% is 25% whichever way the shop reads its pounds.
			MarginPercent: in.MarginPercent,
			MarginAmount:  shop.Base(in.MarginAmount, in.PriceCurrency),

			OpenPrice: in.OpenPrice})
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
		shop, err := moneyShop(ctx, app)
		if err != nil {
			return domain.Product{}, err
		}
		cost := in.CostPrice
		if cost != catalog.ClearCost { // "-" is a word, not a figure
			cost = shop.Base(cost, in.PriceCurrency)
		}
		return app.Catalog.SetPrice(ctx, catalog.SetPriceInput{
			ID: parsed, RowVersion: in.RowVersion, Currency: in.PriceCurrency,
			Price: shop.Base(in.Price, in.PriceCurrency),
			Cost:  cost, MarginPercent: in.MarginPercent,
			MarginAmount: shop.Base(in.MarginAmount, in.PriceCurrency),
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

// SetReorderInput is the level at or below which a product is low, in its own unit. "" clears it.
type SetReorderInput struct {
	ID         string `json:"id"`
	RowVersion int64  `json:"rowVersion"`
	Level      string `json:"level"`
}

// SetReorder records when a product should be called low (2026-09-20).
//
// Not owner-only: it changes no price, no quantity and no money, only when a badge appears. A shopkeeper who has
// noticed the bread keeps running out should be able to say so without fetching the owner.
func (c *Catalog) SetReorder(in SetReorderInput) envelope.Result[ProductDTO] {
	return c.withProduct("Catalog.SetReorder", func(ctx context.Context, app *bootstrap.App) (domain.Product, error) {
		parsed, err := parseProductID(in.ID)
		if err != nil {
			return domain.Product{}, err
		}
		return app.Catalog.SetReorder(ctx, catalog.SetReorderInput{ID: parsed, RowVersion: in.RowVersion, Level: in.Level})
	})
}

// RepriceItemDTO is one proposed price change: the product as it is, and what the proposal would make its price.
type RepriceItemDTO struct {
	ID         string `json:"id"`
	RowVersion int64  `json:"rowVersion"`
	NameAR     string `json:"nameAr"`
	NameEN     string `json:"nameEn"`
	Currency   string `json:"currency"`
	Price      string `json:"price"`
	Proposed   string `json:"proposed"`
	// Shift is how far the exchange rate has moved since the product was priced, in percent with its sign.
	Shift string `json:"shift"`
}

// RepriceProposalDTO is a re-price the owner has not agreed to: nothing has changed yet.
type RepriceProposalDTO struct {
	Rate  string           `json:"rate"`
	Items []RepriceItemDTO `json:"items"`
}

// RepriceChangeInput is one price the owner confirmed, at the version they saw.
type RepriceChangeInput struct {
	ID         string `json:"id"`
	RowVersion int64  `json:"rowVersion"`
	Price      string `json:"price"`
}

// BulkRepriceInput is the prices the owner confirmed, all applied or none.
type BulkRepriceInput struct {
	Items []RepriceChangeInput `json:"items"`
}

// RepriceProposal lists the prices the exchange rate has left behind, with a proposed price for each (2026-09-23).
// NOTHING changes: this is what the owner reads before deciding. percent is "" to follow the rate, or a signed
// percentage to move every listed price by the same amount instead.
func (c *Catalog) RepriceProposal(percent string) envelope.Result[RepriceProposalDTO] {
	return call(c.core, "Catalog.RepriceProposal", func(ctx context.Context, app *bootstrap.App) (RepriceProposalDTO, error) {
		p, err := app.Catalog.RepriceProposal(ctx, percent)
		if err != nil {
			return RepriceProposalDTO{}, err
		}
		v, err := newTillView(ctx, app)
		if err != nil {
			return RepriceProposalDTO{}, err
		}
		out := RepriceProposalDTO{Items: make([]RepriceItemDTO, 0, len(p.Items))}
		if p.RateNano > 0 {
			out.Rate = rateText(v.shop, p.RateNano)
		}
		for _, it := range p.Items {
			d := v.ref.Currencies[it.Product.PriceCurrency].Decimals
			out.Items = append(out.Items, RepriceItemDTO{ID: it.Product.ID.String(), RowVersion: it.Product.RowVersion,
				NameAR: it.Product.NameAR, NameEN: it.Product.NameEN, Currency: it.Product.PriceCurrency,
				Price:    v.shop.Display(domain.FormatMicro(it.Product.PriceMicro, d), it.Product.PriceCurrency),
				Proposed: v.shop.Display(domain.FormatMicro(it.ProposedMicro, d), it.Product.PriceCurrency),
				Shift:    signedPercent(it.ShiftMicro)})
		}
		return out, nil
	})
}

// BulkReprice applies the prices the owner confirmed — all of them or none. Each is an owner's act recorded with its old
// and new price, as a price changed one at a time would be; one PIN covers the batch.
func (c *Catalog) BulkReprice(in BulkRepriceInput) envelope.Result[[]ProductDTO] {
	return call(c.core, "Catalog.BulkReprice", func(ctx context.Context, app *bootstrap.App) ([]ProductDTO, error) {
		shop, err := moneyShop(ctx, app)
		if err != nil {
			return nil, err
		}
		changes := make([]catalog.RepriceChange, 0, len(in.Items))
		for _, it := range in.Items {
			change, cerr := repriceChange(ctx, app, shop, it)
			if cerr != nil {
				return nil, cerr
			}
			changes = append(changes, change)
		}
		updated, err := app.Catalog.BulkReprice(ctx, changes)
		if err != nil {
			return nil, err
		}
		v, err := loadCatalogueView(ctx, app)
		if err != nil {
			return nil, err
		}
		out := make([]ProductDTO, 0, len(updated))
		for _, p := range updated {
			out = append(out, toProductDTO(p, v))
		}
		return out, nil
	})
}

// repriceChange is one confirmed price as the books hold it. The price the owner saw was in the shop's reading of the
// product's currency; the books hold the base figure (L10).
func repriceChange(ctx context.Context, app *bootstrap.App, shop moneyfmt.Shop, it RepriceChangeInput) (catalog.RepriceChange, error) {
	productID, err := parseProductID(it.ID)
	if err != nil {
		return catalog.RepriceChange{}, err
	}
	current, err := app.Catalog.Get(ctx, productID)
	if err != nil {
		return catalog.RepriceChange{}, err
	}
	return catalog.RepriceChange{ID: productID, RowVersion: it.RowVersion, Price: shop.Base(it.Price, current.PriceCurrency)}, nil
}
