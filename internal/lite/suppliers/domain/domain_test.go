package domain_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/suppliers/domain"
)

var currencies = []domain.Currency{{Code: "SYP", Decimals: 0}, {Code: "USD", Decimals: 2}}

func pid(t *testing.T) id.ID {
	t.Helper()
	v, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func codeOf(err error) string { return errs.CodeOf(err) }

func fieldOf(t *testing.T, err error) string {
	t.Helper()
	typed, ok := errs.AsError(err)
	if !ok || len(typed.Fields) == 0 {
		return ""
	}
	return typed.Fields[len(typed.Fields)-1].Field
}

// catalogue is cups by the jar (a count) and olive oil by the litre (three decimals).
func catalogue(t *testing.T) (map[id.ID]domain.Product, id.ID, id.ID) {
	cups, oil := pid(t), pid(t)
	return map[id.ID]domain.Product{
		cups: {ID: cups, NameAR: "فنجان قهوة", UnitCode: "jar", UnitDecimals: 0, Active: true},
		oil:  {ID: oil, NameAR: "زيت زيتون", UnitCode: "l", UnitDecimals: 3, Active: true},
	}, cups, oil
}

func TestASupplierIsANameAndHowToReachThem(t *testing.T) {
	s, err := domain.NewSupplier(pid(t), domain.Draft{Name: "  المروى  ", Phone: "٠٩٨٨ ٧٠٣ ٧٨٥", City: "حلب"})
	if err != nil || s.Name != "المروى" || s.Phone != "0988 703 785" || s.City != "حلب" || !s.Active || s.RowVersion != 1 {
		t.Fatalf("NewSupplier = %+v, %v", s, err)
	}
	for _, d := range []domain.Draft{{Name: ""}, {Name: "ـــ"}, {Name: "x", Phone: "call me"}} {
		if _, err := domain.NewSupplier(pid(t), d); err == nil {
			t.Errorf("%+v was taken", d)
		}
	}
}

// TestThePayablesBookAddsUp: what the shop owes goes up with a purchase and down with a payment, and a payment may take
// it below zero — the supplier then owes the shop, which a refund settles and nothing past it.
func TestThePayablesBookAddsUp(t *testing.T) {
	var p domain.Place
	purchase, err := p.Append(domain.Entry{Kind: domain.KindPurchase, AmountMinor: 10_800, PurchaseID: pid(t)})
	if err != nil || purchase.Seq != 1 || purchase.BalanceAfterMinor != 10_800 {
		t.Fatalf("purchase = %+v, %v", purchase, err)
	}
	p = domain.Place{Seq: 1, BalanceMinor: 10_800}
	pay, err := p.Append(domain.Entry{Kind: domain.KindPayment, AmountMinor: -12_000, Source: domain.SourceDrawer})
	if err != nil || pay.BalanceAfterMinor != -1_200 || pay.DrawerMinor() != -12_000 {
		t.Fatalf("an overpayment = %+v, %v", pay, err)
	}
	p = domain.Place{Seq: 2, BalanceMinor: -1_200}
	if _, err = p.Append(domain.Entry{Kind: domain.KindRefund, AmountMinor: 1_500, Source: domain.SourceOwner}); codeOf(err) != domain.CodeRefundTooLarge {
		t.Fatalf("a refund past what the supplier owes: %v", err)
	}
	refund, err := p.Append(domain.Entry{Kind: domain.KindRefund, AmountMinor: 1_200, Source: domain.SourceOwner})
	if err != nil || refund.BalanceAfterMinor != 0 || refund.DrawerMinor() != 0 {
		t.Fatalf("refund = %+v, %v — the owner's money never touches the drawer", refund, err)
	}
	if _, err := (domain.Place{}).Append(domain.Entry{Kind: domain.KindRefund, AmountMinor: 1, Source: domain.SourceDrawer}); codeOf(err) != domain.CodeNothingOwedBack {
		t.Fatalf("a refund from a supplier who owes nothing: %v", err)
	}
	if _, err := (domain.Place{}).Append(domain.Entry{Kind: domain.KindPayment, AmountMinor: -1}); codeOf(err) != domain.CodeCashSourceInvalid {
		t.Fatalf("a payment that does not say where the money came from: %v", err)
	}
	if _, err := (domain.Place{}).Append(domain.Entry{Kind: domain.KindOpening, AmountMinor: -500}); err != nil {
		t.Fatalf("an opening balance in the shop's favour: %v", err)
	}
}

// TestAReversalUndoesAPaymentAndItsDrawer: undoing a payment from the drawer puts the money back in the drawer's
// expected cash; a purchase is undone only by voiding it.
func TestAReversalUndoesAPaymentAndItsDrawer(t *testing.T) {
	payment := domain.Entry{ID: pid(t), Kind: domain.KindPayment, AmountMinor: -5_000, Source: domain.SourceDrawer}
	rev, err := domain.Reverse(payment, "typed twice", false)
	if err != nil || rev.AmountMinor != 5_000 || rev.Source != domain.SourceDrawer || rev.DrawerMinor() != 5_000 {
		t.Fatalf("Reverse = %+v, %v", rev, err)
	}
	if _, err := domain.Reverse(domain.Entry{Kind: domain.KindPurchase}, "x", false); codeOf(err) != domain.CodeNotReversible {
		t.Fatalf("a purchase reversed without voiding it: %v", err)
	}
	if _, err := domain.Reverse(payment, " ", false); codeOf(err) != domain.CodeReasonRequired {
		t.Fatalf("a reversal without a reason: %v", err)
	}
}

// TestTheModelInvoicesLines: six cups at 18 dollars, the owner's model invoice — 108 dollars, nothing damaged.
func TestTheModelInvoicesLines(t *testing.T) {
	products, cups, _ := catalogue(t)
	p, err := domain.Price(domain.Input{Currency: "USD", Lines: []domain.LineInput{{ProductID: cups, Quantity: "6", UnitCost: "18"}}}, products, currencies, "SYP")
	if err != nil || p.GrossMinor != 10_800 || p.DueMinor() != 10_800 || p.RateNano != 0 {
		t.Fatalf("Price = %+v, %v", p, err)
	}
}

// TestDamagedUnitsAreNotChargedFor: ten arrived, two damaged — the shop pays for eight, and eight enter stock.
func TestDamagedUnitsAreNotChargedFor(t *testing.T) {
	products, cups, _ := catalogue(t)
	p, err := domain.Price(domain.Input{Currency: "USD", Lines: []domain.LineInput{{ProductID: cups, Quantity: "10", Damaged: "2", UnitCost: "5"}}},
		products, currencies, "SYP")
	if err != nil || p.Lines[0].GoodMicro() != 8_000_000 || p.DueMinor() != 4_000 {
		t.Fatalf("Price = %+v, %v", p, err)
	}
	if _, err = domain.Price(domain.Input{Currency: "USD", Lines: []domain.LineInput{{ProductID: cups, Quantity: "2", Damaged: "3", UnitCost: "5"}}},
		products, currencies, "SYP"); codeOf(err) != domain.CodeDamagedTooMany || fieldOf(t, err) != "lines.1.damaged" {
		t.Fatalf("more damaged than arrived: %v on %q", err, fieldOf(t, err))
	}
	// Everything damaged is a line that costs nothing and brings nothing in — recorded, so the damage is on file.
	all, err := domain.Price(domain.Input{Currency: "USD", Lines: []domain.LineInput{{ProductID: cups, Quantity: "3", Damaged: "3", UnitCost: "5"}}},
		products, currencies, "SYP")
	if err != nil || all.DueMinor() != 0 || all.Lines[0].GoodMicro() != 0 {
		t.Fatalf("all damaged = %+v, %v", all, err)
	}
}

// TestDiscountsComeOffTheLineThenTheInvoiceExactly: a line's own discount first, as a percentage or an amount; then the
// invoice's, shared by largest remainder so the shares add to exactly the discount.
func TestDiscountsComeOffTheLineThenTheInvoiceExactly(t *testing.T) {
	products, cups, oil := catalogue(t)
	p, err := domain.Price(domain.Input{Currency: "USD", InvoiceDiscount: "10", Lines: []domain.LineInput{
		{ProductID: cups, Quantity: "10", UnitCost: "11.11", DiscountPercent: "10"}, // 111.10 − 11.11 = 99.99
		{ProductID: oil, Quantity: "12.5", UnitCost: "4", DiscountAmount: "0.01"},   // 50.00 − 0.01 = 49.99
	}}, products, currencies, "SYP")
	if err != nil {
		t.Fatal(err)
	}
	l1, l2 := p.Lines[0], p.Lines[1]
	if l1.GrossMinor != 11_110 || l1.LineDiscountMinor != 1_111 || l2.GrossMinor != 5_000 || l2.LineDiscountMinor != 1 {
		t.Fatalf("lines = %+v / %+v", l1, l2)
	}
	// 10.00 over 99.99 and 49.99: 6.67 and 3.33 — exactly 10.00 between them.
	if l1.InvoiceShareMinor+l2.InvoiceShareMinor != 1_000 || l1.InvoiceShareMinor != 667 {
		t.Fatalf("shares %d + %d", l1.InvoiceShareMinor, l2.InvoiceShareMinor)
	}
	if p.DueMinor() != l1.DueMinor()+l2.DueMinor() || p.DueMinor() != 13_998 {
		t.Fatalf("due %d, lines %d + %d", p.DueMinor(), l1.DueMinor(), l2.DueMinor())
	}
	for _, c := range []struct {
		line domain.LineInput
		code string
	}{
		{domain.LineInput{ProductID: cups, Quantity: "1", UnitCost: "5", DiscountPercent: "5", DiscountAmount: "1"}, domain.CodeDiscountBoth},
		{domain.LineInput{ProductID: cups, Quantity: "1", UnitCost: "5", DiscountPercent: "101"}, domain.CodeDiscountInvalid},
		{domain.LineInput{ProductID: cups, Quantity: "1", UnitCost: "5", DiscountAmount: "6"}, domain.CodeDiscountTooLarge},
	} {
		if _, err := domain.Price(domain.Input{Currency: "USD", Lines: []domain.LineInput{c.line}}, products, currencies, "SYP"); codeOf(err) != c.code {
			t.Errorf("%+v: %v, want %s", c.line, err, c.code)
		}
	}
	if _, err := domain.Price(domain.Input{Currency: "USD", InvoiceDiscount: "6", Lines: []domain.LineInput{{ProductID: cups, Quantity: "1", UnitCost: "5"}}},
		products, currencies, "SYP"); codeOf(err) != domain.CodeDiscountTooLarge {
		t.Fatalf("an invoice discount larger than the invoice: %v", err)
	}
}

// TestAPoundPurchaseCarriesItsRate: a purchase in the local currency says the rate it was paid at — the rate its cost
// is turned into dollars at, as a receipt is (L2).
func TestAPoundPurchaseCarriesItsRate(t *testing.T) {
	products, cups, _ := catalogue(t)
	in := domain.Input{Currency: "SYP", Lines: []domain.LineInput{{ProductID: cups, Quantity: "2", UnitCost: "15000"}}}
	if _, err := domain.Price(in, products, currencies, "SYP"); codeOf(err) != domain.CodeRateRequired {
		t.Fatalf("a pound purchase without a rate: %v", err)
	}
	in.Rate = "١٥٠٠٠"
	p, err := domain.Price(in, products, currencies, "SYP")
	if err != nil || p.RateNano != 15_000_000_000_000 || p.GrossMinor != 30_000 {
		t.Fatalf("Price = %+v, %v", p, err)
	}
}

// TestWhatCannotBeBoughtIsRefusedOnItsLine: an open-priced item has no stock, an inactive product must be reactivated
// first, and a quantity takes its unit's decimals — each refusal names its line.
func TestWhatCannotBeBoughtIsRefusedOnItsLine(t *testing.T) {
	products, cups, oil := catalogue(t)
	bag := pid(t)
	products[bag] = domain.Product{ID: bag, NameAR: "كيس", UnitCode: "piece", Active: true, OpenPrice: true}
	for _, c := range []struct {
		line  domain.LineInput
		code  string
		field string
	}{
		{domain.LineInput{ProductID: bag, Quantity: "1", UnitCost: "1"}, domain.CodeProductOpenPrice, "lines.2.productId"},
		{domain.LineInput{ProductID: pid(t), Quantity: "1", UnitCost: "1"}, domain.CodeProductUnknown, "lines.2.productId"},
		{domain.LineInput{ProductID: cups, Quantity: "1.5", UnitCost: "1"}, domain.CodeQuantityDecimals, "lines.2.quantity"},
		{domain.LineInput{ProductID: oil, Quantity: "0", UnitCost: "1"}, domain.CodeQuantityInvalid, "lines.2.quantity"},
		{domain.LineInput{ProductID: oil, Quantity: "1", UnitCost: "-1"}, domain.CodeCostInvalid, "lines.2.unitCost"},
	} {
		in := domain.Input{Currency: "USD", Lines: []domain.LineInput{{ProductID: cups, Quantity: "1", UnitCost: "1"}, c.line}}
		if _, err := domain.Price(in, products, currencies, "SYP"); codeOf(err) != c.code || fieldOf(t, err) != c.field {
			t.Errorf("%+v: %v on %q, want %s on %s", c.line, err, fieldOf(t, err), c.code, c.field)
		}
	}
}

// TestMoneyPaidAtThePurchaseSaysWhereItCameFrom, and a figure too large is refused, never read as nothing.
func TestMoneyPaidAtThePurchaseSaysWhereItCameFrom(t *testing.T) {
	products, cups, _ := catalogue(t)
	in := domain.Input{Currency: "USD", PaidNow: "50", Lines: []domain.LineInput{{ProductID: cups, Quantity: "6", UnitCost: "18"}}}
	if _, err := domain.Price(in, products, currencies, "SYP"); codeOf(err) != domain.CodePaidFromRequired {
		t.Fatalf("money paid from nowhere: %v", err)
	}
	in.PaidFrom = "drawer"
	if p, err := domain.Price(in, products, currencies, "SYP"); err != nil || p.PaidNowMinor != 5_000 || p.PaidFrom != domain.SourceDrawer {
		t.Fatalf("Price = %+v, %v", p, err)
	}
	huge := domain.Input{Currency: "USD", Lines: []domain.LineInput{{ProductID: cups, Quantity: "999999999999", UnitCost: "999999999999"}}}
	if _, err := domain.Price(huge, products, currencies, "SYP"); codeOf(err) != domain.CodeAmountTooLarge {
		t.Fatalf("an out-of-range purchase: %v", err)
	}
}

func TestANetUnitCostIsTheDueOverTheGoodUnits(t *testing.T) {
	l := domain.Line{QuantityMicro: 10_000_000, DamagedMicro: 2_000_000, GrossMinor: 1_600, LineDiscountMinor: 160, InvoiceShareMinor: 40}
	if got := l.NetUnitMicro(2); got != 1_750_000 {
		t.Fatalf("NetUnitMicro = %d, want $1.75", got)
	}
	// Three jars for 10,000 pounds: 3,333.333333 each, half up at the sixth decimal.
	pounds := domain.Line{QuantityMicro: 3_000_000, GrossMinor: 10_000}
	if got := pounds.NetUnitMicro(0); got != 3_333_333_333 {
		t.Fatalf("NetUnitMicro = %d", got)
	}
	if got := (domain.Line{QuantityMicro: 1_000_000, DamagedMicro: 1_000_000}).NetUnitMicro(2); got != 0 {
		t.Fatalf("a line that all arrived broken costs %d a unit", got)
	}
}
