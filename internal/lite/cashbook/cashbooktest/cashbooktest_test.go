package cashbooktest_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/cashbook/cashbooktest"
)

func TestTheFakeMeetsTheStoreContract(t *testing.T) {
	cashbooktest.StoreContract(t, func(*testing.T) cashbooktest.Subject {
		return cashbooktest.Subject{Store: cashbooktest.NewFake(), NewRate: func(t *testing.T) id.ID {
			v, err := id.New()
			if err != nil {
				t.Fatal(err)
			}
			return v
		}}
	})
}
