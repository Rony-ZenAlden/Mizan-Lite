package bootstrap_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/catalog"
	catalogdomain "github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	"github.com/mizan-erp/mizan/internal/lite/customers"
	customersdomain "github.com/mizan-erp/mizan/internal/lite/customers/domain"
	"github.com/mizan-erp/mizan/internal/lite/fx"
	fxdomain "github.com/mizan-erp/mizan/internal/lite/fx/domain"
	"github.com/mizan-erp/mizan/internal/lite/fx/fxtest"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	ownerdomain "github.com/mizan-erp/mizan/internal/lite/owner/domain"
	"github.com/mizan-erp/mizan/internal/lite/owner/ownertest"
	"github.com/mizan-erp/mizan/internal/lite/sales"
	salesdomain "github.com/mizan-erp/mizan/internal/lite/sales/domain"
	settingsdomain "github.com/mizan-erp/mizan/internal/lite/settings/domain"
	"github.com/mizan-erp/mizan/internal/lite/setup"
	"github.com/mizan-erp/mizan/internal/lite/stock"
	stockdomain "github.com/mizan-erp/mizan/internal/lite/stock/domain"
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
	if _, err = app.Setup.Run(ctx, setup.Input{ShopName: "المونة", Locale: "ar", PIN: "246813", Rate: "15000"}); err != nil {
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

// TestStockReachesTheRealOwnerAndCatalogue proves stock's two adapters: a write-off asks the real owner with the owner
// module's own code, and a receipt reads the real product's unit.
func TestStockReachesTheRealOwnerAndCatalogue(t *testing.T) {
	ctx := context.Background()
	app, p := startFast(t)
	if stock.CodeOwnerRequired != ownerdomain.CodeRequired {
		t.Fatalf("stock's refusal %q is not the owner's %q", stock.CodeOwnerRequired, ownerdomain.CodeRequired)
	}
	if _, err := app.Stock.Opening(ctx, stock.ReceiveInput{ProductID: p.ID, Quantity: "12.5",
		Cost: stockdomain.CostInput{Amount: "40.625", Currency: "USD"}}); errs.CodeOf(err) != stockdomain.CodeCostDecimals {
		t.Fatalf("a cost with a third decimal of a dollar: %v", err)
	}
	if _, err := app.Stock.Opening(ctx, stock.ReceiveInput{ProductID: p.ID, Quantity: "12.5",
		Cost: stockdomain.CostInput{Amount: "40.63", Currency: "USD"}}); err != nil {
		t.Fatalf("12.5 litres: %v", err)
	}
	if _, err := app.Stock.Opening(ctx, stock.ReceiveInput{ProductID: p.ID, Quantity: "1.0001",
		Cost: stockdomain.CostInput{Amount: "1", Currency: "USD"}}); errs.CodeOf(err) != stockdomain.CodeQuantityDecimals {
		t.Fatalf("the litre's decimals were not read from the catalogue: %v", err)
	}

	lower := stock.AdjustInput{ProductID: p.ID, Direction: stock.DirectionOut, Quantity: "2", Reason: stockdomain.ReasonExpired}
	if _, err := app.Stock.Adjust(ctx, lower); errs.CodeOf(err) != ownerdomain.CodeRequired {
		t.Fatalf("a write-off outside owner mode: %v", err)
	}
	if _, err := app.Stock.Valuation(ctx); errs.CodeOf(err) != ownerdomain.CodeRequired {
		t.Fatalf("valuation outside owner mode: %v", err)
	}
	if _, err := app.Owner.Elevate(ctx, "246813"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Stock.Adjust(ctx, lower); err != nil {
		t.Fatal(err)
	}
	valuation, err := app.Stock.Valuation(ctx)
	if err != nil || valuation.TotalMinor != 3413 {
		t.Fatalf("valuation = %+v, %v", valuation, err)
	}
	if guardedActs(t, app) != 1 {
		t.Fatal("the write-off is not in the owner's history, or the valuation was")
	}
	if findings, err := app.Stock.Verify(ctx); err != nil || len(findings) != 0 {
		t.Fatalf("findings = %+v, %v", findings, err)
	}

	// Deactivated, the product receives nothing.
	if _, err := app.Catalog.SetActive(ctx, catalog.SetActiveInput{ID: p.ID, RowVersion: p.RowVersion, Active: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Stock.Receive(ctx, stock.ReceiveInput{ProductID: p.ID, Quantity: "1",
		Cost: stockdomain.CostInput{Amount: "1", Currency: "USD"}}); errs.CodeOf(err) != stockdomain.CodeInactiveProduct {
		t.Fatalf("a receipt into a deactivated product: %v", err)
	}
}

// TestRatesReachTheRealOwnerAndSettings proves fx's two adapters: setting the rate and switching the mode ask the real
// owner, and the mode is stored in the settings table.
func TestRatesReachTheRealOwnerAndSettings(t *testing.T) {
	ctx := context.Background()
	app, _ := startFast(t)
	current, err := app.FX.Current(ctx)
	if err != nil || !current.Found || current.Rate.Source != fxdomain.SourceFirstRun || current.Local != "SYP" || current.Mode != fxdomain.ModeManual {
		t.Fatalf("after first run: %+v, %v", current, err)
	}
	if _, err := app.FX.SetRate(ctx, fx.SetRateInput{Rate: "15200"}); errs.CodeOf(err) != ownerdomain.CodeRequired {
		t.Fatalf("a rate outside owner mode: %v", err)
	}
	if _, err := app.FX.SetMode(ctx, "automatic"); errs.CodeOf(err) != ownerdomain.CodeRequired {
		t.Fatalf("a mode switch outside owner mode: %v", err)
	}
	if _, err := app.Owner.Elevate(ctx, "246813"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FX.SetRate(ctx, fx.SetRateInput{Rate: "15200"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FX.SetMode(ctx, "automatic"); err != nil {
		t.Fatal(err)
	}
	stored, _ := app.Settings.Get(ctx)
	if stored.RateMode != settingsdomain.RateAutomatic {
		t.Fatalf("the mode was not stored: %+v", stored)
	}
	if guardedActs(t, app) != 2 {
		t.Fatal("the rate and the mode switch are not both in the owner's history")
	}
	if app.FX.CanFetch() {
		t.Fatal("a graph built with no rate source can fetch")
	}
	for _, key := range app.Scheduler.Registry().Keys() {
		if key == bootstrap.KeyRateRefresh {
			t.Fatal("the rate job was registered with no provider")
		}
	}
}

// TestTheRateJobFetchesWhenAProviderIsRegistered drives the job directly with a scripted source: the graph offline by
// construction until a source is passed, and the job logging an attempt when one is.
func TestTheRateJobFetchesWhenAProviderIsRegistered(t *testing.T) {
	ctx := context.Background()
	source := &fxtest.Source{}
	source.Answer("currency-api-jsdelivr", 15_100_000_000_000)
	app, err := bootstrap.Start(ctx, bootstrap.Options{Paths: dataDir(t), Logger: litetest.Logger(), PINHasher: ownertest.Hasher(), RateSource: source})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Shutdown(ctx) })
	if _, err = app.Setup.Run(ctx, setup.Input{ShopName: "المونة", Locale: "ar", PIN: "246813", Rate: "15000"}); err != nil {
		t.Fatal(err)
	}
	if err := app.Scheduler.RunNow(ctx, bootstrap.KeyRateRefresh); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	// Manual by default (the owner's decision): the job fetches for reference and applies nothing.
	current, _ := app.FX.Current(ctx)
	if source.Calls != 1 || !current.Fetched || current.Fetch.Outcome != fxdomain.OutcomeHeldMode || current.Rate.Nano != 15_000_000_000_000 {
		t.Fatalf("after the job: %d calls, %+v", source.Calls, current)
	}
}

// TestTheTillReachesTheRealModules proves the till's five adapters on the real graph: a sale moves real stock, snapshots
// the real rate, a discount and a void ask the real owner, and both verifiers walk the result clean.
func TestTheTillReachesTheRealModules(t *testing.T) {
	ctx := context.Background()
	app, oil := startFast(t) // olive oil, by the litre, $3.25; first run at 15,000
	if sales.CodeOwnerRequired != ownerdomain.CodeRequired {
		t.Fatalf("the till's refusal %q is not the owner's", sales.CodeOwnerRequired)
	}
	if _, err := app.Owner.Elevate(ctx, "246813"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Stock.Opening(ctx, stock.ReceiveInput{ProductID: oil.ID, Quantity: "1", Cost: stockdomain.CostInput{Amount: "2", Currency: "USD"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Owner.EndElevation(ctx); err != nil {
		t.Fatal(err)
	}

	// 1.5 L at $3.25 is $4.875 → $4.88, and 73,125 pounds → a 73,000 total on the 500 note: beyond the 1 L on the shelf.
	in := salesdomain.CartInput{Lines: []salesdomain.LineInput{{ProductID: oil.ID, Quantity: "1.5"}}}
	q, err := app.Sales.Quote(ctx, in)
	if err != nil || q.TotalMinor != 73_000 || q.RoundingMinor != -125 || q.Lines[0].GrossUSDMinor != 488 || len(q.Lines[0].Warnings) != 1 {
		t.Fatalf("quote = %+v, %v", q, err)
	}
	sale, err := app.Sales.Checkout(ctx, sales.CheckoutInput{Cart: in, Token: q.Token})
	if err != nil || sale.ReceiptNo != 1 || sale.Lines[0].UnitCostMicro != 2_000_000 || !sale.Lines[0].CostKnown {
		t.Fatalf("sale = %+v, %v", sale, err)
	}
	if levels, _ := app.Stock.Levels(ctx); levels[0].OnHandMicro != -500_000 {
		t.Fatalf("stock after selling beyond it = %+v", levels)
	}

	// A discount needs the real owner.
	discounted := salesdomain.CartInput{Lines: []salesdomain.LineInput{{ProductID: oil.ID, Quantity: "1", DiscountPercent: "10"}}}
	dq, _ := app.Sales.Quote(ctx, discounted)
	if _, err := app.Sales.Checkout(ctx, sales.CheckoutInput{Cart: discounted, Token: dq.Token}); errs.CodeOf(err) != ownerdomain.CodeRequired {
		t.Fatalf("a discount outside owner mode: %v", err)
	}
	if _, err := app.Owner.Elevate(ctx, "246813"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Sales.Checkout(ctx, sales.CheckoutInput{Cart: discounted, Token: dq.Token}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Sales.Void(ctx, sales.VoidInput{SaleID: sale.ID, Reason: "خطأ"}); err != nil {
		t.Fatal(err)
	}
	if levels, _ := app.Stock.Levels(ctx); levels[0].OnHandMicro != 0 {
		t.Fatalf("stock after the void = %+v", levels)
	}
	if findings, err := app.Sales.Verify(ctx); err != nil || len(findings) != 0 {
		t.Fatalf("sales findings = %+v, %v", findings, err)
	}
	if findings, err := app.Stock.Verify(ctx); err != nil || len(findings) != 0 {
		t.Fatalf("stock findings = %+v, %v", findings, err)
	}
	if guardedActs(t, app) != 2 {
		t.Fatalf("the discount and the void are not both in the owner's history")
	}
	if found, ok, err := app.Sales.Scan(ctx, "nothing"); err != nil || ok {
		t.Fatalf("scan = %+v %v %v", found, ok, err)
	}
}

// failSecondLine writes the first line's stock movement for real and fails the second.
type failSecondLine struct {
	sales.Stock
	calls int
}

var errSecondLine = errors.New("the second line's stock movement failed")

func (f *failSecondLine) RecordSale(ctx context.Context, line sales.StockLine) error {
	f.calls++
	if f.calls == 2 {
		return errSecondLine
	}
	return f.Stock.RecordSale(ctx, line)
}

// TestACheckoutThatFailsAfterTheStockMovementLeavesNothing: the first line's movement is written to the real ledger, then
// the second fails — and the one transaction leaves no sale, no line, no movement and no used receipt number.
func TestACheckoutThatFailsAfterTheStockMovementLeavesNothing(t *testing.T) {
	ctx := context.Background()
	app, oil := startFast(t)
	jam, err := app.Catalog.Create(ctx, catalogdomain.Draft{NameAR: "مربى", UnitCode: "jar", PriceCurrency: "SYP", Price: "30000"})
	if err != nil {
		t.Fatal(err)
	}
	failing := &failSecondLine{}
	till := bootstrap.TillWithStock(app, func(s sales.Stock) sales.Stock { failing.Stock = s; return failing })
	in := salesdomain.CartInput{Lines: []salesdomain.LineInput{{ProductID: oil.ID, Quantity: "1"}, {ProductID: jam.ID, Quantity: "1"}}}
	q, err := till.Quote(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = till.Checkout(ctx, sales.CheckoutInput{Cart: in, Token: q.Token}); !errors.Is(err, errSecondLine) || failing.calls != 2 {
		t.Fatalf("checkout = %v after %d movements", err, failing.calls)
	}

	if levels, _ := app.Stock.Levels(ctx); len(levels) != 0 {
		t.Fatalf("the first line's movement survived: %+v", levels)
	}
	if history, _ := app.Stock.History(ctx, oil.ID, 10); len(history.Movements) != 0 {
		t.Fatalf("ledger = %+v", history.Movements)
	}
	if day, _ := app.Sales.Day(ctx, ""); len(day.Sales) != 0 {
		t.Fatalf("sales = %+v", day.Sales)
	}
	sale, err := app.Sales.Checkout(ctx, sales.CheckoutInput{Cart: in, Token: q.Token})
	if err != nil || sale.ReceiptNo != 1 {
		t.Fatalf("the next checkout = %+v, %v — a rolled-back checkout used a receipt number", sale, err)
	}
}

// TestVoidNeedsTheOwnerAndReturnsTheStockAtTheSnapshottedCost, on the real modules: a delivery between the sale and its
// void moves the average; the void averages the goods back in at the cost they left at.
func TestVoidNeedsTheOwnerAndReturnsTheStockAtTheSnapshottedCost(t *testing.T) {
	ctx := context.Background()
	app, oil := startFast(t) // $3.25 a litre
	if _, err := app.Owner.Elevate(ctx, "246813"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Stock.Opening(ctx, stock.ReceiveInput{ProductID: oil.ID, Quantity: "10", Cost: stockdomain.CostInput{Amount: "20", Currency: "USD"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Owner.EndElevation(ctx); err != nil {
		t.Fatal(err)
	}
	in := salesdomain.CartInput{Lines: []salesdomain.LineInput{{ProductID: oil.ID, Quantity: "4"}}}
	q, err := app.Sales.Quote(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	sale, err := app.Sales.Checkout(ctx, sales.CheckoutInput{Cart: in, Token: q.Token})
	if err != nil || sale.Lines[0].UnitCostMicro != 2_000_000 {
		t.Fatalf("sale = %+v, %v", sale, err)
	}
	// 6 L at $2 and 6 L at $4: $3.
	if _, err = app.Stock.Receive(ctx, stock.ReceiveInput{ProductID: oil.ID, Quantity: "6", Cost: stockdomain.CostInput{Amount: "24", Currency: "USD"}}); err != nil {
		t.Fatal(err)
	}

	if _, err = app.Sales.Void(ctx, sales.VoidInput{SaleID: sale.ID, Reason: "أعاده"}); errs.CodeOf(err) != ownerdomain.CodeRequired {
		t.Fatalf("a void outside owner mode: %v", err)
	}
	if levels, _ := app.Stock.Levels(ctx); levels[0].OnHandMicro != 12_000_000 {
		t.Fatalf("a refused void moved stock: %+v", levels)
	}
	if _, err = app.Owner.Elevate(ctx, "246813"); err != nil {
		t.Fatal(err)
	}
	if _, err = app.Sales.Void(ctx, sales.VoidInput{SaleID: sale.ID, Reason: "أعاده"}); err != nil {
		t.Fatal(err)
	}
	// (12 × 3 + 4 × 2) ÷ 16 = 2.75.
	v, err := app.Stock.Valuation(ctx)
	if err != nil || len(v.Lines) != 1 || v.Lines[0].OnHandMicro != 16_000_000 || v.Lines[0].AvgCostMicro != 2_750_000 {
		t.Fatalf("valuation = %+v, %v", v, err)
	}
	if findings, err := app.Stock.Verify(ctx); err != nil || len(findings) != 0 {
		t.Fatalf("stock findings = %+v, %v", findings, err)
	}
	if findings, err := app.Sales.Verify(ctx); err != nil || len(findings) != 0 {
		t.Fatalf("sales findings = %+v, %v", findings, err)
	}
}

// TestConcurrentCheckoutsNeverShareAReceiptNumber: the number is read and used inside the checkout's transaction, so
// checkouts racing each other take 1…N — never the same number twice, and never a refusal for it.
func TestConcurrentCheckoutsNeverShareAReceiptNumber(t *testing.T) {
	ctx := context.Background()
	app, oil := startFast(t)
	in := salesdomain.CartInput{Lines: []salesdomain.LineInput{{ProductID: oil.ID, Quantity: "1"}}}
	q, err := app.Sales.Quote(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	const n = 8
	numbers := make(chan int64, n)
	failures := make(chan error, n)
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sale, err := app.Sales.Checkout(ctx, sales.CheckoutInput{Cart: in, Token: q.Token})
			if err != nil {
				failures <- err
				return
			}
			numbers <- sale.ReceiptNo
		}()
	}
	wg.Wait()
	close(numbers)
	close(failures)
	for err := range failures {
		t.Errorf("a concurrent checkout failed: %v", err)
	}
	seen := map[int64]bool{}
	for no := range numbers {
		if seen[no] || no < 1 || no > n {
			t.Errorf("receipt number %d taken twice or out of 1…%d", no, n)
		}
		seen[no] = true
	}
	if len(seen) != n {
		t.Fatalf("%d distinct receipt numbers, want %d", len(seen), n)
	}
}

// TestAReceiptIsTheSaleAsRecordedNotTheCatalogueNow: renaming a product and changing its price after the sale changes
// nothing on the receipt (L4 §7.2).
func TestAReceiptIsTheSaleAsRecordedNotTheCatalogueNow(t *testing.T) {
	ctx := context.Background()
	app, oil := startFast(t)
	in := salesdomain.CartInput{Lines: []salesdomain.LineInput{{ProductID: oil.ID, Quantity: "2"}}}
	q, _ := app.Sales.Quote(ctx, in)
	sale, err := app.Sales.Checkout(ctx, sales.CheckoutInput{Cart: in, Token: q.Token})
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := app.Catalog.Update(ctx, catalog.UpdateInput{ID: oil.ID, RowVersion: oil.RowVersion, NameAR: "زيت مستورد", NameEN: "Imported oil"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.Owner.Elevate(ctx, "246813"); err != nil {
		t.Fatal(err)
	}
	if _, err = app.Catalog.SetPrice(ctx, catalog.SetPriceInput{ID: oil.ID, RowVersion: renamed.RowVersion, Currency: "USD", Price: "4.00"}); err != nil {
		t.Fatal(err)
	}
	if _, err = app.Settings.Update(ctx, settingsdomain.Update{ShopName: ptr("متجر جديد")}); err != nil {
		t.Fatal(err)
	}
	receipt, err := app.Sales.Receipt(ctx, sale.ID)
	if err != nil {
		t.Fatal(err)
	}
	l := receipt.Lines[0]
	if l.NameAR != "زيت زيتون" || l.UnitPriceMicro != 3_250_000 || l.GrossUSDMinor != 650 || receipt.ShopName != "المونة" || receipt.TotalMinor != sale.TotalMinor {
		t.Fatalf("the receipt followed the catalogue: %+v %+v", receipt, l)
	}
}

func ptr[T any](v T) *T { return &v }

// TestTheTillReachesTheCustomers proves the debt book's adapters on the real graph: a credit sale charged in dollars with
// pounds paid now, its receipt, a repayment in pounds, a void after it, a refund — and all four verifiers clean.
func TestTheTillReachesTheCustomers(t *testing.T) {
	ctx := context.Background()
	app, oil := startFast(t) // olive oil at $3.25 a litre, 15,000
	if customers.CodeOwnerRequired != ownerdomain.CodeRequired {
		t.Fatal("the debt book's refusal is not the owner's")
	}
	samir, err := app.Customers.Create(ctx, customersdomain.Draft{Name: "سمير", Phone: "0933"})
	if err != nil {
		t.Fatal(err)
	}
	// 4 L at $3.25 = $13.00, charged in dollars, 45,000 pounds ($3.00) paid now: $10.00 on the book.
	in := salesdomain.CartInput{Lines: []salesdomain.LineInput{{ProductID: oil.ID, Quantity: "4"}}, Settlement: "USD",
		TenderCurrency: "SYP", Tendered: "45000", Payment: salesdomain.PaymentCredit, CustomerID: samir.ID}
	q, err := app.Sales.Quote(ctx, in)
	if err != nil || q.DebtMinor != 1_000 || q.BalanceBeforeMinor != 0 || q.Customer.Name != "سمير" {
		t.Fatalf("quote = %+v, %v", q, err)
	}
	sale, err := app.Sales.Checkout(ctx, sales.CheckoutInput{Cart: in, Token: q.Token})
	if err != nil || sale.Credit.AmountMinor != 1_000 || sale.Credit.BalanceAfterMinor != 1_000 || sale.Credit.CustomerName != "سمير" {
		t.Fatalf("sale = %+v, %v", sale, err)
	}
	if receipt, _ := app.Sales.Receipt(ctx, sale.ID); receipt.Credit.Currency != "USD" || receipt.Credit.AmountMinor != 1_000 {
		t.Fatalf("receipt credit = %+v", receipt.Credit)
	}

	pay := customersdomain.CashInput{Currency: "USD", TenderCurrency: "SYP", Amount: "60000"} // $4.00
	pq, err := app.Customers.QuotePayment(ctx, samir.ID, pay)
	if err != nil || pq.SettledMinor != 400 {
		t.Fatalf("payment quote = %+v, %v", pq, err)
	}
	if _, err = app.Customers.RecordPayment(ctx, customers.PaymentInput{CustomerID: samir.ID, Cash: pay, Token: pq.Token}); err != nil {
		t.Fatal(err)
	}

	if _, err = app.Owner.Elevate(ctx, "246813"); err != nil {
		t.Fatal(err)
	}
	if _, err = app.Sales.Void(ctx, sales.VoidInput{SaleID: sale.ID, Reason: "أعاد الزيت"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := app.Customers.Balance(ctx, samir.ID, "USD"); b != -400 {
		t.Fatalf("balance after voiding a repaid credit sale = %d, want −400 (the shop owes $4)", b)
	}
	refund, err := app.Customers.Refund(ctx, customers.RefundInput{CustomerID: samir.ID, Cash: customersdomain.CashInput{Currency: "USD", All: true}, Reason: "نقداً"})
	if err != nil || refund.BalanceAfterMinor != 0 {
		t.Fatalf("refund = %+v, %v", refund, err)
	}
	for name, verify := range map[string]func() (int, error){
		"stock": func() (int, error) { f, err := app.Stock.Verify(ctx); return len(f), err },
		"sales": func() (int, error) { f, err := app.Sales.Verify(ctx); return len(f), err },
		"debts": func() (int, error) { f, err := app.Customers.Verify(ctx); return len(f), err },
	} {
		if n, err := verify(); err != nil || n != 0 {
			t.Errorf("%s verifier: %d findings, %v", name, n, err)
		}
	}
	if acts := guardedActs(t, app); acts != 2 { // the void and the refund
		t.Fatalf("%d guarded acts, want 2", acts)
	}
}

// failCharge fails every charge — after the sale and its stock movements were written for real.
type failCharge struct{ sales.Debts }

var errCharge = errors.New("the charge failed")

func (failCharge) Charge(context.Context, sales.ChargeInput) (salesdomain.Credit, error) {
	return salesdomain.Credit{}, errCharge
}

// TestACreditSaleWhoseChargeFailsLeavesNothing: the sale, its lines, its stock movement and its receipt number roll back
// with the charge — one transaction across three modules (L5 H5).
func TestACreditSaleWhoseChargeFailsLeavesNothing(t *testing.T) {
	ctx := context.Background()
	app, oil := startFast(t)
	samir, _ := app.Customers.Create(ctx, customersdomain.Draft{Name: "سمير"})
	till := bootstrap.TillWithDebts(app, func(d sales.Debts) sales.Debts { return failCharge{d} })
	in := salesdomain.CartInput{Lines: []salesdomain.LineInput{{ProductID: oil.ID, Quantity: "1"}}, Payment: salesdomain.PaymentCredit, CustomerID: samir.ID}
	q, err := till.Quote(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = till.Checkout(ctx, sales.CheckoutInput{Cart: in, Token: q.Token}); !errors.Is(err, errCharge) {
		t.Fatalf("checkout = %v", err)
	}
	if levels, _ := app.Stock.Levels(ctx); len(levels) != 0 {
		t.Fatalf("the stock movement survived: %+v", levels)
	}
	if day, _ := app.Sales.Day(ctx, ""); len(day.Sales) != 0 {
		t.Fatalf("the sale survived: %+v", day.Sales)
	}
	sale, err := app.Sales.Checkout(ctx, sales.CheckoutInput{Cart: in, Token: q.Token})
	if err != nil || sale.ReceiptNo != 1 || sale.Credit.AmountMinor != 49_000 { // 48,750 rounded to the note, as a cash total is
		t.Fatalf("the next checkout = %+v, %v", sale, err)
	}
}

// TestConcurrentPaymentsOnOneDebtNeverShareAPlace: five cashiers' screens record the same quoted payment at once. The place
// is read inside each recording's transaction, so one is written and the rest are refused as stale — never a place taken
// twice, never a database error (L5 §4.2, §6.4).
func TestConcurrentPaymentsOnOneDebtNeverShareAPlace(t *testing.T) {
	ctx := context.Background()
	app, _ := startFast(t)
	samir, _ := app.Customers.Create(ctx, customersdomain.Draft{Name: "سمير"})
	if _, err := app.Owner.Elevate(ctx, "246813"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Customers.Opening(ctx, customers.AmountInput{CustomerID: samir.ID, Currency: "USD", Amount: "50"}); err != nil {
		t.Fatal(err)
	}
	pay := customersdomain.CashInput{Currency: "USD", Amount: "5"}
	q, err := app.Customers.QuotePayment(ctx, samir.ID, pay)
	if err != nil {
		t.Fatal(err)
	}
	const n = 5
	results := make(chan error, n)
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := app.Customers.RecordPayment(ctx, customers.PaymentInput{CustomerID: samir.ID, Cash: pay, Token: q.Token})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	written := 0
	for err := range results {
		switch {
		case err == nil:
			written++
		case errs.CodeOf(err) != customersdomain.CodePaymentStale:
			t.Errorf("a concurrent payment failed other than stale: %v", err)
		}
	}
	if written != 1 {
		t.Fatalf("%d payments written from one quote, want 1", written)
	}
	if b, _ := app.Customers.Balance(ctx, samir.ID, "USD"); b != 4_500 {
		t.Fatalf("balance = %d", b)
	}
	if findings, err := app.Customers.Verify(ctx); err != nil || len(findings) != 0 {
		t.Fatalf("findings = %+v, %v", findings, err)
	}
}
