package identity

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"github.com/mizan-erp/mizan/internal/api/appctx"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/identity/domain"
	"github.com/mizan-erp/mizan/internal/modules/identity/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/platform/auth"
)

var _ auth.Authorizer = (*Service)(nil)

// Can reports whether the actor on ctx holds a permission within a scope.
//
// False whenever there is no actor: an unauthenticated context can do nothing. The setup
// wizard and the login screen run in exactly that state, and they are reachable because their
// bindings are declared Public (1.5), not because the authorizer is lenient.
func (s *Service) Can(ctx context.Context, permission string, scope auth.Scope) bool {
	// A wildcard is a GRANT-side concept. A call site asking "may I do sales.*?" and being
	// told yes is how a whole module becomes unprotected by one typo, so the input is refused
	// rather than interpreted.
	if permission == "" || auth.IsWildcard(permission) {
		return false
	}

	actor, ok := appctx.ActorFrom(ctx)
	if !ok || actor.UserID.IsZero() {
		return false
	}

	grants, err := s.repos.GrantsFor(ctx, actor.UserID)
	if err != nil {
		// A failed read must never read as "allowed". Denying on error is the only safe
		// direction for an authorization check.
		return false
	}

	for _, g := range grants {
		if auth.GrantMatches(g.Code, permission) && auth.Satisfies(g.Scope, scope) {
			return true
		}
	}
	return false
}

// Effective lists the concrete grants the actor holds, for the UI to hide unavailable actions.
//
// Cosmetic only — the backend is the sole enforcement point (§14.3). Wildcards are returned as
// themselves rather than expanded: expanding would mean listing every permission in the
// system, and the frontend only needs to answer "should this menu item be visible".
func (s *Service) Effective(ctx context.Context) ([]string, error) {
	actor, ok := appctx.ActorFrom(ctx)
	if !ok || actor.UserID.IsZero() {
		return nil, nil
	}
	grants, err := s.repos.GrantsFor(ctx, actor.UserID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(grants))
	for _, g := range grants {
		if !seen[g.Code] {
			seen[g.Code] = true
			out = append(out, g.Code)
		}
	}
	sort.Strings(out)
	return out, nil
}

// ── the permission sync ─────────────────────────────────────────────────────────

// SyncReport describes what a permission sync changed.
type SyncReport struct {
	Declared int
	Inserted int
	Obsolete int
}

// SyncPermissions reconciles the permissions table with what the running build declares.
//
// Permissions are code-defined (§14.1), which is what stops the permission list drifting from
// what the code actually checks. Rows nothing declares are marked obsolete and REPORTED, never
// deleted (D2) — a data leftover must not stop a shop opening (0.10 D3), and deleting would
// cascade to role_permissions, silently changing what every role grants.
func (s *Service) SyncPermissions(ctx context.Context, byModule map[string][]auth.PermissionDef) (SyncReport, error) {
	var report SyncReport

	// A duplicate code across two modules is a CODE defect: ownership would be ambiguous and
	// whichever module synced last would win. Fatal, naming both (0.10 D3).
	owner := map[string]string{}
	for module, defs := range byModule {
		for _, def := range defs {
			if def.Code == "" {
				return report, errs.Internal(domain.CodeInvalidPermission,
					"module "+module+" declared a permission with no code")
			}
			if previous, clash := owner[def.Code]; clash {
				return report, errs.Internal(domain.CodeDuplicatePermission,
					"permission "+def.Code+" is declared by both "+previous+" and "+module).
					WithParam("permission", def.Code).
					WithParam("modules", previous+", "+module)
			}
			owner[def.Code] = module
		}
	}

	declared := make(map[string]bool, len(owner))
	err := s.db.Do(ctx, func(ctx context.Context) error {
		for module, defs := range byModule {
			for _, def := range defs {
				declared[def.Code] = true
				if upsertErr := s.repos.UpsertPermission(ctx, def, module); upsertErr != nil {
					return upsertErr
				}
			}
		}
		marked, markErr := s.repos.MarkPermissionsObsolete(ctx, declared)
		if markErr != nil {
			return markErr
		}
		report.Obsolete = marked
		return nil
	})
	if err != nil {
		return SyncReport{}, err
	}
	report.Declared = len(declared)
	return report, nil
}

// ── roles ───────────────────────────────────────────────────────────────────────

// SeedRoles creates the default roles for a company, idempotently.
//
// Called by the setup wizard (1.9). Seeded roles are is_system: freely EDITABLE — a shop that
// wants its Cashier to do more should say so — but never deletable, because deleting one would
// orphan everyone assigned to it.
func (s *Service) SeedRoles(ctx context.Context, companyID id.ID) error {
	return s.db.Do(ctx, func(ctx context.Context) error {
		for _, seed := range defaultRoles() {
			role, err := s.repos.RoleByCode(ctx, companyID, seed.Code)
			switch {
			case err == nil:
				// Already present. Grants are NOT re-applied: an administrator who removed a
				// permission from Cashier must not have it silently restored on next boot —
				// the same rule the 0.5 seeder follows for admin-owned rows.
			case errors.Is(err, sql.ErrNoRows):
				identifier, idErr := id.New()
				if idErr != nil {
					return idErr
				}
				role = sqlite.Role{
					ID: identifier, CompanyID: companyID, Code: seed.Code,
					Name: seed.Name, Description: seed.Description,
					IsSystem: true, IsActive: true,
				}
				if insertErr := s.repos.InsertRole(ctx, role); insertErr != nil {
					return insertErr
				}
				for _, grant := range seed.Grants {
					if grantErr := s.repos.GrantPermission(ctx, role.ID, grant); grantErr != nil {
						return grantErr
					}
				}
			default:
				return err
			}
		}
		return nil
	})
}

// Roles lists a company's roles.
func (s *Service) Roles(ctx context.Context, companyID id.ID) ([]sqlite.Role, error) {
	return s.repos.Roles(ctx, companyID)
}

// RoleGrants lists a role's granted permission codes.
func (s *Service) RoleGrants(ctx context.Context, roleID id.ID) ([]string, error) {
	return s.repos.RoleGrants(ctx, roleID)
}

// GrantToRole adds a permission to a role.
//
// Audited, and audited atomically: a permission change is the single most security-relevant
// write this module performs, and "someone gained this permission but we cannot say who
// granted it" is the exact question the trail exists to answer.
func (s *Service) GrantToRole(ctx context.Context, roleID id.ID, permission string) error {
	return s.db.Do(ctx, func(ctx context.Context) error {
		if err := s.repos.GrantPermission(ctx, roleID, permission); err != nil {
			return err
		}
		return s.audit(ctx, auditc.Auditable{
			Action:      ActionRoleGranted,
			EntityType:  EntityRole,
			EntityID:    roleID,
			EntityLabel: s.roleLabel(ctx, roleID),
			After:       grantSnapshot{RoleID: roleID, Permission: permission},
		})
	})
}

// RevokeFromRole removes a permission from a role.
//
// Named for its object rather than shortened to Revoke: sessions are revoked too, and a
// call site reading `svc.Revoke(x, y)` should not have to guess which.
func (s *Service) RevokeFromRole(ctx context.Context, roleID id.ID, permission string) error {
	return s.db.Do(ctx, func(ctx context.Context) error {
		if err := s.repos.RevokePermission(ctx, roleID, permission); err != nil {
			return err
		}
		return s.audit(ctx, auditc.Auditable{
			Action:      ActionRoleRevoked,
			EntityType:  EntityRole,
			EntityID:    roleID,
			EntityLabel: s.roleLabel(ctx, roleID),
			Before:      grantSnapshot{RoleID: roleID, Permission: permission},
		})
	})
}

// AssignRole gives a user a role within a scope.
func (s *Service) AssignRole(ctx context.Context, userID, roleID id.ID, scope auth.Scope) error {
	return s.db.Do(ctx, func(ctx context.Context) error {
		if err := s.repos.AssignRole(ctx, userID, roleID, scope); err != nil {
			return err
		}
		return s.audit(ctx, auditc.Auditable{
			Action:      ActionRoleAssigned,
			EntityType:  EntityUser,
			EntityID:    userID,
			EntityLabel: s.usernameFor(ctx, userID),
			After: assignmentSnapshot{
				UserID: userID, RoleID: roleID, RoleCode: s.roleLabel(ctx, roleID),
				ScopeKind: string(scope.Kind), ScopeID: scope.ID,
			},
		})
	})
}

// UnassignRole removes a role from a user.
func (s *Service) UnassignRole(ctx context.Context, userID, roleID id.ID) error {
	return s.db.Do(ctx, func(ctx context.Context) error {
		if err := s.repos.UnassignRole(ctx, userID, roleID); err != nil {
			return err
		}
		return s.audit(ctx, auditc.Auditable{
			Action:      ActionRoleUnassigned,
			EntityType:  EntityUser,
			EntityID:    userID,
			EntityLabel: s.usernameFor(ctx, userID),
			Before:      assignmentSnapshot{UserID: userID, RoleID: roleID, RoleCode: s.roleLabel(ctx, roleID)},
		})
	})
}

// AssignRoleByCode is the setup wizard's path: make this user an Administrator.
func (s *Service) AssignRoleByCode(ctx context.Context, userID, companyID id.ID, roleCode string) error {
	role, err := s.repos.RoleByCode(ctx, companyID, roleCode)
	if errors.Is(err, sql.ErrNoRows) {
		return errs.NotFound(domain.CodeRoleNotFound, "no such role").WithParam("role", roleCode)
	}
	if err != nil {
		return err
	}
	return s.AssignRole(ctx, userID, role.ID, auth.Global())
}

// roleLabel resolves a role's code for an audit entry, best-effort — an entry with no label
// still answers who did what, which a missing entry does not.
func (s *Service) roleLabel(ctx context.Context, roleID id.ID) string {
	roles, err := s.repos.RoleByID(ctx, roleID)
	if err != nil {
		return ""
	}
	return roles.Code
}

type grantSnapshot struct {
	RoleID     id.ID  `json:"roleId"`
	Permission string `json:"permission"`
}

type assignmentSnapshot struct {
	UserID    id.ID  `json:"userId"`
	RoleID    id.ID  `json:"roleId"`
	RoleCode  string `json:"roleCode,omitempty"`
	ScopeKind string `json:"scopeKind,omitempty"`
	ScopeID   id.ID  `json:"scopeId,omitempty"`
}

// ── the config.Authorizer adapter ───────────────────────────────────────────────

// SettingsAuthorizer adapts the RBAC authorizer to the scope-free port config declared in 0.5.
//
// Two interfaces rather than one, deliberately: a setting is a company-wide fact, and there is
// no meaningful branch-scoped "may change this setting". Forcing the settings port to carry a
// scope it has no use for would be shape without meaning, so the adapter supplies Global.
type SettingsAuthorizer struct{ svc *Service }

// NewSettingsAuthorizer builds the adapter the composition root hands to config.Open.
func NewSettingsAuthorizer(svc *Service) SettingsAuthorizer { return SettingsAuthorizer{svc: svc} }

// Can answers the settings layer's question at global scope.
func (a SettingsAuthorizer) Can(ctx context.Context, permission string) bool {
	if a.svc == nil {
		return false
	}
	return a.svc.Can(ctx, permission, auth.Global())
}
