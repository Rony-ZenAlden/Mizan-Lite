package purchasing_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/purchasing"
	purchasingdomain "github.com/mizan-erp/mizan/internal/modules/purchasing/domain"
)

// delivered runs the whole chain — order, place, receive, confirm — and returns the receipt's
// lines, which is what a bill takes up.
func (f fixture) delivered(
	t *testing.T, quantityMicro int64,
) (orderID, receiptID id.ID, receiptLines []purchasingdomain.ReceiptLine) {
	t.Helper()

	orderID = f.draft(t)
	f.addLine(t, orderID, f.variant.ID, quantityMicro)
	if _, err := f.svc.Place(f.ctx, orderID); err != nil {
		t.Fatalf("Place: %v", err)
	}
	_, orderLines, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}

	receiptID = f.draftReceipt(t, orderID)
	f.receiveLine(t, receiptID, orderLines[0].ID, quantityMicro)
	if _, err = f.svc.ConfirmReceipt(f.ctx, receiptID); err != nil {
		t.Fatalf("ConfirmReceipt: %v", err)
	}

	_, receiptLines, err = f.svc.Receipt(f.ctx, receiptID)
	if err != nil {
		t.Fatalf("Receipt: %v", err)
	}
	return orderID, receiptID, receiptLines
}

func (f fixture) draftBill(t *testing.T, invoiceNumber string) id.ID {
	t.Helper()
	bill, err := f.svc.DraftBill(f.ctx, purchasing.NewBillInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		PartnerID: f.partnerID, PartnerName: "Acme Supplies",
		BillDate: "2026-08-20", SupplierInvoiceNumber: invoiceNumber, Currency: "SAR",
	})
	if err != nil {
		t.Fatalf("DraftBill: %v", err)
	}
	return bill.ID
}

// ── the bill clears GRNI exactly ────────────────────────────────────────────────

func TestABillClearsGRNIExactlyWhenThePriceAgrees(t *testing.T) {
	// Criterion 4. The receipt accrued; the bill clears. When the paperwork agrees the two net
	// to nothing and GRNI returns to zero — which is what makes a GRNI balance worth reading.
	f := newStockedFixture(t)
	books := f.booked(t)

	_, _, receiptLines := f.delivered(t, 10_000_000) // ten at 10.00 = 100.00

	billID := f.draftBill(t, "ACME-5501")
	if _, err := f.svc.AddBillLine(f.ctx, purchasing.BillLineInput{
		CompanyID: f.companyID, BillID: billID, ReceiptLineID: receiptLines[0].ID,
	}); err != nil {
		t.Fatalf("AddBillLine: %v", err)
	}

	posted, err := f.svc.PostBill(f.ctx, billID)
	if err != nil {
		t.Fatalf("PostBill: %v", err)
	}
	if posted.Number == "" {
		t.Fatal("a posted bill took no number")
	}
	if posted.VarianceMinor != 0 {
		t.Fatalf("variance = %d, want 0", posted.VarianceMinor)
	}

	balances := f.balances(t, books)
	// GRNI: credited 10,000 by the receipt, debited 10,000 by the bill. Exactly zero.
	if balances["2150"] != 0 {
		t.Errorf("GRNI = %d, want 0 — the accrual was not cleared exactly", balances["2150"])
	}
	// The stock stays where the receipt put it.
	if balances["1300"] != 10_000 {
		t.Errorf("inventory = %d, want 10000", balances["1300"])
	}
	// Now we owe the supplier, and the tax is recoverable. 15% of 10,000 is 1,500.
	if balances["2100"] != -11_500 {
		t.Errorf("accounts payable = %d, want -11500", balances["2100"])
	}
	if balances["1400"] != 1_500 {
		t.Errorf("recoverable tax = %d, want 1500", balances["1400"])
	}
}

func TestAPriceVarianceIsBookedAndGRNIStillClearsExactly(t *testing.T) {
	// The supplier charges 12.00 where 10.00 was ordered. The goods were accrued at 100.00 and
	// the invoice is for 120.00, so GRNI must still clear at 100.00 and the extra 20.00 must go
	// somewhere — otherwise the difference sits in GRNI forever.
	f := newStockedFixture(t)
	books := f.booked(t)

	_, _, receiptLines := f.delivered(t, 10_000_000)

	billID := f.draftBill(t, "ACME-5502")
	if _, err := f.svc.AddBillLine(f.ctx, purchasing.BillLineInput{
		CompanyID: f.companyID, BillID: billID, ReceiptLineID: receiptLines[0].ID,
		UnitPriceMicro: 12_000_000,
	}); err != nil {
		t.Fatalf("AddBillLine: %v", err)
	}

	posted, err := f.svc.PostBill(f.ctx, billID)
	if err != nil {
		t.Fatalf("PostBill: %v", err)
	}
	if posted.NetMinor != 12_000 || posted.AccruedMinor != 10_000 {
		t.Fatalf("net=%d accrued=%d, want 12000/10000", posted.NetMinor, posted.AccruedMinor)
	}
	if posted.VarianceMinor != 2_000 {
		t.Fatalf("variance = %d, want 2000", posted.VarianceMinor)
	}

	balances := f.balances(t, books)
	if balances["2150"] != 0 {
		t.Errorf("GRNI = %d, want 0 — it must clear at the ACCRUED figure", balances["2150"])
	}
	if balances["5600"] != 2_000 {
		t.Errorf("variance account = %d, want 2000", balances["5600"])
	}
	// Payable is the whole invoice: 12,000 plus 15% tax.
	if balances["2100"] != -13_800 {
		t.Errorf("accounts payable = %d, want -13800", balances["2100"])
	}
}

func TestASupplierChargingLessMovesTheBooksTheOtherWay(t *testing.T) {
	// A supplier honouring a discount nobody recorded is just as real a variance as a surcharge,
	// and the posting engine refuses negative amounts — so the two directions are two rule lines
	// with two amounts, exactly one of which is ever non-zero.
	f := newStockedFixture(t)
	books := f.booked(t)

	_, _, receiptLines := f.delivered(t, 10_000_000)

	billID := f.draftBill(t, "ACME-5503")
	if _, err := f.svc.AddBillLine(f.ctx, purchasing.BillLineInput{
		CompanyID: f.companyID, BillID: billID, ReceiptLineID: receiptLines[0].ID,
		UnitPriceMicro: 8_000_000, // 8.00 where 10.00 was ordered
	}); err != nil {
		t.Fatalf("AddBillLine: %v", err)
	}
	posted, err := f.svc.PostBill(f.ctx, billID)
	if err != nil {
		t.Fatalf("PostBill: %v", err)
	}
	if posted.VarianceMinor != -2_000 {
		t.Fatalf("variance = %d, want -2000", posted.VarianceMinor)
	}

	balances := f.balances(t, books)
	if balances["2150"] != 0 {
		t.Errorf("GRNI = %d, want 0", balances["2150"])
	}
	// The variance account is CREDITED this time: a gain, not a cost.
	if balances["5600"] != -2_000 {
		t.Errorf("variance account = %d, want -2000 (a credit)", balances["5600"])
	}
}

// ── the three-way match ─────────────────────────────────────────────────────────

// TestABillCannotChargeForGoodsThatNeverArrived
//
// The whole reason purchasing exists as a discipline separate from paying bills — and here it is
// guaranteed by CONSTRUCTION rather than by a check.
//
// A bill line must name a receipt line and takes its quantity in full. There is no field through
// which a different quantity could be expressed, so the state is unreachable rather than
// validated against. This test asserts the structural property, which is what actually holds.
func TestABillCannotChargeForGoodsThatNeverArrived(t *testing.T) {
	f := newStockedFixture(t)
	_, _, receiptLines := f.delivered(t, 7_000_000)

	billID := f.draftBill(t, "ACME-5504")
	line, err := f.svc.AddBillLine(f.ctx, purchasing.BillLineInput{
		CompanyID: f.companyID, BillID: billID, ReceiptLineID: receiptLines[0].ID,
	})
	if err != nil {
		t.Fatalf("AddBillLine: %v", err)
	}

	// The billed quantity IS the delivered quantity. Not checked to be equal — taken from it.
	if line.QuantityMicro != receiptLines[0].QuantityMicro {
		t.Fatalf("billed %d against a delivery of %d",
			line.QuantityMicro, receiptLines[0].QuantityMicro)
	}

	// And every stored line agrees, so nothing between the input and the row can change it.
	_, stored, err := f.svc.Bill(f.ctx, billID)
	if err != nil {
		t.Fatalf("Bill: %v", err)
	}
	if stored[0].QuantityMicro != 7_000_000 {
		t.Errorf("stored quantity = %d, want 7000000", stored[0].QuantityMicro)
	}
	// A bill line with no delivery behind it cannot exist: the column is NOT NULL.
	if stored[0].ReceiptLineID != receiptLines[0].ID {
		t.Error("a bill line lost its link to the delivery it pays for")
	}
}

func TestADeliveryCannotBeBilledTwice(t *testing.T) {
	// Criterion 6, and the failure mode that costs real money: paying a supplier twice for one
	// delivery. Kept by the SERVICE (a readable refusal) and by a unique index (which holds when
	// two clerks post at the same moment).
	f := newStockedFixture(t)
	_, _, receiptLines := f.delivered(t, 10_000_000)

	first := f.draftBill(t, "ACME-5505")
	if _, err := f.svc.AddBillLine(f.ctx, purchasing.BillLineInput{
		CompanyID: f.companyID, BillID: first, ReceiptLineID: receiptLines[0].ID,
	}); err != nil {
		t.Fatalf("AddBillLine: %v", err)
	}
	if _, err := f.svc.PostBill(f.ctx, first); err != nil {
		t.Fatalf("PostBill: %v", err)
	}

	second := f.draftBill(t, "ACME-5506")
	_, err := f.svc.AddBillLine(f.ctx, purchasing.BillLineInput{
		CompanyID: f.companyID, BillID: second, ReceiptLineID: receiptLines[0].ID,
	})
	if code := errs.CodeOf(err); code != purchasingdomain.CodeAlreadyBilled {
		t.Fatalf("code = %q, want %q", code, purchasingdomain.CodeAlreadyBilled)
	}
}

func TestTheSameSupplierInvoiceNumberCannotBeEnteredTwice(t *testing.T) {
	// The one duplicate that costs money, and a busy accounts clerk entering a batch is exactly
	// who will make it. Enforced by the schema as well as the service.
	f := newStockedFixture(t)

	f.draftBill(t, "ACME-7001")
	_, err := f.svc.DraftBill(f.ctx, purchasing.NewBillInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		PartnerID: f.partnerID, PartnerName: "Acme Supplies",
		BillDate: "2026-08-21", SupplierInvoiceNumber: "ACME-7001", Currency: "SAR",
	})
	if code := errs.CodeOf(err); code != purchasingdomain.CodeDuplicateInvoice {
		t.Fatalf("code = %q, want %q", code, purchasingdomain.CodeDuplicateInvoice)
	}
}

func TestADeliveryFromAnotherSupplierCannotBeBilledHere(t *testing.T) {
	// Taking up another supplier's delivery would credit the wrong account and leave two
	// suppliers' books both wrong — one owed for goods it never sent, one not owed for goods it
	// did.
	f := newStockedFixture(t)
	_, _, receiptLines := f.delivered(t, 10_000_000)

	other, _ := id.New()
	now := "2026-08-01"
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx, `
		INSERT INTO partners (
			id, company_id, code, name, is_customer, is_supplier, partner_type,
			payment_terms_days, credit_limit_minor, is_active, has_history,
			row_version, created_at, updated_at
		) VALUES (?, ?, 'OTHER', 'Other Supplies', 0, 1, 'company', 30, 0, 1, 0, 1, ?, ?)`,
		string(other), string(f.companyID), now, now); err != nil {
		t.Fatalf("creating a second supplier: %v", err)
	}

	bill, err := f.svc.DraftBill(f.ctx, purchasing.NewBillInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		PartnerID: other, PartnerName: "Other Supplies",
		BillDate: "2026-08-20", SupplierInvoiceNumber: "OTH-1", Currency: "SAR",
	})
	if err != nil {
		t.Fatalf("DraftBill: %v", err)
	}

	if _, err = f.svc.AddBillLine(f.ctx, purchasing.BillLineInput{
		CompanyID: f.companyID, BillID: bill.ID, ReceiptLineID: receiptLines[0].ID,
	}); err == nil {
		t.Fatal("one supplier's bill took up another's delivery")
	}
}

// ── one bill, several deliveries ────────────────────────────────────────────────

func TestOneBillCanCoverSeveralDeliveries(t *testing.T) {
	// Criterion 6's other half. A supplier who delivers weekly and invoices monthly is the
	// ordinary case, and the bill must clear every accrual it takes up.
	f := newStockedFixture(t)
	books := f.booked(t)

	orderID := f.draft(t)
	f.addLine(t, orderID, f.variant.ID, 100_000_000)
	if _, err := f.svc.Place(f.ctx, orderID); err != nil {
		t.Fatalf("Place: %v", err)
	}
	_, orderLines, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}

	billID := f.draftBill(t, "ACME-8001")
	for _, arriving := range []int64{40_000_000, 60_000_000} {
		receiptID := f.draftReceipt(t, orderID)
		f.receiveLine(t, receiptID, orderLines[0].ID, arriving)
		if _, err = f.svc.ConfirmReceipt(f.ctx, receiptID); err != nil {
			t.Fatalf("ConfirmReceipt: %v", err)
		}
		_, receiptLines, receiptErr := f.svc.Receipt(f.ctx, receiptID)
		if receiptErr != nil {
			t.Fatalf("Receipt: %v", receiptErr)
		}
		if _, err = f.svc.AddBillLine(f.ctx, purchasing.BillLineInput{
			CompanyID: f.companyID, BillID: billID, ReceiptLineID: receiptLines[0].ID,
		}); err != nil {
			t.Fatalf("AddBillLine: %v", err)
		}
	}

	posted, err := f.svc.PostBill(f.ctx, billID)
	if err != nil {
		t.Fatalf("PostBill: %v", err)
	}
	// A hundred at 10.00.
	if posted.NetMinor != 100_000 || posted.AccruedMinor != 100_000 {
		t.Fatalf("net=%d accrued=%d, want 100000/100000",
			posted.NetMinor, posted.AccruedMinor)
	}

	balances := f.balances(t, books)
	if balances["2150"] != 0 {
		t.Errorf("GRNI = %d, want 0 — both accruals must be cleared", balances["2150"])
	}

	// And BOTH deliveries are now marked billed, so neither can be billed again.
	receipts, err := f.svc.Receipts(f.ctx, f.companyID, orderID, "")
	if err != nil {
		t.Fatalf("Receipts: %v", err)
	}
	for _, receipt := range receipts {
		if receipt.BilledAt == "" || receipt.BillID != posted.ID {
			t.Errorf("delivery %s was not linked to the bill that took it up", receipt.Number)
		}
	}
}

// ── the match as a report ───────────────────────────────────────────────────────

func TestTheMatchReportsPriceDifferencesRatherThanHidingThem(t *testing.T) {
	// A price variance is ACCEPTED, which makes it invisible unless something surfaces it. The
	// match is that something.
	f := newStockedFixture(t)
	_, _, receiptLines := f.delivered(t, 10_000_000)

	billID := f.draftBill(t, "ACME-9001")
	if _, err := f.svc.AddBillLine(f.ctx, purchasing.BillLineInput{
		CompanyID: f.companyID, BillID: billID, ReceiptLineID: receiptLines[0].ID,
		UnitPriceMicro: 11_000_000,
	}); err != nil {
		t.Fatalf("AddBillLine: %v", err)
	}

	results, err := f.svc.MatchOf(f.ctx, billID)
	if err != nil {
		t.Fatalf("MatchOf: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("%d match results, want 1", len(results))
	}
	if !results[0].QuantityMatches {
		t.Error("the quantity was reported as mismatched")
	}
	if results[0].PriceMatches {
		t.Error("a price difference was reported as matching")
	}
	if results[0].PriceVarianceMinor != 1_000 {
		t.Errorf("variance = %d, want 1000", results[0].PriceVarianceMinor)
	}
	if results[0].Matched() {
		t.Error("a line with a price variance reported a clean match")
	}
}
