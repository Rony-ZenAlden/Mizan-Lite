package backups_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/backups"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/platform/backup"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

var ctx = context.Background()

type db struct{ s *database.Store }

func (d db) WriterPool() *sql.DB     { return d.s.WriterPool() }
func (d db) Dialect() backup.Dialect { return d.s.Dialect() }

type settings struct{ folder string }

func (s *settings) BackupFolder(context.Context) (string, error) { return s.folder, nil }

type gate struct {
	elevated bool
	acts     []string
}

func (g *gate) Require(_ context.Context, act backups.GuardedAct) error {
	if !g.elevated {
		return errs.Permission(backups.CodeOwnerRequired, "owner")
	}
	g.acts = append(g.acts, act.Action)
	return nil
}

func (g *gate) Allowed(context.Context) bool { return g.elevated }

type activity struct{ asked time.Time }

func (a *activity) Since(_ context.Context, at time.Time) (backups.Loss, error) {
	a.asked = at
	return backups.Loss{Sales: 214, Voids: 2, DebtEntries: 9, CashEntries: 3, StockMovements: 31}, nil
}

type fixture struct {
	svc   *backups.Service
	snaps *backup.Service
	set   *settings
	gate  *gate
	act   *activity
	clk   *clock.Fixed
	dir   string
	live  string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	root := t.TempDir()
	live := filepath.Join(root, "lite.db")
	store := litetest.OpenMigratedAt(t, live)
	f := fixture{set: &settings{}, gate: &gate{}, act: &activity{}, clk: clock.NewFixed(time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)),
		dir: filepath.Join(root, "backups"), live: live}
	f.snaps = backup.New(db{store}, backup.Options{Dir: f.dir, Clock: f.clk, AppVersion: "test"})
	f.svc = backups.NewService(f.snaps, f.set, f.gate, f.act, f.clk, backups.Config{LivePath: live, SchemaVersion: litetest.LatestSchemaVersion(t),
		IntegrityStatement: store.Dialect().IntegrityCheckStatement(), BackupDir: f.dir})
	return f
}

func TestAnOutsideCopyIsVerifiedNotAssumed(t *testing.T) {
	f := newFixture(t)
	usb := t.TempDir()
	f.set.folder = usb
	e, err := f.svc.TakeNow(ctx)
	if err != nil || !e.Outside || e.Reason != backup.OnDemand {
		t.Fatalf("%+v %v", e, err)
	}
	copied := filepath.Join(usb, "Mizan Lite backups", e.Name)
	if _, err = backup.Verify(ctx, copied, "PRAGMA integrity_check"); err != nil {
		t.Fatalf("the outside copy does not open: %v", err)
	}
	raw, _ := os.ReadFile(copied + ".json")
	var m backup.Manifest
	if json.Unmarshal(raw, &m) != nil || m.Reason != backup.OnDemand || m.TakenAt == "" {
		t.Fatalf("the copy's manifest: %s", raw)
	}
	list, _ := f.svc.List(ctx)
	if len(list) != 1 || !list[0].Outside {
		t.Fatalf("list %+v", list)
	}
	// A backup that does not verify is not left in the outside folder.
	broken := filepath.Join(t.TempDir(), "broken.db")
	_ = os.WriteFile(broken, []byte("not a database"), 0o600)
	err = f.svc.AfterBackup(ctx, backup.Backup{Path: broken, Name: "broken.db"})
	if errs.CodeOf(err) != backups.CodeCopyFailed {
		t.Fatalf("an unverifiable copy: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(usb, "Mizan Lite backups", "broken.db")); !os.IsNotExist(statErr) {
		t.Fatal("the unverified copy stayed")
	}
	if st, _ := f.svc.Status(ctx); st.OutsideFailed != backups.CodeCopyFailed {
		t.Fatalf("status after a failed copy: %+v", st)
	}
}

func TestAMissingOutsideFolderWarnsAndStillBacksUp(t *testing.T) {
	f := newFixture(t)
	st, _ := f.svc.Status(ctx)
	if st.Last != nil || st.Folder != "" || st.OutsideStale || st.LastOutside != nil {
		t.Fatalf("a fresh shop without a folder: %+v", st)
	}
	if _, err := f.svc.TakeNow(ctx); err != nil {
		t.Fatal(err)
	}
	if st, _ = f.svc.Status(ctx); st.Last == nil || st.OutsideFailed != "" {
		t.Fatalf("no folder chosen is not a failure: %+v", st)
	}
	f.set.folder = filepath.Join(t.TempDir(), "USB stick not inserted")
	f.clk.Advance(time.Minute) // backups are named by the second
	e, err := f.svc.TakeNow(ctx)
	if err != nil || e.Outside {
		t.Fatalf("the backup is taken even with the stick out: %+v %v", e, err)
	}
	st, _ = f.svc.Status(ctx)
	if !st.OutsideStale || st.OutsideFailed != backups.CodeFolderMissing || st.LastOutside != nil || len(mustList(t, f)) != 2 {
		t.Fatalf("status %+v", st)
	}
	// Plugged in, copied; two days later, stale.
	f.set.folder = t.TempDir()
	f.clk.Advance(time.Minute)
	if _, err = f.svc.TakeNow(ctx); err != nil {
		t.Fatal(err)
	}
	if st, _ = f.svc.Status(ctx); st.OutsideStale || st.LastOutside == nil || st.OutsideFailed != "" {
		t.Fatalf("fresh copy: %+v", st)
	}
	f.clk.Advance(49 * time.Hour)
	if st, _ = f.svc.Status(ctx); !st.OutsideStale {
		t.Fatal("a copy two days old is not stale")
	}
}

func mustList(t *testing.T, f fixture) []backups.Entry {
	t.Helper()
	l, err := f.svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestKeepingRulesInTheOutsideFolder(t *testing.T) {
	f := newFixture(t)
	f.set.folder = t.TempDir()
	for range 9 {
		f.clk.Advance(time.Hour)
		if _, err := f.svc.TakeNow(ctx); err != nil {
			t.Fatal(err)
		}
	}
	copies, _ := filepath.Glob(filepath.Join(f.set.folder, "Mizan Lite backups", "*.db"))
	if len(copies) != 7 {
		t.Fatalf("%d copies kept outside, want the newest 7", len(copies))
	}
}

func TestTheLossPreviewCountsWhatARestoreRemoves(t *testing.T) {
	f := newFixture(t)
	e, _ := f.svc.TakeNow(ctx)
	if _, err := f.svc.LossPreview(ctx, e.Name); errs.CodeOf(err) != backups.CodeOwnerRequired {
		t.Fatalf("a preview outside owner mode: %v", err)
	}
	f.gate.elevated = true
	p, err := f.svc.LossPreview(ctx, e.Name)
	if err != nil || p.Loss.Sales != 214 || !f.act.asked.Equal(e.TakenAt) || p.Backup.Name != e.Name {
		t.Fatalf("%+v %v", p, err)
	}
	if len(f.gate.acts) != 0 {
		t.Fatal("looking at a preview is recorded as an act")
	}
	if _, err = f.svc.LossPreview(ctx, "../lite.db"); errs.CodeOf(err) != backup.CodeUnknownBackup {
		t.Fatalf("a path for a name: %v", err)
	}
}

func TestARestoreIsStagedWithASafetySnapshot(t *testing.T) {
	f := newFixture(t)
	e, _ := f.svc.TakeNow(ctx)
	if _, err := f.svc.Restore(ctx, e.Name); errs.CodeOf(err) != backups.CodeOwnerRequired {
		t.Fatalf("restore outside owner mode: %v", err)
	}
	f.gate.elevated = true
	f.clk.Advance(time.Minute)
	intent, err := f.svc.Restore(ctx, e.Name)
	if err != nil || intent.From != e.Name || intent.SafetyBackup == "" {
		t.Fatalf("%+v %v", intent, err)
	}
	if pending, staged, _ := backup.PendingIntent(f.live); !staged || pending.From != e.Name {
		t.Fatal("nothing staged")
	}
	if list := mustList(t, f); list[0].Reason != backup.BeforeRestore {
		t.Fatalf("the safety snapshot is the newest: %+v", list)
	}
	if f.gate.acts[0] != backups.ActRestore {
		t.Fatal("the restore is not an owner's act")
	}
}

func TestANewerBackupIsRefused(t *testing.T) {
	f := newFixture(t)
	f.gate.elevated = true
	e, _ := f.svc.TakeNow(ctx)
	path := filepath.Join(f.dir, e.Name+".json")
	raw, _ := os.ReadFile(path)
	var m backup.Manifest
	_ = json.Unmarshal(raw, &m)
	store, err := database.Open(database.Config{Path: filepath.Join(f.dir, e.Name)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Writer(ctx).ExecContext(ctx, `INSERT INTO schema_migrations (version, name, checksum, applied_at) SELECT MAX(version) + 1, 'from the future', 'x', 't' FROM schema_migrations`); err != nil {
		t.Fatalf("stamping a newer version: %v", err)
	}
	_ = store.Close()
	if _, err = f.svc.Restore(ctx, e.Name); errs.CodeOf(err) != backup.CodeTooNew {
		t.Fatalf("a newer backup: %v", err)
	}
}

func TestImportAndSaveCopy(t *testing.T) {
	f := newFixture(t)
	e, _ := f.svc.TakeNow(ctx)
	source := filepath.Join(f.dir, e.Name)
	elsewhere := filepath.Join(t.TempDir(), "from the shop laptop.db")
	if err := f.svc.SaveCopy(ctx, e.Name, elsewhere); errs.CodeOf(err) != backups.CodeOwnerRequired {
		t.Fatalf("save a copy outside owner mode: %v", err)
	}
	f.gate.elevated = true
	if err := f.svc.SaveCopy(ctx, e.Name, elsewhere); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Verify(ctx, elsewhere, "PRAGMA integrity_check"); err != nil {
		t.Fatal(err)
	}
	f.clk.Advance(time.Hour)
	imported, err := f.svc.Import(ctx, elsewhere)
	if err != nil || imported.Reason != backups.ReasonImported || !imported.TakenAt.Equal(e.TakenAt) || !strings.HasPrefix(imported.Name, "imported-") {
		t.Fatalf("%+v %v (source %s)", imported, err, source)
	}
	garbage := filepath.Join(t.TempDir(), "photo.db")
	_ = os.WriteFile(garbage, []byte("JFIF"), 0o600)
	if _, err = f.svc.Import(ctx, garbage); errs.CodeOf(err) != backups.CodeNotABackup {
		t.Fatalf("a file that is not a backup: %v", err)
	}
	if list := mustList(t, f); len(list) != 2 {
		t.Fatalf("the refused file was listed: %+v", list)
	}
}
