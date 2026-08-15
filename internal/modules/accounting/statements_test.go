package accounting_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/accounting"
	"github.com/mizan-erp/mizan/internal/modules/accounting/domain"
)

// The fiscal year the fixture generates is calendar 2026, in monthly periods.
const (
	march      = "2026-03-01"
	marchEnd   = "2026-03-31"
	midMarch   = "2026-03-15"
	yearEnd    = "2026-12-31"
	beforeTime = "2020-01-01"
)

// ── the statement reads in the direction a reader expects ───────────────────────

// TestRevenueReadsPositiveOnAProfitAndLoss
//
// The ledger is debit-positive throughout, so revenue carries a NEGATIVE balance — which is
// correct, consistent, and the reason an accountant stops trusting a report. This is the
// presentation edge 0012 named, and the conversion happening here is the whole of it.
func TestRevenueReadsPositiveOnAProfitAndLoss(t *testing.T) {
	l := newLedger(t)

	// A sale of 250,000 and rent of 40,000.
	post(t, l, l.ar, l.sale, 250_000)
	post(t, l, accountID(t, l.fixture, "5200"), l.cash, 40_000)

	statement, err := l.svc.ProfitAndLoss(l.ctx, l.companyID, march, marchEnd)
	if err != nil {
		t.Fatalf("ProfitAndLoss: %v", err)
	}

	if statement.RevenueMinor != 250_000 {
		t.Errorf("revenue = %d, want 250000 — a reader who sees -250000 stops believing the "+
			"report", statement.RevenueMinor)
	}
	if statement.ExpenseMinor != 40_000 {
		t.Errorf("expense = %d, want 40000", statement.ExpenseMinor)
	}
	if statement.NetProfitMinor != 210_000 {
		t.Errorf("net profit = %d, want 210000", statement.NetProfitMinor)
	}
}

// TestASubtotalIsTheSumOfItsChildrenAndNotAStoredFigure
//
// The chart is a tree, and a statement that lists every leaf is a trial balance with a filter.
// What makes it a statement is that "Revenue 250,000" can be opened.
//
// The subtotal is summed from the CHILDREN rather than read from the parent's own balance. Both
// give 250,000 today, because a posting to a non-postable account is refused — so this test
// writes the stray posting the schema does not forbid, straight to the table, and requires the
// subtotal to ignore it. Two layers keep one rule; this is the test only the summing layer can
// pass.
func TestASubtotalIsTheSumOfItsChildrenAndNotAStoredFigure(t *testing.T) {
	l := newLedger(t)
	post(t, l, l.ar, l.sale, 250_000)

	statement, err := l.svc.ProfitAndLoss(l.ctx, l.companyID, march, marchEnd)
	if err != nil {
		t.Fatalf("ProfitAndLoss: %v", err)
	}

	var parent *accounting.StatementNode
	for i := range statement.Revenue {
		if len(statement.Revenue[i].Children) > 0 {
			parent = &statement.Revenue[i]
			break
		}
	}
	if parent == nil {
		t.Fatal("the revenue section has no parent with children, so this test proves nothing " +
			"about subtotals")
	}

	var sum int64
	for _, child := range parent.Children {
		sum += child.AmountMinor
	}
	if parent.AmountMinor != sum {
		t.Errorf("%s shows %d and its children add to %d",
			parent.Code, parent.AmountMinor, sum)
	}
	if parent.AmountMinor != 250_000 {
		t.Errorf("the revenue parent shows %d, want 250000", parent.AmountMinor)
	}

	// Now the balance the service cannot produce and the schema does not forbid: 90,000 credited
	// straight onto the PARENT. Every posting path refuses this — `is_postable` is checked — so
	// the only way to write it is the way a restore, a repair script or a future importer would.
	//
	// The subtotal must not move. If it did, it would be reading the parent's own figure, and a
	// single bad row would inflate a heading while every leaf beneath it still tied.
	period := periodContaining(t, l, midMarch)
	identifier, _ := id.New()
	now := clock.Format(clock.System().Now())
	if _, err = l.store.Writer(l.ctx).ExecContext(l.ctx,
		`INSERT INTO account_balances
		   (id, company_id, account_id, fiscal_period_id, branch_id,
		    debit_minor, credit_minor, updated_at)
		 VALUES (?, ?, ?, ?, NULL, 0, 90000, ?)`,
		string(identifier), string(l.companyID), string(parent.AccountID), string(period),
		now); err != nil {
		t.Fatalf("writing a balance onto the parent: %v", err)
	}

	statement, err = l.svc.ProfitAndLoss(l.ctx, l.companyID, march, marchEnd)
	if err != nil {
		t.Fatalf("ProfitAndLoss: %v", err)
	}
	for _, node := range statement.Revenue {
		if node.AccountID != parent.AccountID {
			continue
		}
		if node.AmountMinor != 250_000 {
			t.Errorf("the subtotal moved to %d after a balance was written onto the parent — "+
				"it is reading the parent's own figure rather than summing its children",
				node.AmountMinor)
		}
	}
	// And the section total moved by nothing either, for the same reason.
	if _, total := statement.RevenueMinor, statement.RevenueMinor; total != 250_000 {
		t.Errorf("revenue total = %d, want 250000", total)
	}
}

// periodContaining finds the fiscal period a date falls in.
func periodContaining(t *testing.T, l ledger, date string) id.ID {
	t.Helper()
	var period id.ID
	if err := l.store.Reader(l.ctx).QueryRowContext(l.ctx,
		`SELECT p.id FROM fiscal_periods p
		   JOIN fiscal_years y ON y.id = p.fiscal_year_id
		  WHERE y.company_id = ? AND p.start_date <= ? AND p.end_date >= ?`,
		string(l.companyID), date, date).Scan(&period); err != nil {
		t.Fatalf("finding the period containing %s: %v", date, err)
	}
	return period
}

// ── the criterion this step exists for ──────────────────────────────────────────

// TestABalanceSheetBalancesBeforeTheYearIsClosed
//
// # The one that matters
//
// Assets equal liabilities plus equity only once revenue and expense have been closed out. Until
// the year end, the profit so far sits in accounts a balance sheet does not show — so a sheet
// that ignored them would be out of balance BY EXACTLY THE PROFIT, every day of every year
// except one.
//
// It is the defect that would ship, because it is invisible on an empty company and on the day
// after closing, and because the number it is wrong by looks like a plausible figure.
func TestABalanceSheetBalancesBeforeTheYearIsClosed(t *testing.T) {
	l := newLedger(t)

	// Trading, unclosed: a sale on credit and rent paid in cash.
	post(t, l, l.ar, l.sale, 250_000)
	post(t, l, accountID(t, l.fixture, "5200"), l.cash, 40_000)

	sheet, err := l.svc.BalanceSheet(l.ctx, l.companyID, yearEnd)
	if err != nil {
		t.Fatalf("BalanceSheet: %v", err)
	}

	if sheet.OutOfBalanceMinor != 0 {
		t.Errorf("the balance sheet is out by %d — assets %d, liabilities %d, equity %d "+
			"(of which %d is the unclosed result)", sheet.OutOfBalanceMinor,
			sheet.AssetMinor, sheet.LiabilityMinor, sheet.EquityMinor, sheet.ResultMinor)
	}

	// And it balances for the RIGHT reason: the result is carried, not an accident of two
	// mistakes cancelling.
	if sheet.ResultMinor != 210_000 {
		t.Errorf("unclosed result = %d, want 210000 — the same figure the profit and loss "+
			"reports as net profit", sheet.ResultMinor)
	}
	// Assets moved by the sale less the rent: receivables 250,000, cash -40,000.
	if sheet.AssetMinor != 210_000 {
		t.Errorf("assets = %d, want 210000", sheet.AssetMinor)
	}
}

// TestABalanceSheetThatDoesNotBalanceSaysSo
//
// The other half of the criterion, and the half nothing tested: a drill hardcoding
// `OutOfBalanceMinor = 0` left every balance-sheet test green, because they all assert it IS
// zero. **A detector tested only where it should stay silent is a detector nobody has heard.**
//
// The damage is written straight to `account_balances`, because no path through the service can
// produce it — `NewEntry` cannot construct an unbalanced entry at all (§20.2). What CAN produce
// it is a restore, a repair script, or a bug in the incremental balance maintenance, and those
// are exactly the cases §20.5's verifier exists for.
//
// REPORTED, not refused. A statement that declined to render when the books are wrong would hide
// the evidence needed to find out why — 7.4 D3's rule, applied to a screen instead of a job.
func TestABalanceSheetThatDoesNotBalanceSaysSo(t *testing.T) {
	l := newLedger(t)
	post(t, l, l.ar, l.sale, 250_000)

	// 90,000 debited to cash, matched by nothing.
	period := periodContaining(t, l, midMarch)
	identifier, _ := id.New()
	now := clock.Format(clock.System().Now())
	if _, err := l.store.Writer(l.ctx).ExecContext(l.ctx,
		`INSERT INTO account_balances
		   (id, company_id, account_id, fiscal_period_id, branch_id,
		    debit_minor, credit_minor, updated_at)
		 VALUES (?, ?, ?, ?, NULL, 90000, 0, ?)`,
		string(identifier), string(l.companyID), string(l.cash), string(period),
		now); err != nil {
		t.Fatalf("writing an unmatched balance: %v", err)
	}

	sheet, err := l.svc.BalanceSheet(l.ctx, l.companyID, yearEnd)
	if err != nil {
		t.Fatalf("BalanceSheet: %v", err)
	}

	if sheet.OutOfBalanceMinor != 90_000 {
		t.Errorf("out of balance = %d, want 90000 — the books are wrong by exactly that and the "+
			"statement did not say so", sheet.OutOfBalanceMinor)
	}
	// And it still RENDERED. Refusing would leave nobody able to see which side is wrong.
	if len(sheet.Asset) == 0 {
		t.Error("the balance sheet refused to render, which hides the evidence")
	}
}

// TestTheUnclosedResultIsTheSameFigureBothStatementsReport
//
// Two statements, one company, one truth. A balance sheet whose carried result disagreed with
// the profit and loss would leave a shopkeeper with two answers and no way to choose.
func TestTheUnclosedResultIsTheSameFigureBothStatementsReport(t *testing.T) {
	l := newLedger(t)
	post(t, l, l.ar, l.sale, 250_000)
	post(t, l, accountID(t, l.fixture, "5200"), l.cash, 40_000)

	profit, err := l.svc.ProfitAndLoss(l.ctx, l.companyID, beforeTime, yearEnd)
	if err != nil {
		t.Fatalf("ProfitAndLoss: %v", err)
	}
	sheet, err := l.svc.BalanceSheet(l.ctx, l.companyID, yearEnd)
	if err != nil {
		t.Fatalf("BalanceSheet: %v", err)
	}

	if profit.NetProfitMinor != sheet.ResultMinor {
		t.Errorf("the profit and loss says %d and the balance sheet carries %d",
			profit.NetProfitMinor, sheet.ResultMinor)
	}
}

// ── honesty about what a statement can answer ───────────────────────────────────

// TestAStatementSaysWhichWholePeriodsItActuallyCovers
//
// `account_balances` is a per-PERIOD projection, so a request for the first fortnight of March
// cannot be answered exactly. The statement widens to whole periods and REPORTS both ranges,
// because the alternative is showing a month's figures under a fortnight's heading — which is
// how somebody concludes their sales doubled.
func TestAStatementSaysWhichWholePeriodsItActuallyCovers(t *testing.T) {
	l := newLedger(t)
	post(t, l, l.ar, l.sale, 250_000)

	statement, err := l.svc.ProfitAndLoss(l.ctx, l.companyID, march, midMarch)
	if err != nil {
		t.Fatalf("ProfitAndLoss: %v", err)
	}

	if statement.RequestedTo != midMarch {
		t.Errorf("the statement forgot what was asked for: %q", statement.RequestedTo)
	}
	if statement.Covered.To != marchEnd {
		t.Errorf("covered to %q, want %q — a mid-period request must widen to the whole period",
			statement.Covered.To, marchEnd)
	}
	if statement.Covered.Periods != 1 {
		t.Errorf("covered %d periods, want 1", statement.Covered.Periods)
	}
	// The figures are March's, in full — which is exactly why the covered range must be
	// reported rather than assumed.
	if statement.RevenueMinor != 250_000 {
		t.Errorf("revenue = %d, want the whole period's 250000", statement.RevenueMinor)
	}

	// A range spanning SEVERAL periods, because a one-period range cannot tell the two ends
	// apart: a drill swapping the latest end for the earliest left every assertion above green,
	// since with one period they are the same date.
	statement, err = l.svc.ProfitAndLoss(l.ctx, l.companyID, midMarch, "2026-05-20")
	if err != nil {
		t.Fatalf("ProfitAndLoss: %v", err)
	}
	if statement.Covered.Periods != 3 {
		t.Errorf("covered %d periods, want 3 — March, April and May",
			statement.Covered.Periods)
	}
	if statement.Covered.From != march {
		t.Errorf("covered from %q, want %q — the start widens back to the period's own start",
			statement.Covered.From, march)
	}
	if statement.Covered.To != "2026-05-31" {
		t.Errorf("covered to %q, want 2026-05-31 — the LATEST end of the periods included, not "+
			"the earliest", statement.Covered.To)
	}
}

// TestAStatementOverAnEmptyCompanyIsEmptyAndNotAnError
//
// DoD criterion 11. A company that has posted nothing has no figures, and that is an ordinary
// state on the first day — not a failure, and not a zero dressed up as an answer.
func TestAStatementOverAnEmptyCompanyIsEmptyAndNotAnError(t *testing.T) {
	l := newLedger(t)

	statement, err := l.svc.ProfitAndLoss(l.ctx, l.companyID, march, marchEnd)
	if err != nil {
		t.Fatalf("ProfitAndLoss over an untraded company: %v", err)
	}
	if statement.RevenueMinor != 0 || statement.ExpenseMinor != 0 {
		t.Errorf("an untraded company reports revenue %d and expense %d",
			statement.RevenueMinor, statement.ExpenseMinor)
	}
	// The chart exists, so the SECTIONS are there — an empty report still shows a reader what
	// the company would have reported on.
	if len(statement.Revenue) == 0 {
		t.Error("the revenue section is absent, not empty — a reader cannot tell whether the " +
			"company has no revenue or the report is broken")
	}

	sheet, err := l.svc.BalanceSheet(l.ctx, l.companyID, marchEnd)
	if err != nil {
		t.Fatalf("BalanceSheet over an untraded company: %v", err)
	}
	if sheet.OutOfBalanceMinor != 0 {
		t.Errorf("an untraded company's balance sheet is out by %d", sheet.OutOfBalanceMinor)
	}
}

// TestAStatementBeforeAnyFiscalPeriodExistsCoversNothing
//
// A range that reaches no period at all. MIN over no rows is one row holding NULL, which is the
// scan this would fail on — and "the company's first week" is when a shopkeeper is most likely
// to open the reports screen out of curiosity.
func TestAStatementBeforeAnyFiscalPeriodExistsCoversNothing(t *testing.T) {
	l := newLedger(t)

	statement, err := l.svc.ProfitAndLoss(l.ctx, l.companyID, "2019-01-01", "2019-12-31")
	if err != nil {
		t.Fatalf("ProfitAndLoss before the company existed: %v", err)
	}
	if statement.Covered.Periods != 0 {
		t.Errorf("covered %d periods, want 0", statement.Covered.Periods)
	}
	if statement.Covered.From != "" || statement.Covered.To != "" {
		t.Errorf("covered range is %q..%q, want empty",
			statement.Covered.From, statement.Covered.To)
	}
	if statement.RevenueMinor != 0 {
		t.Errorf("revenue = %d over a range with no periods", statement.RevenueMinor)
	}
}

// TestARangeThatEndsBeforeItStartsIsRefused
func TestARangeThatEndsBeforeItStartsIsRefused(t *testing.T) {
	l := newLedger(t)

	_, err := l.svc.ProfitAndLoss(l.ctx, l.companyID, marchEnd, march)
	if code := errs.CodeOf(err); code != accounting.CodeInvalidRange {
		t.Errorf("a backwards range gave %q, want %q", code, accounting.CodeInvalidRange)
	}

	_, err = l.svc.ProfitAndLoss(l.ctx, l.companyID, "", marchEnd)
	if code := errs.CodeOf(err); code != accounting.CodeInvalidRange {
		t.Errorf("an open-ended range gave %q, want %q", code, accounting.CodeInvalidRange)
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────────

// post writes one balanced entry through the real service, so every figure these tests read has
// been through the ledger's own guards rather than inserted beside them.
func post(t *testing.T, l ledger, debit, credit id.ID, amountMinor int64) {
	t.Helper()
	if _, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: debit, Debit: amountMinor},
			{AccountID: credit, Credit: amountMinor},
		},
	}); err != nil {
		t.Fatalf("Post: %v", err)
	}
}
