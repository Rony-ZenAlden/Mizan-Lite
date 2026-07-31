// Command mizan is the Wails desktop entrypoint for Mizan ERP.
//
// It lives at the module root because Wails embeds the built frontend via go:embed, which
// cannot reference parent directories. It contains no logic: the object graph is built by
// internal/bootstrap, the composition root (ARCHITECTURE_v1 §6).
package main

import (
	"context"
	"embed"
	"log/slog"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/platform/paths"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	ctx := context.Background()

	resolved, err := paths.Resolve("Mizan")
	if err != nil {
		fatal(ctx, "could not prepare the data directory", err)
	}

	// The graph is built BEFORE the window opens. A migration that takes a backup and runs an
	// integrity check must finish before anything can be clicked; opening the UI first would
	// mean rendering a shell over a database that might be about to be restored.
	//
	// The cost is that a long migration shows no window. Step 0.4 emits Progress events for
	// exactly this, and wiring them to a splash screen belongs with the frontend shell in
	// Step 0.11 — it needs a window to draw on, which is precisely what does not exist yet.
	built, err := bootstrap.Start(ctx, bootstrap.Options{
		Paths:          resolved,
		StartScheduler: true,
	})
	if err != nil {
		// A real error dialog needs a UI toolkit that is not running yet. Logging and a
		// non-zero exit is the honest Phase-0 answer; Step 0.11 gives this somewhere to be
		// shown properly.
		fatal(ctx, "mizan could not start", err)
	}

	app := NewApp(built)

	if err := wails.Run(&options.App{
		Title:     "Mizan ERP",
		Width:     1280,
		Height:    800,
		MinWidth:  1024,
		MinHeight: 680,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind:       append([]any{app}, built.Bindings...),
	}); err != nil {
		// Wails failed after the graph was built, so tear it down rather than leaking the
		// database handle and the running scheduler.
		if shutdownErr := built.Shutdown(ctx); shutdownErr != nil {
			slog.WarnContext(ctx, "shutdown after a failed start reported errors",
				slog.Any("error", shutdownErr))
		}
		fatal(ctx, "mizan failed to start", err)
	}
}

func fatal(ctx context.Context, msg string, err error) {
	slog.ErrorContext(ctx, msg, slog.Any("error", err))
	os.Exit(1)
}
