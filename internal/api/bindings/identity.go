package bindings

import (
	"context"

	"github.com/mizan-erp/mizan/internal/api/appctx"
	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/api/policy"
	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/identity"
	"github.com/mizan-erp/mizan/internal/modules/identity/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/platform/auth"
)

// UserDTO is one user as the administration screens see them.
//
// No credential, no hash, no `must_change`: the first two are secrets and the third is a fact
// about a person's sign-in that a list of colleagues has no reason to publish.
type UserDTO struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	IsActive    bool   `json:"isActive"`
	// IsSystem marks the setup administrator: protected from deletion, not from deactivation.
	IsSystem bool `json:"isSystem"`
}

// RoleDTO is one role.
type RoleDTO struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsSystem    bool   `json:"isSystem"`
	IsActive    bool   `json:"isActive"`
}

// PermissionDTO is one entry of the synced catalogue.
type PermissionDTO struct {
	Code   string `json:"code"`
	Module string `json:"module"`
	// Obsolete marks a permission this build no longer declares. Shown rather than hidden: an
	// administrator whose grant stopped working after an upgrade needs to see why.
	Obsolete bool `json:"obsolete"`
}

// SessionDTOAdmin is one live session, for the sessions screen.
//
// It carries no token and no token hash. The screen's job is to let an administrator end a
// session, which needs an identifier, not a credential.
type SessionDTOAdmin struct {
	ID          string `json:"id"`
	UserID      string `json:"userId"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	StartedAt   string `json:"startedAt"`
	LastSeen    string `json:"lastSeen"`
	ExpiresAt   string `json:"expiresAt"`
	DeviceInfo  string `json:"deviceInfo"`
	// Current marks the session making this very call, so the screen can warn before ending it.
	Current bool `json:"current"`
}

// NewUserDTO is the create-user form.
type NewUserDTO struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	// RoleCode is optional: a user with no role can sign in and do nothing, which is a
	// legitimate intermediate state while an administrator decides.
	RoleCode string `json:"roleCode"`
}

// Identity is the administration surface: users, roles, grants, and sessions.
type Identity struct{ graph }

// identityPolicies declares what each method requires.
//
// The six permissions Step 1.4 declared get their first real consumers here. Five of them have
// had NONE until now, which by this project's repeated experience is exactly where a
// declaration turns out to be wrong if it is.
//
// Note ChangeMyPassword: Public, and the reasoning is at the method.
func identityPolicies() map[string]policy.Policy {
	return map[string]policy.Policy{
		"Users":            policy.Requires(identity.PermUserView),
		"UserRoles":        policy.Requires(identity.PermUserView),
		"CreateUser":       policy.Requires(identity.PermUserManage),
		"SetActive":        policy.Requires(identity.PermUserManage),
		"ResetPasswordFor": policy.Requires(identity.PermUserManage),

		"Roles":          policy.Requires(identity.PermRoleView),
		"Permissions":    policy.Requires(identity.PermRoleView),
		"RoleGrants":     policy.Requires(identity.PermRoleView),
		"GrantToRole":    policy.Requires(identity.PermRoleManage),
		"RevokeFromRole": policy.Requires(identity.PermRoleManage),
		"AssignRole":     policy.Requires(identity.PermRoleManage),
		"UnassignRole":   policy.Requires(identity.PermRoleManage),

		"Sessions":      policy.Requires(identity.PermSessionView),
		"RevokeSession": policy.Requires(identity.PermSessionRevoke),

		"ChangeMyPassword": policy.Public(),
	}
}

// ── users ───────────────────────────────────────────────────────────────────────

// Users lists the company's users.
func (i *Identity) Users() envelope.Result[[]UserDTO] {
	ctx, app, err := i.guard("Users")
	if err != nil {
		return envelope.Fail[[]UserDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[[]UserDTO](err)
	}
	users, err := app.Identity.Users(ctx, companyID)
	if err != nil {
		return envelope.Fail[[]UserDTO](err)
	}

	out := make([]UserDTO, 0, len(users))
	for _, user := range users {
		out = append(out, UserDTO{
			ID: string(user.ID), Username: user.Username, DisplayName: user.DisplayName,
			Email: user.Email, IsActive: user.IsActive, IsSystem: user.IsSystem,
		})
	}
	return envelope.Ok(out)
}

// CreateUser adds a user, optionally with a role.
//
// The password is forced to must-change: an administrator typing someone else's password knows
// it, which is exactly what that flag is for (1.11 D1).
func (i *Identity) CreateUser(in NewUserDTO) envelope.Result[UserDTO] {
	ctx, app, err := i.guard("CreateUser")
	if err != nil {
		return envelope.Fail[UserDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[UserDTO](err)
	}

	var created UserDTO
	err = app.DB.Do(ctx, func(ctx context.Context) error {
		user, createErr := app.Identity.CreateUser(ctx, identity.CreateUserInput{
			CompanyID: companyID, Username: in.Username, DisplayName: in.DisplayName,
			Email: in.Email, Password: in.Password, MustChange: true,
		})
		if createErr != nil {
			return createErr
		}
		if in.RoleCode != "" {
			if assignErr := app.Identity.AssignRoleByCode(
				ctx, user.ID, companyID, in.RoleCode); assignErr != nil {
				return assignErr
			}
		}
		created = UserDTO{
			ID: string(user.ID), Username: user.Username, DisplayName: user.DisplayName,
			Email: user.Email, IsActive: user.IsActive, IsSystem: user.IsSystem,
		}
		return nil
	})
	if err != nil {
		return envelope.Fail[UserDTO](err)
	}
	return envelope.Ok(created)
}

// SetActive switches a user on or off.
//
// The three refusals — last active user, yourself, last administrator — live in the service
// (1.11 D6), and their typed codes cross here for the screen to render. The button is
// deliberately not hidden: a control that explains why it refused teaches the rule; a missing
// one teaches nothing.
func (i *Identity) SetActive(userID string, active bool) envelope.Result[bool] {
	ctx, app, err := i.guard("SetActive")
	if err != nil {
		return envelope.Fail[bool](err)
	}
	if err = app.Identity.SetUserActive(ctx, id.ID(userID), active); err != nil {
		return envelope.Fail[bool](err)
	}
	return envelope.Ok(true)
}

// ResetPasswordFor sets someone else's password, forcing them to replace it.
func (i *Identity) ResetPasswordFor(userID, password string) envelope.Result[bool] {
	ctx, app, err := i.guard("ResetPasswordFor")
	if err != nil {
		return envelope.Fail[bool](err)
	}
	if err = app.Identity.ResetPassword(ctx, id.ID(userID), password); err != nil {
		return envelope.Fail[bool](err)
	}
	return envelope.Ok(true)
}

// ChangeMyPassword replaces the CALLER'S OWN password.
//
// # Why this is Public (1.11 D3)
//
// Public means "no permission required", not "no session required". It is the one method a user
// with `must_change` set must be able to call, and that user may hold no permission at all —
// the flag is typically set on an account created seconds earlier with no role assigned yet.
// Gating it behind any permission would make the forced-change flow unreachable for exactly the
// people it is for.
//
// # Why it takes no user id
//
// The subject is read from the context, so there is no argument to tamper with. A caller with
// no session has no actor and is refused. That is what keeps a public method from becoming a
// way to set anybody's password.
func (i *Identity) ChangeMyPassword(current, next string) envelope.Result[bool] {
	ctx, app, err := i.guard("ChangeMyPassword")
	if err != nil {
		return envelope.Fail[bool](err)
	}
	actor, ok := appctx.ActorFrom(ctx)
	if !ok || actor.UserID.IsZero() {
		return envelope.Fail[bool](errs.Permission(CodeForbidden,
			"you must be signed in to change your password"))
	}
	if err = app.Identity.ChangeOwnPassword(ctx, actor.UserID, current, next); err != nil {
		return envelope.Fail[bool](err)
	}
	return envelope.Ok(true)
}

// ── roles and grants ────────────────────────────────────────────────────────────

// Roles lists the company's roles.
func (i *Identity) Roles() envelope.Result[[]RoleDTO] {
	ctx, app, err := i.guard("Roles")
	if err != nil {
		return envelope.Fail[[]RoleDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[[]RoleDTO](err)
	}
	roles, err := app.Identity.Roles(ctx, companyID)
	if err != nil {
		return envelope.Fail[[]RoleDTO](err)
	}
	return envelope.Ok(toRoleDTOs(roles))
}

// UserRoles lists one user's roles.
func (i *Identity) UserRoles(userID string) envelope.Result[[]RoleDTO] {
	ctx, app, err := i.guard("UserRoles")
	if err != nil {
		return envelope.Fail[[]RoleDTO](err)
	}
	roles, err := app.Identity.UserRoles(ctx, id.ID(userID))
	if err != nil {
		return envelope.Fail[[]RoleDTO](err)
	}
	return envelope.Ok(toRoleDTOs(roles))
}

// Permissions lists the synced catalogue, so the role editor shows what THIS build declares.
func (i *Identity) Permissions() envelope.Result[[]PermissionDTO] {
	ctx, app, err := i.guard("Permissions")
	if err != nil {
		return envelope.Fail[[]PermissionDTO](err)
	}
	rows, err := app.Identity.PermissionCatalogue(ctx)
	if err != nil {
		return envelope.Fail[[]PermissionDTO](err)
	}
	out := make([]PermissionDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, PermissionDTO{Code: row.Code, Module: row.Module, Obsolete: row.Obsolete})
	}
	return envelope.Ok(out)
}

// RoleGrants lists the permission codes granted to a role.
func (i *Identity) RoleGrants(roleID string) envelope.Result[[]string] {
	ctx, app, err := i.guard("RoleGrants")
	if err != nil {
		return envelope.Fail[[]string](err)
	}
	grants, err := app.Identity.RoleGrants(ctx, id.ID(roleID))
	if err != nil {
		return envelope.Fail[[]string](err)
	}
	if grants == nil {
		// [] rather than null: a frontend mapping over null is a crash, and "this role grants
		// nothing" is a legitimate state.
		grants = []string{}
	}
	return envelope.Ok(grants)
}

// GrantToRole adds a permission to a role.
func (i *Identity) GrantToRole(roleID, permission string) envelope.Result[bool] {
	ctx, app, err := i.guard("GrantToRole")
	if err != nil {
		return envelope.Fail[bool](err)
	}
	if err = app.Identity.GrantToRole(ctx, id.ID(roleID), permission); err != nil {
		return envelope.Fail[bool](err)
	}
	return envelope.Ok(true)
}

// RevokeFromRole removes a permission from a role.
func (i *Identity) RevokeFromRole(roleID, permission string) envelope.Result[bool] {
	ctx, app, err := i.guard("RevokeFromRole")
	if err != nil {
		return envelope.Fail[bool](err)
	}
	if err = app.Identity.RevokeFromRole(ctx, id.ID(roleID), permission); err != nil {
		return envelope.Fail[bool](err)
	}
	return envelope.Ok(true)
}

// AssignRole gives a user a role, at global scope.
//
// Branch scoping exists in the model (1.4) and has no screen: every Phase 1 permission is
// company-wide, so offering a branch picker would be offering a distinction nothing yet
// observes. It arrives with the Phase 4 multi-branch screens.
func (i *Identity) AssignRole(userID, roleID string) envelope.Result[bool] {
	ctx, app, err := i.guard("AssignRole")
	if err != nil {
		return envelope.Fail[bool](err)
	}
	if err = app.Identity.AssignRole(ctx, id.ID(userID), id.ID(roleID), auth.Global()); err != nil {
		return envelope.Fail[bool](err)
	}
	return envelope.Ok(true)
}

// UnassignRole removes a role from a user, refusing to remove the last administrator's.
func (i *Identity) UnassignRole(userID, roleID string) envelope.Result[bool] {
	ctx, app, err := i.guard("UnassignRole")
	if err != nil {
		return envelope.Fail[bool](err)
	}
	if err = app.Identity.UnassignRole(ctx, id.ID(userID), id.ID(roleID)); err != nil {
		return envelope.Fail[bool](err)
	}
	return envelope.Ok(true)
}

// ── sessions ────────────────────────────────────────────────────────────────────

// Sessions lists every live session.
func (i *Identity) Sessions() envelope.Result[[]SessionDTOAdmin] {
	ctx, app, err := i.guard("Sessions")
	if err != nil {
		return envelope.Fail[[]SessionDTOAdmin](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[[]SessionDTOAdmin](err)
	}
	sessions, err := app.Identity.AllActiveSessions(ctx, companyID)
	if err != nil {
		return envelope.Fail[[]SessionDTOAdmin](err)
	}

	actor, _ := appctx.ActorFrom(ctx)
	out := make([]SessionDTOAdmin, 0, len(sessions))
	for _, session := range sessions {
		out = append(out, SessionDTOAdmin{
			ID: string(session.ID), UserID: string(session.UserID),
			Username: session.Username, DisplayName: session.DisplayName,
			StartedAt: clock.Format(session.StartedAt),
			LastSeen:  clock.Format(session.LastSeen),
			ExpiresAt: clock.Format(session.ExpiresAt),
			// Compared by SESSION id, not by user: the same person may be signed in on the
			// counter terminal and the office machine, and "this one is yours" must mean the
			// one you are using.
			DeviceInfo: session.DeviceInfo,
			Current:    session.ID == actor.SessionID,
		})
	}
	return envelope.Ok(out)
}

// RevokeSession ends a session immediately.
//
// Ending your OWN is permitted and signs you out on the next call — Validate refuses a revoked
// session (1.3). That is not a mistake to prevent: "sign me out everywhere" is a legitimate
// thing to want, and the screen marks which row is yours so it is a decision rather than a
// surprise.
func (i *Identity) RevokeSession(sessionID string) envelope.Result[bool] {
	ctx, app, err := i.guard("RevokeSession")
	if err != nil {
		return envelope.Fail[bool](err)
	}
	if err = app.Identity.Revoke(ctx, id.ID(sessionID)); err != nil {
		return envelope.Fail[bool](err)
	}
	return envelope.Ok(true)
}

func toRoleDTOs(roles []sqlite.Role) []RoleDTO {
	out := make([]RoleDTO, 0, len(roles))
	for _, role := range roles {
		out = append(out, RoleDTO{
			ID: string(role.ID), Code: role.Code, Name: role.Name,
			Description: role.Description, IsSystem: role.IsSystem, IsActive: role.IsActive,
		})
	}
	return out
}
