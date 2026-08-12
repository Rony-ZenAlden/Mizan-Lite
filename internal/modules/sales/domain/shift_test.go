package domain_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/sales/domain"
)

func shift(t *testing.T, floatMinor int64) domain.Shift {
	t.Helper()
	built, err := domain.NewShift(
		id.ID("s1"), branch, "TILL-1", "2026-06-15T08:00:00Z", floatMinor)
	if err != nil {
		t.Fatalf("NewShift: %v", err)
	}
	return built
}

// ── the reconciliation ──────────────────────────────────────────────────────────

func TestAShiftThatBalancesReportsNoDifference(t *testing.T) {
	// Opened with 100, took 500 in cash, counted 600.
	closed, err := shift(t, 100).Close("2026-06-15T18:00:00Z", 600, 500)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}

	if closed.ExpectedMinor != 600 {
		t.Errorf("expected = %d, want 600 (100 float + 500 cash)", closed.ExpectedMinor)
	}
	if !closed.Balanced() {
		t.Errorf("difference = %d, want 0", closed.DifferenceMinor)
	}
}

// The single most useful number a shop's owner gets from a POS, and the one a badly built system
// quietly absorbs.
func TestAShortTillRecordsHowShort(t *testing.T) {
	// Should hold 600; the drawer had 550.
	closed, err := shift(t, 100).Close("2026-06-15T18:00:00Z", 550, 500)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}

	if closed.DifferenceMinor != -50 {
		t.Errorf("difference = %d, want -50", closed.DifferenceMinor)
	}
	if !closed.IsShort() {
		t.Error("a till 50 short did not report as short")
	}
	// Both figures survive, so somebody can see what was expected as well as what was found.
	if closed.ExpectedMinor != 600 || closed.CountedMinor != 550 {
		t.Errorf("expected %d and counted %d — one of them was overwritten",
			closed.ExpectedMinor, closed.CountedMinor)
	}
}

func TestAnOverTillRecordsHowOver(t *testing.T) {
	closed, err := shift(t, 100).Close("2026-06-15T18:00:00Z", 650, 500)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if closed.DifferenceMinor != 50 {
		t.Errorf("difference = %d, want 50", closed.DifferenceMinor)
	}
	if closed.IsShort() {
		t.Error("a till 50 over reported as short")
	}
}

// A till is short because somebody miscounted, gave wrong change, or took money. Refusing to
// close would leave the shop unable to shut, and the system is not the right thing to be deciding
// which of those it was.
func TestAShiftCanCloseEvenWhenItDoesNotBalance(t *testing.T) {
	if _, err := shift(t, 100).Close("2026-06-15T18:00:00Z", 1, 500); err != nil {
		t.Errorf("a shift 599 short could not be closed: %v", err)
	}
}

// CASH only. A card payment does not put money in the till, and counting it would make every
// shift that took a card look short by exactly that amount.
func TestOnlyCashCountsTowardsWhatTheDrawerShouldHold(t *testing.T) {
	// The caller passes only the cash total; this asserts the arithmetic it feeds.
	if got := domain.Expected(100, 500); got != 600 {
		t.Errorf("expected = %d, want 600", got)
	}
	if got := domain.Expected(0, 0); got != 0 {
		t.Errorf("an empty till with no float expects %d, want 0", got)
	}
}

// ── the lifecycle ───────────────────────────────────────────────────────────────

func TestAClosedShiftCannotBeClosedAgain(t *testing.T) {
	closed, err := shift(t, 100).Close("2026-06-15T18:00:00Z", 600, 500)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err = closed.Close("2026-06-15T19:00:00Z", 700, 500)
	if err == nil {
		t.Fatal("a closed shift was closed again")
	}
	if code := errs.CodeOf(err); code != domain.CodeShiftClosed {
		t.Errorf("code = %q, want %q", code, domain.CodeShiftClosed)
	}
}

func TestANewShiftIsOpenAndUnreconciled(t *testing.T) {
	opened := shift(t, 100)

	if opened.Status != domain.ShiftOpen {
		t.Errorf("status = %q, want open", opened.Status)
	}
	// Half a reconciliation is a number somebody will read as complete.
	if opened.ExpectedMinor != 0 || opened.CountedMinor != 0 || opened.DifferenceMinor != 0 {
		t.Error("a new shift already carries reconciliation figures")
	}
}

// ── what a shift refuses ────────────────────────────────────────────────────────

// A negative float is a drawer that owes money before trading starts.
func TestANegativeFloatIsRefused(t *testing.T) {
	_, err := domain.NewShift(id.ID("s1"), branch, "TILL-1", "2026-06-15T08:00:00Z", -100)
	if err == nil {
		t.Fatal("a negative opening float was accepted")
	}
	if code := errs.CodeOf(err); code != domain.CodeNegativeFloat {
		t.Errorf("code = %q, want %q", code, domain.CodeNegativeFloat)
	}
}

// Nobody counts minus three hundred. Accepting it would post a nonsense difference to the books.
func TestANegativeCountIsRefused(t *testing.T) {
	_, err := shift(t, 100).Close("2026-06-15T18:00:00Z", -1, 500)
	if err == nil {
		t.Fatal("a negative counted amount was accepted")
	}
	if code := errs.CodeOf(err); code != domain.CodeNegativeCount {
		t.Errorf("code = %q, want %q", code, domain.CodeNegativeCount)
	}
}

// A float of zero is an ordinary way to open a till, not an error.
func TestAZeroFloatIsFine(t *testing.T) {
	if _, err := domain.NewShift(
		id.ID("s1"), branch, "TILL-1", "2026-06-15T08:00:00Z", 0); err != nil {
		t.Errorf("a till opened with no float was refused: %v", err)
	}
}
