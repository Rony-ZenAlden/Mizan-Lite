package migrations_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/migrations"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
)

// repoRoot walks up from this package to the directory holding go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

var (
	lineComment = regexp.MustCompile(`--[^\n]*`)
	whitespace  = regexp.MustCompile(`\s+`)
	// The table a statement is about: CREATE TABLE t, CREATE [UNIQUE] INDEX x ON t, INSERT INTO t.
	statementTarget = regexp.MustCompile(
		`^(?:CREATE TABLE (\w+)|CREATE (?:UNIQUE )?INDEX \w+ ON (\w+)|INSERT INTO (\w+))`)
)

// statementsFor returns the normalised statements of a migration that concern the given tables,
// in file order. Comments and whitespace are removed because they are documentation, not schema:
// Lite's copy explains its own provenance and must be free to say so.
func statementsFor(t *testing.T, sql string, tables map[string]bool) []string {
	t.Helper()
	var out []string
	for _, raw := range strings.Split(lineComment.ReplaceAllString(sql, ""), ";") {
		stmt := strings.TrimSpace(whitespace.ReplaceAllString(raw, " "))
		if stmt == "" {
			continue
		}
		m := statementTarget.FindStringSubmatch(stmt)
		if m == nil {
			continue
		}
		target := m[1] + m[2] + m[3]
		if tables[target] {
			out = append(out, stmt)
		}
	}
	return out
}

func TestPlatformTablesMatchMizan(t *testing.T) {
	root := repoRoot(t)
	mizan, err := os.ReadFile(filepath.Join(root, "migrations", "sqlite", "0001_platform.sql"))
	if err != nil {
		t.Fatalf("reading Mizan's platform migration: %v", err)
	}
	lite, err := fs.ReadFile(migrations.SQLite(), "0001_lite_platform.sql")
	if err != nil {
		t.Fatalf("reading Lite's platform migration: %v", err)
	}

	// The platform packages Lite reuses read and write exactly these.
	shared := map[string]bool{"schema_migrations": true, "schema_lock": true, "jobs": true, "job_runs": true}
	want := statementsFor(t, string(mizan), shared)
	got := statementsFor(t, string(lite), shared)

	// 4 tables + 3 indexes + the lock row. Asserted so a parser that matched nothing in EITHER file
	// cannot pass by comparing two empty lists.
	if len(want) != 8 {
		t.Fatalf("found %d shared statements in Mizan's migration, want 8 — the parser is not matching:\n%s",
			len(want), strings.Join(want, "\n"))
	}
	if len(got) != len(want) {
		t.Fatalf("Lite has %d shared statements, Mizan has %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("statement %d differs from Mizan's.\n Mizan: %s\n  Lite: %s", i+1, want[i], got[i])
		}
	}
}

func TestMigrationsLoadInContiguousOrder(t *testing.T) {
	loaded, err := migrate.Load(migrations.SQLite())
	if err != nil {
		t.Fatalf("the runner rejected Lite's migrations: %v", err)
	}
	if len(loaded) == 0 {
		t.Fatal("no migrations were loaded")
	}
	for i, m := range loaded {
		if m.Version != int64(i+1) {
			t.Fatalf("migration %d has version %d; versions must be contiguous from 1", i, m.Version)
		}
	}
}

// TestMigrationFilesUseLF and the .gitattributes check are two halves of one guarantee. The file
// check catches a CRLF that is already in the repository; the attribute check stops a Windows
// checkout from introducing one, which would change the bytes, change the checksum, and make a
// database created on a Mac refuse to start under a binary built on Windows.
func TestMigrationFilesUseLF(t *testing.T) {
	entries, err := fs.ReadDir(migrations.SQLite(), ".")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, e := range entries {
		raw, err := fs.ReadFile(migrations.SQLite(), e.Name())
		if err != nil {
			t.Fatal(err)
		}
		checked++
		if strings.Contains(string(raw), "\r") {
			t.Errorf("%s contains a carriage return; migrations are checksummed byte for byte", e.Name())
		}
	}
	if checked == 0 {
		t.Fatal("no migration files were checked")
	}
}

func TestGitattributesPinsSQLToLF(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), ".gitattributes"))
	if err != nil {
		t.Fatalf("reading .gitattributes: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "*.sql" {
			joined := strings.Join(fields[1:], " ")
			if strings.Contains(joined, "eol=lf") {
				return
			}
			t.Fatalf("*.sql is listed but not pinned to LF: %q", line)
		}
	}
	t.Fatal(".gitattributes does not pin *.sql to eol=lf")
}
