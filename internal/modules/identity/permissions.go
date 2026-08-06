package identity

import "github.com/mizan-erp/mizan/internal/platform/auth"

// The permissions this module protects. `<module>.<resource>.<action>` per §14.1.
const (
	PermUserView      = "identity.user.view"
	PermUserManage    = "identity.user.manage"
	PermRoleView      = "identity.role.view"
	PermRoleManage    = "identity.role.manage"
	PermSessionView   = "identity.session.view"
	PermSessionRevoke = "identity.session.revoke"
)

// declaredPermissions is what Module.Permissions() reports to the sync.
func declaredPermissions() []auth.PermissionDef {
	return []auth.PermissionDef{
		{Code: PermUserView, Description: "permissions.identity.user.view"},
		{Code: PermUserManage, Description: "permissions.identity.user.manage"},
		{Code: PermRoleView, Description: "permissions.identity.role.view"},
		{Code: PermRoleManage, Description: "permissions.identity.role.manage"},
		{Code: PermSessionView, Description: "permissions.identity.session.view"},
		{Code: PermSessionRevoke, Description: "permissions.identity.session.revoke"},
	}
}

// roleSeed is a default role and the permissions it starts with.
type roleSeed struct {
	Code        string
	Name        string
	Description string
	Grants      []string
}

// defaultRoles are the six §14.1 names, seeded per company by the setup wizard.
//
// # Administrator holds "*", and there is no bypass
//
// The obvious shortcut is `if role == "administrator" { return true }`. It is refused, and not
// for purity: a bypass means the code path every real user exercises is NOT the path the
// administrator exercises — so a bug in scope or wildcard resolution stays invisible to anyone
// testing as an admin, which during development is everyone.
//
// The other five are deliberately sparse. Phase 1 has six permissions to hand out; the roles
// that will matter (Cashier selling, Stock Keeper adjusting) get their grants when the modules
// that define those permissions exist. Seeding a Cashier with imagined sales permissions would
// be seeding codes nothing declares — which the sync would immediately mark obsolete.
func defaultRoles() []roleSeed {
	return []roleSeed{
		{
			Code: "administrator", Name: "Administrator",
			Description: "roles.administrator",
			Grants:      []string{auth.Wildcard},
		},
		{
			Code: "manager", Name: "Manager",
			Description: "roles.manager",
			Grants: []string{
				PermUserView, PermUserManage, PermRoleView,
				PermSessionView, PermSessionRevoke,
				// The audit LIST but not its payloads (Step 1.6, D4). A manager needs to see
				// that a price changed; the old price is a different question, and making the
				// distinction live in the seeded configuration keeps it honest rather than
				// only exercised by a test.
				"audit.entry.view",
			},
		},
		{
			Code: "accountant", Name: "Accountant",
			Description: "roles.accountant",
			Grants:      []string{PermUserView},
		},
		{
			Code: "cashier", Name: "Cashier",
			Description: "roles.cashier",
			Grants:      nil, // sales permissions arrive in Phase 5
		},
		{
			Code: "stock_keeper", Name: "Stock Keeper",
			Description: "roles.stock_keeper",
			Grants:      nil, // inventory permissions arrive in Phase 4
		},
		{
			Code: "viewer", Name: "Viewer",
			Description: "roles.viewer",
			Grants:      []string{PermUserView, PermRoleView, "audit.entry.view"},
		},
	}
}
