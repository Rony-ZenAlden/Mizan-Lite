package salestest_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/sales/salestest"
)

func TestTheFakeMeetsTheStoreContract(t *testing.T) {
	newID := func(t *testing.T) id.ID {
		v, err := id.New()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	salestest.StoreContract(t, func(*testing.T) salestest.Subject {
		return salestest.Subject{Store: salestest.NewFake(), NewProduct: newID, NewRate: newID}
	})
}
