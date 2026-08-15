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

// The audited actions a debt leaves.
const (
	ActionDebtDrafted   = "expenses.debt.drafted"
	ActionDebtRecorded  = "expenses.debt.recorded"
	ActionDebtCancelled = "expenses.debt.cancelled"

	EntityDebt = "expenses.debt"
)

// The permissions a debt needs.
//
// Separate from expenses, and deliberately harder to hold: money moving with no trade document
// behind it is the shape every misappropriation takes, and "the owner drew 40,000" is a sentence
// somebody should have had to be authorised to write.
const (
	PermDebtView   = "expenses.debt.view"
	PermDebtRecord = "expenses.debt.record"
)

// SeriesDebt numbers debts.
const SeriesDebt = "DEBT"

// NewDebtInput records money in or out with no trade document.
type NewDebtInput struct {
	CompanyID        id.ID
	BranchID         id.ID
	Direction        domain.Direction
	Kind             domain.Kind
	PartnerID        id.ID
	CounterpartyName string
	DebtDate         string
	DueDate          string
	Method           domain.Method
	Reference        string
	Description      string
	Currency         string
	AmountMinor      int64
}

// RecordDebt records money in or out and posts it.
//
// # Drafted and posted in one call, unlike an expense
//
// An expense is entered, checked, and approved — often by different people, which is why drafting
// and posting are separate grants. A debt is a single act somebody performs with the money in
// their hand: the owner takes 4,000 out of the till, and there is no intermediate state in which
// that has half-happened.
//
// A draft status still exists on the table, because a screen that cannot save a half-typed form
// loses work — but the service posts, and nothing else can leave one drafted.
func (s *Service) RecordDebt(ctx context.Context, in NewDebtInput) (domain.Debt, error) {
	identifier, err := id.New()
	if err != nil {
		return domain.Debt{}, err
	}

	debt, err := domain.NewDebt(identifier, in.CompanyID, in.BranchID,
		in.CounterpartyName, in.DebtDate, in.Currency,
		in.Direction, in.Kind, in.Method, in.AmountMinor)
	if err != nil {
		return domain.Debt{}, err
	}
	debt.PartnerID = in.PartnerID
	debt.DueDate = in.DueDate
	debt.Reference = in.Reference
	debt.Description = in.Description

	if err = debt.RequireBalances(); err != nil {
		return domain.Debt{}, err
	}

	var posted domain.Debt
	if err = s.db.Do(ctx, func(txCtx context.Context) error {
		if err = s.repos.InsertDebt(txCtx, debt, s.actorOf(txCtx)); err != nil {
			return err
		}

		debt.Status = domain.Posted
		if debt.Number, err = s.allocate(txCtx, debt.BranchID, SeriesDebt); err != nil {
			return err
		}
		if err = s.repos.UpdateDebt(txCtx, debt, s.actorOf(txCtx)); err != nil {
			return err
		}
		if err = s.publishDebtPosting(txCtx, debt); err != nil {
			return err
		}

		posted = debt
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionDebtRecorded, EntityType: EntityDebt, EntityID: debt.ID,
			After: map[string]any{
				"number": debt.Number, "direction": string(debt.Direction),
				"kind": string(debt.Kind), "amount": debt.AmountMinor,
				"counterparty": debt.CounterpartyName,
			},
		})
	}); err != nil {
		return domain.Debt{}, err
	}
	return posted, nil
}

func (s *Service) publishDebtPosting(ctx context.Context, debt domain.Debt) error {
	date, ok := clock.ParseDate(debt.DebtDate)
	if !ok {
		return errs.Internal(domain.CodeInvalidDebt,
			"a debt has an unreadable date").WithParam("date", debt.DebtDate)
	}

	// The money line, plus one amount per kind of which exactly one is non-zero. Direction and
	// method name the ACTION; the kind is an amount the rule reads.
	amounts := debt.KindAmounts()
	amounts[accountingc.AmountTotal] = debt.AmountMinor
	amounts[accountingc.AmountNet] = debt.AmountMinor

	return s.bus.Publish(ctx, accountingc.Postable{
		Action:    debt.PostingAction(),
		CompanyID: debt.CompanyID,
		BranchID:  debt.BranchID,
		Date:      date,

		DocumentType:   EntityDebt,
		DocumentID:     debt.ID,
		DocumentNumber: debt.Number,

		Amounts:      amounts,
		CurrencyCode: debt.CurrencyCode,
		RateMicro:    1_000_000,
		PartnerID:    debt.PartnerID,
		Memo:         debt.Reference,
	})
}

// Debt reads one debt.
func (s *Service) Debt(ctx context.Context, debtID id.ID) (domain.Debt, error) {
	debt, found, err := s.repos.DebtByID(ctx, debtID)
	if err != nil {
		return domain.Debt{}, err
	}
	if !found {
		return domain.Debt{}, errs.NotFound(CodeUnknownDebt,
			"there is no debt with that identity")
	}
	return debt, nil
}

// Debts lists a company's debts, optionally of one kind.
func (s *Service) Debts(
	ctx context.Context, companyID id.ID, kind domain.Kind,
) ([]domain.Debt, error) {
	return s.repos.Debts(ctx, companyID, kind)
}

// DebtPositions reports the net position of each kind.
//
// Positive means money came in on balance: we have borrowed more than we have repaid, or the owner
// has put in more than they have taken. Negative is the reverse.
//
// This is the one place a SIGN belongs. The documents keep positive amounts and a direction,
// because a signed amount makes SUM() meaningless — but a position is a single answer per kind
// and a reader wants one number.
func (s *Service) DebtPositions(
	ctx context.Context, companyID id.ID,
) (map[domain.Kind]int64, error) {
	return s.repos.NetByKind(ctx, companyID)
}
