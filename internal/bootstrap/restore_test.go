package bootstrap_test

import (
	"context"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/modules/org"
	"github.com/mizan-erp/mizan/internal/platform/backup"
)

// TestARestoreSurvivesARestartAndTakesEffect
//
// # The test the whole step exists for, and it can only live here
//
// The package's own tests prove `Prepare` stages and `Apply` swaps, over a hand-built database
// nothing is holding. What they cannot show is the part that makes a restore work in a desktop
// application: the swap happening at STARTUP, before anything opens the file, and the restored
// database then being the one the application actually uses.
//
// **A seam is only proven by a caller.** This boots, provisions a company, backs up, changes the
// company, restores, restarts, and asks the running application what it sees.
func TestARestoreSurvivesARestartAndTakesEffect(t *testing.T) {
	directory := t.TempDir()

	// ── the first life ───────────────────────────────────────────────────────────
	app := bootIn(t, directory)
	ctx := app.Context()

	provisioned, err := app.Org.Provision(ctx, org.ProvisionInput{
		Company: org.CompanyInput{
			Code: "SHOP", Name: "The Corner Shop", CountryCode: "SA",
			FunctionalCurrency: "SYP",
		},
		Branch:               org.LocationInput{Code: "HQ", Name: "Head Office"},
		Warehouse:            org.LocationInput{Code: "WH1", Name: "Main"},
		FiscalYearStartMonth: time.January,
		FiscalYearStartYear:  2026,
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	taken, err := app.Backups.Take(ctx, backup.OnDemand)
	if err != nil {
		t.Fatalf("Take: %v", err)
	}

	// The shop carries on: a change made AFTER the backup, which restoring must undo.
	if _, err = app.DB.Writer(ctx).ExecContext(ctx,
		`UPDATE companies SET name = 'Renamed After The Backup' WHERE id = ?`,
		string(provisioned.CompanyID)); err != nil {
		t.Fatalf("renaming: %v", err)
	}

	intent, err := app.Backups.Prepare(ctx, taken.Name, app.Paths.DBFile,
		app.SchemaVersion())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if intent.SafetyBackup == "" {
		t.Fatal("no safety snapshot was taken")
	}

	// Preparing changed NOTHING. The application is still running on the renamed company, which
	// is what "a restore needs a restart" means from the user's side.
	if name := companyName(t, app, ctx); name != "Renamed After The Backup" {
		t.Errorf("preparing a restore changed the running application: company is %q", name)
	}

	if err = app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// ── the second life ──────────────────────────────────────────────────────────
	restarted := bootIn(t, directory)
	restartedCtx := restarted.Context()

	if name := companyName(t, restarted, restartedCtx); name != "The Corner Shop" {
		t.Errorf("after restarting, the company is %q — the restore did not take effect", name)
	}

	// The staged restore is consumed, so a THIRD start does not restore again over a database
	// that has already been replaced.
	if _, staged, _ := backup.PendingIntent(restarted.Paths.DBFile); staged {
		t.Error("the restore is still staged after being applied")
	}

	// And the safety snapshot survived the restore, which is the whole reason it was taken: it
	// is what a shop goes back to if the restore was a mistake.
	if _, err = restarted.Backups.Find(intent.SafetyBackup); err != nil {
		t.Errorf("the safety snapshot is gone after the restore: %v", err)
	}
}

// TestAnOrdinaryStartAppliesNoRestore
//
// Every start that is not a restore. The check must be cheap and silent — an error on every boot
// is one somebody learns to ignore, and then misses the real one.
func TestAnOrdinaryStartAppliesNoRestore(t *testing.T) {
	directory := t.TempDir()
	app := bootIn(t, directory)

	if _, staged, err := backup.PendingIntent(app.Paths.DBFile); err != nil || staged {
		t.Errorf("a fresh start has a restore staged: staged=%v err=%v", staged, err)
	}
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// Restarting twice more changes nothing.
	for range 2 {
		restarted := bootIn(t, directory)
		if restarted.DB == nil {
			t.Fatal("the restart produced no database")
		}
		if err := restarted.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
	}
}

// companyName reads the one field this test changes, straight from the table.
func companyName(t *testing.T, app *bootstrap.App, ctx context.Context) string {
	t.Helper()
	var name string
	if err := app.DB.Reader(ctx).QueryRowContext(ctx,
		`SELECT name FROM companies LIMIT 1`).Scan(&name); err != nil {
		t.Fatalf("reading the company name: %v", err)
	}
	return name
}
