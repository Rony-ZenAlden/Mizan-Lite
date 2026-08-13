package purchasing

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// The ports purchasing needs from other modules.
//
// # Each is narrower than the service behind it
//
// The same discipline sales used, and for the same reason: a port that mirrored the whole
// catalog service would make this module depend on every future change to it. What purchasing
// needs from the catalog is "tell me what this variant is called and how its units convert" —
// four fields — and that is the whole interface.
//
// They are satisfied in the composition root, where the concrete services already exist, so no
// module imports another (`module-isolation`).

// Catalog answers what a variant is and how its units convert.
type Catalog interface {
	// FactsFor returns the snapshot a line needs, and the stock-unit quantity the ordered
	// quantity converts to.
	FactsFor(
		ctx context.Context, companyID, variantID, uomID id.ID, quantityMicro int64,
	) (ProductFacts, error)
}

// ProductFacts is everything a purchase line snapshots about a product (§9.3).
type ProductFacts struct {
	ProductID   id.ID
	ProductName string
	VariantSKU  string
	UomID       id.ID
	UomCode     string
	// QuantityStockMicro is the ordered quantity converted into the product's STOCK unit.
	//
	// Both are stored on the line. The day one is derived from the other is the day "2 drums"
	// and "200 metres" stop agreeing on a document already sent to a supplier.
	QuantityStockMicro int64
}

// Pricing answers what we expect to pay.
//
// The same resolution sales uses, in the PURCHASE direction — which 3.6 built and nothing has
// called. A purchase price gets the same tiers, dates, and partner scoping a sale price gets,
// because "this supplier charges us less for a pallet" is the same shape of fact as "this
// customer pays less for a pallet".
type Pricing interface {
	PurchasePriceFor(ctx context.Context, q PriceQuery) (ResolvedPrice, error)
}

// PriceQuery asks what a variant costs from a supplier.
type PriceQuery struct {
	CompanyID     id.ID
	PartnerID     id.ID
	ProductID     id.ID
	VariantID     id.ID
	QuantityMicro int64
}

// ResolvedPrice is a price and WHY it is that price.
type ResolvedPrice struct {
	UnitPriceMicro int64
	ListCode       string
	Source         string
}

// Tax computes what a purchase line is taxed.
type Tax interface {
	TaxFor(ctx context.Context, q TaxQuery) (ResolvedTax, error)
}

// TaxQuery asks what a line is taxed.
type TaxQuery struct {
	CompanyID id.ID
	PartnerID id.ID
	ProductID id.ID
	NetMinor  int64
	Date      string
}

// ResolvedTax is the tax and the rate that produced it.
type ResolvedTax struct {
	AmountMinor int64
	RateMicro   int64
	Code        string
}

// Stock moves goods. Receipts inward, supplier returns outward.
type Stock interface {
	Receive(ctx context.Context, r StockRequest) (StockResult, error)
	ReturnToSupplier(ctx context.Context, r StockRequest) (StockResult, error)
	Revalue(ctx context.Context, r RevaluationRequest) error
	// OnHandMicro reports how much of a variant is in a warehouse now.
	//
	// Needed to decide how much of a price correction can still reach the shelf. It is a
	// QUANTITY, not a value: purchasing must not learn what the goods are carried at, because
	// that is the costing strategy's business.
	OnHandMicro(ctx context.Context, variantID, warehouseID id.ID) (int64, error)
}

// StockRequest is one movement purchasing asks for.
type StockRequest struct {
	CompanyID   id.ID
	WarehouseID id.ID
	ProductID   id.ID
	VariantID   id.ID
	// QuantityMicro is in the product's STOCK unit.
	QuantityMicro int64
	// UnitCostMicro is what the goods cost, which only a RECEIPT supplies — an issue is costed
	// by the costing strategy, and purchasing must not learn which one is in use.
	UnitCostMicro int64
	LotID         id.ID
	SerialID      id.ID

	DocumentType   string
	DocumentID     id.ID
	DocumentLineID id.ID
	OccurredAt     string

	// SourceMovementID is the receipt a supplier return reverses. Required for a return (§D.3),
	// because it must be costed at the ORIGINAL receipt's cost rather than today's average.
	SourceMovementID id.ID
}

// StockResult is what the movement turned out to be worth.
type StockResult struct {
	MovementID    id.ID
	UnitCostMicro int64
	ValueMinor    int64
}

// RevaluationRequest changes what stock is worth without moving any.
//
// Phase 4 built `domain.Revaluation` in 4.2 — Neutral direction, value without quantity — and
// nothing has called it. A bill that disagrees with the order it bills is its first caller.
type RevaluationRequest struct {
	CompanyID   id.ID
	WarehouseID id.ID
	ProductID   id.ID
	VariantID   id.ID
	// DeltaMinor is how much more (or less) the stock on hand is worth. A VALUE, not a new
	// average: purchasing states what changed and inventory decides what that does to the
	// average, which is the whole point of the costing port.
	DeltaMinor int64
	// Decimals is the currency's minor-unit scale.
	Decimals     int
	DocumentType string
	DocumentID   id.ID
	OccurredAt   string
}

// Numbering allocates a document number.
//
// A PORT onto sales' allocator rather than a second implementation. §9.4's guarantee is
// "unique and sequential, not gapless", and it is kept by a transactional read-modify-write on
// one row — two allocators against the same table would each be correct alone and race together.
type Numbering interface {
	Allocate(ctx context.Context, branchID id.ID, seriesCode string) (string, error)
}
