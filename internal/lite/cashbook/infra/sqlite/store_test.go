package sqlite_test

import (
	"context"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/cashbook/cashbooktest"
	"github.com/mizan-erp/mizan/internal/lite/cashbook/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

func insertRate(t *testing.T, db *database.Store) id.ID {
	t.Helper()
	rateID, _ := id.New()
	if _, err := db.Writer(context.Background()).ExecContext(context.Background(), `INSERT INTO fx_rates (id, local_currency, seq, local_per_usd_nano, source, business_date, recorded_at)
		VALUES (?, 'SYP', (SELECT COALESCE(MAX(seq), 0) + 1 FROM fx_rates), 15000000000000, 'manual', '2026-09-14', '2026-09-14T06:00:00.000Z')`, rateID.String()); err != nil {
		t.Fatal(err)
	}
	return rateID
}

func TestSQLiteMeetsTheStoreContract(t *testing.T) {
	cashbooktest.StoreContract(t, func(t *testing.T) cashbooktest.Subject {
		db := litetest.OpenMigrated(t)
		return cashbooktest.Subject{Store: sqlite.NewStore(db, clock.System()), NewRate: func(t *testing.T) id.ID { return insertRate(t, db) }}
	})
}
