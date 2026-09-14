package sqlite_test

import (
	"context"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/lite/sales/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/sales/salestest"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

func insertProduct(t *testing.T, db *database.Store) id.ID {
	t.Helper()
	productID, _ := id.New()
	now := clock.Format(clock.System().Now())
	if _, err := db.Writer(context.Background()).ExecContext(context.Background(), `
		INSERT INTO products (id, name_ar, name_key, search_text, uom_code, price_currency, sell_price_micro, is_active, row_version, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'kg', 'USD', 0, 1, 1, ?, ?)`, productID.String(), "منتج "+productID.String(), productID.String(), productID.String(), now, now); err != nil {
		t.Fatal(err)
	}
	return productID
}

func insertRate(t *testing.T, db *database.Store) id.ID {
	t.Helper()
	rateID, _ := id.New()
	if _, err := db.Writer(context.Background()).ExecContext(context.Background(), `
		INSERT INTO fx_rates (id, local_currency, seq, local_per_usd_nano, source, business_date, recorded_at)
		VALUES (?, 'SYP', (SELECT COALESCE(MAX(seq), 0) + 1 FROM fx_rates), 15000000000000, 'manual', '2026-09-14', '2026-09-14T06:00:00.000Z')`, rateID.String()); err != nil {
		t.Fatal(err)
	}
	return rateID
}

func TestSQLiteMeetsTheStoreContract(t *testing.T) {
	salestest.StoreContract(t, func(t *testing.T) salestest.Subject {
		db := litetest.OpenMigrated(t)
		return salestest.Subject{
			Store:      sqlite.NewStore(db, clock.System()),
			NewProduct: func(t *testing.T) id.ID { return insertProduct(t, db) },
			NewRate:    func(t *testing.T) id.ID { return insertRate(t, db) },
		}
	})
}
