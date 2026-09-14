package sqlite_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/fx"
	"github.com/mizan-erp/mizan/internal/lite/fx/fxtest"
	"github.com/mizan-erp/mizan/internal/lite/fx/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
)

func TestSQLiteMeetsTheStoreContract(t *testing.T) {
	fxtest.StoreContract(t, func(t *testing.T) fx.Store { return sqlite.NewStore(litetest.OpenMigrated(t)) })
}
