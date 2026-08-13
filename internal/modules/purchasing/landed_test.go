package purchasing_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/modules/purchasing"
	purchasingdomain "github.com/mizan-erp/mizan/internal/modules/purchasing/domain"
)

// deliveredTwoLines receives two different products in one delivery, so a charge has something
// to spread ACROSS rather than merely onto.
func (f fixture) deliveredTwoLines(t *testing.T) []purchasingdomain.ReceiptLine {
	t.Helper()

	orderID := f.draft(t)
	f.addLine(t, orderID, f.variant.ID, 10_000_000)    // ten widgets at 10.00 = 100.00
	f.addLine(t, orderID, f.sackVariant.ID, 2_000_000) // two kilos of flour at 10.00 = 20.00
	if _, err := f.svc.Place(f.ctx, orderID); err != nil {
		t.Fatalf("Place: %v", err)
	}
	_, orderLines, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}

	receiptID := f.draftReceipt(t, orderID)
	for _, line := range orderLines {
		f.receiveLine(t, receiptID, line.ID, line.QuantityMicro)
	}
	if _, err = f.svc.ConfirmReceipt(f.ctx, receiptID); err != nil {
		t.Fatalf("ConfirmReceipt: %v", err)
	}
	_, receiptLines, err := f.svc.Receipt(f.ctx, receiptID)
	if err != nil {
		t.Fatalf("Receipt: %v", err)
	}
	return receiptLines
}

func TestFreightReachesTheCostOfTheGoodsItBroughtIn(t *testing.T) {
	// Criterion 7. A business that books freight to an expense account understates what its
	// stock cost and overstates its margin on every sale of it — a gross margin computed without
	// the lorry looks healthy and is not.
	f := newStockedFixture(t)
	books := f.booked(t)

	lines := f.deliveredTwoLines(t)
	before := f.balances(t, books)

	charge, err := f.svc.AddLandedCost(f.ctx, purchasing.NewLandedCostInput{
		CompanyID: f.companyID, ReceiptID: f.receiptOf(t, lines[0].ID),
		ChargeType: "freight", Basis: purchasingdomain.ByValue,
		Currency: "SAR", AmountMinor: 1_200, // 12.00 of freight
	})
	if err != nil {
		t.Fatalf("AddLandedCost: %v", err)
	}

	if _, err = f.svc.ApplyLandedCost(f.ctx, charge.ID); err != nil {
		t.Fatalf("ApplyLandedCost: %v", err)
	}
	after := f.balances(t, books)

	// All the goods are still on hand, so the whole charge reaches inventory.
	if after["1300"]-before["1300"] != 1_200 {
		t.Errorf("inventory rose by %d, want 1200", after["1300"]-before["1300"])
	}
	// And somebody is owed for it.
	if after["2100"]-before["2100"] != -1_200 {
		t.Errorf("payable moved %d, want -1200", after["2100"]-before["2100"])
	}
}

func TestAChargeSplitsByValueAndTiesExactly(t *testing.T) {
	// §D.3's trap 4: naive proportional allocation leaves the total off by a few minor units on
	// every shipment, and those units land in nobody's account.
	//
	// 100.00 of widgets and 20.00 of flour is 120.00; a 10.00 charge splits 8.33 / 1.67, which
	// does not divide evenly and is exactly where the arithmetic goes wrong.
	f := newStockedFixture(t)
	lines := f.deliveredTwoLines(t)

	charge, err := f.svc.AddLandedCost(f.ctx, purchasing.NewLandedCostInput{
		CompanyID: f.companyID, ReceiptID: f.receiptOf(t, lines[0].ID),
		ChargeType: "customs", Basis: purchasingdomain.ByValue,
		Currency: "SAR", AmountMinor: 1_000,
	})
	if err != nil {
		t.Fatalf("AddLandedCost: %v", err)
	}
	if _, err = f.svc.ApplyLandedCost(f.ctx, charge.ID); err != nil {
		t.Fatalf("ApplyLandedCost: %v", err)
	}

	allocations, err := f.svc.AllocationsOf(f.ctx, charge.ID)
	if err != nil {
		t.Fatalf("AllocationsOf: %v", err)
	}
	if len(allocations) != 2 {
		t.Fatalf("%d allocations, want 2", len(allocations))
	}

	var total int64
	for _, allocation := range allocations {
		total += allocation.AmountMinor
	}
	if total != 1_000 {
		t.Fatalf("the allocations sum to %d, want the whole charge of 1000", total)
	}
	// By VALUE: 100.00 of widgets takes five-sixths, 20.00 of flour takes one-sixth.
	if allocations[0].AmountMinor != 833 && allocations[0].AmountMinor != 834 {
		t.Errorf("the larger line took %d, want about 833", allocations[0].AmountMinor)
	}
}

func TestAChargeCanSplitByQuantityInstead(t *testing.T) {
	// Freight follows volume or weight — a lorry is full when it is full, whatever is in it.
	// Customs follows value, because that is what duty is charged on. Forcing one basis makes
	// the other wrong, and a shipment usually carries both kinds of charge at once.
	f := newStockedFixture(t)
	lines := f.deliveredTwoLines(t)

	charge, err := f.svc.AddLandedCost(f.ctx, purchasing.NewLandedCostInput{
		CompanyID: f.companyID, ReceiptID: f.receiptOf(t, lines[0].ID),
		ChargeType: "freight", Basis: purchasingdomain.ByQuantity,
		Currency: "SAR", AmountMinor: 1_000,
	})
	if err != nil {
		t.Fatalf("AddLandedCost: %v", err)
	}
	if _, err = f.svc.ApplyLandedCost(f.ctx, charge.ID); err != nil {
		t.Fatalf("ApplyLandedCost: %v", err)
	}

	allocations, err := f.svc.AllocationsOf(f.ctx, charge.ID)
	if err != nil {
		t.Fatalf("AllocationsOf: %v", err)
	}
	var total int64
	for _, allocation := range allocations {
		total += allocation.AmountMinor
	}
	if total != 1_000 {
		t.Fatalf("the allocations sum to %d, want 1000", total)
	}

	// By QUANTITY the split is quite different: ten widgets against two THOUSAND grams of flour,
	// so the flour absorbs almost all of it. That the two bases disagree this sharply is the
	// point of letting a charge declare its own.
	byQuantity := allocations[1].AmountMinor
	if byQuantity < 900 {
		t.Errorf("the heavier line took %d of 1000 by quantity, which is not a quantity split",
			byQuantity)
	}
}

func TestAnUnsupportedBasisIsRefusedRatherThanSubstituted(t *testing.T) {
	// Weight and volume need product dimensions the catalog does not carry. A freight charge
	// spread by VALUE when the operator asked for WEIGHT is wrong in a way nobody would notice —
	// so it is refused, and the enum names the option so adding it later is a value change.
	f := newStockedFixture(t)
	lines := f.deliveredTwoLines(t)

	_, err := f.svc.AddLandedCost(f.ctx, purchasing.NewLandedCostInput{
		CompanyID: f.companyID, ReceiptID: f.receiptOf(t, lines[0].ID),
		ChargeType: "freight", Basis: purchasingdomain.ByWeight,
		Currency: "SAR", AmountMinor: 1_000,
	})
	if code := errs.CodeOf(err); code != purchasingdomain.CodeUnsupportedBasis {
		t.Fatalf("code = %q, want %q", code, purchasingdomain.CodeUnsupportedBasis)
	}
}

func TestAChargeCannotBeAppliedTwice(t *testing.T) {
	// Applying twice doubles the cost of the goods, and nothing downstream looks wrong — just a
	// margin quietly worse than it should be, discovered months later if at all.
	f := newStockedFixture(t)
	lines := f.deliveredTwoLines(t)

	charge, err := f.svc.AddLandedCost(f.ctx, purchasing.NewLandedCostInput{
		CompanyID: f.companyID, ReceiptID: f.receiptOf(t, lines[0].ID),
		ChargeType: "freight", Currency: "SAR", AmountMinor: 1_000,
	})
	if err != nil {
		t.Fatalf("AddLandedCost: %v", err)
	}
	if _, err = f.svc.ApplyLandedCost(f.ctx, charge.ID); err != nil {
		t.Fatalf("ApplyLandedCost: %v", err)
	}

	_, err = f.svc.ApplyLandedCost(f.ctx, charge.ID)
	if code := errs.CodeOf(err); code != purchasingdomain.CodeLandedApplied {
		t.Fatalf("code = %q, want %q", code, purchasingdomain.CodeLandedApplied)
	}
}

func TestAChargeNeedsAConfirmedDelivery(t *testing.T) {
	// A charge against a draft delivery would spread across lines that may still change, and the
	// allocation stored would describe a receipt that never existed.
	f := newStockedFixture(t)

	orderID := f.draft(t)
	f.addLine(t, orderID, f.variant.ID, 5_000_000)
	if _, err := f.svc.Place(f.ctx, orderID); err != nil {
		t.Fatalf("Place: %v", err)
	}
	_, orderLines, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	receiptID := f.draftReceipt(t, orderID)
	f.receiveLine(t, receiptID, orderLines[0].ID, 5_000_000)

	_, err = f.svc.AddLandedCost(f.ctx, purchasing.NewLandedCostInput{
		CompanyID: f.companyID, ReceiptID: receiptID,
		ChargeType: "freight", Currency: "SAR", AmountMinor: 500,
	})
	if code := errs.CodeOf(err); code != purchasingdomain.CodeNotConfirmed {
		t.Fatalf("code = %q, want %q", code, purchasingdomain.CodeNotConfirmed)
	}
}
