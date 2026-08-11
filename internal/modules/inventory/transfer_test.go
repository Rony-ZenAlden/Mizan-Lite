package inventory_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/inventory"
	"github.com/mizan-erp/mizan/internal/modules/inventory/domain"
)

// secondWarehouse adds another warehouse to the company, so a transfer has somewhere to go.
func (f fixture) secondWarehouse(t *testing.T) id.ID {
	t.Helper()
	identifier, err := id.New()
	if err != nil {
		t.Fatalf("id.New: %v", err)
	}
	now := clock.Format(clock.System().Now())
	if _, err = f.store.Writer(f.ctx).ExecContext(f.ctx, `
		INSERT INTO warehouses (id, branch_id, code, name, is_active,
		                        row_version, created_at, updated_at)
		SELECT ?, branch_id, 'WH2', 'Second store', 1, 1, ?, ?
		FROM warehouses WHERE id = ?`,
		string(identifier), now, now, string(f.warehouseID)); err != nil {
		t.Fatalf("creating a second warehouse: %v", err)
	}
	return identifier
}

// stockIn reads one warehouse's level for the fixture's variant.
func (f fixture) stockIn(t *testing.T, warehouseID id.ID) domain.State {
	t.Helper()
	state, err := f.svc.StockOf(f.ctx, f.variant.ID, warehouseID)
	if err != nil {
		t.Fatalf("StockOf: %v", err)
	}
	return state
}

// companyValue is what all this company's stock is worth, across every warehouse.
func (f fixture) companyValue(t *testing.T) int64 {
	t.Helper()
	levels, err := f.svc.Levels(f.ctx, f.companyID, id.ID(""), id.ID(""))
	if err != nil {
		t.Fatalf("Levels: %v", err)
	}
	var total int64
	for _, level := range levels {
		// One rounding, on the ×10¹² product — the same way the domain values stock.
		total += (level.State.OnHandMicro*level.State.AverageMicro + 500_000_000_000) /
			1_000_000_000_000
	}
	return total
}

func (f fixture) transfer(t *testing.T, to id.ID, qty int64) {
	t.Helper()
	if _, _, err := f.svc.Transfer(f.ctx, inventory.TransferInput{
		CompanyID: f.companyID, FromID: f.warehouseID, ToID: to,
		ProductID: f.product.ID, VariantID: f.variant.ID, QuantityMicro: qty,
	}); err != nil {
		t.Fatalf("Transfer(%d): %v", qty, err)
	}
}

// ── the two legs ────────────────────────────────────────────────────────────────

func TestATransferMovesStockBetweenWarehouses(t *testing.T) {
	f := newFixture(t)
	second := f.secondWarehouse(t)

	f.move(t, domain.Receipt, 10_000_000, 100_000_000)
	f.transfer(t, second, 4_000_000)

	if from := f.stockIn(t, f.warehouseID); from.OnHandMicro != 6_000_000 {
		t.Errorf("source holds %d, want 6000000", from.OnHandMicro)
	}
	if to := f.stockIn(t, second); to.OnHandMicro != 4_000_000 {
		t.Errorf("destination holds %d, want 4000000", to.OnHandMicro)
	}
}

// The property that makes a transfer post nothing: the company owns the same goods in a
// different place, so what they are worth cannot change.
func TestATransferLeavesTotalStockValueUnchanged(t *testing.T) {
	f := newFixture(t)
	second := f.secondWarehouse(t)

	f.move(t, domain.Receipt, 10_000_000, 100_000_000)
	before := f.companyValue(t)

	f.transfer(t, second, 4_000_000)

	if after := f.companyValue(t); after != before {
		t.Errorf("company stock was worth %d and is now worth %d — a transfer created value",
			before, after)
	}
}

// THE SUBTLETY OF THIS STEP.
//
// If the arrival were costed at the destination's own average, moving five units from a
// warehouse holding stock at 100 into one holding stock at 200 would credit the source 500 and
// debit the destination 1000 — inventing 500 of inventory value by moving a box across town.
func TestTheArrivalIsCostedAtTheSourcesAverageNotTheDestinations(t *testing.T) {
	f := newFixture(t)
	second := f.secondWarehouse(t)

	// The source holds cheap stock; the destination already holds expensive stock.
	f.move(t, domain.Receipt, 10_000_000, 100_000_000)
	if _, err := f.svc.Move(f.ctx, inventory.MoveInput{
		CompanyID: f.companyID, WarehouseID: second,
		ProductID: f.product.ID, VariantID: f.variant.ID,
		Type: domain.Receipt, QuantityMicro: 10_000_000, UnitCostMicro: 200_000_000,
	}); err != nil {
		t.Fatalf("stocking the destination: %v", err)
	}

	before := f.companyValue(t)
	f.transfer(t, second, 5_000_000)

	if after := f.companyValue(t); after != before {
		t.Errorf("company stock went from %d to %d — the arrival used the wrong cost",
			before, after)
	}

	// The destination blended the ARRIVING cost of 100 into its own 200:
	// (10×200 + 5×100) ÷ 15 = 166.666… → 166666667 at micro scale.
	to := f.stockIn(t, second)
	if to.AverageMicro != 166_666_667 {
		t.Errorf("destination average = %d, want 166666667", to.AverageMicro)
	}
	// The source's average is untouched by sending goods away.
	if from := f.stockIn(t, f.warehouseID); from.AverageMicro != 100_000_000 {
		t.Errorf("source average = %d, want it unchanged at 100000000", from.AverageMicro)
	}
}

// ── atomicity ───────────────────────────────────────────────────────────────────

// The worst state this module can produce is stock that has left one warehouse and arrived at
// none. Both legs are written in ONE transaction.
func TestAFailedTransferMovesNothingAtAll(t *testing.T) {
	f := newFixture(t)
	second := f.secondWarehouse(t)

	f.move(t, domain.Receipt, 3_000_000, 100_000_000)
	before := f.stockIn(t, f.warehouseID)

	// More than the source holds. Negative stock is blocked by default, so the outbound leg
	// fails — and the inbound must not have happened either.
	_, _, err := f.svc.Transfer(f.ctx, inventory.TransferInput{
		CompanyID: f.companyID, FromID: f.warehouseID, ToID: second,
		ProductID: f.product.ID, VariantID: f.variant.ID, QuantityMicro: 10_000_000,
	})
	if err == nil {
		t.Fatal("ten units were transferred out of a stock of three")
	}

	if after := f.stockIn(t, f.warehouseID); after.OnHandMicro != before.OnHandMicro {
		t.Errorf("source holds %d after a failed transfer, want %d",
			after.OnHandMicro, before.OnHandMicro)
	}
	if to := f.stockIn(t, second); to.OnHandMicro != 0 {
		t.Errorf("destination holds %d after a failed transfer — stock arrived from nowhere",
			to.OnHandMicro)
	}

	movements, err := f.svc.Movements(f.ctx, f.variant.ID, second)
	if err != nil {
		t.Fatalf("Movements: %v", err)
	}
	if len(movements) != 0 {
		t.Errorf("%d movements at the destination, want none", len(movements))
	}
}

func TestTransferringFromAWarehouseWithNoStockIsRefused(t *testing.T) {
	f := newFixture(t)
	second := f.secondWarehouse(t)

	_, _, err := f.svc.Transfer(f.ctx, inventory.TransferInput{
		CompanyID: f.companyID, FromID: f.warehouseID, ToID: second,
		ProductID: f.product.ID, VariantID: f.variant.ID, QuantityMicro: 1_000_000,
	})
	if err == nil {
		t.Fatal("stock was transferred out of an empty warehouse")
	}
	if code := errs.CodeOf(err); code != domain.CodeInsufficient {
		t.Errorf("code = %q, want %q", code, domain.CodeInsufficient)
	}
}

// A transfer to the same warehouse writes two movements that cancel: the ledger gets noisier and
// the stock does not move. It is a mistake, not a no-op.
func TestATransferToTheSameWarehouseIsRefused(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 10_000_000, 100_000_000)

	_, _, err := f.svc.Transfer(f.ctx, inventory.TransferInput{
		CompanyID: f.companyID, FromID: f.warehouseID, ToID: f.warehouseID,
		ProductID: f.product.ID, VariantID: f.variant.ID, QuantityMicro: 1_000_000,
	})
	if err == nil {
		t.Fatal("stock was transferred to the warehouse it was already in")
	}
	if code := errs.CodeOf(err); code != inventory.CodeSameWarehouse {
		t.Errorf("code = %q, want %q", code, inventory.CodeSameWarehouse)
	}
}

// ── the ledger and the books ────────────────────────────────────────────────────

func TestATransferWritesBothLegsToTheLedger(t *testing.T) {
	f := newFixture(t)
	second := f.secondWarehouse(t)

	f.move(t, domain.Receipt, 10_000_000, 100_000_000)
	f.transfer(t, second, 4_000_000)

	fromLedger, err := f.svc.Movements(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("Movements: %v", err)
	}
	if len(fromLedger) != 2 || fromLedger[1].Type != domain.TransferOut {
		t.Errorf("source ledger = %+v, want a receipt then a transfer out", fromLedger)
	}

	toLedger, err := f.svc.Movements(f.ctx, f.variant.ID, second)
	if err != nil {
		t.Fatalf("Movements: %v", err)
	}
	if len(toLedger) != 1 || toLedger[0].Type != domain.TransferIn {
		t.Errorf("destination ledger = %+v, want one transfer in", toLedger)
	}
	// The arrival records the cost it came in at, so a later replay values it the same way.
	if toLedger[0].UnitCostMicro != 100_000_000 {
		t.Errorf("arrival cost = %d, want the source's 100000000", toLedger[0].UnitCostMicro)
	}
}

// Both legs verify: a transfer must not leave either warehouse's projection disagreeing with its
// own ledger.
func TestBothWarehousesVerifyAfterATransfer(t *testing.T) {
	f := newFixture(t)
	second := f.secondWarehouse(t)

	f.move(t, domain.Receipt, 10_000_000, 100_000_000)
	f.transfer(t, second, 4_000_000)
	f.transfer(t, second, 2_000_000)

	discrepancies, err := f.svc.VerifyLedger(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("VerifyLedger: %v", err)
	}
	if len(discrepancies) != 0 {
		t.Errorf("%d discrepancies after transfers: %+v", len(discrepancies), discrepancies)
	}
}
