package sales_test

import (
	"context"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/inventory"
	inventorydomain "github.com/mizan-erp/mizan/internal/modules/inventory/domain"
	"github.com/mizan-erp/mizan/internal/modules/sales"
	"github.com/mizan-erp/mizan/internal/modules/sales/domain"
)

// ── the fakes ───────────────────────────────────────────────────────────────────
//
// Each port is a few lines to fake, which is the whole reason they are narrow. Posting is the
// most complex transaction in the system, and its failure modes have to be reachable without
// standing up five modules.

type fixedPricing struct {
	priceMinor int64
	err        error
}

func (p fixedPricing) PriceFor(
	context.Context, sales.PriceQuery,
) (sales.ResolvedPrice, error) {
	if p.err != nil {
		return sales.ResolvedPrice{}, p.err
	}
	return sales.ResolvedPrice{
		UnitPriceMinor: p.priceMinor, ListCode: "RETAIL", Source: "default_list_variant",
	}, nil
}

type fixedTax struct{ rateMicro int64 }

func (t fixedTax) TaxFor(_ context.Context, q sales.TaxQuery) (sales.ResolvedTax, error) {
	amount := q.NetMinor * t.rateMicro / 1_000_000
	return sales.ResolvedTax{AmountMinor: amount, RateMicro: t.rateMicro, Code: "VAT"}, nil
}

type noCredit struct{ err error }

func (c noCredit) CheckCredit(context.Context, id.ID, id.ID, int64) error { return c.err }

// realStock wires the actual inventory service, so a posted sale really moves goods and the cost
// really comes from the costing port.
type realStock struct{ svc *inventory.Service }

func (s realStock) Issue(
	ctx context.Context, m sales.StockRequest,
) (sales.StockResult, error) {
	return s.move(ctx, m, inventorydomain.Issue)
}

func (s realStock) Return(
	ctx context.Context, m sales.StockRequest,
) (sales.StockResult, error) {
	return s.move(ctx, m, inventorydomain.ReturnIn)
}

func (s realStock) move(
	ctx context.Context, m sales.StockRequest, kind inventorydomain.Type,
) (sales.StockResult, error) {
	moved, err := s.svc.Move(ctx, inventory.MoveInput{
		CompanyID: m.CompanyID, WarehouseID: m.WarehouseID,
		ProductID: m.ProductID, VariantID: m.VariantID,
		Type: kind, QuantityMicro: m.QuantityMicro,
		SourceMovementID: m.SourceMovementID,
		DocumentType:     m.DocumentType, DocumentID: m.DocumentID,
		DocumentLineID: m.DocumentLineID, OccurredAt: m.OccurredAt,
	})
	if err != nil {
		return sales.StockResult{}, err
	}
	return sales.StockResult{
		MovementID: moved.ID, UnitCostMicro: moved.UnitCostMicro, ValueMinor: moved.ValueMinor,
	}, nil
}

// ── posting, end to end ─────────────────────────────────────────────────────────

// The step the whole phase builds towards: one transaction that resolves the price, computes the
// tax, issues the stock, records the cost, allocates the number, and fires the posting rules
// Phase 2 seeded and that have never run until now.
func TestPostingResolvesPriceTaxStockAndNumber(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 150_000})
	f.receive(t, 10_000_000, 60_000_000) // ten in stock at 60 apiece

	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, 2_000_000)

	posted, err := f.svc.Post(f.ctx, document.ID)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}

	if posted.Status != domain.Posted {
		t.Errorf("status = %q, want posted", posted.Status)
	}
	if posted.Number != "INV-000001" {
		t.Errorf("number = %q, want INV-000001", posted.Number)
	}
	// Two at 100 is 200 net; 15% is 30 tax; 230 total.
	if posted.NetMinor != 200 || posted.TaxMinor != 30 || posted.TotalMinor != 230 {
		t.Errorf("totals = %d/%d/%d, want 200/30/230",
			posted.NetMinor, posted.TaxMinor, posted.TotalMinor)
	}
	// Two at a cost of 60 is 120 — from the costing port, not from anything sales computed.
	if posted.CostMinor != 120 {
		t.Errorf("cost = %d, want 120", posted.CostMinor)
	}

	// The stock actually moved.
	state, err := f.inventory.StockOf(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("StockOf: %v", err)
	}
	if state.OnHandMicro != 8_000_000 {
		t.Errorf("on hand = %d, want 8000000", state.OnHandMicro)
	}
}

// §2.6 of Phase 3 requires resolution to record which list answered. This is where that answer
// outlives the resolution.
func TestAPostedLineRecordsWhichPriceListAnsweredAndWhichRate(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 150_000})
	f.receive(t, 10_000_000, 60_000_000)

	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, 2_000_000)
	if _, err := f.svc.Post(f.ctx, document.ID); err != nil {
		t.Fatalf("Post: %v", err)
	}

	_, lines, err := f.svc.Document(f.ctx, document.ID)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if lines[0].PriceListCode != "RETAIL" || lines[0].PriceSource == "" {
		t.Errorf("line = %+v, want the list and the reason recorded", lines[0])
	}
	// The RATE, not the group: a group's rate changes, and what this line was taxed at does not.
	if lines[0].TaxRateMicro != 150_000 {
		t.Errorf("rate = %d, want 150000", lines[0].TaxRateMicro)
	}
	// And what the goods cost us at the moment of sale.
	if lines[0].CostMicro != 60_000_000 {
		t.Errorf("cost = %d, want 60000000", lines[0].CostMicro)
	}
	if lines[0].MovementID.IsZero() {
		t.Error("the line does not name the stock movement it produced")
	}
}

// ── one transaction, or none of it ──────────────────────────────────────────────

// A document that did six of the eight things posting does is worse than one that did none. This
// is the failure most likely to happen in practice: the customer is over their limit, and the
// stock has already been issued by the time anybody finds out.
func TestAFailedCreditCheckRollsBackTheStockAndTheNumber(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 0})
	f.svc = f.withCredit(t, noCredit{err: errs.Conflict("partner.credit_limit_exceeded",
		"over the limit")})
	f.receive(t, 10_000_000, 60_000_000)

	// A NAMED customer: a walk-in has no record and therefore no limit to breach.
	document, err := f.svc.Draft(f.ctx, sales.NewDocumentInput{
		CompanyID: f.companyID, BranchID: f.branchID, WarehouseID: f.warehouseID,
		Type: domain.Invoice, Date: "2026-06-15", Currency: "SYP",
		PartnerID: f.partnerID, PartnerName: "Corner Shop",
	})
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}
	f.addLine(t, document.ID, 2_000_000)

	if _, postErr := f.svc.Post(f.ctx, document.ID); postErr == nil {
		t.Fatal("a sale posted past the customer's credit limit")
	}

	// The stock never left.
	state, err := f.inventory.StockOf(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("StockOf: %v", err)
	}
	if state.OnHandMicro != 10_000_000 {
		t.Errorf("on hand = %d — the stock was issued and not put back", state.OnHandMicro)
	}

	// The document is still a draft, and took no number.
	after, _, docErr := f.svc.Document(f.ctx, document.ID)
	if docErr != nil {
		t.Fatalf("Document: %v", docErr)
	}
	if after.Status != domain.Draft || after.Number != "" {
		t.Errorf("document = %+v, want an unnumbered draft", after)
	}

	// And the number series did not advance: the next successful sale gets 1.
	preview, previewErr := f.svc.PreviewNumber(f.ctx, f.branchID, sales.SeriesInvoice)
	if previewErr != nil {
		t.Fatalf("PreviewNumber: %v", previewErr)
	}
	if preview != "INV-000001" {
		t.Errorf("next number = %q — a failed posting consumed one", preview)
	}
}

// Phase 4's refusal, reaching a caller for the first time.
func TestSellingMoreThanIsInStockIsRefused(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 0})
	f.receive(t, 2_000_000, 60_000_000)

	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, 5_000_000)

	_, err := f.svc.Post(f.ctx, document.ID)
	if err == nil {
		t.Fatal("five were sold from a stock of two")
	}
	if code := errs.CodeOf(err); code != inventorydomain.CodeInsufficient {
		t.Errorf("code = %q, want %q", code, inventorydomain.CodeInsufficient)
	}

	after, _, err := f.svc.Document(f.ctx, document.ID)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if after.Status != domain.Draft {
		t.Error("a document that could not be stocked was posted anyway")
	}
}

// A price that cannot be resolved stops the sale rather than posting a zero.
func TestAnUnpricedItemCannotBeSold(t *testing.T) {
	f := newPostingFixture(t,
		fixedPricing{err: errs.NotFound("pricing.no_price", "nothing prices this")},
		fixedTax{rateMicro: 0})
	f.receive(t, 10_000_000, 60_000_000)

	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, 2_000_000)

	if _, err := f.svc.Post(f.ctx, document.ID); err == nil {
		t.Fatal("an unpriced item was sold")
	}

	state, err := f.inventory.StockOf(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("StockOf: %v", err)
	}
	if state.OnHandMicro != 10_000_000 {
		t.Errorf("on hand = %d — stock moved for a sale that failed", state.OnHandMicro)
	}
}

// ── immutability ────────────────────────────────────────────────────────────────

func TestAPostedDocumentCannotBePostedTwice(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 0})
	f.receive(t, 10_000_000, 60_000_000)

	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, 2_000_000)
	if _, err := f.svc.Post(f.ctx, document.ID); err != nil {
		t.Fatalf("Post: %v", err)
	}

	if _, err := f.svc.Post(f.ctx, document.ID); err == nil {
		t.Fatal("a posted document was posted a second time")
	}

	// Posting twice would issue the stock twice.
	state, err := f.inventory.StockOf(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("StockOf: %v", err)
	}
	if state.OnHandMicro != 8_000_000 {
		t.Errorf("on hand = %d, want 8000000 — the sale posted twice", state.OnHandMicro)
	}
}

func TestAPostedDocumentCannotTakeMoreLines(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 0})
	f.receive(t, 10_000_000, 60_000_000)

	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, 2_000_000)
	if _, err := f.svc.Post(f.ctx, document.ID); err != nil {
		t.Fatalf("Post: %v", err)
	}

	if _, err := f.svc.AddLine(f.ctx, sales.AddLineInput{
		CompanyID: f.companyID, DocumentID: document.ID,
		VariantID: f.variant.ID, QuantityMicro: 1_000_000,
	}); err == nil {
		t.Fatal("a line was added to a posted invoice")
	}
}

// ── numbering at posting (§9.4) ─────────────────────────────────────────────────

// An abandoned draft must consume no number, or a shop that starts and cancels twenty sales in a
// morning ends the day at invoice 400 having issued twenty.
func TestAbandonedDraftsConsumeNoNumbers(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 0})
	f.receive(t, 10_000_000, 60_000_000)

	// Three drafts started and abandoned.
	for i := 0; i < 3; i++ {
		abandoned := f.draft(t, domain.Invoice)
		f.addLine(t, abandoned.ID, 1_000_000)
		if err := f.svc.Cancel(f.ctx, abandoned.ID); err != nil {
			t.Fatalf("Cancel: %v", err)
		}
	}

	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, 1_000_000)
	posted, err := f.svc.Post(f.ctx, document.ID)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}

	if posted.Number != "INV-000001" {
		t.Errorf("number = %q after three abandoned drafts, want INV-000001", posted.Number)
	}
}

// ── what a quotation does not do ────────────────────────────────────────────────

// A quotation is a promise. Posting one must number it without touching stock.
func TestPostingAQuotationMovesNoStock(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 0})
	f.receive(t, 10_000_000, 60_000_000)

	quotation, err := f.svc.Draft(f.ctx, sales.NewDocumentInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		Type: domain.Quotation, Date: "2026-06-15", Currency: "SYP",
	})
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}
	f.addLine(t, quotation.ID, 2_000_000)

	if _, err = f.svc.Post(f.ctx, quotation.ID); err != nil {
		t.Fatalf("Post: %v", err)
	}

	state, err := f.inventory.StockOf(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("StockOf: %v", err)
	}
	if state.OnHandMicro != 10_000_000 {
		t.Errorf("on hand = %d — a quotation moved stock", state.OnHandMicro)
	}
}

// ── the books (Phase 2's rules, firing for the first time) ──────────────────────

// `sale_revenue` and `sale_cost` were seeded in Phase 2 and have never run. This is the test that
// runs them, and it asserts the whole chain: a sale in sales, an entry in accounting, with sales
// having named no account.
func TestPostingASaleWritesTheJournalEntryThroughPhase2sRules(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 150_000})
	books := f.booked(t)
	f.receive(t, 10_000_000, 60_000_000)

	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, 2_000_000)
	if _, err := f.svc.Post(f.ctx, document.ID); err != nil {
		t.Fatalf("Post: %v", err)
	}

	balances := f.balances(t, books)

	// `sale_revenue`: the customer owes the total; the net is revenue; the tax is owed on.
	if balances["1200"] != 230 {
		t.Errorf("receivables = %d, want 230", balances["1200"])
	}
	if balances["4100"] != -200 {
		t.Errorf("sales = %d, want -200 (a credit of 200)", balances["4100"])
	}
	if balances["2200"] != -30 {
		t.Errorf("tax payable = %d, want -30", balances["2200"])
	}

	// `sale_cost`: the goods leave stock and become an expense.
	if balances["5100"] != 120 {
		t.Errorf("cost of goods sold = %d, want 120", balances["5100"])
	}
	if balances["1300"] != -120 {
		t.Errorf("inventory = %d, want -120", balances["1300"])
	}
}

// The §20.3 guarantee, asserted the way Phase 4 asserted it: rewriting the RULE alone changes
// where the money lands, with no sales code involved.
func TestSalesNamesNoAccounts(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 0})
	books := f.booked(t)
	f.receive(t, 10_000_000, 60_000_000)

	// Revenue is redirected to a different account by editing one row.
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
		`UPDATE posting_rule_lines SET account_selector = 'mapping:ROUNDING_DIFF'
		  WHERE account_selector = 'mapping:SALES'`); err != nil {
		t.Fatalf("rewriting the rule: %v", err)
	}

	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, 2_000_000)
	if _, err := f.svc.Post(f.ctx, document.ID); err != nil {
		t.Fatalf("Post: %v", err)
	}

	balances := f.balances(t, books)
	if balances["4100"] != 0 {
		t.Errorf("sales holds %d, but the rule now says otherwise", balances["4100"])
	}
	if balances["5900"] != -200 {
		t.Errorf("the redirected account holds %d, want -200 — the rule alone decided this",
			balances["5900"])
	}
}

// A shop that does not track stock has no cost, and the `sale_cost` rule then posts nothing —
// which is exactly what Phase 2's "lines that resolve to zero are skipped" was designed for.
func TestASaleWithNoCostPostsNoCostEntry(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 0})
	books := f.booked(t)
	// Received at zero cost: a service, or stock nobody costed.
	f.receive(t, 10_000_000, 0)

	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, 2_000_000)
	if _, err := f.svc.Post(f.ctx, document.ID); err != nil {
		t.Fatalf("Post: %v", err)
	}

	balances := f.balances(t, books)
	if balances["4100"] != -200 {
		t.Errorf("sales = %d, want -200", balances["4100"])
	}
	// No cost, so no COGS entry — not a zero-value entry, but no entry at all.
	if balances["5100"] != 0 {
		t.Errorf("cost of goods sold = %d for a sale with no cost", balances["5100"])
	}
}

// The atomicity that matters most: goods gone with the books silent is the worst state this
// module can produce.
func TestASaleWhoseJournalEntryFailsMovesNoStock(t *testing.T) {
	f := newPostingFixture(t, fixedPricing{priceMinor: 100}, fixedTax{rateMicro: 150_000})
	books := f.booked(t)
	f.receive(t, 10_000_000, 60_000_000)

	// Remove the mapping the revenue rule needs.
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
		`DELETE FROM account_mappings WHERE mapping_key = 'SALES'`); err != nil {
		t.Fatalf("removing the mapping: %v", err)
	}

	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, 2_000_000)

	if _, err := f.svc.Post(f.ctx, document.ID); err == nil {
		t.Fatal("a sale posted with no account to book its revenue to")
	}

	state, err := f.inventory.StockOf(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("StockOf: %v", err)
	}
	if state.OnHandMicro != 10_000_000 {
		t.Errorf("on hand = %d — goods left with the books silent", state.OnHandMicro)
	}
	_ = books
}
