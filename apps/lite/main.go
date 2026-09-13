// Command lite is the Mizan Lite desktop application, for Windows and macOS.
//
// It holds no business logic. The object graph is built by internal/lite/bootstrap; this package
// only resolves where the data lives, opens the window, and hands Wails the bindings.
//
// It lives in apps/lite rather than the repository root because go:embed cannot reach a parent
// directory: the embedded frontend must be below the package that embeds it. L0's spike proved a
// Wails project here builds against the repository's single Go module, for both platforms.
package main

import (
	"context"
	"embed"
	"io"
	"log/slog"
	"os"

	"github.com/wailsapp/wails/v2"

	"github.com/mizan-erp/mizan/internal/lite/api"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/logging"
	"github.com/mizan-erp/mizan/internal/lite/paths"
	settingsdb "github.com/mizan-erp/mizan/internal/lite/settings/infra/sqlite"
)

//go:embed all:frontend/dist
var assets embed.FS

// version is stamped at build time: -ldflags "-X main.version=<version>". The default says plainly
// that a binary was not built by the release pipeline.
var version = "0.0.0-dev"

func main() {
	ctx := context.Background()

	resolved, err := paths.Resolve()
	if err != nil {
		// The one failure that exits without a window: it happens before there is anywhere to draw,
		// and before there is a log directory to write to. Every later failure reaches the boot screen.
		slog.ErrorContext(ctx, "the data directory could not be prepared", slog.Any("error", err))
		os.Exit(1)
	}

	log := openLog(ctx, resolved)
	slog.SetDefault(log)

	// Read before the window exists, so the first frame is painted in the stored language and
	// direction (middleware.go).
	locale := settingsdb.PeekLocale(ctx, resolved.DBFile, log)

	set := api.New(version, log)
	sh := newShell(set, resolved, log, version, bootstrap.Start)

	if err := wails.Run(appOptions(appConfig{
		Assets:         assets,
		Paths:          resolved,
		Locale:         locale,
		Version:        version,
		Bindings:       set.Bindings(),
		OnStartup:      sh.startup,
		OnShutdown:     sh.shutdown,
		OnSecondLaunch: sh.secondLaunch,
	})); err != nil {
		sh.shutdown(ctx)
		log.ErrorContext(ctx, "mizan lite failed to start", slog.Any("error", err))
		os.Exit(1)
	}
}

// openLog writes to the data directory's log file and, when there is one, the terminal. A log file
// that cannot be opened is not a reason to refuse to start: the application falls back to stderr.
func openLog(ctx context.Context, resolved paths.Paths) *slog.Logger {
	file, err := logging.Open(resolved.Logs, logging.DefaultMaxBytes)
	if err != nil {
		log := logging.New(os.Stderr)
		log.WarnContext(ctx, "the log file could not be opened; logging to stderr only", slog.Any("error", err))
		return log
	}
	return logging.New(io.MultiWriter(file, os.Stderr))
}
