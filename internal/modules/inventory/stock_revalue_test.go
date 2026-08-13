package inventory_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/modules/inventory"
	"github.com/mizan-erp/mizan/internal/modules/inventory/domain"
)

// TestRevaluingEmptyStockWritesNoMovement
//
// # Why this test exists at the INVENTORY level
//
// Purchasing never asks for it: its own split returns nothing against stock when nothing is on
// hand, so the guard here is unreachable from that caller. A drill removing it changed no
// purchasing test — which means the guard was untested rather than unnecessary.
//
// It is a real guard for any other caller, and this is the test that can only pass if THIS layer
// keeps it. The Phase 3 rule: when two layers keep one rule, each needs a test that names the
// layer it is about.
func TestRevaluingEmptyStockWritesNoMovement(t *testing.T) {
	f := newFixture(t)

	// Nothing has ever moved for this variant, so there is nothing carrying a wrong cost. A
	// revaluation here would write a ledger row implying a correction that did not happen.
	movement, err := f.svc.RevalueBy(f.ctx, inventory.RevalueInput{
		CompanyID: f.companyID, WarehouseID: f.warehouseID,
		ProductID: f.product.ID, VariantID: f.variant.ID,
		DeltaMinor: 5_000, Decimals: 2,
		DocumentType: "test", OccurredAt: "2026-08-20",
	})
	if err != nil {
		t.Fatalf("RevalueBy: %v", err)
	}
	if !movement.ID.IsZero() {
		t.Errorf("a revaluation was written against empty stock: %+v", movement)
	}

	movements, err := f.svc.Movements(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("Movements: %v", err)
	}
	for _, m := range movements {
		if m.Type == domain.Revaluation {
			t.Errorf("the ledger carries a revaluation of nothing: %+v", m)
		}
	}
}
