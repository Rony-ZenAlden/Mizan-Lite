package purchasing_test

import (
	"errors"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/modules/audit"
	"github.com/mizan-erp/mizan/internal/modules/catalog/domain"
	"github.com/mizan-erp/mizan/internal/modules/purchasing"
	purchasingdomain "github.com/mizan-erp/mizan/internal/modules/purchasing/domain"
)

// ── an order is an intention, not a transaction ─────────────────────────────────

func TestPlacingAnOrderMovesNoStockAndWritesNoJournalEntry(t *testing.T) {
	// Criterion 1. A system that books an intention has committed a business to a purchase it
	// can still walk away from — and the supplier has not even acknowledged it.
	f := newFixture(t)

	orderID := f.draft(t)
	f.addLine(t, orderID, f.variant.ID, 10_000_000)

	placed, err := f.svc.Place(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if placed.Number == "" {
		t.Fatal("a placed order took no number")
	}

	// Nothing on the shelf.
	var movements int
	if err = f.store.Reader(f.ctx).QueryRowContext(f.ctx,
		`SELECT COUNT(*) FROM stock_movements`).Scan(&movements); err != nil {
		t.Fatalf("counting movements: %v", err)
	}
	if movements != 0 {
		t.Errorf("placing an order moved stock %d times", movements)
	}

	// Nothing in the books.
	var entries int
	if err = f.store.Reader(f.ctx).QueryRowContext(f.ctx,
		`SELECT COUNT(*) FROM journal_entries`).Scan(&entries); err != nil {
		t.Fatalf("counting journal entries: %v", err)
	}
	if entries != 0 {
		t.Errorf("placing an order wrote %d journal entries", entries)
	}
}

func TestADraftOrderHasNoNumberAndAnAbandonedOneConsumesNone(t *testing.T) {
	// §9.4, the same rule sales keeps. A cancelled order that had taken a number leaves a hole
	// somebody has to explain to an auditor.
	f := newFixture(t)

	first := f.draft(t)
	f.addLine(t, first, f.variant.ID, 1_000_000)
	order, _, err := f.svc.Order(f.ctx, first)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	if order.Number != "" {
		t.Fatalf("a draft carries number %q", order.Number)
	}

	if err = f.svc.Cancel(f.ctx, first); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	second := f.draft(t)
	f.addLine(t, second, f.variant.ID, 1_000_000)
	placed, err := f.svc.Place(f.ctx, second)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if placed.Number != "PO-000001" {
		t.Errorf("number = %q, want PO-000001 — the cancelled draft consumed one", placed.Number)
	}
}

func TestAPlacedOrderRefusesMoreLines(t *testing.T) {
	// The document has been SENT and a supplier is picking from it. Editing afterwards means the
	// paper in their hand and the record in ours describe different orders.
	f := newFixture(t)
	orderID := f.draft(t)
	f.addLine(t, orderID, f.variant.ID, 5_000_000)
	if _, err := f.svc.Place(f.ctx, orderID); err != nil {
		t.Fatalf("Place: %v", err)
	}

	_, err := f.svc.AddLine(f.ctx, purchasing.AddLineInput{
		CompanyID: f.companyID, OrderID: orderID, VariantID: f.variant.ID,
		QuantityMicro: 1_000_000,
	})
	if code := errs.CodeOf(err); code != purchasingdomain.CodeAlreadyPlaced {
		t.Fatalf("code = %q, want %q", code, purchasingdomain.CodeAlreadyPlaced)
	}
}

func TestAnOrderWithNoLinesCannotBePlaced(t *testing.T) {
	f := newFixture(t)
	orderID := f.draft(t)

	_, err := f.svc.Place(f.ctx, orderID)
	if code := errs.CodeOf(err); code != purchasingdomain.CodeNoLines {
		t.Fatalf("code = %q, want %q", code, purchasingdomain.CodeNoLines)
	}
}

// ── the seams Phase 3 left ──────────────────────────────────────────────────────

func TestAnOrderDefaultsToTheProductsPURCHASEUnitNotItsSalesUnit(t *testing.T) {
	// `products.purchase_uom_id` has existed since 3.2 and had no reader until now.
	//
	// Flour is stocked and SOLD by the gram and BOUGHT by the kilogram. An order for 2 that
	// defaulted to the sales unit would be 2 grams where the buyer meant 2 kilograms — wrong by
	// a factor of a thousand, in the direction nobody notices until the lorry arrives.
	f := newFixture(t)
	orderID := f.draft(t)
	f.addLine(t, orderID, f.sackVariant.ID, 2_000_000) // two of something

	_, lines, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	if lines[0].UomCode != "KG" {
		t.Fatalf("the order line is in %q, want KG — the purchase unit was ignored",
			lines[0].UomCode)
	}
	// And the stock quantity is the converted one: 2 kg is 2,000 grams.
	if lines[0].QuantityStockMicro != 2_000_000_000 {
		t.Errorf("stock quantity = %d, want 2000000000 (2 kg in grams)",
			lines[0].QuantityStockMicro)
	}
	// Both are stored. The day one is derived from the other is the day "2 sacks" and
	// "2,000 grams" stop agreeing on a document already sent to a supplier.
	if lines[0].QuantityMicro != 2_000_000 {
		t.Errorf("ordered quantity = %d, want 2000000", lines[0].QuantityMicro)
	}
}

func TestSomethingTheBusinessDoesNotBuyCannotBeOrdered(t *testing.T) {
	// `products.is_purchased` has also existed since 3.2 with no reader. A shop that marks its
	// own manufactured goods unpurchasable means it, and an order for one is a mistake worth
	// catching before it reaches a supplier.
	f := newFixture(t)

	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
		`UPDATE products SET is_purchased = 0 WHERE code = 'WIDGET'`); err != nil {
		t.Fatalf("marking the product unpurchasable: %v", err)
	}

	orderID := f.draft(t)
	_, err := f.svc.AddLine(f.ctx, purchasing.AddLineInput{
		CompanyID: f.companyID, OrderID: orderID, VariantID: f.variant.ID,
		QuantityMicro: 1_000_000,
	})
	if code := errs.CodeOf(err); code != domain.CodeNotPurchased {
		t.Fatalf("code = %q, want %q", code, domain.CodeNotPurchased)
	}
}

func TestOrderingHalfSomethingIndivisibleIsRefused(t *testing.T) {
	// 3.1's rule, reached from the buying side for the first time. You cannot order half a
	// widget any more than you can sell half a chair.
	f := newFixture(t)
	orderID := f.draft(t)

	_, err := f.svc.AddLine(f.ctx, purchasing.AddLineInput{
		CompanyID: f.companyID, OrderID: orderID, VariantID: f.variant.ID,
		QuantityMicro: 1_500_000, // one and a half pieces
	})
	if err == nil {
		t.Fatal("half a widget was ordered")
	}
}

// ── the line snapshot ───────────────────────────────────────────────────────────

func TestALineSnapshotsWhatThingsWereCalled(t *testing.T) {
	// §9.3, on the buying side. A purchase order reprinted after a rename must say what was
	// ordered, not what that product happens to be called now — the supplier is holding a copy.
	f := newFixture(t)
	orderID := f.draft(t)
	f.addLine(t, orderID, f.variant.ID, 3_000_000)

	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
		`UPDATE products SET name = 'Premium Widget (2028)' WHERE code = 'WIDGET'`); err != nil {
		t.Fatalf("renaming: %v", err)
	}

	_, lines, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	if lines[0].ProductName != "Widget" {
		t.Fatalf("the line says %q, want the name as at the order", lines[0].ProductName)
	}
}

// ── pricing at placement ────────────────────────────────────────────────────────

func TestPlacingResolvesThePriceAndTheTax(t *testing.T) {
	// The buyer says what and how many; the price lists answer the rest. A buyer who can type
	// any price is a purchase nobody agreed, and the three-way match has nothing to compare the
	// bill against.
	f := newFixture(t)
	orderID := f.draft(t)
	f.addLine(t, orderID, f.variant.ID, 4_000_000) // four widgets

	placed, err := f.svc.Place(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}

	// Four at 10.00 is 40.00 net; 15% tax is 6.00; total 46.00.
	if placed.NetMinor != 4_000 || placed.TaxMinor != 600 || placed.TotalMinor != 4_600 {
		t.Fatalf("net=%d tax=%d total=%d, want 4000/600/4600",
			placed.NetMinor, placed.TaxMinor, placed.TotalMinor)
	}

	_, lines, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	if lines[0].UnitPriceMicro != 10_000_000 {
		t.Errorf("unit price = %d, want 10000000", lines[0].UnitPriceMicro)
	}
}

func TestAnOrderCarriesNoPriceUntilItIsPlaced(t *testing.T) {
	// The price is resolved AT PLACEMENT, so an order drafted in January and placed in March is
	// placed at March's prices — which is what the supplier will invoice.
	f := newFixture(t)
	orderID := f.draft(t)
	f.addLine(t, orderID, f.variant.ID, 2_000_000)

	_, lines, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	if lines[0].UnitPriceMicro != 0 || lines[0].TotalMinor != 0 {
		t.Fatalf("a draft line is already priced: %+v", lines[0])
	}
}

func TestAFailedPriceLookupLeavesTheOrderUnplacedAndUnnumbered(t *testing.T) {
	// The number is allocated LAST, so a failure anywhere earlier consumes none.
	f := newFixture(t)
	orderID := f.draft(t)
	f.addLine(t, orderID, f.variant.ID, 1_000_000)

	broken := purchasing.NewService(f.store, purchasing.Options{
		Bus: f.bus, Catalog: realCatalog{svc: f.catalog},
		Pricing: fixedPricing{err: errors.New("the price list is unreachable")},
		Tax:     fixedTax{},
		Numbers: platformNumbering{alloc: allocatorFor(f)},
	})
	if _, err := broken.Place(f.ctx, orderID); err == nil {
		t.Fatal("an order was placed with no price")
	}

	order, _, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	if order.Status != purchasingdomain.Draft {
		t.Errorf("status = %q, want draft — the failure was not rolled back", order.Status)
	}
	if order.Number != "" {
		t.Errorf("a failed placement consumed number %q", order.Number)
	}
}

// ── closing ─────────────────────────────────────────────────────────────────────

func TestClosingIsNotCancelling(t *testing.T) {
	// A cancelled order never happened; a closed one happened and is finished with. A supplier
	// who short-delivers and will not send the rest leaves an order closed with an outstanding
	// quantity — a fact worth keeping, not an error to erase.
	f := newFixture(t)
	orderID := f.draft(t)
	f.addLine(t, orderID, f.variant.ID, 10_000_000)
	if _, err := f.svc.Place(f.ctx, orderID); err != nil {
		t.Fatalf("Place: %v", err)
	}

	if err := f.svc.Close(f.ctx, orderID); err != nil {
		t.Fatalf("Close: %v", err)
	}

	order, lines, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	if order.Status != purchasingdomain.Closed {
		t.Fatalf("status = %q, want closed", order.Status)
	}
	// The number survives, and so does the record of what never arrived.
	if order.Number == "" {
		t.Error("closing an order erased its number")
	}
	if lines[0].OutstandingMicro() != 10_000_000 {
		t.Errorf("outstanding = %d, want 10000000 — closing erased the shortfall",
			lines[0].OutstandingMicro())
	}

	// And a draft cannot be closed: there is nothing to finish.
	other := f.draft(t)
	if err = f.svc.Close(f.ctx, other); err == nil {
		t.Error("a draft order was closed")
	}
}

// ── the audit trail ─────────────────────────────────────────────────────────────

func TestEveryActOnAnOrderLeavesAnAuditEntry(t *testing.T) {
	// The Phase 5 DoD review found that nothing REQUIRED this, so the next act added would not
	// have been audited. Purchasing gets the check from its first step rather than its last.
	f := newFixture(t)

	orderID := f.draft(t)
	f.addLine(t, orderID, f.variant.ID, 2_000_000)
	f.addLine(t, orderID, f.variant.ID, 1_000_000)

	_, lines, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	if err = f.svc.RemoveLine(f.ctx, orderID, lines[1].ID); err != nil {
		t.Fatalf("RemoveLine: %v", err)
	}
	if _, err = f.svc.Place(f.ctx, orderID); err != nil {
		t.Fatalf("Place: %v", err)
	}
	if err = f.svc.Close(f.ctx, orderID); err != nil {
		t.Fatalf("Close: %v", err)
	}

	cancelled := f.draft(t)
	if err = f.svc.Cancel(f.ctx, cancelled); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	entries, err := f.audit.Entries(f.ctx, audit.Filter{})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	seen := make(map[string]int, len(entries))
	for _, entry := range entries {
		seen[entry.Action]++
	}

	for _, action := range []string{
		purchasing.ActionOrderDrafted, purchasing.ActionOrderLineAdded,
		purchasing.ActionOrderLineRemoved, purchasing.ActionOrderPlaced,
		purchasing.ActionOrderClosed, purchasing.ActionOrderCancelled,
	} {
		if seen[action] == 0 {
			t.Errorf("%q happened and left no audit entry", action)
		}
	}
}
