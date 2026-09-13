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
	flag.Parse()

	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	resolved, err := paths.Resolve()
	if err != nil {
		fail(err)
	}
	app, err := bootstrap.Start(ctx, bootstrap.Options{Paths: resolved, Logger: log, AppVersion: "demoseed"})
	if err != nil {
		fail(err)
	}
	res, runErr := demoseed.Run(ctx, app, demoseed.Options{PIN: *pin, Locale: *locale})
	if err := app.Shutdown(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "warning: shutdown:", err)
	}
	if runErr != nil {
		fail(runErr)
	}

	fmt.Printf("Seeded %s in %s\n", res.ShopName, resolved.Data)
	fmt.Printf("  products:      %d (%d on the quick grid)\n", res.Products, res.QuickSlots)
	fmt.Printf("  owner PIN:     %s\n", *pin)
	fmt.Printf("  recovery code: %s\n", res.RecoveryCode)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "lite-demoseed:", err)
	os.Exit(1)
}
