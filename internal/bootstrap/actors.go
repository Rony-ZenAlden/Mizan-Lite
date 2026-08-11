package bootstrap

import (
	"context"

	"github.com/mizan-erp/mizan/internal/api/appctx"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/audit"
	"github.com/mizan-erp/mizan/internal/modules/identity"
	"github.com/mizan-erp/mizan/internal/modules/inventory"
)

// auditActors tells the audit module who is acting.
//
// The adapter lives HERE, in the composition root, because it is the only place that may know
// both sides. `appctx` is the API layer and `audit` is a module; a module importing the API
// layer would point a dependency outward (§3.1), and the API layer owning an audit-shaped type
// would put a module's vocabulary in the wrong package. Neither knows the other exists — one
// declares the port at its point of use, the other satisfies it, and eleven lines of wiring in
// between is what that costs.
type auditActors struct{}

var _ audit.ActorResolver = auditActors{}

// Actor reads the principal the 1.5 guard stamped onto the context.
//
// `false` is a legitimate answer, not a failure: the setup wizard, a background job, and the
// login screen all act with no user, and the entry is then recorded unattributed.
func (auditActors) Actor(ctx context.Context) (audit.Actor, bool) {
	a, ok := appctx.ActorFrom(ctx)
	if !ok || a.UserID.IsZero() {
		return audit.Actor{}, false
	}
	return audit.Actor{
		UserID:      a.UserID,
		DisplayName: a.DisplayName,
		BranchID:    a.BranchID,
		SessionID:   a.SessionID,
	}, true
}

// identityActors tells identity who is acting.
//
// A second adapter rather than reusing auditActors: the two modules declare different ports
// because they need different things — audit wants a name and a branch to snapshot, identity
// wants only an identifier to compare. Making one satisfy both would mean one of them carrying
// a field it has no use for, which is shape without meaning.
type identityActors struct{}

var _ identity.ActingUser = identityActors{}

func (identityActors) UserID(ctx context.Context) (id.ID, bool) {
	a, ok := appctx.ActorFrom(ctx)
	if !ok || a.UserID.IsZero() {
		return id.ID(""), false
	}
	return a.UserID, true
}

// inventoryActors satisfies inventory's own ActorResolver from the same context principal.
//
// A second type rather than one satisfying both interfaces: inventory declares its own port so
// that it does not depend on the audit package (§10.3's module isolation), and the two shapes
// are deliberately different — inventory needs a user and a branch, not a session or a display
// name. One implementation, two narrow views of it, and the composition root is where they meet.
type inventoryActors struct{}

var _ inventory.ActorResolver = inventoryActors{}

// Actor reads the principal the 1.5 guard stamped onto the context.
func (inventoryActors) Actor(ctx context.Context) (inventory.Actor, bool) {
	a, ok := appctx.ActorFrom(ctx)
	if !ok || a.UserID.IsZero() {
		return inventory.Actor{}, false
	}
	return inventory.Actor{UserID: a.UserID, BranchID: a.BranchID}, true
}
