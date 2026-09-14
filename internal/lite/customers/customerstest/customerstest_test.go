package customerstest_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/customers/customerstest"
)

func TestTheFakeMeetsTheStoreContract(t *testing.T) {
	newID := func(t *testing.T) id.ID {
		v, err := id.New()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	customerstest.StoreContract(t, func(*testing.T) customerstest.Subject {
		return customerstest.Subject{Store: customerstest.NewFake(), NewSale: newID, NewRate: newID}
	})
}
