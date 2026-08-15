package expenses

import (
	"context"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/settle"
	accountingc "github.com/mizan-erp/mizan/internal/modules/accounting/contract"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/expenses/domain"
)

// The audited actions settling an expense leaves.
const (
	ActionSettlementPosted = "expenses.settlement.recorded"

	EntitySettlement = "expenses.settlement"
)

// PermSettlePost gates paying what an expense left owed.
//
// Separate from recording the expense, which is the last side of the same segregation purchasing
// keeps: somebody who can both enter a bill and pay it can pay themselves.
const PermSettlePost = "expenses.settlement.post"

// SeriesSettlement numbers expense settlements, separately from expenses.
const SeriesSettlement = "EXPENSE_PAYMENT"

// SettleInput pays off expenses recorded on account.
type SettleInput struct {
	CompanyID   id.ID
	BranchID    id.ID
	PartnerID   id.ID
	PayeeName   string
	PaymentDate string
	Method      domain.Method
	Reference   string
	Currency    string
	AmountMinor int64
	// Settle names the expenses this payment clears. It may be empty — money paid on account
	// against a landlord's running balance is real, and forcing an allocation would mean
	// inventing one.
	Settle []Allocation
}

// Allocation is a settlement clearing part of one expense.
type Allocation struct {
	ExpenseID   id.ID
	AmountMinor int64
}

// Settle records money paying off expenses that were recorded on account.
//
// # One transaction: the payment, its allocations, its number, and the entry
//
// A settlement that reached the books without its allocations would show the money gone and every
// expense still owed — so somebody pays the landlord twice.
func (s *Service) Settle(ctx context.Context, in SettleInput) (domain.Payment, error) {
	identifier, err := id.New()
	if err != nil {
		return domain.Payment{}, err
	}
	payment, err := domain.NewPayment(identifier, in.CompanyID, in.BranchID,
		in.PayeeName, in.PaymentDate, in.Currency, in.Method, in.AmountMinor)
	if err != nil {
		return domain.Payment{}, err
	}
	payment.PartnerID = in.PartnerID
	payment.Reference = in.Reference

	if err = requireAllocatable(payment.AmountMinor, in.Settle); err != nil {
		return domain.Payment{}, err
	}

	var posted domain.Payment
	if err = s.db.Do(ctx, func(txCtx context.Context) error {
		if err = s.repos.InsertPayment(txCtx, payment, s.actorOf(txCtx)); err != nil {
			return err
		}

		for _, allocation := range in.Settle {
			expense, expErr := s.requireExpense(txCtx, allocation.ExpenseID)
			if expErr != nil {
				return expErr
			}
			// Only an expense recorded ON ACCOUNT can be settled. One paid immediately is
			// already gone, and settling it again would credit the bank twice for one payment.
			if expErr = expense.RequireSettleable(); expErr != nil {
				return expErr
			}

			settled, settledErr := s.repos.SettledMinor(txCtx, expense.ID)
			if settledErr != nil {
				return settledErr
			}
			if err = settle.RequireSettleable(
				expense.TotalMinor, settled, allocation.AmountMinor); err != nil {
				return errs.Conflict(domain.CodeOverSettled,
					"that would pay more than the expense is for").
					WithParam("expense", expense.Number).
					WithParam("outstanding",
						settle.Itoa(settle.Outstanding(expense.TotalMinor, settled)))
			}

			if err = s.repos.InsertAllocation(
				txCtx, payment.ID, expense.ID, allocation.AmountMinor); err != nil {
				return err
			}
		}

		payment.Status = domain.PaymentPosted
		if payment.Number, err = s.allocate(
			txCtx, payment.BranchID, SeriesSettlement); err != nil {
			return err
		}
		if err = s.repos.UpdatePayment(txCtx, payment, s.actorOf(txCtx)); err != nil {
			return err
		}
		if err = s.publishSettlementPosting(txCtx, payment); err != nil {
			return err
		}

		posted = payment
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionSettlementPosted, EntityType: EntitySettlement, EntityID: payment.ID,
			After: map[string]any{
				"number": payment.Number, "method": string(payment.Method),
				"amount": payment.AmountMinor, "payee": payment.PayeeName,
			},
		})
	}); err != nil {
		return domain.Payment{}, err
	}
	return posted, nil
}

// requireAllocatable translates the kernel's rule into this module's codes.
//
// The arithmetic lives in `kernel/settle` because three modules needed it; the CODES stay here,
// because a code is an i18n key and a screen renders it.
func requireAllocatable(paymentMinor int64, allocations []Allocation) error {
	amounts := make([]settle.Allocation, 0, len(allocations))
	for _, allocation := range allocations {
		amounts = append(amounts, settle.Allocation{AmountMinor: allocation.AmountMinor})
	}

	switch err := settle.RequireAllocatable(paymentMinor, amounts); {
	case errors.Is(err, settle.ErrNonPositive):
		return errs.Validation(domain.CodeNonPositiveAmount,
			"an allocation must be for a positive amount")
	case errors.Is(err, settle.ErrOverAllocated):
		return errs.Validation(domain.CodeOverAllocated,
			"that allocates more than the payment is worth").
			WithParam("payment", settle.Itoa(paymentMinor)).
			WithParam("allocated", settle.Itoa(settle.Total(amounts)))
	case err != nil:
		return errs.Validation(domain.CodeOverAllocated, err.Error())
	}
	return nil
}

func (s *Service) publishSettlementPosting(
	ctx context.Context, payment domain.Payment,
) error {
	date, ok := clock.ParseDate(payment.PaymentDate)
	if !ok {
		return errs.Internal(domain.CodeInvalidPayment,
			"an expense settlement has an unreadable date")
	}

	return s.bus.Publish(ctx, accountingc.Postable{
		Action:    payment.PostingAction(),
		CompanyID: payment.CompanyID,
		BranchID:  payment.BranchID,
		Date:      date,

		DocumentType:   EntitySettlement,
		DocumentID:     payment.ID,
		DocumentNumber: payment.Number,

		Amounts: map[string]int64{
			accountingc.AmountTotal: payment.AmountMinor,
			accountingc.AmountNet:   payment.AmountMinor,
		},

		CurrencyCode: payment.CurrencyCode,
		RateMicro:    1_000_000,
		PartnerID:    payment.PartnerID,
		Memo:         payment.Reference,
	})
}

// OutstandingOn reports what an expense still owes.
//
// Derived from its allocations, never stored. A maintained figure drifts from what justifies it,
// and the drift shows up as a landlord paid twice.
func (s *Service) OutstandingOn(ctx context.Context, expenseID id.ID) (int64, error) {
	expense, err := s.requireExpense(ctx, expenseID)
	if err != nil {
		return 0, err
	}
	// An expense paid when it was recorded owes nothing, whatever the allocations say.
	if expense.Settlement != domain.OnAccount {
		return 0, nil
	}
	settled, err := s.repos.SettledMinor(ctx, expenseID)
	if err != nil {
		return 0, err
	}
	return settle.Outstanding(expense.TotalMinor, settled), nil
}

// Unsettled lists what is still owed, oldest due first.
//
// The report somebody opens to decide what to pay this week.
func (s *Service) Unsettled(
	ctx context.Context, companyID id.ID,
) ([]domain.Expense, error) {
	return s.repos.UnsettledExpenses(ctx, companyID)
}

// Settlements lists a company's expense settlements.
func (s *Service) Settlements(
	ctx context.Context, companyID id.ID,
) ([]domain.Payment, error) {
	return s.repos.Payments(ctx, companyID)
}
