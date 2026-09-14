package api_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/api"
	"github.com/mizan-erp/mizan/internal/lite/catalog"
	catalogdomain "github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	ownerdomain "github.com/mizan-erp/mizan/internal/lite/owner/domain"
	"github.com/mizan-erp/mizan/internal/lite/setup"
)

const testPIN = "246813"

func firstRun(t *testing.T, set *api.Set) string {
	t.Helper()
	status := set.App.FirstRunStatus()
	if !status.OK || status.Data.Complete {
		t.Fatalf("FirstRunStatus before first run = %+v", status)
	}
	done := set.App.CompleteFirstRun(api.FirstRunInput{ShopName: "بقالية المونة", Locale: "ar", PIN: testPIN, Rate: "15000"})
	if !done.OK || done.Data.RecoveryCode == "" {
		t.Fatalf("CompleteFirstRun = %+v", done)
	}
	if again := set.App.FirstRunStatus(); !again.Data.Complete {
		t.Fatal("first run not reported complete")
	}
	return done.Data.RecoveryCode
}

func TestFirstRunThroughTheBindings(t *testing.T) {
	set, _ := attached(t, litetest.Logger())
	firstRun(t, set)
	if s := set.Settings.Get(); s.Data.ShopName != "بقالية المونة" {
		t.Fatalf("shop name = %+v", s.Data)
	}
	second := set.App.CompleteFirstRun(api.FirstRunInput{ShopName: "x", Locale: "ar", PIN: "739251", Rate: "15000"})
	if code := codeOf(t, second); code != setup.CodeAlreadyComplete {
		t.Fatalf("second first run = %s", code)
	}
}

func TestTheCatalogueThroughTheBindings(t *testing.T) {
	set, _ := attached(t, litetest.Logger())
	firstRun(t, set)

	units := set.Catalog.Units()
	if !units.OK || len(units.Data) != 9 || units.Data[0] != (api.UnitDTO{Code: "kg", Kind: "mass", InputDecimals: 3}) {
		t.Fatalf("Units = %+v", units)
	}
	if cur := set.Catalog.Currencies(); !cur.OK || len(cur.Data) != 2 || cur.Data[0].Code != "SYP" {
		t.Fatalf("Currencies = %+v", cur)
	}

	created := set.Catalog.CreateProduct(api.CreateProductInput{
		NameAR: "زيت زيتون", NameEN: "Olive oil", Barcode: "٦٢٢٣", UnitCode: "l", PriceCurrency: "USD", Price: "٣٫٢٥",
	})
	if !created.OK {
		t.Fatalf("CreateProduct = %+v", created.Error)
	}
	p := created.Data
	if p.Price != "3.25" || p.Barcode != "6223" || p.RowVersion != 1 || !p.Active {
		t.Fatalf("created = %+v", p)
	}

	if dup := set.Catalog.CreateProduct(api.CreateProductInput{NameAR: "زيـت زيتون", UnitCode: "l", PriceCurrency: "USD", Price: "1"}); codeOf(t, dup) != catalogdomain.CodeDuplicateName {
		t.Fatal("a second spelling was created through the bindings")
	}

	if found := set.Catalog.Products(api.ProductQueryDTO{Text: "زَيت"}); !found.OK || len(found.Data) != 1 {
		t.Fatalf("Products = %+v", found)
	}
	if one := set.Catalog.Product(p.ID); !one.OK || one.Data != p {
		t.Fatalf("Product = %+v", one)
	}
	if missing := set.Catalog.Product("not-an-id"); codeOf(t, missing) != catalogdomain.CodeNotFound {
		t.Fatal("an unparsable id was not NotFound")
	}

	renamed := set.Catalog.UpdateProduct(api.UpdateProductInput{ID: p.ID, RowVersion: p.RowVersion, NameAR: "زيت زيتون بلدي", NameEN: "Olive oil", Barcode: "6223"})
	if !renamed.OK || renamed.Data.RowVersion != 2 {
		t.Fatalf("UpdateProduct = %+v", renamed)
	}

	// The guarded price change, as the frontend sees it: refused with a code, then accepted in owner mode.
	cut := api.SetPriceInput{ID: p.ID, RowVersion: renamed.Data.RowVersion, PriceCurrency: "SYP", Price: "45000"}
	if code := codeOf(t, set.Catalog.SetPrice(cut)); code != ownerdomain.CodeRequired {
		t.Fatalf("SetPrice outside owner mode = %s", code)
	}
	elevated := set.Owner.Elevate(api.PINInput{PIN: testPIN})
	if !elevated.OK || elevated.Data.ElevatedSeconds != 120 {
		t.Fatalf("Elevate = %+v", elevated)
	}
	priced := set.Catalog.SetPrice(cut)
	if !priced.OK || priced.Data.Price != "45000" || priced.Data.PriceCurrency != "SYP" {
		t.Fatalf("SetPrice in owner mode = %+v", priced)
	}

	slotted := set.Catalog.SetQuickSlot(api.SetQuickSlotInput{ID: p.ID, Slot: 4})
	if !slotted.OK || slotted.Data.QuickSlot != 4 {
		t.Fatalf("SetQuickSlot = %+v", slotted)
	}
	if bad := set.Catalog.SetQuickSlot(api.SetQuickSlotInput{ID: p.ID, Slot: 1 << 40}); codeOf(t, bad) != catalogdomain.CodeSlotInvalid {
		t.Fatal("an absurd slot number was not refused as no such button")
	}

	off := set.Catalog.SetActive(api.SetActiveInput{ID: p.ID, RowVersion: slotted.Data.RowVersion, Active: false})
	if !off.OK || off.Data.Active || off.Data.QuickSlot != 0 {
		t.Fatalf("SetActive(false) = %+v", off)
	}

	events := set.Owner.Events(50)
	if !events.OK {
		t.Fatal(events.Error)
	}
	acts := map[string]bool{}
	for _, e := range events.Data {
		if e.Kind == "guarded_act" {
			acts[e.Action] = true
			if e.SubjectID != p.ID {
				t.Errorf("act %s names subject %s, want %s", e.Action, e.SubjectID, p.ID)
			}
		}
	}
	if !acts[catalog.ActPriceChange] || !acts[catalog.ActDeactivate] {
		t.Fatalf("the owner's history is missing an act: %+v", events.Data)
	}
}

func TestTheOwnerThroughTheBindings(t *testing.T) {
	set, _ := attached(t, litetest.Logger())
	recovery := firstRun(t, set)

	if s := set.Owner.Status(); !s.OK || !s.Data.SetUp || s.Data.ElevatedSeconds != 0 || s.Data.LockedSeconds != 0 {
		t.Fatalf("Status = %+v", s)
	}
	wrong := set.Owner.Elevate(api.PINInput{PIN: "739251"})
	if codeOf(t, wrong) != ownerdomain.CodeWrongPIN || wrong.Error.Params["remaining"] != "3" {
		t.Fatalf("a wrong PIN = %+v", wrong.Error)
	}
	if s := set.Owner.Elevate(api.PINInput{PIN: testPIN}); !s.OK {
		t.Fatal(s.Error)
	}
	if s := set.Owner.EndElevation(); !s.OK || s.Data.ElevatedSeconds != 0 {
		t.Fatalf("EndElevation = %+v", s)
	}
	if s := set.Owner.ChangePIN(api.ChangePINInput{CurrentPIN: testPIN, NewPIN: "581937"}); !s.OK {
		t.Fatalf("ChangePIN = %+v", s.Error)
	}
	recovered := set.Owner.Recover(api.RecoverInput{RecoveryCode: recovery, NewPIN: "905173"})
	if !recovered.OK || recovered.Data.RecoveryCode == "" || recovered.Data.RecoveryCode == recovery {
		t.Fatalf("Recover = %+v", recovered)
	}
	if s := set.Owner.Elevate(api.PINInput{PIN: "905173"}); !s.OK {
		t.Fatalf("the recovered PIN = %+v", s.Error)
	}
}

func TestNoResponseEverCarriesACredential(t *testing.T) {
	set, _ := attached(t, litetest.Logger())
	recovery := firstRun(t, set)
	_ = set.Owner.Elevate(api.PINInput{PIN: "739251"})
	_ = set.Owner.Elevate(api.PINInput{PIN: testPIN})

	for name, result := range map[string]any{
		"status": set.Owner.Status(), "events": set.Owner.Events(100), "settings": set.Settings.Get(),
		"wrong": set.Owner.Elevate(api.PINInput{PIN: "905173"}),
	} {
		wire, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{testPIN, "739251", "905173", recovery, "argon2"} {
			if strings.Contains(string(wire), secret) {
				t.Fatalf("%s crossed the boundary carrying %q: %s", name, secret, wire)
			}
		}
	}
}
