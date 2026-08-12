package domain

import (
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// Stable codes for PINs.
const (
	CodeInvalidPIN     = "identity.invalid_pin"
	CodeWeakPIN        = "identity.weak_pin"
	CodePINNotSet      = "identity.pin_not_set"
	CodeDeviceRequired = "identity.pin_needs_device"
	CodePINScope       = "identity.pin_scope"
)

// AuthMethod is how a session was authenticated.
type AuthMethod string

// The authentication methods.
const (
	// AuthPassword is a full session: everything the user's roles grant.
	AuthPassword AuthMethod = "password"
	// AuthPIN is a till session: the user's grants INTERSECTED with what a PIN may hold.
	AuthPIN AuthMethod = "pin"
)

// PIN length bounds.
//
// Four is the shortest a shop will tolerate typing dozens of times a shift, and twelve is past
// the point where anybody would. The floor is not what makes a PIN safe — scoping and throttling
// are — but a two-digit PIN is a hundred guesses, which no throttle can make reasonable.
const (
	MinPINLength = 4
	MaxPINLength = 12
)

// ValidatePIN refuses a PIN nobody should be allowed to choose.
//
// # Why refuse anything at all, given the real protection is elsewhere
//
// Because the common PINs are tried FIRST. Throttling buys time against exhaustive search; it
// buys nothing against an attacker who tries 1234 on every till in the shop. Refusing the
// handful that people actually pick is worth more than a longer minimum length.
func ValidatePIN(pin string) error {
	pin = strings.TrimSpace(pin)

	if len(pin) < MinPINLength || len(pin) > MaxPINLength {
		return errs.Validation(CodeInvalidPIN,
			"a PIN must be between four and twelve digits").
			WithField("pin", CodeInvalidPIN, "length")
	}
	for _, r := range pin {
		if r < '0' || r > '9' {
			// Digits only: a till keypad has no letters, and a PIN somebody cannot enter at the
			// till is a PIN they will write on a note stuck to the screen.
			return errs.Validation(CodeInvalidPIN, "a PIN must be digits only").
				WithField("pin", CodeInvalidPIN, "digits")
		}
	}

	if allSameDigit(pin) {
		return errs.Validation(CodeWeakPIN,
			"that PIN is too easy to guess").WithField("pin", CodeWeakPIN, "repeated")
	}
	if isSequential(pin) {
		return errs.Validation(CodeWeakPIN,
			"that PIN is too easy to guess").WithField("pin", CodeWeakPIN, "sequential")
	}
	return nil
}

func allSameDigit(pin string) bool {
	for i := 1; i < len(pin); i++ {
		if pin[i] != pin[0] {
			return false
		}
	}
	return true
}

// isSequential catches 1234 and 4321, in both directions.
//
// Not a dictionary of "common PINs": such a list is a maintenance burden that is always out of
// date, and the two patterns below are what people actually choose when a keypad is in front of
// them.
func isSequential(pin string) bool {
	ascending, descending := true, true
	for i := 1; i < len(pin); i++ {
		if pin[i] != pin[i-1]+1 {
			ascending = false
		}
		if pin[i] != pin[i-1]-1 {
			descending = false
		}
	}
	return ascending || descending
}

// POSPermissionPrefixes are the permission namespaces a PIN session may hold.
//
// # The bound that makes a short credential acceptable
//
// A PIN session holds the INTERSECTION of the user's grants and this list. A manager with every
// permission in the system who signs in by PIN gets a till and nothing else — so guessing a PIN
// buys an attacker the ability to sell things, not to change prices, edit stock, or read the
// ledger.
//
// Deliberately a list of PREFIXES rather than a role: a role is data an administrator can edit,
// and the whole point of this bound is that it is not editable from inside the application. A
// shop that wants its cashiers to do more grants them a fuller session with a password.
var POSPermissionPrefixes = []string{
	"sales.document.",
	"pos.",
	"catalog.view",
	"partner.customer.view",
	"pricing.view",
	"inventory.stock.view",
}

// PINMayHold reports whether a permission is within the point-of-sale bound.
func PINMayHold(permission string) bool {
	for _, prefix := range POSPermissionPrefixes {
		if permission == prefix || strings.HasPrefix(permission, prefix) {
			return true
		}
	}
	return false
}
