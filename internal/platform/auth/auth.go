// Package auth holds the authorization primitives: what a permission is, where it applies, and
// the port that answers "may this actor do it?".
//
// It lives in PLATFORM, not in the identity module, and the archlint rule added in Step 0.5 is
// what forced the question: the Module contract needs PermissionDef, platform must not import a
// module, and every module that checks a permission would otherwise have to import identity —
// turning an authorization check into a dependency on one particular implementation of it.
//
// So the TYPES are platform (a permission, a scope, the port) and the IMPLEMENTATION is the
// identity module. Sales asks auth.Authorizer whether it may void an invoice and never learns
// who answers.
package auth

import (
	"context"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// PermissionDef is a permission a module declares.
//
// Permissions are CODE-DEFINED, never user-created (§14.1): each module says what it protects
// and startup reconciles the table, which is what stops the permission list drifting from what
// the code actually checks.
type PermissionDef struct {
	// Code is `<module>.<resource>.<action>`, e.g. "sales.invoice.void" (§14.1). Part of the
	// public contract once shipped — renaming one orphans every grant referencing it.
	Code string
	// Description is an i18n key, never prose (§22.2).
	Description string
}

// ScopeKind is the level a permission is held or checked at (§14.2).
type ScopeKind string

const (
	ScopeGlobal    ScopeKind = "global"
	ScopeBranch    ScopeKind = "branch"
	ScopeWarehouse ScopeKind = "warehouse"
)

// Scope is where a permission applies.
type Scope struct {
	Kind ScopeKind
	ID   id.ID // empty for Global
}

// Global is the scope for company-wide operations.
func Global() Scope { return Scope{Kind: ScopeGlobal} }

// InBranch scopes a check to one branch.
func InBranch(branchID id.ID) Scope { return Scope{Kind: ScopeBranch, ID: branchID} }

// InWarehouse scopes a check to one warehouse.
func InWarehouse(warehouseID id.ID) Scope { return Scope{Kind: ScopeWarehouse, ID: warehouseID} }

// Wildcard is the grant that matches every permission.
const Wildcard = "*"

// IsWildcard reports whether a grant value is a wildcard rather than a concrete permission.
func IsWildcard(grant string) bool {
	return grant == Wildcard || strings.HasSuffix(grant, ".*")
}

// GrantMatches reports whether a granted value covers a concrete permission code.
//
// Wildcards are a GRANT-side concept only (§14.1): "*" matches everything and "sales.*" matches
// every permission in the sales namespace. The caller of Can must always name a concrete
// permission — the authorizer refuses a wildcard input, because a call site asking "may I do
// sales.*?" and being told yes is how a whole module becomes unprotected by one typo.
func GrantMatches(grant, permission string) bool {
	if grant == Wildcard {
		return true
	}
	if prefix, ok := strings.CutSuffix(grant, ".*"); ok {
		return strings.HasPrefix(permission, prefix+".")
	}
	return grant == permission
}

// Satisfies reports whether a role assignment at `held` covers a check at `want`.
//
// Global covers everything; otherwise the kind and the id must match exactly.
//
// # Branch does NOT cover warehouse, deliberately (Step 1.4, D3)
//
// A grant at branch B does not currently satisfy a check on a warehouse inside B. That
// containment is the natural reading and will very likely be wanted — but deciding it needs a
// warehouse→branch lookup on every check, and no warehouse-scoped permission exists until
// Phase 4 (inventory). Designing the rule now would be designing against an imagined consumer,
// the same call 0.9 D1 and 1.1 D5 made. A test pins this behaviour so changing it is
// deliberate.
func Satisfies(held, want Scope) bool {
	if held.Kind == ScopeGlobal {
		return true
	}
	return held.Kind == want.Kind && held.ID == want.ID
}

// Authorizer answers whether the acting user holds a permission.
//
// The port §14.2 requires, taking a scope from the FIRST implementation even though v1 seeds
// every grant globally: a branch manager who may void invoices in their branch is not
// expressible later without touching every call site.
type Authorizer interface {
	// Can reports whether the actor on ctx holds `permission` within `scope`.
	//
	// False for a wildcard input, for an unknown permission, and whenever there is no actor —
	// an unauthenticated context can do nothing.
	Can(ctx context.Context, permission string, scope Scope) bool

	// Effective lists the concrete grants the actor holds, for the UI to hide unavailable
	// actions. Cosmetic only: the backend is the sole enforcement point (§14.3).
	Effective(ctx context.Context) ([]string, error)
}
