package sales_test

import (
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/modules/audit"
	"github.com/mizan-erp/mizan/internal/modules/sales"
	"github.com/mizan-erp/mizan/internal/modules/sales/domain"
	"github.com/mizan-erp/mizan/internal/platform/printing"
)

// The tests this file adds exist because the Phase 5 Definition-of-Done review found each of
// them missing. Every one closes a gap between what a criterion CLAIMS and what was actually
// asserted anywhere — which is the only thing a review of this kind is for.

// ── Criterion 1 ─────────────────────────────────────────────────────────────────
//
// "A posted line reproduces its invoice byte-identically after the PRICE LIST, the TAX RATE, the
// product name, and the unit name have all changed."
//
// 5.9's reprint test changed the product name, the SKU, and the unit code — three of the four.
// The price list and the tax rate were never touched, and they are the two that do not live in
// this module: they arrive through PORTS, and a reprint that re-asked them would be re-deriving
// the document rather than reproducing it.
//
// So this test swaps the ports for ones that answer completely differently — a different price,
// a different rate, a different list code — which is the sharpest available version of the
// claim. Changing a row in a price list table would prove the repository reads it; replacing the
// whole pricing service proves printing never asks.
func TestAReprintIgnoresANewPriceListAndANewTaxRate(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 150_000})
	f.receive(t, 10_000_000, 60_000_000)

	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, 2_000_000)
	posted, err := f.svc.Post(f.ctx, document.ID)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}

	original, err := f.svc.Print(f.ctx, printRequest(posted.ID, "invoice"))
	if err != nil {
		t.Fatalf("Print: %v", err)
	}
	before := printing.HTML(original, printing.HTMLOptions{Paper: "A4", Title: posted.Number})

	// ── the shop revises its prices and the government revises its tax ───────────
	repriced := sales.NewService(f.store, sales.Options{
		Clock: clock.System(), Bus: f.bus,
		Catalog: realCatalog{svc: f.catalog},
		// Nine times the price, on a different list, at four times the rate.
		Pricing: fixedPricing{priceMinor: 900}, Tax: fixedTax{rateMicro: 600_000},
		Stock: realStock{svc: f.inventory}, Credit: noCredit{},
	})

	after, err := repriced.Print(f.ctx, printRequest(posted.ID, "invoice"))
	if err != nil {
		t.Fatalf("reprint: %v", err)
	}
	reprint := printing.HTML(after, printing.HTMLOptions{Paper: "A4", Title: posted.Number})

	if before != reprint {
		t.Error("the reprint changed after the price list and tax rate changed; " +
			"the customer is holding the first one")
	}

	// The guard on the guard: if the invoice never carried a price at all, the assertion above
	// would pass for the wrong reason.
	if !strings.Contains(before, "1.00") {
		t.Fatalf("the invoice carries no unit price, so this proves nothing:\n%s", before)
	}
	if strings.Contains(reprint, "9.00") {
		t.Fatal("the reprint picked up the NEW price")
	}
}

// ── Criterion 4 ─────────────────────────────────────────────────────────────────
//
// "A posted document cannot be edited; correction is by credit note."
//
// Adding a line to a posted document was covered. REMOVING one was not — and removal is the
// worse direction, because it takes goods off an invoice that has already issued stock and
// written a journal entry, leaving both pointing at a line that no longer exists.
func TestALineCannotBeRemovedFromAPostedDocument(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 0})
	f.receive(t, 10_000_000, 60_000_000)

	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, 2_000_000)
	posted, err := f.svc.Post(f.ctx, document.ID)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}

	_, lines, err := f.svc.Document(f.ctx, posted.ID)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}

	err = f.svc.RemoveLine(f.ctx, posted.ID, lines[0].ID)
	if err == nil {
		t.Fatal("a line was removed from a posted invoice")
	}
	if code := errs.CodeOf(err); code != domain.CodeAlreadyPosted {
		t.Fatalf("code = %q, want %q", code, domain.CodeAlreadyPosted)
	}

	// And the line is still there. A refusal that had already deleted the row would be worse
	// than no refusal at all.
	_, after, err := f.svc.Document(f.ctx, posted.ID)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if len(after) != len(lines) {
		t.Fatalf("%d lines remain, want %d — the refusal deleted anyway",
			len(after), len(lines))
	}
}

// ── Criterion 6 ─────────────────────────────────────────────────────────────────
//
// "A sale that would take stock below zero is refused UNLESS THE WAREHOUSE PERMITS IT."
//
// The refusal was tested. The permission was not, and a criterion with an "unless" in it is two
// claims — the second of which is the one a real shop depends on. Selling from a van, or a
// counter whose deliveries are booked in the next morning, means going negative routinely.
//
// The column has been there since Phase 1, commented "Read from Phase 4". This is the test that
// it is genuinely read all the way from a SALE.
func TestASaleMayGoNegativeWhereTheWarehouseAllowsIt(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 0})
	f.receive(t, 2_000_000, 60_000_000) // only two on the shelf

	// First, the refusal — so the test proves the permission CHANGED something, rather than
	// that nothing was ever being checked.
	tooMany := f.draft(t, domain.Invoice)
	f.addLine(t, tooMany.ID, 5_000_000)
	if _, err := f.svc.Post(f.ctx, tooMany.ID); err == nil {
		t.Fatal("selling five of two was allowed while the warehouse forbade it")
	}

	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
		`UPDATE warehouses SET allows_negative_stock = 1 WHERE id = ?`,
		string(f.warehouseID)); err != nil {
		t.Fatalf("permitting negative stock: %v", err)
	}

	permitted := f.draft(t, domain.Invoice)
	f.addLine(t, permitted.ID, 5_000_000)
	posted, err := f.svc.Post(f.ctx, permitted.ID)
	if err != nil {
		t.Fatalf("the warehouse permits negative stock and the sale was still refused: %v", err)
	}
	if posted.Number == "" {
		t.Fatal("the sale posted without taking a number")
	}

	// And the stock really is negative — not clamped at zero, which would make the books say
	// there is nothing on a shelf that owes three.
	level, err := f.inventory.StockOf(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("OnHand: %v", err)
	}
	if level.OnHandMicro != -3_000_000 {
		t.Errorf("stock = %d, want -3000000", level.OnHandMicro)
	}
}

// ── Criterion 11 ────────────────────────────────────────────────────────────────
//
// "A held sale occupies no number AND MOVES NO STOCK."
//
// The number half was tested. The stock half was not — and it is the half that costs money: a
// hold that reserved or issued goods would take them off the shelf for a customer who walked
// away, and nothing would put them back.
func TestAHeldSaleMovesNoStock(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 0})
	f.receive(t, 10_000_000, 60_000_000)

	before, err := f.inventory.StockOf(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("OnHand: %v", err)
	}

	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, 4_000_000)
	if err = f.svc.Hold(f.ctx, document.ID, "the man in the blue coat"); err != nil {
		t.Fatalf("Hold: %v", err)
	}

	held, err := f.inventory.StockOf(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("OnHand: %v", err)
	}
	if held.OnHandMicro != before.OnHandMicro {
		t.Errorf("holding a sale moved stock: %d became %d",
			before.OnHandMicro, held.OnHandMicro)
	}
	// Not reserved either. A reservation is invisible on the shelf and just as costly: it makes
	// the goods unavailable to the next customer with no document to explain why.
	if held.ReservedMicro != before.ReservedMicro {
		t.Errorf("holding a sale reserved stock: %d became %d",
			before.ReservedMicro, held.ReservedMicro)
	}

	// Resuming and posting still moves it, so the absence above is a hold and not a broken sale.
	if err = f.svc.Resume(f.ctx, document.ID); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if _, err = f.svc.Post(f.ctx, document.ID); err != nil {
		t.Fatalf("Post: %v", err)
	}
	after, err := f.inventory.StockOf(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("OnHand: %v", err)
	}
	if after.OnHandMicro != before.OnHandMicro-4_000_000 {
		t.Errorf("posting the resumed sale moved %d, want 4000000",
			before.OnHandMicro-after.OnHandMicro)
	}
}

// ── Criterion 13 ────────────────────────────────────────────────────────────────
//
// "Every state change is audited in-transaction."
//
// One test asserted that creating a series is audited. Nothing asserted it for the acts that
// matter most — drafting, posting, holding, cancelling, taking money, opening and closing a
// till. Each was audited; nothing REQUIRED it, so the next one added would not have been.
//
// This walks a whole day's trading through the service and requires an entry for every act. It
// is written as a checklist rather than seven separate tests, because the failure it guards is
// "somebody added an eighth act and forgot" — and seven tests do not catch that either. What
// catches it is this test failing the moment an act appears in the list without an entry behind
// it.
func TestEveryActOnASaleLeavesAnAuditEntry(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 0})
	f.receive(t, 20_000_000, 60_000_000)
	f.series(t, sales.SeriesPayment, "RCT-", 6)

	// ── a day's trading ──────────────────────────────────────────────────────────
	shift, err := f.svc.OpenShift(f.ctx, sales.OpenShiftInput{
		CompanyID: f.companyID, BranchID: f.branchID, Terminal: "till-1", FloatMinor: 10_000,
	})
	if err != nil {
		t.Fatalf("OpenShift: %v", err)
	}

	parked := f.draft(t, domain.Invoice)
	f.addLine(t, parked.ID, 1_000_000)
	if err = f.svc.Hold(f.ctx, parked.ID, "blue coat"); err != nil {
		t.Fatalf("Hold: %v", err)
	}
	if err = f.svc.Resume(f.ctx, parked.ID); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if err = f.svc.Cancel(f.ctx, parked.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	sale := f.draft(t, domain.Invoice)
	f.addLine(t, sale.ID, 2_000_000)
	f.addLine(t, sale.ID, 1_000_000)
	_, lines, err := f.svc.Document(f.ctx, sale.ID)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if err = f.svc.RemoveLine(f.ctx, sale.ID, lines[1].ID); err != nil {
		t.Fatalf("RemoveLine: %v", err)
	}
	posted, err := f.svc.Post(f.ctx, sale.ID)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}

	if _, err = f.svc.TakePayment(f.ctx, sales.NewPaymentInput{
		CompanyID: f.companyID, BranchID: f.branchID, Date: posted.Date,
		Method: domain.Cash, Currency: posted.CurrencyCode, AmountMinor: posted.TotalMinor,
		ShiftID: shift.ID,
		Settle: []sales.SettleInput{
			{DocumentID: posted.ID, AmountMinor: posted.TotalMinor},
		},
	}); err != nil {
		t.Fatalf("TakePayment: %v", err)
	}

	if _, err = f.svc.CloseShift(
		f.ctx, f.companyID, shift.ID, 10_000+posted.TotalMinor, ""); err != nil {
		t.Fatalf("CloseShift: %v", err)
	}

	// ── every act must have left a record ────────────────────────────────────────
	entries, err := f.audit.Entries(f.ctx, audit.Filter{})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	seen := make(map[string]int, len(entries))
	for _, entry := range entries {
		seen[entry.Action]++
	}

	required := []string{
		sales.ActionDocumentDrafted, sales.ActionLineAdded, sales.ActionLineRemoved,
		sales.ActionDocumentHeld, sales.ActionDocumentResumed, sales.ActionDocumentCancelled,
		sales.ActionDocumentPosted,
		sales.ActionPaymentPosted,
		sales.ActionShiftOpened, sales.ActionShiftClosed,
	}
	for _, action := range required {
		if seen[action] == 0 {
			t.Errorf("%q happened and left no audit entry", action)
		}
	}

	// `sales.invoice.posted` is deliberately NOT in that list. It is a POSTING-RULE key that
	// Phase 2's rules match on, not an audit action — it selects a journal entry rather than
	// recording that somebody did something. The first version of this test asked the audit
	// trail for it and correctly found nothing; the two vocabularies shared a const block and
	// an `Action` prefix, and now they do not.
	if seen[sales.ActionInvoicePosted] != 0 {
		t.Error("a posting-rule key reached the audit trail; the two vocabularies have merged")
	}

	// Every entry names WHO and WHAT, or it is a row that cannot answer the question an audit
	// trail exists for.
	for _, entry := range entries {
		if entry.EntityType == "" || entry.EntityID == "" {
			t.Errorf("%q was audited against nothing: %+v", entry.Action, entry)
		}
	}
}
