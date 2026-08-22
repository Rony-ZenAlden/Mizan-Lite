package bootstrap_test

import (
	"context"
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

// TestClosingTheAppLeavesASnapshotNobodyHadToAskFor
//
// # The guarantee, stated as a shopkeeper would
//
// Nobody presses "Export Backup". They open Mizan in the morning, serve customers all day, and
// close the window. This asserts that closing the window is enough.
//
// It is a bootstrap test rather than a platform one because everything that could be wrong is in
// the WIRING: whether the step is registered, whether it runs before the database closes, and
// whether it runs against the real SQLite dialect and the real backup directory. TakeIfDue's own
// behaviour is drilled in platform/backup.
func TestClosingTheAppLeavesASnapshotNobodyHadToAskFor(t *testing.T) {
	dir := t.TempDir()
	app := bootIn(t, dir)

	before, err := app.Backups.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if err = app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	after, err := app.Backups.List()
	if err != nil {
		t.Fatalf("List after shutdown: %v", err)
	}
	if len(after) <= len(before) {
		t.Fatalf("closing the app took no snapshot (%d before, %d after)", len(before), len(after))
	}

	var closed *backup.Backup
	for i := range after {
		if after[i].Manifest.Reason == backup.OnClose {
			closed = &after[i]
			break
		}
	}
	if closed == nil {
		t.Fatal("a snapshot was written but not as an on-close one")
	}

	// It must be a RESTORABLE database, not a file of the right size. A backup nobody has opened
	// is a file somebody will discover is unreadable on the day they need it.
	manifest, err := backup.Verify(context.Background(), closed.Path, "PRAGMA integrity_check")
	if err != nil {
		t.Fatalf("the close-time snapshot does not verify: %v", err)
	}
	if manifest.SchemaVersion != app.SchemaVersion() {
		t.Errorf("snapshot schema version = %d, live database is at %d",
			manifest.SchemaVersion, app.SchemaVersion())
	}
}

// TestTheSnapshotIsTakenWhileTheDatabaseIsStillOpen
//
// Ordering, which is the one thing a "did a file appear" test cannot see. `Take` runs VACUUM INTO
// on the writer pool; if the step were registered after the database closes, it would fail every
// time — and it would fail QUIETLY, because backupOnClose deliberately logs rather than returns.
//
// So the drill is on the ORDER, read from the registered steps, not on the outcome.
func TestTheSnapshotIsTakenWhileTheDatabaseIsStillOpen(t *testing.T) {
	order := bootstrap.ShutdownStepNamesForTest(bootIn(t, t.TempDir()))

	position := map[string]int{}
	for i, name := range order {
		position[name] = i
	}
	for _, name := range []string{"scheduler", "outbox", "backup", "database"} {
		if _, ok := position[name]; !ok {
			t.Fatalf("no %q step in the shutdown sequence: %v", name, order)
		}
	}
	if position["backup"] > position["database"] {
		t.Errorf("the backup runs after the database closes, so it can never succeed: %v", order)
	}
	if position["backup"] < position["scheduler"] {
		t.Errorf("the backup runs before the scheduler stops, so a scheduled backup can be "+
			"running at the same time: %v", order)
	}
	if position["backup"] < position["outbox"] {
		t.Errorf("the backup runs before the outbox's final pass, so the snapshot misses it: %v",
			order)
	}
}
