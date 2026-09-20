package bootstrap_test

import (
	"context"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
)

// TestTheRebuildsOf0010KeepEveryForeignKey: migration 0010 rebuilds stock_ledger and debt_entries to widen a CHECK,
// and sale_returns — created earlier in the same migration — points at debt_entries. SQLite rewrites such references
// on ALTER TABLE ... RENAME, but a migration that got the order wrong would leave a table pointing at a name that no
// longer exists, and nothing else in the suite would notice until a shop recorded its first return on credit.
func TestTheRebuildsOf0010KeepEveryForeignKey(t *testing.T) {
	app := start(t, dataDir(t), clock.System())
	ctx := context.Background()
	// voucher_numbers points at debt_entries, which 0010 rebuilds; sale_returns points at it too.
	for _, table := range []string{"voucher_numbers", "sale_return_lines", "stock_levels"} {
		var count int
		if err := app.DB.Reader(ctx).QueryRowContext(ctx,
			`SELECT COUNT(*) FROM pragma_foreign_key_list(?) WHERE "table" NOT IN
			 ('debt_entries', 'sale_lines', 'sale_returns', 'products', 'stock_ledger')`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("%s points somewhere unexpected after the rebuilds", table)
		}
	}
	rows, err := app.DB.Reader(ctx).QueryContext(ctx, `SELECT "table", "from", "to" FROM pragma_foreign_key_list('sale_returns')`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	seen := map[string]string{}
	for rows.Next() {
		var tbl, from, to string
		if err := rows.Scan(&tbl, &from, &to); err != nil {
			t.Fatal(err)
		}
		seen[from] = tbl
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("sale_returns foreign keys: %v", seen)
	if seen["debt_entry_id"] != "debt_entries" {
		t.Errorf("debt_entry_id points at %q, not debt_entries", seen["debt_entry_id"])
	}
	if seen["sale_id"] != "sales" {
		t.Errorf("sale_id points at %q", seen["sale_id"])
	}
	var problems int
	if err := app.DB.Reader(ctx).QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&problems); err != nil {
		t.Fatal(err)
	}
	if problems != 0 {
		t.Errorf("%d foreign key problems after the rebuilds", problems)
	}
}
