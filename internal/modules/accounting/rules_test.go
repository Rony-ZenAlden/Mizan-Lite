package accounting_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/accounting"
	"github.com/mizan-erp/mizan/internal/modules/accounting/contract"
	"github.com/mizan-erp/mizan/internal/modules/accounting/domain"
	"github.com/mizan-erp/mizan/internal/modules/accounting/infra/sqlite"
)

// posted is a ledger with the chart AND the posting rules applied — what a real installation
// has after the setup wizard.
func newPosted(t *testing.T) ledger {
	t.Helper()
	l := newLedger(t)
	if err := l.svc.ApplyRules(l.ctx, l.companyID, "generic_trading"); err != nil {
		t.Fatalf("ApplyRules: %v", err)
	}
	return l
}

// closingOf returns the trial balance keyed by account code.
func (l ledger) closingOf(t *testing.T, periodID id.ID) map[string]sqlite.Balance {
	t.Helper()
	balances, err := l.svc.TrialBalance(l.ctx, l.companyID, periodID)
	if err != nil {
		t.Fatalf("TrialBalance: %v", err)
	}
	out := map[string]sqlite.Balance{}
	for _, b := range balances {
		out[b.AccountCode] = b
	}
	return out
}

// ── the point of the phase (§20.3) ──────────────────────────────────────────────

// A sale posts its books from RULE ROWS, with no accounting code anywhere in the caller.
//
// The event says what happened and how much. Which accounts move, and in which direction, comes
// entirely from data — which is what lets a different country or trade keep different books
// from a different seed file rather than a different build.
func TestASaleIsPostedEntirelyFromRules(t *testing.T) {
	l := newPosted(t)

	entries, err := l.svc.Record(l.ctx, contract.Postable{
		Action:         "sales.invoice.posted",
		CompanyID:      l.companyID,
		Date:           inPeriod(),
		DocumentType:   "sales.invoice",
		DocumentNumber: "INV-000042",
		Amounts: map[string]int64{
			contract.AmountTotal: 115_000,
			contract.AmountNet:   100_000,
			contract.AmountTax:   15_000,
			contract.AmountCost:  60_000,
		},
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	// Two rules matched: revenue and cost. Two entries, each balanced.
	if len(entries) != 2 {
		t.Fatalf("%d entries posted, want 2 (revenue and cost)", len(entries))
	}
	for _, entry := range entries {
		if !entry.IsBalanced() {
			t.Errorf("entry %s does not balance", entry.Number)
		}
		if entry.SourceModule != "sales" {
			t.Errorf("source module = %q, want the publishing module", entry.SourceModule)
		}
		if entry.SourceDocumentType != "sales.invoice" {
			t.Errorf("source document type = %q", entry.SourceDocumentType)
		}
	}

	closing := l.closingOf(t, entries[0].PeriodID)

	// The customer owes the gross; revenue and tax are split; stock became cost.
	if got := closing["1200"].Closing; got != 115_000 {
		t.Errorf("receivables = %d, want 115000 (the gross)", got)
	}
	if got := closing["4100"].Closing; got != -100_000 {
		t.Errorf("sales = %d, want -100000 (the net, credit-normal)", got)
	}
	if got := closing["2200"].Closing; got != -15_000 {
		t.Errorf("tax payable = %d, want -15000", got)
	}
	if got := closing["5100"].Closing; got != 60_000 {
		t.Errorf("cost of goods sold = %d, want 60000", got)
	}
	if got := closing["1300"].Closing; got != -60_000 {
		t.Errorf("inventory = %d, want -60000 (stock released)", got)
	}

	// And the whole thing still balances.
	var total int64
	for _, b := range closing {
		total += b.Closing
	}
	if total != 0 {
		t.Errorf("the books sum to %d after a sale, want 0", total)
	}
}

// Changing a rule ROW changes the books, with no code change. This is §20.3's promise, tested
// rather than asserted: a mis-mapped account is a data fix, not a patch release.
func TestChangingARuleRowChangesTheBooks(t *testing.T) {
	l := newPosted(t)

	// An accountant decides sales should post to bank rather than to receivables — the sort of
	// change that would otherwise be a code edit and a release.
	if _, err := l.store.Writer(l.ctx).ExecContext(l.ctx, `
		UPDATE posting_rule_lines SET account_selector = 'mapping:BANK'
		 WHERE account_selector = 'mapping:AR'
		   AND posting_rule_id IN (SELECT id FROM posting_rules WHERE code = 'sale_revenue')`,
	); err != nil {
		t.Fatalf("editing the rule: %v", err)
	}

	entries, err := l.svc.Record(l.ctx, contract.Postable{
		Action:    "sales.invoice.posted",
		CompanyID: l.companyID,
		Date:      inPeriod(),
		Amounts: map[string]int64{
			contract.AmountTotal: 50_000,
			contract.AmountNet:   50_000,
		},
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	closing := l.closingOf(t, entries[0].PeriodID)
	if got := closing["1120"].Closing; got != 50_000 {
		t.Errorf("bank = %d, want 50000 — the edited rule did not take effect", got)
	}
	if got := closing["1200"].Closing; got != 0 {
		t.Errorf("receivables = %d, want 0 — the old rule is still posting", got)
	}
}

// ── zero amounts, and why condition_expr is unimplemented ───────────────────────

// A shop that does not track stock publishes no cost, and the cost rule posts nothing. No
// expression language, no condition — the amount is simply absent.
func TestARuleWhoseAmountsAreAbsentPostsNothing(t *testing.T) {
	l := newPosted(t)

	entries, err := l.svc.Record(l.ctx, contract.Postable{
		Action:    "sales.invoice.posted",
		CompanyID: l.companyID,
		Date:      inPeriod(),
		Amounts: map[string]int64{
			contract.AmountTotal: 30_000,
			contract.AmountNet:   30_000,
			// no tax, no cost
		},
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("%d entries, want only the revenue rule to have posted", len(entries))
	}
	if len(entries[0].Lines) != 2 {
		t.Errorf("%d lines, want the tax line to have been skipped", len(entries[0].Lines))
	}

	closing := l.closingOf(t, entries[0].PeriodID)
	if got := closing["2200"].Closing; got != 0 {
		t.Errorf("tax payable = %d, want 0 on a sale with no tax", got)
	}
	if got := closing["5100"].Closing; got != 0 {
		t.Errorf("cost of goods sold = %d, want 0 for a shop that tracks no stock", got)
	}
}

// A negative amount would flip the side silently. A credit note is a DIFFERENT event with its
// own rule, not a sale with a minus sign.
func TestANegativeAmountIsRefused(t *testing.T) {
	l := newPosted(t)

	_, err := l.svc.Record(l.ctx, contract.Postable{
		Action:    "sales.invoice.posted",
		CompanyID: l.companyID,
		Date:      inPeriod(),
		Amounts:   map[string]int64{contract.AmountTotal: -10_000, contract.AmountNet: -10_000},
	})
	if err == nil {
		t.Fatal("a negative amount was posted; the side would have flipped silently")
	}
	if code := errs.CodeOf(err); code != domain.CodeInvalidLine {
		t.Errorf("code = %q, want %q", code, domain.CodeInvalidLine)
	}
}

// An event nobody wrote a rule for posts nothing and does NOT fail — refusing would stop the
// sale rather than the bookkeeping.
func TestAnEventWithNoRulePostsNothing(t *testing.T) {
	l := newPosted(t)

	entries, err := l.svc.Record(l.ctx, contract.Postable{
		Action:    "inventory.adjustment.posted",
		CompanyID: l.companyID,
		Date:      inPeriod(),
		Amounts:   map[string]int64{contract.AmountTotal: 5_000},
	})
	if err != nil {
		t.Fatalf("an event with no rule failed rather than posting nothing: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("%d entries posted for an event with no rule", len(entries))
	}
}

// ── the subscriber, and its atomicity ───────────────────────────────────────────

// The whole path: a module publishes on the bus, accounting records it, and both commit
// together — or neither does.
func TestPublishingOnTheBusPostsTheBooks(t *testing.T) {
	l := newPosted(t)

	err := l.store.Do(l.ctx, func(ctx context.Context) error {
		return l.bus.Publish(ctx, contract.Postable{
			Action:    "sales.invoice.posted",
			CompanyID: l.companyID,
			Date:      inPeriod(),
			Amounts:   map[string]int64{contract.AmountTotal: 20_000, contract.AmountNet: 20_000},
		})
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	entries, err := l.svc.Entries(l.ctx, l.companyID, 0)
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d entries after publishing one sale, want 1", len(entries))
	}
}

// A sale that fails AFTER publishing leaves no journal entry. The document and its books commit
// together or not at all — and unlike a missing audit row, a missing entry makes the books
// WRONG rather than merely incomplete.
func TestAFailedSaleLeavesNoJournalEntry(t *testing.T) {
	l := newPosted(t)

	wanted := errBusinessRuleFailed{}
	err := l.store.Do(l.ctx, func(ctx context.Context) error {
		if publishErr := l.bus.Publish(ctx, contract.Postable{
			Action:    "sales.invoice.posted",
			CompanyID: l.companyID,
			Date:      inPeriod(),
			Amounts:   map[string]int64{contract.AmountTotal: 99_000, contract.AmountNet: 99_000},
		}); publishErr != nil {
			return publishErr
		}
		// Something later in the sale fails — a stock check, a period lock, a printer.
		return wanted
	})
	if err != wanted {
		t.Fatalf("Do = %v, want the business failure to propagate", err)
	}

	entries, err := l.svc.Entries(context.Background(), l.companyID, 0)
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("%d journal entries survived a rolled-back sale", len(entries))
	}
}

// ── the rule set itself ─────────────────────────────────────────────────────────

func TestEveryShippedRuleSetLoads(t *testing.T) {
	f := newFixture(t, accounting.Options{})

	sets := f.svc.RuleSets()
	if len(sets) == 0 {
		t.Fatal("no posting rule sets shipped")
	}
	if _, ok := f.svc.RuleSet("generic_trading"); !ok {
		t.Error("generic_trading is missing — the setup wizard applies it by name")
	}
	if len(f.svc.Problems()) != 0 {
		t.Errorf("shipped files reported problems: %+v", f.svc.Problems())
	}
}

// Every account role a shipped rule names must be a role the shipped chart supplies. Otherwise
// the pairing fails at the first sale, months after anyone looked at either file.
func TestShippedRulesOnlyNameRolesTheChartSupplies(t *testing.T) {
	f := newFixture(t, accounting.Options{})

	chart, ok := f.svc.Chart("generic_trading")
	if !ok {
		t.Fatal("the generic_trading chart is missing")
	}
	set, ok := f.svc.RuleSet("generic_trading")
	if !ok {
		t.Fatal("the generic_trading rules are missing")
	}

	for _, rule := range set.Rules {
		for _, line := range rule.Lines {
			key, found := strings.CutPrefix(line.Account, "mapping:")
			if !found {
				continue
			}
			if _, mapped := chart.Mappings[key]; !mapped {
				t.Errorf("rule %q needs the %q role, which the chart does not map", rule.Code, key)
			}
		}
	}
}

// A rule with no credit cannot produce a balanced entry from any input, which makes it dead
// data discoverable only by a failed sale.
func TestARuleWithOnlyOneSideIsRefused(t *testing.T) {
	f := newFixture(t, accounting.Options{UserFS: overlay(map[string]string{
		"seeds/posting_rules/lopsided.json": `{
			"code": "lopsided", "name": "Lopsided", "description": "",
			"rules": [{
				"code": "bad", "event": "sales.invoice.posted", "sequence": 1, "description": "",
				"lines": [
					{ "side": "debit", "account": "mapping:AR",    "amount": "document.total", "memo": "" },
					{ "side": "debit", "account": "mapping:SALES", "amount": "document.net",   "memo": "" }
				]
			}]
		}`,
	})})

	if _, ok := f.svc.RuleSet("lopsided"); ok {
		t.Error("a rule with two debits and no credit was accepted")
	}
	if len(f.svc.Problems()) != 1 {
		t.Fatalf("problems = %+v, want the lopsided rule reported", f.svc.Problems())
	}
}

func TestABadAccountSelectorIsRefused(t *testing.T) {
	f := newFixture(t, accounting.Options{UserFS: overlay(map[string]string{
		"seeds/posting_rules/typo.json": `{
			"code": "typo", "name": "Typo", "description": "",
			"rules": [{
				"code": "bad", "event": "sales.invoice.posted", "sequence": 1, "description": "",
				"lines": [
					{ "side": "debit",  "account": "1200",            "amount": "document.total", "memo": "" },
					{ "side": "credit", "account": "mapping:SALES",   "amount": "document.net",   "memo": "" }
				]
			}]
		}`,
	})})

	// `1200` with no prefix is neither form. Caught at load rather than resolving to nothing and
	// posting a half-entry that surfaces in a trial balance weeks later.
	if _, ok := f.svc.RuleSet("typo"); ok {
		t.Error("an account selector with no `mapping:` or `account:` prefix was accepted")
	}
}

func TestRulesAreAppliedOnlyOnce(t *testing.T) {
	l := newPosted(t)

	err := l.svc.ApplyRules(l.ctx, l.companyID, "generic_trading")
	if err == nil {
		t.Fatal("a second rule set was applied over the first")
	}
	if code := errs.CodeOf(err); code != accounting.CodeRulesExist {
		t.Errorf("code = %q, want %q", code, accounting.CodeRulesExist)
	}
}
