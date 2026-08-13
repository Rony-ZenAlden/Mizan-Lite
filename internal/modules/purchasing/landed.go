package purchasing

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	accountingc "github.com/mizan-erp/mizan/internal/modules/accounting/contract"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/purchasing/domain"
)

// The audited actions a landed cost leaves.
const (
	ActionLandedCostAdded   = "purchasing.landed_cost.added"
	ActionLandedCostApplied = "purchasing.landed_cost.applied"

	EntityLandedCost = "purchasing.landed_cost"
)

// PostingLandedCostApplied is the POSTING-RULE key an applied charge fires.
const PostingLandedCostApplied = "purchasing.landed_cost.applied"

// PermLandedCostManage gates adding and applying landed costs.
const PermLandedCostManage = "purchasing.landed_cost.manage"

// NewLandedCostInput adds a charge to a delivery.
type NewLandedCostInput struct {
	CompanyID   id.ID
	ReceiptID   id.ID
	ChargeType  string
	Description string
	PartnerID   id.ID
	Basis       domain.Basis
	Currency    string
	AmountMinor int64
}

// AddLandedCost attaches a charge to a delivery.
func (s *Service) AddLandedCost(
	ctx context.Context, in NewLandedCostInput,
) (domain.LandedCost, error) {
	identifier, err := id.New()
	if err != nil {
		return domain.LandedCost{}, err
	}

	charge, err := domain.NewLandedCost(identifier, in.CompanyID, in.ReceiptID,
		in.ChargeType, in.Currency, in.Basis, in.AmountMinor)
	if err != nil {
		return domain.LandedCost{}, err
	}
	charge.Description = in.Description
	charge.PartnerID = in.PartnerID

	if err = s.db.Do(ctx, func(txCtx context.Context) error {
		// The delivery must have happened. A charge against a draft delivery would spread across
		// lines that may still change, and the allocation stored would describe a receipt that
		// never existed.
		receipt, receiptErr := s.requireReceipt(txCtx, in.ReceiptID)
		if receiptErr != nil {
			return receiptErr
		}
		if receipt.Status != domain.ReceiptConfirmed {
			return errs.Conflict(domain.CodeNotConfirmed,
				"only a confirmed delivery can carry a landed cost")
		}
		if err = s.repos.InsertLandedCost(txCtx, charge, s.actorOf(txCtx)); err != nil {
			return err
		}
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionLandedCostAdded, EntityType: EntityLandedCost, EntityID: charge.ID,
			After: map[string]any{
				"receipt": string(in.ReceiptID), "type": charge.ChargeType,
				"amount": charge.AmountMinor, "basis": string(charge.Basis),
			},
		})
	}); err != nil {
		return domain.LandedCost{}, err
	}
	return charge, nil
}

// ApplyLandedCost spreads a charge across the goods it brought in.
//
// # What applying does
//
// Splits the charge across the delivery's lines on the declared basis, revalues the stock still
// on hand, records what each line absorbed, and posts. Either all of it or none of it: a charge
// that reached the books but not the stock ledger leaves the two disagreeing about what the same
// goods cost.
//
// # The split by where the goods are, again
//
// The same rule 6.4 applies to a price variance, for the same reason: freight on goods already
// sold cannot revalue them, because they are gone. That part is a correction to costs already
// posted, and §D.4's argument against reopening periods holds here too.
func (s *Service) ApplyLandedCost(
	ctx context.Context, chargeID id.ID,
) (domain.LandedCost, error) {
	if s.stock == nil {
		return domain.LandedCost{}, errs.Internal(CodePortMissing,
			"the purchasing service was built without a stock port")
	}

	var applied domain.LandedCost
	err := s.db.Do(ctx, func(txCtx context.Context) error {
		charge, found, err := s.repos.LandedCostByID(txCtx, chargeID)
		if err != nil {
			return err
		}
		if !found {
			return errs.NotFound(CodeUnknownLandedCost,
				"there is no landed cost with that identity")
		}
		if charge.Status != domain.LandedDraft {
			// Applying twice would double the cost of the goods, and nothing downstream would
			// look wrong — just a margin quietly worse than it should be.
			return errs.Conflict(domain.CodeLandedApplied,
				"this charge has already been applied")
		}

		receipt, err := s.requireReceipt(txCtx, charge.ReceiptID)
		if err != nil {
			return err
		}
		lines, err := s.repos.ReceiptLines(txCtx, charge.ReceiptID)
		if err != nil {
			return err
		}

		allocations, err := domain.AllocateLandedCost(charge.AmountMinor, lines, charge.Basis)
		if err != nil {
			return err
		}

		decimals, err := s.decimalsOf(txCtx, charge.CurrencyCode)
		if err != nil {
			return err
		}

		byLine := make(map[id.ID]domain.ReceiptLine, len(lines))
		for _, line := range lines {
			byLine[line.ID] = line
		}

		var split domain.VarianceSplit
		for i, allocation := range allocations {
			if allocation.AmountMinor == 0 {
				continue
			}
			line := byLine[allocation.ReceiptLineID]

			onHand, stockErr := s.stock.OnHandMicro(txCtx, line.VariantID, receipt.WarehouseID)
			if stockErr != nil {
				return stockErr
			}
			lineSplit := domain.SplitByWhereTheGoodsAre(
				allocation.AmountMinor, line.QuantityStockMicro, onHand)
			split.StockMinor += lineSplit.StockMinor
			split.ExpenseMinor += lineSplit.ExpenseMinor

			if lineSplit.StockMinor != 0 {
				if err = s.stock.Revalue(txCtx, RevaluationRequest{
					CompanyID: charge.CompanyID, WarehouseID: receipt.WarehouseID,
					ProductID: line.ProductID, VariantID: line.VariantID,
					DeltaMinor: lineSplit.StockMinor, Decimals: decimals,
					DocumentType: EntityLandedCost, DocumentID: charge.ID,
					OccurredAt: receipt.ReceiptDate,
				}); err != nil {
					return err
				}
			}
			allocations[i] = allocation
			if err = s.repos.InsertAllocation(txCtx, charge.ID, allocation); err != nil {
				return err
			}
		}

		if err = s.repos.MarkLandedCostApplied(txCtx, charge.ID, s.actorOf(txCtx)); err != nil {
			return err
		}
		charge.Status = domain.LandedApplied

		if err = s.publishLandedCostPosting(txCtx, charge, receipt, split); err != nil {
			return err
		}

		applied = charge
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionLandedCostApplied, EntityType: EntityLandedCost, EntityID: charge.ID,
			After: map[string]any{
				"amount": charge.AmountMinor,
				"stock":  split.StockMinor, "expense": split.ExpenseMinor,
			},
		})
	})
	if err != nil {
		return domain.LandedCost{}, err
	}
	return applied, nil
}

func (s *Service) publishLandedCostPosting(
	ctx context.Context, charge domain.LandedCost,
	receipt domain.Receipt, split domain.VarianceSplit,
) error {
	date, ok := clock.ParseDate(receipt.ReceiptDate)
	if !ok {
		return errs.Internal(domain.CodeInvalidReceipt,
			"a goods receipt has an unreadable date")
	}

	stockOver, _ := domain.SplitVariance(split.StockMinor)
	expenseOver, _ := domain.SplitVariance(split.ExpenseMinor)

	return s.bus.Publish(ctx, accountingc.Postable{
		Action:    PostingLandedCostApplied,
		CompanyID: charge.CompanyID,
		BranchID:  receipt.BranchID,
		Date:      date,

		DocumentType: EntityLandedCost,
		DocumentID:   charge.ID,

		Amounts: map[string]int64{
			accountingc.AmountTotal:               charge.AmountMinor,
			accountingc.AmountVarianceStockOver:   stockOver,
			accountingc.AmountVarianceExpenseOver: expenseOver,
		},

		CurrencyCode: charge.CurrencyCode,
		RateMicro:    1_000_000,
		PartnerID:    charge.PartnerID,
		Memo:         charge.ChargeType,
	})
}

// LandedCostsFor lists a delivery's charges.
func (s *Service) LandedCostsFor(
	ctx context.Context, receiptID id.ID,
) ([]domain.LandedCost, error) {
	return s.repos.LandedCostsFor(ctx, receiptID)
}

// AllocationsOf reports what each line absorbed of a charge.
func (s *Service) AllocationsOf(
	ctx context.Context, chargeID id.ID,
) ([]domain.LandedAllocation, error) {
	return s.repos.AllocationsFor(ctx, chargeID)
}
