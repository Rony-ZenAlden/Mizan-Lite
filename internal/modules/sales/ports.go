package sales

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// The ports posting needs.
//
// # Why these are interfaces rather than imports
//
// §10.3: a module reaches another only through its contract package. Sales needs four narrow
// facts at posting — what a thing costs the customer, what tax applies, what it costs US, and
// whether the customer may owe more — and each lives in a module that already knows how to
// answer it.
//
// The shapes are deliberately smaller than the services behind them. `Pricing` here is one
// method; `pricing.Service` has eight. A port that mirrored the service would make every future
// change to that service a change here, which is the coupling the rule exists to prevent.
//
// They also make posting testable against fakes that fit in a few lines — which matters, because
// posting is the most complex transaction in the system and its failure modes need to be
// reachable without standing up five modules.

// Pricing answers what a customer pays.
type Pricing interface {
	// PriceFor resolves one line's unit price and reports which list answered.
	//
	// The REASON crosses with the price because §2.6 of Phase 3 requires resolution to record
	// it, and because a salesperson who cannot explain a price will override it by hand.
	PriceFor(ctx context.Context, q PriceQuery) (ResolvedPrice, error)
}

// PriceQuery is what resolving a price needs.
type PriceQuery struct {
	CompanyID     id.ID
	PartnerID     id.ID
	BranchID      id.ID
	ProductID     id.ID
	VariantID     id.ID
	QuantityMicro int64
	Date          string
}

// ResolvedPrice is a price and the reason for it.
type ResolvedPrice struct {
	UnitPriceMinor int64
	ListCode       string
	Source         string
}

// Tax answers what the authority is owed.
type Tax interface {
	// TaxFor computes the tax on one line's net amount.
	//
	// Returns the RATE as well as the amount, because the line stores the rate: a group's rate
	// changes, and what this line was taxed at does not.
	TaxFor(ctx context.Context, q TaxQuery) (ResolvedTax, error)
}

// TaxQuery is what computing tax needs.
type TaxQuery struct {
	CompanyID id.ID
	BranchID  id.ID
	PartnerID id.ID
	ProductID id.ID
	NetMinor  int64
	// CurrencyCode is the DOCUMENT's currency, not the company's functional one. Tax arithmetic
	// needs to know how many decimal places it is rounding to, and an invoice raised in USD
	// rounds to cents whatever the books are kept in.
	CurrencyCode string
	Date         string
}

// ResolvedTax is a tax amount and the rate that produced it.
type ResolvedTax struct {
	AmountMinor int64
	RateMicro   int64
	Code        string
}

// Stock moves goods and reports what they cost.
type Stock interface {
	// Issue takes goods out of a warehouse and returns what they cost us.
	//
	// The COST comes back because only inventory knows it — the costing port (4.2) decides
	// whether the answer is an average or a layer, and sales must not learn which.
	Issue(ctx context.Context, m StockRequest) (StockResult, error)
	// Return puts goods back, costed at the ORIGINAL issue's cost (§D.3).
	Return(ctx context.Context, m StockRequest) (StockResult, error)
}

// StockRequest is one line's movement.
type StockRequest struct {
	CompanyID   id.ID
	WarehouseID id.ID
	ProductID   id.ID
	VariantID   id.ID
	// QuantityMicro is in the product's STOCK unit — the converted figure, not what the customer
	// bought in.
	QuantityMicro int64
	LotID         id.ID
	SerialID      id.ID

	DocumentType   string
	DocumentID     id.ID
	DocumentLineID id.ID
	OccurredAt     string

	// SourceMovementID is the issue a return reverses. Required for a return (§D.3).
	SourceMovementID id.ID
}

// StockResult is what a movement cost and which movement it was.
type StockResult struct {
	MovementID id.ID
	// UnitCostMicro is what the goods cost US, per stock unit, at the moment of the movement.
	UnitCostMicro int64
	ValueMinor    int64
}

// Credit answers whether a customer may owe more.
type Credit interface {
	// CheckCredit reports whether a further amount would breach a partner's limit.
	//
	// Nil for a walk-in customer, who has no record and therefore no limit — which is most of a
	// shop's trade and must not require one.
	CheckCredit(ctx context.Context, companyID, partnerID id.ID, additionalMinor int64) error
}
