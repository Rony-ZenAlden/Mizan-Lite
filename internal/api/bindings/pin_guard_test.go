package bindings_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/api/appctx"
	"github.com/mizan-erp/mizan/internal/api/bindings"
	"github.com/mizan-erp/mizan/internal/modules/identity"
)

// ── the bound, through the REAL guard ───────────────────────────────────────────
//
// # Why this test exists, and why it is here rather than in the identity package
//
// The identity tests for the PIN bound stamp `PINSession: true` onto the context by hand, because
// that is what the guard does. A mutation drill then removed that line FROM THE GUARD — and every
// one of those tests still passed, because none of them went through it.
//
// The consequence would have been the worst defect in this codebase: every till session holding
// the full authority of the user behind it, so a guessed four-digit PIN would reach the admin
// screen, the ledger, and stock costs. Nothing would have looked wrong.
//
// So the bound is asserted HERE, where a real session token travels the real path: sign in with a
// password, take a PIN session from it, and call a guarded binding method that a till must not
// reach. This is the Phase 3 rule at its most expensive — *a test must name the mechanism it is
// about* — and the mechanism is the guard, not the context helper.

// pinSession signs an administrator in, gives them a PIN, and returns a till session for them.
func pinSession(t *testing.T) (*bindings.Set, string) {
	t.Helper()
	set, app := signedIn(t)
	ctx := app.Context()

	// The administrator holds EVERYTHING, which is what makes the bound worth testing: a user
	// with no grants would pass whether or not it existed.
	me := set.Auth.Me()
	if !me.OK || len(me.Data.Permissions) == 0 {
		t.Fatalf("the signed-in user holds nothing: %+v", me)
	}

	// The device session, taken through the service so the test holds its token. The binding's
	// SessionDTO deliberately does not expose one — a token that crossed to the frontend as data
	// would be a token in a log somewhere.
	device, err := app.Identity.Login(ctx, identity.LoginInput{
		Username: "admin", Password: "a sufficiently long passphrase",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	// SetOwnPIN needs an actor on the context, which the guard normally supplies. Stamped
	// directly here, because this test is about what the guard does with a PIN SESSION, not
	// about how a PIN gets set.
	if err = app.Identity.SetOwnPIN(
		appctx.WithActor(ctx, appctx.Actor{UserID: device.Principal.UserID}),
		"4729"); err != nil {
		t.Fatalf("SetOwnPIN: %v", err)
	}

	till, err := app.Identity.PINLogin(ctx, identity.PINLoginInput{
		DeviceToken: device.Token, Username: "admin", PIN: "4729",
	})
	if err != nil {
		t.Fatalf("PINLogin: %v", err)
	}
	return set, till.Token
}

// THE test. A till session, through the real guard, must not reach an administrative binding.
func TestATillSessionCannotReachAdministrativeBindings(t *testing.T) {
	set, tillToken := pinSession(t)

	// The session the bindings act under is the one just issued.
	set.AdoptSessionForTest(tillToken)

	// What a till needs.
	if products := set.Catalog.Products(); !products.OK {
		t.Errorf("a till cannot read the catalog: %+v", products.Error)
	}

	// What a guessed PIN must not reach. Each of these is a method the ADMINISTRATOR behind this
	// PIN is fully entitled to call with a password.
	refused := map[string]func() (bool, string){
		"Identity.Users": func() (bool, string) {
			r := set.Identity.Users()
			return r.OK, codeOf(r.Error)
		},
		"Audit.Entries": func() (bool, string) {
			r := set.Audit.Entries(bindings.AuditFilterDTO{})
			return r.OK, codeOf(r.Error)
		},
		"Accounting.Chart": func() (bool, string) {
			r := set.Accounting.Chart()
			return r.OK, codeOf(r.Error)
		},
	}
	for name, call := range refused {
		ok, _ := call()
		if ok {
			t.Errorf("a till session reached %s — a guessed PIN would too", name)
		}
	}
}

// The cost of stock is what the business paid. A cashier holding a till session must not read it
// off the screen, and the permission split exists precisely for this.
func TestATillSessionCannotReadStockCosts(t *testing.T) {
	set, tillToken := pinSession(t)
	set.AdoptSessionForTest(tillToken)

	stock := set.Inventory.Stock("")
	if !stock.OK {
		t.Fatalf("a till cannot see what is in stock: %+v", stock.Error)
	}
	for _, row := range stock.Data {
		if row.AverageCostMicro != nil || row.ValueMinor != nil {
			t.Error("a till session was shown what the stock cost")
		}
	}
}
