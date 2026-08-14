package purchasing

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	accountingc "github.com/mizan-erp/mizan/internal/modules/accounting/contract"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/purchasing/domain"
	"github.com/mizan-erp/mizan/internal/platform/config"
)

// The audited actions a delivery leaves.
const (
	ActionReceiptDrafted   = "purchasing.receipt.drafted"
	ActionReceiptLineAdded = "purchasing.receipt.line_added"
	ActionReceiptConfirmed = "purchasing.receipt.confirmed"
	ActionReceiptCancelled = "purchasing.receipt.cancelled"

	EntityReceipt = "purchasing.receipt"
)

// PostingReceiptConfirmed is the POSTING-RULE key a confirmed delivery fires.
//
// Not an audit action, and the separation is deliberate — the Phase 5 DoD review found the two
// vocabularies merged in one const block and a test asking the audit trail for a posting key.
// This one selects a journal entry; `ActionReceiptConfirmed` records that a person did something.
const PostingReceiptConfirmed = "purchasing.receipt.confirmed"

// OverReceiptTolerance is how much more than was ordered a business will accept.
//
// A SETTING, because it is a business decision that differs by trade: a fastener wholesaler
// expects tolerance and a pharmacy dispensing controlled drugs does not. Default zero — the
// strict reading — so a business that wants latitude asks for it rather than discovering it.
var OverReceiptTolerance = config.DeclareInt(config.Def{
	Key:         "purchasing.over_receipt_tolerance_micro",
	Default:     int64(0),
	Scopes:      []config.Scope{config.ScopeSystem, config.ScopeCompany},
	Description: "settings.purchasing.over_receipt_tolerance",
})

// NewReceiptInput opens a delivery.
type NewReceiptInput struct {
	CompanyID   id.ID
	BranchID    id.ID
	WarehouseID id.ID
	// OrderID is optional. Goods arrive against no order more often than a tidy design expects:
	// a replacement for damaged stock, or a cash-and-carry purchase.
	OrderID     id.ID
	PartnerID   id.ID
	PartnerName string
	ReceiptDate string
	Currency    string

	DeliveryNoteReference string
	ReceivedByName        string
	Notes                 string
}

// DraftReceipt opens a delivery.
func (s *Service) DraftReceipt(
	ctx context.Context, in NewReceiptInput,
) (domain.Receipt, error) {
	identifier, err := id.New()
	if err != nil {
		return domain.Receipt{}, err
	}

	receipt, err := domain.NewReceipt(identifier, in.CompanyID, in.BranchID, in.WarehouseID,
		in.PartnerID, in.PartnerName, in.ReceiptDate, in.Currency)
	if err != nil {
		return domain.Receipt{}, err
	}
	receipt.OrderID = in.OrderID
	receipt.DeliveryNoteReference = in.DeliveryNoteReference
	receipt.ReceivedByName = in.ReceivedByName
	receipt.Notes = in.Notes

	if err = s.db.Do(ctx, func(txCtx context.Context) error {
		// A delivery against an order can only happen once the order has been SENT. Goods that
		// arrive against a draft are a receipt against no order — which is allowed — but they
		// must not attach here, or the order's own history becomes fiction.
		if !in.OrderID.IsZero() {
			order, orderErr := s.requireOrder(txCtx, in.OrderID)
			if orderErr != nil {
				return orderErr
			}
			if orderErr = order.RequireReceivable(); orderErr != nil {
				return orderErr
			}
		}
		if err = s.repos.InsertReceipt(txCtx, receipt, s.actorOf(txCtx)); err != nil {
			return err
		}
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionReceiptDrafted, EntityType: EntityReceipt, EntityID: receipt.ID,
			After: map[string]any{
				"partner": receipt.PartnerName, "date": receipt.ReceiptDate,
				"delivery_note": receipt.DeliveryNoteReference,
			},
		})
	}); err != nil {
		return domain.Receipt{}, err
	}
	return receipt, nil
}

// ReceiveLineInput records one thing that arrived.
type ReceiveLineInput struct {
	CompanyID   id.ID
	ReceiptID   id.ID
	OrderLineID id.ID
	// VariantID and UomID are needed only for a delivery against NO order line. Against one,
	// they come from the order — a delivery note that disagrees with the order about WHAT
	// arrived is a different problem from one that disagrees about how much.
	VariantID     id.ID
	UomID         id.ID
	QuantityMicro int64
	LotID         id.ID
	SerialID      id.ID
	Notes         string

	// UnitCostMicro is what the goods cost, for a delivery against NO order line.
	//
	// # Why a pointer
	//
	// nil and zero are different answers and both are legitimate. nil means "I have not said" —
	// resolve it from the purchase price list. A pointer to zero means "these were FREE", which
	// a warranty replacement or a sample genuinely is.
	//
	// A plain int64 would collapse the two, and the collapse has one direction: goods entering
	// stock at zero because nobody typed a price. That understates inventory and overstates
	// margin the day they are sold, and it surfaces months later as a gross profit nobody can
	// explain.
	UnitCostMicro *int64
}

// ReceiveLine records one thing that arrived.
//
// # The over-receipt check happens HERE, on entry, not at confirmation
//
// Somebody is standing at a loading bay with a delivery note. Telling them at entry that 45 is
// too many against an outstanding 40 lets them count again or telephone the supplier; telling
// them at confirmation, after they have keyed twenty lines, means unpicking the whole note.
func (s *Service) ReceiveLine(
	ctx context.Context, in ReceiveLineInput,
) (domain.ReceiptLine, error) {
	if s.catalog == nil {
		return domain.ReceiptLine{}, errs.Internal(CodePortMissing,
			"the purchasing service was built without a catalog")
	}

	var line domain.ReceiptLine
	err := s.db.Do(ctx, func(txCtx context.Context) error {
		receipt, err := s.requireReceipt(txCtx, in.ReceiptID)
		if err != nil {
			return err
		}
		if err = receipt.RequireDraft(); err != nil {
			return err
		}

		variantID, uomID := in.VariantID, in.UomID
		var unitCostMicro int64
		costKnown := false

		if in.UnitCostMicro != nil {
			unitCostMicro, costKnown = *in.UnitCostMicro, true
		}

		if !in.OrderLineID.IsZero() {
			orderLine, orderID, found, lineErr := s.repos.OrderLineByID(txCtx, in.OrderLineID)
			if lineErr != nil {
				return lineErr
			}
			if !found {
				return errs.NotFound(CodeUnknownLine,
					"there is no order line with that identity")
			}
			// A delivery note that names a line from a different order is a mistake worth
			// catching: it would fill somebody else's order and leave this one outstanding.
			if !receipt.OrderID.IsZero() && orderID != receipt.OrderID {
				return errs.Validation(domain.CodeInvalidLine,
					"that line belongs to a different order")
			}

			if err = domain.CheckOverReceipt(
				orderLine.OutstandingMicro(), in.QuantityMicro,
				domain.TolerancePolicy{PercentMicro: OverReceiptTolerance.Get(txCtx)},
			); err != nil {
				return err
			}

			variantID, uomID = orderLine.VariantID, orderLine.UomID
			// Valued at the ORDER's price (D3): the best available estimate, and the number both
			// parties last agreed on. The bill may disagree, and 6.4 corrects it.
			//
			// This OVERRIDES an explicit cost, deliberately. A delivery against an order is
			// worth what the order said until the invoice says otherwise — letting whoever keys
			// the delivery note set a cost would put an unagreed number into the valuation with
			// no document behind it, and the three-way match would have nothing to compare.
			// An explicit cost is for deliveries with no order, which have no agreed price.
			unitCostMicro, costKnown = orderLine.UnitPriceMicro, true
		}

		// A delivery against no order still has to be worth something. Resolved from the same
		// purchase price list an order would have used, so a cash-and-carry delivery enters stock
		// at a defensible number rather than at zero.
		if !costKnown {
			if s.pricing == nil {
				return errs.Internal(CodePortMissing,
					"the purchasing service was built without pricing")
			}
			resolved, priceErr := s.pricing.PurchasePriceFor(txCtx, PriceQuery{
				CompanyID: in.CompanyID, PartnerID: receipt.PartnerID,
				VariantID: variantID, QuantityMicro: in.QuantityMicro,
			})
			if priceErr != nil {
				return priceErr
			}
			unitCostMicro = resolved.UnitPriceMicro
		}

		facts, err := s.catalog.FactsFor(txCtx, in.CompanyID, variantID, uomID, in.QuantityMicro)
		if err != nil {
			return err
		}

		position, err := s.repos.NextReceiptLineNumber(txCtx, in.ReceiptID)
		if err != nil {
			return err
		}
		identifier, err := id.New()
		if err != nil {
			return err
		}

		line, err = domain.NewReceiptLine(identifier, facts.ProductID, variantID, facts.UomID,
			position, in.QuantityMicro, facts.QuantityStockMicro)
		if err != nil {
			return err
		}
		line.OrderLineID = in.OrderLineID
		line.ProductName = facts.ProductName
		line.VariantSKU = facts.VariantSKU
		line.UomCode = facts.UomCode
		line.LotID = in.LotID
		line.SerialID = in.SerialID
		line.Notes = in.Notes

		decimals, err := s.decimalsOf(txCtx, receipt.CurrencyCode)
		if err != nil {
			return err
		}
		line = line.Value(unitCostMicro, decimals)

		if err = s.repos.InsertReceiptLine(txCtx, in.ReceiptID, line); err != nil {
			return err
		}
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionReceiptLineAdded, EntityType: EntityReceipt, EntityID: in.ReceiptID,
			After: map[string]any{
				"variant": string(variantID), "quantity": line.QuantityMicro,
			},
		})
	})
	if err != nil {
		return domain.ReceiptLine{}, err
	}
	return line, nil
}

// ConfirmReceipt takes the goods into stock and accrues what will be owed.
//
// # One transaction, five things
//
// The stock movement, the movement link on each line, the order's received figures, the number,
// and the GRNI journal entry. Either all of it happened or none of it did: goods on a shelf with
// no entry behind them, or an entry for goods nobody received, are both worse than a failure.
//
// # Why this posts at all
//
// D2. When goods arrive and no invoice has, the business genuinely holds an asset and genuinely
// owes somebody for it — both facts are true before the invoice exists. Receipts posting nothing
// would leave stock on the shelf with no value in the books for as long as the invoice takes,
// and a month-end that lands in that window is not an edge case; it is twelve times a year.
func (s *Service) ConfirmReceipt(
	ctx context.Context, receiptID id.ID,
) (domain.Receipt, error) {
	if s.stock == nil {
		return domain.Receipt{}, errs.Internal(CodePortMissing,
			"the purchasing service was built without a stock port")
	}

	var confirmed domain.Receipt
	err := s.db.Do(ctx, func(txCtx context.Context) error {
		receipt, err := s.requireReceipt(txCtx, receiptID)
		if err != nil {
			return err
		}
		if err = receipt.RequireDraft(); err != nil {
			return err
		}

		lines, err := s.repos.ReceiptLines(txCtx, receiptID)
		if err != nil {
			return err
		}
		if len(lines) == 0 {
			return errs.Validation(domain.CodeNoLines,
				"a delivery with no lines received nothing")
		}

		total := int64(0)
		for i, line := range lines {
			moved, moveErr := s.stock.Receive(txCtx, StockRequest{
				CompanyID: receipt.CompanyID, WarehouseID: receipt.WarehouseID,
				ProductID: line.ProductID, VariantID: line.VariantID,
				QuantityMicro: line.QuantityStockMicro,
				UnitCostMicro: line.UnitCostMicro,
				LotID:         line.LotID, SerialID: line.SerialID,
				DocumentType: EntityReceipt, DocumentID: receipt.ID, DocumentLineID: line.ID,
				OccurredAt: receipt.ReceiptDate,
			})
			if moveErr != nil {
				return moveErr
			}

			// The movement id is written back, and it is the link a supplier return follows to
			// cost itself at the ORIGINAL receipt's cost (§D.3).
			line.MovementID = moved.MovementID
			if err = s.repos.UpdateReceiptLine(txCtx, line); err != nil {
				return err
			}

			if !line.OrderLineID.IsZero() {
				if err = s.repos.AddReceived(
					txCtx, line.OrderLineID, line.QuantityMicro); err != nil {
					return err
				}
			}
			total += line.ValueMinor
			lines[i] = line
		}

		receipt.ValueMinor = total
		receipt.Status = domain.ReceiptConfirmed
		if receipt.Number, err = s.allocate(
			txCtx, receipt.BranchID, SeriesReceipt); err != nil {
			return err
		}
		if err = s.repos.UpdateReceipt(txCtx, receipt, s.actorOf(txCtx)); err != nil {
			return err
		}

		if err = s.publishReceiptPosting(txCtx, receipt); err != nil {
			return err
		}

		confirmed = receipt
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionReceiptConfirmed, EntityType: EntityReceipt, EntityID: receiptID,
			After: map[string]any{"number": receipt.Number, "value": receipt.ValueMinor},
		})
	})
	if err != nil {
		return domain.Receipt{}, err
	}
	return confirmed, nil
}

// publishReceiptPosting fires Phase 2's `goods_receipt` rule.
//
// Purchasing names no accounts. It says what HAPPENED — a delivery was confirmed, worth this
// much — and the seeded rules decide which accounts move. That is §20.3, and it is why the
// difference between a business that accrues GRNI and one that does not is a seed file rather
// than a branch in this function.
func (s *Service) publishReceiptPosting(
	ctx context.Context, receipt domain.Receipt,
) error {
	date, ok := clock.ParseDate(receipt.ReceiptDate)
	if !ok {
		return errs.Internal(domain.CodeInvalidReceipt,
			"a goods receipt has an unreadable date").WithParam("date", receipt.ReceiptDate)
	}

	return s.bus.Publish(ctx, accountingc.Postable{
		Action:    PostingReceiptConfirmed,
		CompanyID: receipt.CompanyID,
		BranchID:  receipt.BranchID,
		Date:      date,

		DocumentType:   EntityReceipt,
		DocumentID:     receipt.ID,
		DocumentNumber: receipt.Number,

		// A receipt has no tax: tax arrives with the INVOICE, and accruing it before the
		// supplier has claimed it would put a recoverable asset on the books that no document
		// supports.
		Amounts: map[string]int64{
			accountingc.AmountNet:   receipt.ValueMinor,
			accountingc.AmountTotal: receipt.ValueMinor,
		},

		CurrencyCode: receipt.CurrencyCode,
		RateMicro:    1_000_000,
		PartnerID:    receipt.PartnerID,
		Memo:         receipt.DeliveryNoteReference,
	})
}

// CancelReceipt abandons a draft delivery.
func (s *Service) CancelReceipt(ctx context.Context, receiptID id.ID) error {
	return s.db.Do(ctx, func(txCtx context.Context) error {
		receipt, err := s.requireReceipt(txCtx, receiptID)
		if err != nil {
			return err
		}
		if err = receipt.RequireDraft(); err != nil {
			return err
		}
		receipt.Status = domain.ReceiptCancelled
		if err = s.repos.UpdateReceipt(txCtx, receipt, s.actorOf(txCtx)); err != nil {
			return err
		}
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionReceiptCancelled, EntityType: EntityReceipt, EntityID: receiptID,
		})
	})
}

// Receipt reads one delivery with its lines.
func (s *Service) Receipt(
	ctx context.Context, receiptID id.ID,
) (domain.Receipt, []domain.ReceiptLine, error) {
	receipt, found, err := s.repos.ReceiptByID(ctx, receiptID)
	if err != nil {
		return domain.Receipt{}, nil, err
	}
	if !found {
		return domain.Receipt{}, nil, errs.NotFound(CodeUnknownReceipt,
			"there is no goods receipt with that identity")
	}
	lines, err := s.repos.ReceiptLines(ctx, receiptID)
	if err != nil {
		return domain.Receipt{}, nil, err
	}
	return receipt, lines, nil
}

// Receipts lists deliveries, optionally for one order.
func (s *Service) Receipts(
	ctx context.Context, companyID, orderID id.ID, status domain.ReceiptStatus,
) ([]domain.Receipt, error) {
	return s.repos.Receipts(ctx, companyID, orderID, status)
}

// VerifyReceived checks the maintained received figures against the receipts that justify them.
//
// # Reports, never repairs
//
// The same discipline Phase 4 applies to `stock_levels` (4.3). `received_micro` is a maintained
// projection and the confirmed receipt lines are the truth; this recomputes one from the other
// and reports the difference. It does not correct anything, because a projection that silently
// heals hides the bug that broke it — and the next thing it hides is the one that mattered.
func (s *Service) VerifyReceived(
	ctx context.Context, orderID id.ID,
) ([]ReceivedDiscrepancy, error) {
	lines, err := s.repos.Lines(ctx, orderID)
	if err != nil {
		return nil, err
	}
	actual, err := s.repos.ReceivedFromLines(ctx, orderID)
	if err != nil {
		return nil, err
	}

	discrepancies := make([]ReceivedDiscrepancy, 0)
	for _, line := range lines {
		if recomputed := actual[line.ID]; recomputed != line.ReceivedMicro {
			discrepancies = append(discrepancies, ReceivedDiscrepancy{
				OrderLineID: line.ID,
				Recorded:    line.ReceivedMicro,
				Recomputed:  recomputed,
			})
		}
	}
	return discrepancies, nil
}

// ReceivedDiscrepancy is one order line whose projection disagrees with its receipts.
type ReceivedDiscrepancy struct {
	OrderLineID id.ID
	Recorded    int64
	Recomputed  int64
}

func (s *Service) requireReceipt(
	ctx context.Context, receiptID id.ID,
) (domain.Receipt, error) {
	receipt, found, err := s.repos.ReceiptByID(ctx, receiptID)
	if err != nil {
		return domain.Receipt{}, err
	}
	if !found {
		return domain.Receipt{}, errs.NotFound(CodeUnknownReceipt,
			"there is no goods receipt with that identity")
	}
	return receipt, nil
}
