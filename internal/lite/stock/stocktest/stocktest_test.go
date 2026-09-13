package stocktest_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/stock/stocktest"
)

func TestTheFakeMeetsTheStoreContract(t *testing.T) {
	stocktest.StoreContract(t, func(t *testing.T) stocktest.Subject {
		return stocktest.Subject{Store: stocktest.NewFake(), NewProduct: func(t *testing.T) id.ID {
			productID, err := id.New()
			if err != nil {
				t.Fatal(err)
			}
			return productID
		}}
	})
}
