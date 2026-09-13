package catalogtest_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/catalog"
	"github.com/mizan-erp/mizan/internal/lite/catalog/catalogtest"
)

func TestTheFakeMeetsTheStoreContract(t *testing.T) {
	catalogtest.StoreContract(t, func(*testing.T) catalog.Store { return catalogtest.NewFake() })
}
