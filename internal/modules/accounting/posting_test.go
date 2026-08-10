package accounting_test

import (
	"context"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/accounting"
	"github.com/mizan-erp/mizan/internal/modules/accounting/domain"
	"github.com/mizan-erp/mizan/internal/modules/audit"
)

// ledger is a fixture with a chart applied and the accounts a posting needs.
type ledger struct {
	fixture
	cash id.ID
	ar   id.ID
	sale id.ID
}

func newLedger(t *testing.T) ledger {
	t.Helper()
	f := newFixture(t, accounting.Options{})
	if err := f.svc.ApplyChart(f.ctx, f.companyID, "generic_trading"); err != nil {
		t.Fatalf("ApplyChart: %v", err)
	}
	return ledger{
		fixture: f,
		cash:    accountID(t, f, "1110"),
		ar:      accountID(t, f, "1200"),
		sale:    accountID(t, f, "4100"),
	}
}

func accountID(t *testing.T, f fixture, code string) id.ID {
	t.Helper()
	account, err := f.svc.AccountByCode(f.ctx, f.companyID, code)
	if err != nil {
		t.Fatalf("account %s: %v", code, err)
	}
	return account.ID
}

// A date inside the fiscal year the fixture's wizard generated (2026, starting January).
func inPeriod() time.Time { return time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC) }

// ── the one inviolable rule (§20.2) ─────────────────────────────────────────────

// An unbalanced entry CANNOT BE CONSTRUCTED. Not "is rejected on save" — the aggregate has no
// valid state in which the debits and credits differ, which is the same guarantee money.Money
// gives about currencies.
func TestAnUnbalancedEntryCannotBeConstructed(t *testing.T) {
	identifier, _ := id.New()
	company, _ := id.New()
	period, _ := id.New()
	account, _ := id.New()

	_, err := domain.NewEntry(identifier, company, inPeriod(), period, "manual", []domain.Line{
		{AccountID: account, Debit: 10_000},
		{AccountID: account, Credit: 9_000},
	})
	if err == nil {
		t.Fatal("an entry whose debits and credits differ was constructed")
	}
	if code := errs.CodeOf(err); code != domain.CodeUnbalanced {
		t.Errorf("code = %q, want %q", code, domain.CodeUnbalanced)
	}
	// The difference is in the message because it is the first thing anybody asks, and finding
	// it by hand across forty lines is the tedious part.
	typed, ok := errs.AsError(err)
	if !ok {
		t.Fatalf("the failure is not a typed error: %v", err)
	}
	if typed.Params["difference"] != "1000" {
		t.Errorf("difference = %q, want 1000", typed.Params["difference"])
	}
}

func TestALineIsDebitOrCreditNeverBoth(t *testing.T) {
	identifier, _ := id.New()
	company, _ := id.New()
	period, _ := id.New()
	account, _ := id.New()

	// A line carrying both is not a posting — it is two postings somebody netted off, and
	// netting destroys the trail the ledger exists to keep.
	_, err := domain.NewEntry(identifier, company, inPeriod(), period, "manual", []domain.Line{
		{AccountID: account, Debit: 10_000, Credit: 10_000},
		{AccountID: account, Credit: 10_000, Debit: 10_000},
	})
	if err == nil {
		t.Fatal("a line carrying both a debit and a credit was accepted")
	}
	if code := errs.CodeOf(err); code != domain.CodeInvalidLine {
		t.Errorf("code = %q, want %q", code, domain.CodeInvalidLine)
	}
}

// ── posting ─────────────────────────────────────────────────────────────────────

func TestPostingWritesABalancedEntry(t *testing.T) {
	l := newLedger(t)

	entry, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID:    l.companyID,
		Date:         inPeriod(),
		SourceModule: "manual",
		Memo:         "a cash sale",
		Lines: []domain.Line{
			{AccountID: l.cash, Debit: 120_000},
			{AccountID: l.sale, Credit: 120_000},
		},
	})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}

	if entry.Status != domain.Posted {
		t.Errorf("status = %q, want posted", entry.Status)
	}
	if entry.Number == "" {
		t.Error("the entry was posted with no number")
	}
	if entry.PostedAt.IsZero() {
		t.Error("a posted entry must record when — an entry nobody can place in a sequence")
	}
	if entry.Total() != 120_000 {
		t.Errorf("total = %d, want 120000", entry.Total())
	}

	// Read back: the invariant must survive a round trip, not merely hold in memory.
	stored, err := l.svc.Entry(l.ctx, entry.ID)
	if err != nil {
		t.Fatalf("Entry: %v", err)
	}
	if !stored.IsBalanced() {
		t.Error("the stored entry does not balance")
	}
	if len(stored.Lines) != 2 {
		t.Fatalf("%d lines stored, want 2", len(stored.Lines))
	}
	if stored.Lines[0].Side() != domain.Debit || stored.Lines[1].Side() != domain.Credit {
		t.Error("the sides did not survive the round trip")
	}
}

// Numbers are unique and sequential — not gapless (§9.4, decision 4).
func TestEntryNumbersAreSequential(t *testing.T) {
	l := newLedger(t)

	var numbers []string
	for i := 0; i < 3; i++ {
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
		numbers = append(numbers, entry.Number)
	}

	want := []string{"JE-2026-000001", "JE-2026-000002", "JE-2026-000003"}
	for i, number := range numbers {
		if number != want[i] {
			t.Errorf("number %d = %q, want %q", i, number, want[i])
		}
	}
}

// ONLY LEAF ACCOUNTS ACCEPT POSTINGS. A posting to a roll-up account makes every ancestor
// double-count, and the trial balance still balances.
func TestPostingToARollUpAccountIsRefused(t *testing.T) {
	l := newLedger(t)
	assets := accountID(t, l.fixture, "1000") // has children

	_, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: assets, Debit: 1_000},
			{AccountID: l.sale, Credit: 1_000},
		},
	})
	if err == nil {
		t.Fatal("a heading account accepted a posting")
	}
	if code := errs.CodeOf(err); code != domain.CodeAccountNotPostable {
		t.Errorf("code = %q, want %q", code, domain.CodeAccountNotPostable)
	}
}

// ── fiscal period control (§20.4) ───────────────────────────────────────────────

func TestPostingIntoAClosedPeriodIsRefused(t *testing.T) {
	l := newLedger(t)

	// Post once to establish the period exists and is open.
	first, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: l.cash, Debit: 1_000},
			{AccountID: l.sale, Credit: 1_000},
		},
	})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}

	if err = l.svc.SetPeriodStatus(l.ctx, first.PeriodID, "closed"); err != nil {
		t.Fatalf("SetPeriodStatus: %v", err)
	}

	_, err = l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: l.cash, Debit: 1_000},
			{AccountID: l.sale, Credit: 1_000},
		},
	})
	if err == nil {
		t.Fatal("a closed period accepted a posting; a signed-off month was restated")
	}
	if code := errs.CodeOf(err); code != accounting.CodePeriodClosed {
		t.Errorf("code = %q, want %q", code, accounting.CodePeriodClosed)
	}
}

// A date no fiscal calendar covers is almost always a year typed wrong. Naming that beats
// "posting failed", which sends someone looking at the amounts.
func TestPostingOutsideAnyPeriodIsRefused(t *testing.T) {
	l := newLedger(t)

	_, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID:    l.companyID,
		Date:         time.Date(2019, time.June, 1, 0, 0, 0, 0, time.UTC),
		SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: l.cash, Debit: 1_000},
			{AccountID: l.sale, Credit: 1_000},
		},
	})
	if err == nil {
		t.Fatal("an entry posted outside every fiscal period")
	}
	if code := errs.CodeOf(err); code != accounting.CodeNoPeriod {
		t.Errorf("code = %q, want %q", code, accounting.CodeNoPeriod)
	}
}

// ── immutability and reversal (§20.2) ───────────────────────────────────────────

func TestReversingLeavesTheOriginalExactlyAsPosted(t *testing.T) {
	l := newLedger(t)

	original, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: l.ar, Debit: 75_000},
			{AccountID: l.sale, Credit: 75_000},
		},
	})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}

	reversal, err := l.svc.Reverse(l.ctx, original.ID, inPeriod().AddDate(0, 0, 5), "keyed twice")
	if err != nil {
		t.Fatalf("Reverse: %v", err)
	}

	// Every debit and credit swapped.
	if len(reversal.Lines) != len(original.Lines) {
		t.Fatalf("%d reversal lines, want %d", len(reversal.Lines), len(original.Lines))
	}
	for i, line := range reversal.Lines {
		if line.Debit != original.Lines[i].Credit || line.Credit != original.Lines[i].Debit {
			t.Errorf("line %d was not mirrored", i+1)
		}
	}
	if reversal.ReversalOf != original.ID {
		t.Error("the reversal does not point back at what it reverses")
	}

	// The original is untouched apart from its status: it remains exactly what was posted.
	stored, err := l.svc.Entry(l.ctx, original.ID)
	if err != nil {
		t.Fatalf("Entry: %v", err)
	}
	if stored.Status != domain.Reversed {
		t.Errorf("status = %q, want reversed", stored.Status)
	}
	if stored.Total() != original.Total() {
		t.Error("the reversed entry's amounts changed; it is no longer what was posted")
	}
	if stored.Number != original.Number {
		t.Error("the reversed entry's number changed")
	}

	// Together they net to nothing, which is what a correction means.
	if stored.Total()-reversal.Total() != 0 {
		t.Error("the original and its reversal do not cancel")
	}
}

func TestOnlyAPostedEntryCanBeReversed(t *testing.T) {
	l := newLedger(t)

	original, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: l.cash, Debit: 1_000},
			{AccountID: l.sale, Credit: 1_000},
		},
	})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if _, err = l.svc.Reverse(l.ctx, original.ID, inPeriod(), ""); err != nil {
		t.Fatalf("Reverse: %v", err)
	}

	// A reversal of a reversal is a fresh entry, not a second undo of the same one.
	_, err = l.svc.Reverse(l.ctx, original.ID, inPeriod(), "")
	if err == nil {
		t.Fatal("an already-reversed entry was reversed again")
	}
	if code := errs.CodeOf(err); code != domain.CodeEntryNotPosted {
		t.Errorf("code = %q, want %q", code, domain.CodeEntryNotPosted)
	}
}

// The ledger offers no update and no delete for a posted entry. Asserted on the SURFACE, so
// adding one is a deliberate act somebody has to argue for.
func TestTheLedgerHasNoUpdateOrDelete(t *testing.T) {
	l := newLedger(t)

	var svc any = l.svc
	if _, has := svc.(interface {
		UpdateEntry(context.Context, domain.Entry) error
	}); has {
		t.Error("the accounting service exposes UpdateEntry; a posted entry is never modified (§20.2)")
	}
	if _, has := svc.(interface {
		DeleteEntry(context.Context, id.ID) error
	}); has {
		t.Error("the accounting service exposes DeleteEntry; a posted entry is never deleted (§20.2)")
	}
}

// ── atomicity and audit ─────────────────────────────────────────────────────────

func TestPostingIsAudited(t *testing.T) {
	l := newLedger(t)

	entry, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: l.cash, Debit: 5_000},
			{AccountID: l.sale, Credit: 5_000},
		},
	})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}

	entries, err := l.audit.Entries(l.ctx, audit.Filter{EntityType: accounting.EntityEntry})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d audit entries, want 1", len(entries))
	}
	if entries[0].EntityLabel != entry.Number {
		t.Errorf("label = %q, want the entry number", entries[0].EntityLabel)
	}
}

// A failure after posting leaves NO entry and NO lines — a half-written journal entry would be
// an unbalanced one, which is the failure the whole phase is shaped around.
func TestAFailedTransactionLeavesNoEntry(t *testing.T) {
	l := newLedger(t)

	wanted := errBusinessRuleFailed{}
	err := l.store.Do(l.ctx, func(ctx context.Context) error {
		if _, postErr := l.svc.Post(ctx, accounting.PostInput{
			CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
			Lines: []domain.Line{
				{AccountID: l.cash, Debit: 9_000},
				{AccountID: l.sale, Credit: 9_000},
			},
		}); postErr != nil {
			return postErr
		}
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
		t.Fatalf("%d entries survived a rolled-back transaction", len(entries))
	}
}

// ── the third guard ─────────────────────────────────────────────────────────────

// The integrity check sees entries the other two guards never met — a restore, a repair script,
// a future importer. Proved by writing an unbalanced entry the only way that remains: directly.
func TestTheIntegrityCheckFindsAnUnbalancedEntry(t *testing.T) {
	l := newLedger(t)

	entry, err := l.svc.Post(l.ctx, accounting.PostInput{
		CompanyID: l.companyID, Date: inPeriod(), SourceModule: "manual",
		Lines: []domain.Line{
			{AccountID: l.cash, Debit: 10_000},
			{AccountID: l.sale, Credit: 10_000},
		},
	})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}

	bad, err := l.svc.CheckIntegrity(l.ctx, l.companyID)
	if err != nil {
		t.Fatalf("CheckIntegrity: %v", err)
	}
	if len(bad) != 0 {
		t.Fatalf("a freshly posted ledger reports %v as unbalanced", bad)
	}

	// Corruption of the kind a restore or a hand-edit produces: one side changed, nothing else.
	if _, err = l.store.Writer(l.ctx).ExecContext(l.ctx,
		`UPDATE journal_lines SET debit_minor = 999 WHERE journal_entry_id = ? AND debit_minor > 0`,
		string(entry.ID)); err != nil {
		t.Fatalf("corrupting the ledger: %v", err)
	}

	bad, err = l.svc.CheckIntegrity(l.ctx, l.companyID)
	if err != nil {
		t.Fatalf("CheckIntegrity: %v", err)
	}
	if len(bad) != 1 || bad[0] != entry.Number {
		t.Fatalf("integrity check reported %v, want [%s]", bad, entry.Number)
	}
}
