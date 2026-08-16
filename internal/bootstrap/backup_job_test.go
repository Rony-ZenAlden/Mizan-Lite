package bootstrap_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/platform/backup"
)

// TestTheApplicationCanBackItselfUpAndTheSnapshotIsRealB
//
// # Why this lives here and not in platform/backup
//
// The package's own tests use a fake dialect over a hand-built database. They prove the mechanism
// and say nothing about whether the composition root wired it, whether the real SQLite dialect's
// online-backup statement works against the real schema, or whether the backup directory the
// paths layer chose exists.
//
// **A seam is only proven by a caller** — Phase 6's finding, and 7.6's number series were the
// worst case of ignoring it.
func TestTheApplicationCanBackItselfUpAndTheSnapshotIsReal(t *testing.T) {
	app := boot(t)
	ctx := app.Context()

	if app.Backups == nil {
		t.Fatal("the composition root did not build a backup service")
	}

	taken, err := app.Backups.Take(ctx, backup.OnDemand)
	if err != nil {
		t.Fatalf("Take: %v", err)
	}

	// The manifest read the REAL schema version, which is the number a restore compares against
	// the binary. Zero would mean it read nothing.
	if taken.Manifest.SchemaVersion == 0 {
		t.Error("the manifest records schema version 0 for a fully migrated database")
	}
	if taken.Manifest.Reason != backup.OnDemand {
		t.Errorf("reason = %q", taken.Manifest.Reason)
	}

	// And it verifies independently — the whole point of the mechanism, exercised against the
	// real dialect rather than a fake one.
	manifest, err := backup.Verify(ctx, taken.Path, app.DB.Dialect().IntegrityCheckStatement())
	if err != nil {
		t.Fatalf("the application's own backup does not verify: %v", err)
	}
	if manifest.SchemaVersion != taken.Manifest.SchemaVersion {
		t.Errorf("verification reads version %d and the manifest says %d",
			manifest.SchemaVersion, taken.Manifest.SchemaVersion)
	}

	listed, err := app.Backups.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 {
		t.Errorf("%d backups listed after taking one", len(listed))
	}
}

// TestTheScheduledBackupIsRegistered
//
// A job that is written and never registered is a backup a shop believes it has. The scheduler
// knows what it was given, and asking it is the only way to find out.
func TestTheScheduledBackupIsRegistered(t *testing.T) {
	app := boot(t)

	var found bool
	for _, def := range app.Scheduler.Registry().Defs() {
		if def.Key == bootstrap.KeyScheduledBackup {
			found = true
			if def.Timeout == 0 {
				t.Error("the backup job has no timeout, so a stuck copy would hang the scheduler")
			}
			if def.Schedule == nil {
				t.Error("the backup job has no schedule")
			}
		}
	}
	if !found {
		t.Errorf("%q is not registered — a shop would believe it had nightly backups and have "+
			"none", bootstrap.KeyScheduledBackup)
	}
}
