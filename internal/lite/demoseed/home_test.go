package demoseed_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/demoseed"
	settingsdomain "github.com/mizan-erp/mizan/internal/lite/settings/domain"
	"github.com/mizan-erp/mizan/internal/lite/setup"
)

// theLogo is the demo shop's logo as the brand kit holds it (assets/brands/al-kurdi): the seeder is given it as an owner
// uploads a file — the application holds no shop's logo of its own.
func theLogo(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "assets", "brands", "al-kurdi", "al-kurdi-logo-800.png"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestTheHomeDemoIsARichDollarsOnlyShop (0.10.1, the owner's request of 2026-09-24): the furniture and home-goods shop shown
// to a client — its name, city and logo at the head of every document, a catalogue in dollars, suppliers owed and customers
// owing, and a month and a half of trade. The seeder runs every verifier and every reconciliation of the reports itself and
// fails on a finding, so a nil error is already the main assertion; the rest is that the shop is what it says it is.
func TestTheHomeDemoIsARichDollarsOnlyShop(t *testing.T) {
	if testing.Short() {
		t.Skip("a month and a half of history")
	}
	ctx := context.Background()
	now := time.Date(2026, 9, 24, 13, 0, 0, 0, time.Local)
	clk := clock.NewFixed(now)
	app := startWith(t, clk)
	res, err := demoseed.Run(ctx, app, demoseed.Options{PIN: "481537", Profile: demoseed.ProfileHome, Logo: theLogo(t),
		Days: demoseed.HomeDays, Clock: clk, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if !clk.Now().Equal(now) || app.Reports.Today() != "2026-09-24" {
		t.Fatalf("the seeder left the clock on %s", clk.Now())
	}

	s, err := app.Settings.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.ShopName != "الكردي" || s.MoneyDisplay != "usd" || s.Receipt.City != "حلب" || s.Receipt.Phone == "" || s.Receipt.Address == "" ||
		s.Receipt.Footer == "" {
		t.Fatalf("the store information: %+v", s)
	}
	// Nothing tied to this computer: no outside backup folder, and a printer that prints nothing by itself.
	if s.BackupFolder != "" || s.Receipt.AutoPrint != settingsdomain.AutoPrintNone {
		t.Fatalf("the demo would reach for this computer's folders or printer: folder %q, auto print %q", s.BackupFolder, s.Receipt.AutoPrint)
	}
	logo, found, err := app.Settings.Logo(ctx)
	if err != nil || !found || logo.Width != 800 || logo.Height == 0 {
		t.Fatalf("the logo: found %v, %dx%d, %v", found, logo.Width, logo.Height, err)
	}

	if !res.Logo || res.Products != 51 || res.QuickSlots != 24 || res.Openings != 51 {
		t.Fatalf("the catalogue: %+v", res)
	}
	products, err := app.Catalog.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range products {
		if p.PriceCurrency != "USD" {
			t.Errorf("%s is priced in %s in a dollars-only shop", p.NameEN, p.PriceCurrency)
		}
	}

	if res.Suppliers != 5 || res.Purchases != 8 || res.SupplierPayments != 5 {
		t.Fatalf("the suppliers' book: %+v", res)
	}
	owed, err := app.Suppliers.Payables(ctx)
	if err != nil || owed["USD"] <= 0 || owed["SYP"] != 0 {
		t.Fatalf("what the shop owes its suppliers: %v, %v", owed, err)
	}
	if res.Customers != 11 || res.DebtOpenings != 3 || res.CreditSales != 11 || res.Payments != 13 {
		t.Fatalf("the customers' book: %+v", res)
	}
	out, err := app.Customers.Outstanding(ctx)
	if err != nil {
		t.Fatal(err)
	}
	owing := 0
	for _, c := range out.Customers {
		for _, sum := range c.Summaries {
			if sum.Currency != "USD" && sum.BalanceMinor != 0 {
				t.Errorf("%s owes %s", c.Customer.Name, sum.Currency)
			}
			if sum.Currency == "USD" && sum.BalanceMinor > 0 {
				owing++
			}
		}
	}
	if owing < 6 {
		t.Fatalf("%d customers owe anything — a demo of a debt book needs more", owing)
	}

	if res.HistoryDays != demoseed.HomeDays || res.HistorySales < 150 || res.HistoryVoids != 1 || res.Returns != 1 || res.Losses != 3 ||
		res.Counts < 30 || res.Expenses < 12 || res.HistoryProfitUSD == "" {
		t.Fatalf("the history: %+v", res)
	}
	facts, err := app.Sales.Facts(ctx, "2026-08-01", "2026-09-24")
	if err != nil {
		t.Fatal(err)
	}
	for _, sale := range facts {
		if sale.SettlementCurrency != "USD" || sale.TenderedCurrency != "USD" || sale.ChangeCurrency != "USD" {
			t.Fatalf("sale %d was in %s/%s/%s in a dollars-only shop", sale.ReceiptNo, sale.SettlementCurrency, sale.TenderedCurrency, sale.ChangeCurrency)
		}
		if sale.SoldAt.After(now) {
			t.Fatalf("sale %d is dated %s, after the seeder ran at %s", sale.ReceiptNo, sale.SoldAt, now)
		}
	}
	today, err := app.Sales.Day(ctx, "")
	if err != nil || len(today.Sales) < 3 {
		t.Fatalf("today's sales: %d, %v", len(today.Sales), err)
	}
	if _, err = app.Owner.Elevate(ctx, "481537"); err != nil {
		t.Fatal(err)
	}
	valuation, err := app.Stock.Valuation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range valuation.Lines {
		if l.OnHandMicro < 0 {
			t.Errorf("product %s is below nought on the shelf: the demo sold what it did not have", l.ProductID)
		}
	}
	if res.Backups < 1 {
		t.Fatalf("no backup on the Backups screen: %+v", res)
	}

	// The copy for another computer is a backup the application wrote and verified — one the Backups screen of a fresh
	// installation brings in.
	file := filepath.Join(t.TempDir(), "al-kurdi-demo.db")
	if err = demoseed.Export(ctx, app, "481537", file); err != nil {
		t.Fatal(err)
	}
	other := startWith(t, clock.NewFixed(now))
	if _, err = other.Owner.Elevate(ctx, "481537"); err == nil {
		t.Fatal("a fresh installation took a PIN before its first run")
	}
	if _, err = other.Setup.Run(ctx, setup.Input{ShopName: "تجربة", Locale: "ar", PIN: "135792", Rate: "13700"}); err != nil {
		t.Fatal(err)
	}
	if _, err = other.Owner.Elevate(ctx, "135792"); err != nil {
		t.Fatal(err)
	}
	if imported, err := other.Safety.Import(ctx, file); err != nil || imported.Name == "" {
		t.Fatalf("a fresh installation does not take the exported demo: %+v, %v", imported, err)
	}
}

// TestTheHomeDemoSeededBeforeOpeningHasNothingToday: run in the small hours — as it was, the first time — the demo's today
// is empty rather than full of sales dated later than the moment it ran.
func TestTheHomeDemoSeededBeforeOpeningHasNothingToday(t *testing.T) {
	if testing.Short() {
		t.Skip("a month and a half of history")
	}
	ctx := context.Background()
	now := time.Date(2026, 9, 25, 0, 24, 0, 0, time.Local)
	clk := clock.NewFixed(now)
	app := startWith(t, clk)
	if _, err := demoseed.Run(ctx, app, demoseed.Options{PIN: "481537", Profile: demoseed.ProfileHome, Logo: theLogo(t),
		Days: demoseed.HomeDays, Clock: clk, Now: now}); err != nil {
		t.Fatal(err)
	}
	today, err := app.Sales.Day(ctx, "")
	if err != nil || len(today.Sales) != 0 {
		t.Fatalf("before opening, today has %d sales (%v)", len(today.Sales), err)
	}
	facts, err := app.Sales.Facts(ctx, "2026-08-01", "2026-09-25")
	if err != nil || len(facts) < 150 {
		t.Fatalf("the history: %d sales, %v", len(facts), err)
	}
	for _, sale := range facts {
		if sale.SoldAt.After(now) {
			t.Fatalf("sale %d is dated %s, after the seeder ran at %s", sale.ReceiptNo, sale.SoldAt, now)
		}
	}
}

// TestTheHomeDemoNeedsItsLogoAndItsHistory: without the shop's logo, or with fewer days than its story spans, the home demo
// refuses before it sets anything up.
func TestTheHomeDemoNeedsItsLogoAndItsHistory(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 24, 13, 0, 0, 0, time.Local)
	clk := clock.NewFixed(now)
	app := startWith(t, clk)
	if _, err := demoseed.Run(ctx, app, demoseed.Options{PIN: "481537", Profile: demoseed.ProfileHome, Days: demoseed.HomeDays, Clock: clk, Now: now}); errs.CodeOf(err) != demoseed.CodeHomeNeedsLogo {
		t.Fatalf("no logo: %v", err)
	}
	if _, err := demoseed.Run(ctx, app, demoseed.Options{PIN: "481537", Profile: demoseed.ProfileHome, Logo: theLogo(t), Days: 10, Clock: clk, Now: now}); errs.CodeOf(err) != demoseed.CodeHomeNeedsHistory {
		t.Fatalf("ten days: %v", err)
	}
	if _, err := demoseed.Run(ctx, app, demoseed.Options{PIN: "481537", Profile: "bakery"}); errs.CodeOf(err) != demoseed.CodeUnknownProfile {
		t.Fatalf("an unknown profile: %v", err)
	}
	if done, err := app.Setup.Complete(ctx); err != nil || done {
		t.Fatalf("a refused run set the shop up: %v, %v", done, err)
	}
}
