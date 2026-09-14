package demoseed_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/catalog"
	catalogdomain "github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	"github.com/mizan-erp/mizan/internal/lite/demoseed"
	"github.com/mizan-erp/mizan/internal/lite/fx"
	fxdomain "github.com/mizan-erp/mizan/internal/lite/fx/domain"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	ownerdomain "github.com/mizan-erp/mizan/internal/lite/owner/domain"
	"github.com/mizan-erp/mizan/internal/lite/owner/ownertest"
	"github.com/mizan-erp/mizan/internal/lite/paths"
	"github.com/mizan-erp/mizan/internal/lite/setup"
	"github.com/mizan-erp/mizan/internal/lite/stock"
	stockdomain "github.com/mizan-erp/mizan/internal/lite/stock/domain"
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

func TestTheSeederStocksTheShop(t *testing.T) {
	ctx := context.Background()
	app := start(t)
	res, err := demoseed.Run(ctx, app, demoseed.Options{PIN: "481537"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Openings != 40 || res.Receipts != 3 || res.StockValue == "" {
		t.Fatalf("result = %+v", res)
	}

	byName := map[string]catalogdomain.Product{}
	products, _ := app.Catalog.Search(ctx, "", true)
	for _, p := range products {
		byName[p.NameEN] = p
	}
	levels, err := app.Stock.Levels(ctx)
	if err != nil || len(levels) != 40 {
		t.Fatalf("%d levels, %v — every product has opening stock", len(levels), err)
	}
	onHand := map[string]int64{}
	for _, l := range levels {
		for name, p := range byName {
			if p.ID == l.ProductID {
				onHand[name] = l.OnHandMicro
			}
		}
	}
	for name, want := range map[string]int64{
		"Olive oil tin 16 L":   2_000_000,  // three, one opened
		"Local olive oil":      20_000_000, // four, and sixteen from the tin
		"Pomegranate molasses": 10_000_000, // counted two jars fewer
		"Labneh":               17_000_000, // 8.5 + 10 received − 1.5 spoiled
		"Fine bulgur":          65_000_000, // 40 + 25 received
	} {
		if onHand[name] != want {
			t.Errorf("%s on hand = %d, want %d", name, onHand[name], want)
		}
	}

	tin := byName["Olive oil tin 16 L"]
	if link, found, _ := app.Catalog.Package(ctx, tin.ID); !found || link.ContentProductID != byName["Local olive oil"].ID || link.ContentQuantityMicro != 16_000_000 {
		t.Fatalf("package link = %+v, %v", link, found)
	}

	// A pound delivery keeps what was typed: 337,500 for 25 kg at 15,000 is 13,500 a kilo, $0.90.
	if hidden, err := app.Stock.History(ctx, byName["Fine bulgur"].ID, 10); err != nil || hidden.CostsVisible {
		t.Fatalf("the seeder left costs visible: %v", err)
	}
	if _, err := app.Owner.Elevate(ctx, "481537"); err != nil {
		t.Fatal(err)
	}
	history, _ := app.Stock.History(ctx, byName["Fine bulgur"].ID, 10)
	receipt := history.Movements[0]
	if receipt.Kind != stockdomain.KindReceipt || receipt.Entered.Currency != "SYP" || receipt.Entered.UnitCostMicro != 13_500_000_000 ||
		receipt.Entered.LocalPerUSDNano != 15_000_000_000_000 || receipt.UnitCostMicro != 900_000 {
		t.Fatalf("the pound delivery = %+v", receipt)
	}

	// The count and the write-off went through the owner's guard, and are in the history.
	events, _ := app.Owner.Events(ctx, 200)
	acts := map[string]int{}
	for _, e := range events {
		if e.Kind == ownerdomain.EventGuardedAct {
			acts[e.Action]++
		}
	}
	if acts[stock.ActCountLower] != 1 || acts[stock.ActAdjustLower] != 1 {
		t.Fatalf("guarded acts = %v", acts)
	}

	if findings, err := app.Stock.Verify(ctx); err != nil || len(findings) != 0 {
		t.Fatalf("the verifier found %+v, %v", findings, err)
	}
}

func TestTheSeederSetsTheRates(t *testing.T) {
	ctx := context.Background()
	app := start(t)
	res, err := demoseed.Run(ctx, app, demoseed.Options{PIN: "481537"})
	if err != nil {
		t.Fatal(err)
	}
	current, err := app.FX.Current(ctx)
	if err != nil || !current.Found || res.Rate != "15000" || current.Rate.Seq != 3 || current.Rate.Source != fxdomain.SourceManual || current.Stale {
		t.Fatalf("current = %+v, %v, result %q", current.Rate, err, res.Rate)
	}
	history, _ := app.FX.History(ctx, 10)
	if len(history) != 3 || history[2].Rate.Source != fxdomain.SourceFirstRun || history[2].Rate.Nano != 14_800_000_000_000 ||
		history[0].Change != "-1.3" || history[0].Rate.Note == "" {
		t.Fatalf("history = %+v", history)
	}
	if app.FX.CanFetch() {
		t.Fatal("the seeder's graph can reach the internet")
	}
	events, _ := app.Owner.Events(ctx, 200)
	sets := 0
	for _, e := range events {
		if e.Kind == ownerdomain.EventGuardedAct && e.Action == fx.ActSetRate {
			sets++
		}
	}
	if sets != 2 {
		t.Fatalf("%d rate changes in the owner's history, want 2", sets)
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

func setupInput() setup.Input {
	return setup.Input{ShopName: "x", Locale: "ar", PIN: "739251", Rate: "15000"}
}
