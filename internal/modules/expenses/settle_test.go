package expenses_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/expenses"
	"github.com/mizan-erp/mizan/internal/modules/expenses/domain"
)

// owed records an expense on account and returns it, which is what a settlement clears.
func (f fixture) owed(t *testing.T, netMinor int64) domain.Expense {
	t.Helper()
	expenseID := f.draft(t, domain.OnAccount, "")
	f.addLine(t, expenseID, "RENT", netMinor)
	posted, err := f.svc.Record(f.ctx, expenseID)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	return posted
}

func TestSettlingAnExpenseClearsWhatWasOwed(t *testing.T) {
	// The expense already reached its category account when it was recorded. Settling it moves
	// only what is owed and where the money came from.
	f := newFixture(t)
	expense := f.owed(t, 100_000)

	before := f.balances(t)
	if _, err := f.svc.Settle(f.ctx, expenses.SettleInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		PayeeName: "The landlord", PaymentDate: "2026-09-30",
		Method: domain.BankTransfer, Currency: "SAR", AmountMinor: expense.TotalMinor,
		Settle: []expenses.Allocation{
			{ExpenseID: expense.ID, AmountMinor: expense.TotalMinor},
		},
	}); err != nil {
		t.Fatalf("Settle: %v", err)
	}
	after := f.balances(t)

	// What we owed falls, and the bank falls with it.
	if after["2100"]-before["2100"] != expense.TotalMinor {
		t.Errorf("payable moved %d, want %d", after["2100"]-before["2100"], expense.TotalMinor)
	}
	if after["1120"]-before["1120"] != -expense.TotalMinor {
		t.Errorf("bank moved %d, want -%d", after["1120"]-before["1120"], expense.TotalMinor)
	}
	// The rent account is untouched: the expense reached it when it was recorded, and settling
	// it again would double the cost.
	if after["5200"] != before["5200"] {
		t.Errorf("rent moved on settlement by %d — the expense was counted twice",
			after["5200"]-before["5200"])
	}

	outstanding, err := f.svc.OutstandingOn(f.ctx, expense.ID)
	if err != nil {
		t.Fatalf("OutstandingOn: %v", err)
	}
	if outstanding != 0 {
		t.Errorf("outstanding = %d, want 0", outstanding)
	}
}

func TestTheMethodDecidesWhereTheMoneyCameFrom(t *testing.T) {
	// §20.3 again, on the third module to need it. Which account the money left is a seed-file
	// decision, and a business whose cheques clear elsewhere edits data.
	for _, test := range []struct {
		method  domain.Method
		account string
		other   string
	}{
		{domain.Cash, "1110", "1120"},
		{domain.BankTransfer, "1120", "1110"},
		{domain.Cheque, "1120", "1110"},
	} {
		t.Run(string(test.method), func(t *testing.T) {
			f := newFixture(t)
			expense := f.owed(t, 100_000)

			before := f.balances(t)
			if _, err := f.svc.Settle(f.ctx, expenses.SettleInput{
				CompanyID: f.companyID, BranchID: f.branchID,
				PayeeName: "The landlord", PaymentDate: "2026-09-30",
				Method: test.method, Currency: "SAR", AmountMinor: expense.TotalMinor,
				Settle: []expenses.Allocation{
					{ExpenseID: expense.ID, AmountMinor: expense.TotalMinor},
				},
			}); err != nil {
				t.Fatalf("Settle: %v", err)
			}
			after := f.balances(t)

			if after[test.account]-before[test.account] != -expense.TotalMinor {
				t.Errorf("%s moved %d, want -%d", test.account,
					after[test.account]-before[test.account], expense.TotalMinor)
			}
			if after[test.other] != before[test.other] {
				t.Errorf("paying by %s moved %s, which it must not touch",
					test.method, test.other)
			}
		})
	}
}

func TestAnExpensePaidWhenRecordedCannotBeSettledAgain(t *testing.T) {
	// It is already gone. Settling it again would credit the bank twice for one payment, and
	// nothing downstream would look wrong — just a bank balance quietly short.
	f := newFixture(t)

	expenseID := f.draft(t, domain.Immediate, domain.Cash)
	f.addLine(t, expenseID, "RENT", 100_000)
	posted, err := f.svc.Record(f.ctx, expenseID)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	_, err = f.svc.Settle(f.ctx, expenses.SettleInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		PayeeName: "The landlord", PaymentDate: "2026-09-30",
		Method: domain.Cash, Currency: "SAR", AmountMinor: posted.TotalMinor,
		Settle: []expenses.Allocation{
			{ExpenseID: posted.ID, AmountMinor: posted.TotalMinor},
		},
	})
	if code := errs.CodeOf(err); code != domain.CodeAlreadyPaid {
		t.Fatalf("code = %q, want %q", code, domain.CodeAlreadyPaid)
	}

	// And it reports nothing outstanding, whatever the allocations say.
	outstanding, err := f.svc.OutstandingOn(f.ctx, posted.ID)
	if err != nil {
		t.Fatalf("OutstandingOn: %v", err)
	}
	if outstanding != 0 {
		t.Errorf("an expense paid on the spot owes %d", outstanding)
	}
}

func TestOneSettlementCanClearSeveralExpenses(t *testing.T) {
	// Paying a landlord three months at once. The shape a `paid_minor` column on an expense
	// cannot express.
	f := newFixture(t)

	first := f.owed(t, 100_000)
	second := f.owed(t, 50_000)
	total := first.TotalMinor + second.TotalMinor

	if _, err := f.svc.Settle(f.ctx, expenses.SettleInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		PayeeName: "The landlord", PaymentDate: "2026-09-30",
		Method: domain.BankTransfer, Currency: "SAR", AmountMinor: total,
		Settle: []expenses.Allocation{
			{ExpenseID: first.ID, AmountMinor: first.TotalMinor},
			{ExpenseID: second.ID, AmountMinor: second.TotalMinor},
		},
	}); err != nil {
		t.Fatalf("Settle: %v", err)
	}

	for _, expense := range []domain.Expense{first, second} {
		outstanding, err := f.svc.OutstandingOn(f.ctx, expense.ID)
		if err != nil {
			t.Fatalf("OutstandingOn: %v", err)
		}
		if outstanding != 0 {
			t.Errorf("expense %s still owes %d", expense.Number, outstanding)
		}
	}
}

func TestOneExpenseCanTakeSeveralSettlements(t *testing.T) {
	// Part now, the rest next month. The other shape the column cannot express.
	f := newFixture(t)
	expense := f.owed(t, 100_000)

	half := expense.TotalMinor / 2
	for _, amount := range []int64{half, expense.TotalMinor - half} {
		if _, err := f.svc.Settle(f.ctx, expenses.SettleInput{
			CompanyID: f.companyID, BranchID: f.branchID,
			PayeeName: "The landlord", PaymentDate: "2026-09-30",
			Method: domain.Cash, Currency: "SAR", AmountMinor: amount,
			Settle: []expenses.Allocation{
				{ExpenseID: expense.ID, AmountMinor: amount},
			},
		}); err != nil {
			t.Fatalf("Settle(%d): %v", amount, err)
		}
	}

	outstanding, err := f.svc.OutstandingOn(f.ctx, expense.ID)
	if err != nil {
		t.Fatalf("OutstandingOn: %v", err)
	}
	if outstanding != 0 {
		t.Errorf("outstanding = %d, want 0", outstanding)
	}
}

func TestAnExpenseCannotBeOverSettled(t *testing.T) {
	// A keying error that leaves the payable overdrawn and reconciles against nothing.
	f := newFixture(t)
	expense := f.owed(t, 100_000)

	_, err := f.svc.Settle(f.ctx, expenses.SettleInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		PayeeName: "The landlord", PaymentDate: "2026-09-30",
		Method: domain.Cash, Currency: "SAR", AmountMinor: expense.TotalMinor * 2,
		Settle: []expenses.Allocation{
			{ExpenseID: expense.ID, AmountMinor: expense.TotalMinor * 2},
		},
	})
	if code := errs.CodeOf(err); code != domain.CodeOverSettled {
		t.Fatalf("code = %q, want %q", code, domain.CodeOverSettled)
	}
}

func TestASettlementCannotAllocateMoreThanItIsWorth(t *testing.T) {
	// Arithmetic that cannot be true: the money does not exist. The kernel decides the rule and
	// this module decides what the user is told.
	f := newFixture(t)
	expense := f.owed(t, 100_000)

	_, err := f.svc.Settle(f.ctx, expenses.SettleInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		PayeeName: "The landlord", PaymentDate: "2026-09-30",
		Method: domain.Cash, Currency: "SAR", AmountMinor: 100,
		Settle: []expenses.Allocation{
			{ExpenseID: expense.ID, AmountMinor: 5_000},
		},
	})
	if code := errs.CodeOf(err); code != domain.CodeOverAllocated {
		t.Fatalf("code = %q, want %q", code, domain.CodeOverAllocated)
	}
}

func TestWhatIsStillOwedIsListedOldestDueFirst(t *testing.T) {
	// The report somebody opens to decide what to pay this week. Ordered by due date because
	// that is the question — not by when it was entered.
	f := newFixture(t)

	for _, due := range []string{"2026-11-30", "2026-09-30", "2026-10-31"} {
		expenseID := f.draft(t, domain.OnAccount, "")
		if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
			`UPDATE expenses SET due_date = ? WHERE id = ?`, due, string(expenseID)); err != nil {
			t.Fatalf("setting the due date: %v", err)
		}
		f.addLine(t, expenseID, "RENT", 10_000)
		if _, err := f.svc.Record(f.ctx, expenseID); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	unsettled, err := f.svc.Unsettled(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Unsettled: %v", err)
	}
	if len(unsettled) != 3 {
		t.Fatalf("%d unsettled, want 3", len(unsettled))
	}
	for i := 1; i < len(unsettled); i++ {
		if unsettled[i-1].DueDate > unsettled[i].DueDate {
			t.Fatalf("not ordered by due date: %v",
				[]string{unsettled[0].DueDate, unsettled[1].DueDate, unsettled[2].DueDate})
		}
	}

	// And a settled one drops off the list, because the list is what is still to PAY.
	if _, err = f.svc.Settle(f.ctx, expenses.SettleInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		PayeeName: "The landlord", PaymentDate: "2026-09-30",
		Method: domain.Cash, Currency: "SAR", AmountMinor: unsettled[0].TotalMinor,
		Settle: []expenses.Allocation{
			{ExpenseID: unsettled[0].ID, AmountMinor: unsettled[0].TotalMinor},
		},
	}); err != nil {
		t.Fatalf("Settle: %v", err)
	}

	after, err := f.svc.Unsettled(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Unsettled: %v", err)
	}
	if len(after) != 2 {
		t.Errorf("%d unsettled after paying one, want 2", len(after))
	}
}

func TestAPaymentOnAccountNeedsNoAllocation(t *testing.T) {
	// Money against a landlord's running balance is real, and forcing an allocation would mean
	// inventing one against an expense that has not been entered yet.
	f := newFixture(t)
	books := f.balances(t)

	if _, err := f.svc.Settle(f.ctx, expenses.SettleInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		PartnerID: f.partnerID, PayeeName: "The landlord", PaymentDate: "2026-09-30",
		Method: domain.BankTransfer, Currency: "SAR", AmountMinor: 5_000,
	}); err != nil {
		t.Fatalf("Settle: %v", err)
	}

	after := f.balances(t)
	if after["1120"]-books["1120"] != -5_000 {
		t.Errorf("bank moved %d, want -5000", after["1120"]-books["1120"])
	}
	if after["2100"]-books["2100"] != 5_000 {
		t.Errorf("payable moved %d, want 5000", after["2100"]-books["2100"])
	}
}

func TestAnUnknownExpenseCannotBeSettled(t *testing.T) {
	f := newFixture(t)
	missing, _ := id.New()

	_, err := f.svc.Settle(f.ctx, expenses.SettleInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		PayeeName: "Somebody", PaymentDate: "2026-09-30",
		Method: domain.Cash, Currency: "SAR", AmountMinor: 100,
		Settle: []expenses.Allocation{{ExpenseID: missing, AmountMinor: 100}},
	})
	if code := errs.CodeOf(err); code != expenses.CodeUnknownExpense {
		t.Fatalf("code = %q, want %q", code, expenses.CodeUnknownExpense)
	}
}

// TestADraftSettlementPaysNothing
//
// # The same unreachable guard purchasing found in 6.7
//
// `Settle` drafts and posts in ONE transaction, so no draft settlement ever carries allocations
// through the service — which makes the `status = 'posted'` filter unreachable from the API, and a
// drill removing it changed nothing.
//
// The filter is still right, and the day a draft-settlement flow is added is the day it matters:
// allocations against a payment somebody is still typing would show an expense cleared by money
// that has not left, and the landlord would be paid twice.
//
// Asserted where it can only pass if the REPOSITORY keeps it — writing straight to the table,
// which is the Phase 3 rule applied to a guard the service cannot reach.
func TestADraftSettlementPaysNothing(t *testing.T) {
	f := newFixture(t)
	expense := f.owed(t, 100_000)

	paymentID, _ := id.New()
	allocationID, _ := id.New()
	now := "2026-09-30"

	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx, `
		INSERT INTO expense_payments (
			id, company_id, branch_id, payee_name, payment_date,
			method, currency_code, amount_minor, status,
			row_version, created_at, updated_at
		) VALUES (?, ?, ?, 'The landlord', ?, 'cash', 'SAR', ?, 'draft', 1, ?, ?)`,
		string(paymentID), string(f.companyID), string(f.branchID),
		now, expense.TotalMinor, now, now); err != nil {
		t.Fatalf("inserting a draft settlement: %v", err)
	}
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx, `
		INSERT INTO expense_payment_allocations (
			id, payment_id, expense_id, amount_minor, created_at
		) VALUES (?, ?, ?, ?, ?)`,
		string(allocationID), string(paymentID), string(expense.ID),
		expense.TotalMinor, now); err != nil {
		t.Fatalf("inserting a draft allocation: %v", err)
	}

	outstanding, err := f.svc.OutstandingOn(f.ctx, expense.ID)
	if err != nil {
		t.Fatalf("OutstandingOn: %v", err)
	}
	if outstanding != expense.TotalMinor {
		t.Fatalf("outstanding = %d, want the whole %d — a draft settlement was counted",
			outstanding, expense.TotalMinor)
	}

	// And it is still on the list of what to pay, which is the report somebody acts on.
	unsettled, err := f.svc.Unsettled(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Unsettled: %v", err)
	}
	if len(unsettled) != 1 {
		t.Errorf("%d unsettled, want 1 — a draft settlement removed it from the list",
			len(unsettled))
	}
}
