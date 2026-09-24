package bootstrap_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/lite/owner/ownertest"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// TestMigration12CarriesEveryReturnAcrossTheDebtLedgerRebuild (0.10.0): 0012 §7 rebuilds debt_entries, and sale_returns,
// sale_return_lines and voucher_numbers are copied aside and put back around it. No fixture shop holds a return, so one is
// planted in the 0.9.9 shop — a return settled against a customer's debt, its line, and its debt entry — and the upgrade
// must carry every row, and every foreign key between them, across.
func TestMigration12CarriesEveryReturnAcrossTheDebtLedgerRebuild(t *testing.T) {
	ctx := context.Background()
	file, _ := fixtureShop(t, 11)
	store, err := database.Open(database.Config{Path: file})
	if err != nil {
		t.Fatal(err)
	}
	var customerID, currency, saleID, saleLineID, productID, rateID, name string
	var seq, balance, rateNano int64
	err = store.Reader(ctx).QueryRowContext(ctx, `
		SELECT d.customer_id, d.currency, d.sale_id, l.id, l.product_id, s.fx_rate_id, s.local_per_usd_nano, d.customer_name_snapshot
		  FROM debt_entries d JOIN sales s ON s.id = d.sale_id JOIN sale_lines l ON l.sale_id = s.id
		 WHERE d.kind = 'charge' AND s.status = 'posted' ORDER BY d.occurred_at LIMIT 1`).
		Scan(&customerID, &currency, &saleID, &saleLineID, &productID, &rateID, &rateNano, &name)
	if err != nil {
		t.Fatalf("the 0.9.9 shop has no credit sale to return from: %v", err)
	}
	if err = store.Reader(ctx).QueryRowContext(ctx, `SELECT seq, balance_after_minor FROM debt_entries
		 WHERE customer_id = ? AND currency = ? ORDER BY seq DESC LIMIT 1`, customerID, currency).Scan(&seq, &balance); err != nil {
		t.Fatal(err)
	}
	const entryID, returnID, lineID = "e0000000-0000-4000-8000-000000000012", "r0000000-0000-4000-8000-000000000012", "l0000000-0000-4000-8000-000000000012"
	at := "2026-09-24T09:00:00.000Z"
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO debt_entries (id, customer_id, currency, seq, business_date, occurred_at, kind, amount_minor,
			balance_before_minor, balance_after_minor, customer_name_snapshot, note, created_at)
			VALUES (?, ?, ?, ?, '2026-09-24', ?, 'sale_return', -1, ?, ?, ?, 'أعاد علبة', ?)`,
			[]any{entryID, customerID, currency, seq + 1, at, balance, balance - 1, name, at}},
		{`INSERT INTO sale_returns (id, return_no, sale_id, business_date, returned_at, settlement, settlement_currency,
			refund_minor, refund_local_minor, refund_usd_minor, cost_usd_minor, cost_local_minor, cost_known, fx_rate_id,
			local_per_usd_nano, customer_id, debt_entry_id, reason, created_at)
			VALUES (?, 1, ?, '2026-09-24', ?, 'debt', ?, 1, 1, 1, 0, 0, 0, ?, ?, ?, ?, 'أعاد علبة', ?)`,
			[]any{returnID, saleID, at, currency, rateID, rateNano, customerID, entryID, at}},
		{`INSERT INTO sale_return_lines (id, return_id, sale_line_id, line_no, product_id, quantity_micro, refund_local_minor,
			refund_usd_minor, unit_cost_usd_micro, cost_known, cost_usd_minor, cost_local_minor, restocked)
			VALUES (?, ?, ?, 1, ?, 1000000, 1, 1, 0, 0, 0, 0, 1)`,
			[]any{lineID, returnID, saleLineID, productID}},
	} {
		if _, err = store.Writer(ctx).ExecContext(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("planting the return: %v", err)
		}
	}
	vouchersBefore := count(ctx, t, store, `SELECT COUNT(*) FROM voucher_numbers`)
	entriesBefore := count(ctx, t, store, `SELECT COUNT(*) FROM debt_entries`)
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}

	p := dataDir(t)
	p.Data, p.DBFile, p.Backups = filepath.Dir(file), file, filepath.Join(filepath.Dir(file), "backups")
	app, err := bootstrap.Start(ctx, bootstrap.Options{Paths: p, Logger: litetest.Logger(), PINHasher: ownertest.Hasher()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Shutdown(ctx) }()
	db := app.DB
	for what, want := range map[string]int64{
		`SELECT COUNT(*) FROM sale_returns WHERE id = '` + returnID + `' AND debt_entry_id = '` + entryID + `'`: 1,
		`SELECT COUNT(*) FROM sale_return_lines WHERE id = '` + lineID + `' AND return_id = '` + returnID + `'`: 1,
		`SELECT COUNT(*) FROM debt_entries WHERE id = '` + entryID + `' AND kind = 'sale_return'`:               1,
		`SELECT COUNT(*) FROM voucher_numbers`: vouchersBefore,
		`SELECT COUNT(*) FROM debt_entries`:    entriesBefore,
	} {
		if got := count(ctx, t, db, what); got != want {
			t.Errorf("%s = %d, want %d", what, got, want)
		}
	}
	rows, err := db.Reader(ctx).QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("a foreign key points at nothing after the rebuild")
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	// And the new kind is one the rebuilt ledger takes.
	var kinds string
	if err = db.Reader(ctx).QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE name = 'debt_entries'`).Scan(&kinds); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(kinds, "'conversion'") {
		t.Fatalf("the rebuilt ledger takes no conversion: %s", kinds)
	}
}

func count(ctx context.Context, t *testing.T, db *database.Store, query string) int64 {
	t.Helper()
	var n int64
	if err := db.Reader(ctx).QueryRowContext(ctx, query).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}
