package bindings_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/api/bindings"
	"github.com/mizan-erp/mizan/internal/bootstrap"
	identitydomain "github.com/mizan-erp/mizan/internal/modules/identity/domain"
)

const adminPassword = "a sufficiently long passphrase"

// signedInAdmin is a configured installation with the administrator signed in through the
// wizard and the login binding — the same path a real launch takes.
func signedInAdmin(t *testing.T) (*bindings.Set, *bootstrap.App) {
	t.Helper()
	set, app := freshApp(t)
	if result := set.Setup.Apply(validInput()); !result.OK {
		t.Fatalf("Apply: %+v", result.Error)
	}
	if login := set.Auth.Login("nadia", adminPassword, false); !login.OK {
		t.Fatalf("Login: %+v", login.Error)
	}
	return set, app
}

// ── the two bricking rules (D6) ─────────────────────────────────────────────────

// Deactivating yourself signs you out on the very next call, so the click that does it also
// removes the ability to undo it. If you were the last administrator, the shop cannot open.
func TestYouCannotDeactivateYourself(t *testing.T) {
	set, _ := signedInAdmin(t)

	// A colleague, and a second ADMINISTRATOR, so neither the last-active-user rule nor the
	// last-administrator rule can be what refuses this. The first drill run showed the test
	// passing on the last-active-user rule instead — a test that fails for the wrong reason
	// tells you nothing about the rule it names.
	if created := set.Identity.CreateUser(bindings.NewUserDTO{
		Username: "karim", DisplayName: "Karim", Password: "another long passphrase entirely",
		RoleCode: "administrator",
	}); !created.OK {
		t.Fatalf("CreateUser: %+v", created.Error)
	}

	users := set.Identity.Users()
	if !users.OK {
		t.Fatalf("Users: %+v", users.Error)
	}
	var me string
	for _, user := range users.Data {
		if user.Username == "nadia" {
			me = user.ID
		}
	}
	if me == "" {
		t.Fatal("the signed-in administrator is not in the user list")
	}

	result := set.Identity.SetActive(me, false)
	if result.OK {
		t.Fatal("the signed-in administrator deactivated themselves")
	}
	if result.Error.Code != identitydomain.CodeSelfDeactivation {
		t.Errorf("code = %q, want %q", result.Error.Code, identitydomain.CodeSelfDeactivation)
	}

	// And they are still able to work — the refusal did not half-apply.
	if again := set.Identity.Users(); !again.OK {
		t.Fatalf("the administrator lost access after a refused deactivation: %+v", again.Error)
	}
}

// A DIFFERENT rule from the last-active-user guard: you can be the last administrator among
// several active users. Removing the role leaves people who can sign in and nobody who can
// grant a permission, create a user, or repair the mistake.
func TestTheLastAdministratorsRoleCannotBeRemoved(t *testing.T) {
	set, _ := signedInAdmin(t)

	// A second user, so the last-ACTIVE-user rule cannot be what refuses this.
	created := set.Identity.CreateUser(bindings.NewUserDTO{
		Username: "karim", DisplayName: "Karim", Password: "another long passphrase entirely",
		RoleCode: "cashier",
	})
	if !created.OK {
		t.Fatalf("CreateUser: %+v", created.Error)
	}

	users := set.Identity.Users()
	var me string
	for _, user := range users.Data {
		if user.Username == "nadia" {
			me = user.ID
		}
	}
	roles := set.Identity.UserRoles(me)
	if !roles.OK || len(roles.Data) == 0 {
		t.Fatalf("UserRoles: %+v", roles)
	}
	var adminRole string
	for _, role := range roles.Data {
		if role.Code == "administrator" {
			adminRole = role.ID
		}
	}
	if adminRole == "" {
		t.Fatal("the setup administrator does not hold the administrator role")
	}

	result := set.Identity.UnassignRole(me, adminRole)
	if result.OK {
		t.Fatal("the last administrator's role was removed; nobody can administer the system")
	}
	if result.Error.Code != identitydomain.CodeLastAdministrator {
		t.Errorf("code = %q, want %q", result.Error.Code, identitydomain.CodeLastAdministrator)
	}
}

// The rule is about the LAST one: with two administrators, removing a role is ordinary work.
func TestARoleCanBeRemovedWhileAnotherAdministratorRemains(t *testing.T) {
	set, _ := signedInAdmin(t)

	created := set.Identity.CreateUser(bindings.NewUserDTO{
		Username: "karim", DisplayName: "Karim", Password: "another long passphrase entirely",
		RoleCode: "administrator",
	})
	if !created.OK {
		t.Fatalf("CreateUser: %+v", created.Error)
	}

	roles := set.Identity.UserRoles(created.Data.ID)
	if !roles.OK || len(roles.Data) != 1 {
		t.Fatalf("UserRoles = %+v", roles)
	}
	if result := set.Identity.UnassignRole(created.Data.ID, roles.Data[0].ID); !result.OK {
		t.Fatalf("removing one of two administrators was refused: %+v", result.Error)
	}
}

// ── passwords (D1, D2) ──────────────────────────────────────────────────────────

// An administrator resetting a password now KNOWS it, which is precisely what must_change
// exists for. Before 1.11 the single SetPassword method cleared the flag, so it never fired.
func TestAnAdministratorResetForcesAChange(t *testing.T) {
	set, _ := signedInAdmin(t)

	created := set.Identity.CreateUser(bindings.NewUserDTO{
		Username: "karim", DisplayName: "Karim", Password: "another long passphrase entirely",
	})
	if !created.OK {
		t.Fatalf("CreateUser: %+v", created.Error)
	}

	const reset = "a third distinct long passphrase"
	if result := set.Identity.ResetPasswordFor(created.Data.ID, reset); !result.OK {
		t.Fatalf("ResetPasswordFor: %+v", result.Error)
	}

	if logout := set.Auth.Logout(); !logout.OK {
		t.Fatalf("Logout: %+v", logout.Error)
	}
	login := set.Auth.Login("karim", reset, false)
	if !login.OK {
		t.Fatalf("Login: %+v", login.Error)
	}
	if !login.Data.MustChange {
		t.Error("an administrator-set password does not force a change; the flag is dead code")
	}
}

// The self-service change clears it — the user has now chosen something nobody else has seen.
func TestChangingYourOwnPasswordClearsTheFlag(t *testing.T) {
	set, _ := signedInAdmin(t)

	created := set.Identity.CreateUser(bindings.NewUserDTO{
		Username: "karim", DisplayName: "Karim", Password: "another long passphrase entirely",
		RoleCode: "cashier",
	})
	if !created.OK {
		t.Fatalf("CreateUser: %+v", created.Error)
	}
	_ = set.Auth.Logout()

	if login := set.Auth.Login("karim", "another long passphrase entirely", false); !login.OK {
		t.Fatalf("Login: %+v", login.Error)
	}

	const chosen = "a password karim chose themselves"
	if result := set.Identity.ChangeMyPassword("another long passphrase entirely", chosen); !result.OK {
		t.Fatalf("ChangeMyPassword: %+v", result.Error)
	}

	_ = set.Auth.Logout()
	login := set.Auth.Login("karim", chosen, false)
	if !login.OK {
		t.Fatalf("the chosen password does not work: %+v", login.Error)
	}
	if login.Data.MustChange {
		t.Error("must_change survived a self-service change")
	}
}

// The current password is required even though the session already proves identity.
//
// The threat is an unattended terminal, which is the normal state of a shop counter: a session
// left open is not consent to change the credential that outlives it (D2).
func TestChangingYourOwnPasswordNeedsTheCurrentOne(t *testing.T) {
	set, _ := signedInAdmin(t)

	result := set.Identity.ChangeMyPassword("not the current password", "a brand new long passphrase")
	if result.OK {
		t.Fatal("the password changed without the current one being given")
	}
	if result.Error.Code != identitydomain.CodeInvalidCredentials {
		t.Errorf("code = %q, want %q", result.Error.Code, identitydomain.CodeInvalidCredentials)
	}

	// And the real one still works.
	if login := set.Auth.Login("nadia", adminPassword, false); !login.OK {
		t.Fatalf("the existing password stopped working: %+v", login.Error)
	}
}

// A user with must_change set holds no permission yet — that is the normal case, since the
// account was created seconds ago. ChangeMyPassword must still be reachable (D3).
func TestAUserWithNoPermissionsCanStillChangeTheirPassword(t *testing.T) {
	set, _ := signedInAdmin(t)

	created := set.Identity.CreateUser(bindings.NewUserDTO{
		Username: "karim", DisplayName: "Karim", Password: "another long passphrase entirely",
		// No role: they can sign in and do nothing at all.
	})
	if !created.OK {
		t.Fatalf("CreateUser: %+v", created.Error)
	}
	_ = set.Auth.Logout()

	login := set.Auth.Login("karim", "another long passphrase entirely", false)
	if !login.OK {
		t.Fatalf("Login: %+v", login.Error)
	}
	if len(login.Data.Permissions) != 0 {
		t.Fatalf("the fixture user holds permissions (%v); the test proves nothing",
			login.Data.Permissions)
	}

	if result := set.Identity.ChangeMyPassword(
		"another long passphrase entirely", "a password karim chose"); !result.OK {
		t.Fatalf("a user with no permissions cannot change their password: %+v", result.Error)
	}
	// And they still cannot do anything else.
	if users := set.Identity.Users(); users.OK {
		t.Error("a permissionless user listed the company's users")
	}
}

// ── permissions gate the surface ────────────────────────────────────────────────

func TestAdministrationRequiresItsPermissions(t *testing.T) {
	set, _ := signedInAdmin(t)

	created := set.Identity.CreateUser(bindings.NewUserDTO{
		Username: "karim", DisplayName: "Karim", Password: "another long passphrase entirely",
	})
	if !created.OK {
		t.Fatalf("CreateUser: %+v", created.Error)
	}
	_ = set.Auth.Logout()
	if login := set.Auth.Login("karim", "another long passphrase entirely", false); !login.OK {
		t.Fatalf("Login: %+v", login.Error)
	}

	// Every method, refused. Five of these six permissions had no consumer before this step.
	cases := map[string]bool{
		"Users":       set.Identity.Users().OK,
		"Roles":       set.Identity.Roles().OK,
		"Permissions": set.Identity.Permissions().OK,
		"Sessions":    set.Identity.Sessions().OK,
		"SetActive":   set.Identity.SetActive(created.Data.ID, false).OK,
		"GrantToRole": set.Identity.GrantToRole("any", "identity.user.view").OK,
	}
	for method, allowed := range cases {
		if allowed {
			t.Errorf("%s was permitted to a user holding no permissions", method)
		}
	}
}

// ── sessions ────────────────────────────────────────────────────────────────────

func TestTheSessionsListMarksYourOwn(t *testing.T) {
	set, _ := signedInAdmin(t)

	sessions := set.Identity.Sessions()
	if !sessions.OK {
		t.Fatalf("Sessions: %+v", sessions.Error)
	}
	if len(sessions.Data) != 1 {
		t.Fatalf("%d sessions, want the one just created by Login", len(sessions.Data))
	}
	if !sessions.Data[0].Current {
		t.Error("the caller's own session is not marked as current; ending it would be a surprise")
	}
	if sessions.Data[0].Username != "nadia" {
		t.Errorf("username = %q, want the signed-in administrator", sessions.Data[0].Username)
	}
}

func TestRevokingASessionEndsIt(t *testing.T) {
	set, _ := signedInAdmin(t)

	sessions := set.Identity.Sessions()
	if !sessions.OK || len(sessions.Data) == 0 {
		t.Fatalf("Sessions: %+v", sessions)
	}
	if result := set.Identity.RevokeSession(sessions.Data[0].ID); !result.OK {
		t.Fatalf("RevokeSession: %+v", result.Error)
	}

	// Ending your own signs you out on the next call — Validate refuses a revoked session.
	if again := set.Identity.Sessions(); again.OK {
		t.Error("a revoked session kept working")
	}
}

// ── the permission catalogue ────────────────────────────────────────────────────

// Read from the DATABASE, so the role editor shows what THIS build declares rather than a list
// somebody typed into the frontend.
func TestThePermissionCatalogueComesFromTheSync(t *testing.T) {
	set, _ := signedInAdmin(t)

	catalogue := set.Identity.Permissions()
	if !catalogue.OK {
		t.Fatalf("Permissions: %+v", catalogue.Error)
	}

	byCode := map[string]bindings.PermissionDTO{}
	for _, row := range catalogue.Data {
		byCode[row.Code] = row
	}
	for _, expected := range []string{
		"identity.user.view", "identity.role.manage", "audit.entry.view", "audit.entry.view_payload",
	} {
		row, found := byCode[expected]
		if !found {
			t.Errorf("%q is declared by a module but missing from the catalogue", expected)
			continue
		}
		if row.Obsolete {
			t.Errorf("%q is marked obsolete although this build declares it", expected)
		}
		if row.Module == "" {
			t.Errorf("%q has no module; the editor cannot group it", expected)
		}
	}
}
