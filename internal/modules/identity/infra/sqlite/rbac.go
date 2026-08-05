package sqlite

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/auth"
)

// ── permissions ─────────────────────────────────────────────────────────────────

// PermissionRow is a stored permission.
type PermissionRow struct {
	Code       string
	Module     string
	IsObsolete bool
}

// Permissions lists every permission, obsolete ones included.
func (r *Repos) Permissions(ctx context.Context) ([]PermissionRow, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT code, module, is_obsolete FROM permissions ORDER BY code`)
	if err != nil {
		return nil, r.wrap(err, "listing permissions")
	}
	defer func() { _ = rows.Close() }()

	var out []PermissionRow
	for rows.Next() {
		var p PermissionRow
		var obsolete int
		if scanErr := rows.Scan(&p.Code, &p.Module, &obsolete); scanErr != nil {
			return nil, r.wrap(scanErr, "scanning a permission")
		}
		p.IsObsolete = obsolete == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpsertPermission inserts a permission or refreshes it, clearing is_obsolete.
//
// Clearing on reappearance matters: a downgrade marks a permission obsolete, and upgrading
// again must restore it WITH its grants, which survived because nothing was deleted.
func (r *Repos) UpsertPermission(ctx context.Context, def auth.PermissionDef, module string) error {
	now := r.now()
	res, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE permissions
		   SET module = ?, description = ?, is_obsolete = 0, updated_at = ?,
		       row_version = row_version + 1
		 WHERE code = ?`, module, nullable(def.Description), now, def.Code)
	if err != nil {
		return r.wrap(err, "updating the permission")
	}
	if n, rowsErr := res.RowsAffected(); rowsErr == nil && n > 0 {
		return nil
	}

	identifier, err := id.New()
	if err != nil {
		return err
	}
	_, err = r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO permissions (id, code, module, description, is_obsolete, created_at, updated_at, row_version)
		VALUES (?, ?, ?, ?, 0, ?, ?, 1)`,
		string(identifier), def.Code, module, nullable(def.Description), now, now)
	if err != nil {
		return r.wrap(err, "inserting the permission")
	}
	return nil
}

// MarkPermissionsObsolete flags every permission whose code is not in `declared`.
//
// Marked, never deleted (D2). The first draft justified this as "deleting would cascade to
// role_permissions" — that is WRONG, and the mutation drill caught it: grants reference
// permission_code as a plain string (§CFG.3), so there is no foreign key and nothing cascades.
//
// The real reason is VISIBILITY. A role may still grant a permission the software no longer
// implements, and an administrator has to be able to see that. A deleted row is invisible: the
// grant survives either way, but only the obsolete row lets anyone find out that "Manager can
// approve refunds" now refers to something that does not exist. Deleting also discards the
// module attribution and description a settings screen needs to explain the grant.
func (r *Repos) MarkPermissionsObsolete(ctx context.Context, declared map[string]bool) (int, error) {
	existing, err := r.Permissions(ctx)
	if err != nil {
		return 0, err
	}
	marked := 0
	for _, p := range existing {
		if declared[p.Code] || p.IsObsolete {
			continue
		}
		if _, err = r.db.Writer(ctx).ExecContext(ctx,
			`UPDATE permissions SET is_obsolete = 1, updated_at = ? WHERE code = ?`,
			r.now(), p.Code); err != nil {
			return marked, r.wrap(err, "marking a permission obsolete")
		}
		marked++
	}
	return marked, nil
}

// ── roles ───────────────────────────────────────────────────────────────────────

// Role is a stored role.
type Role struct {
	ID          id.ID
	CompanyID   id.ID
	Code        string
	Name        string
	Description string
	IsSystem    bool
	IsActive    bool
}

// InsertRole writes a role.
func (r *Repos) InsertRole(ctx context.Context, role Role) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO roles (id, company_id, code, name, description, is_system, is_active,
			created_at, updated_at, row_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		string(role.ID), string(role.CompanyID), role.Code, role.Name,
		nullable(role.Description), boolToInt(role.IsSystem), boolToInt(role.IsActive), now, now)
	if err != nil {
		return r.wrap(err, "inserting the role")
	}
	return nil
}

// RoleByCode finds a role.
func (r *Repos) RoleByCode(ctx context.Context, companyID id.ID, code string) (Role, error) {
	var (
		role           Role
		description    any
		system, active int
	)
	err := r.db.Reader(ctx).QueryRowContext(ctx, `
		SELECT id, company_id, code, name, description, is_system, is_active
		  FROM roles WHERE company_id = ? AND code = ?`,
		string(companyID), code).
		Scan(&role.ID, &role.CompanyID, &role.Code, &role.Name, &description, &system, &active)
	if err != nil {
		return Role{}, err // sql.ErrNoRows passes through
	}
	if s, ok := description.(string); ok {
		role.Description = s
	}
	role.IsSystem, role.IsActive = system == 1, active == 1
	return role, nil
}

// Roles lists a company's roles.
func (r *Repos) Roles(ctx context.Context, companyID id.ID) ([]Role, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT id, company_id, code, name, description, is_system, is_active
		  FROM roles WHERE company_id = ? ORDER BY code`, string(companyID))
	if err != nil {
		return nil, r.wrap(err, "listing roles")
	}
	defer func() { _ = rows.Close() }()

	var out []Role
	for rows.Next() {
		var (
			role           Role
			description    any
			system, active int
		)
		if scanErr := rows.Scan(&role.ID, &role.CompanyID, &role.Code, &role.Name,
			&description, &system, &active); scanErr != nil {
			return nil, r.wrap(scanErr, "scanning a role")
		}
		if s, ok := description.(string); ok {
			role.Description = s
		}
		role.IsSystem, role.IsActive = system == 1, active == 1
		out = append(out, role)
	}
	return out, rows.Err()
}

// GrantPermission adds a permission (or wildcard) to a role, idempotently.
func (r *Repos) GrantPermission(ctx context.Context, roleID id.ID, code string) error {
	identifier, err := id.New()
	if err != nil {
		return err
	}
	// Delete-then-insert keeps the operation idempotent without a dialect upsert, and the
	// unique key makes a duplicate impossible either way.
	if _, err = r.db.Writer(ctx).ExecContext(ctx,
		`DELETE FROM role_permissions WHERE role_id = ? AND permission_code = ?`,
		string(roleID), code); err != nil {
		return r.wrap(err, "replacing the grant")
	}
	_, err = r.db.Writer(ctx).ExecContext(ctx,
		`INSERT INTO role_permissions (id, role_id, permission_code, created_at) VALUES (?, ?, ?, ?)`,
		string(identifier), string(roleID), code, r.now())
	if err != nil {
		return r.wrap(err, "granting the permission")
	}
	return nil
}

// RevokePermission removes a grant.
func (r *Repos) RevokePermission(ctx context.Context, roleID id.ID, code string) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx,
		`DELETE FROM role_permissions WHERE role_id = ? AND permission_code = ?`,
		string(roleID), code)
	if err != nil {
		return r.wrap(err, "revoking the permission")
	}
	return nil
}

// RoleGrants lists a role's granted codes.
func (r *Repos) RoleGrants(ctx context.Context, roleID id.ID) ([]string, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT permission_code FROM role_permissions WHERE role_id = ? ORDER BY permission_code`,
		string(roleID))
	if err != nil {
		return nil, r.wrap(err, "listing grants")
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var code string
		if scanErr := rows.Scan(&code); scanErr != nil {
			return nil, r.wrap(scanErr, "scanning a grant")
		}
		out = append(out, code)
	}
	return out, rows.Err()
}

// ── assignments ─────────────────────────────────────────────────────────────────

// AssignRole gives a user a role, optionally scoped.
func (r *Repos) AssignRole(ctx context.Context, userID, roleID id.ID, scope auth.Scope) error {
	identifier, err := id.New()
	if err != nil {
		return err
	}
	var kind, scopeID any
	if scope.Kind != "" && scope.Kind != auth.ScopeGlobal {
		kind, scopeID = string(scope.Kind), string(scope.ID)
	}
	if _, err = r.db.Writer(ctx).ExecContext(ctx, `
		DELETE FROM user_roles
		 WHERE user_id = ? AND role_id = ?
		   AND ((scope_kind IS NULL AND ? IS NULL) OR scope_kind = ?)`,
		string(userID), string(roleID), kind, kind); err != nil {
		return r.wrap(err, "replacing the assignment")
	}
	_, err = r.db.Writer(ctx).ExecContext(ctx,
		`INSERT INTO user_roles (id, user_id, role_id, scope_kind, scope_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		string(identifier), string(userID), string(roleID), kind, scopeID, r.now())
	if err != nil {
		return r.wrap(err, "assigning the role")
	}
	return nil
}

// UnassignRole removes every assignment of a role from a user.
func (r *Repos) UnassignRole(ctx context.Context, userID, roleID id.ID) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx,
		`DELETE FROM user_roles WHERE user_id = ? AND role_id = ?`,
		string(userID), string(roleID))
	if err != nil {
		return r.wrap(err, "unassigning the role")
	}
	return nil
}

// Grant is one (permission, scope) pair a user holds.
type Grant struct {
	Code  string
	Scope auth.Scope
}

// GrantsFor returns every grant a user holds, through their ACTIVE roles.
//
// One indexed query joining user_roles → roles → role_permissions. There is deliberately no
// cache: revoking a role must take effect on the very next check, and a cache would make
// revocation a lie for as long as an entry lived — the same reasoning as sessions (1.3).
func (r *Repos) GrantsFor(ctx context.Context, userID id.ID) ([]Grant, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT rp.permission_code, ur.scope_kind, ur.scope_id
		  FROM user_roles ur
		  JOIN roles r          ON r.id = ur.role_id
		  JOIN role_permissions rp ON rp.role_id = ur.role_id
		 WHERE ur.user_id = ? AND r.is_active = 1`, string(userID))
	if err != nil {
		return nil, r.wrap(err, "reading grants")
	}
	defer func() { _ = rows.Close() }()

	var out []Grant
	for rows.Next() {
		var (
			code           string
			kind, scopeRef any
		)
		if scanErr := rows.Scan(&code, &kind, &scopeRef); scanErr != nil {
			return nil, r.wrap(scanErr, "scanning a grant")
		}
		g := Grant{Code: code, Scope: auth.Scope{Kind: auth.ScopeGlobal}}
		if k, ok := kind.(string); ok && k != "" {
			g.Scope.Kind = auth.ScopeKind(k)
			if sid, ok := scopeRef.(string); ok {
				g.Scope.ID = id.ID(sid)
			}
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
