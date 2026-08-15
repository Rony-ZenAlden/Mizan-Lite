// Package settle holds the arithmetic every settleable document shares.
//
// # Why this exists, and why it is arithmetic and not a table
//
// Three modules now settle documents with payments: sales settles invoices, purchasing settles
// bills, and expenses settles what is owed on account. Each keeps its OWN tables, and that is
// deliberate — Phases 5 and 6 each rejected a shared payments table twice and independently, with
// the same argument:
//
//	"One table would need a nullable foreign key to each document type and a CHECK that exactly
//	 one is set — a discriminated union hand-rolled in SQL, with a direction filter on every query
//	 that somebody eventually writes without."
//
// That argument is about DATA and it still holds: a real foreign key to a real table is worth
// more than one fewer table.
//
// The ARITHMETIC is a different question, and it had been copied twice by the time a third caller
// appeared. Phase 6 recorded the rule that applies: **the moment a fact needs a third home, the
// second home was the wrong one.** So the rules move here — the same resolution `round.Allocate`
// got, for the same reason.
//
// # What is shared and what is not
//
// Shared: what over-allocation means, what "outstanding" means, and that neither can go negative.
// Not shared: which documents exist, who may settle them, and where the money lands. Those are
// each module's business and always were.
package settle

import (
	"errors"
	"strconv"
)

// Errors this package refuses with.
//
// Sentinel errors rather than coded ones, like the rest of `kernel`: this layer sits below the
// vocabulary of categories and codes, and reaching up for them would invert the dependency the
// layering exists to keep. Each caller translates into its own module's codes, where the code is
// the contract a screen renders.
var (
	// ErrNonPositive is an allocation of nothing or less.
	ErrNonPositive = errors.New("settle: an allocation must be for a positive amount")
	// ErrOverAllocated is allocating more than the payment is worth.
	ErrOverAllocated = errors.New("settle: that allocates more than the payment is worth")
	// ErrOverSettled is settling more than a document is for.
	ErrOverSettled = errors.New("settle: that settles more than the document is for")
)

// Allocation is one payment settling part of one document.
//
// Deliberately NOT carrying a document identifier: this package does not know what a document is,
// and a type here with a `DocumentID` would be inviting somebody to make it point at four
// different tables. Callers keep their own allocation types with real foreign keys and pass the
// amounts.
type Allocation struct {
	AmountMinor int64
}

// Total is what a set of allocations comes to.
func Total(allocations []Allocation) int64 {
	var total int64
	for _, allocation := range allocations {
		total += allocation.AmountMinor
	}
	return total
}

// RequireAllocatable checks that a payment can cover what it is being asked to settle.
//
// # Over-allocation is refused; under-allocation is not
//
// Allocating more than the payment is arithmetic that cannot be true: the money does not exist.
//
// Allocating LESS is a deposit or a prepayment with a balance still to assign, which is ordinary —
// a customer paying a round figure, a business paying a supplier's statement to the nearest note.
// Forcing them to balance would mean inventing an allocation against a document that may not
// exist yet.
func RequireAllocatable(paymentMinor int64, allocations []Allocation) error {
	for _, allocation := range allocations {
		if allocation.AmountMinor <= 0 {
			return ErrNonPositive
		}
	}
	if Total(allocations) > paymentMinor {
		return ErrOverAllocated
	}
	return nil
}

// RequireSettleable checks that a document can take one more allocation.
//
// Paying more than a document is for is not generosity: it is a keying error that leaves the
// payable or receivable overdrawn and reconciles against nothing.
func RequireSettleable(totalMinor, alreadySettledMinor, addingMinor int64) error {
	if addingMinor <= 0 {
		return ErrNonPositive
	}
	if alreadySettledMinor+addingMinor > totalMinor {
		return ErrOverSettled
	}
	return nil
}

// Outstanding is what a document still owes.
//
// # Derived, never stored
//
// A maintained `paid_minor` column drifts from the allocations that justify it, and the drift is
// invisible: it shows up as a customer chased for money they had paid, or a supplier paid twice.
//
// Never NEGATIVE. An over-settled document is a fault to investigate, not a debt owed back — and
// a negative outstanding would net against another document in any sum, hiding both.
func Outstanding(totalMinor, settledMinor int64) int64 {
	remaining := totalMinor - settledMinor
	if remaining < 0 {
		return 0
	}
	return remaining
}

// IsSettled reports whether nothing more is owed.
func IsSettled(totalMinor, settledMinor int64) bool {
	return Outstanding(totalMinor, settledMinor) == 0
}

// Itoa renders an amount for an error parameter.
//
// Here so a caller translating one of this package's errors can name the numbers without
// importing strconv for one call.
func Itoa(v int64) string { return strconv.FormatInt(v, 10) }
