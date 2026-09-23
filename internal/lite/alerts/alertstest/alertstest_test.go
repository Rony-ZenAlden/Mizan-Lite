package alertstest_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/alerts"
	"github.com/mizan-erp/mizan/internal/lite/alerts/alertstest"
)

func TestTheFakeMeetsTheStoreContract(t *testing.T) {
	alertstest.StoreContract(t, func(*testing.T) alerts.Store { return alertstest.NewFake() })
}
