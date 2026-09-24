package sqlite_test

import (
	"context"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/lite/suppliers/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/suppliers/supplierstest"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

func insertProduct(t *testing.T, db *database.Store) id.ID {
	t.Helper()
	productID, _ := id.New()
	now := clock.Format(clock.System().Now())
	if _, err := db.Writer(context.Background()).ExecContext(context.Background(), `
		INSERT INTO products (id, name_ar, name_key, search_text, uom_code, price_currency, sell_price_micro, is_active, row_version, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'jar', 'USD', 0, 1, 1, ?, ?)`, productID.String(), "منتج "+productID.String(), productID.String(), productID.String(), now, now); err != nil {
		t.Fatal(err)
	}
	return productID
}

// insertReceipt is the stock book's receipt of one unit — what a purchase line points at.
func insertReceipt(t *testing.T, db *database.Store, productID id.ID) id.ID {
	t.Helper()
	receiptID, _ := id.New()
	if _, err := db.Writer(context.Background()).ExecContext(context.Background(), `
		INSERT INTO stock_ledger (id, product_id, seq, business_date, occurred_at, kind, quantity_micro, unit_cost_usd_micro,
		                          on_hand_before_micro, avg_cost_before_usd_micro, on_hand_after_micro, avg_cost_after_usd_micro, created_at)
		VALUES (?, ?, (SELECT COALESCE(MAX(seq), 0) + 1 FROM stock_ledger WHERE product_id = ?), '2026-09-24', '2026-09-24T09:00:00.000Z',
		        'receipt', 1000000, 0, 0, 0, 1000000, 0, '2026-09-24T09:00:00.000Z')`,
		receiptID.String(), productID.String(), productID.String()); err != nil {
		t.Fatal(err)
	}
	return receiptID
}

func TestSQLiteMeetsTheStoreContract(t *testing.T) {
	supplierstest.StoreContract(t, func(t *testing.T) supplierstest.Subject {
		db := litetest.OpenMigrated(t)
		return supplierstest.Subject{
			Store:      sqlite.NewStore(db, clock.System()),
			NewProduct: func(t *testing.T) id.ID { return insertProduct(t, db) },
			NewReceipt: func(t *testing.T, productID id.ID) id.ID { return insertReceipt(t, db, productID) },
		}
	})
}
