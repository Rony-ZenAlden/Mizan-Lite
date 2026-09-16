package bootstrap_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/lite/owner/ownertest"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
)

// latestSchema is the schema this build migrates to. A new migration fails TestEveryPastSchemaUpgradesToThisRelease until its
// phase's fixture is generated (scripts/lite-schema-fixtures.sh) — a new schema with no shop written by the phase before it is
// an upgrade nobody tested.
const latestSchema = 8

// fixtureShop copies a past schema's shop into a fresh data directory. snapshot reads every table's rows over the columns given
// for it (all its columns when none are given), so a rebuilt table (L4's stock_ledger gained columns) is compared on the columns
// it had.
func fixtureShop(t *testing.T, schema int) (string, func(columns map[string][]string) (map[string]string, map[string][]string)) {
	t.Helper()
	p := dataDir(t)
	body, err := os.ReadFile(filepath.Join("testdata", "schemas", fmt.Sprintf("schema-%d.db", schema)))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(p.DBFile, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return p.DBFile, func(columns map[string][]string) (map[string]string, map[string][]string) {
		return fingerprint(t, p.DBFile, columns)
	}
}

// fingerprint is every table's rows, in order, as text — bookkeeping tables left out.
func fingerprint(t *testing.T, file string, columns map[string][]string) (map[string]string, map[string][]string) {
	t.Helper()
	store, err := database.Open(database.Config{Path: file})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	tables := queryStrings(ctx, t, store, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		AND name NOT IN ('schema_migrations', 'schema_lock', 'jobs', 'job_runs') ORDER BY name`)
	out := map[string]string{}
	seen := map[string][]string{}
	for _, table := range tables {
		selected := "*"
		if cols, ok := columns[table]; ok {
			selected = strings.Join(cols, ", ")
		}
		text, cols := queryRows(ctx, t, store, "SELECT "+selected+" FROM "+table+" ORDER BY 1")
		seen[table] = cols
		out[table] = text
	}
	return out, seen
}

// queryStrings reads one text column.
func queryStrings(ctx context.Context, t *testing.T, store *database.Store, query string) []string {
	t.Helper()
	rows, err := store.Reader(ctx).QueryContext(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var found []string
	for rows.Next() {
		var name string
		if serr := rows.Scan(&name); serr != nil {
			t.Fatal(serr)
		}
		found = append(found, name)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return found
}

// queryRows reads every row as text, with the columns it read them over.
func queryRows(ctx context.Context, t *testing.T, store *database.Store, query string) (string, []string) {
	t.Helper()
	rows, err := store.Reader(ctx).QueryContext(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if serr := rows.Scan(ptrs...); serr != nil {
			t.Fatal(serr)
		}
		fmt.Fprintf(&b, "%v\n", values)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return b.String(), cols
}

// TestEveryPastSchemaUpgradesToThisRelease (L8 D-L8.6): a shop at every schema a released phase shipped — written by that phase's
// own code — opens in this build with every existing row identical, the new tables added, and its verifiers clean.
func TestEveryPastSchemaUpgradesToThisRelease(t *testing.T) {
	entries, err := filepath.Glob(filepath.Join("testdata", "schemas", "schema-*.db"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != latestSchema {
		t.Fatalf("%d fixture shops for %d schemas — run scripts/lite-schema-fixtures.sh", len(entries), latestSchema)
	}
	for schema := 1; schema <= latestSchema; schema++ {
		t.Run(fmt.Sprintf("schema-%d", schema), func(t *testing.T) {
			file, snapshot := fixtureShop(t, schema)
			before, columns := snapshot(nil)
			ctx := context.Background()
			p := dataDir(t)
			p.Data, p.DBFile, p.Backups = filepath.Dir(file), file, filepath.Join(filepath.Dir(file), "backups")
			app, startErr := bootstrap.Start(ctx, bootstrap.Options{Paths: p, Logger: litetest.Logger(), PINHasher: ownertest.Hasher()})
			if startErr != nil {
				t.Fatal(startErr)
			}
			if app.SchemaVersion != latestSchema {
				t.Fatalf("at schema %d after the upgrade", app.SchemaVersion)
			}
			for name, check := range map[string]func(context.Context) (int, error){
				"sales": func(ctx context.Context) (int, error) { f, err := app.Sales.VerifyUnguarded(ctx); return len(f), err },
				"stock": func(ctx context.Context) (int, error) { f, err := app.Stock.VerifyUnguarded(ctx); return len(f), err },
				"customers": func(ctx context.Context) (int, error) {
					f, err := app.Customers.VerifyUnguarded(ctx)
					return len(f), err
				},
			} {
				if n, verifyErr := check(ctx); verifyErr != nil || n != 0 {
					t.Errorf("%s verifier after the upgrade: %d findings, %v", name, n, verifyErr)
				}
			}
			if _, takeErr := app.Safety.TakeNow(ctx); takeErr != nil {
				t.Errorf("a backup after the upgrade: %v", takeErr)
			}
			if shutErr := app.Shutdown(ctx); shutErr != nil {
				t.Fatal(shutErr)
			}
			after, _ := snapshot(columns)
			var changed, kept []string
			for table, rows := range before {
				if after[table] != rows {
					changed = append(changed, table)
				}
				kept = append(kept, table)
			}
			sort.Strings(changed)
			if len(changed) > 0 {
				t.Errorf("rows changed by the upgrade in %v", changed)
			}
			if len(after) < len(kept) {
				t.Errorf("tables lost: %d before, %d after", len(kept), len(after))
			}
			for _, table := range []string{"print_jobs", "voucher_numbers", "cash_entries"} {
				if _, ok := after[table]; !ok {
					t.Errorf("%s missing after the upgrade", table)
				}
			}
		})
	}
}

// TestAnOlderBinaryRefusesANewerDatabase: a shop that a newer Lite has migrated is never opened, and never written, by this one.
func TestAnOlderBinaryRefusesANewerDatabase(t *testing.T) {
	file, snapshot := fixtureShop(t, latestSchema)
	db, err := sql.Open("sqlite", file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO schema_migrations (version, name, checksum, applied_at) VALUES (99, 'from_the_future', ?, '2027-01-01T00:00:00.000Z')`,
		strings.Repeat("0", 64)); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	before, columns := snapshot(nil)
	p := dataDir(t)
	p.Data, p.DBFile, p.Backups = filepath.Dir(file), file, filepath.Join(filepath.Dir(file), "backups")
	_, err = bootstrap.Start(context.Background(), bootstrap.Options{Paths: p, Logger: litetest.Logger(), PINHasher: ownertest.Hasher()})
	if !strings.Contains(errs.CodeOf(err)+" "+fmt.Sprint(err), migrate.CodeDatabaseTooNew) {
		t.Fatalf("a newer database opened: %v", err)
	}
	after, _ := snapshot(columns)
	for table, rows := range before {
		if after[table] != rows {
			t.Errorf("%s written by a refused start", table)
		}
	}
}
