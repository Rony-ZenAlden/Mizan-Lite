package identity

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/identity/domain"
)

// The actions this module records (§15.3).
//
// `<module>.<entity>.<verb>`, and STABLE: an auditor reads these years later, and renaming one
// orphans every historical entry that carries the old name. Constants rather than string
// literals at the call sites for exactly that reason — a typo becomes a compile error instead
// of a category of entry nobody ever finds again.
const (
	ActionUserCreated     = "identity.user.created"
	ActionPasswordChanged = "identity.user.password_changed"
	// Distinct from a self-service change: an administrator now knows this password, which is
	// a materially different fact for anyone reading the trail later.
	ActionPasswordReset   = "identity.user.password_reset"
	ActionUserActivated   = "identity.user.activated"
	ActionUserDeactivated = "identity.user.deactivated"

	ActionRoleGranted    = "identity.role.permission_granted"
	ActionRoleRevoked    = "identity.role.permission_revoked"
	ActionRoleAssigned   = "identity.user.role_assigned"
	ActionRoleUnassigned = "identity.user.role_unassigned"

	ActionLoginSucceeded = "identity.session.login_succeeded"
	ActionLoginFailed    = "identity.session.login_failed"
	ActionLogout         = "identity.session.logout"
	ActionSessionRevoked = "identity.session.revoked"
)

// The entity types entries are filed under.
const (
	EntityUser    = "identity.user"
	EntityRole    = "identity.role"
	EntitySession = "identity.session"
)

// CodePublisherMissing reports a service built without an event publisher.
const CodePublisherMissing = "identity.publisher_missing"

// audit publishes an Auditable event, INSIDE the caller's transaction.
//
// Every call site is already within a `db.Do`, which is not incidental: the publish is
// synchronous (0.6 §3.2), so the audit subscriber's INSERT joins the same transaction and the
// change and its record commit together or neither does (phase D7).
//
// The returned error is deliberately NOT discarded anywhere in this module. `_ = s.audit(...)`
// would silently convert the D7 guarantee back into best-effort logging, which is the failure
// mode the whole step exists to prevent.
func (s *Service) audit(ctx context.Context, a auditc.Auditable) error {
	if s.bus == nil {
		// Not tolerated, and not silently: a service wired without a publisher would record
		// NOTHING, and the trail would be empty in exactly the deployment where it matters.
		// Failing at the first audited write turns a wiring mistake into a caught bug rather
		// than a five-year gap discovered by an auditor.
		return errs.Internal(CodePublisherMissing,
			"the identity service was built without an event publisher")
	}
	return s.bus.Publish(ctx, a)
}

// userSnapshot is what a user looks like in an audit payload.
//
// A hand-written projection, NOT domain.User, and never the credential: an audit payload is
// long-lived plaintext that a support engineer may read, so what goes into it is decided
// deliberately, once, here. Marshalling the domain type directly would mean every field added
// to it in the next ten years silently joins the trail.
type userSnapshot struct {
	ID          id.ID  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email,omitempty"`
	IsActive    bool   `json:"isActive"`
	IsSystem    bool   `json:"isSystem,omitempty"`
}

func snapshotUser(u domain.User) userSnapshot {
	return userSnapshot{
		ID:          u.ID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Email:       u.Email,
		IsActive:    u.IsActive,
		IsSystem:    u.IsSystem,
	}
}
