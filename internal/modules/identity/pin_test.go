package identity_test

import (
	"context"
	"testing"

	"github.com/mizan-erp/mizan/internal/api/appctx"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/identity"
	"github.com/mizan-erp/mizan/internal/modules/identity/domain"
	"github.com/mizan-erp/mizan/internal/platform/auth"
)

// asUser stamps a principal onto the context, the way the binding guard does after a session
// validates.
func asUser(ctx context.Context, userID id.ID, pinSession bool) context.Context {
	return appctx.WithActor(ctx, appctx.Actor{
		UserID: userID, Username: "test", PINSession: pinSession,
	})
}

// unlockedTerminal signs a manager in with a password, which is what a PIN needs to exist.
func unlockedTerminal(
	t *testing.T, svc *identity.Service, ctx context.Context, companyID id.ID,
) (identity.LoginResult, domain.User) {
	t.Helper()
	manager := createUser(t, svc, ctx, companyID, "manager", "correct-horse-battery")
	result, err := svc.Login(ctx, identity.LoginInput{
		Username: "manager", Password: "correct-horse-battery",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	return result, manager
}

// ── a PIN is never a way in from nothing ────────────────────────────────────────

// The first of the three protections, and the one that makes the other two worth having: somebody
// signed the terminal in with a real password, and a PIN changes who is standing at it.
func TestAPINCannotBeUsedWithoutAnUnlockedTerminal(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)

	cashier := createUser(t, svc, ctx, companyID, "cashier", "long-enough-password")
	if err := svc.SetOwnPIN(asUser(ctx, cashier.ID, false), "4729"); err != nil {
		t.Fatalf("SetOwnPIN: %v", err)
	}

	// No device token at all.
	_, err := svc.PINLogin(ctx, identity.PINLoginInput{Username: "cashier", PIN: "4729"})
	if err == nil {
		t.Fatal("a PIN signed somebody in with no terminal session")
	}
	if code := errs.CodeOf(err); code != domain.CodeDeviceRequired {
		t.Errorf("code = %q, want %q", code, domain.CodeDeviceRequired)
	}

	// And a made-up one.
	if _, err = svc.PINLogin(ctx, identity.PINLoginInput{
		DeviceToken: "not-a-real-token", Username: "cashier", PIN: "4729",
	}); err == nil {
		t.Fatal("a fabricated device token was accepted")
	}
}

// One guessed PIN must not let an attacker walk the whole staff list, each hop looking as
// legitimate as the last.
func TestATillSessionCannotSignInAnotherUser(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)

	device, _ := unlockedTerminal(t, svc, ctx, companyID)
	first := createUser(t, svc, ctx, companyID, "cashier1", "long-enough-password")
	second := createUser(t, svc, ctx, companyID, "cashier2", "long-enough-password")
	for _, user := range []domain.User{first, second} {
		if err := svc.SetOwnPIN(asUser(ctx, user.ID, false), "4729"); err != nil {
			t.Fatalf("SetOwnPIN: %v", err)
		}
	}

	till, err := svc.PINLogin(ctx, identity.PINLoginInput{
		DeviceToken: device.Token, Username: "cashier1", PIN: "4729",
	})
	if err != nil {
		t.Fatalf("PINLogin: %v", err)
	}

	// The till session tries to authorise a second one.
	if _, err = svc.PINLogin(ctx, identity.PINLoginInput{
		DeviceToken: till.Token, Username: "cashier2", PIN: "4729",
	}); err == nil {
		t.Fatal("a till session signed in another user")
	}
}

// ── the bound that makes a short credential acceptable ──────────────────────────

// THE test of this step. A manager with every permission in the system signs in by PIN and gets a
// till — so guessing a PIN buys the ability to sell things, not to change prices or read the
// ledger.
func TestAPINSessionIsBoundedEvenForAUserWhoHoldsEverything(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)

	device, manager := unlockedTerminal(t, svc, ctx, companyID)
	if err := svc.SetOwnPIN(asUser(ctx, manager.ID, false), "4729"); err != nil {
		t.Fatalf("SetOwnPIN: %v", err)
	}
	grantEverything(t, svc, ctx, companyID, manager.ID)

	// With a full session, the manager can do administrative things.
	full := asUser(ctx, manager.ID, false)
	if !svc.Can(full, "identity.user.manage", auth.Global()) {
		t.Fatal("the manager cannot administer users with a full session — the grant failed")
	}

	// The same person, signed in by PIN.
	till, err := svc.PINLogin(ctx, identity.PINLoginInput{
		DeviceToken: device.Token, Username: "manager", PIN: "4729",
	})
	if err != nil {
		t.Fatalf("PINLogin: %v", err)
	}
	if till.Session.AuthMethod != domain.AuthPIN {
		t.Fatalf("auth method = %q, want pin", till.Session.AuthMethod)
	}

	pinCtx := asUser(ctx, manager.ID, true)

	// What a till needs, it has.
	for _, permission := range []string{
		"sales.document.post", "pos.shift.open", "catalog.view", "pricing.view",
	} {
		if !svc.Can(pinCtx, permission, auth.Global()) {
			t.Errorf("a till session cannot do %q, which it needs", permission)
		}
	}

	// What it must not have, it does not — even though this user holds every one of them.
	for _, permission := range []string{
		"identity.user.manage", "identity.role.manage", "pricing.manage",
		"inventory.stock.adjust", "inventory.cost.view", "accounting.account.view",
		"audit.entry.view", "sales.series.manage",
	} {
		if svc.Can(pinCtx, permission, auth.Global()) {
			t.Errorf("a guessed PIN would reach %q", permission)
		}
	}
}

// The bound is applied at the SINGLE point every guarded call passes through. A check at each
// call site would be a check somebody forgets.
func TestTheBoundHoldsForAPermissionNobodyThoughtOf(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)

	device, manager := unlockedTerminal(t, svc, ctx, companyID)
	if err := svc.SetOwnPIN(asUser(ctx, manager.ID, false), "4729"); err != nil {
		t.Fatalf("SetOwnPIN: %v", err)
	}
	grantEverything(t, svc, ctx, companyID, manager.ID)

	if _, err := svc.PINLogin(ctx, identity.PINLoginInput{
		DeviceToken: device.Token, Username: "manager", PIN: "4729",
	}); err != nil {
		t.Fatalf("PINLogin: %v", err)
	}

	// A permission invented here, belonging to no namespace the bound allows. The point is that
	// the default is DENY: a module added next year is outside the till's reach until somebody
	// deliberately puts it inside.
	if svc.Can(asUser(ctx, manager.ID, true), "payroll.run", auth.Global()) {
		t.Error("a till session reached a permission from a module that does not exist yet")
	}
}

// ── setting a PIN ───────────────────────────────────────────────────────────────

func TestAWeakPINCannotBeSet(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)

	cashier := createUser(t, svc, ctx, companyID, "cashier", "long-enough-password")
	userCtx := asUser(ctx, cashier.ID, false)

	for _, pin := range []string{"0000", "1234", "12", "abcd"} {
		if err := svc.SetOwnPIN(userCtx, pin); err == nil {
			t.Errorf("the PIN %q was accepted", pin)
		}
	}
	if err := svc.SetOwnPIN(userCtx, "4729"); err != nil {
		t.Errorf("an ordinary PIN was refused: %v", err)
	}
}

// Setting your own PIN needs no permission: a cashier setting the PIN they will type forty times
// a shift is not an administrative act, and the people who need it most hold the least (1.11 D3).
func TestSettingYourOwnPINNeedsNoPermission(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)

	cashier := createUser(t, svc, ctx, companyID, "cashier", "long-enough-password")
	// No grants at all.
	if err := svc.SetOwnPIN(asUser(ctx, cashier.ID, false), "4729"); err != nil {
		t.Errorf("a user with no permissions could not set their own PIN: %v", err)
	}
}

// Setting somebody ELSE's is administrative.
func TestSettingAnotherUsersPINIsGated(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)

	cashier := createUser(t, svc, ctx, companyID, "cashier", "long-enough-password")
	other := createUser(t, svc, ctx, companyID, "other", "long-enough-password")

	if err := svc.SetPINFor(asUser(ctx, cashier.ID, false), other.ID, "4729"); err == nil {
		t.Fatal("a user with no permission set somebody else's PIN")
	}
}

// ── what a wrong PIN reveals ────────────────────────────────────────────────────

// "This person has no PIN" and "that PIN is wrong" must be indistinguishable, or the till becomes
// a way to discover who has one.
func TestAUserWithNoPINIsIndistinguishableFromAWrongPIN(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)

	device, _ := unlockedTerminal(t, svc, ctx, companyID)
	withPIN := createUser(t, svc, ctx, companyID, "haspin", "long-enough-password")
	createUser(t, svc, ctx, companyID, "nopin", "long-enough-password")
	if err := svc.SetOwnPIN(asUser(ctx, withPIN.ID, false), "4729"); err != nil {
		t.Fatalf("SetOwnPIN: %v", err)
	}

	_, wrongErr := svc.PINLogin(ctx, identity.PINLoginInput{
		DeviceToken: device.Token, Username: "haspin", PIN: "1357",
	})
	_, noneErr := svc.PINLogin(ctx, identity.PINLoginInput{
		DeviceToken: device.Token, Username: "nopin", PIN: "1357",
	})
	_, unknownErr := svc.PINLogin(ctx, identity.PINLoginInput{
		DeviceToken: device.Token, Username: "ghost", PIN: "1357",
	})

	if wrongErr == nil || noneErr == nil || unknownErr == nil {
		t.Fatal("one of the three bad attempts succeeded")
	}
	if errs.CodeOf(wrongErr) != errs.CodeOf(noneErr) ||
		errs.CodeOf(noneErr) != errs.CodeOf(unknownErr) {
		t.Errorf("three different answers: %q, %q, %q — the till reveals who has a PIN",
			errs.CodeOf(wrongErr), errs.CodeOf(noneErr), errs.CodeOf(unknownErr))
	}
}

// ── the till session's branch ───────────────────────────────────────────────────

// A cashier signing in at the second shop's till is working at the second shop, whatever their
// record says.
func TestATillSessionInheritsTheTerminalsBranch(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)

	device, _ := unlockedTerminal(t, svc, ctx, companyID)
	cashier := createUser(t, svc, ctx, companyID, "cashier", "long-enough-password")
	if err := svc.SetOwnPIN(asUser(ctx, cashier.ID, false), "4729"); err != nil {
		t.Fatalf("SetOwnPIN: %v", err)
	}

	till, err := svc.PINLogin(ctx, identity.PINLoginInput{
		DeviceToken: device.Token, Username: "cashier", PIN: "4729",
	})
	if err != nil {
		t.Fatalf("PINLogin: %v", err)
	}
	if till.Session.BranchID != device.Session.BranchID {
		t.Error("the till session is working in a different branch from its terminal")
	}
	// And it remembers which terminal vouched for it.
	if till.Session.DeviceSessionID != device.Session.ID {
		t.Error("the till session does not name the terminal it was issued from")
	}
}

// grantEverything gives a user the ADMINISTRATOR role.
//
// The bound is only worth testing against somebody who genuinely holds everything. Testing it
// against a user with no grants would pass whether or not the bound existed — the exact shape of
// weak test this codebase has caught six times.
func grantEverything(
	t *testing.T, svc *identity.Service, ctx context.Context, companyID, userID id.ID,
) {
	t.Helper()
	syncAll(t, svc, ctx)
	if err := svc.SeedRoles(ctx, companyID); err != nil {
		t.Fatalf("SeedRoles: %v", err)
	}
	roles, err := svc.Roles(ctx, companyID)
	if err != nil {
		t.Fatalf("Roles: %v", err)
	}
	for _, role := range roles {
		if role.Code != "administrator" {
			continue
		}
		if err = svc.AssignRole(ctx, userID, role.ID, auth.Global()); err != nil {
			t.Fatalf("AssignRole: %v", err)
		}
		return
	}
	t.Fatal("the administrator role was not seeded")
}
