package printingtest_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/printing/printingtest"
)

func TestTheFakeMeetsTheStoreContract(t *testing.T) {
	printingtest.StoreContract(t, func(*testing.T) printingtest.Subject {
		return printingtest.Subject{Store: printingtest.NewFake(), NewEntry: func(*testing.T) id.ID { v, _ := id.New(); return v }}
	})
}
