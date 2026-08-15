package bootstrap_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/catalog"
	catalogdomain "github.com/mizan-erp/mizan/internal/modules/catalog/domain"
	"github.com/mizan-erp/mizan/internal/modules/inventory"
	inventorydomain "github.com/mizan-erp/mizan/internal/modules/inventory/domain"
)

// TestTheStockValuationReconcilesToTheGeneralLedger
//
// # Why this test can only live here
//
// `Valuation` is unit-tested against a fake ledger, which proves the arithmetic and the reporting
// and says nothing at all about whether the real port asks the right question. A drill pointing
// the composition root's adapter at the COGS mapping instead of INVENTORY left every inventory
// test green — because none of them go through the composition root.
//
// **A seam is only proven by a caller.** Phase 6's central finding, and this is where the caller
// lives.
//
// The two sides are computed by completely different paths: one multiplies each level's quantity
// by the average cost the costing engine maintained, the other sums journal lines written by
// posting rules from values computed at the moment each movement happened. Phase 6.6 is the
// argument for comparing them — a costing conversion was wrong by a factor of a hundred for two
// phases while every level, every movement and every journal entry stayed internally consistent.
func TestTheStockValuationReconcilesToTheGeneralLedger(t *testing.T) {
	app, ctx, companyID := traded(t)

	var warehouseID id.ID
	if err := app.DB.Reader(ctx).QueryRowContext(ctx,
		`SELECT w.id FROM warehouses w
		   JOIN branches b ON b.id = w.branch_id
		  WHERE b.company_id = ? LIMIT 1`,
		string(companyID)).Scan(&warehouseID); err != nil {
		t.Fatalf("finding the warehouse: %v", err)
	}

	if err := app.Catalog.ApplyUnits(ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}
	// The chart alone is not enough: without the posting RULES, a movement fires an event that
	// matches nothing and the ledger stays at zero — which would make this test read as a
	// reconciliation failure when it is a fixture that never wired the books up.
	if err := app.Accounting.ApplyRules(ctx, companyID, "generic_trading"); err != nil {
		t.Fatalf("ApplyRules: %v", err)
	}

	created, variant, err := app.Catalog.CreateProduct(ctx, catalog.NewProductInput{
		CompanyID: companyID, Code: "WIDGET", Name: "Widget",
		Type: catalogdomain.TypeGoods, StockUnit: "PCS",
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}

	// An empty company reconciles: nothing on the shelf, nothing in the control account. This is
	// the state the check must be quiet in, and asserting it first means a later difference is
	// evidence rather than noise.
	difference, err := app.Inventory.VerifyValuation(ctx, companyID)
	if err != nil {
		t.Fatalf("VerifyValuation: %v", err)
	}
	if difference != 0 {
		t.Fatalf("an empty company is out by %d", difference)
	}

	// Ten at 60, received with no document — so inventory posts the stock increase itself and
	// the ledger figure comes from the same movement the level does.
	if _, err = app.Inventory.Move(ctx, inventory.MoveInput{
		CompanyID: companyID, WarehouseID: warehouseID,
		ProductID: created.ID, VariantID: variant.ID,
		Type: inventorydomain.Receipt, QuantityMicro: 10_000_000,
		UnitCostMicro: 60_000_000,
	}); err != nil {
		t.Fatalf("Move: %v", err)
	}

	valuation, err := app.Inventory.Valuation(ctx, companyID)
	if err != nil {
		t.Fatalf("Valuation: %v", err)
	}
	if !valuation.HasLedger {
		t.Fatal("the composition root did not attach a control ledger, so nothing was compared")
	}
	if valuation.TotalMinor != 600 {
		t.Fatalf("the shelf is worth %d, want 600", valuation.TotalMinor)
	}
	// The ledger figure is READ, not assumed. A port pointed at the wrong mapping returns some
	// other account's balance, and that is what this catches.
	if valuation.LedgerMinor != 600 {
		t.Errorf("the ledger says the stock is worth %d and the shelf says %d — either the "+
			"movement posted nothing, or the control port is asking about the wrong account",
			valuation.LedgerMinor, valuation.TotalMinor)
	}
	if valuation.DifferenceMinor != 0 {
		t.Errorf("out by %d", valuation.DifferenceMinor)
	}

	// Writing five off takes 300 off both sides, which is what makes this a reconciliation
	// rather than a coincidence of one number.
	//
	// An ADJUSTMENT, not an issue. A documentless issue deliberately posts nothing (4.3): the
	// other side is a customer, a project or a write-off and only the caller knows which, so
	// Phase 5 supplies the document. Using one here would report a designed state as a
	// reconciliation failure.
	if _, err = app.Inventory.Move(ctx, inventory.MoveInput{
		CompanyID: companyID, WarehouseID: warehouseID,
		ProductID: created.ID, VariantID: variant.ID,
		Type: inventorydomain.AdjustmentOut, QuantityMicro: 5_000_000,
	}); err != nil {
		t.Fatalf("Move(adjustment out): %v", err)
	}

	valuation, err = app.Inventory.Valuation(ctx, companyID)
	if err != nil {
		t.Fatalf("Valuation: %v", err)
	}
	if valuation.TotalMinor != 300 || valuation.LedgerMinor != 300 {
		t.Errorf("after writing five off: shelf %d, ledger %d, both want 300",
			valuation.TotalMinor, valuation.LedgerMinor)
	}
	if valuation.DifferenceMinor != 0 {
		t.Errorf("out by %d after a write-off", valuation.DifferenceMinor)
	}
}
