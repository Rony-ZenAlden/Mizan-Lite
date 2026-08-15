package domain_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/expenses/domain"
)

func newID(t *testing.T) id.ID {
	t.Helper()
	identifier, err := id.New()
	if err != nil {
		t.Fatalf("id.New: %v", err)
	}
	return identifier
}

func line(t *testing.T, netMinor int64) domain.Line {
	t.Helper()
	l, err := domain.NewLine(newID(t), newID(t), newID(t), 1, "Rent", netMinor)
	if err != nil {
		t.Fatalf("NewLine: %v", err)
	}
	return l
}

// ── the settlement and the method must agree ────────────────────────────────────

func TestAnExpensePaidNowMustSayHow(t *testing.T) {
	// The method decides which account the money left. Without it the posting has nowhere to
	// take the money from, and a rule matching "expenses.expense.paid." with nothing after it
	// matches nothing at all.
	_, err := domain.NewExpense(newID(t), newID(t), newID(t),
		"Garage", "2026-09-01", "SAR", domain.Immediate, "")
	if code := errs.CodeOf(err); code != domain.CodeMethodMismatch {
		t.Fatalf("code = %q, want %q", code, domain.CodeMethodMismatch)
	}
}

func TestAnExpenseOnAccountCannotNameAMethod(t *testing.T) {
	// It has not been paid. Naming a method would assert something that has not happened, and a
	// report of "what went out of the bank" would count it.
	_, err := domain.NewExpense(newID(t), newID(t), newID(t),
		"Landlord", "2026-09-01", "SAR", domain.OnAccount, domain.BankTransfer)
	if code := errs.CodeOf(err); code != domain.CodeMethodMismatch {
		t.Fatalf("code = %q, want %q", code, domain.CodeMethodMismatch)
	}
}

func TestAnExpenseNeedsAPayeeButNotAPartner(t *testing.T) {
	// Money leaving the business went to somebody, and "cash, no record of who" is how a cash
	// position stops being defensible. But forcing a partner record for every taxi fare is how a
	// partner list becomes unusable — so it is a NAME, not a foreign key.
	_, err := domain.NewExpense(newID(t), newID(t), newID(t),
		"", "2026-09-01", "SAR", domain.Immediate, domain.Cash)
	if code := errs.CodeOf(err); code != domain.CodeNoPayee {
		t.Fatalf("code = %q, want %q", code, domain.CodeNoPayee)
	}

	expense, err := domain.NewExpense(newID(t), newID(t), newID(t),
		"A taxi", "2026-09-01", "SAR", domain.Immediate, domain.Cash)
	if err != nil {
		t.Fatalf("an expense with a payee and no partner was refused: %v", err)
	}
	if !expense.PartnerID.IsZero() {
		t.Error("an expense invented a partner")
	}
}

// ── the posting action ──────────────────────────────────────────────────────────

func TestTheSettlementAndMethodBecomeThePostingAction(t *testing.T) {
	// Cash credits the till; a transfer credits the bank; on account credits payables and touches
	// no money at all. Three different entries, and none of them decided in Go — the action names
	// what HAPPENED and the seeded rules decide where it lands (§20.3).
	for _, test := range []struct {
		settlement domain.Settlement
		method     domain.Method
		want       string
	}{
		{domain.Immediate, domain.Cash, "expenses.expense.paid.cash"},
		{domain.Immediate, domain.BankTransfer, "expenses.expense.paid.bank_transfer"},
		{domain.Immediate, domain.Cheque, "expenses.expense.paid.cheque"},
		{domain.Immediate, domain.Card, "expenses.expense.paid.card"},
		{domain.OnAccount, "", "expenses.expense.on_account"},
	} {
		expense := domain.Expense{Settlement: test.settlement, PaidMethod: test.method}
		if got := expense.PostingAction(); got != test.want {
			t.Errorf("action = %q, want %q", got, test.want)
		}
	}
}

// ── recoverable tax ─────────────────────────────────────────────────────────────

func TestNonRecoverableTaxBecomesPartOfWhatTheThingCost(t *testing.T) {
	// It is not lost. Most jurisdictions disallow reclaiming tax on entertainment, and the tax
	// paid is still money out — so it lands in the EXPENSE account rather than in the
	// recoverable-tax asset. A business that claimed it anyway would be making a claim a revenue
	// authority disallows.
	recoverable := line(t, 1_000).Tax(150, 150_000, "VAT", true)
	if recoverable.RecoverableTaxMinor() != 150 {
		t.Errorf("recoverable = %d, want 150", recoverable.RecoverableTaxMinor())
	}
	if recoverable.CostMinor() != 1_000 {
		t.Errorf("cost = %d, want 1000 — recoverable tax is not a cost",
			recoverable.CostMinor())
	}

	blocked := line(t, 1_000).Tax(150, 150_000, "VAT", false)
	if blocked.RecoverableTaxMinor() != 0 {
		t.Errorf("recoverable = %d, want 0", blocked.RecoverableTaxMinor())
	}
	if blocked.CostMinor() != 1_150 {
		t.Errorf("cost = %d, want 1150 — the blocked tax is part of what it cost",
			blocked.CostMinor())
	}
	// The TOTAL is the same either way: it is what was handed over.
	if recoverable.TotalMinor != blocked.TotalMinor {
		t.Error("the amount paid differs by whether the tax can be reclaimed")
	}
}

func TestRecoverabilityIsPerLineNotPerDocument(t *testing.T) {
	// A single expense claim routinely carries both — a meal and a tank of fuel on one card
	// statement. Deciding it per document would force somebody to split one receipt into two,
	// which is how the rule gets ignored.
	fuel := line(t, 1_000).Tax(150, 150_000, "VAT", true)
	meal := line(t, 400).Tax(60, 150_000, "VAT", false)

	net, tax, total, recoverable := domain.Totals([]domain.Line{fuel, meal})
	if net != 1_400 || tax != 210 || total != 1_610 {
		t.Fatalf("net=%d tax=%d total=%d, want 1400/210/1610", net, tax, total)
	}
	// Only the fuel's tax can be reclaimed.
	if recoverable != 150 {
		t.Fatalf("recoverable = %d, want 150", recoverable)
	}
	// And the whole thing still ties: what was paid is the cost plus what can be reclaimed.
	if fuel.CostMinor()+meal.CostMinor()+recoverable != total {
		t.Errorf("cost %d + %d plus recoverable %d does not tie to the total %d",
			fuel.CostMinor(), meal.CostMinor(), recoverable, total)
	}
}

// ── grouping ────────────────────────────────────────────────────────────────────

func TestLinesAgainstOneAccountBecomeOneJournalLine(t *testing.T) {
	// A garage invoice with parts and labour spreads across categories; five lines against one
	// category should not produce five journal lines saying the same thing.
	account := newID(t)
	first := domain.Line{AccountID: account, NetMinor: 1_000, TaxRecoverable: true}
	second := domain.Line{AccountID: account, NetMinor: 500, TaxRecoverable: true}
	other := domain.Line{AccountID: newID(t), NetMinor: 300, TaxRecoverable: true}

	grouped := domain.CostByAccount([]domain.Line{first, second, other})
	if len(grouped) != 2 {
		t.Fatalf("%d accounts, want 2", len(grouped))
	}
	if grouped[account] != 1_500 {
		t.Errorf("the shared account got %d, want 1500", grouped[account])
	}
}

// ── what may still change ───────────────────────────────────────────────────────

func TestAPostedExpenseCannotBeEdited(t *testing.T) {
	expense := domain.Expense{Status: domain.Draft}
	if err := expense.RequireDraft(); err != nil {
		t.Fatalf("a draft refused editing: %v", err)
	}
	expense.Status = domain.Posted
	if code := errs.CodeOf(expense.RequireDraft()); code != domain.CodeAlreadyPosted {
		t.Errorf("code = %q, want %q", code, domain.CodeAlreadyPosted)
	}
}

func TestATemplateCannotBePosted(t *testing.T) {
	// A template is a form to fill in, not a document. Posting one would put a specimen rent
	// payment in the books every time somebody opened the screen.
	template := domain.Expense{Status: domain.Draft, IsTemplate: true}
	if code := errs.CodeOf(template.RequirePostable()); code != domain.CodeTemplateNotPosted {
		t.Fatalf("code = %q, want %q", code, domain.CodeTemplateNotPosted)
	}

	ordinary := domain.Expense{Status: domain.Draft}
	if err := ordinary.RequirePostable(); err != nil {
		t.Fatalf("an ordinary draft refused posting: %v", err)
	}
}

func TestSpendingNothingIsRefused(t *testing.T) {
	// Zero is not an expense, and a NEGATIVE one is a refund — money coming back, which belongs
	// in a document that says so.
	for _, amount := range []int64{0, -100} {
		_, err := domain.NewLine(newID(t), newID(t), newID(t), 1, "Rent", amount)
		if errs.CodeOf(err) != domain.CodeNonPositiveAmount {
			t.Errorf("amount %d was accepted", amount)
		}
	}
}

func TestACategoryNeedsAnAccountToPostTo(t *testing.T) {
	// Without one there is nowhere for the spending to land, and an expense against this
	// category could be entered and then fail at posting — after somebody has typed it.
	_, err := domain.NewCategory(newID(t), newID(t), id.ID(""), "RENT", "Rent")
	if code := errs.CodeOf(err); code != domain.CodeInvalidCategory {
		t.Fatalf("code = %q, want %q", code, domain.CodeInvalidCategory)
	}
}
