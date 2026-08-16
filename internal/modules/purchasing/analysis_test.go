package purchasing_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/purchasing"
)

// The fixture bills on 2026-08-20 and returns on 2026-08-25, in SAR.
const (
	august    = "2026-08-01"
	augustEnd = "2026-08-31"
	billDay   = "2026-08-20"
)

// ── what was spent ──────────────────────────────────────────────────────────────

// TestSpendIsWhatTheSupplierCharged
//
// Net of discount and BEFORE tax. Recoverable tax is reclaimed rather than spent, and
// non-recoverable tax is already inside what the goods cost through the accrual — so adding
// `tax_amount_minor` here would count the recoverable part twice over.
func TestSpendIsWhatTheSupplierCharged(t *testing.T) {
	f := newStockedFixture(t)
	bill := f.billed(t, "ACME-001")

	analysis, err := f.svc.SpendByProduct(f.ctx, f.companyID, august, augustEnd)
	if err != nil {
		t.Fatalf("SpendByProduct: %v", err)
	}
	if len(analysis.Rows) != 1 {
		t.Fatalf("%d rows, want 1", len(analysis.Rows))
	}
	if analysis.Total.NetMinor != bill.NetMinor {
		t.Errorf("spend = %d and the bill's net is %d",
			analysis.Total.NetMinor, bill.NetMinor)
	}
	if bill.TaxMinor != 0 && analysis.Total.NetMinor == bill.TotalMinor {
		t.Errorf("spend = %d, which is the bill's TOTAL of %d — tax was counted as spend",
			analysis.Total.NetMinor, bill.TotalMinor)
	}
	if analysis.Rows[0].Documents != 1 {
		t.Errorf("documents = %d, want 1", analysis.Rows[0].Documents)
	}
	if analysis.Rows[0].Label == "" {
		t.Error("a spend row with no label cannot be acted on")
	}
}

// TestAnOrderIsNotSpendAndNeitherIsADelivery
//
// An order is an intention; a receipt is goods arriving. Neither is money owed. The BILL is where
// the supplier states the price — which is the whole reason 6.5 made it a separate document —
// and a spend report built on orders would count what was asked for rather than what was charged.
func TestAnOrderIsNotSpendAndNeitherIsADelivery(t *testing.T) {
	f := newStockedFixture(t)

	// Placed and delivered, never billed.
	f.delivered(t, 10_000_000)

	analysis, err := f.svc.SpendByProduct(f.ctx, f.companyID, august, augustEnd)
	if err != nil {
		t.Fatalf("SpendByProduct: %v", err)
	}
	if analysis.Total.NetMinor != 0 || len(analysis.Rows) != 0 {
		t.Errorf("goods that arrived and were never billed count as spend: %+v", analysis)
	}
}

// TestADraftBillIsNotSpend
func TestADraftBillIsNotSpend(t *testing.T) {
	f := newStockedFixture(t)
	f.billed(t, "ACME-001")

	// A second bill, drafted with a line and never posted.
	_, _, receiptLines := f.delivered(t, 4_000_000)
	draft := f.draftBill(t, "ACME-DRAFT")
	if _, err := f.svc.AddBillLine(f.ctx, purchasing.BillLineInput{
		CompanyID: f.companyID, BillID: draft, ReceiptLineID: receiptLines[0].ID,
	}); err != nil {
		t.Fatalf("AddBillLine: %v", err)
	}

	analysis, err := f.svc.SpendByProduct(f.ctx, f.companyID, august, augustEnd)
	if err != nil {
		t.Fatalf("SpendByProduct: %v", err)
	}
	if analysis.Rows[0].Documents != 1 {
		t.Errorf("%d documents, want 1 — a draft bill was counted",
			analysis.Rows[0].Documents)
	}
	// The quantity is what a draft can leak: it is set when the line is added, whereas a draft
	// bill's money is settled at posting. 8.2's D172 is the reason this assertion exists.
	if analysis.Total.QuantityMicro != 10_000_000 {
		t.Errorf("quantity = %d, want 10000000 — a draft's four units reached the report",
			analysis.Total.QuantityMicro)
	}
}

// ── the sign ────────────────────────────────────────────────────────────────────

// TestASupplierReturnSubtractsRatherThanAdding
//
// A supplier return's lines carry POSITIVE quantities, exactly as a sales credit note's do. An
// analysis that summed both would report goods sent back as more goods bought — and a shop that
// returns a bad delivery would read as having spent twice.
func TestASupplierReturnSubtractsRatherThanAdding(t *testing.T) {
	f := newStockedFixture(t)

	orderID, _, receiptLines := f.delivered(t, 10_000_000)
	_ = orderID

	billID := f.draftBill(t, "ACME-001")
	if _, err := f.svc.AddBillLine(f.ctx, purchasing.BillLineInput{
		CompanyID: f.companyID, BillID: billID, ReceiptLineID: receiptLines[0].ID,
	}); err != nil {
		t.Fatalf("AddBillLine: %v", err)
	}
	bill, err := f.svc.PostBill(f.ctx, billID)
	if err != nil {
		t.Fatalf("PostBill: %v", err)
	}

	// Two of the ten go back.
	returnID := f.draftReturn(t)
	if _, err = f.svc.AddReturnLine(f.ctx, purchasing.ReturnLineInput{
		CompanyID: f.companyID, ReturnID: returnID,
		ReceiptLineID: receiptLines[0].ID, QuantityMicro: 2_000_000,
	}); err != nil {
		t.Fatalf("AddReturnLine: %v", err)
	}
	if _, err = f.svc.PostReturn(f.ctx, returnID); err != nil {
		t.Fatalf("PostReturn: %v", err)
	}

	analysis, err := f.svc.SpendByProduct(f.ctx, f.companyID, august, augustEnd)
	if err != nil {
		t.Fatalf("SpendByProduct: %v", err)
	}

	// Eight of ten remain bought.
	if analysis.Total.QuantityMicro != 8_000_000 {
		t.Errorf("quantity = %d, want 8000000 — 10000000 means the return was counted as "+
			"another purchase", analysis.Total.QuantityMicro)
	}
	if analysis.Total.NetMinor >= bill.NetMinor {
		t.Errorf("spend = %d after returning two of ten, and the bill alone was %d",
			analysis.Total.NetMinor, bill.NetMinor)
	}
	if analysis.Total.NetMinor <= 0 {
		t.Errorf("spend = %d after returning two of ten", analysis.Total.NetMinor)
	}
}

// ── the three cuts ──────────────────────────────────────────────────────────────

// TestTheThreeSpendAnalysesAddUpToTheSameTotal
func TestTheThreeSpendAnalysesAddUpToTheSameTotal(t *testing.T) {
	f := newStockedFixture(t)
	f.billed(t, "ACME-001")
	f.billed(t, "ACME-002")

	byPeriod, err := f.svc.SpendByPeriod(f.ctx, f.companyID, august, augustEnd, purchasing.ByDay)
	if err != nil {
		t.Fatalf("SpendByPeriod: %v", err)
	}
	byProduct, err := f.svc.SpendByProduct(f.ctx, f.companyID, august, augustEnd)
	if err != nil {
		t.Fatalf("SpendByProduct: %v", err)
	}
	bySupplier, err := f.svc.SpendBySupplier(f.ctx, f.companyID, august, augustEnd)
	if err != nil {
		t.Fatalf("SpendBySupplier: %v", err)
	}

	if byPeriod.Total.NetMinor == 0 {
		t.Fatal("two posted bills spent nothing, so this test proves nothing")
	}
	for name, analysis := range map[string]purchasing.PurchaseAnalysis{
		"product": byProduct, "supplier": bySupplier,
	} {
		if analysis.Total.NetMinor != byPeriod.Total.NetMinor {
			t.Errorf("by %s totals %d, by period totals %d",
				name, analysis.Total.NetMinor, byPeriod.Total.NetMinor)
		}
		var summed int64
		for _, row := range analysis.Rows {
			summed += row.NetMinor
		}
		if summed != analysis.Total.NetMinor {
			t.Errorf("the by-%s rows add to %d and the total says %d",
				name, summed, analysis.Total.NetMinor)
		}
	}

	// Every supplier row names a partner. A bill's supplier is NOT NULL (6.5), so unlike the
	// sales side there is no unattributed bucket — and a row without one would be a defect.
	for _, row := range bySupplier.Rows {
		if row.ID == id.ID("") {
			t.Errorf("a spend row names no supplier: %+v", row)
		}
	}
}

// TestAPeriodSpendIsChronologicalAndARankingIsNot
func TestAPeriodSpendIsChronologicalAndARankingIsNot(t *testing.T) {
	f := newStockedFixture(t)

	// A SMALLER bill, earlier in the month. The two orders must disagree, or the test cannot
	// tell a sequence from a ranking — 8.2's D174.
	small := f.billed(t, "ACME-SMALL")
	f.rebill(t, "2026-08-05", small.ID)
	f.billed(t, "ACME-BIG")
	f.billed(t, "ACME-BIG-2")

	analysis, err := f.svc.SpendByPeriod(f.ctx, f.companyID, august, augustEnd, purchasing.ByDay)
	if err != nil {
		t.Fatalf("SpendByPeriod: %v", err)
	}
	if len(analysis.Rows) != 2 {
		t.Fatalf("%d buckets, want 2: %+v", len(analysis.Rows), analysis.Rows)
	}
	if analysis.Rows[0].Key != "2026-08-05" {
		t.Errorf("first bucket = %q, want 2026-08-05 — the earlier day, even though the later "+
			"one spent more", analysis.Rows[0].Key)
	}
	if analysis.Rows[0].NetMinor >= analysis.Rows[1].NetMinor {
		t.Fatalf("the earlier bucket spent %d and the later %d — they must differ, or this "+
			"test cannot tell a sequence from a ranking",
			analysis.Rows[0].NetMinor, analysis.Rows[1].NetMinor)
	}

	monthly, err := f.svc.SpendByPeriod(f.ctx, f.companyID, august, augustEnd, purchasing.ByMonth)
	if err != nil {
		t.Fatalf("SpendByPeriod: %v", err)
	}
	if len(monthly.Rows) != 1 || monthly.Rows[0].Key != "2026-08" {
		t.Errorf("monthly buckets = %+v, want one 2026-08", monthly.Rows)
	}
}

// TestASpendAnalysisOverAnUntradedCompanyIsEmptyAndNotAnError
//
// DoD criterion 11.
func TestASpendAnalysisOverAnUntradedCompanyIsEmptyAndNotAnError(t *testing.T) {
	f := newStockedFixture(t)

	analysis, err := f.svc.SpendBySupplier(f.ctx, f.companyID, august, augustEnd)
	if err != nil {
		t.Fatalf("SpendBySupplier over an untraded company: %v", err)
	}
	if len(analysis.Rows) != 0 || analysis.Total.NetMinor != 0 {
		t.Errorf("an untraded company spent %d over %d rows",
			analysis.Total.NetMinor, len(analysis.Rows))
	}
}

// TestASpendRangeAndGroupingThatCannotMeanAnythingAreRefused
func TestASpendRangeAndGroupingThatCannotMeanAnythingAreRefused(t *testing.T) {
	f := newStockedFixture(t)

	_, err := f.svc.SpendByProduct(f.ctx, f.companyID, augustEnd, august)
	if code := errs.CodeOf(err); code != purchasing.CodeInvalidRange {
		t.Errorf("a backwards range gave %q, want %q", code, purchasing.CodeInvalidRange)
	}

	_, err = f.svc.SpendByPeriod(f.ctx, f.companyID, august, augustEnd,
		purchasing.Grouping("quarter"))
	if code := errs.CodeOf(err); code != purchasing.CodeUnknownGrouping {
		t.Errorf("an unknown grouping gave %q, want %q", code, purchasing.CodeUnknownGrouping)
	}
}

// rebill moves a posted bill's date, straight to the table.
//
// There is no service method for it, correctly: a posted bill's date is what the supplier stated.
// A period analysis needs two dates to have an ORDER at all, and the fixture bills everything on
// the same day.
func (f fixture) rebill(t *testing.T, date string, billID id.ID) {
	t.Helper()
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
		`UPDATE purchase_bills SET bill_date = ? WHERE id = ?`,
		date, string(billID)); err != nil {
		t.Fatalf("rebilling: %v", err)
	}
}
