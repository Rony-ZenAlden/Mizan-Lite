package bootstrap

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/modules/currency"
	"github.com/mizan-erp/mizan/internal/modules/inventory"
	inventorydomain "github.com/mizan-erp/mizan/internal/modules/inventory/domain"
	"github.com/mizan-erp/mizan/internal/modules/partner"
	"github.com/mizan-erp/mizan/internal/modules/pricing"
	"github.com/mizan-erp/mizan/internal/modules/sales"
	"github.com/mizan-erp/mizan/internal/modules/tax"
)

// The four adapters that let sales post.
//
// Each is a few lines, and that is the point: the ports sales declared are narrower than the
// services behind them, so the composition root is where the two shapes meet and neither module
// learns the other's vocabulary (§10.3).

// salesPricing answers what a customer pays, from the pricing module.
type salesPricing struct{ pricing *pricing.Service }

var _ sales.Pricing = salesPricing{}

// PriceFor resolves a line's price and reports which list answered.
func (p salesPricing) PriceFor(
	ctx context.Context, q sales.PriceQuery,
) (sales.ResolvedPrice, error) {
	resolved, err := p.pricing.Price(ctx, pricing.PriceQuery{
		CompanyID: q.CompanyID, PartnerID: q.PartnerID, BranchID: q.BranchID,
		ProductID: q.ProductID, VariantID: q.VariantID,
		QuantityMicro: q.QuantityMicro, Direction: pricing.Sale, Date: q.Date,
	})
	if err != nil {
		return sales.ResolvedPrice{}, err
	}
	return sales.ResolvedPrice{
		UnitPriceMinor: resolved.PriceMinor,
		ListCode:       resolved.ListCode,
		Source:         string(resolved.Source),
	}, nil
}

// salesTax answers what the authority is owed, from the tax module.
type salesTax struct {
	tax      *tax.Service
	currency *currency.Service
}

var _ sales.Tax = salesTax{}

// TaxFor computes a line's tax.
//
// The tax engine takes MONEY, which needs a currency to know its decimal places. It comes from
// the DOCUMENT rather than the company: an invoice raised in USD rounds to cents whatever the
// books are kept in.
func (t salesTax) TaxFor(ctx context.Context, q sales.TaxQuery) (sales.ResolvedTax, error) {
	unit, err := t.currency.Get(ctx, q.CurrencyCode)
	if err != nil {
		return sales.ResolvedTax{}, err
	}
	date, ok := clock.ParseDate(q.Date)
	if !ok {
		date = clock.System().Now()
	}

	quote, err := t.tax.Calculate(ctx, tax.Request{
		CompanyID: q.CompanyID, BranchID: q.BranchID, PartnerID: q.PartnerID,
		Date:   date,
		Amount: money.FromMinor(unit, q.NetMinor),
	})
	if err != nil {
		return sales.ResolvedTax{}, err
	}

	// The RATE the line will store. Summed across components, because a line taxed by two
	// compounding taxes was taxed at their combined effect — and one number is what a reprint
	// has room for.
	var rateMicro int64
	for _, component := range quote.Components {
		rateMicro += component.RateMicro
	}

	return sales.ResolvedTax{
		AmountMinor: quote.Tax.Minor(), RateMicro: rateMicro, Code: quote.GroupCode,
	}, nil
}

// salesStock moves goods and reports what they cost, from the inventory module.
type salesStock struct{ inventory *inventory.Service }

var _ sales.Stock = salesStock{}

// Issue takes goods out and returns what they cost us.
func (s salesStock) Issue(
	ctx context.Context, m sales.StockRequest,
) (sales.StockResult, error) {
	return s.move(ctx, m, inventorydomain.Issue)
}

// Return puts goods back, costed at the original issue's cost (§D.3).
func (s salesStock) Return(
	ctx context.Context, m sales.StockRequest,
) (sales.StockResult, error) {
	return s.move(ctx, m, inventorydomain.ReturnIn)
}

func (s salesStock) move(
	ctx context.Context, m sales.StockRequest, kind inventorydomain.Type,
) (sales.StockResult, error) {
	moved, err := s.inventory.Move(ctx, inventory.MoveInput{
		CompanyID: m.CompanyID, WarehouseID: m.WarehouseID,
		ProductID: m.ProductID, VariantID: m.VariantID,
		Type: kind, QuantityMicro: m.QuantityMicro,
		LotID: m.LotID, SerialID: m.SerialID,
		SourceMovementID: m.SourceMovementID,
		DocumentType:     m.DocumentType, DocumentID: m.DocumentID,
		DocumentLineID: m.DocumentLineID, OccurredAt: m.OccurredAt,
	})
	if err != nil {
		return sales.StockResult{}, err
	}
	return sales.StockResult{
		MovementID: moved.ID, UnitCostMicro: moved.UnitCostMicro, ValueMinor: moved.ValueMinor,
	}, nil
}

// salesCredit answers whether a customer may owe more, from the partner module.
type salesCredit struct{ partner *partner.Service }

var _ sales.Credit = salesCredit{}

// CheckCredit reports whether a further amount would breach a partner's limit.
//
// The OUTSTANDING balance is zero here, and deliberately so: what a customer already owes lives
// in the receivables ledger, which Phase 7 builds. Until then the check catches a single sale
// that exceeds the limit outright, which is the common case and better than nothing — and the
// limit's "zero means no limit" rule is already the partner domain's, so this cannot get that
// half wrong.
func (c salesCredit) CheckCredit(
	ctx context.Context, companyID, partnerID id.ID, additionalMinor int64,
) error {
	found, err := c.partner.PartnerByID(ctx, companyID, partnerID)
	if err != nil {
		return err
	}
	return found.CheckCredit(0, additionalMinor)
}
