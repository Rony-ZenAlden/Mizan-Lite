package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite" // the backup verifier opens a standalone connection

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// freeSpaceMargin is headroom demanded beyond the computed requirement, so a migration
// never fills the disk it is running on. A machine with under this much free space has
// bigger problems than a schema update.
const freeSpaceMargin = 64 << 20 // 64 MiB

// integrityCheck verifies physical database integrity before anything else touches it.
//
// A database that is ALREADY corrupt must never be migrated: migrating it makes things
// worse, and backing it up produces a backup of corruption — which is worse still,
// because it looks like a safety net.
//
// stmt comes from the dialect. An empty stmt means the engine offers no integrity check
// (PostgreSQL, MySQL), and the step is skipped rather than faked.
func integrityCheck(ctx context.Context, db *sql.DB, stmt string) error {
	if stmt == "" {
		return nil
	}

	rows, err := db.QueryContext(ctx, stmt)
	if err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeCorruptDatabase, "running integrity check")
	}
	defer rows.Close()

	var problems []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return errs.Wrap(err, errs.CategoryInternal, CodeCorruptDatabase, "reading integrity check")
		}
		if !strings.EqualFold(strings.TrimSpace(line), "ok") {
			problems = append(problems, line)
		}
	}
	if err := rows.Err(); err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeCorruptDatabase, "reading integrity check")
	}
	if len(problems) > 0 {
		return errs.Conflict(CodeCorruptDatabase,
			"database integrity check failed: "+strings.Join(problems, "; "))
	}
	return nil
}

// checkFreeSpace refuses to start when the disk cannot hold what the safety protocol
// will write. A backup that fails halfway because the disk filled is the worst possible
// outcome of an "update", and it is trivially preventable.
//
// Two volumes are checked, because the backup directory and the database need not share
// one:
//
//	backup volume:   2 × dbSize + margin  (the snapshot, plus room to not run to zero)
//	database volume: 1 × dbSize + margin  (restore writes a temp copy beside the live file)
func (r *Runner) checkFreeSpace() error {
	size, err := r.databaseSize()
	if err != nil {
		// Cannot size the database: report rather than silently skipping a safety gate.
		return errs.Wrap(err, errs.CategoryInternal, CodeInsufficientDisk,
			"determining database size")
	}

	targets := []struct {
		dir      string
		required uint64
		what     string
	}{
		{filepath.Dir(r.dbPath), size + freeSpaceMargin, "the database"},
	}
	if r.backupDir != "" {
		targets = append(targets, struct {
			dir      string
			required uint64
			what     string
		}{r.backupDir, 2*size + freeSpaceMargin, "the pre-migration backup"})
	}

	for _, t := range targets {
		avail, err := r.freeSpace(nearestExistingDir(t.dir))
		if err != nil {
			return errs.Wrap(err, errs.CategoryInternal, CodeInsufficientDisk,
				"checking free space for "+t.what)
		}
		if avail < t.required {
			return errs.Conflict(CodeInsufficientDisk, fmt.Sprintf(
				"not enough free disk space for %s: %d MB available, %d MB required — "+
					"free some space and restart Mizan",
				t.what, avail>>20, t.required>>20))
		}
	}
	return nil
}

// databaseSize is the live file plus its write-ahead log, which is the volume a snapshot
// must actually accommodate.
func (r *Runner) databaseSize() (uint64, error) {
	fi, err := os.Stat(r.dbPath)
	if err != nil {
		return 0, err
	}
	total := fi.Size()
	if wal, err := os.Stat(r.dbPath + "-wal"); err == nil {
		total += wal.Size()
	}
	if total < 0 {
		return 0, nil
	}
	return uint64(total), nil
}

// nearestExistingDir walks up until it finds a directory that exists, because the
// backup directory is created later and free space is a property of the volume.
func nearestExistingDir(dir string) string {
	for {
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir { // reached the root
			return dir
		}
		dir = parent
	}
}

// backup writes a consistent snapshot via the dialect's online-backup statement, then
// verifies it before it is trusted (§4.2) — an unverified backup is a rumour.
func (r *Runner) backup(ctx context.Context, fromV, toV int64) (string, error) {
	if err := os.MkdirAll(r.backupDir, 0o750); err != nil {
		return "", errs.Wrap(err, errs.CategoryInternal, CodeBackupFailed, "creating backup directory")
	}
	name := fmt.Sprintf("pre-migration-%s-v%d-to-v%d.db",
		r.clock.Now().UTC().Format("20060102T150405Z"), fromV, toV)
	dest := filepath.Join(r.backupDir, name)

	stmt := r.db.Dialect().OnlineBackupStatement(dest)
	if stmt == "" {
		// The engine cannot snapshot itself. Proceeding would mean migrating with no
		// safety net while reporting that one exists.
		return "", errs.Internal(CodeBackupFailed, fmt.Sprintf(
			"dialect %q provides no online backup; refusing to migrate without a safety snapshot",
			r.db.Dialect().Name()))
	}

	// The statement refuses to overwrite; a stale file from an aborted run would block us.
	_ = os.Remove(dest)

	if _, err := r.db.WriterPool().ExecContext(ctx, stmt); err != nil {
		return "", errs.Wrap(err, errs.CategoryInternal, CodeBackupFailed, "writing backup")
	}
	if err := verifyBackup(ctx, dest, r.db.Dialect().IntegrityCheckStatement()); err != nil {
		return "", err
	}
	return dest, nil
}

// verifyBackup opens the backup independently, integrity-checks it, and confirms the
// migration history reads back. Anything less is trusting a file we never opened.
func verifyBackup(ctx context.Context, path, integrityStmt string) error {
	db, err := sql.Open("sqlite", fileDSN(path))
	if err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeBackupFailed, "opening backup for verification")
	}
	defer db.Close()

	if err := integrityCheck(ctx, db, integrityStmt); err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeBackupFailed, "backup failed integrity check")
	}
	// The history table must read back; if it cannot, the snapshot is not usable.
	var n int64
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM schema_migrations").Scan(&n); err != nil {
		// A fresh database that has never migrated has no history table yet; that is fine.
		if !isMissingTable(err) {
			return errs.Wrap(err, errs.CategoryInternal, CodeBackupFailed, "backup history unreadable")
		}
	}
	return nil
}

// fileDSN builds a file: DSN with the path properly encoded.
//
// An earlier version escaped only spaces, which silently broke on '?', '#', and '%' —
// all legal in macOS and Windows paths, and '?' in particular would have been read as
// the start of the DSN's query string.
func fileDSN(path string) string {
	u := url.URL{Scheme: "file", Opaque: (&url.URL{Path: path}).EscapedPath()}
	return u.String()
}

// restore replaces the live database with a verified backup.
//
// Called when a migration fails. The migration's own transaction has already rolled
// back, so this is belt-and-braces — the failure mode we cannot rule out is the one we
// did not predict, and this is a customer's business data.
//
// POSTCONDITION: both pools are CLOSED on return, success or failure. The Store is
// unusable afterwards and the caller must reopen the database or exit; continuing to use
// it would operate on a closed handle. The current (failed) database is preserved
// alongside for diagnosis, never deleted.
func (r *Runner) restore(backupPath string) (failedCopy string, err error) {
	live := r.dbPath
	failedCopy = live + fmt.Sprintf(".failed-%s", r.clock.Now().UTC().Format("20060102T150405Z"))

	// Close pools so the files are not in use.
	_ = r.db.Close()

	// Preserve the failed database (rename, not delete).
	if renameErr := os.Rename(live, failedCopy); renameErr != nil {
		failedCopy = ""
	}
	// WAL/SHM sidecars belong to the old file; they must not be applied to the restored one.
	_ = os.Remove(live + "-wal")
	_ = os.Remove(live + "-shm")

	// Copy the backup into place atomically: write a temp file, then rename.
	tmp := live + ".restoring"
	if copyErr := copyFile(backupPath, tmp); copyErr != nil {
		return failedCopy, errs.Wrap(copyErr, errs.CategoryInternal, CodeRestoreFailed,
			"copying backup into place")
	}
	if renameErr := os.Rename(tmp, live); renameErr != nil {
		return failedCopy, errs.Wrap(renameErr, errs.CategoryInternal, CodeRestoreFailed,
			"activating restored database")
	}
	return failedCopy, nil
}

func copyFile(src, dst string) error {
	in, err := os.ReadFile(src) //nolint:gosec // internal, trusted paths
	if err != nil {
		return err
	}
	return os.WriteFile(dst, in, 0o600)
}

// pruneBackups keeps the newest `keep` pre-migration backups. Failed-run artifacts are
// never pruned here — they are the diagnostic record of an incident.
func (r *Runner) pruneBackups(keep int) {
	if keep <= 0 || r.backupDir == "" {
		return
	}
	entries, err := os.ReadDir(r.backupDir)
	if err != nil {
		return
	}
	var backups []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "pre-migration-") && strings.HasSuffix(e.Name(), ".db") {
			backups = append(backups, e.Name())
		}
	}
	if len(backups) <= keep {
		return
	}
	// Names embed a sortable UTC timestamp, so lexical order is chronological.
	sort.Strings(backups)
	for _, name := range backups[:len(backups)-keep] {
		_ = os.Remove(filepath.Join(r.backupDir, name))
	}
}
