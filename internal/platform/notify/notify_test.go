package notify_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/notify"
)

type stubRule struct {
	name    string
	notices []notify.Notice
	err     error
	calls   int
}

func (r *stubRule) Name() string { return r.name }

func (r *stubRule) Check(context.Context, id.ID) ([]notify.Notice, error) {
	r.calls++
	return r.notices, r.err
}

type memoryDismissals struct {
	keys map[string]bool
	err  error
}

func (m *memoryDismissals) Dismissed(
	context.Context, id.ID, id.ID,
) (map[string]bool, error) {
	return m.keys, m.err
}

func (m *memoryDismissals) Dismiss(_ context.Context, _, _ id.ID, key string) error {
	m.keys[key] = true
	return nil
}

func (m *memoryDismissals) Restore(_ context.Context, _, _ id.ID, key string) error {
	delete(m.keys, key)
	return nil
}

func emptyDismissals() *memoryDismissals {
	return &memoryDismissals{keys: map[string]bool{}}
}

// TestANoticeDisappearsWhenItsConditionDoes
//
// # DoD criterion 10, and the reason nothing is stored
//
// A stored notification goes STALE. "Invoice 4471 is overdue" survives the invoice being paid,
// and a user told three times about something they already fixed stops reading notifications at
// all — which costs more than never having had them.
//
// Here the rule simply stops producing the notice, and it is gone. There is nothing to clean up,
// because there was never a row.
func TestANoticeDisappearsWhenItsConditionDoes(t *testing.T) {
	rule := &stubRule{name: "sales.overdue", notices: []notify.Notice{
		{Key: "4471", Severity: notify.Warning, MessageKey: "notices.overdue"},
	}}
	centre := notify.New(emptyDismissals(), rule)

	result, err := centre.Check(context.Background(), id.ID("c"), id.ID("u"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(result.Notices) != 1 {
		t.Fatalf("%d notices, want 1", len(result.Notices))
	}

	// The invoice is paid.
	rule.notices = nil

	result, err = centre.Check(context.Background(), id.ID("c"), id.ID("u"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(result.Notices) != 0 {
		t.Errorf("%d notices after the condition resolved: %+v",
			len(result.Notices), result.Notices)
	}
}

// TestAKeyIsPrefixedByItsRuleSoTwoRulesCannotCollide
//
// A dismissal is stored against the key. Two rules both producing "overdue" would mean dismissing
// one silences the other — and the user would have no way to tell which, or to get it back.
func TestAKeyIsPrefixedByItsRuleSoTwoRulesCannotCollide(t *testing.T) {
	sales := &stubRule{name: "sales.overdue", notices: []notify.Notice{{Key: "4471"}}}
	bills := &stubRule{name: "purchasing.overdue", notices: []notify.Notice{{Key: "4471"}}}

	result, err := notify.New(emptyDismissals(), sales, bills).
		Check(context.Background(), id.ID("c"), id.ID("u"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(result.Notices) != 2 {
		t.Fatalf("%d notices, want 2", len(result.Notices))
	}
	if result.Notices[0].Key == result.Notices[1].Key {
		t.Errorf("two rules produced the same key %q — dismissing one would silence the other",
			result.Notices[0].Key)
	}
	for _, notice := range result.Notices {
		if notice.Rule == "" {
			t.Error("a notice does not say which rule produced it")
		}
	}
}

// TestADismissedNoticeIsSuppressedAndCounted
//
// Counted, because a user who dismissed something in error otherwise has no way back: a screen
// that cannot say "3 hidden" cannot offer to show them.
func TestADismissedNoticeIsSuppressedAndCounted(t *testing.T) {
	rule := &stubRule{name: "ops.backup", notices: []notify.Notice{
		{Key: "stale", Severity: notify.Warning},
	}}
	store := emptyDismissals()
	centre := notify.New(store, rule)

	if err := centre.Dismiss(
		context.Background(), id.ID("c"), id.ID("u"), "ops.backup.stale"); err != nil {
		t.Fatalf("Dismiss: %v", err)
	}

	result, err := centre.Check(context.Background(), id.ID("c"), id.ID("u"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(result.Notices) != 0 {
		t.Errorf("a dismissed notice was shown: %+v", result.Notices)
	}
	if result.Dismissed != 1 {
		t.Errorf("dismissed = %d, want 1 — a screen cannot offer to show what it cannot count",
			result.Dismissed)
	}

	// Restoring brings it back, BECAUSE the condition still holds. Restoring is not a promise
	// that something returns; it is only forgetting that it was silenced.
	if err = centre.Restore(
		context.Background(), id.ID("c"), id.ID("u"), "ops.backup.stale"); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	result, err = centre.Check(context.Background(), id.ID("c"), id.ID("u"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(result.Notices) != 1 {
		t.Errorf("restoring did not bring the notice back: %+v", result)
	}
}

// TestOneBrokenRuleDoesNotSilenceTheRest
//
// A notification centre that goes quiet because one rule broke is worse than one that says so:
// **silence reads as "nothing is wrong".** The same choice `search.Response.Failed` makes.
func TestOneBrokenRuleDoesNotSilenceTheRest(t *testing.T) {
	working := &stubRule{name: "ops.backup", notices: []notify.Notice{
		{Key: "never", Severity: notify.Warning},
	}}
	broken := &stubRule{name: "accounting.ledger", err: errors.New("the table is gone")}

	result, err := notify.New(emptyDismissals(), broken, working).
		Check(context.Background(), id.ID("c"), id.ID("u"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(result.Notices) != 1 {
		t.Errorf("%d notices, want the one the working rule produced", len(result.Notices))
	}
	if len(result.Failed) != 1 || result.Failed[0] != "accounting.ledger" {
		t.Errorf("failed = %v, want [accounting.ledger] — silence reads as nothing is wrong",
			result.Failed)
	}
}

// TestAnUnreadableDismissalStoreFailsRatherThanNagging
//
// The opposite call from a broken RULE, and deliberately. A dismissal store that cannot be read
// means every notice reappears, including ones the user explicitly silenced — and nagging
// somebody who already said no is how a feature gets turned off entirely.
func TestAnUnreadableDismissalStoreFailsRatherThanNagging(t *testing.T) {
	rule := &stubRule{name: "ops.backup", notices: []notify.Notice{{Key: "stale"}}}
	store := &memoryDismissals{err: errors.New("the table is gone")}

	_, err := notify.New(store, rule).Check(context.Background(), id.ID("c"), id.ID("u"))
	if err == nil {
		t.Error("an unreadable dismissal store produced notices anyway, including ones the " +
			"user had silenced")
	}
	if rule.calls != 0 {
		t.Error("the rules ran before the dismissals were readable")
	}
}

// TestNoticesComeBackMostSeriousFirst
//
// And stably: two runs of the same check must agree, or a screen reorders itself while somebody
// is reading it.
func TestNoticesComeBackMostSeriousFirst(t *testing.T) {
	rule := &stubRule{name: "mixed", notices: []notify.Notice{
		{Key: "c", Severity: notify.Info},
		{Key: "b", Severity: notify.Danger},
		{Key: "a", Severity: notify.Warning},
		{Key: "d", Severity: notify.Danger},
	}}

	result, err := notify.New(emptyDismissals(), rule).
		Check(context.Background(), id.ID("c"), id.ID("u"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	want := []string{"mixed.b", "mixed.d", "mixed.a", "mixed.c"}
	for i, key := range want {
		if result.Notices[i].Key != key {
			t.Errorf("position %d is %q, want %q", i, result.Notices[i].Key, key)
		}
	}
}

// TestACleanCompanyHasNoNotices
//
// The good case, and the one a user sees most days. It must be an empty list rather than an
// error or a placeholder.
func TestACleanCompanyHasNoNotices(t *testing.T) {
	result, err := notify.New(emptyDismissals(), &stubRule{name: "ops.backup"}).
		Check(context.Background(), id.ID("c"), id.ID("u"))
	if err != nil {
		t.Fatalf("Check on a clean company: %v", err)
	}
	if len(result.Notices) != 0 || len(result.Failed) != 0 {
		t.Errorf("a clean company produced %+v", result)
	}
}
