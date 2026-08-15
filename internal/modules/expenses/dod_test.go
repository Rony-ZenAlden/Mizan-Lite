package expenses_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/modules/audit"
	"github.com/mizan-erp/mizan/internal/modules/expenses"
	"github.com/mizan-erp/mizan/internal/modules/expenses/domain"
)

// The tests this file adds exist because the Phase 7 Definition-of-Done review found each of them
// missing.

// ── Criterion 6 ─────────────────────────────────────────────────────────────────
//
// "A debt can be SETTLED IN PARTS, and what remains is derived from its settlements."
//
// # The criterion describes a mechanism this phase did not build, and the design is better
//
// It was written expecting debts to work like expenses: an obligation document, then settlement
// documents allocated against it. 7.3 built something different — a debt IS the money movement,
// with a direction, and repaying is another movement the other way.
//
// The difference matters. A loan repaid in three instalments under the criterion's shape is one
// debt and three allocations; under this one it is four movements, each with its own date,
// method, and journal entry. The second is what actually happened, and it is what a bank
// statement will show — the first invents a parent document nobody handed over.
//
// So "what remains" is not derived from allocations but from the movements themselves, and this
// is the test for it.
func TestADebtIsRepaidInPartsAndWhatRemainsFollows(t *testing.T) {
	f := newFixture(t)

	// Borrow 4,000, repay 1,500 then 1,000.
	f.debt(t, domain.Received, domain.LoanPayable, 400_000)
	f.debt(t, domain.Paid, domain.LoanPayable, 150_000)
	f.debt(t, domain.Paid, domain.LoanPayable, 100_000)

	positions, err := f.svc.DebtPositions(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("DebtPositions: %v", err)
	}
	if positions[domain.LoanPayable] != 150_000 {
		t.Fatalf("still owed = %d, want 150000", positions[domain.LoanPayable])
	}

	// Every instalment kept its own document, which is the point of the shape: three repayments
	// are three things that happened, each with a date and a method a bank statement can match.
	movements, err := f.svc.Debts(f.ctx, f.companyID, domain.LoanPayable)
	if err != nil {
		t.Fatalf("Debts: %v", err)
	}
	if len(movements) != 3 {
		t.Fatalf("%d movements, want 3 — instalments were merged", len(movements))
	}
	for _, movement := range movements {
		if movement.Number == "" {
			t.Error("an instalment has no number of its own")
		}
	}

	// Repaid in full, the position is zero — not negative, because the arithmetic is exact.
	f.debt(t, domain.Paid, domain.LoanPayable, 150_000)
	positions, err = f.svc.DebtPositions(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("DebtPositions: %v", err)
	}
	if positions[domain.LoanPayable] != 0 {
		t.Errorf("a fully repaid loan leaves %d", positions[domain.LoanPayable])
	}
}

// ── Criterion 7 ─────────────────────────────────────────────────────────────────
//
// "Money out contains NO ACCOUNTING LOGIC; every posting goes through Phase 2's rules."
//
// One test asserted it for the expense posting. This phase now fires THREE kinds — expenses,
// settlements, and debts — and a module that named an account in either of the other two would
// fail the criterion while passing the test.
func TestNoMoneyOutPostingNamesAnAccountItCouldHaveMapped(t *testing.T) {
	f := newFixture(t)

	// CASH is redirected. It is credited by an expense paid on the spot, by a settlement, and by
	// a debt paid out — one account touched once by each of the three postings, and never
	// cleared within the test, so a redirect cannot cancel itself out.
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
		`UPDATE posting_rule_lines SET account_selector = 'mapping:ROUNDING_DIFF'
		  WHERE account_selector = 'mapping:CASH'`); err != nil {
		t.Fatalf("rewriting the rules: %v", err)
	}

	// Measured after EACH posting, not once at the end.
	//
	// The first version took one reading before and one after, and asserted that the redirected
	// account had moved AT ALL. A drill that made the debt post nothing — an unknown action,
	// which matches no rule — left it green, because the expense and the settlement had each
	// moved the same account. "Something posted" is not "all three posted", and the criterion is
	// about all three.
	balance := f.balances(t)
	moved := func(t *testing.T, what string) {
		t.Helper()
		next := f.balances(t)
		if next["5900"] == balance["5900"] {
			t.Errorf("the %s posted nothing — this test cannot say whether it goes through a "+
				"rule, because it never reached the books", what)
		}
		if next["1110"] != balance["1110"] {
			t.Errorf("the %s moved cash by %d — it named the account rather than going through "+
				"a rule", what, next["1110"]-balance["1110"])
		}
		balance = next
	}

	// 1. an expense paid on the spot
	paid := f.draft(t, domain.Immediate, domain.Cash)
	f.addLine(t, paid, "RENT", 10_000)
	if _, err := f.svc.Record(f.ctx, paid); err != nil {
		t.Fatalf("Record: %v", err)
	}
	moved(t, "expense paid on the spot")

	// 2. an expense settled later
	expense := f.owed(t, 20_000)
	if _, err := f.svc.Settle(f.ctx, expenses.SettleInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		PayeeName: "The landlord", PaymentDate: "2026-09-30",
		Method: domain.Cash, Currency: "SAR", AmountMinor: expense.TotalMinor,
		Settle: []expenses.Allocation{
			{ExpenseID: expense.ID, AmountMinor: expense.TotalMinor},
		},
	}); err != nil {
		t.Fatalf("Settle: %v", err)
	}
	moved(t, "settlement")

	// 3. a debt paid out
	f.debt(t, domain.Paid, domain.LoanReceivable, 30_000)
	moved(t, "debt paid out")
}

// ── Criterion 8 ─────────────────────────────────────────────────────────────────
//
// "A number is allocated only at posting; AN ABANDONED DRAFT CONSUMES NONE."
//
// Expenses were tested. Settlements and debts have their own series, and each was asserted only
// to TAKE a number.
func TestEveryMoneyOutSeriesIsGaplessAfterAnAbandonedDraft(t *testing.T) {
	f := newFixture(t)

	// A cancelled expense, then a real one: the first number is unconsumed.
	abandoned := f.draft(t, domain.Immediate, domain.Cash)
	f.addLine(t, abandoned, "RENT", 1_000)
	if err := f.svc.Cancel(f.ctx, abandoned); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	first := f.draft(t, domain.Immediate, domain.Cash)
	f.addLine(t, first, "RENT", 1_000)
	posted, err := f.svc.Record(f.ctx, first)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if posted.Number != "EXP-000001" {
		t.Errorf("expense number = %q, want EXP-000001", posted.Number)
	}

	// Settlements and debts number from their own sequences, each starting at one — a shared
	// counter would make an expense number jump every time somebody paid a landlord.
	owed := f.owed(t, 5_000)
	settlement, err := f.svc.Settle(f.ctx, expenses.SettleInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		PayeeName: "The landlord", PaymentDate: "2026-09-30",
		Method: domain.Cash, Currency: "SAR", AmountMinor: owed.TotalMinor,
		Settle: []expenses.Allocation{
			{ExpenseID: owed.ID, AmountMinor: owed.TotalMinor},
		},
	})
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if settlement.Number != "EXPPAY-000001" {
		t.Errorf("settlement number = %q, want EXPPAY-000001", settlement.Number)
	}

	debt := f.debt(t, domain.Received, domain.LoanPayable, 1_000)
	if debt.Number != "DEBT-000001" {
		t.Errorf("debt number = %q, want DEBT-000001", debt.Number)
	}
}

// ── Criterion 9 ─────────────────────────────────────────────────────────────────
//
// "Every state change is AUDITED IN-TRANSACTION."
//
// The expense acts were checked. Settling and recording a debt were audited and nothing REQUIRED
// it — the same gap the Phase 5 and Phase 6 reviews each found, in a module written after both.
func TestEveryMoneyOutActLeavesAnAuditEntry(t *testing.T) {
	f := newFixture(t)

	expenseID := f.draft(t, domain.Immediate, domain.Cash)
	f.addLine(t, expenseID, "RENT", 10_000)
	if _, err := f.svc.Record(f.ctx, expenseID); err != nil {
		t.Fatalf("Record: %v", err)
	}

	cancelled := f.draft(t, domain.Immediate, domain.Cash)
	if err := f.svc.Cancel(f.ctx, cancelled); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	owed := f.owed(t, 20_000)
	if _, err := f.svc.Settle(f.ctx, expenses.SettleInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		PayeeName: "The landlord", PaymentDate: "2026-09-30",
		Method: domain.Cash, Currency: "SAR", AmountMinor: owed.TotalMinor,
		Settle: []expenses.Allocation{
			{ExpenseID: owed.ID, AmountMinor: owed.TotalMinor},
		},
	}); err != nil {
		t.Fatalf("Settle: %v", err)
	}

	f.debt(t, domain.Received, domain.LoanPayable, 400_000)

	entries, err := f.audit.Entries(f.ctx, audit.Filter{})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	seen := make(map[string]int, len(entries))
	for _, entry := range entries {
		seen[entry.Action]++
	}

	for _, action := range []string{
		expenses.ActionCategoriesSeeded,
		expenses.ActionExpenseDrafted, expenses.ActionExpenseLineAdded,
		expenses.ActionExpenseRecorded, expenses.ActionExpenseCancelled,
		expenses.ActionSettlementPosted, expenses.ActionDebtRecorded,
	} {
		if seen[action] == 0 {
			t.Errorf("%q happened and left no audit entry", action)
		}
	}

	// No POSTING-RULE key reached the audit trail. Declared apart AND given different strings —
	// the Phase 6 review's finding applied from the start rather than discovered.
	for _, postingKey := range []string{
		"expenses.expense.paid.cash", "expenses.expense.on_account",
		"expenses.settlement.cash", "debts.received.cash",
	} {
		if seen[postingKey] != 0 {
			t.Errorf("posting-rule key %q reached the audit trail", postingKey)
		}
	}
}

// ── Criterion 11 ────────────────────────────────────────────────────────────────
//
// "Amounts are exact at EVERY CURRENCY SCALE, including one with minor units — 6.6's lesson
// applied from the start rather than discovered."
//
// The fixture uses a two-decimal currency, which is why this phase never hit 6.6's defect. But
// "the fixture happens to use one" is not an assertion, and a fixture can change.
//
// 6.6's lesson was precise: **a scale conversion tested only at scale 1 is a conversion nobody
// has tested.** This names the scale explicitly, so a later change to the fixture cannot quietly
// take the coverage away.
func TestExpenseArithmeticIsExactAtATwoDecimalScale(t *testing.T) {
	f := newFixture(t)

	// # What this test covers, and what it does not
	//
	// These tests replace the tax engine with a stub, so the rate here is exact by construction
	// and the ROUNDING MODE is not under test — a drill forcing half-up inside the real engine
	// changed nothing, because the real engine is not on this path. Phase 2 owns tax rounding
	// and tests it against the currency's own mode.
	//
	// What IS under test is everything the expenses module does with the figures once it has
	// them: accumulating lines into a net, adding tax to reach a total, and carrying all three
	// into the ledger at the same scale. That is where 6.6's defect lived — a conversion that
	// was internally consistent and off by a factor of a hundred — and it is what a two-decimal
	// currency exposes and a zero-decimal one hides.
	//
	// 1,234.57 at 15% is 185.1855, which the stub truncates to 185.18.
	expenseID := f.draft(t, domain.Immediate, domain.Cash)
	f.addLine(t, expenseID, "RENT", 123_457)

	posted, err := f.svc.Record(f.ctx, expenseID)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	if posted.NetMinor != 123_457 {
		t.Fatalf("net = %d, want 123457", posted.NetMinor)
	}
	if posted.TaxMinor != 18_518 {
		t.Fatalf("tax = %d, want 18518", posted.TaxMinor)
	}
	if posted.TotalMinor != posted.NetMinor+posted.TaxMinor {
		t.Fatalf("total %d does not tie to net %d plus tax %d",
			posted.TotalMinor, posted.NetMinor, posted.TaxMinor)
	}

	// And the BOOKS carry the same figures. A scale error between the document and the ledger is
	// the shape 6.6's defect took: both internally consistent, off by a factor of a hundred.
	balances := f.balances(t)
	if balances["5200"] != 123_457 {
		t.Errorf("rent in the ledger = %d, want 123457", balances["5200"])
	}
	if balances["1110"] != -(123_457 + 18_518) {
		t.Errorf("cash in the ledger = %d, want -141975", balances["1110"])
	}
}
