package domain_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/sales/domain"
)

const (
	branch    = id.ID("branch-1")
	warehouse = id.ID("wh-1")
	product   = id.ID("p-1")
	variant   = id.ID("v-1")
	uom       = id.ID("u-1")
)

func document(t *testing.T, kind domain.Type) domain.Document {
	t.Helper()
	built, err := domain.NewDocument(id.ID("d1"), branch, kind, "2026-06-15", "SYP")
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return built
}

func line(t *testing.T, quantityMicro int64) domain.Line {
	t.Helper()
	built, err := domain.NewLine(id.ID("l1"), product, variant, uom, 1, quantityMicro)
	if err != nil {
		t.Fatalf("NewLine: %v", err)
	}
	return built
}

// ── line arithmetic ─────────────────────────────────────────────────────────────

func TestALineIsQuantityTimesPriceLessDiscount(t *testing.T) {
	cases := []struct {
		name          string
		quantityMicro int64
		priceMinor    int64
		discountMinor int64
		wantNet       int64
	}{
		{"three at a hundred", 3_000_000, 100, 0, 300},
		{"with a discount", 3_000_000, 100, 50, 250},
		{"a fractional quantity", 2_500_000, 100, 0, 250},
		{"a discount that takes it to zero", 1_000_000, 100, 100, 0},
		{"one at zero is free, not an error", 1_000_000, 0, 0, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			priced, err := line(t, tc.quantityMicro).Price(tc.priceMinor, tc.discountMinor)
			if err != nil {
				t.Fatalf("Price: %v", err)
			}
			if priced.NetMinor != tc.wantNet {
				t.Errorf("net = %d, want %d", priced.NetMinor, tc.wantNet)
			}
		})
	}
}

// Rounded ONCE, half away from zero. Rounding the quantity first, or the product twice, drifts a
// document total by a minor unit per line — which a customer notices on a fifty-line invoice and
// nobody can explain.
func TestALineIsRoundedOnce(t *testing.T) {
	// 0.333333 × 100 = 33.3333 → 33.
	priced, err := line(t, 333_333).Price(100, 0)
	if err != nil {
		t.Fatalf("Price: %v", err)
	}
	if priced.NetMinor != 33 {
		t.Errorf("net = %d, want 33", priced.NetMinor)
	}

	// 0.005 × 100 = 0.5 → 1, away from zero.
	half, err := line(t, 5_000).Price(100, 0)
	if err != nil {
		t.Fatalf("Price: %v", err)
	}
	if half.NetMinor != 1 {
		t.Errorf("net = %d, want 1 — a half rounds away from zero", half.NetMinor)
	}
}

// A wholesaler's quantity times a hyperinflated currency's price overflows int64 long before
// either figure looks unusual. The 128-bit intermediate is what keeps this honest.
func TestALineSurvivesAmountsThatOverflowSixtyFourBits(t *testing.T) {
	// A million units at ten billion minor units apiece.
	priced, err := line(t, 1_000_000_000_000).Price(10_000_000_000, 0)
	if err != nil {
		t.Fatalf("Price: %v", err)
	}
	// 10^12 / 10^6 × 10^10 = 10^16, which fits int64 but whose intermediate (10^22) does not.
	if priced.NetMinor != 10_000_000_000_000_000 {
		t.Errorf("net = %d, want 10000000000000000 — the intermediate overflowed",
			priced.NetMinor)
	}
}

// A discount larger than the line makes the net negative — a refund wearing a sale's clothes.
// Refused rather than clamped: clamping would silently give the goods away and print a total the
// operator did not intend.
func TestADiscountCannotExceedItsLine(t *testing.T) {
	_, err := line(t, 1_000_000).Price(100, 101)
	if err == nil {
		t.Fatal("a discount larger than the line was accepted")
	}
	if code := errs.CodeOf(err); code != domain.CodeDiscountTooBig {
		t.Errorf("code = %q, want %q", code, domain.CodeDiscountTooBig)
	}
	// Both figures are named, because "discount too large" is not actionable and
	// "you discounted 101 off a line of 100" is.
	typed, _ := errs.AsError(err)
	if typed.Params["discount"] != "101" || typed.Params["line"] != "100" {
		t.Errorf("params = %v, want both figures", typed.Params)
	}
}

func TestANegativePriceIsRefused(t *testing.T) {
	if _, err := line(t, 1_000_000).Price(-1, 0); err == nil {
		t.Fatal("a negative price was accepted")
	}
}

// The tax RATE is stored alongside the amount: a group's rate changes, and what this line was
// taxed at does not.
func TestALineRecordsTheRateItWasTaxedAt(t *testing.T) {
	priced, err := line(t, 2_000_000).Price(100, 0)
	if err != nil {
		t.Fatalf("Price: %v", err)
	}
	taxed := priced.Tax(150_000, 30, "VAT15")

	if taxed.TaxRateMicro != 150_000 {
		t.Errorf("rate = %d, want 150000", taxed.TaxRateMicro)
	}
	if taxed.TotalMinor != 230 {
		t.Errorf("total = %d, want 230 (200 net + 30 tax)", taxed.TotalMinor)
	}
}

// ── document totals ─────────────────────────────────────────────────────────────

// Summed from lines rather than accumulated as they are added, so a document's totals cannot
// drift from the lines that justify them.
func TestDocumentTotalsAreSummedFromTheLines(t *testing.T) {
	first, _ := line(t, 2_000_000).Price(100, 20)
	first = first.Tax(150_000, 27, "VAT15")
	second, _ := line(t, 1_000_000).Price(50, 0)
	second = second.Tax(150_000, 8, "VAT15")

	net, tax, discount, total := domain.Totals([]domain.Line{first, second})

	if net != 230 {
		t.Errorf("net = %d, want 230 (180 + 50)", net)
	}
	if tax != 35 {
		t.Errorf("tax = %d, want 35", tax)
	}
	if discount != 20 {
		t.Errorf("discount = %d, want 20", discount)
	}
	if total != 265 {
		t.Errorf("total = %d, want 265", total)
	}
}

func TestAnEmptyDocumentTotalsToZero(t *testing.T) {
	net, tax, discount, total := domain.Totals(nil)
	if net != 0 || tax != 0 || discount != 0 || total != 0 {
		t.Errorf("an empty document totalled %d/%d/%d/%d", net, tax, discount, total)
	}
}

// ── the lifecycle ───────────────────────────────────────────────────────────────

// A posted invoice is corrected by a CREDIT NOTE, never by editing. Editing would silently
// restate a period whose books may be closed, and would leave the stock movement and the journal
// entry describing a document that no longer says what they were made from.
func TestAPostedDocumentCannotBeChanged(t *testing.T) {
	d := document(t, domain.Invoice)
	if err := d.RequireDraft(); err != nil {
		t.Fatalf("a draft was not editable: %v", err)
	}

	d.Status = domain.Posted
	d.Number = "INV-000001"
	err := d.RequireDraft()
	if err == nil {
		t.Fatal("a posted document was editable")
	}
	if code := errs.CodeOf(err); code != domain.CodeAlreadyPosted {
		t.Errorf("code = %q, want %q", code, domain.CodeAlreadyPosted)
	}
	// The message tells the operator what to do instead, which is the difference between a
	// refusal and an obstruction.
	typed, _ := errs.AsError(err)
	if typed.Params["number"] != "INV-000001" {
		t.Errorf("params = %v, want the document number", typed.Params)
	}
}

func TestACancelledDocumentCannotBeChanged(t *testing.T) {
	d := document(t, domain.Invoice)
	d.Status = domain.Cancelled

	if err := d.RequireDraft(); err == nil {
		t.Fatal("a cancelled document was editable")
	} else if code := errs.CodeOf(err); code != domain.CodeCancelled {
		t.Errorf("code = %q, want %q", code, domain.CodeCancelled)
	}
}

// ── what moves stock ────────────────────────────────────────────────────────────

// A quotation is a promise and an order is an intention; neither takes anything off a shelf.
// Putting this on the TYPE means a report, a screen, and the poster cannot disagree about it.
func TestOnlyInvoicesAndCreditNotesMoveStock(t *testing.T) {
	cases := map[domain.Type]bool{
		domain.Quotation:  false,
		domain.Order:      false,
		domain.Invoice:    true,
		domain.CreditNote: true,
	}
	for kind, moves := range cases {
		if kind.MovesStock() != moves {
			t.Errorf("%s.MovesStock() = %v, want %v", kind, kind.MovesStock(), moves)
		}
	}
}

func TestADocumentThatMovesStockNeedsAWarehouse(t *testing.T) {
	d := document(t, domain.Invoice)
	lines := []domain.Line{line(t, 1_000_000)}

	if err := d.RequirePostable(lines); err == nil {
		t.Fatal("an invoice with no warehouse was postable")
	}

	d.WarehouseID = warehouse
	if err := d.RequirePostable(lines); err != nil {
		t.Errorf("an invoice with a warehouse was refused: %v", err)
	}
}

// A quotation naming a warehouse implies goods are reserved there, which they are not. Refused
// rather than ignored, because a screen that showed it would be telling the operator something
// untrue.
func TestAQuotationWithAWarehouseIsRefused(t *testing.T) {
	d := document(t, domain.Quotation)
	d.WarehouseID = warehouse

	err := d.RequirePostable([]domain.Line{line(t, 1_000_000)})
	if err == nil {
		t.Fatal("a quotation reserved a warehouse")
	}
	if code := errs.CodeOf(err); code != domain.CodeQuotationMoves {
		t.Errorf("code = %q, want %q", code, domain.CodeQuotationMoves)
	}
}

// An empty invoice consumes a number, posts a zero entry, and tells a customer nothing.
func TestADocumentWithNoLinesCannotBePosted(t *testing.T) {
	d := document(t, domain.Invoice)
	d.WarehouseID = warehouse

	if err := d.RequirePostable(nil); err == nil {
		t.Fatal("an empty document was postable")
	} else if code := errs.CodeOf(err); code != domain.CodeNoLines {
		t.Errorf("code = %q, want %q", code, domain.CodeNoLines)
	}
}

// ── construction ────────────────────────────────────────────────────────────────

// The business date decides the fiscal period. Defaulting it to today would put a sale entered on
// Monday for Saturday's trading into the wrong period, silently.
func TestADocumentNeedsADate(t *testing.T) {
	if _, err := domain.NewDocument(id.ID("d1"), branch, domain.Invoice, "  ", "SYP"); err == nil {
		t.Fatal("a document with no date was accepted")
	}
}

func TestADocumentStartsAsADraftWithNoNumber(t *testing.T) {
	d := document(t, domain.Invoice)

	if d.Status != domain.Draft {
		t.Errorf("status = %q, want draft", d.Status)
	}
	// §9.4: an abandoned draft must consume no number.
	if d.Number != "" {
		t.Errorf("a new draft already holds number %q", d.Number)
	}
}

// The money is NOT set at construction: a caller that could name its own price is a till operator
// who can type a discount nobody approved.
func TestALineIsBornWithNoPrice(t *testing.T) {
	l := line(t, 1_000_000)
	if l.UnitPriceMinor != 0 || l.NetMinor != 0 || l.TaxAmountMinor != 0 || l.CostMicro != 0 {
		t.Error("a new line already carries money")
	}
}

func TestALineNeedsAPositiveQuantity(t *testing.T) {
	for _, quantity := range []int64{0, -1_000_000} {
		if _, err := domain.NewLine(
			id.ID("l1"), product, variant, uom, 1, quantity); err == nil {
			t.Errorf("a line of %d was accepted", quantity)
		}
	}
}

func TestAnUnknownDocumentTypeIsRefused(t *testing.T) {
	if _, err := domain.NewDocument(
		id.ID("d1"), branch, "receipt", "2026-06-15", "SYP"); err == nil {
		t.Fatal("an unknown document type was accepted")
	}
}
