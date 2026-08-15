package expenses

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	accountingc "github.com/mizan-erp/mizan/internal/modules/accounting/contract"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/expenses/domain"
)

// AmountByAccount is the amount key an expense's posting uses for one account.
//
// # Why an expense's posting cannot name its accounts in a rule
//
// Every other posting in this application knows which accounts it touches: a sale moves revenue,
// receivables, and tax. An expense does not — a business has forty categories, each with its own
// account, and a rule cannot name them.
//
// So the DOCUMENT carries the accounts (snapshotted on its lines) and the rule says "debit what
// the document says". The rule still decides the OTHER side — where the money came from — which
// is the part that differs by payment method and is exactly what §20.3 exists for.
const AmountByAccount = "document.by_account"

// NewExpenseInput opens an expense.
type NewExpenseInput struct {
	CompanyID   id.ID
	BranchID    id.ID
	PartnerID   id.ID
	PayeeName   string
	ExpenseDate string
	Reference   string
	Description string
	Settlement  domain.Settlement
	PaidMethod  domain.Method
	DueDate     string
	Currency    string

	IsTemplate        bool
	RecursEveryMonths int
}

// Draft opens an expense.
func (s *Service) Draft(ctx context.Context, in NewExpenseInput) (domain.Expense, error) {
	identifier, err := id.New()
	if err != nil {
		return domain.Expense{}, err
	}

	expense, err := domain.NewExpense(identifier, in.CompanyID, in.BranchID,
		in.PayeeName, in.ExpenseDate, in.Currency, in.Settlement, in.PaidMethod)
	if err != nil {
		return domain.Expense{}, err
	}
	expense.PartnerID = in.PartnerID
	expense.Reference = in.Reference
	expense.Description = in.Description
	expense.DueDate = in.DueDate
	expense.IsTemplate = in.IsTemplate
	expense.RecursEveryMonths = in.RecursEveryMonths

	if err = s.db.Do(ctx, func(txCtx context.Context) error {
		if err = s.repos.InsertExpense(txCtx, expense, s.actorOf(txCtx)); err != nil {
			return err
		}
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionExpenseDrafted, EntityType: EntityExpense, EntityID: expense.ID,
			After: map[string]any{
				"payee": expense.PayeeName, "date": expense.ExpenseDate,
				"settlement": string(expense.Settlement),
			},
		})
	}); err != nil {
		return domain.Expense{}, err
	}
	return expense, nil
}

// AddLineInput puts a category and an amount on an expense.
type AddLineInput struct {
	ExpenseID   id.ID
	CategoryID  id.ID
	Description string
	NetMinor    int64
}

// AddLine puts a category and an amount on a draft expense.
func (s *Service) AddLine(ctx context.Context, in AddLineInput) (domain.Line, error) {
	var line domain.Line

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		expense, err := s.requireExpense(txCtx, in.ExpenseID)
		if err != nil {
			return err
		}
		if err = expense.RequireDraft(); err != nil {
			return err
		}

		category, found, err := s.repos.CategoryByID(txCtx, in.CategoryID)
		if err != nil {
			return err
		}
		if !found {
			return errs.NotFound(CodeUnknownCategory,
				"there is no expense category with that identity")
		}

		position, err := s.repos.NextLineNumber(txCtx, in.ExpenseID)
		if err != nil {
			return err
		}
		identifier, err := id.New()
		if err != nil {
			return err
		}

		// The account and the name are SNAPSHOTTED here (§9.3). A category re-pointed at a
		// different account next year must not move where a posted expense landed.
		line, err = domain.NewLine(identifier, category.ID, category.AccountID,
			position, category.Name, in.NetMinor)
		if err != nil {
			return err
		}
		line.Description = in.Description
		line.TaxRecoverable = category.TaxRecoverable

		if err = s.repos.InsertLine(txCtx, in.ExpenseID, line); err != nil {
			return err
		}
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionExpenseLineAdded, EntityType: EntityExpense, EntityID: in.ExpenseID,
			After: map[string]any{
				"category": category.Code, "net": in.NetMinor,
			},
		})
	})
	if err != nil {
		return domain.Line{}, err
	}
	return line, nil
}

// RemoveLine takes a line off a draft.
func (s *Service) RemoveLine(ctx context.Context, expenseID, lineID id.ID) error {
	return s.db.Do(ctx, func(txCtx context.Context) error {
		expense, err := s.requireExpense(txCtx, expenseID)
		if err != nil {
			return err
		}
		if err = expense.RequireDraft(); err != nil {
			return err
		}
		return s.repos.DeleteLine(txCtx, lineID)
	})
}

// Record posts an expense: taxes it, numbers it, and writes the entry.
//
// # One transaction, and the tax decided per LINE
//
// Most jurisdictions disallow reclaiming tax on entertainment and allow it on fuel, and a single
// expense claim routinely carries both. Deciding it per document would force somebody to split
// one receipt into two, which is how the rule gets ignored.
func (s *Service) Record(ctx context.Context, expenseID id.ID) (domain.Expense, error) {
	if s.tax == nil {
		return domain.Expense{}, errs.Internal(CodePortMissing,
			"the expenses service was built without tax")
	}

	var posted domain.Expense
	err := s.db.Do(ctx, func(txCtx context.Context) error {
		expense, err := s.requireExpense(txCtx, expenseID)
		if err != nil {
			return err
		}
		if err = expense.RequirePostable(); err != nil {
			return err
		}

		lines, err := s.repos.Lines(txCtx, expenseID)
		if err != nil {
			return err
		}
		if len(lines) == 0 {
			return errs.Validation(domain.CodeNoLines,
				"an expense with no lines spends nothing")
		}

		for i, line := range lines {
			taxed, taxErr := s.tax.TaxFor(txCtx, TaxQuery{
				CompanyID: expense.CompanyID, PartnerID: expense.PartnerID,
				NetMinor: line.NetMinor, Date: expense.ExpenseDate,
			})
			if taxErr != nil {
				return taxErr
			}
			line = line.Tax(taxed.AmountMinor, taxed.RateMicro, taxed.Code, line.TaxRecoverable)
			if err = s.repos.UpdateLine(txCtx, line); err != nil {
				return err
			}
			lines[i] = line
		}

		var recoverable int64
		expense.NetMinor, expense.TaxMinor, expense.TotalMinor, recoverable =
			domain.Totals(lines)
		expense.Status = domain.Posted

		if expense.Number, err = s.allocate(
			txCtx, expense.BranchID, SeriesExpense); err != nil {
			return err
		}
		if err = s.repos.UpdateExpense(txCtx, expense, s.actorOf(txCtx)); err != nil {
			return err
		}
		if err = s.publishPosting(txCtx, expense, lines, recoverable); err != nil {
			return err
		}

		posted = expense
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionExpenseRecorded, EntityType: EntityExpense, EntityID: expenseID,
			After: map[string]any{
				"number": expense.Number, "total": expense.TotalMinor,
				"payee": expense.PayeeName,
			},
		})
	})
	if err != nil {
		return domain.Expense{}, err
	}
	return posted, nil
}

// publishPosting fires the rule the SETTLEMENT and METHOD name.
//
// The document carries the debits — one per account its categories point at — and the rule decides
// the credit: the till, the bank, or payables. That split is the only shape that works when the
// debit side is open-ended and the credit side is not.
func (s *Service) publishPosting(
	ctx context.Context, expense domain.Expense, lines []domain.Line, recoverableMinor int64,
) error {
	date, ok := clock.ParseDate(expense.ExpenseDate)
	if !ok {
		return errs.Internal(domain.CodeInvalidExpense,
			"an expense has an unreadable date").WithParam("date", expense.ExpenseDate)
	}

	postable := accountingc.Postable{
		Action:    expense.PostingAction(),
		CompanyID: expense.CompanyID,
		BranchID:  expense.BranchID,
		Date:      date,

		DocumentType:   EntityExpense,
		DocumentID:     expense.ID,
		DocumentNumber: expense.Number,

		Amounts: map[string]int64{
			accountingc.AmountTotal: expense.TotalMinor,
			accountingc.AmountNet:   expense.NetMinor,
			// Only the RECOVERABLE part reaches the tax asset. The rest is part of what the
			// thing cost and is already in the per-account debits below.
			accountingc.AmountTax: recoverableMinor,
		},
		// The debits, one per account. Named by the DOCUMENT because a rule cannot know forty
		// expense accounts — but the rule still decides where the money came from.
		AccountAmounts: byAccount(lines),

		CurrencyCode: expense.CurrencyCode,
		RateMicro:    expense.ExchangeRateMicro,
		PartnerID:    expense.PartnerID,
		Memo:         expense.Reference,
	}
	return s.bus.Publish(ctx, postable)
}

func byAccount(lines []domain.Line) map[id.ID]int64 {
	return domain.CostByAccount(lines)
}

// Cancel abandons a draft expense.
func (s *Service) Cancel(ctx context.Context, expenseID id.ID) error {
	return s.db.Do(ctx, func(txCtx context.Context) error {
		expense, err := s.requireExpense(txCtx, expenseID)
		if err != nil {
			return err
		}
		if err = expense.RequireDraft(); err != nil {
			return err
		}
		expense.Status = domain.Cancelled
		if err = s.repos.UpdateExpense(txCtx, expense, s.actorOf(txCtx)); err != nil {
			return err
		}
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionExpenseCancelled, EntityType: EntityExpense, EntityID: expenseID,
		})
	})
}

// Expense reads one expense with its lines.
func (s *Service) Expense(
	ctx context.Context, expenseID id.ID,
) (domain.Expense, []domain.Line, error) {
	expense, found, err := s.repos.ExpenseByID(ctx, expenseID)
	if err != nil {
		return domain.Expense{}, nil, err
	}
	if !found {
		return domain.Expense{}, nil, errs.NotFound(CodeUnknownExpense,
			"there is no expense with that identity")
	}
	lines, err := s.repos.Lines(ctx, expenseID)
	if err != nil {
		return domain.Expense{}, nil, err
	}
	return expense, lines, nil
}

// Expenses lists a company's expenses.
func (s *Service) Expenses(
	ctx context.Context, companyID id.ID, status domain.Status,
) ([]domain.Expense, error) {
	return s.repos.Expenses(ctx, companyID, status, false)
}

// Templates lists a company's recurring expense templates.
func (s *Service) Templates(
	ctx context.Context, companyID id.ID,
) ([]domain.Expense, error) {
	return s.repos.Expenses(ctx, companyID, "", true)
}

func (s *Service) requireExpense(
	ctx context.Context, expenseID id.ID,
) (domain.Expense, error) {
	expense, found, err := s.repos.ExpenseByID(ctx, expenseID)
	if err != nil {
		return domain.Expense{}, err
	}
	if !found {
		return domain.Expense{}, errs.NotFound(CodeUnknownExpense,
			"there is no expense with that identity")
	}
	return expense, nil
}

func (s *Service) allocate(
	ctx context.Context, branchID id.ID, series string,
) (string, error) {
	if s.numbers == nil {
		return "", errs.Internal(CodePortMissing,
			"the expenses service was built without a numbering port")
	}
	return s.numbers.Allocate(ctx, branchID, series)
}
