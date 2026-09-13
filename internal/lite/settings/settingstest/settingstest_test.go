package settingstest_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/settings"
	"github.com/mizan-erp/mizan/internal/lite/settings/settingstest"
)

// TestTheFakeMeetsTheStoreContract is half of the guarantee that the fake is honest. The other half
// runs the same contract against SQLite in infra/sqlite.
func TestTheFakeMeetsTheStoreContract(t *testing.T) {
	settingstest.StoreContract(t, func(*testing.T) settings.Store { return settingstest.NewFake() })
}
