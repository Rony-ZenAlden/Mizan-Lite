package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wailsapp/wails/v2/pkg/options"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/api"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/lite/paths"
	"github.com/mizan-erp/mizan/internal/platform/backup"
)

func testPaths(t *testing.T) paths.Paths {
	t.Helper()
	p := paths.Layout(filepath.Join(t.TempDir(), "Mizan Lite"))
	for _, dir := range []string{p.Data, p.Backups, p.Logs, p.WebView} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func waitBooted(t *testing.T, s *shell) {
	t.Helper()
	select {
	case <-s.booted:
	case <-time.After(30 * time.Second):
		t.Fatal("boot did not finish")
	}
}

func TestBootAttachesTheGraphAndShutdownClosesIt(t *testing.T) {
	set := api.New("1.0-test", litetest.Logger())
	s := newShell(set, testPaths(t), litetest.Logger(), "1.0-test", bootstrap.Start)

	s.startup(context.Background())
	waitBooted(t, s)

	health := set.App.Health()
	if !health.OK || health.Data.Version != "1.0-test" || health.Data.SchemaVersion != 1 {
		t.Fatalf("Health after boot = %+v %+v", health.Data, health.Error)
	}

	s.mu.Lock()
	app := s.app
	s.mu.Unlock()
	s.shutdown(context.Background())
	if err := app.DB.WriterPool().PingContext(context.Background()); err == nil {
		t.Fatal("the database is still open after the window closed")
	}
	s.shutdown(context.Background()) // a second close is a no-op, not a double-close
}

func TestAFailedBootIsReportedToTheBootScreen(t *testing.T) {
	set := api.New("v", litetest.Logger())
	failing := func(context.Context, bootstrap.Options) (*bootstrap.App, error) {
		return nil, errs.Internal(bootstrap.CodeMigrationFailed, "failed").WithParam("backup", "/b.db")
	}
	s := newShell(set, testPaths(t), litetest.Logger(), "v", failing)

	s.startup(context.Background())
	waitBooted(t, s)

	status := set.App.BootStatus().Data
	if status.State != "failed" || status.Error == nil || status.Error.Code != bootstrap.CodeMigrationFailed {
		t.Fatalf("BootStatus = %+v", status)
	}
	if status.BackupPath != "/b.db" {
		t.Fatalf("backupPath = %q", status.BackupPath)
	}
	s.shutdown(context.Background()) // nothing was built; closing must not panic
}

// TestAWindowClosedDuringBootStillReleasesTheDatabase: shutdown arrives while the graph is still
// being built. Nothing will call shutdown again, so the boot that finishes afterwards must close
// what it built — otherwise the database stays open, and on Windows locked, until the process dies.
func TestAWindowClosedDuringBootStillReleasesTheDatabase(t *testing.T) {
	set := api.New("v", litetest.Logger())
	release := make(chan struct{})
	var built *bootstrap.App
	slow := func(ctx context.Context, opts bootstrap.Options) (*bootstrap.App, error) {
		<-release
		app, err := bootstrap.Start(ctx, opts)
		built = app
		return app, err
	}
	s := newShell(set, testPaths(t), litetest.Logger(), "v", slow)

	s.startup(context.Background())
	s.shutdown(context.Background()) // the window closes first
	close(release)                   // then the graph finishes
	waitBooted(t, s)

	if built == nil {
		t.Fatal("the slow start never built a graph")
	}
	if err := built.DB.WriterPool().PingContext(context.Background()); err == nil {
		t.Fatal("a graph finished after the window closed was left open")
	}
	if set.App.BootStatus().Data.State == "ready" {
		t.Fatal("a graph finished after the window closed was attached to the bindings")
	}
}

func TestASecondLaunchBeforeTheWindowOpensIsIgnored(t *testing.T) {
	s := newShell(api.New("v", litetest.Logger()), testPaths(t), litetest.Logger(), "v", bootstrap.Start)
	s.secondLaunch(options.SecondInstanceData{}) // no context yet: must return rather than call the runtime with nil
}

// TestClosingWithACancelledContextStillTakesTheClosingBackup pins the shutdown's independence from
// its caller's cancellation. Wails does not cancel the context today; if a future version did, a
// shutdown that inherited it would skip the end-of-day snapshot silently.
func TestClosingWithACancelledContextStillTakesTheClosingBackup(t *testing.T) {
	p := testPaths(t)
	clk := clock.NewFixed(time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC))
	withClock := func(ctx context.Context, opts bootstrap.Options) (*bootstrap.App, error) {
		opts.Clock = clk
		return bootstrap.Start(ctx, opts)
	}
	set := api.New("v", litetest.Logger())
	s := newShell(set, p, litetest.Logger(), "v", withClock)
	s.startup(context.Background())
	waitBooted(t, s)

	clk.Advance(3 * time.Hour) // a trading day later, past the close-time snapshot's minimum age
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	s.shutdown(cancelled)

	list, err := backup.New(nil, backup.Options{Dir: p.Backups}).List()
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range list {
		if b.Manifest.Reason == backup.OnClose {
			return
		}
	}
	t.Fatalf("no close-time snapshot after closing with a cancelled context; backups: %+v", list)
}
