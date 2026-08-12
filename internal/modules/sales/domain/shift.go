package domain

import (
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Stable codes for shifts.
const (
	CodeInvalidShift  = "sales.invalid_shift"
	CodeShiftClosed   = "sales.shift_closed"
	CodeShiftOpen     = "sales.shift_already_open"
	CodeNegativeFloat = "sales.negative_float"
	CodeNegativeCount = "sales.negative_count"
)

// ShiftStatus is whether a till is trading.
type ShiftStatus string

// The shift statuses.
const (
	ShiftOpen   ShiftStatus = "open"
	ShiftClosed ShiftStatus = "closed"
)

// Shift is one till's trading session.
type Shift struct {
	ID       id.ID
	BranchID id.ID
	Terminal string

	OpenedAt          string
	OpenedBy          id.ID
	OpeningFloatMinor int64

	ClosedAt string
	ClosedBy id.ID

	// ExpectedMinor is the float plus every cash payment taken. Derived at close, then FROZEN.
	ExpectedMinor int64
	CountedMinor  int64
	// DifferenceMinor is counted − expected. Negative is short; positive is over.
	DifferenceMinor int64

	Status ShiftStatus
	Notes  string
}

// NewShift opens a till, or refuses.
func NewShift(
	identifier, branchID id.ID, terminal, openedAt string, floatMinor int64,
) (Shift, error) {
	if identifier.IsZero() || branchID.IsZero() {
		return Shift{}, errs.Validation(CodeInvalidShift,
			"a shift needs an identity and a branch")
	}
	if openedAt == "" {
		return Shift{}, errs.Validation(CodeInvalidShift, "a shift needs an opening time")
	}
	if floatMinor < 0 {
		// A negative float is a drawer that owes money before trading starts.
		return Shift{}, errs.Validation(CodeNegativeFloat,
			"an opening float cannot be negative")
	}

	return Shift{
		ID: identifier, BranchID: branchID, Terminal: terminal,
		OpenedAt: openedAt, OpeningFloatMinor: floatMinor, Status: ShiftOpen,
	}, nil
}

// Expected is what should be in the drawer: the float plus the cash taken.
//
// CASH only. A card payment does not put money in the till, and counting it would make every
// shift that took a card look short by exactly that amount — the same distinction the posting
// rules make, here for the same reason.
func Expected(floatMinor, cashTakenMinor int64) int64 {
	return floatMinor + cashTakenMinor
}

// Close reconciles a till, or refuses.
//
// # The difference is RECORDED, never absorbed
//
// A shop's owner learns more from "the till was 300 short on Tuesday" than from any report this
// system produces. A POS that quietly adjusts the expected figure to match the count destroys
// exactly that information, and does it silently — which is why this returns the difference as
// part of the shift rather than hiding it in a correction.
//
// It does NOT refuse a difference. A till is short because somebody miscounted, gave wrong
// change, or took money; refusing to close would leave the shop unable to shut, and the system is
// not the right thing to be deciding which of those it was.
func (s Shift) Close(closedAt string, countedMinor, cashTakenMinor int64) (Shift, error) {
	if s.Status != ShiftOpen {
		return s, errs.Conflict(CodeShiftClosed,
			"this shift is already closed").WithParam("id", string(s.ID))
	}
	if countedMinor < 0 {
		// Nobody counts minus three hundred. This is a typo, and accepting it would post a
		// nonsense difference to the books.
		return s, errs.Validation(CodeNegativeCount,
			"a counted amount cannot be negative")
	}

	expected := Expected(s.OpeningFloatMinor, cashTakenMinor)
	s.ClosedAt = closedAt
	s.ExpectedMinor = expected
	s.CountedMinor = countedMinor
	s.DifferenceMinor = countedMinor - expected
	s.Status = ShiftClosed
	return s, nil
}

// IsShort reports whether the drawer held less than it should have.
func (s Shift) IsShort() bool { return s.DifferenceMinor < 0 }

// Balanced reports whether the count matched.
func (s Shift) Balanced() bool { return s.DifferenceMinor == 0 }
