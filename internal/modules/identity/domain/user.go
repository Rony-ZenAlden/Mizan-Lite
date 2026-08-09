// Package domain holds the identity entities and their invariants.
//
// Pure Go: stdlib and kernel only (enforced by domain-purity). It knows nothing about how a
// password is hashed — that is platform/crypto's job — only about what a valid one is.
package domain

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Stable error codes. They double as i18n keys and are part of the public contract.
const (
	CodeInvalidUser         = "identity.invalid_user"
	CodeInvalidCredentials  = "identity.invalid_credentials"
	CodeUserNotFound        = "identity.user_not_found"
	CodeUsernameTaken       = "identity.username_taken"
	CodePasswordTooShort    = "identity.password_too_short"
	CodePasswordReused      = "identity.password_reused"
	CodeLastAdministrator   = "identity.last_administrator"
	CodeSelfDeactivation    = "identity.self_deactivation"
	CodeLastActiveUser      = "identity.last_active_user"
	CodeSystemUser          = "identity.system_user"
	CodePasswordRequired    = "identity.password_required"
	CodeInvalidPermission   = "identity.invalid_permission"
	CodeDuplicatePermission = "identity.duplicate_permission"
	CodeRoleNotFound        = "identity.role_not_found"
)

// CredentialPassword and CredentialPIN are the credential kinds.
//
// PIN is reserved and unimplemented (phase design D2): it is meaningless without the POS
// screen that switches cashiers, and its rules — which permissions a PIN session may hold,
// what "device already unlocked" means — belong to that design.
const (
	CredentialPassword = "password"
	CredentialPIN      = "pin"
)

// User is a person who can sign in.
type User struct {
	ID          id.ID
	CompanyID   id.ID
	Username    string
	DisplayName string
	Email       string
	Locale      string
	IsSystem    bool
	IsActive    bool
}

// NormaliseUsername is the single definition of how a username is stored and looked up.
//
// Lower-cased and trimmed, so "Admin" and "admin" cannot both exist: the unique constraint is
// on the STORED value, and letting case through would make it enforce nothing while appearing
// to. Exported because the login path must normalise a typed username exactly the same way —
// two different normalisations would mean an account that can be created but not signed into.
func NormaliseUsername(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// NewUser builds a validated user.
func NewUser(identifier, companyID id.ID, username, displayName string) (User, error) {
	u := User{
		ID:          identifier,
		CompanyID:   companyID,
		Username:    NormaliseUsername(username),
		DisplayName: strings.TrimSpace(displayName),
		IsActive:    true,
	}
	switch {
	case u.ID.IsZero() || u.CompanyID.IsZero():
		return User{}, errs.Validation(CodeInvalidUser, "a user needs identifiers")
	case u.Username == "":
		return User{}, errs.Validation(CodeInvalidUser, "a user needs a username").
			WithField("username", CodeInvalidUser, "required")
	case strings.ContainsAny(u.Username, " \t\n"):
		return User{}, errs.Validation(CodeInvalidUser, "a username may not contain spaces").
			WithField("username", CodeInvalidUser, "invalid")
	case u.DisplayName == "":
		return User{}, errs.Validation(CodeInvalidUser, "a user needs a display name").
			WithField("displayName", CodeInvalidUser, "required")
	}
	return u, nil
}

// ErrInvalidCredentials is the ONE failure every authentication path returns.
//
// Unknown user, wrong password, and deactivated account are indistinguishable to the caller.
// Anything else is a username oracle.
func ErrInvalidCredentials() error {
	return errs.Permission(CodeInvalidCredentials, "the username or password is incorrect")
}

// ErrLastActiveUser refuses to lock everyone out.
//
// Deactivating the only active user has no in-application recovery — the next launch shows a
// login screen nobody can pass. An unrecoverable state should be unrepresentable, not
// documented. Same shape as Step 1.1's last-active-branch guard.
func ErrLastActiveUser() error {
	return errs.Conflict(CodeLastActiveUser,
		"the last active user cannot be deactivated")
}

// ErrSelfDeactivation refuses to switch off the account making the request.
//
// Deactivating yourself signs you out on the very next call — Validate ends a session whose
// user is inactive (1.3) — so the click that does it also removes the ability to undo it. If
// you were also the last administrator, the installation is closed to everyone.
//
// The obvious objection is "an administrator should be able to do what they like". They can:
// another administrator can deactivate this one. What is refused is the one ordering that has
// no way back.
func ErrSelfDeactivation() error {
	return errs.Conflict(CodeSelfDeactivation,
		"you cannot deactivate the account you are signed in with")
}

// ErrLastAdministrator refuses to remove the last route into the system.
//
// Distinct from ErrLastActiveUser, and the difference is the whole reason this exists: you can
// be the last ADMINISTRATOR among five active users. Removing the role leaves five people who
// can sign in and nobody who can grant a permission, create a user, or repair the mistake.
func ErrLastAdministrator() error {
	return errs.Conflict(CodeLastAdministrator,
		"the last administrator's role cannot be removed")
}

// ErrSystemUser refuses to delete the setup administrator.
func ErrSystemUser() error {
	return errs.Conflict(CodeSystemUser, "this user is part of the system and cannot be deleted")
}

// ErrPasswordReused refuses a password the user has recently used.
func ErrPasswordReused() error {
	return errs.Validation(CodePasswordReused, "this password was used recently").
		WithField("password", CodePasswordReused, "reused")
}

// CanDeactivate reports whether a user may be switched off, given how many remain active.
func CanDeactivate(activeUsers int) bool { return activeUsers > 1 }

// ── password policy ─────────────────────────────────────────────────────────────

// PasswordPolicy is the configured rule set. Read from settings, never from constants, so an
// installation that must satisfy an auditor can tighten it without a release (§13.1).
type PasswordPolicy struct {
	MinLength int
	// HistoryCount is how many previous passwords may not be reused. 0 disables the check.
	HistoryCount int
	// ExpiryDays is 0 for "never expires" — the default, deliberately (Step 1.2, D3).
	ExpiryDays int
}

// Validate checks a candidate password against the policy.
//
// # What is deliberately NOT checked
//
// No composition rules (a symbol, a digit, mixed case) and no maximum length. NIST SP 800-63B
// advises against both: composition rules push users toward predictable substitutions —
// Password1! — while barely raising entropy, and a maximum length is a historical artefact of
// storing passwords badly, which a hash does not suffer from. A passphrase must work.
//
// Length is measured in RUNES, not bytes: an Arabic or emoji passphrase of ten characters is
// ten characters, and counting bytes would silently apply a different rule per script.
func (p PasswordPolicy) Validate(password string) error {
	if password == "" {
		return errs.Validation(CodePasswordRequired, "a password is required").
			WithField("password", CodePasswordRequired, "required")
	}
	if minLength := p.effectiveMinLength(); utf8.RuneCountInString(password) < minLength {
		return errs.Validation(CodePasswordTooShort, "the password is too short").
			WithParam("min", strconv.Itoa(minLength)).
			WithField("password", CodePasswordTooShort, "too_short")
	}
	return nil
}

// effectiveMinLength floors the configured minimum at a hard 8.
//
// A setting is configuration, not a licence to disable security: an administrator who types 1
// into the box has made a mistake, not a policy decision. §CFG.5 draws exactly this line —
// "safety and integrity rules are never configurable".
func (p PasswordPolicy) effectiveMinLength() int {
	const floor = 8
	if p.MinLength < floor {
		return floor
	}
	return p.MinLength
}
