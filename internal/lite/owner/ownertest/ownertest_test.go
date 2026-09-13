package ownertest_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/owner"
	"github.com/mizan-erp/mizan/internal/lite/owner/ownertest"
)

func TestTheFakeMeetsTheStoreContract(t *testing.T) {
	ownertest.StoreContract(t, func(*testing.T) owner.Store { return ownertest.NewFake() })
}
