// Command lite-demoseed fills an empty Mizan Lite data directory with a demo shop: the pantry shop (the default), or the
// furniture and home-goods shop of 0.10.1 with its logo.
//
//	MIZAN_LITE_DATA_DIR=~/Desktop/lite-demo go run ./cmd/lite-demoseed -pin 481537
//	MIZAN_LITE_DATA_DIR=~/Desktop/kurdi go run ./cmd/lite-demoseed -profile home \
//	    -logo assets/brands/al-kurdi/al-kurdi-logo-800.png -export ~/Desktop/al-kurdi-demo.db
//
// Without MIZAN_LITE_DATA_DIR it seeds the real data directory for this user — and refuses if that
// installation has already been set up. Open the seeded data with the same variable set. With -export, a verified backup
// of the seeded shop is also written where it says: a file any Mizan Lite restores from Backups → Restore from a file.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/demoseed"
	"github.com/mizan-erp/mizan/internal/lite/paths"
)

func main() {
	pin := flag.String("pin", "481537", "the owner PIN the seeded shop gets (6-12 digits)")
	locale := flag.String("locale", "ar", "the interface language: ar or en")
	days := flag.Int("days", 0, "days of history before today (L6); 0 is each profile's own: 30 for the pantry, 45 for home")
	profile := flag.String("profile", demoseed.ProfilePantry, "the demo shop: pantry or home (furniture and home goods)")
	logo := flag.String("logo", "", "the shop's logo, a PNG or JPEG, set as an owner uploads one (required by -profile home)")
	export := flag.String("export", "", "also write a verified backup of the seeded shop to this file (.db)")
	flag.Parse()

	if *days == 0 {
		*days = 30
		if *profile == demoseed.ProfileHome {
			*days = demoseed.HomeDays
		}
	}
	var logoBytes []byte
	if *logo != "" {
		raw, err := os.ReadFile(*logo)
		if err != nil {
			fail(err)
		}
		logoBytes = raw
	}

	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	resolved, err := paths.Resolve()
	if err != nil {
		fail(err)
	}
	// The graph runs on a clock the seeder steps through the history, then leaves on now.
	clk := demoseed.NewClock()
	now := clk.Now()
	app, err := bootstrap.Start(ctx, bootstrap.Options{Paths: resolved, Logger: log, AppVersion: "demoseed", Clock: clk})
	if err != nil {
		fail(err)
	}
	res, runErr := demoseed.Run(ctx, app, demoseed.Options{PIN: *pin, Locale: *locale, Days: *days, Clock: clk, Now: now,
		Profile: *profile, Logo: logoBytes})
	if runErr == nil && *export != "" {
		runErr = demoseed.Export(ctx, app, *pin, *export)
	}
	if err := app.Shutdown(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "warning: shutdown:", err)
	}
	if runErr != nil {
		fail(runErr)
	}

	fmt.Printf("Seeded %s (%s) in %s\n", res.ShopName, res.Profile, resolved.Data)
	fmt.Printf("  products:      %d (%d on the quick grid)\n", res.Products, res.QuickSlots)
	fmt.Printf("  stock:         %d opening balances, %d deliveries, value %s USD\n", res.Openings, res.Receipts, res.StockValue)
	if res.Profile == demoseed.ProfileHome {
		fmt.Printf("  money:         US dollars only, from the first day; logo set: %v\n", res.Logo)
		fmt.Printf("  suppliers:     %d, %d purchases, %d payments\n", res.Suppliers, res.Purchases, res.SupplierPayments)
		fmt.Printf("  customers:     %d, %d opening balances, %d credit sales, %d repayments\n", res.Customers, res.DebtOpenings, res.CreditSales, res.Payments)
		fmt.Printf("  history:       %d days: %d walk-in sales, %d voided, %d returned, %d written off, %d expenses, %d drawer counts\n",
			res.HistoryDays, res.HistorySales, res.HistoryVoids, res.Returns, res.Losses, res.Expenses, res.Counts)
		fmt.Printf("  net profit:    %s USD over the history and today; today's takings %s USD\n", res.HistoryProfitUSD, res.Takings["USD"])
		fmt.Printf("  backups:       %d\n", res.Backups)
	} else {
		fmt.Printf("  rate:          1 USD = %s SYP (manual mode: the app shows the internet rate for reference)\n", res.Rate)
		fmt.Printf("  till:          %d sales today (%d of them on credit), %d voided; sold %s SYP and %s USD\n", res.Sales+res.CreditSales, res.CreditSales, res.Voids, res.Takings["SYP"], res.Takings["USD"])
		fmt.Printf("  debts:         %d customers, %d opening balances, %d credit sales, %d payments\n", res.Customers, res.DebtOpenings, res.CreditSales, res.Payments)
		if res.HistoryDays > 0 {
			fmt.Printf("  history:       %d days before today: %d sales, %d voided, %d deliveries, %d expenses, %d drawer counts\n",
				res.HistoryDays, res.HistorySales, res.HistoryVoids, res.HistoryReceipts, res.Expenses, res.Counts)
			fmt.Printf("  net profit:    %s USD at cost, %s SYP at each sale's rate, over the history and today\n", res.HistoryProfitUSD, res.HistoryProfitLocal)
		}
		fmt.Printf("  printing:      printer %q, %d print jobs, %d numbered vouchers; %d backups, copied to the demo outside folder\n", demoseed.DemoPrinter, res.PrintJobs, res.Vouchers, res.Backups)
	}
	if *export != "" {
		fmt.Printf("  exported:      %s — restore it from Backups → Restore from a file\n", *export)
	}
	fmt.Printf("  owner PIN:     %s\n", *pin)
	fmt.Printf("  recovery code: %s\n", res.RecoveryCode)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "lite-demoseed:", err)
	os.Exit(1)
}
