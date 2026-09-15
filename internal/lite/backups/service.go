// Package backups keeps the shop's data safe away from its computer and brings a backup back (L7 §6): it lists the verified
// snapshots platform/backup takes, copies each to an outside folder the owner picks and verifies the copy, says how old the
// last outside copy is, counts what a restore would remove, and stages a restore for the next start.
package backups

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/platform/backup"
)

// Stable error codes. They double as i18n keys.
const (
	CodeNoFolder      = "lite.backups.no_folder"
	CodeFolderMissing = "lite.backups.folder_missing"
	CodeCopyFailed    = "lite.backups.copy_failed"
	CodeNotABackup    = "lite.backups.not_a_backup"
	CodeOwnerRequired = "lite.owner.required"
)

// ReasonImported files a backup brought in from a file.
const ReasonImported = backup.Reason("imported")

const outsideSubdirectory = "Mizan Lite backups"

// The owner-only acts (§10).
const (
	ActRestore  = "backups.restore"
	ActSaveCopy = "backups.save_copy"
	ActImport   = "backups.import"
)

// OutsideStale is how old the last outside copy may be before the shop is warned (D-L7.14).
const OutsideStale = 48 * time.Hour

// Snapshots is what the module needs of platform/backup.
type Snapshots interface {
	Take(ctx context.Context, reason backup.Reason) (backup.Backup, error)
	List() ([]backup.Backup, error)
	Find(name string) (backup.Backup, error)
	Prepare(ctx context.Context, name, livePath string, binaryVersion int64) (backup.Intent, error)
}

// Settings is where the outside folder is kept.
type Settings interface {
	BackupFolder(ctx context.Context) (string, error)
}

// OwnerGate is what the module needs of the owner.
type OwnerGate interface {
	Require(ctx context.Context, act GuardedAct) error
	Allowed(ctx context.Context) bool
}

// GuardedAct describes an owner-only act.
type GuardedAct struct {
	Action string
	Before string
	After  string
}

// Activity counts what was recorded after an instant: what a restore to a backup of that instant removes (L7 §6.4).
type Activity interface {
	Since(ctx context.Context, at time.Time) (Loss, error)
}

// Loss is what a restore would remove.
type Loss struct {
	Sales          int
	Voids          int
	DebtEntries    int
	CashEntries    int
	StockMovements int
}

// Config is fixed for a running graph.
type Config struct {
	LivePath           string
	SchemaVersion      int64
	IntegrityStatement string
	BackupDir          string
}

// Service is the backups use cases.
type Service struct {
	snaps    Snapshots
	settings Settings
	gate     OwnerGate
	activity Activity
	clk      clock.Clock
	cfg      Config

	mu          sync.Mutex
	lastFailure string // the last outside copy's failure code, "" after a success
}

// NewService builds the service.
func NewService(snaps Snapshots, settings Settings, gate OwnerGate, activity Activity, clk clock.Clock, cfg Config) *Service {
	return &Service{snaps: snaps, settings: settings, gate: gate, activity: activity, clk: clk, cfg: cfg}
}

// Entry is a backup as listed.
type Entry struct {
	Name          string
	Reason        backup.Reason
	TakenAt       time.Time
	SizeBytes     int64
	SchemaVersion int64
	Outside       bool
}

// List is every local backup, newest first, each marked when a verified copy is in the outside folder.
func (s *Service) List(ctx context.Context) ([]Entry, error) {
	found, err := s.snaps.List()
	if err != nil {
		return nil, err
	}
	outside := map[string]bool{}
	if dir, derr := s.outsideDir(ctx); derr == nil {
		for _, b := range listDir(dir) {
			outside[b.Name] = true
		}
	}
	out := make([]Entry, 0, len(found))
	for _, b := range found {
		out = append(out, entry(b, outside[b.Name]))
	}
	return out, nil
}

func entry(b backup.Backup, outside bool) Entry {
	at, _ := time.Parse(time.RFC3339, b.Manifest.TakenAt)
	return Entry{Name: b.Name, Reason: b.Manifest.Reason, TakenAt: at, SizeBytes: b.Manifest.SizeBytes, SchemaVersion: b.Manifest.SchemaVersion, Outside: outside}
}

// Status is what Home and the Backups screen say about safety.
type Status struct {
	Last          *Entry
	Folder        string
	LastOutside   *Entry
	OutsideStale  bool
	OutsideFailed string // the code of the last failed outside copy, "" when the last one succeeded
}

// Status reports the newest backup and the newest outside copy.
func (s *Service) Status(ctx context.Context) (Status, error) {
	found, err := s.snaps.List()
	if err != nil {
		return Status{}, err
	}
	var st Status
	if len(found) > 0 {
		e := entry(found[0], false)
		st.Last = &e
	}
	if st.Folder, err = s.settings.BackupFolder(ctx); err != nil {
		return Status{}, err
	}
	if st.Folder != "" {
		if copies := listDir(filepath.Join(st.Folder, outsideSubdirectory)); len(copies) > 0 {
			e := entry(copies[0], true)
			st.LastOutside = &e
		}
		st.OutsideStale = st.LastOutside == nil || s.clk.Now().Sub(st.LastOutside.TakenAt) > OutsideStale
	}
	s.mu.Lock()
	st.OutsideFailed = s.lastFailure
	s.mu.Unlock()
	return st, nil
}

// TakeNow takes a backup on demand and copies it outside. Anyone may: a backup can only help (§10).
func (s *Service) TakeNow(ctx context.Context) (Entry, error) {
	b, err := s.snaps.Take(ctx, backup.OnDemand)
	if err != nil {
		return Entry{}, err
	}
	return entry(b, s.AfterBackup(ctx, b) == nil), nil
}

// AfterBackup copies a backup to the outside folder and verifies the copy; a missing folder is remembered for the status and
// never fails the backup itself. It returns the copy's failure, if any.
func (s *Service) AfterBackup(ctx context.Context, b backup.Backup) error {
	err := s.copyOutside(ctx, b)
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case err == nil:
		s.lastFailure = ""
	case errs.CodeOf(err) == CodeNoFolder:
		s.lastFailure = ""
	default:
		s.lastFailure = errs.CodeOf(err)
	}
	return err
}

func (s *Service) outsideDir(ctx context.Context) (string, error) {
	folder, err := s.settings.BackupFolder(ctx)
	if err != nil {
		return "", err
	}
	if folder == "" {
		return "", errs.Conflict(CodeNoFolder, "no outside folder is chosen")
	}
	return filepath.Join(folder, outsideSubdirectory), nil
}

func (s *Service) copyOutside(ctx context.Context, b backup.Backup) error {
	dir, err := s.outsideDir(ctx)
	if err != nil {
		return err
	}
	if info, statErr := os.Stat(filepath.Dir(dir)); statErr != nil || !info.IsDir() {
		return errs.Conflict(CodeFolderMissing, "the outside folder cannot be reached").WithParam("folder", filepath.Dir(dir))
	}
	if err = os.MkdirAll(dir, 0o750); err != nil {
		return errs.Wrap(err, errs.CategoryConflict, CodeCopyFailed, "creating the outside folder")
	}
	if err = s.copyVerified(ctx, b.Path, filepath.Join(dir, b.Name), &b.Manifest); err != nil {
		return err
	}
	return pruneDir(dir, 7)
}

// copyVerified copies a database file and, when manifest is not nil, its manifest; the copy is opened and integrity-checked,
// not trusted, and removed when it fails (D-L7.14).
func (s *Service) copyVerified(ctx context.Context, from, to string, manifest *backup.Manifest) error {
	tmp := to + ".partial"
	if err := copyFile(from, tmp); err != nil {
		_ = os.Remove(tmp)
		return errs.Wrap(err, errs.CategoryConflict, CodeCopyFailed, "copying the backup")
	}
	if _, err := backup.Verify(ctx, tmp, s.cfg.IntegrityStatement); err != nil {
		_ = os.Remove(tmp)
		return errs.Wrap(err, errs.CategoryConflict, CodeCopyFailed, "the copy did not verify")
	}
	if err := os.Rename(tmp, to); err != nil {
		_ = os.Remove(tmp)
		return errs.Wrap(err, errs.CategoryConflict, CodeCopyFailed, "placing the copy")
	}
	if manifest == nil {
		return nil
	}
	raw, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(to+".json", raw, 0o600); err != nil {
		_ = os.Remove(to)
		return errs.Wrap(err, errs.CategoryConflict, CodeCopyFailed, "writing the copy's manifest")
	}
	return nil
}

// Preview is a restore's warning: the backup and what would be lost (L7 §6.4). Owner only.
type Preview struct {
	Backup Entry
	Loss   Loss
}

// LossPreview counts what restoring a backup would remove.
func (s *Service) LossPreview(ctx context.Context, name string) (Preview, error) {
	if !s.gate.Allowed(ctx) {
		return Preview{}, errs.Permission(CodeOwnerRequired, "the owner's PIN is required")
	}
	b, err := s.snaps.Find(name)
	if err != nil {
		return Preview{}, err
	}
	e := entry(b, false)
	loss, err := s.activity.Since(ctx, e.TakenAt)
	return Preview{Backup: e, Loss: loss}, err
}

// Restore stages a backup for the next start: owner PIN, a safety snapshot of what is replaced, the copy staged. The caller
// restarts the graph; bootstrap applies the staged file before opening the database (A-L7.5).
func (s *Service) Restore(ctx context.Context, name string) (backup.Intent, error) {
	b, err := s.snaps.Find(name)
	if err != nil {
		return backup.Intent{}, err
	}
	if err = s.gate.Require(ctx, GuardedAct{Action: ActRestore, Before: name, After: b.Manifest.TakenAt}); err != nil {
		return backup.Intent{}, err
	}
	return s.snaps.Prepare(ctx, name, s.cfg.LivePath, s.cfg.SchemaVersion)
}

// Import verifies a backup file from elsewhere — another computer, the outside folder — and lists it. Owner only.
func (s *Service) Import(ctx context.Context, path string) (Entry, error) {
	if err := s.gate.Require(ctx, GuardedAct{Action: ActImport, After: filepath.Base(path)}); err != nil {
		return Entry{}, err
	}
	manifest, err := backup.Verify(ctx, path, s.cfg.IntegrityStatement)
	if err != nil {
		return Entry{}, errs.Wrap(err, errs.CategoryValidation, CodeNotABackup, "the file is not a Mizan Lite backup").WithParam("file", filepath.Base(path))
	}
	if sidecar, rerr := os.ReadFile(path + ".json"); rerr == nil {
		var original backup.Manifest
		if json.Unmarshal(sidecar, &original) == nil && original.TakenAt != "" {
			manifest.TakenAt, manifest.AppVersion = original.TakenAt, original.AppVersion
		}
	}
	if manifest.TakenAt == "" {
		manifest.TakenAt = s.clk.Now().UTC().Format(time.RFC3339)
	}
	manifest.Reason = ReasonImported
	name := string(ReasonImported) + "-" + s.clk.Now().UTC().Format("20060102T150405Z") + ".db"
	if err = os.MkdirAll(s.cfg.BackupDir, 0o750); err != nil {
		return Entry{}, errs.Wrap(err, errs.CategoryInternal, CodeCopyFailed, "creating the backup folder")
	}
	if err = s.copyVerified(ctx, path, filepath.Join(s.cfg.BackupDir, name), &manifest); err != nil {
		return Entry{}, err
	}
	b, err := s.snaps.Find(name)
	if err != nil {
		return Entry{}, err
	}
	return entry(b, false), nil
}

// SaveCopy writes a verified copy of a backup where the owner chose. Owner only: a backup is the whole business.
func (s *Service) SaveCopy(ctx context.Context, name, destination string) error {
	b, err := s.snaps.Find(name)
	if err != nil {
		return err
	}
	if err = s.gate.Require(ctx, GuardedAct{Action: ActSaveCopy, Before: name, After: filepath.Base(destination)}); err != nil {
		return err
	}
	return s.copyVerified(ctx, b.Path, destination, &b.Manifest)
}

// listDir reads the backups in a folder by their manifests, newest first.
func listDir(dir string) []backup.Backup {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []backup.Backup
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".db") {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(dir, e.Name()+".json"))
		var m backup.Manifest
		if rerr != nil || json.Unmarshal(raw, &m) != nil {
			continue
		}
		out = append(out, backup.Backup{Path: filepath.Join(dir, e.Name()), Manifest: m, Name: e.Name()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.TakenAt > out[j].Manifest.TakenAt })
	return out
}

// pruneDir keeps the newest keep copies of each reason in the outside folder; the newest of each is never removed.
func pruneDir(dir string, keep int) error {
	seen := map[backup.Reason]int{}
	for _, b := range listDir(dir) {
		seen[b.Manifest.Reason]++
		if seen[b.Manifest.Reason] <= max(keep, 1) {
			continue
		}
		if err := os.Remove(b.Path); err != nil {
			return errs.Wrap(err, errs.CategoryConflict, CodeCopyFailed, "pruning the outside folder")
		}
		_ = os.Remove(b.Path + ".json")
	}
	return nil
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err = out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
