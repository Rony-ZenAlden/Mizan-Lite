package sales_test

import (
	"context"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/catalog"
	"github.com/mizan-erp/mizan/internal/modules/sales"
	"github.com/mizan-erp/mizan/internal/modules/sales/domain"
)

// realCatalog satisfies the Catalog port from the actual catalog service, so a line's snapshot
// and its unit conversion travel the real path.
type realCatalog struct{ svc *catalog.Service }

func (c realCatalog) LineFacts(
	ctx context.Context, companyID, variantID, uomID id.ID, quantityMicro int64,
) (sales.LineFacts, error) {
	product, variant, unit, inStock, err := c.svc.SaleFacts(
		ctx, companyID, variantID, uomID, quantityMicro)
	if err != nil {
		return sales.LineFacts{}, err
	}
	return sales.LineFacts{
		ProductID: product.ID, ProductName: product.Name, VariantSKU: variant.SKU,
		UomID: unit.ID, UomCode: unit.Code, QuantityStockMicro: inStock,
	}, nil
}

func (f fixture) draft(t *testing.T, kind domain.Type) domain.Document {
	t.Helper()
	created, err := f.svc.Draft(f.ctx, sales.NewDocumentInput{
		CompanyID: f.companyID, BranchID: f.branchID, WarehouseID: f.warehouseID,
		Type: kind, Date: "2026-06-15", Currency: "SYP",
	})
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}
	return created
}

func (f fixture) addLine(t *testing.T, documentID id.ID, qty int64) domain.Line {
	t.Helper()
	added, err := f.svc.AddLine(f.ctx, sales.AddLineInput{
		CompanyID: f.companyID, DocumentID: documentID,
		VariantID: f.variant.ID, QuantityMicro: qty,
	})
	if err != nil {
		t.Fatalf("AddLine: %v", err)
	}
	return added
}

// ── THE SNAPSHOT RULE (§9.3) ────────────────────────────────────────────────────

// The single most important rule in the phase. A line stores what it was computed FROM, not a
// reference to something that can change — because the join answers today's question, and an
// invoice is a record of what was agreed then.
func TestALineSnapshotsWhatThingsWereCalled(t *testing.T) {
	f := newSellingFixture(t)
	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, 2_000_000)

	// Everything the line referred to is now renamed.
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
		`UPDATE products SET name = 'Widget (DISCONTINUED)' WHERE code = 'WIDGET'`); err != nil {
		t.Fatalf("renaming the product: %v", err)
	}
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
		`UPDATE product_variants SET sku = 'OLD-SKU' WHERE id = ?`,
		string(f.variant.ID)); err != nil {
		t.Fatalf("renaming the SKU: %v", err)
	}
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
		`UPDATE units_of_measure SET code = 'RENAMED' WHERE code = 'PCS'`); err != nil {
		t.Fatalf("renaming the unit: %v", err)
	}

	_, lines, err := f.svc.Document(f.ctx, document.ID)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("%d lines, want 1", len(lines))
	}

	// The line still says what the customer bought.
	if lines[0].ProductName != "Widget" {
		t.Errorf("product name = %q — the reprint followed the rename", lines[0].ProductName)
	}
	if lines[0].VariantSKU != "WIDGET" {
		t.Errorf("SKU = %q — the reprint followed the rename", lines[0].VariantSKU)
	}
	if lines[0].UomCode != "PCS" {
		t.Errorf("unit = %q — the reprint followed the rename", lines[0].UomCode)
	}
}

// ── dual quantity (§B.3) ────────────────────────────────────────────────────────

// A reprinted invoice says "2 rolls" while inventory correctly moved 200 metres — and neither
// figure depends on a conversion factor that might be edited later.
func TestALineStoresBothTheSoldAndTheStockQuantity(t *testing.T) {
	f := newSellingFixture(t)
	document := f.draft(t, domain.Invoice)

	// Sold in kilograms; stocked in grams.
	line, err := f.svc.AddLine(f.ctx, sales.AddLineInput{
		CompanyID: f.companyID, DocumentID: document.ID,
		VariantID: f.heavyVariant.ID, UomID: f.kilogramID, QuantityMicro: 2_000_000,
	})
	if err != nil {
		t.Fatalf("AddLine: %v", err)
	}

	if line.QuantityMicro != 2_000_000 {
		t.Errorf("sold quantity = %d, want 2000000 (2 kg)", line.QuantityMicro)
	}
	if line.UomCode != "KG" {
		t.Errorf("unit = %q, want KG", line.UomCode)
	}
	// 2 kg is 2000 g, and that is what will leave the warehouse.
	if line.QuantityStockMicro != 2_000_000_000 {
		t.Errorf("stock quantity = %d, want 2000000000 (2000 g)", line.QuantityStockMicro)
	}
}

// The conversion happens in CATALOG, which owns units — so the refusals 3.1 built reach a caller
// for the first time.
func TestSellingHalfOfSomethingIndivisibleIsRefused(t *testing.T) {
	f := newSellingFixture(t)
	document := f.draft(t, domain.Invoice)

	_, err := f.svc.AddLine(f.ctx, sales.AddLineInput{
		CompanyID: f.companyID, DocumentID: document.ID,
		VariantID: f.variant.ID, QuantityMicro: 500_000, // half a widget
	})
	if err == nil {
		t.Fatal("half of an indivisible unit was sold")
	}
	// The error comes from 3.1's domain, unchanged — the seam held.
	if code := errs.CodeOf(err); code != "catalog.fractional_not_allowed" {
		t.Errorf("code = %q, want catalog.fractional_not_allowed", code)
	}
}

// ── the lifecycle ───────────────────────────────────────────────────────────────

// §9.4: an abandoned draft must consume no number.
func TestADraftHasNoNumber(t *testing.T) {
	f := newSellingFixture(t)
	document := f.draft(t, domain.Invoice)

	if document.Number != "" {
		t.Errorf("a draft holds number %q", document.Number)
	}
	if document.Status != domain.Draft {
		t.Errorf("status = %q, want draft", document.Status)
	}
}

func TestACancelledDraftCannotBeChanged(t *testing.T) {
	f := newSellingFixture(t)
	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, 1_000_000)

	if err := f.svc.Cancel(f.ctx, document.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if _, err := f.svc.AddLine(f.ctx, sales.AddLineInput{
		CompanyID: f.companyID, DocumentID: document.ID,
		VariantID: f.variant.ID, QuantityMicro: 1_000_000,
	}); err == nil {
		t.Fatal("a line was added to a cancelled document")
	}
	if err := f.svc.Cancel(f.ctx, document.ID); err == nil {
		t.Fatal("a cancelled document was cancelled again")
	}
}

// §9.4's rule, kept by the SCHEMA rather than only by the allocator.
//
// The posting service (5.3) will be the only thing that numbers a document, but a bad import is
// not the posting service. This writes straight to the table — the shape 3.2, 3.3, 3.5, and 4.5
// each needed, and the fifth time this codebase has wanted it.
//
// Both halves matter: a draft holding a number means somebody consumed one and abandoned it, and
// a posted document without one means a posting skipped allocation entirely.
func TestTheDatabaseRefusesADraftWithANumberOrAPostedDocumentWithout(t *testing.T) {
	f := newSellingFixture(t)

	const insert = `
		INSERT INTO sales_documents (
			id, company_id, branch_id, document_type, status, document_number,
			document_date, currency_code, exchange_rate_micro,
			net_minor, tax_minor, discount_minor, total_minor, cost_minor,
			is_held, row_version, created_at, updated_at
		) VALUES (?, ?, ?, 'invoice', ?, ?, '2026-06-15', 'SYP', 1000000,
		          0, 0, 0, 0, 0, 0, 1, '2026-01-01', '2026-01-01')`

	// A draft that took a number.
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx, insert,
		"a", string(f.companyID), string(f.branchID), "draft", "INV-000001"); err == nil {
		t.Error("a draft was allowed to hold a number")
	}

	// A posted document that skipped allocation.
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx, insert,
		"b", string(f.companyID), string(f.branchID), "posted", nil); err == nil {
		t.Error("a posted document was allowed to have no number")
	}

	// And the two legitimate shapes are accepted.
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx, insert,
		"c", string(f.companyID), string(f.branchID), "draft", nil); err != nil {
		t.Errorf("an ordinary draft was refused: %v", err)
	}
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx, insert,
		"d", string(f.companyID), string(f.branchID), "posted", "INV-000001"); err != nil {
		t.Errorf("an ordinary posted document was refused: %v", err)
	}
}

// Two documents can never share a number (§9.4). The allocator prevents it; the index makes it
// unrepresentable.
func TestTheDatabaseRefusesADuplicateDocumentNumber(t *testing.T) {
	f := newSellingFixture(t)

	const insert = `
		INSERT INTO sales_documents (
			id, company_id, branch_id, document_type, status, document_number,
			document_date, currency_code, exchange_rate_micro,
			net_minor, tax_minor, discount_minor, total_minor, cost_minor,
			is_held, row_version, created_at, updated_at
		) VALUES (?, ?, ?, 'invoice', 'posted', 'INV-000001', '2026-06-15', 'SYP', 1000000,
		          0, 0, 0, 0, 0, 0, 1, '2026-01-01', '2026-01-01')`

	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx, insert,
		"first", string(f.companyID), string(f.branchID)); err != nil {
		t.Fatalf("the first invoice was refused: %v", err)
	}
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx, insert,
		"second", string(f.companyID), string(f.branchID)); err == nil {
		t.Fatal("two invoices shared a number")
	}
}

// ── held sales (§2.6) ───────────────────────────────────────────────────────────

// A held sale is the SAME document, parked. Modelling it as a different kind of thing would mean
// resuming had to convert one into the other, and a conversion is a place for a line to get lost.
func TestAHeldSaleKeepsItsLinesAndTakesNoNumber(t *testing.T) {
	f := newSellingFixture(t)
	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, 2_000_000)
	f.addLine(t, document.ID, 3_000_000)

	if err := f.svc.Hold(f.ctx, document.ID, "the man in the blue coat"); err != nil {
		t.Fatalf("Hold: %v", err)
	}

	held, lines, err := f.svc.Document(f.ctx, document.ID)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if !held.IsHeld || held.HoldLabel != "the man in the blue coat" {
		t.Errorf("document = %+v, want held with its label", held)
	}
	if held.Number != "" {
		t.Errorf("a held sale took number %q", held.Number)
	}
	if len(lines) != 2 {
		t.Errorf("%d lines survived the hold, want 2", len(lines))
	}

	if err = f.svc.Resume(f.ctx, document.ID); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	resumed, resumedLines, err := f.svc.Document(f.ctx, document.ID)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if resumed.IsHeld {
		t.Error("the sale is still held after being resumed")
	}
	if len(resumedLines) != 2 {
		t.Errorf("%d lines survived the resume, want 2", len(resumedLines))
	}
}

// ── lines ───────────────────────────────────────────────────────────────────────

func TestLinesAreNumberedInOrder(t *testing.T) {
	f := newSellingFixture(t)
	document := f.draft(t, domain.Invoice)

	first := f.addLine(t, document.ID, 1_000_000)
	second := f.addLine(t, document.ID, 2_000_000)

	if first.LineNumber != 1 || second.LineNumber != 2 {
		t.Errorf("line numbers = %d, %d — want 1, 2", first.LineNumber, second.LineNumber)
	}
}

func TestALineCanBeRemovedFromADraft(t *testing.T) {
	f := newSellingFixture(t)
	document := f.draft(t, domain.Invoice)
	first := f.addLine(t, document.ID, 1_000_000)
	f.addLine(t, document.ID, 2_000_000)

	if err := f.svc.RemoveLine(f.ctx, document.ID, first.ID); err != nil {
		t.Fatalf("RemoveLine: %v", err)
	}

	_, lines, err := f.svc.Document(f.ctx, document.ID)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if len(lines) != 1 || lines[0].LineNumber != 2 {
		t.Errorf("lines = %+v, want only the second", lines)
	}
}

// A line carries no money until posting resolves it. A caller that could name its own price is a
// till operator who can type a discount nobody approved.
func TestALineCarriesNoPriceUntilItIsPosted(t *testing.T) {
	f := newSellingFixture(t)
	document := f.draft(t, domain.Invoice)
	line := f.addLine(t, document.ID, 2_000_000)

	if line.UnitPriceMinor != 0 || line.NetMinor != 0 || line.TaxAmountMinor != 0 {
		t.Errorf("a drafted line already carries money: %+v", line)
	}
}

// ── reading ─────────────────────────────────────────────────────────────────────

func TestDocumentsCanBeListedByTypeAndStatus(t *testing.T) {
	f := newSellingFixture(t)
	f.draft(t, domain.Invoice)
	f.draft(t, domain.Quotation)

	invoices, err := f.svc.Documents(f.ctx, f.companyID, domain.Invoice, "")
	if err != nil {
		t.Fatalf("Documents: %v", err)
	}
	if len(invoices) != 1 {
		t.Errorf("%d invoices, want 1", len(invoices))
	}

	drafts, err := f.svc.Documents(f.ctx, f.companyID, "", domain.Draft)
	if err != nil {
		t.Fatalf("Documents: %v", err)
	}
	if len(drafts) != 2 {
		t.Errorf("%d drafts, want 2", len(drafts))
	}
}

func TestAnUnknownDocumentIsRefusedByName(t *testing.T) {
	f := newSellingFixture(t)

	_, _, err := f.svc.Document(f.ctx, id.ID("no-such-document"))
	if err == nil {
		t.Fatal("a document that does not exist was read")
	}
	if code := errs.CodeOf(err); code != sales.CodeUnknownDocument {
		t.Errorf("code = %q, want %q", code, sales.CodeUnknownDocument)
	}
}

// backdate moves a posted document's date, straight to the table.
//
// Posting stamps the date the caller drafted with, and there is no service method to change it —
// correctly, because a posted document's date is part of what was agreed. A period analysis
// needs two dates to have an ORDER at all, and the fixture dates everything the same day.
func (f fixture) backdate(t *testing.T, date string, documentID id.ID) {
	t.Helper()
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
		`UPDATE sales_documents SET document_date = ? WHERE id = ?`,
		date, string(documentID)); err != nil {
		t.Fatalf("backdating: %v", err)
	}
}
