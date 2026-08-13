package round

import (
	"errors"
	"math/big"
)

// Errors an allocation can refuse with.
//
// Plain sentinel errors rather than coded ones: `kernel` sits below `errs`' vocabulary of
// categories and codes, and a kernel package that reached for module error codes would invert the
// dependency the whole layering exists to keep.
var (
	// ErrNoWeights is an allocation across nothing.
	ErrNoWeights = errors.New("round: nothing to allocate across")
	// ErrNegativeWeight is a weight below zero, which has no meaning as a share.
	ErrNegativeWeight = errors.New("round: an allocation weight cannot be negative")
	// ErrAllocationOverflow is a part too large to hold in an int64.
	ErrAllocationOverflow = errors.New("round: that allocation is too large to represent")
)

// Allocate splits an amount across weights so the parts sum to EXACTLY the amount.
//
// # Largest remainder, and why nothing else will do
//
// Naive proportional division leaves the parts summing to a little less than the whole, and those
// missing units land in nobody's account. Over a year of shipments, sales, and tax lines it is
// not a rounding difference — it is a reconciliation nobody can close.
//
// Each part takes its exact quotient; the leftover units go one at a time to the largest
// fractional remainders, each recipient dropping out afterwards so no single line collects them
// all. The sum is the amount, exactly, always.
//
// # Why this lives in the kernel
//
// It was written four times before it was written once. `money.Money.Allocate` had it for
// currency amounts (Phase 0), `inventory/domain.Allocate` for landed costs (Phase 4),
// `printing.Widths` for column shares (Phase 5), and Phase 6 needed a fourth for freight — at
// which point "look for the one an earlier phase already left" stopped meaning *find the copy*
// and started meaning *stop making copies*.
//
// Neither of the first two could be reused where the others needed it: one requires a currency,
// the other lives in a module that platform and other modules must not import. The kernel is the
// one place every layer can reach.
//
// # Zero weights
//
// All-zero weights mean there is nothing to weigh by, and spreading evenly is the only defensible
// answer. It still goes through the remainder pass, so it still ties exactly.
func Allocate(amount int64, weights []int64) ([]int64, error) {
	if len(weights) == 0 {
		return nil, ErrNoWeights
	}

	// A COPY, because the caller's slice is not ours to rewrite. The original of this function
	// mutated its argument to implement the even-spread case, so a caller who passed a slice it
	// still needed got it back full of ones.
	shares := make([]int64, len(weights))
	var total int64
	for i, weight := range weights {
		if weight < 0 {
			return nil, ErrNegativeWeight
		}
		shares[i] = weight
		total += weight
	}
	if total == 0 {
		for i := range shares {
			shares[i] = 1
		}
		total = int64(len(shares))
	}

	out := make([]int64, len(shares))
	remainders := make([]*big.Int, len(shares))
	var allocated int64

	for i, weight := range shares {
		numerator := new(big.Int).Mul(big.NewInt(amount), big.NewInt(weight))
		quotient, rest := new(big.Int).QuoRem(numerator, big.NewInt(total), new(big.Int))
		if !quotient.IsInt64() {
			return nil, ErrAllocationOverflow
		}
		out[i] = quotient.Int64()
		allocated += out[i]
		remainders[i] = new(big.Int).Abs(rest)
	}

	// Hand the leftover units to the largest fractional parts, biggest first. Each recipient is
	// then taken out of the running, so a single line cannot collect them all.
	leftover := amount - allocated
	step := int64(1)
	if leftover < 0 {
		step, leftover = -1, -leftover
	}
	for n := int64(0); n < leftover; n++ {
		best := -1
		for i, value := range remainders {
			if value.Sign() < 0 {
				continue
			}
			if best == -1 || value.Cmp(remainders[best]) > 0 {
				best = i
			}
		}
		if best == -1 {
			// Every line has already taken a unit. Only reachable when the leftover exceeds the
			// number of lines, which the arithmetic above forbids — but returning silently short
			// would be worse than stopping.
			break
		}
		out[best] += step
		remainders[best] = big.NewInt(-1)
	}
	return out, nil
}
