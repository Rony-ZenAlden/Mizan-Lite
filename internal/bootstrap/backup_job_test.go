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

// TestABackupManifestRecordsTheBuildThatTookIt
//
// # A field added in Phase 9 that nothing set until Phase 10 went looking
//
// 9.1 D2 put `AppVersion` in the manifest "for a support conversation", and `bootstrap.Options`
// gained the field — and nothing ever assigned it. Every manifest carried an empty string where a
// build number belongs, which is exactly the value it was added to avoid.
//
// It is set from `buildinfo.Version` in `shell.go` now, stamped by the Makefile and both packaging
// scripts from `scripts/version.sh` — one source, four consumers.
//
// This test passes an explicit version rather than reading `buildinfo`, because in a test binary
// that variable is empty: asserting it were non-empty would assert the ldflags, which no test run
// has.
func TestABackupManifestRecordsTheBuildThatTookIt(t *testing.T) {
	app := bootWithVersion(t, "1.2.3-test")
	ctx := app.Context()

	taken, err := app.Backups.Take(ctx, backup.OnDemand)
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if taken.Manifest.AppVersion != "1.2.3-test" {
		t.Errorf("the manifest records app version %q, want the build's — a support conversation "+
			"starts by asking which version wrote a backup", taken.Manifest.AppVersion)
	}

	// And it survives to the listing, which is what a screen reads.
	listed, err := app.Backups.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 || listed[0].Manifest.AppVersion != "1.2.3-test" {
		t.Errorf("the listed backup records %+v", listed)
	}
}
