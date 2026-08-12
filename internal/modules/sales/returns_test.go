package sales_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/modules/sales"
	"github.com/mizan-erp/mizan/internal/modules/sales/domain"
)

// sold posts an invoice and returns it with its lines.
func (f fixture) sold(t *testing.T, qty int64) (domain.Document, []domain.Line) {
	t.Helper()
	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, qty)
	posted, err := f.svc.Post(f.ctx, document.ID)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	_, lines, err := f.svc.Document(f.ctx, posted.ID)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	return posted, lines
}

// ── §D.3: the cost that matters ─────────────────────────────────────────────────

// The trap with the most expensive consequence, now reachable end to end. Returning an item sold
// at a cost of 60 when today's average is 300 must credit inventory with 60 — crediting 300 would
// invent 240 of profit out of a customer changing their mind.
func TestAReturnIsCostedAtTheOriginalSaleNotTodaysAverage(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 0})
	f.receive(t, 10_000_000, 60_000_000)

	invoice, lines := f.sold(t, 2_000_000)

	// Prices rise sharply after the sale.
	f.receive(t, 10_000_000, 300_000_000)

	note, err := f.svc.DraftReturn(f.ctx, sales.ReturnInput{
		CompanyID: f.companyID, InvoiceID: invoice.ID, Reason: "damaged",
	})
	if err != nil {
		t.Fatalf("DraftReturn: %v", err)
	}
	if _, err = f.svc.Post(f.ctx, note.ID); err != nil {
		t.Fatalf("Post: %v", err)
	}

	_, creditLines, err := f.svc.Document(f.ctx, note.ID)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	// The ORIGINAL cost, walked through: credit-note line → invoice line → its movement.
	if creditLines[0].CostMicro != 60_000_000 {
		t.Errorf("return costed at %d, want the original 60000000", creditLines[0].CostMicro)
	}
	_ = lines
}

// The chain that makes the above possible. Without a source line there is no correct cost, and
// using the average would be exactly the phantom profit §D.3 forbids.
func TestACreditNoteLineNamesTheSaleLineItReverses(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 0})
	f.receive(t, 10_000_000, 60_000_000)

	invoice, lines := f.sold(t, 2_000_000)
	note, err := f.svc.DraftReturn(f.ctx, sales.ReturnInput{
		CompanyID: f.companyID, InvoiceID: invoice.ID,
	})
	if err != nil {
		t.Fatalf("DraftReturn: %v", err)
	}

	_, creditLines, err := f.svc.Document(f.ctx, note.ID)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if creditLines[0].SourceLineID != lines[0].ID {
		t.Error("the credit note line does not name the sale line it reverses")
	}
	// And the note names the invoice.
	credited, _, err := f.svc.Document(f.ctx, note.ID)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if credited.SourceID != invoice.ID {
		t.Error("the credit note does not name the invoice it credits")
	}
}

// ── the snapshot travels with the return ────────────────────────────────────────

// A credit note must describe what was SOLD, including what the product was called then.
// Re-fetching from the catalog would make a return of a renamed product say something the
// original invoice does not.
func TestACreditNoteCopiesTheInvoicesSnapshotRatherThanRefetching(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 0})
	f.receive(t, 10_000_000, 60_000_000)

	invoice, _ := f.sold(t, 2_000_000)

	// The product is renamed between the sale and the return.
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
		`UPDATE products SET name = 'Widget (RENAMED)' WHERE code = 'WIDGET'`); err != nil {
		t.Fatalf("renaming: %v", err)
	}

	note, err := f.svc.DraftReturn(f.ctx, sales.ReturnInput{
		CompanyID: f.companyID, InvoiceID: invoice.ID,
	})
	if err != nil {
		t.Fatalf("DraftReturn: %v", err)
	}

	_, creditLines, err := f.svc.Document(f.ctx, note.ID)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if creditLines[0].ProductName != "Widget" {
		t.Errorf("the credit note says %q — it re-fetched instead of copying",
			creditLines[0].ProductName)
	}
}

// ── stock comes back ────────────────────────────────────────────────────────────

func TestPostingACreditNoteReturnsTheStock(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 0})
	f.receive(t, 10_000_000, 60_000_000)

	invoice, _ := f.sold(t, 3_000_000)
	if state := f.stockNow(t); state != 7_000_000 {
		t.Fatalf("on hand = %d after the sale, want 7000000", state)
	}

	note, err := f.svc.DraftReturn(f.ctx, sales.ReturnInput{
		CompanyID: f.companyID, InvoiceID: invoice.ID,
	})
	if err != nil {
		t.Fatalf("DraftReturn: %v", err)
	}
	if _, err = f.svc.Post(f.ctx, note.ID); err != nil {
		t.Fatalf("Post: %v", err)
	}

	if state := f.stockNow(t); state != 10_000_000 {
		t.Errorf("on hand = %d after the return, want 10000000", state)
	}
}

// ── the books ───────────────────────────────────────────────────────────────────

// Drill 20's obligation, discharged: a credit note publishes its OWN action, and the rules that
// action matches reverse the sale. A negative invoice would flip a line's side silently, which is
// why the engine refuses negatives and why this is a separate event.
func TestACreditNotePostsItsOwnActionAndReversesTheSale(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 150_000})
	books := f.booked(t)
	f.receive(t, 10_000_000, 60_000_000)

	invoice, _ := f.sold(t, 2_000_000)
	afterSale := f.balances(t, books)
	if afterSale["1200"] != 230 {
		t.Fatalf("receivables = %d after the sale, want 230", afterSale["1200"])
	}

	note, err := f.svc.DraftReturn(f.ctx, sales.ReturnInput{
		CompanyID: f.companyID, InvoiceID: invoice.ID,
	})
	if err != nil {
		t.Fatalf("DraftReturn: %v", err)
	}
	if _, err = f.svc.Post(f.ctx, note.ID); err != nil {
		t.Fatalf("Post: %v", err)
	}

	afterReturn := f.balances(t, books)

	// Everything the sale did is undone: what the customer owes, the revenue, the tax, and the
	// cost.
	if afterReturn["1200"] != 0 {
		t.Errorf("receivables = %d after the return, want 0", afterReturn["1200"])
	}
	if afterReturn["4100"] != 0 {
		t.Errorf("sales = %d after the return, want 0", afterReturn["4100"])
	}
	if afterReturn["2200"] != 0 {
		t.Errorf("tax payable = %d after the return, want 0", afterReturn["2200"])
	}
	if afterReturn["5100"] != 0 {
		t.Errorf("cost of goods sold = %d after the return, want 0", afterReturn["5100"])
	}
	if afterReturn["1300"] != 0 {
		t.Errorf("inventory = %d after the return, want 0", afterReturn["1300"])
	}
}

// ── what a return refuses ───────────────────────────────────────────────────────

// Three of two would put stock on the shelf that was never sold and credit money that was never
// taken.
func TestReturningMoreThanWasSoldIsRefused(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 0})
	f.receive(t, 10_000_000, 60_000_000)

	invoice, lines := f.sold(t, 2_000_000)

	_, err := f.svc.DraftReturn(f.ctx, sales.ReturnInput{
		CompanyID: f.companyID, InvoiceID: invoice.ID,
		Lines: []sales.ReturnLineInput{
			{SourceLineID: lines[0].ID, QuantityMicro: 3_000_000},
		},
	})
	if err == nil {
		t.Fatal("three were returned from a sale of two")
	}
	if code := errs.CodeOf(err); code != sales.CodeReturnTooMuch {
		t.Errorf("code = %q, want %q", code, sales.CodeReturnTooMuch)
	}
}

// Summed ACROSS credit notes: two today and two more tomorrow is the same overreturn as four at
// once, and a per-note check would miss it.
func TestReturningInStagesCannotExceedTheSaleInTotal(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 0})
	f.receive(t, 10_000_000, 60_000_000)

	invoice, lines := f.sold(t, 4_000_000)

	first, err := f.svc.DraftReturn(f.ctx, sales.ReturnInput{
		CompanyID: f.companyID, InvoiceID: invoice.ID,
		Lines: []sales.ReturnLineInput{{SourceLineID: lines[0].ID, QuantityMicro: 3_000_000}},
	})
	if err != nil {
		t.Fatalf("first return: %v", err)
	}
	if _, err = f.svc.Post(f.ctx, first.ID); err != nil {
		t.Fatalf("Post: %v", err)
	}

	// One more is fine; two would be one too many.
	if _, err = f.svc.DraftReturn(f.ctx, sales.ReturnInput{
		CompanyID: f.companyID, InvoiceID: invoice.ID,
		Lines: []sales.ReturnLineInput{{SourceLineID: lines[0].ID, QuantityMicro: 2_000_000}},
	}); err == nil {
		t.Fatal("five were returned in total from a sale of four")
	}

	if _, err = f.svc.DraftReturn(f.ctx, sales.ReturnInput{
		CompanyID: f.companyID, InvoiceID: invoice.ID,
		Lines: []sales.ReturnLineInput{{SourceLineID: lines[0].ID, QuantityMicro: 1_000_000}},
	}); err != nil {
		t.Errorf("the last unreturned unit was refused: %v", err)
	}
}

// Returning everything is the common case, and an operator should not have to enumerate it.
func TestAReturnWithNoLinesCreditsTheWholeInvoice(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 0})
	f.receive(t, 10_000_000, 60_000_000)

	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, 2_000_000)
	f.addLine(t, document.ID, 3_000_000)
	invoice, err := f.svc.Post(f.ctx, document.ID)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}

	note, err := f.svc.DraftReturn(f.ctx, sales.ReturnInput{
		CompanyID: f.companyID, InvoiceID: invoice.ID,
	})
	if err != nil {
		t.Fatalf("DraftReturn: %v", err)
	}

	_, creditLines, err := f.svc.Document(f.ctx, note.ID)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if len(creditLines) != 2 {
		t.Fatalf("%d lines credited, want both", len(creditLines))
	}
	if creditLines[0].QuantityMicro != 2_000_000 || creditLines[1].QuantityMicro != 3_000_000 {
		t.Errorf("credited %d and %d, want the whole invoice",
			creditLines[0].QuantityMicro, creditLines[1].QuantityMicro)
	}
}

// An unposted invoice is corrected by editing it — that is what draft means. A credit note
// against a draft would reverse something that never happened.
func TestOnlyAPostedInvoiceCanBeCredited(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 0})
	f.receive(t, 10_000_000, 60_000_000)

	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, 2_000_000)

	_, err := f.svc.DraftReturn(f.ctx, sales.ReturnInput{
		CompanyID: f.companyID, InvoiceID: document.ID,
	})
	if err == nil {
		t.Fatal("a draft invoice was credited")
	}
	if code := errs.CodeOf(err); code != sales.CodeInvoiceNotPosted {
		t.Errorf("code = %q, want %q", code, sales.CodeInvoiceNotPosted)
	}
}

func TestACreditNoteCannotBeCredited(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 0})
	f.receive(t, 10_000_000, 60_000_000)

	invoice, _ := f.sold(t, 2_000_000)
	note, err := f.svc.DraftReturn(f.ctx, sales.ReturnInput{
		CompanyID: f.companyID, InvoiceID: invoice.ID,
	})
	if err != nil {
		t.Fatalf("DraftReturn: %v", err)
	}
	if _, err = f.svc.Post(f.ctx, note.ID); err != nil {
		t.Fatalf("Post: %v", err)
	}

	if _, err = f.svc.DraftReturn(f.ctx, sales.ReturnInput{
		CompanyID: f.companyID, InvoiceID: note.ID,
	}); err == nil {
		t.Fatal("a credit note was itself credited")
	}
}

// ── numbering ───────────────────────────────────────────────────────────────────

// An invoice and a credit note both starting at 1 is correct; sharing a counter would make
// invoice numbers jump every time a return was issued.
func TestCreditNotesHaveTheirOwnSequence(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 0})
	f.receive(t, 10_000_000, 60_000_000)

	invoice, _ := f.sold(t, 2_000_000)
	if invoice.Number != "INV-000001" {
		t.Fatalf("invoice number = %q", invoice.Number)
	}

	note, err := f.svc.DraftReturn(f.ctx, sales.ReturnInput{
		CompanyID: f.companyID, InvoiceID: invoice.ID,
	})
	if err != nil {
		t.Fatalf("DraftReturn: %v", err)
	}
	posted, err := f.svc.Post(f.ctx, note.ID)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if posted.Number != "CN-000001" {
		t.Errorf("credit note number = %q, want CN-000001", posted.Number)
	}

	// And the invoice sequence is untouched.
	next, err := f.svc.PreviewNumber(f.ctx, f.branchID, sales.SeriesInvoice)
	if err != nil {
		t.Fatalf("PreviewNumber: %v", err)
	}
	if next != "INV-000002" {
		t.Errorf("next invoice = %q — the credit note took an invoice number", next)
	}
}
