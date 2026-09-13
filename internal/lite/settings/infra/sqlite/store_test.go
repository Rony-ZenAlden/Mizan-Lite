package sqlite_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/lite/settings"
	"github.com/mizan-erp/mizan/internal/lite/settings/domain"
	"github.com/mizan-erp/mizan/internal/lite/settings/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/settings/settingstest"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

func TestSQLiteMeetsTheStoreContract(t *testing.T) {
	settingstest.StoreContract(t, func(t *testing.T) settings.Store {
		return sqlite.NewStore(litetest.OpenMigrated(t))
	})
}

// TestAFailedUpdateRollsBackEveryWrite runs the service over a REAL transaction. The fake's
// Immediate transactor cannot prove atomicity; only the database can.
func TestAFailedUpdateRollsBackEveryWrite(t *testing.T) {
	ctx := context.Background()
	db := litetest.OpenMigrated(t)
	store := sqlite.NewStore(db)
	svc := settings.NewService(db, store, clock.System(), litetest.Logger())

	injected := errors.New("fail after the write")
	err := db.Do(ctx, func(ctx context.Context) error {
		en := "en"
		if _, err := svc.Update(ctx, domain.Update{Locale: &en}); err != nil {
			return err
		}
		return injected
	})
	if !errors.Is(err, injected) {
		t.Fatalf("err = %v", err)
	}
	got, err := svc.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Locale != domain.Arabic {
		t.Fatalf("a rolled-back update survived: locale = %q", got.Locale)
	}
}

func TestUpdatePersistsAcrossReopening(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "shop.db")

	store := openMigratedAt(t, path)
	svc := settings.NewService(store, sqlite.NewStore(store), clock.System(), litetest.Logger())
	en := "en"
	if _, err := svc.Update(ctx, domain.Update{Locale: &en}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	if got := sqlite.PeekLocale(ctx, path, litetest.Logger()); got != domain.English {
		t.Fatalf("PeekLocale after reopening = %q, want en", got)
	}
}

func TestPeekLocale(t *testing.T) {
	ctx := context.Background()

	t.Run("no file is the default, and no file is created", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "absent.db")
		if got := sqlite.PeekLocale(ctx, path, litetest.Logger()); got != domain.Arabic {
			t.Fatalf("got %q", got)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("peeking created a database file: %v", err)
		}
	})

	t.Run("a database with no settings table is the default", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "old.db")
		store, err := database.Open(database.Config{Path: path})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.WriterPool().ExecContext(ctx, "CREATE TABLE unrelated (a INTEGER)"); err != nil {
			t.Fatal(err)
		}
		_ = store.Close()
		if got := sqlite.PeekLocale(ctx, path, litetest.Logger()); got != domain.Arabic {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("a migrated database with nothing stored is the default", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "fresh.db")
		_ = openMigratedAt(t, path).Close()
		if got := sqlite.PeekLocale(ctx, path, litetest.Logger()); got != domain.Arabic {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("a damaged stored value is the default", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "damaged.db")
		store := openMigratedAt(t, path)
		if err := sqlite.NewStore(store).Save(ctx, domain.KeyLocale, "not-a-locale", time.Now()); err != nil {
			t.Fatal(err)
		}
		_ = store.Close()
		if got := sqlite.PeekLocale(ctx, path, litetest.Logger()); got != domain.Arabic {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("a file that is not a database is the default", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "garbage.db")
		if err := os.WriteFile(path, []byte("this is not sqlite, it is a text file of some length"), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := sqlite.PeekLocale(ctx, path, litetest.Logger()); got != domain.Arabic {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("it releases the file, so the database can be moved afterwards", func(t *testing.T) {
		// Meaningful on Windows, where a file still held open cannot be renamed — which is exactly
		// what a staged restore does to the live database before boot opens it.
		dir := filepath.Join(t.TempDir(), "محمد #1")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "shop.db")
		_ = openMigratedAt(t, path).Close()
		_ = sqlite.PeekLocale(ctx, path, litetest.Logger())
		if err := os.Rename(path, path+".moved"); err != nil {
			t.Fatalf("the database could not be moved after peeking: %v", err)
		}
	})
}

// openMigratedAt is litetest.OpenMigrated at a chosen path, for tests that reopen the file.
func openMigratedAt(t *testing.T, path string) *database.Store {
	t.Helper()
	return litetest.OpenMigratedAt(t, path)
}
