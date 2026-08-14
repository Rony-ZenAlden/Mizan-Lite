package purchasing_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/purchasing"
	purchasingdomain "github.com/mizan-erp/mizan/internal/modules/purchasing/domain"
)

func (f fixture) draftReturn(t *testing.T) id.ID {
	t.Helper()
	document, err := f.svc.DraftReturn(f.ctx, purchasing.NewReturnInput{
		CompanyID: f.companyID, BranchID: f.branchID, WarehouseID: f.warehouseID,
		PartnerID: f.partnerID, PartnerName: "Acme Supplies",
		ReturnDate: "2026-08-25", Reason: "damaged in transit", Currency: "SAR",
	})
	if err != nil {
		t.Fatalf("DraftReturn: %v", err)
	}
	return document.ID
}

func TestAReturnIsCostedAtTheOriginalDeliveryNotTodaysAverage(t *testing.T) {
	// Criterion 8, and §D.3's rule in the direction 0030 added `ReturnOut` for.
	//
	// Ten arrive at 10.00. Then ten more arrive at 20.00, so the average becomes 15.00. Sending
	// back two of the FIRST delivery must cost 10.00 each — not 15.00. Costing them at the
	// average would invent a 10.00 loss that never happened.
	f := newStockedFixture(t)
	books := f.booked(t)

	_, _, first := f.delivered(t, 10_000_000) // ten at the list price of 10.00

	// A second, dearer delivery moves the average. Against NO order, because a delivery that
	// has one is worth what the order said (D3) — an explicit cost belongs to deliveries with
	// no agreed price, which is exactly what a cash-and-carry top-up is.
	dearer := int64(20_000_000)
	receiptID := f.draftReceipt(t, id.ID(""))
	if _, err := f.svc.ReceiveLine(f.ctx, purchasing.ReceiveLineInput{
		CompanyID: f.companyID, ReceiptID: receiptID,
		VariantID: f.variant.ID, QuantityMicro: 10_000_000, UnitCostMicro: &dearer,
	}); err != nil {
		t.Fatalf("ReceiveLine: %v", err)
	}
	if _, err := f.svc.ConfirmReceipt(f.ctx, receiptID); err != nil {
		t.Fatalf("ConfirmReceipt: %v", err)
	}

	level, err := f.inventory.StockOf(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("StockOf: %v", err)
	}
	if level.AverageMicro != 15_000_000 {
		t.Fatalf("average = %d, want 15000000 — the fixture is not set up", level.AverageMicro)
	}

	// Two of the FIRST delivery go back.
	before := f.balances(t, books)
	returnID := f.draftReturn(t)
	if _, err = f.svc.AddReturnLine(f.ctx, purchasing.ReturnLineInput{
		CompanyID: f.companyID, ReturnID: returnID, ReceiptLineID: first[0].ID,
		QuantityMicro: 2_000_000,
	}); err != nil {
		t.Fatalf("AddReturnLine: %v", err)
	}
	posted, err := f.svc.PostReturn(f.ctx, returnID)
	if err != nil {
		t.Fatalf("PostReturn: %v", err)
	}
	after := f.balances(t, books)

	// Two at the ORIGINAL 10.00 is 20.00 of stock leaving — not 30.00 at the average.
	if posted.CostMinor != 2_000 {
		t.Fatalf("cost = %d, want 2000 (two at the original 10.00, not the 15.00 average)",
			posted.CostMinor)
	}
	if after["1300"]-before["1300"] != -2_000 {
		t.Errorf("inventory moved %d, want -2000", after["1300"]-before["1300"])
	}
	// And the supplier is debited what they charged for them.
	if after["2100"]-before["2100"] != 2_300 {
		t.Errorf("payable moved %d, want 2300 (2000 plus 15%% tax)",
			after["2100"]-before["2100"])
	}
	// The recoverable tax we claimed is given back.
	if after["1400"]-before["1400"] != -300 {
		t.Errorf("recoverable tax moved %d, want -300", after["1400"]-before["1400"])
	}
}

func TestAReturnLeavesTheRemainingAverageCorrect(t *testing.T) {
	// Taking goods out at a cost different from the average changes the average of everything
	// left. Leaving it alone would quietly park the difference in the valuation of stock that
	// never went anywhere.
	f := newStockedFixture(t)
	_, _, first := f.delivered(t, 10_000_000)

	returnID := f.draftReturn(t)
	if _, err := f.svc.AddReturnLine(f.ctx, purchasing.ReturnLineInput{
		CompanyID: f.companyID, ReturnID: returnID, ReceiptLineID: first[0].ID,
		QuantityMicro: 4_000_000,
	}); err != nil {
		t.Fatalf("AddReturnLine: %v", err)
	}
	if _, err := f.svc.PostReturn(f.ctx, returnID); err != nil {
		t.Fatalf("PostReturn: %v", err)
	}

	level, err := f.inventory.StockOf(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("StockOf: %v", err)
	}
	if level.OnHandMicro != 6_000_000 {
		t.Errorf("on hand = %d, want 6000000", level.OnHandMicro)
	}
	// Everything came in at 10.00 and left at 10.00, so what remains is still worth 10.00.
	if level.AverageMicro != 10_000_000 {
		t.Errorf("average = %d, want 10000000", level.AverageMicro)
	}
}

func TestMoreCannotGoBackThanArrived(t *testing.T) {
	// A debit note for goods the supplier never sent is the mirror of being invoiced for goods
	// that never arrived, and just as much a way for money to move for nothing.
	f := newStockedFixture(t)
	_, _, lines := f.delivered(t, 10_000_000)

	returnID := f.draftReturn(t)
	_, err := f.svc.AddReturnLine(f.ctx, purchasing.ReturnLineInput{
		CompanyID: f.companyID, ReturnID: returnID, ReceiptLineID: lines[0].ID,
		QuantityMicro: 12_000_000,
	})
	if code := errs.CodeOf(err); code != purchasingdomain.CodeTooMuchReturned {
		t.Fatalf("code = %q, want %q", code, purchasingdomain.CodeTooMuchReturned)
	}
}

func TestTheSameGoodsCannotGoBackTwice(t *testing.T) {
	// The subtler bound. Each return looks reasonable on its own, and only the running total
	// shows that fourteen of ten have gone back.
	f := newStockedFixture(t)
	_, _, lines := f.delivered(t, 10_000_000)

	first := f.draftReturn(t)
	if _, err := f.svc.AddReturnLine(f.ctx, purchasing.ReturnLineInput{
		CompanyID: f.companyID, ReturnID: first, ReceiptLineID: lines[0].ID,
		QuantityMicro: 7_000_000,
	}); err != nil {
		t.Fatalf("AddReturnLine: %v", err)
	}
	if _, err := f.svc.PostReturn(f.ctx, first); err != nil {
		t.Fatalf("PostReturn: %v", err)
	}

	// Three are left. Seven more is too many.
	second := f.draftReturn(t)
	_, err := f.svc.AddReturnLine(f.ctx, purchasing.ReturnLineInput{
		CompanyID: f.companyID, ReturnID: second, ReceiptLineID: lines[0].ID,
		QuantityMicro: 7_000_000,
	})
	if code := errs.CodeOf(err); code != purchasingdomain.CodeTooMuchReturned {
		t.Fatalf("code = %q, want %q", code, purchasingdomain.CodeTooMuchReturned)
	}

	// Three is fine.
	if _, err = f.svc.AddReturnLine(f.ctx, purchasing.ReturnLineInput{
		CompanyID: f.companyID, ReturnID: second, ReceiptLineID: lines[0].ID,
		QuantityMicro: 3_000_000,
	}); err != nil {
		t.Fatalf("returning the remaining three was refused: %v", err)
	}
}

func TestADraftReturnDoesNotConsumeWhatIsLeftToReturn(t *testing.T) {
	// Only POSTED returns count against the bound. A draft is somebody typing, and counting it
	// would refuse a second genuine return while the first is still being entered.
	f := newStockedFixture(t)
	_, _, lines := f.delivered(t, 10_000_000)

	pending := f.draftReturn(t)
	if _, err := f.svc.AddReturnLine(f.ctx, purchasing.ReturnLineInput{
		CompanyID: f.companyID, ReturnID: pending, ReceiptLineID: lines[0].ID,
		QuantityMicro: 6_000_000,
	}); err != nil {
		t.Fatalf("AddReturnLine: %v", err)
	}

	other := f.draftReturn(t)
	if _, err := f.svc.AddReturnLine(f.ctx, purchasing.ReturnLineInput{
		CompanyID: f.companyID, ReturnID: other, ReceiptLineID: lines[0].ID,
		QuantityMicro: 8_000_000,
	}); err != nil {
		t.Fatalf("a draft return blocked another: %v", err)
	}
}

func TestGoodsFromAnotherSupplierCannotBeReturnedHere(t *testing.T) {
	f := newStockedFixture(t)
	_, _, lines := f.delivered(t, 10_000_000)

	other, _ := id.New()
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx, `
		INSERT INTO partners (
			id, company_id, code, name, is_customer, is_supplier, partner_type,
			payment_terms_days, credit_limit_minor, is_active, has_history,
			row_version, created_at, updated_at
		) VALUES (?, ?, 'OTHER2', 'Other Supplies', 0, 1, 'company', 30, 0, 1, 0, 1, ?, ?)`,
		string(other), string(f.companyID), "2026-08-01", "2026-08-01"); err != nil {
		t.Fatalf("creating a second supplier: %v", err)
	}

	document, err := f.svc.DraftReturn(f.ctx, purchasing.NewReturnInput{
		CompanyID: f.companyID, BranchID: f.branchID, WarehouseID: f.warehouseID,
		PartnerID: other, PartnerName: "Other Supplies",
		ReturnDate: "2026-08-25", Currency: "SAR",
	})
	if err != nil {
		t.Fatalf("DraftReturn: %v", err)
	}

	if _, err = f.svc.AddReturnLine(f.ctx, purchasing.ReturnLineInput{
		CompanyID: f.companyID, ReturnID: document.ID, ReceiptLineID: lines[0].ID,
		QuantityMicro: 1_000_000,
	}); err == nil {
		t.Fatal("goods went back to a supplier that never sent them")
	}
}

func TestAPostedReturnCannotBeChanged(t *testing.T) {
	f := newStockedFixture(t)
	_, _, lines := f.delivered(t, 10_000_000)

	returnID := f.draftReturn(t)
	if _, err := f.svc.AddReturnLine(f.ctx, purchasing.ReturnLineInput{
		CompanyID: f.companyID, ReturnID: returnID, ReceiptLineID: lines[0].ID,
		QuantityMicro: 2_000_000,
	}); err != nil {
		t.Fatalf("AddReturnLine: %v", err)
	}
	if _, err := f.svc.PostReturn(f.ctx, returnID); err != nil {
		t.Fatalf("PostReturn: %v", err)
	}

	_, err := f.svc.AddReturnLine(f.ctx, purchasing.ReturnLineInput{
		CompanyID: f.companyID, ReturnID: returnID, ReceiptLineID: lines[0].ID,
		QuantityMicro: 1_000_000,
	})
	if code := errs.CodeOf(err); code != purchasingdomain.CodeReturnPosted {
		t.Fatalf("code = %q, want %q", code, purchasingdomain.CodeReturnPosted)
	}
}
