package domain_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/inventory/domain"
)

// wac builds the strategy with negative stock blocked — the default (§2.5).
func wac() domain.WAC { return domain.WAC{} }

// permissive builds it for the businesses that genuinely need negative stock.
func permissive() domain.WAC {
	return domain.WAC{Options: domain.Options{AllowNegativeStock: true}}
}

func costed(t *testing.T, kind domain.Type, qty, unitCost int64) domain.Movement {
	t.Helper()
	m := movement(t, "m", kind, qty)
	m.UnitCostMicro = unitCost
	return m
}

// ── the average ─────────────────────────────────────────────────────────────────

// The formula §D.3 states, worked by hand:
//
//	10 @ 100 then 10 @ 200 → (10×100 + 10×200) ÷ 20 = 150
func TestReceiptsBlendIntoAWeightedAverage(t *testing.T) {
	state := domain.State{OnHandMicro: 10_000_000, AverageMicro: 100_000_000}

	result, err := wac().Cost(state, costed(t, domain.Receipt, 10_000_000, 200_000_000),
		domain.Movement{})
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}
	if result.NewAverageMicro != 150_000_000 {
		t.Errorf("average = %d, want 150000000", result.NewAverageMicro)
	}
	// The receipt is valued at what was paid, not at the new average.
	if result.UnitCostMicro != 200_000_000 {
		t.Errorf("unit cost = %d, want the receipt cost", result.UnitCostMicro)
	}
}

// Weighted, not arithmetic: 90 units at 100 and 10 at 200 is 110, not 150.
func TestTheAverageIsWeightedByQuantityNotByReceipt(t *testing.T) {
	state := domain.State{OnHandMicro: 90_000_000, AverageMicro: 100_000_000}

	result, err := wac().Cost(state, costed(t, domain.Receipt, 10_000_000, 200_000_000),
		domain.Movement{})
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}
	if result.NewAverageMicro != 110_000_000 {
		t.Errorf("average = %d, want 110000000 — a plain mean would give 150000000",
			result.NewAverageMicro)
	}
}

// The defining property of a MOVING average, and getting it wrong drifts the valuation on every
// single sale.
func TestAnIssueDoesNotChangeTheAverage(t *testing.T) {
	state := domain.State{OnHandMicro: 10_000_000, AverageMicro: 150_000_000}

	result, err := wac().Cost(state, costed(t, domain.Issue, 3_000_000, 0), domain.Movement{})
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}
	if result.NewAverageMicro != 150_000_000 {
		t.Errorf("average = %d, want it unchanged at 150000000", result.NewAverageMicro)
	}
	// And the issue is costed at the average, not at zero.
	if result.UnitCostMicro != 150_000_000 {
		t.Errorf("unit cost = %d, want the current average", result.UnitCostMicro)
	}
	if result.ValueDeltaMinor != -450 {
		t.Errorf("value delta = %d, want -450 (3 × 150)", result.ValueDeltaMinor)
	}
}

// ── TRAP 1: zero or negative on-hand at receipt (§D.3) ──────────────────────────

// With nothing on hand, the new average is the receipt cost.
//
// This test DOCUMENTS the behaviour; it does not pin the guard. The mutation drill for the
// `<= 0` branch showed why: with on-hand at zero the general formula already produces the
// receipt cost, because the first term vanishes for any stale average whatsoever. Deleting the
// guard leaves this test passing — correctly.
//
// The half of that guard which IS load-bearing is the negative case, and it has the test below.
// Phase 3's rule again: a test must name the mechanism it is about.
func TestAReceiptIntoEmptyStockTakesTheReceiptCost(t *testing.T) {
	var empty domain.State

	result, err := wac().Cost(empty, costed(t, domain.Receipt, 5_000_000, 120_000_000),
		domain.Movement{})
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}
	if result.NewAverageMicro != 120_000_000 {
		t.Errorf("average = %d, want the receipt cost 120000000", result.NewAverageMicro)
	}
}

// Negative on-hand carries an average that is a fiction — there was nothing there to have a
// cost — and the divisor is smaller than the received quantity. Receiving 10 at 120 into a
// stock of −5 carrying a phantom average of 999 yields −759 rather than 120: a NEGATIVE unit
// cost, which then values the whole warehouse below nothing.
//
// This is the test that pins the guard.
func TestAReceiptIntoNegativeStockTakesTheReceiptCost(t *testing.T) {
	negative := domain.State{OnHandMicro: -5_000_000, AverageMicro: 999_000_000}

	result, err := wac().Cost(negative, costed(t, domain.Receipt, 10_000_000, 120_000_000),
		domain.Movement{})
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}
	if result.NewAverageMicro != 120_000_000 {
		t.Errorf("average = %d, want 120000000 — the fictional average was blended in",
			result.NewAverageMicro)
	}
}

// ── TRAP 2: issuing more than is on hand (§D.3) ─────────────────────────────────

// Blocked by DEFAULT. The failure it prevents is selling what does not exist.
func TestIssuingMoreThanIsHeldIsRefusedByDefault(t *testing.T) {
	state := domain.State{OnHandMicro: 2_000_000, AverageMicro: 100_000_000}

	_, err := wac().Cost(state, costed(t, domain.Issue, 5_000_000, 0), domain.Movement{})
	if err == nil {
		t.Fatal("five units were issued from a stock of two")
	}
	if code := errs.CodeOf(err); code != domain.CodeInsufficient {
		t.Errorf("code = %q, want %q", code, domain.CodeInsufficient)
	}
	// Both figures are named: "not enough stock" is not actionable, "you asked for 5 and have 2"
	// is.
	typed, _ := errs.AsError(err)
	if typed.Params["requested"] != "5000000" || typed.Params["available"] != "2000000" {
		t.Errorf("params = %v, want both quantities", typed.Params)
	}
}

// Where a business genuinely needs it, the shortfall is recorded as a VARIANCE rather than
// absorbed into cost of sales. Absorbing it makes gross margin quietly wrong; recording it
// makes the shortfall visible and postable.
func TestAPermittedOversellRecordsAVisibleVariance(t *testing.T) {
	state := domain.State{OnHandMicro: 2_000_000, AverageMicro: 100_000_000}

	result, err := permissive().Cost(state, costed(t, domain.Issue, 5_000_000, 0),
		domain.Movement{})
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}
	// The whole issue leaves inventory at the average.
	if result.ValueDeltaMinor != -500 {
		t.Errorf("value delta = %d, want -500 (5 × 100)", result.ValueDeltaMinor)
	}
	// Three of the five were never there.
	if result.VarianceMinor != 300 {
		t.Errorf("variance = %d, want 300 (3 × 100) — the part that did not exist",
			result.VarianceMinor)
	}
}

func TestAnIssueWithinStockRecordsNoVariance(t *testing.T) {
	state := domain.State{OnHandMicro: 10_000_000, AverageMicro: 100_000_000}

	result, err := permissive().Cost(state, costed(t, domain.Issue, 3_000_000, 0),
		domain.Movement{})
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}
	if result.VarianceMinor != 0 {
		t.Errorf("variance = %d on an ordinary issue, want 0", result.VarianceMinor)
	}
}

// ── TRAP 3: a return is costed at its ORIGINAL cost (§D.3) ──────────────────────

// The trap with the most expensive consequence. Returning an item sold last year at a cost of
// 100 when today's average is 150 would credit inventory with 150 — inventing 50 of profit out
// of a customer changing their mind.
func TestAReturnIsCostedAtTheOriginalIssueNotTodaysAverage(t *testing.T) {
	// Today's stock is expensive; the item being returned was cheap.
	state := domain.State{OnHandMicro: 10_000_000, AverageMicro: 150_000_000}

	original := costed(t, domain.Issue, 1_000_000, 100_000_000)
	original.ID = id.ID("last-years-sale")

	returned := costed(t, domain.ReturnIn, 1_000_000, 0)
	returned.SourceMovementID = original.ID

	result, err := wac().Cost(state, returned, original)
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}

	if result.UnitCostMicro != 100_000_000 {
		t.Errorf("unit cost = %d, want the original 100000000 — today's average invents profit",
			result.UnitCostMicro)
	}
	// Inventory goes up by what the item actually cost, not by what it would cost today.
	if result.ValueDeltaMinor != 100 {
		t.Errorf("value delta = %d, want 100", result.ValueDeltaMinor)
	}
	// And the return drags the average down, because a cheap item genuinely re-entered stock:
	// (10×150 + 1×100) ÷ 11 = 145.4545…, which rounds DOWN — the fractional part is below a half.
	if result.NewAverageMicro != 145_454_545 {
		t.Errorf("average = %d, want 145454545", result.NewAverageMicro)
	}
}

// Without the source there is no correct cost available, so it is refused rather than costed at
// today's average — the refusal being the whole point of the design.
func TestAReturnWithNoSourceIsRefusedRatherThanCostedAtTheAverage(t *testing.T) {
	state := domain.State{OnHandMicro: 10_000_000, AverageMicro: 150_000_000}

	returned := costed(t, domain.ReturnIn, 1_000_000, 0)
	// deliberately no SourceMovementID

	_, err := wac().Cost(state, returned, domain.Movement{})
	if err == nil {
		t.Fatal("a return with no source was costed")
	}
	if code := errs.CodeOf(err); code != domain.CodeReturnNeedsSource {
		t.Errorf("code = %q, want %q", code, domain.CodeReturnNeedsSource)
	}
}

// ── TRAP 4: allocation ties exactly (§D.3, landed costs) ────────────────────────

// Naive proportion leaves the total off by a few minor units, and those pennies land in nobody's
// account — inventory value and the invoice stop agreeing, by a little, on every shipment.
func TestAllocationAlwaysTiesToTheAmount(t *testing.T) {
	cases := []struct {
		name    string
		amount  int64
		weights []int64
	}{
		{"the classic thirds", 100, []int64{1, 1, 1}},
		{"uneven weights", 1000, []int64{7, 11, 13}},
		{"one line takes it all", 500, []int64{1}},
		{"a line worth nothing", 100, []int64{0, 1, 1}},
		{"large and awkward", 999_999, []int64{3, 5, 7, 11, 13}},
		{"a credit note", -100, []int64{1, 1, 1}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parts, err := domain.Allocate(tc.amount, tc.weights)
			if err != nil {
				t.Fatalf("Allocate: %v", err)
			}
			var total int64
			for _, part := range parts {
				total += part
			}
			if total != tc.amount {
				t.Errorf("parts %v sum to %d, want exactly %d", parts, total, tc.amount)
			}
			if len(parts) != len(tc.weights) {
				t.Errorf("%d parts for %d weights", len(parts), len(tc.weights))
			}
		})
	}
}

// Largest-remainder, so the leftover goes where the fractional part was biggest rather than
// always to the first line.
func TestAllocationGivesTheLeftoverToTheLargestRemainder(t *testing.T) {
	// 100 across 1:1:1 is 33.33 each. The three exact quotients are 33, and one unit is left.
	parts, err := domain.Allocate(100, []int64{1, 1, 1})
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	var thirtyFours, thirtyThrees int
	for _, part := range parts {
		switch part {
		case 34:
			thirtyFours++
		case 33:
			thirtyThrees++
		default:
			t.Errorf("unexpected part %d", part)
		}
	}
	if thirtyFours != 1 || thirtyThrees != 2 {
		t.Errorf("parts = %v, want one 34 and two 33s", parts)
	}
}

// Weighing by value when every line is worth nothing still has to produce something that ties.
func TestAllocationWithNoWeightSpreadsEvenlyAndStillTies(t *testing.T) {
	parts, err := domain.Allocate(10, []int64{0, 0, 0})
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	var total int64
	for _, part := range parts {
		total += part
	}
	if total != 10 {
		t.Errorf("parts %v sum to %d, want 10", parts, total)
	}
}

func TestAllocatingAcrossNothingIsRefused(t *testing.T) {
	if _, err := domain.Allocate(100, nil); err == nil {
		t.Fatal("an amount was allocated across no lines at all")
	}
}

// ── revaluation and counts ──────────────────────────────────────────────────────

func TestARevaluationPostsTheDifferenceInValue(t *testing.T) {
	state := domain.State{OnHandMicro: 10_000_000, AverageMicro: 100_000_000}

	result, err := wac().Cost(state, costed(t, domain.Revaluation, 1, 120_000_000),
		domain.Movement{})
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}
	// 10 units moving from 100 to 120 is 200 more inventory value.
	if result.ValueDeltaMinor != 200 {
		t.Errorf("value delta = %d, want 200", result.ValueDeltaMinor)
	}
	if result.NewAverageMicro != 120_000_000 {
		t.Errorf("average = %d, want 120000000", result.NewAverageMicro)
	}
}

// A count says how MANY there are, not what they cost. Letting it move the average would mean a
// warehouse recount silently restated the valuation.
func TestACountDoesNotMoveTheAverage(t *testing.T) {
	state := domain.State{OnHandMicro: 10_000_000, AverageMicro: 100_000_000}

	counted := costed(t, domain.Count, 2_000_000, 0)
	counted.BalanceAfterMicro = 8_000_000 // two were missing

	result, err := wac().Cost(state, counted, domain.Movement{})
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}
	if result.NewAverageMicro != 100_000_000 {
		t.Errorf("average = %d, want it unchanged", result.NewAverageMicro)
	}
	// Two units at 100 have left the building.
	if result.ValueDeltaMinor != -200 {
		t.Errorf("value delta = %d, want -200", result.ValueDeltaMinor)
	}
}

// ── the port ────────────────────────────────────────────────────────────────────

// Nothing outside the interface knows the method. This asserts the seam exists, so that a later
// FIFO implementation has somewhere to go that is not a rewrite.
func TestTheStrategyIsReachedThroughThePort(t *testing.T) {
	var strategy domain.Strategy = wac()
	if strategy.Key() != "wac" {
		t.Errorf("key = %q, want wac", strategy.Key())
	}

	result, err := strategy.Cost(domain.State{}, costed(t, domain.Receipt, 1_000_000, 100_000_000),
		domain.Movement{})
	if err != nil {
		t.Fatalf("Cost through the port: %v", err)
	}
	if result.NewAverageMicro != 100_000_000 {
		t.Errorf("average = %d through the port", result.NewAverageMicro)
	}
}

// ── precision ───────────────────────────────────────────────────────────────────

// Quantity and cost are both ×10⁶, so their product is ×10¹² and overflows int64 at figures a
// real wholesaler reaches. The 128-bit intermediate is what keeps this honest.
func TestValuationSurvivesQuantitiesThatOverflowSixtyFourBits(t *testing.T) {
	// A million units at a cost of a million minor units: 10⁶×10⁶ × 10⁶×10⁶ = 10²⁴ before the
	// divide, which int64 cannot hold.
	state := domain.State{OnHandMicro: 1_000_000_000_000, AverageMicro: 1_000_000_000_000}

	result, err := wac().Cost(state,
		costed(t, domain.Receipt, 1_000_000_000_000, 1_000_000_000_000), domain.Movement{})
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}
	// Same cost in and out, so the average must not move at all.
	if result.NewAverageMicro != 1_000_000_000_000 {
		t.Errorf("average = %d, want it unchanged — the intermediate overflowed",
			result.NewAverageMicro)
	}
}

// A cost that does not divide evenly is rounded ONCE, so a movement's stored value and the
// ledger's posting cannot differ by a rounding.
func TestAnAwkwardAverageIsRoundedOnce(t *testing.T) {
	// (1×100 + 2×101) ÷ 3 = 100.666… → 100666667 at micro scale, half away from zero.
	state := domain.State{OnHandMicro: 1_000_000, AverageMicro: 100_000_000}

	result, err := wac().Cost(state, costed(t, domain.Receipt, 2_000_000, 101_000_000),
		domain.Movement{})
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}
	if result.NewAverageMicro != 100_666_667 {
		t.Errorf("average = %d, want 100666667", result.NewAverageMicro)
	}
}
