// Package domain holds the owner's PIN, lockout and recovery rules as values. No I/O.
//
// What the owner PIN defends, stated plainly (L1 §7.1): a person at the counter doing what only the owner
// should. It does NOT defend against someone who can open the database file or administer the computer —
// they can change any row directly — and nothing here pretends otherwise.
package domain

import (
	"io"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
)

// Stable error codes. They double as i18n keys.
const (
	// CodeRequired is the refusal every guarded act returns outside owner mode. The frontend answers it
	// with the PIN dialog and retries the act once (D-L1.10).
	CodeRequired          = "lite.owner.required"
	CodeWrongPIN          = "lite.owner.wrong_pin"
	CodeWrongRecoveryCode = "lite.owner.wrong_recovery_code"
	CodeLocked            = "lite.owner.locked"
	CodePINInvalid        = "lite.owner.pin_invalid"
	CodePINWeak           = "lite.owner.pin_weak"
	CodeNotSetUp          = "lite.owner.not_set_up"
	CodeAlreadySetUp      = "lite.owner.already_set_up"
)

// PIN length (Q-L1.2).
const (
	MinPINLength = 6
	MaxPINLength = 12
)

// FieldPIN names the PIN in validation errors.
const FieldPIN = "pin"

// NormalisePIN returns the PIN as Latin digits, or refuses it.
//
// Arabic-Indic digits are accepted: an owner on an Arabic keyboard types ٢٤٦٨١٣. Refused: fewer than 6 or
// more than 12 digits, anything that is not a digit, all one digit, and a straight run up or down — the PINs
// somebody watching the counter guesses first.
func NormalisePIN(raw string) (string, error) {
	pin := numinput.LatinDigits(raw)
	if len(pin) < MinPINLength || len(pin) > MaxPINLength {
		return "", errs.Validation(CodePINInvalid, "a PIN is 6 to 12 digits").
			WithField(FieldPIN, CodePINInvalid, "length").
			WithParam("min", strconv.Itoa(MinPINLength)).WithParam("max", strconv.Itoa(MaxPINLength))
	}
	for _, r := range pin {
		if r < '0' || r > '9' {
			return "", errs.Validation(CodePINInvalid, "a PIN is digits only").
				WithField(FieldPIN, CodePINInvalid, "digits").
				WithParam("min", strconv.Itoa(MinPINLength)).WithParam("max", strconv.Itoa(MaxPINLength))
		}
	}
	if allSame(pin) || straightRun(pin, 1) || straightRun(pin, -1) {
		return "", errs.Validation(CodePINWeak, "that PIN is too easy to guess").WithField(FieldPIN, CodePINWeak, "weak")
	}
	return pin, nil
}

func allSame(s string) bool {
	for i := 1; i < len(s); i++ {
		if s[i] != s[0] {
			return false
		}
	}
	return true
}

// straightRun reports a run such as 123456 (step 1) or 987654 (step -1). Wrapping past 9 or 0 does not count:
// 890123 is not a sequence a person reads as one.
func straightRun(s string, step int) bool {
	for i := 1; i < len(s); i++ {
		if int(s[i])-int(s[i-1]) != step {
			return false
		}
	}
	return true
}

// Lockout (D-L1.7): four consecutive failures are free; the fifth brings a 30-second wait, doubling with each further failure,
// capped at 15 minutes. Never permanent — with no server to lift it, a permanent lock would be a denial of
// service against the owner.
const (
	FreeAttempts = 4
	BaseDelay    = 30 * time.Second
	MaxDelay     = 15 * time.Minute
)

// LockDelay is how long to wait after the given number of consecutive failures.
func LockDelay(failures int) time.Duration {
	if failures <= FreeAttempts {
		return 0
	}
	delay := BaseDelay
	for i := FreeAttempts + 1; i < failures; i++ {
		delay *= 2
		if delay >= MaxDelay {
			return MaxDelay
		}
	}
	return delay
}

// ElevationWindow is how long owner mode lasts after the PIN is entered (Q-L1.3). Counted from entry, not
// extended by activity: a window kept alive by the owner's last act can stay open through an hour of work
// while they step away between two acts.
const ElevationWindow = 2 * time.Minute

// Recovery codes (L1 §7.5): 16 characters from an alphabet without look-alikes (no 0/O, 1/I/L), shown as
// XXXX-XXXX-XXXX-XXXX. 31 symbols × 16 positions ≈ 79 bits.
const (
	RecoveryAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"
	RecoveryLength   = 16
)

// NewRecoveryCode draws a code from random, uniformly: each character is an unbiased index into the
// alphabet (crypto/rand.Int rejection-samples), never a byte reduced modulo 31.
func NewRecoveryCode(random io.Reader) (string, error) {
	var b strings.Builder
	limit := big.NewInt(int64(len(RecoveryAlphabet)))
	for i := 0; i < RecoveryLength; i++ {
		if i > 0 && i%4 == 0 {
			b.WriteByte('-')
		}
		n, err := randInt(random, limit)
		if err != nil {
			return "", errs.Wrap(err, errs.CategoryInternal, CodeNotSetUp, "drawing a recovery code")
		}
		b.WriteByte(RecoveryAlphabet[n])
	}
	return b.String(), nil
}

// CanonicalRecoveryCode is the form a code is hashed and compared in: upper case, no dashes or spaces.
// A person copying XXXX-XXXX from paper may type it in lower case or without the dashes.
func CanonicalRecoveryCode(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(raw) {
		if r == '-' || r == ' ' || r == '\t' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// EventKind is what an owner event records.
type EventKind string

// Owner event kinds, as the owner_events CHECK constraint lists them.
const (
	EventPINSet          EventKind = "pin_set"
	EventPINChanged      EventKind = "pin_changed"
	EventElevated        EventKind = "elevated"
	EventElevationFailed EventKind = "elevation_failed"
	EventLockedOut       EventKind = "locked_out"
	EventRecovered       EventKind = "recovered"
	EventElevationEnded  EventKind = "elevation_ended"
	EventGuardedAct      EventKind = "guarded_act"
)
