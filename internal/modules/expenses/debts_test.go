package expenses_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/modules/audit"
	"github.com/mizan-erp/mizan/internal/modules/expenses"
	"github.com/mizan-erp/mizan/internal/modules/expenses/domain"
)

func (f fixture) debt(
	t *testing.T, direction domain.Direction, kind domain.Kind, amountMinor int64,
) domain.Debt {
	t.Helper()
	debt, err := f.svc.RecordDebt(f.ctx, expenses.NewDebtInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		Direction: direction, Kind: kind,
		CounterpartyName: "Abu Khaled", DebtDate: "2026-09-05",
		Method: domain.Cash, Currency: "SAR", AmountMinor: amountMinor,
	})
	if err != nil {
		t.Fatalf("RecordDebt: %v", err)
	}
	return debt
}

// ── a debt is not revenue and not cost ──────────────────────────────────────────

func TestADebtMovesMoneyWithoutTouchingRevenueOrCost(t *testing.T) {
	// Criterion 5, and the whole reason debts are their own document. A business whose "sales"
	// included the owner's own capital would show a month it never had — and the mistake is
	// invisible, because the totals all add up, they are just about the wrong thing.
	f := newFixture(t)

	before := f.balances(t)
	f.debt(t, domain.Received, domain.LoanPayable, 400_000) // 4,000.00 borrowed
	after := f.balances(t)

	// The money arrived and we owe it.
	if after["1110"]-before["1110"] != 400_000 {
		t.Errorf("cash moved %d, want 400000", after["1110"]-before["1110"])
	}
	if after["2300"]-before["2300"] != -400_000 {
		t.Errorf("loans payable moved %d, want -400000", after["2300"]-before["2300"])
	}

	// Nothing reached revenue, cost of sales, or any expense account.
	for _, code := range []string{"4100", "5100", "5200", "5290"} {
		if after[code] != before[code] {
			t.Errorf("account %s moved by %d — a debt reached a trading account",
				code, after[code]-before[code])
		}
	}
}

func TestEachKindLandsInItsOwnAccount(t *testing.T) {
	// Three kinds, three accounts, and exactly one obligation line non-zero on each posting.
	for _, test := range []struct {
		kind      domain.Kind
		direction domain.Direction
		account   string
		want      int64
	}{
		{domain.LoanPayable, domain.Received, "2300", -400_000},
		{domain.LoanReceivable, domain.Paid, "1500", 400_000},
		{domain.OwnerEquity, domain.Received, "3200", -400_000},
		{domain.OwnerEquity, domain.Paid, "3200", 400_000},
	} {
		t.Run(string(test.kind)+"/"+string(test.direction), func(t *testing.T) {
			f := newFixture(t)
			before := f.balances(t)
			f.debt(t, test.direction, test.kind, 400_000)
			after := f.balances(t)

			if after[test.account]-before[test.account] != test.want {
				t.Errorf("%s moved %d, want %d", test.account,
					after[test.account]-before[test.account], test.want)
			}
			// And the OTHER two obligation accounts are untouched, which is what "exactly one
			// non-zero" means in practice.
			for _, other := range []string{"2300", "1500", "3200"} {
				if other == test.account {
					continue
				}
				if after[other] != before[other] {
					t.Errorf("account %s moved by %d on a %s debt",
						other, after[other]-before[other], test.kind)
				}
			}
		})
	}
}

func TestTheDirectionDecidesWhichWayTheMoneyWent(t *testing.T) {
	// Not a sign on the amount: a signed amount makes SUM(amount) meaningless and every query a
	// minefield, which is why stock movements and payments keep the same discipline.
	f := newFixture(t)

	before := f.balances(t)
	f.debt(t, domain.Received, domain.LoanPayable, 400_000)
	f.debt(t, domain.Paid, domain.LoanPayable, 150_000)
	after := f.balances(t)

	// Borrowed 4,000 and repaid 1,500: 2,500 still owed, and 2,500 more cash than we started.
	if after["2300"]-before["2300"] != -250_000 {
		t.Errorf("loans payable moved %d, want -250000", after["2300"]-before["2300"])
	}
	if after["1110"]-before["1110"] != 250_000 {
		t.Errorf("cash moved %d, want 250000", after["1110"]-before["1110"])
	}
}

func TestTheMethodDecidesWhichMoneyAccountMoved(t *testing.T) {
	// §20.3 on the fourth module to need it.
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
			before := f.balances(t)

			if _, err := f.svc.RecordDebt(f.ctx, expenses.NewDebtInput{
				CompanyID: f.companyID, BranchID: f.branchID,
				Direction: domain.Received, Kind: domain.LoanPayable,
				CounterpartyName: "Abu Khaled", DebtDate: "2026-09-05",
				Method: test.method, Currency: "SAR", AmountMinor: 400_000,
			}); err != nil {
				t.Fatalf("RecordDebt: %v", err)
			}
			after := f.balances(t)

			if after[test.account]-before[test.account] != 400_000 {
				t.Errorf("%s moved %d, want 400000", test.account,
					after[test.account]-before[test.account])
			}
			if after[test.other] != before[test.other] {
				t.Errorf("a %s debt moved %s, which it must not touch",
					test.method, test.other)
			}
		})
	}
}

// ── the net position ────────────────────────────────────────────────────────────

func TestTheNetPositionIsOneNumberPerKind(t *testing.T) {
	// The documents keep positive amounts and a direction; a POSITION is the one place a sign
	// belongs, because a reader wants one number per kind.
	f := newFixture(t)

	f.debt(t, domain.Received, domain.LoanPayable, 400_000)
	f.debt(t, domain.Paid, domain.LoanPayable, 150_000)
	f.debt(t, domain.Paid, domain.LoanReceivable, 30_000)
	f.debt(t, domain.Received, domain.OwnerEquity, 1_000_000)

	positions, err := f.svc.DebtPositions(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("DebtPositions: %v", err)
	}

	if positions[domain.LoanPayable] != 250_000 {
		t.Errorf("loans payable = %d, want 250000 (borrowed 400000 less 150000 repaid)",
			positions[domain.LoanPayable])
	}
	// We LENT 300.00, so the position is negative: money went out and is owed back to us.
	if positions[domain.LoanReceivable] != -30_000 {
		t.Errorf("loans receivable = %d, want -30000", positions[domain.LoanReceivable])
	}
	if positions[domain.OwnerEquity] != 1_000_000 {
		t.Errorf("owner's capital = %d, want 1000000", positions[domain.OwnerEquity])
	}
}

// ── what is refused ─────────────────────────────────────────────────────────────

func TestADebtNeedsACounterparty(t *testing.T) {
	// The owner is usually not a partner record and a loan from a relative rarely is — but money
	// that moved to or from nobody is a hole in the cash position nobody can explain.
	f := newFixture(t)

	_, err := f.svc.RecordDebt(f.ctx, expenses.NewDebtInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		Direction: domain.Received, Kind: domain.LoanPayable,
		CounterpartyName: "", DebtDate: "2026-09-05",
		Method: domain.Cash, Currency: "SAR", AmountMinor: 100,
	})
	if code := errs.CodeOf(err); code != domain.CodeInvalidDebt {
		t.Fatalf("code = %q, want %q", code, domain.CodeInvalidDebt)
	}
}

func TestAnUnknownKindIsRefused(t *testing.T) {
	// A closed set of three. A fourth would need an account, a mapping, and a line in eight
	// rules — so it fails here rather than posting a half-entry nobody notices.
	f := newFixture(t)

	_, err := f.svc.RecordDebt(f.ctx, expenses.NewDebtInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		Direction: domain.Received, Kind: domain.Kind("interest_payable"),
		CounterpartyName: "A bank", DebtDate: "2026-09-05",
		Method: domain.Cash, Currency: "SAR", AmountMinor: 100,
	})
	if code := errs.CodeOf(err); code != domain.CodeUnknownDebtKind {
		t.Fatalf("code = %q, want %q", code, domain.CodeUnknownDebtKind)
	}
}

func TestADebtOfNothingIsRefused(t *testing.T) {
	f := newFixture(t)
	for _, amount := range []int64{0, -100} {
		_, err := f.svc.RecordDebt(f.ctx, expenses.NewDebtInput{
			CompanyID: f.companyID, BranchID: f.branchID,
			Direction: domain.Received, Kind: domain.LoanPayable,
			CounterpartyName: "Abu Khaled", DebtDate: "2026-09-05",
			Method: domain.Cash, Currency: "SAR", AmountMinor: amount,
		})
		if errs.CodeOf(err) != domain.CodeNonPositiveAmount {
			t.Errorf("amount %d was accepted", amount)
		}
	}
}

func TestExactlyOneObligationLineCarriesTheWholeAmount(t *testing.T) {
	// Checked in the domain so the message names the DEBT. Left to Phase 2, the refusal would be
	// "this entry does not balance", which says nothing about which document or why.
	debt := domain.Debt{Kind: domain.LoanPayable, AmountMinor: 400_000}
	if err := debt.RequireBalances(); err != nil {
		t.Fatalf("a well-formed debt was refused: %v", err)
	}

	amounts := debt.KindAmounts()
	if len(amounts) != 3 {
		t.Fatalf("%d kind amounts, want all three present", len(amounts))
	}
	nonZero := 0
	for _, amount := range amounts {
		if amount != 0 {
			nonZero++
		}
	}
	if nonZero != 1 {
		t.Errorf("%d obligation lines are non-zero, want exactly 1", nonZero)
	}
}

// ── numbering and the audit trail ───────────────────────────────────────────────

func TestADebtIsNumberedAndAudited(t *testing.T) {
	f := newFixture(t)
	debt := f.debt(t, domain.Received, domain.LoanPayable, 400_000)

	if debt.Number != "DEBT-000001" {
		t.Errorf("number = %q, want DEBT-000001", debt.Number)
	}

	entries, err := f.audit.Entries(f.ctx, audit.Filter{EntityType: expenses.EntityDebt})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 1 || entries[0].Action != expenses.ActionDebtRecorded {
		t.Fatalf("entries = %+v, want one debt-recorded", entries)
	}
}
