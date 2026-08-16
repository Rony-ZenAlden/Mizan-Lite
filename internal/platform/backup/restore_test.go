package backup_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/platform/backup"
)

// restoreFixture builds a live database, a service, and a backup to restore from.
func restoreFixture(t *testing.T) (*backup.Service, string, backup.Backup) {
	t.Helper()
	service, root, pool := fixture(t)

	taken, err := service.Take(context.Background(), backup.OnDemand)
	if err != nil {
		t.Fatalf("Take: %v", err)
	}

	// The shop carries on trading after the backup, so the live database and the backup DIFFER —
	// which is what makes a restore observable at all.
	if _, err = pool.Exec(`INSERT INTO things VALUES (2, 'sold after the backup')`); err != nil {
		t.Fatalf("trading on: %v", err)
	}
	return service, filepath.Join(root, "live.db"), taken
}

// ── preparing ───────────────────────────────────────────────────────────────────

// TestPreparingARestoreTouchesNothingAndSnapshotsWhatItWillReplace
//
// DoD criterion 4, and the step easiest to leave out. A restore that goes wrong having left
// nothing to go back to is worse than no restore feature, because the user chose it believing it
// was safe.
func TestPreparingARestoreTouchesNothingAndSnapshotsWhatItWillReplace(t *testing.T) {
	service, livePath, taken := restoreFixture(t)

	before, err := os.ReadFile(livePath)
	if err != nil {
		t.Fatalf("reading the live database: %v", err)
	}

	intent, err := service.Prepare(context.Background(), taken.Name, livePath, 36)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	// The live database is BYTE-IDENTICAL. Preparing is not destructive, and a shop that changes
	// its mind before restarting has lost nothing.
	after, err := os.ReadFile(livePath)
	if err != nil {
		t.Fatalf("re-reading the live database: %v", err)
	}
	if string(before) != string(after) {
		t.Error("preparing a restore modified the live database")
	}

	// The safety snapshot exists and is itself a verified backup.
	if intent.SafetyBackup == "" {
		t.Fatal("no safety snapshot was taken of the database about to be replaced")
	}
	safety, err := service.Find(intent.SafetyBackup)
	if err != nil {
		t.Fatalf("the safety snapshot is not listed: %v", err)
	}
	if safety.Manifest.Reason != backup.BeforeRestore {
		t.Errorf("the safety snapshot's reason is %q, want before_restore",
			safety.Manifest.Reason)
	}

	// It captured the CURRENT database, not the backup being restored: the row added after the
	// backup must be in it.
	snapshot, err := sql.Open("sqlite", "file:"+safety.Path)
	if err != nil {
		t.Fatalf("opening the safety snapshot: %v", err)
	}
	defer func() { _ = snapshot.Close() }()
	var rows int
	if err = snapshot.QueryRow(`SELECT COUNT(*) FROM things`).Scan(&rows); err != nil {
		t.Fatalf("reading the safety snapshot: %v", err)
	}
	if rows != 2 {
		t.Errorf("the safety snapshot holds %d rows, want 2 — it captured the backup rather "+
			"than the database being replaced", rows)
	}
}

// TestABackupFromANewerBuildIsRefused
//
// DoD criterion 3. The failure this prevents is specific: a shop that upgraded, took backups,
// then reinstalled an older version to work around something. Restoring would give them a
// database whose schema the code does not match, and the symptom would be a column that does not
// exist appearing hours later in an unrelated screen.
func TestABackupFromANewerBuildIsRefused(t *testing.T) {
	service, livePath, taken := restoreFixture(t)

	// The backup is at version 36; this binary only knows 30.
	_, err := service.Prepare(context.Background(), taken.Name, livePath, 30)
	if code := errs.CodeOf(err); code != backup.CodeTooNew {
		t.Fatalf("code = %q, want %q", code, backup.CodeTooNew)
	}

	// Nothing was staged, and no safety snapshot was taken for a restore that will not happen.
	if _, staged, _ := backup.PendingIntent(livePath); staged {
		t.Error("a refused restore left an intent staged")
	}
	listed, err := service.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, candidate := range listed {
		if candidate.Manifest.Reason == backup.BeforeRestore {
			t.Error("a refused restore still took a safety snapshot")
		}
	}

	// An OLDER backup is fine: the migration runner brings it forward on the next start, which
	// is its ordinary job.
	if _, err = service.Prepare(context.Background(), taken.Name, livePath, 41); err != nil {
		t.Errorf("a backup older than the binary was refused: %v", err)
	}
}

// ── applying ────────────────────────────────────────────────────────────────────

// TestApplyingAStagedRestoreReplacesTheDatabaseAndKeepsTheOriginal
func TestApplyingAStagedRestoreReplacesTheDatabaseAndKeepsTheOriginal(t *testing.T) {
	service, livePath, taken := restoreFixture(t)

	if _, err := service.Prepare(context.Background(), taken.Name, livePath, 36); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	intent, applied, err := backup.Apply(livePath)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !applied {
		t.Fatal("a staged restore was not applied")
	}
	if intent.From != taken.Name {
		t.Errorf("applied %q, want %q", intent.From, taken.Name)
	}

	// The live database is now the BACKUP's contents: one row, not two.
	live, err := sql.Open("sqlite", "file:"+livePath)
	if err != nil {
		t.Fatalf("opening the restored database: %v", err)
	}
	defer func() { _ = live.Close() }()
	var rows int
	if err = live.QueryRow(`SELECT COUNT(*) FROM things`).Scan(&rows); err != nil {
		t.Fatalf("reading the restored database: %v", err)
	}
	if rows != 1 {
		t.Errorf("the restored database holds %d rows, want 1 — the backup's contents", rows)
	}

	// The displaced original is KEPT. It and the safety snapshot are the two things a support
	// conversation has to work with, and deleting one to save space is a trade nobody asked for.
	if _, err = os.Stat(livePath + ".replaced"); err != nil {
		t.Errorf("the replaced database was deleted rather than kept: %v", err)
	}

	// The intent is cleared, so a second start does not restore again over a database that has
	// already been replaced.
	if _, staged, _ := backup.PendingIntent(livePath); staged {
		t.Error("the restore is still staged after being applied")
	}
}

// TestApplyingWithNothingStagedIsTheOrdinaryCase
//
// Every normal start. It must be cheap and silent, not an error somebody learns to ignore.
func TestApplyingWithNothingStagedIsTheOrdinaryCase(t *testing.T) {
	_, livePath, _ := restoreFixture(t)

	_, applied, err := backup.Apply(livePath)
	if err != nil {
		t.Fatalf("Apply with nothing staged: %v", err)
	}
	if applied {
		t.Error("a restore was applied when none was staged")
	}
}

// TestAStagedRestoreWhoseFileHasVanishedChangesNothing
//
// The staged copy removed by hand, or a failed copy. The live database must be untouched and the
// intent cleared, rather than failing on every start forever.
func TestAStagedRestoreWhoseFileHasVanishedChangesNothing(t *testing.T) {
	service, livePath, taken := restoreFixture(t)

	intent, err := service.Prepare(context.Background(), taken.Name, livePath, 36)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if err = os.Remove(intent.StagedPath); err != nil {
		t.Fatalf("removing the staged file: %v", err)
	}

	before, err := os.ReadFile(livePath)
	if err != nil {
		t.Fatalf("reading the live database: %v", err)
	}

	_, applied, err := backup.Apply(livePath)
	if err == nil {
		t.Error("a vanished staged restore was reported as applied without complaint")
	}
	if applied {
		t.Error("a vanished staged restore was applied")
	}

	after, err := os.ReadFile(livePath)
	if err != nil {
		t.Fatalf("re-reading the live database: %v", err)
	}
	if string(before) != string(after) {
		t.Error("a failed restore modified the live database")
	}
	// And it does not fail forever.
	if _, staged, _ := backup.PendingIntent(livePath); staged {
		t.Error("the broken intent was left in place, so every start will fail the same way")
	}
}

// TestCancellingAStagedRestoreLeavesNothingBehind
func TestCancellingAStagedRestoreLeavesNothingBehind(t *testing.T) {
	service, livePath, taken := restoreFixture(t)

	intent, err := service.Prepare(context.Background(), taken.Name, livePath, 36)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if err = backup.Cancel(livePath); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if _, statErr := os.Stat(intent.StagedPath); !os.IsNotExist(statErr) {
		t.Error("cancelling left the staged copy on disk")
	}
	if _, staged, _ := backup.PendingIntent(livePath); staged {
		t.Error("cancelling left the intent in place")
	}
	if _, _, err = backup.Apply(livePath); err != nil {
		t.Fatalf("Apply after Cancel: %v", err)
	}

	// The backup itself is untouched: staging COPIES, so an abandoned restore leaves everything
	// where it was.
	if _, err = service.Find(taken.Name); err != nil {
		t.Errorf("cancelling a restore consumed the backup: %v", err)
	}
}

// TestAFailedSwapLeavesTheOriginalIntact
//
// # DoD criterion 5, and the only path in this phase that cannot be reached by arranging files
//
// `Apply` renames the outgoing database aside, then renames the incoming one into place. If the
// SECOND rename fails, the live path holds nothing — and putting the original back is the
// difference between "the restore did not happen" and "the shop has no database".
//
// Those three lines are the most dangerous in the phase, and there is no way to make the second
// rename fail from outside the process. **A path that cannot be tested is a path that has never
// run**, so the package exposes the rename as a replaceable variable for exactly this.
func TestAFailedSwapLeavesTheOriginalIntact(t *testing.T) {
	service, livePath, taken := restoreFixture(t)

	if _, err := service.Prepare(context.Background(), taken.Name, livePath, 36); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	before, err := os.ReadFile(livePath)
	if err != nil {
		t.Fatalf("reading the live database: %v", err)
	}

	// The FIRST rename succeeds — the original is moved aside — and the second fails, which is
	// the window this test exists for.
	var calls int
	backup.SetRenameForTest(func(from, to string) error {
		calls++
		if calls == 2 {
			return errors.New("the disk went away")
		}
		return os.Rename(from, to)
	})
	t.Cleanup(backup.ResetRenameForTest)

	_, applied, err := backup.Apply(livePath)
	if err == nil {
		t.Error("a failed swap was reported as a success")
	}
	if applied {
		t.Error("a failed swap reported the restore as applied")
	}

	// The shop's database is EXACTLY where it was. This is the assertion the whole seam exists
	// for: a failed restore must be a restore that did not happen, never a missing database.
	after, err := os.ReadFile(livePath)
	if err != nil {
		t.Fatalf("the live database is gone after a failed swap: %v", err)
	}
	if string(before) != string(after) {
		t.Error("a failed swap changed the live database")
	}

	// And it is still usable, not merely present.
	pool, err := sql.Open("sqlite", "file:"+livePath)
	if err != nil {
		t.Fatalf("opening the database after a failed swap: %v", err)
	}
	defer func() { _ = pool.Close() }()
	var rows int
	if err = pool.QueryRow(`SELECT COUNT(*) FROM things`).Scan(&rows); err != nil {
		t.Fatalf("the database after a failed swap is unreadable: %v", err)
	}
	if rows != 2 {
		t.Errorf("%d rows after a failed swap, want the 2 that were there", rows)
	}
}
