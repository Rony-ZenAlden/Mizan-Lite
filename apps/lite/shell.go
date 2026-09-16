package main

import (
	"context"
	"log/slog"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/options"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/api"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/fx"
	"github.com/mizan-erp/mizan/internal/lite/fx/infra/httpsource"
	"github.com/mizan-erp/mizan/internal/lite/paths"
	settingsdomain "github.com/mizan-erp/mizan/internal/lite/settings/domain"
)

// startFunc builds the graph. bootstrap.Start in the application; a controllable fake in tests.
type startFunc func(context.Context, bootstrap.Options) (*bootstrap.App, error)

// shell owns the lifecycle around the window: build the graph after the window opens, attach it to
// the bindings, and tear it down when the window closes.
type shell struct {
	set     *api.Set
	paths   paths.Paths
	log     *slog.Logger
	version string
	start   startFunc
	// rates fetches the exchange rate from the internet. Set by main to the real providers; nil in tests, so no test
	// of the shell reaches the network (L3 D-L3.26).
	rates fx.Source

	// reload repaints the window after a restart; wailsruntime.WindowReloadApp in the application, nil in tests.
	reload func(context.Context)

	mu      sync.Mutex
	ctx     context.Context
	app     *bootstrap.App
	closing bool
	booted  chan struct{}
}

// localRateProvider is the shop's own market endpoint, read from its settings at the moment of a fetch. It answers
// ok=false — leaving the published providers to it — when the shop has not chosen the local source, has not set a URL, or
// the graph is not up yet.
//
// A bad URL is not an error here: the endpoint is checked when it is saved, and a fetch that refused to run because of a
// stored value would leave the shop with no rate at all rather than the published one.
func (s *shell) localRateProvider(ctx context.Context) (httpsource.Provider, bool) {
	s.mu.Lock()
	app := s.app
	s.mu.Unlock()
	if app == nil {
		return httpsource.Provider{}, false
	}
	stored, err := app.Settings.Get(ctx)
	if err != nil || stored.RateSource != settingsdomain.RateSourceLocal {
		return httpsource.Provider{}, false
	}
	// With no address of its own the shop gets the market page Mizan ships with (2026-09-17), so choosing the local source
	// works without being asked for anything. A shop that HAS its own endpoint keeps using it.
	if stored.LocalRateURL == "" {
		return httpsource.SPTodayProvider(), true
	}
	return httpsource.LocalProvider(httpsource.LocalConfig{URL: stored.LocalRateURL, Field: stored.LocalRateField}), true
}

func newShell(set *api.Set, p paths.Paths, log *slog.Logger, version string, start startFunc) *shell {
	return &shell{set: set, paths: p, log: log, version: version, start: start, booted: make(chan struct{})}
}

// startup runs when the window is ready. It must return at once: Wails calls it on the main thread,
// and a migration can take minutes.
func (s *shell) startup(ctx context.Context) {
	s.mu.Lock()
	s.ctx = ctx
	s.mu.Unlock()
	s.set.SetContext(ctx)
	go s.boot(ctx)
}

func (s *shell) boot(ctx context.Context) {
	s.mu.Lock()
	booted := s.booted
	s.mu.Unlock()
	defer close(booted)

	app, err := s.start(ctx, bootstrap.Options{
		Paths:          s.paths,
		Logger:         s.log,
		AppVersion:     s.version,
		StartScheduler: true,
		Progress:       s.set.Progress,
		RateSource:     s.rates,
	})
	if err != nil {
		s.set.Fail(err)
		s.log.ErrorContext(ctx, "mizan lite could not start",
			slog.String("code", errs.CodeOf(err)), slog.Any("error", err))
		return
	}

	s.mu.Lock()
	if s.closing {
		// The window closed while the graph was still being built. Nothing will ever call shutdown
		// again, so the graph this boot produced must be torn down here or the database stays open —
		// on Windows, locked until the process dies.
		s.mu.Unlock()
		if err := app.Shutdown(context.WithoutCancel(ctx)); err != nil {
			s.log.WarnContext(ctx, "shutting down a graph finished after the window closed", slog.Any("error", err))
		}
		return
	}
	s.app = app
	s.mu.Unlock()

	s.set.Attach(app)
	s.log.InfoContext(ctx, "mizan lite ready", slog.String("version", s.version))
}

// shutdown tears the graph down when the window closes. Safe to call more than once, and safe to
// call while boot is still running.
func (s *shell) shutdown(ctx context.Context) {
	s.mu.Lock()
	s.closing = true
	app := s.app
	s.app = nil
	s.mu.Unlock()

	if app == nil {
		return
	}
	// WithoutCancel, so a cancellation can never skip the close-time backup. Wails v2.13 passes an
	// UNcancelled context here (internal/app/app_production.go hands OnShutdown the app's own
	// context), so today this changes nothing — it exists so the backup does not depend on that
	// framework detail staying true. The shutdown steps carry their own timeouts.
	if err := app.Shutdown(context.WithoutCancel(ctx)); err != nil {
		s.log.WarnContext(ctx, "shutdown completed with errors", slog.Any("error", err))
	}
}

// restart rebuilds the graph after a restore is staged (L7 §6.4): the bindings refuse while it happens, the graph closes, and
// a new start applies the staged file before opening the database. In the same process — nothing is relaunched — and the
// window reloads onto the restored shop.
func (s *shell) restart() {
	s.mu.Lock()
	ctx, app, closing := s.ctx, s.app, s.closing
	if closing || ctx == nil {
		s.mu.Unlock()
		return
	}
	s.app = nil
	s.booted = make(chan struct{})
	s.mu.Unlock()

	s.set.Detach()
	if app != nil {
		if err := app.Shutdown(context.WithoutCancel(ctx)); err != nil {
			s.log.WarnContext(ctx, "closing the graph before a restore completed with errors", slog.Any("error", err))
		}
	}
	s.log.InfoContext(ctx, "restarting to apply a restore")
	s.boot(ctx)
	if s.reload != nil {
		s.reload(ctx)
	}
}

// secondLaunch brings the running window forward when the application is launched again.
func (s *shell) secondLaunch(options.SecondInstanceData) {
	s.mu.Lock()
	ctx := s.ctx
	s.mu.Unlock()
	if ctx == nil {
		return // launched again before the first window finished opening; nothing to focus yet
	}
	wailsruntime.WindowUnminimise(ctx)
	wailsruntime.Show(ctx)
}
