package sales_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/sales"
	"github.com/mizan-erp/mizan/internal/modules/sales/domain"
)

func (f fixture) openShift(t *testing.T, floatMinor int64) domain.Shift {
	t.Helper()
	opened, err := f.svc.OpenShift(f.ctx, sales.OpenShiftInput{
		CompanyID: f.companyID, BranchID: f.branchID, Terminal: "TILL-1",
		FloatMinor: floatMinor,
	})
	if err != nil {
		t.Fatalf("OpenShift: %v", err)
	}
	return opened
}

// payInto takes a payment against a shift.
func (f fixture) payInto(
	t *testing.T, shiftID id.ID, method domain.Method, amount int64,
	settle ...sales.SettleInput,
) {
	t.Helper()
	if _, err := f.svc.TakePayment(f.ctx, sales.NewPaymentInput{
		CompanyID: f.companyID, BranchID: f.branchID, Date: "2026-06-15",
		Method: method, Currency: "SYP", AmountMinor: amount,
		ShiftID: shiftID, Settle: settle,
	}); err != nil {
		t.Fatalf("TakePayment: %v", err)
	}
}

// ── the reconciliation, end to end ──────────────────────────────────────────────

func TestAShiftExpectsItsFloatPlusTheCashItTook(t *testing.T) {
	f := newPayingFixture(t)
	shift := f.openShift(t, 100)

	invoice, _ := f.sold(t, 3_000_000) // 300
	f.payInto(t, shift.ID, domain.Cash, 300,
		sales.SettleInput{DocumentID: invoice.ID, AmountMinor: 300})

	closed, err := f.svc.CloseShift(f.ctx, f.companyID, shift.ID, 400, "")
	if err != nil {
		t.Fatalf("CloseShift: %v", err)
	}

	if closed.ExpectedMinor != 400 {
		t.Errorf("expected = %d, want 400 (100 float + 300 cash)", closed.ExpectedMinor)
	}
	if !closed.Balanced() {
		t.Errorf("difference = %d, want 0", closed.DifferenceMinor)
	}
}

// A card payment does not put money in the drawer. Counting it would make every shift that took a
// card look short by exactly that amount.
func TestOnlyCashCountsTowardsTheDrawer(t *testing.T) {
	f := newPayingFixture(t)
	shift := f.openShift(t, 100)

	cashSale, _ := f.sold(t, 2_000_000) // 200
	cardSale, _ := f.sold(t, 5_000_000) // 500

	f.payInto(t, shift.ID, domain.Cash, 200,
		sales.SettleInput{DocumentID: cashSale.ID, AmountMinor: 200})
	f.payInto(t, shift.ID, domain.Card, 500,
		sales.SettleInput{DocumentID: cardSale.ID, AmountMinor: 500})

	// The drawer should hold the float plus the CASH only.
	closed, err := f.svc.CloseShift(f.ctx, f.companyID, shift.ID, 300, "")
	if err != nil {
		t.Fatalf("CloseShift: %v", err)
	}
	if closed.ExpectedMinor != 300 {
		t.Errorf("expected = %d, want 300 — the card payment was counted into the drawer",
			closed.ExpectedMinor)
	}
	if !closed.Balanced() {
		t.Errorf("difference = %d, want 0", closed.DifferenceMinor)
	}
}

// A payment taken outside the POS belongs to no till. Forcing one would make a shift's cash
// figure include money that never reached its drawer.
func TestAPaymentOutsideTheTillDoesNotAffectTheShift(t *testing.T) {
	f := newPayingFixture(t)
	shift := f.openShift(t, 100)

	invoice, _ := f.sold(t, 2_000_000)
	// No ShiftID: a transfer that arrived while the shop was shut.
	f.pay(t, domain.BankTransfer, 200,
		sales.SettleInput{DocumentID: invoice.ID, AmountMinor: 200})

	closed, err := f.svc.CloseShift(f.ctx, f.companyID, shift.ID, 100, "")
	if err != nil {
		t.Fatalf("CloseShift: %v", err)
	}
	if closed.ExpectedMinor != 100 {
		t.Errorf("expected = %d, want just the float", closed.ExpectedMinor)
	}
}

// ── the difference reaches the books ────────────────────────────────────────────

// The single most useful number a shop's owner gets from a POS. A system that quietly adjusts the
// expectation to match the count destroys exactly that.
func TestAShortTillIsPostedAsAnExpense(t *testing.T) {
	f := newPayingFixture(t)
	books := f.booked(t)
	shift := f.openShift(t, 100)

	invoice, _ := f.sold(t, 3_000_000)
	f.payInto(t, shift.ID, domain.Cash, 300,
		sales.SettleInput{DocumentID: invoice.ID, AmountMinor: 300})

	// Should hold 400; the drawer had 350.
	closed, err := f.svc.CloseShift(f.ctx, f.companyID, shift.ID, 350, "counted twice")
	if err != nil {
		t.Fatalf("CloseShift: %v", err)
	}
	if closed.DifferenceMinor != -50 {
		t.Fatalf("difference = %d, want -50", closed.DifferenceMinor)
	}

	balances := f.balances(t, books)
	// The shortfall is an expense, and cash is reduced to what was actually counted.
	if balances["5900"] != 50 {
		t.Errorf("till differences = %d, want 50", balances["5900"])
	}
	// 300 taken in cash, less the 50 that was missing.
	if balances["1110"] != 250 {
		t.Errorf("cash = %d, want 250", balances["1110"])
	}
}

func TestAnOverTillIsPostedTheOtherWay(t *testing.T) {
	f := newPayingFixture(t)
	books := f.booked(t)
	shift := f.openShift(t, 100)

	invoice, _ := f.sold(t, 3_000_000)
	f.payInto(t, shift.ID, domain.Cash, 300,
		sales.SettleInput{DocumentID: invoice.ID, AmountMinor: 300})

	if _, err := f.svc.CloseShift(f.ctx, f.companyID, shift.ID, 450, ""); err != nil {
		t.Fatalf("CloseShift: %v", err)
	}

	balances := f.balances(t, books)
	if balances["5900"] != -50 {
		t.Errorf("till differences = %d, want -50 (a credit)", balances["5900"])
	}
	if balances["1110"] != 350 {
		t.Errorf("cash = %d, want 350", balances["1110"])
	}
}

// An entry of zero on both sides is noise in a ledger somebody has to read.
func TestAShiftThatBalancesPostsNothing(t *testing.T) {
	f := newPayingFixture(t)
	books := f.booked(t)
	shift := f.openShift(t, 100)

	invoice, _ := f.sold(t, 3_000_000)
	f.payInto(t, shift.ID, domain.Cash, 300,
		sales.SettleInput{DocumentID: invoice.ID, AmountMinor: 300})

	if _, err := f.svc.CloseShift(f.ctx, f.companyID, shift.ID, 400, ""); err != nil {
		t.Fatalf("CloseShift: %v", err)
	}

	balances := f.balances(t, books)
	if balances["5900"] != 0 {
		t.Errorf("till differences = %d for a shift that balanced", balances["5900"])
	}
}

// ── one open shift per till ─────────────────────────────────────────────────────

// Two would make "which shift did this payment belong to" unanswerable, and that answer is the
// whole point of the table.
func TestATillCannotOpenTwoShifts(t *testing.T) {
	f := newPayingFixture(t)
	f.openShift(t, 100)

	_, err := f.svc.OpenShift(f.ctx, sales.OpenShiftInput{
		CompanyID: f.companyID, BranchID: f.branchID, Terminal: "TILL-1", FloatMinor: 100,
	})
	if err == nil {
		t.Fatal("a till opened two shifts")
	}
	if code := errs.CodeOf(err); code != domain.CodeShiftOpen {
		t.Errorf("code = %q, want %q", code, domain.CodeShiftOpen)
	}
}

// The index is the SCHEMA's guarantee, not the service's.
//
// OpenShift looks for an existing open shift and refuses before the index is ever consulted, so
// weakening it changed nothing a test could see — the sixth time this codebase has needed a test
// that writes straight to the table, after 3.2, 3.3, 3.5, 4.5, and 5.2.
//
// The service guard protects writes that go through the service. A bad import does not.
func TestTheDatabaseRefusesASecondOpenShiftOnOneTill(t *testing.T) {
	f := newPayingFixture(t)
	f.openShift(t, 100)

	const insert = `
		INSERT INTO pos_shifts (
			id, company_id, branch_id, terminal, opened_at, opening_float_minor,
			status, row_version, created_at, updated_at
		) VALUES (?, ?, ?, 'TILL-1', '2026-06-15T08:00:00Z', 100, ?, 1,
		          '2026-01-01', '2026-01-01')`

	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx, insert,
		"second", string(f.companyID), string(f.branchID), "open"); err == nil {
		t.Fatal("one till held two open shifts")
	}

	// A CLOSED shift on the same till is ordinary — a till trades day after day — and the
	// partial index must not forbid it. Closed rows carry their reconciliation figures, which
	// the CHECK requires.
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx, `
		INSERT INTO pos_shifts (
			id, company_id, branch_id, terminal, opened_at, opening_float_minor,
			closed_at, expected_minor, counted_minor, difference_minor,
			status, row_version, created_at, updated_at
		) VALUES ('yesterday', ?, ?, 'TILL-1', '2026-06-14T08:00:00Z', 100,
		          '2026-06-14T18:00:00Z', 100, 100, 0, 'closed', 1,
		          '2026-01-01', '2026-01-01')`,
		string(f.companyID), string(f.branchID)); err != nil {
		t.Errorf("a closed shift on the same till was refused: %v", err)
	}
}

// Half a reconciliation is a number somebody will read as complete.
func TestTheDatabaseRefusesAHalfReconciledShift(t *testing.T) {
	f := newPayingFixture(t)

	// Closed, but with no count.
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx, `
		INSERT INTO pos_shifts (
			id, company_id, branch_id, terminal, opened_at, opening_float_minor,
			closed_at, status, row_version, created_at, updated_at
		) VALUES ('half', ?, ?, 'TILL-9', '2026-06-15T08:00:00Z', 100,
		          '2026-06-15T18:00:00Z', 'closed', 1, '2026-01-01', '2026-01-01')`,
		string(f.companyID), string(f.branchID)); err == nil {
		t.Error("a shift was closed without being counted")
	}

	// Open, but carrying a count.
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx, `
		INSERT INTO pos_shifts (
			id, company_id, branch_id, terminal, opened_at, opening_float_minor,
			counted_minor, status, row_version, created_at, updated_at
		) VALUES ('early', ?, ?, 'TILL-8', '2026-06-15T08:00:00Z', 100,
		          400, 'open', 1, '2026-01-01', '2026-01-01')`,
		string(f.companyID), string(f.branchID)); err == nil {
		t.Error("an open shift carried a count")
	}
}

// A second TILL is a different drawer, and must be able to trade at the same time.
func TestASecondTillCanTradeAtTheSameTime(t *testing.T) {
	f := newPayingFixture(t)
	f.openShift(t, 100)

	if _, err := f.svc.OpenShift(f.ctx, sales.OpenShiftInput{
		CompanyID: f.companyID, BranchID: f.branchID, Terminal: "TILL-2", FloatMinor: 50,
	}); err != nil {
		t.Errorf("a second till could not open: %v", err)
	}
}

// And after closing, the same till can open again.
func TestATillCanOpenAgainAfterClosing(t *testing.T) {
	f := newPayingFixture(t)
	first := f.openShift(t, 100)

	if _, err := f.svc.CloseShift(f.ctx, f.companyID, first.ID, 100, ""); err != nil {
		t.Fatalf("CloseShift: %v", err)
	}
	if _, err := f.svc.OpenShift(f.ctx, sales.OpenShiftInput{
		CompanyID: f.companyID, BranchID: f.branchID, Terminal: "TILL-1", FloatMinor: 100,
	}); err != nil {
		t.Errorf("a till could not reopen after closing: %v", err)
	}
}

func TestAClosedShiftCannotBeClosedAgainThroughTheService(t *testing.T) {
	f := newPayingFixture(t)
	shift := f.openShift(t, 100)

	if _, err := f.svc.CloseShift(f.ctx, f.companyID, shift.ID, 100, ""); err != nil {
		t.Fatalf("CloseShift: %v", err)
	}
	if _, err := f.svc.CloseShift(f.ctx, f.companyID, shift.ID, 200, ""); err == nil {
		t.Fatal("a closed shift was closed again")
	}
}

// ── finding the current shift ───────────────────────────────────────────────────

func TestATillReportsItsOpenShift(t *testing.T) {
	f := newPayingFixture(t)
	opened := f.openShift(t, 100)

	current, err := f.svc.CurrentShift(f.ctx, f.branchID, "TILL-1")
	if err != nil {
		t.Fatalf("CurrentShift: %v", err)
	}
	if current.ID != opened.ID {
		t.Error("the till reported a different shift")
	}

	if _, err = f.svc.CurrentShift(f.ctx, f.branchID, "TILL-9"); err == nil {
		t.Fatal("a till with no shift reported one")
	}
}
