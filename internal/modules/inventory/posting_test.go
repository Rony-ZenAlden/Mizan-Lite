package inventory_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/modules/accounting"
	"github.com/mizan-erp/mizan/internal/modules/inventory"
	"github.com/mizan-erp/mizan/internal/modules/inventory/domain"
)

// booked wires accounting onto the same bus, with the shipped chart and posting rules applied —
// so a movement travels the real path: publish → rule → journal entry → trial balance.
func (f fixture) booked(t *testing.T) *accounting.Service {
	t.Helper()

	svc, err := accounting.NewService(f.store, accounting.Options{
		Clock: clock.System(), Bus: f.bus,
	})
	if err != nil {
		t.Fatalf("accounting.NewService: %v", err)
	}
	if err = accounting.NewModule(svc).Subscribe(f.bus, nil); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err = svc.ApplyChart(f.ctx, f.companyID, "generic_trading"); err != nil {
		t.Fatalf("ApplyChart: %v", err)
	}
	if err = svc.ApplyRules(f.ctx, f.companyID, "generic_trading"); err != nil {
		t.Fatalf("ApplyRules: %v", err)
	}
	return svc
}

// balances reads every account's closing position for the open period, keyed by code.
func (f fixture) balances(t *testing.T, books *accounting.Service) map[string]int64 {
	t.Helper()

	years, err := books.Years(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Years: %v", err)
	}
	if len(years) == 0 {
		t.Fatal("the company has no fiscal year")
	}
	periods, err := books.Periods(f.ctx, years[0].ID)
	if err != nil {
		t.Fatalf("Periods: %v", err)
	}

	// Summed as NET MOVEMENT (debit less credit) across every period, not as Closing: Closing is
	// cumulative — it already includes the opening — so adding it up across periods would count
	// each earlier period once more for every later one. Every account here starts at zero, so
	// total movement is the balance.
	out := map[string]int64{}
	for _, period := range periods {
		rows, tbErr := books.TrialBalance(f.ctx, f.companyID, period.ID)
		if tbErr != nil {
			t.Fatalf("TrialBalance: %v", tbErr)
		}
		for _, row := range rows {
			out[row.AccountCode] += row.Debit - row.Credit
		}
	}
	return out
}

// bought receives stock the way a real purchase does: against a bill, which purchasing will
// post. Inventory therefore posts nothing for it, which is what lets the tests below isolate the
// adjustment they are actually about.
func (f fixture) bought(t *testing.T, qty, unitCost int64) {
	t.Helper()
	if _, err := f.svc.Move(f.ctx, inventory.MoveInput{
		CompanyID: f.companyID, WarehouseID: f.warehouseID,
		ProductID: f.product.ID, VariantID: f.variant.ID,
		Type: domain.Receipt, QuantityMicro: qty, UnitCostMicro: unitCost,
		DocumentType: "purchasing.bill", DocumentID: f.variant.ID,
	}); err != nil {
		t.Fatalf("bought(%d): %v", qty, err)
	}
}

// ── stock found and stock lost are DIFFERENT events ─────────────────────────────

// Phase 2's engine refuses a negative amount — a negative would flip a line's side silently, and
// "debit XOR credit, both non-negative" is what makes an entry checkable. So the reverse
// operation is its own event with its own rule.
func TestStockFoundDebitsInventoryAndCreditsTheAdjustmentAccount(t *testing.T) {
	f := newFixture(t)
	books := f.booked(t)

	// Stock arrives on a bill, which purchasing posts. Then two more units are FOUND.
	f.bought(t, 10_000_000, 100_000_000)
	f.move(t, domain.AdjustmentIn, 2_000_000, 100_000_000)

	balances := f.balances(t, books)
	// Debit-positive throughout: inventory rises by the two found units, and the adjustment
	// account is credited. The ten bought units are the bill's entry, not this one's.
	if balances["1300"] != 200 {
		t.Errorf("inventory = %d, want 200 (the 2 found units at 100)", balances["1300"])
	}
	if balances["5600"] != -200 {
		t.Errorf("stock adjustments = %d, want -200 (a credit of 200)", balances["5600"])
	}
}

func TestStockLostDebitsTheAdjustmentAccountAndCreditsInventory(t *testing.T) {
	f := newFixture(t)
	books := f.booked(t)

	f.bought(t, 10_000_000, 100_000_000)
	f.move(t, domain.AdjustmentOut, 3_000_000, 0)

	balances := f.balances(t, books)
	// Only the write-off is inventory's entry: three units at 100, credited out.
	if balances["1300"] != -300 {
		t.Errorf("inventory = %d, want -300 (3 units written off at 100)", balances["1300"])
	}
	// The loss is an expense: a debit, positive under the debit-positive convention.
	if balances["5600"] != 300 {
		t.Errorf("stock adjustments = %d, want 300", balances["5600"])
	}
}

// The whole reason inventory posts at all: stock value must reconcile to the inventory account,
// on every day, without anybody adding it up by hand.
func TestStockValueReconcilesToTheInventoryAccount(t *testing.T) {
	f := newFixture(t)
	books := f.booked(t)

	// Every movement here is inventory's own, so the inventory account should end up holding
	// exactly what the shelf is worth.
	f.move(t, domain.Receipt, 10_000_000, 100_000_000)
	f.move(t, domain.Receipt, 5_000_000, 200_000_000)
	f.move(t, domain.AdjustmentOut, 2_000_000, 0)
	f.move(t, domain.AdjustmentIn, 1_000_000, 150_000_000)

	state := f.stock(t)
	// Value on the shelf. Both figures are ×10⁶, so the product is ×10¹² — computed in one step
	// with a single rounding, exactly as the domain does it. Dividing each factor down first
	// truncates twice and manufactures a difference that is not there.
	shelf := (state.OnHandMicro*state.AverageMicro + 500_000_000_000) / 1_000_000_000_000

	balances := f.balances(t, books)
	if balances["1300"] != shelf {
		t.Errorf("the inventory account holds %d but the shelf is worth %d — the two have drifted",
			balances["1300"], shelf)
	}
}

// ── who posts (the double-posting rule) ─────────────────────────────────────────

// A movement caused by a DOCUMENT is posted by that document's module, as part of one entry that
// also records the payable or the revenue. If inventory posted those too, every purchase would
// debit inventory twice.
func TestAMovementWithADocumentIsNotPostedByInventory(t *testing.T) {
	f := newFixture(t)
	books := f.booked(t)

	if _, err := f.svc.Move(f.ctx, inventory.MoveInput{
		CompanyID: f.companyID, WarehouseID: f.warehouseID,
		ProductID: f.product.ID, VariantID: f.variant.ID,
		Type: domain.Receipt, QuantityMicro: 10_000_000, UnitCostMicro: 100_000_000,
		DocumentType: "purchasing.bill", DocumentID: f.variant.ID,
	}); err != nil {
		t.Fatalf("Move: %v", err)
	}

	balances := f.balances(t, books)
	if balances["1300"] != 0 {
		t.Errorf("inventory = %d — the bill's own entry will post this, so inventory posted twice",
			balances["1300"])
	}
	// The stock still moved; only the posting was left to the document's module.
	if state := f.stock(t); state.OnHandMicro != 10_000_000 {
		t.Errorf("on hand = %d, want the stock to have moved regardless", state.OnHandMicro)
	}
}

// A receipt with no document IS inventory's to post — an opening balance, or goods found.
func TestAMovementWithNoDocumentIsPostedByInventory(t *testing.T) {
	f := newFixture(t)
	books := f.booked(t)

	f.move(t, domain.Receipt, 10_000_000, 100_000_000)

	if balances := f.balances(t, books); balances["1300"] != 1000 {
		t.Errorf("inventory = %d, want 1000 — nobody else was going to post this",
			balances["1300"])
	}
}

// A transfer moves value between warehouses; the company owns the same goods in a different
// place, so total inventory value is unchanged and there is no entry to make. Posting one would
// be a journal entry whose two sides are the same account.
func TestATransferPostsNothing(t *testing.T) {
	f := newFixture(t)
	books := f.booked(t)

	f.bought(t, 10_000_000, 100_000_000)
	before := f.balances(t, books)

	f.move(t, domain.TransferOut, 4_000_000, 0)

	after := f.balances(t, books)
	if after["1300"] != before["1300"] {
		t.Errorf("inventory moved from %d to %d on a transfer", before["1300"], after["1300"])
	}
	if after["5600"] != before["5600"] {
		t.Errorf("a transfer touched the adjustment account: %d → %d",
			before["5600"], after["5600"])
	}
}

// A quantity change with no money in it posts nothing. An entry of zero on both sides is noise
// in a ledger somebody has to read.
func TestAMovementWorthNothingPostsNothing(t *testing.T) {
	f := newFixture(t)
	books := f.booked(t)

	// A variant whose average cost is zero: a real quantity change, no money.
	f.move(t, domain.AdjustmentIn, 5_000_000, 0)

	balances := f.balances(t, books)
	if balances["1300"] != 0 || balances["5600"] != 0 {
		t.Errorf("a costless movement posted: inventory %d, adjustments %d",
			balances["1300"], balances["5600"])
	}
	if state := f.stock(t); state.OnHandMicro != 5_000_000 {
		t.Errorf("on hand = %d — the quantity should still have moved", state.OnHandMicro)
	}

	// NOTE: this test cannot distinguish inventory's zero guard from Phase 2's own skipping of
	// zero-valued lines — the drill for the guard passed, and checking for a stray journal entry
	// header passed too, because the engine declines to create one when every line is zero.
	//
	// The guard is kept anyway, and its benefit is honestly stated in postingFor: it avoids
	// publishing and a rules lookup, not a wrong number. No assertion is written for it, because
	// an assertion that passes either way teaches the next reader that something is pinned when
	// nothing is.
}

// ── a count's variance reaches the books ────────────────────────────────────────

func TestACountShortfallIsPostedAsALoss(t *testing.T) {
	f := newFixture(t)
	books := f.booked(t)

	f.bought(t, 10_000_000, 100_000_000)
	if _, err := f.svc.Move(f.ctx, inventory.MoveInput{
		CompanyID: f.companyID, WarehouseID: f.warehouseID,
		ProductID: f.product.ID, VariantID: f.variant.ID,
		Type: domain.Count, QuantityMicro: 2_000_000, CountedMicro: 8_000_000,
		Reason: "annual count",
	}); err != nil {
		t.Fatalf("Move(count): %v", err)
	}

	balances := f.balances(t, books)
	// The count's own entry: two units gone.
	if balances["1300"] != -200 {
		t.Errorf("inventory = %d, want -200", balances["1300"])
	}
	if balances["5600"] != 200 {
		t.Errorf("stock adjustments = %d, want 200 — the two missing units", balances["5600"])
	}
}

// ── the entry is atomic with the movement ───────────────────────────────────────

// A stock write-off whose accounting entry is missing is worse than a write-off that failed. The
// bus is synchronous and inside the transaction, so a rule that refuses stops the movement.
func TestAMovementIsRolledBackWhenItsPostingFails(t *testing.T) {
	f := newFixture(t)
	books := f.booked(t)

	f.bought(t, 10_000_000, 100_000_000)
	before := f.stock(t)

	// Remove the mapping the increase rule needs. The next adjustment then cannot post.
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
		`DELETE FROM account_mappings WHERE mapping_key = 'INVENTORY_ADJUSTMENT'`); err != nil {
		t.Fatalf("removing the mapping: %v", err)
	}

	_, err := f.svc.Move(f.ctx, inventory.MoveInput{
		CompanyID: f.companyID, WarehouseID: f.warehouseID,
		ProductID: f.product.ID, VariantID: f.variant.ID,
		Type: domain.AdjustmentIn, QuantityMicro: 5_000_000, UnitCostMicro: 100_000_000,
	})
	if err == nil {
		t.Fatal("a movement whose posting could not be made was recorded anyway")
	}

	// Nothing moved: not the level, not the ledger.
	if after := f.stock(t); after.OnHandMicro != before.OnHandMicro {
		t.Errorf("on hand = %d after a failed posting, want %d",
			after.OnHandMicro, before.OnHandMicro)
	}
	movements, err := f.svc.Movements(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("Movements: %v", err)
	}
	if len(movements) != 1 {
		t.Errorf("%d movements, want only the original receipt", len(movements))
	}
	_ = books
}

// ── inventory contains no accounting logic ──────────────────────────────────────

// The §20.3 guarantee, asserted structurally: this module names no account, no debit, and no
// credit. It says what happened and how much it was worth; the RULES decide the rest, which is
// what lets a country that books shrinkage differently be a seed file rather than a build.
func TestInventoryPublishesAmountsAndNamesNoAccounts(t *testing.T) {
	f := newFixture(t)
	books := f.booked(t)

	f.bought(t, 10_000_000, 100_000_000)

	// Rewriting the RULE alone changes where the money lands — no inventory code involved.
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
		`UPDATE posting_rule_lines SET account_selector = 'mapping:COGS'
		 WHERE account_selector = 'mapping:INVENTORY_ADJUSTMENT'`); err != nil {
		t.Fatalf("rewriting the rule: %v", err)
	}

	f.move(t, domain.AdjustmentOut, 2_000_000, 0)

	balances := f.balances(t, books)
	if balances["5600"] != 0 {
		t.Errorf("the adjustment account holds %d, but the rule now says COGS", balances["5600"])
	}
	if balances["5100"] != 200 {
		t.Errorf("COGS = %d, want 200 — the rule alone decided this", balances["5100"])
	}
}
