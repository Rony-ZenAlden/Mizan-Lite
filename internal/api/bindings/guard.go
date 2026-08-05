package bindings

import (
	"context"
	"sync"

	"github.com/mizan-erp/mizan/internal/api/appctx"
	"github.com/mizan-erp/mizan/internal/api/policy"
	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/identity/contract"
	"github.com/mizan-erp/mizan/internal/modules/identity/domain"
	"github.com/mizan-erp/mizan/internal/platform/auth"
)

// Stable error codes.
const (
	// CodePolicyMissing is an internal defect: a method guarded under a name nobody declared.
	// Startup should have caught it, so reaching it at runtime means a misspelling.
	CodePolicyMissing = "app.policy_missing"
	// CodeForbidden is the denial the frontend renders as its permission-denied state.
	CodeForbidden = "app.forbidden"
)

// currentSession holds the token of the window's signed-in user.
//
// # Why a process-wide value (Step 1.5, D2)
//
// A Wails binding is called directly from JavaScript: there is no request object to carry a
// token, and threading one through every signature would put an opaque string in every frontend
// call site — where it could be logged, stored, or read by an injected script.
//
// A desktop ERP has exactly one user at the machine and one window (§2: single-process desktop
// application, no HTTP layer), so the process holds one current token. This is not a shortcut
// around multi-tenancy; there is no in-process multi-tenancy to work around. Sessions remain
// per-row, expiring and revocable exactly as Step 1.3 built them — this only decides which
// token the current window presents.
type currentSession struct {
	mu    sync.RWMutex
	token string
}

func (c *currentSession) set(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = token
}

func (c *currentSession) get() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token
}

func (c *currentSession) clear() { c.set("") }

// guard is the ONLY way a binding method reaches the object graph.
//
// # Why this is the accessor and not a decorator (Step 1.5, D1)
//
// The phase design proposed declaring a policy per method and validating at startup that each
// has one. That guarantees a policy is DECLARED, not that it is CONSULTED — a method could
// carry a perfect entry and never check it.
//
// Making the guard the accessor closes that: a method which does not call it has no database,
// no services, and no context. It is not unprotected, it is non-functional. The guarantee
// becomes structural:
//
//	A binding method cannot reach the database without passing its policy.
//
// Order matters. Not-ready first (a fresh install has no graph); then the policy, whose absence
// is an internal defect rather than a denial; then the session; then authorization.
func (g *graph) guard(method string) (context.Context, *bootstrap.App, error) {
	app, ok := g.resolve()
	if !ok {
		return nil, nil, notReady()
	}

	p, declared := g.policies[method]
	if !declared || !p.IsValid() {
		// Startup validation should have refused this build. Reaching here means the name
		// passed to guard does not match a declared policy — a typo, not a permission problem,
		// so it is reported as an internal error rather than a denial.
		return nil, nil, errs.Internal(CodePolicyMissing,
			"no policy is declared for binding method "+method)
	}

	ctx := app.Context()

	if p.Public {
		// The setup wizard and the login screen run here. Public is explicit, never a default.
		return ctx, app, nil
	}

	token := g.session.get()
	if token == "" {
		return nil, nil, domain.ErrSessionInvalid()
	}
	principal, session, err := app.Identity.Validate(ctx, token)
	if err != nil {
		// The token is cleared so a dead session does not keep being presented on every call.
		g.session.clear()
		return nil, nil, err
	}

	// Stamping the actor is what makes settings resolve at USER scope (1.3 §6) and what Can
	// reads to find the grants. It must happen before the authorization check.
	ctx = withActor(ctx, principal, session.BranchID)

	if !app.Identity.Can(ctx, p.Permission, scopeFor(p, session.BranchID)) {
		return nil, nil, errs.Permission(CodeForbidden,
			"the signed-in user may not perform this action").
			WithParam("permission", p.Permission)
	}
	return ctx, app, nil
}

// scopeFor resolves a policy's scope KIND against the session.
//
// A policy says "in the branch you are acting in", not "in branch X": which branch that is
// depends on who is asking, and only the session knows.
func scopeFor(p policy.Policy, branchID id.ID) auth.Scope {
	switch p.Scope {
	case auth.ScopeBranch:
		return auth.InBranch(branchID)
	default:
		return auth.Global()
	}
}

// withActor stamps a principal onto a context. One definition, used by the guard and by Auth.Me,
// so the two cannot drift about what an actor is.
func withActor(ctx context.Context, p contract.Principal, branchID id.ID) context.Context {
	return appctx.WithActor(ctx, appctx.Actor{
		UserID:    p.UserID,
		CompanyID: p.CompanyID,
		BranchID:  branchID,
		Username:  p.Username,
	})
}
