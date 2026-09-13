package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/api"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/lite/paths"
	"github.com/mizan-erp/mizan/internal/lite/settings/domain"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
)

func attached(t *testing.T, log *slog.Logger) (*api.Set, *bootstrap.App) {
	t.Helper()
	p := paths.Layout(filepath.Join(t.TempDir(), "data"))
	for _, dir := range []string{p.Data, p.Backups, p.Logs, p.WebView} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	app, err := bootstrap.Start(context.Background(), bootstrap.Options{Paths: p, Logger: litetest.Logger()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
	set := api.New("1.2.3-test", log)
	set.Attach(app)
	return set, app
}

func codeOf[T any](t *testing.T, r envelope.Result[T]) string {
	t.Helper()
	if r.OK {
		t.Fatalf("expected a failure, got %+v", r.Data)
	}
	if r.Error == nil {
		t.Fatal("a failed result carries no error")
	}
	return r.Error.Code
}

func TestBeforeBootEveryGraphMethodRefusesButBootStatusAnswers(t *testing.T) {
	set := api.New("v", litetest.Logger())

	if got := codeOf(t, set.App.Health()); got != api.CodeNotReady {
		t.Errorf("Health code = %q", got)
	}
	if got := codeOf(t, set.Settings.Get()); got != api.CodeNotReady {
		t.Errorf("Settings.Get code = %q", got)
	}
	en := "en"
	if got := codeOf(t, set.Settings.Update(api.SettingsInput{Locale: &en})); got != api.CodeNotReady {
		t.Errorf("Settings.Update code = %q", got)
	}

	status := set.App.BootStatus()
	if !status.OK || status.Data.State != "starting" || status.Data.Error != nil {
		t.Fatalf("BootStatus = %+v", status)
	}
}

func TestBootStatusReportsMigrationProgress(t *testing.T) {
	set := api.New("v", litetest.Logger())
	set.Progress(migrate.Progress{Phase: migrate.PhaseMigrating, Current: 2, Total: 5})

	got := set.App.BootStatus().Data
	if got.Phase != "migrating" || got.Current != 2 || got.Total != 5 || got.State != "starting" {
		t.Fatalf("BootStatus = %+v", got)
	}
}

func TestAFailedBootReportsTheHeadlineTheReasonAndTheBackup(t *testing.T) {
	set := api.New("v", litetest.Logger())
	cause := errs.Internal(migrate.CodeInsufficientDisk, "disk full").WithParam("backup", "/data/backups/b.db")
	boot := errs.Wrap(cause, errs.CategoryInternal, bootstrap.CodeMigrationFailed, "update failed").
		WithParam("backup", "/data/backups/b.db")
	set.Fail(boot)

	got := set.App.BootStatus()
	if !got.OK {
		t.Fatal("BootStatus must succeed even after a failed boot")
	}
	d := got.Data
	if d.State != "failed" {
		t.Fatalf("state = %q", d.State)
	}
	if d.Error == nil || d.Error.Code != bootstrap.CodeMigrationFailed {
		t.Fatalf("error = %+v", d.Error)
	}
	if d.Reason == nil || d.Reason.Code != migrate.CodeInsufficientDisk {
		t.Fatalf("reason = %+v, want the specific cause", d.Reason)
	}
	if d.BackupPath != "/data/backups/b.db" {
		t.Fatalf("backupPath = %q", d.BackupPath)
	}

	if code := codeOf(t, set.App.Health()); code != api.CodeStartupFailed {
		t.Fatalf("after a failed boot Health code = %q", code)
	}
}

func TestAFailureWithNoDeeperCauseHasNoSeparateReason(t *testing.T) {
	set := api.New("v", litetest.Logger())
	set.Fail(errs.Internal(bootstrap.CodeStartupFailed, "plain"))
	if r := set.App.BootStatus().Data.Reason; r != nil {
		t.Fatalf("reason = %+v, want none — repeating the headline as its own reason says nothing", r)
	}
}

func TestAttachedBindingsServeTheRealGraph(t *testing.T) {
	var logs bytes.Buffer
	set, app := attached(t, slog.New(slog.NewTextHandler(&logs, nil)))

	health := set.App.Health()
	if !health.OK {
		t.Fatalf("Health = %+v", health.Error)
	}
	want := api.HealthDTO{Version: "1.2.3-test", SchemaVersion: 1, Platform: runtime.GOOS, DataDir: app.Paths.Data}
	if health.Data != want {
		t.Fatalf("Health = %+v, want %+v", health.Data, want)
	}
	if !strings.Contains(logs.String(), "health requested by the frontend") {
		t.Fatal("Health was not logged; the packaged-build check depends on that line")
	}
	if status := set.App.BootStatus().Data; status.State != "ready" {
		t.Fatalf("BootStatus state = %q", status.State)
	}

	got := set.Settings.Get()
	if !got.OK || got.Data != (api.SettingsDTO{Locale: "ar", Direction: "rtl"}) {
		t.Fatalf("Settings.Get = %+v", got)
	}

	en := "en"
	updated := set.Settings.Update(api.SettingsInput{Locale: &en})
	if !updated.OK || updated.Data != (api.SettingsDTO{Locale: "en", Direction: "ltr"}) {
		t.Fatalf("Settings.Update = %+v", updated)
	}

	bad := "fr"
	if code := codeOf(t, set.Settings.Update(api.SettingsInput{Locale: &bad})); code != domain.CodeInvalidLocale {
		t.Fatalf("invalid locale code = %q", code)
	}
	if again := set.Settings.Get(); again.Data.Locale != "en" {
		t.Fatalf("a refused update changed the stored locale: %+v", again.Data)
	}

	if unchanged := set.Settings.Update(api.SettingsInput{}); !unchanged.OK || unchanged.Data.Locale != "en" {
		t.Fatalf("an empty update = %+v", unchanged)
	}
}

func TestAPanicBecomesATranslatableCodeAndIsLoggedWithItsStack(t *testing.T) {
	var logs bytes.Buffer
	set, _ := attached(t, slog.New(slog.NewTextHandler(&logs, nil)))

	result := api.CallForTest(set, "Test.Panics", func(context.Context, *bootstrap.App) (int, error) {
		unset()["boom"] = 1 // a nil-map write: a real runtime panic, not a hand-thrown one
		return 0, nil
	})
	if code := codeOf(t, result); code != api.CodeInternal {
		t.Fatalf("code = %q", code)
	}
	if !strings.Contains(logs.String(), "a binding panicked") || !strings.Contains(logs.String(), "Test.Panics") {
		t.Fatalf("the panic was not logged with its method:\n%s", logs.String())
	}
}

func TestAnUntypedErrorIsLoggedButItsTextNeverCrossesTheBoundary(t *testing.T) {
	var logs bytes.Buffer
	set, _ := attached(t, slog.New(slog.NewTextHandler(&logs, nil)))
	secret := "near \"SELEC\": syntax error in SELECT * FROM customers"

	result := api.CallForTest(set, "Test.Untyped", func(context.Context, *bootstrap.App) (int, error) {
		return 0, errors.New(secret)
	})
	if code := codeOf(t, result); code != envelope.CodeInternal {
		t.Fatalf("code = %q", code)
	}
	wire, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), "SELEC") {
		t.Fatalf("a developer string crossed the boundary: %s", wire)
	}
	if !strings.Contains(logs.String(), "SELEC") {
		t.Fatal("the untyped error's text was lost entirely; it must survive in the log")
	}
}

func TestTheWireShapeIsWhatTheFrontendUnwraps(t *testing.T) {
	set, _ := attached(t, litetest.Logger())
	wire, err := json.Marshal(set.Settings.Get())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"ok":true,"data":{"locale":"ar","direction":"rtl"}}`
	if string(wire) != want {
		t.Fatalf("wire = %s\nwant %s", wire, want)
	}

	failed, err := json.Marshal(api.New("v", litetest.Logger()).Settings.Get())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(failed), `"ok":false`) || !strings.Contains(string(failed), `"code":"lite.api.not_ready"`) {
		t.Fatalf("failure wire = %s", failed)
	}
}

// TestBootStateIsSafeUnderConcurrentReads runs under -race in lite-ci: the boot goroutine writes
// progress while the webview polls BootStatus from another.
func TestBootStateIsSafeUnderConcurrentReads(t *testing.T) {
	set := api.New("v", litetest.Logger())
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			set.Progress(migrate.Progress{Phase: migrate.PhaseMigrating, Current: i, Total: 8})
		}(i)
		go func() {
			defer wg.Done()
			_ = set.App.BootStatus()
			_ = set.App.Health()
		}()
	}
	wg.Wait()
}

// unset returns a nil map from a call, so the write above is a genuine runtime panic that static analysis
// does not (rightly) refuse as an obvious nil dereference written inline.
func unset() map[string]int { return nil }
