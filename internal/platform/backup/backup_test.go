package backup_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/platform/backup"
)

// ── the harness ─────────────────────────────────────────────────────────────────

type fakeDialect struct {
	name      string
	integrity string
	online    func(dest string) string
}

func (d fakeDialect) Name() string                    { return d.name }
func (d fakeDialect) IntegrityCheckStatement() string { return d.integrity }
func (d fakeDialect) OnlineBackupStatement(dest string) string {
	return d.online(dest)
}

type fakeDB struct {
	pool    *sql.DB
	dialect backup.Dialect
}

func (f fakeDB) WriterPool() *sql.DB     { return f.pool }
func (f fakeDB) Dialect() backup.Dialect { return f.dialect }

// fixture builds a real SQLite database with a migration history, and a service over it.
//
// The clock comes back so a test about AGE can move time rather than sleep. A test that slept
// would be slow and, worse, flaky on a loaded machine — the two properties a drill must not have.
func fixture(t *testing.T) (*backup.Service, string, *sql.DB, *clock.Fixed) {
	t.Helper()
	root := t.TempDir()
	livePath := filepath.Join(root, "live.db")

	pool, err := sql.Open("sqlite", "file:"+livePath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = pool.Close() })

	// A migration history, because that is what Verify reads and what a restore compares.
	if _, err = pool.Exec(
		`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT);
		 INSERT INTO schema_migrations VALUES (1, 'platform'), (36, 'debts');
		 CREATE TABLE things (id INTEGER PRIMARY KEY, note TEXT);
		 INSERT INTO things VALUES (1, 'a shop''s data');`); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	clk := clock.NewFixed(time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC))
	service := backup.New(fakeDB{
		pool: pool,
		dialect: fakeDialect{
			name:      "sqlite",
			integrity: "PRAGMA integrity_check",
			online: func(dest string) string {
				if dest == "" {
					return "VACUUM INTO ''"
				}
				return "VACUUM INTO '" + dest + "'"
			},
		},
	}, backup.Options{
		Dir: filepath.Join(root, "backups"),
		// A fixed clock, so filenames are predictable and two snapshots taken in one test do not
		// collide by landing in the same second.
		Clock:      clk,
		AppVersion: "test",
	})
	return service, root, pool, clk
}

// ── what a backup is ────────────────────────────────────────────────────────────

// TestASnapshotIsVerifiedAndCarriesWhatItIsABackupOf
//
// DoD criteria 2 and 3. A filename tells a user the date and nothing else; a restore needs the
// SCHEMA VERSION, because restoring a v41 backup into a v36 binary is a downgrade the migration
// runner cannot perform and must refuse rather than attempt.
//
// Every field is read from the DATABASE. A manifest recording the version the caller believed it
// was backing up would be right until the one time it mattered.
func TestASnapshotIsVerifiedAndCarriesWhatItIsABackupOf(t *testing.T) {
	service, _, _, _ := fixture(t)

	taken, err := service.Take(context.Background(), backup.OnDemand)
	if err != nil {
		t.Fatalf("Take: %v", err)
	}

	if taken.Manifest.SchemaVersion != 36 {
		t.Errorf("schema version = %d, want 36 — read from the database, not from the caller",
			taken.Manifest.SchemaVersion)
	}
	if taken.Manifest.Reason != backup.OnDemand {
		t.Errorf("reason = %q, want on_demand", taken.Manifest.Reason)
	}
	if taken.Manifest.SizeBytes == 0 {
		t.Error("the manifest records a size of zero")
	}
	if taken.Manifest.AppVersion != "test" {
		t.Errorf("app version = %q, want test", taken.Manifest.AppVersion)
	}
	if taken.Manifest.TakenAt == "" {
		t.Error("the manifest records no time")
	}

	// The snapshot is a real database with the shop's data in it — not an empty file with a
	// convincing name.
	restored, err := sql.Open("sqlite", "file:"+taken.Path)
	if err != nil {
		t.Fatalf("opening the snapshot: %v", err)
	}
	defer func() { _ = restored.Close() }()

	var note string
	if err = restored.QueryRow(`SELECT note FROM things WHERE id = 1`).Scan(&note); err != nil {
		t.Fatalf("reading the snapshot: %v", err)
	}
	if note != "a shop's data" {
		t.Errorf("the snapshot holds %q", note)
	}
}

// TestAnUnverifiableSnapshotIsRemovedRatherThanListed
//
// # The criterion that makes this package worth promoting
//
// A file in the backup directory is a PROMISE. One that cannot be opened is a promise that will
// be discovered broken at the worst possible moment — which is the moment somebody needs it.
//
// The snapshot here is written as an empty file, which is what a truncated copy or a full disk
// produces: it exists, it has a plausible name, and it is not a database.
func TestAnUnverifiableSnapshotIsRemovedRatherThanListed(t *testing.T) {
	service, root, pool, _ := fixture(t)

	// The FAKE writes the unusable file, which is what a truncated copy or a full disk produces:
	// the destination exists, has a plausible name, and is not a database.
	//
	// The first version of this test pre-created that file and used a no-op statement — and
	// `Take` removes the destination before writing, so the rubbish was gone before the statement
	// ran and `Verify` failed on a MISSING file rather than an unusable one. A drill deleting the
	// cleanup left it green, because there was nothing on disk to clean up.
	var rubbish string
	broken := backup.New(fakeDB{
		pool: pool,
		dialect: fakeDialect{
			name: "sqlite", integrity: "PRAGMA integrity_check",
			online: func(dest string) string {
				if dest != "" {
					rubbish = dest
					_ = os.WriteFile(dest, []byte("this is not a database"), 0o600)
				}
				return "SELECT 1"
			},
		},
	}, backup.Options{
		Dir:   filepath.Join(root, "broken"),
		Clock: clock.NewFixed(time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)),
	})

	_, err := broken.Take(context.Background(), backup.OnDemand)
	if err == nil {
		t.Fatal("a snapshot that is not a database was reported as a backup")
	}

	// And it is GONE. Left on disk, it would be offered for restore.
	if _, statErr := os.Stat(rubbish); !os.IsNotExist(statErr) {
		t.Error("the unverifiable snapshot is still on disk, where it will be offered as a " +
			"backup somebody can restore from")
	}

	listed, err := broken.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 0 {
		t.Errorf("%d backups listed after a failure: %+v", len(listed), listed)
	}
	_ = service
}

// TestAnEngineThatCannotSnapshotIsRefusedRatherThanFaked
//
// Proceeding would mean taking no backup while reporting that one exists — which is worse than
// failing, because the caller stops looking.
func TestAnEngineThatCannotSnapshotIsRefusedRatherThanFaked(t *testing.T) {
	_, root, pool, _ := fixture(t)

	service := backup.New(fakeDB{
		pool:    pool,
		dialect: fakeDialect{name: "postgres", online: func(string) string { return "" }},
	}, backup.Options{Dir: filepath.Join(root, "pg")})

	_, err := service.Take(context.Background(), backup.OnDemand)
	if code := errs.CodeOf(err); code != backup.CodeNoOnlineBackup {
		t.Errorf("code = %q, want %q", code, backup.CodeNoOnlineBackup)
	}
}

// ── verification, which restore depends on ──────────────────────────────────────

// TestVerifyRefusesAFileThatIsNotAMizanDatabase
//
// Restore calls this BEFORE it touches the live database, which is the whole reason it is
// exported. Discovering afterwards that the incoming file was rubbish is unrecoverable.
func TestVerifyRefusesAFileThatIsNotAMizanDatabase(t *testing.T) {
	root := t.TempDir()

	notADatabase := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(notADatabase, []byte("shopping list"), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}

	_, err := backup.Verify(context.Background(), notADatabase, "PRAGMA integrity_check")
	if err == nil {
		t.Fatal("a text file verified as a database")
	}

	// A real database with no migration history verifies as VERSION ZERO, and does not fail.
	//
	// The first version of this test expected a refusal, and the migration suite showed why that
	// was wrong within minutes: the snapshot taken before the very first migration has no
	// `schema_migrations` table, because the migration that creates it has not run. Refusing
	// there means a fresh install cannot be migrated at all.
	//
	// So verification reports what it FOUND, and "is this something we can restore from" is the
	// restore's question — asked in 9.2 against this version number, where the policy belongs.
	stranger := filepath.Join(root, "stranger.db")
	pool, err := sql.Open("sqlite", "file:"+stranger)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err = pool.Exec(`CREATE TABLE unrelated (id INTEGER)`); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	_ = pool.Close()

	manifest, err := backup.Verify(context.Background(), stranger, "PRAGMA integrity_check")
	if err != nil {
		t.Fatalf("a database with no migration history did not verify: %v", err)
	}
	if manifest.SchemaVersion != 0 {
		t.Errorf("schema version = %d, want 0", manifest.SchemaVersion)
	}

	missing := filepath.Join(root, "gone.db")
	if _, err = backup.Verify(context.Background(), missing, ""); err == nil {
		t.Error("a file that does not exist verified")
	}
}

// ── listing and finding ─────────────────────────────────────────────────────────

// TestAFileWithNoManifestIsNotABackup
//
// It was written by something else, or by a Take that failed between the copy and the manifest.
// Offering to restore from it would be offering a file this package cannot describe — and the
// version check a restore depends on has nothing to read.
func TestAFileWithNoManifestIsNotABackup(t *testing.T) {
	service, root, _, _ := fixture(t)

	if _, err := service.Take(context.Background(), backup.OnDemand); err != nil {
		t.Fatalf("Take: %v", err)
	}
	stray := filepath.Join(root, "backups", "somebody-copied-this-here.db")
	if err := os.WriteFile(stray, []byte("x"), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}

	listed, err := service.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("%d backups listed, want 1 — the stray file was counted", len(listed))
	}
}

// TestABackupIsFoundByNameAndOnlyByName
//
// A binding that accepted a PATH would let a caller name any file on the disk. "Restore
// /etc/passwd" is a question this package should never be asked, and the way to never be asked it
// is to be incapable of answering.
func TestABackupIsFoundByNameAndOnlyByName(t *testing.T) {
	service, _, _, _ := fixture(t)

	taken, err := service.Take(context.Background(), backup.OnDemand)
	if err != nil {
		t.Fatalf("Take: %v", err)
	}

	found, err := service.Find(taken.Name)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if found.Path != taken.Path {
		t.Errorf("found %q, want %q", found.Path, taken.Path)
	}

	// # What actually makes traversal impossible, and what merely looks like it does
	//
	// `Find` refuses a name containing a separator or "..", and a drill removing that check
	// changed nothing — because Find only ever returns entries from `List`, whose paths are built
	// by joining this service's directory with basenames from `ReadDir`. A traversal cannot
	// escape a set it is never compared against.
	//
	// The guard stays, because a hostile name deserves a validation error rather than a
	// not-found, and because the next person to add a lookup here should find the intent
	// written down. What is ASSERTED is the mechanism: whatever Find returns lives in the
	// backup directory.
	for _, hostile := range []string{
		"../../../etc/passwd",
		"/etc/passwd",
		"..",
		"",
	} {
		if _, err = service.Find(hostile); err == nil {
			t.Errorf("Find(%q) succeeded", hostile)
		}
	}

	directory := filepath.Dir(taken.Path)
	for _, candidate := range mustBackups(t, service) {
		if filepath.Dir(candidate.Path) != directory {
			t.Errorf("a listed backup lives outside the backup directory: %q", candidate.Path)
		}
		if candidate.Name != filepath.Base(candidate.Path) {
			t.Errorf("backup %q names a path rather than a file", candidate.Name)
		}
	}
}

func mustBackups(t *testing.T, service *backup.Service) []backup.Backup {
	t.Helper()
	found, err := service.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return found
}

// ── pruning ─────────────────────────────────────────────────────────────────────

// TestPruningKeepsTheNewestOfEachReasonAndNeverEmptiesTheDirectory
//
// DoD criterion 6. PER REASON, because a nightly scheduled backup would otherwise push out the
// pre-migration snapshot from the upgrade that broke something — which is the one a support
// conversation is about.
//
// And a `keep` of zero must not delete everything: a configuration mistake that leaves a shop
// with no backup at all is exactly the failure backups exist to prevent.
func TestPruningKeepsTheNewestOfEachReasonAndNeverEmptiesTheDirectory(t *testing.T) {
	_, root, pool, _ := fixture(t)

	// A moving clock, so each snapshot gets its own filename and a comparable timestamp.
	moment := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	moving := &steppingClock{now: moment}

	service := backup.New(fakeDB{
		pool: pool,
		dialect: fakeDialect{
			name: "sqlite", integrity: "PRAGMA integrity_check",
			online: func(dest string) string {
				if dest == "" {
					return "VACUUM INTO ''"
				}
				return "VACUUM INTO '" + dest + "'"
			},
		},
	}, backup.Options{
		Dir: filepath.Join(root, "pruned"), Clock: moving, Keep: 2,
	})

	// Three scheduled and one pre-migration.
	for range 3 {
		if _, err := service.Take(context.Background(), backup.Scheduled); err != nil {
			t.Fatalf("Take: %v", err)
		}
	}
	if _, err := service.Take(context.Background(), backup.BeforeMigration); err != nil {
		t.Fatalf("Take: %v", err)
	}

	removed, err := service.Prune(context.Background())
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if removed != 1 {
		t.Errorf("pruned %d, want 1 — three scheduled keeping two", removed)
	}

	listed, err := service.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byReason := map[backup.Reason]int{}
	for _, candidate := range listed {
		byReason[candidate.Manifest.Reason]++
	}
	if byReason[backup.Scheduled] != 2 {
		t.Errorf("%d scheduled backups survived, want 2", byReason[backup.Scheduled])
	}
	// The pre-migration snapshot is untouched, although the scheduled ones are newer.
	if byReason[backup.BeforeMigration] != 1 {
		t.Errorf("the pre-migration snapshot was pruned by unrelated scheduled backups")
	}

	// The manifest goes with the file. A manifest left behind would make List skip a file that
	// no longer exists — harmless — but would also accumulate forever.
	strays, err := filepath.Glob(filepath.Join(root, "pruned", "*.json"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(strays) != len(listed) {
		t.Errorf("%d manifests for %d backups", len(strays), len(listed))
	}
}

// TestKeepingZeroStillKeepsOne
func TestKeepingZeroStillKeepsOne(t *testing.T) {
	_, root, pool, _ := fixture(t)
	moving := &steppingClock{now: time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)}

	// Keep: -1 is the misconfiguration. `New` normalises it to the default, which is where the
	// floor lives — and where a drill can reach it. Prune trusts that normalisation rather than
	// re-applying a floor of its own, because two floors are two things to keep in step and the
	// second is the one nobody tests.
	service := backup.New(fakeDB{
		pool: pool,
		dialect: fakeDialect{
			name: "sqlite", integrity: "PRAGMA integrity_check",
			online: func(dest string) string {
				if dest == "" {
					return "VACUUM INTO ''"
				}
				return "VACUUM INTO '" + dest + "'"
			},
		},
	}, backup.Options{Dir: filepath.Join(root, "zero"), Clock: moving, Keep: -1})

	for range 3 {
		if _, err := service.Take(context.Background(), backup.Scheduled); err != nil {
			t.Fatalf("Take: %v", err)
		}
	}
	if _, err := service.Prune(context.Background()); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	listed, err := service.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) == 0 {
		t.Fatal("a keep of zero deleted every backup — a configuration mistake left the shop " +
			"with nothing, which is the failure backups exist to prevent")
	}
}

// TestListingWhenNothingHasEverBeenBackedUp
//
// An ordinary state on a fresh install, not a failure.
func TestListingWhenNothingHasEverBeenBackedUp(t *testing.T) {
	_, root, pool, _ := fixture(t)
	service := backup.New(fakeDB{pool: pool}, backup.Options{
		Dir: filepath.Join(root, "never"),
	})

	listed, err := service.List()
	if err != nil {
		t.Fatalf("List on a fresh install: %v", err)
	}
	if len(listed) != 0 {
		t.Errorf("%d backups on a fresh install", len(listed))
	}
	if !strings.HasSuffix(root, "") {
		t.Fatal("unreachable")
	}
}

// steppingClock advances a second on every read, so snapshots taken in a loop get distinct names.
type steppingClock struct{ now time.Time }

func (c *steppingClock) Now() time.Time {
	c.now = c.now.Add(time.Second)
	return c.now
}

// TestThereIsOnlyOneBackupImplementation
//
// # DoD criterion 1, checked structurally
//
// The whole argument for promoting this package was that TWO implementations means two things
// that can be wrong about whether a backup is trustworthy — and only one of them would have the
// verification in it.
//
// A comment saying so does not stop somebody writing a second one. This reads the tree and
// requires every online-backup statement to be executed from this package: anywhere else is a
// snapshot taken outside the mechanism that verifies it.
func TestThereIsOnlyOneBackupImplementation(t *testing.T) {
	root := filepath.Join("..", "..", "..", "internal")

	// The dialect DECLARES the statement and this package EXECUTES it. Any other caller is a
	// second implementation.
	callers := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if !strings.Contains(string(source), "OnlineBackupStatement(") {
			return nil
		}
		callers[filepath.ToSlash(filepath.Dir(path))] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}

	allowed := map[string]bool{
		// Where the statement is DEFINED.
		"../../../internal/platform/database/dialect": true,
		// Where it is EXECUTED, and verified.
		"../../../internal/platform/backup": true,
	}
	for caller := range callers {
		if !allowed[caller] {
			t.Errorf("%s executes an online-backup statement — a snapshot taken outside "+
				"platform/backup is a snapshot nothing verifies, which is the failure this "+
				"package was promoted to prevent", caller)
		}
	}

	// The walk found the two it expects. A path that matched nothing would pass this test while
	// checking nothing at all.
	if len(callers) < 2 {
		t.Fatalf("only %d packages mention OnlineBackupStatement (%v); the dialect defines it "+
			"and this package uses it, so there should be at least two", len(callers), callers)
	}
}

// ── the snapshot nobody has to ask for ──────────────────────────────────────────

// TestARecentSnapshotMeansTheCloseTimeOneIsSkipped
//
// The whole point of TakeIfDue. A shop that opens and closes the window six times in an hour must
// not get six snapshots — they would be near-identical, and since retention keeps seven per
// reason they would push the useful ones out within the hour.
//
// Note WHICH snapshot suppresses it: a `scheduled` one. The question TakeIfDue answers is "is this
// data already safe", and a copy from two minutes ago answers it regardless of why it was taken.
func TestARecentSnapshotMeansTheCloseTimeOneIsSkipped(t *testing.T) {
	service, _, _, clk := fixture(t)
	ctx := context.Background()

	if _, err := service.Take(ctx, backup.Scheduled); err != nil {
		t.Fatalf("the first snapshot: %v", err)
	}

	clk.Advance(20 * time.Minute)
	taken, took, err := service.TakeIfDue(ctx, backup.OnClose, time.Hour)
	if err != nil {
		t.Fatalf("TakeIfDue: %v", err)
	}
	if took {
		t.Errorf("a second snapshot was taken 20 minutes after the first: %q", taken.Name)
	}

	found, err := service.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(found) != 1 {
		t.Errorf("the directory holds %d snapshots, want 1", len(found))
	}
}

// TestOnceTheWindowHasPassedTheCloseTimeSnapshotIsTaken
//
// The other half, and the one that matters: this is the drill that fails if the close-time backup
// silently stops happening. A test that only proved the SKIP would pass just as happily against a
// TakeIfDue that never took anything at all.
func TestOnceTheWindowHasPassedTheCloseTimeSnapshotIsTaken(t *testing.T) {
	service, _, _, clk := fixture(t)
	ctx := context.Background()

	if _, err := service.Take(ctx, backup.Scheduled); err != nil {
		t.Fatalf("the morning snapshot: %v", err)
	}

	// A trading day.
	clk.Advance(9 * time.Hour)
	taken, took, err := service.TakeIfDue(ctx, backup.OnClose, time.Hour)
	if err != nil {
		t.Fatalf("TakeIfDue: %v", err)
	}
	if !took {
		t.Fatal("nine hours of trading closed without a snapshot")
	}
	if taken.Manifest.Reason != backup.OnClose {
		t.Errorf("reason = %q, want %q", taken.Manifest.Reason, backup.OnClose)
	}
	// Filed in its own bucket, which is what stops it evicting the daily ones.
	if !strings.HasPrefix(taken.Name, string(backup.OnClose)) {
		t.Errorf("name = %q, want it to start with %q", taken.Name, backup.OnClose)
	}
	// And it is a REAL backup, verified and restorable — not a marker file.
	if taken.Manifest.SchemaVersion != 36 {
		t.Errorf("schema version = %d, want 36", taken.Manifest.SchemaVersion)
	}
}

// TestTheFirstEverCloseTakesASnapshotRatherThanFindingNothingAndSkipping
//
// An empty directory is the state of every fresh install, and the boundary an "is there a recent
// one" check is most likely to get backwards. Skipping here would mean the very first shop to
// install Mizan, trade for a day and close gets no backup at all.
func TestTheFirstEverCloseTakesASnapshotRatherThanFindingNothingAndSkipping(t *testing.T) {
	service, _, _, _ := fixture(t)

	_, took, err := service.TakeIfDue(context.Background(), backup.OnClose, time.Hour)
	if err != nil {
		t.Fatalf("TakeIfDue: %v", err)
	}
	if !took {
		t.Error("the first close on a fresh install took no snapshot")
	}
}

// TestASnapshotStampedInTheFutureDoesNotSuppressBackups
//
// A clock corrected backwards — a machine that booted with a bad RTC and then reached an NTP
// server — leaves a snapshot dated ahead of `now`. A naive `now.Sub(takenAt) < minAge` reads that
// as "taken recently" and skips, and it keeps skipping until the file ages out of retention.
//
// That is a backup system silently switched off by a wrong clock. When the two readings disagree
// the safe direction is the extra snapshot.
func TestASnapshotStampedInTheFutureDoesNotSuppressBackups(t *testing.T) {
	service, _, _, clk := fixture(t)
	ctx := context.Background()

	// Taken "tomorrow", then the clock is corrected back to today.
	clk.Advance(24 * time.Hour)
	if _, err := service.Take(ctx, backup.Scheduled); err != nil {
		t.Fatalf("the future snapshot: %v", err)
	}
	clk.Advance(-24 * time.Hour)

	_, took, err := service.TakeIfDue(ctx, backup.OnClose, time.Hour)
	if err != nil {
		t.Fatalf("TakeIfDue: %v", err)
	}
	if !took {
		t.Error("a snapshot dated in the future suppressed the close-time backup")
	}
}

// TestAFailedSnapshotLeavesNothingBehindToFillTheDisk
//
// # The leak this closes
//
// A half-written file has no manifest. List skips anything whose manifest will not read, so the
// file is invisible to the backup screen AND to Prune — which only removes what List returns. It
// therefore accumulates, once per failure, until it fills the disk.
//
// That is the worst shape a bug can have: silent, unbounded, and it surfaces as "backups are
// failing for want of space" long after the cause. A close-time backup is what made it reachable
// in practice, because that one CAN be cancelled — the shutdown budget expires mid-copy on a
// large database.
func TestAFailedSnapshotLeavesNothingBehindToFillTheDisk(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "backups")

	pool, err := sql.Open("sqlite", "file:"+filepath.Join(root, "live.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if _, err = pool.Exec(
		`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT);
		 INSERT INTO schema_migrations VALUES (1, 'platform');`); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	// A statement that writes the destination and THEN fails, which is what a cancelled or
	// out-of-space VACUUM INTO leaves behind: a file on disk and an error returned.
	service := backup.New(fakeDB{pool: pool, dialect: fakeDialect{
		name:      "sqlite",
		integrity: "PRAGMA integrity_check",
		online: func(dest string) string {
			if dest == "" {
				return "VACUUM INTO ''"
			}
			return "VACUUM INTO '" + dest + "'; SELECT this_is_not_a_function();"
		},
	}}, backup.Options{Dir: dir, AppVersion: "test"})

	if _, err = service.Take(context.Background(), backup.OnClose); err == nil {
		t.Fatal("a snapshot that could not be written reported success")
	}

	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("reading the backup directory: %v", err)
	}
	for _, entry := range entries {
		t.Errorf("a failed snapshot left %q behind; nothing will ever remove it", entry.Name())
	}
}
