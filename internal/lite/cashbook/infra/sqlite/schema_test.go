package sqlite_test

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/lite/migrations"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
)

// TestCashEntriesAreInsertOnly reads the store's source (L6 §8).
func TestCashEntriesAreInsertOnly(t *testing.T) {
	raw, err := os.ReadFile("store.go")
	if err != nil {
		t.Fatal(err)
	}
	src := strings.ToUpper(string(raw))
	if !strings.Contains(src, "INSERT INTO CASH_ENTRIES") {
		t.Fatal("the scan found no insert; it is not reading the store")
	}
	for _, forbidden := range []*regexp.Regexp{
		regexp.MustCompile(`UPDATE\s+`), regexp.MustCompile(`DELETE\s+FROM`), regexp.MustCompile(`REPLACE\s+INTO`), regexp.MustCompile(`ON\s+CONFLICT`),
	} {
		if forbidden.MatchString(src) {
			t.Errorf("the store writes what it must not: %s", forbidden)
		}
	}
}

type row map[string]any

func (r row) with(changes row) row {
	out := row{}
	for k, v := range r {
		out[k] = v
	}
	for k, v := range changes {
		out[k] = v
	}
	return out
}

func insert(db *database.Store, table string, r row) error {
	columns := make([]string, 0, len(r))
	for c := range r {
		columns = append(columns, c)
	}
	sort.Strings(columns)
	args := make([]any, len(columns))
	for i, c := range columns {
		args[i] = r[c]
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(columns)), ", ")
	_, err := db.Writer(context.Background()).ExecContext(context.Background(),
		`INSERT INTO `+table+` (`+strings.Join(columns, ", ")+`) VALUES (`+placeholders+`)`, args...)
	return err
}

func uid(t *testing.T) string {
	t.Helper()
	v, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	return v.String()
}

// TestACheckRefusesAnImpossibleCashEntry is L6 §8.1's verification, through the real migration runner: every valid row
// accepted, every impossible one refused by the constraint named — NULL cases included (PROGRESS O7).
func TestACheckRefusesAnImpossibleCashEntry(t *testing.T) {
	db := litetest.OpenMigrated(t)
	ctx := context.Background()
	rate := insertRate(t, db).String()
	seq := 0
	next := func() int { seq++; return seq }
	base := func(changes row) row {
		return row{"id": uid(t), "seq": next(), "business_date": "2026-09-14", "occurred_at": "t", "kind": "withdrawal", "currency": "SYP",
			"amount_minor": 100_000, "fx_rate_id": rate, "local_per_usd_nano": 15_000_000_000_000, "created_at": "t"}.with(changes)
	}
	expense := func(changes row) row {
		return base(row{"kind": "expense", "category": "rent", "from_drawer": 0}).with(changes)
	}
	count := func(changes row) row {
		return base(row{"kind": "count", "amount_minor": 0, "expected_minor": 5_000, "fx_rate_id": nil, "local_per_usd_nano": nil}).with(changes)
	}
	tx := func(fn func()) {
		if _, err := db.Writer(ctx).ExecContext(ctx, "SAVEPOINT t"); err != nil {
			t.Fatal(err)
		}
		fn()
		_, _ = db.Writer(ctx).ExecContext(ctx, "ROLLBACK TO t")
		_, _ = db.Writer(ctx).ExecContext(ctx, "RELEASE t")
	}

	tx(func() {
		first := expense(nil)
		zeroCount := count(nil)
		for _, good := range []struct {
			name string
			r    row
		}{
			{"an expense paid from the bank", first},
			{"an expense from the drawer", expense(row{"category": "electricity", "from_drawer": 1})},
			{"a withdrawal", base(nil)},
			{"a dollar deposit", base(row{"kind": "deposit", "currency": "USD", "amount_minor": 5_000})},
			{"a count of zero", zeroCount},
			{"a reversal with its reason", base(row{"kind": "reversal", "reverses_id": first["id"], "note": "خطأ", "fx_rate_id": nil, "local_per_usd_nano": nil})},
			{"a reversal of a zero count (A building decision: it copies the zero)", base(row{"kind": "reversal", "amount_minor": 0,
				"reverses_id": zeroCount["id"], "note": "عدّ خطأ", "fx_rate_id": nil, "local_per_usd_nano": nil})},
		} {
			if err := insert(db, "cash_entries", good.r); err != nil {
				t.Errorf("%s refused: %v", good.name, err)
			}
		}
	})

	for _, c := range []struct {
		name, constraint string
		r                row
	}{
		{"a zero expense", "ck_cash_money_is_positive", expense(row{"amount_minor": 0})},
		{"a negative withdrawal", "ck_cash_money_is_positive", base(row{"amount_minor": -1})},
		{"a negative count", "ck_cash_", count(row{"amount_minor": -1})},
		{"a count with no expected figure (NULL)", "ck_cash_count_is_complete", count(row{"expected_minor": nil})},
		{"an expense carrying an expected figure", "ck_cash_count_is_complete", expense(row{"expected_minor": 5})},
		{"an expense with no category (NULL)", "ck_cash_expense_is_complete", expense(row{"category": nil})},
		{"an unknown category", "ck_cash_expense_is_complete", expense(row{"category": "bribes"})},
		{"an expense with no from-drawer (NULL)", "ck_cash_expense_is_complete", expense(row{"from_drawer": nil})},
		{"from_drawer = 2", "from_drawer IN (0, 1)", expense(row{"from_drawer": 2})},
		{"a withdrawal with a category", "ck_cash_expense_is_complete", base(row{"category": "rent"})},
		{"a reversal of nothing", "ck_cash_reversal_names_its_entry", base(row{"kind": "reversal", "note": "x", "fx_rate_id": nil, "local_per_usd_nano": nil})},
		{"an expense with no rate (NULL)", "ck_cash_rate_on_money", expense(row{"fx_rate_id": nil})},
		{"an expense with a NULL rate value (NULL)", "ck_cash_rate_on_money", expense(row{"local_per_usd_nano": nil})},
		{"a zero rate", "local_per_usd_nano > 0", base(row{"local_per_usd_nano": 0})},
		{"a count carrying a rate", "ck_cash_rate_on_money", count(row{"fx_rate_id": rate, "local_per_usd_nano": 15_000_000_000_000})},
		{"an unknown kind", "kind IN", base(row{"kind": "loan"})},
		{"an unknown currency", "FOREIGN KEY", base(row{"currency": "EUR"})},
		{"seq 0", "seq >= 1", base(row{"seq": 0})},
	} {
		tx(func() {
			if err := insert(db, "cash_entries", c.r); err == nil || !strings.Contains(err.Error(), c.constraint) {
				t.Errorf("%s: %v, want refused by %s", c.name, err, c.constraint)
			}
		})
	}

	// A reversal naming a real entry but with no reason, or a blank one; a place taken; an entry reversed twice.
	tx(func() {
		first := base(nil)
		if err := insert(db, "cash_entries", first); err != nil {
			t.Fatal(err)
		}
		reversal := func(changes row) row {
			return base(row{"kind": "reversal", "reverses_id": first["id"], "note": "x", "fx_rate_id": nil, "local_per_usd_nano": nil}).with(changes)
		}
		for _, c := range []struct {
			name, constraint string
			r                row
		}{
			{"a reversal with no reason (NULL)", "ck_cash_reversal_has_a_reason", reversal(row{"note": nil})},
			{"a reversal with a blank reason", "ck_cash_reversal_has_a_reason", reversal(row{"note": "   "})},
			{"a place taken", "cash_entries.seq", base(row{"seq": first["seq"]})},
		} {
			if err := insert(db, "cash_entries", c.r); err == nil || !strings.Contains(err.Error(), c.constraint) {
				t.Errorf("%s: %v, want refused by %s", c.name, err, c.constraint)
			}
		}
		if err := insert(db, "cash_entries", reversal(nil)); err != nil {
			t.Fatal(err)
		}
		if err := insert(db, "cash_entries", reversal(nil)); err == nil || !strings.Contains(err.Error(), "cash_entries.reverses_id") {
			t.Errorf("an entry reversed twice: %v", err)
		}
	})
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

// TestTheMigrationAddsNothingToExistingTables: an L5 database with a sale, a rate, a customer and a debt migrates to 0007,
// and every table it had — schema and rows — reads back exactly; the cash book starts empty.
func TestTheMigrationAddsNothingToExistingTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "l5.db")
	db, err := database.Open(database.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	migrateTo(t, db, path, 6)
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
		t.Fatal("the L5 database holds no sale or debt; the comparison would prove nothing")
	}

	migrateTo(t, db, path, 7)
	after := dump(t, db)
	for table, rows := range before {
		if table == "schema_migrations" || table == "schema_lock" {
			continue
		}
		if after[table] != rows {
			t.Errorf("%s changed in the migration:\nbefore %s\nafter  %s", table, rows, after[table])
		}
	}
	if _, existed := before["cash_entries"]; existed {
		t.Error("cash_entries existed before 0007")
	}
	var entries, problems int
	if err = db.Reader(ctx).QueryRowContext(ctx, `SELECT COUNT(*) FROM cash_entries`).Scan(&entries); err != nil || entries != 0 {
		t.Errorf("cash_entries holds %d rows after the migration: %v", entries, err)
	}
	if err = db.Reader(ctx).QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&problems); err != nil || problems != 0 {
		t.Fatalf("foreign key check: %d, %v", problems, err)
	}
}
