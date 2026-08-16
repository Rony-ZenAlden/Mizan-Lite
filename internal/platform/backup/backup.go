// Package backup takes and verifies database snapshots (§4.2).
//
// # Why this is platform, and why it was promoted rather than written
//
// The mechanism already existed. `migrate/safety.go` has snapshotted before every migration since
// Phase 0, and it does the part everybody skips: it opens the snapshot INDEPENDENTLY,
// integrity-checks it, and confirms the migration history reads back — on the grounds that "an
// unverified backup is a rumour".
//
// Phase 9 needs the same thing on demand and on a schedule. Writing a second implementation would
// mean two things that can be wrong about whether a backup is trustworthy, and only one of them
// has the verification in it.
//
// The promotion happens at the SECOND caller rather than the third, which is where
// `round.Allocate` and `domain.ValueOf` were promoted. Those were arithmetic: cheap to duplicate,
// obvious when wrong. A silently unverified backup looks identical to a verified one, and the
// cost of finding out is the whole database.
package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // verification opens a standalone connection

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// Stable codes. Each doubles as an i18n key.
const (
	CodeBackupFailed   = "backup.failed"
	CodeCorrupt        = "backup.corrupt"
	CodeNoOnlineBackup = "backup.engine_cannot_snapshot"
	CodeUnknownBackup  = "backup.unknown"
	CodeManifestFailed = "backup.manifest_unreadable"
)

// Reason says why a snapshot was taken. It becomes part of the filename and the manifest.
type Reason string

// The reasons a snapshot exists.
const (
	// BeforeMigration is the original caller: 0.7's safety net.
	BeforeMigration Reason = "before_migration"
	// BeforeRestore is the snapshot a restore takes of what it is about to replace.
	//
	// The one that matters most and is easiest to leave out. A restore that goes wrong having
	// left nothing to go back to is worse than no restore feature, because the user chose it
	// believing it was safe.
	BeforeRestore Reason = "before_restore"
	// OnDemand is a person pressing the button.
	OnDemand Reason = "on_demand"
	// Scheduled is the job.
	Scheduled Reason = "scheduled"
)

// Manifest is what a backup is a backup OF.
//
// # Why a filename is not enough
//
// `mizan-20260816.db` tells a user the date and nothing else. A restore needs the SCHEMA VERSION,
// because restoring a v41 backup into a v36 binary is a downgrade the migration runner cannot
// perform — and it must be refused rather than attempted.
//
// Every field is read from the DATABASE, never from what the caller believed. A manifest that
// recorded the version the caller thought it was backing up would be right until the one time it
// mattered.
type Manifest struct {
	// SchemaVersion is the highest migration the snapshot has applied.
	SchemaVersion int64 `json:"schemaVersion"`
	// TakenAt is when, in RFC3339 UTC.
	TakenAt string `json:"takenAt"`
	// Reason is why.
	Reason Reason `json:"reason"`
	// SizeBytes is the snapshot's size, so a screen can show it without stat-ing the file.
	SizeBytes int64 `json:"sizeBytes"`
	// AppVersion is the build that took it, for a support conversation.
	AppVersion string `json:"appVersion"`
}

// Backup is a snapshot on disk: its file, and what it is a backup of.
type Backup struct {
	// Path is the .db file.
	Path string
	// Manifest is the sidecar's contents.
	Manifest Manifest
	// Name is the file's basename, and the identifier a caller passes back.
	//
	// Not the full path: a binding that accepted one would let a caller name any file on the
	// disk, and "restore /etc/passwd" is a question this package should never be asked.
	Name string
}

// Database is the narrow surface a snapshot needs.
//
// Deliberately not `database.DB`: this package writes no application data and reads no
// application tables, so taking the full interface would let it grow into something that does.
type Database interface {
	WriterPool() *sql.DB
	Dialect() Dialect
}

// Dialect is the engine-specific half.
type Dialect interface {
	Name() string
	IntegrityCheckStatement() string
	OnlineBackupStatement(dest string) string
}

// Options configures a Service.
type Options struct {
	// Dir is where snapshots live.
	Dir string
	// Clock stamps filenames and manifests.
	Clock clock.Clock
	// AppVersion is recorded in every manifest.
	AppVersion string
	// Keep is how many snapshots to retain per reason. Zero means the default.
	Keep int
}

// defaultKeep is how many snapshots of each reason survive pruning.
//
// PER REASON, not overall. A shop taking a nightly scheduled backup would otherwise push out the
// pre-migration snapshot from the upgrade that broke something — which is the one a support
// conversation is about.
const defaultKeep = 7

// Service takes, lists, verifies and prunes snapshots.
type Service struct {
	db         Database
	dir        string
	clk        clock.Clock
	appVersion string
	keep       int
}

// New builds a Service.
func New(db Database, opts Options) *Service {
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}
	if opts.Keep <= 0 {
		opts.Keep = defaultKeep
	}
	return &Service{
		db: db, dir: opts.Dir, clk: opts.Clock,
		appVersion: opts.AppVersion, keep: opts.Keep,
	}
}

// Take writes a verified snapshot and its manifest.
//
// # The verification is the whole value
//
// A snapshot that is written and never opened is a file somebody will discover is unreadable on
// the day they need it. This opens it independently, integrity-checks it, and reads its migration
// history back — and DELETES it if any of that fails, so an unusable file is never listed as a
// backup.
func (s *Service) Take(ctx context.Context, reason Reason) (Backup, error) {
	if err := os.MkdirAll(s.dir, 0o750); err != nil {
		return Backup{}, errs.Wrap(err, errs.CategoryInternal, CodeBackupFailed,
			"creating the backup directory")
	}

	statement := s.db.Dialect().OnlineBackupStatement("")
	if statement == "" {
		// Proceeding would mean taking no backup while reporting that one exists.
		return Backup{}, errs.Internal(CodeNoOnlineBackup, fmt.Sprintf(
			"engine %q cannot snapshot itself; refusing to report a backup that does not exist",
			s.db.Dialect().Name()))
	}

	name := fmt.Sprintf("%s-%s.db", string(reason),
		s.clk.Now().UTC().Format("20060102T150405Z"))
	destination := filepath.Join(s.dir, name)

	// The statement refuses to overwrite, and a stale file from an aborted run would block it.
	_ = os.Remove(destination)

	if _, err := s.db.WriterPool().ExecContext(
		ctx, s.db.Dialect().OnlineBackupStatement(destination),
	); err != nil {
		return Backup{}, errs.Wrap(err, errs.CategoryInternal, CodeBackupFailed,
			"writing the snapshot")
	}

	manifest, err := Verify(ctx, destination, s.db.Dialect().IntegrityCheckStatement())
	if err != nil {
		// An unverifiable snapshot is REMOVED rather than left on disk. A file in the backup
		// directory is a promise, and one that cannot be opened is a promise that will be
		// discovered broken at the worst moment.
		_ = os.Remove(destination)
		return Backup{}, err
	}
	manifest.Reason = reason
	manifest.AppVersion = s.appVersion
	manifest.TakenAt = s.clk.Now().UTC().Format(time.RFC3339)

	if err = writeManifest(destination, manifest); err != nil {
		_ = os.Remove(destination)
		return Backup{}, err
	}
	return Backup{Path: destination, Manifest: manifest, Name: name}, nil
}

// Verify opens a database file independently and reports what it contains.
//
// Exported because RESTORE needs exactly this before it touches anything: a file that is not a
// database, is corrupt, or has no migration history must be refused BEFORE the live database is
// replaced. Discovering it afterwards is unrecoverable.
func Verify(ctx context.Context, path, integrityStatement string) (Manifest, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Manifest{}, errs.NotFound(CodeUnknownBackup, "there is no such backup").
			WithParam("path", filepath.Base(path))
	}

	db, err := sql.Open("sqlite", fileDSN(path))
	if err != nil {
		return Manifest{}, errs.Wrap(err, errs.CategoryInternal, CodeCorrupt,
			"opening the snapshot for verification")
	}
	defer func() { _ = db.Close() }()

	if err = integrityCheck(ctx, db, integrityStatement); err != nil {
		return Manifest{}, err
	}

	// # Why a missing history is version 0 rather than a refusal
	//
	// The first version of this function refused it, and the migration tests caught the problem
	// immediately: the snapshot taken BEFORE the very first migration has no `schema_migrations`
	// table, because the migration that creates it has not run. Refusing there would mean a
	// fresh install could not be migrated at all.
	//
	// 0.7's original said the same thing in one line — "a fresh database that has never migrated
	// has no history table yet; that is fine" — and losing it in the promotion is exactly the
	// kind of behaviour a move quietly drops.
	//
	// So VERIFICATION reports what it found, and the stricter question — "is this something we
	// can restore from" — belongs to the restore, which is where the policy is. A verification
	// that enforced a restore's rules would have no way to serve the migration path.
	var version sql.NullInt64
	if err = db.QueryRowContext(ctx,
		`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		if !isMissingTable(err) {
			return Manifest{}, errs.Wrap(err, errs.CategoryInternal, CodeCorrupt,
				"the migration history is unreadable")
		}
		return Manifest{SchemaVersion: 0, SizeBytes: info.Size()}, nil
	}

	return Manifest{SchemaVersion: version.Int64, SizeBytes: info.Size()}, nil
}

// List reports every verified snapshot, newest first.
//
// A file with no manifest is SKIPPED rather than listed with unknown provenance: it was written
// by something else, or a `Take` that failed between the copy and the manifest, and offering to
// restore from it would be offering a file this package cannot describe.
func (s *Service) List() ([]Backup, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		// No directory means no backups, which is an ordinary state on a fresh install rather
		// than a failure.
		return []Backup{}, nil
	}
	if err != nil {
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeBackupFailed,
			"reading the backup directory")
	}

	out := make([]Backup, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		path := filepath.Join(s.dir, entry.Name())
		manifest, readErr := readManifest(path)
		if readErr != nil {
			continue
		}
		out = append(out, Backup{Path: path, Manifest: manifest, Name: entry.Name()})
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Manifest.TakenAt > out[j].Manifest.TakenAt
	})
	return out, nil
}

// Find resolves a NAME to a backup, refusing anything that is not one.
//
// The name is a basename and is rejoined to this service's directory, so a caller cannot name a
// file elsewhere on the disk. "Restore /etc/passwd" is a question this package should never be
// asked, and the way to never be asked it is to be incapable of answering.
func (s *Service) Find(name string) (Backup, error) {
	if name == "" || name != filepath.Base(name) || strings.Contains(name, "..") {
		return Backup{}, errs.Validation(CodeUnknownBackup, "that is not a backup name").
			WithParam("name", name)
	}
	for _, candidate := range mustList(s) {
		if candidate.Name == name {
			return candidate, nil
		}
	}
	return Backup{}, errs.NotFound(CodeUnknownBackup, "there is no such backup").
		WithParam("name", name)
}

func mustList(s *Service) []Backup {
	found, err := s.List()
	if err != nil {
		return nil
	}
	return found
}

// Prune removes old snapshots, keeping the newest of each reason.
//
// # The most recent is never pruned, whatever the policy says
//
// A `keep` of zero would otherwise delete everything — and a configuration mistake that leaves a
// shop with no backup at all is exactly the failure backups exist to prevent. The floor is one
// per reason, enforced here rather than trusted to the caller.
func (s *Service) Prune(ctx context.Context) (int, error) {
	_ = ctx

	found, err := s.List()
	if err != nil {
		return 0, err
	}

	seen := make(map[Reason]int, 4)
	removed := 0
	for _, candidate := range found {
		seen[candidate.Manifest.Reason]++
		// `s.keep` is already at least one: New normalises a zero or negative Keep to the
		// default. A `max(s.keep, 1)` here read as the floor that stops a misconfiguration
		// emptying the directory — and a drill removing it changed nothing, because the value
		// can never be below one by the time it arrives.
		//
		// The floor belongs in ONE place. Two would be two things to keep in step, and the
		// second would be the one nobody tested.
		if seen[candidate.Manifest.Reason] <= s.keep {
			continue
		}
		if err = os.Remove(candidate.Path); err != nil {
			return removed, errs.Wrap(err, errs.CategoryInternal, CodeBackupFailed,
				"removing an old snapshot")
		}
		_ = os.Remove(manifestPath(candidate.Path))
		removed++
	}
	return removed, nil
}

// ── the file layer ──────────────────────────────────────────────────────────────

func manifestPath(dbPath string) string { return dbPath + ".json" }

func writeManifest(dbPath string, manifest Manifest) error {
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeManifestFailed, "encoding the manifest")
	}
	if err = os.WriteFile(manifestPath(dbPath), encoded, 0o600); err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeManifestFailed, "writing the manifest")
	}
	return nil
}

func readManifest(dbPath string) (Manifest, error) {
	encoded, err := os.ReadFile(manifestPath(dbPath))
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err = json.Unmarshal(encoded, &manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// integrityCheck runs the engine's own check and reports every problem it names.
//
// An empty statement means the engine offers none (PostgreSQL, MySQL), and the step is SKIPPED
// rather than faked — 0.7's decision, carried over unchanged.
func integrityCheck(ctx context.Context, db *sql.DB, statement string) error {
	if statement == "" {
		return nil
	}

	rows, err := db.QueryContext(ctx, statement)
	if err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeCorrupt, "running the integrity check")
	}
	defer func() { _ = rows.Close() }()

	var problems []string
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			return errs.Wrap(err, errs.CategoryInternal, CodeCorrupt,
				"reading the integrity check")
		}
		if !strings.EqualFold(strings.TrimSpace(line), "ok") {
			problems = append(problems, line)
		}
	}
	if err = rows.Err(); err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeCorrupt, "reading the integrity check")
	}
	if len(problems) > 0 {
		return errs.Conflict(CodeCorrupt,
			"that database failed its integrity check: "+strings.Join(problems, "; "))
	}
	return nil
}

// fileDSN builds a file: DSN with the path properly encoded.
//
// Carried over from 0.7 with its reasoning: an earlier version escaped only spaces, which
// silently broke on '?', '#' and '%' — all legal in macOS and Windows paths, and '?' in
// particular would have been read as the start of the DSN's query string.
func fileDSN(path string) string {
	u := url.URL{Scheme: "file", Opaque: (&url.URL{Path: path}).EscapedPath()}
	return u.String()
}

// isMissingTable reports whether an error is "that table does not exist".
//
// Matched on the message because `modernc.org/sqlite` returns no typed error for it. Narrow
// enough not to swallow a genuine failure: a corrupt file gives a different message entirely.
func isMissingTable(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "no such table")
}
