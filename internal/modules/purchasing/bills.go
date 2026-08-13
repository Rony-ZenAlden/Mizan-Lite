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

// The audited actions a supplier invoice leaves.
const (
	ActionBillDrafted   = "purchasing.bill.drafted"
	ActionBillLineAdded = "purchasing.bill.line_added"
	ActionBillPosted    = "purchasing.bill.posted"
	ActionBillCancelled = "purchasing.bill.cancelled"

	EntityBill = "purchasing.bill"
)

// PostingBillPosted is the POSTING-RULE key a posted bill fires.
//
// The event Phase 2 seeded `purchase` against, and which has never fired until now.
const PostingBillPosted = "purchasing.bill.posted"

// The permissions a bill needs, separate from ordering and receiving.
//
// Segregation of duties: one person ordering, taking delivery, AND approving the invoice can pay
// a supplier for nothing, and no amount of matching detects it because they control all three
// sides. Three grants is the mechanism that makes the match worth having.
const (
	PermBillView  = "purchasing.bill.view"
	PermBillDraft = "purchasing.bill.draft"
	PermBillPost  = "purchasing.bill.post"
)

// NewBillInput opens a supplier invoice.
type NewBillInput struct {
	CompanyID   id.ID
	BranchID    id.ID
	PartnerID   id.ID
	PartnerName string
	BillDate    string
	DueDate     string
	// SupplierInvoiceNumber is THEIR number. Required.
	SupplierInvoiceNumber string
	Currency              string
	Notes                 string
}

// DraftBill opens a supplier invoice.
func (s *Service) DraftBill(ctx context.Context, in NewBillInput) (domain.Bill, error) {
	identifier, err := id.New()
	if err != nil {
		return domain.Bill{}, err
	}

	bill, err := domain.NewBill(identifier, in.CompanyID, in.BranchID, in.PartnerID,
		in.PartnerName, in.BillDate, in.SupplierInvoiceNumber, in.Currency)
	if err != nil {
		return domain.Bill{}, err
	}
	bill.DueDate = in.DueDate
	bill.Notes = in.Notes

	if err = s.db.Do(ctx, func(txCtx context.Context) error {
		// # Two layers keep this rule, and both are needed
		//
		// This check gives a clerk something they can act on — WHICH invoice, from WHICH
		// supplier — where a raw constraint violation tells them nothing. The unique index on
		// (company, partner, supplier invoice number) is the other half, and it is the one that
		// holds when two clerks enter the same invoice at the same moment.
		//
		// Entering a supplier invoice twice is the one duplicate that costs money: it is paid
		// twice.
		if _, found, dupErr := s.repos.BillBySupplierInvoice(
			txCtx, bill.CompanyID, bill.PartnerID, bill.SupplierInvoiceNumber,
		); dupErr != nil {
			return dupErr
		} else if found {
			return errs.Conflict(domain.CodeDuplicateInvoice,
				"that supplier invoice number has already been entered").
				WithParam("invoice", bill.SupplierInvoiceNumber).
				WithParam("supplier", bill.PartnerName)
		}

		if err = s.repos.InsertBill(txCtx, bill, s.actorOf(txCtx)); err != nil {
			return err
		}
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionBillDrafted, EntityType: EntityBill, EntityID: bill.ID,
			After: map[string]any{
				"supplier": bill.PartnerName, "invoice": bill.SupplierInvoiceNumber,
			},
		})
	}); err != nil {
		return domain.Bill{}, err
	}
	return bill, nil
}

// BillLineInput takes up one delivery line.
//
// # There is no quantity field
//
// A bill line takes a RECEIPT LINE IN FULL. The receipt line is already the record of what
// physically arrived in one delivery, at one time, signed for by one person — and suppliers do
// not invoice half a delivery line. Allowing a partial take-up would mean tracking how much of
// each receipt line remains unbilled, which is a third projection to maintain, in exchange for a
// case nobody has.
//
// The PRICE is a field, because the supplier's price is the one thing a bill genuinely adds.
type BillLineInput struct {
	CompanyID     id.ID
	BillID        id.ID
	ReceiptLineID id.ID
	// UnitPriceMicro is what the supplier is charging. Zero means "what was ordered", which is
	// the ordinary case and saves keying a price that already agrees.
	UnitPriceMicro int64
	DiscountMinor  int64
	Notes          string
}

// AddBillLine takes up one delivery line.
func (s *Service) AddBillLine(
	ctx context.Context, in BillLineInput,
) (domain.BillLine, error) {
	var line domain.BillLine

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		bill, err := s.requireBill(txCtx, in.BillID)
		if err != nil {
			return err
		}
		if err = bill.RequireDraft(); err != nil {
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
		// A receipt can be billed only once. Billing it twice pays a supplier twice for one
		// delivery, which is exactly what the three-way match exists to prevent.
		if err = receipt.RequireBillable(); err != nil {
			return err
		}
		// And it must be the same supplier: taking up another supplier's delivery would credit
		// the wrong account and leave two suppliers' books both wrong.
		if receipt.PartnerID != bill.PartnerID {
			return errs.Validation(domain.CodeInvalidLine,
				"that delivery came from a different supplier")
		}

		position, err := s.repos.NextBillLineNumber(txCtx, in.BillID)
		if err != nil {
			return err
		}
		identifier, err := id.New()
		if err != nil {
			return err
		}

		line = domain.BillLine{
			ID: identifier, LineNumber: position, ReceiptLineID: receiptLine.ID,
			ProductID: receiptLine.ProductID, VariantID: receiptLine.VariantID,
			ProductName: receiptLine.ProductName, VariantSKU: receiptLine.VariantSKU,
			UomCode: receiptLine.UomCode,
			// IN FULL: what arrived is what is billed. The match then compares this with the
			// delivery rather than with a number somebody typed.
			QuantityMicro:        receiptLine.QuantityMicro,
			AccruedUnitCostMicro: receiptLine.UnitCostMicro,
			AccruedMinor:         receiptLine.ValueMinor,
			Notes:                in.Notes,
		}

		// The supplier's price, or the ordered one where they agree.
		unitPrice := in.UnitPriceMicro
		if unitPrice == 0 {
			unitPrice = receiptLine.UnitCostMicro
		}
		decimals, err := s.decimalsOf(txCtx, bill.CurrencyCode)
		if err != nil {
			return err
		}
		if line, err = line.Price(unitPrice, in.DiscountMinor, decimals); err != nil {
			return err
		}

		// # The quantity side of the match needs no check, and that is the stronger guarantee
		//
		// A bill line MUST name a receipt line (the column is NOT NULL) and takes its quantity
		// IN FULL. There is no field through which a different quantity could be expressed, so
		// "the supplier is invoicing for goods that never arrived" is not a state this schema
		// can hold — rather than one it validates against.
		//
		// A runtime check here was written first and then removed: it compared the quantity with
		// the value it had just been assigned from, so it could never fire. An unreachable guard
		// is worse than none, because a test for it passes whatever the code does (4.3).

		// The unique index on receipt_line_id is the backstop here, holding when two clerks bill
		// one delivery at the same moment. `RequireBillable` above is the readable half.
		if err = s.repos.InsertBillLine(txCtx, in.BillID, line); err != nil {
			return err
		}
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionBillLineAdded, EntityType: EntityBill, EntityID: in.BillID,
			After: map[string]any{
				"receipt_line": string(receiptLine.ID), "net": line.NetMinor,
			},
		})
	})
	if err != nil {
		return domain.BillLine{}, err
	}
	return line, nil
}

// PostBill commits a supplier invoice.
//
// # What the entry has to do
//
//	DR GRNI            exactly what the receipts accrued
//	DR/CR variance     the difference between what was ordered and what is charged
//	DR recoverable tax
//	CR accounts payable the total
//
// GRNI is cleared at the ACCRUED figure, not the bill's own net. Clearing the bill's net instead
// would leave the price difference sitting in GRNI forever — and a GRNI balance nobody can
// explain is a GRNI balance nobody reads, which costs the business the one report that tells it
// what it has received and not been invoiced for.
func (s *Service) PostBill(ctx context.Context, billID id.ID) (domain.Bill, error) {
	if s.tax == nil {
		return domain.Bill{}, errs.Internal(CodePortMissing,
			"the purchasing service was built without tax")
	}

	var posted domain.Bill
	err := s.db.Do(ctx, func(txCtx context.Context) error {
		bill, err := s.requireBill(txCtx, billID)
		if err != nil {
			return err
		}
		if err = bill.RequireDraft(); err != nil {
			return err
		}

		lines, err := s.repos.BillLines(txCtx, billID)
		if err != nil {
			return err
		}
		if len(lines) == 0 {
			return errs.Validation(domain.CodeNoLines,
				"a bill with no lines charges for nothing")
		}

		for i, line := range lines {
			taxed, taxErr := s.tax.TaxFor(txCtx, TaxQuery{
				CompanyID: bill.CompanyID, PartnerID: bill.PartnerID,
				ProductID: line.ProductID, NetMinor: line.NetMinor, Date: bill.BillDate,
			})
			if taxErr != nil {
				return taxErr
			}
			line = line.Tax(taxed.AmountMinor, taxed.RateMicro, taxed.Code)
			if err = s.repos.UpdateBillLine(txCtx, line); err != nil {
				return err
			}
			lines[i] = line
		}

		bill.NetMinor, bill.TaxMinor, bill.DiscountMinor, bill.TotalMinor, bill.AccruedMinor =
			domain.BillTotals(lines)
		bill.VarianceMinor = bill.NetMinor - bill.AccruedMinor
		bill.Status = domain.BillPosted

		// The price correction, split by where the goods are now (6.4).
		split, err := s.correctCosts(txCtx, bill, lines)
		if err != nil {
			return err
		}

		if bill.Number, err = s.allocate(txCtx, bill.BranchID, SeriesBill); err != nil {
			return err
		}
		if err = s.repos.UpdateBill(txCtx, bill, s.actorOf(txCtx)); err != nil {
			return err
		}

		// Every delivery this bill took up is now billed, and cannot be billed again.
		receiptIDs, err := s.receiptsBehind(txCtx, lines)
		if err != nil {
			return err
		}
		if err = s.repos.MarkReceiptsBilled(txCtx, bill.ID, receiptIDs); err != nil {
			return err
		}

		if err = s.publishBillPosting(txCtx, bill, split); err != nil {
			return err
		}

		posted = bill
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionBillPosted, EntityType: EntityBill, EntityID: billID,
			After: map[string]any{
				"number": bill.Number, "total": bill.TotalMinor,
				"variance": bill.VarianceMinor,
			},
		})
	})
	if err != nil {
		return domain.Bill{}, err
	}
	return posted, nil
}

// receiptsBehind collects the distinct deliveries a bill's lines came from.
func (s *Service) receiptsBehind(
	ctx context.Context, lines []domain.BillLine,
) ([]id.ID, error) {
	seen := make(map[id.ID]bool, len(lines))
	out := make([]id.ID, 0, len(lines))
	for _, line := range lines {
		_, receiptID, found, err := s.repos.ReceiptLineByID(ctx, line.ReceiptLineID)
		if err != nil {
			return nil, err
		}
		if !found || seen[receiptID] {
			continue
		}
		seen[receiptID] = true
		out = append(out, receiptID)
	}
	return out, nil
}

// correctCosts revalues the stock a price difference still applies to.
//
// # What this does and why it is not one number
//
// A bill disagreeing with the order it bills is correcting what the goods cost. Where those goods
// are now decides what the correction can do — still on the shelf means REVALUE them, already
// sold means the cost of that sale posted at the old figure in a period that may be closed.
//
// The revaluation is written as a stock MOVEMENT, not merely a journal line, because the stock
// ledger carries its own value and the two must not disagree. Phase 4 built `Revaluation` — value
// without quantity, Neutral direction — in 4.2 for exactly this, and nothing has called it until
// now.
func (s *Service) correctCosts(
	ctx context.Context, bill domain.Bill, lines []domain.BillLine,
) (domain.VarianceSplit, error) {
	var total domain.VarianceSplit
	if s.stock == nil {
		return total, errs.Internal(CodePortMissing,
			"the purchasing service was built without a stock port")
	}

	decimals, err := s.decimalsOf(ctx, bill.CurrencyCode)
	if err != nil {
		return total, err
	}

	for _, line := range lines {
		variance := line.PriceVarianceMinor()
		if variance == 0 {
			continue
		}

		receiptLine, receiptID, found, lineErr := s.repos.ReceiptLineByID(ctx, line.ReceiptLineID)
		if lineErr != nil {
			return total, lineErr
		}
		if !found {
			continue
		}
		receipt, receiptErr := s.requireReceipt(ctx, receiptID)
		if receiptErr != nil {
			return total, receiptErr
		}

		onHand, stockErr := s.stock.OnHandMicro(ctx, line.VariantID, receipt.WarehouseID)
		if stockErr != nil {
			return total, stockErr
		}

		split := domain.SplitByWhereTheGoodsAre(
			variance, receiptLine.QuantityStockMicro, onHand)
		total.StockMinor += split.StockMinor
		total.ExpenseMinor += split.ExpenseMinor

		if split.StockMinor != 0 {
			if err = s.stock.Revalue(ctx, RevaluationRequest{
				CompanyID: bill.CompanyID, WarehouseID: receipt.WarehouseID,
				ProductID: line.ProductID, VariantID: line.VariantID,
				DeltaMinor: split.StockMinor, Decimals: decimals,
				DocumentType: EntityBill, DocumentID: bill.ID,
				OccurredAt: bill.BillDate,
			}); err != nil {
				return total, err
			}
		}
	}
	return total, nil
}

func (s *Service) publishBillPosting(
	ctx context.Context, bill domain.Bill, split domain.VarianceSplit,
) error {
	date, ok := clock.ParseDate(bill.BillDate)
	if !ok {
		return errs.Internal(domain.CodeInvalidBill,
			"a purchase bill has an unreadable date").WithParam("date", bill.BillDate)
	}

	stockOver, stockUnder := domain.SplitVariance(split.StockMinor)
	expenseOver, expenseUnder := domain.SplitVariance(split.ExpenseMinor)

	return s.bus.Publish(ctx, accountingc.Postable{
		Action:    PostingBillPosted,
		CompanyID: bill.CompanyID,
		BranchID:  bill.BranchID,
		Date:      date,

		DocumentType:   EntityBill,
		DocumentID:     bill.ID,
		DocumentNumber: bill.Number,

		Amounts: map[string]int64{
			accountingc.AmountTotal:                bill.TotalMinor,
			accountingc.AmountNet:                  bill.NetMinor,
			accountingc.AmountTax:                  bill.TaxMinor,
			accountingc.AmountAccrued:              bill.AccruedMinor,
			accountingc.AmountVarianceStockOver:    stockOver,
			accountingc.AmountVarianceStockUnder:   stockUnder,
			accountingc.AmountVarianceExpenseOver:  expenseOver,
			accountingc.AmountVarianceExpenseUnder: expenseUnder,
		},

		CurrencyCode: bill.CurrencyCode,
		RateMicro:    bill.ExchangeRateMicro,
		PartnerID:    bill.PartnerID,
		Memo:         bill.SupplierInvoiceNumber,
	})
}

// CancelBill abandons a draft invoice.
func (s *Service) CancelBill(ctx context.Context, billID id.ID) error {
	return s.db.Do(ctx, func(txCtx context.Context) error {
		bill, err := s.requireBill(txCtx, billID)
		if err != nil {
			return err
		}
		if err = bill.RequireDraft(); err != nil {
			return err
		}
		bill.Status = domain.BillCancelled
		if err = s.repos.UpdateBill(txCtx, bill, s.actorOf(txCtx)); err != nil {
			return err
		}
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionBillCancelled, EntityType: EntityBill, EntityID: billID,
		})
	})
}

// Bill reads one invoice with its lines.
func (s *Service) Bill(
	ctx context.Context, billID id.ID,
) (domain.Bill, []domain.BillLine, error) {
	bill, found, err := s.repos.BillByID(ctx, billID)
	if err != nil {
		return domain.Bill{}, nil, err
	}
	if !found {
		return domain.Bill{}, nil, errs.NotFound(CodeUnknownBill,
			"there is no purchase bill with that identity")
	}
	lines, err := s.repos.BillLines(ctx, billID)
	if err != nil {
		return domain.Bill{}, nil, err
	}
	return bill, lines, nil
}

// Bills lists a company's supplier invoices.
func (s *Service) Bills(
	ctx context.Context, companyID id.ID, status domain.BillStatus,
) ([]domain.Bill, error) {
	return s.repos.Bills(ctx, companyID, status)
}

// MatchOf reports how a bill's lines compare with the deliveries behind them.
//
// The three-way match as a REPORT rather than only as a refusal. The quantity side is already
// enforced; this is what shows somebody the price differences, which are accepted and therefore
// invisible unless something surfaces them.
func (s *Service) MatchOf(
	ctx context.Context, billID id.ID,
) ([]domain.MatchResult, error) {
	lines, err := s.repos.BillLines(ctx, billID)
	if err != nil {
		return nil, err
	}

	results := make([]domain.MatchResult, 0, len(lines))
	for _, line := range lines {
		receiptLine, _, found, lineErr := s.repos.ReceiptLineByID(ctx, line.ReceiptLineID)
		if lineErr != nil {
			return nil, lineErr
		}
		if !found {
			continue
		}
		results = append(results, domain.Match(line, receiptLine.QuantityMicro))
	}
	return results, nil
}

func (s *Service) requireBill(ctx context.Context, billID id.ID) (domain.Bill, error) {
	bill, found, err := s.repos.BillByID(ctx, billID)
	if err != nil {
		return domain.Bill{}, err
	}
	if !found {
		return domain.Bill{}, errs.NotFound(CodeUnknownBill,
			"there is no purchase bill with that identity")
	}
	return bill, nil
}
