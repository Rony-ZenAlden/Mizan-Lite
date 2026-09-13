package bootstrap_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	mizanmigrations "github.com/mizan-erp/mizan/migrations"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/locale"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/lite/paths"
	"github.com/mizan-erp/mizan/internal/lite/settings/domain"
	"github.com/mizan-erp/mizan/internal/platform/backup"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
)

// dataDir builds a real data tree the way paths.Resolve does, under a name carrying the characters
// that have broken paths before: Arabic, a space, and `#`.
func dataDir(t *testing.T) paths.Paths {
	t.Helper()
	p := paths.Layout(filepath.Join(t.TempDir(), "محمد #1", "Mizan Lite"))
	for _, dir := range []string{p.Data, p.Backups, p.Logs, p.WebView} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func start(t *testing.T, p paths.Paths, clk clock.Clock) *bootstrap.App {
	t.Helper()
	app, err := bootstrap.Start(context.Background(), bootstrap.Options{
		Paths: p, Logger: litetest.Logger(), Clock: clk, AppVersion: "test-1",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
	return app
}

func TestAFreshInstallationStartsMigratedAndUsable(t *testing.T) {
	ctx := context.Background()
	p := dataDir(t)
	app := start(t, p, clock.System())

	if want := litetest.LatestSchemaVersion(t); app.SchemaVersion != want {
		t.Errorf("SchemaVersion = %d, want %d", app.SchemaVersion, want)
	}
	if _, err := os.Stat(p.DBFile); err != nil {
		t.Fatalf("no database at %q: %v", p.DBFile, err)
	}
	got, err := app.Settings.Get(ctx)
	if err != nil || got != domain.Defaults() {
		t.Fatalf("Settings.Get = %+v, %v", got, err)
	}
	for _, l := range []locale.Locale{"ar", "en"} {
		if !app.Messages.Has(l, "app.name") {
			t.Errorf("the %s catalog was not loaded", l)
		}
	}
	keys := app.Scheduler.Registry().Keys()
	if len(keys) != 1 || keys[0] != bootstrap.KeyScheduledBackup {
		t.Fatalf("declared jobs = %v, want only the scheduled backup", keys)
	}
}

func TestAFreshInstallationIsBackedUpImmediatelyNotADayLater(t *testing.T) {
	// Mizan 1.0.0's promise: the daily snapshot runs on a fresh install at once. If the job's first
	// occurrence were a day away, a shop's first day would have no copy at all.
	ctx := context.Background()
	clk := clock.NewFixed(time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC))
	app := start(t, dataDir(t), clk)

	states, err := app.Scheduler.JobStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("job states = %+v", states)
	}
	if states[0].NextRunAt.After(clk.Now()) {
		t.Fatalf("the first backup is due at %v, after launch at %v", states[0].NextRunAt, clk.Now())
	}
}

func TestTheScheduledBackupTakesAVerifiedSnapshot(t *testing.T) {
	ctx := context.Background()
	app := start(t, dataDir(t), clock.System())

	if err := app.Scheduler.RunNow(ctx, bootstrap.KeyScheduledBackup); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	list, err := app.Backups.List()
	if err != nil {
		t.Fatal(err)
	}
	// The migration runner also snapshots the (empty) database before applying 0001, so the list
	// holds a before_migration entry too. Only the scheduled one is this job's.
	var scheduled []backup.Backup
	for _, b := range list {
		if b.Manifest.Reason == backup.Scheduled {
			scheduled = append(scheduled, b)
		}
	}
	if len(scheduled) != 1 {
		t.Fatalf("scheduled snapshots = %d in %+v, want 1", len(scheduled), list)
	}
	if m := scheduled[0].Manifest; m.SchemaVersion != litetest.LatestSchemaVersion(t) || m.AppVersion != "test-1" {
		t.Fatalf("manifest = %+v, want the latest schema and the build's version", m)
	}
}

func TestRestartingKeepsDataAndAppliesNothing(t *testing.T) {
	ctx := context.Background()
	p := dataDir(t)

	first, err := bootstrap.Start(ctx, bootstrap.Options{Paths: p, Logger: litetest.Logger()})
	if err != nil {
		t.Fatal(err)
	}
	en := "en"
	if _, err = first.Settings.Update(ctx, domain.Update{Locale: &en}); err != nil {
		t.Fatal(err)
	}
	if err = first.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	second := start(t, p, clock.System())
	got, err := second.Settings.Get(ctx)
	if err != nil || got.Locale != domain.English {
		t.Fatalf("after restart Settings.Get = %+v, %v", got, err)
	}
	if second.SchemaVersion != litetest.LatestSchemaVersion(t) {
		t.Fatalf("SchemaVersion = %d", second.SchemaVersion)
	}
}

func TestShutdownTakesAClosingSnapshotOnlyWhenNoneIsRecent(t *testing.T) {
	ctx := context.Background()
	p := dataDir(t)
	clk := clock.NewFixed(time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC))

	onClose := func() int {
		t.Helper()
		svc := backup.New(nil, backup.Options{Dir: p.Backups})
		list, err := svc.List()
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, b := range list {
			if b.Manifest.Reason == backup.OnClose {
				n++
			}
		}
		return n
	}

	open := func() *bootstrap.App {
		app, err := bootstrap.Start(ctx, bootstrap.Options{Paths: p, Logger: litetest.Logger(), Clock: clk})
		if err != nil {
			t.Fatal(err)
		}
		return app
	}

	// The rule is Mizan 10.16's: skip when ANY snapshot is under an hour old, because the question
	// is whether the data is covered, not which kind of snapshot covered it. A fresh start takes a
	// before_migration snapshot at launch, so a close inside that hour is correctly skipped — and
	// the first close below comes after a day's trading.
	app := open()
	clk.Advance(2 * time.Hour)
	if err := app.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if n := onClose(); n != 1 {
		t.Fatalf("closing two hours after launch took %d close-time snapshots, want 1", n)
	}

	// Opened and closed again ten minutes later: the day's work is already covered.
	clk.Advance(10 * time.Minute)
	app = open()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if n := onClose(); n != 1 {
		t.Fatalf("a close ten minutes after the last snapshot took another: %d", n)
	}

	// Two hours later it is not.
	clk.Advance(2 * time.Hour)
	app = open()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if n := onClose(); n != 2 {
		t.Fatalf("a close two hours later took %d close-time snapshots in total, want 2", n)
	}
}

func TestShutdownIsSafeToCallTwiceAndReleasesTheFile(t *testing.T) {
	ctx := context.Background()
	p := dataDir(t)
	app, err := bootstrap.Start(ctx, bootstrap.Options{Paths: p, Logger: litetest.Logger()})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := app.Shutdown(ctx); err != nil {
		t.Fatalf("a second Shutdown failed: %v", err)
	}
	// The direct proof, on every platform: the pools are closed.
	if err := app.DB.WriterPool().PingContext(ctx); err == nil {
		t.Fatal("the database writer still answers after Shutdown")
	}
	// On Windows a file still held open cannot be renamed. macOS allows renaming an open file, so
	// this line proves nothing here — the ping above is what fails on this platform.
	if err := os.Rename(p.DBFile, p.DBFile+".moved"); err != nil {
		t.Fatalf("the database is still held after Shutdown: %v", err)
	}
}

func TestStartRequiresPaths(t *testing.T) {
	_, err := bootstrap.Start(context.Background(), bootstrap.Options{Logger: litetest.Logger()})
	if errs.CodeOf(err) != bootstrap.CodeStartupFailed {
		t.Fatalf("code = %q", errs.CodeOf(err))
	}
}

// migratedAt creates a Lite database at p and closes it, for tests that then damage it.
func migratedAt(t *testing.T, p paths.Paths) {
	t.Helper()
	app, err := bootstrap.Start(context.Background(), bootstrap.Options{Paths: p, Logger: litetest.Logger()})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func execOn(t *testing.T, path, statement string) {
	t.Helper()
	store, err := database.Open(database.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if _, err := store.WriterPool().ExecContext(context.Background(), statement); err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
}

// innermostCode walks the error chain to the deepest typed error — the specific reason under the
// bootstrap's wrapper.
func innermostCode(err error) string {
	code := ""
	for e := err; e != nil; e = errors.Unwrap(e) {
		var typed *errs.Error
		if errors.As(e, &typed) && typed == e {
			code = typed.Code
		}
	}
	return code
}

func TestAnAlteredMigrationRefusesToStart(t *testing.T) {
	p := dataDir(t)
	migratedAt(t, p)
	execOn(t, p.DBFile, `UPDATE schema_migrations SET checksum = 'tampered' WHERE version = 1`)

	_, err := bootstrap.Start(context.Background(), bootstrap.Options{Paths: p, Logger: litetest.Logger()})
	if errs.CodeOf(err) != bootstrap.CodeMigrationFailed {
		t.Fatalf("outer code = %q (err %v)", errs.CodeOf(err), err)
	}
	if got := innermostCode(err); got != migrate.CodeChecksumMismatch {
		t.Fatalf("inner reason = %q, want %q", got, migrate.CodeChecksumMismatch)
	}
}

func TestADatabaseFromANewerBuildRefusesToStart(t *testing.T) {
	p := dataDir(t)
	migratedAt(t, p)
	execOn(t, p.DBFile, `INSERT INTO schema_migrations (version, name, checksum, applied_at)
		VALUES (99, 'future', 'x', '2030-01-01T00:00:00.000Z')`)

	_, err := bootstrap.Start(context.Background(), bootstrap.Options{Paths: p, Logger: litetest.Logger()})
	if got := innermostCode(err); got != migrate.CodeDatabaseTooNew {
		t.Fatalf("inner reason = %q (err %v), want %q", got, err, migrate.CodeDatabaseTooNew)
	}
}

func TestAFileThatIsNotADatabaseFailsWithATypedError(t *testing.T) {
	p := dataDir(t)
	if err := os.WriteFile(p.DBFile, []byte("this is a text file, not a database, and it is long enough"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := bootstrap.Start(context.Background(), bootstrap.Options{Paths: p, Logger: litetest.Logger()})
	code := errs.CodeOf(err)
	if code != bootstrap.CodeStartupFailed && code != bootstrap.CodeMigrationFailed {
		t.Fatalf("code = %q (err %v), want a typed bootstrap failure", code, err)
	}
}

// TestAMizanDatabaseIsRefusedAndLeftUntouched is the side-by-side guarantee from the other
// direction. Lite's own resolver never points at Mizan's file, but an override or a copied file
// could; when it happens, Lite must refuse to start rather than migrate somebody's books.
func TestAMizanDatabaseIsRefusedAndLeftUntouched(t *testing.T) {
	ctx := context.Background()
	p := dataDir(t)

	store, err := database.Open(database.Config{Path: p.DBFile})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := migrate.New(store, migrate.Options{
		FS: mizanmigrations.SQLite(), DBPath: p.DBFile, SkipBackup: true, Logger: litetest.Logger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runner.Up(ctx); err != nil {
		t.Fatalf("building a Mizan database: %v", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	before := fileHash(t, p.DBFile)

	_, err = bootstrap.Start(ctx, bootstrap.Options{Paths: p, Logger: litetest.Logger()})
	if errs.CodeOf(err) != bootstrap.CodeMigrationFailed {
		t.Fatalf("Lite started on a Mizan database: code %q, err %v", errs.CodeOf(err), err)
	}
	if after := fileHash(t, p.DBFile); after != before {
		t.Fatal("refusing a Mizan database still changed the file")
	}
}

func fileHash(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestAWrappedFailureKeepsTheCausesParameters(t *testing.T) {
	// The migration runner attaches the pre-migration backup's path to its error. The boot screen
	// reads the OUTERMOST error's parameters, so a wrapper that dropped them would hide the one
	// thing the shopkeeper needs to hear: where their data is.
	cause := errs.Internal(migrate.CodeMigrationFailed, "failed").WithParam("backup", "/data/backups/x.db")
	wrapped := bootstrap.WrapKeepingParams(cause, bootstrap.CodeMigrationFailed, "wrapped")

	typed, ok := errs.AsError(wrapped)
	if !ok {
		t.Fatalf("the wrapper is not a typed error: %v", wrapped)
	}
	if typed.Code != bootstrap.CodeMigrationFailed {
		t.Fatalf("outer code = %q", typed.Code)
	}
	if typed.Params["backup"] != "/data/backups/x.db" {
		t.Fatalf("params = %v, want the cause's backup path carried", typed.Params)
	}
	if !errors.Is(wrapped, cause) {
		t.Fatal("the cause is no longer reachable through the chain")
	}
}

// TestAnUnreadableSettingsTableIsFoundAtLaunch pins the launch-time read. Without it, a damaged
// table would surface as an error on the first screen that asked — after the shell had drawn itself
// over a graph that could not work — rather than on the boot screen, where it can be explained.
func TestAnUnreadableSettingsTableIsFoundAtLaunch(t *testing.T) {
	p := dataDir(t)
	migratedAt(t, p)
	execOn(t, p.DBFile, `DROP TABLE settings`)

	_, err := bootstrap.Start(context.Background(), bootstrap.Options{Paths: p, Logger: litetest.Logger()})
	if errs.CodeOf(err) != bootstrap.CodeStartupFailed {
		t.Fatalf("code = %q (err %v), want %q", errs.CodeOf(err), err, bootstrap.CodeStartupFailed)
	}
}
