package sqlite_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/lite/owner"
	"github.com/mizan-erp/mizan/internal/lite/owner/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/owner/ownertest"
)

func TestSQLiteMeetsTheStoreContract(t *testing.T) {
	ownertest.StoreContract(t, func(t *testing.T) owner.Store { return sqlite.NewStore(litetest.OpenMigrated(t)) })
}
