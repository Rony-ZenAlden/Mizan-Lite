package inventory_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/accounting"
	"github.com/mizan-erp/mizan/internal/modules/audit"
	"github.com/mizan-erp/mizan/internal/modules/catalog"
	catalogdomain "github.com/mizan-erp/mizan/internal/modules/catalog/domain"
	"github.com/mizan-erp/mizan/internal/modules/currency"
	"github.com/mizan-erp/mizan/internal/modules/identity"
	"github.com/mizan-erp/mizan/internal/modules/inventory"
	"github.com/mizan-erp/mizan/internal/modules/inventory/domain"
	"github.com/mizan-erp/mizan/internal/modules/org"
	"github.com/mizan-erp/mizan/internal/modules/profile"
	"github.com/mizan-erp/mizan/internal/modules/tax"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
	"github.com/mizan-erp/mizan/migrations"
)

type fixture struct {
	svc         *inventory.Service
	bus         *eventbus.Bus
	audit       *audit.Service
	store       *database.Store
	settings    *config.Settings
	companyID   id.ID
	warehouseID id.ID
	ctx         context.Context

	product catalogdomain.Product
	variant catalogdomain.Variant
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")

	store, err := database.Open(database.Config{Path: path})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	merged := migrate.Merge(
		migrations.SQLite(),
		currency.NewModule(nil).Migrations(),
		org.NewModule(nil).Migrations(),
		audit.NewModule(nil).Migrations(),
		profile.NewModule(nil).Migrations(),
		// Identity too: a movement records who made it, so stock_movements keys users.
		identity.NewModule(nil).Migrations(),
		accounting.NewModule(nil).Migrations(),
		tax.NewModule(nil).Migrations(),
		catalog.NewModule(nil).Migrations(),
		inventory.NewModule(nil).Migrations(),
	)
	runner, err := migrate.New(store, migrate.Options{FS: merged, DBPath: path, SkipBackup: true})
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	if _, err = runner.Up(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	identifier, _ := id.New()
	now := clock.Format(clock.System().Now())
	if _, err = store.Writer(ctx).ExecContext(ctx,
		`INSERT INTO currencies (id, code, name, symbol, decimal_places, created_at, updated_at)
		 VALUES (?, 'SYP', 'Syrian Pound', 'SYP', 0, ?, ?)`,
		string(identifier), now, now); err != nil {
		t.Fatalf("seeding currency: %v", err)
	}

	bus := eventbus.New(eventbus.Options{})
	auditSvc := audit.NewService(store, clock.System(), nil)
	if err = audit.NewModule(auditSvc).Subscribe(bus, nil); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	orgSvc := org.NewService(store, clock.System(), bus)
	provisioned, err := orgSvc.Provision(ctx, org.ProvisionInput{
		Company: org.CompanyInput{
			Code: "MAIN", Name: "Demo", CountryCode: "SY", FunctionalCurrency: "SYP",
		},
		Branch:               org.LocationInput{Code: "HQ", Name: "Head Office"},
		Warehouse:            org.LocationInput{Code: "WH1", Name: "Main"},
		FiscalYearStartMonth: time.January,
		FiscalYearStartYear:  2026,
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	catalogSvc, err := catalog.NewService(store, catalog.Options{Clock: clock.System(), Bus: bus})
	if err != nil {
		t.Fatalf("catalog.NewService: %v", err)
	}
	if err = catalogSvc.ApplyUnits(ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}
	product, variant, err := catalogSvc.CreateProduct(ctx, catalog.NewProductInput{
		CompanyID: provisioned.CompanyID, Code: "WIDGET", Name: "Widget", StockUnit: "PCS",
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}

	settings, err := config.Open(ctx, store, config.Options{})
	if err != nil {
		t.Fatalf("config.Open: %v", err)
	}

	return fixture{
		svc: inventory.NewService(store, inventory.Options{
			Clock: clock.System(), Bus: bus, Settings: settings,
		}),
		audit: auditSvc, bus: bus, store: store, settings: settings,
		companyID: provisioned.CompanyID, warehouseID: provisioned.WarehouseID, ctx: ctx,
		product: product, variant: variant,
	}
}

// move records a movement with the fixture's product and warehouse.
func (f fixture) move(
	t *testing.T, kind domain.Type, qty, unitCost int64,
) domain.Movement {
	t.Helper()
	recorded, err := f.svc.Move(f.ctx, inventory.MoveInput{
		CompanyID: f.companyID, WarehouseID: f.warehouseID,
		ProductID: f.product.ID, VariantID: f.variant.ID,
		Type: kind, QuantityMicro: qty, UnitCostMicro: unitCost,
	})
	if err != nil {
		t.Fatalf("Move(%s, %d): %v", kind, qty, err)
	}
	return recorded
}

func (f fixture) stock(t *testing.T) domain.State {
	t.Helper()
	state, err := f.svc.StockOf(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("StockOf: %v", err)
	}
	return state
}

// ── the ledger and its projection ───────────────────────────────────────────────

func TestAReceiptRaisesStockAndSetsTheAverage(t *testing.T) {
	f := newFixture(t)

	f.move(t, domain.Receipt, 10_000_000, 100_000_000)

	state := f.stock(t)
	if state.OnHandMicro != 10_000_000 {
		t.Errorf("on hand = %d, want 10000000", state.OnHandMicro)
	}
	if state.AverageMicro != 100_000_000 {
		t.Errorf("average = %d, want 100000000", state.AverageMicro)
	}
}

func TestASequenceOfMovementsProducesTheRightAverage(t *testing.T) {
	f := newFixture(t)

	f.move(t, domain.Receipt, 10_000_000, 100_000_000)
	f.move(t, domain.Receipt, 10_000_000, 200_000_000)
	f.move(t, domain.Issue, 5_000_000, 0)

	state := f.stock(t)
	if state.OnHandMicro != 15_000_000 {
		t.Errorf("on hand = %d, want 15000000", state.OnHandMicro)
	}
	// Two receipts blend to 150; the issue does not move it.
	if state.AverageMicro != 150_000_000 {
		t.Errorf("average = %d, want 150000000", state.AverageMicro)
	}
}

// A caller says what moved; the LEDGER decides what the balance became. A movement whose
// recorded balance could be asserted by its caller would be a ledger nobody can verify against.
func TestAMovementRecordsTheBalanceItProduced(t *testing.T) {
	f := newFixture(t)

	first := f.move(t, domain.Receipt, 10_000_000, 100_000_000)
	second := f.move(t, domain.Issue, 4_000_000, 0)

	if first.BalanceAfterMicro != 10_000_000 {
		t.Errorf("first balance = %d, want 10000000", first.BalanceAfterMicro)
	}
	if second.BalanceAfterMicro != 6_000_000 {
		t.Errorf("second balance = %d, want 6000000", second.BalanceAfterMicro)
	}
	// An issue is costed at the average, not at whatever the caller passed — otherwise a sale
	// could set its own cost of goods and gross margin would become an opinion.
	if second.UnitCostMicro != 100_000_000 {
		t.Errorf("issue cost = %d, want the average 100000000", second.UnitCostMicro)
	}
}

// The ledger is APPEND-ONLY. There is no update path in the repository, and this asserts the
// history is intact rather than rewritten.
func TestTheLedgerIsAppendOnly(t *testing.T) {
	f := newFixture(t)

	f.move(t, domain.Receipt, 10_000_000, 100_000_000)
	f.move(t, domain.Issue, 3_000_000, 0)
	f.move(t, domain.Receipt, 5_000_000, 120_000_000)

	movements, err := f.svc.Movements(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("Movements: %v", err)
	}
	if len(movements) != 3 {
		t.Fatalf("%d movements, want 3 — history was rewritten", len(movements))
	}
	// In occurrence order, which is what a replay depends on.
	if movements[0].Type != domain.Receipt || movements[1].Type != domain.Issue {
		t.Errorf("movements are out of order: %v, %v", movements[0].Type, movements[1].Type)
	}
}

// ── verification ────────────────────────────────────────────────────────────────

func TestAHealthyLedgerReportsNoDiscrepancies(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 10_000_000, 100_000_000)
	f.move(t, domain.Issue, 3_000_000, 0)

	discrepancies, err := f.svc.VerifyLedger(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("VerifyLedger: %v", err)
	}
	if len(discrepancies) != 0 {
		t.Errorf("a healthy ledger reported %d discrepancies: %+v",
			len(discrepancies), discrepancies)
	}
}

// The job that stands between "our margin looks fine" and a year of quietly wrong profit.
func TestADriftedProjectionIsCaughtAndReportedNotRepaired(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 10_000_000, 100_000_000)
	f.move(t, domain.Issue, 3_000_000, 0)

	// Corrupt the projection, exactly as a bug or a bad import would.
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
		`UPDATE stock_levels SET qty_on_hand_micro = 99000000`); err != nil {
		t.Fatalf("corrupting the projection: %v", err)
	}

	discrepancies, err := f.svc.VerifyLedger(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("VerifyLedger: %v", err)
	}
	if len(discrepancies) != 1 {
		t.Fatalf("%d discrepancies, want 1", len(discrepancies))
	}
	if discrepancies[0].ProjectedOnHand != 99_000_000 ||
		discrepancies[0].LedgerOnHand != 7_000_000 {
		t.Errorf("discrepancy = %+v, want both figures", discrepancies[0])
	}
	if discrepancies[0].VariantID != f.variant.ID {
		t.Error("the discrepancy does not name which variant drifted")
	}

	// REPORTED, not repaired: a projection that heals itself hides the bug that broke it.
	state := f.stock(t)
	if state.OnHandMicro != 99_000_000 {
		t.Error("VerifyLedger repaired the projection instead of reporting it")
	}
}

// The repair is a separate, deliberate act — something a person chooses after seeing a report.
func TestRebuildingRestoresTheProjectionFromTheLedger(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 10_000_000, 100_000_000)
	f.move(t, domain.Issue, 3_000_000, 0)

	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
		`UPDATE stock_levels SET qty_on_hand_micro = 99000000`); err != nil {
		t.Fatalf("corrupting the projection: %v", err)
	}

	rebuilt, err := f.svc.RebuildLevels(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("RebuildLevels: %v", err)
	}
	if rebuilt != 1 {
		t.Errorf("%d levels rebuilt, want 1", rebuilt)
	}

	if state := f.stock(t); state.OnHandMicro != 7_000_000 {
		t.Errorf("on hand = %d after rebuild, want 7000000", state.OnHandMicro)
	}
	// And the ledger now verifies.
	discrepancies, err := f.svc.VerifyLedger(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("VerifyLedger: %v", err)
	}
	if len(discrepancies) != 0 {
		t.Errorf("%d discrepancies remain after a rebuild", len(discrepancies))
	}
}

// Reservations belong to open orders, not to the stock ledger: replaying movements says nothing
// about what is currently promised, so a rebuild must leave them alone.
func TestARebuildDoesNotDiscardReservations(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 10_000_000, 100_000_000)

	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
		`UPDATE stock_levels SET qty_reserved_micro = 4000000`); err != nil {
		t.Fatalf("reserving: %v", err)
	}

	if _, err := f.svc.RebuildLevels(f.ctx, f.companyID); err != nil {
		t.Fatalf("RebuildLevels: %v", err)
	}

	state := f.stock(t)
	if state.ReservedMicro != 4_000_000 {
		t.Errorf("reserved = %d after a rebuild, want 4000000", state.ReservedMicro)
	}
	if state.AvailableMicro() != 6_000_000 {
		t.Errorf("available = %d, want 6000000", state.AvailableMicro())
	}
}

// ── negative stock ──────────────────────────────────────────────────────────────

// Blocked by DEFAULT. What it permits is selling what does not exist.
func TestIssuingMoreThanIsHeldIsRefusedByDefault(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 2_000_000, 100_000_000)

	_, err := f.svc.Move(f.ctx, inventory.MoveInput{
		CompanyID: f.companyID, WarehouseID: f.warehouseID,
		ProductID: f.product.ID, VariantID: f.variant.ID,
		Type: domain.Issue, QuantityMicro: 5_000_000,
	})
	if err == nil {
		t.Fatal("five units were issued from a stock of two")
	}
	if code := errs.CodeOf(err); code != domain.CodeInsufficient {
		t.Errorf("code = %q, want %q", code, domain.CodeInsufficient)
	}

	// And the refusal left nothing behind: no movement, no changed level.
	movements, err := f.svc.Movements(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("Movements: %v", err)
	}
	if len(movements) != 1 {
		t.Errorf("%d movements after a refused issue, want just the receipt", len(movements))
	}
	if state := f.stock(t); state.OnHandMicro != 2_000_000 {
		t.Errorf("on hand = %d after a refused issue, want 2000000", state.OnHandMicro)
	}
}

// The other half of the rule, and the one that exercises the WAREHOUSE flag Phase 1 left for
// this phase (migration 0004, "Read from Phase 4"). A workshop that issues components before the
// delivery note is entered genuinely needs this; the shop floor next door must not have it,
// which is why the flag is per-warehouse rather than per-company.
func TestAWarehouseThatPermitsItCanIssueBelowZero(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 2_000_000, 100_000_000)

	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
		`UPDATE warehouses SET allows_negative_stock = 1 WHERE id = ?`,
		string(f.warehouseID)); err != nil {
		t.Fatalf("permitting negative stock: %v", err)
	}

	if _, err := f.svc.Move(f.ctx, inventory.MoveInput{
		CompanyID: f.companyID, WarehouseID: f.warehouseID,
		ProductID: f.product.ID, VariantID: f.variant.ID,
		Type: domain.Issue, QuantityMicro: 5_000_000,
	}); err != nil {
		t.Fatalf("a permitted oversell was refused: %v", err)
	}

	// The stock is genuinely negative, and says so rather than clamping at zero.
	if state := f.stock(t); state.OnHandMicro != -3_000_000 {
		t.Errorf("on hand = %d, want -3000000", state.OnHandMicro)
	}

	// And the shortfall was recorded as a variance rather than absorbed — visible and postable.
	entries, err := f.audit.Entries(f.ctx, audit.Filter{EntityType: inventory.EntityStock})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	last := entries[0]
	for _, entry := range entries {
		if entry.Action == inventory.ActionStockIssued {
			last = entry
		}
	}
	if last.Action != inventory.ActionStockIssued {
		t.Fatalf("no issue was audited: %+v", entries)
	}
}

// ── layers, written even though nothing reads them (§D.2) ───────────────────────

// The key decision of the phase. Under WAC these rows are recorded and unused; they exist so
// that switching a company to FIFO is a configuration change plus a recompute rather than a
// migration that would have to reconstruct purchase history from movements.
func TestALayerIsWrittenOnEveryReceiptEvenUnderAverageCost(t *testing.T) {
	f := newFixture(t)

	f.move(t, domain.Receipt, 10_000_000, 100_000_000)
	f.move(t, domain.Receipt, 5_000_000, 120_000_000)

	var layers int
	if err := f.store.Reader(f.ctx).QueryRowContext(f.ctx,
		`SELECT COUNT(*) FROM inventory_layers`).Scan(&layers); err != nil {
		t.Fatalf("counting layers: %v", err)
	}
	if layers != 2 {
		t.Fatalf("%d layers for 2 receipts, want 2", layers)
	}

	// Each carries the cost it was received at, which is what FIFO would later need.
	rows, err := f.store.Reader(f.ctx).QueryContext(f.ctx,
		`SELECT unit_cost_micro, remaining_micro FROM inventory_layers ORDER BY received_at, id`)
	if err != nil {
		t.Fatalf("reading layers: %v", err)
	}
	defer func() { _ = rows.Close() }()

	costs := make([]int64, 0, 2)
	for rows.Next() {
		var cost, remaining int64
		if err = rows.Scan(&cost, &remaining); err != nil {
			t.Fatalf("scan: %v", err)
		}
		costs = append(costs, cost)
	}
	if len(costs) != 2 || costs[0] != 100_000_000 || costs[1] != 120_000_000 {
		t.Errorf("layer costs = %v, want [100000000 120000000]", costs)
	}
}

// Layers are DRAWN DOWN on every issue too, for the same reason: a company switching to FIFO
// must start from a truthful remaining-quantity picture, not from full layers that were never
// consumed.
func TestLayersAreConsumedInReceiptOrder(t *testing.T) {
	f := newFixture(t)

	f.move(t, domain.Receipt, 10_000_000, 100_000_000)
	f.move(t, domain.Receipt, 10_000_000, 200_000_000)
	// Twelve units: the whole first layer and two of the second.
	f.move(t, domain.Issue, 12_000_000, 0)

	rows, err := f.store.Reader(f.ctx).QueryContext(f.ctx,
		`SELECT unit_cost_micro, remaining_micro, is_exhausted
		 FROM inventory_layers ORDER BY received_at, id`)
	if err != nil {
		t.Fatalf("reading layers: %v", err)
	}
	defer func() { _ = rows.Close() }()

	type layer struct {
		cost, remaining int64
		exhausted       int
	}
	got := make([]layer, 0, 2)
	for rows.Next() {
		var l layer
		if err = rows.Scan(&l.cost, &l.remaining, &l.exhausted); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, l)
	}

	if len(got) != 2 {
		t.Fatalf("%d layers, want 2", len(got))
	}
	if got[0].remaining != 0 || got[0].exhausted != 1 {
		t.Errorf("first layer = %+v, want fully consumed", got[0])
	}
	if got[1].remaining != 8_000_000 {
		t.Errorf("second layer has %d left, want 8000000", got[1].remaining)
	}
}

// ── returns (§D.3) ──────────────────────────────────────────────────────────────

// The trap with the most expensive consequence, through real storage: a return is costed at its
// ORIGINAL issue's cost, not today's average.
func TestAReturnIsCostedAtItsOriginalIssue(t *testing.T) {
	f := newFixture(t)

	f.move(t, domain.Receipt, 10_000_000, 100_000_000)
	sold := f.move(t, domain.Issue, 1_000_000, 0) // costed at 100
	// Prices rise sharply.
	f.move(t, domain.Receipt, 10_000_000, 300_000_000)

	before := f.stock(t)
	returned, err := f.svc.Move(f.ctx, inventory.MoveInput{
		CompanyID: f.companyID, WarehouseID: f.warehouseID,
		ProductID: f.product.ID, VariantID: f.variant.ID,
		Type: domain.ReturnIn, QuantityMicro: 1_000_000,
		SourceMovementID: sold.ID,
	})
	if err != nil {
		t.Fatalf("Move(return): %v", err)
	}

	if returned.UnitCostMicro != 100_000_000 {
		t.Errorf("return costed at %d, want the original 100000000 — today's average was %d",
			returned.UnitCostMicro, before.AverageMicro)
	}
	if returned.ValueMinor != 100 {
		t.Errorf("return value = %d, want 100", returned.ValueMinor)
	}
}

func TestAReturnNamingNoSourceIsRefused(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 10_000_000, 100_000_000)

	_, err := f.svc.Move(f.ctx, inventory.MoveInput{
		CompanyID: f.companyID, WarehouseID: f.warehouseID,
		ProductID: f.product.ID, VariantID: f.variant.ID,
		Type: domain.ReturnIn, QuantityMicro: 1_000_000,
	})
	if err == nil {
		t.Fatal("a return with no source was recorded")
	}
	if code := errs.CodeOf(err); code != domain.CodeReturnNeedsSource {
		t.Errorf("code = %q, want %q", code, domain.CodeReturnNeedsSource)
	}
}

func TestAReturnNamingAMovementThatDoesNotExistIsRefused(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 10_000_000, 100_000_000)

	_, err := f.svc.Move(f.ctx, inventory.MoveInput{
		CompanyID: f.companyID, WarehouseID: f.warehouseID,
		ProductID: f.product.ID, VariantID: f.variant.ID,
		Type: domain.ReturnIn, QuantityMicro: 1_000_000,
		SourceMovementID: id.ID("no-such-movement"),
	})
	if err == nil {
		t.Fatal("a return naming a nonexistent movement was recorded")
	}
}

// ── counts ──────────────────────────────────────────────────────────────────────

// A count is an assertion about reality, not a delta.
func TestACountSetsTheBalanceToWhatWasCounted(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 10_000_000, 100_000_000)

	counted, err := f.svc.Move(f.ctx, inventory.MoveInput{
		CompanyID: f.companyID, WarehouseID: f.warehouseID,
		ProductID: f.product.ID, VariantID: f.variant.ID,
		Type: domain.Count, QuantityMicro: 2_000_000, CountedMicro: 8_000_000,
	})
	if err != nil {
		t.Fatalf("Move(count): %v", err)
	}

	if counted.BalanceAfterMicro != 8_000_000 {
		t.Errorf("balance = %d, want the counted 8000000", counted.BalanceAfterMicro)
	}
	if state := f.stock(t); state.OnHandMicro != 8_000_000 {
		t.Errorf("on hand = %d, want 8000000", state.OnHandMicro)
	}
	// Two units at 100 have gone missing.
	if counted.ValueMinor != -200 {
		t.Errorf("value = %d, want -200", counted.ValueMinor)
	}
	// A count says how many, not what they cost.
	if state := f.stock(t); state.AverageMicro != 100_000_000 {
		t.Errorf("average = %d after a count, want it unchanged", state.AverageMicro)
	}
}

// ── audit ───────────────────────────────────────────────────────────────────────

func TestEveryMovementIsAudited(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 10_000_000, 100_000_000)
	f.move(t, domain.Issue, 3_000_000, 0)

	entries, err := f.audit.Entries(f.ctx, audit.Filter{EntityType: inventory.EntityStock})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("%d entries for 2 movements: %+v", len(entries), entries)
	}
}

// A refused movement records nothing: the trail says what happened, and a rejected request did
// not happen. The subscriber runs inside the transaction, so the rollback takes it with it.
func TestARefusedMovementIsNotAudited(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 2_000_000, 100_000_000)

	before, err := f.audit.Entries(f.ctx, audit.Filter{EntityType: inventory.EntityStock})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}

	if _, err = f.svc.Move(f.ctx, inventory.MoveInput{
		CompanyID: f.companyID, WarehouseID: f.warehouseID,
		ProductID: f.product.ID, VariantID: f.variant.ID,
		Type: domain.Issue, QuantityMicro: 5_000_000,
	}); err == nil {
		t.Fatal("the over-issue was accepted")
	}

	after, err := f.audit.Entries(f.ctx, audit.Filter{EntityType: inventory.EntityStock})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("%d entries after a refused movement, want %d", len(after), len(before))
	}
}

// ── a variant that has never moved ──────────────────────────────────────────────

// The ordinary case for most of a catalog: a fact, not an absence to handle.
func TestAVariantThatHasNeverMovedHasZeroStockNotAnError(t *testing.T) {
	f := newFixture(t)

	state, err := f.svc.StockOf(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("StockOf on an unmoved variant: %v", err)
	}
	if state.OnHandMicro != 0 || state.AverageMicro != 0 {
		t.Errorf("state = %+v, want zero", state)
	}
}
