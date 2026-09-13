package catalog_test

import (
	"context"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/catalog"
	"github.com/mizan-erp/mizan/internal/lite/catalog/catalogtest"
	"github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
)

type fixture struct {
	svc   *catalog.Service
	store *catalogtest.Fake
	gate  *catalogtest.Gate
}

func newFixture() fixture {
	store, gate := catalogtest.NewFake(), &catalogtest.Gate{}
	return fixture{svc: catalog.NewService(litetest.Immediate{}, store, gate), store: store, gate: gate}
}

func draft(nameAR string) domain.Draft {
	return domain.Draft{NameAR: nameAR, UnitCode: "l", PriceCurrency: "USD", Price: "3.25"}
}

func (f fixture) create(t *testing.T, d domain.Draft) domain.Product {
	t.Helper()
	p, err := f.svc.Create(context.Background(), d)
	if err != nil {
		t.Fatalf("Create(%q): %v", d.NameAR, err)
	}
	return p
}

func param(t *testing.T, err error, key string) string {
	t.Helper()
	typed, ok := errs.AsError(err)
	if !ok {
		t.Fatalf("not typed: %v", err)
	}
	return typed.Params[key]
}

func TestDuplicateSpellingsAreOneProduct(t *testing.T) {
	f := newFixture()
	first := f.create(t, draft("أسطنبولي"))

	_, err := f.svc.Create(context.Background(), draft("إسطنبولي"))
	if errs.CodeOf(err) != domain.CodeDuplicateName {
		t.Fatalf("a second spelling was created: %v", err)
	}
	if param(t, err, "existingId") != first.ID.String() || param(t, err, "existingActive") != "true" {
		t.Fatalf("the refusal does not name the existing product: %v", err)
	}
}

func TestADuplicateOfAnInactiveProductOffersIt(t *testing.T) {
	f := newFixture()
	f.gate.Elevated = true
	old := f.create(t, draft("زيت قديم"))
	if _, err := f.svc.SetActive(context.Background(), catalog.SetActiveInput{ID: old.ID, RowVersion: old.RowVersion}); err != nil {
		t.Fatal(err)
	}
	_, err := f.svc.Create(context.Background(), draft("زيت قديم"))
	if errs.CodeOf(err) != domain.CodeDuplicateName || param(t, err, "existingActive") != "false" {
		t.Fatalf("err = %v", err)
	}
}

func TestBarcodeMatchesAcrossKeyboardLayouts(t *testing.T) {
	f := newFixture()
	d := draft("زيت")
	d.Barcode = "6223000112345"
	f.create(t, d)

	other := draft("سمن")
	other.Barcode = "٦٢٢٣٠٠٠١١٢٣٤٥" // the same barcode, scanned with an Arabic layout active
	if _, err := f.svc.Create(context.Background(), other); errs.CodeOf(err) != domain.CodeDuplicateBarcode {
		t.Fatalf("the same barcode in Arabic digits was accepted: %v", err)
	}

	found, err := f.svc.Search(context.Background(), "٦٢٢٣٠٠٠١١٢٣٤٥", false)
	if err != nil || len(found) != 1 || found[0].NameAR != "زيت" {
		t.Fatalf("scanning with an Arabic layout did not find the product: %v %v", found, err)
	}
}

func TestSearchFindsEverySpelling(t *testing.T) {
	f := newFixture()
	f.create(t, draft("دبس رمانة"))
	for _, query := range []string{"رمانه", "رمانة", "دبـــس", "دِبس"} {
		got, err := f.svc.Search(context.Background(), query, false)
		if err != nil || len(got) != 1 {
			t.Errorf("Search(%q) = %d results, %v", query, len(got), err)
		}
	}
}

func TestRenamingToItsOwnNameUnderAnotherSpellingIsAllowed(t *testing.T) {
	f := newFixture()
	p := f.create(t, draft("أسطنبولي"))
	updated, err := f.svc.Update(context.Background(), catalog.UpdateInput{ID: p.ID, RowVersion: p.RowVersion, NameAR: "اسطنبولي"})
	if err != nil || updated.NameAR != "اسطنبولي" {
		t.Fatalf("correcting a product's own spelling was refused as a duplicate: %v", err)
	}
}

func TestRenamingOntoAnotherProductIsRefused(t *testing.T) {
	f := newFixture()
	f.create(t, draft("زيت"))
	p := f.create(t, draft("سمن"))
	_, err := f.svc.Update(context.Background(), catalog.UpdateInput{ID: p.ID, RowVersion: p.RowVersion, NameAR: "زيـت"})
	if errs.CodeOf(err) != domain.CodeDuplicateName {
		t.Fatalf("err = %v", err)
	}
}

func TestPriceChangeWithoutTheOwnerIsRefusedAndWritesNothing(t *testing.T) {
	f := newFixture()
	p := f.create(t, draft("زيت"))

	_, err := f.svc.SetPrice(context.Background(), catalog.SetPriceInput{ID: p.ID, RowVersion: p.RowVersion, Currency: "USD", Price: "2.00"})
	if errs.CodeOf(err) != catalogtest.CodeRequired {
		t.Fatalf("a price was cut without the owner: %v", err)
	}
	stored, _ := f.svc.Get(context.Background(), p.ID)
	if stored.PriceMicro != 3_250_000 || stored.RowVersion != p.RowVersion {
		t.Fatalf("a refused price change was written: %+v", stored)
	}
}

func TestPriceChangeInOwnerModeIsRecorded(t *testing.T) {
	f := newFixture()
	f.gate.Elevated = true
	p := f.create(t, draft("زيت"))

	got, err := f.svc.SetPrice(context.Background(), catalog.SetPriceInput{ID: p.ID, RowVersion: p.RowVersion, Currency: "SYP", Price: "٤٥٠٠٠"})
	if err != nil || got.PriceCurrency != "SYP" || got.PriceMicro != 45_000_000_000 || got.RowVersion != p.RowVersion+1 {
		t.Fatalf("SetPrice = %+v, %v", got, err)
	}
	if len(f.gate.Acts) != 1 {
		t.Fatalf("acts = %+v", f.gate.Acts)
	}
	act := f.gate.Acts[0]
	if act.Action != catalog.ActPriceChange || act.SubjectID != p.ID || act.Before != "USD 3.25" || act.After != "SYP 45000" {
		t.Fatalf("recorded act = %+v", act)
	}
}

func TestAnUnchangedPriceDoesNotAskForThePIN(t *testing.T) {
	f := newFixture()
	p := f.create(t, draft("زيت"))
	got, err := f.svc.SetPrice(context.Background(), catalog.SetPriceInput{ID: p.ID, RowVersion: p.RowVersion, Currency: "USD", Price: "3.25"})
	if err != nil || got.RowVersion != p.RowVersion {
		t.Fatalf("an unchanged price = %+v, %v", got, err)
	}
	if f.gate.Asked != 0 {
		t.Fatal("saving an unchanged price asked for the owner's PIN")
	}
}

func TestAStaleEditIsRefusedBeforeTheOwnerIsAsked(t *testing.T) {
	// A PIN typed for an edit that cannot be saved is a PIN typed for nothing.
	f := newFixture()
	p := f.create(t, draft("زيت"))
	_, err := f.svc.SetPrice(context.Background(), catalog.SetPriceInput{ID: p.ID, RowVersion: p.RowVersion + 7, Currency: "USD", Price: "1"})
	if errs.CodeOf(err) != "database.concurrent_modification" {
		t.Fatalf("err = %v", err)
	}
	if f.gate.Asked != 0 {
		t.Fatal("the owner was asked for a PIN for a stale edit")
	}
}

func TestDeactivationNeedsTheOwnerAndReactivationDoesNot(t *testing.T) {
	ctx := context.Background()
	f := newFixture()
	p := f.create(t, draft("زيت"))
	p, _ = f.svc.SetQuickSlot(ctx, p.ID, 3)

	if _, err := f.svc.SetActive(ctx, catalog.SetActiveInput{ID: p.ID, RowVersion: p.RowVersion, Active: false}); errs.CodeOf(err) != catalogtest.CodeRequired {
		t.Fatalf("deactivated without the owner: %v", err)
	}

	f.gate.Elevated = true
	off, err := f.svc.SetActive(ctx, catalog.SetActiveInput{ID: p.ID, RowVersion: p.RowVersion, Active: false})
	if err != nil || off.Active || off.QuickSlot != 0 {
		t.Fatalf("deactivate = %+v, %v", off, err)
	}
	if len(f.gate.Acts) != 1 || f.gate.Acts[0].Action != catalog.ActDeactivate {
		t.Fatalf("acts = %+v", f.gate.Acts)
	}

	f.gate.Elevated = false
	on, err := f.svc.SetActive(ctx, catalog.SetActiveInput{ID: off.ID, RowVersion: off.RowVersion, Active: true})
	if err != nil || !on.Active {
		t.Fatalf("reactivation was refused without the owner: %+v, %v", on, err)
	}
	asked := f.gate.Asked
	if _, err := f.svc.SetActive(ctx, catalog.SetActiveInput{ID: on.ID, RowVersion: on.RowVersion, Active: true}); err != nil || f.gate.Asked != asked {
		t.Fatalf("a no-op activation asked or failed: %v", err)
	}
}

func TestMovingASlotUnpinsTheHolder(t *testing.T) {
	ctx := context.Background()
	f := newFixture()
	a := f.create(t, draft("زيت"))
	b := f.create(t, draft("سمن"))

	if _, err := f.svc.SetQuickSlot(ctx, a.ID, 1); err != nil {
		t.Fatal(err)
	}
	moved, err := f.svc.SetQuickSlot(ctx, b.ID, 1)
	if err != nil || moved.QuickSlot != 1 {
		t.Fatalf("moving onto a held button = %+v, %v", moved, err)
	}
	holder, _ := f.svc.Get(ctx, a.ID)
	if holder.QuickSlot != 0 {
		t.Fatalf("the previous holder kept the button: %+v", holder)
	}

	cleared, err := f.svc.SetQuickSlot(ctx, b.ID, 0)
	if err != nil || cleared.QuickSlot != 0 {
		t.Fatalf("clearing = %+v, %v", cleared, err)
	}
}

func TestAFailedSlotMoveReportsTheFailure(t *testing.T) {
	ctx := context.Background()
	f := newFixture()
	a := f.create(t, draft("زيت"))
	b := f.create(t, draft("سمن"))
	if _, err := f.svc.SetQuickSlot(ctx, a.ID, 2); err != nil {
		t.Fatal(err)
	}
	f.store.FailUpdates()
	if _, err := f.svc.SetQuickSlot(ctx, b.ID, 2); err == nil {
		t.Fatal("a failed write was reported as success")
	}
}

func TestUnitsAndCurrenciesAreTheSeededOnes(t *testing.T) {
	f := newFixture()
	units, _ := f.svc.Units(context.Background())
	if len(units) != 9 {
		t.Fatalf("units = %v — the owner asked for nine (Q-L1.5)", units)
	}
	ref, err := f.svc.Reference(context.Background())
	if err != nil || len(ref.Units) != 9 || len(ref.Currencies) != 2 {
		t.Fatalf("reference = %+v, %v", ref, err)
	}
}

func TestAPackageOpensOneLevelOnly(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	counted := func(name string) domain.Product {
		d := draft(name)
		d.UnitCode = "tin"
		return f.create(t, d)
	}
	carton := counted("كرتونة تنك")
	tin := counted("تنكة زيت")
	oil := f.create(t, draft("زيت فرط"))

	link, err := f.svc.SetPackage(ctx, catalog.SetPackageInput{PackageProductID: tin.ID, ContentProductID: oil.ID, ContentQuantity: "16"})
	if err != nil || link.ContentQuantityMicro != 16_000_000 || link.RowVersion != 1 {
		t.Fatalf("link = %+v, %v", link, err)
	}
	again, err := f.svc.SetPackage(ctx, catalog.SetPackageInput{PackageProductID: tin.ID, ContentProductID: oil.ID, ContentQuantity: "16.000"})
	if err != nil || again != link {
		t.Fatalf("saving the same link wrote a new version: %+v, %v", again, err)
	}
	// A carton of tins would open into a product that itself opens.
	if _, err = f.svc.SetPackage(ctx, catalog.SetPackageInput{PackageProductID: carton.ID, ContentProductID: tin.ID, ContentQuantity: "12"}); errs.CodeOf(err) != domain.CodePackageNested {
		t.Fatalf("a carton of tins: %v", err)
	}
	// And a tin may not become the content of something else while it opens.
	other := counted("صندوق")
	if _, err = f.svc.SetPackage(ctx, catalog.SetPackageInput{PackageProductID: other.ID, ContentProductID: tin.ID, ContentQuantity: "1"}); errs.CodeOf(err) != domain.CodePackageNested {
		t.Fatalf("a package inside another: %v", err)
	}
	if _, err = f.svc.SetPackage(ctx, catalog.SetPackageInput{PackageProductID: oil.ID, ContentProductID: other.ID, ContentQuantity: "1"}); errs.CodeOf(err) != domain.CodePackageNotCounted {
		t.Fatalf("loose oil as a package: %v", err)
	}
	relinked, err := f.svc.SetPackage(ctx, catalog.SetPackageInput{PackageProductID: tin.ID, ContentProductID: oil.ID, ContentQuantity: "15"})
	if err != nil || relinked.RowVersion != 2 || relinked.ContentQuantityMicro != 15_000_000 {
		t.Fatalf("relink = %+v, %v", relinked, err)
	}
	if err = f.svc.ClearPackage(ctx, tin.ID); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := f.svc.Package(ctx, tin.ID); found {
		t.Fatal("the link survived ClearPackage")
	}
	// Cleared, the carton may now hold tins.
	if _, err = f.svc.SetPackage(ctx, catalog.SetPackageInput{PackageProductID: carton.ID, ContentProductID: tin.ID, ContentQuantity: "12"}); err != nil {
		t.Fatalf("a carton of tins that no longer open: %v", err)
	}
	if f.gate.Asked != 0 {
		t.Fatal("linking packages asked for the owner")
	}
	all, err := f.svc.All(ctx)
	if err != nil || len(all) != 4 {
		t.Fatalf("All = %d, %v", len(all), err)
	}
}
