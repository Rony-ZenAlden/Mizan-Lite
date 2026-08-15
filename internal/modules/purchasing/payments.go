package purchasing

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/settle"
	accountingc "github.com/mizan-erp/mizan/internal/modules/accounting/contract"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/purchasing/domain"
)

// The audited actions a supplier payment leaves.
const (
	ActionPaymentDrafted = "purchasing.payment.drafted"
	ActionPaymentPosted  = "purchasing.payment.posted"

	EntityPayment = "purchasing.payment"
)

// The permissions paying a supplier needs.
//
// Separate from billing, and that separation is the last side of the segregation the three-way
// match depends on: somebody who can enter a bill AND pay it can pay themselves.
const (
	PermPaymentView = "purchasing.payment.view"
	PermPaymentPost = "purchasing.payment.post"
)

// NewPaymentInput pays a supplier.
type NewPaymentInput struct {
	CompanyID   id.ID
	BranchID    id.ID
	PartnerID   id.ID
	PartnerName string
	PaymentDate string
	Method      domain.Method
	Reference   string
	Currency    string
	AmountMinor int64
	Notes       string
	// Settle names the bills this payment clears. It may be empty — a prepayment against future
	// orders is a real thing and must not require inventing an allocation.
	Settle []domain.Allocation
}

// Pay records money going out to a supplier and posts it.
//
// # One transaction: the payment, its allocations, its number, and the entry
//
// A payment that reached the books without its allocations would show the supplier paid and every
// bill still outstanding — so somebody chases them for money already sent.
func (s *Service) Pay(ctx context.Context, in NewPaymentInput) (domain.Payment, error) {
	if s.bus == nil {
		return domain.Payment{}, errs.Internal(CodePublisherMissing,
			"the purchasing service was built without an event publisher")
	}

	identifier, err := id.New()
	if err != nil {
		return domain.Payment{}, err
	}
	payment, err := domain.NewPayment(identifier, in.CompanyID, in.BranchID,
		in.PartnerID, in.PartnerName, in.PaymentDate, in.Currency, in.Method, in.AmountMinor)
	if err != nil {
		return domain.Payment{}, err
	}
	payment.Reference = in.Reference
	payment.Notes = in.Notes

	if err = domain.RequireAllocatable(payment.AmountMinor, in.Settle); err != nil {
		return domain.Payment{}, err
	}

	var posted domain.Payment
	if err = s.db.Do(ctx, func(txCtx context.Context) error {
		if err = s.repos.InsertPayment(txCtx, payment, s.actorOf(txCtx)); err != nil {
			return err
		}

		for _, allocation := range in.Settle {
			bill, found, billErr := s.repos.BillByID(txCtx, allocation.BillID)
			if billErr != nil {
				return billErr
			}
			if !found {
				return errs.NotFound(CodeUnknownBill,
					"there is no purchase bill with that identity")
			}
			// A draft bill is not yet an obligation. Paying one would settle a debt the books do
			// not carry, leaving the payable negative and the bill still unposted.
			if bill.Status != domain.BillPosted {
				return errs.Conflict(domain.CodeInvalidBill,
					"only a posted bill can be paid")
			}
			if bill.PartnerID != payment.PartnerID {
				return errs.Validation(domain.CodeInvalidLine,
					"that bill belongs to a different supplier")
			}

			// Paying more than a bill owes is not generosity: it is a keying error that leaves
			// the payable overdrawn and reconciles against nothing.
			settled, settledErr := s.repos.SettledMinor(txCtx, bill.ID)
			if settledErr != nil {
				return settledErr
			}
			if err = settle.RequireSettleable(
				bill.TotalMinor, settled, allocation.AmountMinor); err != nil {
				return errs.Conflict(domain.CodeOverSettled,
					"that would pay more than the bill is for").
					WithParam("bill", bill.Number).
					WithParam("outstanding",
						settle.Itoa(settle.Outstanding(bill.TotalMinor, settled)))
			}

			if err = s.repos.InsertAllocationFor(txCtx, payment.ID, allocation); err != nil {
				return err
			}
		}

		payment.Status = domain.PaymentPosted
		if payment.Number, err = s.allocate(
			txCtx, payment.BranchID, SeriesPayment); err != nil {
			return err
		}
		if err = s.repos.UpdatePayment(txCtx, payment, s.actorOf(txCtx)); err != nil {
			return err
		}
		if err = s.publishPaymentPosting(txCtx, payment); err != nil {
			return err
		}

		posted = payment
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionPaymentPosted, EntityType: EntityPayment, EntityID: payment.ID,
			After: map[string]any{
				"number": payment.Number, "method": string(payment.Method),
				"amount": payment.AmountMinor, "supplier": payment.PartnerName,
			},
		})
	}); err != nil {
		return domain.Payment{}, err
	}
	return posted, nil
}

// publishPaymentPosting fires the rule the METHOD names.
//
// Phase 2 seeded ONE `supplier_payment` rule crediting CASH whatever the method, so a bank
// transfer would have reduced the till. The identical defect existed on the receiving side and
// 5.5 found it there; both are fixed the same way — four actions, four seeded rules, and no Go
// that knows which account any of them touches (§20.3).
func (s *Service) publishPaymentPosting(
	ctx context.Context, payment domain.Payment,
) error {
	date, ok := clock.ParseDate(payment.PaymentDate)
	if !ok {
		return errs.Internal(domain.CodeInvalidPayment,
			"a supplier payment has an unreadable date")
	}

	return s.bus.Publish(ctx, accountingc.Postable{
		Action:    payment.PostingAction(),
		CompanyID: payment.CompanyID,
		BranchID:  payment.BranchID,
		Date:      date,

		DocumentType:   EntityPayment,
		DocumentID:     payment.ID,
		DocumentNumber: payment.Number,

		Amounts: map[string]int64{
			accountingc.AmountTotal: payment.AmountMinor,
			accountingc.AmountNet:   payment.AmountMinor,
		},

		CurrencyCode: payment.CurrencyCode,
		RateMicro:    payment.ExchangeRateMicro,
		PartnerID:    payment.PartnerID,
		Memo:         payment.Reference,
	})
}

// Payment reads one payment with what it settled.
func (s *Service) Payment(
	ctx context.Context, paymentID id.ID,
) (domain.Payment, []domain.Allocation, error) {
	payment, found, err := s.repos.PaymentByID(ctx, paymentID)
	if err != nil {
		return domain.Payment{}, nil, err
	}
	if !found {
		return domain.Payment{}, nil, errs.NotFound(CodeUnknownPayment,
			"there is no supplier payment with that identity")
	}
	allocations, err := s.repos.AllocationsOfPayment(ctx, paymentID)
	if err != nil {
		return domain.Payment{}, nil, err
	}
	return payment, allocations, nil
}

// Payments lists a company's supplier payments.
func (s *Service) Payments(
	ctx context.Context, companyID id.ID, status domain.PaymentStatus,
) ([]domain.Payment, error) {
	return s.repos.Payments(ctx, companyID, status)
}

// OutstandingOnBill reports what a bill still owes.
//
// Derived on read from the allocations, never stored. A maintained figure drifts from the
// allocations that justify it, and the drift is invisible until somebody chases a supplier for
// money already sent.
func (s *Service) OutstandingOnBill(ctx context.Context, billID id.ID) (int64, error) {
	bill, found, err := s.repos.BillByID(ctx, billID)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, errs.NotFound(CodeUnknownBill,
			"there is no purchase bill with that identity")
	}
	settled, err := s.repos.SettledMinor(ctx, billID)
	if err != nil {
		return 0, err
	}
	return settle.Outstanding(bill.TotalMinor, settled), nil
}
