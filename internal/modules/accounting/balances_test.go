package accounting_test

import (
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/modules/accounting"
	"github.com/mizan-erp/mizan/internal/modules/accounting/domain"
	"github.com/mizan-erp/mizan/internal/modules/accounting/infra/sqlite"
)

// ── the trial balance ───────────────────────────────────────────────────────────

// A correct trial balance sums to ZERO under the debit-positive convention — the same fact as
// "total debits equal total credits", and cheaper to assert.
func TestATrialBalanceSumsToZero(t *testing.T) {
	l := newLedger(t)

	entry, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: l.ar, Debit: 250_000},
			{AccountID: l.sale, Credit: 250_000},
		},
	})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}

	balances, err := l.svc.TrialBalance(l.ctx, l.companyID, entry.PeriodID)
	if err != nil {
		t.Fatalf("TrialBalance: %v", err)
	}

	var total, debits, credits int64
	byCode := map[string]sqlite.Balance{}
	for _, b := range balances {
		total += b.Closing
		debits += b.Debit
		credits += b.Credit
		byCode[b.AccountCode] = b
	}

	if total != 0 {
		t.Errorf("the trial balance sums to %d, want 0 — the books do not balance", total)
	}
	if debits != credits {
		t.Errorf("debits %d, credits %d", debits, credits)
	}

	// Receivables is an asset: debit-normal, so a debit balance is positive.
	if got := byCode["1200"].Closing; got != 250_000 {
		t.Errorf("receivables closing = %d, want 250000", got)
	}
	// Sales is revenue: credit-normal, so under the debit-positive convention it reads negative.
	// One convention, converted once at the presentation edge.
	if got := byCode["4100"].Closing; got != -250_000 {
		t.Errorf("sales closing = %d, want -250000", got)
	}
	if byCode["4100"].Normal != domain.Credit {
		t.Error("sales is not marked credit-normal; a report would show it the wrong way up")
	}
}

// Every account appears, including ones nothing has posted to. A trial balance that silently
// omits an untouched account is one an accountant cannot reconcile against their own list.
func TestTheTrialBalanceListsEveryAccount(t *testing.T) {
	l := newLedger(t)

	entry, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: l.cash, Debit: 1_000},
			{AccountID: l.sale, Credit: 1_000},
		},
	})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}

	balances, err := l.svc.TrialBalance(l.ctx, l.companyID, entry.PeriodID)
	if err != nil {
		t.Fatalf("TrialBalance: %v", err)
	}
	accounts, err := l.svc.Accounts(l.ctx, l.companyID)
	if err != nil {
		t.Fatalf("Accounts: %v", err)
	}
	if len(balances) != len(accounts) {
		t.Errorf("%d balances for %d accounts", len(balances), len(accounts))
	}
}

// The opening balance is DERIVED from earlier periods rather than stored (0012), so a position
// carried forward cannot drift from the movements it came from.
func TestAnOpeningBalanceCarriesForward(t *testing.T) {
	l := newLedger(t)

	january := time.Date(2026, time.January, 20, 0, 0, 0, 0, time.UTC)
	february := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)

	if _, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: january, SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: l.cash, Debit: 100_000},
			{AccountID: l.sale, Credit: 100_000},
		},
	}); err != nil {
		t.Fatalf("Post january: %v", err)
	}

	second, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: february, SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: l.cash, Debit: 40_000},
			{AccountID: l.sale, Credit: 40_000},
		},
	})
	if err != nil {
		t.Fatalf("Post february: %v", err)
	}

	balances, err := l.svc.TrialBalance(l.ctx, l.companyID, second.PeriodID)
	if err != nil {
		t.Fatalf("TrialBalance: %v", err)
	}
	for _, b := range balances {
		if b.AccountCode != "1110" {
			continue
		}
		if b.Opening != 100_000 {
			t.Errorf("february opening = %d, want january's 100000", b.Opening)
		}
		if b.Debit != 40_000 {
			t.Errorf("february movement = %d, want 40000 — the period's own, not cumulative", b.Debit)
		}
		if b.Closing != 140_000 {
			t.Errorf("february closing = %d, want 140000", b.Closing)
		}
		return
	}
	t.Fatal("cash is missing from the trial balance")
}

// A reversal must undo its original in the balances too, not merely in the ledger.
func TestAReversalUndoesTheBalance(t *testing.T) {
	l := newLedger(t)

	entry, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: l.cash, Debit: 60_000},
			{AccountID: l.sale, Credit: 60_000},
		},
	})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if _, err = l.svc.Reverse(l.ctx, entry.ID, inPeriod(), "keyed twice"); err != nil {
		t.Fatalf("Reverse: %v", err)
	}

	balances, err := l.svc.TrialBalance(l.ctx, l.companyID, entry.PeriodID)
	if err != nil {
		t.Fatalf("TrialBalance: %v", err)
	}
	for _, b := range balances {
		if b.AccountCode == "1110" && b.Closing != 0 {
			t.Errorf("cash closing = %d after a reversal, want 0", b.Closing)
		}
	}
}

// ── the rebuild, and why it ASSERTS (§20.5) ─────────────────────────────────────

// A healthy ledger rebuilds to exactly what was maintained.
func TestRebuildingAgreesWithWhatWasMaintained(t *testing.T) {
	l := newLedger(t)

	for _, amount := range []int64{10_000, 25_000, 3_500} {
		if _, err := l.svc.Post(l.ctx, accounting.PostInput{
			CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
			Lines: []domain.Line{
				{AccountID: l.ar, Debit: amount},
				{AccountID: l.sale, Credit: amount},
			},
		}); err != nil {
			t.Fatalf("Post: %v", err)
		}
	}

	mismatches, err := l.svc.RebuildAndVerifyBalances(l.ctx, l.companyID)
	if err != nil {
		t.Fatalf("RebuildAndVerifyBalances: %v", err)
	}
	if mismatches != 0 {
		t.Errorf("%d balance rows disagreed with a fresh recomputation", mismatches)
	}
}

// The assertion is the whole value of the job.
//
// A rebuild alone silently repairs, which means a bug in the incremental path is corrected
// nightly and never reported — the books wrong for exactly one day at a time, forever. This
// corrupts a maintained total the way a bad incremental path would and checks it is REPORTED.
func TestRebuildingReportsDriftRatherThanSilentlyFixingIt(t *testing.T) {
	l := newLedger(t)

	if _, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: l.cash, Debit: 80_000},
			{AccountID: l.sale, Credit: 80_000},
		},
	}); err != nil {
		t.Fatalf("Post: %v", err)
	}

	if _, err := l.store.Writer(l.ctx).ExecContext(l.ctx,
		`UPDATE account_balances SET debit_minor = debit_minor + 1 WHERE debit_minor > 0`); err != nil {
		t.Fatalf("corrupting a balance: %v", err)
	}

	mismatches, err := l.svc.RebuildAndVerifyBalances(l.ctx, l.companyID)
	if err != nil {
		t.Fatalf("RebuildAndVerifyBalances: %v", err)
	}
	if mismatches == 0 {
		t.Fatal("drift was repaired silently; a bug in the incremental path would never be reported")
	}

	// And it did rebuild: a second pass finds nothing, because the first one corrected the rows
	// while reporting that it had to.
	again, err := l.svc.RebuildAndVerifyBalances(l.ctx, l.companyID)
	if err != nil {
		t.Fatalf("RebuildAndVerifyBalances: %v", err)
	}
	if again != 0 {
		t.Errorf("%d rows still disagree after a rebuild", again)
	}
}

// ── the nightly job ─────────────────────────────────────────────────────────────

func TestTheIntegrityJobPassesOnHealthyBooks(t *testing.T) {
	l := newLedger(t)

	if _, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: l.cash, Debit: 7_000},
			{AccountID: l.sale, Credit: 7_000},
		},
	}); err != nil {
		t.Fatalf("Post: %v", err)
	}

	if err := l.svc.VerifyLedger(l.ctx); err != nil {
		t.Fatalf("healthy books failed the integrity job: %v", err)
	}
}

// It FAILS rather than repairs. A failed job is visible in the run history and on the
// operations screen; a silent repair is a bug reported never.
func TestTheIntegrityJobFailsOnACorruptLedger(t *testing.T) {
	l := newLedger(t)

	entry, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: l.cash, Debit: 12_000},
			{AccountID: l.sale, Credit: 12_000},
		},
	})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}

	if _, err = l.store.Writer(l.ctx).ExecContext(l.ctx,
		`UPDATE journal_lines SET debit_minor = 5 WHERE journal_entry_id = ? AND debit_minor > 0`,
		string(entry.ID)); err != nil {
		t.Fatalf("corrupting the ledger: %v", err)
	}

	err = l.svc.VerifyLedger(l.ctx)
	if err == nil {
		t.Fatal("the integrity job passed over an unbalanced entry")
	}
	if code := errs.CodeOf(err); code != accounting.CodeLedgerCorrupt {
		t.Errorf("code = %q, want %q", code, accounting.CodeLedgerCorrupt)
	}
	// The offending entry is named: a failure that says only "the ledger is corrupt" sends
	// somebody through a hundred thousand lines by hand.
	typed, _ := errs.AsError(err)
	if typed.Params["entries"] != entry.Number {
		t.Errorf("entries = %q, want %q", typed.Params["entries"], entry.Number)
	}
}

// The module declares the job with a handler that runs — the shape Step 1.3 found broken the
// first time a module tried to register one.
func TestTheModuleDeclaresTheIntegrityJobWithAHandler(t *testing.T) {
	f := newFixture(t, accounting.Options{})
	registrations := accounting.NewModule(f.svc).Jobs()

	if len(registrations) != 1 {
		t.Fatalf("%d jobs declared, want 1", len(registrations))
	}
	if registrations[0].Def.Key != "accounting.ledger_integrity" {
		t.Errorf("key = %q", registrations[0].Def.Key)
	}
	if registrations[0].Handler == nil {
		t.Fatal("the job is declared with no handler; the scheduler would refuse it")
	}
}
