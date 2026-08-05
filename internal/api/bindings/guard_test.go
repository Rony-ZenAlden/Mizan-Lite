package bindings_test

import (
	"encoding/json"

	"github.com/mizan-erp/mizan/internal/api/appctx"
	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/api/bindings"
	"github.com/mizan-erp/mizan/internal/modules/identity"
	"github.com/mizan-erp/mizan/internal/platform/auth"
)

// ── authentication through the binding ──────────────────────────────────────────

func TestLoginAndMe(t *testing.T) {
	set, _ := signedIn(t)

	me := set.Auth.Me()
	if !me.OK {
		t.Fatalf("Me failed: %+v", me.Error)
	}
	if !me.Data.SignedIn || me.Data.Username != "admin" {
		t.Errorf("Me = %+v", me.Data)
	}
	if len(me.Data.Permissions) == 0 {
		t.Error("Me returned no permissions; the UI cannot hide unavailable actions")
	}
}

// TestMeReportsSignedOutCleanly — "not signed in" is an answer, not a failure, and it is the
// frontend's first call on every launch.
func TestMeReportsSignedOutCleanly(t *testing.T) {
	app := boot(t)
	set := bindings.New()
	set.Attach(app)

	me := set.Auth.Me()
	if !me.OK {
		t.Fatalf("Me failed when nobody is signed in: %+v", me.Error)
	}
	if me.Data.SignedIn {
		t.Error("Me reported a signed-in user with no session")
	}
}

func TestLogoutEndsTheSession(t *testing.T) {
	set, _ := signedIn(t)

	if r := set.Auth.Logout(); !r.OK {
		t.Fatalf("Logout failed: %+v", r.Error)
	}
	if me := set.Auth.Me(); me.Data.SignedIn {
		t.Error("still signed in after Logout")
	}
	if r := set.System.Health(); r.OK {
		t.Error("a guarded method still worked after Logout")
	}
}

// TestLogoutWorksWithoutASession: a session that has just expired must still be able to clear
// itself, or the frontend is stranded holding a dead token.
func TestLogoutWorksWithoutASession(t *testing.T) {
	app := boot(t)
	set := bindings.New()
	set.Attach(app)

	if r := set.Auth.Logout(); !r.OK {
		t.Errorf("Logout failed with no session: %+v", r.Error)
	}
}

func TestWrongPasswordCrossesAsTheIdentityCode(t *testing.T) {
	set, _ := signedIn(t)
	_ = set.Auth.Logout()

	r := set.Auth.Login("admin", "not the password at all", false)
	if r.OK {
		t.Fatal("a wrong password logged in")
	}
	if r.Error.Code != "identity.invalid_credentials" {
		t.Errorf("code = %q, want identity.invalid_credentials", r.Error.Code)
	}
}

// TestTheTokenNeverCrossesTheBoundary is D3.
//
// The token lives in the process, so it cannot be written to localStorage, printed to a
// console, or read by an injected script. Asserted on the MARSHALLED bytes, not struct fields.
func TestTheTokenNeverCrossesTheBoundary(t *testing.T) {
	set, app := signedIn(t)

	// The token that actually exists, so the assertion is not vacuous.
	sessions, err := app.Identity.ActiveSessions(app.Context(), currentUserID(t, set))
	if err != nil || len(sessions) == 0 {
		t.Fatalf("no active session to check against (err=%v)", err)
	}

	for name, payload := range map[string]any{
		"Me":    set.Auth.Me(),
		"Login": set.Auth.Login("admin", "a sufficiently long passphrase", false),
	} {
		raw, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			t.Fatalf("marshal %s: %v", name, marshalErr)
		}
		lower := strings.ToLower(string(raw))
		for _, forbidden := range []string{"token", sessions[0].TokenHash} {
			if strings.Contains(lower, strings.ToLower(forbidden)) {
				t.Errorf("%s serialised something containing %q: %s", name, forbidden, raw)
			}
		}
	}
}

func currentUserID(t *testing.T, set *bindings.Set) id.ID {
	t.Helper()
	me := set.Auth.Me()
	if !me.OK || !me.Data.SignedIn {
		t.Fatal("not signed in")
	}
	return id.ID(me.Data.UserID)
}

// ── authorization ───────────────────────────────────────────────────────────────

// TestAPermissionlessUserIsRefused is the authorization drill.
//
// Signed in, valid session, correct everything — and still refused, because the role holds no
// grant. That is the difference between authentication and authorization, and the case a
// system that only checks "are you logged in?" gets wrong.
//
// Mutation check: make guard skip the Can call, and this passes when it must not.
func TestAPermissionlessUserIsRefused(t *testing.T) {
	set, app := signedIn(t)
	ctx := app.Context()

	// Strip every grant from the administrator role.
	company, err := app.Org.Company(ctx)
	if err != nil {
		t.Fatalf("Company: %v", err)
	}
	roles, err := app.Identity.Roles(ctx, company.ID)
	if err != nil {
		t.Fatalf("Roles: %v", err)
	}
	for _, r := range roles {
		grants, grantErr := app.Identity.RoleGrants(ctx, r.ID)
		if grantErr != nil {
			t.Fatalf("RoleGrants: %v", grantErr)
		}
		for _, g := range grants {
			if err = app.Identity.RevokeFromRole(ctx, r.ID, g); err != nil {
				t.Fatalf("RevokeFromRole: %v", err)
			}
		}
	}

	r := set.System.Health()
	if r.OK {
		t.Fatal("a signed-in user with NO permissions reached a guarded method — " +
			"the guard authenticates but does not authorize")
	}
	if r.Error.Code != bindings.CodeForbidden {
		t.Errorf("code = %q, want %q", r.Error.Code, bindings.CodeForbidden)
	}
}

// TestForbiddenIsAPermissionCategory so the frontend renders the permission-denied state built
// in Step 0.11 (D6) rather than a generic error.
func TestForbiddenIsAPermissionCategory(t *testing.T) {
	set, app := signedIn(t)
	ctx := app.Context()

	company, _ := app.Org.Company(ctx)
	roles, _ := app.Identity.Roles(ctx, company.ID)
	for _, r := range roles {
		grants, _ := app.Identity.RoleGrants(ctx, r.ID)
		for _, g := range grants {
			_ = app.Identity.RevokeFromRole(ctx, r.ID, g)
		}
	}

	raw, err := json.Marshal(set.Ops.Jobs())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), bindings.CodeForbidden) {
		t.Errorf("the denial does not carry the forbidden code: %s", raw)
	}
	// And no developer prose crosses (§22.2).
	if strings.Contains(strings.ToLower(string(raw)), "may not perform") {
		t.Errorf("developer text crossed the boundary: %s", raw)
	}
}

// TestPreferencesNowResolveAtUserScope closes the limitation Step 0.11 (D4) recorded: it wrote
// at system scope because no user existed.
func TestPreferencesNowResolveAtUserScope(t *testing.T) {
	set, app := signedIn(t)

	if r := set.Config.SetTheme("dark"); !r.OK {
		t.Fatalf("SetTheme: %+v", r.Error)
	}
	if r := set.Config.Preferences(); r.Data.Theme != "dark" {
		t.Errorf("theme = %q, want dark", r.Data.Theme)
	}

	// Stored at USER scope, not system: a second person on this machine keeps their own.
	var scope string
	if err := app.DB.Reader(app.Context()).QueryRowContext(app.Context(),
		`SELECT scope FROM settings WHERE setting_key = 'ui.theme'`).Scan(&scope); err != nil {
		t.Fatalf("reading the setting: %v", err)
	}
	if scope != "user" {
		t.Errorf("ui.theme stored at %q scope, want user — two people sharing a machine "+
			"would overwrite each other's theme", scope)
	}
}

// TestScopeForUsesTheSessionBranch documents that a policy names a scope KIND, and the session
// supplies the identifier.
func TestScopeForUsesTheSessionBranch(t *testing.T) {
	set, app := signedIn(t)
	ctx := app.Context()

	branch, err := app.Org.DefaultBranch(ctx)
	if err != nil {
		t.Fatalf("DefaultBranch: %v", err)
	}

	// The administrator's * grant is global, so a branch-scoped check still passes.
	actor := appctx.WithActor(ctx, appctx.Actor{
		UserID:    currentUserID(t, set),
		CompanyID: mustCompanyID(t, app),
		BranchID:  branch.ID,
	})
	if !app.Identity.Can(actor, identity.PermUserView, auth.InBranch(branch.ID)) {
		t.Error("a global grant did not satisfy a branch-scoped check")
	}
}

func mustCompanyID(t *testing.T, app *bootstrap.App) id.ID {
	t.Helper()
	company, err := app.Org.Company(app.Context())
	if err != nil {
		t.Fatalf("Company: %v", err)
	}
	return company.ID
}
