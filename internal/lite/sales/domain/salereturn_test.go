package domain_test

import (
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	domain "github.com/mizan-erp/mizan/internal/lite/sales/domain"
)

// A sale of three tins at 1,000 pounds each with 10% off the line: gross 3,000, discount 300, net 2,700. In dollars at
// a rate of 15,000 the same line is 0.20 net. Each tin cost the shop 0.05.
func returnedSale(t *testing.T) (domain.Sale, id.ID) {
	t.Helper()
	saleID, _ := id.New()
	lineID, _ := id.New()
	productID, _ := id.New()
	return domain.Sale{
		ID: saleID, ReceiptNo: 1042, BusinessDate: "2026-09-14", Status: domain.StatusPosted,
		Payment: domain.PaymentCash, LocalCurrency: "SYP", SettlementCurrency: "SYP", RateNano: 15_000_000_000_000,
		TotalMinor: 2_700,
		Lines: []domain.Line{{
			ID: lineID, LineNo: 1, ProductID: productID, NameAR: "معجون بندورة", UnitCode: "pcs",
			QuantityMicro: 3_000_000, PriceCurrency: "SYP", UnitPriceMicro: 1_000_000_000,
			GrossLocalMinor: 3_000, GrossUSDMinor: 20, DiscountPercentMicro: 10_000_000,
			DiscountLocalMinor: 300, DiscountUSDMinor: 2,
			UnitCostMicro: 50_000, CostKnown: true, CostUSDMinor: 15, CostLocalMinor: 2_250,
		}},
	}, lineID
}

func returnUnits() map[string]int { return map[string]int{"pcs": 0, "kg": 3} }

func returnedAt() time.Time { return time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC) }

func priceOne(t *testing.T, sale domain.Sale, already domain.Returned, d domain.ReturnDraft) (domain.Return, error) {
	t.Helper()
	rateID, _ := id.New()
	return domain.PriceReturn(sale, already, d, domain.Rate{ID: rateID, Nano: 15_000_000_000_000},
		syp, usd, returnUnits(), returnedAt(), "2026-09-17")
}

// TestOneTinOfThreeComesBackAtItsDiscountedShare is the arithmetic the whole feature rests on: a customer who bought
// three at 10% off and brings one back is owed a third of what the line was CHARGED, not a third of the list price.
func TestOneTinOfThreeComesBackAtItsDiscountedShare(t *testing.T) {
	sale, line := returnedSale(t)
	got, err := priceOne(t, sale, nil, domain.ReturnDraft{
		Lines:      []domain.ReturnLineDraft{{SaleLineID: line, Quantity: "1", Restock: true}},
		Settlement: domain.SettleCash, Reason: "العلبة منتفخة",
	})
	if err != nil {
		t.Fatal(err)
	}
	// A third of 2,700 net, not a third of the 3,000 gross.
	if got.RefundLocalMinor != 900 {
		t.Fatalf("refund = %d pounds, want 900 — a third of the DISCOUNTED line", got.RefundLocalMinor)
	}
	if got.RefundMinor != 900 {
		t.Fatalf("the refund in the settlement currency = %d", got.RefundMinor)
	}
	// And the cost goes back with it, so the day loses the margin and not the whole price.
	if got.CostLocalMinor != 750 || got.CostUSDMinor != 5 {
		t.Fatalf("cost re-credited = %d local, %d usd", got.CostLocalMinor, got.CostUSDMinor)
	}
	if !got.CostKnown || len(got.Lines) != 1 || !got.Lines[0].Restocked {
		t.Fatalf("return = %+v", got)
	}
	// The line says what is still out there, so a person can see what is left to bring back.
	if got.Lines[0].SoldMicro != 3_000_000 || got.Lines[0].AlreadyReturnedMicro != 0 {
		t.Fatalf("line = %+v", got.Lines[0])
	}
}

// TestTheWholeLineBackIsWorthExactlyWhatWasCharged: no division, so no remainder can be lost or invented.
func TestTheWholeLineBackIsWorthExactlyWhatWasCharged(t *testing.T) {
	sale, line := returnedSale(t)
	got, err := priceOne(t, sale, nil, domain.ReturnDraft{
		Lines:      []domain.ReturnLineDraft{{SaleLineID: line, Quantity: "3", Restock: true}},
		Settlement: domain.SettleCash, Reason: "رجّع كل شي",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.RefundLocalMinor != 2_700 || got.RefundUSDMinor != 18 {
		t.Fatalf("a whole line back = %d local, %d usd — want exactly the line's net", got.RefundLocalMinor, got.RefundUSDMinor)
	}
	if got.CostLocalMinor != 2_250 || got.CostUSDMinor != 15 {
		t.Fatalf("a whole line's cost = %d local, %d usd", got.CostLocalMinor, got.CostUSDMinor)
	}
}

// TestTwoReturnsOfOneLineNeverGiveBackMoreThanWasSold: the customer brings one tin back today and another next week.
// The second return is priced against what is LEFT, and a third attempt for two tins is refused.
func TestTwoReturnsOfOneLineNeverGiveBackMoreThanWasSold(t *testing.T) {
	sale, line := returnedSale(t)
	first, err := priceOne(t, sale, nil, domain.ReturnDraft{
		Lines:      []domain.ReturnLineDraft{{SaleLineID: line, Quantity: "1", Restock: true}},
		Settlement: domain.SettleCash, Reason: "واحدة",
	})
	if err != nil {
		t.Fatal(err)
	}
	already := domain.Returned{line: first.Lines[0].QuantityMicro}

	second, err := priceOne(t, sale, already, domain.ReturnDraft{
		Lines:      []domain.ReturnLineDraft{{SaleLineID: line, Quantity: "1", Restock: true}},
		Settlement: domain.SettleCash, Reason: "تانية",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Lines[0].AlreadyReturnedMicro != 1_000_000 {
		t.Fatalf("the second return does not know about the first: %+v", second.Lines[0])
	}
	// Two thirds given back over two returns: 900 + 900, never more than the 2,700 charged.
	if first.RefundLocalMinor+second.RefundLocalMinor != 1_800 {
		t.Fatalf("two returns gave back %d", first.RefundLocalMinor+second.RefundLocalMinor)
	}

	already[line] = 2_000_000
	if _, tooMuch := priceOne(t, sale, already, domain.ReturnDraft{
		Lines:      []domain.ReturnLineDraft{{SaleLineID: line, Quantity: "2", Restock: true}},
		Settlement: domain.SettleCash, Reason: "أكتر من اللازم",
	}); errs.CodeOf(tooMuch) != domain.CodeReturnTooMuch {
		t.Fatalf("returning more than was sold = %v", tooMuch)
	}
}

// TestAReturnIsRefusedWhenItMakesNoSense covers the ways a person can ask for one that cannot exist.
func TestAReturnIsRefusedWhenItMakesNoSense(t *testing.T) {
	sale, line := returnedSale(t)
	stranger, _ := id.New()

	for name, tc := range map[string]struct {
		draft domain.ReturnDraft
		want  string
	}{
		"nothing ticked": {
			domain.ReturnDraft{Settlement: domain.SettleCash, Reason: "لا شي"}, domain.CodeReturnNoLines},
		"no reason": {
			domain.ReturnDraft{Lines: []domain.ReturnLineDraft{{SaleLineID: line, Quantity: "1"}}, Settlement: domain.SettleCash},
			domain.CodeReturnReasonRequired},
		"a line from another sale": {
			domain.ReturnDraft{Lines: []domain.ReturnLineDraft{{SaleLineID: stranger, Quantity: "1"}},
				Settlement: domain.SettleCash, Reason: "غلط"}, domain.CodeReturnNotThatLine},
		"more than was sold": {
			domain.ReturnDraft{Lines: []domain.ReturnLineDraft{{SaleLineID: line, Quantity: "4"}},
				Settlement: domain.SettleCash, Reason: "أربعة"}, domain.CodeReturnTooMuch},
		"to the debt of a cash sale": {
			domain.ReturnDraft{Lines: []domain.ReturnLineDraft{{SaleLineID: line, Quantity: "1"}},
				Settlement: domain.SettleDebt, Reason: "على الدين"}, domain.CodeReturnNotCredit},
	} {
		if _, err := priceOne(t, sale, nil, tc.draft); errs.CodeOf(err) != tc.want {
			t.Errorf("%s = %v, want %s", name, err, tc.want)
		}
	}

	// A voided sale has nothing to return: the whole thing was already handed back.
	voided := sale
	voided.Status = domain.StatusVoided
	if _, err := priceOne(t, voided, nil, domain.ReturnDraft{
		Lines:      []domain.ReturnLineDraft{{SaleLineID: line, Quantity: "1"}},
		Settlement: domain.SettleCash, Reason: "ملغاة",
	}); errs.CodeOf(err) != domain.CodeReturnVoided {
		t.Fatalf("returning against a voided sale = %v", err)
	}
}

// TestALineWhoseCostWasNeverKnownRecreditsNothingAndSaysSo: guessing zero would quietly report the whole refund as
// profit lost, which is a different and wrong story.
func TestALineWhoseCostWasNeverKnownRecreditsNothingAndSaysSo(t *testing.T) {
	sale, line := returnedSale(t)
	sale.Lines[0].CostKnown = false
	sale.Lines[0].UnitCostMicro, sale.Lines[0].CostUSDMinor, sale.Lines[0].CostLocalMinor = 0, 0, 0

	got, err := priceOne(t, sale, nil, domain.ReturnDraft{
		Lines:      []domain.ReturnLineDraft{{SaleLineID: line, Quantity: "1", Restock: true}},
		Settlement: domain.SettleCash, Reason: "بدون كلفة",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.CostKnown || got.CostLocalMinor != 0 || got.CostUSDMinor != 0 {
		t.Fatalf("an unknown cost was guessed: %+v", got)
	}
	// The refund is unaffected — what the customer paid is known even when what the shop paid is not.
	if got.RefundLocalMinor != 900 {
		t.Fatalf("refund = %d", got.RefundLocalMinor)
	}
}

// TestAWeighedLineReturnsPartOfItsWeight: a customer brings back 0.5 kg of the 2 kg they bought.
func TestAWeighedLineReturnsPartOfItsWeight(t *testing.T) {
	sale, _ := returnedSale(t)
	lineID, _ := id.New()
	productID, _ := id.New()
	sale.Lines = []domain.Line{{
		ID: lineID, LineNo: 1, ProductID: productID, NameAR: "رز", UnitCode: "kg",
		QuantityMicro: 2_000_000, PriceCurrency: "SYP", UnitPriceMicro: 8_000_000_000,
		GrossLocalMinor: 16_000, GrossUSDMinor: 107, DiscountLocalMinor: 0, DiscountUSDMinor: 0,
		UnitCostMicro: 300_000, CostKnown: true, CostUSDMinor: 40, CostLocalMinor: 6_000,
	}}
	got, err := priceOne(t, sale, nil, domain.ReturnDraft{
		Lines:      []domain.ReturnLineDraft{{SaleLineID: lineID, Quantity: "0.5", Restock: false}},
		Settlement: domain.SettleCash, Reason: "رجّع نص كيلو",
	})
	if err != nil {
		t.Fatal(err)
	}
	// A quarter of 16,000.
	if got.RefundLocalMinor != 4_000 || got.Lines[0].QuantityMicro != 500_000 {
		t.Fatalf("half a kilo back = %d pounds, %d micro", got.RefundLocalMinor, got.Lines[0].QuantityMicro)
	}
	// Not restocked: refunded, but it does not go back on the shelf.
	if got.Lines[0].Restocked {
		t.Fatal("a damaged return was put back on the shelf")
	}
}
