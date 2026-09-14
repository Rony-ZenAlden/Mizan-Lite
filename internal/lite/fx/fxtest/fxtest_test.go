package fxtest_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/fx"
	"github.com/mizan-erp/mizan/internal/lite/fx/fxtest"
)

func TestTheFakeMeetsTheStoreContract(t *testing.T) {
	fxtest.StoreContract(t, func(*testing.T) fx.Store { return fxtest.NewFake() })
}
