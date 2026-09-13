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

func TestSQLiteMeetsTheStoreContract(t *testing.T) {
	stocktest.StoreContract(t, func(t *testing.T) stocktest.Subject {
		db := litetest.OpenMigrated(t)
		return stocktest.Subject{
			Store:      sqlite.NewStore(db, clock.System()),
			NewProduct: func(t *testing.T) id.ID { return insertProduct(t, db) },
		}
	})
}
