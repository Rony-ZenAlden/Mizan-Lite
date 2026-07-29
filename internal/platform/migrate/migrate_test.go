// Tests live in package migrate (not migrate_test) so the failure drills can inject a
// fake free-space probe. The drills are the point of this suite: a migration runner that
// works on the happy path is not what this step exists to buy.
package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/migrations"
)

// ── fixtures ────────────────────────────────────────────────────────────────────

// test0001 is the minimum a migration set must create for the runner to function: the
// history table, the advisory lock row, and one business table whose survival proves the
// restore drill actually protected data.
const test0001 = `
CREATE TABLE schema_migrations (
  version     BIGINT       NOT NULL PRIMARY KEY,
  name        VARCHAR(200) NOT NULL,
  checksum    CHAR(64)     NOT NULL,
  applied_at  CHAR(24)     NOT NULL,
  duration_ms BIGINT       NOT NULL DEFAULT 0
);
CREATE TABLE schema_lock (
  lock_id   INTEGER      NOT NULL PRIMARY KEY,
  is_locked SMALLINT     NOT NULL DEFAULT 0,
  locked_at CHAR(24),
  locked_by VARCHAR(200)
);
INSERT INTO schema_lock (lock_id, is_locked) VALUES (1, 0);
CREATE TABLE invoices (id CHAR(36) NOT NULL PRIMARY KEY, total BIGINT NOT NULL);
`

const test0002 = `CREATE TABLE payments (id CHAR(36) NOT NULL PRIMARY KEY);`

// test0002Broken fails at execution time, not load time: valid-looking SQL referencing a
// table that does not exist. This is the realistic shape of a migration bug.
const test0002Broken = `INSERT INTO table_that_does_not_exist (id) VALUES ('x');`

func mapFS(files map[string]string) fs.FS {
	m := fstest.MapFS{}
	for name, body := range files {
		m[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return m
}

func oneMigration() fs.FS { return mapFS(map[string]string{"0001_init.sql": test0001}) }

// ── helpers ─────────────────────────────────────────────────────────────────────

func openStore(t *testing.T, path string) *database.Store {
	t.Helper()
	st, err := database.Open(database.Config{Path: path})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	// Close is idempotent enough for cleanup; drills close it themselves via restore.
	t.Cleanup(func() { _ = st.Close() })
	return st
}

type runnerOpt func(*Options)

func withBackups(dir string) runnerOpt {
	return func(o *Options) { o.BackupDir = dir; o.SkipBackup = false }
}

func withProgress(sink *[]Progress) runnerOpt {
	return func(o *Options) {
		o.Progress = func(p Progress) { *sink = append(*sink, p) }
	}
}

func newRunner(t *testing.T, st *database.Store, dbPath string, fsys fs.FS, opts ...runnerOpt) *Runner {
	t.Helper()
	o := Options{
		FS:         fsys,
		DBPath:     dbPath,
		Clock:      clock.NewFixed(time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)),
		SkipBackup: true, // drills opt back in; most tests should not touch the disk
	}
	for _, fn := range opts {
		fn(&o)
	}
	r, err := New(st, o)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	return r
}

// tempDB gives a database path inside a per-test directory.
func tempDB(t *testing.T) (dir, dbPath string) {
	t.Helper()
	dir = t.TempDir()
	return dir, filepath.Join(dir, "mizan.db")
}

func tableExists(t *testing.T, path, table string) bool {
	t.Helper()
	db, err := sql.Open("sqlite", fileDSN(path))
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer db.Close()

	var name string
	err = db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&name)
	switch {
	case err == sql.ErrNoRows:
		return false
	case err != nil:
		t.Fatalf("table lookup %s: %v", table, err)
	}
	return true
}

func assertCode(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %q, got nil", want)
	}
	if got := errs.CodeOf(err); got != want {
		t.Fatalf("error code = %q, want %q (err: %v)", got, want, err)
	}
}

// ── loader unit tests ───────────────────────────────────────────────────────────

func TestLoadOrdersByVersionNotFilename(t *testing.T) {
	// "0010" sorts before "0002" lexically; ordering must be numeric.
	got, err := Load(mapFS(map[string]string{
		"0010_ten.sql": "SELECT 1;",
		"0002_two.sql": "SELECT 1;",
		"0001_one.sql": "SELECT 1;",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var versions []int64
	for _, m := range got {
		versions = append(versions, m.Version)
	}
	if fmt.Sprint(versions) != "[1 2 10]" {
		t.Fatalf("order = %v, want [1 2 10]", versions)
	}
}

func TestLoadRejectsBadFileNames(t *testing.T) {
	for _, name := range []string{"init.sql", "_init.sql", "abc_init.sql", "0_init.sql", "-1_x.sql"} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(mapFS(map[string]string{name: "SELECT 1;"}))
			assertCode(t, err, CodeBadFileName)
		})
	}
}

func TestLoadRejectsDuplicateVersion(t *testing.T) {
	_, err := Load(mapFS(map[string]string{
		"0001_platform.sql": "SELECT 1;",
		"0001_currency.sql": "SELECT 1;",
	}))
	assertCode(t, err, CodeDuplicateVersion)
}

func TestLoadRejectsForbiddenStatements(t *testing.T) {
	cases := map[string]string{
		"pragma on its own line": "PRAGMA journal_mode = DELETE;",
		"vacuum":                 "VACUUM;",
		"attach":                 "ATTACH DATABASE 'other.db' AS other;",
		"lowercase":              "pragma foreign_keys = 0;",
		"indented":               "   \n\t PRAGMA synchronous = OFF;",
		// The regression: a forbidden statement sharing a line with a legal one. The
		// original line-prefix scan accepted this.
		"second statement on the same line": "CREATE TABLE t (id TEXT); PRAGMA journal_mode = DELETE;",
		"after a block comment":             "/* set up */ VACUUM;",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(mapFS(map[string]string{"0001_x.sql": body}))
			assertCode(t, err, CodeForbiddenStatement)
		})
	}
}

func TestLoadAcceptsLegitimateSQLThatMentionsForbiddenWords(t *testing.T) {
	// A forbidden word must only matter as a leading keyword. These are all valid.
	cases := map[string]string{
		"check constraint":          "CREATE TABLE t (f SMALLINT, CONSTRAINT ck CHECK (f IN (0,1)));",
		"word inside an identifier": "CREATE TABLE vacuum_log (id TEXT);",
		"word in a comment":         "-- remember to VACUUM later\nCREATE TABLE t (id TEXT);",
		"word in a string literal":  "CREATE TABLE t (id TEXT DEFAULT 'PRAGMA; VACUUM');",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(mapFS(map[string]string{"0001_x.sql": body})); err != nil {
				t.Fatalf("Load rejected valid SQL: %v", err)
			}
		})
	}
}

func TestSplitStatementsHandlesCommentsAndLiterals(t *testing.T) {
	got := splitStatements(`
-- a line comment with ; inside
CREATE TABLE a (x TEXT DEFAULT 'semi;colon');
/* block ; comment */
CREATE TABLE b (y TEXT); -- trailing
CREATE TABLE c (z TEXT)`)

	if len(got) != 3 {
		t.Fatalf("got %d statements, want 3: %#v", len(got), got)
	}
	if !strings.Contains(got[0], "'semi;colon'") {
		t.Errorf("string literal was split: %q", got[0])
	}
	for i, want := range []string{"a", "b", "c"} {
		if !strings.Contains(got[i], "TABLE "+want) {
			t.Errorf("statement %d = %q, want table %s", i, got[i], want)
		}
	}
}

func TestChecksumIsContentAddressed(t *testing.T) {
	a, err := Load(mapFS(map[string]string{"0001_x.sql": "SELECT 1;"}))
	if err != nil {
		t.Fatal(err)
	}
	same, _ := Load(mapFS(map[string]string{"0001_x.sql": "SELECT 1;"}))
	diff, _ := Load(mapFS(map[string]string{"0001_x.sql": "SELECT 2;"}))

	if a[0].Checksum != same[0].Checksum {
		t.Error("identical content produced different checksums")
	}
	if a[0].Checksum == diff[0].Checksum {
		t.Error("different content produced the same checksum")
	}
	if len(a[0].Checksum) != 64 {
		t.Errorf("checksum length = %d, want 64 (SHA-256 hex)", len(a[0].Checksum))
	}
}

// ── happy path ──────────────────────────────────────────────────────────────────

func TestUpAppliesRealPlatformSchema(t *testing.T) {
	// The shipped 0001_platform.sql, through the embedded FS the binary actually uses.
	_, dbPath := tempDB(t)
	st := openStore(t, dbPath)
	r := newRunner(t, st, dbPath, migrations.SQLite())

	res, err := r.Up(context.Background())
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	if res.Applied != 1 || res.ToVersion != 1 {
		t.Fatalf("Applied=%d ToVersion=%d, want 1 and 1", res.Applied, res.ToVersion)
	}

	// All eight platform tables from design §7, plus the two bookkeeping ones.
	for _, table := range []string{
		"schema_migrations", "schema_lock", "settings", "feature_flags",
		"outbox_events", "jobs", "job_runs", "translations", "number_series",
	} {
		if !tableExists(t, dbPath, table) {
			t.Errorf("table %q was not created", table)
		}
	}
}

func TestUpIsIdempotent(t *testing.T) {
	_, dbPath := tempDB(t)
	st := openStore(t, dbPath)
	fsys := mapFS(map[string]string{"0001_init.sql": test0001, "0002_more.sql": test0002})

	first, err := newRunner(t, st, dbPath, fsys).Up(context.Background())
	if err != nil {
		t.Fatalf("first Up: %v", err)
	}
	if first.Applied != 2 {
		t.Fatalf("first Up applied %d, want 2", first.Applied)
	}

	second, err := newRunner(t, st, dbPath, fsys).Up(context.Background())
	if err != nil {
		t.Fatalf("second Up: %v", err)
	}
	if second.Applied != 0 {
		t.Errorf("second Up applied %d migrations, want 0 (fast path)", second.Applied)
	}
	if second.BackupPath != "" {
		t.Errorf("fast path took a backup (%q); it must cost nothing", second.BackupPath)
	}
}

func TestStatusReportsPendingAccurately(t *testing.T) {
	ctx := context.Background()
	_, dbPath := tempDB(t)
	st := openStore(t, dbPath)
	fsys := mapFS(map[string]string{"0001_init.sql": test0001, "0002_more.sql": test0002})

	r := newRunner(t, st, dbPath, fsys)
	before, err := r.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if before.CurrentVersion != 0 || before.TargetVersion != 2 || len(before.Pending) != 2 {
		t.Fatalf("fresh status = %+v, want current 0 target 2 pending 2", before)
	}

	if _, upErr := r.Up(ctx); upErr != nil {
		t.Fatalf("Up: %v", upErr)
	}

	after, err := newRunner(t, st, dbPath, fsys).Status(ctx)
	if err != nil {
		t.Fatalf("Status after: %v", err)
	}
	if after.CurrentVersion != 2 || len(after.Pending) != 0 {
		t.Fatalf("migrated status = %+v, want current 2 pending 0", after)
	}
}

func TestMigrationHistoryRecordsMetadata(t *testing.T) {
	ctx := context.Background()
	_, dbPath := tempDB(t)
	st := openStore(t, dbPath)
	r := newRunner(t, st, dbPath, oneMigration())
	if _, err := r.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}

	var name, checksum, appliedAt string
	var durationMS int64
	err := st.WriterPool().QueryRowContext(ctx,
		`SELECT name, checksum, applied_at, duration_ms FROM schema_migrations WHERE version = 1`).
		Scan(&name, &checksum, &appliedAt, &durationMS)
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if name != "init" {
		t.Errorf("name = %q, want %q", name, "init")
	}
	if len(checksum) != 64 {
		t.Errorf("checksum = %q, want 64 hex chars", checksum)
	}
	// CHAR(24) ISO-8601 UTC per the portable type contract (§8.1).
	if len(appliedAt) != 24 || !strings.HasSuffix(appliedAt, "Z") {
		t.Errorf("applied_at = %q, want a 24-char ISO-8601 UTC timestamp", appliedAt)
	}
	if durationMS < 0 {
		t.Errorf("duration_ms = %d, want >= 0", durationMS)
	}
}

// ── DRILL (a): injected failure must not cost data ──────────────────────────────

func TestDrillInjectedFailureRestoresData(t *testing.T) {
	ctx := context.Background()
	dir, dbPath := tempDB(t)
	backupDir := filepath.Join(dir, "backups")

	// Arrange: a database at v1 holding real business data.
	st := openStore(t, dbPath)
	if _, err := newRunner(t, st, dbPath, oneMigration()).Up(ctx); err != nil {
		t.Fatalf("baseline Up: %v", err)
	}
	if _, err := st.WriterPool().ExecContext(ctx,
		`INSERT INTO invoices (id, total) VALUES ('inv-1', 12345)`); err != nil {
		t.Fatalf("seed invoice: %v", err)
	}

	// Act: a second migration that fails at execution time.
	broken := mapFS(map[string]string{
		"0001_init.sql":   test0001,
		"0002_broken.sql": test0002Broken,
	})
	r := newRunner(t, st, dbPath, broken, withBackups(backupDir))
	_, err := r.Up(ctx)

	// Assert: it failed, and said which migration and where the backup is.
	assertCode(t, err, CodeMigrationFailed)
	if !strings.Contains(err.Error(), "0002_broken.sql") {
		t.Errorf("error does not name the failed migration: %v", err)
	}
	if !strings.Contains(err.Error(), backupDir) {
		t.Errorf("error does not name the backup location: %v", err)
	}

	// Assert: the backup exists and is a valid database.
	backups, _ := filepath.Glob(filepath.Join(backupDir, "pre-migration-*.db"))
	if len(backups) != 1 {
		t.Fatalf("found %d backups, want 1", len(backups))
	}
	if vErr := verifyBackup(ctx, backups[0], "PRAGMA integrity_check"); vErr != nil {
		t.Errorf("backup does not verify: %v", vErr)
	}

	// Assert: the failed database was retained for diagnosis, never deleted.
	failed, _ := filepath.Glob(dbPath + ".failed-*")
	if len(failed) != 1 {
		t.Errorf("found %d retained failed databases, want 1", len(failed))
	}

	// Assert — the promise of this whole step: the data is still there, at v1.
	reopened := openStore(t, dbPath)
	var total int64
	if qErr := reopened.WriterPool().QueryRowContext(ctx,
		`SELECT total FROM invoices WHERE id = 'inv-1'`).Scan(&total); qErr != nil {
		t.Fatalf("invoice did not survive the failed migration: %v", qErr)
	}
	if total != 12345 {
		t.Errorf("invoice total = %d, want 12345", total)
	}
	if tableExists(t, dbPath, "payments") {
		t.Error("a table from the failed migration set survived the restore")
	}

	status, err := newRunner(t, reopened, dbPath, broken).Status(ctx)
	if err != nil {
		t.Fatalf("Status after restore: %v", err)
	}
	if status.CurrentVersion != 1 {
		t.Errorf("version after restore = %d, want 1", status.CurrentVersion)
	}
}

// ── DRILL (b): an edited migration must fail loudly ─────────────────────────────

func TestDrillChecksumTamperIsHardFailure(t *testing.T) {
	ctx := context.Background()
	_, dbPath := tempDB(t)
	st := openStore(t, dbPath)

	if _, err := newRunner(t, st, dbPath, oneMigration()).Up(ctx); err != nil {
		t.Fatalf("baseline Up: %v", err)
	}

	// The same version, edited after shipping — the silent-divergence scenario.
	tampered := mapFS(map[string]string{
		"0001_init.sql": test0001 + "\n-- an innocent-looking edit\n",
	})
	_, err := newRunner(t, st, dbPath, tampered).Up(ctx)

	assertCode(t, err, CodeChecksumMismatch)
	if !strings.Contains(err.Error(), "0001_init.sql") {
		t.Errorf("error does not name the changed file: %v", err)
	}
}

// ── DRILL (c): an older binary must refuse a newer database ─────────────────────

func TestDrillVersionGateRefusesNewerDatabase(t *testing.T) {
	ctx := context.Background()
	_, dbPath := tempDB(t)
	st := openStore(t, dbPath)

	// A newer build migrated this database to v2.
	newer := mapFS(map[string]string{"0001_init.sql": test0001, "0002_more.sql": test0002})
	if _, err := newRunner(t, st, dbPath, newer).Up(ctx); err != nil {
		t.Fatalf("Up to v2: %v", err)
	}

	// An older build, which only knows v1, must refuse rather than write through it.
	_, err := newRunner(t, st, dbPath, oneMigration()).Up(ctx)
	assertCode(t, err, CodeDatabaseTooNew)
}

// ── DRILL (d): a corrupt database must abort before anything is written ─────────

func TestDrillCorruptDatabaseAbortsPreflight(t *testing.T) {
	ctx := context.Background()
	dir, dbPath := tempDB(t)

	// Build a database large enough to have pages past the header, then corrupt one.
	st := openStore(t, dbPath)
	if _, err := newRunner(t, st, dbPath, oneMigration()).Up(ctx); err != nil {
		t.Fatalf("baseline Up: %v", err)
	}
	for i := 0; i < 400; i++ {
		if _, err := st.WriterPool().ExecContext(ctx,
			`INSERT INTO invoices (id, total) VALUES (?, ?)`, fmt.Sprintf("inv-%d", i), i); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	if err := st.Close(); err != nil {
		t.Logf("close: %v", err) // WAL checkpoint noise is not the subject of this test
	}

	corrupt := filepath.Join(dir, "corrupt.db")
	raw, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read db: %v", err)
	}
	if len(raw) < 8192 {
		t.Fatalf("database too small to corrupt meaningfully: %d bytes", len(raw))
	}
	// Keep the 100-byte header intact so the file still opens; shred a later page so
	// only a real structural check can notice.
	for i := 4096; i < 6144 && i < len(raw); i++ {
		raw[i] = 0xFF
	}
	if wErr := os.WriteFile(corrupt, raw, 0o600); wErr != nil {
		t.Fatalf("write corrupt db: %v", wErr)
	}

	db, err := sql.Open("sqlite", fileDSN(corrupt))
	if err != nil {
		t.Fatalf("open corrupt db: %v", err)
	}
	defer db.Close()

	err = integrityCheck(ctx, db, "PRAGMA integrity_check")
	assertCode(t, err, CodeCorruptDatabase)
}

func TestIntegrityCheckPassesOnHealthyDatabase(t *testing.T) {
	ctx := context.Background()
	_, dbPath := tempDB(t)
	st := openStore(t, dbPath)
	if _, err := newRunner(t, st, dbPath, oneMigration()).Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if err := integrityCheck(ctx, st.WriterPool(), "PRAGMA integrity_check"); err != nil {
		t.Errorf("healthy database reported corrupt: %v", err)
	}
}

func TestIntegrityCheckSkippedWhenDialectHasNone(t *testing.T) {
	// An engine with no self-check (PostgreSQL, MySQL) must skip the step, not fake it.
	if err := integrityCheck(context.Background(), nil, ""); err != nil {
		t.Errorf("empty statement should be a no-op, got %v", err)
	}
}

// ── free-space pre-flight ───────────────────────────────────────────────────────

func TestFreeSpacePreflightRefusesWhenDiskIsTight(t *testing.T) {
	ctx := context.Background()
	dir, dbPath := tempDB(t)

	st := openStore(t, dbPath)
	if _, err := newRunner(t, st, dbPath, oneMigration()).Up(ctx); err != nil {
		t.Fatalf("baseline Up: %v", err)
	}

	fsys := mapFS(map[string]string{"0001_init.sql": test0001, "0002_more.sql": test0002})
	r := newRunner(t, st, dbPath, fsys, withBackups(filepath.Join(dir, "backups")))
	r.freeSpace = func(string) (uint64, error) { return 1 << 10, nil } // 1 KiB

	_, err := r.Up(ctx)
	assertCode(t, err, CodeInsufficientDisk)

	// Nothing may have been written: the gate exists to act before the backup.
	if got, _ := filepath.Glob(filepath.Join(dir, "backups", "*.db")); len(got) != 0 {
		t.Errorf("a backup was written despite the disk gate: %v", got)
	}
	if tableExists(t, dbPath, "payments") {
		t.Error("a migration was applied despite the disk gate")
	}
}

func TestFreeSpacePreflightPassesWithHeadroom(t *testing.T) {
	ctx := context.Background()
	dir, dbPath := tempDB(t)

	st := openStore(t, dbPath)
	if _, err := newRunner(t, st, dbPath, oneMigration()).Up(ctx); err != nil {
		t.Fatalf("baseline Up: %v", err)
	}

	fsys := mapFS(map[string]string{"0001_init.sql": test0001, "0002_more.sql": test0002})
	r := newRunner(t, st, dbPath, fsys, withBackups(filepath.Join(dir, "backups")))
	r.freeSpace = func(string) (uint64, error) { return 8 << 30, nil } // 8 GiB

	if _, err := r.Up(ctx); err != nil {
		t.Fatalf("Up with ample space: %v", err)
	}
	if !tableExists(t, dbPath, "payments") {
		t.Error("migration did not apply")
	}
}

func TestAvailableBytesReportsSomethingPlausible(t *testing.T) {
	// Exercises the real platform probe; the exact figure is the OS's business.
	got, err := availableBytes(t.TempDir())
	if err != nil {
		t.Fatalf("availableBytes: %v", err)
	}
	if got == 0 {
		t.Error("availableBytes reported 0 bytes free on a writable temp dir")
	}
}

// ── advisory lock ───────────────────────────────────────────────────────────────

func TestLockBlocksASecondMigrator(t *testing.T) {
	ctx := context.Background()
	_, dbPath := tempDB(t)
	st := openStore(t, dbPath)
	if _, err := newRunner(t, st, dbPath, oneMigration()).Up(ctx); err != nil {
		t.Fatalf("baseline Up: %v", err)
	}

	first := newRunner(t, st, dbPath, oneMigration())
	if err := first.acquireLock(ctx); err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	second := newRunner(t, st, dbPath, oneMigration())
	assertCode(t, second.acquireLock(ctx), CodeLockUnavailable)

	// Once released, the lock is available again.
	first.releaseLock(ctx)
	if err := second.acquireLock(ctx); err != nil {
		t.Errorf("acquire after release: %v", err)
	}
}

func TestStaleLockIsReclaimed(t *testing.T) {
	ctx := context.Background()
	_, dbPath := tempDB(t)
	st := openStore(t, dbPath)
	if _, err := newRunner(t, st, dbPath, oneMigration()).Up(ctx); err != nil {
		t.Fatalf("baseline Up: %v", err)
	}

	// A migrator that crashed while holding the lock must not lock the app out forever.
	crashed := clock.NewFixed(time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC))
	r := newRunner(t, st, dbPath, oneMigration())
	r.clock = crashed
	if err := r.acquireLock(ctx); err != nil {
		t.Fatalf("initial acquire: %v", err)
	}

	// Still held inside the stale window.
	next := newRunner(t, st, dbPath, oneMigration())
	next.clock = clock.NewFixed(crashed.Now().Add(staleLockAfter - time.Minute))
	assertCode(t, next.acquireLock(ctx), CodeLockUnavailable)

	// Reclaimable past it.
	next.clock = clock.NewFixed(crashed.Now().Add(staleLockAfter + time.Minute))
	if err := next.acquireLock(ctx); err != nil {
		t.Errorf("stale lock was not reclaimed after %v: %v", staleLockAfter, err)
	}
}

func TestLockSkippedOnVirginDatabase(t *testing.T) {
	// The very first migration runs before schema_lock exists; that must not error.
	ctx := context.Background()
	_, dbPath := tempDB(t)
	st := openStore(t, dbPath)

	r := newRunner(t, st, dbPath, oneMigration())
	held, err := r.lockTableExists(ctx)
	if err != nil {
		t.Fatalf("lockTableExists on virgin db: %v", err)
	}
	if held {
		t.Fatal("schema_lock reported present on a virgin database")
	}
	if _, upErr := r.Up(ctx); upErr != nil {
		t.Fatalf("Up on virgin db: %v", upErr)
	}
	if held, err = r.lockTableExists(ctx); err != nil || !held {
		t.Fatalf("schema_lock missing after migration (held=%v, err=%v)", held, err)
	}
}

// ── progress reporting ──────────────────────────────────────────────────────────

func TestProgressReportsEveryPhaseInOrder(t *testing.T) {
	ctx := context.Background()
	dir, dbPath := tempDB(t)
	st := openStore(t, dbPath)

	var seen []Progress
	fsys := mapFS(map[string]string{"0001_init.sql": test0001, "0002_more.sql": test0002})
	r := newRunner(t, st, dbPath, fsys,
		withBackups(filepath.Join(dir, "backups")), withProgress(&seen))
	r.freeSpace = func(string) (uint64, error) { return 8 << 30, nil }

	if _, err := r.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}

	if len(seen) == 0 {
		t.Fatal("no progress was emitted")
	}
	if seen[0].Phase != PhaseChecking {
		t.Errorf("first phase = %q, want %q", seen[0].Phase, PhaseChecking)
	}
	if last := seen[len(seen)-1]; last.Phase != PhaseDone {
		t.Errorf("last phase = %q, want %q", last.Phase, PhaseDone)
	}

	var migrating []Progress
	for _, p := range seen {
		if p.Phase == PhaseMigrating {
			migrating = append(migrating, p)
		}
	}
	if len(migrating) != 2 {
		t.Fatalf("got %d migrating events, want 2", len(migrating))
	}
	for i, p := range migrating {
		if p.Current != i+1 || p.Total != 2 {
			t.Errorf("event %d: Current=%d Total=%d, want %d and 2", i, p.Current, p.Total, i+1)
		}
		if p.Name == "" {
			t.Errorf("event %d has no migration name", i)
		}
	}
}

func TestProgressReportsRestoringOnFailure(t *testing.T) {
	ctx := context.Background()
	dir, dbPath := tempDB(t)
	st := openStore(t, dbPath)
	if _, err := newRunner(t, st, dbPath, oneMigration()).Up(ctx); err != nil {
		t.Fatalf("baseline Up: %v", err)
	}

	var seen []Progress
	broken := mapFS(map[string]string{"0001_init.sql": test0001, "0002_broken.sql": test0002Broken})
	r := newRunner(t, st, dbPath, broken,
		withBackups(filepath.Join(dir, "backups")), withProgress(&seen))

	if _, err := r.Up(ctx); err == nil {
		t.Fatal("expected the broken migration to fail")
	}
	var sawRestoring bool
	for _, p := range seen {
		if p.Phase == PhaseRestoring {
			sawRestoring = true
		}
	}
	if !sawRestoring {
		t.Errorf("no %q phase was emitted; the UI could not explain the pause: %+v", PhaseRestoring, seen)
	}
}

// ── retention ───────────────────────────────────────────────────────────────────

func TestPruneBackupsKeepsNewestAndSparesFailedArtifacts(t *testing.T) {
	dir := t.TempDir()
	// Names embed a sortable UTC timestamp, so lexical order is chronological.
	for _, name := range []string{
		"pre-migration-20260101T000000Z-v0-to-v1.db",
		"pre-migration-20260102T000000Z-v1-to-v2.db",
		"pre-migration-20260103T000000Z-v2-to-v3.db",
		"pre-migration-20260104T000000Z-v3-to-v4.db",
		"mizan.db.failed-20260104T000000Z",
		"unrelated.txt",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	r := &Runner{backupDir: dir}
	r.pruneBackups(2)

	kept, _ := filepath.Glob(filepath.Join(dir, "pre-migration-*.db"))
	if len(kept) != 2 {
		t.Fatalf("kept %d backups, want 2: %v", len(kept), kept)
	}
	for _, p := range kept {
		if base := filepath.Base(p); !strings.Contains(base, "20260103") && !strings.Contains(base, "20260104") {
			t.Errorf("pruning kept the wrong (older) backup: %s", base)
		}
	}
	// Incident evidence and unrelated files are never touched.
	for _, name := range []string{"mizan.db.failed-20260104T000000Z", "unrelated.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("pruning removed %s, which it must never do", name)
		}
	}
}

func TestPruneBackupsNoOpBelowThreshold(t *testing.T) {
	dir := t.TempDir()
	name := "pre-migration-20260101T000000Z-v0-to-v1.db"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &Runner{backupDir: dir}
	r.pruneBackups(5)
	if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
		t.Errorf("pruned below the retention threshold: %v", err)
	}
}

// ── construction & misc ─────────────────────────────────────────────────────────

func TestNewRejectsMissingRequirements(t *testing.T) {
	_, dbPath := tempDB(t)
	st := openStore(t, dbPath)

	cases := map[string]Options{
		"no FS":     {DBPath: dbPath},
		"no DBPath": {FS: oneMigration()},
	}
	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := New(st, opts); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
	t.Run("no store", func(t *testing.T) {
		if _, err := New(nil, Options{FS: oneMigration(), DBPath: dbPath}); err == nil {
			t.Fatal("expected an error")
		}
	})
}

func TestFileDSNEncodesAwkwardPaths(t *testing.T) {
	// Real macOS/Windows paths contain spaces; '?' and '#' are legal too and used to
	// truncate the DSN silently.
	dir := t.TempDir()
	for _, name := range []string{"plain.db", "with space.db", "with#hash.db", "with%pct.db"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			db, err := sql.Open("sqlite", fileDSN(path))
			if err != nil {
				t.Fatalf("open %q: %v", path, err)
			}
			defer db.Close()
			if _, err := db.Exec("CREATE TABLE t (id TEXT)"); err != nil {
				t.Fatalf("write to %q: %v", path, err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Errorf("DSN did not resolve to the intended path %q: %v", path, err)
			}
		})
	}
}

func TestForwardOnlyNoDownAPI(t *testing.T) {
	// D2 is a deliberate, load-bearing decision (§3.1): a down-migration never run on
	// customer data is a liability, not a safety net. This test is documentation that
	// fails if someone adds one.
	if _, hasDown := any(&Runner{}).(interface {
		Down(context.Context) error
	}); hasDown {
		t.Error("Runner has a Down() method; migrations are forward-only by design (D2)")
	}
}
