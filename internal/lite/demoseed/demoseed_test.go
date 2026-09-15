package demoseed_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/catalog"
	catalogdomain "github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	"github.com/mizan-erp/mizan/internal/lite/customers"
	customersdomain "github.com/mizan-erp/mizan/internal/lite/customers/domain"
	"github.com/mizan-erp/mizan/internal/lite/demoseed"
	"github.com/mizan-erp/mizan/internal/lite/fx"
	fxdomain "github.com/mizan-erp/mizan/internal/lite/fx/domain"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	ownerdomain "github.com/mizan-erp/mizan/internal/lite/owner/domain"
	"github.com/mizan-erp/mizan/internal/lite/owner/ownertest"
	"github.com/mizan-erp/mizan/internal/lite/paths"
	reportsdomain "github.com/mizan-erp/mizan/internal/lite/reports/domain"
	"github.com/mizan-erp/mizan/internal/lite/sales"
	salesdomain "github.com/mizan-erp/mizan/internal/lite/sales/domain"
	"github.com/mizan-erp/mizan/internal/lite/setup"
	"github.com/mizan-erp/mizan/internal/lite/stock"
	stockdomain "github.com/mizan-erp/mizan/internal/lite/stock/domain"
)

func start(t *testing.T) *bootstrap.App {
	t.Helper()
	return startWith(t, nil)
}

func startWith(t *testing.T, clk *clock.Fixed) *bootstrap.App {
	t.Helper()
	p := paths.Layout(filepath.Join(t.TempDir(), "demo"))
	for _, dir := range []string{p.Data, p.Backups, p.Logs, p.WebView} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	app, err := bootstrap.Start(context.Background(), bootstrap.Options{Paths: p, Logger: litetest.Logger(), PINHasher: ownertest.Hasher(), Clock: optionalClock(clk)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
	return app
}

func optionalClock(clk *clock.Fixed) clock.Clock {
	if clk == nil {
		return nil
	}
	return clk
}

// TestTheSeederBuildsAMonthOfHistory is L6 §11: thirty days before today through the real services, every verifier clean
// and every reconciliation of the reports holding — the seeder fails on any finding, so a nil error is the assertion.
func TestTheSeederBuildsAMonthOfHistory(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 14, 7, 30, 0, 0, time.Local)
	clk := clock.NewFixed(now)
	app := startWith(t, clk)
	res, err := demoseed.Run(ctx, app, demoseed.Options{PIN: "481537", Days: 30, Clock: clk, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if res.HistoryDays != 30 || res.HistorySales < 300 || res.HistoryVoids != 3 || res.HistoryReceipts != 24 || res.Expenses != 6 || res.Counts < 40 {
		t.Fatalf("history = %+v", res)
	}
	if res.Openings != 40 || res.Sales != 10 || res.CreditSales != 3 || res.HistoryProfitUSD == "" {
		t.Fatalf("today on top of the history = %+v", res)
	}
	if app.Reports.Today() != "2026-09-14" || !clk.Now().Equal(now) {
		t.Fatalf("the seeder left the clock on %s", clk.Now())
	}
	if _, err = app.Owner.Elevate(ctx, "481537"); err != nil {
		t.Fatal(err)
	}
	month, err := app.Reports.Month(ctx, "2026-08")
	if err != nil || len(month.Days) < 15 || month.Total.Losses.Spoiled.USD == 0 || month.Total.Expenses.USD == 0 {
		t.Fatalf("August = %+v, %v", month.Total, err)
	}
	september, err := app.Reports.Month(ctx, "2026-09")
	if err != nil || september.Total.Losses.Shortfall.USD == 0 {
		t.Fatalf("September's count shortfall = %+v, %v", september.Total.Losses, err)
	}
	if month.Days[0].Rate.Nano == september.Days[len(september.Days)-1].Rate.Nano {
		t.Fatal("the rate did not drift over the month")
	}
	shortDay := false
	for _, date := range reportsdomain.Dates("2026-08-15", "2026-09-13") {
		d, err := app.Reports.Drawer(ctx, date)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range d.Currencies {
			if diff, counted := c.Difference(); counted && diff == -5_000 && c.Currency == "SYP" {
				shortDay = true
			}
		}
	}
	if !shortDay {
		t.Fatal("no closing count with a difference in the history")
	}
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
	// 1,179,000 in all, and $16.25 — with L5's credit sales, 16.25 + 16.25 (honey and tahini) + 2.93 (lentils) = 35.43 USD;
	// the pounds credit sale was voided the same day, so it adds as much as it refunds.
	if res.Sales != 10 || res.Voids != 2 || res.Takings["SYP"] != "1179000" || res.Takings["USD"] != "35.43" {
		t.Fatalf("result = %+v", res)
	}

	day, err := app.Sales.Day(ctx, "")
	if err != nil || len(day.Sales) != 13 { // ten at the till, three on credit (L5)
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
	if acts[sales.ActDiscount] != 2 || acts[sales.ActVoid] != 2 { // the till's void, and L5's voided credit sale
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

// TestTheSeederKeepsADebtBook is L5 §12: eight customers, the paper book adopted, credit and repayments both ways, a
// voided repaid credit sale refunded, a write-off — and the balances each chain ends at.
func TestTheSeederKeepsADebtBook(t *testing.T) {
	ctx := context.Background()
	app := start(t)
	res, err := demoseed.Run(ctx, app, demoseed.Options{PIN: "481537"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Customers != 8 || res.DebtOpenings != 7 || res.CreditSales != 3 || res.Payments != 4 {
		t.Fatalf("result = %+v", res)
	}
	all, err := app.Customers.Search(ctx, "", false, true)
	if err != nil || len(all) != 8 {
		t.Fatalf("%d customers, %v", len(all), err)
	}
	balances := map[string]map[string]int64{}
	for _, w := range all {
		balances[w.Customer.Name] = map[string]int64{}
		for _, s := range w.Summaries {
			balances[w.Customer.Name][s.Currency] = s.BalanceMinor
		}
	}
	for name, want := range map[string]map[string]int64{
		"أبو محمد - الحلاق": {"USD": 2_500},
		"أبو محمد - الخضري": {"SYP": 180_000},
		"أم خالد":           {"USD": 1_250, "SYP": 95_000},
		"سمير الحداد":       {"USD": 0},                  // a $50 note on $40, $10 change
		"رامي":              {"SYP": 60_000, "USD": 160}, // 2.93 less 20,000 pounds
		"أبو فادي":          {"USD": 0},                  // written off
		"جميلة":             {"USD": 0},                  // 100,000 pounds, then pay all: 143,500
		"وليد":              {"SYP": 0},                  // voided after 30,000 paid, refunded
	} {
		for cur, minor := range want {
			if balances[name][cur] != minor {
				t.Errorf("%s %s = %d, want %d", name, cur, balances[name][cur], minor)
			}
		}
	}
	jamila, _ := app.Customers.Search(ctx, "جميلة", false, false)
	st, _ := app.Customers.Statement(ctx, jamila[0].Customer.ID, "USD")
	if len(st.Entries) != 3 || st.Entries[2].Cash.TenderedMinor != 143_500 || st.Entries[2].AmountMinor != -958 {
		t.Fatalf("Jamila's pay all = %+v", st.Entries)
	}
	waleed, _ := app.Customers.Search(ctx, "وليد", false, false)
	if st, _ := app.Customers.Statement(ctx, waleed[0].Customer.ID, "SYP"); len(st.Entries) != 4 || st.Entries[2].BalanceAfterMinor != -30_000 || st.Entries[3].Kind != customersdomain.KindRefund {
		t.Fatalf("Waleed's chain = %+v", st.Entries)
	}

	events, _ := app.Owner.Events(ctx, 500)
	acts := map[string]int{}
	for _, e := range events {
		if e.Kind == ownerdomain.EventGuardedAct {
			acts[e.Action]++
		}
	}
	if acts[customers.ActOpening] != 7 || acts[customers.ActWriteOff] != 1 || acts[customers.ActRefund] != 1 || acts[sales.ActVoid] != 2 {
		t.Fatalf("guarded acts = %v", acts)
	}
	if status, _ := app.Owner.Status(ctx); status.ElevatedFor != 0 {
		t.Fatal("the seeder left the installation in owner mode")
	}
	if findings, err := app.Customers.VerifyUnguarded(ctx); err != nil || len(findings) != 0 {
		t.Fatalf("debt findings = %+v, %v", findings, err)
	}
	if findings, err := app.Sales.VerifyUnguarded(ctx); err != nil || len(findings) != 0 {
		t.Fatalf("sales findings = %+v, %v", findings, err)
	}
}
