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
	"github.com/mizan-erp/mizan/internal/lite/sales"
	salesdomain "github.com/mizan-erp/mizan/internal/lite/sales/domain"
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

func TestTheSeederRingsUpADayAtTheTill(t *testing.T) {
	ctx := context.Background()
	app := start(t)
	res, err := demoseed.Run(ctx, app, demoseed.Options{PIN: "481537"})
	if err != nil {
		t.Fatal(err)
	}
	// Ten sales, one voided: 38,000 + 30,000 + 135,000 + 487,500 + 243,000 + 89,500 + 120,000 + 36,000 pounds still posted,
	// 1,179,000 in all, and $16.25.
	if res.Sales != 10 || res.Voids != 1 || res.Takings["SYP"] != "1179000" || res.Takings["USD"] != "16.25" {
		t.Fatalf("result = %+v", res)
	}

	day, err := app.Sales.Day(ctx, "")
	if err != nil || len(day.Sales) != 10 {
		t.Fatalf("day = %d sales, %v", len(day.Sales), err)
	}
	byReceipt := map[int64]salesdomain.Sale{}
	for _, s := range day.Sales {
		byReceipt[s.ReceiptNo] = s
	}
	for no, want := range map[int64]struct {
		settlement, tender, change string
		total, rounding, tendered  int64
		changeMinor                int64
	}{
		2:  {"SYP", "SYP", "SYP", 30_000, 250, 30_000, 0},     // weighed, rounded up to the note
		3:  {"SYP", "USD", "SYP", 135_000, 0, 2_000, 165_000}, // a $20 note for pounds, change in pounds
		4:  {"USD", "USD", "USD", 1_625, 0, 2_000, 375},       // a dollar sale, dollar change
		7:  {"SYP", "SYP", "SYP", 89_500, -200, 89_500, 0},    // rounded down to the note
		10: {"SYP", "SYP", "SYP", 36_000, 0, 50_000, 14_000},  // change in pounds
	} {
		s := byReceipt[no]
		if s.SettlementCurrency != want.settlement || s.TenderedCurrency != want.tender || s.ChangeCurrency != want.change ||
			s.TotalMinor != want.total || s.RoundingMinor != want.rounding || s.TenderedMinor != want.tendered || s.ChangeMinor != want.changeMinor {
			t.Errorf("receipt %d = %+v", no, s)
		}
	}
	if v := byReceipt[8]; v.Status != salesdomain.StatusVoided || v.VoidReason == "" {
		t.Fatalf("receipt 8 = %+v", v)
	}
	if d := byReceipt[6]; d.Lines[0].DiscountPercentMicro != 100_000 || d.TotalMinor != 243_000 {
		t.Fatalf("the discounted ghee = %+v", d)
	}
	if d := byReceipt[9]; d.DiscountLocalMinor != 2_000 || d.TotalMinor != 120_000 {
		t.Fatalf("the sale discount = %+v", d)
	}

	// The cheese went below zero, and the voided coffee came back.
	levels, _ := app.Stock.Levels(ctx)
	products, _ := app.Catalog.Search(ctx, "", true)
	onHand := map[string]int64{}
	for _, l := range levels {
		for _, p := range products {
			if p.ID == l.ProductID {
				onHand[p.NameEN] = l.OnHandMicro
			}
		}
	}
	if onHand["White cheese"] != -250_000 || onHand["Coffee with cardamom"] != 20_000_000 || onHand["Coarse bulgur"] != 18_250_000 {
		t.Fatalf("on hand = cheese %d, coffee %d, bulgur %d", onHand["White cheese"], onHand["Coffee with cardamom"], onHand["Coarse bulgur"])
	}

	events, _ := app.Owner.Events(ctx, 500)
	acts := map[string]int{}
	for _, e := range events {
		if e.Kind == ownerdomain.EventGuardedAct {
			acts[e.Action]++
		}
	}
	if acts[sales.ActDiscount] != 2 || acts[sales.ActVoid] != 1 {
		t.Fatalf("guarded acts = %v", acts)
	}
	if status, _ := app.Owner.Status(ctx); status.ElevatedFor != 0 {
		t.Fatal("the seeder left the installation in owner mode")
	}
	if _, err := app.Sales.Verify(ctx); errs.CodeOf(err) != sales.CodeOwnerRequired {
		t.Fatal("the sales verifier answered outside owner mode")
	}
	if findings, err := app.Sales.VerifyUnguarded(ctx); err != nil || len(findings) != 0 {
		t.Fatalf("the sales verifier found %+v, %v", findings, err)
	}
}
