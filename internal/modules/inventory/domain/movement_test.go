package domain_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/inventory/domain"
)

const (
	warehouse = id.ID("wh1")
	product   = id.ID("p1")
	variant   = id.ID("v1")
)

func movement(t *testing.T, identifier string, kind domain.Type, qty int64) domain.Movement {
	t.Helper()
	built, err := domain.NewMovement(
		id.ID(identifier), warehouse, product, variant, kind, qty)
	if err != nil {
		t.Fatalf("NewMovement(%s): %v", identifier, err)
	}
	return built
}

// folded builds a movement with the balance it would have after being applied, which is what
// the service records at write time.
func folded(t *testing.T, state domain.State, m domain.Movement) (domain.State, domain.Movement) {
	t.Helper()
	next, err := domain.Apply(state, m)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	m.BalanceAfterMicro = next.OnHandMicro
	return next, m
}

// ── direction ───────────────────────────────────────────────────────────────────

// The decision this whole design rests on: quantity is always positive and the TYPE carries the
// direction, so a query that forgets direction cannot silently produce a plausible wrong number.
func TestEveryMovementTypeHasExactlyOneDirection(t *testing.T) {
	cases := []struct {
		kind domain.Type
		want domain.Direction
	}{
		{domain.Receipt, domain.Inward},
		{domain.ReturnIn, domain.Inward},
		{domain.AdjustmentIn, domain.Inward},
		{domain.TransferIn, domain.Inward},
		{domain.Issue, domain.Outward},
		{domain.AdjustmentOut, domain.Outward},
		{domain.TransferOut, domain.Outward},
		// A revaluation moves VALUE without moving quantity, which is why it must be its own
		// case rather than an "in" of zero.
		{domain.Revaluation, domain.Neutral},
		// A count REPLACES rather than adjusts, so it contributes no signed delta.
		{domain.Count, domain.Neutral},
	}

	for _, tc := range cases {
		got, err := domain.DirectionOf(tc.kind)
		if err != nil {
			t.Fatalf("DirectionOf(%s): %v", tc.kind, err)
		}
		if got != tc.want {
			t.Errorf("%s points %d, want %d", tc.kind, got, tc.want)
		}
	}
}

func TestAnUnknownMovementTypeIsRefused(t *testing.T) {
	if _, err := domain.DirectionOf("teleport"); err == nil {
		t.Fatal("an unknown movement type was given a direction")
	} else if code := errs.CodeOf(err); code != domain.CodeUnknownType {
		t.Errorf("code = %q, want %q", code, domain.CodeUnknownType)
	}

	// And a movement cannot be built with one, so an unknown type never reaches the ledger.
	if _, err := domain.NewMovement(
		id.ID("m"), warehouse, product, variant, "teleport", 1); err == nil {
		t.Error("a movement of an unknown type was built")
	}
}

// ── construction ────────────────────────────────────────────────────────────────

// Zero moves nothing and would still write a ledger row — a movement that happened according to
// the trail and did not according to the stock. Negative is the signed-quantity mistake this
// design exists to make unrepresentable.
func TestAMovementMustMoveAPositiveQuantity(t *testing.T) {
	for _, quantity := range []int64{0, -1_000_000} {
		_, err := domain.NewMovement(
			id.ID("m"), warehouse, product, variant, domain.Receipt, quantity)
		if err == nil {
			t.Errorf("a movement of %d was accepted", quantity)
			continue
		}
		if code := errs.CodeOf(err); code != domain.CodeNonPositiveQty {
			t.Errorf("code = %q, want %q", code, domain.CodeNonPositiveQty)
		}
	}
}

// The constructor does not accept a balance: that is what the fold produces, and a caller able
// to assert one could write a balance the ledger does not support.
func TestAMovementIsBornWithNoBalance(t *testing.T) {
	built := movement(t, "m1", domain.Receipt, 10_000_000)
	if built.BalanceAfterMicro != 0 || built.AverageAfterMicro != 0 {
		t.Error("construction asserted a balance the ledger has not agreed to")
	}
}

// §D.3: a return is costed at its ORIGINAL issue's cost. Without the source there is no correct
// cost, and using today's average would invent profit on an item bought last year.
func TestAReturnMustNameTheIssueItReverses(t *testing.T) {
	built := movement(t, "m1", domain.ReturnIn, 1_000_000)

	if err := built.RequireCostable(); err == nil {
		t.Fatal("a return with no source movement was accepted")
	} else if code := errs.CodeOf(err); code != domain.CodeReturnNeedsSource {
		t.Errorf("code = %q, want %q", code, domain.CodeReturnNeedsSource)
	}

	built.SourceMovementID = id.ID("original-issue")
	if err := built.RequireCostable(); err != nil {
		t.Errorf("a return naming its source was refused: %v", err)
	}
}

// A negative unit cost would credit inventory on a receipt and make the valuation smaller for
// having received goods.
func TestANegativeUnitCostIsRefused(t *testing.T) {
	built := movement(t, "m1", domain.Receipt, 1_000_000)
	built.UnitCostMicro = -1

	if err := built.RequireCostable(); err == nil {
		t.Fatal("a negative unit cost was accepted")
	} else if code := errs.CodeOf(err); code != domain.CodeNegativeCost {
		t.Errorf("code = %q, want %q", code, domain.CodeNegativeCost)
	}
}

// ── the fold ────────────────────────────────────────────────────────────────────

func TestReceiptsAddAndIssuesSubtract(t *testing.T) {
	var state domain.State

	state, _ = folded(t, state, movement(t, "m1", domain.Receipt, 10_000_000))
	if state.OnHandMicro != 10_000_000 {
		t.Fatalf("after a receipt of 10: %d", state.OnHandMicro)
	}

	state, _ = folded(t, state, movement(t, "m2", domain.Issue, 3_000_000))
	if state.OnHandMicro != 7_000_000 {
		t.Fatalf("after issuing 3: %d, want 7000000", state.OnHandMicro)
	}

	state, _ = folded(t, state, movement(t, "m3", domain.ReturnIn, 1_000_000))
	if state.OnHandMicro != 8_000_000 {
		t.Errorf("after a return of 1: %d, want 8000000", state.OnHandMicro)
	}
}

// A physical count is an ASSERTION about reality, not a delta. Treating it as an adjustment
// would double-count: the counted figure would be added to what the system already believed.
func TestACountReplacesTheBalanceRatherThanAdjustingIt(t *testing.T) {
	state := domain.State{OnHandMicro: 10_000_000}

	counted := movement(t, "m1", domain.Count, 2_000_000) // the DIFFERENCE found
	counted.BalanceAfterMicro = 8_000_000                 // the figure actually counted

	after, err := domain.Apply(state, counted)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if after.OnHandMicro != 8_000_000 {
		t.Errorf("on hand = %d after counting 8, want 8000000", after.OnHandMicro)
	}
}

// Value moves, quantity does not. A revaluation that changed the count would restate the
// warehouse as well as the books.
func TestARevaluationMovesValueWithoutMovingQuantity(t *testing.T) {
	state := domain.State{OnHandMicro: 10_000_000, AverageMicro: 5_000_000}

	revaluation := movement(t, "m1", domain.Revaluation, 1)
	revaluation.AverageAfterMicro = 6_000_000

	after, err := domain.Apply(state, revaluation)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if after.OnHandMicro != 10_000_000 {
		t.Errorf("a revaluation changed the quantity to %d", after.OnHandMicro)
	}
	if after.AverageMicro != 6_000_000 {
		t.Errorf("average = %d, want 6000000", after.AverageMicro)
	}
}

func TestAvailableIsOnHandLessReserved(t *testing.T) {
	state := domain.State{OnHandMicro: 10_000_000, ReservedMicro: 4_000_000}
	if state.AvailableMicro() != 6_000_000 {
		t.Errorf("available = %d, want 6000000", state.AvailableMicro())
	}

	// Reserving more than is held is Phase 5's problem to prevent; the arithmetic here simply
	// tells the truth about it rather than clamping at zero and hiding the oversell.
	over := domain.State{OnHandMicro: 1_000_000, ReservedMicro: 4_000_000}
	if over.AvailableMicro() != -3_000_000 {
		t.Errorf("available = %d, want -3000000 — a clamp would hide the oversell",
			over.AvailableMicro())
	}
}

// ── replay and verification ─────────────────────────────────────────────────────

// The projection and its own verification share ONE fold, so they cannot disagree about what a
// movement means.
func TestReplayingTheLedgerReproducesTheProjection(t *testing.T) {
	var state domain.State
	ledger := make([]domain.Movement, 0, 4)

	for _, step := range []struct {
		kind domain.Type
		qty  int64
	}{
		{domain.Receipt, 10_000_000},
		{domain.Issue, 3_000_000},
		{domain.Receipt, 5_000_000},
		{domain.AdjustmentOut, 1_000_000},
	} {
		var m domain.Movement
		state, m = folded(t, state, movement(t, string(step.kind), step.kind, step.qty))
		ledger = append(ledger, m)
	}

	replayed, err := domain.Replay(ledger)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if replayed.OnHandMicro != state.OnHandMicro {
		t.Errorf("replay = %d, projection = %d", replayed.OnHandMicro, state.OnHandMicro)
	}
	if replayed.OnHandMicro != 11_000_000 {
		t.Errorf("on hand = %d, want 11000000", replayed.OnHandMicro)
	}
}

func TestAnAgreeingProjectionReportsNoDiscrepancy(t *testing.T) {
	var state domain.State
	ledger := make([]domain.Movement, 0, 2)
	for _, qty := range []int64{10_000_000, 4_000_000} {
		var m domain.Movement
		state, m = folded(t, state, movement(t, "m", domain.Receipt, qty))
		ledger = append(ledger, m)
	}

	if _, differs, err := domain.Verify(state, ledger); err != nil {
		t.Fatalf("Verify: %v", err)
	} else if differs {
		t.Error("a projection that agrees with its ledger was reported as drifted")
	}
}

// The whole point of the job: a drift is CAUGHT, and reported rather than repaired.
func TestADriftedProjectionIsReportedNotRepaired(t *testing.T) {
	var state domain.State
	ledger := make([]domain.Movement, 0, 2)
	for _, qty := range []int64{10_000_000, 4_000_000} {
		var m domain.Movement
		state, m = folded(t, state, movement(t, "m", domain.Receipt, qty))
		ledger = append(ledger, m)
	}

	// The projection has been corrupted — the exact failure this job exists to find.
	drifted := domain.State{OnHandMicro: 99_000_000, AverageMicro: state.AverageMicro}

	discrepancy, differs, err := domain.Verify(drifted, ledger)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !differs {
		t.Fatal("a corrupted projection was reported as agreeing")
	}
	if discrepancy.ProjectedOnHand != 99_000_000 || discrepancy.LedgerOnHand != 14_000_000 {
		t.Errorf("discrepancy = %+v, want both figures reported", discrepancy)
	}

	// Verify RETURNED a report; it did not mutate the input. A projection that heals itself
	// hides the bug that broke it, and the next drift is silent too.
	if drifted.OnHandMicro != 99_000_000 {
		t.Error("Verify repaired the projection instead of reporting it")
	}
}

// "The total is wrong" is a bug report; "it went wrong here" is an investigation. That is what
// balance_after buys, and this is the test that makes it worth its column.
func TestVerifyNamesTheFirstSuspectMovement(t *testing.T) {
	var state domain.State
	ledger := make([]domain.Movement, 0, 4)
	for i, qty := range []int64{10_000_000, 4_000_000, 2_000_000, 1_000_000} {
		var m domain.Movement
		state, m = folded(t, state, movement(t, string(rune('a'+i)), domain.Receipt, qty))
		ledger = append(ledger, m)
	}

	// The third movement recorded a balance the ledger does not support.
	ledger[2].BalanceAfterMicro = 999_000_000

	discrepancy, differs, err := domain.Verify(
		domain.State{OnHandMicro: 0, AverageMicro: state.AverageMicro}, ledger)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !differs {
		t.Fatal("no drift was reported")
	}
	if discrepancy.FirstSuspectMovementID != ledger[2].ID {
		t.Errorf("first suspect = %q, want %q — the point of recording balance_after",
			discrepancy.FirstSuspectMovementID, ledger[2].ID)
	}
}

func TestReplayingAnEmptyLedgerIsZeroNotAnError(t *testing.T) {
	// A variant that has never moved is the ordinary case for most of a catalog, not a fault.
	state, err := domain.Replay(nil)
	if err != nil {
		t.Fatalf("Replay(nil): %v", err)
	}
	if state.OnHandMicro != 0 {
		t.Errorf("on hand = %d, want 0", state.OnHandMicro)
	}
}
