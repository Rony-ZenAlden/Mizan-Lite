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

// The audited actions and codes this step adds.
const (
	ActionPaymentDrafted = "sales.payment.drafted"
	ActionPaymentPosted  = "sales.payment.posted"

	EntityPayment = "sales.payment"

	// SeriesPayment numbers receipts, separately from invoices.
	SeriesPayment = "SALES_PAYMENT"

	CodeUnknownPayment = "sales.unknown_payment"
)

// paymentAction is the posting action for a method.
//
// # Why the METHOD is part of the action
//
// Cash goes to the till; a card or a transfer goes to the bank. Phase 2's seeded rule debited
// `mapping:CASH` for every payment, which is right for a market stall and wrong for anybody who
// takes cards — and it had looked correct for three phases because nothing had ever fired it.
//
// The fix is not a `case` in this module deciding which account to name. It is a distinct ACTION
// per method, so the rules differ and sales still names no account (§20.3). A business whose card
// settlements land in a third account changes a seed file.
func paymentAction(method domain.Method) string {
	return "sales.payment." + string(method) + ".received"
}

// NewPaymentInput records money received.
type NewPaymentInput struct {
	CompanyID   id.ID
	BranchID    id.ID
	PartnerID   id.ID
	PartnerName string
	Date        string
	Method      domain.Method
	Reference   string
	Currency    string
	AmountMinor int64
	Notes       string

	// ShiftID is the till that took the money. Empty for a payment taken outside the POS — a
	// bank transfer arriving while the shop is shut belongs to no till, and forcing one would
	// make the shift's cash figure include money that never reached its drawer.
	ShiftID id.ID

	// Settle names the documents this payment clears, and by how much. Empty is a deposit
	// against nothing yet, which is a real thing a shop takes.
	Settle []SettleInput
}

// SettleInput is one document a payment clears.
type SettleInput struct {
	DocumentID  id.ID
	AmountMinor int64
}

// TakePayment records money received and what it settles.
//
// # Drafted and posted in one call
//
// Unlike an invoice, which is built up line by line over minutes, a payment is a single fact
// known in full at the moment it happens: this much arrived, this way, against these invoices.
// Splitting it into draft-then-post would add a state a till has no use for — and a payment left
// in draft is money the books have not heard about.
func (s *Service) TakePayment(ctx context.Context, in NewPaymentInput) (domain.Payment, error) {
	if s.bus == nil {
		return domain.Payment{}, errs.Internal(CodePublisherMissing,
			"sales cannot take a payment without an event publisher")
	}

	var taken domain.Payment

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		identifier, err := id.New()
		if err != nil {
			return err
		}
		payment, err := domain.NewPayment(identifier, in.BranchID, in.Method,
			in.Date, in.Currency, in.AmountMinor)
		if err != nil {
			return err
		}
		payment.PartnerID = in.PartnerID
		payment.PartnerName = in.PartnerName
		payment.Reference = in.Reference
		payment.Notes = in.Notes

		allocations := make([]domain.Allocation, 0, len(in.Settle))
		for _, settle := range in.Settle {
			allocations = append(allocations, domain.Allocation{
				DocumentID: settle.DocumentID, AmountMinor: settle.AmountMinor,
			})
		}
		if err = payment.RequireAllocatable(allocations); err != nil {
			return err
		}

		// Each document must have room for what is being allocated to it. Checked against what
		// is ALREADY settled, so two payments cannot each clear the same invoice in full.
		for _, allocation := range allocations {
			document, docErr := s.requireDocument(txCtx, allocation.DocumentID)
			if docErr != nil {
				return docErr
			}
			if document.Status != domain.Posted {
				return errs.Conflict(domain.CodeNothingToSettle,
					"only a posted document can be settled").
					WithParam("id", string(allocation.DocumentID))
			}
			settled, settledErr := s.repos.SettledOn(txCtx, allocation.DocumentID)
			if settledErr != nil {
				return settledErr
			}
			outstanding := domain.Outstanding(document.TotalMinor, settled)
			if allocation.AmountMinor > outstanding {
				return errs.Validation(domain.CodeOverSettled,
					"more is being settled than this document still owes").
					WithParam("allocated", itoa(allocation.AmountMinor)).
					WithParam("outstanding", itoa(outstanding))
			}
		}

		if err = s.repos.InsertPayment(
			txCtx, in.CompanyID, payment, s.actorOf(txCtx)); err != nil {
			return err
		}
		for _, allocation := range allocations {
			allocationID, allocErr := id.New()
			if allocErr != nil {
				return allocErr
			}
			if err = s.repos.InsertAllocation(
				txCtx, allocationID, payment.ID, allocation); err != nil {
				return err
			}
		}

		// A credit sale moves no money: the customer will pay later, and the invoice stays
		// outstanding. Numbering it and posting an entry would show a shop as having been paid
		// for everything it had ever sold.
		if !payment.Method.SettlesImmediately() {
			taken = payment
			return s.audit(txCtx, auditc.Auditable{
				Action: ActionPaymentDrafted, EntityType: EntityPayment,
				EntityID: payment.ID,
				After: map[string]any{
					"method": string(payment.Method), "amount_minor": payment.AmountMinor,
				},
			})
		}

		number, err := s.allocateNumber(txCtx, payment.BranchID, SeriesPayment)
		if err != nil {
			return err
		}
		payment.Number = number
		payment.PostedAt = clock.Format(s.clk.Now())
		payment.Status = domain.Posted

		if err = s.repos.SetPaymentPosted(
			txCtx, payment.ID, number, payment.PostedAt); err != nil {
			return err
		}
		if !in.ShiftID.IsZero() {
			if err = s.repos.AttachPaymentToShift(
				txCtx, payment.ID, in.ShiftID); err != nil {
				return err
			}
		}
		if err = s.publishPayment(txCtx, in.CompanyID, payment); err != nil {
			return err
		}
		taken = payment

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionPaymentPosted, EntityType: EntityPayment, EntityID: payment.ID,
			After: map[string]any{
				"number": number, "method": string(payment.Method),
				"amount_minor": payment.AmountMinor, "settles": len(allocations),
			},
		})
	})
	if err != nil {
		return domain.Payment{}, err
	}
	return taken, nil
}

// publishPayment announces money received to the books.
func (s *Service) publishPayment(
	ctx context.Context, companyID id.ID, payment domain.Payment,
) error {
	date, ok := clock.ParseDate(payment.Date)
	if !ok {
		return errs.Internal(domain.CodeInvalidPayment,
			"a payment has an unreadable date").WithParam("date", payment.Date)
	}

	return s.bus.Publish(ctx, accountingc.Postable{
		Action:    paymentAction(payment.Method),
		CompanyID: companyID,
		BranchID:  payment.BranchID,
		Date:      date,

		DocumentType:   EntityPayment,
		DocumentID:     payment.ID,
		DocumentNumber: payment.Number,

		Amounts: map[string]int64{
			accountingc.AmountTotal: payment.AmountMinor,
		},

		CurrencyCode: payment.CurrencyCode,
		RateMicro:    payment.RateMicro,
		PartnerID:    payment.PartnerID,
		Memo:         payment.Reference,
	})
}

// Payment reads one payment with what it settles.
func (s *Service) Payment(
	ctx context.Context, paymentID id.ID,
) (domain.Payment, []domain.Allocation, error) {
	payment, found, err := s.repos.PaymentByID(ctx, paymentID)
	if err != nil {
		return domain.Payment{}, nil, err
	}
	if !found {
		return domain.Payment{}, nil, errs.NotFound(CodeUnknownPayment,
			"there is no payment with that identity")
	}
	allocations, err := s.repos.Allocations(ctx, paymentID)
	if err != nil {
		return domain.Payment{}, nil, err
	}
	return payment, allocations, nil
}

// Outstanding reports what a document still owes.
//
// Derived: the document's total less what posted payments have settled. Never a maintained
// column, which would drift from the allocations that justify it — and the drift would show up as
// a customer chased for money they had paid.
func (s *Service) Outstanding(ctx context.Context, documentID id.ID) (int64, error) {
	document, found, err := s.repos.DocumentByID(ctx, documentID)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, errs.NotFound(CodeUnknownDocument,
			"there is no sales document with that identity")
	}
	settled, err := s.repos.SettledOn(ctx, documentID)
	if err != nil {
		return 0, err
	}
	return domain.Outstanding(document.TotalMinor, settled), nil
}
