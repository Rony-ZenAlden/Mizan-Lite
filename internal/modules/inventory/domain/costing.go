package domain

import (
	"math/big"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/round"
)

// Stable codes for costing.
const (
	CodeUnknownStrategy = "inventory.unknown_costing_method"
	CodeInsufficient    = "inventory.insufficient_stock"
)

// quantityScale is the scale a quantity is held at: 10⁶.
const quantityScale = 1_000_000

// Strategy is how a movement is costed (§D.1).
//
// # Why this is a port with one implementation
//
// The temptation is to write weighted-average inline and "extract an interface when FIFO
// arrives". That fails for a specific reason: the call sites are what would have to change, and
// by the time FIFO is asked for there are call sites in sales, purchasing, adjustments,
// transfers, counts, and three reports.
//
// Nothing outside this interface knows which method is in use. Sales asks for the cost of an
// issue; it does not know or care whether the answer came from an average or a layer. That is
// what makes switching a company to FIFO a configuration change plus a recompute rather than a
// redesign — the thing §33.3 asked for.
//
// Pure, like every other calculator in this codebase: no database, no clock. The service loads
// the state, calls the strategy, and writes what it returns.
type Strategy interface {
	// Key identifies the method, and is what a company setting stores.
	Key() string

	// Cost prices one movement against the state before it.
	//
	// `original` is the movement a return reverses, and is the zero value otherwise. It is a
	// parameter rather than something the strategy fetches, because a strategy that could
	// fetch would need a database and would stop being testable against a table.
	Cost(state State, m Movement, original Movement) (Result, error)
}

// Result is what costing decides about a movement.
type Result struct {
	// UnitCostMicro is the cost applied to this movement.
	UnitCostMicro int64
	// NewAverageMicro is the resulting average, which becomes the level's.
	NewAverageMicro int64
	// ValueDeltaMinor is the change in inventory VALUE, signed: positive when stock is worth
	// more afterwards. This is what drives the GL posting — and it is all inventory contributes
	// to it. Which accounts it lands in is Phase 2's table-driven decision (§20.3), and this
	// module never learns them.
	ValueDeltaMinor int64
	// VarianceMinor is value that could not be accounted for: an issue taken from stock that
	// was not there. Recorded rather than absorbed, so it is visible and postable.
	VarianceMinor int64
}

// Options configures a costing run.
type Options struct {
	// AllowNegativeStock permits an issue that takes on-hand below zero.
	//
	// Blocked by DEFAULT (§2.5). Some businesses genuinely need it — a workshop that issues
	// components before the delivery note is entered — and most should not have it, because the
	// failure it permits is selling what does not exist.
	AllowNegativeStock bool
}

// WAC is moving weighted average cost — the v1 default (§21.1).
//
// Simple to explain to a shop owner, cheap to compute, and immune to the layer-tracking
// complexity of FIFO. Layers are still written on every receipt (§D.2); this strategy simply
// does not read them.
type WAC struct{ Options Options }

var _ Strategy = WAC{}

// Key identifies the method.
func (w WAC) Key() string { return "wac" }

// Cost prices one movement.
//
// The four cases §D.3 names as the ones naive implementations get wrong are each handled
// explicitly below, and each has a named test.
func (w WAC) Cost(state State, m Movement, original Movement) (Result, error) {
	if err := m.RequireCostable(); err != nil {
		return Result{}, err
	}

	switch m.Type {
	case Receipt, AdjustmentIn, TransferIn:
		return w.inward(state, m)
	case ReturnIn:
		return w.returned(state, m, original)
	case Issue, AdjustmentOut, TransferOut:
		return w.outward(state, m)
	case Revaluation:
		return w.revalue(state, m)
	case Count:
		return w.counted(state, m)
	default:
		return Result{}, errs.Validation(CodeUnknownType,
			"that is not a kind of stock movement").WithParam("type", string(m.Type))
	}
}

// inward folds a receipt into the average.
//
//	new = (on_hand × average + received × cost) ÷ (on_hand + received)
func (w WAC) inward(state State, m Movement) (Result, error) {
	value := valueOf(m.QuantityMicro, m.UnitCostMicro)

	// TRAP 1 (§D.3): zero or negative on-hand at receipt.
	//
	// The two halves of this guard are NOT doing the same amount of work, and a mutation drill
	// is what established which is which:
	//
	//   - on_hand == 0 → the general formula already gives the receipt cost, because the first
	//     term vanishes: (0×avg + qty×cost) ÷ qty = cost, for any stale average whatsoever. This
	//     half is DEFENSIVE, not load-bearing. It is kept because it states the intent plainly
	//     and would survive a change to the formula, but no test can distinguish it.
	//
	//   - on_hand < 0 → the average it carries is a fiction (there was nothing there to have a
	//     cost) and the divisor is smaller than the received quantity. Receiving 10 at 120 into
	//     a stock of −5 carrying a phantom average of 999 yields −759 rather than 120. THIS is
	//     the half that matters, and it has its own test.
	if state.OnHandMicro <= 0 {
		return Result{
			UnitCostMicro: m.UnitCostMicro, NewAverageMicro: m.UnitCostMicro,
			ValueDeltaMinor: value,
		}, nil
	}

	// 128-bit intermediate: on-hand and average are both scaled by 10⁶, so their product is
	// scaled by 10¹² and overflows int64 at quantities and costs a real wholesaler reaches.
	existing := new(big.Int).Mul(
		big.NewInt(state.OnHandMicro), big.NewInt(state.AverageMicro))
	incoming := new(big.Int).Mul(
		big.NewInt(m.QuantityMicro), big.NewInt(m.UnitCostMicro))
	total := new(big.Int).Add(existing, incoming)

	quantity := state.OnHandMicro + m.QuantityMicro
	average, err := divRound(total, quantity)
	if err != nil {
		return Result{}, err
	}

	return Result{
		UnitCostMicro: m.UnitCostMicro, NewAverageMicro: average, ValueDeltaMinor: value,
	}, nil
}

// outward issues at the current average.
func (w WAC) outward(state State, m Movement) (Result, error) {
	// TRAP 2 (§D.3): an issue larger than what is on hand.
	if m.QuantityMicro > state.OnHandMicro {
		if !w.Options.AllowNegativeStock {
			return Result{}, errs.Conflict(CodeInsufficient,
				"there is not enough stock to issue that quantity").
				WithParam("requested", itoa(m.QuantityMicro)).
				WithParam("available", itoa(state.OnHandMicro))
		}
		// Permitted, so it proceeds — but the part that was not there is recorded as a VARIANCE
		// rather than absorbed into cost of sales. Absorbing it would make gross margin quietly
		// wrong; recording it makes the shortfall visible and postable.
		shortfall := m.QuantityMicro - maxInt64(state.OnHandMicro, 0)
		return Result{
			UnitCostMicro:   state.AverageMicro,
			NewAverageMicro: state.AverageMicro,
			ValueDeltaMinor: -valueOf(m.QuantityMicro, state.AverageMicro),
			VarianceMinor:   valueOf(shortfall, state.AverageMicro),
		}, nil
	}

	// An issue does not change the average — that is the defining property of a moving average,
	// and getting it wrong is how a valuation drifts on every single sale.
	return Result{
		UnitCostMicro:   state.AverageMicro,
		NewAverageMicro: state.AverageMicro,
		ValueDeltaMinor: -valueOf(m.QuantityMicro, state.AverageMicro),
	}, nil
}

// returned costs a return at its ORIGINAL issue's cost (§D.3).
//
// TRAP 3, and the one with the most expensive consequence. Returning an item sold last year at a
// cost of 100 when today's average is 150 would credit inventory with 150 — inventing 50 of
// profit out of a customer changing their mind. Over a year of returns that is a margin figure
// nobody can reconcile and nobody can explain.
//
// This is why `source_movement_id` exists on the movement and why the return document line must
// carry its source line, designed in from the start rather than retrofitted: every historical
// return without one has no correct cost available, ever.
func (w WAC) returned(state State, m Movement, original Movement) (Result, error) {
	cost := original.UnitCostMicro
	value := valueOf(m.QuantityMicro, cost)

	// Same trap-1 reasoning: with nothing on hand there is no average to blend into.
	if state.OnHandMicro <= 0 {
		return Result{
			UnitCostMicro: cost, NewAverageMicro: cost, ValueDeltaMinor: value,
		}, nil
	}

	existing := new(big.Int).Mul(
		big.NewInt(state.OnHandMicro), big.NewInt(state.AverageMicro))
	incoming := new(big.Int).Mul(big.NewInt(m.QuantityMicro), big.NewInt(cost))
	total := new(big.Int).Add(existing, incoming)

	average, err := divRound(total, state.OnHandMicro+m.QuantityMicro)
	if err != nil {
		return Result{}, err
	}
	return Result{
		UnitCostMicro: cost, NewAverageMicro: average, ValueDeltaMinor: value,
	}, nil
}

// revalue moves value without moving quantity.
func (w WAC) revalue(state State, m Movement) (Result, error) {
	before := valueOf(state.OnHandMicro, state.AverageMicro)
	after := valueOf(state.OnHandMicro, m.UnitCostMicro)

	return Result{
		UnitCostMicro: m.UnitCostMicro, NewAverageMicro: m.UnitCostMicro,
		ValueDeltaMinor: after - before,
	}, nil
}

// counted prices the difference a physical count found.
//
// The average is untouched: a count says how MANY there are, not what they cost. Letting a count
// move the average would mean a warehouse recount silently restated the valuation.
func (w WAC) counted(state State, m Movement) (Result, error) {
	difference := m.BalanceAfterMicro - state.OnHandMicro
	return Result{
		UnitCostMicro:   state.AverageMicro,
		NewAverageMicro: state.AverageMicro,
		ValueDeltaMinor: valueOf(difference, state.AverageMicro),
	}, nil
}

// valueOf turns a scaled quantity and a scaled unit cost into whole minor units.
//
// Both inputs are ×10⁶, so their product is ×10¹² and the division is by 10¹² — which overflows
// int64 well before a real wholesaler's numbers do. Rounded ONCE, here, so that a movement's
// stored value and the ledger's posting cannot differ by a rounding.
func valueOf(quantityMicro, unitCostMicro int64) int64 {
	product := new(big.Int).Mul(big.NewInt(quantityMicro), big.NewInt(unitCostMicro))
	divisor := big.NewInt(quantityScale * quantityScale)

	quotient, _ := divRoundBig(product, divisor)
	return quotient
}

// divRound divides a 128-bit numerator by an int64, half away from zero.
func divRound(numerator *big.Int, denominator int64) (int64, error) {
	if denominator == 0 {
		return 0, errs.Internal(CodeInvalidMovement,
			"a costing division had no quantity to divide by")
	}
	value, ok := divRoundBig(numerator, big.NewInt(denominator))
	if !ok {
		return 0, errs.Internal(CodeInvalidMovement,
			"that valuation is too large to represent")
	}
	return value, nil
}

// divRoundBig rounds half away from zero, matching every other rounding a user sees.
func divRoundBig(numerator, denominator *big.Int) (int64, bool) {
	quotient, remainder := new(big.Int).QuoRem(numerator, denominator, new(big.Int))

	twice := new(big.Int).Abs(remainder)
	twice.Lsh(twice, 1)
	if twice.CmpAbs(denominator) >= 0 {
		if (numerator.Sign() < 0) != (denominator.Sign() < 0) {
			quotient.Sub(quotient, big.NewInt(1))
		} else {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	if !quotient.IsInt64() {
		return 0, false
	}
	return quotient.Int64(), true
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}

// Allocate spreads an amount across weights using largest-remainder, so the parts tie exactly
// to the whole (§D.3, landed costs).
//
// TRAP 4. Allocating freight across a receipt's lines by naive proportion leaves the total off
// by a few minor units, and those pennies land in nobody's account — the inventory value and the
// invoice stop agreeing, by a little, on every single shipment.
//
// Largest-remainder gives the leftover units to the lines with the largest fractional parts, so
// the sum is the amount, exactly, always.
func Allocate(amountMinor int64, weights []int64) ([]int64, error) {
	parts, err := round.Allocate(amountMinor, weights)
	if err != nil {
		// The kernel speaks in sentinel errors, because it sits below the coded-error
		// vocabulary. Translated here, where the module's codes are the contract.
		return nil, errs.Validation(CodeInvalidMovement, err.Error())
	}
	return parts, nil
}
