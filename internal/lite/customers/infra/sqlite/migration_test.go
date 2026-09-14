package sqlite_test

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/lite/migrations"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
)

// migrateTo runs the migrations numbered at most last.
func migrateTo(t *testing.T, db *database.Store, path string, last int) {
	t.Helper()
	subset := fstest.MapFS{}
	err := fs.WalkDir(migrations.SQLite(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		var version int
		if _, scanErr := fmt.Sscanf(name, "%04d_", &version); scanErr != nil || version > last {
			return nil //nolint:nilerr // not a numbered migration, or beyond the target
		}
		body, err := fs.ReadFile(migrations.SQLite(), name)
		subset[name] = &fstest.MapFile{Data: body}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := migrate.New(db, migrate.Options{FS: subset, DBPath: path, SkipBackup: true, Logger: litetest.Logger()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runner.Up(context.Background()); err != nil {
		t.Fatalf("migrating through %04d: %v", last, err)
	}
}

// snapshot is every row of every table, as text, table by table.
func snapshot(t *testing.T, db *database.Store) map[string]string {
	t.Helper()
	ctx := context.Background()
	rows, err := db.Reader(ctx).QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	_ = rows.Close()
	out := map[string]string{}
	for _, table := range tables {
		r, err := db.Reader(ctx).QueryContext(ctx, `SELECT * FROM `+table+` ORDER BY 1`)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := r.Columns()
		var b strings.Builder
		fmt.Fprintln(&b, strings.Join(cols, ","))
		for r.Next() {
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err = r.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintln(&b, values...)
		}
		_ = r.Close()
		out[table] = b.String()
	}
	return out
}

// TestTheMigrationAddsNothingToExistingTables: an L4 database with sales in it migrates to 0006, and every table it had
// — schema and rows — reads back exactly; the two new tables start empty (A-L4.2 paid for this: no rebuild).
func TestTheMigrationAddsNothingToExistingTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "l4.db")
	db, err := database.Open(database.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	migrateTo(t, db, path, 5)
	for range 3 {
		insertSale(t, db) // with its rate
	}
	before := snapshot(t, db)
	if !strings.Contains(before["sales"], "credit") {
		t.Fatal("the L4 database holds no sales; the comparison would prove nothing")
	}

	migrateTo(t, db, path, 6)
	after := snapshot(t, db)
	for table, rows := range before {
		if table == "schema_migrations" || table == "schema_lock" {
			continue
		}
		if after[table] != rows {
			t.Errorf("%s changed in the migration:\nbefore %s\nafter  %s", table, rows, after[table])
		}
	}
	for _, added := range []string{"customers", "debt_entries"} {
		if _, existed := before[added]; existed {
			t.Errorf("%s existed before 0006", added)
		}
		if lines := strings.Count(after[added], "\n"); lines != 1 {
			t.Errorf("%s holds %d rows after the migration", added, lines-1)
		}
	}
	var problems int
	if err = db.Reader(context.Background()).QueryRowContext(context.Background(), `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&problems); err != nil || problems != 0 {
		t.Fatalf("foreign key check: %d, %v", problems, err)
	}
}
