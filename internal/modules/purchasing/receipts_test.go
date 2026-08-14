package purchasing_test

import (
	"context"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/accounting"
	"github.com/mizan-erp/mizan/internal/modules/inventory"
	inventorydomain "github.com/mizan-erp/mizan/internal/modules/inventory/domain"
	"github.com/mizan-erp/mizan/internal/modules/purchasing"
	purchasingdomain "github.com/mizan-erp/mizan/internal/modules/purchasing/domain"
	"github.com/mizan-erp/mizan/internal/platform/config"
)

// realStock wires the ACTUAL inventory service, so a confirmed delivery really moves goods and
// really writes an inventory layer — which is the seam Phase 4 has had no reader for.
type realStock struct{ svc *inventory.Service }

func (s realStock) Receive(
	ctx context.Context, r purchasing.StockRequest,
) (purchasing.StockResult, error) {
	moved, err := s.svc.Move(ctx, inventory.MoveInput{
		CompanyID: r.CompanyID, WarehouseID: r.WarehouseID,
		ProductID: r.ProductID, VariantID: r.VariantID,
		Type: inventorydomain.Receipt, QuantityMicro: r.QuantityMicro,
		UnitCostMicro: r.UnitCostMicro,
		LotID:         r.LotID, SerialID: r.SerialID,
		DocumentType: r.DocumentType, DocumentID: r.DocumentID,
		DocumentLineID: r.DocumentLineID, OccurredAt: r.OccurredAt,
	})
	if err != nil {
		return purchasing.StockResult{}, err
	}
	return purchasing.StockResult{
		MovementID: moved.ID, UnitCostMicro: moved.UnitCostMicro, ValueMinor: moved.ValueMinor,
	}, nil
}

func (s realStock) ReturnToSupplier(
	ctx context.Context, r purchasing.StockRequest,
) (purchasing.StockResult, error) {
	moved, err := s.svc.Move(ctx, inventory.MoveInput{
		CompanyID: r.CompanyID, WarehouseID: r.WarehouseID,
		ProductID: r.ProductID, VariantID: r.VariantID,
		Type: inventorydomain.ReturnOut, QuantityMicro: r.QuantityMicro,
		LotID: r.LotID, SerialID: r.SerialID,
		DocumentType: r.DocumentType, DocumentID: r.DocumentID,
		DocumentLineID: r.DocumentLineID, OccurredAt: r.OccurredAt,
		SourceMovementID: r.SourceMovementID,
	})
	if err != nil {
		return purchasing.StockResult{}, err
	}
	return purchasing.StockResult{
		MovementID: moved.ID, UnitCostMicro: moved.UnitCostMicro, ValueMinor: moved.ValueMinor,
	}, nil
}

func (s realStock) Revalue(ctx context.Context, r purchasing.RevaluationRequest) error {
	_, err := s.svc.RevalueBy(ctx, inventory.RevalueInput{
		CompanyID: r.CompanyID, WarehouseID: r.WarehouseID,
		ProductID: r.ProductID, VariantID: r.VariantID,
		DeltaMinor: r.DeltaMinor, Decimals: r.Decimals,
		DocumentType: r.DocumentType, DocumentID: r.DocumentID, OccurredAt: r.OccurredAt,
	})
	return err
}

func (s realStock) OnHandMicro(
	ctx context.Context, variantID, warehouseID id.ID,
) (int64, error) {
	state, err := s.svc.StockOf(ctx, variantID, warehouseID)
	if err != nil {
		return 0, err
	}
	return state.OnHandMicro, nil
}

// ── a delivery is what actually arrived ─────────────────────────────────────────

func TestConfirmingADeliveryMovesStockAndAccruesGRNI(t *testing.T) {
	// Criterion 4. When goods arrive and no invoice has, the business genuinely holds an asset
	// and genuinely owes somebody for it — both facts are true before the invoice exists.
	f := newStockedFixture(t)
	books := f.booked(t)

	orderID := f.draft(t)
	f.addLine(t, orderID, f.variant.ID, 10_000_000) // ten widgets at 10.00
	if _, err := f.svc.Place(f.ctx, orderID); err != nil {
		t.Fatalf("Place: %v", err)
	}
	_, orderLines, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}

	receiptID := f.draftReceipt(t, orderID)
	f.receiveLine(t, receiptID, orderLines[0].ID, 10_000_000)

	confirmed, err := f.svc.ConfirmReceipt(f.ctx, receiptID)
	if err != nil {
		t.Fatalf("ConfirmReceipt: %v", err)
	}
	if confirmed.Number == "" {
		t.Fatal("a confirmed delivery took no number")
	}
	// Ten at 10.00 is 100.00 — 10,000 minor units.
	if confirmed.ValueMinor != 10_000 {
		t.Fatalf("value = %d, want 10000", confirmed.ValueMinor)
	}

	// The goods are on the shelf.
	level, err := f.inventory.StockOf(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("StockOf: %v", err)
	}
	if level.OnHandMicro != 10_000_000 {
		t.Errorf("on hand = %d, want 10000000", level.OnHandMicro)
	}

	// And the books say so: inventory up, GRNI accrued.
	balances := f.balances(t, books)
	if balances["1300"] != 10_000 {
		t.Errorf("inventory = %d, want 10000", balances["1300"])
	}
	if balances["2150"] != -10_000 {
		t.Errorf("GRNI = %d, want -10000 (a credit)", balances["2150"])
	}
	// No tax yet. Tax arrives with the INVOICE, and accruing it before the supplier has claimed
	// it would put a recoverable asset on the books that no document supports.
	//
	// # This assertion pins a PAIR, and no single mutation can break it
	//
	// Two things keep it: the service publishes no tax amount, and the seeded rule has no tax
	// line. Drills broke each alone and neither changed anything — because Phase 2 SKIPS a rule
	// line that resolves to zero, so a tax line with no tax behind it is indistinguishable from
	// no tax line at all.
	//
	// That is not a gap in the test; it is the two layers being individually inert and jointly
	// load-bearing. No extra test was written, because one that could fail on a single-layer
	// mutation would have to assert something neither layer actually promises (4.3s rule).
	if balances["1400"] != 0 {
		t.Errorf("recoverable tax = %d, want 0 — a receipt is not an invoice", balances["1400"])
	}
	// And nothing is payable yet: we owe for the goods, but no supplier has invoiced us.
	if balances["2100"] != 0 {
		t.Errorf("accounts payable = %d, want 0", balances["2100"])
	}
}

func TestOneOrderCanBeReceivedInSeveralDeliveries(t *testing.T) {
	// Criterion 2. One order in three shipments is the ordinary case, and the outstanding figure
	// must always be what was ordered less what has actually arrived.
	f := newStockedFixture(t)

	orderID := f.draft(t)
	f.addLine(t, orderID, f.variant.ID, 100_000_000)
	if _, err := f.svc.Place(f.ctx, orderID); err != nil {
		t.Fatalf("Place: %v", err)
	}
	_, orderLines, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	lineID := orderLines[0].ID

	for _, arriving := range []int64{60_000_000, 30_000_000, 10_000_000} {
		receiptID := f.draftReceipt(t, orderID)
		f.receiveLine(t, receiptID, lineID, arriving)
		if _, err = f.svc.ConfirmReceipt(f.ctx, receiptID); err != nil {
			t.Fatalf("ConfirmReceipt(%d): %v", arriving, err)
		}
	}

	_, after, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	if after[0].ReceivedMicro != 100_000_000 {
		t.Errorf("received = %d, want 100000000", after[0].ReceivedMicro)
	}
	if after[0].OutstandingMicro() != 0 {
		t.Errorf("outstanding = %d, want 0", after[0].OutstandingMicro())
	}
	if !after[0].IsFullyReceived() {
		t.Error("the line does not report itself fully received")
	}

	// And the maintained figure agrees with the deliveries that justify it.
	discrepancies, err := f.svc.VerifyReceived(f.ctx, orderID)
	if err != nil {
		t.Fatalf("VerifyReceived: %v", err)
	}
	if len(discrepancies) != 0 {
		t.Errorf("the received projection drifted: %+v", discrepancies)
	}
}

// TestADraftDeliveryCountsForNothing
//
// Somebody keying tomorrow's delivery note this afternoon has not received anything yet.
//
// If the recomputation counted drafts it would disagree with the maintained figure — which is
// correct — and report a discrepancy on every order with a delivery in progress. A verifier that
// cries wolf is one nobody reads, and that is how the real drift gets missed.
func TestADraftDeliveryCountsForNothing(t *testing.T) {
	f := newStockedFixture(t)

	orderID := f.draft(t)
	f.addLine(t, orderID, f.variant.ID, 100_000_000)
	if _, err := f.svc.Place(f.ctx, orderID); err != nil {
		t.Fatalf("Place: %v", err)
	}
	_, orderLines, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	lineID := orderLines[0].ID

	// Forty arrive and are confirmed.
	confirmedReceipt := f.draftReceipt(t, orderID)
	f.receiveLine(t, confirmedReceipt, lineID, 40_000_000)
	if _, err = f.svc.ConfirmReceipt(f.ctx, confirmedReceipt); err != nil {
		t.Fatalf("ConfirmReceipt: %v", err)
	}

	// Twenty-five more are being keyed and have NOT been confirmed.
	pending := f.draftReceipt(t, orderID)
	f.receiveLine(t, pending, lineID, 25_000_000)

	_, after, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	if after[0].ReceivedMicro != 40_000_000 {
		t.Fatalf("received = %d, want 40000000 — a draft was counted", after[0].ReceivedMicro)
	}

	discrepancies, err := f.svc.VerifyReceived(f.ctx, orderID)
	if err != nil {
		t.Fatalf("VerifyReceived: %v", err)
	}
	if len(discrepancies) != 0 {
		t.Errorf("the verifier counted a draft delivery: %+v", discrepancies)
	}
}

func TestTheReceivedProjectionIsCheckedNotRepaired(t *testing.T) {
	// 4.3's discipline, applied to a second projection. A projection that silently heals hides
	// the bug that broke it — and the next thing it hides is the one that mattered.
	f := newStockedFixture(t)

	orderID := f.draft(t)
	f.addLine(t, orderID, f.variant.ID, 50_000_000)
	if _, err := f.svc.Place(f.ctx, orderID); err != nil {
		t.Fatalf("Place: %v", err)
	}
	_, orderLines, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}

	receiptID := f.draftReceipt(t, orderID)
	f.receiveLine(t, receiptID, orderLines[0].ID, 20_000_000)
	if _, err = f.svc.ConfirmReceipt(f.ctx, receiptID); err != nil {
		t.Fatalf("ConfirmReceipt: %v", err)
	}

	// Corrupt the projection behind the service's back.
	if _, err = f.store.Writer(f.ctx).ExecContext(f.ctx,
		`UPDATE purchase_order_lines SET received_micro = 999000000 WHERE id = ?`,
		string(orderLines[0].ID)); err != nil {
		t.Fatalf("corrupting: %v", err)
	}

	discrepancies, err := f.svc.VerifyReceived(f.ctx, orderID)
	if err != nil {
		t.Fatalf("VerifyReceived: %v", err)
	}
	if len(discrepancies) != 1 {
		t.Fatalf("found %d discrepancies, want 1", len(discrepancies))
	}
	if discrepancies[0].Recorded != 999_000_000 || discrepancies[0].Recomputed != 20_000_000 {
		t.Errorf("discrepancy = %+v, want recorded 999000000 recomputed 20000000",
			discrepancies[0])
	}

	// And it did NOT repair it.
	_, after, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	if after[0].ReceivedMicro != 999_000_000 {
		t.Error("the verifier repaired the projection instead of reporting it")
	}
}

// ── the over-receipt policy ─────────────────────────────────────────────────────

func TestADeliveryBeyondToleranceIsRefusedAtEntry(t *testing.T) {
	// Criterion 3. Refused where somebody is standing at a loading bay with a delivery note, so
	// they can count again or telephone the supplier — not after twenty lines have been keyed.
	f := newStockedFixture(t)

	orderID := f.draft(t)
	f.addLine(t, orderID, f.variant.ID, 100_000_000)
	if _, err := f.svc.Place(f.ctx, orderID); err != nil {
		t.Fatalf("Place: %v", err)
	}
	_, orderLines, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}

	receiptID := f.draftReceipt(t, orderID)
	_, err = f.svc.ReceiveLine(f.ctx, purchasing.ReceiveLineInput{
		CompanyID: f.companyID, ReceiptID: receiptID, OrderLineID: orderLines[0].ID,
		QuantityMicro: 140_000_000,
	})
	if code := errs.CodeOf(err); code != purchasingdomain.CodeOverReceipt {
		t.Fatalf("code = %q, want %q", code, purchasingdomain.CodeOverReceipt)
	}

	// Nothing was written: the refusal happened before the line existed.
	_, lines, err := f.svc.Receipt(f.ctx, receiptID)
	if err != nil {
		t.Fatalf("Receipt: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("%d lines survived the refusal", len(lines))
	}
}

func TestTheToleranceIsASettingAShopCanRaise(t *testing.T) {
	// A fastener wholesaler expects tolerance and a pharmacy does not. The default is the strict
	// reading, so a business that wants latitude asks for it rather than discovering it.
	f := newStockedFixture(t)

	orderID := f.draft(t)
	f.addLine(t, orderID, f.variant.ID, 100_000_000)
	if _, err := f.svc.Place(f.ctx, orderID); err != nil {
		t.Fatalf("Place: %v", err)
	}
	_, orderLines, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}

	// Two over a hundred: refused at the default of zero.
	receiptID := f.draftReceipt(t, orderID)
	if _, err = f.svc.ReceiveLine(f.ctx, purchasing.ReceiveLineInput{
		CompanyID: f.companyID, ReceiptID: receiptID, OrderLineID: orderLines[0].ID,
		QuantityMicro: 102_000_000,
	}); err == nil {
		t.Fatal("a 2% over-delivery passed the default zero tolerance")
	}

	// The shop declares a 2% allowance.
	if err = f.settings.Set(f.ctx, config.ScopeCompany, f.companyID,
		purchasing.OverReceiptTolerance.Key(), int64(20_000)); err != nil {
		t.Fatalf("setting the tolerance: %v", err)
	}

	if _, err = f.svc.ReceiveLine(f.ctx, purchasing.ReceiveLineInput{
		CompanyID: f.companyID, ReceiptID: receiptID, OrderLineID: orderLines[0].ID,
		QuantityMicro: 102_000_000,
	}); err != nil {
		t.Fatalf("a 2%% over-delivery was refused under a 2%% tolerance: %v", err)
	}
}

// ── deliveries against no order ─────────────────────────────────────────────────

func TestGoodsCanArriveAgainstNoOrder(t *testing.T) {
	// A replacement for damaged stock, or a cash-and-carry purchase. A real delivery with a real
	// cost, and the three-way match simply has one fewer side.
	f := newStockedFixture(t)

	receiptID := f.draftReceipt(t, id.ID(""))
	if _, err := f.svc.ReceiveLine(f.ctx, purchasing.ReceiveLineInput{
		CompanyID: f.companyID, ReceiptID: receiptID,
		VariantID: f.variant.ID, QuantityMicro: 5_000_000,
	}); err != nil {
		t.Fatalf("ReceiveLine: %v", err)
	}

	if _, err := f.svc.ConfirmReceipt(f.ctx, receiptID); err != nil {
		t.Fatalf("ConfirmReceipt: %v", err)
	}

	level, err := f.inventory.StockOf(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("StockOf: %v", err)
	}
	if level.OnHandMicro != 5_000_000 {
		t.Errorf("on hand = %d, want 5000000", level.OnHandMicro)
	}
}

func TestADeliveryWithNoOrderIsStillValued(t *testing.T) {
	// # The defect this test was written for
	//
	// A receipt against no order took its cost from the order line it did not have, so the cost
	// was zero. Goods from a cash-and-carry entered stock worth NOTHING — which understates
	// inventory and overstates margin the day they are sold, and surfaces months later as a
	// gross profit nobody can explain. The posting rules made it invisible: Phase 2 skips lines
	// that resolve to zero, so the whole journal entry simply did not appear.
	//
	// It was found because a §20.3 test asserted a redirected account received something, and
	// nothing had been posted at all.
	f := newStockedFixture(t)
	books := f.booked(t)

	receiptID := f.draftReceipt(t, id.ID(""))
	if _, err := f.svc.ReceiveLine(f.ctx, purchasing.ReceiveLineInput{
		CompanyID: f.companyID, ReceiptID: receiptID,
		VariantID: f.variant.ID, QuantityMicro: 5_000_000,
	}); err != nil {
		t.Fatalf("ReceiveLine: %v", err)
	}
	confirmed, err := f.svc.ConfirmReceipt(f.ctx, receiptID)
	if err != nil {
		t.Fatalf("ConfirmReceipt: %v", err)
	}

	// Five at the purchase list's 10.00: 50.00.
	if confirmed.ValueMinor != 5_000 {
		t.Fatalf("value = %d, want 5000 — the delivery entered stock unvalued",
			confirmed.ValueMinor)
	}
	balances := f.balances(t, books)
	if balances["1300"] != 5_000 {
		t.Errorf("inventory = %d, want 5000", balances["1300"])
	}
	if balances["2150"] != -5_000 {
		t.Errorf("GRNI = %d, want -5000", balances["2150"])
	}
}

func TestGoodsThatWereGenuinelyFreeCanBeEnteredAtZero(t *testing.T) {
	// nil and zero are different answers and both are legitimate: a warranty replacement or a
	// sample really did cost nothing. An EXPLICIT zero says so; an omitted price does not.
	f := newStockedFixture(t)

	free := int64(0)
	receiptID := f.draftReceipt(t, id.ID(""))
	if _, err := f.svc.ReceiveLine(f.ctx, purchasing.ReceiveLineInput{
		CompanyID: f.companyID, ReceiptID: receiptID,
		VariantID: f.variant.ID, QuantityMicro: 2_000_000, UnitCostMicro: &free,
	}); err != nil {
		t.Fatalf("ReceiveLine: %v", err)
	}
	confirmed, err := f.svc.ConfirmReceipt(f.ctx, receiptID)
	if err != nil {
		t.Fatalf("ConfirmReceipt: %v", err)
	}
	if confirmed.ValueMinor != 0 {
		t.Fatalf("value = %d, want 0 — an explicit zero was overruled", confirmed.ValueMinor)
	}

	// The goods are still on the shelf. Free stock is stock.
	level, err := f.inventory.StockOf(f.ctx, f.variant.ID, f.warehouseID)
	if err != nil {
		t.Fatalf("StockOf: %v", err)
	}
	if level.OnHandMicro != 2_000_000 {
		t.Errorf("on hand = %d, want 2000000", level.OnHandMicro)
	}
}

func TestGoodsCannotArriveAgainstAnUnplacedOrder(t *testing.T) {
	// A draft has not been sent, so nothing can have been delivered against it. Attaching goods
	// to it would make the order's own history fiction.
	f := newStockedFixture(t)
	orderID := f.draft(t)

	_, err := f.svc.DraftReceipt(f.ctx, purchasing.NewReceiptInput{
		CompanyID: f.companyID, BranchID: f.branchID, WarehouseID: f.warehouseID,
		OrderID: orderID, PartnerID: f.partnerID, PartnerName: "Acme Supplies",
		ReceiptDate: "2026-08-14", Currency: "SAR",
	})
	if code := errs.CodeOf(err); code != purchasingdomain.CodeNotDraft {
		t.Fatalf("code = %q, want %q", code, purchasingdomain.CodeNotDraft)
	}
}

// ── the transaction ─────────────────────────────────────────────────────────────

func TestADeliveryWhoseStockMovementFailsMovesNothingAndTakesNoNumber(t *testing.T) {
	// Five things happen in one transaction: the movement, the movement links, the received
	// figures, the number, and the journal entry. Goods on a shelf with no entry behind them,
	// or an entry for goods nobody received, are both worse than a failure.
	f := newStockedFixture(t)

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

	broken := purchasing.NewService(f.store, purchasing.Options{
		Bus: f.bus, Catalog: realCatalog{svc: f.catalog},
		Pricing: fixedPricing{priceMicro: 10_000_000}, Tax: fixedTax{},
		Stock:   failingStock{},
		Numbers: platformNumbering{alloc: allocatorFor(f)},
	})
	if _, err = broken.ConfirmReceipt(f.ctx, receiptID); err == nil {
		t.Fatal("a delivery confirmed with a broken stock port")
	}

	receipt, _, err := f.svc.Receipt(f.ctx, receiptID)
	if err != nil {
		t.Fatalf("Receipt: %v", err)
	}
	if receipt.Status != purchasingdomain.ReceiptDraft {
		t.Errorf("status = %q, want draft", receipt.Status)
	}
	if receipt.Number != "" {
		t.Errorf("a failed confirmation consumed number %q", receipt.Number)
	}

	// The order's received figure did not move either.
	_, after, err := f.svc.Order(f.ctx, orderID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	if after[0].ReceivedMicro != 0 {
		t.Errorf("received = %d after a failed delivery, want 0", after[0].ReceivedMicro)
	}
}

// ── §20.3 ───────────────────────────────────────────────────────────────────────

func TestPurchasingNamesNoAccounts(t *testing.T) {
	// The same guarantee sales asserts: rewriting the RULE alone changes where the money lands,
	// with no purchasing code involved. It is why "a business that accrues GRNI" and "one that
	// does not" is a seed file rather than a branch in the service.
	f := newStockedFixture(t)
	books := f.booked(t)

	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
		`UPDATE posting_rule_lines SET account_selector = 'mapping:ROUNDING_DIFF'
		  WHERE account_selector = 'mapping:GRNI'`); err != nil {
		t.Fatalf("rewriting the rule: %v", err)
	}

	receiptID := f.draftReceipt(t, id.ID(""))
	if _, err := f.svc.ReceiveLine(f.ctx, purchasing.ReceiveLineInput{
		CompanyID: f.companyID, ReceiptID: receiptID,
		VariantID: f.variant.ID, QuantityMicro: 5_000_000,
	}); err != nil {
		t.Fatalf("ReceiveLine: %v", err)
	}
	if _, err := f.svc.ConfirmReceipt(f.ctx, receiptID); err != nil {
		t.Fatalf("ConfirmReceipt: %v", err)
	}

	balances := f.balances(t, books)
	if balances["2150"] != 0 {
		t.Errorf("GRNI = %d, want 0 — the rewritten rule was ignored", balances["2150"])
	}
	if balances["5900"] == 0 {
		t.Error("the redirected account received nothing")
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────────

type failingStock struct{}

func (failingStock) Receive(
	context.Context, purchasing.StockRequest,
) (purchasing.StockResult, error) {
	return purchasing.StockResult{}, errs.Internal("test.stock_down", "the warehouse is down")
}

func (failingStock) ReturnToSupplier(
	context.Context, purchasing.StockRequest,
) (purchasing.StockResult, error) {
	return purchasing.StockResult{}, nil
}

func (failingStock) Revalue(context.Context, purchasing.RevaluationRequest) error { return nil }

func (failingStock) OnHandMicro(context.Context, id.ID, id.ID) (int64, error) { return 0, nil }

func (f fixture) booked(t *testing.T) *accounting.Service {
	t.Helper()
	svc, err := accounting.NewService(f.store, accounting.Options{
		Clock: clock.System(), Bus: f.bus,
	})
	if err != nil {
		t.Fatalf("accounting.NewService: %v", err)
	}
	if err = accounting.NewModule(svc).Subscribe(f.bus, nil); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err = svc.ApplyChart(f.ctx, f.companyID, "generic_trading"); err != nil {
		t.Fatalf("ApplyChart: %v", err)
	}
	if err = svc.ApplyRules(f.ctx, f.companyID, "generic_trading"); err != nil {
		t.Fatalf("ApplyRules: %v", err)
	}
	return svc
}

// balances reads every account's net movement, keyed by code.
//
// Net movement rather than Closing, which is cumulative: summing Closing across periods counts
// each earlier one again for every later one (the 4.3/5.3 lesson).
func (f fixture) balances(t *testing.T, books *accounting.Service) map[string]int64 {
	t.Helper()
	years, err := books.Years(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Years: %v", err)
	}
	if len(years) == 0 {
		t.Fatal("the company has no fiscal year")
	}
	periods, err := books.Periods(f.ctx, years[0].ID)
	if err != nil {
		t.Fatalf("Periods: %v", err)
	}

	out := map[string]int64{}
	for _, period := range periods {
		rows, tbErr := books.TrialBalance(f.ctx, f.companyID, period.ID)
		if tbErr != nil {
			t.Fatalf("TrialBalance: %v", tbErr)
		}
		for _, row := range rows {
			out[row.AccountCode] += row.Debit - row.Credit
		}
	}
	return out
}
