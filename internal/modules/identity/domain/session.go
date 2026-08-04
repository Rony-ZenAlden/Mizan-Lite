package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Session-related error codes.
const (
	CodeSessionInvalid  = "identity.session_invalid"
	CodeSessionExpired  = "identity.session_expired"
	CodeTooManyAttempts = "identity.too_many_attempts"
	CodeTokenGeneration = "identity.token_generation_failed"
)

// End reasons.
const (
	EndLogout   = "logout"
	EndIdle     = "idle"
	EndAbsolute = "absolute"
	EndRevoked  = "revoked"
)

// Attempt reasons, recorded but never told to the caller (§0005: one error for every failure).
const (
	ReasonUnknownUser = "unknown_user"
	ReasonBadPassword = "bad_password"
	ReasonInactive    = "inactive"
	ReasonThrottled   = "throttled"
	ReasonOK          = "ok"
)

// tokenBytes is the size of a session token: 256 bits of uniform randomness.
const tokenBytes = 32

// Session is a signed-in period.
type Session struct {
	ID        id.ID
	UserID    id.ID
	BranchID  id.ID
	TokenHash string
	CreatedAt time.Time
	LastSeen  time.Time
	// IdleExpires rolls forward on use; AbsoluteExpires never does.
	IdleExpires     time.Time
	AbsoluteExpires time.Time
	EndedAt         time.Time
	EndReason       string
	DeviceInfo      string
}

// NewToken mints a session token.
//
// Returned to the caller ONCE; only its hash is stored. Deliberately not a UUIDv7: v7 is
// time-ordered and therefore partly predictable, which is the right property for a primary key
// and exactly the wrong one for a bearer credential.
func NewToken() (string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", errs.Wrap(err, errs.CategoryInternal, CodeTokenGeneration,
			"could not generate a session token")
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// HashToken is the one definition of how a token maps to its stored form.
//
// SHA-256 rather than Argon2id, on purpose: a 256-bit random token has no dictionary to attack
// and nothing to slow down. Argon2 protects low-entropy secrets; spending 19 MiB per request
// here would buy nothing.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// SessionPolicy is the configured session lifetime.
type SessionPolicy struct {
	Idle        time.Duration
	Absolute    time.Duration
	RememberFor time.Duration
}

// NewSession builds a session for a user, starting now.
//
// `remember` selects the longer absolute window ("stay signed in", §13.1). The IDLE window is
// unaffected: staying signed in should survive a restart, not remove the protection against an
// unattended machine.
func NewSession(
	identifier, userID, branchID id.ID, tokenHash string,
	now time.Time, policy SessionPolicy, remember bool, device string,
) Session {
	absolute := policy.Absolute
	if remember && policy.RememberFor > absolute {
		absolute = policy.RememberFor
	}
	return Session{
		ID:              identifier,
		UserID:          userID,
		BranchID:        branchID,
		TokenHash:       tokenHash,
		CreatedAt:       now,
		LastSeen:        now,
		IdleExpires:     now.Add(policy.Idle),
		AbsoluteExpires: now.Add(absolute),
		DeviceInfo:      device,
	}
}

// ExpiryReason reports why a session is no longer usable, or "" if it still is.
//
// Absolute is checked FIRST: a session that has been used constantly for thirteen hours is
// still over its absolute limit, and checking idle first would let activity mask that.
func (s Session) ExpiryReason(now time.Time) string {
	if s.EndReason != "" {
		return s.EndReason
	}
	if !now.Before(s.AbsoluteExpires) {
		return EndAbsolute
	}
	if !now.Before(s.IdleExpires) {
		return EndIdle
	}
	return ""
}

// IsLive reports whether the session may still be used.
func (s Session) IsLive(now time.Time) bool { return s.ExpiryReason(now) == "" }

// NeedsTouch reports whether last_seen_at is stale enough to be worth a write.
//
// Session upkeep would otherwise put a write on EVERY binding call, and SQLite has exactly one
// writer (0.3) — so a read-only screen would queue behind session bookkeeping. Idle timeout is
// measured in tens of minutes, so recording it to the minute loses nothing. The expiry CHECK
// still happens every call; only the persistence is coarse.
func (s Session) NeedsTouch(now time.Time, granularity time.Duration) bool {
	return now.Sub(s.LastSeen) >= granularity
}

// ErrSessionInvalid is returned for an unknown, ended, or expired session.
//
// One error for all of them, for the same reason authentication has one: distinguishing
// "no such session" from "expired session" tells a holder of a stolen token whether it was
// ever real.
func ErrSessionInvalid() error {
	return errs.Permission(CodeSessionInvalid, "the session is not valid")
}

// ── throttling ──────────────────────────────────────────────────────────────────

// ThrottlePolicy is the configured lockout behaviour.
type ThrottlePolicy struct {
	Threshold int
	Base      time.Duration
	Max       time.Duration
}

// Delay returns how long a username must wait after `failures` consecutive failures.
//
// Zero below the threshold. Exponential above it, capped — and the cap is the point (D2):
//
// A desktop ERP has no password-reset email, no help desk, and on a fresh install exactly one
// administrator. A PERMANENT lock on that account is not a security feature, it is an outage an
// attacker can trigger by failing five times against a username printed on the shop's own
// invoices, recoverable only by editing the database by hand. So the delay is bounded and
// always elapses: the worst an attacker achieves is fifteen minutes, which costs them as much
// as it costs the shop.
func (p ThrottlePolicy) Delay(failures int) time.Duration {
	if p.Threshold <= 0 || failures < p.Threshold {
		return 0
	}
	delay := p.Base
	for i := 0; i < failures-p.Threshold; i++ {
		delay *= 2
		if delay >= p.Max {
			return p.Max
		}
	}
	if delay > p.Max {
		return p.Max
	}
	return delay
}

// ErrTooManyAttempts refuses a login until retryAfter has elapsed.
//
// A DISTINCT code, deliberately breaking the one-error rule that governs every other
// authentication failure. It reveals nothing: whoever is being throttled already knows they
// have been trying this username, because they are the one who tried. Meanwhile the legitimate
// user who mistyped five times genuinely needs to be told to wait, rather than left to conclude
// the application is broken.
func ErrTooManyAttempts(retryAfter time.Duration) error {
	seconds := int(retryAfter.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	return errs.Permission(CodeTooManyAttempts, "too many failed attempts; try again later").
		WithParam("retryAfter", strconv.Itoa(seconds))
}
