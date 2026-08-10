package accounting

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/accounting/domain"
	"github.com/mizan-erp/mizan/internal/modules/accounting/infra/sqlite"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
)

// Stable codes for closing.
const (
	CodePeriodNotFound     = "accounting.period_not_found"
	CodeYearNotFound       = "accounting.year_not_found"
	CodeOutOfOrder         = "accounting.close_out_of_order"
	CodePeriodLocked       = "accounting.period_locked"
	CodeLaterPeriodShut    = "accounting.later_period_closed"
	CodeYearNotReady       = "accounting.year_not_ready"
	CodeYearAlreadyShut    = "accounting.year_already_closed"
	CodeNothingToClose     = "accounting.nothing_to_close"
	CodeNoRetainedEarnings = "accounting.no_retained_earnings"
)

// The audited closing actions.
const (
	ActionPeriodClosed   = "accounting.period.closed"
	ActionPeriodReopened = "accounting.period.reopened"
	ActionYearClosed     = "accounting.year.closed"

	EntityPeriod = "accounting.period"
	EntityYear   = "accounting.year"
)

// ClosePeriod shuts a period to further posting (§20.4).
//
// # Why closing must be in order
//
// A period is closed because somebody has reconciled it and signed it off. Closing March while
// February is still open means February can still receive postings that change the balance
// March was reconciled against — so March's sign-off was about a number that is no longer true.
//
// Refused, naming the earlier period. The rule costs nothing when work is done in order, which
// it almost always is.
func (s *Service) ClosePeriod(ctx context.Context, periodID id.ID) error {
	return s.db.Do(ctx, func(ctx context.Context) error {
		period, err := s.repos.PeriodByID(ctx, periodID)
		if errors.Is(err, sql.ErrNoRows) {
			return errs.NotFound(CodePeriodNotFound, "no such accounting period")
		}
		if err != nil {
			return err
		}
		if period.Status == "locked" {
			return errs.Conflict(CodePeriodLocked, "that period is locked")
		}
		if period.Status == "closed" {
			return nil // closing a closed period is a no-op, not a failure
		}

		siblings, err := s.repos.PeriodsOfYear(ctx, period.FiscalYearID)
		if err != nil {
			return err
		}
		for _, sibling := range siblings {
			if sibling.Sequence < period.Sequence && sibling.Status == "open" {
				return errs.Conflict(CodeOutOfOrder,
					"an earlier period is still open").
					WithParam("period", itoa(sibling.Sequence))
			}
		}

		if err = s.repos.SetPeriodStatus(ctx, periodID, "closed"); err != nil {
			return err
		}
		return s.audit(ctx, auditc.Auditable{
			Action:      ActionPeriodClosed,
			EntityType:  EntityPeriod,
			EntityID:    periodID,
			EntityLabel: period.Start.Format("2006-01"),
			Before:      periodSnapshot{Status: period.Status},
			After:       periodSnapshot{Status: "closed"},
			Changed:     []string{"status"},
		})
	})
}

// ReopenPeriod puts a closed period back into use.
//
// # Two refusals, for different reasons
//
// A LOCKED period never reopens. That is what locking means, and it is the state a year-end
// close leaves behind — the point at which the books stop being negotiable.
//
// A closed period does not reopen while a LATER one is closed. Reopening February under a
// closed March would let a backdated posting change the opening balance March was signed off
// against, silently. Reopen forwards, in order, or not at all.
func (s *Service) ReopenPeriod(ctx context.Context, periodID id.ID) error {
	return s.db.Do(ctx, func(ctx context.Context) error {
		period, err := s.repos.PeriodByID(ctx, periodID)
		if errors.Is(err, sql.ErrNoRows) {
			return errs.NotFound(CodePeriodNotFound, "no such accounting period")
		}
		if err != nil {
			return err
		}
		if period.Status == "locked" {
			return errs.Conflict(CodePeriodLocked,
				"a locked period cannot be reopened")
		}
		if period.Status == "open" {
			return nil
		}

		siblings, err := s.repos.PeriodsOfYear(ctx, period.FiscalYearID)
		if err != nil {
			return err
		}
		for _, sibling := range siblings {
			if sibling.Sequence > period.Sequence && sibling.Status != "open" {
				return errs.Conflict(CodeLaterPeriodShut,
					"a later period is already closed").
					WithParam("period", itoa(sibling.Sequence))
			}
		}

		if err = s.repos.SetPeriodStatus(ctx, periodID, "open"); err != nil {
			return err
		}
		return s.audit(ctx, auditc.Auditable{
			Action:      ActionPeriodReopened,
			EntityType:  EntityPeriod,
			EntityID:    periodID,
			EntityLabel: period.Start.Format("2006-01"),
			Before:      periodSnapshot{Status: period.Status},
			After:       periodSnapshot{Status: "open"},
			Changed:     []string{"status"},
		})
	})
}

type periodSnapshot struct {
	Status string `json:"status"`
}

// CloseYear posts the year-end closing entry and shuts the year (§20.4).
//
// # What closing a year actually does
//
// Revenue and expense accounts measure ONE year. At its end their balances move to retained
// earnings, so the next year starts them at zero and the accumulated result sits in equity
// where a balance sheet reads it. Assets and liabilities carry forward untouched — they measure
// a position, not a period.
//
// # It is an ordinary journal entry
//
// §20.4: "as a normal, reversible journal entry". Not a special case, not a flag, not a
// computed view — a real entry with real lines, posted through the same path as everything
// else, which means it appears in the ledger, hits the balances, and can be reversed if the
// year has to be reopened. A special case would be the one thing nobody could undo.
//
// # Ordering
//
// The entry is posted BEFORE the periods are shut, because posting into a closed period is
// refused — by design, and this method is not exempt from its own rule.
func (s *Service) CloseYear(ctx context.Context, companyID, yearID id.ID) (domain.Entry, error) {
	var closing domain.Entry

	err := s.db.Do(ctx, func(ctx context.Context) error {
		year, err := s.repos.YearByID(ctx, yearID)
		if errors.Is(err, sql.ErrNoRows) {
			return errs.NotFound(CodeYearNotFound, "no such financial year")
		}
		if err != nil {
			return err
		}
		if year.Status != "open" {
			return errs.Conflict(CodeYearAlreadyShut,
				"that financial year is already closed").WithParam("status", year.Status)
		}

		periods, err := s.repos.PeriodsOfYear(ctx, yearID)
		if err != nil {
			return err
		}
		if len(periods) == 0 {
			return errs.Validation(CodeYearNotReady, "that financial year has no periods")
		}
		last := periods[len(periods)-1]
		if last.Status != "open" {
			// The closing entry has to land somewhere, and it belongs in the year it closes.
			return errs.Conflict(CodeYearNotReady,
				"the final period must be open to receive the closing entry").
				WithParam("status", last.Status)
		}

		retained, err := s.repos.ResolveMapping(ctx, companyID, id.ID(""), domain.MappingRetained)
		if err != nil {
			return errs.Wrap(err, errs.CategoryValidation, CodeNoRetainedEarnings,
				"no account is mapped to retained earnings, so the year cannot be closed")
		}

		lines, err := s.closingLines(ctx, companyID, last.ID, retained)
		if err != nil {
			return err
		}
		if len(lines) == 0 {
			return errs.Conflict(CodeNothingToClose,
				"the year has no revenue or expenses to close")
		}

		closing, err = s.postWithin(ctx, PostInput{
			CompanyID:    companyID,
			Date:         year.End,
			SourceModule: "accounting",
			Memo:         "year-end closing " + year.Code,
			Lines:        lines,
		})
		if err != nil {
			return err
		}

		// Everything is LOCKED, not merely closed: a closed year could be reopened period by
		// period, and each reopening would leave the closing entry posted against a year that
		// is accepting movement again. Reopening a locked year is a deliberate act with its own
		// path, which is exactly the ceremony it deserves.
		for _, period := range periods {
			if err = s.repos.SetPeriodStatus(ctx, period.ID, "locked"); err != nil {
				return err
			}
		}
		if err = s.repos.SetYearStatus(ctx, yearID, "closed"); err != nil {
			return err
		}

		return s.audit(ctx, auditc.Auditable{
			Action:      ActionYearClosed,
			EntityType:  EntityYear,
			EntityID:    yearID,
			EntityLabel: year.Code,
			After: yearSnapshot{
				Code: year.Code, Entry: closing.Number,
				Moved: closing.Total(), Accounts: len(lines) - 1,
			},
		})
	})
	if err != nil {
		return domain.Entry{}, err
	}
	return closing, nil
}

type yearSnapshot struct {
	Code     string `json:"code"`
	Entry    string `json:"entry"`
	Moved    int64  `json:"movedMinor"`
	Accounts int    `json:"accounts"`
}

// closingLines builds the entry that empties revenue and expense into retained earnings.
//
// Each account is zeroed by posting the OPPOSITE of its closing balance, and the net of all of
// them lands on retained earnings — so the entry balances by construction rather than by a
// separately-computed figure that could disagree with the lines above it.
func (s *Service) closingLines(
	ctx context.Context, companyID, periodID id.ID, retained domain.Account,
) ([]domain.Line, error) {
	balances, err := s.repos.TrialBalance(ctx, companyID, periodID)
	if err != nil {
		return nil, err
	}

	var lines []domain.Line
	var net int64
	for _, balance := range balances {
		if balance.AccountType != domain.Revenue && balance.AccountType != domain.Expense {
			continue
		}
		if balance.Closing == 0 {
			continue
		}

		line := domain.Line{AccountID: balance.AccountID, Memo: "year-end closing"}
		if balance.Closing > 0 {
			// A debit-positive balance (an expense) is cleared with a credit.
			line.Credit = balance.Closing
		} else {
			line.Debit = -balance.Closing
		}
		lines = append(lines, line)
		net += balance.Closing
	}

	if len(lines) == 0 {
		return nil, nil
	}

	// The counterweight. `net` is the year's result, debit-positive: expenses exceeding revenue
	// makes it positive, so retained earnings is DEBITED — a loss reduces equity, which is what
	// a debit to a credit-normal account does.
	counter := domain.Line{AccountID: retained.ID, Memo: "year-end result"}
	if net > 0 {
		counter.Debit = net
	} else {
		counter.Credit = -net
	}
	return append(lines, counter), nil
}

// postWithin is Post's body, for callers that are already inside a transaction and have already
// resolved the period.
//
// # Why this exists rather than calling Post
//
// Post opens its own Unit of Work, which would JOIN this one and work fine — but it also
// re-resolves the period from the date, and CloseYear has already established which period the
// entry belongs to and that it is open. Two lookups that must agree is one more thing that can
// disagree.
func (s *Service) postWithin(ctx context.Context, in PostInput) (domain.Entry, error) {
	period, err := s.repos.PeriodForDate(ctx, in.CompanyID, in.Date)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Entry{}, errs.Validation(CodeNoPeriod,
			"no fiscal period covers that date").WithParam("date", in.Date.Format("2006-01-02"))
	}
	if err != nil {
		return domain.Entry{}, err
	}
	if period.Status != "open" {
		return domain.Entry{}, errs.Conflict(CodePeriodClosed,
			"that fiscal period is not open for posting").WithParam("status", period.Status)
	}

	identifier, err := id.New()
	if err != nil {
		return domain.Entry{}, err
	}
	entry, err := domain.NewEntry(
		identifier, in.CompanyID, in.Date, period.ID, in.SourceModule, in.Lines)
	if err != nil {
		return domain.Entry{}, err
	}
	entry.Memo = in.Memo
	entry.BranchID = in.BranchID
	entry.SourceDocumentType = in.SourceDocumentType
	entry.SourceDocumentID = in.SourceDocumentID

	if err = s.checkAccounts(ctx, entry); err != nil {
		return domain.Entry{}, err
	}
	if !entry.IsBalanced() {
		return domain.Entry{}, errs.Validation(domain.CodeUnbalanced, "the entry does not balance")
	}

	entry.Number, err = s.repos.NextEntryNumber(ctx, in.CompanyID, in.Date.Year())
	if err != nil {
		return domain.Entry{}, err
	}
	entry.Status = domain.Posted
	entry.PostedAt = s.clk.Now()

	if err = s.repos.InsertEntry(ctx, entry); err != nil {
		return domain.Entry{}, err
	}
	for _, line := range entry.Lines {
		if err = s.repos.ApplyMovement(ctx, entry.CompanyID, entry.PeriodID, line); err != nil {
			return domain.Entry{}, err
		}
	}
	return entry, nil
}

// Periods lists a year's periods, for the close screen.
func (s *Service) Periods(ctx context.Context, yearID id.ID) ([]sqlite.Period, error) {
	return s.repos.PeriodsOfYear(ctx, yearID)
}

// Years lists a company's financial years, newest first.
func (s *Service) Years(ctx context.Context, companyID id.ID) ([]sqlite.FiscalYear, error) {
	return s.repos.Years(ctx, companyID)
}

// PeriodFor finds the period a date falls in.
func (s *Service) PeriodFor(ctx context.Context, companyID id.ID, on time.Time) (sqlite.Period, error) {
	return s.repos.PeriodForDate(ctx, companyID, on)
}
