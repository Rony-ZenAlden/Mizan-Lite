package sales_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/sales"
	"github.com/mizan-erp/mizan/internal/modules/sales/domain"
	"github.com/mizan-erp/mizan/internal/platform/printing"
)

func letterhead() sales.Letterhead {
	return sales.Letterhead{
		Company: "Al Noor Trading", Branch: "Main", Address: "12 Market Street",
		Phone: "+971 4 555 0100", TaxNumber: "100123456700003",
		Footer: "Thank you for your custom",
	}
}

func printRequest(documentID id.ID, template string) sales.PrintRequest {
	return sales.PrintRequest{
		DocumentID: documentID, Template: template, Direction: "ltr",
		Letterhead: letterhead(), Decimals: 2,
	}
}

// ── the test the whole phase was built for ──────────────────────────────────────

// TestAReprintSurvivesEveryChangeToMasterData is §9.3, asserted the only way it can be.
//
// The phase design says it plainly: "Reprinting a two-year-old invoice must reproduce it
// byte-identically", and "the test that matters is not 'does the total add up' but 'does this
// invoice still print the same'".
//
// So this test prints a posted invoice, then goes behind the service and changes EVERYTHING a
// naive implementation would have looked up — the product's name, its SKU, its unit code, and
// the price list — and prints again. The bytes must be identical.
//
// Written straight to the tables on purpose. Going through the catalog service would test that
// the service can rename a product; going to the table tests that PRINTING does not care, which
// is the claim being made.
func TestAReprintSurvivesEveryChangeToMasterData(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 150_000})
	f.receive(t, 10_000_000, 60_000_000)

	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, 2_000_000)
	posted, err := f.svc.Post(f.ctx, document.ID)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if posted.Number == "" {
		t.Fatal("a posted invoice has no number, so this test is not printing an invoice")
	}

	before, err := f.svc.Print(f.ctx, printRequest(posted.ID, "invoice"))
	if err != nil {
		t.Fatalf("Print: %v", err)
	}
	original := printing.HTML(before, printing.HTMLOptions{Paper: "A4", Title: posted.Number})

	// ── two years pass ───────────────────────────────────────────────────────────
	//
	// Scoped to the variant that was actually sold. An unscoped UPDATE collides with the other
	// variants' unique SKUs, which is the schema doing its job rather than a problem to route
	// around.
	_, lines, err := f.svc.Document(f.ctx, posted.ID)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	sold := lines[0]

	for _, change := range []struct {
		statement string
		args      []any
	}{
		{`UPDATE products SET name = 'Premium Widget (2028 revision)'
		    WHERE id = (SELECT product_id FROM product_variants WHERE id = ?)`,
			[]any{string(sold.VariantID)}},
		{`UPDATE product_variants SET sku = 'WIDGET-NEW-001' WHERE id = ?`,
			[]any{string(sold.VariantID)}},
		{`UPDATE units_of_measure SET code = 'EACH' WHERE code = ?`, []any{sold.UomCode}},
	} {
		if _, err = f.store.Writer(f.ctx).ExecContext(
			f.ctx, change.statement, change.args...); err != nil {
			t.Fatalf("%s: %v", change.statement, err)
		}
	}

	after, err := f.svc.Print(f.ctx, printRequest(posted.ID, "invoice"))
	if err != nil {
		t.Fatalf("reprint: %v", err)
	}
	reprint := printing.HTML(after, printing.HTMLOptions{Paper: "A4", Title: posted.Number})

	if original != reprint {
		was, now := firstDifference(original, reprint)
		t.Errorf("the reprint changed after master data changed. "+
			"The customer is holding the first one.\n--- first ---\n%s\n--- reprint ---\n%s",
			was, now)
	}

	// And the guard on the guard: the test must be exercising the renamed things. If the
	// document never mentioned the product's name, the assertion above would pass for the wrong
	// reason — which is how a test like this rots into decoration.
	if !strings.Contains(original, sold.ProductName) {
		t.Fatalf("the printed invoice never carried the product name %q, "+
			"so this test proves nothing about snapshots", sold.ProductName)
	}
	if strings.Contains(reprint, "Premium Widget") {
		t.Fatal("the reprint picked up the NEW product name")
	}
	if strings.Contains(reprint, "WIDGET-NEW-001") || strings.Contains(reprint, "EACH") {
		t.Fatal("the reprint picked up a new SKU or unit code")
	}
}

// ── what may be printed ─────────────────────────────────────────────────────────

func TestADraftCannotBePrinted(t *testing.T) {
	// A draft has no number, no tax point, and no agreement behind it. Printing one produces
	// something that looks like an invoice and is not — the sort of thing a customer keeps and
	// an auditor later finds.
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 0})
	f.receive(t, 10_000_000, 60_000_000)

	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, 1_000_000)

	_, err := f.svc.Print(f.ctx, printRequest(document.ID, "invoice"))
	if code := errs.CodeOf(err); code != sales.CodeNotPrintable {
		t.Fatalf("err = %v (code %q), want %q", err, code, sales.CodeNotPrintable)
	}
}

func TestAnUnknownTemplateIsRefusedByName(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 0})
	f.receive(t, 10_000_000, 60_000_000)
	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, 1_000_000)
	posted, err := f.svc.Post(f.ctx, document.ID)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}

	_, err = f.svc.Print(f.ctx, printRequest(posted.ID, "till_roll_v2"))
	if errs.CodeOf(err) != sales.CodeUnknownTemplate {
		t.Fatalf("err = %v, want %s", err, sales.CodeUnknownTemplate)
	}
}

// ── the shipped templates ───────────────────────────────────────────────────────

func TestEveryShippedTemplateRendersARealDocument(t *testing.T) {
	// A template is DATA, so a typo in it is not a compile error — it is a till that cannot
	// print, discovered by a shop at their busiest. Every shipped template is rendered against
	// a real posted invoice here, which is the only place the two halves meet.
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 150_000})
	f.receive(t, 10_000_000, 60_000_000)
	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, 2_000_000)
	posted, err := f.svc.Post(f.ctx, document.ID)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}

	templates, err := sales.Templates()
	if err != nil {
		t.Fatalf("Templates: %v", err)
	}
	if len(templates) < 2 {
		t.Fatalf("only %d templates ship, expected a receipt and an invoice", len(templates))
	}

	for _, template := range templates {
		t.Run(template.Code, func(t *testing.T) {
			rendered, renderErr := f.svc.Print(f.ctx, printRequest(posted.ID, template.Code))
			if renderErr != nil {
				// This is the failure a shop would meet, so the message names the file.
				t.Fatalf("the shipped template %q does not render: %v", template.Code, renderErr)
			}
			if len(rendered.Blocks) == 0 {
				t.Fatal("rendered to nothing")
			}

			page := printing.HTML(rendered, printing.HTMLOptions{Paper: "A4"})
			for _, required := range []string{posted.Number, "Al Noor Trading", "Widget"} {
				if !strings.Contains(page, required) {
					t.Errorf("the printed document does not mention %q", required)
				}
			}

			// And it prints on a till, too — the ESC/POS renderer refuses anything it cannot
			// print faithfully, so this also proves the shipped templates are Latin-safe.
			if _, escErr := printing.ESCPOS(rendered,
				printing.ESCPOSOptions{Columns: 48}); escErr != nil {
				t.Errorf("the shipped template cannot reach a till printer: %v", escErr)
			}
		})
	}
}

func TestAZeroOutstandingPrintsNothingRatherThanZero(t *testing.T) {
	// "Outstanding 0.00" on a receipt for a sale paid in full reads as a debt to anybody
	// scanning it. Blank is what drives omitWhenEmpty, and it is the mechanism that lets one
	// template serve a cash sale and a credit sale.
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 0})
	f.receive(t, 10_000_000, 60_000_000)
	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, 1_000_000)
	posted, err := f.svc.Post(f.ctx, document.ID)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}

	// Unpaid: the line appears.
	unpaid, err := f.svc.Print(f.ctx, printRequest(posted.ID, "receipt"))
	if err != nil {
		t.Fatalf("Print: %v", err)
	}
	if !mentionsOutstanding(unpaid) {
		t.Fatal("an unpaid invoice does not show what is owed")
	}

	// Paid in full: the line is gone entirely, not zeroed.
	//
	// A receipt is numbered from its OWN series, separately from invoices — a customer's
	// receipt and their invoice are different documents and an auditor follows them apart.
	f.series(t, sales.SeriesPayment, "RCT-", 6)
	if _, err = f.svc.TakePayment(f.ctx, sales.NewPaymentInput{
		CompanyID: f.companyID, BranchID: f.branchID, Method: domain.Cash,
		Date:     posted.Date,
		Currency: posted.CurrencyCode, AmountMinor: posted.TotalMinor,
		Settle: []sales.SettleInput{
			{DocumentID: posted.ID, AmountMinor: posted.TotalMinor},
		},
	}); err != nil {
		t.Fatalf("TakePayment: %v", err)
	}

	settled, err := f.svc.Print(f.ctx, printRequest(posted.ID, "receipt"))
	if err != nil {
		t.Fatalf("reprint: %v", err)
	}
	if mentionsOutstanding(settled) {
		t.Fatal("a fully paid receipt still carries an outstanding line")
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────────

func mentionsOutstanding(document printing.Document) bool {
	for _, block := range document.Blocks {
		if strings.Contains(block.Text, "outstanding") ||
			strings.Contains(block.Text, "Outstanding") {
			return true
		}
	}
	return false
}

// firstDifference reports where two renderings diverge, with context.
//
// A diff of two full A4 invoices is unreadable in a test log, and an unreadable failure is one
// somebody re-runs rather than reads.
func firstDifference(left, right string) (string, string) {
	a, b := []byte(left), []byte(right)
	limit := min(len(a), len(b))
	at := limit
	for i := range limit {
		if a[i] != b[i] {
			at = i
			break
		}
	}
	from := max(at-80, 0)
	return excerpt(a, from), excerpt(b, from)
}

func excerpt(data []byte, from int) string {
	end := min(from+200, len(data))
	return string(bytes.TrimSpace(data[from:end]))
}
