package expenses_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/audit"
	"github.com/mizan-erp/mizan/internal/modules/expenses"
	"github.com/mizan-erp/mizan/internal/modules/expenses/domain"
)

func (f fixture) draft(
	t *testing.T, settlement domain.Settlement, method domain.Method,
) id.ID {
	t.Helper()
	expense, err := f.svc.Draft(f.ctx, expenses.NewExpenseInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		PayeeName: "The landlord", ExpenseDate: "2026-09-01", Currency: "SAR",
		Settlement: settlement, PaidMethod: method,
	})
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}
	return expense.ID
}

func (f fixture) addLine(t *testing.T, expenseID id.ID, code string, netMinor int64) {
	t.Helper()
	if _, err := f.svc.AddLine(f.ctx, expenses.AddLineInput{
		ExpenseID: expenseID, CategoryID: f.category(t, code), NetMinor: netMinor,
	}); err != nil {
		t.Fatalf("AddLine(%s): %v", code, err)
	}
}

// ── an expense lands where its CATEGORY says ────────────────────────────────────

func TestAnExpenseLandsInTheAccountItsCategoryNames(t *testing.T) {
	// Criterion 1. A business has forty categories, each with its own account, and no posting
	// rule can name them — so the DOCUMENT carries the debits and the rule decides only where the
	// money came from.
	f := newFixture(t)

	expenseID := f.draft(t, domain.Immediate, domain.Cash)
	f.addLine(t, expenseID, "RENT", 100_000) // 1,000.00 of rent

	posted, err := f.svc.Record(f.ctx, expenseID)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if posted.Number == "" {
		t.Fatal("a posted expense took no number")
	}

	balances := f.balances(t)
	// Rent is account 5200 and the tax is recoverable, so 1,000.00 lands there.
	if balances["5200"] != 100_000 {
		t.Errorf("rent = %d, want 100000", balances["5200"])
	}
	if balances["1400"] != 15_000 {
		t.Errorf("recoverable tax = %d, want 15000", balances["1400"])
	}
	// Paid in cash: the till is credited the whole amount handed over.
	if balances["1110"] != -115_000 {
		t.Errorf("cash = %d, want -115000", balances["1110"])
	}
	// And nothing is owed.
	if balances["2100"] != 0 {
		t.Errorf("payable = %d, want 0 — an expense paid now owes nothing", balances["2100"])
	}
}

func TestOneExpenseCanSpreadAcrossSeveralAccounts(t *testing.T) {
	// A garage invoice with parts and labour. Each category lands in its own account, and the
	// document says which — which is the whole reason the debit side is document-named.
	f := newFixture(t)

	expenseID := f.draft(t, domain.Immediate, domain.Cash)
	f.addLine(t, expenseID, "MAINTENANCE", 60_000)
	f.addLine(t, expenseID, "TRANSPORT", 40_000)

	if _, err := f.svc.Record(f.ctx, expenseID); err != nil {
		t.Fatalf("Record: %v", err)
	}

	balances := f.balances(t)
	if balances["5240"] != 60_000 {
		t.Errorf("maintenance = %d, want 60000", balances["5240"])
	}
	if balances["5230"] != 40_000 {
		t.Errorf("transport = %d, want 40000", balances["5230"])
	}
}

// ── paid now versus owed ────────────────────────────────────────────────────────

func TestTheSettlementDecidesWhetherAnythingIsOwed(t *testing.T) {
	// Criterion 2. Fuel bought with cash is spent and gone; rent invoiced monthly is owed until
	// paid. The same business fact at different moments, and one document type.
	for _, test := range []struct {
		name       string
		settlement domain.Settlement
		method     domain.Method
		account    string
		other      string
	}{
		{"cash leaves the till", domain.Immediate, domain.Cash, "1110", "2100"},
		{"a transfer leaves the bank", domain.Immediate, domain.BankTransfer, "1120", "2100"},
		{"on account owes", domain.OnAccount, "", "2100", "1110"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			expenseID := f.draft(t, test.settlement, test.method)
			f.addLine(t, expenseID, "RENT", 100_000)
			if _, err := f.svc.Record(f.ctx, expenseID); err != nil {
				t.Fatalf("Record: %v", err)
			}

			balances := f.balances(t)
			if balances[test.account] != -115_000 {
				t.Errorf("%s = %d, want -115000", test.account, balances[test.account])
			}
			if balances[test.other] != 0 {
				t.Errorf("%s = %d, want 0 — this settlement must not touch it",
					test.other, balances[test.other])
			}
		})
	}
}

// ── recoverable tax ─────────────────────────────────────────────────────────────

func TestNonRecoverableTaxLandsInTheExpenseNotTheTaxAsset(t *testing.T) {
	// Entertainment is seeded as non-recoverable. The tax paid is still money out — so it is part
	// of what the thing cost, and a business that claimed it would be making a claim a revenue
	// authority disallows.
	f := newFixture(t)

	expenseID := f.draft(t, domain.Immediate, domain.Cash)
	f.addLine(t, expenseID, "ENTERTAINMENT", 100_000)

	if _, err := f.svc.Record(f.ctx, expenseID); err != nil {
		t.Fatalf("Record: %v", err)
	}

	balances := f.balances(t)
	// The whole 1,150.00 is the cost, not 1,000.00 plus a claim.
	if balances["5280"] != 115_000 {
		t.Errorf("entertainment = %d, want 115000 — the blocked tax is part of the cost",
			balances["5280"])
	}
	if balances["1400"] != 0 {
		t.Errorf("recoverable tax = %d, want 0", balances["1400"])
	}
	// And the cash out is the same either way: it is what was handed over.
	if balances["1110"] != -115_000 {
		t.Errorf("cash = %d, want -115000", balances["1110"])
	}
}

func TestOneExpenseCanMixRecoverableAndBlockedTax(t *testing.T) {
	// A meal and a tank of fuel on one card statement. Deciding recoverability per DOCUMENT
	// would force somebody to split one receipt into two, which is how the rule gets ignored.
	f := newFixture(t)

	expenseID := f.draft(t, domain.Immediate, domain.Card)
	f.addLine(t, expenseID, "TRANSPORT", 100_000)    // recoverable
	f.addLine(t, expenseID, "ENTERTAINMENT", 40_000) // not

	if _, err := f.svc.Record(f.ctx, expenseID); err != nil {
		t.Fatalf("Record: %v", err)
	}

	balances := f.balances(t)
	if balances["5230"] != 100_000 {
		t.Errorf("transport = %d, want 100000", balances["5230"])
	}
	if balances["5280"] != 46_000 {
		t.Errorf("entertainment = %d, want 46000 (40000 plus its blocked tax)", balances["5280"])
	}
	// Only the fuel's tax is claimed.
	if balances["1400"] != 15_000 {
		t.Errorf("recoverable tax = %d, want 15000", balances["1400"])
	}
}

// ── §20.3 ───────────────────────────────────────────────────────────────────────

func TestExpensesNamesNoAccountItCouldHaveMapped(t *testing.T) {
	// Criterion 7. The DEBIT side is document-named because no rule could enumerate forty
	// categories — that is the narrow exception. The CREDIT side, which is what differs by
	// payment method, still goes through a rule and must follow a rewrite.
	f := newFixture(t)

	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
		`UPDATE posting_rule_lines SET account_selector = 'mapping:ROUNDING_DIFF'
		  WHERE account_selector = 'mapping:CASH'`); err != nil {
		t.Fatalf("rewriting the rule: %v", err)
	}

	expenseID := f.draft(t, domain.Immediate, domain.Cash)
	f.addLine(t, expenseID, "RENT", 100_000)
	if _, err := f.svc.Record(f.ctx, expenseID); err != nil {
		t.Fatalf("Record: %v", err)
	}

	balances := f.balances(t)
	if balances["1110"] != 0 {
		t.Errorf("cash moved by %d — the module named it rather than going through a rule",
			balances["1110"])
	}
	if balances["5900"] != -115_000 {
		t.Errorf("the redirected account = %d, want -115000", balances["5900"])
	}
}

// ── categories are data ─────────────────────────────────────────────────────────

func TestCategoriesAreSeededAndSeedingTwiceAddsNothing(t *testing.T) {
	// Criterion 3. What a business must report separately is a tax question that differs by
	// jurisdiction, and adding a category must not need a release. Idempotent by code, so a
	// re-run after a partial failure is safe.
	f := newFixture(t)

	first, err := f.svc.Categories(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	if len(first) < 5 {
		t.Fatalf("%d categories seeded, expected the shipped set", len(first))
	}

	if err = f.svc.ApplyCategories(f.ctx, f.companyID, "generic_trading"); err != nil {
		t.Fatalf("re-applying: %v", err)
	}
	second, err := f.svc.Categories(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	if len(second) != len(first) {
		t.Errorf("re-seeding added %d categories", len(second)-len(first))
	}
}

// ── the snapshot ────────────────────────────────────────────────────────────────

func TestALineKeepsTheAccountItsCategoryPointedAtWhenItWasEntered(t *testing.T) {
	// §9.3. A category re-pointed at a different account next year must not move where a posted
	// expense landed — the journal entry would say one thing and the document another.
	f := newFixture(t)

	expenseID := f.draft(t, domain.Immediate, domain.Cash)
	f.addLine(t, expenseID, "RENT", 100_000)
	if _, err := f.svc.Record(f.ctx, expenseID); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// The category is re-pointed at "other expenses".
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx, `
		UPDATE expense_categories
		   SET account_id = (SELECT id FROM accounts WHERE company_id = ? AND code = '5290')
		 WHERE company_id = ? AND code = 'RENT'`,
		string(f.companyID), string(f.companyID)); err != nil {
		t.Fatalf("re-pointing the category: %v", err)
	}

	_, lines, err := f.svc.Expense(f.ctx, expenseID)
	if err != nil {
		t.Fatalf("Expense: %v", err)
	}
	balances := f.balances(t)
	if balances["5200"] != 100_000 {
		t.Errorf("rent = %d — the posted expense followed the category's new account",
			balances["5200"])
	}
	if lines[0].CategoryName != "Rent" {
		t.Errorf("the line says %q, want the name as at entry", lines[0].CategoryName)
	}
}

// ── numbering and templates ─────────────────────────────────────────────────────

func TestAnAbandonedExpenseConsumesNoNumber(t *testing.T) {
	// Criterion 8, the rule every document in this application keeps.
	f := newFixture(t)

	abandoned := f.draft(t, domain.Immediate, domain.Cash)
	f.addLine(t, abandoned, "RENT", 10_000)
	if err := f.svc.Cancel(f.ctx, abandoned); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	expenseID := f.draft(t, domain.Immediate, domain.Cash)
	f.addLine(t, expenseID, "RENT", 10_000)
	posted, err := f.svc.Record(f.ctx, expenseID)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if posted.Number != "EXP-000001" {
		t.Errorf("number = %q, want EXP-000001", posted.Number)
	}
}

func TestATemplateCannotBeRecorded(t *testing.T) {
	// A template pre-fills a form somebody CONFIRMS. An expense that appears in the books
	// without a person deciding it did is an expense nobody checked, and a standing order posted
	// after the lease ended is silent and compounding.
	f := newFixture(t)

	template, err := f.svc.Draft(f.ctx, expenses.NewExpenseInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		PayeeName: "The landlord", ExpenseDate: "2026-09-01", Currency: "SAR",
		Settlement: domain.OnAccount, IsTemplate: true, RecursEveryMonths: 1,
	})
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}
	f.addLine(t, template.ID, "RENT", 100_000)

	_, err = f.svc.Record(f.ctx, template.ID)
	if code := errs.CodeOf(err); code != domain.CodeTemplateNotPosted {
		t.Fatalf("code = %q, want %q", code, domain.CodeTemplateNotPosted)
	}

	// And it does not appear among ordinary expenses, or every list would show a specimen.
	ordinary, err := f.svc.Expenses(f.ctx, f.companyID, "")
	if err != nil {
		t.Fatalf("Expenses: %v", err)
	}
	for _, expense := range ordinary {
		if expense.ID == template.ID {
			t.Error("a template appeared in the expense list")
		}
	}
	templates, err := f.svc.Templates(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Templates: %v", err)
	}
	if len(templates) != 1 {
		t.Errorf("%d templates, want 1", len(templates))
	}
}

// ── the audit trail ─────────────────────────────────────────────────────────────

func TestEveryActOnAnExpenseLeavesAnAuditEntry(t *testing.T) {
	// Criterion 9, written from the START rather than discovered by a review — which is what
	// Phase 5 and Phase 6 both had to do.
	f := newFixture(t)

	expenseID := f.draft(t, domain.Immediate, domain.Cash)
	f.addLine(t, expenseID, "RENT", 100_000)
	if _, err := f.svc.Record(f.ctx, expenseID); err != nil {
		t.Fatalf("Record: %v", err)
	}

	cancelled := f.draft(t, domain.Immediate, domain.Cash)
	if err := f.svc.Cancel(f.ctx, cancelled); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	entries, err := f.audit.Entries(f.ctx, audit.Filter{})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	seen := make(map[string]int, len(entries))
	for _, entry := range entries {
		seen[entry.Action]++
	}

	for _, action := range []string{
		expenses.ActionCategoriesSeeded, expenses.ActionExpenseDrafted,
		expenses.ActionExpenseLineAdded, expenses.ActionExpenseRecorded,
		expenses.ActionExpenseCancelled,
	} {
		if seen[action] == 0 {
			t.Errorf("%q happened and left no audit entry", action)
		}
	}

	// And no POSTING-RULE key reached the audit trail. Declared apart AND given different
	// strings, which is the Phase 6 review's finding applied from the start.
	for _, postingKey := range []string{
		"expenses.expense.paid.cash", "expenses.expense.on_account",
	} {
		if seen[postingKey] != 0 {
			t.Errorf("posting-rule key %q reached the audit trail", postingKey)
		}
	}
}

// TestADocumentNamedEntryHasStableLineOrder
//
// # Why order is worth pinning
//
// The debits come from a MAP, and Go randomises map iteration deliberately. Without a sort, one
// expense would produce its journal lines in a different order on every run — which means a
// golden test cannot pin the entry, a reader comparing two printouts of the same document sees
// them differ, and any future export is unstable for no reason.
//
// Nothing else in this application posts from a map, so this is the only place the problem
// arises and the only place a test can catch it.
func TestADocumentNamedEntryHasStableLineOrder(t *testing.T) {
	f := newFixture(t)

	expenseID := f.draft(t, domain.Immediate, domain.Cash)
	// Three categories whose accounts are deliberately added out of order, so a stable result
	// cannot come from the insertion sequence.
	f.addLine(t, expenseID, "MARKETING", 30_000)   // 5260
	f.addLine(t, expenseID, "RENT", 10_000)        // 5200
	f.addLine(t, expenseID, "MAINTENANCE", 20_000) // 5240

	posted, err := f.svc.Record(f.ctx, expenseID)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	rows, err := f.store.Reader(f.ctx).QueryContext(f.ctx, `
		SELECT l.account_id
		  FROM journal_lines l
		  JOIN journal_entries e ON e.id = l.journal_entry_id
		 WHERE e.source_document_id = ? AND l.debit_minor > 0
		 ORDER BY l.line_number`, string(posted.ID))
	if err != nil {
		t.Fatalf("reading journal lines: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var accounts []string
	for rows.Next() {
		var accountID string
		if err = rows.Scan(&accountID); err != nil {
			t.Fatalf("scan: %v", err)
		}
		accounts = append(accounts, accountID)
	}
	if err = rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	// Three expense debits plus the recoverable tax.
	if len(accounts) < 3 {
		t.Fatalf("%d debit lines, want at least 3", len(accounts))
	}

	// The document-named lines come first and are in ascending account order. Sorted by
	// IDENTIFIER rather than by code, because that is what the entry sorts on and what a reader
	// comparing two runs would be relying on.
	documentLines := accounts[:3]
	for i := 1; i < len(documentLines); i++ {
		if documentLines[i-1] >= documentLines[i] {
			t.Fatalf("the document-named lines are not in a stable order: %v", documentLines)
		}
	}
}
