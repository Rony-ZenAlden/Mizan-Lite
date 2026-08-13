package purchasing

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/purchasing/domain"
)

// NewOrderInput opens a purchase order.
type NewOrderInput struct {
	CompanyID         id.ID
	BranchID          id.ID
	WarehouseID       id.ID
	PartnerID         id.ID
	PartnerName       string
	OrderDate         string
	ExpectedDate      string
	Currency          string
	SupplierReference string
	Notes             string
}

// Draft opens a purchase order.
func (s *Service) Draft(ctx context.Context, in NewOrderInput) (domain.Order, error) {
	identifier, err := id.New()
	if err != nil {
		return domain.Order{}, err
	}

	order, err := domain.NewOrder(identifier, in.CompanyID, in.BranchID, in.WarehouseID,
		in.PartnerID, in.PartnerName, in.OrderDate, in.Currency)
	if err != nil {
		return domain.Order{}, err
	}
	order.ExpectedDate = in.ExpectedDate
	order.SupplierReference = in.SupplierReference
	order.Notes = in.Notes

	if err = s.db.Do(ctx, func(txCtx context.Context) error {
		if err = s.repos.InsertOrder(txCtx, order, s.actorOf(txCtx)); err != nil {
			return err
		}
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionOrderDrafted, EntityType: EntityOrder, EntityID: order.ID,
			After: map[string]any{
				"partner": order.PartnerName, "date": order.OrderDate,
			},
		})
	}); err != nil {
		return domain.Order{}, err
	}
	return order, nil
}

// AddLineInput puts something on an order.
//
// # There is no price field
//
// The caller says WHAT and HOW MANY; the price is resolved from the purchase price lists at
// posting, the same way a sale's is. A buyer who can type any price into an order is a purchase
// nobody agreed, and the three-way match has nothing to match the bill against.
type AddLineInput struct {
	CompanyID     id.ID
	OrderID       id.ID
	VariantID     id.ID
	UomID         id.ID
	QuantityMicro int64
	SupplierCode  string
	Notes         string
}

// AddLine puts something on a draft order.
func (s *Service) AddLine(ctx context.Context, in AddLineInput) (domain.Line, error) {
	if s.catalog == nil {
		return domain.Line{}, errs.Internal(CodePortMissing,
			"the purchasing service was built without a catalog")
	}

	var line domain.Line
	err := s.db.Do(ctx, func(txCtx context.Context) error {
		order, err := s.requireOrder(txCtx, in.OrderID)
		if err != nil {
			return err
		}
		if err = order.RequireDraft(); err != nil {
			return err
		}

		// The catalog answers what this thing is called and how the ordered unit converts to
		// stock. Both are SNAPSHOTTED, because a line must say what was ordered rather than what
		// the product is called now (§9.3).
		facts, err := s.catalog.FactsFor(
			txCtx, in.CompanyID, in.VariantID, in.UomID, in.QuantityMicro)
		if err != nil {
			return err
		}

		position, err := s.repos.NextLineNumber(txCtx, in.OrderID)
		if err != nil {
			return err
		}

		identifier, err := id.New()
		if err != nil {
			return err
		}
		line, err = domain.NewLine(identifier, facts.ProductID, in.VariantID, facts.UomID,
			position, in.QuantityMicro, facts.QuantityStockMicro)
		if err != nil {
			return err
		}
		line.ProductName = facts.ProductName
		line.VariantSKU = facts.VariantSKU
		line.UomCode = facts.UomCode
		line.SupplierCode = in.SupplierCode
		line.Notes = in.Notes

		if err = s.repos.InsertLine(txCtx, in.OrderID, line); err != nil {
			return err
		}
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionOrderLineAdded, EntityType: EntityOrder, EntityID: in.OrderID,
			After: map[string]any{
				"variant": string(in.VariantID), "quantity": line.QuantityMicro,
			},
		})
	})
	if err != nil {
		return domain.Line{}, err
	}
	return line, nil
}

// RemoveLine takes something off a draft order.
func (s *Service) RemoveLine(ctx context.Context, orderID, lineID id.ID) error {
	return s.db.Do(ctx, func(txCtx context.Context) error {
		order, err := s.requireOrder(txCtx, orderID)
		if err != nil {
			return err
		}
		if err = order.RequireDraft(); err != nil {
			return err
		}
		if err = s.repos.DeleteLine(txCtx, lineID); err != nil {
			return err
		}
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionOrderLineRemoved, EntityType: EntityOrder, EntityID: orderID,
			Before: map[string]any{"line_id": string(lineID)},
		})
	})
}

// Place prices an order, numbers it, and sends it.
//
// # What placing does and deliberately does not do
//
// It resolves prices and tax, computes the totals, allocates a number, and marks the order
// placed. It moves NO STOCK and writes NO JOURNAL ENTRY — an intention is not a transaction, and
// a system that books one has committed a business to a purchase it can still walk away from.
//
// §9.4's rule — an abandoned order consumes no number — is kept by the TRANSACTION, not by where
// the allocation sits. A 6.1 drill proved it: moving the allocation to the top, before everything
// that can fail, changed no test, because the counter advance rolls back with the rest.
func (s *Service) Place(ctx context.Context, orderID id.ID) (domain.Order, error) {
	if s.pricing == nil || s.tax == nil {
		return domain.Order{}, errs.Internal(CodePortMissing,
			"the purchasing service was built without pricing or tax")
	}

	var placed domain.Order
	err := s.db.Do(ctx, func(txCtx context.Context) error {
		order, err := s.requireOrder(txCtx, orderID)
		if err != nil {
			return err
		}
		if err = order.RequireDraft(); err != nil {
			return err
		}

		lines, err := s.repos.Lines(txCtx, orderID)
		if err != nil {
			return err
		}
		if len(lines) == 0 {
			return errs.Validation(domain.CodeNoLines,
				"an order with no lines has nothing to ask for")
		}

		decimals, err := s.decimalsOf(txCtx, order.CurrencyCode)
		if err != nil {
			return err
		}

		for i, line := range lines {
			priced, priceErr := s.priceLine(txCtx, order, line, decimals)
			if priceErr != nil {
				return priceErr
			}
			if err = s.repos.UpdateLine(txCtx, priced); err != nil {
				return err
			}
			lines[i] = priced
		}

		order.NetMinor, order.TaxMinor, order.DiscountMinor, order.TotalMinor =
			domain.Totals(lines)
		order.Status = domain.Placed

		// Allocated last, but the ORDERING is not what protects the number — the TRANSACTION is.
		//
		// A drill in 6.1 moved this allocation to the very top, before anything that can fail,
		// and no test changed: `AdvanceSeries` runs inside `db.Do`, so a rollback takes the
		// counter back with everything else. The earlier comment here claimed the ordering was
		// "§9.4's rule expressed as an ordering", which overclaimed — §9.4 is kept by the
		// transaction, and this ordering only avoids a pointless counter write on the failure
		// path.
		//
		// No test pins the ordering, deliberately: a test that passes either way claims
		// something is pinned when nothing is (4.3's rule).
		if order.Number, err = s.allocate(txCtx, order.BranchID, SeriesOrder); err != nil {
			return err
		}
		if err = s.repos.UpdateOrder(txCtx, order, s.actorOf(txCtx)); err != nil {
			return err
		}

		placed = order
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionOrderPlaced, EntityType: EntityOrder, EntityID: orderID,
			After: map[string]any{"number": order.Number, "total": order.TotalMinor},
		})
	})
	if err != nil {
		return domain.Order{}, err
	}
	return placed, nil
}

func (s *Service) priceLine(
	ctx context.Context, order domain.Order, line domain.Line, decimals int,
) (domain.Line, error) {
	resolved, err := s.pricing.PurchasePriceFor(ctx, PriceQuery{
		CompanyID: order.CompanyID, PartnerID: order.PartnerID,
		ProductID: line.ProductID, VariantID: line.VariantID,
		QuantityMicro: line.QuantityMicro,
	})
	if err != nil {
		return domain.Line{}, err
	}

	priced, err := line.Price(resolved.UnitPriceMicro, line.DiscountMinor, decimals)
	if err != nil {
		return domain.Line{}, err
	}

	taxed, err := s.tax.TaxFor(ctx, TaxQuery{
		CompanyID: order.CompanyID, PartnerID: order.PartnerID,
		ProductID: line.ProductID, NetMinor: priced.NetMinor, Date: order.OrderDate,
	})
	if err != nil {
		return domain.Line{}, err
	}
	return priced.Tax(taxed.AmountMinor, taxed.RateMicro, taxed.Code), nil
}

// Cancel abandons a draft order.
func (s *Service) Cancel(ctx context.Context, orderID id.ID) error {
	return s.db.Do(ctx, func(txCtx context.Context) error {
		order, err := s.requireOrder(txCtx, orderID)
		if err != nil {
			return err
		}
		if err = order.RequireDraft(); err != nil {
			return err
		}
		order.Status = domain.Cancelled
		if err = s.repos.UpdateOrder(txCtx, order, s.actorOf(txCtx)); err != nil {
			return err
		}
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionOrderCancelled, EntityType: EntityOrder, EntityID: orderID,
		})
	})
}

// Close stops an order receiving more goods.
//
// Separate from cancelling, and the difference matters: a cancelled order never happened, while a
// closed one happened and is finished with. A supplier who short-delivers and will not send the
// rest leaves an order that is closed with an outstanding quantity — which is a fact worth
// keeping, not an error to erase.
func (s *Service) Close(ctx context.Context, orderID id.ID) error {
	return s.db.Do(ctx, func(txCtx context.Context) error {
		order, err := s.requireOrder(txCtx, orderID)
		if err != nil {
			return err
		}
		if order.Status != domain.Placed {
			return errs.Conflict(domain.CodeNotDraft,
				"only a placed order can be closed")
		}
		order.Status = domain.Closed
		if err = s.repos.UpdateOrder(txCtx, order, s.actorOf(txCtx)); err != nil {
			return err
		}
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionOrderClosed, EntityType: EntityOrder, EntityID: orderID,
		})
	})
}

// Order reads one order with its lines.
func (s *Service) Order(
	ctx context.Context, orderID id.ID,
) (domain.Order, []domain.Line, error) {
	order, _, found, err := s.repos.OrderByID(ctx, orderID)
	if err != nil {
		return domain.Order{}, nil, err
	}
	if !found {
		return domain.Order{}, nil, errs.NotFound(CodeUnknownOrder,
			"there is no purchase order with that identity")
	}
	lines, err := s.repos.Lines(ctx, orderID)
	if err != nil {
		return domain.Order{}, nil, err
	}
	return order, lines, nil
}

// Orders lists a company's purchase orders.
func (s *Service) Orders(
	ctx context.Context, companyID id.ID, status domain.Status,
) ([]domain.Order, error) {
	return s.repos.Orders(ctx, companyID, status)
}

func (s *Service) requireOrder(ctx context.Context, orderID id.ID) (domain.Order, error) {
	order, _, found, err := s.repos.OrderByID(ctx, orderID)
	if err != nil {
		return domain.Order{}, err
	}
	if !found {
		return domain.Order{}, errs.NotFound(CodeUnknownOrder,
			"there is no purchase order with that identity")
	}
	return order, nil
}

func (s *Service) allocate(ctx context.Context, branchID id.ID, series string) (string, error) {
	if s.numbers == nil {
		return "", errs.Internal(CodePortMissing,
			"the purchasing service was built without a numbering port")
	}
	return s.numbers.Allocate(ctx, branchID, series)
}

// decimalsOf reports a currency's minor-unit scale.
//
// Read rather than assumed to be 2. A currency with no minor unit priced to two decimals
// multiplies every figure on the order by a hundred.
func (s *Service) decimalsOf(ctx context.Context, code string) (int, error) {
	return s.repos.CurrencyDecimals(ctx, code)
}
