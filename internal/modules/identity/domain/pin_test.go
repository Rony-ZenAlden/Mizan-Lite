package domain_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/modules/identity/domain"
)

// ── what a PIN may be ───────────────────────────────────────────────────────────

func TestAnOrdinaryPINIsAccepted(t *testing.T) {
	for _, pin := range []string{"4729", "817263", "902847561"} {
		if err := domain.ValidatePIN(pin); err != nil {
			t.Errorf("ValidatePIN(%q): %v", pin, err)
		}
	}
}

// The common PINs are tried FIRST. Throttling buys time against exhaustive search; it buys
// nothing against an attacker who tries 1234 on every till in the shop.
func TestTheObviousPINsAreRefused(t *testing.T) {
	cases := map[string]string{
		"0000":   "every digit the same",
		"1111":   "every digit the same",
		"1234":   "ascending",
		"4321":   "descending",
		"123456": "ascending, longer",
		"987654": "descending, longer",
	}
	for pin, why := range cases {
		err := domain.ValidatePIN(pin)
		if err == nil {
			t.Errorf("ValidatePIN(%q) accepted a PIN that is %s", pin, why)
			continue
		}
		if code := errs.CodeOf(err); code != domain.CodeWeakPIN {
			t.Errorf("ValidatePIN(%q) gave %q, want %q", pin, code, domain.CodeWeakPIN)
		}
	}
}

// A two-digit PIN is a hundred guesses, which no throttle can make reasonable.
func TestAPINMustBeLongEnoughAndShortEnough(t *testing.T) {
	for _, pin := range []string{"", "1", "72", "836"} {
		if err := domain.ValidatePIN(pin); err == nil {
			t.Errorf("ValidatePIN(%q) accepted a PIN shorter than four digits", pin)
		}
	}
	if err := domain.ValidatePIN("1234567890123"); err == nil {
		t.Error("a thirteen-digit PIN was accepted")
	}
}

// A till keypad has no letters, and a PIN somebody cannot enter at the till is a PIN they will
// write on a note stuck to the screen.
func TestAPINMustBeDigitsOnly(t *testing.T) {
	for _, pin := range []string{"12a4", "pass", "47 29", "47-29"} {
		if err := domain.ValidatePIN(pin); err == nil {
			t.Errorf("ValidatePIN(%q) accepted a non-numeric PIN", pin)
		}
	}
}

// ── the bound that makes a short credential acceptable ──────────────────────────

// A PIN session holds the INTERSECTION of the user's grants and this bound. Guessing a PIN buys
// an attacker the ability to sell things — not to change prices, edit stock, or read the ledger.
func TestAPINSessionMayHoldOnlyPointOfSalePermissions(t *testing.T) {
	allowed := []string{
		"sales.document.view", "sales.document.draft", "sales.document.post",
		"pos.shift.open", "pos.shift.close",
		"catalog.view", "partner.customer.view", "pricing.view",
		"inventory.stock.view",
	}
	for _, permission := range allowed {
		if !domain.PINMayHold(permission) {
			t.Errorf("a till cannot do %q, which it needs", permission)
		}
	}

	// The ones that matter: a guessed PIN must not reach any of these.
	refused := []string{
		"identity.user.manage", "identity.role.manage",
		"pricing.manage", "catalog.manage",
		"inventory.stock.adjust", "inventory.cost.view",
		"accounting.account.view", "audit.entry.view",
		"partner.supplier.view", "sales.series.manage",
	}
	for _, permission := range refused {
		if domain.PINMayHold(permission) {
			t.Errorf("a guessed PIN would reach %q", permission)
		}
	}
}

// The prefix must not match a longer namespace by accident: `pricing.view` is allowed and
// `pricing.manage` is not, and a careless prefix would let the second through.
func TestThePointOfSaleBoundDoesNotLeakThroughPrefixes(t *testing.T) {
	if domain.PINMayHold("pricing.manage") {
		t.Error("pricing.manage matched a prefix meant for pricing.view")
	}
	if domain.PINMayHold("catalog.manage") {
		t.Error("catalog.manage matched a prefix meant for catalog.view")
	}
	// And an empty permission is not "everything".
	if domain.PINMayHold("") {
		t.Error("an empty permission was within the bound")
	}
}

// A cost figure is what the business paid, and a cashier holding a PIN should not read it off a
// till. This is the one inside `inventory.` that must NOT be allowed, and it is worth its own
// test because the prefix `inventory.stock.view` was chosen precisely to exclude it.
func TestAPINCannotSeeWhatStockCost(t *testing.T) {
	if domain.PINMayHold("inventory.cost.view") {
		t.Error("a till session could read stock costs")
	}
	if !domain.PINMayHold("inventory.stock.view") {
		t.Error("a till session cannot see whether something is in stock")
	}
}
