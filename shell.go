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
		// The version a backup's manifest records (9.1 D2). Added in Phase 9 and never set until
		// 10.4 went looking — so every manifest carried an empty string where a support
		// conversation expects a build number.
		//
		// From `buildinfo`, which the Makefile and both packaging scripts stamp from
		// `scripts/version.sh` — one source, three consumers, and now a fourth.
		AppVersion: buildinfo.Version,
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

	// The binding surface must be fully declared before it is usable (Step 1.5).
	//
	// Checked here rather than in bootstrap because the SET is the shell's, not the graph's —
	// and it is fatal: a method reachable from JavaScript with no declared policy is exactly
	// what §14.3 says must never ship.
	if err := s.bindings.ValidatePolicies(declaredPermissionCodes(built)); err != nil {
		s.bindings.Fail(err, "")
		wailsruntime.EventsEmit(ctx, eventBootFailed)
		slog.ErrorContext(ctx, "the binding surface is not fully protected", slog.Any("error", err))
		return
	}

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

// declaredPermissionCodes collects every permission the built modules declare, so the policy
// check can reject a policy naming one that could never be granted.
func declaredPermissionCodes(app *bootstrap.App) map[string]bool {
	out := map[string]bool{}
	for _, m := range app.Modules {
		for _, def := range m.Permissions() {
			out[def.Code] = true
		}
	}
	return out
}
