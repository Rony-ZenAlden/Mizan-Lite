// Command lite-demoseed fills an empty Mizan Lite data directory with a pantry shop's worth of data.
//
//	MIZAN_LITE_DATA_DIR=~/Desktop/lite-demo go run ./cmd/lite-demoseed -pin 481537
//
// Without MIZAN_LITE_DATA_DIR it seeds the real data directory for this user — and refuses if that
// installation has already been set up. Open the seeded data with the same variable set.
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
	days := flag.Int("days", 30, "days of history before today (L6): drifting rates, deliveries, sales, expenses and counts")
	flag.Parse()

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
	res, runErr := demoseed.Run(ctx, app, demoseed.Options{PIN: *pin, Locale: *locale, Days: *days, Clock: clk, Now: now})
	if err := app.Shutdown(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "warning: shutdown:", err)
	}
	if runErr != nil {
		fail(runErr)
	}

	fmt.Printf("Seeded %s in %s\n", res.ShopName, resolved.Data)
	fmt.Printf("  products:      %d (%d on the quick grid)\n", res.Products, res.QuickSlots)
	fmt.Printf("  stock:         %d opening balances, %d deliveries, value %s USD\n", res.Openings, res.Receipts, res.StockValue)
	fmt.Printf("  rate:          1 USD = %s SYP (manual mode: the app shows the internet rate for reference)\n", res.Rate)
	fmt.Printf("  till:          %d sales today (%d of them on credit), %d voided; sold %s SYP and %s USD\n", res.Sales+res.CreditSales, res.CreditSales, res.Voids, res.Takings["SYP"], res.Takings["USD"])
	fmt.Printf("  debts:         %d customers, %d opening balances, %d credit sales, %d payments\n", res.Customers, res.DebtOpenings, res.CreditSales, res.Payments)
	if res.HistoryDays > 0 {
		fmt.Printf("  history:       %d days before today: %d sales, %d voided, %d deliveries, %d expenses, %d drawer counts\n",
			res.HistoryDays, res.HistorySales, res.HistoryVoids, res.HistoryReceipts, res.Expenses, res.Counts)
		fmt.Printf("  net profit:    %s USD at cost, %s SYP at each sale's rate, over the history and today\n", res.HistoryProfitUSD, res.HistoryProfitLocal)
	}
	fmt.Printf("  owner PIN:     %s\n", *pin)
	fmt.Printf("  recovery code: %s\n", res.RecoveryCode)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "lite-demoseed:", err)
	os.Exit(1)
}
