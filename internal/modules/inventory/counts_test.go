package inventory_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/modules/inventory"
	"github.com/mizan-erp/mizan/internal/modules/inventory/domain"
)

// counting opens a count and sends the sheet out, returning the lines.
func (f fixture) counting(t *testing.T, reference string) []domain.CountLine {
	t.Helper()
	if _, err := f.svc.OpenCount(f.ctx, inventory.NewCountInput{
		CompanyID: f.companyID, WarehouseID: f.warehouseID, Reference: reference,
	}); err != nil {
		t.Fatalf("OpenCount: %v", err)
	}
	if _, err := f.svc.BeginCounting(f.ctx, f.companyID, reference); err != nil {
		t.Fatalf("BeginCounting: %v", err)
	}
	lines, err := f.svc.CountLines(f.ctx, f.companyID, reference)
	if err != nil {
		t.Fatalf("CountLines: %v", err)
	}
	return lines
}

// ── THE correctness point: a count preserves concurrent movements ───────────────

// A count takes hours and stock moves during it. Applying the counted FIGURE would silently undo
// every movement made while people were counting — a sale shipped at 11am reappearing when the
// count is applied at 4pm.
func TestASaleDuringACountIsNotUndoneByApplyingIt(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 10_000_000, 100_000_000)

	lines := f.counting(t, "MARCH")
	if len(lines) != 1 {
		t.Fatalf("%d lines, want one for the variant with stock", len(lines))
	}

	// The counter walks the aisle and finds 8: two are genuinely missing.
	if err := f.svc.RecordCount(f.ctx, lines[0].ID, 8_000_000, "two missing"); err != nil {
		t.Fatalf("RecordCount: %v", err)
	}

	// Meanwhile, three are sold. This is the movement that must survive.
	f.move(t, domain.Issue, 3_000_000, 0)

	if _, err := f.svc.SubmitCount(f.ctx, f.companyID, "MARCH"); err != nil {
		t.Fatalf("SubmitCount: %v", err)
	}
	if _, err := f.svc.ApplyCount(f.ctx, f.companyID, "MARCH"); err != nil {
		t.Fatalf("ApplyCount: %v", err)
	}

	// 10 received, 3 sold, 2 found missing = 5.
	//
	// Applying the counted figure of 8 would give 8 — resurrecting the three that were sold and
	// leaving the ledger showing stock that had already left the building.
	if state := f.stock(t); state.OnHandMicro != 5_000_000 {
		t.Errorf("on hand = %d, want 5000000 — applying the counted figure would give 8000000",
			state.OnHandMicro)
	}

	// And the ledger still verifies: the count's movement is consistent with what it recorded.
	discrepancies, err := f.svc.VerifyLedger(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("VerifyLedger: %v", err)
	}
	if len(discrepancies) != 0 {
		t.Errorf("%d discrepancies after a count: %+v", len(discrepancies), discrepancies)
	}
}

func TestACountThatAgreesWritesNoMovement(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 10_000_000, 100_000_000)

	lines := f.counting(t, "MARCH")
	if err := f.svc.RecordCount(f.ctx, lines[0].ID, 10_000_000, ""); err != nil {
		t.Fatalf("RecordCount: %v", err)
	}
	if _, err := f.svc.SubmitCount(f.ctx, f.companyID, "MARCH"); err != nil {
		t.Fatalf("SubmitCount: %v", err)
	}

	plan, err := f.svc.ApplyCount(f.ctx, f.companyID, "MARCH")
	if err != nil {
		t.Fatalf("ApplyCount: %v", err)
	}
	if len(plan.Adjustments) != 0 {
		t.Errorf("%d adjustments for a count that agreed", len(plan.Adjustments))
	}

	movements, err := f.svc.Movements(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("Movements: %v", err)
	}
	if len(movements) != 1 {
		t.Errorf("%d movements, want only the original receipt", len(movements))
	}
}

// An uncounted line is not a zero. Treating it as one would write off the entire stock of
// everything the counter did not reach.
func TestAnUncountedLineIsNotWrittenOff(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 10_000_000, 100_000_000)

	f.counting(t, "MARCH")
	// Nobody enters a figure at all.
	if _, err := f.svc.SubmitCount(f.ctx, f.companyID, "MARCH"); err != nil {
		t.Fatalf("SubmitCount: %v", err)
	}

	plan, err := f.svc.ApplyCount(f.ctx, f.companyID, "MARCH")
	if err != nil {
		t.Fatalf("ApplyCount: %v", err)
	}
	if plan.Uncounted != 1 || len(plan.Adjustments) != 0 {
		t.Errorf("plan = %+v, want one uncounted line and no adjustments", plan)
	}
	if state := f.stock(t); state.OnHandMicro != 10_000_000 {
		t.Errorf("on hand = %d — an uncounted line was written off", state.OnHandMicro)
	}
}

// A deliberate zero IS a finding, and must be applied.
func TestADeliberateZeroIsApplied(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 10_000_000, 100_000_000)

	lines := f.counting(t, "MARCH")
	if err := f.svc.RecordCount(f.ctx, lines[0].ID, 0, "shelf empty"); err != nil {
		t.Fatalf("RecordCount: %v", err)
	}
	if _, err := f.svc.SubmitCount(f.ctx, f.companyID, "MARCH"); err != nil {
		t.Fatalf("SubmitCount: %v", err)
	}
	if _, err := f.svc.ApplyCount(f.ctx, f.companyID, "MARCH"); err != nil {
		t.Fatalf("ApplyCount: %v", err)
	}

	if state := f.stock(t); state.OnHandMicro != 0 {
		t.Errorf("on hand = %d, want 0 — a deliberate zero is a real finding", state.OnHandMicro)
	}
}

// ── the blind control ───────────────────────────────────────────────────────────

// The single most valuable control here. Showing the expectation is how a count comes back
// agreeing with the system on every line while the shelves say otherwise.
func TestABlindCountDoesNotTellTheCounterWhatToExpect(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 10_000_000, 100_000_000)

	lines := f.counting(t, "MARCH")
	if lines[0].ExpectedMicro != 0 {
		t.Errorf("the counter was shown an expectation of %d", lines[0].ExpectedMicro)
	}

	// But the expectation IS stored — the apply needs it. Hiding it at the READ is what makes
	// the control real: a screen cannot show what it was never given.
	if err := f.svc.RecordCount(f.ctx, lines[0].ID, 8_000_000, ""); err != nil {
		t.Fatalf("RecordCount: %v", err)
	}
	if _, err := f.svc.SubmitCount(f.ctx, f.companyID, "MARCH"); err != nil {
		t.Fatalf("SubmitCount: %v", err)
	}

	// In REVIEW the variance is the whole point, so the expectation comes back.
	reviewed, err := f.svc.CountLines(f.ctx, f.companyID, "MARCH")
	if err != nil {
		t.Fatalf("CountLines: %v", err)
	}
	if reviewed[0].ExpectedMicro != 10_000_000 {
		t.Errorf("the reviewer was shown an expectation of %d, want 10000000",
			reviewed[0].ExpectedMicro)
	}
	if reviewed[0].VarianceMicro() != -2_000_000 {
		t.Errorf("variance = %d, want -2000000", reviewed[0].VarianceMicro())
	}
}

// The dangerous option must be asked for by name.
func TestAnOpenCountShowsTheExpectationOnlyWhenAsked(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 10_000_000, 100_000_000)

	if _, err := f.svc.OpenCount(f.ctx, inventory.NewCountInput{
		CompanyID: f.companyID, WarehouseID: f.warehouseID, Reference: "OPEN",
		ShowExpected: true,
	}); err != nil {
		t.Fatalf("OpenCount: %v", err)
	}
	if _, err := f.svc.BeginCounting(f.ctx, f.companyID, "OPEN"); err != nil {
		t.Fatalf("BeginCounting: %v", err)
	}

	lines, err := f.svc.CountLines(f.ctx, f.companyID, "OPEN")
	if err != nil {
		t.Fatalf("CountLines: %v", err)
	}
	if lines[0].ExpectedMicro != 10_000_000 {
		t.Errorf("an open count hid the expectation: %d", lines[0].ExpectedMicro)
	}
}

// ── the lifecycle, through storage ──────────────────────────────────────────────

// The variances are the entire product of the exercise: a line reading 3 where 300 was expected
// is a miscount far more often than a theft.
func TestACountCannotBeAppliedWithoutReview(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 10_000_000, 100_000_000)

	lines := f.counting(t, "MARCH")
	if err := f.svc.RecordCount(f.ctx, lines[0].ID, 3_000_000, ""); err != nil {
		t.Fatalf("RecordCount: %v", err)
	}

	_, err := f.svc.ApplyCount(f.ctx, f.companyID, "MARCH")
	if err == nil {
		t.Fatal("a count was applied without anybody reviewing the variances")
	}
	if code := errs.CodeOf(err); code != domain.CodeCountNotInReview {
		t.Errorf("code = %q, want %q", code, domain.CodeCountNotInReview)
	}

	// And nothing moved.
	if state := f.stock(t); state.OnHandMicro != 10_000_000 {
		t.Errorf("on hand = %d after a refused apply", state.OnHandMicro)
	}
}

func TestAnAppliedCountCannotBeAppliedAgain(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 10_000_000, 100_000_000)

	lines := f.counting(t, "MARCH")
	if err := f.svc.RecordCount(f.ctx, lines[0].ID, 8_000_000, ""); err != nil {
		t.Fatalf("RecordCount: %v", err)
	}
	if _, err := f.svc.SubmitCount(f.ctx, f.companyID, "MARCH"); err != nil {
		t.Fatalf("SubmitCount: %v", err)
	}
	if _, err := f.svc.ApplyCount(f.ctx, f.companyID, "MARCH"); err != nil {
		t.Fatalf("ApplyCount: %v", err)
	}

	before := f.stock(t)
	if _, err := f.svc.ApplyCount(f.ctx, f.companyID, "MARCH"); err == nil {
		t.Fatal("an applied count was applied a second time")
	}
	// Applying twice would take another two off the shelf.
	if after := f.stock(t); after.OnHandMicro != before.OnHandMicro {
		t.Errorf("on hand = %d, want %d — the count applied twice",
			after.OnHandMicro, before.OnHandMicro)
	}
}

func TestACancelledCountLeavesNoMovements(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 10_000_000, 100_000_000)

	lines := f.counting(t, "MARCH")
	if err := f.svc.RecordCount(f.ctx, lines[0].ID, 2_000_000, ""); err != nil {
		t.Fatalf("RecordCount: %v", err)
	}
	if _, err := f.svc.CancelCount(f.ctx, f.companyID, "MARCH"); err != nil {
		t.Fatalf("CancelCount: %v", err)
	}

	if _, err := f.svc.ApplyCount(f.ctx, f.companyID, "MARCH"); err == nil {
		t.Fatal("a cancelled count was applied")
	}
	if state := f.stock(t); state.OnHandMicro != 10_000_000 {
		t.Errorf("on hand = %d — a cancelled count moved stock", state.OnHandMicro)
	}
}

// ── the sheet ───────────────────────────────────────────────────────────────────

// A count of a warehouse is a count of what is IN it. Asking the operator to list the variants
// first would make a full count an afternoon's data entry before anybody reaches a shelf.
func TestBeginningACountGeneratesALinePerThingInStock(t *testing.T) {
	f := newFixture(t)
	other := f.secondVariant(t)

	f.move(t, domain.Receipt, 10_000_000, 100_000_000)
	if _, err := f.svc.Move(f.ctx, inventory.MoveInput{
		CompanyID: f.companyID, WarehouseID: f.warehouseID,
		ProductID: f.product.ID, VariantID: other,
		Type: domain.Receipt, QuantityMicro: 4_000_000, UnitCostMicro: 100_000_000,
	}); err != nil {
		t.Fatalf("Move: %v", err)
	}

	lines := f.counting(t, "MARCH")
	if len(lines) != 2 {
		t.Errorf("%d lines, want one per variant with stock", len(lines))
	}
}

// A negative count is not a finding, it is a typo: nobody counts minus three boxes.
func TestANegativeCountedQuantityIsRefused(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 10_000_000, 100_000_000)

	lines := f.counting(t, "MARCH")
	if err := f.svc.RecordCount(f.ctx, lines[0].ID, -1_000_000, ""); err == nil {
		t.Fatal("a negative counted quantity was accepted")
	}
}

func TestTwoCountsCannotShareAReference(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.OpenCount(f.ctx, inventory.NewCountInput{
		CompanyID: f.companyID, WarehouseID: f.warehouseID, Reference: "MARCH",
	}); err != nil {
		t.Fatalf("OpenCount: %v", err)
	}
	if _, err := f.svc.OpenCount(f.ctx, inventory.NewCountInput{
		CompanyID: f.companyID, WarehouseID: f.warehouseID, Reference: "MARCH",
	}); err == nil {
		t.Fatal("two counts shared a reference")
	}
}
