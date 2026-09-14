package sqlite_test

import (
	"context"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/lite/stock/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/stock/stocktest"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// insertProduct writes a products row directly: the stock store's rows reference real products, and this package
// may not reach the catalogue module to make one.
func insertProduct(t *testing.T, db *database.Store) id.ID {
	t.Helper()
	productID, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	now := clock.Format(clock.System().Now())
	_, err = db.Writer(context.Background()).ExecContext(context.Background(), `
		INSERT INTO products (id, name_ar, name_key, search_text, uom_code, price_currency, sell_price_micro,
		                      is_active, row_version, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'kg', 'USD', 0, 1, 1, ?, ?)`,
		productID.String(), "منتج "+productID.String(), productID.String(), productID.String(), now, now)
	if err != nil {
		t.Fatal(err)
	}
	return productID
}

// insertSaleLine writes a rate, a sale and one line directly: stock's sale movements reference real rows, and this
// package may not reach the sales module to make them.
func insertSaleLine(t *testing.T, db *database.Store, productID id.ID) (id.ID, id.ID) {
	t.Helper()
	ctx := context.Background()
	rateID, _ := id.New()
	saleID, _ := id.New()
	lineID, _ := id.New()
	for _, stmt := range []struct {
		q    string
		args []any
	}{
		{`INSERT INTO fx_rates (id, local_currency, seq, local_per_usd_nano, source, business_date, recorded_at)
		  VALUES (?, 'SYP', (SELECT COALESCE(MAX(seq), 0) + 1 FROM fx_rates), 15000000000000, 'manual', '2026-09-14', '2026-09-14T06:00:00.000Z')`,
			[]any{rateID.String()}},
		{`INSERT INTO sales (id, receipt_no, business_date, sold_at, status, payment, local_currency, fx_rate_id,
		  local_per_usd_nano, rate_recorded_at, settlement_currency, lines_local_minor, lines_usd_minor, discount_local_minor,
		  discount_usd_minor, cash_increment_minor, rounding_minor, total_minor, tendered_currency, tendered_minor,
		  change_currency, change_minor, cost_usd_minor, shop_name_snapshot, created_at)
		  VALUES (?, (SELECT COALESCE(MAX(receipt_no), 0) + 1 FROM sales), '2026-09-14', '2026-09-14T06:00:00.000Z',
		  'posted', 'cash', 'SYP', ?, 15000000000000, '2026-09-14T06:00:00.000Z', 'SYP', 0, 0, 0, 0, 500, 0, 0, 'SYP', 0,
		  'SYP', 0, 0, 'المونة', '2026-09-14T06:00:00.000Z')`, []any{saleID.String(), rateID.String()}},
		{`INSERT INTO sale_lines (id, sale_id, line_no, product_id, name_ar_snapshot, unit_code_snapshot, quantity_micro,
		  price_currency, unit_price_micro, gross_local_minor, gross_usd_minor, discount_percent_micro, discount_local_minor,
		  discount_usd_minor, unit_cost_usd_micro, cost_known, cost_usd_minor, cost_local_minor)
		  VALUES (?, ?, 1, ?, 'منتج', 'kg', 1000000, 'USD', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)`,
			[]any{lineID.String(), saleID.String(), productID.String()}},
	} {
		if _, err := db.Writer(ctx).ExecContext(ctx, stmt.q, stmt.args...); err != nil {
			t.Fatal(err)
		}
	}
	return saleID, lineID
}

func TestSQLiteMeetsTheStoreContract(t *testing.T) {
	stocktest.StoreContract(t, func(t *testing.T) stocktest.Subject {
		db := litetest.OpenMigrated(t)
		return stocktest.Subject{
			Store:       sqlite.NewStore(db, clock.System()),
			NewProduct:  func(t *testing.T) id.ID { return insertProduct(t, db) },
			NewSaleLine: func(t *testing.T, productID id.ID) (id.ID, id.ID) { return insertSaleLine(t, db, productID) },
		}
	})
}
