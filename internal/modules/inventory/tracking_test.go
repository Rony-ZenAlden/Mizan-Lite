package inventory_test

import (
	"context"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/inventory"
	"github.com/mizan-erp/mizan/internal/modules/inventory/domain"
)

// fixedTracking is the Products port, answered from a constant — the two-line fake the port
// exists to make possible.
type fixedTracking struct{ mode domain.Tracking }

func (f fixedTracking) TrackingOf(context.Context, id.ID) (domain.Tracking, error) {
	return f.mode, nil
}

// tracked rebuilds the service with a tracking mode in force.
func (f fixture) tracked(t *testing.T, mode domain.Tracking) fixture {
	t.Helper()
	f.svc = inventory.NewService(f.store, inventory.Options{
		Clock: f.svc.Clock(), Bus: f.bus, Settings: f.settings,
		Products: fixedTracking{mode: mode},
	})
	return f
}

func (f fixture) lot(t *testing.T, number, expires string) domain.Lot {
	t.Helper()
	created, err := f.svc.CreateLot(f.ctx, inventory.NewLotInput{
		CompanyID: f.companyID, VariantID: f.variant.ID,
		Number: number, ExpiresOn: expires,
	})
	if err != nil {
		t.Fatalf("CreateLot(%s): %v", number, err)
	}
	return created
}

// receiveLot brings a batch into stock.
func (f fixture) receiveLot(t *testing.T, lotID id.ID, qty, cost int64) {
	t.Helper()
	if _, err := f.svc.Move(f.ctx, inventory.MoveInput{
		CompanyID: f.companyID, WarehouseID: f.warehouseID,
		ProductID: f.product.ID, VariantID: f.variant.ID,
		Type: domain.Receipt, QuantityMicro: qty, UnitCostMicro: cost, LotID: lotID,
	}); err != nil {
		t.Fatalf("receiveLot: %v", err)
	}
}

// ── the tracking rules, through real storage ────────────────────────────────────

func TestALotTrackedProductCannotMoveWithoutALotThroughTheService(t *testing.T) {
	f := newFixture(t).tracked(t, domain.TrackLot)

	_, err := f.svc.Move(f.ctx, inventory.MoveInput{
		CompanyID: f.companyID, WarehouseID: f.warehouseID,
		ProductID: f.product.ID, VariantID: f.variant.ID,
		Type: domain.Receipt, QuantityMicro: 10_000_000, UnitCostMicro: 100_000_000,
	})
	if err == nil {
		t.Fatal("a lot-tracked product was received with no lot")
	}
	if code := errs.CodeOf(err); code != domain.CodeLotRequired {
		t.Errorf("code = %q, want %q", code, domain.CodeLotRequired)
	}
}

// The direction that gets forgotten: a furniture shop must never see the concept, so its data
// must never carry it.
func TestAnUntrackedProductCannotMoveWithALotThroughTheService(t *testing.T) {
	f := newFixture(t).tracked(t, domain.TrackQuantity)
	batch := f.lot(t, "LOT-1", "2027-01-01")

	_, err := f.svc.Move(f.ctx, inventory.MoveInput{
		CompanyID: f.companyID, WarehouseID: f.warehouseID,
		ProductID: f.product.ID, VariantID: f.variant.ID,
		Type: domain.Receipt, QuantityMicro: 10_000_000, UnitCostMicro: 100_000_000,
		LotID: batch.ID,
	})
	if err == nil {
		t.Fatal("an untracked product was received against a lot")
	}
	if code := errs.CodeOf(err); code != domain.CodeLotNotAllowed {
		t.Errorf("code = %q, want %q", code, domain.CodeLotNotAllowed)
	}
}

// ── the lot-grain projection ────────────────────────────────────────────────────

func TestReceivingALotRaisesItsOwnLevelAndTheCoarseOne(t *testing.T) {
	f := newFixture(t).tracked(t, domain.TrackLot)
	first := f.lot(t, "LOT-1", "2027-01-01")
	second := f.lot(t, "LOT-2", "2026-09-01")

	f.receiveLot(t, first.ID, 10_000_000, 100_000_000)
	f.receiveLot(t, second.ID, 4_000_000, 100_000_000)

	// The coarse level is the sum, and is what "how many do we have" reads.
	if state := f.stock(t); state.OnHandMicro != 14_000_000 {
		t.Errorf("on hand = %d, want 14000000", state.OnHandMicro)
	}

	// And each lot knows its own share.
	stock, err := f.svc.LotStock(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("LotStock: %v", err)
	}
	byNumber := map[string]int64{}
	for _, entry := range stock {
		byNumber[entry.Lot.Number] = entry.QuantityMicro
	}
	if byNumber["LOT-1"] != 10_000_000 || byNumber["LOT-2"] != 4_000_000 {
		t.Errorf("lot levels = %v, want LOT-1 at 10 and LOT-2 at 4", byNumber)
	}
}

func TestIssuingFromALotLowersItsLevel(t *testing.T) {
	f := newFixture(t).tracked(t, domain.TrackLot)
	batch := f.lot(t, "LOT-1", "2027-01-01")
	f.receiveLot(t, batch.ID, 10_000_000, 100_000_000)

	if _, err := f.svc.Move(f.ctx, inventory.MoveInput{
		CompanyID: f.companyID, WarehouseID: f.warehouseID,
		ProductID: f.product.ID, VariantID: f.variant.ID,
		Type: domain.Issue, QuantityMicro: 3_000_000, LotID: batch.ID,
	}); err != nil {
		t.Fatalf("Move(issue): %v", err)
	}

	stock, err := f.svc.LotStock(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("LotStock: %v", err)
	}
	if len(stock) != 1 || stock[0].QuantityMicro != 7_000_000 {
		t.Errorf("lot stock = %+v, want 7000000 left", stock)
	}
}

// ── FEFO through real storage ───────────────────────────────────────────────────

// The batch that expires soonest goes first, whatever order it arrived in. Picking by receipt
// order is how a wholesaler ships the batch expiring next week and writes off the one expiring
// next year.
func TestPickingTakesTheSoonestToExpireFirst(t *testing.T) {
	f := newFixture(t).tracked(t, domain.TrackLot)

	// Received in the WRONG order for FEFO: the long-dated batch arrives first.
	late := f.lot(t, "LATE", "2027-01-01")
	f.receiveLot(t, late.ID, 10_000_000, 100_000_000)
	soon := f.lot(t, "SOON", "2026-07-01")
	f.receiveLot(t, soon.ID, 10_000_000, 100_000_000)

	picks, err := f.svc.PickLots(f.ctx, f.variant.ID, f.warehouseID, 12_000_000, "2026-06-15")
	if err != nil {
		t.Fatalf("PickLots: %v", err)
	}
	if len(picks) != 2 {
		t.Fatalf("%d picks, want 2", len(picks))
	}
	if picks[0].LotID != soon.ID {
		t.Error("the first pick was not the soonest to expire")
	}
	if picks[0].QuantityMicro != 10_000_000 || picks[1].QuantityMicro != 2_000_000 {
		t.Errorf("picks = %+v, want 10 then 2", picks)
	}
}

func TestPickingSkipsAnExpiredBatchAndReportsAShortfall(t *testing.T) {
	f := newFixture(t).tracked(t, domain.TrackLot)

	expired := f.lot(t, "OLD", "2026-01-01")
	f.receiveLot(t, expired.ID, 100_000_000, 100_000_000)
	good := f.lot(t, "GOOD", "2027-01-01")
	f.receiveLot(t, good.ID, 3_000_000, 100_000_000)

	// Plenty on hand, but only three units of it usable.
	_, err := f.svc.PickLots(f.ctx, f.variant.ID, f.warehouseID, 10_000_000, "2026-06-15")
	if err == nil {
		t.Fatal("ten units were picked from three usable ones")
	}
	if code := errs.CodeOf(err); code != domain.CodeNotEnoughLots {
		t.Errorf("code = %q, want %q", code, domain.CodeNotEnoughLots)
	}
	typed, _ := errs.AsError(err)
	if typed.Params["available"] != "3000000" {
		t.Errorf("params = %v, want the USABLE quantity", typed.Params)
	}
}

// ── lots ────────────────────────────────────────────────────────────────────────

// The same number twice for one variant is a duplicate nobody can tell apart at a recall —
// which is the one moment lot tracking has to work.
func TestALotNumberIsUniquePerVariant(t *testing.T) {
	f := newFixture(t).tracked(t, domain.TrackLot)
	f.lot(t, "LOT-1", "2027-01-01")

	_, err := f.svc.CreateLot(f.ctx, inventory.NewLotInput{
		CompanyID: f.companyID, VariantID: f.variant.ID, Number: "LOT-1",
	})
	if err == nil {
		t.Fatal("one variant was given two lots with the same number")
	}
	if code := errs.CodeOf(err); code != inventory.CodeDuplicateLot {
		t.Errorf("code = %q, want %q", code, inventory.CodeDuplicateLot)
	}
}

// ── serials ─────────────────────────────────────────────────────────────────────

func TestASerialisedProductMovesOneUnitAtATime(t *testing.T) {
	f := newFixture(t).tracked(t, domain.TrackSerial)

	serial, err := f.svc.CreateSerial(f.ctx, inventory.NewSerialInput{
		CompanyID: f.companyID, VariantID: f.variant.ID, Number: "IMEI-1",
		WarehouseID: f.warehouseID,
	})
	if err != nil {
		t.Fatalf("CreateSerial: %v", err)
	}

	// Three units under one serial is not a thing.
	if _, err = f.svc.Move(f.ctx, inventory.MoveInput{
		CompanyID: f.companyID, WarehouseID: f.warehouseID,
		ProductID: f.product.ID, VariantID: f.variant.ID,
		Type: domain.Receipt, QuantityMicro: 3_000_000, UnitCostMicro: 100_000_000,
		SerialID: serial.ID,
	}); err == nil {
		t.Fatal("three units moved under one serial number")
	} else if code := errs.CodeOf(err); code != domain.CodeSerialQuantity {
		t.Errorf("code = %q, want %q", code, domain.CodeSerialQuantity)
	}

	// Exactly one is fine.
	if _, err = f.svc.Move(f.ctx, inventory.MoveInput{
		CompanyID: f.companyID, WarehouseID: f.warehouseID,
		ProductID: f.product.ID, VariantID: f.variant.ID,
		Type: domain.Receipt, QuantityMicro: 1_000_000, UnitCostMicro: 100_000_000,
		SerialID: serial.ID,
	}); err != nil {
		t.Errorf("one unit under a serial was refused: %v", err)
	}
}

// A serial that has been sold keeps its row, because the warranty claim two years later needs to
// find it — and a deleted row cannot be found.
func TestASoldSerialIsStillFindable(t *testing.T) {
	f := newFixture(t).tracked(t, domain.TrackSerial)

	if _, err := f.svc.CreateSerial(f.ctx, inventory.NewSerialInput{
		CompanyID: f.companyID, VariantID: f.variant.ID, Number: "IMEI-1",
		WarehouseID: f.warehouseID,
	}); err != nil {
		t.Fatalf("CreateSerial: %v", err)
	}

	if err := f.svc.MoveSerial(f.ctx, f.companyID, "IMEI-1", domain.SerialSold,
		id.ID(""), id.ID("")); err != nil {
		t.Fatalf("MoveSerial: %v", err)
	}

	found, err := f.svc.SerialByNumber(f.ctx, f.companyID, "IMEI-1")
	if err != nil {
		t.Fatalf("a sold serial could not be found: %v", err)
	}
	if found.Status != domain.SerialSold {
		t.Errorf("status = %q, want sold", found.Status)
	}

	// And it cannot be sold twice.
	if err = f.svc.MoveSerial(f.ctx, f.companyID, "IMEI-1", domain.SerialSold,
		id.ID(""), id.ID("")); err == nil {
		t.Fatal("a sold serial was sold again")
	}
}

// A serial number identifies exactly one physical thing within a COMPANY — not merely within a
// variant. Two would leave a repair counter with no way to choose which device was scanned.
//
// The two serials here are on DIFFERENT variants, which is what makes this test able to tell
// company-scope from variant-scope. The first version used one variant and could not: the
// mutation drill that narrowed the index to `(variant_id, serial_number)` passed.
func TestASerialNumberIsUniqueAcrossVariantsNotJustWithinOne(t *testing.T) {
	f := newFixture(t).tracked(t, domain.TrackSerial)
	other := f.secondVariant(t)

	if _, err := f.svc.CreateSerial(f.ctx, inventory.NewSerialInput{
		CompanyID: f.companyID, VariantID: f.variant.ID, Number: "IMEI-1",
	}); err != nil {
		t.Fatalf("CreateSerial: %v", err)
	}
	if _, err := f.svc.CreateSerial(f.ctx, inventory.NewSerialInput{
		CompanyID: f.companyID, VariantID: other, Number: "IMEI-1",
	}); err == nil {
		t.Fatal("two different products carried the same serial number in one company")
	}
}

// The lot-number index is the SCHEMA's guarantee, not the service's.
//
// CreateLot looks the number up and refuses a duplicate before the index is ever consulted, so
// the drill that weakened the index passed. But the service guard only protects writes that go
// through the service, and a bad import does not. This writes straight to the table.
//
// The same shape as 3.2's default variant, 3.3's barcode, and 3.5's price target — the fourth
// time this codebase has needed it, and the reason the rule is written down.
func TestTheDatabaseRefusesADuplicateLotNumberWithoutTheService(t *testing.T) {
	f := newFixture(t).tracked(t, domain.TrackLot)
	f.lot(t, "LOT-1", "2027-01-01")

	const insert = `
		INSERT INTO stock_lots (
			id, company_id, variant_id, lot_number, is_quarantined, is_active,
			row_version, created_at, updated_at
		) VALUES (?, ?, ?, 'LOT-1', 0, 1, 1, '2026-01-01', '2026-01-01')`

	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx, insert,
		"dup", string(f.companyID), string(f.variant.ID)); err == nil {
		t.Fatal("the database accepted two lots with the same number for one variant")
	}

	// The same number against a DIFFERENT variant is ordinary — a supplier's batch covers
	// several products — and must still be allowed.
	other := f.secondVariant(t)
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx, insert,
		"ok", string(f.companyID), string(other)); err != nil {
		t.Errorf("the same lot number on a different variant was refused: %v", err)
	}
}
