package accounting_test

import (
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/modules/accounting"
	"github.com/mizan-erp/mizan/internal/modules/accounting/domain"
	"github.com/mizan-erp/mizan/internal/modules/accounting/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/modules/audit"
)

// periods returns the fixture's twelve periods, in calendar order.
func (l ledger) periods(t *testing.T) []sqlite.Period {
	t.Helper()
	years, err := l.svc.Years(l.ctx, l.companyID)
	if err != nil {
		t.Fatalf("Years: %v", err)
	}
	if len(years) != 1 {
		t.Fatalf("%d financial years, want the one the wizard created", len(years))
	}
	list, err := l.svc.Periods(l.ctx, years[0].ID)
	if err != nil {
		t.Fatalf("Periods: %v", err)
	}
	if len(list) != 12 {
		t.Fatalf("%d periods, want 12", len(list))
	}
	return list
}

func (l ledger) year(t *testing.T) sqlite.FiscalYear {
	t.Helper()
	years, err := l.svc.Years(l.ctx, l.companyID)
	if err != nil || len(years) == 0 {
		t.Fatalf("Years: %v", err)
	}
	return years[0]
}

// ── closing order (§20.4) ───────────────────────────────────────────────────────

// A period is closed because somebody reconciled it. Closing March while February is still open
// means February can still receive postings that change what March was signed off against.
func TestAPeriodCannotBeClosedWhileAnEarlierOneIsOpen(t *testing.T) {
	l := newLedger(t)
	list := l.periods(t)

	err := l.svc.ClosePeriod(l.ctx, list[2].ID) // March, with January and February open
	if err == nil {
		t.Fatal("March closed while February was still open")
	}
	if code := errs.CodeOf(err); code != accounting.CodeOutOfOrder {
		t.Errorf("code = %q, want %q", code, accounting.CodeOutOfOrder)
	}

	// In order, it works.
	for i := 0; i < 3; i++ {
		if err = l.svc.ClosePeriod(l.ctx, list[i].ID); err != nil {
			t.Fatalf("closing period %d: %v", i+1, err)
		}
	}
}

// Reopening February under a closed March would let a backdated posting silently change the
// opening balance March was signed off against.
func TestAPeriodCannotBeReopenedUnderAClosedLaterOne(t *testing.T) {
	l := newLedger(t)
	list := l.periods(t)

	for i := 0; i < 3; i++ {
		if err := l.svc.ClosePeriod(l.ctx, list[i].ID); err != nil {
			t.Fatalf("closing period %d: %v", i+1, err)
		}
	}

	err := l.svc.ReopenPeriod(l.ctx, list[1].ID) // February, with March closed
	if err == nil {
		t.Fatal("February reopened while March was closed")
	}
	if code := errs.CodeOf(err); code != accounting.CodeLaterPeriodShut {
		t.Errorf("code = %q, want %q", code, accounting.CodeLaterPeriodShut)
	}

	// Backwards, in order, it works — and posting into it is possible again.
	if err = l.svc.ReopenPeriod(l.ctx, list[2].ID); err != nil {
		t.Fatalf("reopening March: %v", err)
	}
	if err = l.svc.ReopenPeriod(l.ctx, list[1].ID); err != nil {
		t.Fatalf("reopening February: %v", err)
	}
	if _, err = l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID:    l.companyID,
		Date:         time.Date(2026, time.February, 3, 0, 0, 0, 0, time.UTC),
		SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: l.cash, Debit: 1_000},
			{AccountID: l.sale, Credit: 1_000},
		},
	}); err != nil {
		t.Fatalf("posting into a reopened period: %v", err)
	}
}

func TestClosingAndReopeningAreAudited(t *testing.T) {
	l := newLedger(t)
	list := l.periods(t)

	if err := l.svc.ClosePeriod(l.ctx, list[0].ID); err != nil {
		t.Fatalf("ClosePeriod: %v", err)
	}
	if err := l.svc.ReopenPeriod(l.ctx, list[0].ID); err != nil {
		t.Fatalf("ReopenPeriod: %v", err)
	}

	entries, err := l.audit.Entries(l.ctx, audit.Filter{EntityType: accounting.EntityPeriod})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("%d audit entries, want a close and a reopen", len(entries))
	}
	// Reopening a signed-off period is exactly the act an auditor asks about later.
	actions := map[string]bool{entries[0].Action: true, entries[1].Action: true}
	if !actions[accounting.ActionPeriodClosed] || !actions[accounting.ActionPeriodReopened] {
		t.Errorf("actions = %v", actions)
	}
}

// ── year-end closing (§20.4) ────────────────────────────────────────────────────

// Revenue and expense measure ONE year; at its end they move to retained earnings so the next
// year starts at zero and the result sits in equity.
func TestClosingAYearEmptiesRevenueAndExpenseIntoRetainedEarnings(t *testing.T) {
	l := newLedger(t)
	cogs := accountID(t, l.fixture, "5100")

	// A profitable year: 500,000 of sales against 300,000 of cost.
	if _, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: l.ar, Debit: 500_000},
			{AccountID: l.sale, Credit: 500_000},
		},
	}); err != nil {
		t.Fatalf("Post sales: %v", err)
	}
	if _, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: cogs, Debit: 300_000},
			{AccountID: l.cash, Credit: 300_000},
		},
	}); err != nil {
		t.Fatalf("Post cost: %v", err)
	}

	closing, err := l.svc.CloseYear(l.ctx, l.companyID, l.year(t).ID)
	if err != nil {
		t.Fatalf("CloseYear: %v", err)
	}
	if !closing.IsBalanced() {
		t.Fatal("the closing entry does not balance")
	}

	// Read the final period: revenue and expense at zero, the 200,000 result in equity.
	list := l.periods(t)
	balances, err := l.svc.TrialBalance(l.ctx, l.companyID, list[11].ID)
	if err != nil {
		t.Fatalf("TrialBalance: %v", err)
	}
	byCode := map[string]sqlite.Balance{}
	for _, b := range balances {
		byCode[b.AccountCode] = b
	}

	if got := byCode["4100"].Closing; got != 0 {
		t.Errorf("sales closing = %d after year-end, want 0", got)
	}
	if got := byCode["5100"].Closing; got != 0 {
		t.Errorf("cost of sales closing = %d after year-end, want 0", got)
	}
	// Retained earnings is credit-normal, so a 200,000 profit reads as -200,000 debit-positive.
	if got := byCode["3100"].Closing; got != -200_000 {
		t.Errorf("retained earnings = %d, want -200000 (a 200000 profit)", got)
	}
	// And the books still balance.
	var total int64
	for _, b := range balances {
		total += b.Closing
	}
	if total != 0 {
		t.Errorf("the trial balance sums to %d after year-end, want 0", total)
	}
}

// A loss reduces equity: retained earnings is DEBITED, which is what a debit to a credit-normal
// account means.
func TestALossReducesRetainedEarnings(t *testing.T) {
	l := newLedger(t)
	cogs := accountID(t, l.fixture, "5100")

	if _, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: l.ar, Debit: 100_000},
			{AccountID: l.sale, Credit: 100_000},
		},
	}); err != nil {
		t.Fatalf("Post sales: %v", err)
	}
	if _, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: cogs, Debit: 175_000},
			{AccountID: l.cash, Credit: 175_000},
		},
	}); err != nil {
		t.Fatalf("Post cost: %v", err)
	}

	if _, err := l.svc.CloseYear(l.ctx, l.companyID, l.year(t).ID); err != nil {
		t.Fatalf("CloseYear: %v", err)
	}

	list := l.periods(t)
	balances, err := l.svc.TrialBalance(l.ctx, l.companyID, list[11].ID)
	if err != nil {
		t.Fatalf("TrialBalance: %v", err)
	}
	for _, b := range balances {
		if b.AccountCode == "3100" && b.Closing != 75_000 {
			t.Errorf("retained earnings = %d, want 75000 (a 75000 loss, debit-positive)", b.Closing)
		}
	}
}

// §20.4: "as a normal, reversible journal entry". Not a flag, not a computed view — a real entry
// that appears in the ledger and could be reversed if the year had to be reopened.
func TestTheClosingEntryIsAnOrdinaryJournalEntry(t *testing.T) {
	l := newLedger(t)

	if _, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: l.ar, Debit: 90_000},
			{AccountID: l.sale, Credit: 90_000},
		},
	}); err != nil {
		t.Fatalf("Post: %v", err)
	}

	closing, err := l.svc.CloseYear(l.ctx, l.companyID, l.year(t).ID)
	if err != nil {
		t.Fatalf("CloseYear: %v", err)
	}

	stored, err := l.svc.Entry(l.ctx, closing.ID)
	if err != nil {
		t.Fatalf("Entry: %v", err)
	}
	if stored.Status != domain.Posted {
		t.Errorf("status = %q, want posted", stored.Status)
	}
	if stored.Number == "" {
		t.Error("the closing entry has no number; it is not in the ledger's sequence")
	}
	if len(stored.Lines) < 2 {
		t.Errorf("%d lines, want the closed accounts plus retained earnings", len(stored.Lines))
	}
	if !stored.IsBalanced() {
		t.Error("the stored closing entry does not balance")
	}
}

// After closing, the year and every period are LOCKED — not merely closed, which could be
// reopened period by period and would leave the closing entry posted against a year accepting
// movement again.
func TestClosingAYearLocksIt(t *testing.T) {
	l := newLedger(t)

	if _, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: l.ar, Debit: 10_000},
			{AccountID: l.sale, Credit: 10_000},
		},
	}); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if _, err := l.svc.CloseYear(l.ctx, l.companyID, l.year(t).ID); err != nil {
		t.Fatalf("CloseYear: %v", err)
	}

	for _, period := range l.periods(t) {
		if period.Status != "locked" {
			t.Errorf("period %d is %q after year-end, want locked", period.Sequence, period.Status)
		}
	}
	if status := l.year(t).Status; status != "closed" {
		t.Errorf("year status = %q, want closed", status)
	}

	// Nothing more can be posted into it, and no period can be talked back open.
	list := l.periods(t)
	if err := l.svc.ReopenPeriod(l.ctx, list[0].ID); err == nil {
		t.Error("a locked period was reopened")
	} else if code := errs.CodeOf(err); code != accounting.CodePeriodLocked {
		t.Errorf("code = %q, want %q", code, accounting.CodePeriodLocked)
	}

	if _, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: l.cash, Debit: 1_000},
			{AccountID: l.sale, Credit: 1_000},
		},
	}); err == nil {
		t.Error("a locked year accepted a posting")
	}
}

func TestAYearCannotBeClosedTwice(t *testing.T) {
	l := newLedger(t)

	if _, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: l.ar, Debit: 10_000},
			{AccountID: l.sale, Credit: 10_000},
		},
	}); err != nil {
		t.Fatalf("Post: %v", err)
	}
	yearID := l.year(t).ID
	if _, err := l.svc.CloseYear(l.ctx, l.companyID, yearID); err != nil {
		t.Fatalf("CloseYear: %v", err)
	}

	_, err := l.svc.CloseYear(l.ctx, l.companyID, yearID)
	if err == nil {
		t.Fatal("a year was closed twice; the result would be counted into equity again")
	}
	if code := errs.CodeOf(err); code != accounting.CodeYearAlreadyShut {
		t.Errorf("code = %q, want %q", code, accounting.CodeYearAlreadyShut)
	}
}

// A year with no trading has nothing to move, and an empty closing entry would be a journal
// entry with no lines — which the aggregate refuses anyway.
func TestClosingAYearWithNoTradingIsRefused(t *testing.T) {
	l := newLedger(t)

	_, err := l.svc.CloseYear(l.ctx, l.companyID, l.year(t).ID)
	if err == nil {
		t.Fatal("a year with no revenue or expenses was closed")
	}
	if code := errs.CodeOf(err); code != accounting.CodeNothingToClose {
		t.Errorf("code = %q, want %q", code, accounting.CodeNothingToClose)
	}
}

// A failed close leaves the year open and nothing posted — a half-closed year is one nobody
// could finish or undo.
func TestAFailedYearCloseLeavesTheYearOpen(t *testing.T) {
	l := newLedger(t)

	if _, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: l.ar, Debit: 40_000},
			{AccountID: l.sale, Credit: 40_000},
		},
	}); err != nil {
		t.Fatalf("Post: %v", err)
	}

	// Remove the retained-earnings mapping: the close has nowhere to put the result.
	if _, err := l.store.Writer(l.ctx).ExecContext(l.ctx,
		`DELETE FROM account_mappings WHERE mapping_key = ?`, domain.MappingRetained); err != nil {
		t.Fatalf("removing the mapping: %v", err)
	}

	_, err := l.svc.CloseYear(l.ctx, l.companyID, l.year(t).ID)
	if err == nil {
		t.Fatal("a year closed with nowhere to put the result")
	}
	if code := errs.CodeOf(err); code != accounting.CodeNoRetainedEarnings {
		t.Errorf("code = %q, want %q", code, accounting.CodeNoRetainedEarnings)
	}

	if status := l.year(t).Status; status != "open" {
		t.Errorf("year status = %q after a failed close, want open", status)
	}
	for _, period := range l.periods(t) {
		if period.Status != "open" {
			t.Errorf("period %d is %q after a failed close, want open", period.Sequence, period.Status)
		}
	}
}

func TestClosingAYearIsAudited(t *testing.T) {
	l := newLedger(t)

	if _, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: l.ar, Debit: 33_000},
			{AccountID: l.sale, Credit: 33_000},
		},
	}); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if _, err := l.svc.CloseYear(l.ctx, l.companyID, l.year(t).ID); err != nil {
		t.Fatalf("CloseYear: %v", err)
	}

	entries, err := l.audit.Entries(l.ctx, audit.Filter{EntityType: accounting.EntityYear})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 1 || entries[0].Action != accounting.ActionYearClosed {
		t.Fatalf("audit entries = %+v, want one year-closed", entries)
	}
}

// The ledger stays internally consistent through a year-end close — the strongest single check
// available, run over the whole thing.
func TestTheLedgerIsIntactAfterAYearEnd(t *testing.T) {
	l := newLedger(t)
	cogs := accountID(t, l.fixture, "5100")

	for _, pair := range [][2]int64{{120_000, 70_000}, {80_000, 45_000}} {
		if _, err := l.svc.Post(l.ctx, accounting.PostInput{
			CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
			Lines: []domain.Line{
				{AccountID: l.ar, Debit: pair[0]},
				{AccountID: l.sale, Credit: pair[0]},
			},
		}); err != nil {
			t.Fatalf("Post: %v", err)
		}
		if _, err := l.svc.Post(l.ctx, accounting.PostInput{
			CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
			Lines: []domain.Line{
				{AccountID: cogs, Debit: pair[1]},
				{AccountID: l.cash, Credit: pair[1]},
			},
		}); err != nil {
			t.Fatalf("Post: %v", err)
		}
	}

	if _, err := l.svc.CloseYear(l.ctx, l.companyID, l.year(t).ID); err != nil {
		t.Fatalf("CloseYear: %v", err)
	}
	if err := l.svc.VerifyLedger(l.ctx); err != nil {
		t.Fatalf("the ledger is not intact after a year-end close: %v", err)
	}
}
