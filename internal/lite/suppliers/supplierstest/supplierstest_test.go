package supplierstest_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/suppliers/supplierstest"
)

func TestTheFakeMeetsTheStoreContract(t *testing.T) {
	newID := func(t *testing.T) id.ID {
		v, err := id.New()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	supplierstest.StoreContract(t, func(t *testing.T) supplierstest.Subject {
		return supplierstest.Subject{
			Store:      supplierstest.NewFake(),
			NewProduct: newID,
			NewReceipt: func(t *testing.T, _ id.ID) id.ID { return newID(t) },
		}
	})
}
