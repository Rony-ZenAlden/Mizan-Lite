package sales

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/sales/domain"
)

// The audited action and codes this step adds.
const (
	ActionCreditNoteDrafted = "sales.credit_note.drafted"

	CodeNotAnInvoice      = "sales.not_an_invoice"
	CodeInvoiceNotPosted  = "sales.invoice_not_posted"
	CodeReturnTooMuch     = "sales.return_exceeds_sale"
	CodeUnknownSourceLine = "sales.unknown_source_line"
)

// ReturnInput describes what a customer is bringing back.
type ReturnInput struct {
	CompanyID id.ID
	// InvoiceID is the posted sale being reversed. Required: a credit note against nothing is a
	// refund with no record of what was refunded.
	InvoiceID id.ID
	Date      string
	Reason    string

	// Lines names what comes back and how much of it. Empty means the whole invoice, which is
	// the common case — a customer returning everything should not require the operator to list
	// it.
	Lines []ReturnLineInput
}

// ReturnLineInput is one line coming back.
type ReturnLineInput struct {
	// SourceLineID is the invoice line being reversed. This is what makes §D.3 reachable: the
	// line knows its stock movement, and that movement knows what the goods cost when they left.
	SourceLineID  id.ID
	QuantityMicro int64
}

// DraftReturn opens a credit note against a posted invoice.
//
// # Why a credit note rather than editing the invoice
//
// A posted invoice is immutable (§1.4). Editing it would silently restate a period whose books
// may be closed, and would leave the stock movement and the journal entry describing a document
// that no longer says what they were made from. A credit note is a second document that says what
// changed, and both remain readable.
//
// # Every line carries its source
//
// Not for tidiness: it is the only path to the ORIGINAL cost. §D.3 requires a return to be costed
// at what the goods cost when they were sold, not at today's average — returning an item bought
// at last year's price at today's cost invents profit out of a customer changing their mind.
func (s *Service) DraftReturn(ctx context.Context, in ReturnInput) (domain.Document, error) {
	var created domain.Document

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		invoice, err := s.requireDocument(txCtx, in.InvoiceID)
		if err != nil {
			return err
		}
		if invoice.Type != domain.Invoice {
			return errs.Validation(CodeNotAnInvoice,
				"only an invoice can be credited").WithParam("type", string(invoice.Type))
		}
		if invoice.Status != domain.Posted {
			// An unposted invoice is corrected by editing it — that is what draft means. A
			// credit note against a draft would be a reversal of something that never happened.
			return errs.Conflict(CodeInvoiceNotPosted,
				"only a posted invoice can be credited").WithParam("id", string(in.InvoiceID))
		}

		sold, err := s.repos.Lines(txCtx, in.InvoiceID)
		if err != nil {
			return err
		}
		byID := make(map[id.ID]domain.Line, len(sold))
		for _, line := range sold {
			byID[line.ID] = line
		}

		// Already-credited quantities, so a customer cannot return three of two.
		credited, err := s.repos.CreditedQuantities(txCtx, in.InvoiceID)
		if err != nil {
			return err
		}

		wanted := in.Lines
		if len(wanted) == 0 {
			// The whole invoice, less anything already returned. The common case, and one an
			// operator should not have to enumerate.
			wanted = make([]ReturnLineInput, 0, len(sold))
			for _, line := range sold {
				remaining := line.QuantityMicro - credited[line.ID]
				if remaining <= 0 {
					continue
				}
				wanted = append(wanted, ReturnLineInput{
					SourceLineID: line.ID, QuantityMicro: remaining,
				})
			}
		}
		if len(wanted) == 0 {
			return errs.Conflict(CodeReturnTooMuch,
				"every line on this invoice has already been credited").
				WithParam("id", string(in.InvoiceID))
		}

		date := in.Date
		if date == "" {
			date = invoice.Date
		}

		identifier, err := id.New()
		if err != nil {
			return err
		}
		note, err := domain.NewDocument(
			identifier, invoice.BranchID, domain.CreditNote, date, invoice.CurrencyCode)
		if err != nil {
			return err
		}
		note.WarehouseID = invoice.WarehouseID
		note.PartnerID = invoice.PartnerID
		note.PartnerName = invoice.PartnerName
		note.SourceID = invoice.ID
		note.Notes = in.Reason

		if err = s.repos.InsertDocument(
			txCtx, in.CompanyID, note, s.actorOf(txCtx)); err != nil {
			return err
		}

		for position, request := range wanted {
			source, ok := byID[request.SourceLineID]
			if !ok {
				return errs.NotFound(CodeUnknownSourceLine,
					"that line is not on this invoice").
					WithParam("line", string(request.SourceLineID))
			}
			if request.QuantityMicro <= 0 {
				return errs.Validation(domain.CodeInvalidLine,
					"a returned quantity must be positive")
			}
			// Three of two is a mistake, and letting it through would put stock on the shelf
			// that was never sold and credit money that was never taken.
			if remaining := source.QuantityMicro - credited[source.ID]; request.QuantityMicro > remaining {
				return errs.Conflict(CodeReturnTooMuch,
					"more is being returned than was sold").
					WithParam("requested", itoa(request.QuantityMicro)).
					WithParam("available", itoa(remaining))
			}

			lineID, lineErr := id.New()
			if lineErr != nil {
				return lineErr
			}
			line, lineErr := domain.NewLine(lineID, source.ProductID, source.VariantID,
				source.UomID, position+1, request.QuantityMicro)
			if lineErr != nil {
				return lineErr
			}

			// The SNAPSHOT is copied from the invoice, not re-fetched from the catalog. A credit
			// note must describe what was sold — including what the product was called then —
			// and re-fetching would make a return of a renamed product say something the
			// original invoice does not.
			line.ProductName = source.ProductName
			line.VariantSKU = source.VariantSKU
			line.UomCode = source.UomCode
			line.QuantityStockMicro = proportional(
				source.QuantityStockMicro, request.QuantityMicro, source.QuantityMicro)
			line.LotID = source.LotID
			line.SerialID = source.SerialID
			line.SourceLineID = source.ID

			if err = s.repos.InsertLine(txCtx, note.ID, line); err != nil {
				return err
			}
		}
		created = note

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionCreditNoteDrafted, EntityType: EntityDocument, EntityID: note.ID,
			After: map[string]any{
				"invoice": invoice.Number, "lines": len(wanted), "reason": in.Reason,
			},
		})
	})
	if err != nil {
		return domain.Document{}, err
	}
	return created, nil
}

// proportional scales a stock quantity to a partial return.
//
// Returning 1 of 3 rolls returns a third of the metres. Computed from the ORIGINAL line's two
// quantities rather than re-converting, so a partial return uses the same factor the sale did —
// even if the unit's conversion factor has been edited since.
func proportional(stockTotal, returning, soldTotal int64) int64 {
	if soldTotal == 0 {
		return 0
	}
	if returning == soldTotal {
		// The whole line: no arithmetic, no rounding, no chance of drift.
		return stockTotal
	}
	return stockTotal * returning / soldTotal
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}
