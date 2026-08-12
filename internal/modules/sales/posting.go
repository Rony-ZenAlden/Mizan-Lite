package sales

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	accountingc "github.com/mizan-erp/mizan/internal/modules/accounting/contract"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/sales/domain"
)

// The postable actions and audited actions this step adds.
const (
	// ActionInvoicePosted is what Phase 2's `sale_revenue` and `sale_cost` rules match on. It was
	// seeded in Phase 2 and has never fired until now.
	ActionInvoicePosted = "sales.invoice.posted"
	// ActionCreditNotePosted is the reverse. A SEPARATE action, not a negative invoice, because
	// the posting engine refuses negative amounts — a negative would flip a line's side silently.
	ActionCreditNotePosted = "sales.credit_note.posted"

	ActionDocumentPosted = "sales.document.posted"

	CodePortMissing = "sales.port_missing"
)

// Post commits a sales document.
//
// # One transaction, or none of it
//
// Posting does eight things, and a document that did six of them is worse than one that did none:
//
//  1. refuses anything that is not a postable draft
//  2. resolves each line's price, and records which list answered
//  3. computes each line's tax, and records the RATE
//  4. issues the stock, and takes back what it cost us
//  5. totals the document from its lines
//  6. checks the customer's credit limit against the new total
//  7. allocates the document number
//  8. publishes the event Phase 2's posting rules turn into a journal entry
//
// Stock gone with no invoice, an invoice with no journal entry, a number consumed by a failure —
// each is a state somebody has to reconcile by hand, and each is unreachable because all eight
// happen inside one `db.Do`.
//
// # The order is not arbitrary
//
// Price before tax, because tax is computed on the net. Stock before totals, because the cost
// comes back from the movement. Credit after totals, because the limit is checked against the
// figure the customer will actually owe. The number LAST of the writes, so a failure anywhere
// earlier consumes none.
func (s *Service) Post(ctx context.Context, documentID id.ID) (domain.Document, error) {
	if err := s.requirePorts(); err != nil {
		return domain.Document{}, err
	}

	var posted domain.Document

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		document, err := s.requireDocument(txCtx, documentID)
		if err != nil {
			return err
		}
		lines, err := s.repos.Lines(txCtx, documentID)
		if err != nil {
			return err
		}
		if err = document.RequirePostable(lines); err != nil {
			return err
		}
		companyID, err := s.repos.CompanyOf(txCtx, documentID)
		if err != nil {
			return err
		}

		priced := make([]domain.Line, 0, len(lines))
		for _, line := range lines {
			settled, lineErr := s.settle(txCtx, companyID, document, line)
			if lineErr != nil {
				return lineErr
			}
			priced = append(priced, settled)
		}

		net, tax, discount, total := domain.Totals(priced)
		document.NetMinor = net
		document.TaxMinor = tax
		document.DiscountMinor = discount
		document.TotalMinor = total
		document.CostMinor = costOf(priced)

		// Checked against the figure the customer will actually owe, which is why it happens
		// after the totals rather than against an estimate at drafting.
		if document.Type == domain.Invoice && !document.PartnerID.IsZero() {
			if err = s.credit.CheckCredit(
				txCtx, companyID, document.PartnerID, total); err != nil {
				return err
			}
		}

		// LAST of the writes. A failure anywhere above consumes no number, which is §9.4's rule
		// expressed as an ordering rather than as a comment.
		number, err := s.allocateNumber(txCtx, document.BranchID, seriesFor(document.Type))
		if err != nil {
			return err
		}
		document.Number = number
		document.PostedAt = clock.Format(s.clk.Now())
		document.Status = domain.Posted

		for _, line := range priced {
			if err = s.repos.UpdateLineOnPost(txCtx, line); err != nil {
				return err
			}
		}
		if err = s.repos.SetDocumentTotals(txCtx, document); err != nil {
			return err
		}
		if err = s.repos.SetDocumentStatus(
			txCtx, documentID, domain.Posted, number, document.PostedAt); err != nil {
			return err
		}

		if err = s.publishPosting(txCtx, companyID, document); err != nil {
			return err
		}
		posted = document

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionDocumentPosted, EntityType: EntityDocument, EntityID: documentID,
			After: map[string]any{
				"number": number, "type": string(document.Type),
				"total_minor": total, "cost_minor": document.CostMinor, "lines": len(priced),
			},
		})
	})
	if err != nil {
		return domain.Document{}, err
	}
	return posted, nil
}

// settle resolves one line's price, tax, and cost.
func (s *Service) settle(
	ctx context.Context, companyID id.ID, document domain.Document, line domain.Line,
) (domain.Line, error) {
	price, err := s.pricing.PriceFor(ctx, PriceQuery{
		CompanyID: companyID, PartnerID: document.PartnerID, BranchID: document.BranchID,
		ProductID: line.ProductID, VariantID: line.VariantID,
		QuantityMicro: line.QuantityMicro, Date: document.Date,
	})
	if err != nil {
		return domain.Line{}, err
	}

	line, err = line.Price(price.UnitPriceMinor, line.DiscountMinor)
	if err != nil {
		return domain.Line{}, err
	}
	// The REASON, frozen. §2.6 requires resolution to record which list answered, and this is
	// where the answer outlives the resolution.
	line.PriceSource = price.Source
	line.PriceListCode = price.ListCode

	computed, err := s.tax.TaxFor(ctx, TaxQuery{
		CompanyID: companyID, BranchID: document.BranchID, PartnerID: document.PartnerID,
		ProductID: line.ProductID, NetMinor: line.NetMinor,
		CurrencyCode: document.CurrencyCode, Date: document.Date,
	})
	if err != nil {
		return domain.Line{}, err
	}
	line = line.Tax(computed.RateMicro, computed.AmountMinor, computed.Code)

	// A quotation is a promise and an order is an intention; neither takes anything off a shelf.
	// The decision lives on the TYPE (see Type.MovesStock), so a report, a screen, and this
	// cannot disagree about it.
	if !document.Type.MovesStock() {
		return line, nil
	}

	// The stock movement, and the cost it reports back. Only inventory knows what the goods cost
	// — the costing port decides whether the answer is an average or a layer, and sales must not
	// learn which.
	request := StockRequest{
		CompanyID: companyID, WarehouseID: document.WarehouseID,
		ProductID: line.ProductID, VariantID: line.VariantID,
		// The STOCK quantity, not what the customer bought in.
		QuantityMicro: line.QuantityStockMicro,
		LotID:         line.LotID, SerialID: line.SerialID,
		DocumentType: EntityDocument, DocumentID: document.ID, DocumentLineID: line.ID,
		OccurredAt: document.Date,
	}

	var moved StockResult
	if document.Type == domain.CreditNote {
		// §D.3: a return is costed at its ORIGINAL issue's cost, not today's average. The chain
		// is credit-note line → the invoice line it reverses → the movement that line produced.
		//
		// Resolved HERE rather than carried on the line, because the movement id belongs to the
		// invoice's line and copying it onto the credit note would be a second copy of a fact
		// that can be looked up — and a copy that could be wrong.
		if line.SourceLineID.IsZero() {
			return domain.Line{}, errs.Validation(CodeUnknownSourceLine,
				"a credit note line must name the sale line it reverses")
		}
		sourceMovement, movementErr := s.repos.MovementOfLine(ctx, line.SourceLineID)
		if movementErr != nil {
			return domain.Line{}, movementErr
		}
		request.SourceMovementID = sourceMovement
		moved, err = s.stock.Return(ctx, request)
	} else {
		moved, err = s.stock.Issue(ctx, request)
	}
	if err != nil {
		return domain.Line{}, err
	}
	line.MovementID = moved.MovementID
	line.CostMicro = moved.UnitCostMicro
	return line, nil
}

// publishPosting announces the document to the books.
//
// # Sales contains no accounting logic
//
// It says what happened and supplies the amounts. Which accounts those move is Phase 2's
// table-driven decision (§20.3) — the `sale_revenue` and `sale_cost` rules seeded in Phase 2,
// firing here for the first time. A country that books sales differently is a seed file.
//
// A credit note is a SEPARATE action rather than an invoice with negative amounts, because the
// posting engine refuses negatives: a negative would flip a line's side silently, and "debit XOR
// credit, both non-negative" is what makes an entry checkable.
func (s *Service) publishPosting(
	ctx context.Context, companyID id.ID, document domain.Document,
) error {
	date, ok := clock.ParseDate(document.Date)
	if !ok {
		return errs.Internal(domain.CodeInvalidDocument,
			"a sales document has an unreadable date").WithParam("date", document.Date)
	}

	action := ActionInvoicePosted
	if document.Type == domain.CreditNote {
		action = ActionCreditNotePosted
	}

	return s.bus.Publish(ctx, accountingc.Postable{
		Action:    action,
		CompanyID: companyID,
		BranchID:  document.BranchID,
		Date:      date,

		DocumentType:   string(document.Type),
		DocumentID:     document.ID,
		DocumentNumber: document.Number,

		Amounts: map[string]int64{
			accountingc.AmountTotal: document.TotalMinor,
			accountingc.AmountNet:   document.NetMinor,
			accountingc.AmountTax:   document.TaxMinor,
			// The cost, which drives the COGS entry. A shop that does not track stock simply has
			// no cost, and the `sale_cost` rule then posts nothing — which is exactly what
			// Phase 2's "lines that resolve to zero are skipped" was designed for.
			accountingc.AmountCost: document.CostMinor,
		},

		CurrencyCode: document.CurrencyCode,
		RateMicro:    document.RateMicro,
		PartnerID:    document.PartnerID,
		Memo:         document.Notes,
	})
}

// seriesFor maps a document type to the series that numbers it.
//
// Separate sequences per kind: an invoice and a credit note both starting at 1 is correct, and
// sharing a counter would make invoice numbers jump every time a return was issued.
func seriesFor(t domain.Type) string {
	switch t {
	case domain.CreditNote:
		return SeriesCreditNote
	case domain.Order:
		return SeriesOrder
	case domain.Quotation:
		return SeriesQuotation
	default:
		return SeriesInvoice
	}
}

// costOf sums what a document's goods cost us, in whole minor units.
func costOf(lines []domain.Line) int64 {
	var total int64
	for _, line := range lines {
		total += domain.CostOfLine(line)
	}
	return total
}

// requirePorts refuses to post without the modules posting depends on.
//
// A build missing one of these could otherwise post an invoice with no price, no tax, or no stock
// movement — and the failure would look like a working sale.
func (s *Service) requirePorts() error {
	switch {
	case s.pricing == nil:
		return errs.Internal(CodePortMissing, "sales cannot post without pricing")
	case s.tax == nil:
		return errs.Internal(CodePortMissing, "sales cannot post without tax")
	case s.stock == nil:
		return errs.Internal(CodePortMissing, "sales cannot post without inventory")
	case s.credit == nil:
		return errs.Internal(CodePortMissing, "sales cannot post without partners")
	case s.bus == nil:
		return errs.Internal(CodePublisherMissing,
			"sales cannot post without an event publisher")
	}
	return nil
}
