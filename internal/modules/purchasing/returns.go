package purchasing

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/round"
	accountingc "github.com/mizan-erp/mizan/internal/modules/accounting/contract"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/purchasing/domain"
)

// The audited actions a supplier return leaves.
const (
	ActionReturnDrafted   = "purchasing.return.drafted"
	ActionReturnLineAdded = "purchasing.return.line_added"
	ActionReturnPosted    = "purchasing.return.posted"
	ActionReturnCancelled = "purchasing.return.cancelled"

	EntityReturn = "purchasing.return"
)

// PostingReturnPosted is the POSTING-RULE key a posted return fires.
const PostingReturnPosted = "purchasing.return.posted"

// The permissions a supplier return needs.
const (
	PermReturnView = "purchasing.return.view"
	PermReturnPost = "purchasing.return.post"
)

// SeriesReturn numbers debit notes, separately from bills.
//
// Their own sequence, for the reason invoices and credit notes have theirs in sales: a debit note
// and a bill are different documents to an auditor, and sharing a counter would make bill numbers
// jump every time goods went back.
const SeriesReturn = "SUPPLIER_RETURN"

// NewReturnInput opens a supplier return.
type NewReturnInput struct {
	CompanyID         id.ID
	BranchID          id.ID
	WarehouseID       id.ID
	PartnerID         id.ID
	PartnerName       string
	ReturnDate        string
	Reason            string
	SupplierReference string
	Currency          string
}

// DraftReturn opens a supplier return.
func (s *Service) DraftReturn(
	ctx context.Context, in NewReturnInput,
) (domain.SupplierReturn, error) {
	identifier, err := id.New()
	if err != nil {
		return domain.SupplierReturn{}, err
	}

	built, err := domain.NewSupplierReturn(identifier, in.CompanyID, in.BranchID,
		in.WarehouseID, in.PartnerID, in.PartnerName, in.ReturnDate, in.Currency)
	if err != nil {
		return domain.SupplierReturn{}, err
	}
	built.Reason = in.Reason
	built.SupplierReference = in.SupplierReference

	if err = s.db.Do(ctx, func(txCtx context.Context) error {
		if err = s.repos.InsertReturn(txCtx, built, s.actorOf(txCtx)); err != nil {
			return err
		}
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionReturnDrafted, EntityType: EntityReturn, EntityID: built.ID,
			After: map[string]any{
				"supplier": built.PartnerName, "reason": built.Reason,
			},
		})
	}); err != nil {
		return domain.SupplierReturn{}, err
	}
	return built, nil
}

// ReturnLineInput sends part of a delivery back.
type ReturnLineInput struct {
	CompanyID     id.ID
	ReturnID      id.ID
	ReceiptLineID id.ID
	// QuantityMicro is what is going back, in the delivery's unit. PARTIAL returns are ordinary:
	// two of ten arrived damaged.
	QuantityMicro int64
	Notes         string
}

// AddReturnLine sends part of a delivery back.
func (s *Service) AddReturnLine(
	ctx context.Context, in ReturnLineInput,
) (domain.ReturnLine, error) {
	var line domain.ReturnLine

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		document, err := s.requireReturn(txCtx, in.ReturnID)
		if err != nil {
			return err
		}
		if err = document.RequireDraft(); err != nil {
			return err
		}

		receiptLine, receiptID, found, err := s.repos.ReceiptLineByID(txCtx, in.ReceiptLineID)
		if err != nil {
			return err
		}
		if !found {
			return errs.NotFound(CodeUnknownLine,
				"there is no delivery line with that identity")
		}
		receipt, err := s.requireReceipt(txCtx, receiptID)
		if err != nil {
			return err
		}
		if receipt.Status != domain.ReceiptConfirmed {
			return errs.Conflict(domain.CodeNotConfirmed,
				"only goods that were actually received can go back")
		}
		if receipt.PartnerID != document.PartnerID {
			return errs.Validation(domain.CodeInvalidLine,
				"that delivery came from a different supplier")
		}

		// Bounded by what arrived and by what has already gone back. Each return looks
		// reasonable alone; only the running total shows fourteen of ten leaving.
		already, err := s.repos.AlreadyReturnedMicro(txCtx, in.ReceiptLineID)
		if err != nil {
			return err
		}
		if err = domain.RequireReturnable(
			receiptLine.QuantityMicro, already, in.QuantityMicro); err != nil {
			return err
		}

		position, err := s.repos.NextReturnLineNumber(txCtx, in.ReturnID)
		if err != nil {
			return err
		}
		identifier, err := id.New()
		if err != nil {
			return err
		}

		// The stock quantity moves in proportion to what is going back, so a partial return of a
		// line entered in kilograms sends back the right number of grams.
		stockMicro := scaleQuantity(
			receiptLine.QuantityStockMicro, in.QuantityMicro, receiptLine.QuantityMicro)

		line = domain.ReturnLine{
			ID: identifier, LineNumber: position, ReceiptLineID: receiptLine.ID,
			ProductID: receiptLine.ProductID, VariantID: receiptLine.VariantID,
			ProductName: receiptLine.ProductName, VariantSKU: receiptLine.VariantSKU,
			UomCode:       receiptLine.UomCode,
			QuantityMicro: in.QuantityMicro, QuantityStockMicro: stockMicro,
			Notes: in.Notes,
		}

		decimals, err := s.decimalsOf(txCtx, document.CurrencyCode)
		if err != nil {
			return err
		}
		// The price the supplier charged, and the cost we carried — the ORIGINAL delivery's, per
		// §D.3. They differ whenever a bill corrected the price after delivery (6.4), and that
		// difference is a real gain or loss on the return.
		if line, err = line.Price(
			receiptLine.UnitCostMicro, receiptLine.UnitCostMicro, decimals); err != nil {
			return err
		}

		if err = s.repos.InsertReturnLine(txCtx, in.ReturnID, line); err != nil {
			return err
		}
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionReturnLineAdded, EntityType: EntityReturn, EntityID: in.ReturnID,
			After: map[string]any{
				"receipt_line": string(receiptLine.ID), "quantity": in.QuantityMicro,
			},
		})
	})
	if err != nil {
		return domain.ReturnLine{}, err
	}
	return line, nil
}

// PostReturn sends the goods back and debits the supplier.
//
// # Costed at the ORIGINAL delivery, never today's average
//
// §D.3, in the direction Phase 4 built `ReturnIn` for and this build added `ReturnOut` for. The
// movement names the receipt's movement as its source, and inventory reads the cost from there —
// returning goods bought at last year's price at this year's average invents a gain or a loss
// that never happened.
func (s *Service) PostReturn(
	ctx context.Context, returnID id.ID,
) (domain.SupplierReturn, error) {
	if s.stock == nil || s.tax == nil {
		return domain.SupplierReturn{}, errs.Internal(CodePortMissing,
			"the purchasing service was built without stock or tax")
	}

	var posted domain.SupplierReturn
	err := s.db.Do(ctx, func(txCtx context.Context) error {
		document, err := s.requireReturn(txCtx, returnID)
		if err != nil {
			return err
		}
		if err = document.RequireDraft(); err != nil {
			return err
		}

		lines, err := s.repos.ReturnLines(txCtx, returnID)
		if err != nil {
			return err
		}
		if len(lines) == 0 {
			return errs.Validation(domain.CodeNoLines,
				"a return with no lines sends nothing back")
		}

		for i, line := range lines {
			receiptLine, _, found, lineErr := s.repos.ReceiptLineByID(
				txCtx, line.ReceiptLineID)
			if lineErr != nil {
				return lineErr
			}
			if !found || receiptLine.MovementID.IsZero() {
				// Without the receipt's movement there is nothing to cost against, and a return
				// costed at today's average is the defect §D.3 exists to prevent.
				return errs.Conflict(domain.CodeInvalidLine,
					"that delivery line has no stock movement to return against")
			}

			taxed, taxErr := s.tax.TaxFor(txCtx, TaxQuery{
				CompanyID: document.CompanyID, PartnerID: document.PartnerID,
				ProductID: line.ProductID, NetMinor: line.NetMinor,
				Date: document.ReturnDate,
			})
			if taxErr != nil {
				return taxErr
			}
			line = line.Tax(taxed.AmountMinor, taxed.RateMicro, taxed.Code)

			moved, stockErr := s.stock.ReturnToSupplier(txCtx, StockRequest{
				CompanyID: document.CompanyID, WarehouseID: document.WarehouseID,
				ProductID: line.ProductID, VariantID: line.VariantID,
				QuantityMicro: line.QuantityStockMicro,
				LotID:         receiptLine.LotID, SerialID: receiptLine.SerialID,
				DocumentType: EntityReturn, DocumentID: document.ID, DocumentLineID: line.ID,
				OccurredAt: document.ReturnDate,
				// The link that carries the original cost.
				SourceMovementID: receiptLine.MovementID,
			})
			if stockErr != nil {
				return stockErr
			}
			line.MovementID = moved.MovementID
			// What the goods ACTUALLY cost, as inventory computed it from the source movement.
			// Taken from the answer rather than assumed, because the strategy decides — this
			// module must not learn whether it is average or FIFO.
			if moved.ValueMinor != 0 {
				line.CostMinor = abs64(moved.ValueMinor)
			}

			if err = s.repos.UpdateReturnLine(txCtx, line); err != nil {
				return err
			}
			lines[i] = line
		}

		document.NetMinor, document.TaxMinor, document.TotalMinor, document.CostMinor =
			domain.ReturnTotals(lines)
		document.Status = domain.ReturnPosted

		if document.Number, err = s.allocate(
			txCtx, document.BranchID, SeriesReturn); err != nil {
			return err
		}
		if err = s.repos.UpdateReturn(txCtx, document, s.actorOf(txCtx)); err != nil {
			return err
		}
		if err = s.publishReturnPosting(txCtx, document); err != nil {
			return err
		}

		posted = document
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionReturnPosted, EntityType: EntityReturn, EntityID: returnID,
			After: map[string]any{
				"number": document.Number, "total": document.TotalMinor,
				"cost": document.CostMinor,
			},
		})
	})
	if err != nil {
		return domain.SupplierReturn{}, err
	}
	return posted, nil
}

func (s *Service) publishReturnPosting(
	ctx context.Context, document domain.SupplierReturn,
) error {
	date, ok := clock.ParseDate(document.ReturnDate)
	if !ok {
		return errs.Internal(domain.CodeInvalidReturn,
			"a supplier return has an unreadable date")
	}

	// The difference between what they credit us and what the goods cost us. Real whenever a
	// bill corrected the price after delivery — a gain or a loss on the return, not an error.
	over, under := domain.SplitVariance(document.CostMinor - document.NetMinor)

	return s.bus.Publish(ctx, accountingc.Postable{
		Action:    PostingReturnPosted,
		CompanyID: document.CompanyID,
		BranchID:  document.BranchID,
		Date:      date,

		DocumentType:   EntityReturn,
		DocumentID:     document.ID,
		DocumentNumber: document.Number,

		Amounts: map[string]int64{
			accountingc.AmountTotal:                document.TotalMinor,
			accountingc.AmountNet:                  document.NetMinor,
			accountingc.AmountTax:                  document.TaxMinor,
			accountingc.AmountCost:                 document.CostMinor,
			accountingc.AmountVarianceExpenseOver:  over,
			accountingc.AmountVarianceExpenseUnder: under,
		},

		CurrencyCode: document.CurrencyCode,
		RateMicro:    1_000_000,
		PartnerID:    document.PartnerID,
		Memo:         document.Reason,
	})
}

// CancelReturn abandons a draft return.
func (s *Service) CancelReturn(ctx context.Context, returnID id.ID) error {
	return s.db.Do(ctx, func(txCtx context.Context) error {
		document, err := s.requireReturn(txCtx, returnID)
		if err != nil {
			return err
		}
		if err = document.RequireDraft(); err != nil {
			return err
		}
		document.Status = domain.ReturnCancelled
		if err = s.repos.UpdateReturn(txCtx, document, s.actorOf(txCtx)); err != nil {
			return err
		}
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionReturnCancelled, EntityType: EntityReturn, EntityID: returnID,
		})
	})
}

// Return reads one return with its lines.
func (s *Service) Return(
	ctx context.Context, returnID id.ID,
) (domain.SupplierReturn, []domain.ReturnLine, error) {
	document, found, err := s.repos.ReturnByID(ctx, returnID)
	if err != nil {
		return domain.SupplierReturn{}, nil, err
	}
	if !found {
		return domain.SupplierReturn{}, nil, errs.NotFound(CodeUnknownReturn,
			"there is no supplier return with that identity")
	}
	lines, err := s.repos.ReturnLines(ctx, returnID)
	if err != nil {
		return domain.SupplierReturn{}, nil, err
	}
	return document, lines, nil
}

// Returns lists a company's supplier returns.
func (s *Service) Returns(
	ctx context.Context, companyID id.ID, status domain.ReturnStatus,
) ([]domain.SupplierReturn, error) {
	return s.repos.Returns(ctx, companyID, status)
}

func (s *Service) requireReturn(
	ctx context.Context, returnID id.ID,
) (domain.SupplierReturn, error) {
	document, found, err := s.repos.ReturnByID(ctx, returnID)
	if err != nil {
		return domain.SupplierReturn{}, err
	}
	if !found {
		return domain.SupplierReturn{}, errs.NotFound(CodeUnknownReturn,
			"there is no supplier return with that identity")
	}
	return document, nil
}

// scaleQuantity moves a stock quantity in proportion to what is going back.
func scaleQuantity(stockMicro, returningMicro, receivedMicro int64) int64 {
	if receivedMicro <= 0 {
		return 0
	}
	if returningMicro == receivedMicro {
		return stockMicro
	}
	parts, err := roundAllocate(stockMicro, returningMicro, receivedMicro)
	if err != nil {
		return 0
	}
	return parts
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// roundAllocate scales a quantity by a ratio, exactly.
//
// The kernel allocator with two weights: the part going back and the part staying. Using it
// rather than a bare multiply-divide means the two halves of a split delivery always sum to what
// arrived, however awkward the conversion factor.
func roundAllocate(total, part, whole int64) (int64, error) {
	parts, err := round.Allocate(total, []int64{part, whole - part})
	if err != nil {
		return 0, err
	}
	return parts[0], nil
}
