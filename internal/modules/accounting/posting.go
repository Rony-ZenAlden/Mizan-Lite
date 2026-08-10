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

// Stable codes for the posting path.
const (
	CodePeriodClosed  = "accounting.period_closed"
	CodeNoPeriod      = "accounting.no_period"
	CodeEntryNotFound = "accounting.entry_not_found"
)

// The audited ledger actions (§15.3).
const (
	ActionEntryPosted   = "accounting.entry.posted"
	ActionEntryReversed = "accounting.entry.reversed"

	EntityEntry = "accounting.entry"
)

// PostInput is a journal entry to record.
//
// The caller supplies lines and a date; everything else — the number, the period, the balance
// check, the audit entry — is this service's job. A caller that had to find its own fiscal
// period would be a caller that could get it wrong.
type PostInput struct {
	CompanyID id.ID
	Date      time.Time
	Lines     []domain.Line

	SourceModule       string
	SourceDocumentType string
	SourceDocumentID   id.ID

	BranchID id.ID
	Memo     string
}

// Post records a balanced entry, in one transaction.
//
// # The three guards, and why this is the second
//
// The aggregate cannot be CONSTRUCTED unbalanced (domain.NewEntry). This re-checks anyway,
// because a repository is a separate trust boundary and this method is the last place that can
// refuse before the rows exist. The third guard is the integrity job, which sees entries that
// never passed through either.
//
// Belt and braces on the one invariant whose failure is invisible: an unbalanced ledger still
// produces reports, and they are simply wrong.
//
// # Period control (§20.4)
//
// The period is resolved from the entry's DATE, not from today. Posting into a closed or locked
// period is refused, naming it — restating a month somebody has already signed off is exactly
// what closing a period means to prevent.
func (s *Service) Post(ctx context.Context, in PostInput) (domain.Entry, error) {
	var posted domain.Entry

	err := s.db.Do(ctx, func(ctx context.Context) error {
		entry, err := s.postWithin(ctx, in)
		if err != nil {
			return err
		}
		posted = entry

		return s.audit(ctx, auditc.Auditable{
			Action:      ActionEntryPosted,
			EntityType:  EntityEntry,
			EntityID:    entry.ID,
			EntityLabel: entry.Number,
			After: entrySnapshot{
				Number: entry.Number, Date: entry.Date.Format("2006-01-02"),
				Total: entry.Total(), Lines: len(entry.Lines), Source: entry.SourceModule,
			},
		})
	})
	if err != nil {
		return domain.Entry{}, err
	}
	return posted, nil
}

// checkAccounts refuses a line pointing at an account that cannot take one.
//
// Every account is loaded and asked, rather than trusting the caller: a posting to a roll-up
// account makes every ancestor double-count, and the resulting trial balance still balances
// (§20.1). One read per distinct account, which for a real entry is a handful.
func (s *Service) checkAccounts(ctx context.Context, entry domain.Entry) error {
	seen := make(map[id.ID]bool, len(entry.Lines))
	for _, line := range entry.Lines {
		if seen[line.AccountID] {
			continue
		}
		seen[line.AccountID] = true

		account, err := s.repos.AccountByID(ctx, line.AccountID)
		if errors.Is(err, sql.ErrNoRows) {
			return errs.Validation(domain.CodeInvalidLine,
				"a line names an account that does not exist").
				WithParam("account", string(line.AccountID))
		}
		if err != nil {
			return err
		}
		if err = account.RequirePostable(); err != nil {
			return err
		}
		if account.CompanyID != entry.CompanyID {
			return errs.Validation(domain.CodeInvalidLine,
				"a line names an account belonging to another company").
				WithParam("account", account.Code)
		}
	}
	return nil
}

type entrySnapshot struct {
	Number string `json:"number"`
	Date   string `json:"date"`
	Total  int64  `json:"totalMinor"`
	Lines  int    `json:"lines"`
	Source string `json:"source"`
}

// Reverse posts the correcting entry for a posted one and marks the original reversed.
//
// # Why the original is untouched apart from its status
//
// §20.2: a posted entry is never modified or deleted. The correction is a first-class entry
// with every debit and credit swapped, linked back — so the ledger shows what was posted, that
// it was corrected, and when. An edit would show only the current opinion, which is precisely
// what an auditor is not interested in.
//
// The reversal is dated on ITS OWN date, so reversing a March entry in May is a May event.
// Back-dating it would silently restate a month that may already be closed — and the period
// check below would then be checking the wrong month.
func (s *Service) Reverse(
	ctx context.Context, entryID id.ID, on time.Time, memo string,
) (domain.Entry, error) {
	var reversal domain.Entry

	err := s.db.Do(ctx, func(ctx context.Context) error {
		original, err := s.repos.EntryByID(ctx, entryID)
		if errors.Is(err, sql.ErrNoRows) {
			return errs.NotFound(CodeEntryNotFound, "no such journal entry")
		}
		if err != nil {
			return err
		}

		period, err := s.repos.PeriodForDate(ctx, original.CompanyID, on)
		if errors.Is(err, sql.ErrNoRows) {
			return errs.Validation(CodeNoPeriod, "no fiscal period covers that date").
				WithParam("date", on.Format("2006-01-02"))
		}
		if err != nil {
			return err
		}
		if period.Status != "open" {
			return errs.Conflict(CodePeriodClosed,
				"that fiscal period is not open for posting").
				WithParam("status", period.Status)
		}

		identifier, err := id.New()
		if err != nil {
			return err
		}
		reversal, err = original.Reverse(identifier, on, period.ID, memo)
		if err != nil {
			return err
		}

		reversal.Number, err = s.repos.NextEntryNumber(ctx, original.CompanyID, on.Year())
		if err != nil {
			return err
		}
		reversal.Status = domain.Posted
		reversal.PostedAt = s.clk.Now()

		if err = s.repos.InsertEntry(ctx, reversal); err != nil {
			return err
		}
		for _, line := range reversal.Lines {
			if err = s.repos.ApplyMovement(ctx, reversal.CompanyID, reversal.PeriodID, line); err != nil {
				return err
			}
		}
		if err = s.repos.MarkReversed(ctx, original.ID); err != nil {
			return err
		}

		return s.audit(ctx, auditc.Auditable{
			Action:      ActionEntryReversed,
			EntityType:  EntityEntry,
			EntityID:    original.ID,
			EntityLabel: original.Number,
			Before:      entrySnapshot{Number: original.Number, Total: original.Total()},
			After:       entrySnapshot{Number: reversal.Number, Total: reversal.Total()},
			Changed:     []string{"status"},
		})
	})
	if err != nil {
		return domain.Entry{}, err
	}
	return reversal, nil
}

// Entry loads one entry with its lines.
func (s *Service) Entry(ctx context.Context, entryID id.ID) (domain.Entry, error) {
	return s.repos.EntryByID(ctx, entryID)
}

// Entries lists a company's entries, newest first.
func (s *Service) Entries(ctx context.Context, companyID id.ID, limit int) ([]domain.Entry, error) {
	return s.repos.Entries(ctx, companyID, limit)
}

// SetPeriodStatus opens, closes, or locks a fiscal period (§20.4).
func (s *Service) SetPeriodStatus(ctx context.Context, periodID id.ID, status string) error {
	switch status {
	case "open", "closed", "locked":
	default:
		return errs.Validation(CodePeriodClosed, "that is not a period status").
			WithParam("status", status)
	}
	return s.db.Do(ctx, func(ctx context.Context) error {
		return s.repos.SetPeriodStatus(ctx, periodID, status)
	})
}

// CheckIntegrity is the ledger's third guard (§20.2).
//
// Returns the numbers of every stored entry whose lines do not balance. Empty is the only
// acceptable answer, and anything else is a defect worth stopping for — an unbalanced ledger
// produces reports that look fine and are wrong.
//
// It exists because the other two guards protect only the paths that go through them. A
// restore, a repair script, or a future importer does not.
func (s *Service) CheckIntegrity(ctx context.Context, companyID id.ID) ([]string, error) {
	return s.repos.UnbalancedEntries(ctx, companyID)
}

// ── balances (§20.5) ────────────────────────────────────────────────────────────

// TrialBalance reads every account's position as at the end of a period.
//
// Read from `account_balances`, never from the ledger: a shop with three years of history has
// hundreds of thousands of journal lines and about two hundred accounts.
func (s *Service) TrialBalance(
	ctx context.Context, companyID, periodID id.ID,
) ([]sqlite.Balance, error) {
	return s.repos.TrialBalance(ctx, companyID, periodID)
}

// RebuildAndVerifyBalances recomputes the balances and asserts they match what was maintained.
//
// # Why an assertion and not just a rebuild
//
// §20.5 asks for "a rebuild job that recomputes from journal_lines and asserts equality — a
// cheap, strong correctness check". The assertion is the whole value. A rebuild alone silently
// repairs, which means a bug in the incremental path is corrected nightly and never reported,
// and the reports are wrong for exactly one day at a time forever.
//
// Comparing FIRST and rebuilding second turns the same work into a defect report.
//
// Returns the number of rows that disagreed. Zero is the only good answer.
func (s *Service) RebuildAndVerifyBalances(ctx context.Context, companyID id.ID) (int, error) {
	var mismatches int

	err := s.db.Do(ctx, func(ctx context.Context) error {
		before, err := s.repos.BalanceRows(ctx, companyID)
		if err != nil {
			return err
		}
		if err = s.repos.RebuildBalances(ctx, companyID); err != nil {
			return err
		}
		after, err := s.repos.BalanceRows(ctx, companyID)
		if err != nil {
			return err
		}

		mismatches = compareBalances(before, after)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return mismatches, nil
}

// compareBalances counts rows that differ between two snapshots.
//
// Keyed rather than compared positionally: a row present in one snapshot and absent from the
// other is exactly the kind of drift worth catching, and a positional walk would report every
// subsequent row as wrong once the two lists went out of step.
func compareBalances(before, after []sqlite.BalanceSnapshot) int {
	key := func(b sqlite.BalanceSnapshot) string {
		return string(b.AccountID) + "|" + string(b.PeriodID) + "|" + string(b.BranchID)
	}

	old := make(map[string]sqlite.BalanceSnapshot, len(before))
	for _, row := range before {
		old[key(row)] = row
	}

	mismatches := 0
	for _, row := range after {
		previous, existed := old[key(row)]
		if !existed || previous.Debit != row.Debit || previous.Credit != row.Credit {
			mismatches++
		}
		delete(old, key(row))
	}
	// Whatever is left was maintained but should not exist at all.
	return mismatches + len(old)
}

// CodeLedgerCorrupt is raised when the nightly check finds the books disagreeing with
// themselves.
const CodeLedgerCorrupt = "accounting.ledger_corrupt"

// VerifyLedger is the nightly job's body: both integrity checks, over every company.
//
// It FAILS rather than repairs when it finds something. A failed job is visible in the run
// history and on the operations screen; a silent repair is a bug that gets corrected every
// night and reported never — and the books would be wrong for exactly one day at a time,
// forever.
func (s *Service) VerifyLedger(ctx context.Context) error {
	companies, err := s.repos.CompaniesWithLedgers(ctx)
	if err != nil {
		return err
	}

	for _, companyID := range companies {
		unbalanced, checkErr := s.CheckIntegrity(ctx, companyID)
		if checkErr != nil {
			return checkErr
		}
		if len(unbalanced) > 0 {
			return errs.Internal(CodeLedgerCorrupt,
				"journal entries were found whose debits and credits do not agree").
				WithParam("entries", joinFirst(unbalanced, 10))
		}

		mismatches, rebuildErr := s.RebuildAndVerifyBalances(ctx, companyID)
		if rebuildErr != nil {
			return rebuildErr
		}
		if mismatches > 0 {
			return errs.Internal(CodeLedgerCorrupt,
				"the maintained account balances disagree with the ledger they come from").
				WithParam("rows", itoa(mismatches))
		}
	}
	return nil
}

// joinFirst names the first few offenders. A job failure that lists four hundred entry numbers
// is one nobody reads.
func joinFirst(values []string, limit int) string {
	if len(values) > limit {
		values = append(values[:limit:limit], "…")
	}
	out := ""
	for i, value := range values {
		if i > 0 {
			out += ", "
		}
		out += value
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}
