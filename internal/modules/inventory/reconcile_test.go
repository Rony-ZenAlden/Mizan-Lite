package inventory_test

import (
	"context"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/accounting"
	"github.com/mizan-erp/mizan/internal/modules/inventory"
	"github.com/mizan-erp/mizan/internal/modules/inventory/domain"
)

// booksLedger satisfies inventory's Ledger port from the real accounting service.
type booksLedger struct{ books *accounting.Service }

func (l booksLedger) BalanceOfMapping(
	ctx context.Context, companyID id.ID, key string,
) (int64, error) {
	return l.books.BalanceOfMapping(ctx, companyID, key)
}

// reconciling wires accounting AND the ledger port, so a movement travels the whole path:
// costing → posting rule → journal entry → account balance → reconciliation.
func (f fixture) reconciling(t *testing.T) fixture {
	t.Helper()
	books := f.booked(t)
	f.svc = inventory.NewService(f.store, inventory.Options{
		Clock: clock.System(), Bus: f.bus, Settings: f.settings,
		Ledger: booksLedger{books: books},
	})
	return f
}

// ── the check this phase exists for ─────────────────────────────────────────────

// Every other guarantee here is about one side of the system. This is the one that holds the two
// together, and its failure mode is the worst in the module: stock value and the inventory
// account drift apart, gross margin is quietly wrong, and nothing says so until somebody
// reconciles by hand — which is usually never.
func TestStockReconcilesToTheInventoryAccount(t *testing.T) {
	f := newFixture(t).reconciling(t)

	f.move(t, domain.Receipt, 10_000_000, 100_000_000)
	f.move(t, domain.Receipt, 5_000_000, 200_000_000)
	f.move(t, domain.AdjustmentOut, 2_000_000, 0)
	f.move(t, domain.AdjustmentIn, 1_000_000, 150_000_000)

	result, err := f.svc.Reconcile(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if !result.Balanced() {
		t.Errorf("stock is worth %d and the inventory account holds %d — a difference of %d",
			result.StockValueMinor, result.LedgerValueMinor, result.DifferenceMinor)
	}
	if result.StockValueMinor == 0 {
		t.Error("the reconciliation compared two zeros, which proves nothing")
	}
}

// A movement whose posting is somebody else's — a purchase bill — must NOT be double-counted.
// The stock rises and so does the account, but by the bill's entry rather than inventory's.
func TestReconciliationHoldsWhenAnotherModulePosts(t *testing.T) {
	f := newFixture(t).reconciling(t)

	// Stock arrives on a bill. Inventory publishes nothing; purchasing would post it. Here
	// nobody does, so the ledger is SHORT — and the reconciliation must say so rather than
	// quietly agreeing.
	f.bought(t, 10_000_000, 100_000_000)

	result, err := f.svc.Reconcile(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if result.Balanced() {
		t.Fatal("the shelves hold 1000 and the books hold nothing, and the check agreed")
	}
	if result.DifferenceMinor != 1000 {
		t.Errorf("difference = %d, want 1000 — the bill's entry has not been made",
			result.DifferenceMinor)
	}
}

// The drift in the stock projection is reported alongside, because it is the usual cause of a
// difference and finding both at once saves a second investigation.
func TestReconciliationReportsProjectionDriftToo(t *testing.T) {
	f := newFixture(t).reconciling(t)
	f.move(t, domain.Receipt, 10_000_000, 100_000_000)

	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
		`UPDATE stock_levels SET qty_on_hand_micro = 99000000`); err != nil {
		t.Fatalf("corrupting the projection: %v", err)
	}

	result, err := f.svc.Reconcile(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if result.Balanced() {
		t.Fatal("a corrupted projection reconciled cleanly")
	}
	if len(result.Discrepancies) != 1 {
		t.Errorf("%d discrepancies reported, want the drifted level", len(result.Discrepancies))
	}
}

// It REPORTS. A repair that runs automatically fixes the symptom nightly and hides the bug
// forever — the Phase 2 precedent, unchanged.
func TestReconciliationRepairsNothing(t *testing.T) {
	f := newFixture(t).reconciling(t)
	f.move(t, domain.Receipt, 10_000_000, 100_000_000)

	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
		`UPDATE stock_levels SET qty_on_hand_micro = 99000000`); err != nil {
		t.Fatalf("corrupting the projection: %v", err)
	}
	if _, err := f.svc.Reconcile(f.ctx, f.companyID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if state := f.stock(t); state.OnHandMicro != 99_000_000 {
		t.Error("the reconciliation repaired the projection instead of reporting it")
	}
}

// ── the sweep ───────────────────────────────────────────────────────────────────

// A finding is not an OUTAGE. A failing job retries and eventually alerts as one, and a stock
// reconciliation that does not balance cannot be fixed by running it again — so the finding is
// logged and the job succeeds.
func TestTheSweepSucceedsEvenWhenACompanyDoesNotBalance(t *testing.T) {
	f := newFixture(t).reconciling(t)
	f.bought(t, 10_000_000, 100_000_000) // deliberately unposted: the books are short

	// The finding is real...
	result, err := f.svc.Reconcile(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if result.Balanced() {
		t.Fatal("the fixture does not actually produce a difference")
	}

	// ...and the job still succeeds, because a bookkeeping problem is not a red light on an
	// operations dashboard.
	if err = f.svc.ReconcileAll(f.ctx); err != nil {
		t.Errorf("the sweep failed on a company that did not balance: %v", err)
	}
}

// The sweep asks inventory's own levels which companies to check: a company with no stock has
// nothing to reconcile, which makes it the more precise question as well as the one needing no
// port to org.
func TestTheSweepRunsWithNoStockAtAll(t *testing.T) {
	f := newFixture(t).reconciling(t)

	if err := f.svc.ReconcileAll(f.ctx); err != nil {
		t.Errorf("the sweep failed with nothing to sweep: %v", err)
	}
}

// Without the books wired, the stock half is still worth reporting — but claiming a difference
// of zero against a ledger nobody asked would be a false clean bill of health.
func TestWithoutTheBooksTheCheckDoesNotClaimToBalance(t *testing.T) {
	f := newFixture(t) // no Ledger port
	f.move(t, domain.Receipt, 10_000_000, 100_000_000)

	result, err := f.svc.Reconcile(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if result.StockValueMinor != 1000 {
		t.Errorf("stock value = %d, want 1000", result.StockValueMinor)
	}
	if result.Balanced() {
		t.Error("the check claimed to balance against books it never read")
	}
}
