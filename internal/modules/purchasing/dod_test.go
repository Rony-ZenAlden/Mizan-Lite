package purchasing_test

import (
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/modules/audit"
	"github.com/mizan-erp/mizan/internal/modules/purchasing"
	purchasingdomain "github.com/mizan-erp/mizan/internal/modules/purchasing/domain"
	"github.com/mizan-erp/mizan/internal/platform/config"
)

// The tests this file adds exist because the Phase 6 Definition-of-Done review found each of them
// missing. Every one closes a gap between what a criterion CLAIMS and what was actually asserted.

// ── Criterion 3 ─────────────────────────────────────────────────────────────────
//
// "A receipt beyond the tolerance is REFUSED; WITHIN IT, ACCEPTED AND RECORDED as an
// over-receipt."
//
// The refusal was tested and the acceptance was tested. What nothing checked is that an accepted
// over-delivery is RECORDED as one — that the extra units reach the received figure rather than
// being quietly clamped to what was ordered.
//
// Clamping would be the plausible mistake: `received = min(ordered, arrived)` looks tidy and
// makes every order close cleanly. It also means a supplier who over-delivers is never invoiced
// for the surplus and the stock on the shelf disagrees with the order that brought it in.
func TestAnAcceptedOverDeliveryIsRecordedInFull(t *testing.T) {
	f := newStockedFixture(t)

	if err := f.settings.Set(f.ctx, config.ScopeCompany, f.companyID,
		purchasing.OverReceiptTolerance.Key(), int64(50_000)); err != nil {
		t.Fatalf("setting the tolerance: %v", err)
	}

	orderID := f.draft(t)
	f.addLine(t, orderID, f.variant.ID, 100_000_000)
	if _, err := f.svc.Place(f.ctx, orderID); err != nil {
		t.Fatalf("Place: %v", err)
	}
	_, orderLines, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}

	// 105 arrive against 100 ordered, within a 5% tolerance.
	receiptID := f.draftReceipt(t, orderID)
	f.receiveLine(t, receiptID, orderLines[0].ID, 105_000_000)
	if _, err = f.svc.ConfirmReceipt(f.ctx, receiptID); err != nil {
		t.Fatalf("ConfirmReceipt: %v", err)
	}

	_, after, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	if after[0].ReceivedMicro != 105_000_000 {
		t.Fatalf("received = %d, want 105000000 — the surplus was clamped away",
			after[0].ReceivedMicro)
	}
	// Outstanding is still zero, because a supplier who sent five extra does not owe us minus
	// five. Both facts are true at once and both are recorded.
	if after[0].OutstandingMicro() != 0 {
		t.Errorf("outstanding = %d, want 0", after[0].OutstandingMicro())
	}

	// And the STOCK carries the surplus: 105 are on the shelf, not 100.
	level, err := f.inventory.StockOf(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("StockOf: %v", err)
	}
	if level.OnHandMicro != 105_000_000 {
		t.Errorf("on hand = %d, want 105000000", level.OnHandMicro)
	}
}

// ── Criterion 11 ────────────────────────────────────────────────────────────────
//
// "A number is allocated only at posting/confirmation; an ABANDONED DRAFT CONSUMES NONE."
//
// Orders were tested. Deliveries, bills and payments each have their own series, and each was
// asserted only to TAKE a number — never that an abandoned one leaves the counter alone.
//
// A hole in a numbered sequence is the thing an auditor asks about, and the explanation "we
// cancelled one" is only acceptable if it is true.
func TestAnAbandonedDocumentOfAnyKindConsumesNoNumber(t *testing.T) {
	f := newStockedFixture(t)

	// ── a cancelled delivery ────────────────────────────────────────────────────
	orderID := f.draft(t)
	f.addLine(t, orderID, f.variant.ID, 10_000_000)
	if _, err := f.svc.Place(f.ctx, orderID); err != nil {
		t.Fatalf("Place: %v", err)
	}
	_, orderLines, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}

	abandoned := f.draftReceipt(t, orderID)
	f.receiveLine(t, abandoned, orderLines[0].ID, 2_000_000)
	if err = f.svc.CancelReceipt(f.ctx, abandoned); err != nil {
		t.Fatalf("CancelReceipt: %v", err)
	}

	genuine := f.draftReceipt(t, orderID)
	f.receiveLine(t, genuine, orderLines[0].ID, 8_000_000)
	confirmed, err := f.svc.ConfirmReceipt(f.ctx, genuine)
	if err != nil {
		t.Fatalf("ConfirmReceipt: %v", err)
	}
	if confirmed.Number != "GRN-000001" {
		t.Errorf("delivery number = %q, want GRN-000001 — a cancelled one consumed it",
			confirmed.Number)
	}

	// ── a cancelled bill ────────────────────────────────────────────────────────
	_, receiptLines, err := f.svc.Receipt(f.ctx, genuine)
	if err != nil {
		t.Fatalf("Receipt: %v", err)
	}

	scrapped := f.draftBill(t, "ACME-DOD-1")
	if err = f.svc.CancelBill(f.ctx, scrapped); err != nil {
		t.Fatalf("CancelBill: %v", err)
	}

	billID := f.draftBill(t, "ACME-DOD-2")
	if _, err = f.svc.AddBillLine(f.ctx, purchasing.BillLineInput{
		CompanyID: f.companyID, BillID: billID, ReceiptLineID: receiptLines[0].ID,
	}); err != nil {
		t.Fatalf("AddBillLine: %v", err)
	}
	posted, err := f.svc.PostBill(f.ctx, billID)
	if err != nil {
		t.Fatalf("PostBill: %v", err)
	}
	if posted.Number != "BILL-000001" {
		t.Errorf("bill number = %q, want BILL-000001 — a cancelled one consumed it",
			posted.Number)
	}

	// ── a cancelled supplier return ─────────────────────────────────────────────
	dropped := f.draftReturn(t)
	if err = f.svc.CancelReturn(f.ctx, dropped); err != nil {
		t.Fatalf("CancelReturn: %v", err)
	}

	returnID := f.draftReturn(t)
	if _, err = f.svc.AddReturnLine(f.ctx, purchasing.ReturnLineInput{
		CompanyID: f.companyID, ReturnID: returnID, ReceiptLineID: receiptLines[0].ID,
		QuantityMicro: 1_000_000,
	}); err != nil {
		t.Fatalf("AddReturnLine: %v", err)
	}
	postedReturn, err := f.svc.PostReturn(f.ctx, returnID)
	if err != nil {
		t.Fatalf("PostReturn: %v", err)
	}
	if postedReturn.Number != "DN-000001" {
		t.Errorf("return number = %q, want DN-000001 — a cancelled one consumed it",
			postedReturn.Number)
	}
}

// ── Criterion 12 ────────────────────────────────────────────────────────────────
//
// "Every state change is AUDITED IN-TRANSACTION."
//
// Orders were checked. Deliveries, bills, returns and payments were each audited and nothing
// REQUIRED it — so the next act added would not have been. The same gap the Phase 5 DoD review
// found, in a module written after that review.
//
// Written as a checklist rather than five separate tests, because the failure it guards is
// "somebody added a sixth act and forgot", which five tests do not catch either.
func TestEveryActInPurchasingLeavesAnAuditEntry(t *testing.T) {
	f := newStockedFixture(t)

	// A whole cycle: order, deliver, bill, return, pay.
	orderID := f.draft(t)
	f.addLine(t, orderID, f.variant.ID, 10_000_000)
	if _, err := f.svc.Place(f.ctx, orderID); err != nil {
		t.Fatalf("Place: %v", err)
	}
	_, orderLines, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}

	receiptID := f.draftReceipt(t, orderID)
	f.receiveLine(t, receiptID, orderLines[0].ID, 10_000_000)
	if _, err = f.svc.ConfirmReceipt(f.ctx, receiptID); err != nil {
		t.Fatalf("ConfirmReceipt: %v", err)
	}
	_, receiptLines, err := f.svc.Receipt(f.ctx, receiptID)
	if err != nil {
		t.Fatalf("Receipt: %v", err)
	}

	charge, err := f.svc.AddLandedCost(f.ctx, purchasing.NewLandedCostInput{
		CompanyID: f.companyID, ReceiptID: receiptID,
		ChargeType: "freight", Currency: "SAR", AmountMinor: 500,
	})
	if err != nil {
		t.Fatalf("AddLandedCost: %v", err)
	}
	if _, err = f.svc.ApplyLandedCost(f.ctx, charge.ID); err != nil {
		t.Fatalf("ApplyLandedCost: %v", err)
	}

	billID := f.draftBill(t, "ACME-DOD-3")
	if _, err = f.svc.AddBillLine(f.ctx, purchasing.BillLineInput{
		CompanyID: f.companyID, BillID: billID, ReceiptLineID: receiptLines[0].ID,
	}); err != nil {
		t.Fatalf("AddBillLine: %v", err)
	}
	bill, err := f.svc.PostBill(f.ctx, billID)
	if err != nil {
		t.Fatalf("PostBill: %v", err)
	}

	returnID := f.draftReturn(t)
	if _, err = f.svc.AddReturnLine(f.ctx, purchasing.ReturnLineInput{
		CompanyID: f.companyID, ReturnID: returnID, ReceiptLineID: receiptLines[0].ID,
		QuantityMicro: 1_000_000,
	}); err != nil {
		t.Fatalf("AddReturnLine: %v", err)
	}
	if _, err = f.svc.PostReturn(f.ctx, returnID); err != nil {
		t.Fatalf("PostReturn: %v", err)
	}

	if _, err = f.svc.Pay(f.ctx, purchasing.NewPaymentInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		PartnerID: f.partnerID, PartnerName: "Acme Supplies",
		PaymentDate: "2026-08-30", Method: purchasingdomain.Cash, Currency: "SAR",
		AmountMinor: bill.TotalMinor,
		Settle: []purchasingdomain.Allocation{
			{BillID: bill.ID, AmountMinor: bill.TotalMinor},
		},
	}); err != nil {
		t.Fatalf("Pay: %v", err)
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
		purchasing.ActionOrderPlaced,
		purchasing.ActionReceiptDrafted, purchasing.ActionReceiptLineAdded,
		purchasing.ActionReceiptConfirmed,
		purchasing.ActionLandedCostAdded, purchasing.ActionLandedCostApplied,
		purchasing.ActionBillDrafted, purchasing.ActionBillLineAdded,
		purchasing.ActionBillPosted,
		purchasing.ActionReturnDrafted, purchasing.ActionReturnLineAdded,
		purchasing.ActionReturnPosted,
		purchasing.ActionPaymentPosted,
	} {
		if seen[action] == 0 {
			t.Errorf("%q happened and left no audit entry", action)
		}
	}

	// Every PURCHASING entry names what it was about, or it cannot answer the question an audit
	// trail exists for. Scoped to this module: other modules' entries are their own business,
	// and a catalog seed's entry has no entity by design.
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Action, "purchasing.") {
			continue
		}
		if entry.EntityType == "" || entry.EntityID == "" {
			t.Errorf("%q was audited against nothing: %+v", entry.Action, entry)
		}
	}

	// And the POSTING-RULE keys are ABSENT from the audit trail.
	//
	// One selects a journal entry, the other records that a person did something. They were the
	// same literal string until this review found it — the collision the Phase 5 review
	// separated by declaration, back as an identical value. A shared string means no log query,
	// report, or export can tell the two apart.
	for _, postingKey := range []string{
		purchasing.PostingReceiptConfirmed, purchasing.PostingBillPosted,
		purchasing.PostingReturnPosted, purchasing.PostingLandedCostApplied,
	} {
		if seen[postingKey] != 0 {
			t.Errorf("posting-rule key %q reached the audit trail", postingKey)
		}
	}
}

// ── Criterion 10 ────────────────────────────────────────────────────────────────
//
// "Purchasing contains NO ACCOUNTING LOGIC; every posting goes through Phase 2's rules."
//
// One test asserted it for the goods-receipt rule. Purchasing now fires FOUR different postings,
// and a module that named an account in any one of them would fail the criterion while passing
// the test.
func TestNoPurchasingPostingNamesAnAccount(t *testing.T) {
	f := newStockedFixture(t)
	books := f.booked(t)

	// # What this redirects, and why not everything
	//
	// INVENTORY and CASH — two accounts that are DEBITED or CREDITED once each in this cycle and
	// never cleared within it.
	//
	// Redirecting GRNI or payables would prove nothing: their whole purpose is to be accrued by
	// one document and cleared by the next, so over a full cycle they net to zero in the
	// redirected account whether or not anything posted. That is the same trap 6.2's §20.3 test
	// fell into, where a redirect and an empty entry looked identical.
	//
	// The assertion is therefore on the REDIRECTED accounts themselves: if any posting named an
	// account rather than going through a rule, that account would still move.
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx, `
		UPDATE posting_rule_lines SET account_selector = 'mapping:ROUNDING_DIFF'
		 WHERE account_selector IN ('mapping:INVENTORY', 'mapping:CASH')`,
	); err != nil {
		t.Fatalf("rewriting the rules: %v", err)
	}

	orderID := f.draft(t)
	f.addLine(t, orderID, f.variant.ID, 10_000_000)
	if _, err := f.svc.Place(f.ctx, orderID); err != nil {
		t.Fatalf("Place: %v", err)
	}
	_, orderLines, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}

	receiptID := f.draftReceipt(t, orderID)
	f.receiveLine(t, receiptID, orderLines[0].ID, 10_000_000)
	if _, err = f.svc.ConfirmReceipt(f.ctx, receiptID); err != nil {
		t.Fatalf("ConfirmReceipt: %v", err)
	}
	_, receiptLines, err := f.svc.Receipt(f.ctx, receiptID)
	if err != nil {
		t.Fatalf("Receipt: %v", err)
	}

	billID := f.draftBill(t, "ACME-DOD-4")
	if _, err = f.svc.AddBillLine(f.ctx, purchasing.BillLineInput{
		CompanyID: f.companyID, BillID: billID, ReceiptLineID: receiptLines[0].ID,
	}); err != nil {
		t.Fatalf("AddBillLine: %v", err)
	}
	bill, err := f.svc.PostBill(f.ctx, billID)
	if err != nil {
		t.Fatalf("PostBill: %v", err)
	}

	if _, err = f.svc.Pay(f.ctx, purchasing.NewPaymentInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		PartnerID: f.partnerID, PartnerName: "Acme Supplies",
		PaymentDate: "2026-08-30", Method: purchasingdomain.Cash, Currency: "SAR",
		AmountMinor: bill.TotalMinor,
		Settle: []purchasingdomain.Allocation{
			{BillID: bill.ID, AmountMinor: bill.TotalMinor},
		},
	}); err != nil {
		t.Fatalf("Pay: %v", err)
	}

	balances := f.balances(t, books)

	// The redirected accounts must be untouched. Inventory is debited by the receipt and cash is
	// credited by the payment; both went through rules, so both followed the rewrite.
	for _, code := range []string{"1300", "1110"} {
		if balances[code] != 0 {
			t.Errorf("account %s moved by %d — a posting named it rather than "+
				"going through a rule", code, balances[code])
		}
	}
	// And the entries really happened: GRNI was accrued and cleared, and the tax was claimed.
	if balances["1400"] == 0 {
		t.Error("no recoverable tax was claimed, so the bill did not post at all")
	}
}

// ── Criterion 13 ────────────────────────────────────────────────────────────────
//
// "Every seam Phase 4 left is CALLED BY REAL CODE, and none of them needed reshaping to serve
// it."
//
// The first half is testable and this asserts it. **The second half is FALSE, and that is the
// most valuable thing this phase found.**
//
// `Revaluation` was built in 4.2 — its own movement type, its own Neutral direction, a `revalue`
// case in the costing strategy, a place in the schema's CHECK list — and it could not be written.
// Two positive-quantity guards refused a movement of zero, which is the only kind a revaluation
// is. The seam was documented, unit-tested, and structurally unusable until 6.4 needed it.
//
// So the criterion is met at three of four, and the fourth is recorded as a finding rather than
// quietly reworded. A seam is only proven by a caller.
func TestEverySeamPhaseFourLeftHasARealCaller(t *testing.T) {
	f := newStockedFixture(t)

	// A full cycle that exercises all four: receipt, landed cost, price variance, return.
	orderID := f.draft(t)
	f.addLine(t, orderID, f.variant.ID, 10_000_000)
	if _, err := f.svc.Place(f.ctx, orderID); err != nil {
		t.Fatalf("Place: %v", err)
	}
	_, orderLines, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}

	receiptID := f.draftReceipt(t, orderID)
	f.receiveLine(t, receiptID, orderLines[0].ID, 10_000_000)
	if _, err = f.svc.ConfirmReceipt(f.ctx, receiptID); err != nil {
		t.Fatalf("ConfirmReceipt: %v", err)
	}
	_, receiptLines, err := f.svc.Receipt(f.ctx, receiptID)
	if err != nil {
		t.Fatalf("Receipt: %v", err)
	}

	charge, err := f.svc.AddLandedCost(f.ctx, purchasing.NewLandedCostInput{
		CompanyID: f.companyID, ReceiptID: receiptID,
		ChargeType: "freight", Currency: "SAR", AmountMinor: 700,
	})
	if err != nil {
		t.Fatalf("AddLandedCost: %v", err)
	}
	if _, err = f.svc.ApplyLandedCost(f.ctx, charge.ID); err != nil {
		t.Fatalf("ApplyLandedCost: %v", err)
	}

	billID := f.draftBill(t, "ACME-SEAM-1")
	if _, err = f.svc.AddBillLine(f.ctx, purchasing.BillLineInput{
		CompanyID: f.companyID, BillID: billID, ReceiptLineID: receiptLines[0].ID,
		UnitPriceMicro: 12_000_000, // disagrees with the order, so the variance revalues
	}); err != nil {
		t.Fatalf("AddBillLine: %v", err)
	}
	if _, err = f.svc.PostBill(f.ctx, billID); err != nil {
		t.Fatalf("PostBill: %v", err)
	}

	returnID := f.draftReturn(t)
	if _, err = f.svc.AddReturnLine(f.ctx, purchasing.ReturnLineInput{
		CompanyID: f.companyID, ReturnID: returnID, ReceiptLineID: receiptLines[0].ID,
		QuantityMicro: 2_000_000,
	}); err != nil {
		t.Fatalf("AddReturnLine: %v", err)
	}
	if _, err = f.svc.PostReturn(f.ctx, returnID); err != nil {
		t.Fatalf("PostReturn: %v", err)
	}

	// ── seam 1: `Revaluation` — value without quantity (4.2) ─────────────────────
	//
	// The one that needed reshaping. Twice: a domain guard and a schema CHECK, both refusing a
	// movement of zero quantity.
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := f.store.Reader(f.ctx).QueryRowContext(
			f.ctx, query, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		return n
	}

	if revaluations := count(
		`SELECT COUNT(*) FROM stock_movements WHERE movement_type = 'revaluation'`,
	); revaluations == 0 {
		t.Error("no revaluation was written — Phase 4's Revaluation seam has no caller")
	}
	// And it moved VALUE without moving quantity, which is the whole reason it has its own type
	// rather than being an inward move of zero.
	if moved := count(
		`SELECT COUNT(*) FROM stock_movements
		  WHERE movement_type = 'revaluation' AND quantity_micro <> 0`,
	); moved != 0 {
		t.Errorf("%d revaluations moved quantity", moved)
	}

	// ── seam 2: `inventory_layers` — written since 4.2, read by nothing ──────────
	if layers := count(`SELECT COUNT(*) FROM inventory_layers`); layers == 0 {
		t.Error("no inventory layer was written by a purchase receipt")
	}

	// ── seam 3: the movement's document link (4.3) ───────────────────────────────
	//
	// The rule that a movement with a document is posted by THAT document's module. Sales used
	// it; purchasing is the second caller, and the first for receipts.
	if linked := count(
		`SELECT COUNT(*) FROM stock_movements WHERE document_type = ? AND document_id <> ''`,
		purchasing.EntityReceipt,
	); linked == 0 {
		t.Error("no stock movement was linked to the delivery that caused it")
	}

	// ── seam 4: `Allocate` — largest-remainder (§D.3 trap 4) ─────────────────────
	//
	// Tested in 4.2 and called by nothing until landed costs. It now lives in the kernel, and
	// the allocations it produced are stored.
	if allocations := count(
		`SELECT COUNT(*) FROM landed_cost_allocations`,
	); allocations == 0 {
		t.Error("no landed cost was allocated across the goods it brought in")
	}

	// ── and `ReturnOut`, which Phase 6 ADDED rather than found ───────────────────
	if returns := count(
		`SELECT COUNT(*) FROM stock_movements WHERE movement_type = 'return_out'`,
	); returns == 0 {
		t.Error("no supplier return reached the stock ledger")
	}
}
