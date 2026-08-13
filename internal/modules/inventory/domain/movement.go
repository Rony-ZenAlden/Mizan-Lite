// Package domain holds the inventory module's business rules.
//
// The stock ledger first: what a movement is, which way it points, and how a sequence of them
// folds into a level. All pure — no database, no clock — because a valuation that cannot be
// tested against a table is a valuation nobody can defend.
package domain

import (
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Stable error codes. They double as i18n keys and are part of the public contract.
const (
	CodeInvalidMovement   = "inventory.invalid_movement"
	CodeUnknownType       = "inventory.unknown_movement_type"
	CodeNegativeStock     = "inventory.negative_stock"
	CodeNonPositiveQty    = "inventory.non_positive_quantity"
	CodeNegativeCost      = "inventory.negative_cost"
	CodeLedgerMismatch    = "inventory.ledger_mismatch"
	CodeReturnNeedsSource = "inventory.return_needs_source"
)

// Type is what a movement is, and — decisively — which way it points.
//
// # Why direction lives here and not in the sign of the quantity
//
// `quantity_micro` is always positive. A signed quantity makes `SUM(quantity)` meaningful and
// every other query a minefield: "how much did we receive this month" becomes a filtered sum
// that somebody eventually writes without the filter, and the answer looks perfectly plausible.
//
// With direction on the type, a query that forgets to consider it does not compile into
// something wrong — it fails to express anything at all, which is the failure mode to prefer.
type Type string

// The movement types.
const (
	Receipt       Type = "receipt"
	Issue         Type = "issue"
	AdjustmentIn  Type = "adjustment_in"
	AdjustmentOut Type = "adjustment_out"
	TransferOut   Type = "transfer_out"
	TransferIn    Type = "transfer_in"
	Count         Type = "count"
	Revaluation   Type = "revaluation"
	ReturnIn      Type = "return_in"
	// ReturnOut is goods going BACK to a supplier (6.6). The opposite direction to ReturnIn and
	// a different fact, so a different type rather than a negative one.
	ReturnOut Type = "return_out"
)

// Direction is the sign a type contributes to on-hand.
type Direction int

// The directions. Revaluation moves value without moving quantity, which is why it is neither
// in nor out and must be its own case rather than an "in" of zero.
const (
	Outward Direction = -1
	Neutral Direction = 0
	Inward  Direction = 1
)

// directions is the single table mapping a type to its sign.
//
// One table, consulted everywhere. The alternative — a switch in the service, another in the
// verifier, a third in a report — is three places to disagree about whether a count adds or
// replaces, and they will.
var directions = map[Type]Direction{
	Receipt:       Inward,
	ReturnIn:      Inward,
	AdjustmentIn:  Inward,
	TransferIn:    Inward,
	Issue:         Outward,
	ReturnOut:     Outward,
	AdjustmentOut: Outward,
	TransferOut:   Outward,
	Count:         Neutral,
	Revaluation:   Neutral,
}

// DirectionOf reports which way a type moves stock.
func DirectionOf(t Type) (Direction, error) {
	direction, known := directions[t]
	if !known {
		return Neutral, errs.Validation(CodeUnknownType,
			"that is not a kind of stock movement").WithParam("type", string(t))
	}
	return direction, nil
}

// IsInward reports whether a type increases on-hand.
func (t Type) IsInward() bool { return directions[t] == Inward }

// IsOutward reports whether a type decreases on-hand.
func (t Type) IsOutward() bool { return directions[t] == Outward }

// Movement is one entry in the append-only ledger.
type Movement struct {
	ID          id.ID
	WarehouseID id.ID
	ProductID   id.ID
	VariantID   id.ID
	Type        Type

	// QuantityMicro is always positive; Type carries the direction.
	QuantityMicro int64
	// UnitCostMicro is the cost applied to this movement, per unit, in minor units ×10⁶.
	UnitCostMicro int64
	// ValueMinor is what reaches the general ledger: quantity × unit cost, rounded once.
	ValueMinor int64

	BalanceAfterMicro int64
	AverageAfterMicro int64

	// SourceMovementID is the issue a return reverses (§D.3).
	SourceMovementID id.ID

	// LotID and SerialID are set when the product's tracking mode demands them, and must be
	// empty otherwise — see RequireTracking, which enforces both directions.
	LotID    id.ID
	SerialID id.ID

	DocumentType   string
	DocumentID     id.ID
	DocumentLineID id.ID
	ReasonCode     string
	Reason         string
	OccurredAt     string
}

// NewMovement builds a movement, or refuses.
//
// The balance and average are NOT set here: they are what the costing strategy and the fold
// produce, and a constructor that accepted them would let a caller assert a balance the ledger
// does not support.
func NewMovement(
	identifier, warehouseID, productID, variantID id.ID,
	movementType Type, quantityMicro int64,
) (Movement, error) {
	if identifier.IsZero() || warehouseID.IsZero() ||
		productID.IsZero() || variantID.IsZero() {
		return Movement{}, errs.Validation(CodeInvalidMovement,
			"a stock movement needs an identity, a warehouse, and a variant")
	}
	if _, err := DirectionOf(movementType); err != nil {
		return Movement{}, err
	}
	// # A revaluation is the one movement that moves NO quantity
	//
	// It changes what stock is worth without changing how much there is — which is why 4.2 gave
	// it the Neutral direction rather than treating it as an inward move of zero. Its ledger row
	// is not "a movement that did not happen": it is a value change, and the ledger carries
	// value as well as quantity.
	//
	// This exemption was missing until Phase 6.4 tried to write one. The type existed, the
	// direction table knew it, the costing strategy had a `revalue` case — and the constructor
	// refused every one, so the seam could not actually be used. A seam is only proven by a
	// caller.
	if quantityMicro < 0 || (quantityMicro == 0 && movementType != Revaluation) {
		// Zero moves nothing and would still write a ledger row, which is a movement that
		// happened according to the trail and did not according to the stock. Negative is the
		// signed-quantity mistake this design exists to make unrepresentable.
		return Movement{}, errs.Validation(CodeNonPositiveQty,
			"a stock movement must move a positive quantity").
			WithParam("type", string(movementType))
	}

	return Movement{
		ID: identifier, WarehouseID: warehouseID, ProductID: productID, VariantID: variantID,
		Type: movementType, QuantityMicro: quantityMicro,
	}, nil
}

// RequireCostable checks the money side of a movement.
func (m Movement) RequireCostable() error {
	if m.UnitCostMicro < 0 {
		// A negative unit cost would credit inventory on a receipt and make the valuation
		// smaller for having received goods.
		return errs.Validation(CodeNegativeCost,
			"a unit cost cannot be negative").WithParam("type", string(m.Type))
	}
	if m.Type == ReturnIn && m.SourceMovementID.IsZero() {
		// §D.3: a return is costed at its ORIGINAL issue's cost, not today's average.
		// Without the source there is no correct cost to apply, and using the average would
		// invent profit on an item bought at last year's price.
		return errs.Validation(CodeReturnNeedsSource,
			"a return must name the issue it reverses")
	}
	return nil
}

// State is a variant's stock in one warehouse, as the costing strategy sees it.
type State struct {
	OnHandMicro   int64
	ReservedMicro int64
	AverageMicro  int64
}

// AvailableMicro is what may be sold: on hand less what is already promised.
//
// Derived rather than stored, unlike balance_after on a movement: nothing needs its value at a
// past instant, and a third quantity column is a third thing that can disagree with the other
// two.
func (s State) AvailableMicro() int64 { return s.OnHandMicro - s.ReservedMicro }

// Apply folds a movement into a state, returning the state after it.
//
// # The single fold
//
// This is the ONLY place a movement changes a quantity. The service uses it to maintain the
// projection; the verifier uses it to replay the ledger from zero. One function, so a
// projection and its own verification cannot disagree about what a movement means — which they
// would, eventually, if each carried its own switch over the type.
func Apply(state State, m Movement) (State, error) {
	direction, err := DirectionOf(m.Type)
	if err != nil {
		return state, err
	}

	if m.Type == Count {
		// A count REPLACES the on-hand rather than adjusting it. That is what a physical count
		// is: an assertion about reality, not a delta. The difference is recorded as the
		// movement's quantity so the trail shows what changed, but the resulting balance is the
		// counted figure. This is the ONE type the direction table cannot express.
		state.OnHandMicro = m.BalanceAfterMicro
	} else {
		// Everything else is its direction times its quantity — including a REVALUATION, which
		// moves value without moving quantity and needs no special case here because its
		// direction is Neutral and Neutral × anything is nothing.
		//
		// There was a `case Revaluation:` here doing exactly that by hand. A mutation drill
		// deleted it and every test still passed, which is the signal that a second mechanism
		// was restating what the first already said. The direction table is the mechanism; this
		// line is the whole of the arithmetic.
		state.OnHandMicro += int64(direction) * m.QuantityMicro
	}

	if m.AverageAfterMicro > 0 || m.Type == Revaluation {
		state.AverageMicro = m.AverageAfterMicro
	}
	return state, nil
}

// Replay folds a whole ledger from zero.
//
// What the verifier uses. Movements must arrive in occurrence order; the caller owns that,
// because the ordering is a query concern and this stays pure.
func Replay(movements []Movement) (State, error) {
	var state State
	for _, m := range movements {
		next, err := Apply(state, m)
		if err != nil {
			return State{}, err
		}
		state = next
	}
	return state, nil
}

// Discrepancy is one place a projection disagrees with the ledger.
type Discrepancy struct {
	VariantID   id.ID
	WarehouseID id.ID

	ProjectedOnHand  int64
	LedgerOnHand     int64
	ProjectedAverage int64
	LedgerAverage    int64

	// FirstSuspectMovementID is the earliest movement whose recorded balance_after does not
	// match the replay to that point. It is what turns "the total is wrong" into "it went wrong
	// here", which is the difference between a bug report and an investigation.
	FirstSuspectMovementID id.ID
}

// Verify compares a projected level against a replay of the ledger.
//
// # Reports, never repairs
//
// The Phase 2 precedent, and the reason is unchanged: a projection that heals itself hides the
// bug that broke it. The next drift is then silent too, and by the time anybody looks, the
// evidence has been overwritten by the repair.
func Verify(level State, movements []Movement) (Discrepancy, bool, error) {
	ledger, err := Replay(movements)
	if err != nil {
		return Discrepancy{}, false, err
	}
	if level.OnHandMicro == ledger.OnHandMicro && level.AverageMicro == ledger.AverageMicro {
		return Discrepancy{}, false, nil
	}

	discrepancy := Discrepancy{
		ProjectedOnHand: level.OnHandMicro, LedgerOnHand: ledger.OnHandMicro,
		ProjectedAverage: level.AverageMicro, LedgerAverage: ledger.AverageMicro,
	}

	// Walk forward to the first movement whose own recorded balance disagrees with the replay.
	var running State
	for _, m := range movements {
		next, applyErr := Apply(running, m)
		if applyErr != nil {
			return Discrepancy{}, false, applyErr
		}
		if next.OnHandMicro != m.BalanceAfterMicro {
			discrepancy.FirstSuspectMovementID = m.ID
			break
		}
		running = next
	}
	return discrepancy, true, nil
}
