package backup

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// Stable codes for restoring.
const (
	CodeRestoreFailed = "backup.restore_failed"
	CodeTooNew        = "backup.newer_than_this_build"
	CodeNothingStaged = "backup.nothing_staged"
	CodeStagingFailed = "backup.staging_failed"
)

// Intent is a restore that has been prepared and is waiting for a restart.
//
// # Why a restore is two acts separated by a restart
//
// The application HOLDS the database. SQLite will let its file be replaced underneath an open
// connection and then behave in ways nobody can reason about — and even closing every pool first
// leaves a window in which a background job or an in-flight binding call touches a file that no
// longer exists.
//
// So the swap happens at STARTUP, before anything opens the database. `Prepare` does everything
// that can fail while the application is running, and `Apply` does the one thing that cannot be
// undone at the one moment when nothing is looking.
type Intent struct {
	// From is the backup being restored, by name.
	From string
	// StagedPath is the verified copy waiting to become the live database.
	StagedPath string
	// ReplacingVersion is the schema version of the database being replaced, recorded so a
	// support conversation can reconstruct what happened.
	ReplacingVersion int64
	// RestoringVersion is the staged file's schema version.
	RestoringVersion int64
	// SafetyBackup is the snapshot taken of what is about to be replaced.
	SafetyBackup string
	// PreparedAt is when, RFC3339 UTC.
	PreparedAt string
}

// intentFile is where an Intent waits. Beside the database, because a restore is a property of
// THIS database rather than of the machine.
func intentFile(dbPath string) string { return dbPath + ".restore.json" }

// stagedFile is where the verified copy waits.
func stagedFile(dbPath string) string { return dbPath + ".restoring" }

// Prepare validates a restore and stages it, without touching the live database.
//
// # The order is the whole design
//
//  1. VERIFY the incoming file. A file that is not a database, is corrupt, or has no migration
//     history is refused here — because discovering it after the swap is unrecoverable.
//  2. Compare versions. Older is fine: the migration runner will bring it forward on the next
//     start, which is its ordinary job. NEWER is refused, because this binary does not know how
//     to read it and migrating downwards is not something the runner can do.
//  3. SNAPSHOT the live database. The step easiest to leave out and worst to omit: a restore that
//     goes wrong having left nothing to go back to is worse than no restore feature at all,
//     because the user chose it believing it was safe.
//  4. Stage a copy and record the intent.
//
// Nothing here is destructive. A failure at any step leaves the live database exactly as it was.
func (s *Service) Prepare(
	ctx context.Context, name, livePath string, binaryVersion int64,
) (Intent, error) {
	candidate, err := s.Find(name)
	if err != nil {
		return Intent{}, err
	}

	// 1. Verify, before anything else looks at it.
	manifest, err := Verify(ctx, candidate.Path, s.db.Dialect().IntegrityCheckStatement())
	if err != nil {
		return Intent{}, err
	}

	// 2. A backup from a NEWER build cannot be read by this one.
	//
	// The failure this prevents is specific: a shop that upgraded, took backups, then reinstalled
	// an older version to work around something. Restoring would give them a database whose
	// schema the code does not match, and the symptom would be a column that does not exist
	// appearing hours later in an unrelated screen.
	if manifest.SchemaVersion > binaryVersion {
		return Intent{}, errs.Conflict(CodeTooNew,
			"that backup was taken by a newer version of Mizan; update before restoring it").
			WithParam("backup_version", itoa(manifest.SchemaVersion)).
			WithParam("this_version", itoa(binaryVersion))
	}

	// 3. Snapshot what is about to be replaced. THIS is the step that makes a restore safe.
	safety, err := s.Take(ctx, BeforeRestore)
	if err != nil {
		return Intent{}, errs.Wrap(err, errs.CategoryInternal, CodeRestoreFailed,
			"taking a safety snapshot before the restore")
	}

	// The live database's version, for the record. Read rather than assumed: the binary's target
	// version and what the file actually holds can differ if a migration was interrupted.
	liveManifest, err := Verify(ctx, livePath, s.db.Dialect().IntegrityCheckStatement())
	if err != nil {
		return Intent{}, errs.Wrap(err, errs.CategoryInternal, CodeRestoreFailed,
			"reading the current database's version")
	}

	// 4. Stage. A COPY, not a move: the backup stays in the backup directory, so a restore that
	// is abandoned before restart leaves everything where it was.
	staged := stagedFile(livePath)
	if err = copyFile(candidate.Path, staged); err != nil {
		return Intent{}, err
	}

	intent := Intent{
		From: name, StagedPath: staged,
		ReplacingVersion: liveManifest.SchemaVersion,
		RestoringVersion: manifest.SchemaVersion,
		SafetyBackup:     safety.Name,
		PreparedAt:       s.clk.Now().UTC().Format(rfc3339),
	}
	if err = writeIntent(livePath, intent); err != nil {
		_ = os.Remove(staged)
		return Intent{}, err
	}
	return intent, nil
}

// PendingIntent reports a staged restore, if there is one.
func PendingIntent(livePath string) (Intent, bool, error) {
	encoded, err := os.ReadFile(intentFile(livePath))
	if errors.Is(err, os.ErrNotExist) {
		return Intent{}, false, nil
	}
	if err != nil {
		return Intent{}, false, errs.Wrap(err, errs.CategoryInternal, CodeRestoreFailed,
			"reading the staged restore")
	}
	var intent Intent
	if err = json.Unmarshal(encoded, &intent); err != nil {
		return Intent{}, false, errs.Wrap(err, errs.CategoryInternal, CodeRestoreFailed,
			"reading the staged restore")
	}
	return intent, true, nil
}

// Cancel discards a staged restore.
func Cancel(livePath string) error {
	intent, staged, err := PendingIntent(livePath)
	if err != nil {
		return err
	}
	if !staged {
		return errs.NotFound(CodeNothingStaged, "no restore is waiting")
	}
	_ = os.Remove(intent.StagedPath)
	if err = os.Remove(intentFile(livePath)); err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeRestoreFailed,
			"discarding the staged restore")
	}
	return nil
}

// Apply performs the swap. Called at STARTUP, before anything opens the database.
//
// # Why the original is renamed rather than deleted
//
// The staged file is moved into place and the outgoing one is moved aside first, so that a
// failure between the two leaves SOMETHING at the live path. A delete-then-rename would have a
// window in which the live path holds nothing at all, and a crash there costs the shop
// everything.
//
// The displaced original is left on disk under a `.replaced` suffix. It is not deleted, because
// the safety snapshot and it are the two things a support conversation has to work with, and
// deleting one to save space is a trade nobody asked for.
//
// Returns false when there was nothing to do, which is the ordinary case on every normal start.
func Apply(livePath string) (Intent, bool, error) {
	intent, staged, err := PendingIntent(livePath)
	if err != nil || !staged {
		return Intent{}, false, err
	}

	if _, err = os.Stat(intent.StagedPath); err != nil {
		// The staged file is gone — removed by hand, or a failed copy. The intent is cleared
		// rather than left to fail on every start, and the live database is untouched.
		_ = os.Remove(intentFile(livePath))
		return Intent{}, false, errs.Conflict(CodeStagingFailed,
			"the staged restore has disappeared; nothing was changed")
	}

	displaced := livePath + ".replaced"
	_ = os.Remove(displaced)

	// Move the outgoing file aside FIRST. A missing live file at this point is not an error: a
	// restore onto a fresh install has nothing to displace.
	if err = os.Rename(livePath, displaced); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Intent{}, false, errs.Wrap(err, errs.CategoryInternal, CodeRestoreFailed,
			"moving the current database aside")
	}

	if err = os.Rename(intent.StagedPath, livePath); err != nil {
		// Put it back. The shop's database is where it was, and the restore simply did not
		// happen — which is the only acceptable outcome of a failed swap.
		_ = os.Rename(displaced, livePath)
		return Intent{}, false, errs.Wrap(err, errs.CategoryInternal, CodeRestoreFailed,
			"putting the restored database in place")
	}

	// SQLite's sidecars belong to the database that was replaced, and leaving them would let a
	// stale write-ahead log be replayed over the restored file.
	for _, sidecar := range []string{"-wal", "-shm"} {
		_ = os.Remove(livePath + sidecar)
	}

	// # Why a failure to clear the intent is not a failed restore
	//
	// The swap SUCCEEDED. Returning an error now would report a restore that did not happen, and
	// an operator would go looking for a database that has already been replaced.
	//
	// The next start is safe either way: it finds the intent, finds the staged file gone —
	// because it was renamed into place — and clears the intent without touching the live
	// database. That path has its own test.
	_ = os.Remove(intentFile(livePath))
	return intent, true, nil
}

// ── files ───────────────────────────────────────────────────────────────────────

const rfc3339 = "2006-01-02T15:04:05Z07:00"

func writeIntent(livePath string, intent Intent) error {
	encoded, err := json.MarshalIndent(intent, "", "  ")
	if err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeStagingFailed, "encoding the restore")
	}
	if err = os.WriteFile(intentFile(livePath), encoded, 0o600); err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeStagingFailed, "staging the restore")
	}
	return nil
}

// copyFile copies through a temporary file and renames, so a partial copy is never mistaken for a
// staged one.
func copyFile(from, to string) error {
	source, err := os.ReadFile(from)
	if err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeStagingFailed, "reading the backup")
	}
	temporary := to + ".partial"
	if err = os.WriteFile(temporary, source, 0o600); err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeStagingFailed, "staging the backup")
	}
	if err = os.Rename(temporary, to); err != nil {
		_ = os.Remove(temporary)
		return errs.Wrap(err, errs.CategoryInternal, CodeStagingFailed, "staging the backup")
	}
	return nil
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}
