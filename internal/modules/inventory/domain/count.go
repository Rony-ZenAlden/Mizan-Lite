package domain

import (
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Stable codes for stock counts.
const (
	CodeInvalidCount     = "inventory.invalid_count"
	CodeCountNotOpen     = "inventory.count_not_open"
	CodeCountNotInReview = "inventory.count_not_in_review"
	CodeCountTerminal    = "inventory.count_terminal"
	CodeLineNotCounted   = "inventory.line_not_counted"
	CodeNegativeCounted  = "inventory.negative_counted"
)

// CountStatus is where a count has got to.
type CountStatus string

// The count statuses.
const (
	CountDraft     CountStatus = "draft"
	CountCounting  CountStatus = "counting"
	CountReview    CountStatus = "review"
	CountApplied   CountStatus = "applied"
	CountCancelled CountStatus = "cancelled"
)

// IsTerminal reports whether a count can still change.
func (s CountStatus) IsTerminal() bool {
	return s == CountApplied || s == CountCancelled
}

// StockCount is a physical stock count.
//
// Named StockCount rather than Count because `Count` is already a movement TYPE in this package
// — the third collision of this shape in the codebase, after Auditable's EventType (1.7) and the
// i18n catalogue versus the catalog module (3.1). Each time the answer has been the same: rename
// the newcomer to something that says what it is, rather than shortening the incumbent.
type StockCount struct {
	ID          id.ID
	WarehouseID id.ID
	Reference   string
	Description string
	Status      CountStatus
	// IsBlind hides the expected quantity from whoever is counting. The single most valuable
	// control here: showing the expectation is how a count comes back agreeing with the system
	// on every line while the shelves say otherwise.
	IsBlind    bool
	SnapshotAt string
	AppliedAt  string
}

// NewCount builds a count, or refuses.
//
// Blind by DEFAULT. A count that shows the counter what to expect is barely a count, and making
// the safe mode the one you get by doing nothing is the same reasoning that turns tax off and
// negative stock off by default.
func NewCount(identifier, warehouseID id.ID, reference string) (StockCount, error) {
	reference = strings.TrimSpace(reference)

	if identifier.IsZero() || warehouseID.IsZero() {
		return StockCount{}, errs.Validation(CodeInvalidCount,
			"a stock count needs an identity and a warehouse")
	}
	if reference == "" {
		return StockCount{}, errs.Validation(CodeInvalidCount, "a stock count needs a reference").
			WithField("reference", CodeInvalidCount, "required")
	}
	return StockCount{
		ID: identifier, WarehouseID: warehouseID, Reference: reference,
		Status: CountDraft, IsBlind: true,
	}, nil
}

// CountLine is one thing counted.
type CountLine struct {
	ID        id.ID
	ProductID id.ID
	VariantID id.ID
	LotID     id.ID

	// ExpectedMicro is what the system believed AT THE SNAPSHOT, frozen and never refreshed.
	ExpectedMicro int64
	// CountedMicro is what was on the shelf. Nil until somebody counts it — which is NOT the
	// same as zero.
	CountedMicro *int64
	Note         string
}

// IsCounted reports whether a figure has been entered.
//
// The distinction this exists for: an uncounted line must not be applied as "there are none",
// which would write off the entire stock of anything the counter did not reach. A zero that was
// deliberately entered is a real finding; a zero that means "nobody looked" is not.
func (l CountLine) IsCounted() bool { return l.CountedMicro != nil }

// VarianceMicro is what the counter found, less what the sheet said.
func (l CountLine) VarianceMicro() int64 {
	if !l.IsCounted() {
		return 0
	}
	return *l.CountedMicro - l.ExpectedMicro
}

// BeginCounting freezes the expectations and sends the sheet out.
func (c StockCount) BeginCounting(snapshotAt string) (StockCount, error) {
	if c.Status != CountDraft {
		return c, errs.Conflict(CodeCountNotOpen,
			"only a draft count can be started").WithParam("status", string(c.Status))
	}
	c.Status = CountCounting
	c.SnapshotAt = snapshotAt
	return c, nil
}

// SubmitForReview closes entry and opens the variance review.
func (c StockCount) SubmitForReview() (StockCount, error) {
	if c.Status != CountCounting {
		return c, errs.Conflict(CodeCountNotOpen,
			"only a count in progress can be submitted").WithParam("status", string(c.Status))
	}
	c.Status = CountReview
	return c, nil
}

// Cancel abandons a count, leaving no movements.
func (c StockCount) Cancel() (StockCount, error) {
	if c.Status.IsTerminal() {
		return c, errs.Conflict(CodeCountTerminal,
			"this count is finished and cannot be changed").
			WithParam("status", string(c.Status))
	}
	c.Status = CountCancelled
	return c, nil
}

// Adjustment is one movement an applied count will make.
type Adjustment struct {
	Line          CountLine
	VarianceMicro int64
	// BalanceAfterMicro is what on-hand BECOMES: the current balance plus the variance — not the
	// counted figure. See Plan.
	BalanceAfterMicro int64
}

// CountPlan is what applying a count would do.
type CountPlan struct {
	Adjustments []Adjustment
	// Unchanged counts the lines whose count matched the sheet, so a review screen can say
	// "412 agreed, 6 did not" rather than showing six rows with no context.
	Unchanged int
	// Uncounted counts the lines nobody reached. They are LEFT ALONE, never written off.
	Uncounted int
}

// PlanCount works out the movements an applied count would make.
//
// # The variance is applied, not the counted figure
//
// This is the correctness point of the whole step.
//
// A count takes hours. Stock moves during it: a sale ships, a delivery arrives. If applying the
// count SET the balance to the counted figure, every one of those movements would be silently
// undone — a sale made at 11am would vanish when the count was applied at 4pm, and the ledger
// would show stock that had already left the building.
//
// So the count contributes what the counter actually established: a DIFFERENCE between what the
// sheet said and what was on the shelf. Two fewer than expected is two fewer, whatever else
// happened meanwhile. `current` is read at apply time and the variance is added to it.
//
// # An uncounted line is not a zero
//
// Lines nobody reached are left alone. Treating them as zero would write off the entire stock of
// everything the counter did not get to, which on a partial count is most of the warehouse.
func PlanCount(lines []CountLine, current map[id.ID]int64) CountPlan {
	plan := CountPlan{Adjustments: make([]Adjustment, 0, len(lines))}

	for _, line := range lines {
		if !line.IsCounted() {
			plan.Uncounted++
			continue
		}
		variance := line.VarianceMicro()
		if variance == 0 {
			plan.Unchanged++
			continue
		}
		plan.Adjustments = append(plan.Adjustments, Adjustment{
			Line:          line,
			VarianceMicro: variance,
			// The CURRENT balance plus the variance — never the counted figure.
			BalanceAfterMicro: current[line.VariantID] + variance,
		})
	}
	return plan
}

// RequireApplicable refuses to apply a count that is not ready.
//
// Only from REVIEW. A count applied straight from counting has had nobody look at the variances,
// and the variances are the entire product of the exercise: a line reading 3 where 300 was
// expected is a miscount far more often than it is a theft, and applying it unseen writes off
// stock that is sitting on the shelf.
func (c StockCount) RequireApplicable() error {
	if c.Status.IsTerminal() {
		return errs.Conflict(CodeCountTerminal,
			"this count has already been finished").WithParam("status", string(c.Status))
	}
	if c.Status != CountReview {
		return errs.Conflict(CodeCountNotInReview,
			"a count is applied from review, once its variances have been looked at").
			WithParam("status", string(c.Status))
	}
	return nil
}

// Applied marks a count finished.
func (c StockCount) Applied(at string) StockCount {
	c.Status = CountApplied
	c.AppliedAt = at
	return c
}
