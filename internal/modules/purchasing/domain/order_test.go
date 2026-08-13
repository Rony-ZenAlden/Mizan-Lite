package domain_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/purchasing/domain"
)

func newID(t *testing.T) id.ID {
	t.Helper()
	identifier, err := id.New()
	if err != nil {
		t.Fatalf("id.New: %v", err)
	}
	return identifier
}

func draftOrder(t *testing.T) domain.Order {
	t.Helper()
	order, err := domain.NewOrder(
		newID(t), newID(t), newID(t), newID(t), newID(t),
		"Acme Supplies", "2026-08-13", "SAR")
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	return order
}

func line(t *testing.T, quantityMicro int64) domain.Line {
	t.Helper()
	l, err := domain.NewLine(newID(t), newID(t), newID(t), newID(t), 1,
		quantityMicro, quantityMicro)
	if err != nil {
		t.Fatalf("NewLine: %v", err)
	}
	return l
}

// ── the supplier ────────────────────────────────────────────────────────────────

func TestAnOrderNeedsASupplier(t *testing.T) {
	// Unlike a sale's customer. A shop sells to whoever walks in and §2.6 requires that a walk-in
	// needs no record; nobody orders from nobody. The whole content of the document is that a
	// NAMED party has undertaken to deliver.
	_, err := domain.NewOrder(newID(t), newID(t), newID(t), newID(t), id.ID(""),
		"", "2026-08-13", "SAR")
	if code := errs.CodeOf(err); code != domain.CodeNoSupplier {
		t.Fatalf("code = %q, want %q", code, domain.CodeNoSupplier)
	}
}

func TestAnOrderNeedsToSayWhereGoodsAreExpected(t *testing.T) {
	// A business with two warehouses orders for one of them. A delivery that arrives at the
	// other is a problem somebody solves on the loading bay, at the worst possible moment.
	_, err := domain.NewOrder(newID(t), newID(t), newID(t), id.ID(""), newID(t),
		"Acme Supplies", "2026-08-13", "SAR")
	if errs.CodeOf(err) != domain.CodeInvalidOrder {
		t.Fatalf("err = %v, want an invalid-order refusal", err)
	}
}

// ── what may still change ───────────────────────────────────────────────────────

func TestAPlacedOrderCannotBeEdited(t *testing.T) {
	// Placing is the irreversible act here, as posting is in sales: the document has been SENT
	// and a supplier is picking from it. Editing afterwards means the paper in their hand and
	// the record in ours describe different orders, and the delivery settles which was real.
	order := draftOrder(t)
	if err := order.RequireDraft(); err != nil {
		t.Fatalf("a draft refused editing: %v", err)
	}

	order.Status = domain.Placed
	if code := errs.CodeOf(order.RequireDraft()); code != domain.CodeAlreadyPlaced {
		t.Errorf("code = %q, want %q", code, domain.CodeAlreadyPlaced)
	}

	order.Status = domain.Cancelled
	if code := errs.CodeOf(order.RequireDraft()); code != domain.CodeCancelled {
		t.Errorf("code = %q, want %q", code, domain.CodeCancelled)
	}
}

func TestGoodsCannotArriveAgainstAnUnplacedOrder(t *testing.T) {
	// A draft has not been sent, so nothing can have been delivered against it. Goods that turn
	// up anyway are a receipt against no order — allowed as its own document — but they must not
	// silently attach here, or the order's own history becomes fiction.
	order := draftOrder(t)
	if errs.CodeOf(order.RequireReceivable()) != domain.CodeNotDraft {
		t.Errorf("a draft order accepted a delivery")
	}

	order.Status = domain.Placed
	if err := order.RequireReceivable(); err != nil {
		t.Errorf("a placed order refused a delivery: %v", err)
	}

	order.Status = domain.Closed
	if errs.CodeOf(order.RequireReceivable()) != domain.CodeClosed {
		t.Errorf("a closed order accepted a delivery")
	}
}

// ── the outstanding quantity ────────────────────────────────────────────────────

func TestOutstandingNeverGoesNegative(t *testing.T) {
	// An over-receipt is a real thing that 6.2 decides whether to permit. But "outstanding" is
	// what is still owed TO US, and a supplier who sent two extra does not owe us minus two —
	// a negative here would net off against another line and hide a genuine shortfall.
	l := line(t, 100_000_000)

	l.ReceivedMicro = 40_000_000
	if got := l.OutstandingMicro(); got != 60_000_000 {
		t.Errorf("outstanding = %d, want 60000000", got)
	}
	if l.IsFullyReceived() {
		t.Error("a partly received line reports itself complete")
	}

	l.ReceivedMicro = 102_000_000
	if got := l.OutstandingMicro(); got != 0 {
		t.Errorf("outstanding = %d after an over-receipt, want 0", got)
	}
	if !l.IsFullyReceived() {
		t.Error("an over-received line reports itself incomplete")
	}
}

// ── the arithmetic ──────────────────────────────────────────────────────────────

func TestALineRoundsOnceAtTheEndAndNotPerUnit(t *testing.T) {
	// §E. The unit price is 10⁻⁶ of the MAJOR unit because the price of a metre of cable is
	// routinely a fraction of a minor unit.
	//
	// 3,000 metres at 1.234567 per metre is 3,703.701 — 370,370.1 minor units, so 370,370.
	// Rounding the price to 1.23 first and multiplying gives 369,000: wrong by 1,370 minor units
	// on ONE line, which is the defect this scale exists to prevent.
	l := line(t, 3_000_000_000) // 3,000 metres
	priced, err := l.Price(1_234_567, 0, 2)
	if err != nil {
		t.Fatalf("Price: %v", err)
	}
	if priced.NetMinor != 370_370 {
		t.Fatalf("net = %d, want 370370", priced.NetMinor)
	}
}

func TestALargeOrderDoesNotOverflowTheIntermediate(t *testing.T) {
	// quantity(10⁶) × price(10⁶) is a 10¹²-scaled product. A big-enough order overflows int64 on
	// the INTERMEDIATE long before either operand is near its own limit — which is why this goes
	// through big.Int, and why the classic version of this bug works in testing and fails on a
	// genuinely large purchase.
	//
	// Ten million units at 900.00 each: 9,000,000,000 major units.
	// As an int64 intermediate that is 10^13 × 9×10^8 = 9×10^21, far past 9.2×10^18.
	l := line(t, 10_000_000_000_000) // ten million units
	priced, err := l.Price(900_000_000, 0, 2)
	if err != nil {
		t.Fatalf("Price: %v", err)
	}
	if priced.NetMinor != 900_000_000_000 {
		t.Fatalf("net = %d, want 900000000000", priced.NetMinor)
	}
}

func TestADiscountCannotExceedTheLineItDiscounts(t *testing.T) {
	// A line that discounts below zero is a supplier paying us to take goods, which is a credit
	// note and not a purchase.
	l := line(t, 2_000_000)
	if _, err := l.Price(1_000_000, 5_000, 2); err == nil {
		t.Fatal("a discount larger than the line was accepted")
	}
}

func TestANegativePriceIsRefused(t *testing.T) {
	l := line(t, 1_000_000)
	if code := errs.CodeOf(mustErr(l.Price(-1, 0, 2))); code != domain.CodeNegativePrice {
		t.Fatalf("code = %q, want %q", code, domain.CodeNegativePrice)
	}
}

func TestOrderingNothingIsRefused(t *testing.T) {
	// Zero has no reading that makes sense, and a NEGATIVE order is a return wearing an order's
	// clothes — returns are their own document.
	for _, quantity := range []int64{0, -1_000_000} {
		_, err := domain.NewLine(newID(t), newID(t), newID(t), newID(t), 1, quantity, quantity)
		if errs.CodeOf(err) != domain.CodeNonPositiveQty {
			t.Errorf("quantity %d was accepted", quantity)
		}
	}
}

// ── the totals ──────────────────────────────────────────────────────────────────

func TestTotalsAreSummedFromTheLines(t *testing.T) {
	// Summed, never accumulated as lines are added. An accumulated total drifts the moment a
	// line is edited or removed, and the drift is invisible — the document still balances
	// against itself, just not against what it contains.
	// Two units at 15.00, taxed at 15%: net 3000 minor, tax 450.
	first, err := line(t, 2_000_000).Price(15_000_000, 0, 2)
	if err != nil {
		t.Fatalf("Price: %v", err)
	}
	first = first.Tax(450, 150_000, "VAT")

	// One unit at 10.00 less a 1.00 discount, taxed at 15% of the net: net 900, tax 135.
	second, err := line(t, 1_000_000).Price(10_000_000, 100, 2)
	if err != nil {
		t.Fatalf("Price: %v", err)
	}
	second = second.Tax(135, 150_000, "VAT")

	net, tax, discount, total := domain.Totals([]domain.Line{first, second})
	if net != 3_900 || tax != 585 || discount != 100 || total != 4_485 {
		t.Fatalf("net=%d tax=%d discount=%d total=%d, want 3900/585/100/4485",
			net, tax, discount, total)
	}
	if total != net+tax {
		t.Errorf("the total does not tie to net plus tax")
	}
}

func mustErr(_ domain.Line, err error) error { return err }
