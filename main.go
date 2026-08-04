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

	"github.com/mizan-erp/mizan/internal/api/bindings"
	"github.com/mizan-erp/mizan/internal/platform/paths"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	ctx := context.Background()

	resolved, err := paths.Resolve("Mizan")
	if err != nil {
		// The ONE failure that still exits without a window, because it happens before Wails
		// starts and there is genuinely nothing to draw on. Every other startup failure now
		// reaches the boot screen (Step 0.11 D2). Keeping a second, untestable "show an error
		// with no UI toolkit" path would be worse than this honest exit.
		fatal(ctx, "could not prepare the data directory", err)
	}

	// The bindings are constructed BEFORE the graph and handed to Wails empty.
	//
	// Wails binds a fixed set at Run time, so the window cannot open before the bindings
	// exist — but the window must open before the graph is built, or a long migration has
	// nowhere to show progress and a failed start has nowhere to show an error. Façades
	// resolve that: they are declared now and attached when boot succeeds, and every
	// graph-backed method returns a typed not-ready error until then (§2.3).
	set := bindings.New()
	shell := NewShell(set, resolved)

	if err := wails.Run(&options.App{
		Title:     "Mizan ERP",
		Width:     1280,
		Height:    800,
		MinWidth:  1024,
		MinHeight: 680,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  shell.startup,
		OnShutdown: shell.shutdown,
		Bind:       set.All(),
	}); err != nil {
		// Wails itself failed. Tear down whatever the boot goroutine managed to build rather
		// than leaking the database handle and the running scheduler.
		shell.abandon(ctx)
		fatal(ctx, "mizan failed to start", err)
	}
}

func fatal(ctx context.Context, msg string, err error) {
	slog.ErrorContext(ctx, msg, slog.Any("error", err))
	os.Exit(1)
}
