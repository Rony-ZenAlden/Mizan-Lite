// Package contract is what other modules may use from identity.
//
// The FIRST contract package in the codebase, and the channel `module-isolation` (Step 1.1)
// made mandatory: §3.2 requires a module to export "a small contract package containing
// interfaces and DTOs" and never its aggregates.
//
// Note what is not here: no User aggregate, no credential type, no repository. A module that
// needs to know who is acting gets a Principal — a flat, read-only snapshot — and nothing that
// would let it change a password or construct a user.
package contract

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Principal is an authenticated actor, as the rest of the system sees them.
//
// Deliberately carries no credential material, no role list, and no session. Roles arrive with
// RBAC in 1.4 through a separate lookup, because a principal that carried its permissions
// would be a snapshot that goes stale the moment an administrator revokes one.
type Principal struct {
	UserID      id.ID
	CompanyID   id.ID
	Username    string
	DisplayName string
	// MustChange reports that the password has to be replaced before anything else is allowed.
	MustChange bool
}

// IsZero reports an absent principal.
func (p Principal) IsZero() bool { return p.UserID.IsZero() }

// Authenticator verifies credentials.
//
// The port §13.2 names: Windows Hello, Touch ID, LDAP, and cloud SSO all plug in behind this
// with no schema change. Local password authentication is the only implementation in v1.
type Authenticator interface {
	// Authenticate returns the principal for a valid username/password pair.
	//
	// Every failure — unknown user, wrong password, deactivated account — returns the SAME
	// typed error. Distinguishable failures are a username oracle, which turns an untargeted
	// password spray into a targeted one. The audit trail records which case it was (1.7); the
	// caller never learns.
	Authenticate(ctx context.Context, username, password string) (Principal, error)
}
