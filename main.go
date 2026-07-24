// Command mizan is the Wails desktop entrypoint for Mizan ERP.
//
// It lives at the module root because Wails embeds the built frontend via go:embed,
// which cannot reference parent directories. All real logic lives in internal/; this
// file only starts the desktop shell. (ARCHITECTURE_v1.md §4 lists cmd/mizan/; the
// root location is the Wails constraint — the composition root still lives in
// internal/bootstrap once Phase 0 wiring lands.)
package main

import (
	"embed"
	"log/slog"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
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
		Bind: []any{
			app, // Phase 0: health only. Later phases bind module structs here.
		},
	})
	if err != nil {
		slog.Error("mizan failed to start", "error", err)
		os.Exit(1)
	}
}
