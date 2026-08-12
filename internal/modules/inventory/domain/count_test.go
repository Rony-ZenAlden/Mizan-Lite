package domain_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/inventory/domain"
)

func counted(qty int64) *int64 { return &qty }

func line(variantID string, expected int64, found *int64) domain.CountLine {
	return domain.CountLine{
		ID: id.ID(variantID + "-line"), ProductID: product, VariantID: id.ID(variantID),
		ExpectedMicro: expected, CountedMicro: found,
	}
}

// ── the correctness point: the VARIANCE is applied, not the counted figure ──────

// A count takes hours, and stock moves during it. If applying the count SET the balance to the
// counted figure, every movement made while people were counting would be silently undone — a
// sale shipped at 11am would reappear when the count was applied at 4pm.
func TestApplyingACountPreservesMovementsMadeWhileCounting(t *testing.T) {
	// The sheet was printed when there were 10. The counter found 8: two are genuinely missing.
	lines := []domain.CountLine{line("v1", 10_000_000, counted(8_000_000))}

	// Meanwhile, three were sold. Current stock is 7, not 10.
	current := map[id.ID]int64{id.ID("v1"): 7_000_000}

	plan := domain.PlanCount(lines, current)
	if len(plan.Adjustments) != 1 {
		t.Fatalf("%d adjustments, want 1", len(plan.Adjustments))
	}

	adjustment := plan.Adjustments[0]
	// The counter established a shortfall of two, and that is what the count contributes.
	if adjustment.VarianceMicro != -2_000_000 {
		t.Errorf("variance = %d, want -2000000", adjustment.VarianceMicro)
	}
	// 7 on hand less the 2 the counter could not find = 5. NOT 8, which would put back the
	// three that were sold.
	if adjustment.BalanceAfterMicro != 5_000_000 {
		t.Errorf("balance after = %d, want 5000000 — applying the counted figure would give "+
			"8000000 and resurrect three sold units", adjustment.BalanceAfterMicro)
	}
}

func TestACountThatAgreesMakesNoAdjustment(t *testing.T) {
	lines := []domain.CountLine{line("v1", 10_000_000, counted(10_000_000))}
	current := map[id.ID]int64{id.ID("v1"): 10_000_000}

	plan := domain.PlanCount(lines, current)
	if len(plan.Adjustments) != 0 {
		t.Errorf("%d adjustments for a count that agreed", len(plan.Adjustments))
	}
	// Reported, so a review screen can say "412 agreed, 6 did not" rather than showing six rows
	// with no context.
	if plan.Unchanged != 1 {
		t.Errorf("unchanged = %d, want 1", plan.Unchanged)
	}
}

// An uncounted line is NOT a zero. Treating it as one would write off the entire stock of
// everything the counter did not reach — on a partial count, most of the warehouse.
func TestAnUncountedLineIsLeftAloneRatherThanWrittenOff(t *testing.T) {
	lines := []domain.CountLine{
		line("v1", 10_000_000, nil),       // nobody reached it
		line("v2", 5_000_000, counted(0)), // deliberately counted as none
	}
	current := map[id.ID]int64{id.ID("v1"): 10_000_000, id.ID("v2"): 5_000_000}

	plan := domain.PlanCount(lines, current)

	if plan.Uncounted != 1 {
		t.Errorf("uncounted = %d, want 1", plan.Uncounted)
	}
	// Only the deliberate zero adjusts. A zero somebody entered is a real finding; a zero
	// meaning "nobody looked" is not.
	if len(plan.Adjustments) != 1 {
		t.Fatalf("%d adjustments, want only the deliberate zero", len(plan.Adjustments))
	}
	if plan.Adjustments[0].Line.VariantID != id.ID("v2") {
		t.Error("the uncounted line was adjusted")
	}
	if plan.Adjustments[0].VarianceMicro != -5_000_000 {
		t.Errorf("variance = %d, want -5000000", plan.Adjustments[0].VarianceMicro)
	}
}

func TestACountCanFindMoreThanExpected(t *testing.T) {
	lines := []domain.CountLine{line("v1", 10_000_000, counted(12_000_000))}
	current := map[id.ID]int64{id.ID("v1"): 10_000_000}

	plan := domain.PlanCount(lines, current)
	if len(plan.Adjustments) != 1 {
		t.Fatalf("%d adjustments, want 1", len(plan.Adjustments))
	}
	if plan.Adjustments[0].VarianceMicro != 2_000_000 {
		t.Errorf("variance = %d, want 2000000", plan.Adjustments[0].VarianceMicro)
	}
	if plan.Adjustments[0].BalanceAfterMicro != 12_000_000 {
		t.Errorf("balance = %d, want 12000000", plan.Adjustments[0].BalanceAfterMicro)
	}
}

// ── the lifecycle ───────────────────────────────────────────────────────────────

func TestACountRunsDraftCountingReviewApplied(t *testing.T) {
	count, err := domain.NewCount(id.ID("c1"), warehouse, "MARCH")
	if err != nil {
		t.Fatalf("NewCount: %v", err)
	}
	if count.Status != domain.CountDraft {
		t.Fatalf("status = %q, want draft", count.Status)
	}

	// Applying a draft is refused: nobody has counted anything.
	if err = count.RequireApplicable(); err == nil {
		t.Error("a draft count was applicable")
	}

	count, err = count.BeginCounting("2026-06-15T09:00:00Z")
	if err != nil {
		t.Fatalf("BeginCounting: %v", err)
	}
	if count.SnapshotAt == "" {
		t.Error("beginning a count did not freeze the expectations")
	}

	// Applying straight from counting is refused — see below.
	if err = count.RequireApplicable(); err == nil {
		t.Error("a count still being counted was applicable")
	}

	count, err = count.SubmitForReview()
	if err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}
	if err = count.RequireApplicable(); err != nil {
		t.Errorf("a count in review was not applicable: %v", err)
	}
}

// The variances are the entire product of the exercise. A line reading 3 where 300 was expected
// is a miscount far more often than a theft, and applying it unseen writes off stock sitting on
// the shelf.
func TestACountIsAppliedFromReviewNotFromCounting(t *testing.T) {
	count, _ := domain.NewCount(id.ID("c1"), warehouse, "MARCH")
	count, _ = count.BeginCounting("2026-06-15T09:00:00Z")

	err := count.RequireApplicable()
	if err == nil {
		t.Fatal("a count was applied without anybody reviewing the variances")
	}
	if code := errs.CodeOf(err); code != domain.CodeCountNotInReview {
		t.Errorf("code = %q, want %q", code, domain.CodeCountNotInReview)
	}
}

func TestAnAppliedCountCannotBeAppliedTwice(t *testing.T) {
	count, _ := domain.NewCount(id.ID("c1"), warehouse, "MARCH")
	count, _ = count.BeginCounting("2026-06-15T09:00:00Z")
	count, _ = count.SubmitForReview()
	count = count.Applied("2026-06-15T17:00:00Z")

	err := count.RequireApplicable()
	if err == nil {
		t.Fatal("an applied count was applied again")
	}
	if code := errs.CodeOf(err); code != domain.CodeCountTerminal {
		t.Errorf("code = %q, want %q", code, domain.CodeCountTerminal)
	}
}

func TestACancelledCountCannotBeAppliedOrCancelledAgain(t *testing.T) {
	count, _ := domain.NewCount(id.ID("c1"), warehouse, "MARCH")
	count, err := count.Cancel()
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if err = count.RequireApplicable(); err == nil {
		t.Error("a cancelled count was applied")
	}
	if _, err = count.Cancel(); err == nil {
		t.Error("a cancelled count was cancelled again")
	}
}

func TestACountCannotSkipStraightToReview(t *testing.T) {
	count, _ := domain.NewCount(id.ID("c1"), warehouse, "MARCH")

	if _, err := count.SubmitForReview(); err == nil {
		t.Fatal("a draft count was submitted for review without being counted")
	}
}

// ── blind by default ────────────────────────────────────────────────────────────

// The single most valuable control here. Showing the expectation is how a count comes back
// agreeing with the system on every line while the shelves say otherwise — not through
// dishonesty, but because a tired person looking at "10" finds ten.
func TestACountIsBlindByDefault(t *testing.T) {
	count, err := domain.NewCount(id.ID("c1"), warehouse, "MARCH")
	if err != nil {
		t.Fatalf("NewCount: %v", err)
	}
	if !count.IsBlind {
		t.Error("a new count showed the counter what to expect")
	}
}

func TestACountNeedsAReference(t *testing.T) {
	if _, err := domain.NewCount(id.ID("c1"), warehouse, "   "); err == nil {
		t.Fatal("a count with no reference was accepted")
	}
}
