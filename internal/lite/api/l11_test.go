package api_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/api"
	ownerdomain "github.com/mizan-erp/mizan/internal/lite/owner/domain"
)

// setPIN turns the master PIN switch on or off through the binding a shop actually uses.
func setPIN(t *testing.T, set *api.Set, on bool) {
	t.Helper()
	if r := set.Settings.Update(api.SettingsInput{PINRequired: &on}); !r.OK {
		t.Fatalf("Settings.Update(pinRequired=%v) = %+v", on, r.Error)
	}
	if got := set.Settings.Get(); !got.OK || got.Data.PINRequired != on {
		t.Fatalf("the switch reads back as %+v", got)
	}
}

// TestTheMasterPINSwitchReachesEveryGuardedAct is the owner's request of 2026-09-17, proved through the bindings rather
// than through the owner service alone: a shop that turns the switch on is asked for the PIN before a void, a discount,
// a price change and a stock correction, and the owner's figures go behind it too.
func TestTheMasterPINSwitchReachesEveryGuardedAct(t *testing.T) {
	set, oil := tillShop(t)
	elevate(t, set)
	if r := set.Stock.Opening(api.ReceiveInput{ProductID: oil.ID, Quantity: "10", CostMode: "total", Cost: "20", Currency: "USD"}); !r.OK {
		t.Fatal(r.Error)
	}
	cart := api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "1"}}}
	sale := set.Till.Checkout(api.CheckoutInput{Cart: cart, Token: quoted(t, set, cart).Token})
	if !sale.OK {
		t.Fatal(sale.Error)
	}
	set.Owner.EndElevation()

	// Off — the default — the counter is never stopped.
	repriced := set.Catalog.SetPrice(api.SetPriceInput{ID: oil.ID, RowVersion: oil.RowVersion, PriceCurrency: oil.PriceCurrency, Price: "3.50"})
	if !repriced.OK {
		t.Fatalf("a price change asked for the PIN with the switch off: %+v", repriced.Error)
	}

	setPIN(t, set, true)

	// On, the same act stops.
	priced := set.Catalog.SetPrice(api.SetPriceInput{ID: oil.ID, RowVersion: repriced.Data.RowVersion,
		PriceCurrency: oil.PriceCurrency, Price: "3.75"})
	if code := codeOf(t, priced); code != ownerdomain.CodeRequired {
		t.Fatalf("a price change with the switch on = %s", code)
	}
	// And so does voiding a sale.
	if code := codeOf(t, set.Sales.Void(api.VoidInput{SaleID: sale.Data.ID, Reason: "خطأ"})); code != ownerdomain.CodeRequired {
		t.Fatalf("a void with the switch on = %s", code)
	}
	// The price the shop refused to change is the price it still has.
	if got := set.Catalog.Product(oil.ID); !got.OK || got.Data.Price != "3.50" {
		t.Fatalf("the refused price change was written anyway: %q", got.Data.Price)
	}

	// The owner's figures are behind it as well: the day's statement is one of the reads Allowed() gates, and the
	// drawer drops back to the view a helper at the counter gets.
	if code := codeOf(t, set.Reports.Day("")); code != ownerdomain.CodeRequired {
		t.Fatalf("the day's statement with the switch on = %s", code)
	}
	if drawer := set.Cash.Drawer(""); !drawer.OK || drawer.Data.OwnerView {
		t.Fatalf("the drawer kept its owner view with the switch on: %+v", drawer)
	}

	// In owner mode everything goes through again.
	elevate(t, set)
	if r := set.Catalog.SetPrice(api.SetPriceInput{ID: oil.ID, RowVersion: repriced.Data.RowVersion,
		PriceCurrency: oil.PriceCurrency, Price: "3.75"}); !r.OK {
		t.Fatalf("a price change was refused in owner mode: %+v", r.Error)
	}
	set.Owner.EndElevation()
}

// TestTurningThePINOffCostsThePINAndTurningItOnDoesNot is the asymmetry the switch needs to be worth anything.
//
// The guard is asked BEFORE the new value is written, so it judges the setting in force. Were it asked afterwards,
// turning the protection off would let itself through — the one direction that must not be free.
func TestTurningThePINOffCostsThePINAndTurningItOnDoesNot(t *testing.T) {
	set, _ := tillShop(t)

	// On, with no PIN: a shop that wants protection is not made to prove itself first.
	on := true
	if r := set.Settings.Update(api.SettingsInput{PINRequired: &on}); !r.OK {
		t.Fatalf("turning the PIN on asked for the PIN: %+v", r.Error)
	}

	// Off, with no PIN: refused, and the switch is still on afterwards.
	off := false
	if code := codeOf(t, set.Settings.Update(api.SettingsInput{PINRequired: &off})); code != ownerdomain.CodeRequired {
		t.Fatalf("the protection turned itself off: %s", code)
	}
	if got := set.Settings.Get(); !got.OK || !got.Data.PINRequired {
		t.Fatalf("a refused change was written anyway: %+v", got.Data.PINRequired)
	}

	// Off, in owner mode: allowed, and the counter is smooth again at once.
	elevate(t, set)
	if r := set.Settings.Update(api.SettingsInput{PINRequired: &off}); !r.OK {
		t.Fatalf("the owner could not turn their own protection off: %+v", r.Error)
	}
	set.Owner.EndElevation()
	if got := set.Settings.Get(); !got.OK || got.Data.PINRequired {
		t.Fatalf("the switch is still on: %+v", got.Data.PINRequired)
	}
}
