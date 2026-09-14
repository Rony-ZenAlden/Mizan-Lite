package sqlite_test

import (
	"context"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/customers/customerstest"
	"github.com/mizan-erp/mizan/internal/lite/customers/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

func exec(t *testing.T, db *database.Store, query string, args ...any) {
	t.Helper()
	if _, err := db.Writer(context.Background()).ExecContext(context.Background(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func insertRate(t *testing.T, db *database.Store) id.ID {
	t.Helper()
	rateID, _ := id.New()
	exec(t, db, `INSERT INTO fx_rates (id, local_currency, seq, local_per_usd_nano, source, business_date, recorded_at)
		VALUES (?, 'SYP', (SELECT COALESCE(MAX(seq), 0) + 1 FROM fx_rates), 15000000000000, 'manual', '2026-09-14', '2026-09-14T06:00:00.000Z')`, rateID.String())
	return rateID
}

// insertSale writes a minimal credit sale for an entry to name.
func insertSale(t *testing.T, db *database.Store) id.ID {
	t.Helper()
	saleID, _ := id.New()
	rate := insertRate(t, db)
	exec(t, db, `INSERT INTO sales (id, receipt_no, business_date, sold_at, status, payment, local_currency, fx_rate_id,
		local_per_usd_nano, rate_recorded_at, settlement_currency, lines_local_minor, lines_usd_minor, discount_local_minor,
		discount_usd_minor, cash_increment_minor, rounding_minor, total_minor, tendered_currency, tendered_minor,
		change_currency, change_minor, cost_usd_minor, shop_name_snapshot, created_at)
		VALUES (?, (SELECT COALESCE(MAX(receipt_no), 0) + 1 FROM sales), '2026-09-14', '2026-09-14T08:00:00.000Z', 'posted', 'credit',
		'SYP', ?, 15000000000000, '2026-09-14T06:00:00.000Z', 'USD', 12000, 80, 0, 0, 500, 0, 80, 'USD', 0, 'USD', 0, 0, 'x', 'x')`,
		saleID.String(), rate.String())
	return saleID
}

func TestSQLiteMeetsTheStoreContract(t *testing.T) {
	customerstest.StoreContract(t, func(t *testing.T) customerstest.Subject {
		db := litetest.OpenMigrated(t)
		return customerstest.Subject{
			Store:   sqlite.NewStore(db, clock.System()),
			NewSale: func(t *testing.T) id.ID { return insertSale(t, db) },
			NewRate: func(t *testing.T) id.ID { return insertRate(t, db) },
		}
	})
}
