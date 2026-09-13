package sqlite_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/lite/catalog"
	"github.com/mizan-erp/mizan/internal/lite/catalog/catalogtest"
	"github.com/mizan-erp/mizan/internal/lite/catalog/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
)

func TestSQLiteMeetsTheStoreContract(t *testing.T) {
	catalogtest.StoreContract(t, func(t *testing.T) catalog.Store {
		return sqlite.NewStore(litetest.OpenMigrated(t), clock.System())
	})
}
