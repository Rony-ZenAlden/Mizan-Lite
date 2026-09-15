package sqlite_test

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"context"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/lite/migrations"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
)

func uid(t *testing.T) string {
	t.Helper()
	v, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	return v.String()
}

func insertRate(t *testing.T, db *database.Store) id.ID {
	t.Helper()
	rateID, _ := id.New()
	exec(t, db, `INSERT INTO fx_rates (id, local_currency, seq, local_per_usd_nano, source, business_date, recorded_at)
		VALUES (?, 'SYP', (SELECT COALESCE(MAX(seq), 0) + 1 FROM fx_rates), 15000000000000, 'manual', '2026-09-14', '2026-09-14T06:00:00.000Z')`, rateID.String())
	return rateID
}

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

// dump is every row of every table, as text.
func dump(t *testing.T, db *database.Store) map[string]string {
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
		var b strings.Builder
		var schema string
		_ = db.Reader(ctx).QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE name = ?`, table).Scan(&schema)
		fmt.Fprintln(&b, schema)
		r, err := db.Reader(ctx).QueryContext(ctx, `SELECT * FROM `+table+` ORDER BY 1`)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := r.Columns()
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

// TestTheMigrationAddsNothingToExistingTables: an L6 database with a sale, a customer, a debt and a cash book entry migrates to
// 0008, and every table it had — schema and rows — reads back exactly; the printing tables start empty.
func TestTheMigrationAddsNothingToExistingTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "l6.db")
	db, err := database.Open(database.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	migrateTo(t, db, path, 7)
	rate := insertRate(t, db)
	ctx := context.Background()
	customer := uid(t)
	for _, statement := range []struct {
		q    string
		args []any
	}{
		{`INSERT INTO sales (id, receipt_no, business_date, sold_at, status, payment, local_currency, fx_rate_id,
		local_per_usd_nano, rate_recorded_at, settlement_currency, lines_local_minor, lines_usd_minor, discount_local_minor,
		discount_usd_minor, cash_increment_minor, rounding_minor, total_minor, tendered_currency, tendered_minor,
		change_currency, change_minor, cost_usd_minor, shop_name_snapshot, created_at)
		VALUES (?, 1, '2026-09-14', '2026-09-14T08:00:00.000Z', 'posted', 'cash', 'SYP', ?, 15000000000000,
		'2026-09-14T06:00:00.000Z', 'SYP', 12000, 80, 0, 0, 500, 0, 12000, 'SYP', 12000, 'SYP', 0, 0, 'x', 'x')`, []any{uid(t), rate.String()}},
		{`INSERT INTO customers (id, name, name_key, created_at, updated_at) VALUES (?, 'أبو محمد', 'ابو محمد', 't', 't')`, []any{customer}},
		{`INSERT INTO cash_entries (id, seq, business_date, occurred_at, kind, currency, amount_minor, expected_minor, created_at)
		VALUES (?, 1, '2026-09-14', 't', 'count', 'SYP', 0, 0, 't')`, []any{uid(t)}},
		{`INSERT INTO debt_entries (id, customer_id, currency, seq, business_date, occurred_at, kind, amount_minor,
		balance_before_minor, balance_after_minor, customer_name_snapshot, created_at)
		VALUES (?, ?, 'USD', 1, '2026-09-14', 't', 'opening', 1500, 0, 1500, 'أبو محمد', 't')`, []any{uid(t), customer}},
	} {
		if _, err = db.Writer(ctx).ExecContext(ctx, statement.q, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	before := dump(t, db)
	if !strings.Contains(before["sales"], "posted") || !strings.Contains(before["debt_entries"], "opening") {
		t.Fatal("the L6 database holds no sale or debt; the comparison would prove nothing")
	}

	migrateTo(t, db, path, 8)
	after := dump(t, db)
	for table, rows := range before {
		if table == "schema_migrations" || table == "schema_lock" {
			continue
		}
		if after[table] != rows {
			t.Errorf("%s changed in the migration:\nbefore %s\nafter  %s", table, rows, after[table])
		}
	}
	var entries, problems int
	for _, added := range []string{"print_jobs", "voucher_numbers"} {
		if _, existed := before[added]; existed {
			t.Errorf("%s existed before 0008", added)
		}
		if err = db.Reader(ctx).QueryRowContext(ctx, `SELECT COUNT(*) FROM `+added).Scan(&entries); err != nil || entries != 0 {
			t.Errorf("%s holds %d rows after the migration: %v", added, entries, err)
		}
	}
	if err = db.Reader(ctx).QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&problems); err != nil || problems != 0 {
		t.Fatalf("foreign key check: %d, %v", problems, err)
	}
}
