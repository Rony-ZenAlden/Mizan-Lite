package bootstrap

import (
	"context"

	"github.com/mizan-erp/mizan/internal/api/appctx"
	"github.com/mizan-erp/mizan/internal/modules/audit"
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
