package main

import (
	"context"
	"log/slog"
	"sync"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/mizan-erp/mizan/internal/api/bindings"
	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/buildinfo"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
	"github.com/mizan-erp/mizan/internal/platform/paths"
)

// Boot event names. The frontend subscribes to these to update sooner than its poll would.
const (
	eventBootProgress = "boot:progress"
	eventBootReady    = "boot:ready"
	eventBootFailed   = "boot:failed"
)

// Shell owns the application lifecycle around the Wails window.
//
// # Why the graph is built AFTER the window opens (Step 0.11 D2)
//
// Step 0.10 built the graph first, deliberately: "opening the UI first would mean rendering a
// shell over a database that might be about to be restored." That reasoning is preserved
// exactly — but it applies to the SHELL, not to the window. What renders while the graph
// builds is a boot screen with no data access and no controls; the application shell does not
// mount until boot succeeds.
//
// Separating those two is what lets both properties hold: migration progress is visible, a
// failed start has somewhere to explain itself, and nothing touches a database mid-restore.
type Shell struct {
	bindings *bindings.Set
	paths    paths.Paths

	mu  sync.Mutex
	app *bootstrap.App
}

// NewShell wires the shell to the binding set Wails was handed.
func NewShell(set *bindings.Set, resolved paths.Paths) *Shell {
	return &Shell{bindings: set, paths: resolved}
}

// startup runs when the window is ready. It must not block: Wails calls it on the main thread,
// and a migration can take minutes.
func (s *Shell) startup(ctx context.Context) {
	slog.InfoContext(ctx, "mizan window ready", slog.String("version", buildinfo.Version))
	go s.boot(ctx)
}

// boot builds the object graph, reporting progress and failure to the boot screen.
func (s *Shell) boot(ctx context.Context) {
	built, err := bootstrap.Start(ctx, bootstrap.Options{
		Paths:          s.paths,
		StartScheduler: true,
		Progress: func(p migrate.Progress) {
			s.bindings.Progress(p)
			wailsruntime.EventsEmit(ctx, eventBootProgress)
		},
	})
	if err != nil {
		// Nothing is attached, so every graph-backed binding keeps reporting not-ready and the
		// shell stays on the failure screen. The user reads a translated message built from
		// the error's code and params — never English prose (§22.2) — including where their
		// backup is, which is the only thing that actually matters to them.
		s.bindings.Fail(err, backupPathOf(err))
		wailsruntime.EventsEmit(ctx, eventBootFailed)
		slog.ErrorContext(ctx, "mizan could not start",
			slog.String("code", errs.CodeOf(err)),
			slog.Any("error", err))
		return
	}

	s.mu.Lock()
	s.app = built
	s.mu.Unlock()

	s.bindings.Attach(built)
	wailsruntime.EventsEmit(ctx, eventBootReady)
	slog.InfoContext(ctx, "mizan ready")
}

// shutdown tears the graph down when the window closes.
//
// A boot that failed, or one still in flight, leaves app nil — closing is then a no-op, which
// is correct: bootstrap.Start already abandoned whatever it had built.
func (s *Shell) shutdown(ctx context.Context) {
	if app := s.take(); app != nil {
		if err := app.Shutdown(ctx); err != nil {
			slog.WarnContext(ctx, "shutdown completed with errors", slog.Any("error", err))
		}
	}
}

// abandon tears down the graph after Wails itself failed.
func (s *Shell) abandon(ctx context.Context) {
	if app := s.take(); app != nil {
		if err := app.Shutdown(ctx); err != nil {
			slog.WarnContext(ctx, "shutdown after a failed start reported errors",
				slog.Any("error", err))
		}
	}
}

// take returns the built graph exactly once, so a shutdown followed by an abandon (or two
// shutdowns) cannot double-close the database.
func (s *Shell) take() *bootstrap.App {
	s.mu.Lock()
	defer s.mu.Unlock()
	app := s.app
	s.app = nil
	return app
}

// backupPathOf extracts the pre-migration backup path from a startup failure, if it has one.
//
// Step 0.4 attaches it as a parameter on the migration error, and bootstrap now carries the
// cause's parameters onto its wrapper — so a single lookup finds it whether the error came
// from the runner directly or through the composition root.
func backupPathOf(err error) string {
	e, ok := errs.AsError(err)
	if !ok {
		return ""
	}
	return e.Params["backup"]
}
