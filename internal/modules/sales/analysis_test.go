package sales_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/sales"
	"github.com/mizan-erp/mizan/internal/modules/sales/domain"
)

// The fixture's documents are dated 2026-06-15.
const (
	june      = "2026-06-01"
	juneEnd   = "2026-06-30"
	juneOnly  = "2026-06-15"
	yearStart = "2026-01-01"
	yearFin   = "2026-12-31"
)

// analysisFixture sells at 100 apiece against stock received at 60, so every figure below is
// checkable by hand: two sold is 200 revenue, 120 cost, 80 margin.
func analysisFixture(t *testing.T) fixture {
	t.Helper()
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 150_000})
	f.receive(t, 100_000_000, 60_000_000)
	return f
}

// soldID posts a sale and returns just its id, for a caller that only needs to move its date.
func soldID(t *testing.T, f fixture, qtyMicro int64) id.ID {
	t.Helper()
	posted, _ := f.sold(t, qtyMicro)
	return posted.ID
}

// ── the figure the phase is named for ───────────────────────────────────────────

// TestMarginIsRevenueLessTheCostFrozenAtTheTimeOfSale
//
// DoD criterion 4. The cost is the one SNAPSHOTTED onto the line when the sale posted, not
// today's — so a report run next year, after every price and every average cost has moved,
// reproduces the margin that actually applied.
//
// The test makes the two differ: it sells at a cost of 60, then receives more stock at 200,
// which moves the weighted average. A report reading today's cost would show a loss.
func TestMarginIsRevenueLessTheCostFrozenAtTheTimeOfSale(t *testing.T) {
	f := analysisFixture(t)
	f.sold(t, 2_000_000) // two at 100, costing 60 each

	// The world moves on: more stock at more than triple the price.
	f.receive(t, 100_000_000, 200_000_000)

	analysis, err := f.svc.SalesByProduct(f.ctx, f.companyID, june, juneEnd)
	if err != nil {
		t.Fatalf("SalesByProduct: %v", err)
	}
	if len(analysis.Rows) != 1 {
		t.Fatalf("%d rows, want 1", len(analysis.Rows))
	}

	row := analysis.Rows[0]
	if row.RevenueMinor != 200 {
		t.Errorf("revenue = %d, want 200 — net of discount and BEFORE tax", row.RevenueMinor)
	}
	if row.CostMinor != 120 {
		t.Errorf("cost = %d, want 120 — the cost frozen at the sale, not today's average",
			row.CostMinor)
	}
	if row.GrossMarginMinor != 80 {
		t.Errorf("gross margin = %d, want 80", row.GrossMarginMinor)
	}
	// 80/200 is 40%, which is 400,000 at §E's Percent scale.
	if got := row.MarginPercentMicro(); got != 400_000 {
		t.Errorf("margin percent = %d, want 400000 (40%%)", got)
	}
}

// TestRevenueExcludesTax
//
// Tax is collected on behalf of a government and owed to it, so it is not revenue. A margin
// computed against a tax-inclusive figure would flatter every product by the tax rate — 15% here,
// which is more than most shops make.
func TestRevenueExcludesTax(t *testing.T) {
	f := analysisFixture(t)
	posted, _ := f.sold(t, 2_000_000)

	// The document itself carries tax, so this is not a fixture that happens to have none.
	if posted.TaxMinor == 0 {
		t.Fatal("the fixture charged no tax, so this test proves nothing")
	}

	analysis, err := f.svc.SalesByProduct(f.ctx, f.companyID, june, juneEnd)
	if err != nil {
		t.Fatalf("SalesByProduct: %v", err)
	}
	if analysis.Total.RevenueMinor != 200 {
		t.Errorf("revenue = %d, want 200 — the total of %d includes the %d of tax",
			analysis.Total.RevenueMinor, posted.TotalMinor, posted.TaxMinor)
	}
}

// ── the sign ────────────────────────────────────────────────────────────────────

// TestACreditNoteSubtractsRatherThanAdding
//
// # The single most important test in this file
//
// A credit note's lines carry POSITIVE quantities — `NewLine` refuses a negative one, because a
// return is a document rather than a negative row. An analysis that summed both document types
// would report a returned sale as TWO sales, and a shop with heavy returns would read as its own
// best month.
func TestACreditNoteSubtractsRatherThanAdding(t *testing.T) {
	f := analysisFixture(t)
	invoice, lines := f.sold(t, 4_000_000) // four at 100

	// Two come back, through the real return path — a credit note line must name the sale line
	// it reverses, which is what makes the cost of a return the ORIGINAL cost (§D.3).
	credit, err := f.svc.DraftReturn(f.ctx, sales.ReturnInput{
		CompanyID: f.companyID, InvoiceID: invoice.ID,
		Lines: []sales.ReturnLineInput{
			{SourceLineID: lines[0].ID, QuantityMicro: 2_000_000},
		},
	})
	if err != nil {
		t.Fatalf("DraftReturn: %v", err)
	}
	if _, err = f.svc.Post(f.ctx, credit.ID); err != nil {
		t.Fatalf("posting the credit note: %v", err)
	}

	analysis, err := f.svc.SalesByProduct(f.ctx, f.companyID, june, juneEnd)
	if err != nil {
		t.Fatalf("SalesByProduct: %v", err)
	}

	// Four sold less two returned is two: 200 revenue, not 600.
	if analysis.Total.RevenueMinor != 200 {
		t.Errorf("revenue = %d, want 200 — four sold less two returned. 600 means the credit "+
			"note was counted as another sale", analysis.Total.RevenueMinor)
	}
	if analysis.Total.QuantityMicro != 2_000_000 {
		t.Errorf("quantity = %d, want 2000000", analysis.Total.QuantityMicro)
	}
	if analysis.Total.CostMinor != 120 {
		t.Errorf("cost = %d, want 120", analysis.Total.CostMinor)
	}
}

// TestADraftIsNotASale
//
// The filter an aggregate is most likely to leave out. A draft is a screen somebody has open, and
// counting one reports revenue nobody has agreed to.
func TestADraftIsNotASale(t *testing.T) {
	f := analysisFixture(t)
	f.sold(t, 2_000_000)

	// A second document, drafted with lines and never posted.
	pending := f.draft(t, domain.Invoice)
	f.addLine(t, pending.ID, 50_000_000)

	analysis, err := f.svc.SalesByProduct(f.ctx, f.companyID, june, juneEnd)
	if err != nil {
		t.Fatalf("SalesByProduct: %v", err)
	}
	if analysis.Total.RevenueMinor != 200 {
		t.Errorf("revenue = %d, want 200 — a draft was counted", analysis.Total.RevenueMinor)
	}

	// The QUANTITY is what actually detects this.
	//
	// A draft line has no money on it at all: price, tax and cost are resolved at posting, so a
	// draft contributes zero revenue whether it is filtered out or not. A drill that deleted
	// `status = 'posted'` left the revenue assertion above green for exactly that reason. The
	// quantity IS set when the line is added, so it is the one field a draft can leak.
	if analysis.Total.QuantityMicro != 2_000_000 {
		t.Errorf("quantity = %d, want 2000000 — a draft's 50 units reached the report",
			analysis.Total.QuantityMicro)
	}
	if len(analysis.Rows) != 1 {
		t.Errorf("%d rows, want 1", len(analysis.Rows))
	}
}

// ── the three cuts ──────────────────────────────────────────────────────────────

// TestTheThreeAnalysesAddUpToTheSameTotal
//
// Cut three ways, one company, one answer. A shopkeeper WILL add the product rows up and compare
// them with the month — and a walk-in sale silently dropped from the partner analysis is exactly
// how the three stop agreeing.
func TestTheThreeAnalysesAddUpToTheSameTotal(t *testing.T) {
	f := analysisFixture(t)
	f.sold(t, 2_000_000)
	f.sold(t, 3_000_000)

	byPeriod, err := f.svc.SalesByPeriod(f.ctx, f.companyID, june, juneEnd, sales.ByDay)
	if err != nil {
		t.Fatalf("SalesByPeriod: %v", err)
	}
	byProduct, err := f.svc.SalesByProduct(f.ctx, f.companyID, june, juneEnd)
	if err != nil {
		t.Fatalf("SalesByProduct: %v", err)
	}
	byPartner, err := f.svc.SalesByPartner(f.ctx, f.companyID, june, juneEnd)
	if err != nil {
		t.Fatalf("SalesByPartner: %v", err)
	}

	if byPeriod.Total.RevenueMinor != 500 {
		t.Errorf("period revenue = %d, want 500", byPeriod.Total.RevenueMinor)
	}
	for name, analysis := range map[string]sales.Analysis{
		"product": byProduct, "partner": byPartner,
	} {
		if analysis.Total.RevenueMinor != byPeriod.Total.RevenueMinor {
			t.Errorf("by %s totals %d, by period totals %d",
				name, analysis.Total.RevenueMinor, byPeriod.Total.RevenueMinor)
		}
		var summed int64
		for _, row := range analysis.Rows {
			summed += row.RevenueMinor
		}
		if summed != analysis.Total.RevenueMinor {
			t.Errorf("the by-%s rows add to %d and the total says %d",
				name, summed, analysis.Total.RevenueMinor)
		}
	}

	// The walk-in row exists rather than being dropped. These sales name no partner, and a
	// partner analysis that skipped them would total less than the other two.
	if len(byPartner.Rows) != 1 || byPartner.Rows[0].Label != "walk-in" {
		t.Errorf("partner rows = %+v, want one walk-in row", byPartner.Rows)
	}
}

// TestAPeriodAnalysisIsChronologicalAndARankingIsNot
//
// "Which products make money" is a ranking; "is this month better than last" is a sequence. A
// period report sorted by margin puts July before March whenever July did better, which answers
// a question nobody asked and hides the one they did.
func TestAPeriodAnalysisIsChronologicalAndARankingIsNot(t *testing.T) {
	f := analysisFixture(t)

	// A WORSE day, earlier in the month. The two orders must disagree, or the test cannot tell
	// them apart: the first version put the bigger sale on the earlier day, so ranking by margin
	// and sorting by date gave the same answer and a drill removing the chronological branch
	// changed nothing.
	//
	// Backdated straight to the table, because posting stamps the date the caller drafted with
	// and a posted document's date is not a service method's to change.
	f.backdate(t, "2026-06-02", soldID(t, f, 2_000_000))
	f.sold(t, 5_000_000)

	analysis, err := f.svc.SalesByPeriod(f.ctx, f.companyID, june, juneEnd, sales.ByDay)
	if err != nil {
		t.Fatalf("SalesByPeriod: %v", err)
	}
	if len(analysis.Rows) != 2 {
		t.Fatalf("%d buckets, want 2", len(analysis.Rows))
	}
	if analysis.Rows[0].Key != "2026-06-02" {
		t.Errorf("first bucket = %q, want 2026-06-02 — the earlier day, even though the later "+
			"one earned MORE", analysis.Rows[0].Key)
	}
	if analysis.Rows[0].RevenueMinor >= analysis.Rows[1].RevenueMinor {
		t.Fatalf("the earlier bucket earned %d and the later %d — they must differ, or this "+
			"test cannot tell a sequence from a ranking",
			analysis.Rows[0].RevenueMinor, analysis.Rows[1].RevenueMinor)
	}

	// And a RANKING of the same two days really does put them the other way round, which is what
	// makes the arrangement a choice rather than an accident of the data.
	ranked, err := f.svc.SalesByProduct(f.ctx, f.companyID, june, juneEnd)
	if err != nil {
		t.Fatalf("SalesByProduct: %v", err)
	}
	if len(ranked.Rows) != 1 {
		t.Fatalf("%d product rows, want 1 — both days sold the same variant", len(ranked.Rows))
	}

	// By month, the two days collapse into one bucket.
	monthly, err := f.svc.SalesByPeriod(f.ctx, f.companyID, june, juneEnd, sales.ByMonth)
	if err != nil {
		t.Fatalf("SalesByPeriod: %v", err)
	}
	if len(monthly.Rows) != 1 || monthly.Rows[0].Key != "2026-06" {
		t.Errorf("monthly buckets = %+v, want one 2026-06", monthly.Rows)
	}
	if monthly.Rows[0].RevenueMinor != analysis.Total.RevenueMinor {
		t.Errorf("the month totals %d and the days total %d",
			monthly.Rows[0].RevenueMinor, analysis.Total.RevenueMinor)
	}
}

// TestARangeExcludesWhatFallsOutsideIt
func TestARangeExcludesWhatFallsOutsideIt(t *testing.T) {
	f := analysisFixture(t)
	f.sold(t, 2_000_000)

	analysis, err := f.svc.SalesByProduct(f.ctx, f.companyID, "2026-07-01", "2026-07-31")
	if err != nil {
		t.Fatalf("SalesByProduct: %v", err)
	}
	if analysis.Total.RevenueMinor != 0 || len(analysis.Rows) != 0 {
		t.Errorf("a July report picked up a June sale: %+v", analysis)
	}

	// And the boundary is INCLUSIVE at both ends: a sale on the last day of the range counts.
	analysis, err = f.svc.SalesByProduct(f.ctx, f.companyID, juneOnly, juneOnly)
	if err != nil {
		t.Fatalf("SalesByProduct: %v", err)
	}
	if analysis.Total.RevenueMinor != 200 {
		t.Errorf("a single-day range covering the sale's own date reported %d",
			analysis.Total.RevenueMinor)
	}
}

// TestAnAnalysisOverAnUntradedCompanyIsEmptyAndNotAnError
//
// DoD criterion 11.
func TestAnAnalysisOverAnUntradedCompanyIsEmptyAndNotAnError(t *testing.T) {
	f := analysisFixture(t)

	analysis, err := f.svc.SalesByProduct(f.ctx, f.companyID, yearStart, yearFin)
	if err != nil {
		t.Fatalf("SalesByProduct over an untraded company: %v", err)
	}
	if len(analysis.Rows) != 0 {
		t.Errorf("%d rows, want none", len(analysis.Rows))
	}
	if analysis.Total.MarginPercentMicro() != 0 {
		t.Errorf("margin percent = %d over no revenue", analysis.Total.MarginPercentMicro())
	}
}

// TestARangeAndAGroupingThatCannotMeanAnythingAreRefused
func TestARangeAndAGroupingThatCannotMeanAnythingAreRefused(t *testing.T) {
	f := analysisFixture(t)

	_, err := f.svc.SalesByProduct(f.ctx, f.companyID, juneEnd, june)
	if code := errs.CodeOf(err); code != sales.CodeInvalidRange {
		t.Errorf("a backwards range gave %q, want %q", code, sales.CodeInvalidRange)
	}

	_, err = f.svc.SalesByPeriod(f.ctx, f.companyID, june, juneEnd, sales.Grouping("quarter"))
	if code := errs.CodeOf(err); code != sales.CodeUnknownGrouping {
		t.Errorf("an unknown grouping gave %q, want %q", code, sales.CodeUnknownGrouping)
	}
}
