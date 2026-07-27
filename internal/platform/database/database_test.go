package database_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/database/dbtest"
)

// newTestStore opens a Store against a fresh temp-file database, cleaned up after the
// (sub)test. WAL requires a real file, so an on-disk temp DB is used, not :memory:.
func newTestStore(t *testing.T) *database.Store {
	t.Helper()
	st, err := database.Open(database.Config{
		Path: filepath.Join(t.TempDir(), "mizan_test.db"),
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// TestSQLiteContract runs the full dialect-agnostic contract suite against SQLite.
// A future PostgreSQL dialect must pass this identical suite.
func TestSQLiteContract(t *testing.T) {
	dbtest.RunContractSuite(t, newTestStore)
}

func TestOpenRejectsEmptyPath(t *testing.T) {
	if _, err := database.Open(database.Config{}); err == nil {
		t.Fatal("empty path should error")
	}
}

func TestLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "life.db")
	st, err := database.Open(database.Config{Path: path})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// After close, connectivity checks must fail rather than silently succeed.
	if err := st.Ping(context.Background()); err == nil {
		t.Error("ping after close should fail")
	}
}
