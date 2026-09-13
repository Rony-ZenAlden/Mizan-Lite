package demoseed_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/catalog"
	"github.com/mizan-erp/mizan/internal/lite/demoseed"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	ownerdomain "github.com/mizan-erp/mizan/internal/lite/owner/domain"
	"github.com/mizan-erp/mizan/internal/lite/owner/ownertest"
	"github.com/mizan-erp/mizan/internal/lite/paths"
	"github.com/mizan-erp/mizan/internal/lite/setup"
)

func start(t *testing.T) *bootstrap.App {
	t.Helper()
	p := paths.Layout(filepath.Join(t.TempDir(), "demo"))
	for _, dir := range []string{p.Data, p.Backups, p.Logs, p.WebView} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	app, err := bootstrap.Start(context.Background(), bootstrap.Options{Paths: p, Logger: litetest.Logger(), PINHasher: ownertest.Hasher()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
	return app
}

func TestTheSeederBuildsAShop(t *testing.T) {
	ctx := context.Background()
	app := start(t)

	res, err := demoseed.Run(ctx, app, demoseed.Options{PIN: "481537"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Products != 40 || res.QuickSlots != 12 || res.RecoveryCode == "" || res.ShopName == "" {
		t.Fatalf("result = %+v", res)
	}
	if done, _ := app.Setup.Complete(ctx); !done {
		t.Fatal("first run is not complete")
	}

	products, err := app.Catalog.Search(ctx, "", false)
	if err != nil || len(products) != 40 {
		t.Fatalf("%d products, %v", len(products), err)
	}
	slots := map[int]bool{}
	units := map[string]bool{}
	for _, p := range products {
		if p.NameEN == "" {
			t.Errorf("%s has no English name", p.NameAR)
		}
		if p.QuickSlot > 0 {
			slots[p.QuickSlot] = true
		}
		units[p.UnitCode] = true
	}
	if len(slots) != 12 {
		t.Fatalf("%d distinct till buttons, want 12", len(slots))
	}
	if len(units) < 8 {
		t.Fatalf("the demo uses %d of the nine units; it should exercise nearly all: %v", len(units), units)
	}

	events, _ := app.Owner.Events(ctx, 100)
	acts := 0
	for _, e := range events {
		if e.Kind == ownerdomain.EventGuardedAct && e.Action == catalog.ActPriceChange {
			acts++
		}
	}
	if acts != 1 {
		t.Fatalf("%d price changes in the owner's history, want 1", acts)
	}
	if status, _ := app.Owner.Status(ctx); status.ElevatedFor != 0 {
		t.Fatal("the seeder left the installation in owner mode")
	}
}

func TestTheSeederRefusesAnInstallationSomebodySetUp(t *testing.T) {
	ctx := context.Background()
	app := start(t)
	if _, err := app.Setup.Run(ctx, setupInput()); err != nil {
		t.Fatal(err)
	}
	if _, err := demoseed.Run(ctx, app, demoseed.Options{PIN: "481537"}); errs.CodeOf(err) != demoseed.CodeAlreadySetUp {
		t.Fatalf("seeding a set-up installation = %v", err)
	}
}

func TestASecondRunIsRefused(t *testing.T) {
	ctx := context.Background()
	app := start(t)
	if _, err := demoseed.Run(ctx, app, demoseed.Options{PIN: "481537"}); err != nil {
		t.Fatal(err)
	}
	if _, err := demoseed.Run(ctx, app, demoseed.Options{PIN: "481537"}); errs.CodeOf(err) != demoseed.CodeAlreadySetUp {
		t.Fatalf("a second run = %v", err)
	}
}

func TestAWeakPINFailsTheRunAndSeedsNothing(t *testing.T) {
	ctx := context.Background()
	app := start(t)
	if _, err := demoseed.Run(ctx, app, demoseed.Options{PIN: "123456"}); errs.CodeOf(err) != ownerdomain.CodePINWeak {
		t.Fatalf("err = %v", err)
	}
	if products, _ := app.Catalog.Search(ctx, "", true); len(products) != 0 {
		t.Fatalf("a refused run left %d products", len(products))
	}
}

func setupInput() setup.Input { return setup.Input{ShopName: "x", Locale: "ar", PIN: "739251"} }
