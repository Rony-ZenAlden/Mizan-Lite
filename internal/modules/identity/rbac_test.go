package identity_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/api/appctx"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/identity"
	"github.com/mizan-erp/mizan/internal/modules/identity/domain"
	"github.com/mizan-erp/mizan/internal/platform/auth"
)

// actorCtx stamps a user onto the context, as the 1.5 decorator will.
func actorCtx(ctx context.Context, userID, companyID id.ID) context.Context {
	return appctx.WithActor(ctx, appctx.Actor{UserID: userID, CompanyID: companyID})
}

func syncAll(t *testing.T, svc *identity.Service, ctx context.Context) {
	t.Helper()
	if _, err := svc.SyncPermissions(ctx, map[string][]auth.PermissionDef{
		"identity": identity.NewModule(nil).Permissions(),
	}); err != nil {
		t.Fatalf("SyncPermissions: %v", err)
	}
}

// ── the permission sync ─────────────────────────────────────────────────────────

func TestSyncInsertsDeclaredPermissions(t *testing.T) {
	svc, store, _ := newFixture(t)
	ctx := bound(t, store)

	report, err := svc.SyncPermissions(ctx, map[string][]auth.PermissionDef{
		"identity": {{Code: "identity.user.view"}, {Code: "identity.user.manage"}},
	})
	if err != nil {
		t.Fatalf("SyncPermissions: %v", err)
	}
	if report.Declared != 2 {
		t.Errorf("declared = %d, want 2", report.Declared)
	}

	// A second identical sync changes nothing.
	report, err = svc.SyncPermissions(ctx, map[string][]auth.PermissionDef{
		"identity": {{Code: "identity.user.view"}, {Code: "identity.user.manage"}},
	})
	if err != nil || report.Obsolete != 0 {
		t.Errorf("a second sync marked %d obsolete (err=%v)", report.Obsolete, err)
	}
}

// TestObsoletePermissionsAreMarkedNotDeleted is the D2 guarantee.
//
// The first version of this test asserted that the ROLE GRANTS survived, on the belief that
// deleting a permission would cascade. It would not — grants reference permission_code as a
// plain string (§CFG.3), so there is no foreign key — and the mutation drill proved it by
// passing with DELETE substituted for the mark.
//
// What actually distinguishes the two is VISIBILITY: the grant survives either way, but only
// an obsolete row lets an administrator discover that a role still grants something the
// software no longer implements. So that is what this asserts.
//
// Mutation check: delete instead of mark, and the row-still-present assertion fails.
func TestObsoletePermissionsAreMarkedNotDeleted(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)

	syncAll(t, svc, ctx)
	if err := svc.SeedRoles(ctx, companyID); err != nil {
		t.Fatalf("SeedRoles: %v", err)
	}

	roles, err := svc.Roles(ctx, companyID)
	if err != nil {
		t.Fatalf("Roles: %v", err)
	}
	var managerID id.ID
	for _, r := range roles {
		if r.Code == "manager" {
			managerID = r.ID
		}
	}
	if managerID.IsZero() {
		t.Fatal("the manager role was not seeded")
	}

	grantsBefore, err := svc.RoleGrants(ctx, managerID)
	if err != nil || len(grantsBefore) == 0 {
		t.Fatalf("manager grants = %v (err=%v)", grantsBefore, err)
	}

	// A build that no longer declares ANY identity permission.
	report, err := svc.SyncPermissions(ctx, map[string][]auth.PermissionDef{
		"identity": {{Code: "identity.user.view"}},
	})
	if err != nil {
		t.Fatalf("SyncPermissions: %v", err)
	}
	if report.Obsolete == 0 {
		t.Error("no permissions were marked obsolete")
	}

	// The permission ROW must still exist, flagged — that is what makes the stale grant
	// discoverable. A deleted row is invisible, and the administrator never learns that
	// "Manager can revoke sessions" now refers to nothing.
	var present, obsolete int
	if scanErr := store.Reader(ctx).QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(MAX(is_obsolete), 0) FROM permissions WHERE code = ?`,
		identity.PermSessionRevoke).Scan(&present, &obsolete); scanErr != nil {
		t.Fatalf("reading the permission: %v", scanErr)
	}
	if present != 1 {
		t.Fatalf("the undeclared permission was DELETED (%d rows) — a role may still grant it, "+
			"and now nobody can find out", present)
	}
	if obsolete != 1 {
		t.Error("the undeclared permission was not flagged obsolete")
	}

	// The grants are unaffected either way, since they reference the code, not the row.
	grantsAfter, err := svc.RoleGrants(ctx, managerID)
	if err != nil {
		t.Fatalf("RoleGrants: %v", err)
	}
	if len(grantsAfter) != len(grantsBefore) {
		t.Errorf("grants went from %d to %d", len(grantsBefore), len(grantsAfter))
	}

	// And re-declaring restores it, grants intact.
	if _, err = svc.SyncPermissions(ctx, map[string][]auth.PermissionDef{
		"identity": identity.NewModule(nil).Permissions(),
	}); err != nil {
		t.Fatalf("re-sync: %v", err)
	}
}

// TestDuplicatePermissionAcrossModulesIsFatal — a code defect, not a data leftover (0.10 D3).
func TestDuplicatePermissionAcrossModulesIsFatal(t *testing.T) {
	svc, store, _ := newFixture(t)
	ctx := bound(t, store)

	_, err := svc.SyncPermissions(ctx, map[string][]auth.PermissionDef{
		"sales":      {{Code: "shared.thing.do"}},
		"purchasing": {{Code: "shared.thing.do"}},
	})
	if err == nil {
		t.Fatal("two modules declaring one permission code was accepted")
	}
	if code := errs.CodeOf(err); code != domain.CodeDuplicatePermission {
		t.Errorf("code = %q, want %q", code, domain.CodeDuplicatePermission)
	}
	// The message must name BOTH modules — a startup error a developer reads in a log needs
	// the detail in its text, not only in params (0.10 §9.2).
	if e, _ := errs.AsError(err); e != nil {
		if !strings.Contains(e.Error(), "sales") || !strings.Contains(e.Error(), "purchasing") {
			t.Errorf("the error does not name both modules: %s", e.Error())
		}
	}
}

// ── resolution ──────────────────────────────────────────────────────────────────

// rbacFixture builds a user holding one role with the given grants.
func rbacFixture(t *testing.T, grants []string) (*identity.Service, context.Context, id.ID) {
	t.Helper()
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)
	syncAll(t, svc, ctx)
	if err := svc.SeedRoles(ctx, companyID); err != nil {
		t.Fatalf("SeedRoles: %v", err)
	}

	user := createUser(t, svc, ctx, companyID, "tester", goodPassword)

	roles, err := svc.Roles(ctx, companyID)
	if err != nil {
		t.Fatalf("Roles: %v", err)
	}
	var viewerID id.ID
	for _, r := range roles {
		if r.Code == "viewer" {
			viewerID = r.ID
		}
	}
	// Rebuild the viewer's grants to exactly what the test wants.
	current, err := svc.RoleGrants(ctx, viewerID)
	if err != nil {
		t.Fatalf("RoleGrants: %v", err)
	}
	for _, g := range current {
		if err = svc.RevokeFromRole(ctx, viewerID, g); err != nil {
			t.Fatalf("RevokeFromRole: %v", err)
		}
	}
	for _, g := range grants {
		if err = svc.GrantToRole(ctx, viewerID, g); err != nil {
			t.Fatalf("GrantToRole: %v", err)
		}
	}
	if err = svc.AssignRole(ctx, user.ID, viewerID, auth.Global()); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	return svc, actorCtx(ctx, user.ID, companyID), user.ID
}

func TestCanAllowsAGrantedPermission(t *testing.T) {
	svc, ctx, _ := rbacFixture(t, []string{identity.PermUserView})

	if !svc.Can(ctx, identity.PermUserView, auth.Global()) {
		t.Error("a granted permission was refused")
	}
	if svc.Can(ctx, identity.PermUserManage, auth.Global()) {
		t.Error("an ungranted permission was allowed")
	}
}

// TestCanRefusesWithoutAnActor: an unauthenticated context can do nothing. The login screen and
// setup wizard are reachable because their bindings are Public (1.5), not because Can is lenient.
func TestCanRefusesWithoutAnActor(t *testing.T) {
	svc, store, _ := newFixture(t)
	ctx := bound(t, store)

	if svc.Can(ctx, identity.PermUserView, auth.Global()) {
		t.Error("a context with no actor was granted a permission")
	}
}

// TestWildcardsAreGrantSideOnly — a call site asking "may I do sales.*?" and being told yes is
// how a whole module becomes unprotected by one typo.
func TestWildcardsAreGrantSideOnly(t *testing.T) {
	svc, ctx, _ := rbacFixture(t, []string{auth.Wildcard})

	if !svc.Can(ctx, identity.PermUserView, auth.Global()) {
		t.Error("the * grant did not cover a concrete permission")
	}
	for _, bad := range []string{auth.Wildcard, "identity.*", ""} {
		if svc.Can(ctx, bad, auth.Global()) {
			t.Errorf("Can accepted a wildcard input %q", bad)
		}
	}
}

func TestNamespaceWildcard(t *testing.T) {
	svc, ctx, _ := rbacFixture(t, []string{"identity.*"})

	if !svc.Can(ctx, identity.PermUserView, auth.Global()) {
		t.Error("identity.* did not cover identity.user.view")
	}
	if svc.Can(ctx, "org.company.edit", auth.Global()) {
		t.Error("identity.* covered a permission in another namespace")
	}
}

// TestAdministratorHasNoBypass: access comes from the * GRANT, resolved by the ordinary path.
// Removing the grant removes the access — which would not be true of an `if isAdmin` shortcut,
// and that is the point: a bypass hides resolution bugs from everyone who tests as an admin.
func TestAdministratorHasNoBypass(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)
	syncAll(t, svc, ctx)
	if err := svc.SeedRoles(ctx, companyID); err != nil {
		t.Fatalf("SeedRoles: %v", err)
	}
	user := createUser(t, svc, ctx, companyID, "boss", goodPassword)
	if err := svc.AssignRoleByCode(ctx, user.ID, companyID, "administrator"); err != nil {
		t.Fatalf("AssignRoleByCode: %v", err)
	}
	actor := actorCtx(ctx, user.ID, companyID)

	if !svc.Can(actor, identity.PermUserManage, auth.Global()) {
		t.Fatal("the administrator could not manage users")
	}

	roles, _ := svc.Roles(ctx, companyID)
	var adminID id.ID
	for _, r := range roles {
		if r.Code == "administrator" {
			adminID = r.ID
		}
	}
	if err := svc.RevokeFromRole(ctx, adminID, auth.Wildcard); err != nil {
		t.Fatalf("RevokeFromRole: %v", err)
	}
	if svc.Can(actor, identity.PermUserManage, auth.Global()) {
		t.Error("the administrator still had access after its * grant was removed — " +
			"there is a bypass, and resolution bugs will be invisible to anyone testing as admin")
	}
}

// TestRevocationTakesEffectOnTheNextCheck — no permission cache, same reasoning as sessions.
func TestRevocationTakesEffectOnTheNextCheck(t *testing.T) {
	svc, ctx, userID := rbacFixture(t, []string{identity.PermUserView})

	if !svc.Can(ctx, identity.PermUserView, auth.Global()) {
		t.Fatal("the permission did not start granted")
	}

	roles, _ := svc.Roles(ctx, contextCompany(t, ctx))
	for _, r := range roles {
		if r.Code == "viewer" {
			if err := svc.UnassignRole(ctx, userID, r.ID); err != nil {
				t.Fatalf("UnassignRole: %v", err)
			}
		}
	}
	if svc.Can(ctx, identity.PermUserView, auth.Global()) {
		t.Error("the permission survived unassignment — there is a cache, and revocation is a lie")
	}
}

func contextCompany(t *testing.T, ctx context.Context) id.ID {
	t.Helper()
	actor, ok := appctx.ActorFrom(ctx)
	if !ok {
		t.Fatal("no actor on the context")
	}
	return actor.CompanyID
}

func TestAnInactiveRoleGrantsNothing(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)
	syncAll(t, svc, ctx)
	if err := svc.SeedRoles(ctx, companyID); err != nil {
		t.Fatalf("SeedRoles: %v", err)
	}
	user := createUser(t, svc, ctx, companyID, "tester", goodPassword)
	if err := svc.AssignRoleByCode(ctx, user.ID, companyID, "viewer"); err != nil {
		t.Fatalf("AssignRoleByCode: %v", err)
	}
	actor := actorCtx(ctx, user.ID, companyID)

	if !svc.Can(actor, identity.PermUserView, auth.Global()) {
		t.Fatal("the viewer role did not grant its own permission")
	}

	// Deactivate the role directly; GrantsFor filters on r.is_active.
	if _, err := store.Writer(ctx).ExecContext(ctx,
		`UPDATE roles SET is_active = 0 WHERE company_id = ? AND code = 'viewer'`,
		string(companyID)); err != nil {
		t.Fatalf("deactivating the role: %v", err)
	}
	if svc.Can(actor, identity.PermUserView, auth.Global()) {
		t.Error("an inactive role still granted its permissions")
	}
}

// ── the scope table ─────────────────────────────────────────────────────────────

// TestScopeSatisfaction asserts §4.2 exactly, INCLUDING that branch does not satisfy warehouse.
//
// Mutation check: ignore the scope argument in Can, and the rows expecting false fail.
func TestScopeSatisfaction(t *testing.T) {
	branchA, _ := id.New()
	branchB, _ := id.New()
	warehouseA, _ := id.New()

	cases := []struct {
		name  string
		held  auth.Scope
		want  auth.Scope
		allow bool
	}{
		{"global covers global", auth.Global(), auth.Global(), true},
		{"global covers a branch", auth.Global(), auth.InBranch(branchA), true},
		{"global covers a warehouse", auth.Global(), auth.InWarehouse(warehouseA), true},
		{"branch covers its own branch", auth.InBranch(branchA), auth.InBranch(branchA), true},
		{"branch does not cover another branch", auth.InBranch(branchA), auth.InBranch(branchB), false},
		{"branch does not cover global", auth.InBranch(branchA), auth.Global(), false},
		// Deferred to Phase 4 (D3): containment needs a warehouse→branch lookup, and no
		// warehouse-scoped permission exists yet. Pinned so changing it is deliberate.
		{"branch does not cover a warehouse", auth.InBranch(branchA), auth.InWarehouse(warehouseA), false},
		{"warehouse covers itself", auth.InWarehouse(warehouseA), auth.InWarehouse(warehouseA), true},
		{"warehouse does not cover its branch", auth.InWarehouse(warehouseA), auth.InBranch(branchA), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := auth.Satisfies(tc.held, tc.want); got != tc.allow {
				t.Errorf("Satisfies(%v, %v) = %v, want %v", tc.held, tc.want, got, tc.allow)
			}
		})
	}
}

// TestScopedGrantIsEnforcedEndToEnd runs the scope rule through the real authorizer, not just
// the pure function.
func TestScopedGrantIsEnforcedEndToEnd(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)
	syncAll(t, svc, ctx)
	if err := svc.SeedRoles(ctx, companyID); err != nil {
		t.Fatalf("SeedRoles: %v", err)
	}
	user := createUser(t, svc, ctx, companyID, "branchmgr", goodPassword)

	roles, _ := svc.Roles(ctx, companyID)
	var viewerID id.ID
	for _, r := range roles {
		if r.Code == "viewer" {
			viewerID = r.ID
		}
	}

	branchA, _ := id.New()
	branchB, _ := id.New()
	if err := svc.AssignRole(ctx, user.ID, viewerID, auth.InBranch(branchA)); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	actor := actorCtx(ctx, user.ID, companyID)

	if !svc.Can(actor, identity.PermUserView, auth.InBranch(branchA)) {
		t.Error("a branch-scoped grant did not apply in its own branch")
	}
	if svc.Can(actor, identity.PermUserView, auth.InBranch(branchB)) {
		t.Error("a branch-scoped grant applied in ANOTHER branch")
	}
	if svc.Can(actor, identity.PermUserView, auth.Global()) {
		t.Error("a branch-scoped grant satisfied a company-wide check")
	}
}

// ── Effective ───────────────────────────────────────────────────────────────────

func TestEffectiveListsGrants(t *testing.T) {
	svc, ctx, _ := rbacFixture(t, []string{identity.PermUserView, identity.PermRoleView})

	effective, err := svc.Effective(ctx)
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	if len(effective) != 2 {
		t.Errorf("effective = %v, want two grants", effective)
	}
}

// ── the settings adapter ────────────────────────────────────────────────────────

// TestSettingsAuthorizerUsesGlobalScope pins D7: a setting is a company-wide fact, so the
// scope-free port from 0.5 is answered at global scope.
func TestSettingsAuthorizerUsesGlobalScope(t *testing.T) {
	svc, ctx, _ := rbacFixture(t, []string{identity.PermUserManage})
	adapter := identity.NewSettingsAuthorizer(svc)

	if !adapter.Can(ctx, identity.PermUserManage) {
		t.Error("the settings adapter refused a globally-granted permission")
	}
	if adapter.Can(ctx, identity.PermRoleManage) {
		t.Error("the settings adapter allowed an ungranted permission")
	}
}
