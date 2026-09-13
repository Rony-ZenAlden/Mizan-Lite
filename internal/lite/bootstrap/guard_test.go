package bootstrap_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/catalog"
	catalogdomain "github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	ownerdomain "github.com/mizan-erp/mizan/internal/lite/owner/domain"
	"github.com/mizan-erp/mizan/internal/lite/owner/ownertest"
	"github.com/mizan-erp/mizan/internal/lite/setup"
)

// startFast is a started graph with a cheap PIN hasher, set up and holding one product.
func startFast(t *testing.T) (*bootstrap.App, catalogdomain.Product) {
	t.Helper()
	ctx := context.Background()
	app, err := bootstrap.Start(ctx, bootstrap.Options{Paths: dataDir(t), Logger: litetest.Logger(), PINHasher: ownertest.Hasher()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Shutdown(ctx) })
	if _, err = app.Setup.Run(ctx, setup.Input{ShopName: "المونة", Locale: "ar", PIN: "246813"}); err != nil {
		t.Fatal(err)
	}
	p, err := app.Catalog.Create(ctx, catalogdomain.Draft{NameAR: "زيت زيتون", UnitCode: "l", PriceCurrency: "USD", Price: "3.25"})
	if err != nil {
		t.Fatal(err)
	}
	return app, p
}

func guardedActs(t *testing.T, app *bootstrap.App) int {
	t.Helper()
	events, err := app.Owner.Events(context.Background(), 500)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range events {
		if e.Kind == ownerdomain.EventGuardedAct {
			n++
		}
	}
	return n
}

// TestTheCatalogueGuardIsTheRealOwner proves the adapter in the composition root: the catalogue's port reaches
// the owner service, with the owner module's own refusal code.
func TestTheCatalogueGuardIsTheRealOwner(t *testing.T) {
	ctx := context.Background()
	app, p := startFast(t)

	_, err := app.Catalog.SetPrice(ctx, catalog.SetPriceInput{ID: p.ID, RowVersion: p.RowVersion, Currency: "USD", Price: "2.00"})
	if errs.CodeOf(err) != ownerdomain.CodeRequired {
		t.Fatalf("a price change outside owner mode = %v", err)
	}

	if _, err = app.Owner.Elevate(ctx, "246813"); err != nil {
		t.Fatal(err)
	}
	changed, err := app.Catalog.SetPrice(ctx, catalog.SetPriceInput{ID: p.ID, RowVersion: p.RowVersion, Currency: "USD", Price: "2.00"})
	if err != nil || changed.PriceMicro != 2_000_000 {
		t.Fatalf("SetPrice in owner mode = %+v, %v", changed, err)
	}
	if guardedActs(t, app) != 1 {
		t.Fatal("the price change is not in the owner's history")
	}
}

// TestARolledBackPriceChangeLeavesNoRecordOfIt: the guard records inside the catalogue's transaction, and the
// catalogue's transaction joins its caller's. Roll the caller back and neither the new price nor the record of
// changing it may survive.
func TestARolledBackPriceChangeLeavesNoRecordOfIt(t *testing.T) {
	ctx := context.Background()
	app, p := startFast(t)
	if _, err := app.Owner.Elevate(ctx, "246813"); err != nil {
		t.Fatal(err)
	}

	failed := errors.New("something after the price change failed")
	err := app.DB.Do(ctx, func(ctx context.Context) error {
		if _, err := app.Catalog.SetPrice(ctx, catalog.SetPriceInput{ID: p.ID, RowVersion: p.RowVersion, Currency: "USD", Price: "2.00"}); err != nil {
			return err
		}
		return failed
	})
	if !errors.Is(err, failed) {
		t.Fatalf("err = %v", err)
	}
	stored, _ := app.Catalog.Get(ctx, p.ID)
	if stored.PriceMicro != 3_250_000 {
		t.Fatalf("a rolled-back price change was kept: %d", stored.PriceMicro)
	}
	if guardedActs(t, app) != 0 {
		t.Fatal("the owner's history records a price change that did not happen")
	}
}

func TestDeactivationThroughTheRealGuard(t *testing.T) {
	ctx := context.Background()
	app, p := startFast(t)
	if _, err := app.Catalog.SetActive(ctx, catalog.SetActiveInput{ID: p.ID, RowVersion: p.RowVersion, Active: false}); errs.CodeOf(err) != ownerdomain.CodeRequired {
		t.Fatalf("deactivation outside owner mode = %v", err)
	}
	if _, err := app.Owner.Elevate(ctx, "246813"); err != nil {
		t.Fatal(err)
	}
	off, err := app.Catalog.SetActive(ctx, catalog.SetActiveInput{ID: p.ID, RowVersion: p.RowVersion, Active: false})
	if err != nil || off.Active {
		t.Fatalf("deactivation in owner mode = %+v, %v", off, err)
	}
}
