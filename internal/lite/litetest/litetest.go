// Package litetest opens real, migrated Mizan Lite databases for tests.
//
// Tests run against a real SQLite FILE through platform/database — never :memory:, because WAL
// mode requires a file and a test that cannot exercise the journal cannot find a journal bug.
// Each call gets its own directory, so tests are isolated and can run in parallel.
package litetest

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/migrations"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
)

// Logger discards output. Tests that assert on logs build their own handler.
func Logger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// OpenMigrated returns a store over a fresh database with every Lite migration applied, closed
// when the test ends.
func OpenMigrated(t testing.TB) *database.Store {
	t.Helper()
	return OpenMigratedAt(t, filepath.Join(t.TempDir(), "lite-test.db"))
}

// OpenMigratedAt is OpenMigrated at a chosen path, for tests that close and reopen the file. The
// store is also closed when the test ends; closing it earlier is harmless.
func OpenMigratedAt(t testing.TB, path string) *database.Store {
	t.Helper()
	store, err := database.Open(database.Config{Path: path})
	if err != nil {
		t.Fatalf("opening a test database: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	runner, err := migrate.New(store, migrate.Options{
		FS:     migrations.SQLite(),
		DBPath: path,
		// The safety snapshot is the migration runner's own concern and is tested there; a test
		// database is disposable by construction.
		SkipBackup: true,
		Logger:     Logger(),
	})
	if err != nil {
		t.Fatalf("preparing migrations: %v", err)
	}
	if _, err := runner.Up(context.Background()); err != nil {
		t.Fatalf("migrating a test database: %v", err)
	}
	return store
}

// LatestSchemaVersion is the version a fully migrated Lite database is at: the highest migration this
// build carries. Tests compare against it rather than a literal, so adding a phase's migration does not
// turn every schema assertion into a false failure.
func LatestSchemaVersion(t testing.TB) int64 {
	t.Helper()
	loaded, err := migrate.Load(migrations.SQLite())
	if err != nil {
		t.Fatalf("loading migrations: %v", err)
	}
	if len(loaded) == 0 {
		t.Fatal("no migrations")
	}
	return loaded[len(loaded)-1].Version
}

// Immediate is a transactor that runs fn with no transaction, for service tests over an in-memory fake.
// Atomicity is never proven through it — only against a real database.
type Immediate struct{}

// Do runs fn.
func (Immediate) Do(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }
