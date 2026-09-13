package database_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mizan-erp/mizan/internal/platform/database"
)

// TestTheDatabaseOpensAtExactlyThePathGiven
//
// # The defect this pins
//
// The DSN is a `file:` URI. Until this test existed only spaces were escaped, so a data directory
// containing `#` opened a database at a TRUNCATED path with no error at all, and one containing
// `%` plus two hex digits could not be opened. Windows permits both characters in a user name, and
// the data directory lives under the user's profile.
//
// # Why it reopens rather than only checking the file exists
//
// Existence alone would pass if the store wrote to the right file AND to a truncated one. Writing
// a row, closing, reopening at the same path and reading the row back proves one file carried it.
func TestTheDatabaseOpensAtExactlyThePathGiven(t *testing.T) {
	names := []string{
		"plain",
		"with space",
		"محمد",       // an Arabic profile name, the common case for this product
		"O'Brien",    // an apostrophe, which VACUUM INTO must also survive
		"Shop#1",     // truncated the path silently
		"100%AB",     // decoded as an escape and failed to open
		"already%20", // a literal "%20" in a name must not collapse to a space
		"semi;colon",
		"amp&ersand",
	}
	if runtime.GOOS != "windows" {
		// `?` cannot appear in a Windows file name, so the case only exists elsewhere.
		names = append(names, "why?")
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), name)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatalf("creating %q: %v", dir, err)
			}
			path := filepath.Join(dir, "shop.db")
			ctx := context.Background()

			first := openAt(t, path)
			if _, err := first.WriterPool().ExecContext(ctx,
				"CREATE TABLE marker (value TEXT NOT NULL)"); err != nil {
				t.Fatalf("creating a table: %v", err)
			}
			if _, err := first.WriterPool().ExecContext(ctx,
				"INSERT INTO marker (value) VALUES ('here')"); err != nil {
				t.Fatalf("writing a row: %v", err)
			}
			if err := first.Close(); err != nil {
				t.Fatalf("closing: %v", err)
			}

			if _, err := os.Stat(path); err != nil {
				t.Fatalf("no database file at the requested path %q: %v", path, err)
			}

			second := openAt(t, path)
			defer func() { _ = second.Close() }()
			var value string
			if err := second.Reader(ctx).QueryRowContext(ctx,
				"SELECT value FROM marker").Scan(&value); err != nil {
				t.Fatalf("reopening %q did not find the row written before closing: %v", path, err)
			}
			if value != "here" {
				t.Fatalf("read %q, want %q", value, "here")
			}
		})
	}
}

func openAt(t *testing.T, path string) *database.Store {
	t.Helper()
	store, err := database.Open(database.Config{Path: path})
	if err != nil {
		t.Fatalf("opening %q: %v", path, err)
	}
	return store
}
