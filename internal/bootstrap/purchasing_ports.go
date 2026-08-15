package bootstrap

import (
	"context"

	"github.com/mizan-erp/mizan/internal/api/appctx"
	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/modules/catalog"
	"github.com/mizan-erp/mizan/internal/modules/currency"
	"github.com/mizan-erp/mizan/internal/modules/expenses"
	"github.com/mizan-erp/mizan/internal/modules/inventory"
	inventorydomain "github.com/mizan-erp/mizan/internal/modules/inventory/domain"
	"github.com/mizan-erp/mizan/internal/modules/pricing"
	"github.com/mizan-erp/mizan/internal/modules/purchasing"
	"github.com/mizan-erp/mizan/internal/modules/tax"
	"github.com/mizan-erp/mizan/internal/platform/numbering"
)

// The adapters that satisfy purchasing's ports.
//
// Each is a few lines, which is the return on declaring ports narrower than the services behind
// them: purchasing asks for "tell me what this variant is called", not for the catalog service,
// so this is where the two shapes meet and nowhere else has to know either of them.

// purchasingCatalog answers what a variant is, from the catalog module.
type purchasingCatalog struct{ catalog *catalog.Service }

var _ purchasing.Catalog = purchasingCatalog{}

// FactsFor reads the PURCHASE side of the catalog (3.2's `purchase_uom_id`), so an order for a
// product bought by the drum and sold by the metre defaults to drums.
func (c purchasingCatalog) FactsFor(
	ctx context.Context, companyID, variantID, uomID id.ID, quantityMicro int64,
) (purchasing.ProductFacts, error) {
	product, variant, unit, inStock, err := c.catalog.PurchaseFacts(
		ctx, companyID, variantID, uomID, quantityMicro)
	if err != nil {
		return purchasing.ProductFacts{}, err
	}
	return purchasing.ProductFacts{
		ProductID: product.ID, ProductName: product.Name, VariantSKU: variant.SKU,
		UomID: unit.ID, UomCode: unit.Code, QuantityStockMicro: inStock,
	}, nil
}

// purchasingPricing resolves what we expect to pay.
type purchasingPricing struct{ pricing *pricing.Service }

var _ purchasing.Pricing = purchasingPricing{}

// PurchasePriceFor asks the same resolution sales uses, in the PURCHASE direction — which 3.6
// built and nothing called until now.
func (p purchasingPricing) PurchasePriceFor(
	ctx context.Context, q purchasing.PriceQuery,
) (purchasing.ResolvedPrice, error) {
	resolved, err := p.pricing.Price(ctx, pricing.PriceQuery{
		CompanyID: q.CompanyID, PartnerID: q.PartnerID,
		ProductID: q.ProductID, VariantID: q.VariantID,
		QuantityMicro: q.QuantityMicro, Direction: pricing.Purchase,
	})
	if err != nil {
		return purchasing.ResolvedPrice{}, err
	}
	return purchasing.ResolvedPrice{
		UnitPriceMicro: resolved.PriceMinor,
		ListCode:       resolved.ListCode,
		Source:         string(resolved.Source),
	}, nil
}

// purchasingTax computes what a purchase line is taxed.
type purchasingTax struct {
	tax      *tax.Service
	currency *currency.Service
}

var _ purchasing.Tax = purchasingTax{}

// TaxFor computes a line's tax.
//
// The functional currency, not the document's: a purchase bill's recoverable tax is claimed in
// the currency the books are kept in, and the exchange rate on the document has already brought
// the amounts there.
func (t purchasingTax) TaxFor(
	ctx context.Context, q purchasing.TaxQuery,
) (purchasing.ResolvedTax, error) {
	unit, err := t.currency.Functional(ctx)
	if err != nil {
		return purchasing.ResolvedTax{}, err
	}
	date, ok := clock.ParseDate(q.Date)
	if !ok {
		date = clock.System().Now()
	}

	quote, err := t.tax.Calculate(ctx, tax.Request{
		CompanyID: q.CompanyID, PartnerID: q.PartnerID,
		Date:   date,
		Amount: money.FromMinor(unit, q.NetMinor),
	})
	if err != nil {
		return purchasing.ResolvedTax{}, err
	}

	var rateMicro int64
	for _, component := range quote.Components {
		rateMicro += component.RateMicro
	}
	return purchasing.ResolvedTax{
		AmountMinor: quote.Tax.Minor(), RateMicro: rateMicro, Code: quote.GroupCode,
	}, nil
}

// purchasingStock moves goods, from the inventory module.
type purchasingStock struct{ inventory *inventory.Service }

var _ purchasing.Stock = purchasingStock{}

// Receive takes goods in at what the order said they cost.
func (s purchasingStock) Receive(
	ctx context.Context, r purchasing.StockRequest,
) (purchasing.StockResult, error) {
	return s.move(ctx, r, inventorydomain.Receipt)
}

// ReturnToSupplier sends goods back at what the ORIGINAL delivery cost (§D.3).
func (s purchasingStock) ReturnToSupplier(
	ctx context.Context, r purchasing.StockRequest,
) (purchasing.StockResult, error) {
	return s.move(ctx, r, inventorydomain.ReturnOut)
}

func (s purchasingStock) move(
	ctx context.Context, r purchasing.StockRequest, kind inventorydomain.Type,
) (purchasing.StockResult, error) {
	moved, err := s.inventory.Move(ctx, inventory.MoveInput{
		CompanyID: r.CompanyID, WarehouseID: r.WarehouseID,
		ProductID: r.ProductID, VariantID: r.VariantID,
		Type: kind, QuantityMicro: r.QuantityMicro, UnitCostMicro: r.UnitCostMicro,
		LotID: r.LotID, SerialID: r.SerialID,
		SourceMovementID: r.SourceMovementID,
		DocumentType:     r.DocumentType, DocumentID: r.DocumentID,
		DocumentLineID: r.DocumentLineID, OccurredAt: r.OccurredAt,
	})
	if err != nil {
		return purchasing.StockResult{}, err
	}
	return purchasing.StockResult{
		MovementID: moved.ID, UnitCostMicro: moved.UnitCostMicro, ValueMinor: moved.ValueMinor,
	}, nil
}

// Revalue changes what stock is worth without moving any — Phase 4's `Revaluation`, which had no
// caller until 6.4's price variance.
func (s purchasingStock) Revalue(
	ctx context.Context, r purchasing.RevaluationRequest,
) error {
	_, err := s.inventory.RevalueBy(ctx, inventory.RevalueInput{
		CompanyID: r.CompanyID, WarehouseID: r.WarehouseID,
		ProductID: r.ProductID, VariantID: r.VariantID,
		DeltaMinor: r.DeltaMinor, Decimals: r.Decimals,
		DocumentType: r.DocumentType, DocumentID: r.DocumentID, OccurredAt: r.OccurredAt,
	})
	return err
}

// OnHandMicro reports how much of a variant is in a warehouse now.
func (s purchasingStock) OnHandMicro(
	ctx context.Context, variantID, warehouseID id.ID,
) (int64, error) {
	state, err := s.inventory.StockOf(ctx, variantID, warehouseID)
	if err != nil {
		return 0, err
	}
	return state.OnHandMicro, nil
}

// purchasingNumbering allocates document numbers from the PLATFORM's allocator.
//
// The one 6.1 moved out of sales precisely so this could exist: purchasing cannot import sales,
// and two allocators against one table would each be correct alone and race together.
type purchasingNumbering struct{ allocator *numbering.Allocator }

var _ purchasing.Numbering = purchasingNumbering{}

func (n purchasingNumbering) Allocate(
	ctx context.Context, branchID id.ID, seriesCode string,
) (string, error) {
	return n.allocator.Allocate(ctx, branchID, seriesCode)
}

// purchasingActors reports who is acting, from the request context.
type purchasingActors struct{}

var _ purchasing.ActorResolver = purchasingActors{}

func (purchasingActors) Actor(ctx context.Context) (purchasing.Actor, bool) {
	a, ok := appctx.ActorFrom(ctx)
	if !ok || a.UserID.IsZero() {
		return purchasing.Actor{}, false
	}
	return purchasing.Actor{UserID: a.UserID, BranchID: a.BranchID}, true
}

// ── expenses ────────────────────────────────────────────────────────────────────

// expensesTax computes what an expense line is taxed.
type expensesTax struct {
	tax      *tax.Service
	currency *currency.Service
}

var _ expenses.Tax = expensesTax{}

func (t expensesTax) TaxFor(
	ctx context.Context, q expenses.TaxQuery,
) (expenses.ResolvedTax, error) {
	unit, err := t.currency.Functional(ctx)
	if err != nil {
		return expenses.ResolvedTax{}, err
	}
	date, ok := clock.ParseDate(q.Date)
	if !ok {
		date = clock.System().Now()
	}

	quote, err := t.tax.Calculate(ctx, tax.Request{
		CompanyID: q.CompanyID, PartnerID: q.PartnerID,
		Date:   date,
		Amount: money.FromMinor(unit, q.NetMinor),
	})
	if err != nil {
		return expenses.ResolvedTax{}, err
	}

	var rateMicro int64
	for _, component := range quote.Components {
		rateMicro += component.RateMicro
	}
	return expenses.ResolvedTax{
		AmountMinor: quote.Tax.Minor(), RateMicro: rateMicro, Code: quote.GroupCode,
	}, nil
}

// expensesActors reports who is acting.
type expensesActors struct{}

var _ expenses.ActorResolver = expensesActors{}

func (expensesActors) Actor(ctx context.Context) (expenses.Actor, bool) {
	a, ok := appctx.ActorFrom(ctx)
	if !ok || a.UserID.IsZero() {
		return expenses.Actor{}, false
	}
	return expenses.Actor{UserID: a.UserID, BranchID: a.BranchID}, true
}
