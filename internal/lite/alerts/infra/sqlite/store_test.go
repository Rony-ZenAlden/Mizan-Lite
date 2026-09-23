package sqlite_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/alerts"
	"github.com/mizan-erp/mizan/internal/lite/alerts/alertstest"
	"github.com/mizan-erp/mizan/internal/lite/alerts/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
)

func TestSQLiteMeetsTheStoreContract(t *testing.T) {
	alertstest.StoreContract(t, func(t *testing.T) alerts.Store { return sqlite.NewStore(litetest.OpenMigrated(t)) })
}
